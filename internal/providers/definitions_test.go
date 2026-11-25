package providers

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

// TestDefinitionsMatchJS proves the embedded definition data is the data the JS
// registry produced: every definition, in order, and every key of every
// definition survives a trip through the Go types. A field the Go types do not
// model would be dropped by the round trip and fail here.
func TestDefinitionsMatchJS(t *testing.T) {
	want := loadDefinitionFixture(t)
	registry, err := Load(nil, nil)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	got := registry.All()
	if len(got) != len(want) {
		t.Fatalf("definitions = %d, want %d", len(got), len(want))
	}
	for i, definition := range got {
		expected := want[i]
		if definition.ID != expected["id"] {
			t.Fatalf("definition %d = %q, want %q", i, definition.ID, expected["id"])
		}
		if diff := compareJSON(t, definition, expected); diff != "" {
			t.Fatalf("%s: %s", definition.ID, diff)
		}
	}
}

// compareJSON reports the first value difference between a Go definition and
// the fixture's decoded JSON.
func compareJSON(t *testing.T, value any, expected any) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	var actual any
	if err := json.Unmarshal(encoded, &actual); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return diffValues("", actual, expected)
}

// diffValues walks two decoded JSON values and returns the first difference.
func diffValues(path string, actual, expected any) string {
	if reflect.DeepEqual(actual, expected) {
		return ""
	}
	actualObject, actualIsObject := actual.(map[string]any)
	expectedObject, expectedIsObject := expected.(map[string]any)
	if actualIsObject && expectedIsObject {
		for key, expectedValue := range expectedObject {
			actualValue, ok := actualObject[key]
			if !ok {
				return join(path, key) + ": missing from Go"
			}
			if diff := diffValues(join(path, key), actualValue, expectedValue); diff != "" {
				return diff
			}
		}
		for key := range actualObject {
			if _, ok := expectedObject[key]; !ok {
				return join(path, key) + ": not in the JS data"
			}
		}
	}
	return strings.Join([]string{path, "=", display(actual), "want", display(expected)}, " ")
}

func join(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}

func display(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "?"
	}
	return string(encoded)
}

// TestDefinitionDataInvariants checks the facts the rest of the server relies
// on: a known adapter, a known category, a resolved quota parser and no
// duplicate id.
func TestDefinitionDataInvariants(t *testing.T) {
	registry, err := Load(nil, nil)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if registry.Len() < 100 {
		t.Fatalf("definitions = %d, want the whole catalog", registry.Len())
	}
	seen := map[string]bool{}
	for _, definition := range registry.All() {
		if seen[definition.ID] {
			t.Fatalf("duplicate id %q", definition.ID)
		}
		seen[definition.ID] = true
		if !definition.Adapter.valid() {
			t.Fatalf("%s: adapter %q is not implemented", definition.ID, definition.Adapter)
		}
		if !definition.Category.valid() {
			t.Fatalf("%s: category %q is not known", definition.ID, definition.Category)
		}
		if definition.Quota.Probe != nil && definition.Quota.Probe.Parse == nil {
			t.Fatalf("%s: quota probe parser %q did not resolve", definition.ID, definition.Quota.Probe.ParseName)
		}
		if definition.Auth.Type == "" {
			t.Fatalf("%s: auth type is missing", definition.ID)
		}
		if _, ok := registry.Get(definition.ID); !ok {
			t.Fatalf("%s: Get did not find the definition", definition.ID)
		}
	}
	if _, ok := registry.Get("no-such-provider"); ok {
		t.Fatal("Get found a definition that does not exist")
	}
}

