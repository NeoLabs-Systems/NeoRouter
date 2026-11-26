package gateway

// The playground chat and the API reference.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/NeoLabs-Systems/neorouter/internal/routerkeys"
)

// playgroundFixtures wires the space the playground route reads.
func playgroundFixtures(f *fixture) {
	f.service.opts.Space = func(ctx context.Context) (Space, bool) {
		return Space{OwnerID: f.userID}, true
	}
}

func TestPlaygroundFreeToggleKeepsAPaidCapableKeyOnFreeModels(t *testing.T) {
	f := newFixture(t, nil)
	f.mockProvider("freebie", "free_tier", "ok-two")
	f.mockProvider("paidmock", "paid", "ok-two")
	for index := range f.catalog.entries {
		entry := f.catalog.entries[index]
		if entry.ProviderSlug == "paidmock" {
			entry.IsFree = false
			entry.BillingMode = "paid"
		}
	}
	playgroundFixtures(f)
	mode := routerkeys.ModeBoth
	_, public := f.key(func(input *routerkeys.Input) {
		input.Mode = routerkeys.Field[string]{Set: true, Value: &mode}
	})
	chat := func(freeOnly bool) *httptest.ResponseRecorder {
		body := newObjOf(
			"keyId", public.ID,
			"model", "paidmock/ok-two",
			"messages", []any{newObjOf("role", "user", "content", "hi")},
		)
		if freeOnly {
			body.Set("freeOnly", true)
		}
		return f.do(http.MethodPost, "/api/v1/playground/chat", "", body, nil)
	}
	paid := chat(false)
	if paid.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", paid.Code, paid.Body.String())
	}
	if model := paid.Header().Get("X-NeoRouter-Model"); model != "paidmock/ok-two" {
		t.Errorf("model = %q", model)
	}
	free := chat(true)
	if free.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", free.Code, free.Body.String())
	}
	if model := free.Header().Get("X-NeoRouter-Model"); model != "freebie/ok-two" {
		t.Errorf("model = %q", model)
	}
}

func TestPlaygroundRejectsABadBody(t *testing.T) {
	f := newFixture(t, nil)
	f.mockProvider("mock", "free_tier", "ok-alpha")
	playgroundFixtures(f)
	_, public := f.key(nil)
	missing := f.do(http.MethodPost, "/api/v1/playground/chat", "", newObjOf("model", "auto"), nil)
	if missing.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", missing.Code)
	}
	body := decode(t, missing)
	errorBody := getObj(body, "error")
	if code := getString(errorBody, "code"); code != "VALIDATION_ERROR" {
		t.Errorf("code = %q", code)
	}
	if message := getString(errorBody, "message"); message != "keyId: Required" {
		t.Errorf("message = %q", message)
	}
	badRole := f.do(http.MethodPost, "/api/v1/playground/chat", "", newObjOf(
		"keyId", public.ID,
		"messages", []any{newObjOf("role", "robot", "content", "hi")},
	), nil)
	if badRole.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", badRole.Code)
	}
	if message := getString(getObj(decode(t, badRole), "error"), "message"); message != "messages.0.role: Invalid enum value. Expected 'system' | 'user' | 'assistant', received 'robot'" {
		t.Errorf("message = %q", message)
	}
}

func TestPlaygroundRefusesARevokedKey(t *testing.T) {
	f := newFixture(t, nil)
	f.mockProvider("mock", "free_tier", "ok-alpha")
	playgroundFixtures(f)
	_, public := f.key(nil)
	if _, err := f.keys.Revoke(f.userID, public.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	response := f.do(http.MethodPost, "/api/v1/playground/chat", "", newObjOf(
		"keyId", public.ID,
		"messages", []any{newObjOf("role", "user", "content", "hi")},
	), nil)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", response.Code)
	}
	if code := errorCode(t, response); code != "KEY_REVOKED" {
		t.Errorf("code = %q", code)
	}
}

func TestAPIDocsServesTheGatewayReference(t *testing.T) {
	f := newFixture(t, nil)
	request, err := http.NewRequest(http.MethodGet, "/api/docs", nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	request.Host = "router.example.test"
	recorder := httptest.NewRecorder()
	f.mux.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
	if contentType := recorder.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "text/markdown") {
		t.Errorf("content-type = %q", contentType)
	}
	text := recorder.Body.String()
	if !strings.HasPrefix(text, "# NeoRouter API") {
		t.Errorf("start = %q", text[:minInt(40, len(text))])
	}
	for _, want := range []string{
		"http://router.example.test/v1/chat/completions",
		"ws://router.example.test/v1/realtime",
		"## Authentication",
		"## Choosing a model",
		"## Errors",
		"## Getting a key from an app (OAuth)",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the document is missing %q", want)
		}
	}
	// The old address keeps working.
	redirect := f.do(http.MethodGet, "/api", "", nil, nil)
	if redirect.Code != http.StatusMovedPermanently {
		t.Errorf("status = %d", redirect.Code)
	}
	if location := redirect.Header().Get("Location"); location != "/api/docs" {
		t.Errorf("location = %q", location)
	}
}

func TestPublicURLIsUsedForTheDocument(t *testing.T) {
	f := newFixture(t, map[string]string{"NEOROUTER_PUBLIC_URL": "https://router.example/"})
	text := f.service.apiDocs(f.service.publicOrigin(&http.Request{Host: "ignored.test"}))
	if !strings.Contains(text, "https://router.example/v1") {
		t.Error("the public URL is not used")
	}
	if !strings.Contains(text, "wss://router.example/v1/realtime") {
		t.Error("the websocket URL is not derived from the public URL")
	}
}

func TestCapabilityRegistryList(t *testing.T) {
	list := List()
	ids := make([]string, 0, len(list))
	for _, capability := range list {
		ids = append(ids, capability.ID)
	}
	want := []string{"chat", "embedding", "image", "speech", "transcription", "moderation", "video", "search", "weather", "live"}
	if len(ids) != len(want) {
		t.Fatalf("ids = %v", ids)
	}
	for index := range want {
		if ids[index] != want[index] {
			t.Fatalf("ids = %v", ids)
		}
	}
	if method := MethodFor("embedding"); method != "embed" {
		t.Errorf("embedding method = %q", method)
	}
	if method := MethodFor("chat"); method != "chat" {
		t.Errorf("chat method = %q", method)
	}
	if method := MethodFor("moderation"); method != "" {
		t.Errorf("moderation method = %q", method)
	}
	if !IsKind("weather") || IsKind("nope") {
		t.Error("IsKind is wrong")
	}
}

func TestUnknownGatewayPathsAreNotTheGateways(t *testing.T) {
	f := newFixture(t, nil)
	recorder := httptest.NewRecorder()
	f.mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v1/nope", nil))
	// The app's own handler answers unknown paths (JSON NOT_FOUND for an API
	// client), so the gateway must not register a catch-all.
	if recorder.Code != http.StatusNotFound {
		t.Errorf("status = %d", recorder.Code)
	}
	if strings.Contains(recorder.Body.String(), `"error"`) {
		t.Errorf("the gateway answered with its own error: %q", recorder.Body.String())
	}
}
