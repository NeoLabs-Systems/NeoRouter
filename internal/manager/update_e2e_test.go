package manager

// `neorouter update` from end to end: a release on a fake GitHub, a signed
// manifest, the archive, and what the command prints and leaves behind.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NeoLabs-Systems/neolabs-go/cli/release"
	"github.com/NeoLabs-Systems/neolabs-go/cli/service"
	"github.com/NeoLabs-Systems/neorouter/internal/config"
)

// releaseServer serves one release of a channel: the list GitHub's API returns,
// the signed manifest and the runtime archive.
type releaseServer struct {
	*httptest.Server
	tag        string
	body       []byte
	sig        string
	name       string
	blob       []byte
	prerelease bool
}

func newReleaseServer(t *testing.T, version string, keys keypair, prerelease bool) *releaseServer {
	t.Helper()
	archive := runtimeArchive(t)
	digest, size, err := release.FileDigest(archive)
	if err != nil {
		t.Fatalf("digest archive: %v", err)
	}
	name := RuntimeAssetName(Platform(), Architecture(), version)
	manifest, err := release.NewRuntimeManifest(1, version, []release.Artifact{{
		Platform: Platform(), Architecture: Architecture(), AssetName: name, SHA256: digest, SizeBytes: size,
	}}, SupportedTargets())
	if err != nil {
		t.Fatalf("build manifest: %v", err)
	}
	body, err := release.Serialize(manifest)
	if err != nil {
		t.Fatalf("serialize manifest: %v", err)
	}
	signature, err := release.Sign(body, keys.private)
	if err != nil {
		t.Fatalf("sign manifest: %v", err)
	}
	blob, err := os.ReadFile(archive)
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	server := &releaseServer{tag: "v" + version, body: body, sig: signature, name: name, blob: blob, prerelease: prerelease}
	server.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/repos/"):
			asset := func(name string) map[string]string {
				return map[string]string{"name": name, "browser_download_url": server.URL + "/" + name}
			}
			releases := []map[string]any{{
				"tag_name": server.tag, "draft": false, "prerelease": server.prerelease,
				"assets": []any{
					asset(RuntimeManifestPrefix + version + ".json"),
					asset(RuntimeManifestPrefix + version + ".json.sig"),
					asset(name),
				},
			}}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(releases)
		case r.URL.Path == "/"+RuntimeManifestPrefix+version+".json":
			w.Write(server.body)
		case r.URL.Path == "/"+RuntimeManifestPrefix+version+".json.sig":
			fmt.Fprintln(w, server.sig)
		case r.URL.Path == "/"+name:
			w.Write(server.blob)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

// updatable puts the manager on a fake release channel with nothing running.
func updatable(t *testing.T, version string, keys keypair) (*testSetup, *releaseServer) {
	t.Helper()
	s := newTestSetup(t)
	// Port 1 is never open, so an update in a test never looks at a real server.
	s.env["NEOROUTER_PORT"] = "1"
	server := newReleaseServer(t, version, keys, false)
	s.m.ReleaseAPIBase = server.URL
	s.m.RuntimePublicKey = keys.public
	s.m.AllowHTTPDownloads = true
	s.m.HTTP = server.Client()
	return s, server
}

// fakeService replaces the service manager with one that answers every command
// itself, so a test never starts a real server. installed creates the unit file,
// which is what makes an install "installed".
func fakeService(t *testing.T, s *testSetup, installed bool) *recordingRunner {
	t.Helper()
	runner := &recordingRunner{}
	unitPath := config.SystemdUnitPath(s.home)
	if installed {
		if err := os.MkdirAll(filepath.Dir(unitPath), 0o700); err != nil {
			t.Fatalf("create unit dir: %v", err)
		}
		if err := os.WriteFile(unitPath, []byte("[Service]\n"), 0o600); err != nil {
			t.Fatalf("write unit: %v", err)
		}
	}
	s.m.ServiceFactory = func(cfg config.Config) (*service.Manager, error) {
		return &service.Manager{
			Spec:      service.Spec{Name: AppCommand, LogDir: cfg.Paths.Logs, LogName: AppCommand, PIDFile: cfg.Paths.PIDFile},
			Platform:  "linux",
			Runner:    runner,
			UnitPath:  unitPath,
			PlistPath: config.LaunchAgentPlistPath(s.home),
		}, nil
	}
	return runner
}

func TestUpdateInstallsAndActivatesTheNewRuntime(t *testing.T) {
	keys := newKeypair(t)
	s, _ := updatable(t, "0.3.0", keys)
	out, err := runCLI(t, s, "update")
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	for _, want := range []string{"Updating NeoRouter", "0.2.1 → 0.3.0", "NeoRouter 0.3.0 is ready", "Your settings, keys and usage were kept."} {
		if !strings.Contains(out, want) {
			t.Errorf("update is missing %q:\n%s", want, out)
		}
	}
	cfg := s.config(t)
	marker, err := RuntimeStore(cfg).Current()
	if err != nil {
		t.Fatalf("the update did not activate a runtime: %v", err)
	}
	if marker.Version != "0.3.0" {
		t.Errorf("active version = %q, want 0.3.0", marker.Version)
	}
	if s.m.Version != "0.3.0" {
		t.Errorf("the manager still reports %q after the update", s.m.Version)
	}
	// A fresh process reports the runtime it runs, not the constant it was built from.
	fresh, err := New(nil, Options{Getenv: func(key string) string { return s.env[key] }, UserHome: s.home, Cwd: s.home})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if fresh.Version != "0.3.0" {
		t.Errorf("a new process reports version %q, want the active runtime", fresh.Version)
	}
}

func TestUpdateSaysWhenItIsAlreadyCurrent(t *testing.T) {
	keys := newKeypair(t)
	s, _ := updatable(t, "0.2.1", keys)
	out, err := runCLI(t, s, "update")
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if !strings.Contains(out, "You have the latest version (0.2.1)") {
		t.Errorf("update printed %q", out)
	}
	cfg := s.config(t)
	if _, err := RuntimeStore(cfg).Current(); err == nil {
		t.Error("an up-to-date install activated a runtime anyway")
	}
}

func TestUpdateRefusesAReleaseWithoutASignedRuntime(t *testing.T) {
	keys := newKeypair(t)
	s, server := updatable(t, "0.3.0", keys)
	// Drop the manifest from the release, leaving only the archive.
	server.Close()
	bare := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `[{"tag_name":"v0.3.0","draft":false,"prerelease":false,"assets":[{"name":%q,"browser_download_url":%q}]}]`,
			server.name, server.URL+"/"+server.name)
	}))
	defer bare.Close()
	s.m.ReleaseAPIBase = bare.URL
	s.m.HTTP = bare.Client()

	// A release without a signed manifest is not offered at all: the client only
	// resolves releases that carry one.
	_, err := runCLI(t, s, "update")
	if err == nil || !strings.Contains(err.Error(), "No verified beta runtime is published yet.") {
		t.Fatalf("err = %v, want the no-verified-runtime refusal", err)
	}
}

