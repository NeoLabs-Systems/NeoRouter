package oauth

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// openRouterUpstream is a fake OpenRouter token endpoint.
func openRouterUpstream(t *testing.T, status int, body string, onRequest func(body map[string]string)) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var payload map[string]string
		_ = json.Unmarshal(raw, &payload)
		if onRequest != nil {
			onRequest(payload)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(server.Close)
	return server
}

// startOpenRouter runs POST /api/v1/oauth/openrouter/start and returns the URL.
func (e *oauthEnv) startOpenRouter(token, returnTo string) *httptest.ResponseRecorder {
	return e.postJSON(token, "/api/v1/oauth/openrouter/start", `{"returnTo":"`+returnTo+`"}`)
}

// callbackState pulls the one-time state out of the OpenRouter URL.
func callbackState(t *testing.T, location string) string {
	t.Helper()
	parsed, err := url.Parse(location)
	if err != nil {
		t.Fatalf("parse %q: %v", location, err)
	}
	callback := parsed.Query().Get("callback_url")
	if callback == "" {
		t.Fatalf("%q carries no callback_url", location)
	}
	parts := strings.Split(callback, "/")
	return parts[len(parts)-1]
}

// TestOpenRouterConnectionStoresTheKey covers the whole PKCE flow end to end.
func TestOpenRouterConnectionStoresTheKey(t *testing.T) {
	env := newOAuthEnv(t)
	var exchange map[string]string
	upstream := openRouterUpstream(t, http.StatusOK, `{"key":"openrouter-test-key"}`, func(body map[string]string) {
		exchange = body
	})
	env.svc.openRouterExchange = upstream.URL + "/api/v1/auth/keys"

	started := env.startOpenRouter("owner-token", "http://127.0.0.1:4600/app/?tab=providers")
	if started.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", started.Code, started.Body)
	}
	var start startResponse
	decode(t, started, &start)
	authURL, err := url.Parse(start.URL)
	if err != nil {
		t.Fatalf("parse %q: %v", start.URL, err)
	}
	if !strings.HasPrefix(start.URL, "https://openrouter.ai/auth?") {
		t.Errorf("url = %q", start.URL)
	}
	if authURL.Query().Get("code_challenge_method") != "S256" {
		t.Errorf("code_challenge_method = %q", authURL.Query().Get("code_challenge_method"))
	}
	if challenge := authURL.Query().Get("code_challenge"); len(challenge) != 43 {
		t.Errorf("code_challenge = %q, want a S256 challenge", challenge)
	}
	if !strings.HasPrefix(authURL.Query().Get("callback_url"), "http://example.com/api/v1/oauth/openrouter/callback/") {
		t.Errorf("callback_url = %q", authURL.Query().Get("callback_url"))
	}
	state := callbackState(t, start.URL)

	finished := env.get("", "/api/v1/oauth/openrouter/callback/"+state+"?code=one-time-code")
	if finished.Code != http.StatusFound {
		t.Fatalf("status = %d, body %s", finished.Code, finished.Body)
	}
	back, err := url.Parse(finished.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse location: %v", err)
	}
	if back.Query().Get("oauth") != "openrouter" || back.Query().Get("oauth_status") != "connected" {
		t.Errorf("location = %q", finished.Header().Get("Location"))
	}
	if back.Query().Get("tab") != "providers" {
		t.Errorf("location lost the dashboard state: %q", finished.Header().Get("Location"))
	}

	// The exchange sent the code with the flow's verifier, and the key became an
	// account of the owner's.
	if exchange["code"] != "one-time-code" || exchange["code_challenge_method"] != "S256" {
		t.Errorf("exchange body = %v", exchange)
	}
	if exchange["code_verifier"] == "" || len(exchange["code_verifier"]) < 43 {
		t.Errorf("code_verifier = %q", exchange["code_verifier"])
	}
	if len(env.accounts.accounts) != 1 {
		t.Fatalf("accounts = %d", len(env.accounts.accounts))
	}
	account := env.accounts.accounts[0]
	if account.DefinitionID != "openrouter" || account.Secret != "openrouter-test-key" || account.BillingMode != "paid" {
		t.Errorf("account = %+v", account)
	}
	if env.accounts.owners[0] != "user-owner/user-owner" {
		t.Errorf("owner = %q", env.accounts.owners[0])
	}
	if len(env.catalog.synced) != 1 || env.catalog.synced[0] != "acct-1" {
		t.Errorf("synced = %v", env.catalog.synced)
	}

	// A browser can reach the callback twice (link prefetch, refresh, back). The
	// code is exchanged once and the repeat replays the same redirect, not an
	// expired-link error.
	again := env.get("", "/api/v1/oauth/openrouter/callback/"+state+"?code=one-time-code")
	if again.Code != http.StatusFound || again.Header().Get("Location") != finished.Header().Get("Location") {
		t.Errorf("repeat callback = %d %s, want the same redirect %s", again.Code, again.Body, finished.Header().Get("Location"))
	}
	if len(env.accounts.accounts) != 1 {
		t.Errorf("the code was exchanged again: accounts = %d", len(env.accounts.accounts))
	}

	// Once the flow's window passes, the link really is expired.
	env.now = env.now.Add(openRouterFlowTTL + time.Minute)
	expired := env.get("", "/api/v1/oauth/openrouter/callback/"+state+"?code=one-time-code")
	if expired.Code != http.StatusBadRequest || !strings.Contains(expired.Body.String(), `"code":"OAUTH_EXPIRED"`) {
		t.Errorf("status = %d, body %s", expired.Code, expired.Body)
	}
}

