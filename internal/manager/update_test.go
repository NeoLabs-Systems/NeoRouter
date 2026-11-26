package manager

// The update path: a signed runtime manifest, its archive, and what an install
// accepts from it. The download client is the library's; the tests serve the
// release over a local HTTP server and sign with a throwaway key.

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NeoLabs-Systems/neolabs-go/cli/release"
	"github.com/NeoLabs-Systems/neorouter/internal/updates"
)

// keypair is one throwaway signing key.
type keypair struct {
	public, private string
}

func newKeypair(t *testing.T) keypair {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(private)
	if err != nil {
		t.Fatalf("marshal private key: %v", err)
	}
	return keypair{
		public:  base64.StdEncoding.EncodeToString(public),
		private: base64.StdEncoding.EncodeToString(der),
	}
}

// runtimeArchive builds the archive a release ships for this computer.
func runtimeArchive(t *testing.T) string {
	t.Helper()
	stage := t.TempDir()
	if err := os.MkdirAll(filepath.Join(stage, "bin"), 0o700); err != nil {
		t.Fatalf("create bin: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stage, "bin", BinaryName()), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write binary: %v", err)
	}
	web := filepath.Join(stage, "flutter_app", "build", "web")
	if err := os.MkdirAll(web, 0o700); err != nil {
		t.Fatalf("create web: %v", err)
	}
	if err := os.WriteFile(filepath.Join(web, "index.html"), []byte("<html></html>"), 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}
	if err := os.WriteFile(filepath.Join(web, "main.dart.js"), []byte("//"), 0o644); err != nil {
		t.Fatalf("write main.dart.js: %v", err)
	}
	archive := filepath.Join(t.TempDir(), "runtime.zip")
	if err := release.CreateZip(stage, archive); err != nil {
		t.Fatalf("build archive: %v", err)
	}
	return archive
}

