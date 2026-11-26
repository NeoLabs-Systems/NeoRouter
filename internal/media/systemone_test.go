package media

import (
	"encoding/json"
	"testing"

	"github.com/NeoLabs-Systems/neorouter/internal/providers"
)

// Ported from test/unit/system_one.test.js: the outputs below were captured from
// the real models (Groq, NVIDIA, 2026-10-28).

func TestSystemOneFamilies(t *testing.T) {
	cases := []struct{ model, want string }{
		{"meta-llama/llama-prompt-guard-2-86m", "prompt_guard"},
		{"meta/llama-guard-4-12b", "llama_guard"},
		{"nvidia/nemotron-3.5-content-safety", "nemoguard"},
		{"openai/gpt-oss-safeguard-20b", "safeguard"},
		{"google/shieldgemma-2-9b", "shieldgemma"},
		{"acme/topic-control-v1", "topic_control"},
		{"openai/omni-moderation-latest", "native"},
		{"mistral/mistral-large-2411", "generic"},
	}
	for _, testCase := range cases {
		if got := systemOneFamily(testCase.model); got != testCase.want {
			t.Errorf("family(%q) = %q, want %q", testCase.model, got, testCase.want)
		}
	}
}

func TestPromptGuardScoresBecomeAnInjectionVerdict(t *testing.T) {
	attack, err := parseSystemOne("prompt_guard", "0.999582827091217")
	if err != nil {
		t.Fatalf("parse attack: %v", err)
	}
	if !attack.Flagged || !attack.Categories.get("prompt_injection") {
		t.Fatalf("attack = %+v, want flagged prompt injection", attack)
	}
	if attack.CategoryScores.get("prompt_injection") != 0.999582827091217 {
		t.Fatalf("score = %v", attack.CategoryScores.get("prompt_injection"))
	}
	benign, err := parseSystemOne("prompt_guard", "0.00035239275894127786")
	if err != nil {
		t.Fatalf("parse benign: %v", err)
	}
	if benign.Flagged {
		t.Fatalf("benign = %+v, want unflagged", benign)
	}
	if _, err := parseSystemOne("prompt_guard", "not a number"); err == nil {
		t.Fatal("a non-numeric score is an unusable answer")
	} else if providers.KindOf(err) != providers.KindInvalidOutput {
		t.Fatalf("kind = %q, want invalid_output", providers.KindOf(err))
	}
}

func TestLlamaGuardHazardCodesMapToOpenAICategories(t *testing.T) {
	unsafe, err := parseSystemOne(systemOneFamily("meta/llama-guard-4-12b"), "unsafe\nS9")
	if err != nil {
		t.Fatalf("parse unsafe: %v", err)
	}
	if !unsafe.Flagged || !unsafe.Categories.get("illicit/violent") {
		t.Fatalf("unsafe = %+v, want illicit/violent", unsafe)
	}
	safe, err := parseSystemOne("llama_guard", "safe")
	if err != nil {
		t.Fatalf("parse safe: %v", err)
	}
	if safe.Flagged {
		t.Fatalf("safe = %+v, want unflagged", safe)
	}
	// ShieldGemma answers Yes/No, and a code it does not name is illicit.
	shield, err := parseSystemOne("shieldgemma", "Yes")
	if err != nil {
		t.Fatalf("parse shieldgemma: %v", err)
	}
	if !shield.Flagged || !shield.Categories.get("illicit") {
		t.Fatalf("shieldgemma = %+v, want illicit", shield)
	}
	if _, err := parseSystemOne("llama_guard", "maybe"); err == nil {
		t.Fatal("an unexpected verdict is an unusable answer")
	}
}

