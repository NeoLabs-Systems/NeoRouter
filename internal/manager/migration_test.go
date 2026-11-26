package manager

// Upgrading a Node.js install, from a database the Node.js server wrote
// (testdata/node.sql): the upgrade, a failed one that puts everything back, and
// a second run.

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/NeoLabs-Systems/neorouter/internal/backup"
	"github.com/NeoLabs-Systems/neorouter/internal/config"
	"github.com/NeoLabs-Systems/neorouter/internal/secrets"
	"github.com/NeoLabs-Systems/neorouter/internal/updates"
)

// fixtureKey seals the values in testdata/node.sql.
const fixtureKey = "neorouter-test-fixture-key-00000"

const nodeVersion = "0.2.2-beta.1"

type nodeInstall struct {
	cfg      config.Config
	service  *fakeService
	unit     string
	env      string
	npmLink  string
	checkout string
	sealed   string
}

func queryString(t *testing.T, database, query string) string {
	t.Helper()
	db, err := sql.Open("sqlite", database)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var out string
	if err := db.QueryRow(query).Scan(&out); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return out
}

// newNodeInstall lays out what the Node.js CLI left: its database, secret.key,
// .env, a service definition running server/index.js (and "running"), the npm
// command link and a local model runtime with a symlinked library.
func newNodeInstall(t *testing.T, s *testSetup) *nodeInstall {
	t.Helper()
	node := &nodeInstall{service: newFakeService(t, s)}
	cfg := s.config(t)
	node.cfg = cfg
	node.checkout = filepath.Join(s.home, "NeoRouter")
	writeFile(t, filepath.Join(node.checkout, "server", "index.js"), "// node", 0o644)
	writeFile(t, filepath.Join(node.checkout, "bin", "neorouter.js"), "// node", 0o755)

	writeFile(t, cfg.Paths.SecretKey, fixtureKey, 0o600)
	db, err := sql.Open("sqlite", "file:"+cfg.Paths.Database+"?_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(readFile(t, filepath.Join("testdata", "node.sql"))); err != nil {
		t.Fatalf("load the Node.js database: %v", err)
	}
	db.Close()
	node.sealed = queryString(t, cfg.Paths.Database, `SELECT secret_encrypted FROM provider_accounts`)

	node.env = "NEOROUTER_PORT=" + strconv.Itoa(node.service.port) + "\nNEOROUTER_APP_DIR=" + node.checkout + "\n"
	writeFile(t, cfg.Paths.EnvFile, node.env, 0o600)
	node.unit = "ProgramArguments /usr/local/bin/node " + node.checkout + "/server/index.js\n"
	writeFile(t, serviceFile(s.home), node.unit, 0o600)

	node.npmLink = filepath.Join(s.home, "npm-bin", AppCommand)
	if err := os.MkdirAll(filepath.Dir(node.npmLink), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(node.checkout, "bin", "neorouter.js"), node.npmLink); err != nil {
		t.Fatal(err)
	}
	s.env["PATH"] = filepath.Dir(node.npmLink) + string(os.PathListSeparator) + "/usr/bin:/bin"

	runtimes := filepath.Join(cfg.Paths.Data, "local-models", "runtimes")
	writeFile(t, filepath.Join(runtimes, "libllama.1.dylib"), "lib", 0o755)
	if err := os.Symlink("libllama.1.dylib", filepath.Join(runtimes, "libllama.dylib")); err != nil {
		t.Fatal(err)
	}

	// The fake service runs whatever its definition names.
	node.service.version = func() string {
		if strings.Contains(readFile(t, serviceFile(s.home)), "server/index.js") {
			return nodeVersion
		}
		return node.service.activeVersion()
	}
	node.service.run()
	return node
}

func migrationBackups(t *testing.T, cfg config.Config) []backup.Artifact {
	t.Helper()
	items, err := backup.List(cfg.Paths.Backups)
	if err != nil {
		t.Fatal(err)
	}
	var out []backup.Artifact
	for _, item := range items {
		if item.Kind == backup.KindMigration {
			out = append(out, item)
		}
	}
	return out
}

func TestInspectLegacyJSRecognisesANodeInstallation(t *testing.T) {
	s := newTestSetup(t)
	node := newNodeInstall(t, s)
	state := InspectLegacyJS(node.cfg, s.home, s.m.Getenv)
	if !state.NodeDB || !(state.LegacySystemd || state.LegacyPlist) || state.LegacyNPMBin != node.npmLink || !state.NeedsMigration {
		t.Fatalf("state = %+v", state)
	}
}

