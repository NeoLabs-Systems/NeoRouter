package manager

// The migration of a Node.js install: a fake legacy installation in a temporary
// home, migrated the way the CLI migrates a real one.

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NeoLabs-Systems/neolabs-go/cli/service"
	"github.com/NeoLabs-Systems/neolabs-go/secretbox"
	"github.com/NeoLabs-Systems/neorouter/internal/backup"
	"github.com/NeoLabs-Systems/neorouter/internal/config"
	"github.com/NeoLabs-Systems/neorouter/internal/providers"
)

// legacySeal writes a value exactly the way server/utils/crypto.js
// encryptString did: AES-256-GCM under the raw secret.key bytes, base64url of
// iv(12) | tag(16) | ciphertext, no prefix. It is written out here on purpose, so
// the test proves the format rather than trusting the code under test.
func legacySeal(t *testing.T, key []byte, plaintext string) string {
	t.Helper()
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatalf("legacy cipher: %v", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatalf("legacy gcm: %v", err)
	}
	iv := make([]byte, 12)
	if _, err := rand.Read(iv); err != nil {
		t.Fatalf("legacy iv: %v", err)
	}
	sealed := aead.Seal(nil, iv, []byte(plaintext), nil)
	payload := make([]byte, 0, 12+len(sealed))
	payload = append(payload, iv...)
	payload = append(payload, sealed[len(sealed)-16:]...)
	payload = append(payload, sealed[:len(sealed)-16]...)
	return base64.RawURLEncoding.EncodeToString(payload)
}

// legacyInstall is one fake Node.js installation and what it holds in clear.
type legacyInstall struct {
	secretKey   []byte
	providerKey string
	twoFactor   string
	pending     string
	poolKey     string
	poolBoxKey  string
	proxyURL    string
	aaKey       string
	userSecret  string
}

