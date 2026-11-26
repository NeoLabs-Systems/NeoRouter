package manager

// `neorouter update` from end to end: a signed release on a fake GitHub, the
// service switched over, and the way back when the new version does not answer.

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/NeoLabs-Systems/neorouter/internal/backup"
	"github.com/NeoLabs-Systems/neorouter/internal/config"
	"github.com/NeoLabs-Systems/neorouter/internal/updates"
)

// installed is a Go install of version, running.
func installed(t *testing.T, version string) (*testSetup, *fakeService, *releases, keypair, config.Config) {
	t.Helper()
	keys := newKeypair(t)
	s := newTestSetup(t)
	fake := newFakeService(t, s)
	published := newReleases(t, keys, version)
	published.use(s, keys)
	if out, err := runCLI(t, s, "install"); err != nil {
		t.Fatalf("install: %v\n%s", err, out)
	}
	return s, fake, published, keys, s.config(t)
}

func running(t *testing.T, fake *fakeService) string {
	t.Helper()
	health, ok := fetchHealth(context.Background(), fake.port)
	if !ok {
		return ""
	}
	version, _ := health["version"].(string)
	return version
}

func TestUpdateSwitchesToTheNewRuntimeOnce(t *testing.T) {
	s, fake, published, keys, cfg := installed(t, "0.9.0")
	published.publish(t, keys, "0.9.1")

	out, err := runCLI(t, s, "update")
	if err != nil {
		t.Fatalf("update: %v\n%s", err, out)
	}
	for _, want := range []string{"Updating NeoRouter", "0.9.0 → 0.9.1", "NeoRouter 0.9.1 is ready", "Your settings, keys and usage were kept."} {
		if !strings.Contains(out, want) {
			t.Errorf("update is missing %q:\n%s", want, out)
		}
	}
	if marker, err := RuntimeStore(cfg).Current(); err != nil || marker.Version != "0.9.1" || running(t, fake) != "0.9.1" {
		t.Fatalf("active = %+v %v, running %s", marker, err, running(t, fake))
	}
	items, err := backup.List(cfg.Paths.Backups)
	if err != nil || len(items) != 1 || items[0].Kind != backup.KindUpdate {
		t.Errorf("backups = %+v, %v", items, err)
	}
	fake.calls = nil
	out, err = runCLI(t, s, "update")
	if err != nil || !strings.Contains(out, "You have the latest version (0.9.1)") {
		t.Fatalf("second update: %v\n%s", err, out)
	}
	if len(fake.calls) != 0 {
		t.Errorf("a second update touched the service: %v", fake.calls)
	}
}

func TestChannelSwitchAlsoGoesBack(t *testing.T) {
	s, _, published, keys, cfg := installed(t, "0.9.1")
	published.publish(t, keys, "0.9.0")
	// Without a channel on the command line an older release is not taken.
	if out, err := runCLI(t, s, "update"); err != nil || !strings.Contains(out, "latest version (0.9.1)") {
		t.Fatalf("update: %v\n%s", err, out)
	}
	if out, err := runCLI(t, s, "update", "stable"); err != nil {
		t.Fatalf("update stable: %v\n%s", err, out)
	}
	if marker, err := RuntimeStore(cfg).Current(); err != nil || marker.Version != "0.9.0" {
		t.Fatalf("active = %+v %v", marker, err)
	}
}

func TestLookingForUpdatesChangesNothing(t *testing.T) {
	s, fake, published, keys, cfg := installed(t, "0.9.0")
	published.publish(t, keys, "0.9.1")
	env := readFile(t, cfg.Paths.EnvFile)
	fake.calls = nil
	for _, args := range [][]string{{"update", "--check"}, {"update", "--notes"}, {"update", "beta", "--check"}} {
		out, err := runCLI(t, s, args...)
		if err != nil || !strings.Contains(out, "0.9.1 is ready") {
			t.Errorf("%v: %v\n%s", args, err, out)
		}
	}
	if readFile(t, cfg.Paths.EnvFile) != env || len(fake.calls) != 0 {
		t.Errorf("looking changed the install: %v", fake.calls)
	}
}

