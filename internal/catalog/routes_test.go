package catalog

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeRouting is the routing domain as the models routes consume it.
type fakeRouting struct {
	metrics map[int64]ModelMetrics
	cleared []int64
}

func (f *fakeRouting) ModelMetrics(_ context.Context, ref int64) ModelMetrics {
	if metrics, ok := f.metrics[ref]; ok {
		return metrics
	}
	return ModelMetrics{}
}

func (f *fakeRouting) ClearCooldown(_ context.Context, ref int64) { f.cleared = append(f.cleared, ref) }

// testRoutes is a mounted API with one synced account.
type testRoutes struct {
	env     *testEnv
	mux     *http.ServeMux
	routing *fakeRouting
}

func newTestRoutes(t *testing.T) *testRoutes {
	t.Helper()
	env := newTestEnv(t)
	routing := &fakeRouting{metrics: map[int64]ModelMetrics{}}
	routes := &Routes{
		Svc:     env.svc,
		Log:     slog.New(slog.DiscardHandler),
		Routing: routing,
		OwnerID: func(*http.Request) string { return "user-1" },
		UserID:  func(*http.Request) string { return "user-1" },
		Capabilities: func() []Capability {
			return []Capability{{ID: "chat", Label: "Chat"}, {ID: "embedding", Label: "Embeddings"}, {ID: "live", Label: "Live voice"}}
		},
		LiveProtocols: func(_ context.Context, _ string) []string { return []string{"gemini_live"} },
	}
	mux := http.NewServeMux()
	routes.Mount(mux)
	routes.MountPresets(mux)
	return &testRoutes{env: env, mux: mux, routing: routing}
}

// syncOne seeds one account with the given models and syncs it.
func (r *testRoutes) syncOne(t *testing.T, models ...ListedModel) {
	t.Helper()
	r.env.seedAccount(t, "acc-1", "groq", "groq-main", "free_tier", func(seed *accountSeed) {
		seed.billingState = "free"
	})
	r.env.models.models["acc-1"] = models
	r.env.syncAccount(t, "acc-1")
}

// do performs one request against the mounted API.
func (r *testRoutes) do(t *testing.T, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	request := httptest.NewRequest(method, target, reader)
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	recorder := httptest.NewRecorder()
	r.mux.ServeHTTP(recorder, request)
	return recorder
}

func decodeBody(t *testing.T, recorder *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	decoded := map[string]any{}
	if err := json.Unmarshal(recorder.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decode %q: %v", recorder.Body.String(), err)
	}
	return decoded
}

