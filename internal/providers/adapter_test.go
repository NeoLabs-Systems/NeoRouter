package providers

import (
	"context"
	"encoding/json"
	"testing"
)

// TestAccountOptions reads the per-account settings adapters need, in both the
// decoded-JSON and the typed forms the account service builds.
func TestAccountOptions(t *testing.T) {
	var decoded map[string]any
	if err := json.Unmarshal([]byte(`{
		"accountId": "cf-account",
		"authHeader": "x-api-key",
		"manualModels": ["one", "two"],
		"headers": {"X-Title": "NeoRouter"},
		"modelsPath": "/v1/models",
		"count": 3
	}`), &decoded); err != nil {
		t.Fatal(err)
	}
	account := Account{Options: decoded}
	if got := account.OptionString("accountId"); got != "cf-account" {
		t.Fatalf("accountId = %q", got)
	}
	if got := account.OptionString("missing"); got != "" {
		t.Fatalf("missing = %q", got)
	}
	if got := account.OptionString("count"); got != "3" {
		t.Fatalf("count = %q", got)
	}
	if got := account.OptionStrings("manualModels"); len(got) != 2 || got[0] != "one" {
		t.Fatalf("manualModels = %v", got)
	}
	if got := account.OptionStrings("count"); got != nil {
		t.Fatalf("count = %v", got)
	}
	if got := account.OptionHeaders(); got["X-Title"] != "NeoRouter" {
		t.Fatalf("headers = %v", got)
	}
	if account.Option("authHeader") != "x-api-key" {
		t.Fatalf("option = %v", account.Option("authHeader"))
	}
	typed := Account{Options: map[string]any{"manualModels": []string{"three"}, "headers": map[string]string{"X-A": "1"}}}
	if got := typed.OptionStrings("manualModels"); len(got) != 1 || got[0] != "three" {
		t.Fatalf("typed models = %v", got)
	}
	if got := typed.OptionHeaders(); got["X-A"] != "1" {
		t.Fatalf("typed headers = %v", got)
	}
	if got := (Account{}).OptionHeaders(); got != nil {
		t.Fatalf("no headers = %v", got)
	}
}

// TestCapabilitiesSet is the set an adapter reports from Supports.
func TestCapabilitiesSet(t *testing.T) {
	set := Of(CapChat, CapSpeech)
	if !set.Supports(CapChat) || !set.Supports(CapSpeech) {
		t.Fatalf("set = %v", set)
	}
	if set.Supports(CapEmbed) {
		t.Fatal("embed reported")
	}
	if (Capabilities(nil)).Supports(CapChat) {
		t.Fatal("empty set reported chat")
	}
}

// TestCanTranslate: an adapter that cannot translate says so, and one that can
// decide per model says so.
func TestCanTranslate(t *testing.T) {
	plain := stubAdapter{}
	if !CanTranslate(plain, "any") {
		t.Fatal("a translating adapter was refused")
	}
	refusing := stubTranslator{stubAdapter: plain, allowed: "translate-only"}
	if CanTranslate(refusing, "other-model") {
		t.Fatal("a per-model refusal was ignored")
	}
	if !CanTranslate(refusing, "translate-only") {
		t.Fatal("a per-model permission was ignored")
	}
}

// stubTranslator decides translation per model.
type stubTranslator struct {
	stubAdapter
	allowed string
}

func (s stubTranslator) CanTranslate(modelID string) bool { return modelID == s.allowed }

// TestValidationVerdicts keeps the three shapes and their JSON.
func TestValidationVerdicts(t *testing.T) {
	cases := []struct {
		validation Validation
		json       string
	}{
		{ValidKey(), `{"status":"healthy","message":null}`},
		{InvalidKey("Invalid API key"), `{"status":"invalid","message":"Invalid API key"}`},
		{UnknownKey("HTML page"), `{"status":"unknown","message":"HTML page"}`},
	}
	for _, testCase := range cases {
		encoded, err := json.Marshal(testCase.validation)
		if err != nil {
			t.Fatalf("encode: %v", err)
		}
		if string(encoded) != testCase.json {
			t.Fatalf("got %s, want %s", encoded, testCase.json)
		}
	}
}

// TestParsePricing keeps the exported per-token scaling and the free markers.
func TestParsePricing(t *testing.T) {
	in, out, known, free := ParsePricing(map[string]any{"pricing": map[string]any{"prompt": "0.0000025", "completion": "0.00001"}})
	if !known || free || *in != 2.5 || *out != 10 {
		t.Fatalf("per-token = %v %v %v %v", *in, *out, known, free)
	}
	in, out, known, free = ParsePricing(map[string]any{"pricing": map[string]any{"prompt": "0", "completion": "0"}})
	if !known || !free || *in != 0 || *out != 0 {
		t.Fatalf("free = %v %v %v %v", *in, *out, known, free)
	}
	in, out, known, free = ParsePricing(map[string]any{})
	if known || free || in != nil || out != nil {
		t.Fatalf("unknown = %v %v %v %v", in, out, known, free)
	}
}

