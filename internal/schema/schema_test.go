package schema

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NeoLabs-Systems/neolabs-go/dbx"
)

func openDB(t *testing.T, path string) *dbx.DB {
	t.Helper()
	db, err := dbx.Open(context.Background(), path, dbx.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestEnsureFreshAndIdempotent(t *testing.T) {
	ctx := context.Background()
	db := openDB(t, filepath.Join(t.TempDir(), "neorouter.sqlite3"))

	if err := Ensure(ctx, db.W); err != nil {
		t.Fatalf("first Ensure: %v", err)
	}
	if err := Ensure(ctx, db.W); err != nil {
		t.Fatalf("second Ensure: %v", err)
	}

	var uv int
	if err := db.R.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&uv); err != nil {
		t.Fatal(err)
	}
	if want := len(Steps()); uv != want {
		t.Fatalf("user_version = %d, want %d", uv, want)
	}
	if uv != 41 || BaselineVersion() != 41 {
		t.Fatalf("baseline version = %d (steps %d), want 41", BaselineVersion(), uv)
	}
	for _, table := range []string{"users", "user_sessions", "schema_migrations", "settings"} {
		var name string
		err := db.R.QueryRowContext(ctx, `SELECT name FROM sqlite_schema WHERE type='table' AND name=?`, table).Scan(&name)
		if err != nil {
			t.Fatalf("missing table %s: %v", table, err)
		}
	}
}

// TestEnsureLegacyNodeDatabase builds the database the JS server leaves behind:
// the baseline tables plus schema_migrations rows, with no PRAGMA user_version.
func TestEnsureLegacyNodeDatabase(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "neorouter.sqlite3")
	db := openDB(t, path)
	if err := Ensure(ctx, db.W); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if _, err := db.W.ExecContext(ctx, `INSERT INTO schema_migrations (version, name) VALUES (?, ?)`, 41, "041_final.js"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.W.ExecContext(ctx, `PRAGMA user_version = 0`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	legacy := openDB(t, path)
	var uv int
	if err := legacy.R.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&uv); err != nil {
		t.Fatal(err)
	}
	if uv != 0 {
		t.Fatalf("setup: user_version = %d, want 0", uv)
	}
	if err := Ensure(ctx, legacy.W); err != nil {
		t.Fatalf("Ensure on legacy database: %v", err)
	}
	if err := legacy.R.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&uv); err != nil {
		t.Fatal(err)
	}
	if uv != 41 {
		t.Fatalf("user_version = %d after sync, want 41", uv)
	}
	if err := Verify(ctx, legacy.R); err != nil {
		t.Fatalf("Verify after sync: %v", err)
	}
}

func TestVerifyReportsMissingColumn(t *testing.T) {
	ctx := context.Background()
	raw, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "raw.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	raw.SetMaxOpenConns(1)
	if _, err := raw.ExecContext(ctx, baseline); err != nil {
		t.Fatalf("apply baseline: %v", err)
	}
	if _, err := raw.ExecContext(ctx, `PRAGMA user_version = 41`); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.ExecContext(ctx, `ALTER TABLE users DROP COLUMN last_login_at`); err != nil {
		t.Fatalf("drop column: %v", err)
	}
	err = Verify(ctx, raw)
	if err == nil {
		t.Fatal("Verify accepted a database with a missing column")
	}
	if !strings.Contains(err.Error(), "missing column users.last_login_at") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestVerifyRefusesPreBaselineDatabase(t *testing.T) {
	ctx := context.Background()
	db := openDB(t, filepath.Join(t.TempDir(), "neorouter.sqlite3"))
	if _, err := db.W.ExecContext(ctx, `CREATE TABLE users (id TEXT PRIMARY KEY); PRAGMA user_version = 3`); err != nil {
		t.Fatal(err)
	}
	if err := Ensure(ctx, db.W); err == nil {
		t.Fatal("Ensure accepted a database below the baseline version")
	}
}

func TestEnsureRefusesNewerDatabase(t *testing.T) {
	ctx := context.Background()
	db := openDB(t, filepath.Join(t.TempDir(), "neorouter.sqlite3"))
	if err := Ensure(ctx, db.W); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if _, err := db.W.ExecContext(ctx, `PRAGMA user_version = 99`); err != nil {
		t.Fatal(err)
	}
	err := Ensure(ctx, db.W)
	if err == nil {
		t.Fatal("Ensure accepted a database newer than this build")
	}
	if !strings.Contains(err.Error(), "newer than this build supports") {
		t.Fatalf("unexpected error: %v", err)
	}
}
