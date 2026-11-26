package main

// The release tool's contract with CI: version resolution and bumping, the signed
// runtime bundles an install updates itself with, and the download section of a
// release's notes.

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NeoLabs-Systems/neolabs-go/cli/release"
	"github.com/NeoLabs-Systems/neorouter/internal/manager"
)

// testKeypair is one throwaway signing key.
type testKeypair struct{ public, private string }

func newTestKeypair(t *testing.T) testKeypair {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(private)
	if err != nil {
		t.Fatalf("marshal private key: %v", err)
	}
	return testKeypair{public: base64.StdEncoding.EncodeToString(public), private: base64.StdEncoding.EncodeToString(der)}
}

func TestNextVersionForTags(t *testing.T) {
	cases := []struct {
		name    string
		current string
		tags    []string
		channel string
		want    string
	}{
		{"first release", "0.1.0", nil, "stable", "0.1.0"},
		{"stable patch above the newest tag", "0.1.0", []string{"v0.1.0", "v0.2.0"}, "stable", "0.2.1"},
		{"the file version wins when it is ahead", "0.4.0", []string{"v0.2.0"}, "stable", "0.4.0"},
		{"the first beta above the stable tag", "0.2.0", []string{"v0.2.0"}, "beta", "0.2.1-beta.0"},
		{"the next beta counter", "0.3.0", []string{"v0.2.0", "v0.3.0-beta.0", "v0.3.0-beta.2"}, "beta", "0.3.0-beta.3"},
		{"a beta counts only for its own version", "0.3.0", []string{"v0.3.0-beta.7", "v0.4.0"}, "beta", "0.4.1-beta.0"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			got, err := NextVersionForTags(test.current, test.tags, test.channel)
			if err != nil {
				t.Fatalf("NextVersionForTags: %v", err)
			}
			if got != test.want {
				t.Errorf("version = %q, want %q", got, test.want)
			}
		})
	}
	if _, err := NextVersionForTags("0.1.0", nil, "gamma"); err == nil {
		t.Error("an unknown channel was accepted")
	}
}

func TestReadAndSetVersionTouchTheCommittedFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "internal", "config"), 0o700); err != nil {
		t.Fatalf("create config dir: %v", err)
	}
	config := "package config\n\n// Version is the release version.\nconst Version = \"0.2.1\"\n"
	if err := os.WriteFile(filepath.Join(root, configFile), []byte(config), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte("{\n  \"name\": \"neorouter\",\n  \"version\": \"0.2.1\"\n}\n"), 0o600); err != nil {
		t.Fatalf("write package.json: %v", err)
	}
	got, err := ReadVersion(root)
	if err != nil {
		t.Fatalf("ReadVersion: %v", err)
	}
	if got != "0.2.1" {
		t.Fatalf("version = %q, want 0.2.1", got)
	}
	if err := SetVersion(root, "v0.3.0-beta.1"); err != nil {
		t.Fatalf("SetVersion: %v", err)
	}
	for _, file := range []string{configFile, "package.json"} {
		raw, err := os.ReadFile(filepath.Join(root, file))
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		if !strings.Contains(string(raw), "0.3.0-beta.1") {
			t.Errorf("%s was not bumped:\n%s", file, raw)
		}
	}
	if err := SetVersion(root, "not-a-version"); err == nil {
		t.Error("an invalid version was accepted")
	}
}

func TestDownloadNotesMarkBuiltAndMissingPlatforms(t *testing.T) {
	assets := []ReleaseAsset{
		{Name: "neorouter-cli-linux-x64", URL: "https://example.test/linux"},
		{Name: manager.RuntimeAssetName("linux", "x64", "0.2.1"), URL: "https://example.test/runtime"},
	}
	notes := DownloadNotes("v0.2.1", manager.Repo, assets)
	if !strings.HasPrefix(notes, notesStart) || !strings.HasSuffix(notes, notesEnd) {
		t.Fatalf("the notes have no marker block:\n%s", notes)
	}
	for _, want := range []string{
		"https://example.test/linux", "Download NeoRouter v0.2.1",
		"/releases/tag/v0.2.1", "The rest attach as soon as their builds finish",
	} {
		if !strings.Contains(notes, want) {
			t.Errorf("the notes are missing %q:\n%s", want, notes)
		}
	}
	empty := DownloadNotes("v0.2.1", manager.Repo, nil)
	if !strings.Contains(empty, "The downloads are building now") {
		t.Errorf("empty notes = %s", empty)
	}
}

func TestMergeDownloadNotesReplacesItsOwnSection(t *testing.T) {
	body := "## What's new\n\n- a fix\n"
	section := DownloadNotes("v0.2.1", manager.Repo, nil)
	first := MergeDownloadNotes(body, section)
	if !strings.Contains(first, "## What's new") || !strings.Contains(first, notesStart) {
		t.Fatalf("merged notes lost a part:\n%s", first)
	}
	second := MergeDownloadNotes(first, section)
	if second != first {
		t.Errorf("merging twice changed the notes:\n%s\n%s", first, second)
	}
	if got := MergeDownloadNotes("", section); strings.TrimSpace(got) != strings.TrimSpace(section) {
		t.Errorf("merging into an empty body = %q", got)
	}
}

