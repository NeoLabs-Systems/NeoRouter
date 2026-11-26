package app

// End-to-end validation of the provider sign-ins with no real provider and no
// human approval: the real app (internal/app, every domain wired as in
// production) is served through its own handler while the upstreams the flows
// talk to are fakes, chosen through the environment and HTTP options the code
// already offers (NEOROUTER_CODEX_ISSUER, NEOROUTER_CODEX_BASE_URL,
// NEOROUTER_NOUS_PORTAL_URL, NEOROUTER_NOUS_BASE_URL,
// NEOROUTER_OPENROUTER_BASE_URL, app.Options.HTTPClient).
//
// Each flow is driven start → poll/callback → done, and the result is checked in
// the database: the account row, its decrypted secret, the validation verdict
// and the models the catalog synced.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/NeoLabs-Systems/neorouter/internal/providers"
)

// ---- shared helpers ---------------------------------------------------------

// writeJSON answers with a JSON body.
func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// signInJWT is a token shaped like the provider's: three base64url segments with
// the claims the flows read in the payload.
func signInJWT(claims map[string]any) string {
	payload, err := json.Marshal(claims)
	if err != nil {
		panic(err)
	}
	encode := base64.RawURLEncoding.EncodeToString
	return encode([]byte(`{"alg":"RS256","typ":"JWT"}`)) + "." + encode(payload) + ".c2lnbmF0dXJl"
}

// decodeJSONBody reads a JSON answer and fails the test on a non-JSON body.
func decodeJSONBody(t *testing.T, recorder *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var decoded map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decode %d %s: %v", recorder.Code, recorder.Body.String(), err)
	}
	return decoded
}

// signInApp is the running server plus the session the flows are driven with.
type signInApp struct {
	*App
	token string
}