// fakeLegacyInstall builds the Node.js installation: a database with legacy
// values, a systemd unit that runs server/index.js, a PID file and an npm-style
// `neorouter` symlink.
func fakeLegacyInstall(t *testing.T, s *testSetup) *legacyInstall {
	t.Helper()
	cfg := s.config(t)
	install := &legacyInstall{
		secretKey:   make([]byte, providers.SecretKeySize),
		providerKey: "sk-provider-legacy",
		twoFactor:   "JBSWY3DPEHPK3PXP",
		pending:     "KRSXG5CTMVRXEZLU",
		poolKey:     "MIGHAgEAMBMGByqGSM49AgEGCCqGSM49AwEHBG0wawIBAQQg",
		poolBoxKey:  "MC4CAQAwBQYDK2VuBCIEIHS8",
		proxyURL:    "socks5://user:pass@127.0.0.1:1080",
		aaKey:       "aa-legacy-key",
		userSecret:  "user-legacy-secret",
	}
	if _, err := rand.Read(install.secretKey); err != nil {
		t.Fatalf("secret key: %v", err)
	}
	if err := os.MkdirAll(cfg.Paths.Data, 0o700); err != nil {
		t.Fatalf("create data dir: %v", err)
	}
	if err := os.WriteFile(cfg.Paths.SecretKey, install.secretKey, 0o600); err != nil {
		t.Fatalf("write secret.key: %v", err)
	}
	if err := os.WriteFile(cfg.Paths.EnvFile, []byte("NEOROUTER_PORT=4600\nNEOROUTER_APP_DIR=/old/node/checkout\n"), 0o600); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	db, err := s.m.openDB(context.Background(), cfg)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()
	statements := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO users (id, username, email, password_hash, role) VALUES ('u-ada', 'ada', 'ada@example.com', 'x', 'admin')`, nil},
		{`INSERT INTO provider_accounts (id, user_id, definition_id, slug, label, secret_encrypted, billing_mode)
			VALUES ('a-1', 'u-ada', 'openai', 'openai', 'OpenAI', ?, 'paid')`,
			[]any{legacySeal(t, install.secretKey, install.providerKey)}},
		{`INSERT INTO user_two_factor (user_id, secret_encrypted, pending_secret_encrypted, enabled) VALUES ('u-ada', ?, ?, 1)`,
			[]any{legacySeal(t, install.secretKey, install.twoFactor), legacySeal(t, install.secretKey, install.pending)}},
		{`INSERT INTO pool_identities (user_id, member_id, public_key, private_key_encrypted, box_private_key_encrypted)
			VALUES ('u-ada', 'm-1', 'pub', ?, ?)`,
			[]any{legacySeal(t, install.secretKey, install.poolKey), legacySeal(t, install.secretKey, install.poolBoxKey)}},
		{`INSERT INTO outbound_proxies (id, fingerprint, endpoint, secret_encrypted) VALUES ('p-1', 'fp', '127.0.0.1:1080', ?)`,
			[]any{legacySeal(t, install.secretKey, install.proxyURL)}},
		{`INSERT INTO settings (key, value) VALUES ('artificial_analysis_key', ?)`,
			[]any{jsonString(t, legacySeal(t, install.secretKey, install.aaKey))}},
		{`INSERT INTO user_settings (user_id, key, value) VALUES ('u-ada', 'openai_key', ?)`,
			[]any{jsonString(t, legacySeal(t, install.secretKey, install.userSecret))}},
		{`INSERT INTO user_settings (user_id, key, value) VALUES ('u-ada', 'smoothing', 'true')`, nil},
	}
	for _, statement := range statements {
		if _, err := db.W.ExecContext(context.Background(), statement.query, statement.args...); err != nil {
			t.Fatalf("seed %q: %v", statement.query, err)
		}
	}

	unitPath := config.SystemdUnitPath(s.home)
	if err := os.MkdirAll(filepath.Dir(unitPath), 0o700); err != nil {
		t.Fatalf("create unit dir: %v", err)
	}
	unit := "[Service]\nExecStart=/usr/bin/node /old/node/checkout/server/index.js\n"
	if err := os.WriteFile(unitPath, []byte(unit), 0o600); err != nil {
		t.Fatalf("write unit: %v", err)
	}
	if err := os.WriteFile(cfg.Paths.PIDFile, []byte("999999\n"), 0o600); err != nil {
		t.Fatalf("write pid file: %v", err)
	}
	linkDir := filepath.Join(s.home, "npm-bin")
	if err := os.MkdirAll(linkDir, 0o700); err != nil {
		t.Fatalf("create npm bin: %v", err)
	}
	if err := os.Symlink("/old/node/checkout/bin/neorouter.js", filepath.Join(linkDir, AppCommand)); err != nil {
		t.Fatalf("create npm symlink: %v", err)
	}
	s.env["PATH"] = linkDir + string(os.PathListSeparator) + "/usr/bin:/bin"
	return install
}

// jsonString encodes a value the way JSON.stringify does for a string setting.
func jsonString(t *testing.T, value string) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("encode %q: %v", value, err)
	}
	return string(encoded)
}

// recordingRunner answers the service manager's commands without touching the
// machine.
type recordingRunner struct {
	calls []string
}

func (r *recordingRunner) Run(name string, args ...string) (string, error) {
	r.calls = append(r.calls, name+" "+strings.Join(args, " "))
	return "", nil
}

func (r *recordingRunner) Exists(name string) bool { return true }

func TestInspectLegacyJSRecognisesANodeInstallation(t *testing.T) {
	s := newTestSetup(t)
	fakeLegacyInstall(t, s)
	cfg := s.config(t)
	state := InspectLegacyJS(cfg, s.home, s.m.Getenv)
	if !state.HasDB || !state.LegacySystemd {
		t.Fatalf("state = %+v, want a database and a Node.js unit", state)
	}
	if state.LegacyNPMBin == "" {
		t.Error("the npm symlink was not found")
	}
	if state.MigratedMarker {
		t.Error("a fresh legacy install already had the migration marker")
	}
	if !state.NeedsMigration {
		t.Error("NeedsMigration = false for a Node.js installation")
	}
}

func TestMigrationResealsEverythingAndLeavesAMarker(t *testing.T) {
	s := newTestSetup(t)
	install := fakeLegacyInstall(t, s)
	cfg := s.config(t)
	runner := &recordingRunner{}
	s.m.ServiceFactory = func(spec config.Config) (*service.Manager, error) {
		return &service.Manager{
			Spec:      service.Spec{Name: AppCommand, LogDir: spec.Paths.Logs, LogName: AppCommand, PIDFile: spec.Paths.PIDFile},
			Platform:  "linux",
			Runner:    runner,
			UnitPath:  config.SystemdUnitPath(s.home),
			PlistPath: config.LaunchAgentPlistPath(s.home),
		}, nil
	}

	migrated, err := s.m.migrateLegacyIfNeeded(context.Background(), cfg)
	if err != nil {
		t.Fatalf("migrateLegacyIfNeeded: %v", err)
	}

	// The marker records the version and the backup.
	marker, err := os.ReadFile(MarkerPath(migrated))
	if err != nil {
		t.Fatalf("read marker: %v", err)
	}
	var recorded MigratedMarker
	if err := json.Unmarshal(marker, &recorded); err != nil {
		t.Fatalf("decode marker: %v", err)
	}
	if recorded.Version == "" || recorded.MigratedAt == "" || recorded.BackupKey == "" {
		t.Fatalf("marker = %+v, want the version, the time and the backup", recorded)
	}

	// The backup holds the database and the settings, so a rollback is possible.
	backupPath := filepath.Join(cfg.Paths.Backups, recorded.BackupKey)
	for _, name := range []string{".env", filepath.Join("data", "neorouter.sqlite3"), filepath.Join("data", "secret.key")} {
		if !exists(filepath.Join(backupPath, name)) {
			t.Errorf("the backup is missing %s", name)
		}
	}
	// The legacy command is gone and the stale app directory was cleared.
	if exists(filepath.Join(s.home, "npm-bin", AppCommand)) {
		t.Error("the npm symlink survived the migration")
	}
	envRaw, err := os.ReadFile(cfg.Paths.EnvFile)
	if err != nil {
		t.Fatalf("read .env: %v", err)
	}
	if strings.Contains(string(envRaw), "NEOROUTER_APP_DIR") {
		t.Errorf(".env still points at the Node.js checkout: %q", envRaw)
	}

	// Every legacy value is sealed with secretbox now, and still the same text.
	box, err := secretbox.New(providers.SealedPrefix, hex.EncodeToString(install.secretKey))
	if err != nil {
		t.Fatalf("build box: %v", err)
	}
	db, err := s.m.openDB(context.Background(), cfg)
	if err != nil {
		t.Fatalf("reopen database: %v", err)
	}
	defer db.Close()
	checks := []struct {
		query string
		want  string
	}{
		{`SELECT secret_encrypted FROM provider_accounts WHERE id='a-1'`, install.providerKey},
		{`SELECT secret_encrypted FROM user_two_factor WHERE user_id='u-ada'`, install.twoFactor},
		{`SELECT pending_secret_encrypted FROM user_two_factor WHERE user_id='u-ada'`, install.pending},
		{`SELECT private_key_encrypted FROM pool_identities WHERE user_id='u-ada'`, install.poolKey},
		{`SELECT box_private_key_encrypted FROM pool_identities WHERE user_id='u-ada'`, install.poolBoxKey},
		{`SELECT secret_encrypted FROM outbound_proxies WHERE id='p-1'`, install.proxyURL},
	}
	for _, check := range checks {
		var stored string
		if err := db.R.QueryRowContext(context.Background(), check.query).Scan(&stored); err != nil {
			t.Fatalf("%s: %v", check.query, err)
		}
		if !strings.HasPrefix(stored, providers.SealedPrefix) {
			t.Errorf("%s = %q, want a sealed value", check.query, stored)
			continue
		}
		plain, err := box.OpenSealed(stored)
		if err != nil {
			t.Errorf("%s does not open with secretbox: %v", check.query, err)
			continue
		}
		if plain != check.want {
			t.Errorf("%s = %q, want %q", check.query, plain, check.want)
		}
	}
	for _, check := range []struct {
		table, key, want string
	}{
		{"settings", "artificial_analysis_key", install.aaKey},
		{"user_settings", "openai_key", install.userSecret},
	} {
		var stored string
		query := `SELECT value FROM ` + check.table + ` WHERE key=?`
		args := []any{check.key}
		if check.table == "user_settings" {
			query = `SELECT value FROM user_settings WHERE user_id='u-ada' AND key=?`
		}
		if err := db.R.QueryRowContext(context.Background(), query, args...).Scan(&stored); err != nil {
			t.Fatalf("read %s.%s: %v", check.table, check.key, err)
		}
		var inner string
		if err := json.Unmarshal([]byte(stored), &inner); err != nil {
			t.Fatalf("%s is not a JSON string: %v", check.key, err)
		}
		if !strings.HasPrefix(inner, providers.SealedPrefix) {
			t.Errorf("%s = %q, want a sealed JSON string", check.key, inner)
			continue
		}
		plain, err := box.OpenSealed(inner)
		if err != nil || plain != check.want {
			t.Errorf("%s opens as %q, %v; want %q", check.key, plain, err, check.want)
		}
	}

	// The plain settings were left exactly as they were.
	var smoothing string
	if err := db.R.QueryRowContext(context.Background(),
		`SELECT value FROM user_settings WHERE user_id='u-ada' AND key='smoothing'`).Scan(&smoothing); err != nil {
		t.Fatalf("read smoothing: %v", err)
	}
	if smoothing != "true" {
		t.Errorf("smoothing = %q, want it untouched", smoothing)
	}

	// Running it again changes nothing: the marker stops the migration.
	before := string(marker)
	again, err := s.m.migrateLegacyIfNeeded(context.Background(), cfg)
	if err != nil {
		t.Fatalf("second migration: %v", err)
	}
	after, err := os.ReadFile(MarkerPath(again))
	if err != nil {
		t.Fatalf("read marker again: %v", err)
	}
	if string(after) != before {
		t.Errorf("a second run rewrote the marker:\n%s\n%s", before, after)
	}
}

func TestMigrationBacksUpBeforeReSealing(t *testing.T) {
	s := newTestSetup(t)
	fakeLegacyInstall(t, s)
	cfg := s.config(t)
	state := LegacyJSState{HasDB: true}
	if _, err := s.m.migrateLegacyJSState(context.Background(), cfg, state); err != nil {
		t.Fatalf("migrateLegacyJSState: %v", err)
	}
	items, err := backup.List(cfg.Paths.Backups)
	if err != nil {
		t.Fatalf("list backups: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("backups = %+v, want exactly one", items)
	}
	if items[0].Key == "" || !strings.HasPrefix(items[0].Key, "pre-update-") {
		t.Errorf("backup key = %q", items[0].Key)
	}
}

func TestMigratedMarkerIsWrittenOnce(t *testing.T) {
	s := newTestSetup(t)
	cfg := s.config(t)
	if err := WriteMigratedMarker(cfg, "0.2.1", "pre-update-20260101T000000Z", "test"); err != nil {
		t.Fatalf("WriteMigratedMarker: %v", err)
	}
	raw, err := os.ReadFile(MarkerPath(cfg))
	if err != nil {
		t.Fatalf("read marker: %v", err)
	}
	for _, want := range []string{`"migratedAt"`, `"version": "0.2.1"`, `"backupKey": "pre-update-20260101T000000Z"`, `"note": "test"`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("marker is missing %s:\n%s", want, raw)
		}
	}
	info, err := os.Stat(MarkerPath(cfg))
	if err != nil {
		t.Fatalf("stat marker: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("marker mode = %v, want 0600", info.Mode())
	}
}

func TestServiceSpecRunsTheGoBinaryNotNode(t *testing.T) {
	s := newTestSetup(t)
	fakeLegacyInstall(t, s)
	cfg := s.config(t)
	binary := filepath.Join(cfg.Paths.Home, "app", "versions", "0.2.1", "bin", "neorouter")
	sm, err := NewServiceManager(cfg, binary, "/usr/bin:/bin")
	if err != nil {
		t.Fatalf("NewServiceManager: %v", err)
	}
	unit := service.RenderSystemdUnit(sm.Spec)
	if !strings.Contains(unit, `ExecStart="`+binary+`" "serve"`) {
		t.Errorf("the unit does not run the Go binary:\n%s", unit)
	}
	if strings.Contains(unit, "server/index.js") {
		t.Errorf("the unit still runs the Node server:\n%s", unit)
	}
	if !strings.Contains(unit, "EnvironmentFile=-"+cfg.Paths.EnvFile) {
		t.Errorf("the unit does not load the install's .env:\n%s", unit)
	}
	if !strings.Contains(unit, `Environment=NEOROUTER_HOME="`+cfg.Paths.Home+`"`) {
		t.Errorf("the unit has no NEOROUTER_HOME:\n%s", unit)
	}
	agent := service.RenderLaunchAgent(sm.Spec)
	if !strings.Contains(agent, "<string>"+binary+"</string>") || !strings.Contains(agent, "<string>serve</string>") {
		t.Errorf("the launch agent does not run the binary:\n%s", agent)
	}
}