// TestOpenRouterCallbackWithoutCodeIsCancelled covers the browser saying no.
func TestOpenRouterCallbackWithoutCodeIsCancelled(t *testing.T) {
	env := newOAuthEnv(t)
	started := env.startOpenRouter("owner-token", "http://127.0.0.1:4600/app/")
	var start startResponse
	decode(t, started, &start)
	state := callbackState(t, start.URL)

	finished := env.get("", "/api/v1/oauth/openrouter/callback/"+state)
	if finished.Code != http.StatusFound {
		t.Fatalf("status = %d, body %s", finished.Code, finished.Body)
	}
	back, err := url.Parse(finished.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse location: %v", err)
	}
	if back.Query().Get("oauth_error") != "cancelled" {
		t.Errorf("location = %q", finished.Header().Get("Location"))
	}
	if len(env.accounts.accounts) != 0 {
		t.Errorf("a cancelled flow stored an account")
	}
}

// TestOpenRouterFailuresReturnToTheDashboard covers the failure paths of
// openrouter_oauth.complete.
func TestOpenRouterFailuresReturnToTheDashboard(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		body    string
		wantErr string
	}{
		{"provider message", http.StatusUnauthorized, `{"error":{"message":"Invalid code"}}`, "Invalid code"},
		{"provider status", http.StatusInternalServerError, `not json`, "OpenRouter returned HTTP 500"},
		{"no key in the body", http.StatusOK, `{"ok":true}`, "OpenRouter returned HTTP 200"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			env := newOAuthEnv(t)
			upstream := openRouterUpstream(t, testCase.status, testCase.body, nil)
			env.svc.openRouterExchange = upstream.URL + "/api/v1/auth/keys"

			started := env.startOpenRouter("owner-token", "http://127.0.0.1:4600/app/")
			var start startResponse
			decode(t, started, &start)
			state := callbackState(t, start.URL)

			finished := env.get("", "/api/v1/oauth/openrouter/callback/"+state+"?code=one-time-code")
			if finished.Code != http.StatusFound {
				t.Fatalf("status = %d, body %s", finished.Code, finished.Body)
			}
			back, err := url.Parse(finished.Header().Get("Location"))
			if err != nil {
				t.Fatalf("parse location: %v", err)
			}
			if got := back.Query().Get("oauth_error"); got != testCase.wantErr {
				t.Errorf("oauth_error = %q, want %q", got, testCase.wantErr)
			}
			if len(env.accounts.accounts) != 0 {
				t.Errorf("a failed flow stored an account")
			}
		})
	}
}

