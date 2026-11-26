package manager

// The signed runtime bundles NeoRouter updates itself with
// (CONVERSION_PLAN.md "Migration design" 4): the release tooling in
// cmd/neorouter-release builds one archive per platform, signs a manifest that
// lists them, and both sides agree on the names here.

import (
	_ "embed"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/NeoLabs-Systems/neolabs-go/cli/release"
	"github.com/NeoLabs-Systems/neorouter/internal/config"
)

const (
	// Repo is the GitHub repository releases are published in.
	Repo = "NeoLabs-Systems/NeoRouter"
	// RuntimeManifestPrefix names a release's signed runtime manifest; the
	// signature is the same name plus ".sig".
	RuntimeManifestPrefix = "neorouter-runtime-manifest-"
	// AppCommand is the CLI the release assets are named after.
	AppCommand = "neorouter"
)

//go:embed public_key.txt
var embeddedPublicKey string

// RuntimePublicKey is the Ed25519 key a downloaded runtime manifest must be
// signed with, base64 of the raw 32 bytes. CI holds the private half in the
// NEOROUTER_RUNTIME_SIGNING_PRIVATE_KEY secret.
func RuntimePublicKey() (string, error) {
	return release.NormalizePublicKey(strings.TrimSpace(embeddedPublicKey))
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
	if runtime.GOOS == "darwin" {
		return "macos"
	}
	return runtime.GOOS
}

// Architecture is the release architecture name of this machine (x64, arm64).
func Architecture() string {
	if runtime.GOARCH == "amd64" {
		return "x64"
	}
	return runtime.GOARCH
}

// SupportedTargets are the platforms a runtime manifest may carry.
func SupportedTargets() release.Targets {
	return release.Targets{"linux": {"x64", "arm64"}, "macos": {"x64", "arm64"}, "windows": {"x64"}}
}

// RuntimeAssetName is one platform's archive: neorouter-runtime-<platform>-<arch>-<version>.zip.
func RuntimeAssetName(platform, arch, version string) string {
	return fmt.Sprintf("%s-runtime-%s-%s-%s.zip", AppCommand, platform, arch, strings.TrimPrefix(version, "v"))
}

// RuntimeArchiveRoot is the top-level directory of a runtime archive. It holds
// bin/neorouter plus the web client and landing page the server serves.
const RuntimeArchiveRoot = "app"

// RuntimeStore is where downloaded runtimes live: <home>/app/versions/<version>,
// with current.json naming the active one. The required files are what makes an
// activated runtime complete enough to serve.
func RuntimeStore(cfg config.Config) release.RuntimeStore {
	return release.RuntimeStore{
		Root: filepath.Join(cfg.Paths.Home, RuntimeArchiveRoot),
		RequiredFiles: []string{
			filepath.Join("bin", BinaryName()),
			filepath.Join("flutter_app", "build", "web", "index.html"),
		},
		SchemaVersion: 1,
	}
}

// runtimeBinary is the binary of an installed runtime version.
func runtimeBinary(dir string) string { return filepath.Join(dir, "bin", BinaryName()) }

// hasManifest reports whether a release carries the signed manifest for version.
func hasManifest(assets map[string]string, version string) bool {
	name := RuntimeManifestPrefix + version + ".json"
	return assets[name] != "" && assets[name+".sig"] != ""
}
