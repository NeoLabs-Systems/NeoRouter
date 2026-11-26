package updates

import (
	"os"
	"path/filepath"
	"testing"
)

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
		if got := NormalizeChannel(value); got != "stable" {
			t.Fatalf("NormalizeChannel(%q) = %q, want stable", value, got)
		}
	}
	if ChannelBranch("stable") != "main" || ChannelBranch("beta") != "beta" {
		t.Fatal("branch mapping changed")
	}
	if ChannelLabel("beta") != "Beta" || ChannelLabel("") != "Stable" {
		t.Fatal("channel label changed")
	}
}

func TestConfiguredChannelPrefersTheEnvironment(t *testing.T) {
	envFile := filepath.Join(t.TempDir(), ".env")
	if got := ReadConfiguredChannel(nil, envFile); got != "stable" {
		t.Fatalf("channel without an env file = %q", got)
	}
	if _, err := WriteChannelToEnvFile("preview", envFile); err != nil {
		t.Fatal(err)
	}
	if got := ReadConfiguredChannel(func(string) string { return "" }, envFile); got != "beta" {
		t.Fatalf("channel from the env file = %q", got)
	}
	if got := ReadConfiguredChannel(func(string) string { return "stable" }, envFile); got != "stable" {
		t.Fatalf("channel from the environment = %q", got)
	}
	if _, err := WriteChannelToEnvFile("nightly", envFile); err == nil {
		t.Fatal("an unknown channel was stored")
	}
	raw, err := os.ReadFile(envFile)
	if err != nil || string(raw) != "NEOROUTER_RELEASE_CHANNEL=beta\n" {
		t.Fatalf("env file = %q, %v", raw, err)
	}
}
