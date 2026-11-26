package manager

// The bundled web client at /app. A release carries a built one; a source
// checkout builds it with Flutter, and a machine without Flutter downloads the
// client GitHub built for the channel. Port of lib/install_helpers.js.

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/NeoLabs-Systems/neorouter/internal/config"
)

// requiredFlutter is the oldest Flutter the web client builds with: older
// versions still build it, but iOS 26 Safari needs this one (install_helpers.js
// REQUIRED_FLUTTER_VERSION).
const requiredFlutter = "3.47.9"

// prebuiltArchive is the asset name of the client GitHub builds on every push.
const prebuiltArchive = "neorouter-web.tar.gz"

// prebuiltReleaseTag is the tag the prebuilt client is attached to.
func prebuiltReleaseTag(channel string) string {
	if channel == "beta" {
		return "web-beta"
	}
	return "web-main"
}

// defaultGitHubBase and defaultGitHubAPIBase are where releases live.
const (
	defaultGitHubBase    = "https://github.com"
	defaultGitHubAPIBase = "https://api.github.com"
)

// HasBundledWebClient reports whether dir holds a complete built client
// (install_helpers.js hasBundledWebClient).
func HasBundledWebClient(dir string) bool {
	if !exists(filepath.Join(dir, "index.html")) {
		return false
	}
	for _, marker := range []string{"main.dart.js", "flutter_bootstrap.js", "flutter.js"} {
		if exists(filepath.Join(dir, marker)) {
			return true
		}
	}
	return false
}

// webBuild is one attempt at producing the client; the callbacks are the lines
// the CLI prints while it runs.
type webBuild struct {
	cfg      config.Config
	required bool
	toolsDir string
	channel  string
	run      func(ctx context.Context, dir, name string, args ...string) (string, error)
	lookPath func(string) bool
	info     func(string)
	ok       func(string)
	warn     func(string)
	// githubBase and githubAPIBase point the download at a test server.
	githubBase, githubAPIBase string
	failedOut                 *string
}

// ensureWebClient makes sure a web client is there, building or downloading one
// when it is missing. A failed build never fails the command that asked for it
// unless required is set (install, repair) — start and restart keep the client
// that is already there.
func (m *Manager) ensureWebClient(ctx context.Context, cfg config.Config, required bool) error {
	b := &webBuild{
		cfg:        cfg,
		required:   required,
		toolsDir:   filepath.Join(cfg.Paths.Home, "tools"),
		channel:    m.currentChannel(cfg),
		run:        m.run,
		lookPath:   m.commandExists,
		githubBase: m.GitHubBase, githubAPIBase: m.GitHubAPIBase,
		info: m.UI.Info,
		ok:   m.UI.OK,
		warn: m.UI.Warn,
	}
	return b.build(ctx)
}

