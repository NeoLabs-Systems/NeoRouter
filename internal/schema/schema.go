// Package schema owns NeoRouter's SQLite layout and versioned migrations.
//
// baseline.sql is the schema of existing Node.js installs after all their
// migrations, including the legacy schema_migrations table. A new database gets
// the baseline in one transaction; an existing Node database keeps its tables,
// has PRAGMA user_version synchronised from schema_migrations, and is verified
// against the baseline. From there on, schema changes are Go steps appended to
// Steps().
package schema

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/NeoLabs-Systems/neolabs-go/dbx"
)

//go:embed baseline.sql
var baseline string

//go:embed USER_VERSION
var userVersionText string

// BaselineVersion is the Node-era migration version encoded in baseline.sql.
func BaselineVersion() int {
	n, err := strconv.Atoi(strings.TrimSpace(userVersionText))
	if err != nil {
		panic("schema: USER_VERSION is not a number: " + err.Error())
	}
	return n
}

// Steps returns the ordered migrations. Entries 0..BaselineVersion()-1 are the
// Node-era migrations, already part of baseline.sql and therefore no-ops here;
// real Go migrations are appended after them.
func Steps() []dbx.Step {
	steps := make([]dbx.Step, BaselineVersion(), BaselineVersion()+2)
	for i := range steps {
		steps[i] = func(context.Context, *sql.Tx) error { return nil }
	}
	return append(steps, sharedSignIn, providerPrivateAllowed)
}

// sharedSignIn (step 42) adds the tables of the shared NeoLabs sign-in packages
// and moves live sessions into neolabs-go/sessionx. A session's sid is the same
// token digest the old table stored, so nobody is signed out. The old rows are
// removed so that rolling back to the Node.js server cannot bring back sessions
// revoked since; the table stays so that it still starts.
func sharedSignIn(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS sessions (sid TEXT NOT NULL PRIMARY KEY, sess JSON NOT NULL, expire TEXT NOT NULL);
INSERT OR IGNORE INTO sessions (sid, sess, expire)
	SELECT token_hash,
		json_object('id', id, 'userId', user_id, 'ipAddress', ip_address, 'userAgent', user_agent),
		expires_at
	FROM user_sessions
	WHERE revoked_at IS NULL AND datetime(expires_at) > datetime('now');
DELETE FROM user_sessions;

ALTER TABLE user_two_factor ADD COLUMN last_totp_step INTEGER NOT NULL DEFAULT 0;

CREATE TABLE login_failures (account_key TEXT NOT NULL, kind TEXT NOT NULL, failed_at_ms INTEGER NOT NULL);
CREATE INDEX idx_login_failures_key ON login_failures(account_key, failed_at_ms);

CREATE TABLE user_webauthn_credentials (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	credential_id TEXT NOT NULL UNIQUE,
	public_key TEXT NOT NULL,
	counter INTEGER NOT NULL DEFAULT 0,
	rp_id TEXT NOT NULL,
	transports TEXT NOT NULL DEFAULT '[]',
	device_type TEXT,
	backed_up INTEGER NOT NULL DEFAULT 0,
	user_verified INTEGER NOT NULL DEFAULT 0,
	label TEXT NOT NULL,
	created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
	last_used_at TEXT
);
CREATE INDEX idx_user_webauthn_credentials_user ON user_webauthn_credentials(user_id, rp_id);
CREATE TABLE webauthn_challenges (
	id TEXT PRIMARY KEY,
	purpose TEXT NOT NULL CHECK (purpose IN ('registration', 'authentication')),
	user_id TEXT REFERENCES users(id) ON DELETE CASCADE,
	challenge TEXT NOT NULL,
	rp_id TEXT NOT NULL,
	expires_at TEXT NOT NULL
);

CREATE TABLE user_email_tokens (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	type TEXT NOT NULL,
	email TEXT NOT NULL,
	token_hash TEXT UNIQUE NOT NULL,
	created_at TEXT DEFAULT (datetime('now')),
	expires_at TEXT NOT NULL,
	consumed_at TEXT
);
CREATE TABLE user_qr_login_challenges (
	id TEXT PRIMARY KEY,
	poll_token_hash TEXT UNIQUE NOT NULL,
	approve_secret_hash TEXT NOT NULL,
	status TEXT DEFAULT 'pending',
	request_user_agent TEXT,
	request_ip_address TEXT,
	request_location_label TEXT,
	request_location_json TEXT DEFAULT '{}',
	request_metadata_json TEXT DEFAULT '{}',
	approved_by_user_id TEXT REFERENCES users(id) ON DELETE SET NULL,
	approved_session_hash TEXT,
	approved_metadata_json TEXT DEFAULT '{}',
	scanned_metadata_json TEXT DEFAULT '{}',
	expires_at TEXT NOT NULL,
	scanned_at TEXT,
	approved_at TEXT,
	claimed_at TEXT,
	created_at TEXT DEFAULT (datetime('now'))
);
`)
	if err != nil {
		return fmt.Errorf("schema: shared sign-in: %w", err)
	}
	return nil
}

// providerPrivateAllowed (step 43) records on each connection whether it may
// reach local and private addresses, decided when its destination was set.
// Before, that was worked out on every call from the space owner's role alone,
// so a member of the instance admin's team could point a connection of the
// admin's space at a host that redirects or rebinds inwards.
//
// Existing connections keep working where nothing says a member set them up:
// a connection in an admin's space is allowed when it has no recorded creator
// (it predates created_by_user_id, or came from the CLI or a pool link) or an
// admin created it. A connection a non-admin created stays public-only. Who
// last changed a base URL was never recorded, so that cannot be told apart.
func providerPrivateAllowed(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
ALTER TABLE provider_accounts ADD COLUMN private_allowed INTEGER NOT NULL DEFAULT 0;
UPDATE provider_accounts SET private_allowed = 1
	WHERE user_id IN (SELECT id FROM users WHERE role = 'admin')
	AND (created_by_user_id IS NULL OR created_by_user_id IN (SELECT id FROM users WHERE role = 'admin'));
`)
	if err != nil {
		return fmt.Errorf("schema: provider private access: %w", err)
	}
	return nil
}

