package manager

// The signed runtime bundles NeoRouter installs and updates itself with: the
// release tooling in cmd/neorouter-release builds one archive per platform,
// signs a manifest that lists them, and both sides agree on the names here.

import (
	_ "embed"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/NeoLabs-Systems/neolabs-go/cli"
	"github.com/NeoLabs-Systems/neolabs-go/cli/release"
	"github.com/NeoLabs-Systems/neolabs-go/cli/update"
	"github.com/NeoLabs-Systems/neorouter/internal/config"
	"github.com/NeoLabs-Systems/neorouter/internal/updates"
)

const (
	// Repo is the GitHub repository releases are published in.
	Repo = updates.Repo
	// RuntimeManifestPrefix names a release's signed runtime manifest.
	RuntimeManifestPrefix = updates.RuntimeManifestPrefix
	// AppCommand is the CLI the release assets are named after.
	AppCommand = "neorouter"
	// RuntimeArchiveRoot is the top-level directory of a runtime archive and
	// the folder under the NeoRouter home that holds installed runtimes.
	RuntimeArchiveRoot = "app"
)

// public_key.txt is the release key (`neorouter-release runtime-keygen`); a
// build without one trusts nothing signed.
//
//go:embed public_key.txt
var embeddedPublicKey string

// RuntimePublicKey is the Ed25519 key a downloaded runtime manifest must be
// signed with, base64 of the raw 32 bytes (the file holds either form). CI holds the private half in the
// NEOROUTER_RUNTIME_SIGNING_PRIVATE_KEY secret.
func RuntimePublicKey() (string, error) { return parseTrustRoot(embeddedPublicKey) }

func parseTrustRoot(embedded string) (string, error) {
	if strings.TrimSpace(embedded) == "" {
		return "", cli.Errorf("SETUP_TRUST_NOT_CONFIGURED", "This NeoRouter build has no release signing key, "+
			"so it cannot verify or install downloaded releases. The project owner has to publish one first.")
	}
	return release.NormalizePublicKey(strings.TrimSpace(embedded))
}

// BinaryName is the server binary inside a runtime archive on this platform.
func BinaryName() string {
	if runtime.GOOS == "windows" {
		return AppCommand + ".exe"
	}
	return AppCommand
}

// Platform is the release platform name of this machine (linux, macos, windows).
func Platform() string {
	platform, _ := update.Host()
	return platform
}

// Architecture is the release architecture name of this machine (x64, arm64).
func Architecture() string {
	_, architecture := update.Host()
	return architecture
}

// SupportedTargets are the platforms every runtime release carries.
func SupportedTargets() release.Targets {
	return release.Targets{"linux": {"x64", "arm64"}, "macos": {"x64", "arm64"}, "windows": {"x64"}}
}

// RuntimeAssetName is one platform's archive: neorouter-runtime-<platform>-<arch>-<version>.zip.
func RuntimeAssetName(platform, arch, version string) string {
	return fmt.Sprintf("%s-runtime-%s-%s-%s.zip", AppCommand, platform, arch, strings.TrimPrefix(version, "v"))
}

// RuntimeStore is where downloaded runtimes live: <home>/app/versions/<version>,
// with current.json naming the active one. The required files are what makes an
// activated runtime complete enough to serve.
func RuntimeStore(cfg config.Config) release.RuntimeStore {
	return release.RuntimeStore{
		Root: filepath.Join(cfg.Paths.Home, RuntimeArchiveRoot),
		RequiredFiles: []string{
			filepath.Join(RuntimeArchiveRoot, "bin", BinaryName()),
			filepath.Join(RuntimeArchiveRoot, "flutter_app", "build", "web", "index.html"),
		},
		SchemaVersion: 1,
	}
}

// runtimeAppDir is the app directory of an installed runtime version.
func runtimeAppDir(versionDir string) string { return filepath.Join(versionDir, RuntimeArchiveRoot) }

// runtimeBinary is the binary of an installed runtime version.
func runtimeBinary(versionDir string) string {
	return filepath.Join(runtimeAppDir(versionDir), "bin", BinaryName())
}