// openSignInApp starts the real app against an environment and HTTP client.
func openSignInApp(t *testing.T, env map[string]string, client *http.Client) *signInApp {
	t.Helper()
	cfg := testConfig(t)
	a, err := Open(context.Background(), Options{
		StaticConfig: &cfg,
		Version:      "9.9.9",
		Getenv:       func(name string) string { return env[name] },
		HTTPClient:   client,
	})
	if err != nil {
		t.Fatalf("app.Open: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })
	return &signInApp{App: a}
}

// call sends one request with the harness session, when there is one.
func (h *signInApp) call(t *testing.T, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	return h.callAs(t, method, target, body, h.token)
}

// callAs sends one request with an explicit session; "" is an anonymous caller,
// which is what a browser following a redirect is.
func (h *signInApp) callAs(t *testing.T, method, target, body, token string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	recorder := httptest.NewRecorder()
	h.Handler.ServeHTTP(recorder, request)
	return recorder
}

// register makes the bootstrap administrator and keeps its session.
func (h *signInApp) register(t *testing.T) {
	t.Helper()
	response := h.call(t, http.MethodPost, "/api/v1/auth/register",
		`{"username":"signin","email":"signin@example.test","password":"correct horse battery staple"}`)
	if response.Code != http.StatusCreated {
		t.Fatalf("register = %d %s", response.Code, response.Body.String())
	}
	session, _ := decodeJSONBody(t, response)["session"].(map[string]any)
	token, _ := session["token"].(string)
	if token == "" {
		t.Fatalf("register carried no session: %s", response.Body.String())
	}
	h.token = token
}

// accountRow is what the flows are judged by: the stored row and its models.
type accountRow struct {
	ID          string
	Definition  string
	BillingMode string
	Status      string
	Secret      string
	Models      int
}

// storedAccount reads one definition's account out of the database.
func (h *signInApp) storedAccount(t *testing.T, definitionID string) accountRow {
	t.Helper()
	row := accountRow{Definition: definitionID}
	err := h.DB.R.QueryRowContext(context.Background(),
		`SELECT id, billing_mode, status FROM provider_accounts WHERE definition_id = ?`, definitionID).
		Scan(&row.ID, &row.BillingMode, &row.Status)
	if err != nil {
		t.Fatalf("no %s account: %v", definitionID, err)
	}
	runtime, err := h.svc.Accounts.Runtime(context.Background(), row.ID)
	if err != nil || runtime == nil {
		t.Fatalf("read %s secret: %v", definitionID, err)
	}
	row.Secret = runtime.Account.Secret
	if err := h.DB.R.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM models WHERE account_id = ?`, row.ID).Scan(&row.Models); err != nil {
		t.Fatalf("count %s models: %v", definitionID, err)
	}
	return row
}

// requireHealthy fails unless the account was created, validated and synced.
func (row accountRow) requireHealthy(t *testing.T, wantBilling string) {
	t.Helper()
	if row.Status != "healthy" {
		t.Fatalf("%s account status = %q, want healthy", row.Definition, row.Status)
	}
	if row.BillingMode != wantBilling {
		t.Fatalf("%s billing mode = %q, want %q", row.Definition, row.BillingMode, wantBilling)
	}
	if row.Secret == "" {
		t.Fatalf("%s account has no stored secret", row.Definition)
	}
	if row.Models == 0 {
		t.Fatalf("%s account synced no models", row.Definition)
	}
}

// ---- fake ChatGPT (Codex) upstream -----------------------------------------

// codexUpstream is the fake OpenAI auth host and ChatGPT backend.
type codexUpstream struct {
	mu        sync.Mutex
	polls     int
	refreshes int
	models    int
	server    *httptest.Server
}

func newCodexUpstream(t *testing.T) *codexUpstream {
	t.Helper()
	fake := &codexUpstream{}
	fake.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		switch r.URL.Path {
		case "/api/accounts/deviceauth/usercode":
			writeJSON(w, http.StatusOK, map[string]any{
				"device_auth_id": "dev-1", "user_code": "ABCD-1234", "interval": 1,
			})
		case "/api/accounts/deviceauth/token":
			fake.polls++
			if fake.polls < 2 {
				writeJSON(w, http.StatusForbidden, map[string]any{"error": "authorization_pending"})
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"authorization_code": "code-1", "code_verifier": "verifier-1"})
		case "/oauth/token":
			if err := r.ParseForm(); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": "bad_form"})
				return
			}
			if r.PostForm.Get("grant_type") == "authorization_code" {
				if r.PostForm.Get("code") != "code-1" || r.PostForm.Get("code_verifier") != "verifier-1" ||
					r.PostForm.Get("redirect_uri") != fake.server.URL+"/deviceauth/callback" {
					writeJSON(w, http.StatusBadRequest, map[string]any{"error": "bad_exchange"})
					return
				}
				writeJSON(w, http.StatusOK, map[string]any{
					"access_token":  "access-one",
					"refresh_token": "refresh-one",
					"expires_in":    3600,
					"id_token": signInJWT(map[string]any{
						"https://api.openai.com/auth": map[string]any{
							"chatgpt_account_id": "acct-123",
							"chatgpt_plan_type":  "plus",
						},
					}),
				})
				return
			}
			if r.PostForm.Get("grant_type") != "refresh_token" || r.PostForm.Get("refresh_token") != "refresh-one" {
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_grant"})
				return
			}
			fake.refreshes++
			writeJSON(w, http.StatusOK, map[string]any{
				"access_token":  "access-two",
				"refresh_token": "refresh-two",
				"expires_in":    3600,
				"id_token": signInJWT(map[string]any{
					"https://api.openai.com/auth": map[string]any{
						"chatgpt_account_id": "acct-123",
						"chatgpt_plan_type":  "plus",
					},
				}),
			})
		case "/backend-api/codex/models":
			if r.Header.Get("Authorization") != "Bearer access-one" {
				writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "no bearer"})
				return
			}
			fake.models++
			writeJSON(w, http.StatusOK, map[string]any{"models": []any{
				map[string]any{"slug": "gpt-5.1-codex", "display_name": "GPT-5.1 Codex", "visibility": "list", "context_window": 272000},
				map[string]any{"slug": "gpt-5.1-codex-mini", "display_name": "GPT-5.1 Codex Mini", "visibility": "list", "context_window": 272000},
			}})
		default:
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "unexpected path " + r.URL.Path})
		}
	}))
	t.Cleanup(fake.server.Close)
	return fake
}

// ---- fake Nous Portal upstream ---------------------------------------------

// nousUpstream is the fake Portal: device flow, account endpoint and the
// inference API under /v1.
type nousUpstream struct {
	mu        sync.Mutex
	polls     int
	refreshes int
	models    int
	// slowDown makes the Portal ask for a longer interval on the first poll.
	slowDown bool
	server   *httptest.Server
}

// nousToken is one token answer of the fake Portal; the access token is a
// real-shaped JWT so the claims of the account are read from it.
func nousToken(suffix string, expiresIn int) map[string]any {
	return map[string]any{
		"access_token": signInJWT(map[string]any{
			"sub": "user-1", "email": "portal@example.test", "account_tier": "paid",
		}) + "-" + suffix,
		"refresh_token": "refresh-" + suffix,
		"expires_in":    expiresIn,
		"scope":         "inference:invoke",
	}
}

func newNousUpstream(t *testing.T) *nousUpstream {
	t.Helper()
	fake := &nousUpstream{}
	fake.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		switch r.URL.Path {
		case "/api/oauth/device/code":
			if err := r.ParseForm(); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": "bad_form"})
				return
			}
			if r.PostForm.Get("client_id") != providers.NousClientID || r.PostForm.Get("scope") != providers.NousScope {
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_client"})
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"device_code": "dc-1", "user_code": "WXYZ-9876", "expires_in": 600, "interval": 1,
				"verification_uri":          "https://portal.test/manage-subscription",
				"verification_uri_complete": "https://portal.test/manage-subscription?user_code=WXYZ-9876",
			})
		case "/api/oauth/token":
			if err := r.ParseForm(); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": "bad_form"})
				return
			}
			switch r.PostForm.Get("grant_type") {
			case "urn:ietf:params:oauth:grant-type:device_code":
				fake.polls++
				approved := 2
				if fake.slowDown {
					approved = 3
					if fake.polls == 1 {
						writeJSON(w, http.StatusBadRequest, map[string]any{"error": "slow_down"})
						return
					}
				}
				if fake.polls < approved {
					writeJSON(w, http.StatusBadRequest, map[string]any{"error": "authorization_pending"})
					return
				}
				// A short-lived token: the sync after the sign-in refreshes it.
				writeJSON(w, http.StatusOK, nousToken("one", 60))
			case "refresh_token":
				if header := r.Header.Get("x-nous-refresh-token"); header != "refresh-one" {
					writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_grant"})
					return
				}
				fake.refreshes++
				writeJSON(w, http.StatusOK, nousToken("two", 3600))
			default:
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": "unsupported_grant_type"})
			}
		case "/api/oauth/account":
			writeJSON(w, http.StatusOK, map[string]any{
				"paid_service_access": map[string]any{"total_usable_credits": 12.5},
			})
		case "/v1/models":
			if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
				writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "no bearer"})
				return
			}
			fake.models++
			writeJSON(w, http.StatusOK, map[string]any{"data": []any{
				map[string]any{"id": "hermes-5", "name": "Hermes 5"},
			}})
		case "/v1/chat/completions":
			writeJSON(w, http.StatusOK, map[string]any{
				"id": "c1", "object": "chat.completion", "model": "hermes-5",
				"choices": []any{map[string]any{"index": 0, "message": map[string]any{"role": "assistant", "content": "Hello from Nous"}, "finish_reason": "stop"}},
				"usage":   map[string]any{"prompt_tokens": 3, "completion_tokens": 3, "total_tokens": 6},
			})
		default:
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "unexpected path " + r.URL.Path})
		}
	}))
	t.Cleanup(fake.server.Close)
	return fake
}

// ---- fake OpenRouter upstream ----------------------------------------------

// openRouterUpstream is the fake OpenRouter: the PKCE token exchange and the API
// the new connection is validated and synced against.
type openRouterUpstream struct {
	mu      sync.Mutex
	exhange map[string]string
	server  *httptest.Server
}

// openRouterKey is the fake key the exchange hands out.
const openRouterKey = "sk-or-v1-signin-e2e"

func newOpenRouterUpstream(t *testing.T) *openRouterUpstream {
	t.Helper()
	fake := &openRouterUpstream{}
	fake.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		switch r.URL.Path {
		case "/api/v1/auth/keys":
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			fake.exhange = body
			if body["code"] == "" || body["code_verifier"] == "" || body["code_challenge_method"] != "S256" {
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": map[string]any{"message": "bad exchange"}})
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"key": openRouterKey})
		case "/api/v1/key":
			if r.Header.Get("Authorization") != "Bearer "+openRouterKey {
				writeJSON(w, http.StatusUnauthorized, map[string]any{"error": map[string]any{"message": "No auth credentials found"}})
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{
				"label": "signin-e2e", "usage": 0, "limit": nil, "is_free_tier": false,
			}})
		case "/api/v1/models":
			writeJSON(w, http.StatusOK, map[string]any{"data": []any{
				map[string]any{"id": "openrouter/fake-model", "name": "Fake Model", "context_length": 8192,
					"pricing": map[string]any{"prompt": "0", "completion": "0"}},
			}})
		default:
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "unexpected path " + r.URL.Path})
		}
	}))
	t.Cleanup(fake.server.Close)
	return fake
}

// openRouterClient is the HTTP client the OAuth service is given: OpenRouter's
// fixed auth host is answered by the fake, everything else goes out unchanged.
// It is the seam oauth.Options.HTTP exists for.
func openRouterClient(fake *openRouterUpstream) *http.Client {
	target := strings.TrimPrefix(fake.server.URL, "http://")
	return &http.Client{Transport: rewriteTransport{host: "openrouter.ai", target: target}}
}

// rewriteTransport sends requests for one host to a local server.
type rewriteTransport struct {
	host   string
	target string
}

func (t rewriteTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.URL.Host != t.host {
		return http.DefaultTransport.RoundTrip(request)
	}
	clone := request.Clone(request.Context())
	clone.URL.Scheme = "http"
	clone.URL.Host = t.target
	clone.Host = ""
	return http.DefaultTransport.RoundTrip(clone)
}

// ---- the flows -------------------------------------------------------------

// TestSignInOpenRouterBrowserPKCE drives the dashboard's PKCE connection from the
// start link to the browser callback and checks the stored connection.
func TestSignInOpenRouterBrowserPKCE(t *testing.T) {
	upstream := newOpenRouterUpstream(t)
	app := openSignInApp(t, map[string]string{
		"NEOROUTER_OPENROUTER_BASE_URL": upstream.server.URL + "/api/v1",
	}, openRouterClient(upstream))
	app.register(t)

	started := app.call(t, http.MethodPost, "/api/v1/oauth/openrouter/start",
		`{"returnTo":"http://127.0.0.1:4600/app/?tab=providers"}`)
	if started.Code != http.StatusOK {
		t.Fatalf("start = %d %s", started.Code, started.Body.String())
	}
	authURL, _ := decodeJSONBody(t, started)["url"].(string)
	if !strings.HasPrefix(authURL, "https://openrouter.ai/auth?") {
		t.Fatalf("start url = %q", authURL)
	}
	state := callbackStateOf(t, authURL)

	// The browser comes back with no session: the callback is mounted outside
	// the session middleware on purpose.
	callback := app.callAs(t, http.MethodGet, "/api/v1/oauth/openrouter/callback/"+state+"?code=one-time-code", "", "")
	if callback.Code != http.StatusFound {
		t.Fatalf("callback = %d %s", callback.Code, callback.Body.String())
	}
	location := callback.Header().Get("Location")
	if !strings.Contains(location, "oauth_status=connected") || !strings.Contains(location, "oauth=openrouter") {
		t.Fatalf("callback location = %q", location)
	}
	if upstream.exhange["code"] != "one-time-code" {
		t.Fatalf("exchange body = %v", upstream.exhange)
	}

	row := app.storedAccount(t, "openrouter")
	row.requireHealthy(t, "paid")
	if row.Secret != openRouterKey {
		t.Fatalf("stored secret = %q, want the key OpenRouter issued", row.Secret)
	}

	// A browser can reach the callback twice (prefetch, refresh, back): the
	// second hit replays the outcome instead of burning the one-time state.
	repeat := app.callAs(t, http.MethodGet, "/api/v1/oauth/openrouter/callback/"+state+"?code=one-time-code", "", "")
	if repeat.Code != http.StatusFound || !strings.Contains(repeat.Header().Get("Location"), "oauth_status=connected") {
		t.Fatalf("repeat callback = %d %q", repeat.Code, repeat.Header().Get("Location"))
	}
	var openRouterAccounts int
	if err := app.DB.R.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM provider_accounts WHERE definition_id = 'openrouter'`).Scan(&openRouterAccounts); err != nil {
		t.Fatalf("count openrouter accounts: %v", err)
	}
	if openRouterAccounts != 1 {
		t.Fatalf("openrouter accounts = %d, want one connection", openRouterAccounts)
	}
}

// callbackStateOf pulls the one-time state out of the OpenRouter sign-in URL.
func callbackStateOf(t *testing.T, authURL string) string {
	t.Helper()
	parsed, err := url.Parse(authURL)
	if err != nil {
		t.Fatalf("parse %q: %v", authURL, err)
	}
	callback := parsed.Query().Get("callback_url")
	if callback == "" {
		t.Fatalf("%q carries no callback_url", authURL)
	}
	parts := strings.Split(callback, "/")
	state := parts[len(parts)-1]
	if state == "" {
		t.Fatalf("callback url %q carries no state", callback)
	}
	return state
}

// TestSignInChatGPTDeviceFlow drives the Codex device sign-in to a stored
// account.
func TestSignInChatGPTDeviceFlow(t *testing.T) {
	upstream := newCodexUpstream(t)
	app := openSignInApp(t, map[string]string{
		"NEOROUTER_CODEX_ISSUER":   upstream.server.URL,
		"NEOROUTER_CODEX_BASE_URL": upstream.server.URL + "/backend-api/codex",
	}, nil)
	app.register(t)

	started := app.call(t, http.MethodPost, "/api/v1/providers/chatgpt/login", `{"billingMode":"plan_paid"}`)
	if started.Code != http.StatusOK {
		t.Fatalf("start = %d %s", started.Code, started.Body.String())
	}
	login := decodeJSONBody(t, started)
	loginID, _ := login["loginId"].(string)
	if loginID == "" || login["userCode"] != "ABCD-1234" {
		t.Fatalf("login = %s", started.Body.String())
	}

	pending := app.call(t, http.MethodPost, "/api/v1/providers/chatgpt/login/"+loginID, "")
	if pending.Code != http.StatusOK || decodeJSONBody(t, pending)["status"] != "pending" {
		t.Fatalf("pending = %d %s", pending.Code, pending.Body.String())
	}

	done := app.call(t, http.MethodPost, "/api/v1/providers/chatgpt/login/"+loginID, "")
	if done.Code != http.StatusOK {
		t.Fatalf("done = %d %s", done.Code, done.Body.String())
	}
	body := decodeJSONBody(t, done)
	if body["status"] != "done" || body["plan"] != "plus" {
		t.Fatalf("done = %s", done.Body.String())
	}
	account, _ := body["account"].(map[string]any)
	if account["definitionId"] != "chatgpt" {
		t.Fatalf("account = %v", account)
	}

	row := app.storedAccount(t, "chatgpt")
	row.requireHealthy(t, "paid")
	credentials, err := providers.ParseCodexSecret(row.Secret)
	if err != nil {
		t.Fatalf("stored secret is not Codex credentials: %v", err)
	}
	if credentials.Access != "access-one" || credentials.Refresh != "refresh-one" || credentials.AccountID != "acct-123" {
		t.Fatalf("credentials = %+v", credentials)
	}
	if upstream.models == 0 {
		t.Fatal("the ChatGPT model listing was never read")
	}
}

// TestSignInNousDeviceFlow drives the Nous Portal device sign-in to a stored,
// validated account, and checks that the sync stored the rotated tokens.
func TestSignInNousDeviceFlow(t *testing.T) {
	upstream := newNousUpstream(t)
	app := openSignInApp(t, map[string]string{
		"NEOROUTER_NOUS_PORTAL_URL": upstream.server.URL,
		"NEOROUTER_NOUS_BASE_URL":   upstream.server.URL + "/v1",
	}, nil)
	app.register(t)

	started := app.call(t, http.MethodPost, "/api/v1/providers/nous/login", `{}`)
	if started.Code != http.StatusOK {
		t.Fatalf("start = %d %s", started.Code, started.Body.String())
	}
	login := decodeJSONBody(t, started)
	loginID, _ := login["loginId"].(string)
	if loginID == "" || login["userCode"] != "WXYZ-9876" {
		t.Fatalf("login = %s", started.Body.String())
	}

	pending := app.call(t, http.MethodPost, "/api/v1/providers/nous/login/"+loginID, "")
	if pending.Code != http.StatusOK || decodeJSONBody(t, pending)["status"] != "pending" {
		t.Fatalf("pending = %d %s", pending.Code, pending.Body.String())
	}

	done := app.call(t, http.MethodPost, "/api/v1/providers/nous/login/"+loginID, "")
	if done.Code != http.StatusOK {
		t.Fatalf("done = %d %s", done.Code, done.Body.String())
	}
	body := decodeJSONBody(t, done)
	if body["status"] != "done" || body["email"] != "portal@example.test" || body["tier"] != "paid" {
		t.Fatalf("done = %s", done.Body.String())
	}
	account, _ := body["account"].(map[string]any)
	if account["definitionId"] != "nous" {
		t.Fatalf("account = %v", account)
	}

	row := app.storedAccount(t, "nous")
	row.requireHealthy(t, "paid")
	credentials, err := providers.ParseNousSecret(row.Secret)
	if err != nil {
		t.Fatalf("stored secret is not Portal credentials: %v", err)
	}
	if credentials.Refresh == nil || *credentials.Refresh != "refresh-two" {
		t.Fatalf("the rotated refresh token was not stored: %+v", credentials)
	}
	if credentials.Email != "portal@example.test" || credentials.Tier != "paid" {
		t.Fatalf("credentials = %+v", credentials)
	}
	if upstream.refreshes == 0 {
		t.Fatal("the sync never refreshed the short-lived Portal token")
	}
	if upstream.models == 0 {
		t.Fatal("the Nous model listing was never read")
	}

	// The connection the dashboard checks, syncs and reads the credits of.
	validated := app.call(t, http.MethodPost, "/api/v1/providers/accounts/"+row.ID+"/validate", "")
	if validated.Code != http.StatusOK {
		t.Fatalf("validate = %d %s", validated.Code, validated.Body.String())
	}
	if status, _ := decodeJSONBody(t, validated)["validation"].(map[string]any)["status"].(string); status != "healthy" {
		t.Fatalf("validate = %s", validated.Body.String())
	}
	synced := app.call(t, http.MethodPost, "/api/v1/providers/accounts/"+row.ID+"/sync", "")
	if synced.Code != http.StatusOK || decodeJSONBody(t, synced)["sync"] == nil {
		t.Fatalf("sync = %d %s", synced.Code, synced.Body.String())
	}
	quota := app.call(t, http.MethodPost, "/api/v1/providers/accounts/"+row.ID+"/quota", "")
	if quota.Code != http.StatusOK {
		t.Fatalf("quota = %d %s", quota.Code, quota.Body.String())
	}
	windows, _ := decodeJSONBody(t, quota)["quota"].([]any)
	if len(windows) != 1 {
		t.Fatalf("quota = %s, want the Portal credits", quota.Body.String())
	}
	if window, _ := windows[0].(map[string]any); window["metric"] != "balance" || window["remaining"] != 12.5 {
		t.Fatalf("quota window = %v", windows[0])
	}

	// Signing in again re-keys that connection instead of adding another one.
	upstream.mu.Lock()
	upstream.polls = 0
	upstream.mu.Unlock()
	resumed := app.call(t, http.MethodPost, "/api/v1/providers/nous/login", `{"accountId":"`+row.ID+`"}`)
	if resumed.Code != http.StatusOK {
		t.Fatalf("re-sign-in start = %d %s", resumed.Code, resumed.Body.String())
	}
	resumedID, _ := decodeJSONBody(t, resumed)["loginId"].(string)
	if resumed := app.call(t, http.MethodPost, "/api/v1/providers/nous/login/"+resumedID, ""); resumed.Code != http.StatusOK {
		t.Fatalf("re-sign-in poll = %d %s", resumed.Code, resumed.Body.String())
	}
	rekeyed := app.call(t, http.MethodPost, "/api/v1/providers/nous/login/"+resumedID, "")
	if rekeyed.Code != http.StatusOK || decodeJSONBody(t, rekeyed)["status"] != "done" {
		t.Fatalf("re-sign-in done = %d %s", rekeyed.Code, rekeyed.Body.String())
	}
	rekeyedAccount, _ := decodeJSONBody(t, rekeyed)["account"].(map[string]any)
	if rekeyedAccount["id"] != row.ID {
		t.Fatalf("re-sign-in created a second connection: %v", rekeyedAccount["id"])
	}
	var nousAccounts int
	if err := app.DB.R.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM provider_accounts WHERE definition_id = 'nous'`).Scan(&nousAccounts); err != nil {
		t.Fatalf("count nous accounts: %v", err)
	}
	if nousAccounts != 1 {
		t.Fatalf("nous accounts = %d, want the one connection re-keyed", nousAccounts)
	}
}

// TestSignInNousSlowDown covers what the Portal does when the app polls too
// fast: it asks for a longer interval, and the poll answer carries it.
func TestSignInNousSlowDown(t *testing.T) {
	upstream := newNousUpstream(t)
	upstream.slowDown = true
	app := openSignInApp(t, map[string]string{
		"NEOROUTER_NOUS_PORTAL_URL": upstream.server.URL,
		"NEOROUTER_NOUS_BASE_URL":   upstream.server.URL + "/v1",
	}, nil)
	app.register(t)

	started := app.call(t, http.MethodPost, "/api/v1/providers/nous/login", `{}`)
	if started.Code != http.StatusOK {
		t.Fatalf("start = %d %s", started.Code, started.Body.String())
	}
	loginID, _ := decodeJSONBody(t, started)["loginId"].(string)

	slowed := app.call(t, http.MethodPost, "/api/v1/providers/nous/login/"+loginID, "")
	if slowed.Code != http.StatusOK {
		t.Fatalf("slow_down poll = %d %s", slowed.Code, slowed.Body.String())
	}
	slowBody := decodeJSONBody(t, slowed)
	if slowBody["status"] != "pending" || slowBody["interval"] != float64(10) {
		t.Fatalf("slow_down answer = %s", slowed.Body.String())
	}
	// The longer interval is remembered, so a waiting client keeps it.
	waiting := app.call(t, http.MethodPost, "/api/v1/providers/nous/login/"+loginID, "")
	if waiting.Code != http.StatusOK {
		t.Fatalf("pending poll = %d %s", waiting.Code, waiting.Body.String())
	}
	waitBody := decodeJSONBody(t, waiting)
	if waitBody["status"] != "pending" || waitBody["interval"] != float64(10) {
		t.Fatalf("pending answer = %s", waiting.Body.String())
	}

	done := app.call(t, http.MethodPost, "/api/v1/providers/nous/login/"+loginID, "")
	if done.Code != http.StatusOK || decodeJSONBody(t, done)["status"] != "done" {
		t.Fatalf("done = %d %s", done.Code, done.Body.String())
	}
	row := app.storedAccount(t, "nous")
	row.requireHealthy(t, "paid")
}
