package updates

import (
	"os"
	"path/filepath"
	"testing"
)

// TestBetaChannelStaysOnBeta is test/unit/release_channel.test.js: the beta
// channel stays on the beta branch unless a stable release is newer.
func TestBetaChannelStaysOnBeta(t *testing.T) {
	cases := []struct {
		name     string
		channel  string
		versions ChannelVersions
		want     string
	}{
		{"no tags at all", "beta", ChannelVersions{}, "beta"},
		{"no beta tag yet", "beta", ChannelVersions{Stable: "0.1.0"}, "beta"},
		{"beta ahead", "beta", ChannelVersions{Beta: "0.2.0-beta.1", Stable: "0.1.0"}, "beta"},
		{"the stable release caught up", "beta", ChannelVersions{Beta: "0.2.0-beta.1", Stable: "0.2.0"}, "main"},
		{"stable channel", "stable", ChannelVersions{Beta: "0.3.0-beta.1"}, "main"},
	}
	for _, c := range cases {
		if got := ChoosePreferredBranchForChannel(c.channel, c.versions); got != c.want {
			t.Fatalf("%s: branch = %q, want %q", c.name, got, c.want)
		}
	}
}

// TestChannelNames keeps the accepted spellings and the beta default.
func TestChannelNames(t *testing.T) {
	for _, value := range []string{"stable", "normal", "default", "latest", "main", " STABLE "} {
		if got := ParseChannel(value); got != "stable" {
			t.Fatalf("ParseChannel(%q) = %q, want stable", value, got)
		}
	}
	for _, value := range []string{"beta", "preview", "prerelease", "pre-release", "Beta"} {
		if got := ParseChannel(value); got != "beta" {
			t.Fatalf("ParseChannel(%q) = %q, want beta", value, got)
		}
	}
	for _, value := range []string{"", "nightly", "dev"} {
		if got := ParseChannel(value); got != "" {
			t.Fatalf("ParseChannel(%q) = %q, want no channel", value, got)
		}
		if got := NormalizeChannel(value); got != DefaultChannel {
			t.Fatalf("NormalizeChannel(%q) = %q, want beta", value, got)
		}
	}
	if DefaultChannel != "beta" {
		t.Fatalf("default channel = %q, want beta", DefaultChannel)
	}
	if ChannelBranch("stable") != "main" || ChannelBranch("beta") != "beta" {
		t.Fatal("branch mapping changed")
	}
	if ChannelDistTag("stable") != "latest" || ChannelDistTag("beta") != "beta" {
		t.Fatal("dist-tag mapping changed")
	}
	if ChannelLabel("stable") != "Stable" || ChannelLabel("beta") != "Beta" || ChannelLabel("") != "Beta" {
		t.Fatal("label mapping changed")
	}
	if got := DescribeChannelPolicy("beta"); got != "Beta (git newest of beta or main, npm newest of beta or latest)" {
		t.Fatalf("policy = %q", got)
	}
	if got := DescribeChannelPolicy("stable"); got != "Stable (git main only, npm latest only)" {
		t.Fatalf("policy = %q", got)
	}
}

// TestPreferredNPMTagForChannel covers choosePreferredNpmTagForChannel.
func TestPreferredNPMTagForChannel(t *testing.T) {
	if got := ChoosePreferredNPMTagForChannel("stable", NPMVersions{Beta: "9.0.0", Latest: "1.0.0"}); got != "latest" {
		t.Fatalf("stable tag = %q", got)
	}
	if got := ChoosePreferredNPMTagForChannel("beta", NPMVersions{Beta: "0.2.0", Latest: "0.1.0"}); got != "beta" {
		t.Fatalf("beta tag = %q", got)
	}
	if got := ChoosePreferredNPMTagForChannel("beta", NPMVersions{Beta: "0.1.0", Latest: "0.2.0"}); got != "latest" {
		t.Fatalf("beta tag = %q", got)
	}
}

// TestConfiguredChannelPrecedence keeps the environment ahead of the .env file
// and the default behind both.
func TestConfiguredChannelPrecedence(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")
	if got := ReadConfiguredChannel(nil, envFile); got != DefaultChannel {
		t.Fatalf("missing file = %q, want beta", got)
	}
	if err := os.WriteFile(envFile, []byte("# comment\nNEOROUTER_RELEASE_CHANNEL=stable\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := ReadConfiguredChannel(func(string) string { return "" }, envFile); got != "stable" {
		t.Fatalf("env file = %q, want stable", got)
	}
	if got := ReadConfiguredChannel(func(k string) string {
		if k == ChannelEnvKey {
			return "beta"
		}
		return ""
	}, envFile); got != "beta" {
		t.Fatalf("environment = %q, want beta", got)
	}
	if got := ReadChannelFromRaw("A=1\nNEOROUTER_RELEASE_CHANNEL=preview\n"); got != "beta" {
		t.Fatalf("raw = %q, want beta", got)
	}
}

// TestWriteChannelToEnvFile keeps the file private, replaces the existing line
// and rejects an unknown channel.
func TestWriteChannelToEnvFile(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")
	if err := os.WriteFile(envFile, []byte("A=1\nNEOROUTER_RELEASE_CHANNEL=beta\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := WriteChannelToEnvFile("stable", envFile)
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if got != "stable" {
		t.Fatalf("returned %q", got)
	}
	raw, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "A=1\nNEOROUTER_RELEASE_CHANNEL=stable\n" {
		t.Fatalf("env file = %q", raw)
	}
	info, err := os.Stat(envFile)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("mode = %v, want 0600", mode)
	}
	if _, err := WriteChannelToEnvFile("nightly", envFile); err == nil {
		t.Fatal("an unknown channel must be refused")
	}
}

// TestVersionOrdering pins the semver comparisons the channel choice relies on.
func TestVersionOrdering(t *testing.T) {
	if CompareVersionsPtr(ptr("0.2.0"), ptr("0.2.0-beta.1")) <= 0 {
		t.Fatal("a release must sort after its prerelease")
	}
	if CompareVersionsPtr(nil, ptr("0.1.0")) >= 0 {
		t.Fatal("a missing version must sort first")
	}
}

func ptr[T any](v T) *T { return &v }