// signedRelease serves version's archive and its signed manifest.
func signedRelease(t *testing.T, version string, keys keypair, platform, arch string) (release.Release, *httptest.Server) {
	t.Helper()
	archive := runtimeArchive(t)
	digest, size, err := release.FileDigest(archive)
	if err != nil {
		t.Fatalf("digest archive: %v", err)
	}
	name := RuntimeAssetName(platform, arch, version)
	manifest, err := release.NewRuntimeManifest(1, version, []release.Artifact{{
		Platform: platform, Architecture: arch, AssetName: name, SHA256: digest, SizeBytes: size,
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
	archiveBody, err := os.ReadFile(archive)
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/" + RuntimeManifestPrefix + version + ".json":
			w.Write(body)
		case "/" + RuntimeManifestPrefix + version + ".json.sig":
			fmt.Fprintln(w, signature)
		case "/" + name:
			w.Write(archiveBody)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	rel := release.Release{
		Tag:          "v" + version,
		ManifestName: RuntimeManifestPrefix + version + ".json",
		Assets: map[string]string{
			RuntimeManifestPrefix + version + ".json":     server.URL + "/" + RuntimeManifestPrefix + version + ".json",
			RuntimeManifestPrefix + version + ".json.sig": server.URL + "/" + RuntimeManifestPrefix + version + ".json.sig",
			name: server.URL + "/" + name,
		},
	}
	return rel, server
}

func TestStageSignedRuntimeInstallsAVerifiedArchive(t *testing.T) {
	s := newTestSetup(t)
	cfg := s.config(t)
	keys := newKeypair(t)
	rel, server := signedRelease(t, "0.3.0", keys, Platform(), Architecture())
	s.m.RuntimePublicKey = keys.public
	s.m.AllowHTTPDownloads = true
	s.m.HTTP = server.Client()

	if err := s.m.stageSignedRuntime(s.ctx, cfg, rel, "0.3.0"); err != nil {
		t.Fatalf("stageSignedRuntime: %v", err)
	}
	if err := RuntimeStore(cfg).Validate("0.3.0"); err != nil {
		t.Fatalf("the installed runtime is incomplete: %v", err)
	}
	dir, err := RuntimeStore(cfg).RuntimeDir("0.3.0")
	if err != nil {
		t.Fatalf("runtime dir: %v", err)
	}
	if !exists(runtimeBinary(dir)) {
		t.Errorf("the runtime has no binary at %s", runtimeBinary(dir))
	}
	if !HasBundledWebClient(filepath.Join(dir, "flutter_app", "build", "web")) {
		t.Error("the runtime has no web client")
	}
	// Nothing is activated by a download alone.
	if _, err := RuntimeStore(cfg).Current(); err == nil {
		t.Error("staging a runtime activated it")
	}
}

func TestStageSignedRuntimeRefusesAForeignSignature(t *testing.T) {
	s := newTestSetup(t)
	cfg := s.config(t)
	trusted, forger := newKeypair(t), newKeypair(t)
	rel, server := signedRelease(t, "0.3.0", forger, Platform(), Architecture())
	s.m.RuntimePublicKey = trusted.public
	s.m.AllowHTTPDownloads = true
	s.m.HTTP = server.Client()

	err := s.m.stageSignedRuntime(s.ctx, cfg, rel, "0.3.0")
	if err == nil {
		t.Fatal("a manifest signed by another key was accepted")
	}
	if !strings.Contains(err.Error(), "signature") {
		t.Errorf("error = %v, want a signature failure", err)
	}
}

func TestStageSignedRuntimeRefusesAnotherPlatform(t *testing.T) {
	s := newTestSetup(t)
	cfg := s.config(t)
	keys := newKeypair(t)
	other := "linux"
	if Platform() == "linux" {
		other = "macos"
	}
	rel, server := signedRelease(t, "0.3.0", keys, other, "x64")
	s.m.RuntimePublicKey = keys.public
	s.m.AllowHTTPDownloads = true
	s.m.HTTP = server.Client()

	err := s.m.stageSignedRuntime(s.ctx, cfg, rel, "0.3.0")
	if err == nil || !strings.Contains(err.Error(), "the signed runtime for this computer is missing") {
		t.Fatalf("err = %v, want the missing-platform failure", err)
	}
}

func TestUpdateStatusIsWrittenOnlyWhenTheServerStartedTheUpdate(t *testing.T) {
	s := newTestSetup(t)
	cfg := s.config(t)
	quiet := s.m.updateJob(cfg, "0.2.1")
	quiet.record("backup", 15, "Saving a backup", "running", "", false)
	if exists(cfg.Paths.UpdateStatusFile) {
		t.Error("a plain CLI update wrote the update status file")
	}
	file := updates.NewStatusFile(cfg.Paths.UpdateStatusFile)
	// The server records its own pid as the runner's when it starts one.
	if _, err := file.Write(map[string]any{"state": "running", "phase": "starting", "runnerPid": os.Getpid()}); err != nil {
		t.Fatalf("seed status file: %v", err)
	}
	runner := s.m.updateJob(cfg, "0.2.1")
	runner.record("downloading", 35, "Downloading the new version", "running", "", false)
	status := file.Read()
	if status["phase"] != "downloading" || status["progress"] != float64(35) {
		t.Errorf("status = %v, want the download phase", status)
	}
	if logs, ok := status["logs"].([]any); !ok || len(logs) != 1 {
		t.Errorf("logs = %v, want the phase in them", status["logs"])
	}
	runner.record("completed", 100, "Updated NeoRouter to 0.3.0.", "completed", "0.3.0", false)
	status = file.Read()
	if status["state"] != "completed" || status["completedAt"] == nil || status["runnerPid"] != nil {
		t.Errorf("finished status = %v", status)
	}
}

func TestWaitForServerVersionWaitsForItsOwnVersion(t *testing.T) {
	s := newTestSetup(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"version":"0.2.1"}`)
	}))
	defer server.Close()
	port := 0
	if _, err := fmt.Sscanf(strings.TrimPrefix(server.URL, "http://127.0.0.1:"), "%d", &port); err != nil {
		t.Fatalf("port from %s: %v", server.URL, err)
	}
	err := s.m.waitForServerVersion(s.ctx, port, "0.3.0", 300e6)
	if err == nil || !strings.Contains(err.Error(), "did not report version 0.3.0") {
		t.Fatalf("err = %v, want a version mismatch", err)
	}
	if err := s.m.waitForServerVersion(s.ctx, port, "0.2.1", 300e6); err != nil {
		t.Fatalf("waiting for the running version: %v", err)
	}
}

func TestReleaseClientUsesTheConfiguredAPIRoot(t *testing.T) {
	s := newTestSetup(t)
	s.m.ReleaseAPIBase = "http://127.0.0.1:1"
	if got := s.m.releaseClient().APIBase; got != "http://127.0.0.1:1" {
		t.Errorf("APIBase = %q, want the configured root", got)
	}
}