func TestModelsListReturnsTheCatalogShape(t *testing.T) {
	api := newTestRoutes(t)
	api.syncOne(t,
		ListedModel{ID: "llama-3.1-8b-instant", Name: "Llama 3.1 8B", Kind: "chat", ContextWindow: ip(131072), Tools: bp(true)},
		ListedModel{ID: "text-embedding-3-small", Kind: "embedding"},
	)
	recorder := api.do(t, http.MethodGet, "/api/v1/models", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	body := decodeBody(t, recorder)
	models, ok := body["models"].([]any)
	if !ok || len(models) != 1 {
		t.Fatalf("models = %v, want one chat model", body["models"])
	}
	entry := models[0].(map[string]any)
	for key, value := range map[string]any{
		"id": "groq-main/llama-3.1-8b-instant", "modelId": "llama-3.1-8b-instant",
		"canonicalId": "llama318b", "capability": "chat", "accountId": "acc-1", "userId": "user-1",
		"providerSlug": "groq-main", "definitionId": "groq", "providerKey": "groq", "providerName": "Groq",
		"billingMode": "free_tier", "overflowPolicy": "auto", "accountStatus": "healthy",
	} {
		if entry[key] != value {
			t.Errorf("%s = %v, want %v", key, entry[key], value)
		}
	}
	if _, present := entry["liveProtocols"]; present {
		t.Error("a chat model must not report liveProtocols")
	}
	for _, key := range []string{"reliability", "speedScore", "costScore", "availability", "stats"} {
		if _, present := entry[key]; !present {
			t.Errorf("%s is missing", key)
		}
	}
	if _, present := entry["freeReason"]; !present {
		t.Error("freeReason must be present (null when paid)")
	}
	stats := entry["stats"].(map[string]any)
	for _, key := range []string{"successes", "failures", "decayedAt", "latencyMs", "ttftMs", "tps", "samples",
		"penalty", "penaltyAt", "cooldownUntil", "cooldownReason", "breakerOpenUntil", "lastError",
		"lastSuccessAt", "lastFailureAt"} {
		if _, present := stats[key]; !present {
			t.Errorf("stats.%s is missing", key)
		}
	}
	// Key order is part of the contract.
	raw := recorder.Body.String()
	previous := -1
	for _, key := range []string{`"ref"`, `"id"`, `"modelId"`, `"name"`, `"canonicalId"`, `"capability"`,
		`"accountId"`, `"userId"`, `"providerSlug"`, `"definitionId"`, `"providerKey"`, `"providerName"`,
		`"billingMode"`, `"isFlatRate"`, `"slotPlan"`, `"overflowPolicy"`, `"slotWaitMs"`, `"enabled"`,
		`"available"`, `"accountEnabled"`, `"accountStatus"`, `"contextWindow"`, `"maxOutput"`, `"tools"`,
		`"vision"`, `"json"`, `"reasoning"`, `"priceIn"`, `"priceOut"`, `"priceCachedIn"`, `"priceCacheWrite"`,
		`"priceSource"`, `"priceUnit"`, `"unitPrice"`, `"isFree"`, `"decision"`, `"isPlan"`, `"planOverflow"`,
		`"freeReason"`, `"intelligence"`, `"skills"`, `"intelligenceSource"`, `"intelligenceConfidence"`,
		`"speedPriorTps"`, `"releaseDate"`, `"paramsB"`, `"overrides"`, `"stats"`, `"reliability"`,
		`"speedScore"`, `"costScore"`, `"availability"`} {
		at := strings.Index(raw, key+":")
		if at < 0 {
			t.Fatalf("%s is missing from %s", key, raw)
		}
		if at < previous {
			t.Errorf("%s is out of order in %s", key, raw)
		}
		previous = at
	}
}

func TestModelsListReportsLiveProtocols(t *testing.T) {
	api := newTestRoutes(t)
	api.syncOne(t, ListedModel{ID: "gemini-3.8-live", Kind: "live"})
	recorder := api.do(t, http.MethodGet, "/api/v1/models?capability=live", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	models := decodeBody(t, recorder)["models"].([]any)
	if len(models) != 1 {
		t.Fatalf("models = %v, want one live model", models)
	}
	protocols, ok := models[0].(map[string]any)["liveProtocols"].([]any)
	if !ok || len(protocols) != 1 || protocols[0] != "gemini_live" {
		t.Errorf("liveProtocols = %v, want [gemini_live]", models[0].(map[string]any)["liveProtocols"])
	}
}

func TestModelsCapabilitiesIncludeRoutableKindsOnlyWithAll(t *testing.T) {
	api := newTestRoutes(t)
	api.syncOne(t, ListedModel{ID: "llama-3.1-8b-instant", Kind: "chat"}, ListedModel{ID: "embed-1", Kind: "embedding"})
	recorder := api.do(t, http.MethodGet, "/api/v1/models/capabilities", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	items := decodeBody(t, recorder)["capabilities"].([]any)
	if len(items) != 2 {
		t.Fatalf("capabilities = %v, want the two kinds that have models", items)
	}
	foundChat := false
	for _, item := range items {
		entry := item.(map[string]any)
		if entry["id"] != "chat" {
			continue
		}
		foundChat = true
		if entry["routable"] != true {
			t.Errorf("chat capability = %v, want routable", entry)
		}
		if _, present := entry["total"]; !present {
			t.Error("total is missing")
		}
	}
	if !foundChat {
		t.Errorf("capabilities = %v, want chat", items)
	}

	recorder = api.do(t, http.MethodGet, "/api/v1/models/capabilities?all=1", "")
	items = decodeBody(t, recorder)["capabilities"].([]any)
	foundLive := false
	for _, item := range items {
		entry := item.(map[string]any)
		switch entry["id"] {
		case "live":
			foundLive = true
			if entry["total"] != float64(0) || entry["routable"] != true {
				t.Errorf("live capability = %v, want total 0 and routable", entry)
			}
		case "chat":
			if entry["free"] != float64(1) {
				t.Errorf("chat free = %v, want 1", entry["free"])
			}
		}
	}
	if !foundLive {
		t.Error("?all=1 must list the routable kinds nothing is connected for")
	}
}

func TestModelsUnknownRefIs404(t *testing.T) {
	api := newTestRoutes(t)
	api.syncOne(t, ListedModel{ID: "llama-3.1-8b-instant", Kind: "chat"})
	for _, target := range []string{"/api/v1/models/abc", "/api/v1/models/4242", "/api/v1/models/1.5"} {
		recorder := api.do(t, http.MethodGet, target, "")
		if recorder.Code != http.StatusNotFound {
			t.Errorf("%s status = %d, want 404", target, recorder.Code)
			continue
		}
		errorBody := decodeBody(t, recorder)["error"].(map[string]any)
		if errorBody["code"] != "MODEL_NOT_FOUND" || errorBody["message"] != "This model does not exist." {
			t.Errorf("%s error = %v", target, errorBody)
		}
	}
}

func TestModelsPatchValidatesTheBodyLikeZod(t *testing.T) {
	api := newTestRoutes(t)
	api.syncOne(t, ListedModel{ID: "llama-3.1-8b-instant", Kind: "chat"})
	cases := []struct{ body, message string }{
		{`{"enabled":"x"}`, "enabled: Expected boolean, received string"},
		{`{"overrides":"x"}`, "overrides: Expected object, received string"},
		{`{"overrides":{"priceIn":-1}}`, "overrides.priceIn: Number must be greater than or equal to 0"},
		{`{"overrides":{"contextWindow":1.5}}`, "overrides.contextWindow: Expected integer, received float"},
		{`{"overrides":{"contextWindow":0}}`, "overrides.contextWindow: Number must be greater than 0"},
		{`{"overrides":{"intelligence":2}}`, "overrides.intelligence: Number must be less than or equal to 1"},
		{`{"overrides":{"priceUnit":"hour"}}`, "overrides.priceUnit: Invalid enum value. Expected 'image' | 'second' | 'minute' | 'mchar' | 'search' | 'call', received 'hour'"},
		{`{"overrides":{"billedAt":"yesterday"}}`, "overrides.billedAt: Expected null, received string"},
	}
	for _, testCase := range cases {
		recorder := api.do(t, http.MethodPatch, "/api/v1/models/1", testCase.body)
		if recorder.Code != http.StatusBadRequest {
			t.Errorf("%s status = %d, want 400", testCase.body, recorder.Code)
			continue
		}
		errorBody := decodeBody(t, recorder)["error"].(map[string]any)
		if errorBody["code"] != "VALIDATION_ERROR" {
			t.Errorf("%s code = %v", testCase.body, errorBody["code"])
		}
		if errorBody["message"] != testCase.message {
			t.Errorf("%s message = %q, want %q", testCase.body, errorBody["message"], testCase.message)
		}
		details, ok := errorBody["details"].(map[string]any)
		if !ok {
			t.Fatalf("%s details = %v", testCase.body, errorBody["details"])
		}
		if _, present := details["fieldErrors"]; !present {
			t.Errorf("%s details = %v, want fieldErrors", testCase.body, details)
		}
	}
}

func TestModelsPatchAppliesTheOverride(t *testing.T) {
	api := newTestRoutes(t)
	api.syncOne(t, ListedModel{ID: "llama-3.1-8b-instant", Kind: "chat"})
	recorder := api.do(t, http.MethodPatch, "/api/v1/models/1", `{"enabled":false,"overrides":{"free":true}}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	model := decodeBody(t, recorder)["model"].(map[string]any)
	if model["isFree"] != true {
		t.Errorf("isFree = %v, want true", model["isFree"])
	}
	if model["enabled"] != false {
		t.Errorf("enabled = %v, want false", model["enabled"])
	}
	if model["freeReason"] != "manual_free" {
		t.Errorf("freeReason = %v, want manual_free", model["freeReason"])
	}
}

func TestModelsBulkSwitchesModels(t *testing.T) {
	api := newTestRoutes(t)
	api.syncOne(t, ListedModel{ID: "model-a", Kind: "chat"}, ListedModel{ID: "model-b", Kind: "chat"})
	recorder := api.do(t, http.MethodPost, "/api/v1/models/bulk", `{"enabled":false,"onlyFree":true}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if updated := decodeBody(t, recorder)["updated"]; updated != float64(2) {
		t.Errorf("updated = %v, want 2", updated)
	}
	recorder = api.do(t, http.MethodPost, "/api/v1/models/bulk", `{}`)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("missing enabled status = %d, want 400", recorder.Code)
	}
	if message := decodeBody(t, recorder)["error"].(map[string]any)["message"]; message != "enabled: Required" {
		t.Errorf("message = %q, want enabled: Required", message)
	}
}

func TestModelsResetHealthClearsTheCooldown(t *testing.T) {
	api := newTestRoutes(t)
	api.syncOne(t, ListedModel{ID: "llama-3.1-8b-instant", Kind: "chat"})
	recorder := api.do(t, http.MethodPost, "/api/v1/models/1/reset-health", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if len(api.routing.cleared) != 1 || api.routing.cleared[0] != 1 {
		t.Errorf("cleared = %v, want [1]", api.routing.cleared)
	}
	if _, present := decodeBody(t, recorder)["model"]; !present {
		t.Error("the response must carry the refreshed model")
	}
}

func TestModelsPresentUsesTheRoutingMetrics(t *testing.T) {
	api := newTestRoutes(t)
	api.syncOne(t, ListedModel{ID: "llama-3.1-8b-instant", Kind: "chat"})
	api.routing.metrics[1] = ModelMetrics{
		Reliability: 0.75, SpeedScore: 0.5, CostScore: 0.25,
		Availability: map[string]any{"available": false, "reason": "cooldown", "until": 42.0},
		Penalty:      0.5, Successes: 3, Failures: 1,
	}
	recorder := api.do(t, http.MethodGet, "/api/v1/models/1", "")
	model := decodeBody(t, recorder)["model"].(map[string]any)
	if model["reliability"] != 0.75 || model["speedScore"] != 0.5 || model["costScore"] != 0.25 {
		t.Errorf("scores = %v/%v/%v", model["reliability"], model["speedScore"], model["costScore"])
	}
	availability := model["availability"].(map[string]any)
	if availability["available"] != false || availability["reason"] != "cooldown" {
		t.Errorf("availability = %v", availability)
	}
	stats := model["stats"].(map[string]any)
	if stats["penalty"] != 0.5 || stats["successes"] != float64(3) || stats["failures"] != float64(1) {
		t.Errorf("stats = %v", stats)
	}
}

func TestModelPresetsAreSavedPerUserAndValidated(t *testing.T) {
	api := newTestRoutes(t)
	recorder := api.do(t, http.MethodGet, "/api/v1/model-presets", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	presets, ok := decodeBody(t, recorder)["presets"].([]any)
	if !ok || len(presets) != 0 {
		t.Fatalf("presets = %v, want an empty list", decodeBody(t, recorder)["presets"])
	}

	body := `{"presets":[{"id":"p1","name":"  Smart  ","models":["deepseek/deepseek-v4","qwen/qwen-max"]}]}`
	recorder = api.do(t, http.MethodPut, "/api/v1/model-presets", body)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	saved := decodeBody(t, recorder)["presets"].([]any)
	first := saved[0].(map[string]any)
	if first["name"] != "Smart" {
		t.Errorf("name = %v, want the trimmed name", first["name"])
	}
	if len(first) != 3 {
		t.Errorf("preset = %v, want exactly id, name and models", first)
	}

	recorder = api.do(t, http.MethodGet, "/api/v1/model-presets", "")
	saved = decodeBody(t, recorder)["presets"].([]any)
	if len(saved) != 1 || saved[0].(map[string]any)["id"] != "p1" {
		t.Errorf("stored presets = %v", saved)
	}

	recorder = api.do(t, http.MethodPut, "/api/v1/model-presets", `{"presets":[{"id":"p2","name":"","models":[]}]}`)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("invalid presets status = %d, want 400", recorder.Code)
	}
	message := decodeBody(t, recorder)["error"].(map[string]any)["message"]
	if message != "presets.0.name: String must contain at least 1 character(s)" {
		t.Errorf("message = %q", message)
	}
	if recorder := api.do(t, http.MethodPut, "/api/v1/model-presets", `{"presets":"x"}`); recorder.Code != http.StatusBadRequest {
		t.Errorf("non-array presets status = %d, want 400", recorder.Code)
	}
}
