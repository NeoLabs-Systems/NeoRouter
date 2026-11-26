package main

// The signed runtime archives an install updates itself with: one zip per
// platform holding the server binary, the web client and the landing page, plus a
// manifest that lists their digests and is signed with the release key. The
// manifest format and the archive layout are what internal/manager expects
// (RuntimeStore, RuntimeArchiveRoot), and CI attaches both to the release.

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/NeoLabs-Systems/neolabs-go/cli/release"
	"github.com/NeoLabs-Systems/neorouter/internal/manager"
)

// runtimeTarget is one platform's runtime archive and the CLI binary it carries.
type runtimeTarget struct {
	platform, arch, binary string
}

// runtimeTargets are the platforms a release publishes.
var runtimeTargets = []runtimeTarget{
	{"linux", "x64", "neorouter-cli-linux-x64"},
	{"linux", "arm64", "neorouter-cli-linux-arm64"},
	{"macos", "x64", "neorouter-cli-macos-x64"},
	{"macos", "arm64", "neorouter-cli-macos-arm64"},
	{"windows", "x64", "neorouter-cli-windows-x64.exe"},
}

// bundleAssets are the directories that travel with the binary, relative to both
// the repository and the runtime directory.
var bundleAssets = []string{"flutter_app/build/web", "landing"}

// BuildRuntimeBundles packages the binaries in dist into the runtime archives of
// version and signs the manifest that lists them.
func BuildRuntimeBundles(root, dist, version, privateKey string) error {
	publicKey, err := manager.RuntimePublicKey()
	if err != nil {
		return err
	}
	return buildRuntimeBundles(root, dist, version, privateKey, publicKey)
}

func buildRuntimeBundles(root, dist, version, privateKey, publicKey string) error {
	version = strings.TrimPrefix(strings.TrimSpace(version), "v")
	if _, err := release.NormalizeVersion(version); err != nil {
		return err
	}
	if strings.TrimSpace(privateKey) == "" {
		return errors.New("NEOROUTER_RUNTIME_SIGNING_PRIVATE_KEY is required to sign runtime manifests")
	}
	if err := release.CheckKeypair(privateKey, publicKey); err != nil {
		return fmt.Errorf("the signing key does not match the embedded runtime public key: %w", err)
	}
	if err := os.MkdirAll(dist, 0o755); err != nil {
		return err
	}
	var artifacts []release.Artifact
	for _, target := range runtimeTargets {
		stage, err := os.MkdirTemp("", "neorouter-runtime-")
		if err != nil {
			return err
		}
		artifact, err := stageTarget(root, dist, stage, version, target)
		cleanupErr := os.RemoveAll(stage)
		if err != nil {
			return err
		}
		if cleanupErr != nil {
			return cleanupErr
		}
		artifacts = append(artifacts, artifact)
	}
	manifest, err := release.NewRuntimeManifest(1, version, artifacts, manager.SupportedTargets())
	if err != nil {
		return err
	}
	data, err := release.Serialize(manifest)
	if err != nil {
		return err
	}
	return release.WriteSigned(filepath.Join(dist, manager.RuntimeManifestPrefix+version+".json"), data, privateKey, publicKey)
}

// stageTarget builds one platform's archive and returns its manifest entry.
func stageTarget(root, dist, stage, version string, target runtimeTarget) (release.Artifact, error) {
	for _, asset := range bundleAssets {
		index := filepath.Join(root, filepath.FromSlash(asset), "index.html")
		info, err := os.Stat(index)
		if err != nil {
			return release.Artifact{}, fmt.Errorf("%s is missing: %w", asset, err)
		}
		if !info.Mode().IsRegular() {
			return release.Artifact{}, fmt.Errorf("%s/index.html is not a regular file", asset)
		}
	}
	binaryName := manager.BinaryName()
	if target.platform == "windows" {
		binaryName = "neorouter.exe"
	}
	if err := os.MkdirAll(filepath.Join(stage, "bin"), 0o755); err != nil {
		return release.Artifact{}, err
	}
	executable, err := os.ReadFile(filepath.Join(dist, target.binary))
	if err != nil {
		return release.Artifact{}, fmt.Errorf("read %s (was it built?): %w", target.binary, err)
	}
	if err := os.WriteFile(filepath.Join(stage, "bin", binaryName), executable, 0o755); err != nil {
		return release.Artifact{}, err
	}
	for _, asset := range bundleAssets {
		source := filepath.Join(root, filepath.FromSlash(asset))
		if err := os.CopyFS(filepath.Join(stage, filepath.FromSlash(asset)), os.DirFS(source)); err != nil {
			return release.Artifact{}, err
		}
	}
	name := manager.RuntimeAssetName(target.platform, target.arch, version)
	archive := filepath.Join(dist, name)
	if err := release.CreateZip(stage, archive); err != nil {
		return release.Artifact{}, err
	}
	digest, size, err := release.FileDigest(archive)
	if err != nil {
		return release.Artifact{}, err
	}
	fmt.Printf("built %s (%d bytes)\n", name, size)
	return release.Artifact{Platform: target.platform, Architecture: target.arch, AssetName: name, SHA256: digest, SizeBytes: size}, nil
}

// WriteKeypair generates the Ed25519 release keypair in dir: the private half is
// what CI keeps as NEOROUTER_RUNTIME_SIGNING_PRIVATE_KEY, and the public half is
// what internal/manager/public_key.txt must hold. Only the public key is printed.
func WriteKeypair(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	der, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return err
	}
	privatePath := filepath.Join(dir, "neorouter-runtime-signing.key")
	if err := os.WriteFile(privatePath, []byte(base64.StdEncoding.EncodeToString(der)+"\n"), 0o600); err != nil {
		return err
	}
	fmt.Println(base64.StdEncoding.EncodeToString(publicKey))
	fmt.Fprintf(os.Stderr, "private key written to %s: store it as the NEOROUTER_RUNTIME_SIGNING_PRIVATE_KEY secret\n", privatePath)
	fmt.Fprintf(os.Stderr, "put the printed public key in %s\n", filepath.Join("internal", "manager", "public_key.txt"))
	return nil
}
