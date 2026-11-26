package manager

// Port of test/unit/install_helpers.test.js: the same four ways a web client
// gets to a machine, with the same order of attempts. The Node tests shelled out
// to curl and tar; this port downloads over HTTP and unpacks in process, so the
// tests serve the archive instead of faking curl.

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// workspace is a source checkout: flutter_app with a pubspec, no build yet.
func workspace(t *testing.T) (root, flutterApp, webDir string) {
	t.Helper()
	root = t.TempDir()
	flutterApp = filepath.Join(root, "flutter_app")
	if err := os.MkdirAll(flutterApp, 0o700); err != nil {
		t.Fatalf("create flutter_app: %v", err)
	}
	if err := os.WriteFile(filepath.Join(flutterApp, "pubspec.yaml"), []byte("name: neorouter\n"), 0o600); err != nil {
		t.Fatalf("write pubspec: %v", err)
	}
	return root, flutterApp, filepath.Join(flutterApp, "build", "web")
}

// writeClient writes a complete built client with a marker in index.html.
func writeClient(t *testing.T, dir, marker string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("create %s: %v", dir, err)
	}
	for name, body := range map[string]string{"index.html": marker, "main.dart.js": "//"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
}

// archiveOf packs a built client into the tar.gz GitHub publishes.
func archiveOf(t *testing.T, marker string) []byte {
	t.Helper()
	source := t.TempDir()
	writeClient(t, source, marker)
	path := filepath.Join(t.TempDir(), "client.tar.gz")
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create archive: %v", err)
	}
	gz := gzip.NewWriter(file)
	tarball := tar.NewWriter(gz)
	err = filepath.Walk(source, func(name string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, err := filepath.Rel(source, name)
		if err != nil {
			return err
		}
		if err := tarball.WriteHeader(&tar.Header{Name: filepath.ToSlash(rel), Mode: 0o644, Size: info.Size()}); err != nil {
			return err
		}
		body, err := os.ReadFile(name)
		if err != nil {
			return err
		}
		_, err = tarball.Write(body)
		return err
	})
	if err != nil {
		t.Fatalf("archive: %v", err)
	}
	if err := tarball.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close file: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	return raw
}

// prebuiltServer serves the prebuilt client. directDown makes the download link
// answer 504 the way GitHub's does, while its API still serves the file.
type prebuiltServer struct {
	*httptest.Server
	calls []string
}

