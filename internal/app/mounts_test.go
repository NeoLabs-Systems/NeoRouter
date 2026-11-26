package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// postJSON sends a management request with an optional bearer token.
func postJSON(t *testing.T, a *App, target, body, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	a.Handler.ServeHTTP(rec, req)
	return rec
}

// TestManagementRoutesRequireSession pins the dashboard-only endpoints behind
// the session middleware. They are gateway/oauth handlers but management
// routes: mounted at the root they would run with no caller in the context and
// reject a signed-in user ("Sign in to use the playground", OAUTH 401).
func TestManagementRoutesRequireSession(t *testing.T) {
	a := openApp(t)

	// Without a session the auth middleware answers, not the handler.
	for _, target := range []string{"/api/v1/playground/chat", "/api/v1/oauth/openrouter/start"} {
		rec := postJSON(t, a, target, `{}`, "")
		if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), `"code":"AUTHENTICATION_REQUIRED"`) {
			t.Fatalf("POST %s without session = %d %s, want the middleware's 401", target, rec.Code, rec.Body)
		}
	}

	// Register the bootstrap admin so the requests carry a real session.
	reg := postJSON(t, a, "/api/v1/auth/register",
		`{"username":"mounts","email":"mounts@example.com","password":"correct horse battery staple"}`, "")
	if reg.Code != http.StatusCreated {
		t.Fatalf("register = %d %s", reg.Code, reg.Body)
	}
	var session struct {
		Session struct {
			Token string `json:"token"`
		} `json:"session"`
	}
	if err := json.Unmarshal(reg.Body.Bytes(), &session); err != nil {
		t.Fatalf("decode register: %v", err)
	}
	token := session.Session.Token

	// With a session the playground reaches its handler: an unknown key is a
	// 404 from the route, not a sign-in error.
	pg := postJSON(t, a, "/api/v1/playground/chat",
		`{"keyId":"00000000-0000-0000-0000-000000000000","model":"auto","messages":[{"role":"user","content":"hi"}]}`, token)
	if pg.Code != http.StatusNotFound || !strings.Contains(pg.Body.String(), `"code":"NOT_FOUND"`) {
		t.Fatalf("playground with session = %d %s, want the route's 404", pg.Code, pg.Body)
	}

	// With a session the OpenRouter start issues a sign-in link.
	start := postJSON(t, a, "/api/v1/oauth/openrouter/start",
		`{"returnTo":"http://127.0.0.1:4600/app/"}`, token)
	if start.Code != http.StatusOK || !strings.Contains(start.Body.String(), `"url"`) {
		t.Fatalf("openrouter start with session = %d %s", start.Code, start.Body)
	}
}
