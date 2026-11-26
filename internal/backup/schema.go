package backup

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/NeoLabs-Systems/neolabs-go/dbx"
	"github.com/NeoLabs-Systems/neorouter/internal/config"
	"github.com/NeoLabs-Systems/neorouter/internal/schema"
)

// InstallOf is the snapshot description of an install's paths.
func InstallOf(paths config.Paths) Install {
	return Install{Dir: paths.Backups, EnvFile: paths.EnvFile, DataDir: paths.Data, Database: paths.Database}
}

// OpenDatabase opens the install's database and applies the schema, after
// taking a snapshot when that will change an existing database. It returns the
// snapshot, or nil when none was needed.
func OpenDatabase(ctx context.Context, install Install, options dbx.Options) (*dbx.DB, *Artifact, error) {
	item, taken, err := BeforeSchemaChange(ctx, install, len(schema.Steps()))
	if err != nil {
		return nil, nil, fmt.Errorf("backup: save the data before upgrading it: %w", err)
	}
	var before Schema
	if taken {
		if before, err = ReadSchema(ctx, install.Database); err != nil {
			return nil, nil, err
		}
	}
	db, err := dbx.Open(ctx, install.Database, options)
	if err == nil {
		if err = schema.Ensure(ctx, db.W); err != nil {
			db.Close()
		}
	}
	if err != nil {
		// An upgrade that changed nothing needs no copy, and a server that keeps
		// restarting must not fill the disk with them.
		if after, readErr := ReadSchema(ctx, install.Database); taken && readErr == nil && after.Version == before.Version {
			_ = os.RemoveAll(item.Path)
		}
		return nil, nil, err
	}
	if !taken {
		return db, nil, nil
	}
	return db, &item, nil
}

// Schema is what a database file says about who last migrated it.
type Schema struct {
	// Tables reports a database that holds anything at all.
	Tables bool
	// Version is PRAGMA user_version.
	Version int
	// Node reports a database only the Node.js server has migrated: it kept its
	// own schema_migrations table and never set user_version.
	Node bool
}

// ReadSchema inspects the database at path without changing it. A missing file
// is an empty Schema.
func ReadSchema(ctx context.Context, path string) (Schema, error) {
	var out Schema
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) || (err == nil && info.Size() == 0) {
		return out, nil
	}
	if err != nil {
		return out, fmt.Errorf("backup: inspect database: %w", err)
	}
	db, err := openSQLite(path, "&_pragma=query_only(1)")
	if err != nil {
		return out, err
	}
	defer db.Close()
	var tables, legacy int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(name='schema_migrations'),0)
		FROM sqlite_schema WHERE type='table' AND name NOT LIKE 'sqlite_%'`).Scan(&tables, &legacy); err != nil {
		return out, fmt.Errorf("backup: read database: %w", err)
	}
	if err := db.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&out.Version); err != nil {
		return out, fmt.Errorf("backup: read database version: %w", err)
	}
	out.Tables = tables > 0
	out.Node = legacy > 0 && out.Version == 0
	return out, nil
}

// BeforeSchemaChange snapshots the install when this build, whose schema version
// is latest, is about to change its database. taken is false when there is
// nothing to change.
func BeforeSchemaChange(ctx context.Context, install Install, latest int) (item Artifact, taken bool, err error) {
	current, err := ReadSchema(ctx, install.Database)
	if err != nil || !current.Tables || current.Version >= latest {
		return Artifact{}, false, err
	}
	kind := KindUpdate
	if current.Node {
		kind = KindMigration
	}
	item, err = Create(ctx, install, kind)
	return item, err == nil, err
}
