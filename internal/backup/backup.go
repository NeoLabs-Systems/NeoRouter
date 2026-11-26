// Package backup writes and restores snapshots of an install's settings and
// data: what `neorouter backup` and `restore` use, and what the update, the
// Node.js migration and the server take before they change a database.
//
// A snapshot is a directory under <home>/backups, <kind>-<stamp>, holding .env,
// data/ and backup.json. The database in it is a consistent copy made with
// VACUUM INTO, so it can be taken while the server runs.
package backup

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/cli/envfile"
	_ "modernc.org/sqlite"
)

// The kinds of snapshot. All but KindMigration are pruned to Keep each: the copy
// made before a Node.js install was migrated is the only way back to it.
const (
	KindUpdate    = "pre-update"
	KindManual    = "manual"
	KindRestore   = "pre-restore"
	KindMigration = "pre-migration"
)

// Keep is how many snapshots of a prunable kind are kept.
const Keep = 3

const (
	manifestName = "backup.json"
	// outsideDatabase is where a database that lives outside data/ is stored.
	outsideDatabase = "database.sqlite3"
	markerName      = ".go-migrated"
)

// Names the Go CLI writes, and the pre-update-2006-01-02T15-04-05Z form the
// Node.js CLI wrote.
var keyPattern = regexp.MustCompile(
	`^(pre-update|manual|pre-restore|pre-migration)-(\d{8}T\d{6}Z|\d{4}-\d{2}-\d{2}T\d{2}-\d{2}-\d{2}Z)(-\d+)?$`)

// Install names what a snapshot covers.
type Install struct {
	// Dir holds the snapshots.
	Dir string
	// EnvFile, DataDir and Database are what gets copied; each may be missing.
	EnvFile  string
	DataDir  string
	Database string
}

// Artifact is one saved snapshot.
type Artifact struct {
	Key       string `json:"key"`
	Kind      string `json:"kind"`
	Path      string `json:"path"`
	Bytes     int64  `json:"bytes"`
	CreatedAt string `json:"createdAt"`
}

type manifest struct {
	CreatedAt string `json:"createdAt"`
	// Database is where the database was, Stored where the snapshot keeps it.
	Database string `json:"database,omitempty"`
	Stored   string `json:"stored,omitempty"`
}

// Create takes a snapshot of kind and prunes older ones of the same kind. A
// missing .env, data directory or database is skipped.
func Create(ctx context.Context, install Install, kind string) (Artifact, error) {
	if install.Dir == "" {
		return Artifact{}, errors.New("backup: no backup directory configured")
	}
	now := time.Now().UTC()
	target, err := newSnapshotDir(install.Dir, kind+"-"+now.Format("20060102T150405Z"))
	if err != nil {
		return Artifact{}, err
	}
	if err := fill(ctx, install, target, now); err != nil {
		_ = os.RemoveAll(target)
		return Artifact{}, err
	}
	if kind != KindMigration {
		prune(install.Dir, kind, filepath.Base(target))
	}
	return artifactOf(target)
}

func newSnapshotDir(dir, name string) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("backup: create %s: %w", dir, err)
	}
	target := filepath.Join(dir, name)
	for n := 2; ; n++ {
		err := os.Mkdir(target, 0o700)
		if err == nil {
			return target, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return "", fmt.Errorf("backup: create snapshot: %w", err)
		}
		target = filepath.Join(dir, fmt.Sprintf("%s-%d", name, n))
	}
}

func fill(ctx context.Context, install Install, target string, now time.Time) error {
	if install.EnvFile != "" {
		if err := copyFile(install.EnvFile, filepath.Join(target, ".env")); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	saved := manifest{CreatedAt: now.Format(time.RFC3339)}
	data := resolve(install.DataDir)
	database := resolve(install.Database)
	if data != "" {
		skip := map[string]bool{database: true, database + "-wal": true, database + "-shm": true, database + "-journal": true}
		if err := copyTree(data, filepath.Join(target, "data"), skip); err != nil {
			return err
		}
	}
	if database != "" {
		if _, err := os.Stat(database); err == nil {
			stored := outsideDatabase
			if rel, err := filepath.Rel(data, database); data != "" && err == nil && filepath.IsLocal(rel) {
				stored = filepath.Join("data", rel)
			}
			if err := snapshotDatabase(ctx, database, filepath.Join(target, stored)); err != nil {
				return err
			}
			saved.Database, saved.Stored = install.Database, stored
		} else if !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("backup: inspect database: %w", err)
		}
	}
	raw, err := json.MarshalIndent(saved, "", "  ")
	if err != nil {
		return fmt.Errorf("backup: encode manifest: %w", err)
	}
	return envfile.WriteAtomic(filepath.Join(target, manifestName), append(raw, '\n'), 0o600)
}