func newPrebuiltServer(t *testing.T, client []byte, directDown bool) *prebuiltServer {
	t.Helper()
	server := &prebuiltServer{}
	server.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		server.calls = append(server.calls, r.URL.Path)
		switch {
		case strings.HasSuffix(r.URL.Path, "/web-beta/"+prebuiltArchive):
			if directDown {
				w.WriteHeader(http.StatusGatewayTimeout)
				return
			}
			w.Write(client)
		case strings.HasSuffix(r.URL.Path, "/releases/tags/web-beta"):
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"assets":[{"name":%q,"url":%q}]}`, prebuiltArchive, server.URL+"/assets/1")
		case r.URL.Path == "/assets/1":
			w.Write(client)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

// builder is the manager wired to a scripted runner and the test server.
func builder(t *testing.T, s *testSetup, flutterApp, webDir string, server *prebuiltServer, run CommandRunner, hasFlutter bool) *webBuild {
	t.Helper()
	cfg := s.config(t)
	cfg.AppDir = filepath.Dir(flutterApp)
	cfg.WebDir = webDir
	b := &webBuild{
		cfg:           cfg,
		toolsDir:      filepath.Join(s.home, "tools"),
		channel:       "beta",
		run:           run,
		lookPath:      func(name string) bool { return hasFlutter && name == "flutter" },
		info:          func(string) {},
		ok:            func(string) {},
		warn:          func(string) {},
		githubBase:    server.URL,
		githubAPIBase: server.URL,
	}
	return b
}

// scriptedRunner answers commands the way a real machine would, minus the
// network: git clones a Flutter, flutter writes a client.
func scriptedRunner(t *testing.T, root, flutterApp, webDir string, prebuilt bool, versions string) (CommandRunner, *[]string) {
	t.Helper()
	var calls []string
	run := func(_ context.Context, dir, name string, args ...string) (string, error) {
		calls = append(calls, filepath.Base(name)+" "+strings.Join(args, " "))
		switch filepath.Base(name) {
		case "git":
			target := args[len(args)-1]
			if err := os.MkdirAll(filepath.Join(target, "bin"), 0o700); err != nil {
				return "", err
			}
			return "", os.WriteFile(filepath.Join(target, "bin", "flutter"), []byte(""), 0o755)
		case "flutter":
			if len(args) > 0 && args[0] == "--version" {
				return "Welcome\n{\"frameworkVersion\":\"" + versions + "\"}", nil
			}
			if len(args) > 0 && args[0] == "build" {
				target := ""
				for i, arg := range args {
					if arg == "--output" && i+1 < len(args) {
						target = args[i+1]
					}
				}
				writeClient(t, target, "built")
			}
			return "", nil
		}
		return "", fmt.Errorf("unexpected command %s", name)
	}
	return run, &calls
}

func TestWebClientDownloadsThePrebuiltOneWithoutFlutter(t *testing.T) {
	s := newTestSetup(t)
	root, flutterApp, webDir := workspace(t)
	server := newPrebuiltServer(t, archiveOf(t, "prebuilt"), false)
	run, calls := scriptedRunner(t, root, flutterApp, webDir, true, "0.0.0-unknown")
	if err := builder(t, s, flutterApp, webDir, server, run, false).build(s.ctx); err != nil {
		t.Fatalf("build: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(webDir, "index.html"))
	if err != nil || string(raw) != "prebuilt" {
		t.Fatalf("index.html = %q, %v; want the prebuilt client", raw, err)
	}
	if !strings.Contains(strings.Join(server.calls, " "), "/web-beta/"+prebuiltArchive) {
		t.Errorf("downloads = %v, want the beta prebuilt archive", server.calls)
	}
	for _, call := range *calls {
		if strings.HasPrefix(call, "git ") {
			t.Errorf("Flutter was installed even though the download worked: %v", *calls)
		}
	}
}

func TestWebClientInstallsFlutterWhenNothingIsPublished(t *testing.T) {
	s := newTestSetup(t)
	root, flutterApp, webDir := workspace(t)
	server := newPrebuiltServer(t, archiveOf(t, "prebuilt"), false)
	server.Close() // nothing is published for this channel
	run, calls := scriptedRunner(t, root, flutterApp, webDir, false, "0.0.0-unknown")
	if err := builder(t, s, flutterApp, webDir, server, run, false).build(s.ctx); err != nil {
		t.Fatalf("build: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(webDir, "index.html"))
	if err != nil || string(raw) != "built" {
		t.Fatalf("index.html = %q, %v; want the locally built client", raw, err)
	}
	joined := strings.Join(*calls, "\n")
	if !strings.Contains(joined, "git clone --depth 1 --branch stable") {
		t.Errorf("Flutter was not cloned: %v", *calls)
	}
	if !exists(filepath.Join(s.home, "tools", "flutter", "bin", "flutter")) {
		t.Error("the Flutter it installed was not kept for the next update")
	}
}

func TestWebClientBuildsLocallyWhenFlutterIsInstalled(t *testing.T) {
	s := newTestSetup(t)
	root, flutterApp, webDir := workspace(t)
	server := newPrebuiltServer(t, archiveOf(t, "prebuilt"), false)
	run, calls := scriptedRunner(t, root, flutterApp, webDir, true, "3.47.9")
	if err := builder(t, s, flutterApp, webDir, server, run, true).build(s.ctx); err != nil {
		t.Fatalf("build: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(webDir, "index.html"))
	if err != nil || string(raw) != "built" {
		t.Fatalf("index.html = %q, %v; want the locally built client", raw, err)
	}
	if len(server.calls) != 0 {
		t.Errorf("it downloaded %v while Flutter was installed", server.calls)
	}
	if joined := strings.Join(*calls, "\n"); !strings.Contains(joined, "--base-href /app/") {
		t.Errorf("the build did not use the /app/ base href: %v", *calls)
	}
}

func TestWebClientFallsBackToThePrebuiltOneForAnInstallWithoutSources(t *testing.T) {
	s := newTestSetup(t)
	root, flutterApp, webDir := workspace(t)
	if err := os.Remove(filepath.Join(flutterApp, "pubspec.yaml")); err != nil {
		t.Fatalf("remove pubspec: %v", err)
	}
	server := newPrebuiltServer(t, archiveOf(t, "prebuilt"), false)
	run, _ := scriptedRunner(t, root, flutterApp, webDir, true, "3.47.9")
	b := builder(t, s, flutterApp, webDir, server, run, false)
	b.required = true
	if err := b.build(s.ctx); err != nil {
		t.Fatalf("build: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(webDir, "index.html"))
	if err != nil || string(raw) != "prebuilt" {
		t.Fatalf("index.html = %q, %v; want the prebuilt client", raw, err)
	}
}

func TestWebClientRequiresSourcesWhenNothingCanBeDownloaded(t *testing.T) {
	s := newTestSetup(t)
	root, flutterApp, webDir := workspace(t)
	if err := os.Remove(filepath.Join(flutterApp, "pubspec.yaml")); err != nil {
		t.Fatalf("remove pubspec: %v", err)
	}
	server := newPrebuiltServer(t, archiveOf(t, "prebuilt"), false)
	server.Close()
	run, _ := scriptedRunner(t, root, flutterApp, webDir, false, "3.47.9")
	b := builder(t, s, flutterApp, webDir, server, run, false)
	b.required = true
	err := b.build(s.ctx)
	if err == nil || !strings.Contains(err.Error(), "Missing Flutter app sources") {
		t.Fatalf("err = %v, want the missing-sources failure", err)
	}
}

func TestWebClientUpgradesAnOlderFlutterBeforeBuilding(t *testing.T) {
	for _, test := range []struct {
		version      string
		wantsUpgrade bool
	}{
		{"3.35.5", true},
		{"3.47.9", false},
		{"0.0.0-unknown", false},
	} {
		t.Run(test.version, func(t *testing.T) {
			s := newTestSetup(t)
			root, flutterApp, webDir := workspace(t)
			server := newPrebuiltServer(t, archiveOf(t, "prebuilt"), false)
			server.Close()
			run, calls := scriptedRunner(t, root, flutterApp, webDir, false, test.version)
			if err := builder(t, s, flutterApp, webDir, server, run, true).build(s.ctx); err != nil {
				t.Fatalf("build: %v", err)
			}
			joined := strings.Join(*calls, "\n")
			upgraded := strings.Contains(joined, "flutter upgrade")
			if upgraded != test.wantsUpgrade {
				t.Fatalf("upgrade = %v, want %v for Flutter %s:\n%s", upgraded, test.wantsUpgrade, test.version, joined)
			}
			if test.wantsUpgrade && strings.Index(joined, "flutter upgrade") > strings.Index(joined, "flutter pub get") {
				t.Errorf("the upgrade ran after pub get:\n%s", joined)
			}
		})
	}
}

func TestWebClientFallsBackToTheDownloadApi(t *testing.T) {
	s := newTestSetup(t)
	root, flutterApp, webDir := workspace(t)
	server := newPrebuiltServer(t, archiveOf(t, "prebuilt"), true)
	run, _ := scriptedRunner(t, root, flutterApp, webDir, true, "3.47.9")
	if err := builder(t, s, flutterApp, webDir, server, run, false).build(s.ctx); err != nil {
		t.Fatalf("build: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(webDir, "index.html"))
	if err != nil || string(raw) != "prebuilt" {
		t.Fatalf("index.html = %q, %v; want the prebuilt client", raw, err)
	}
	if !strings.Contains(strings.Join(server.calls, " "), "/assets/1") {
		t.Errorf("calls = %v, want the API asset", server.calls)
	}
}

func TestHasBundledWebClient(t *testing.T) {
	dir := t.TempDir()
	if HasBundledWebClient(dir) {
		t.Error("an empty directory looked like a built client")
	}
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("x"), 0o600); err != nil {
		t.Fatalf("write index: %v", err)
	}
	if HasBundledWebClient(dir) {
		t.Error("an index.html without a script looked like a built client")
	}
	if err := os.WriteFile(filepath.Join(dir, "flutter.js"), []byte(""), 0o600); err != nil {
		t.Fatalf("write flutter.js: %v", err)
	}
	if !HasBundledWebClient(dir) {
		t.Error("a complete client was not recognised")
	}
}

func TestExtractTarGzRefusesPathsOutsideTheDestination(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "evil.tar.gz")
	file, err := os.Create(archive)
	if err != nil {
		t.Fatalf("create archive: %v", err)
	}
	gz := gzip.NewWriter(file)
	tarball := tar.NewWriter(gz)
	body := []byte("x")
	if err := tarball.WriteHeader(&tar.Header{Name: "../escape", Mode: 0o644, Size: int64(len(body))}); err != nil {
		t.Fatalf("write header: %v", err)
	}
	if _, err := tarball.Write(body); err != nil {
		t.Fatalf("write body: %v", err)
	}
	tarball.Close()
	gz.Close()
	file.Close()
	dest := t.TempDir()
	if err := ExtractTarGz(archive, dest); err == nil {
		t.Fatal("an archive escaping its directory was unpacked")
	}
}