// build is install_helpers.js buildBundledWebClientIfPossible.
func (b *webBuild) build(ctx context.Context) error {
	flutterApp := config.FlutterAppDir(b.cfg.AppDir)
	webDir := b.cfg.WebDir
	if !exists(filepath.Join(flutterApp, "pubspec.yaml")) {
		if HasBundledWebClient(webDir) {
			b.info("Using the app that came with NeoRouter")
			return nil
		}
		// An install without the app sources: the client GitHub built.
		downloaded, err := b.downloadPrebuilt(ctx)
		if err != nil {
			return err
		}
		if downloaded {
			b.ok("App downloaded")
			return nil
		}
		if b.required {
			return fmt.Errorf("Missing Flutter app sources at %s, and the prebuilt web client could not be downloaded.", flutterApp)
		}
		b.info("Using the app that came with NeoRouter")
		return nil
	}

	flutter := ""
	if b.lookPath("flutter") {
		flutter = "flutter"
	} else if local := localFlutter(b.toolsDir); local != "" {
		flutter = local
	}
	if flutter == "" {
		b.info("Downloading the app")
		downloaded, err := b.downloadPrebuilt(ctx)
		if err != nil {
			return err
		}
		if downloaded {
			b.ok("App downloaded")
			return nil
		}
		b.info("Installing Flutter to build the app — a few minutes, once")
		installed, err := b.installFlutter(ctx)
		if err != nil {
			return err
		}
		if installed == "" {
			return fmt.Errorf("The web client could not be downloaded and Flutter could not be installed. " +
				"Install Flutter (https://docs.flutter.dev/get-started/install) and run `neorouter rebuild-web`.")
		}
		flutter = installed
	}
	b.upgradeOlderFlutter(ctx, flutter)
	// The running server keeps serving the current build while the new one is
	// made next to it; only the build cache is cleared, never the live build.
	_ = os.RemoveAll(filepath.Join(flutterApp, ".dart_tool", "flutter_build"))
	if out, err := b.run(ctx, flutterApp, flutter, "pub", "get"); err != nil {
		b.failedOutput(out)
		return fmt.Errorf("Flutter pub get failed before web rebuild.")
	}
	nextDir := webDir + "-next"
	_ = os.RemoveAll(nextDir)
	b.info("Building the app — this can take a few minutes")
	out, err := b.run(ctx, flutterApp, flutter, "build", "web", "--release", "--no-tree-shake-icons",
		"--base-href", "/app/", "--output", nextDir)
	if err != nil || !HasBundledWebClient(nextDir) {
		b.failedOutput(out)
		_ = os.RemoveAll(nextDir)
		// A failed local build can still fall back to the prebuilt client.
		if downloaded, derr := b.downloadPrebuilt(ctx); derr == nil && downloaded {
			b.ok("App downloaded")
			return nil
		}
		if HasBundledWebClient(webDir) {
			b.warn("The app couldn't be rebuilt; keeping the one you have")
			return nil
		}
		return fmt.Errorf("Flutter web build failed and no bundled web client is available.\n%s", b.tailedOutput())
	}
	if err := swapIn(nextDir, webDir); err != nil {
		_ = os.RemoveAll(nextDir)
		return err
	}
	b.ok("App rebuilt")
	return nil
}

// failedOutput keeps the last lines a failed command printed.
func (b *webBuild) failedOutput(out string) {
	tail := tailLines(out)
	if tail != "" {
		b.failedOut = &tail
	}
}

func (b *webBuild) tailedOutput() string {
	if b.failedOut == nil {
		return ""
	}
	return *b.failedOut
}

// downloadPrebuilt fetches the client GitHub built for the channel and swaps it
// in; false means it was not available.
func (b *webBuild) downloadPrebuilt(ctx context.Context) (bool, error) {
	nextDir := b.cfg.WebDir + "-next"
	_ = os.RemoveAll(nextDir)
	if err := os.MkdirAll(filepath.Dir(b.cfg.WebDir), 0o700); err != nil {
		return false, err
	}
	archive, err := b.fetchPrebuilt(ctx)
	if err != nil {
		return false, err
	}
	if archive == "" {
		return false, nil
	}
	if err := os.MkdirAll(nextDir, 0o700); err != nil {
		return false, err
	}
	if err := ExtractTarGz(archive, nextDir); err != nil || !HasBundledWebClient(nextDir) {
		_ = os.RemoveAll(nextDir)
		return false, nil
	}
	if err := swapIn(nextDir, b.cfg.WebDir); err != nil {
		_ = os.RemoveAll(nextDir)
		return false, err
	}
	return true, nil
}

// fetchPrebuilt downloads the prebuilt archive and returns its path, or "" when
// GitHub has none for this channel. The API serves the same file when the
// download link fails.
func (b *webBuild) fetchPrebuilt(ctx context.Context) (string, error) {
	archive := b.cfg.WebDir + "-download.tar.gz"
	for _, url := range []string{b.prebuiltURL(), b.apiAssetURL(ctx)} {
		if url == "" {
			continue
		}
		if err := b.download(ctx, url, archive); err == nil {
			return archive, nil
		}
	}
	_ = os.Remove(archive)
	return "", nil
}

// prebuiltURL is the release download link of the channel's prebuilt client.
func (b *webBuild) prebuiltURL() string {
	return fmt.Sprintf("%s/%s/releases/download/%s/%s", b.base(), Repo, prebuiltReleaseTag(b.channel), prebuiltArchive)
}