func TestUpdateNotesExplainThatThereIsNothingToShow(t *testing.T) {
	keys := newKeypair(t)
	s, _ := updatable(t, "0.3.0", keys)
	out, err := runCLI(t, s, "update", "--notes")
	if err != nil {
		t.Fatalf("update --notes: %v", err)
	}
	if !strings.Contains(out, "This install didn't come from Git, so there are no notes to show.") {
		t.Errorf("notes printed %q", out)
	}
	cfg := s.config(t)
	if _, err := RuntimeStore(cfg).Current(); err == nil {
		t.Error("--notes changed the installation")
	}
}

func TestUpdateRejectsAnUnknownChannel(t *testing.T) {
	keys := newKeypair(t)
	s, _ := updatable(t, "0.3.0", keys)
	_, err := runCLI(t, s, "update", "gamma")
	if err == nil || !strings.Contains(err.Error(), "Usage: neorouter update [stable|beta] [--notes]") {
		t.Fatalf("err = %v, want the usage line", err)
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

func TestUpdateRollsBackWhenTheNewRuntimeDoesNotStart(t *testing.T) {
	keys := newKeypair(t)
	s, _ := updatable(t, "0.3.0", keys)
	// An installed service, so the update restarts it and then checks the
	// version the server reports. Nothing answers on port 1.
	fakeService(t, s, true)
	s.m.RuntimeStartupTimeout = 300e6
	s.m.RuntimeRollbackTimeout = 300e6

	_, err := runCLI(t, s, "update")
	if err == nil {
		t.Fatal("an update whose server never answered did not fail")
	}
	if !strings.Contains(err.Error(), "0.3.0") {
		t.Errorf("err = %v, want the version it waited for", err)
	}
	// The previous state is back: no runtime is active.
	cfg := s.config(t)
	if _, currentErr := RuntimeStore(cfg).Current(); currentErr == nil {
		t.Error("a failed update left the new runtime active")
	}
}

func TestUpdateOnALegacyInstallMigratesFirst(t *testing.T) {
	keys := newKeypair(t)
	s, _ := updatable(t, "0.3.0", keys)
	install := fakeLegacyInstall(t, s)
	// The legacy install has a unit on disk, so the update replaces the service
	// it starts; the fake manager answers those commands itself.
	fakeService(t, s, true)
	// The restarted server cannot answer in a test, so the update fails after its
	// health check; the migration before it has already happened.
	s.m.RuntimeStartupTimeout = 300e6
	s.m.RuntimeRollbackTimeout = 300e6
	context := context.Background()
	if _, err := runCLI(t, s, "update"); err == nil {
		t.Log("update finished without a server answering")
	}
	cfg := s.config(t)
	if !exists(MarkerPath(cfg)) {
		t.Error("the update did not finish the migration")
	}
	var stored string
	db, err := s.m.openDB(context, cfg)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()
	if err := db.R.QueryRowContext(context, `SELECT secret_encrypted FROM provider_accounts WHERE id='a-1'`).Scan(&stored); err != nil {
		t.Fatalf("read the provider secret: %v", err)
	}
	if strings.HasPrefix(stored, "enc:v1:") == false {
		t.Errorf("the provider secret is still in the Node.js format: %q", stored)
	}
	if install.providerKey == "" {
		t.Error("the fake install lost its secret")
	}
}
