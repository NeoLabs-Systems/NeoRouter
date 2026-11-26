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
	if uv != 42 || BaselineVersion() != 41 {
		t.Fatalf("baseline version = %d (steps %d), want 41 and 42", BaselineVersion(), uv)
	}
	for _, table := range []string{"users", "user_sessions", "schema_migrations", "settings"} {
		var name string
		err := db.R.QueryRowContext(ctx, `SELECT name FROM sqlite_schema WHERE type='table' AND name=?`, table).Scan(&name)
		if err != nil {
			t.Fatalf("missing table %s: %v", table, err)
		}
	}
}

// TestEnsureLegacyNodeDatabase builds the database a Node.js install leaves
// behind: the baseline tables plus schema_migrations rows, with no PRAGMA
// user_version.
func TestEnsureLegacyNodeDatabase(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "neorouter.sqlite3")
	db := openDB(t, path)
	if err := applyBaseline(ctx, db.W); err != nil {
		t.Fatalf("baseline: %v", err)
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
	if uv != 42 {
		t.Fatalf("user_version = %d after sync, want 42", uv)
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

// Sessions issued by earlier releases keep working: the new store's sid is the
// token digest the old table held. Revoked and expired ones stay dead, and the
// old table is emptied but kept for a rollback.
func TestEnsureMovesLiveSessions(t *testing.T) {
	ctx := context.Background()
	db := openDB(t, filepath.Join(t.TempDir(), "neorouter.sqlite3"))
	if _, err := db.W.ExecContext(ctx, baseline); err != nil {
		t.Fatal(err)
	}
	if _, err := db.W.ExecContext(ctx, `
		INSERT INTO schema_migrations (version, name) VALUES (41, 'final');
		INSERT INTO users (id, username, email, password_hash, role) VALUES ('u1', 'ada', 'ada@example.test', 'x', 'admin');
		INSERT INTO user_sessions (id, user_id, token_hash, expires_at, ip_address, user_agent, revoked_at) VALUES
			('live', 'u1', 'digest-live', '2999-01-01T00:00:00.000Z', '127.0.0.1', 'node', NULL),
			('revoked', 'u1', 'digest-revoked', '2999-01-01T00:00:00.000Z', NULL, NULL, '2026-01-01T00:00:00.000Z'),
			('expired', 'u1', 'digest-expired', '2000-01-01T00:00:00.000Z', NULL, NULL, NULL);
	`); err != nil {
		t.Fatal(err)
	}
	if err := Ensure(ctx, db.W); err != nil {
		t.Fatalf("Ensure: %v", err)
	}

	var count, old int
	var sid, id, user, ip, agent, expire string
	if err := db.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if err := db.R.QueryRowContext(ctx, `SELECT sid, json_extract(sess,'$.id'), json_extract(sess,'$.userId'),
		json_extract(sess,'$.ipAddress'), json_extract(sess,'$.userAgent'), expire FROM sessions`).
		Scan(&sid, &id, &user, &ip, &agent, &expire); err != nil {
		t.Fatal(err)
	}
	if err := db.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM user_sessions`).Scan(&old); err != nil {
		t.Fatalf("the old table must stay: %v", err)
	}
	if count != 1 || sid != "digest-live" || id != "live" || user != "u1" || ip != "127.0.0.1" ||
		agent != "node" || expire != "2999-01-01T00:00:00.000Z" || old != 0 {
		t.Fatalf("moved %d: %q %q %q %q %q %q, old rows %d", count, sid, id, user, ip, agent, expire, old)
	}
}