// resolve follows a symlinked data directory or database to what it points at,
// so the snapshot holds the content rather than the link.
func resolve(path string) string {
	if path == "" {
		return ""
	}
	if real, err := filepath.EvalSymlinks(path); err == nil {
		return real
	}
	return filepath.Clean(path)
}

func openSQLite(path string, pragmas string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(10000)"+pragmas)
	if err != nil {
		return nil, fmt.Errorf("backup: open database: %w", err)
	}
	db.SetMaxOpenConns(1)
	return db, nil
}

func snapshotDatabase(ctx context.Context, source, target string) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return fmt.Errorf("backup: create %s: %w", filepath.Dir(target), err)
	}
	db, err := openSQLite(source, "")
	if err != nil {
		return err
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, `VACUUM INTO '`+strings.ReplaceAll(target, `'`, `''`)+`'`); err != nil {
		return fmt.Errorf("backup: snapshot database %s: %w", source, err)
	}
	return os.Chmod(target, 0o600)
}

// copyTree copies a directory. Files keep their owner permissions, symbolic
// links are recreated as links (local model runtimes ship them), and running
// state (PID file, update status, sockets) is left out.
func copyTree(source, target string, skip map[string]bool) error {
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			// A running server removes temporary files while this walks.
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return fmt.Errorf("backup: read %s: %w", path, err)
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return fmt.Errorf("backup: resolve %s: %w", path, err)
		}
		out := filepath.Join(target, rel)
		switch {
		case entry.IsDir():
			return os.MkdirAll(out, 0o700)
		case skip[path] || strings.HasSuffix(entry.Name(), ".pid") || entry.Name() == "update-status.json":
			return nil
		case entry.Type()&fs.ModeSymlink != 0:
			link, err := os.Readlink(path)
			if err != nil {
				return fmt.Errorf("backup: read link %s: %w", path, err)
			}
			if err := os.Remove(out); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return fmt.Errorf("backup: replace %s: %w", out, err)
			}
			return os.Symlink(link, out)
		case !entry.Type().IsRegular():
			return nil
		}
		if err := copyFile(path, out); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return nil
	})
}

// copyFile replaces target through a temporary file, keeping the source's owner
// permissions and nothing for anyone else.
func copyFile(source, target string) error {
	in, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("backup: open %s: %w", source, err)
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return fmt.Errorf("backup: inspect %s: %w", source, err)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return fmt.Errorf("backup: create %s: %w", filepath.Dir(target), err)
	}
	out, err := os.CreateTemp(filepath.Dir(target), ".copy-*")
	if err != nil {
		return fmt.Errorf("backup: create %s: %w", target, err)
	}
	defer os.Remove(out.Name())
	_, err = io.Copy(out, in)
	if err = errors.Join(err, out.Chmod(info.Mode().Perm()&0o700|0o600), out.Close()); err != nil {
		return fmt.Errorf("backup: copy %s: %w", source, err)
	}
	if err := os.Rename(out.Name(), target); err != nil {
		return fmt.Errorf("backup: write %s: %w", target, err)
	}
	return nil
}

// prune keeps the newest Keep snapshots of kind, always including current. It is
// best effort: it never fails the backup that just succeeded.
func prune(dir, kind, current string) {
	items, err := List(dir)
	if err != nil {
		return
	}
	kept := 1
	for _, item := range items {
		switch {
		case item.Kind != kind || item.Key == current:
		case kept < Keep:
			kept++
		default:
			_ = os.RemoveAll(item.Path)
		}
	}
}

// List returns the snapshots in dir, newest first.
func List(dir string) ([]Artifact, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("backup: list snapshots: %w", err)
	}
	var out []Artifact
	for _, entry := range entries {
		if !entry.IsDir() || !keyPattern.MatchString(entry.Name()) {
			continue
		}
		if item, err := artifactOf(filepath.Join(dir, entry.Name())); err == nil {
			out = append(out, item)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt != out[j].CreatedAt {
			return out[i].CreatedAt > out[j].CreatedAt
		}
		return out[i].Key > out[j].Key
	})
	return out, nil
}

