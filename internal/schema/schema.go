// Package schema owns NeoRouter's SQLite layout and versioned migrations.
//
// baseline.sql is the schema produced by the 41 Node migrations in
// server/db/migrations, including the legacy schema_migrations table that the
// Node server migrated with. A new database gets the baseline in one
// transaction; an existing Node database keeps its tables, has PRAGMA
// user_version synchronised from schema_migrations, and is verified against
// the baseline. From there on, schema changes are Go steps appended to Steps().
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
	steps := make([]dbx.Step, BaselineVersion())
	for i := range steps {
		steps[i] = func(context.Context, *sql.Tx) error { return nil }
	}
	return steps
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
// Node tracked migrations in schema_migrations only and never set the pragma,
// so a database written by the JS server looks version 0 to dbx.Migrate.
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