// TestBaseURLOverrides follow the definition modules' process.env reads.
func TestBaseURLOverrides(t *testing.T) {
	env := map[string]string{
		"NEOROUTER_NOUS_BASE_URL":  "https://nous.internal/v1",
		"NEOROUTER_CODEX_BASE_URL": "https://codex.internal/backend",
	}
	getenv := func(name string) string { return env[name] }
	registry, err := Load(nil, getenv)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	nous, _ := registry.Get("nous")
	if nous.BaseURL != "https://nous.internal/v1" {
		t.Fatalf("nous base URL = %q", nous.BaseURL)
	}
	chatgpt, _ := registry.Get("chatgpt")
	if chatgpt.BaseURL != "https://codex.internal/backend" {
		t.Fatalf("chatgpt base URL = %q", chatgpt.BaseURL)
	}
	// Every other definition keeps its own base URL.
	openrouter, _ := registry.Get("openrouter")
	if openrouter.BaseURL != "https://openrouter.ai/api/v1" {
		t.Fatalf("openrouter base URL = %q", openrouter.BaseURL)
	}
	plain, err := Load(nil, nil)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	plainNous, _ := plain.Get("nous")
	if plainNous.BaseURL != "https://inference-api.nousresearch.com/v1" {
		t.Fatalf("nous default base URL = %q", plainNous.BaseURL)
	}
}

