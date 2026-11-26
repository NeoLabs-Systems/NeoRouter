package catalog

import (
	"net/http"
	"testing"
)

// TestModelOverridesPinContextAndOutputLimits ports
// test/api/model_overrides.test.js: contextWindow and maxOutput can be pinned
// per model from the API, and cleared back to what the listing reports.
func TestModelOverridesPinContextAndOutputLimits(t *testing.T) {
	api := newTestRoutes(t)
	// The host reports a 32K window and no output cap.
	api.syncOne(t, ListedModel{ID: "ok-limits", Kind: "chat", ContextWindow: ip(32768)})

	recorder := api.do(t, http.MethodPatch, "/api/v1/models/1", `{"overrides":{"contextWindow":262144,"maxOutput":16384}}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("pin status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	model := decodeBody(t, recorder)["model"].(map[string]any)
	if model["contextWindow"] != float64(262144) {
		t.Errorf("contextWindow = %v, want 262144", model["contextWindow"])
	}
	if model["maxOutput"] != float64(16384) {
		t.Errorf("maxOutput = %v, want 16384", model["maxOutput"])
	}

	// The pin is stored, so a fresh read reports it too.
	recorder = api.do(t, http.MethodGet, "/api/v1/models/1", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("get status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	model = decodeBody(t, recorder)["model"].(map[string]any)
	if model["contextWindow"] != float64(262144) || model["maxOutput"] != float64(16384) {
		t.Errorf("read back = %v/%v, want 262144/16384", model["contextWindow"], model["maxOutput"])
	}

	// Clearing falls back to what the listing reports: 32768 context, no output
	// cap.
	recorder = api.do(t, http.MethodPatch, "/api/v1/models/1", `{"overrides":{"contextWindow":null,"maxOutput":null}}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("clear status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	model = decodeBody(t, recorder)["model"].(map[string]any)
	if model["contextWindow"] != float64(32768) {
		t.Errorf("cleared contextWindow = %v, want the listed 32768", model["contextWindow"])
	}
	if model["maxOutput"] != nil {
		t.Errorf("cleared maxOutput = %v, want null", model["maxOutput"])
	}
	overrides, ok := model["overrides"].(map[string]any)
	if !ok {
		t.Fatalf("overrides = %v", model["overrides"])
	}
	if _, present := overrides["maxOutput"]; present {
		t.Errorf("overrides = %v, want maxOutput removed", overrides)
	}
}

// TestMaxOutputOverrideSurvivesLaterUpdates guards the override key list: a
// value the PATCH route accepts must survive the update that stores it.
func TestMaxOutputOverrideSurvivesLaterUpdates(t *testing.T) {
	api := newTestRoutes(t)
	api.syncOne(t, ListedModel{ID: "ok-limits", Kind: "chat", ContextWindow: ip(32768), MaxOutput: ip(4096)})
	if recorder := api.do(t, http.MethodPatch, "/api/v1/models/1", `{"overrides":{"maxOutput":16384}}`); recorder.Code != http.StatusOK {
		t.Fatalf("pin status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	recorder := api.do(t, http.MethodPatch, "/api/v1/models/1", `{"enabled":false}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("update status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	model := decodeBody(t, recorder)["model"].(map[string]any)
	if model["maxOutput"] != float64(16384) {
		t.Errorf("maxOutput = %v, want the pin to survive", model["maxOutput"])
	}
}
