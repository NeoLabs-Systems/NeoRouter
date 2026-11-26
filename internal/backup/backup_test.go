package backup

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// install builds a fake install: a .env, a data directory with a database and a
// secret key.
func install(t *testing.T) (dir, envFile, dataDir string) {
	t.Helper()
	dir = t.TempDir()
	envFile = filepath.Join(dir, ".env")
	if err := os.WriteFile(envFile, []byte("NEOROUTER_PORT=4600\n"), 0o600); err != nil {
		t.Fatalf("write .env: %v", err)
	}
	dataDir = filepath.Join(dir, "data")
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		t.Fatalf("create data: %v", err)
	}
	for name, body := range map[string]string{"neorouter.sqlite3": "db", "secret.key": "k"} {
		if err := os.WriteFile(filepath.Join(dataDir, name), []byte(body), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return dir, envFile, dataDir
}

func TestCreateCopiesSettingsAndData(t *testing.T) {
	dir, envFile, dataDir := install(t)
	backups := filepath.Join(dir, "backups")

	artifact, err := Create(Options{Dir: backups, EnvFile: envFile, DataDir: dataDir})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if artifact.Key == "" || filepath.Dir(artifact.Path) != backups {
		t.Fatalf("artifact = %+v", artifact)
	}
	for name, want := range map[string]string{
		".env":                   "NEOROUTER_PORT=4600\n",
		"data/neorouter.sqlite3": "db",
		"data/secret.key":        "k",
	} {
		got, err := os.ReadFile(filepath.Join(artifact.Path, filepath.FromSlash(name)))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if string(got) != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
	if artifact.Bytes == 0 {
		t.Error("artifact size was not measured")
	}
}

func TestCreateSkipsAMissingEnvAndDataDir(t *testing.T) {
	dir := t.TempDir()
	backups := filepath.Join(dir, "backups")
	artifact, err := Create(Options{Dir: backups, EnvFile: filepath.Join(dir, "nope.env"), DataDir: filepath.Join(dir, "nope")})
	if err != nil {
		t.Fatalf("Create with nothing to copy: %v", err)
	}
	if _, err := os.Stat(artifact.Path); err != nil {
		t.Fatalf("snapshot directory: %v", err)
	}
}

func TestCreateKeepsTheNewestThree(t *testing.T) {
	dir, envFile, dataDir := install(t)
	backups := filepath.Join(dir, "backups")
	base := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		now := base.Add(time.Duration(i) * time.Hour)
		artifact, err := Create(Options{
			Dir: backups, EnvFile: envFile, DataDir: dataDir,
			Now: func() time.Time { return now },
		})
		if err != nil {
			t.Fatalf("Create %d: %v", i, err)
		}
		// Give each snapshot a distinct modification time, which is what the
		// newest-first order and the prune both read.
		if err := os.Chtimes(artifact.Path, now, now); err != nil {
			t.Fatalf("touch %s: %v", artifact.Path, err)
		}
	}
	items, err := List(backups)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != Keep {
		t.Fatalf("kept %d snapshots, want %d: %+v", len(items), Keep, items)
	}
	if items[0].Key != "pre-update-20260101T140000Z" {
		t.Errorf("newest = %s, want the last one written", items[0].Key)
	}
	for _, item := range items {
		if _, err := os.Stat(item.Path); err != nil {
			t.Errorf("kept snapshot %s: %v", item.Key, err)
		}
	}
}

func TestPruneIgnoresDirectoriesItDidNotMake(t *testing.T) {
	dir := t.TempDir()
	foreign := filepath.Join(dir, "keep-me")
	if err := os.MkdirAll(foreign, 0o700); err != nil {
		t.Fatalf("create foreign dir: %v", err)
	}
	for i := 0; i < 4; i++ {
		name := filepath.Join(dir, "pre-update-2026010"+string(rune('0'+i))+"T000000Z")
		if err := os.MkdirAll(name, 0o700); err != nil {
			t.Fatalf("create snapshot: %v", err)
		}
	}
	Prune(dir, 2)
	if _, err := os.Stat(foreign); err != nil {
		t.Errorf("Prune removed a directory it did not make: %v", err)
	}
	items, err := List(dir)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 2 {
		t.Errorf("kept %d snapshots, want 2", len(items))
	}
}
