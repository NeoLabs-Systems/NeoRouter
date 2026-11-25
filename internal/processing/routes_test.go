package processing

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// testServer mounts the processing routes with a resolved space and permissions.
func testServer(t *testing.T, permissions ...string) (*httptest.Server, *Service) {
	t.Helper()
	service, _ := testService(t)
	granted := map[string]bool{}
	for _, permission := range permissions {
		granted[permission] = true
	}
	service.Space = func(ctx context.Context) (Space, bool) {
		return Space{OwnerID: "u1", Permissions: granted}, true
	}
	mux := http.NewServeMux()
	service.Routes(mux)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server, service
}

func doJSON(t *testing.T, method, url string, body string) (int, map[string]any) {
	t.Helper()
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	request, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	decoded := map[string]any{}
	_ = json.NewDecoder(response.Body).Decode(&decoded)
	return response.StatusCode, decoded
}

// TestSettingsRoutes is the ported settings part of test/api/processing.test.js.
func TestSettingsRoutes(t *testing.T) {
	server, _ := testServer(t, "processing.view", "processing.edit")
	status, body := doJSON(t, http.MethodGet, server.URL+"/api/v1/processing/settings", "")
	if status != http.StatusOK {
		t.Fatalf("GET settings: %d %v", status, body)
	}
	for _, key := range []string{"settings", "categories", "aiOnlyCategories", "levels", "engines", "privacyModel"} {
		if _, ok := body[key]; !ok {
			t.Fatalf("%s missing from %v", key, body)
		}
	}
	settings := body["settings"].(map[string]any)
	privacy := settings["privacy"].(map[string]any)
	if privacy["enabled"] != false {
		t.Fatalf("processing starts off: %v", privacy)
	}
	categories := body["categories"].([]any)
	if len(categories) != len(Categories) {
		t.Fatalf("%d categories", len(categories))
	}
	if got := body["levels"].([]any); len(got) != 4 || got[0] != "off" {
		t.Fatalf("levels %v", got)
	}
	if got := body["aiOnlyCategories"].([]any); len(got) != 1 || got[0] != "organization" {
		t.Fatalf("AI-only categories %v", got)
	}
	model := body["privacyModel"].(map[string]any)
	if model["model"] != PrivacyModelID || model["memoryMb"] != float64(PrivacyModelMemoryMB) || model["status"] != "off" {
		t.Fatalf("privacy model status %v", model)
	}
	if model["progress"] != nil || model["error"] != nil {
		t.Fatalf("progress and error start as null: %v", model)
	}

	// Saving settings and reading them back.
	privacy["enabled"] = true
	privacy["customTerms"] = []any{"Acme Holding"}
	settings["compression"].(map[string]any)["level"] = "lossless"
	encoded, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	status, body = doJSON(t, http.MethodPut, server.URL+"/api/v1/processing/settings", string(encoded))
	if status != http.StatusOK {
		t.Fatalf("PUT settings: %d %v", status, body)
	}
	saved := body["settings"].(map[string]any)["privacy"].(map[string]any)
	if saved["enabled"] != true {
		t.Fatalf("settings not saved: %v", saved)
	}
	if terms, _ := saved["customTerms"].([]any); len(terms) != 1 || terms[0] != "Acme Holding" {
		t.Fatalf("custom terms %v", saved["customTerms"])
	}
	_, body = doJSON(t, http.MethodGet, server.URL+"/api/v1/processing/settings", "")
	reloaded := body["settings"].(map[string]any)
	if reloaded["compression"].(map[string]any)["level"] != "lossless" {
		t.Fatalf("compression level not saved: %v", reloaded["compression"])
	}

	// An unknown level is a validation error, as in the zod schema.
	invalid := `{"privacy":{"enabled":true,"modes":{},"customTerms":[],"allowTerms":[],"ai":{"enabled":false}},"compression":{"level":"ultra","engines":{},"minTokens":2000,"targetTokens":null,"keepRecent":6}}`
	if status, body = doJSON(t, http.MethodPut, server.URL+"/api/v1/processing/settings", invalid); status != http.StatusBadRequest {
		t.Fatalf("invalid level answered %d %v", status, body)
	}
	if status, _ = doJSON(t, http.MethodPut, server.URL+"/api/v1/processing/settings",
		`{"privacy":{"enabled":true,"modes":{"nope":"hide"},"customTerms":[],"allowTerms":[],"ai":{"enabled":false}},"compression":{"level":"off","engines":{},"minTokens":2000,"targetTokens":null,"keepRecent":6}}`); status != http.StatusBadRequest {
		t.Fatalf("unknown category answered %d", status)
	}
}