// releaseWorkspace is a checkout with the web client and landing page a runtime
// archive carries, plus the CLI binaries the build workflow produces.
func releaseWorkspace(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	dist := filepath.Join(root, "dist")
	for _, dir := range []string{
		filepath.Join(root, "flutter_app", "build", "web"),
		filepath.Join(root, "landing"),
		dist,
	} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("create %s: %v", dir, err)
		}
	}
	for _, file := range []string{
		filepath.Join(root, "flutter_app", "build", "web", "index.html"),
		filepath.Join(root, "flutter_app", "build", "web", "main.dart.js"),
		filepath.Join(root, "landing", "index.html"),
	} {
		if err := os.WriteFile(file, []byte("<!doctype html>"), 0o600); err != nil {
			t.Fatalf("write %s: %v", file, err)
		}
	}
	for _, target := range runtimeTargets {
		if err := os.WriteFile(filepath.Join(dist, target.binary), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatalf("write %s: %v", target.binary, err)
		}
	}
	return root
}

func TestBuildRuntimeBundlesSignsOneArchivePerPlatform(t *testing.T) {
	root := releaseWorkspace(t)
	dist := filepath.Join(root, "dist")
	keys := newTestKeypair(t)

	if err := buildRuntimeBundles(root, dist, "v0.3.0", keys.private, keys.public); err != nil {
		t.Fatalf("buildRuntimeBundles: %v", err)
	}
	manifestPath := filepath.Join(dist, manager.RuntimeManifestPrefix+"0.3.0.json")
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	signature, err := os.ReadFile(manifestPath + ".sig")
	if err != nil {
		t.Fatalf("read signature: %v", err)
	}
	if ok, err := release.Verify(raw, string(signature), keys.public); err != nil || !ok {
		t.Fatalf("the manifest is not signed by the release key: %v", err)
	}
	var manifest release.RuntimeManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	if manifest.Version != "0.3.0" || len(manifest.Artifacts) != len(runtimeTargets) {
		t.Fatalf("manifest = %+v", manifest)
	}
	for _, artifact := range manifest.Artifacts {
		want := manager.RuntimeAssetName(artifact.Platform, artifact.Architecture, "0.3.0")
		if artifact.AssetName != want {
			t.Errorf("asset = %q, want %q", artifact.AssetName, want)
		}
		digest, size, err := release.FileDigest(filepath.Join(dist, artifact.AssetName))
		if err != nil {
			t.Fatalf("digest %s: %v", artifact.AssetName, err)
		}
		if digest != artifact.SHA256 || size != artifact.SizeBytes {
			t.Errorf("%s does not match its manifest entry", artifact.AssetName)
		}
	}
	// An install unpacks exactly this archive shape.
	versionDir := t.TempDir()
	if err := release.ExtractZip(filepath.Join(dist, manifest.Artifacts[0].AssetName), versionDir); err != nil {
		t.Fatalf("extract: %v", err)
	}
	store := release.RuntimeStore{Root: filepath.Join(t.TempDir(), "app"), SchemaVersion: 1,
		RequiredFiles: []string{
			filepath.Join("bin", "neorouter"),
			filepath.Join("flutter_app", "build", "web", "index.html"),
		}}
	for _, name := range store.RequiredFiles {
		if _, err := os.Stat(filepath.Join(versionDir, name)); err != nil {
			t.Errorf("the archive is missing %s: %v", name, err)
		}
	}
}

func TestBuildRuntimeBundlesRefusesAMismatchedKey(t *testing.T) {
	root := releaseWorkspace(t)
	dist := filepath.Join(root, "dist")
	signer, other := newTestKeypair(t), newTestKeypair(t)
	err := buildRuntimeBundles(root, dist, "0.3.0", signer.private, other.public)
	if err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("err = %v, want a key mismatch", err)
	}
	if err := buildRuntimeBundles(root, dist, "0.3.0", "", signer.public); err == nil {
		t.Fatal("a bundle was built without a signing key")
	}
}

func TestBuildRuntimeBundlesNeedsTheWebClient(t *testing.T) {
	root := releaseWorkspace(t)
	dist := filepath.Join(root, "dist")
	keys := newTestKeypair(t)
	if err := os.Remove(filepath.Join(root, "flutter_app", "build", "web", "index.html")); err != nil {
		t.Fatalf("remove index: %v", err)
	}
	err := buildRuntimeBundles(root, dist, "0.3.0", keys.private, keys.public)
	if err == nil || !strings.Contains(err.Error(), "flutter_app/build/web is missing") {
		t.Fatalf("err = %v, want the missing web client", err)
	}
}

func TestWriteKeypairProducesAUsableKeyFile(t *testing.T) {
	dir := t.TempDir()
	if err := WriteKeypair(dir); err != nil {
		t.Fatalf("WriteKeypair: %v", err)
	}
	path := filepath.Join(dir, "neorouter-runtime-signing.key")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read private key: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat private key: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("private key mode = %v, want 0600", info.Mode())
	}
	private, err := release.ParsePrivateKey(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatalf("the file is not a signing key: %v", err)
	}
	public := base64.StdEncoding.EncodeToString(private.Public().(ed25519.PublicKey))
	if err := release.CheckKeypair(strings.TrimSpace(string(raw)), public); err != nil {
		t.Fatalf("the keypair does not sign and verify: %v", err)
	}
}