// Find returns the snapshot named key.
func Find(dir, key string) (Artifact, error) {
	if !keyPattern.MatchString(key) {
		return Artifact{}, fmt.Errorf("%q is not a backup name", key)
	}
	item, err := artifactOf(filepath.Join(dir, key))
	if errors.Is(err, fs.ErrNotExist) {
		return Artifact{}, fmt.Errorf("there is no backup named %s", key)
	}
	return item, err
}

func artifactOf(path string) (Artifact, error) {
	info, err := os.Stat(path)
	if err != nil {
		return Artifact{}, err
	}
	var size int64
	err = filepath.WalkDir(path, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		if info, err := entry.Info(); err == nil {
			size += info.Size()
		}
		return nil
	})
	if err != nil {
		return Artifact{}, err
	}
	key := filepath.Base(path)
	kind := ""
	if match := keyPattern.FindStringSubmatch(key); match != nil {
		kind = match[1]
	}
	return Artifact{
		Key: key, Kind: kind, Path: path, Bytes: size,
		CreatedAt: info.ModTime().UTC().Format("2006-01-02T15:04:05.000Z"),
	}, nil
}

// Restore puts a snapshot's settings, data and database back. Files the snapshot
// does not have stay. The server must be stopped.
func Restore(install Install, key string) error {
	item, err := Find(install.Dir, key)
	if err != nil {
		return err
	}
	env := filepath.Join(item.Path, ".env")
	data := filepath.Join(item.Path, "data")
	hasEnv, hasData := exists(env), exists(data)
	saved, err := readManifest(item.Path)
	if err != nil {
		return err
	}
	if !hasEnv && !hasData && saved.Stored == "" {
		return fmt.Errorf("%s holds no settings and no data", key)
	}
	if hasEnv {
		if err := copyFile(env, install.EnvFile); err != nil {
			return err
		}
	}
	database := install.Database
	if saved.Database != "" {
		database = saved.Database
	}
	// A restored database must never meet the newer one's write-ahead log.
	if saved.Stored != "" || exists(filepath.Join(data, filepath.Base(database))) {
		if err := removeSidecars(database); err != nil {
			return err
		}
	}
	if hasData {
		// The database in data/ is written to its own path below.
		skip := map[string]bool{}
		if saved.Stored != "" {
			skip[filepath.Join(data, strings.TrimPrefix(saved.Stored, "data"+string(filepath.Separator)))] = true
		}
		if err := copyTree(data, resolve(install.DataDir), skip); err != nil {
			return err
		}
		// A snapshot from before the migration undoes its record of it.
		if !exists(filepath.Join(data, markerName)) {
			if err := os.Remove(filepath.Join(install.DataDir, markerName)); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return fmt.Errorf("backup: remove migration marker: %w", err)
			}
		}
	}
	if saved.Stored != "" {
		return copyFile(filepath.Join(item.Path, saved.Stored), resolve(database))
	}
	return nil
}

// RestoreDatabase puts back only the database of a snapshot. The server must be
// stopped.
func RestoreDatabase(item Artifact) error {
	saved, err := readManifest(item.Path)
	if err != nil {
		return err
	}
	if saved.Stored == "" {
		return fmt.Errorf("backup %s holds no database", item.Key)
	}
	database := resolve(saved.Database)
	if err := removeSidecars(database); err != nil {
		return err
	}
	return copyFile(filepath.Join(item.Path, saved.Stored), database)
}

func readManifest(snapshot string) (manifest, error) {
	var saved manifest
	raw, err := os.ReadFile(filepath.Join(snapshot, manifestName))
	if errors.Is(err, fs.ErrNotExist) {
		// A Node.js backup: plain copies of .env and data/.
		return saved, nil
	}
	if err != nil {
		return saved, fmt.Errorf("backup: read manifest: %w", err)
	}
	if err := json.Unmarshal(raw, &saved); err != nil {
		return saved, fmt.Errorf("backup: read manifest: %w", err)
	}
	return saved, nil
}

func removeSidecars(database string) error {
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		if err := os.Remove(resolve(database) + suffix); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("backup: remove %s: %w", database+suffix, err)
		}
	}
	return nil
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
