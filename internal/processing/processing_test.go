package processing

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestDetectorsValidate is the ported "detectors find real sensitive values and
// validate them" case of test/unit/processing.test.js: the checksums matter as
// much as the patterns.
func TestDetectorsValidate(t *testing.T) {
	fixture := loadVaultFixture(t)
	engine := testEngine(t)
	cases := []struct {
		text    string
		custom  []string
		allow   []string
		want    []string
		because string
	}{
		{text: "Mail anna.schmidt@example.de now", want: []string{"email:anna.schmidt@example.de"}},
		{text: "IBAN DE89 3704 0044 0532 0130 00 ok", want: []string{"iban:DE89 3704 0044 0532 0130 00"}},
		{text: "IBAN DE89 3704 0044 0532 0130 01", because: "a wrong check digit is not an IBAN"},
		{text: "card 4111 1111 1111 1111", want: []string{"card:4111 1111 1111 1111"}},
		{text: "order 4111 1111 1111 1112", because: "fails the Luhn check"},
		{text: "call +49 170 1234567 today", want: []string{"phone:+49 170 1234567"}},
		{text: "in 2026 we sold 1500 units"},
		{text: "key sk-or-v1-abcdefghijklmnopqrstuvwxyz0123456789", want: []string{"secret:sk-or-v1-abcdefghijklmnopqrstuvwxyz0123456789"}},
		{text: "password: " + fixture.Key, want: []string{"secret:" + fixture.Key}},
		{text: "das Server-Passwort: " + fixture.Key + ".", want: []string{"secret:" + fixture.Key}, because: "German keywords count too"},
		{text: "the pin is 4711", want: []string{"secret:4711"}},
		{text: "the topic is passwords in general", because: "the word alone is not a secret"},
		{text: "postgres://app:" + fixture.Key + "@db.local/x", want: []string{"secret:" + fixture.Key}},
		{text: "commit 9fceb02d0ae598e95dc970b74767f19372d61af8", because: "git hashes are not secrets"},
		{text: "server 10.0.4.17 down", want: []string{"ip:10.0.4.17"}},
		{text: "Grüße von Acme Holding", custom: []string{"Acme Holding"}, want: []string{"custom:Acme Holding"}},
	}
	for _, test := range cases {
		spans := engine.Scan(test.text, ScanOptions{Modes: DefaultModes, CustomTerms: test.custom, AllowTerms: test.allow})
		got := []string{}
		for _, span := range spans {
			got = append(got, span.Category+":"+span.Value)
		}
		if strings.Join(got, ",") != strings.Join(test.want, ",") {
			t.Errorf("%q: found %q, want %q (%s)", test.text, got, test.want, test.because)
		}
	}
}

// TestRequestsGetAnInstructionAndResponsesAreRestored is the ported third case.
func TestRequestsGetAnInstructionAndResponsesAreRestored(t *testing.T) {
	fixture := loadVaultFixture(t)
	engine := testEngine(t)
	redaction := RedactRequest(engine, userMessage("My mail is x@y.org"), RequestRedactOptions{Modes: restoreModes()})
	if redaction == nil {
		t.Fatal("the mail address should be hidden")
	}
	messages := redaction.Canonical.Messages()
	if messageRole(messages[0]) != "system" || !strings.Contains(messages[0]["content"].(string), "placeholders") {
		t.Fatalf("the model is told to keep placeholders: %v", messages[0])
	}
	_ = fixture
	if messages[1]["content"] != "My mail is [EMAIL_1]" {
		t.Fatalf("redacted message %q", messages[1]["content"])
	}
	answer := map[string]any{"choices": []any{map[string]any{"message": map[string]any{
		"role": "assistant", "content": "Sent to [EMAIL_1]",
	}}}}
	restored := RestoreResponse(answer, redaction.Vault)
	message := restored["choices"].([]any)[0].(map[string]any)["message"].(map[string]any)
	if message["content"] != "Sent to x@y.org" {
		t.Fatalf("restored answer %q", message["content"])
	}
	if RedactRequest(engine, userMessage("nothing private"), RequestRedactOptions{Modes: restoreModes()}) != nil {
		t.Fatal("a request without private values is left alone")
	}
}

// TestPlaceholdersSplitAcrossChunks is the ported chunk case.
func TestPlaceholdersSplitAcrossChunks(t *testing.T) {
	engine := testEngine(t)
	vault := NewVault(engine, restoreModes())
	vault.RedactText("x@y.org", RedactOptions{})
	restorer := NewStreamRestorer(vault)
	out := ""
	for _, piece := range []string{"Hi [EM", "AIL", "_1", "] and [", "unrelated] text"} {
		chunk := map[string]any{"choices": []any{map[string]any{"index": float64(0), "delta": map[string]any{"content": piece}}}}
		delta := restorer.Process(chunk)["choices"].([]any)[0].(map[string]any)["delta"].(map[string]any)
		content, _ := delta["content"].(string)
		out += content
	}
	final := map[string]any{"choices": []any{map[string]any{"index": float64(0), "delta": map[string]any{}, "finish_reason": "stop"}}}
	delta := restorer.Process(final)["choices"].([]any)[0].(map[string]any)["delta"].(map[string]any)
	if content, ok := delta["content"].(string); ok {
		out += content
	}
	if out != "Hi x@y.org and [unrelated] text" {
		t.Fatalf("streamed text %q", out)
	}
}

