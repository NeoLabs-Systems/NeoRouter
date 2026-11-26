package routing

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// The /api/v1/routing routes: same paths, status codes and response bodies.

func newRoutesServer(t *testing.T) (*fixture, *httptest.Server) {
	t.Helper()
	f := newFixture(t)
	mux := http.NewServeMux()
	f.svc.Routes(mux, RouteDeps{
		Owner: func(r *http.Request) (string, string) { return "u", "u" },
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return f, server
}

func callJSON(t *testing.T, method, url string, body any) (*http.Response, []byte) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("encode body: %v", err)
		}
		reader = bytes.NewReader(encoded)
	}
	request, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer response.Body.Close()
	payload, _ := io.ReadAll(response.Body)
	return response, payload
}

func TestRoutingProfileRoutes(t *testing.T) {
	_, server := newRoutesServer(t)
	response, payload := callJSON(t, "GET", server.URL+"/api/v1/routing/profiles", nil)
	if response.StatusCode != 200 {
		t.Fatalf("GET profiles = %d %s", response.StatusCode, payload)
	}
	var list struct {
		Profiles []Profile `json:"profiles"`
	}
	if err := json.Unmarshal(payload, &list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(list.Profiles) != 3 || list.Profiles[0].ID != "balanced" || list.Profiles[2].ID != "smart" {
		t.Fatalf("profiles = %+v", list.Profiles)
	}
	if !list.Profiles[0].Preset {
		t.Fatalf("presets are marked as presets")
	}

	response, payload = callJSON(t, "POST", server.URL+"/api/v1/routing/profiles", map[string]any{
		"name":    "Cheap and fast",
		"weights": map[string]any{"intelligence": 0.2, "speed": 0.5, "reliability": 0.2, "cost": 0.1},
	})
	if response.StatusCode != 201 {
		t.Fatalf("POST profile = %d %s", response.StatusCode, payload)
	}
	var created struct {
		Profile map[string]any `json:"profile"`
	}
	if err := json.Unmarshal(payload, &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if created.Profile["slug"] != "cheap-and-fast" {
		t.Fatalf("slug = %v", created.Profile["slug"])
	}
	id, _ := created.Profile["id"].(string)
	if id == "" {
		t.Fatalf("profile id missing")
	}

	// The profile's constraints serialize with every default key.
	constraints, ok := created.Profile["constraints"].(map[string]any)
	if !ok {
		t.Fatalf("constraints missing: %v", created.Profile)
	}
	for _, key := range []string{"minIntelligence", "maxPriceOut", "maxTtftMs", "includeProviders", "excludeProviders", "excludeModels", "adaptive", "exploration", "kinds"} {
		if _, present := constraints[key]; !present {
			t.Fatalf("constraint %q missing from %v", key, constraints)
		}
	}

	response, payload = callJSON(t, "PATCH", server.URL+"/api/v1/routing/profiles/"+id, map[string]any{"name": "Cheaper"})
	if response.StatusCode != 200 {
		t.Fatalf("PATCH profile = %d %s", response.StatusCode, payload)
	}
	// A preset cannot be changed.
	response, _ = callJSON(t, "PATCH", server.URL+"/api/v1/routing/profiles/balanced", map[string]any{"name": "Nope"})
	if response.StatusCode != 404 {
		t.Fatalf("PATCH preset = %d, want 404", response.StatusCode)
	}
	// Weights must have something above zero.
	response, payload = callJSON(t, "POST", server.URL+"/api/v1/routing/profiles", map[string]any{
		"name":    "Nothing",
		"weights": map[string]any{"intelligence": 0, "speed": 0, "reliability": 0, "cost": 0},
	})
	if response.StatusCode != 400 {
		t.Fatalf("POST empty weights = %d %s", response.StatusCode, payload)
	}
	response, _ = callJSON(t, "DELETE", server.URL+"/api/v1/routing/profiles/"+id, nil)
	if response.StatusCode != 204 {
		t.Fatalf("DELETE profile = %d, want 204", response.StatusCode)
	}
	response, _ = callJSON(t, "DELETE", server.URL+"/api/v1/routing/profiles/"+id, nil)
	if response.StatusCode != 404 {
		t.Fatalf("DELETE missing profile = %d, want 404", response.StatusCode)
	}
}

// TestRoutingGroupsEmptyIsArray pins the wire shape the dashboard casts to a
// List: with no groups the field is [], never null (a nil Go slice marshals to
// null and would throw in the UI).
func TestRoutingGroupsEmptyIsArray(t *testing.T) {
	_, server := newRoutesServer(t)
	response, payload := callJSON(t, "GET", server.URL+"/api/v1/routing/groups", nil)
	if response.StatusCode != 200 {
		t.Fatalf("GET groups = %d %s", response.StatusCode, payload)
	}
	if !strings.Contains(string(payload), `"groups":[]`) {
		t.Fatalf("empty groups must serialize as [], got %s", payload)
	}
}

func TestRoutingGroupRoutes(t *testing.T) {
	f, server := newRoutesServer(t)
	f.account("a", "u", "openai", 0, "paid")
	f.entry("m", "a", func(e *Entry) { e.DefinitionID = "openai" })

	response, payload := callJSON(t, "POST", server.URL+"/api/v1/routing/groups", map[string]any{
		"name": "work", "accounts": []string{"a"}, "allowMetered": true,
	})
	if response.StatusCode != 201 {
		t.Fatalf("POST group = %d %s", response.StatusCode, payload)
	}
	var created struct {
		Group Group `json:"group"`
	}
	if err := json.Unmarshal(payload, &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if created.Group.ID == "" || created.Group.Name != "work" {
		t.Fatalf("group = %+v", created.Group)
	}
	// A name must be a routing name.
	response, payload = callJSON(t, "POST", server.URL+"/api/v1/routing/groups", map[string]any{
		"name": "Work Group", "accounts": []string{"a"},
	})
	if response.StatusCode != 400 {
		t.Fatalf("bad name = %d %s", response.StatusCode, payload)
	}
	// At least one account or model.
	response, _ = callJSON(t, "POST", server.URL+"/api/v1/routing/groups", map[string]any{"name": "empty"})
	if response.StatusCode != 400 {
		t.Fatalf("empty group = %d, want 400", response.StatusCode)
	}

	response, payload = callJSON(t, "GET", server.URL+"/api/v1/routing/groups", nil)
	if response.StatusCode != 200 {
		t.Fatalf("GET groups = %d %s", response.StatusCode, payload)
	}
	var list struct {
		Groups []map[string]any `json:"groups"`
	}
	if err := json.Unmarshal(payload, &list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(list.Groups) != 1 {
		t.Fatalf("groups = %v", list.Groups)
	}
	if _, ok := list.Groups[0]["plans"]; !ok {
		t.Fatalf("groups carry their plan usage: %v", list.Groups[0])
	}

	response, payload = callJSON(t, "PUT", server.URL+"/api/v1/routing/groups/"+created.Group.ID, map[string]any{
		"name": "work", "accounts": []string{"a"}, "allowMetered": true, "fallback": nil,
	})
	if response.StatusCode != 200 {
		t.Fatalf("PUT group = %d %s", response.StatusCode, payload)
	}
	response, _ = callJSON(t, "DELETE", server.URL+"/api/v1/routing/groups/"+created.Group.ID, nil)
	if response.StatusCode != 204 {
		t.Fatalf("DELETE group = %d, want 204", response.StatusCode)
	}
}

func TestRoutingPreviewRoute(t *testing.T) {
	f, server := newRoutesServer(t)
	now := time.Now()
	model := f.entry("alpha", "a", func(e *Entry) { e.IsFree = true })
	response, payload := callJSON(t, "POST", server.URL+"/api/v1/routing/preview", map[string]any{
		"profile": "smart",
		"prompt":  "hello",
	})
	if response.StatusCode != 200 {
		t.Fatalf("POST preview = %d %s", response.StatusCode, payload)
	}
	var preview struct {
		Group      *string           `json:"group"`
		Profile    Profile           `json:"profile"`
		Weights    Weights           `json:"weights"`
		Complexity *float64          `json:"complexity"`
		Task       map[string]any    `json:"task"`
		Need       *float64          `json:"need"`
		Candidates []map[string]any  `json:"candidates"`
		Selection  *PreviewSelection `json:"selection"`
	}
	if err := json.Unmarshal(payload, &preview); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if preview.Profile.ID != "smart" {
		t.Fatalf("profile = %+v", preview.Profile)
	}
	if len(preview.Candidates) == 0 {
		t.Fatalf("no candidates: %s", payload)
	}
	if preview.Candidates[0]["id"] != model.ID {
		t.Fatalf("candidate = %v", preview.Candidates[0])
	}
	if preview.Selection == nil || preview.Selection.Model != model.ID {
		t.Fatalf("selection = %+v", preview.Selection)
	}
	if preview.Task == nil || preview.Task["primary"] == nil {
		t.Fatalf("the preview classifies the prompt: %v", preview.Task)
	}
	// A candidate carries the routing fields the UI reads.
	for _, key := range []string{"id", "ref", "name", "isFree", "isPlan", "rejected", "score", "features", "factors", "tier"} {
		if _, ok := preview.Candidates[0][key]; !ok {
			t.Fatalf("candidate field %q missing: %v", key, preview.Candidates[0])
		}
	}
	// An explicit model that does not exist is a 404 with suggestions.
	response, payload = callJSON(t, "POST", server.URL+"/api/v1/routing/preview", map[string]any{"model": "a/alpah"})
	if response.StatusCode != 404 {
		t.Fatalf("unknown model = %d %s", response.StatusCode, payload)
	}
	var problem struct {
		Error struct {
			Code    string         `json:"code"`
			Details map[string]any `json:"details"`
		} `json:"error"`
	}
	_ = json.Unmarshal(payload, &problem)
	if problem.Error.Code != "MODEL_NOT_FOUND" {
		t.Fatalf("code = %q", problem.Error.Code)
	}
	if _, ok := problem.Error.Details["suggestions"]; !ok {
		t.Fatalf("suggestions missing: %s", payload)
	}
	_ = now
}

func TestRoutingSmoothingRoutes(t *testing.T) {
	_, server := newRoutesServer(t)
	response, payload := callJSON(t, "GET", server.URL+"/api/v1/routing/smoothing", nil)
	if response.StatusCode != 200 {
		t.Fatalf("GET smoothing = %d %s", response.StatusCode, payload)
	}
	var status struct {
		Enabled   bool `json:"enabled"`
		Active    bool `json:"active"`
		Readiness struct {
			Ready  bool `json:"ready"`
			Needed struct {
				Days       int     `json:"days"`
				ActiveDays int     `json:"activeDays"`
				Requests   int     `json:"requests"`
				Coverage   float64 `json:"coverage"`
			} `json:"needed"`
		} `json:"readiness"`
	}
	if err := json.Unmarshal(payload, &status); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if status.Enabled || status.Active || status.Readiness.Ready {
		t.Fatalf("a new account sees nothing on: %s", payload)
	}
	needed := status.Readiness.Needed
	if needed.Days != 7 || needed.ActiveDays != 5 || needed.Requests != 100 || needed.Coverage != 0.5 {
		t.Fatalf("needed = %+v", needed)
	}
	response, payload = callJSON(t, "PUT", server.URL+"/api/v1/routing/smoothing", map[string]any{"enabled": true})
	if response.StatusCode != 409 {
		t.Fatalf("PUT smoothing on = %d %s", response.StatusCode, payload)
	}
	var problem struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal(payload, &problem)
	if problem.Error.Code != "NOT_ENOUGH_DATA" {
		t.Fatalf("code = %q", problem.Error.Code)
	}
	response, payload = callJSON(t, "PUT", server.URL+"/api/v1/routing/smoothing", map[string]any{"enabled": false})
	if response.StatusCode != 200 {
		t.Fatalf("PUT smoothing off = %d %s", response.StatusCode, payload)
	}
	var off struct {
		Enabled bool `json:"enabled"`
	}
	_ = json.Unmarshal(payload, &off)
	if off.Enabled {
		t.Fatalf("enabled = true after switching off")
	}
}

func TestRoutingLiveAndLoadRoutes(t *testing.T) {
	f, server := newRoutesServer(t)
	f.account("a", "u", "openai", 3, "paid")
	entry := f.entry("m", "a", func(e *Entry) { e.IsFlatRate = true; e.DefinitionID = "openai" })
	if _, err := f.db.W.Exec(`INSERT INTO router_keys (id, user_id, name, prefix, key_hash, mode) VALUES ('k', 'u', 'mine', 'p', 'h', 'both')`); err != nil {
		t.Fatalf("insert key: %v", err)
	}

	// A first read is a snapshot of what is in flight.
	f.svc.LiveStarted("u", "req-1", "mine", "coding")
	response, payload := callJSON(t, "GET", server.URL+"/api/v1/routing/live?since=0", nil)
	if response.StatusCode != 200 {
		t.Fatalf("GET live = %d %s", response.StatusCode, payload)
	}
	if response.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("cache control = %q", response.Header.Get("Cache-Control"))
	}
	var snapshot struct {
		Seq      int64 `json:"seq"`
		Snapshot bool  `json:"snapshot"`
		InFlight []struct {
			ID       string `json:"id"`
			Key      string `json:"key"`
			Task     string `json:"task"`
			Attempts []any  `json:"attempts"`
		} `json:"inFlight"`
		Events []map[string]any `json:"events"`
		Cards  *struct {
			Keys      []map[string]any `json:"keys"`
			Providers []map[string]any `json:"providers"`
		} `json:"cards"`
	}
	if err := json.Unmarshal(payload, &snapshot); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !snapshot.Snapshot || snapshot.Seq == 0 {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	if len(snapshot.InFlight) != 1 || snapshot.InFlight[0].ID != "req-1" || snapshot.InFlight[0].Task != "coding" {
		t.Fatalf("in flight = %+v", snapshot.InFlight)
	}
	if snapshot.Cards == nil || len(snapshot.Cards.Providers) != 1 || len(snapshot.Cards.Keys) != 1 {
		t.Fatalf("cards = %+v", snapshot.Cards)
	}

	// Later reads return the events after the sequence number.
	seq := snapshot.Seq
	f.svc.LiveAttempt("u", "req-1", "a", entry.ID)
	f.svc.LiveFinished("u", "req-1", true, entry.ID)
	response, payload = callJSON(t, "GET", server.URL+"/api/v1/routing/live?since="+formatRef(seq), nil)
	if response.StatusCode != 200 {
		t.Fatalf("GET live events = %d %s", response.StatusCode, payload)
	}
	var events struct {
		Seq      int64            `json:"seq"`
		Snapshot bool             `json:"snapshot"`
		Events   []map[string]any `json:"events"`
	}
	if err := json.Unmarshal(payload, &events); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if events.Snapshot || len(events.Events) != 2 {
		t.Fatalf("events = %+v", events)
	}
	if events.Events[0]["type"] != "attempt" || events.Events[1]["type"] != "done" {
		t.Fatalf("events = %+v", events.Events)
	}
	if events.Events[1]["success"] != true || events.Events[1]["model"] != entry.ID {
		t.Fatalf("done event = %+v", events.Events[1])
	}

	// The load route reports what the router is running.
	release := f.svc.Lease(entry)
	defer release()
	response, payload = callJSON(t, "GET", server.URL+"/api/v1/routing/load", nil)
	if response.StatusCode != 200 {
		t.Fatalf("GET load = %d %s", response.StatusCode, payload)
	}
	var load LoadSnapshot
	if err := json.Unmarshal(payload, &load); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if load.Models[formatRef(entry.Ref)] != 1 || load.Accounts["a"] != 1 {
		t.Fatalf("load = %+v", load)
	}
}
