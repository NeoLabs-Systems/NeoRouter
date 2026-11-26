package providers

import "testing"

// Kourier's listing is bare — id, object, created, owned_by, no limits
// (api.kourier.sh/v1/models, 2026-11-20). The limits the provider documents
// (docs.kourier.sh/docs/models: 256K total context, 16K output cap per
// response) live in the definition as models.facts, so a sync can fill what
// the listing leaves empty.
func TestKourierDefinitionCarriesDocumentedModelFacts(t *testing.T) {
	definition, ok := mustLoad(t).Get("kourier")
	if !ok {
		t.Fatal("kourier: no definition")
	}
	facts, ok := definition.Models.Facts["deepseek-v4.1-flash"]
	if !ok {
		t.Fatalf("kourier models.facts = %v, want deepseek-v4.1-flash", definition.Models.Facts)
	}
	if facts.ContextWindow == nil || *facts.ContextWindow != 262144 {
		t.Errorf("contextWindow = %v, want 262144", facts.ContextWindow)
	}
	if facts.MaxOutput == nil || *facts.MaxOutput != 16384 {
		t.Errorf("maxOutput = %v, want 16384", facts.MaxOutput)
	}
	if got := len(definition.Models.Facts); got != 1 {
		t.Errorf("models.facts = %d entries, want only the documented model", got)
	}
}

// TestModelFactsAreOptionalPerField proves a definition may document one limit
// without the other: the sync then fills only the missing one.
func TestModelFactsAreOptionalPerField(t *testing.T) {
	data := `[{
		"id": "example", "label": "Example", "description": "", "category": "paid",
		"adapter": "openai_compatible", "capabilities": ["chat"], "media": {},
		"live": [], "baseUrl": "https://example.test/v1", "editableBaseUrl": false,
		"baseUrlSchemes": ["http", "https"], "auth": {"type": "bearer"},
		"keyOptional": false, "fields": [], "headers": {},
		"models": {"extraPaths": [], "optIn": [], "facts": {"m": {"maxOutput": 4096}}}, "extraModels": [],
		"billing": {"defaultMode": "paid", "freeTierAll": false, "freeTierModels": [],
			"localOnly": false, "keylessOnly": false, "alwaysFree": [], "trustListingFree": true, "note": ""},
		"limits": {"pool": "model", "account": {}, "defaults": {}, "models": []},
		"quota": {"headerPeriods": {"requests": "minute", "tokens": "minute"}, "plainHeaderMetric": "rpm"},
		"quirks": {}
	}]`
	registry, err := Parse([]byte(data), nil, nil)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	definition, _ := registry.Get("example")
	facts := definition.Models.Facts["m"]
	if facts.ContextWindow != nil {
		t.Errorf("contextWindow = %v, want absent", facts.ContextWindow)
	}
	if facts.MaxOutput == nil || *facts.MaxOutput != 4096 {
		t.Errorf("maxOutput = %v, want 4096", facts.MaxOutput)
	}
}
