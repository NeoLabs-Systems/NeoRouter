package settings

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/NeoLabs-Systems/neolabs-go/dbx"
	"github.com/NeoLabs-Systems/neolabs-go/secretbox"
	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
	"github.com/NeoLabs-Systems/neorouter/internal/schema"
)

// testDB opens a real SQLite database with the baseline schema in a temp dir.
func testDB(t *testing.T) *dbx.DB {
	t.Helper()
	ctx := context.Background()
	db, err := dbx.Open(ctx, filepath.Join(t.TempDir(), "neorouter.sqlite3"), dbx.Options{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := schema.Ensure(ctx, db.W); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	return db
}

// testCrypt returns the sealer the app builds from the install's secret key.
func testCrypt(t *testing.T) *secretbox.Box {
	t.Helper()
	box, err := secretbox.New("enc:v1:", "settings-test-key-material")
	if err != nil {
		t.Fatalf("build sealer: %v", err)
	}
	return box
}

func testService(t *testing.T) *Service {
	t.Helper()
	return &Service{DB: testDB(t), Log: slog.New(slog.DiscardHandler), Sealer: testCrypt(t)}
}

// exec runs one statement, failing the test on error.
func exec(t *testing.T, db *dbx.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.W.ExecContext(context.Background(), query, args...); err != nil {
		t.Fatalf("exec %q: %v", query, err)
	}
}

// httpError returns the *HttpError inside err, for asserting codes and texts.
func httpError(t *testing.T, err error) *httpserver.HttpError {
	t.Helper()
	var target *httpserver.HttpError
	if !errors.As(err, &target) {
		t.Fatalf("expected an *httpserver.HttpError, got %v", err)
	}
	return target
}

func TestGlobalSettings(t *testing.T) {
	svc := testService(t)
	ctx := context.Background()

	if _, found, err := svc.GetGlobal(ctx, KeyRegistrationOpen); err != nil || found {
		t.Fatalf("missing key: found=%v err=%v", found, err)
	}
	if open, err := svc.RegistrationOpen(ctx); err != nil || open {
		t.Fatalf("registration closed by default, got %v (err %v)", open, err)
	}

	if err := svc.SetGlobal(ctx, KeyRegistrationOpen, true); err != nil {
		t.Fatalf("set registration_open: %v", err)
	}
	raw, found, err := svc.GetGlobal(ctx, KeyRegistrationOpen)
	if err != nil || !found {
		t.Fatalf("read registration_open: found=%v err=%v", found, err)
	}
	if string(raw) != "true" {
		t.Fatalf("stored JSON = %s, want true", raw)
	}
	if open, err := svc.RegistrationOpen(ctx); err != nil || !open {
		t.Fatalf("registration should be open, got %v (err %v)", open, err)
	}

	// Only the JSON literal true opens registration: registrationOpen() is
	// getGlobal('registration_open', false) === true.
	for _, value := range []any{"true", 1, map[string]bool{"open": true}} {
		if err := svc.SetGlobal(ctx, KeyRegistrationOpen, value); err != nil {
			t.Fatalf("set %v: %v", value, err)
		}
		if open, err := svc.RegistrationOpen(ctx); err != nil || open {
			t.Fatalf("%v must not open registration, got %v (err %v)", value, open, err)
		}
	}

	if err := svc.SetGlobal(ctx, KeyRegistrationOpen, nil); err != nil {
		t.Fatalf("set null: %v", err)
	}
	if raw, _, _ := svc.GetGlobal(ctx, KeyRegistrationOpen); string(raw) != "null" {
		t.Fatalf("null stores JSON null, got %s", raw)
	}
}

func TestGlobalStringAndRaw(t *testing.T) {
	svc := testService(t)
	ctx := context.Background()

	if got := svc.GetGlobalString(ctx, "missing", "fallback"); got != "fallback" {
		t.Fatalf("default = %q, want fallback", got)
	}
	if err := svc.SetGlobal(ctx, "provider_key", "sk-test"); err != nil {
		t.Fatalf("set string: %v", err)
	}
	if got := svc.GetGlobalString(ctx, "provider_key", "fallback"); got != "sk-test" {
		t.Fatalf("string = %q, want sk-test", got)
	}
	if raw, _, _ := svc.GetGlobal(ctx, "provider_key"); string(raw) != `"sk-test"` {
		t.Fatalf("stored JSON = %s, want a JSON string", raw)
	}

	if err := svc.SetGlobalRaw(ctx, "policy", json.RawMessage(`{"enabled":true,"fallbackDirect":false}`)); err != nil {
		t.Fatalf("set raw: %v", err)
	}
	raw, _, err := svc.GetGlobal(ctx, "policy")
	if err != nil {
		t.Fatalf("read policy: %v", err)
	}
	if string(raw) != `{"enabled":true,"fallbackDirect":false}` {
		t.Fatalf("raw = %s", raw)
	}
	if svc.GetGlobalBool(ctx, "policy", false) {
		t.Fatalf("an object is not a boolean, so the default applies")
	}
	if !svc.GetGlobalBool(ctx, "policy", true) {
		t.Fatalf("an object is not a boolean, so the default applies")
	}
	if !svc.GetGlobalBool(ctx, "missing", true) {
		t.Fatalf("missing boolean falls back to the default")
	}
}

func TestUserSettings(t *testing.T) {
	svc := testService(t)
	ctx := context.Background()
	insertUser(t, svc.DB, "u1", "ada", "user")

	if _, found, err := svc.GetUserSetting(ctx, "u1", "processing"); err != nil || found {
		t.Fatalf("missing user setting: found=%v err=%v", found, err)
	}
	if err := svc.SetUserSetting(ctx, "u1", "processing", map[string]any{"shield": true}); err != nil {
		t.Fatalf("set user setting: %v", err)
	}
	var settings struct {
		Shield bool `json:"shield"`
	}
	if found, err := svc.GetUserJSON(ctx, "u1", "processing", &settings); err != nil || !found {
		t.Fatalf("read user setting: found=%v err=%v", found, err)
	}
	if !settings.Shield {
		t.Fatalf("shield = false, want true")
	}

	if err := svc.SetUserSetting(ctx, "u1", "smoothing", true); err != nil {
		t.Fatalf("set smoothing: %v", err)
	}
	if !svc.GetUserBool(ctx, "u1", "smoothing", false) {
		t.Fatalf("smoothing should default to true")
	}
	if !svc.GetUserBool(ctx, "u1", "absent", true) {
		t.Fatalf("absent user setting falls back to the default")
	}

	if err := svc.DeleteUserSetting(ctx, "u1", "smoothing"); err != nil {
		t.Fatalf("delete user setting: %v", err)
	}
	if _, found, _ := svc.GetUserSetting(ctx, "u1", "smoothing"); found {
		t.Fatalf("the setting should be gone")
	}
	if err := svc.DeleteUserSetting(ctx, "u1", "smoothing"); err != nil {
		t.Fatalf("deleting a missing setting is fine: %v", err)
	}
}

func TestUserSecret(t *testing.T) {
	svc := testService(t)
	ctx := context.Background()
	insertUser(t, svc.DB, "u1", "ada", "user")

	if _, found, err := svc.GetUserSecret(ctx, "u1", "key"); err != nil || found {
		t.Fatalf("missing secret: found=%v err=%v", found, err)
	}
	if err := svc.SetUserSecret(ctx, "u1", "key", "sk-secret"); err != nil {
		t.Fatalf("set secret: %v", err)
	}
	raw, _, err := svc.GetUserSetting(ctx, "u1", "key")
	if err != nil {
		t.Fatalf("read stored secret: %v", err)
	}
	var sealed string
	if err := json.Unmarshal(raw, &sealed); err != nil {
		t.Fatalf("a stored secret is a JSON string: %s", raw)
	}
	if !svc.Sealer.(*secretbox.Box).IsSealed(sealed) {
		t.Fatalf("a stored secret must be sealed, got %q", sealed)
	}
	if plain, found, err := svc.GetUserSecret(ctx, "u1", "key"); err != nil || !found || plain != "sk-secret" {
		t.Fatalf("secret = %q found=%v err=%v", plain, found, err)
	}

	// An empty secret deletes the stored one.
	if err := svc.SetUserSecret(ctx, "u1", "key", ""); err != nil {
		t.Fatalf("clear secret: %v", err)
	}
	if _, found, _ := svc.GetUserSetting(ctx, "u1", "key"); found {
		t.Fatalf("an empty secret deletes the setting")
	}

	// An empty stored value counts as no secret.
	if err := svc.SetUserSetting(ctx, "u1", "key", ""); err != nil {
		t.Fatalf("store empty: %v", err)
	}
	if _, found, err := svc.GetUserSecret(ctx, "u1", "key"); err != nil || found {
		t.Fatalf("empty stored value: found=%v err=%v", found, err)
	}

	noSealer := &Service{DB: svc.DB}
	if err := noSealer.SetUserSecret(ctx, "u1", "key", "sk-secret"); !errors.Is(err, ErrNoSealer) {
		t.Fatalf("without a sealer, got %v", err)
	}
}

// insertUser adds one account row.
func insertUser(t *testing.T, db *dbx.DB, id, username, role string) {
	t.Helper()
	exec(t, db, `INSERT INTO users (id, username, email, role, password_hash) VALUES (?, ?, ?, ?, ?)`,
		id, username, username+"@example.test", role, "hash-"+username)
}
