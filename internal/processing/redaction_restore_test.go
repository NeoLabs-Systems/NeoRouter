package processing

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestPlaceholderIsFoundHoweverAModelWritesIt is the ported first case of
// test/unit/redaction_restore.test.js.
func TestPlaceholderIsFoundHoweverAModelWritesIt(t *testing.T) {
	fixture := loadVaultFixture(t)
	engine := testEngine(t)
	vault := vaultWith(t, engine, restoreModes(), fixture.Key)
	for _, written := range []string{
		"[SECRET_1]", "[Secret_1]", "[ SECRET 1 ]", "[SECRET-1]", `\[SECRET_1\]`, `[SECRET\_1]`,
		"<SECRET_1>", "{SECRET_1}", "【SECRET_1】", "SECRET_1", "`[SECRET_1]`", "**[SECRET_1]**",
	} {
		restored := vault.RestoreText("use "+written+".", false)
		if !strings.Contains(restored, fixture.Key) {
			t.Errorf("%s is restored: %q", written, restored)
		}
		if strings.Contains(strings.ToUpper(restored), "SECRET") {
			t.Errorf("%s leaves nothing behind: %q", written, restored)
		}
	}
}

// TestOnlyTheVaultsOwnTokensAreRestored is the ported look-alike case.
func TestOnlyTheVaultsOwnTokensAreRestored(t *testing.T) {
	fixture := loadVaultFixture(t)
	engine := testEngine(t)
	vault := vaultWith(t, engine, restoreModes(), fixture.Key)
	text := "SECRET_2 and MY_SECRET_1 and SECRET_10 and SECRET_1X stay"
	if restored := vault.RestoreText(text, false); restored != text {
		t.Fatalf("look-alikes changed: %q", restored)
	}
}

// TestValuesHiddenByChoiceStayHidden is the ported "hidden for good" case.
func TestValuesHiddenByChoiceStayHidden(t *testing.T) {
	fixture := loadVaultFixture(t)
	engine := testEngine(t)
	redaction := RedactRequest(engine, userMessage(fixture.Key), RequestRedactOptions{Modes: hideSecretModes()})
	if redaction == nil {
		t.Fatal("the key should be hidden")
	}
	if restored := redaction.Vault.RestoreText("[SECRET_1]", false); restored != "[SECRET_1]" {
		t.Fatalf("a hidden value came back: %q", restored)
	}
	if redaction.Vault.Size() != 1 {
		t.Fatalf("vault holds %d values", redaction.Vault.Size())
	}
}

// TestMultiLineSecretKeepsToolArgumentsValid is the ported private-key case.
func TestMultiLineSecretKeepsToolArgumentsValid(t *testing.T) {
	fixture := loadVaultFixture(t)
	engine := testEngine(t)
	vault := vaultWith(t, engine, restoreModes(), fixture.Key, fixture.PEM)
	token := ""
	for _, item := range vault.Entries() {
		if strings.Contains(item.Entry.Value, "PRIVATE") {
			token = item.Token
		}
	}
	if token == "" {
		t.Fatalf("no placeholder for the private key in %d entries", vault.Size())
	}
	arguments, err := marshalJSON(map[string]any{
		"command": `echo "[` + token + `]" > key.pem`,
		"key":     "[SECRET_1]",
	})
	if err != nil {
		t.Fatal(err)
	}
	answer := map[string]any{"choices": []any{map[string]any{"message": map[string]any{
		"role":    "assistant",
		"content": "see [" + token + "]",
		"tool_calls": []any{map[string]any{"function": map[string]any{
			"arguments": arguments,
		}}},
	}}}}
	restored := RestoreResponse(answer, vault)
	message := restored["choices"].([]any)[0].(map[string]any)["message"].(map[string]any)
	call := message["tool_calls"].([]any)[0].(map[string]any)["function"].(map[string]any)
	var decoded map[string]any
	if err := json.Unmarshal([]byte(call["arguments"].(string)), &decoded); err != nil {
		t.Fatalf("restored arguments are not JSON: %v (%q)", err, call["arguments"])
	}
	if decoded["command"] != `echo "`+fixture.PEM+`" > key.pem` {
		t.Errorf("the private key arrives as written: %q", decoded["command"])
	}
	if decoded["key"] != fixture.Key {
		t.Errorf("the key arrives as written: %q", decoded["key"])
	}
	if content, _ := message["content"].(string); !strings.Contains(content, fixture.PEM) {
		t.Errorf("content is restored: %q", content)
	}
}

// TestReasoningIsRestored is the ported reasoning case.
func TestReasoningIsRestored(t *testing.T) {
	fixture := loadVaultFixture(t)
	engine := testEngine(t)
	vault := vaultWith(t, engine, restoreModes(), fixture.Key)
	answer := map[string]any{"choices": []any{map[string]any{"message": map[string]any{
		"content":           "a",
		"reasoning":         "think [SECRET_1]",
		"reasoning_content": "rc [SECRET_1]",
	}}}}
	message := RestoreResponse(answer, vault)["choices"].([]any)[0].(map[string]any)["message"].(map[string]any)
	if message["reasoning"] != "think "+fixture.Key || message["reasoning_content"] != "rc "+fixture.Key {
		t.Fatalf("reasoning: %q / %q", message["reasoning"], message["reasoning_content"])
	}
}