// Ensure prepares db for use: an empty database receives baseline.sql, an
// existing Node database gets PRAGMA user_version synchronised from
// schema_migrations and is verified against the baseline, then pending Go
// migrations run via dbx.Migrate (which also refuses a newer database).
func Ensure(ctx context.Context, db *sql.DB) error {
	empty, err := isEmpty(ctx, db)
	if err != nil {
		return err
	}
	if empty {
		if err := applyBaseline(ctx, db); err != nil {
			return err
		}
	} else {
		if err := syncLegacyUserVersion(ctx, db); err != nil {
			return err
		}
		if err := Verify(ctx, db); err != nil {
			return err
		}
	}
	return dbx.Migrate(ctx, db, Steps())
}

func isEmpty(ctx context.Context, db *sql.DB) (bool, error) {
	var n int
	err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_schema WHERE name NOT LIKE 'sqlite_%'`).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("schema: inspect database: %w", err)
	}
	return n == 0, nil
}

// syncLegacyUserVersion copies the Node bookkeeping into PRAGMA user_version.
// The Node.js server tracked migrations in schema_migrations only and never set
// the pragma, so a database it wrote looks version 0 to dbx.Migrate.
func syncLegacyUserVersion(ctx context.Context, db *sql.DB) error {
	var uv int
	if err := db.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&uv); err != nil {
		return fmt.Errorf("schema: read user_version: %w", err)
	}
	if uv > 0 {
		return nil
	}
	var hasTable int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_schema WHERE type='table' AND name='schema_migrations'`).Scan(&hasTable); err != nil {
		return fmt.Errorf("schema: inspect schema_migrations: %w", err)
	}
	if hasTable == 0 {
		return nil
	}
	var maxVer sql.NullInt64
	if err := db.QueryRowContext(ctx, `SELECT MAX(version) FROM schema_migrations`).Scan(&maxVer); err != nil {
		return fmt.Errorf("schema: read schema_migrations: %w", err)
	}
	if maxVer.Valid && int(maxVer.Int64) >= BaselineVersion() {
		if _, err := db.ExecContext(ctx, fmt.Sprintf(`PRAGMA user_version = %d`, BaselineVersion())); err != nil {
			return fmt.Errorf("schema: record version: %w", err)
		}
	}
	return nil
}