// TestDefinitionValidation ports the rules of registry.js's definitionSchema
// and keeps their error text shape.
func TestDefinitionValidation(t *testing.T) {
	valid := `{
		"id": "example", "label": "Example", "description": "", "category": "paid",
		"adapter": "openai_compatible", "capabilities": ["chat"], "media": {},
		"live": [], "baseUrl": "https://example.test/v1", "editableBaseUrl": false,
		"baseUrlSchemes": ["http", "https"], "auth": {"type": "bearer"},
		"keyOptional": false, "fields": [], "headers": {},
		"models": {"extraPaths": [], "optIn": []}, "extraModels": [],
		"billing": {"defaultMode": "paid", "freeTierAll": false, "freeTierModels": [],
			"localOnly": false, "keylessOnly": false, "alwaysFree": [], "trustListingFree": true, "note": ""},
		"limits": {"pool": "model", "account": {}, "defaults": {}, "models": []},
		"quota": {"headerPeriods": {"requests": "minute", "tokens": "minute"}, "plainHeaderMetric": "rpm"},
		"quirks": {}
	}`
	if _, err := Parse([]byte("["+valid+"]"), nil, nil); err != nil {
		t.Fatalf("valid definition refused: %v", err)
	}

	cases := []struct {
		name   string
		edit   func(def map[string]any)
		expect string
	}{
		{"bad id", func(def map[string]any) { def["id"] = "Not An Id" }, "id: Invalid"},
		{"empty label", func(def map[string]any) { def["label"] = "" }, "label: String must contain at least 1 character(s)"},
		{"unknown category", func(def map[string]any) { def["category"] = "other" }, "category: Invalid enum value"},
		{"unknown adapter", func(def map[string]any) { def["adapter"] = "magic" }, "adapter: Invalid enum value"},
		{"unknown website", func(def map[string]any) { def["website"] = "not a url" }, "website: Invalid url"},
		{"unknown capability", func(def map[string]any) { def["capabilities"] = []any{"video"} }, "capabilities: Invalid enum value"},
		{"unknown scheme", func(def map[string]any) { def["baseUrlSchemes"] = []any{"ftp"} }, "baseUrlSchemes: Invalid enum value"},
		{"unknown auth", func(def map[string]any) { def["auth"] = map[string]any{"type": "jwt"} }, "auth.type: Invalid enum value"},
		{"unknown billing mode", func(def map[string]any) {
			def["billing"] = map[string]any{"defaultMode": "gratis"}
		}, "billing.defaultMode: Invalid enum value"},
		{"unknown pool", func(def map[string]any) {
			def["limits"] = map[string]any{"pool": "bucket"}
		}, "limits.pool: Invalid enum value"},
		{"unknown quota period", func(def map[string]any) {
			def["quota"] = map[string]any{"headerPeriods": map[string]any{"requests": "hour", "tokens": "minute"}, "plainHeaderMetric": "rpm"}
		}, "quota.headerPeriods.requests: Invalid enum value"},
		{"unknown parser", func(def map[string]any) {
			def["quota"] = map[string]any{
				"headerPeriods":     map[string]any{"requests": "minute", "tokens": "minute"},
				"plainHeaderMetric": "rpm",
				"probe":             map[string]any{"path": "/usage", "parse": "no_such_parser"},
			}
		}, `quota.probe.parse: Unknown parser "no_such_parser"`},
		{"unknown image api", func(def map[string]any) {
			def["quirks"] = map[string]any{"imageApi": "dall"}
		}, "quirks.imageApi: Invalid enum value"},
		{"flat rate without plans", func(def map[string]any) {
			def["billing"] = map[string]any{"defaultMode": "flat_rate", "flatRate": map[string]any{"defaultPlan": "pro"}}
		}, "billing.flatRate.plans: Array must contain at least 1 element(s)"},
		{"flat rate plan without a size", func(def map[string]any) {
			def["billing"] = map[string]any{"defaultMode": "flat_rate", "flatRate": map[string]any{
				"defaultPlan": "pro", "plans": []any{map[string]any{"id": "pro", "label": "Pro"}},
			}}
		}, "billing.flatRate.plans.concurrent: Number must be greater than 0"},
		{"verify without its evidence", func(def map[string]any) {
			def["billing"] = map[string]any{"defaultMode": "free_tier", "verify": map[string]any{"type": "paid_model_probe", "paidOnlyModels": []any{}}}
		}, "billing.verify.paidOnlyModels: Array must contain at least 1 element(s)"},
		{"unknown verify kind", func(def map[string]any) {
			def["billing"] = map[string]any{"defaultMode": "free_tier", "verify": map[string]any{"type": "vibes"}}
		}, "billing.verify.type: Invalid discriminator value"},
		{"model voices without a default", func(def map[string]any) {
			def["quirks"] = map[string]any{"modelVoices": []any{map[string]any{"match": "m", "voices": []any{"v"}}}}
		}, "quirks.modelVoices: Required"},
		{"negative timeout", func(def map[string]any) {
			def["quirks"] = map[string]any{"timeoutMs": -5}
		}, "quirks.timeoutMs: Number must be greater than 0"},
		{"unknown speech api", func(def map[string]any) {
			def["quirks"] = map[string]any{"speechApi": "soap"}
		}, "quirks.speechApi: Invalid enum value"},
		{"validate path off the base URL", func(def map[string]any) {
			def["validate"] = map[string]any{"path": "models"}
		}, "validate.path: Invalid path"},
		{"field without a key", func(def map[string]any) {
			def["fields"] = []any{map[string]any{"label": "Account id"}}
		}, "fields.key: Required"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			var def map[string]any
			if err := json.Unmarshal([]byte(valid), &def); err != nil {
				t.Fatal(err)
			}
			testCase.edit(def)
			encoded, err := json.Marshal([]any{def})
			if err != nil {
				t.Fatal(err)
			}
			_, err = Parse(encoded, nil, nil)
			if err == nil {
				t.Fatal("invalid definition accepted")
			}
			if !strings.Contains(err.Error(), testCase.expect) {
				t.Fatalf("error = %q, want it to mention %q", err, testCase.expect)
			}
			if !strings.HasPrefix(err.Error(), "Invalid provider definition in definitions.json (") {
				t.Fatalf("error = %q, want the JS text shape", err)
			}
		})
	}

	// A duplicate id is refused with the JS text.
	var first, second map[string]any
	_ = json.Unmarshal([]byte(valid), &first)
	_ = json.Unmarshal([]byte(valid), &second)
	encoded, _ := json.Marshal([]any{first, second})
	if _, err := Parse(encoded, nil, nil); err == nil || err.Error() != "Duplicate provider definition id example in definitions.json." {
		t.Fatalf("duplicate = %v", err)
	}
}

