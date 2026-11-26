package backup

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func write(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// open keeps a WAL writer open for the test, the state a running server leaves
// the database in.
func open(t *testing.T, path string, statements ...string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}
	return db
}

func value(t *testing.T, path string) string {
	t.Helper()
	db := open(t, path)
	var out string
	if err := db.QueryRow(`SELECT v FROM t`).Scan(&out); err != nil {
		t.Fatal(err)
	}
	db.Close()
	return out
}

func newInstall(t *testing.T) Install {
	t.Helper()
	home := t.TempDir()
	return Install{
		Dir:      filepath.Join(home, "backups"),
		EnvFile:  filepath.Join(home, ".env"),
		DataDir:  filepath.Join(home, "data"),
		Database: filepath.Join(home, "data", "neorouter.sqlite3"),
	}
}

func TestSnapshotAndRestoreOfARunningInstall(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symbolic links")
	}
	ctx := context.Background()
	install := newInstall(t)
	// The database lives outside data/, as NEOROUTER_DATABASE_PATH allows.
	install.Database = filepath.Join(t.TempDir(), "elsewhere.sqlite3")
	write(t, install.EnvFile, "A=1\n", 0o600)
	runtimes := filepath.Join(install.DataDir, "local-models", "runtimes")
	write(t, filepath.Join(runtimes, "libllama.so.1"), "binary", 0o755)
	if err := os.Symlink("libllama.so.1", filepath.Join(runtimes, "libllama.so")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/nowhere/at/all", filepath.Join(runtimes, "dangling")); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(install.DataDir, "neorouter.pid"), "123", 0o600)
	live := open(t, install.Database, `CREATE TABLE t(v TEXT)`, `INSERT INTO t VALUES ('before')`, `PRAGMA user_version=7`)

	item, err := Create(ctx, install, KindMigration)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got := value(t, filepath.Join(item.Path, outsideDatabase)); got != "before" {
		t.Fatalf("snapshot value = %q", got)
	}
	if link, err := os.Readlink(filepath.Join(item.Path, "data", "local-models", "runtimes", "libllama.so")); err != nil || link != "libllama.so.1" {
		t.Fatalf("link = %q, %v", link, err)
	}
	if _, err := os.Stat(filepath.Join(item.Path, "data", "neorouter.pid")); !os.IsNotExist(err) {
		t.Fatal("the PID file was saved")
	}

	if _, err := live.Exec(`UPDATE t SET v='after'; PRAGMA user_version=9`); err != nil {
		t.Fatal(err)
	}
	live.Close()
	write(t, install.EnvFile, "A=2\n", 0o600)
	write(t, filepath.Join(install.DataDir, markerName), "{}", 0o600)
	if err := Restore(install, item.Key); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if got := value(t, install.Database); got != "before" {
		t.Errorf("restored value = %q", got)
	}
	schema, err := ReadSchema(ctx, install.Database)
	if err != nil || schema.Version != 7 {
		t.Errorf("restored version = %d, %v", schema.Version, err)
	}
	if got := read(t, install.EnvFile); got != "A=1\n" {
		t.Errorf("restored .env = %q", got)
	}
	info, err := os.Stat(filepath.Join(runtimes, "libllama.so.1"))
	if err != nil || info.Mode().Perm() != 0o700 {
		t.Errorf("restored binary mode = %v, %v", info.Mode(), err)
	}
	if _, err := os.Stat(filepath.Join(install.DataDir, markerName)); !os.IsNotExist(err) {
		t.Error("the migration marker survived restoring a snapshot without it")
	}
}

func TestPruningNeverRemovesTheMigrationSnapshot(t *testing.T) {
	ctx := context.Background()
	install := newInstall(t)
	write(t, install.EnvFile, "A=1\n", 0o600)
	first, err := Create(ctx, install, KindMigration)
	if err != nil {
		t.Fatal(err)
	}
	for range Keep + 2 {
		for _, kind := range []string{KindUpdate, KindManual} {
			if _, err := Create(ctx, install, kind); err != nil {
				t.Fatal(err)
			}
		}
	}
	items, err := List(install.Dir)
	if err != nil {
		t.Fatal(err)
	}
	count := map[string]int{}
	for _, item := range items {
		count[item.Kind]++
	}
	if count[KindUpdate] != Keep || count[KindManual] != Keep || count[KindMigration] != 1 {
		t.Fatalf("snapshots = %v", count)
	}
	if _, err := Find(install.Dir, first.Key); err != nil {
		t.Fatal(err)
	}
	if _, err := Find(install.Dir, "../etc"); err == nil {
		t.Fatal("a path was accepted as a backup name")
	}
}

func TestRestoreReadsANodeBackup(t *testing.T) {
	install := newInstall(t)
	key := "pre-update-2026-01-02T03-04-05Z"
	write(t, filepath.Join(install.Dir, key, ".env"), "A=node\n", 0o600)
	write(t, filepath.Join(install.Dir, key, "data", "secret.key"), "k", 0o600)
	write(t, install.Database+"-wal", "stale", 0o600)
	write(t, filepath.Join(install.Dir, key, "data", "neorouter.sqlite3"), "db", 0o600)
	if err := Restore(install, key); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if read(t, install.EnvFile) != "A=node\n" || read(t, install.Database) != "db" {
		t.Error("the Node backup was not put back")
	}
	if _, err := os.Stat(install.Database + "-wal"); !os.IsNotExist(err) {
		t.Error("the newer write-ahead log survived")
	}
}

func TestBeforeSchemaChange(t *testing.T) {
	ctx := context.Background()
	install := newInstall(t)
	if _, taken, err := BeforeSchemaChange(ctx, install, 5); taken || err != nil {
		t.Fatalf("no database: taken=%v err=%v", taken, err)
	}
	if err := os.MkdirAll(install.DataDir, 0o700); err != nil {
		t.Fatal(err)
	}
	db := open(t, install.Database, `CREATE TABLE schema_migrations(version INTEGER)`)
	item, taken, err := BeforeSchemaChange(ctx, install, 5)
	if err != nil || !taken || item.Kind != KindMigration {
		t.Fatalf("Node database: %+v, %v, %v", item, taken, err)
	}
	if _, err := db.Exec(`PRAGMA user_version=4`); err != nil {
		t.Fatal(err)
	}
	if item, taken, err = BeforeSchemaChange(ctx, install, 5); err != nil || !taken || item.Kind != KindUpdate {
		t.Fatalf("older database: %+v, %v, %v", item, taken, err)
	}
	if _, err := db.Exec(`PRAGMA user_version=5`); err != nil {
		t.Fatal(err)
	}
	if _, taken, err = BeforeSchemaChange(ctx, install, 5); taken || err != nil {
		t.Fatalf("current database: taken=%v err=%v", taken, err)
	}
}