func applyBaseline(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("schema: begin: %w", err)
	}
	if _, err := tx.ExecContext(ctx, baseline); err != nil {
		tx.Rollback()
		return fmt.Errorf("schema: apply baseline: %w", err)
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`PRAGMA user_version = %d`, BaselineVersion())); err != nil {
		tx.Rollback()
		return fmt.Errorf("schema: record version: %w", err)
	}
	return tx.Commit()
}

// Verify checks that an existing database was migrated to at least the
// baseline version and has every baseline table and column.
func Verify(ctx context.Context, db *sql.DB) error {
	var version int
	if err := db.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		return fmt.Errorf("schema: read user_version: %w", err)
	}
	if version < BaselineVersion() {
		return fmt.Errorf("schema: database is at version %d, this build needs %d; upgrade with the last NeoRouter 0.2 release first", version, BaselineVersion())
	}

	ref, err := sql.Open("sqlite", "file::memory:")
	if err != nil {
		return fmt.Errorf("schema: build reference: %w", err)
	}
	defer ref.Close()
	ref.SetMaxOpenConns(1)
	if _, err := ref.ExecContext(ctx, baseline); err != nil {
		return fmt.Errorf("schema: build reference: %w", err)
	}
	want, err := columns(ctx, ref)
	if err != nil {
		return err
	}
	have, err := columns(ctx, db)
	if err != nil {
		return err
	}

	var problems []string
	for table, cols := range want {
		got, ok := have[table]
		if !ok {
			problems = append(problems, "missing table "+table)
			continue
		}
		for c := range cols {
			if !got[c] {
				problems = append(problems, "missing column "+table+"."+c)
			}
		}
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		return fmt.Errorf("schema: incompatible database: %s", strings.Join(problems, ", "))
	}
	return nil
}

func columns(ctx context.Context, db *sql.DB) (map[string]map[string]bool, error) {
	rows, err := db.QueryContext(ctx, `SELECT name FROM sqlite_schema WHERE type='table' AND name NOT LIKE 'sqlite_%'`)
	if err != nil {
		return nil, fmt.Errorf("schema: list tables: %w", err)
	}
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return nil, fmt.Errorf("schema: list tables: %w", err)
		}
		names = append(names, name)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("schema: list tables: %w", err)
	}
	out := make(map[string]map[string]bool, len(names))
	for _, table := range names {
		colRows, err := db.QueryContext(ctx, fmt.Sprintf(`PRAGMA table_info("%s")`, strings.ReplaceAll(table, `"`, `""`)))
		if err != nil {
			return nil, fmt.Errorf("schema: columns of %s: %w", table, err)
		}
		cols := map[string]bool{}
		for colRows.Next() {
			var cid int
			var name, colType string
			var notNull, pk int
			var dflt sql.NullString
			if err := colRows.Scan(&cid, &name, &colType, &notNull, &dflt, &pk); err != nil {
				colRows.Close()
				return nil, fmt.Errorf("schema: columns of %s: %w", table, err)
			}
			cols[name] = true
		}
		colRows.Close()
		if err := colRows.Err(); err != nil {
			return nil, fmt.Errorf("schema: columns of %s: %w", table, err)
		}
		out[table] = cols
	}
	return out, nil
}