// TestPublicDefinitionParity compares the connect-screen view with what the JS
// registry returned for the same provides.
func TestPublicDefinitionParity(t *testing.T) {
	fixtures := loadFixture(t)
	registry, err := Load(nil, nil)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	waits := Waits{
		SlotWait:    time.Duration(fixtures.Waits.SlotWaitMS) * time.Millisecond,
		SlotMaxWait: time.Duration(fixtures.Waits.SlotMaxWaitMS) * time.Millisecond,
	}
	if len(fixtures.PublicDefinition) != registry.Len() {
		t.Fatalf("fixtures = %d, definitions = %d", len(fixtures.PublicDefinition), registry.Len())
	}
	for _, testCase := range fixtures.PublicDefinition {
		definition, ok := registry.Get(testCase.ID)
		if !ok {
			t.Fatalf("%s: no definition", testCase.ID)
		}
		got := genericOf(t, registry.PublicDefinition(definition, waits))
		want := generic(t, testCase.Expect)
		if diff := diffValues("", got, want); diff != "" {
			t.Fatalf("%s: %s", testCase.ID, diff)
		}
	}
}

// TestDiffValuesDetectsDifferences guards the round-trip test above: a missing
// or renamed key must fail it, or the data comparison proves nothing.
func TestDiffValuesDetectsDifferences(t *testing.T) {
	same := map[string]any{"id": "x", "n": 1.0, "nested": map[string]any{"a": true}}
	if diff := diffValues("", same, map[string]any{"id": "x", "n": 1.0, "nested": map[string]any{"a": true}}); diff != "" {
		t.Fatalf("equal values differed: %s", diff)
	}
	if diff := diffValues("", map[string]any{"id": "x"}, map[string]any{"id": "x", "label": "X"}); diff == "" {
		t.Fatal("a missing key was not reported")
	}
	if diff := diffValues("", map[string]any{"id": "x", "extra": 1.0}, map[string]any{"id": "x"}); diff == "" {
		t.Fatal("a key the JS data does not have was not reported")
	}
	if diff := diffValues("", map[string]any{"id": "x"}, map[string]any{"id": "y"}); diff == "" {
		t.Fatal("a changed value was not reported")
	}
	if diff := diffValues("", map[string]any{"nested": map[string]any{"a": true}}, map[string]any{"nested": map[string]any{"a": false}}); diff == "" {
		t.Fatal("a changed nested value was not reported")
	}
}

// TestCreateAdapterUsesTheFactory: the registry resolves adapters through the
// injected factory and never builds one itself.
func TestCreateAdapterUsesTheFactory(t *testing.T) {
	registry, err := Load(nil, nil)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	definition, _ := registry.Get("openrouter")
	if _, err := registry.CreateAdapter(definition, AdapterDeps{}); err != ErrNoFactory {
		t.Fatalf("no factory = %v, want %v", err, ErrNoFactory)
	}

	var built Definition
	var account Account
	factory := func(def Definition, deps AdapterDeps) (Adapter, error) {
		built, account = def, deps.Account
		return stubAdapter{definition: def}, nil
	}
	withFactory, err := Load(factory, nil)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	adapter, err := withFactory.CreateAdapter(definition, AdapterDeps{Account: Account{ID: "account-1", UserID: "user-1"}})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if built.ID != "openrouter" || account.ID != "account-1" || account.UserID != "user-1" {
		t.Fatalf("factory saw %q %+v", built.ID, account)
	}
	if adapter.Definition().ID != "openrouter" {
		t.Fatalf("adapter definition = %q", adapter.Definition().ID)
	}
	// A definition that names an adapter nobody implements is refused.
	broken := definition
	broken.Adapter = "telepathy"
	if _, err := withFactory.CreateAdapter(broken, AdapterDeps{}); err == nil || !strings.Contains(err.Error(), "unknown adapter") {
		t.Fatalf("broken adapter = %v", err)
	}
}