func TestUpdateRefusesAnIncompleteOrUnsignedRelease(t *testing.T) {
	s, fake, published, keys, cfg := installed(t, "0.9.0")
	published.publish(t, keys, "0.9.1")
	fake.calls = nil

	published.missing = []string{RuntimeAssetName("windows", "x64", "0.9.1")}
	if _, err := runCLI(t, s, "update"); err == nil || !strings.Contains(err.Error(), "missing the windows x64 runtime") {
		t.Errorf("incomplete release: %v", err)
	}
	published.missing = []string{RuntimeManifestPrefix + "0.9.1.json.sig"}
	if _, err := runCLI(t, s, "update"); err == nil || !strings.Contains(err.Error(), "No verified stable runtime") {
		t.Errorf("unsigned release: %v", err)
	}
	published.missing = nil
	s.m.RuntimePublicKey = newKeypair(t).public
	if _, err := runCLI(t, s, "update"); err == nil || !strings.Contains(err.Error(), "signature") {
		t.Errorf("foreign signature: %v", err)
	}
	if marker, err := RuntimeStore(cfg).Current(); err != nil || marker.Version != "0.9.0" || len(fake.calls) != 0 {
		t.Errorf("a refused release changed the install: %+v %v %v", marker, err, fake.calls)
	}
}

func TestNoReleaseKeyMeansNoDownloads(t *testing.T) {
	if _, err := parseTrustRoot(" \n"); err == nil || !strings.Contains(err.Error(), "no release signing key") {
		t.Fatalf("empty key: %v", err)
	}
	if _, err := RuntimePublicKey(); err == nil {
		t.Skip("a release key is committed")
	}
	s := newTestSetup(t)
	fake := newFakeService(t, s)
	if _, err := runCLI(t, s, "install"); err == nil || !strings.Contains(err.Error(), "no release signing key") {
		t.Fatalf("install: %v", err)
	}
	if len(fake.calls) > 1 {
		t.Errorf("the service was touched: %v", fake.calls)
	}
}

func TestFailedUpdateRestoresTheDatabaseOnlyAfterASchemaChange(t *testing.T) {
	for _, schemaChanges := range []bool{true, false} {
		s, fake, published, keys, cfg := installed(t, "0.9.0")
		published.publish(t, keys, "0.9.1")
		s.m.RuntimeStartupTimeout = 300e6
		exec := func(statement string) {
			db, err := sql.Open("sqlite", cfg.Paths.Database)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if _, err := db.Exec(statement); err != nil {
				t.Fatal(err)
			}
		}
		before := queryString(t, cfg.Paths.Database, `PRAGMA user_version`)
		// The new server writes, perhaps upgrades the schema, and never answers.
		fake.version = func() string {
			version := fake.activeVersion()
			if version != "0.9.1" {
				return version
			}
			exec(`INSERT INTO settings (key, value) VALUES ('written_by_new_server', '1')`)
			if schemaChanges {
				exec(`PRAGMA user_version = 999`)
			}
			return ""
		}

		out, err := runCLI(t, s, "update")
		if err == nil || !strings.Contains(err.Error(), "running 0.9.0 again") {
			t.Fatalf("update: %v\n%s", err, out)
		}
		if marker, err := RuntimeStore(cfg).Current(); err != nil || marker.Version != "0.9.0" || running(t, fake) != "0.9.0" {
			t.Fatalf("active = %+v %v, running %q", marker, err, running(t, fake))
		}
		if got := queryString(t, cfg.Paths.Database, `PRAGMA user_version`); got != before {
			t.Errorf("schema version = %s, want %s", got, before)
		}
		kept := queryString(t, cfg.Paths.Database, `SELECT COUNT(*) FROM settings WHERE key='written_by_new_server'`)
		if want := map[bool]string{true: "0", false: "1"}[schemaChanges]; kept != want {
			t.Errorf("schema change %v: new rows = %s, want %s", schemaChanges, kept, want)
		}
		fake.stop()
	}
}

func TestAServerStartedUpdateReportsThroughTheStatusFile(t *testing.T) {
	s, _, published, keys, cfg := installed(t, "0.9.0")
	published.publish(t, keys, "0.9.1")
	file := updates.NewStatusFile(cfg.Paths.UpdateStatusFile)
	if _, err := file.Write(map[string]any{"state": "running", "runnerPid": 1}); err != nil {
		t.Fatal(err)
	}
	if out, err := runCLI(t, s, "update", "stable"); err != nil {
		t.Fatalf("update: %v\n%s", err, out)
	}
	if job := file.Job(); job.State != "completed" || job.VersionAfter != "0.9.1" {
		t.Errorf("job = %+v", job)
	}
}

func TestUpdateInAContainerExplainsTheImage(t *testing.T) {
	s := newTestSetup(t)
	s.env["NEOROUTER_CONTAINER"] = "1"
	out, err := runCLI(t, s, "update")
	if err != nil {
		t.Fatalf("update in a container: %v", err)
	}
	if !strings.Contains(out, "Update the image, not the code inside it") || !strings.Contains(out, "docker compose pull") {
		t.Errorf("container update printed %q", out)
	}
}
