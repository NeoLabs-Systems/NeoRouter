package processing

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// TestShieldTextsHidesOnlySecrets is the ported "embeddings and image prompts hide
// secrets only" behaviour: values that must never leave, nothing that could be put
// back (a name would break what the caller asked for).
func TestShieldTextsHidesOnlySecrets(t *testing.T) {
	service, _ := testService(t)
	settings := DefaultSettings()
	settings.Privacy.Enabled = true
	if _, err := service.SaveSettings(context.Background(), "u1", settings); err != nil {
		t.Fatal(err)
	}
	text := "deploy key sk-or-v1-abcdefghijklmnopqrstuvwxyz0123456789 for jana@example.org"
	texts, redacted := service.ShieldTexts(context.Background(), "u1", []string{text, "plain"})
	if redacted != 1 {
		t.Fatalf("redacted %d values", redacted)
	}
	want := "deploy key [SECRET_1] for jana@example.org"
	if texts[0] != want {
		t.Fatalf("shielded %q, want %q", texts[0], want)
	}
	if texts[1] != "plain" {
		t.Fatalf("plain text %q", texts[1])
	}

	// Switching the shield off leaves the texts alone.
	settings.Privacy.Enabled = false
	if _, err := service.SaveSettings(context.Background(), "u1", settings); err != nil {
		t.Fatal(err)
	}
	if texts, redacted := service.ShieldTexts(context.Background(), "u1", []string{text}); redacted != 0 || texts[0] != text {
		t.Fatalf("with the shield off: %q / %d", texts[0], redacted)
	}
}

// TestApplyHonoursTheHeaderLevel proves a request header may lower the compression
// level and the pool minimum may raise the shield.
func TestApplyHonoursTheHeaderLevel(t *testing.T) {
	service, _ := testService(t)
	settings := DefaultSettings()
	settings.Compression.Level = "aggressive"
	settings.Compression.MinTokens = 10
	canonical := Canonical{"messages": []any{map[string]any{"role": "user", "content": "Hello   \n\n\n\nworld"}}}
	result, err := service.Apply(context.Background(), "u1", canonical, ApplyOptions{Settings: &settings, CompressionLevel: "lossless"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Stats.Compression.Level != "lossless" {
		t.Fatalf("level %q, want lossless", result.Stats.Compression.Level)
	}
	// A pool partner raises the modes to at least the pool's minimum.
	service.PoolMinimum = func(own PrivacyConfig) PrivacyConfig {
		own.Enabled = true
		own.Modes = map[string]string{}
		for category, mode := range DefaultModes {
			own.Modes[category] = mode
		}
		own.Modes["email"] = "hide"
		return own
	}
	result, err = service.Apply(context.Background(), "u1", userMessage("mail jana@example.org"), ApplyOptions{Settings: &settings, PoolMinimum: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Vault == nil || result.Vault.Size() != 1 {
		t.Fatalf("the pool minimum turns the shield on: %+v", result.Stats)
	}
	if lastContent(result.Canonical) != "mail [EMAIL_1]" {
		t.Fatalf("redacted request %q", lastContent(result.Canonical))
	}
	if result.Stats.RedactedByCategory["email"] != 1 || result.Stats.Redacted != 1 {
		t.Fatalf("stats %+v", result.Stats)
	}
}

// TestSettingsRoundTrip proves the stored JSON is merged over the defaults, which
// is where a settings row written by the Node server may be partial.
func TestSettingsRoundTrip(t *testing.T) {
	service, store := testService(t)
	stored := `{"privacy":{"enabled":true,"customTerms":["Acme Holding"]},"compression":{"level":"balanced"}}`
	if err := store.SetUserSetting(context.Background(), "u1", SettingKey, json.RawMessage(stored)); err != nil {
		t.Fatal(err)
	}
	settings := service.GetSettings(context.Background(), "u1")
	if !settings.Privacy.Enabled || len(settings.Privacy.CustomTerms) != 1 {
		t.Fatalf("privacy %+v", settings.Privacy)
	}
	if settings.Privacy.Modes["secret"] != "hide" || len(settings.Privacy.Modes) != len(Categories) {
		t.Fatalf("modes %+v", settings.Privacy.Modes)
	}
	if settings.Compression.Level != "balanced" || settings.Compression.MinTokens != 2000 || settings.Compression.KeepRecent != 6 {
		t.Fatalf("compression %+v", settings.Compression)
	}
	if settings.Compression.TargetTokens != nil {
		t.Fatalf("target tokens %v", *settings.Compression.TargetTokens)
	}
	encoded, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{`"customTerms":["Acme Holding"]`, `"allowTerms":[]`, `"targetTokens":null`, `"engines":{}`, `"level":"balanced"`} {
		if !strings.Contains(string(encoded), fragment) {
			t.Fatalf("%s missing from %s", fragment, encoded)
		}
	}
}

// TestPipelineOrder proves compression runs before the shield, so a value that
// compression cuts away is never redacted.
func TestPipelineOrder(t *testing.T) {
	service, _ := testService(t)
	settings := DefaultSettings()
	settings.Privacy.Enabled = true
	settings.Compression.Level = "balanced"
	settings.Compression.MinTokens = 10
	// The phone number sits in the middle of a long tool output, which the head and
	// tail cut drops: the shield never sees it.
	lines := strings.Split(longTool("run"), "\n")
	lines[len(lines)/2] = "call +49 170 1234567 today"
	canonical := Canonical{"messages": []any{
		map[string]any{"role": "assistant", "content": "step"},
		map[string]any{"role": "tool", "content": strings.Join(lines, "\n")},
		map[string]any{"role": "user", "content": "and now?"},
	}}
	result, err := service.Apply(context.Background(), "u1", canonical, ApplyOptions{Settings: &settings})
	if err != nil {
		t.Fatal(err)
	}
	if result.Stats.Compression.SavedTokens <= 0 {
		t.Fatalf("compression saved nothing: %+v", result.Stats.Compression)
	}
	if result.Vault != nil {
		t.Fatalf("the tool output was cut before the shield saw it, nothing to redact: %+v", result.Stats)
	}
}