// TestAdapterDepsTimeout picks the call's timeout, then the definition's, then
// the configured default.
func TestAdapterDepsTimeout(t *testing.T) {
	deps := AdapterDeps{UpstreamTimeout: 120 * time.Second}
	definition := Definition{Quirks: Quirks{TimeoutMS: 600000}}
	if got := deps.TimeoutFor(definition, Call{}); got != 600*time.Second {
		t.Fatalf("definition timeout = %v", got)
	}
	if got := deps.TimeoutFor(definition, Call{Timeout: 5 * time.Second}); got != 5*time.Second {
		t.Fatalf("call timeout = %v", got)
	}
	if got := deps.TimeoutFor(Definition{}, Call{}); got != 120*time.Second {
		t.Fatalf("default timeout = %v", got)
	}
}

// TestMediaEndpoint reads the media entries.
func TestMediaEndpoint(t *testing.T) {
	media := MediaSpec{Embed: "/embeddings", GenerateImage: true, Search: true}
	if path, ok := media.Endpoint(CapEmbed); !ok || path != "/embeddings" {
		t.Fatalf("embed = %q %v", path, ok)
	}
	if path, ok := media.Endpoint(CapGenerateImage); !ok || path != "" {
		t.Fatalf("generateImage = %q %v", path, ok)
	}
	if _, ok := media.Endpoint(CapSearch); !ok {
		t.Fatal("search not offered")
	}
	if _, ok := media.Endpoint(CapSpeech); ok {
		t.Fatal("speech offered")
	}
}

// TestDefinitionJSONUnionForms keeps `validate: false` and the two verify forms
// readable and writable.
func TestDefinitionJSONUnionForms(t *testing.T) {
	var disabled ValidateSpec
	if err := json.Unmarshal([]byte("false"), &disabled); err != nil {
		t.Fatalf("false: %v", err)
	}
	if !disabled.Disabled {
		t.Fatal("false form not read")
	}
	encoded, err := json.Marshal(ValidateSpec{Disabled: true})
	if err != nil || string(encoded) != "false" {
		t.Fatalf("false form written as %s (%v)", encoded, err)
	}
	var chatProbe ValidateSpec
	if err := json.Unmarshal([]byte(`{"chatProbe":true,"probeModels":["cheap"]}`), &chatProbe); err != nil {
		t.Fatalf("object: %v", err)
	}
	if !chatProbe.ChatProbe || len(chatProbe.ProbeModels) != 1 {
		t.Fatalf("object form = %+v", chatProbe)
	}
	for _, raw := range []string{
		`{"type":"ratelimit_headers","maxFreeRequestsPerDay":1000,"probeModels":["cheap"]}`,
		`{"type":"paid_model_probe","paidOnlyModels":["expensive"]}`,
	} {
		var verify Verify
		if err := json.Unmarshal([]byte(raw), &verify); err != nil {
			t.Fatalf("%s: %v", raw, err)
		}
		back, err := json.Marshal(verify)
		if err != nil {
			t.Fatalf("%s: %v", raw, err)
		}
		if diff := diffValues("", generic(t, back), generic(t, []byte(raw))); diff != "" {
			t.Fatalf("%s: %s", raw, diff)
		}
	}
	// The fields a definition leaves unset are absent, not zero.
	if encoded, err := json.Marshal(Quirks{StreamUsage: boolean(false)}); err != nil || string(encoded) != `{"streamUsage":false}` {
		t.Fatalf("quirks = %s (%v)", encoded, err)
	}
	if !IsTrue(boolean(true)) || IsTrue(nil) || IsTrue(boolean(false)) {
		t.Fatal("IsTrue misreads a flag")
	}
}

// stubAdapter is the smallest Adapter, for the registry tests.
type stubAdapter struct {
	definition Definition
}

func (s stubAdapter) Definition() Definition { return s.definition }

func (s stubAdapter) Validate(context.Context) (Validation, error) { return ValidKey(), nil }

func (s stubAdapter) ListModels(context.Context) ([]Model, error) { return nil, nil }

func (s stubAdapter) FetchQuota(context.Context) ([]QuotaWindow, error) { return nil, nil }

func (s stubAdapter) Supports(Capability) bool { return true }

func (s stubAdapter) TranscribeTranslate() bool { return true }

func (s stubAdapter) TranscribeTimestamps() bool { return true }