func TestNemoGuardAnswersInTextOrJSON(t *testing.T) {
	if got := systemOneFamily("nvidia/nemotron-3.5-content-safety"); got != "nemoguard" {
		t.Fatalf("family = %q", got)
	}
	text, err := parseSystemOne("nemoguard", "User Safety: unsafe")
	if err != nil {
		t.Fatalf("parse text: %v", err)
	}
	if !text.Flagged {
		t.Fatalf("text = %+v, want flagged", text)
	}
	structured, err := parseSystemOne("nemoguard", `{"User Safety": "unsafe", "Safety Categories": "Violence, Hate/Identity Hate"}`)
	if err != nil {
		t.Fatalf("parse json: %v", err)
	}
	if !structured.Categories.get("violence") || !structured.Categories.get("hate") {
		t.Fatalf("json = %+v, want violence and hate", structured)
	}
	safe, err := parseSystemOne("nemoguard", `{"User Safety": "safe"}`)
	if err != nil {
		t.Fatalf("parse safe: %v", err)
	}
	if safe.Flagged {
		t.Fatalf("safe = %+v, want unflagged", safe)
	}
	if _, err := parseSystemOne("nemoguard", "no idea"); err == nil {
		t.Fatal("an answer with no verdict is unusable")
	}
}

func TestSafeguardJSONKeepsTheRationale(t *testing.T) {
	verdict, err := parseSystemOne(systemOneFamily("openai/gpt-oss-safeguard-20b"),
		`{"violation":1,"category":"Illicit behavior","rationale":"Providing instructions for making a pipe bomb is disallowed content."}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !verdict.Flagged || !verdict.Categories.get("illicit") {
		t.Fatalf("verdict = %+v, want illicit", verdict)
	}
	if verdict.Rationale == nil || *verdict.Rationale == "" {
		t.Fatal("the rationale is kept")
	}
	clean, err := parseSystemOne("safeguard", `{"violation":0,"category":"none","rationale":"ok"}`)
	if err != nil {
		t.Fatalf("parse clean: %v", err)
	}
	if clean.Flagged {
		t.Fatalf("clean = %+v, want unflagged", clean)
	}
	if _, err := parseSystemOne("safeguard", "I cannot help with that."); err == nil {
		t.Fatal("an answer without the JSON contract is unusable")
	}
	// A fenced object is read like a bare one.
	fenced, err := parseSystemOne("safeguard", "```json\n{\"violation\":1,\"category\":\"Hate\"}\n```")
	if err != nil {
		t.Fatalf("parse fenced: %v", err)
	}
	if !fenced.Categories.get("hate") {
		t.Fatalf("fenced = %+v, want hate", fenced)
	}
}

// TestModerationResultJSON locks the wire shape: OpenAI's category list in
// OpenAI's order, whatever order the model named them in.
func TestModerationResultJSON(t *testing.T) {
	result, err := parseSystemOne("llama_guard", "unsafe\nS9")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"flagged":true,"categories":{"harassment":false,"harassment/threatening":false,"hate":false,` +
		`"hate/threatening":false,"illicit":false,"illicit/violent":true,"self-harm":false,"self-harm/intent":false,` +
		`"self-harm/instructions":false,"sexual":false,"sexual/minors":false,"violence":false,"violence/graphic":false,` +
		`"prompt_injection":false,"privacy":false,"intellectual_property":false,"elections":false},` +
		`"category_scores":{"harassment":0,"harassment/threatening":0,"hate":0,"hate/threatening":0,"illicit":0,` +
		`"illicit/violent":1,"self-harm":0,"self-harm/intent":0,"self-harm/instructions":0,"sexual":0,"sexual/minors":0,` +
		`"violence":0,"violence/graphic":0,"prompt_injection":0,"privacy":0,"intellectual_property":0,"elections":0}}`
	if string(encoded) != want {
		t.Fatalf("moderation JSON:\n got %s\nwant %s", encoded, want)
	}
}

func TestFromNativeKeepsTheProvidersRecords(t *testing.T) {
	records := []any{map[string]any{
		"flagged":         true,
		"categories":      map[string]any{"violence": true, "harassment": false},
		"category_scores": map[string]any{"violence": 0.91, "harassment": 0.02},
	}}
	results := fromNative(records)
	if len(results) != 1 {
		t.Fatalf("results = %d", len(results))
	}
	if !results[0].Flagged || !results[0].Categories.get("violence") {
		t.Fatalf("result = %+v", results[0])
	}
	if results[0].CategoryScores.get("violence") != 0.91 {
		t.Fatalf("score = %v", results[0].CategoryScores.get("violence"))
	}
	if results[0].Categories.get("harassment") {
		t.Fatal("a false category stays false")
	}
}