// TestDefinitionHelpers covers the small readers an adapter or route uses.
func TestDefinitionHelpers(t *testing.T) {
	if optionalString("") != nil {
		t.Fatal("an empty fact became a value")
	}
	if got := optionalString("x"); got == nil || *got != "x" {
		t.Fatalf("optionalString = %v", got)
	}
	media := MediaSpec{Speech: true, Transcribe: "/audio/transcriptions"}
	if path, ok := media.Endpoint(CapTranscribe); !ok || path != "/audio/transcriptions" {
		t.Fatalf("transcribe = %q %v", path, ok)
	}
	if path, ok := media.Endpoint(CapSpeech); !ok || path != "" {
		t.Fatalf("speech = %q %v", path, ok)
	}
	if _, ok := media.Endpoint(CapChat); ok {
		t.Fatal("chat listed as a media endpoint")
	}
	// A media entry of any other JSON type is not an endpoint.
	if _, ok := mediaPath(3.5); ok {
		t.Fatal("a number was read as an endpoint")
	}
	if _, ok := mediaPath(false); ok {
		t.Fatal("false was read as an endpoint")
	}
	// The accepted auth types.
	for _, name := range []string{AuthBearer, AuthHeader, AuthNone} {
		if name == "" {
			t.Fatal("empty auth type")
		}
	}
	if len(Adapters) != 11 || len(Categories) != 9 || len(BillingModes) != 5 || len(LiveDialects) != 3 {
		t.Fatalf("lists = %d %d %d %d", len(Adapters), len(Categories), len(BillingModes), len(LiveDialects))
	}
}

// TestLimitsAndQuirksDefaults keeps the zero values meaning "not published".
func TestLimitsAndQuirksDefaults(t *testing.T) {
	registry, err := Load(nil, nil)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	custom, ok := registry.Get("custom_openai")
	if !ok {
		t.Fatal("custom_openai is missing")
	}
	if !custom.EditableBaseURL || !custom.KeyOptional {
		t.Fatalf("custom endpoint = %+v", custom)
	}
	if custom.Billing.DefaultMode != BillingPaid || !custom.Billing.FreeTierAll {
		t.Fatalf("custom billing = %+v", custom.Billing)
	}
	if len(custom.Live) != 2 {
		t.Fatalf("custom live dialects = %v", custom.Live)
	}
	openrouter, _ := registry.Get("openrouter")
	if openrouter.Limits.Pool != "model" || openrouter.Limits.Account.RPM != 20 || openrouter.Limits.PoolMatch != ":free$" {
		t.Fatalf("openrouter limits = %+v", openrouter.Limits)
	}
	if len(openrouter.Models.ExtraPaths) != 5 {
		t.Fatalf("openrouter extra paths = %v", openrouter.Models.ExtraPaths)
	}
	if path, ok := openrouter.Media.Endpoint(CapGenerateImage); !ok || path != "/images" {
		t.Fatalf("openrouter images = %q %v", path, ok)
	}
	// A definition that cannot be validated without spending quota.
	plan, _ := registry.Get("minimax_plan")
	if plan.Validate == nil || !plan.Validate.Disabled {
		t.Fatalf("minimax plan validate = %+v", plan.Validate)
	}
	// A chat-probe definition names the cheap models to try.
	opencode, _ := registry.Get("opencode_go")
	if opencode.Validate == nil || !opencode.Validate.ChatProbe || opencode.SessionHeader != "x-opencode-session" {
		t.Fatalf("opencode_go = %+v", opencode)
	}
	// A plan whose base URL must be overridable for tests.
	chatgpt, _ := registry.Get("chatgpt")
	if chatgpt.Connect != "codex_device" || chatgpt.Billing.Plan == nil {
		t.Fatalf("chatgpt = %+v", chatgpt)
	}
	// Voice providers the media adapters serve: Deepgram speaks bearer-token
	// auth in its adapter (a Token prefix), ElevenLabs names its own header.
	deepgram, _ := registry.Get("deepgram")
	if deepgram.Auth.Type != AuthBearer {
		t.Fatalf("deepgram auth = %+v", deepgram.Auth)
	}
	if _, ok := deepgram.Media.Endpoint(CapTranscribe); !ok {
		t.Fatalf("deepgram media = %+v", deepgram.Media)
	}
	elevenlabs, _ := registry.Get("elevenlabs")
	if elevenlabs.Auth.Type != AuthHeader || elevenlabs.Auth.Header != "xi-api-key" {
		t.Fatalf("elevenlabs auth = %+v", elevenlabs.Auth)
	}
}

// TestAdapterCallDefaultsDocuments what a zero Call means.
func TestAdapterCallDefaults(t *testing.T) {
	call := Call{}
	if call.Timeout != 0 || call.SessionID != "" || call.ModelID != "" || call.OnHeaders != nil {
		t.Fatalf("zero call = %+v", call)
	}
	ctx := context.Background()
	if ctx == nil {
		t.Fatal("unreachable")
	}
}