func TestUpgradeOfANodeInstall(t *testing.T) {
	keys := newKeypair(t)
	s := newTestSetup(t)
	node := newNodeInstall(t, s)
	newReleases(t, keys, "0.9.0").use(s, keys)
	cfg := node.cfg

	out, err := runCLI(t, s, "install")
	if err != nil {
		t.Fatalf("install: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Detected an existing NeoRouter installation") {
		t.Errorf("install did not say it upgrades:\n%s", out)
	}
	var marker MigratedMarker
	if err := json.Unmarshal([]byte(readFile(t, MarkerPath(cfg))), &marker); err != nil {
		t.Fatal(err)
	}
	saved := migrationBackups(t, cfg)
	if len(saved) != 1 || marker.BackupKey != saved[0].Key || marker.Version != "0.9.0" {
		t.Fatalf("marker = %+v, backups = %+v", marker, saved)
	}
	// The copy is from before anything changed.
	copied := filepath.Join(saved[0].Path, "data", "neorouter.sqlite3")
	if got := queryString(t, copied, `PRAGMA user_version`); got != "0" {
		t.Errorf("the backup is at schema version %s", got)
	}
	if got := queryString(t, copied, `SELECT secret_encrypted FROM provider_accounts`); got != node.sealed {
		t.Error("the backup holds a re-sealed value")
	}
	if got := readFile(t, filepath.Join(saved[0].Path, ".env")); got != node.env {
		t.Errorf("the backed up .env = %q", got)
	}

	sealer, err := secrets.New([]byte(fixtureKey))
	if err != nil {
		t.Fatal(err)
	}
	for query, want := range map[string]string{
		`SELECT secret_encrypted FROM provider_accounts`:                            "fixture-provider-key",
		`SELECT secret_encrypted FROM user_two_factor`:                              "JBSWY3DPEHPK3PXP",
		`SELECT json_extract(value,'$') FROM settings WHERE key LIKE 'artificial%'`: "fixture-analysis-key",
	} {
		stored := queryString(t, cfg.Paths.Database, query)
		if plain, err := sealer.Open(stored); !strings.HasPrefix(stored, secrets.Prefix) || err != nil || plain != want {
			t.Errorf("%s = %q (%v), want %q sealed", query, plain, err, want)
		}
	}
	if got := queryString(t, cfg.Paths.Database, `SELECT json_extract(sess,'$.userId') FROM sessions`); got != "u-fixture" {
		t.Errorf("the signed-in session was lost: %q", got)
	}

	env := readFile(t, cfg.Paths.EnvFile)
	if strings.Contains(env, "NEOROUTER_APP_DIR") || !strings.Contains(env, updates.ChannelEnvKey+"=beta") {
		t.Errorf(".env = %q, want the Node.js app folder gone and the beta channel kept", env)
	}
	unit := readFile(t, serviceFile(s.home))
	if strings.Contains(unit, "server/index.js") || !strings.Contains(unit, filepath.Join("versions", "0.9.0", "app", "bin", "neorouter")) {
		t.Errorf("service definition:\n%s", unit)
	}
	if _, err := os.Lstat(node.npmLink); !os.IsNotExist(err) {
		t.Error("the npm command survived")
	}
	if launcher := readFile(t, s.m.launcherPath()); !isOwnLauncher([]byte(launcher)) {
		t.Errorf("no neorouter command replaced the npm link: %q", launcher)
	}

	// A second run neither migrates nor saves another migration copy.
	before := readFile(t, MarkerPath(cfg))
	out, err = runCLI(t, s, "install")
	if err != nil {
		t.Fatalf("second install: %v\n%s", err, out)
	}
	if strings.Contains(out, "Detected an existing") || len(migrationBackups(t, cfg)) != 1 || readFile(t, MarkerPath(cfg)) != before {
		t.Errorf("the second run migrated again:\n%s", out)
	}
	node.service.calls = nil
	out, err = runCLI(t, s, "update")
	if err != nil || !strings.Contains(out, "You have the latest version (0.9.0)") {
		t.Fatalf("update after the upgrade: %v\n%s", err, out)
	}
	for _, call := range node.service.calls {
		if strings.Contains(call, "stop") || strings.Contains(call, "bootout") {
			t.Errorf("an up-to-date update stopped the server: %s", call)
		}
	}
	if !node.service.running() {
		t.Error("the server is not running")
	}
}

func TestFailedUpgradePutsTheNodeInstallBack(t *testing.T) {
	keys := newKeypair(t)
	s := newTestSetup(t)
	node := newNodeInstall(t, s)
	newReleases(t, keys, "0.9.0").use(s, keys)
	cfg := node.cfg
	// The new server never answers.
	node.service.version = func() string {
		if strings.Contains(readFile(t, serviceFile(s.home)), "server/index.js") {
			return nodeVersion
		}
		return ""
	}
	s.m.RuntimeStartupTimeout = 300e6

	out, err := runCLI(t, s, "install")
	if err == nil || !strings.Contains(err.Error(), "0.9.0") {
		t.Fatalf("err = %v\n%s", err, out)
	}
	if got := queryString(t, cfg.Paths.Database, `PRAGMA user_version`); got != "0" {
		t.Errorf("schema version = %s, want the Node.js database back", got)
	}
	if got := queryString(t, cfg.Paths.Database, `SELECT secret_encrypted FROM provider_accounts`); got != node.sealed {
		t.Error("the provider key stayed re-sealed")
	}
	if got := queryString(t, cfg.Paths.Database, `SELECT COUNT(*) FROM user_sessions`); got != "1" {
		t.Errorf("sessions = %s, want the signed-in session back", got)
	}
	if got := readFile(t, cfg.Paths.EnvFile); got != node.env {
		t.Errorf(".env = %q, want %q", got, node.env)
	}
	if got := readFile(t, serviceFile(s.home)); got != node.unit {
		t.Errorf("service definition = %q", got)
	}
	if target, err := os.Readlink(node.npmLink); err != nil || target != filepath.Join(node.checkout, "bin", "neorouter.js") {
		t.Errorf("npm link = %q, %v", target, err)
	}
	if exists(MarkerPath(cfg)) {
		t.Error("a failed upgrade left the migration marker")
	}
	if exists(filepath.Join(RuntimeStore(cfg).Root, "current.json")) {
		t.Error("a failed upgrade left a runtime active")
	}
	if health, ok := fetchHealth(context.Background(), node.service.port); !ok || health["version"] != nodeVersion {
		t.Errorf("the old server is not answering again: %v %v", health, ok)
	}
	if len(migrationBackups(t, cfg)) != 1 {
		t.Error("the pre-migration backup is gone")
	}
	if !InspectLegacyJS(cfg, s.home, s.m.Getenv).NeedsMigration {
		t.Error("the install no longer looks like Node.js, so it would never be upgraded")
	}
}

func TestUpgradeChangesNothingWhenTheDownloadIsRefused(t *testing.T) {
	keys := newKeypair(t)
	s := newTestSetup(t)
	node := newNodeInstall(t, s)
	newReleases(t, keys, "0.9.0").use(s, newKeypair(t))

	if _, err := runCLI(t, s, "update"); err == nil || !strings.Contains(err.Error(), "signature") {
		t.Fatalf("err = %v, want a signature failure", err)
	}
	if len(node.service.calls) > 1 || !node.service.running() {
		t.Errorf("the old server was touched: %v", node.service.calls)
	}
	if readFile(t, node.cfg.Paths.EnvFile) != node.env || len(migrationBackups(t, node.cfg)) != 0 {
		t.Error("a refused download changed the install")
	}
}

func TestCommandsThatWouldBypassTheUpgradeRefuseANodeInstall(t *testing.T) {
	s := newTestSetup(t)
	node := newNodeInstall(t, s)
	for _, args := range [][]string{{"setup"}, {"start"}, {"restart"}, {"reset-2fa", "fixture"}} {
		if _, err := runCLI(t, s, args...); err == nil || !strings.Contains(err.Error(), "neorouter install") {
			t.Errorf("%v: err = %v, want a pointer to install", args, err)
		}
	}
	if readFile(t, node.cfg.Paths.EnvFile) != node.env || queryString(t, node.cfg.Paths.Database, `PRAGMA user_version`) != "0" {
		t.Error("a refused command changed the install")
	}
}

func TestAFreshInstallIsNeverLegacy(t *testing.T) {
	keys := newKeypair(t)
	s := newTestSetup(t)
	fake := newFakeService(t, s)
	newReleases(t, keys, "0.9.0").use(s, keys)

	out, err := runCLI(t, s, "install", "--channel", "beta")
	if err != nil {
		t.Fatalf("install: %v\n%s", err, out)
	}
	cfg := s.config(t)
	if marker, err := RuntimeStore(cfg).Current(); err != nil || marker.Version != "0.9.0" {
		t.Fatalf("the install did not download the signed runtime: %+v %v", marker, err)
	}
	if !strings.Contains(readFile(t, cfg.Paths.EnvFile), updates.ChannelEnvKey+"=beta") {
		t.Error("install --channel was ignored")
	}
	if InspectLegacyJS(cfg, s.home, s.m.Getenv).NeedsMigration {
		t.Error("a fresh Go install looks like Node.js")
	}
	if exists(MarkerPath(cfg)) || len(migrationBackups(t, cfg)) != 0 {
		t.Error("a fresh install was migrated")
	}
	if !HasBundledWebClient(cfg.WebDir) || !fake.running() {
		t.Error("the installed app is not being served")
	}
}