// releaseAPIURL is the GitHub API URL of the same release, which serves the
// asset when the download link answers 504 (install_helpers.js fetchPrebuiltArchive).
func (b *webBuild) releaseAPIURL() string {
	return fmt.Sprintf("%s/repos/%s/releases/tags/%s", b.apiBase(), Repo, prebuiltReleaseTag(b.channel))
}

// base and apiBase are the GitHub roots, so a test can serve the download.
func (b *webBuild) base() string {
	if b.githubBase != "" {
		return strings.TrimRight(b.githubBase, "/")
	}
	return defaultGitHubBase
}

func (b *webBuild) apiBase() string {
	if b.githubAPIBase != "" {
		return strings.TrimRight(b.githubAPIBase, "/")
	}
	return defaultGitHubAPIBase
}

// apiAssetURL is the API download URL of the prebuilt archive, or "" when the
// release or the asset is not there.
func (b *webBuild) apiAssetURL(ctx context.Context) string {
	client := b.httpClient()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.releaseAPIURL(), nil)
	if err != nil {
		return ""
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	var release struct {
		Assets []struct {
			Name string `json:"name"`
			URL  string `json:"url"`
		} `json:"assets"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&release) != nil {
		return ""
	}
	for _, asset := range release.Assets {
		if asset.Name == prebuiltArchive && asset.URL != "" {
			return asset.URL
		}
	}
	return ""
}

// download writes url to path, following the API's octet-stream redirect.
func (b *webBuild) download(ctx context.Context, url, path string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/octet-stream")
	resp, err := b.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("manager: download %s: HTTP %d", url, resp.StatusCode)
	}
	out, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, resp.Body); err != nil {
		out.Close()
		_ = os.Remove(path)
		return err
	}
	return out.Close()
}

func (b *webBuild) httpClient() *http.Client {
	return &http.Client{Timeout: 5 * time.Minute}
}

// localFlutter is a Flutter installed into the install's own tools folder.
func localFlutter(toolsDir string) string {
	if toolsDir == "" {
		return ""
	}
	name := "flutter"
	if Platform() == "windows" {
		name = "flutter.bat"
	}
	binary := filepath.Join(toolsDir, "flutter", "bin", name)
	if exists(binary) {
		return binary
	}
	return ""
}

// installFlutter clones Flutter stable into the tools folder and precaches the
// web engine, once, when nothing else can produce the client.
func (b *webBuild) installFlutter(ctx context.Context) (string, error) {
	if err := os.MkdirAll(b.toolsDir, 0o700); err != nil {
		return "", err
	}
	target := filepath.Join(b.toolsDir, "flutter")
	_ = os.RemoveAll(target)
	if _, err := b.run(ctx, "", "git", "clone", "--depth", "1", "--branch", "stable",
		"https://github.com/flutter/flutter.git", target); err != nil {
		return "", nil
	}
	flutter := localFlutter(b.toolsDir)
	if flutter == "" {
		return "", nil
	}
	// The first run fetches the Dart SDK and the web engine.
	_, _ = b.run(ctx, "", flutter, "config", "--no-analytics")
	if _, err := b.run(ctx, "", flutter, "precache", "--web"); err != nil {
		return "", nil
	}
	return flutter, nil
}

// versionPattern matches a real Flutter version; a shallow clone reports
// something else and is left alone.
var versionPattern = regexp.MustCompile(`^[1-9]\d*\.\d+\.\d+`)

// upgradeOlderFlutter updates a Flutter older than the client needs. A version
// that cannot be read is left alone, so a shallow clone is never upgraded.
func (b *webBuild) upgradeOlderFlutter(ctx context.Context, flutter string) {
	out, err := b.run(ctx, "", flutter, "--version", "--machine")
	if err != nil {
		return
	}
	// First runs can print banners before the JSON.
	start := strings.Index(out, "{")
	if start < 0 {
		return
	}
	var version struct {
		FrameworkVersion string `json:"frameworkVersion"`
	}
	if json.Unmarshal([]byte(out[start:]), &version) != nil {
		return
	}
	if !versionPattern.MatchString(version.FrameworkVersion) || !isOlderVersion(version.FrameworkVersion, requiredFlutter) {
		return
	}
	b.info("Updating Flutter to " + requiredFlutter + " or newer")
	_, _ = b.run(ctx, "", flutter, "upgrade")
}

// isOlderVersion compares the numeric part of two versions.
func isOlderVersion(version, minimum string) bool {
	parse := func(value string) []int {
		parts := strings.SplitN(value, "-", 2)[0]
		fields := strings.Split(parts, ".")
		out := make([]int, len(fields))
		for i, field := range fields {
			n := 0
			fmt.Sscan(field, &n)
			out[i] = n
		}
		return out
	}
	left, right := parse(version), parse(minimum)
	for i := range right {
		if i >= len(left) {
			return true
		}
		if left[i] != right[i] {
			return left[i] < right[i]
		}
	}
	return false
}

// swapIn replaces the live client with the new build using two renames, so the
// old one is out of the way for only a moment.
func swapIn(nextDir, liveDir string) error {
	oldDir := liveDir + "-old"
	_ = os.RemoveAll(oldDir)
	if exists(liveDir) {
		if err := os.Rename(liveDir, oldDir); err != nil {
			return fmt.Errorf("manager: replace web client: %w", err)
		}
	}
	if err := os.Rename(nextDir, liveDir); err != nil {
		_ = os.Rename(oldDir, liveDir)
		return fmt.Errorf("manager: replace web client: %w", err)
	}
	_ = os.RemoveAll(oldDir)
	return nil
}

// ExtractTarGz unpacks a .tar.gz into dest, refusing entries that would land
// outside it and links of any kind.
func ExtractTarGz(archive, dest string) error {
	file, err := os.Open(archive)
	if err != nil {
		return fmt.Errorf("manager: open archive: %w", err)
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("manager: read archive: %w", err)
	}
	defer gz.Close()
	root, err := filepath.Abs(dest)
	if err != nil {
		return err
	}
	reader := tar.NewReader(gz)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("manager: read archive: %w", err)
		}
		rel := filepath.Clean(filepath.FromSlash(strings.TrimPrefix(header.Name, "./")))
		if rel == "." || rel == "" || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
			return fmt.Errorf("manager: archive contains an unsafe path: %q", header.Name)
		}
		target := filepath.Join(root, rel)
		if target != root && !strings.HasPrefix(target, root+string(filepath.Separator)) {
			return fmt.Errorf("manager: archive entry escapes its directory: %q", header.Name)
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o700); err != nil {
				return fmt.Errorf("manager: create %s: %w", target, err)
			}
		case tar.TypeReg:
			if err := writeArchiveFile(target, reader); err != nil {
				return err
			}
		default:
			return fmt.Errorf("manager: archive contains an unsupported entry: %q", header.Name)
		}
	}
}

func writeArchiveFile(target string, reader io.Reader) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return fmt.Errorf("manager: create %s: %w", filepath.Dir(target), err)
	}
	out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("manager: create %s: %w", target, err)
	}
	if _, err := io.Copy(out, reader); err != nil {
		out.Close()
		return fmt.Errorf("manager: write %s: %w", target, err)
	}
	return out.Close()
}

// tailLines is the last twelve lines a failed command printed.
func tailLines(text string) string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	if len(lines) > 12 {
		lines = lines[len(lines)-12:]
	}
	return strings.Join(lines, "\n")
}

// cmdRebuildWeb is `neorouter rebuild-web`: build the client from flutter_app.
func (m *Manager) cmdRebuildWeb(ctx context.Context, _ []string) error {
	cfg, err := m.configWithPaths()
	if err != nil {
		return err
	}
	if err := m.UI.Step("Rebuilding the app", "Rebuilt the app", func() error {
		return m.ensureWebClient(ctx, cfg, true)
	}); err != nil {
		return err
	}
	m.UI.Blank()
	m.UI.OK("App rebuilt")
	m.UI.Blank()
	return nil
}
