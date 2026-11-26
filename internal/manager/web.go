package manager

// The browser app at /app. Releases carry it built; only a source checkout
// builds it.

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/NeoLabs-Systems/neorouter/internal/config"
)

// HasBundledWebClient reports whether dir holds a complete built client.
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

// cmdRebuildWeb is `neorouter rebuild-web`, for developers working on the app.
func (m *Manager) cmdRebuildWeb(ctx context.Context, _ []string) error {
	cfg, err := m.loadConfig()
	if err != nil {
		return err
	}
	sources := config.FlutterAppDir(cfg.AppDir)
	if !exists(filepath.Join(sources, "pubspec.yaml")) {
		return inputError("There are no app sources to build here. Releases already include the app; run this from a source checkout.")
	}
	return m.UI.Step("Building the app — this can take a few minutes", "Built the app", func() error {
		for _, args := range [][]string{
			{"pub", "get"},
			// The full icon font keeps its glyphs stable across releases.
			{"build", "web", "--release", "--no-tree-shake-icons", "--base-href", "/app/"},
		} {
			if out, err := m.run(ctx, sources, "flutter", args...); err != nil {
				lines := strings.Split(strings.TrimSpace(out), "\n")
				return fmt.Errorf("flutter %s failed: %w\n%s", args[0], err, strings.Join(lines[max(0, len(lines)-15):], "\n"))
			}
		}
		return nil
	})
}