// fakePipeline is the model the JS test swapped in with setLibraryForTests.
type fakePipeline struct {
	classified bool
	disposed   bool
}

func (f *fakePipeline) Classify(ctx context.Context, text string) ([]RawEntity, error) {
	f.classified = true
	if strings.Contains(text, "Johanna Krause") {
		return []RawEntity{
			{EntityGroup: "private_person", Word: " Johanna Krause", Score: 0.98},
			{EntityGroup: "private_person", Word: "Made Up", Score: 0.9},
		}, nil
	}
	return nil, nil
}

func (f *fakePipeline) Dispose() error {
	f.disposed = true
	return nil
}

// TestPrivacyFilterAddsWhatItFinds is the ported privacy-filter case: nothing runs
// until an admin turns it on, then the filter's names are hidden and restored.
func TestPrivacyFilterAddsWhatItFinds(t *testing.T) {
	service, _ := testService(t)
	pipeline := &fakePipeline{}
	service.Privacy.SetLoader(func(ctx context.Context, progress func(float64)) (Pipeline, error) {
		return pipeline, nil
	})
	settings := DefaultSettings()
	settings.Privacy.Enabled = true
	settings.Privacy.AI = AIEnabled{Enabled: true}
	canonical := userMessage("Lunch with Johanna Krause tomorrow, okay?")

	off, err := service.Apply(context.Background(), "u1", canonical, ApplyOptions{Settings: &settings})
	if err != nil {
		t.Fatal(err)
	}
	if off.Stats.AIDetection {
		t.Fatal("nothing runs until an admin turns the filter on")
	}
	if _, err := service.Privacy.SetEnabled(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	defer service.Privacy.SetEnabled(context.Background(), false)
	on, err := service.Apply(context.Background(), "u1", canonical, ApplyOptions{Settings: &settings})
	if err != nil {
		t.Fatal(err)
	}
	if !on.Stats.AIDetection || !pipeline.classified {
		t.Fatal("the filter runs once it is on")
	}
	sent := lastContent(on.Canonical)
	if strings.Contains(sent, "Johanna Krause") || strings.Contains(sent, "Made Up") {
		t.Fatalf("the names are hidden: %q", sent)
	}
	if restored := on.Vault.RestoreText(sent, false); restored != "Lunch with Johanna Krause tomorrow, okay?" {
		t.Fatalf("placeholders are put back: %q", restored)
	}
}

// longTool is a tool output long enough for the lossy engines.
func longTool(seed string) string {
	lines := make([]string, 0, 400)
	for index := 0; index < 400; index++ {
		lines = append(lines, seed+" line "+itoa(index)+" "+strings.Repeat("x", 40))
	}
	return strings.Join(lines, "\n")
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	digits := ""
	for value > 0 {
		digits = string(rune('0'+value%10)) + digits
		value /= 10
	}
	return digits
}

// TestLosslessCompression is the ported lossless case: meaning kept, short chats
// untouched.
func TestLosslessCompression(t *testing.T) {
	canonical := Canonical{"messages": []any{
		map[string]any{"role": "system", "content": "Be brief."},
		map[string]any{"role": "tool", "content": "{\n  \"a\": 1,\n  \"b\": [1, 2, 3]\n}"},
		map[string]any{"role": "user", "content": "Hello   \n\n\n\nworld\n```\ncode   \n\n\n\n```"},
	}}
	settings := DefaultCompression()
	settings.Level = "lossless"
	result := Compress(canonical, settings, "")
	if got := lastContent(result.Canonical); got != "Hello\n\nworld\n```\ncode   \n\n\n\n```" {
		t.Fatalf("code blocks are untouched: %q", got)
	}
	messages := result.Canonical.Messages()
	if messages[1]["content"] != `{"a":1,"b":[1,2,3]}` {
		t.Fatalf("minified JSON %q", messages[1]["content"])
	}
	off := DefaultCompression()
	if got := Compress(canonical, off, ""); got.Stats.Level != "off" || got.Stats.SavedTokens != 0 {
		t.Fatalf("off keeps the request: %+v", got.Stats)
	}
}

// TestBalancedAndAggressiveCompression is the ported lossy case.
func TestBalancedAndAggressiveCompression(t *testing.T) {
	messages := []any{map[string]any{"role": "system", "content": "S"}}
	for index := 0; index < 5; index++ {
		messages = append(messages,
			map[string]any{"role": "assistant", "content": "step " + itoa(index)},
			map[string]any{"role": "tool", "content": longTool("run" + itoa(index))},
		)
	}
	messages = append(messages, map[string]any{"role": "user", "content": longTool("question")})
	canonical := Canonical{"messages": messages}
	balancedSettings := DefaultCompression()
	balancedSettings.Level = "balanced"
	balancedSettings.MinTokens = 100
	balanced := Compress(canonical, balancedSettings, "")
	if balanced.Stats.SavedTokens <= 0 {
		t.Fatalf("balanced saved nothing: %+v", balanced.Stats)
	}
	balancedMessages := balanced.Canonical.Messages()
	if content, _ := balancedMessages[2]["content"].(string); !strings.Contains(content, "characters omitted") {
		t.Fatalf("old tool output is cut: %q", content)
	}
	if balancedMessages[len(balancedMessages)-1]["content"] != messages[len(messages)-1].(map[string]any)["content"] {
		t.Fatal("the latest user message is kept whole")
	}
	aggressiveSettings := balancedSettings
	aggressiveSettings.Level = "aggressive"
	aggressive := Compress(canonical, aggressiveSettings, "")
	if content, _ := aggressive.Canonical.Messages()[2]["content"].(string); !strings.Contains(content, "Older tool output omitted") {
		t.Fatalf("older tool output is masked: %q", content)
	}
	if aggressive.Stats.SavedTokens <= balanced.Stats.SavedTokens {
		t.Fatalf("aggressive saves more: %+v vs %+v", aggressive.Stats, balanced.Stats)
	}
	shortSettings := aggressiveSettings
	shortSettings.MinTokens = 1_000_000
	short := Compress(Canonical{"messages": messages[:3]}, shortSettings, "")
	if _, ok := short.Stats.Engines["maskOldToolOutput"]; ok {
		t.Fatal("lossy engines wait for the size threshold")
	}
	if lowered := Compress(canonical, aggressiveSettings, "lossless"); lowered.Stats.Level != "lossless" {
		t.Fatalf("a header can only lower the level: %s", lowered.Stats.Level)
	}
}

// TestRepeatedLinesAndHeadTail pin the two lossy helpers.
func TestRepeatedLinesAndHeadTail(t *testing.T) {
	repeated := RepeatedLines("a\na\na\na\nb")
	if !strings.Contains(repeated, "previous line repeated 3 more times") {
		t.Fatalf("repeated lines: %q", repeated)
	}
	long := strings.Repeat("y", 5000)
	cut := HeadTail(long, 4000)
	if !strings.Contains(cut, "characters omitted to save space") {
		t.Fatalf("head and tail: %q", cut[:200])
	}
	if len([]rune(cut)) >= len([]rune(long)) {
		t.Fatal("the cut text is shorter")
	}
	if got := groupThousands(1234567); got != "1,234,567" {
		t.Fatalf("thousands separators: %s", got)
	}
}

// TestSensitivityLevels pins the sensitivity judgement.
func TestSensitivityLevels(t *testing.T) {
	service, _ := testService(t)
	assess := func(text string) Assessment { return Assess(service.Engine, userMessage(text)) }
	if got := assess("deploy key sk-or-v1-abcdefghijklmnopqrstuvwxyz0123456789"); got.Level != "high" || got.Reasons[0] != "secret" {
		t.Fatalf("an API key is very sensitive: %+v", got)
	}
	if got := assess("card 4111 1111 1111 1111"); got.Level != "high" || got.Reasons[0] != "card" {
		t.Fatalf("a card number is very sensitive: %+v", got)
	}
	if got := assess("Mail anna.schmidt@example.de now"); got.Level != "low" {
		t.Fatalf("an address is ordinary personal data: %+v", got)
	}
	if got := assess("hello there, how are you"); got.Level != "none" {
		t.Fatalf("nothing private: %+v", got)
	}
	if got := assess("your code is 482913"); got.Level == "high" {
		t.Fatalf("a one-time code is not worth keeping home: %+v", got)
	}
	if got := assess("password: " + loadVaultFixture(t).Key); got.Level != "high" {
		t.Fatalf("a password is very sensitive: %+v", got)
	}
}

// TestPrivacyModelIdleUnload proves the model is freed after idling.
func TestPrivacyModelIdleUnload(t *testing.T) {
	service, _ := testService(t)
	pipeline := &fakePipeline{}
	service.Privacy.Idle = 20 * time.Millisecond
	service.Privacy.SetLoader(func(ctx context.Context, progress func(float64)) (Pipeline, error) {
		return pipeline, nil
	})
	if _, err := service.Privacy.SetEnabled(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	defer service.Privacy.SetEnabled(context.Background(), false)
	items, err := service.Privacy.Detect(context.Background(), "Lunch with Johanna Krause tomorrow")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Text != "Johanna Krause" || items[0].Category != "person" {
		t.Fatalf("entities %+v", items)
	}
	deadline := time.Now().Add(2 * time.Second)
	for !pipeline.disposed && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if !pipeline.disposed {
		t.Fatal("the model was not unloaded after idling")
	}
	if status := service.Privacy.Status(context.Background()); status.Status != "idle" {
		t.Fatalf("status after idling: %s", status.Status)
	}
}