// TestStreamedPlaceholdersAreRestored is the ported chunk-by-chunk case.
func TestStreamedPlaceholdersAreRestored(t *testing.T) {
	fixture := loadVaultFixture(t)
	engine := testEngine(t)
	vault := vaultWith(t, engine, restoreModes(), fixture.Key, fixture.PEM)
	token := ""
	for _, item := range vault.Entries() {
		if strings.Contains(item.Entry.Value, "PRIVATE") {
			token = item.Token
		}
	}
	chunk := func(delta map[string]any, finish any) map[string]any {
		return map[string]any{"id": "x", "choices": []any{map[string]any{"index": float64(0), "delta": delta, "finish_reason": finish}}}
	}
	textRestorer := NewStreamRestorer(vault)
	out := ""
	pieces := append(strings.Split("Use [SECRET_1] and \\[SECRET\\_1\\] and SECRET_1 now", ""), "")
	for index, piece := range pieces {
		var finish any
		if index == len(pieces)-1 {
			finish = "stop"
		}
		got := textRestorer.Process(chunk(map[string]any{"content": piece}, finish))
		delta := got["choices"].([]any)[0].(map[string]any)["delta"].(map[string]any)
		content, _ := delta["content"].(string)
		out += content
	}
	want := "Use " + fixture.Key + " and " + fixture.Key + " and " + fixture.Key + " now"
	if out != want {
		t.Fatalf("streamed text %q, want %q", out, want)
	}

	argumentsRestorer := NewStreamRestorer(vault)
	arguments, err := marshalJSON(map[string]any{"c": "[" + token + "]"})
	if err != nil {
		t.Fatal(err)
	}
	// The JS test reads `(delta.tool_calls || [])`; a chunk without tool calls
	// contributes nothing.
	argumentsOf := func(chunk map[string]any) string {
		out := ""
		delta := chunk["choices"].([]any)[0].(map[string]any)["delta"].(map[string]any)
		calls, _ := delta["tool_calls"].([]any)
		for _, raw := range calls {
			call := raw.(map[string]any)
			value, _ := call["function"].(map[string]any)["arguments"].(string)
			out += value
		}
		return out
	}
	joined := ""
	for _, piece := range strings.Split(arguments, "") {
		joined += argumentsOf(argumentsRestorer.Process(chunk(map[string]any{"tool_calls": []any{map[string]any{
			"index":    float64(0),
			"function": map[string]any{"arguments": piece},
		}}}, nil)))
	}
	joined += argumentsOf(argumentsRestorer.Process(chunk(map[string]any{}, "tool_calls")))
	var decoded map[string]any
	if err := json.Unmarshal([]byte(joined), &decoded); err != nil {
		t.Fatalf("streamed arguments are not JSON: %v (%q)", err, joined)
	}
	if decoded["c"] != fixture.PEM {
		t.Fatalf("streamed arguments restored %q", decoded["c"])
	}
}

// TestSecretInToolCallArgumentsIsHiddenAsWritten is the ported JSON-arguments
// case.
func TestSecretInToolCallArgumentsIsHiddenAsWritten(t *testing.T) {
	fixture := loadVaultFixture(t)
	engine := testEngine(t)
	arguments, err := marshalJSON(map[string]any{"text": fixture.PEM})
	if err != nil {
		t.Fatal(err)
	}
	canonical := Canonical{"messages": []any{
		map[string]any{"role": "user", "content": "go"},
		map[string]any{"role": "assistant", "content": nil, "tool_calls": []any{map[string]any{
			"id": "c1", "type": "function", "function": map[string]any{"name": "write", "arguments": arguments},
		}}},
	}}
	redaction := RedactRequest(engine, canonical, RequestRedactOptions{Modes: restoreModes()})
	if redaction == nil {
		t.Fatal("the key inside the JSON string should be found")
	}
	sent := ""
	for _, message := range redaction.Canonical.Messages() {
		if messageRole(message) != "assistant" {
			continue
		}
		call := message["tool_calls"].([]any)[0].(map[string]any)["function"].(map[string]any)
		sent, _ = call["arguments"].(string)
	}
	if strings.Contains(sent, "PRIVATE KEY") {
		t.Fatalf("the key does not leave: %q", sent)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(sent), &decoded); err != nil {
		t.Fatalf("redacted arguments are not JSON: %v", err)
	}
	if !strings.HasPrefix(decoded["text"].(string), "[SECRET_") {
		t.Fatalf("arguments hold a placeholder: %q", decoded["text"])
	}
}

// TestSameValueSamePlaceholder is the ported vault case.
func TestSameValueSamePlaceholder(t *testing.T) {
	fixture := loadVaultFixture(t)
	engine := testEngine(t)
	vault := NewVault(engine, restoreModes())
	if vault.TokenFor("secret", fixture.Key, "ai_provider_key") != vault.TokenFor("secret", fixture.Key, "ai_provider_key") {
		t.Fatal("the same value got two placeholders")
	}
	text := vault.RedactText("a@b.de and again a@b.de, card 4111111111111111", RedactOptions{})
	if text != "[EMAIL_1] and again [EMAIL_1], card [CARD_1]" {
		t.Fatalf("redacted text %q", text)
	}
	if restored := vault.RestoreText("Write to [EMAIL_1] or [ email 1 ]. Card [CARD_1].", false); restored != "Write to a@b.de or a@b.de. Card [CARD_1]." {
		t.Fatalf("restored text %q", restored)
	}
}

// vaultWith hides the given values in one message and checks they all landed.
func vaultWith(t *testing.T, engine *Engine, modes map[string]string, secrets ...string) *Vault {
	t.Helper()
	redaction := RedactRequest(engine, userMessage(strings.Join(secrets, "\n")), RequestRedactOptions{Modes: modes})
	if redaction == nil {
		t.Fatalf("nothing was hidden in %q", secrets)
	}
	if redaction.Vault.Size() != len(secrets) {
		t.Fatalf("vault holds %d values, want %d", redaction.Vault.Size(), len(secrets))
	}
	return redaction.Vault
}