// TestOpenRouterStartChecksTheReturnURL covers the return address checks and
// the route's validation.
func TestOpenRouterStartChecksTheReturnURL(t *testing.T) {
	env := newOAuthEnv(t)
	for _, returnTo := range []string{
		"https://elsewhere.example/callback",
		"myapp://callback",
		"/relative",
	} {
		response := env.startOpenRouter("owner-token", returnTo)
		if response.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d", returnTo, response.Code)
			continue
		}
		body := response.Body.String()
		if !strings.Contains(body, `"code":"INVALID_RETURN_URL"`) && !strings.Contains(body, `"code":"VALIDATION_ERROR"`) {
			t.Errorf("%s: body = %s", returnTo, body)
		}
		if strings.Contains(body, `"code":"INVALID_RETURN_URL"`) && !strings.Contains(body, "The return URL must point to this NeoRouter instance.") {
			t.Errorf("%s: message = %s", returnTo, body)
		}
	}
	// The dashboard's own host is accepted, and so is another loopback port.
	for _, returnTo := range []string{"http://example.com/app/", "http://127.0.0.1:5173/app/", "http://localhost:4600/app/"} {
		response := env.startOpenRouter("owner-token", returnTo)
		if response.Code != http.StatusOK {
			t.Errorf("%s: status = %d, body %s", returnTo, response.Code, response.Body)
		}
	}

	// the messages for a missing, malformed or too long returnTo.
	cases := []struct{ body, message string }{
		{`{}`, "returnTo: Required"},
		{`{"returnTo":"nope"}`, "returnTo: Invalid url"},
		{`{"returnTo":"https://example.com/` + strings.Repeat("a", maxReturnTo) + `"}`, "returnTo: String must contain at most 2048 character(s)"},
	}
	for _, testCase := range cases {
		response := env.postJSON("owner-token", "/api/v1/oauth/openrouter/start", testCase.body)
		if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), testCase.message) {
			t.Errorf("%s: %d %s", testCase.body, response.Code, response.Body)
		}
	}
}

// TestOpenRouterStartNeedsTheWholeSpace covers createdBy following the space.
func TestOpenRouterStartNeedsTheWholeSpace(t *testing.T) {
	env := newOAuthEnv(t)
	original := spaces["user-team"]
	spaces["user-team"] = Space{
		OwnerID: "user-owner", ActorID: "user-team", IsOwner: false,
		Permissions: map[string]bool{"keys.create": true, "providers.manage": true},
	}
	t.Cleanup(func() { spaces["user-team"] = original })
	response := env.startOpenRouter("team-token", "http://example.com/app/")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", response.Code, response.Body)
	}
	started := env.svc.flowsSnapshot()
	if len(started) != 1 {
		t.Fatalf("flows = %d", len(started))
	}
	for _, flow := range started {
		if flow.userID != "user-owner" || flow.createdBy != "user-team" {
			t.Errorf("flow owner = %q, created by %q", flow.userID, flow.createdBy)
		}
	}
}

// flowsSnapshot is a copy of the pending connection flows.
func (s *Service) flowsSnapshot() []openRouterFlow {
	s.mu.Lock()
	defer s.mu.Unlock()
	flows := make([]openRouterFlow, 0, len(s.flows))
	for _, flow := range s.flows {
		flows = append(flows, *flow)
	}
	return flows
}

// TestSafeReturnTo covers the address check on its own.
func TestSafeReturnTo(t *testing.T) {
	cases := []struct {
		returnTo    string
		requestHost string
		want        bool
	}{
		{"http://127.0.0.1:4600/app/", "example.com", true},
		{"http://localhost:4600/app/", "example.com", true},
		{"http://example.com/app/", "example.com", true},
		{"http://example.com:4600/app/", "example.com", false},
		{"https://elsewhere.example/app/", "example.com", false},
		{"javascript:alert(1)", "example.com", false},
		{"file:///etc/passwd", "example.com", false},
		{"", "example.com", false},
	}
	for _, testCase := range cases {
		_, ok := safeReturnTo(testCase.returnTo, testCase.requestHost)
		if ok != testCase.want {
			t.Errorf("safeReturnTo(%q, %q) = %v, want %v", testCase.returnTo, testCase.requestHost, ok, testCase.want)
		}
	}
}
