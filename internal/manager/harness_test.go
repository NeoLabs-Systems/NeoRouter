package manager

// A fake GitHub that publishes signed releases, and a fake launchd/systemd that
// "starts" a server answering /health on the install's port.

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/NeoLabs-Systems/neolabs-go/cli/release"
	"github.com/NeoLabs-Systems/neolabs-go/cli/service"
	"github.com/NeoLabs-Systems/neorouter/internal/config"
)

// keypair is one throwaway signing key.
type keypair struct{ public, private string }

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
	return keypair{public: base64.StdEncoding.EncodeToString(public), private: base64.StdEncoding.EncodeToString(der)}
}

func writeFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// runtimeArchive builds the archive a release ships.
func runtimeArchive(t *testing.T) string {
	t.Helper()
	stage := t.TempDir()
	app := filepath.Join(stage, "app")
	writeFile(t, filepath.Join(app, "bin", BinaryName()), "#!/bin/sh\n", 0o755)
	writeFile(t, filepath.Join(app, "flutter_app", "build", "web", "index.html"), "<html></html>", 0o644)
	writeFile(t, filepath.Join(app, "flutter_app", "build", "web", "main.dart.js"), "//", 0o644)
	writeFile(t, filepath.Join(app, "landing", "index.html"), "<html></html>", 0o644)
	archive := filepath.Join(t.TempDir(), "runtime.zip")
	if err := release.CreateZip(stage, archive); err != nil {
		t.Fatalf("build archive: %v", err)
	}
	return archive
}

// releases is the fake GitHub: one signed release with every platform's runtime.
type releases struct {
	*httptest.Server
	mu      sync.Mutex
	version string
	files   map[string][]byte
	// missing names assets the release list leaves out.
	missing []string
}

func newReleases(t *testing.T, keys keypair, version string) *releases {
	t.Helper()
	r := &releases{files: map[string][]byte{}}
	r.publish(t, keys, version)
	r.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.mu.Lock()
		defer r.mu.Unlock()
		if req.URL.Path == "/repos/"+Repo+"/releases" {
			var assets []map[string]string
			for name := range r.files {
				if !slices.Contains(r.missing, name) {
					assets = append(assets, map[string]string{"name": name, "browser_download_url": r.URL + "/" + name})
				}
			}
			_ = json.NewEncoder(w).Encode([]map[string]any{{"tag_name": "v" + r.version, "assets": assets}})
			return
		}
		body, ok := r.files[strings.TrimPrefix(req.URL.Path, "/")]
		if !ok {
			http.NotFound(w, req)
			return
		}
		_, _ = w.Write(body)
	}))
	t.Cleanup(r.Close)
	return r
}

func (r *releases) publish(t *testing.T, keys keypair, version string) {
	t.Helper()
	archive := runtimeArchive(t)
	digest, size, err := release.FileDigest(archive)
	if err != nil {
		t.Fatal(err)
	}
	blob := []byte(readFile(t, archive))
	files := map[string][]byte{}
	var artifacts []release.Artifact
	for platform, architectures := range SupportedTargets() {
		for _, architecture := range architectures {
			name := RuntimeAssetName(platform, architecture, version)
			files[name] = blob
			artifacts = append(artifacts, release.Artifact{Platform: platform, Architecture: architecture, AssetName: name, SHA256: digest, SizeBytes: size})
		}
	}
	manifest, err := release.NewRuntimeManifest(1, version, artifacts, SupportedTargets())
	if err != nil {
		t.Fatal(err)
	}
	body, err := release.Serialize(manifest)
	if err != nil {
		t.Fatal(err)
	}
	signature, err := release.Sign(body, keys.private)
	if err != nil {
		t.Fatal(err)
	}
	files[RuntimeManifestPrefix+version+".json"] = body
	files[RuntimeManifestPrefix+version+".json.sig"] = []byte(signature + "\n")
	r.mu.Lock()
	defer r.mu.Unlock()
	r.version, r.files = version, files
}

// use points the manager at the fake GitHub with the key it is signed with.
func (r *releases) use(s *testSetup, keys keypair) {
	s.m.ReleaseAPIBase = r.URL
	s.m.RuntimePublicKey = keys.public
	s.m.AllowHTTPDownloads = true
	s.m.HTTP = r.Client()
}

// fakeService answers the service manager's commands itself.
type fakeService struct {
	t     *testing.T
	s     *testSetup
	port  int
	calls []string
	// version is what a server started now reports; "" means it never answers.
	version func() string

	mu     sync.Mutex
	server *http.Server
	// enabled is what the service manager believes: started and not stopped,
	// whether or not the server answers.
	enabled bool
}

func freePortNumber(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

// newFakeService installs a fake launchd/systemd for the test install. By
// default a started server reports the active runtime's version.
func newFakeService(t *testing.T, s *testSetup) *fakeService {
	t.Helper()
	f := &fakeService{t: t, s: s, port: freePortNumber(t)}
	s.env["NEOROUTER_PORT"] = strconv.Itoa(f.port)
	f.version = f.activeVersion
	s.m.ServiceFactory = func(cfg config.Config) (*service.Manager, error) {
		real, err := NewServiceManager(cfg, s.m.currentBinary(), "/usr/bin:/bin")
		if err != nil {
			return nil, err
		}
		real.Runner = f
		real.PlistPath = config.LaunchAgentPlistPath(s.home)
		real.UnitPath = config.SystemdUnitPath(s.home)
		return real, nil
	}
	t.Cleanup(f.stop)
	return f
}

func (f *fakeService) activeVersion() string {
	cfg, err := f.s.m.loadConfig()
	if err != nil {
		return ""
	}
	return f.s.m.expectedVersion(cfg)
}

// serviceFile is the definition this platform's service manager reads.
func serviceFile(home string) string {
	if Platform() == "macos" {
		return config.LaunchAgentPlistPath(home)
	}
	return config.SystemdUnitPath(home)
}

// run is a server that is already up when the test begins.
func (f *fakeService) run() {
	f.enabled = true
	f.start()
}

func (f *fakeService) start() {
	f.stop()
	version := f.version()
	if version == "" {
		return
	}
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", f.port))
	if err != nil {
		f.t.Errorf("fake server: %v", err)
		return
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok", "version": version})
	})}
	f.mu.Lock()
	f.server = server
	f.mu.Unlock()
	go func() { _ = server.Serve(listener) }()
}

func (f *fakeService) stop() {
	f.mu.Lock()
	server := f.server
	f.server = nil
	f.mu.Unlock()
	if server != nil {
		_ = server.Close()
	}
}

func (f *fakeService) running() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.server != nil
}

// Run implements service.Runner for launchctl and systemctl.
func (f *fakeService) Run(name string, args ...string) (string, error) {
	f.calls = append(f.calls, name+" "+strings.Join(args, " "))
	switch {
	case slices.Contains(args, "bootstrap"), slices.Contains(args, "load"), slices.Contains(args, "start"), slices.Contains(args, "restart"):
		f.enabled = true
		f.start()
	case slices.Contains(args, "bootout"), slices.Contains(args, "unload"), slices.Contains(args, "stop"):
		f.enabled = false
		f.stop()
	case slices.Contains(args, "list"), slices.Contains(args, "is-active"):
		if !f.enabled {
			return "", errors.New("not running")
		}
		return "active", nil
	}
	return "", nil
}

// Exists implements service.Runner.
func (f *fakeService) Exists(string) bool { return true }