// TestPreviewRoute is the ported preview case.
func TestPreviewRoute(t *testing.T) {
	server, service := testServer(t, "processing.view", "processing.edit")
	settings := service.GetSettings(context.Background(), "u1")
	settings.Privacy.Enabled = true
	settings.Privacy.CustomTerms = []string{"Acme Holding"}
	if _, err := service.SaveSettings(context.Background(), "u1", settings); err != nil {
		t.Fatal(err)
	}
	status, body := doJSON(t, http.MethodPost, server.URL+"/api/v1/processing/preview", `{"text":"Call +49 170 1234567"}`)
	if status != http.StatusOK {
		t.Fatalf("preview: %d %v", status, body)
	}
	messages := body["messages"].([]any)
	content := messages[len(messages)-1].(map[string]any)["content"]
	if content != "Call [PHONE_1]" {
		t.Fatalf("preview content %q", content)
	}
	if body["stats"].(map[string]any)["redacted"] != float64(1) {
		t.Fatalf("stats %v", body["stats"])
	}

	// The findings name the rule that found each value.
	status, body = doJSON(t, http.MethodPost, server.URL+"/api/v1/processing/preview",
		`{"text":"Hallo, ich bin Anna Schmidt, Steuer-ID 86 095 742 719, wohnhaft Hauptstraße 5, 10115 Berlin."}`)
	if status != http.StatusOK {
		t.Fatalf("preview: %d %v", status, body)
	}
	messages = body["messages"].([]any)
	if got := messages[len(messages)-1].(map[string]any)["content"]; got != "Hallo, ich bin Anna [LASTNAME_1], Steuer-ID [ID_1], wohnhaft [ADDRESS_1]." {
		t.Fatalf("preview content %q", got)
	}
	labels := map[string]string{}
	for _, raw := range body["findings"].([]any) {
		finding := raw.(map[string]any)
		label := finding["label"].(map[string]any)
		labels[finding["token"].(string)] = label["de"].(string)
	}
	want := map[string]string{"[LASTNAME_1]": "Nachname", "[ID_1]": "Steuer-ID", "[ADDRESS_1]": "Postanschrift"}
	if len(labels) != len(want) {
		t.Fatalf("findings %v", labels)
	}
	for token, label := range want {
		if labels[token] != label {
			t.Fatalf("finding %s: %q, want %q", token, labels[token], label)
		}
	}
}

// TestPreviewNeedsNoEditRight proves a viewer previews the saved settings, not a
// draft.
func TestPreviewNeedsNoEditRight(t *testing.T) {
	server, _ := testServer(t, "processing.view")
	status, body := doJSON(t, http.MethodPost, server.URL+"/api/v1/processing/preview",
		`{"text":"Call +49 170 1234567","settings":{"privacy":{"enabled":true,"modes":{},"customTerms":[],"allowTerms":[],"ai":{"enabled":false}},"compression":{"level":"off","engines":{},"minTokens":2000,"targetTokens":null,"keepRecent":6}}}`)
	if status != http.StatusOK {
		t.Fatalf("preview: %d %v", status, body)
	}
	messages := body["messages"].([]any)
	if got := messages[len(messages)-1].(map[string]any)["content"]; got != "Call +49 170 1234567" {
		t.Fatalf("a viewer only sees the saved settings, got %q", got)
	}
}

// TestStatsRoute is the ported stats shape; the counting query belongs to the
// gateway's requests table and is wired by internal/app.
func TestStatsRoute(t *testing.T) {
	server, service := testServer(t, "processing.view")
	status, body := doJSON(t, http.MethodGet, server.URL+"/api/v1/processing/stats", "")
	if status != http.StatusOK {
		t.Fatalf("stats: %d %v", status, body)
	}
	if body["days"] != float64(30) || body["redacted"] != float64(0) {
		t.Fatalf("stats %v", body)
	}
	service.StatsQuery = func(ctx context.Context, userID string, sinceMillis int64) (Stats, error) {
		if userID != "u1" || sinceMillis <= 0 {
			t.Fatalf("query for %q since %d", userID, sinceMillis)
		}
		return Stats{Requests: 5, ProtectedRequests: 2, Redacted: 4, CompressedRequests: 1, SavedTokens: 88}, nil
	}
	_, body = doJSON(t, http.MethodGet, server.URL+"/api/v1/processing/stats", "")
	if body["requests"] != float64(5) || body["protectedRequests"] != float64(2) || body["savedTokens"] != float64(88) {
		t.Fatalf("stats %v", body)
	}
}

// TestRoutesNeedASession answers 401 without a resolved space.
func TestRoutesNeedASession(t *testing.T) {
	service, err := New(nil, newMemorySettings(), nil)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	service.Routes(mux)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/processing/settings", nil)
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status %d", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "AUTHENTICATION_REQUIRED") {
		t.Fatalf("body %s", recorder.Body.String())
	}
}
