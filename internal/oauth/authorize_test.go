package oauth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/NeoLabs-Systems/neorouter/internal/config"
	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
)

// redirectURI is a loopback callback, as an app on this computer uses.
const redirectURI = "http://127.0.0.1:4555/callback"

// access is the approval the JS test sends.
const accessBody = `{"allowFree":true,"allowPlan":false,"allowPaid":false}`

type oauthEnv struct {
	t        *testing.T
	svc      *Service
	mux      *http.ServeMux
	keys     *fakeKeys
	accounts *fakeAccounts
	catalog  *fakeCatalog
	now      time.Time
}

func newOAuthEnv(t *testing.T) *oauthEnv {
	t.Helper()
	env := &oauthEnv{
		t:        t,
		keys:     &fakeKeys{},
		accounts: &fakeAccounts{},
		catalog:  &fakeCatalog{},
		now:      time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
	}
	env.svc = New(Options{
		Config:   func() config.Config { return config.Config{Port: 4600} },
		Log:      slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
		Keys:     env.keys,
		Accounts: env.accounts,
		Catalog:  env.catalog,
		Guard:    Guard{Authenticate: authenticate, Space: space},
		Now:      func() time.Time { return env.now },
	})
	env.mux = http.NewServeMux()
	env.svc.Routes(env.mux)
	env.svc.ManagementRoutes(env.mux)
	return env
}

// tokens map a bearer token to the caller it authenticates, like the sessions
// table of internal/auth and the membership table of internal/teams.
var sessions = map[string]Session{
	"owner-token": {UserID: "user-owner", Role: "user"},
	"team-token":  {UserID: "user-team", Role: "user"},
}

var spaces = map[string]Space{
	"user-owner": {OwnerID: "user-owner", ActorID: "user-owner", IsOwner: true,
		Permissions: map[string]bool{"keys.create": true, "providers.manage": true}},
	// A team member working in the owner's space, without the rights to connect
	// an app or an account.
	"user-team": {OwnerID: "user-owner", ActorID: "user-team", IsOwner: false,
		Permissions: map[string]bool{"keys.view": true}},
}

func authenticate(r *http.Request) (*Session, error) {
	session, ok := sessions[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")]
	if !ok {
		return nil, ErrAuthenticationRequired()
	}
	return &session, nil
}

func space(_ *http.Request, session *Session) (*Space, error) {
	candidate, ok := spaces[session.UserID]
	if !ok {
		return nil, httpserver.New(http.StatusNotFound, "SPACE_NOT_FOUND", "This space does not exist.")
	}
	return &candidate, nil
}

// do sends one request; an empty contentType leaves the body unlabelled.
func (e *oauthEnv) do(token, method, path, contentType, body string) *httptest.ResponseRecorder {
	e.t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	recorder := httptest.NewRecorder()
	e.mux.ServeHTTP(recorder, request)
	return recorder
}

func (e *oauthEnv) get(token, path string) *httptest.ResponseRecorder {
	return e.do(token, http.MethodGet, path, "", "")
}

func (e *oauthEnv) postJSON(token, path, body string) *httptest.ResponseRecorder {
	return e.do(token, http.MethodPost, path, "application/json", body)
}

func (e *oauthEnv) postForm(path string, form url.Values) *httptest.ResponseRecorder {
	return e.do("", http.MethodPost, path, "application/x-www-form-urlencoded", form.Encode())
}

// authorize is GET /oauth/authorize with the JS test's query, plus overrides. A
// value of "-" removes that parameter.
func (e *oauthEnv) authorize(overrides map[string]string) *httptest.ResponseRecorder {
	e.t.Helper()
	query := url.Values{}
	params := map[string]string{
		"response_type":         "code",
		"client_id":             "neoagent",
		"client_name":           "NeoAgent",
		"redirect_uri":          redirectURI,
		"code_challenge":        challengeOf(pkceVerifier()),
		"code_challenge_method": "S256",
		"state":                 "xyz",
		"scope":                 "chat live bogus",
	}
	for key, value := range overrides {
		if value == "-" {
			delete(params, key)
			continue
		}
		params[key] = value
	}
	for key, value := range params {
		query.Set(key, value)
	}
	return e.get("", "/oauth/authorize?"+query.Encode())
}

func challengeOf(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// pkceVerifier is the PKCE verifier the test's app keeps.
func pkceVerifier() string {
	return base64.RawURLEncoding.EncodeToString([]byte("SI_SECRET_TOKEN_0FT1S3CG-0123456789abcdef"))
}

// connectID is the request id an authorize redirect carries.
func (e *oauthEnv) connectID(recorder *httptest.ResponseRecorder) string {
	e.t.Helper()
	location := recorder.Header().Get("Location")
	if !strings.HasPrefix(location, "/app/?connect=") {
		e.t.Fatalf("location = %q, want /app/?connect=...", location)
	}
	return strings.TrimPrefix(location, "/app/?connect=")
}

func decode(t *testing.T, recorder *httptest.ResponseRecorder, dst any) {
	t.Helper()
	if err := json.Unmarshal(recorder.Body.Bytes(), dst); err != nil {
		t.Fatalf("decode %q: %v", recorder.Body.String(), err)
	}
}

// TestCodeFlowGivesTheAppAWorkingKey ports the JS test of the same name.
func TestCodeFlowGivesTheAppAWorkingKey(t *testing.T) {
	env := newOAuthEnv(t)
	start := env.authorize(nil)
	if start.Code != http.StatusFound {
		t.Fatalf("status = %d, body %s", start.Code, start.Body)
	}
	requestID := env.connectID(start)
	if requestID == "" {
		t.Fatal("no request id")
	}

	shown := env.get("owner-token", "/api/v1/oauth/apps/requests/"+requestID)
	if shown.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", shown.Code, shown.Body)
	}
	var described struct {
		Request describeRequest `json:"request"`
	}
	decode(t, shown, &described)
	if described.Request.Name != "NeoAgent" {
		t.Errorf("name = %q", described.Request.Name)
	}
	if strings.Join(described.Request.Scopes, ",") != "chat,live" {
		t.Errorf("scopes = %v, want the unknown one dropped", described.Request.Scopes)
	}
	if described.Request.Redirect == nil || described.Request.Redirect.Host != "127.0.0.1:4555" || !described.Request.Redirect.Secure {
		t.Errorf("redirect = %+v", described.Request.Redirect)
	}
	if described.Request.ExpiresAt != "2026-01-02T03:14:05.000Z" {
		t.Errorf("expiresAt = %q", described.Request.ExpiresAt)
	}

	approved := env.postJSON("owner-token", "/api/v1/oauth/apps/requests/"+requestID+"/approve", accessBody)
	if approved.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", approved.Code, approved.Body)
	}
	var approval approveResult
	decode(t, approved, &approval)
	if approval.Redirect == nil {
		t.Fatal("no redirect for the code flow")
	}
	back, err := url.Parse(*approval.Redirect)
	if err != nil {
		t.Fatalf("parse redirect: %v", err)
	}
	if back.Query().Get("state") != "xyz" {
		t.Errorf("state = %q", back.Query().Get("state"))
	}
	code := back.Query().Get("code")
	if code == "" {
		t.Fatal("no code")
	}

	wrong := env.postForm("/oauth/token", url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"client_id":     {"neoagent"},
		"redirect_uri":  {redirectURI},
		"code_verifier": {strings.Repeat("x", 43)},
	})
	if wrong.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", wrong.Code)
	}
	if !strings.Contains(wrong.Body.String(), `"error":"invalid_grant"`) {
		t.Errorf("body = %s", wrong.Body)
	}

	// A failed exchange burns the code too.
	reused := env.postForm("/oauth/token", url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"client_id":     {"neoagent"},
		"redirect_uri":  {redirectURI},
		"code_verifier": {pkceVerifier()},
	})
	if !strings.Contains(reused.Body.String(), `"error":"invalid_grant"`) {
		t.Errorf("reused code body = %s", reused.Body)
	}
}

// TestCorrectExchangeReturnsKeyAndWhereToUseIt ports the JS test of the same name.
func TestCorrectExchangeReturnsKeyAndWhereToUseIt(t *testing.T) {
	env := newOAuthEnv(t)
	// The key service compares an expiry against its own clock, so the flow runs
	// on the real one here, as it does in production.
	env.svc.now = time.Now
	start := env.authorize(map[string]string{"state": "-"})
	requestID := env.connectID(start)
	expiresAt := time.Now().Add(24 * time.Hour).UTC().Format("2006-01-02T15:04:05.000Z")
	approved := env.postJSON("owner-token", "/api/v1/oauth/apps/requests/"+requestID+"/approve",
		`{"allowFree":true,"allowPlan":false,"allowPaid":false,"expiresAt":"`+expiresAt+`"}`)
	if approved.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", approved.Code, approved.Body)
	}
	var approval approveResult
	decode(t, approved, &approval)
	back, err := url.Parse(*approval.Redirect)
	if err != nil {
		t.Fatalf("parse redirect: %v", err)
	}
	if back.Query().Get("state") != "" {
		t.Errorf("state = %q, want none (it was not sent)", back.Query().Get("state"))
	}

	token := env.postForm("/oauth/token", url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {back.Query().Get("code")},
		"client_id":     {"neoagent"},
		"redirect_uri":  {redirectURI},
		"code_verifier": {pkceVerifier()},
	})
	if token.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", token.Code, token.Body)
	}
	var issued tokenResponse
	decode(t, token, &issued)
	if !strings.HasPrefix(issued.AccessToken, "nrt_") {
		t.Errorf("access_token = %q", issued.AccessToken)
	}
	if issued.ExpiresIn == nil || *issued.ExpiresIn <= 0 || *issued.ExpiresIn > 86400 {
		t.Errorf("expires_in = %v", issued.ExpiresIn)
	}
	if !strings.HasSuffix(issued.BaseURL, "/v1") {
		t.Errorf("base_url = %q", issued.BaseURL)
	}
	if got := token.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("Access-Control-Allow-Origin = %q", got)
	}
	if got := token.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q", got)
	}
	if !strings.Contains(token.Body.String(), `"token_type":"Bearer"`) {
		t.Errorf("body = %s", token.Body)
	}

	// The key records the expiry, the app it was made for and its scopes.
	created := env.keys.only(t)
	if created.request.Name != "NeoAgent" {
		t.Errorf("key name = %q", created.request.Name)
	}
	if created.request.ExpiresAt == nil || *created.request.ExpiresAt != expiresAt {
		t.Errorf("expiresAt = %v", created.request.ExpiresAt)
	}
	if !created.request.AllowFree || created.request.AllowPlan || created.request.AllowPaid {
		t.Errorf("switches = %+v", created.request)
	}
	if created.request.BudgetUSD != nil || created.request.TokenLimit != nil || created.request.MaxPricePerMillion != nil {
		t.Errorf("limits leaked into a free-only key: %+v", created.request)
	}
	if created.app.Name != "NeoAgent" || created.app.Origin == nil || *created.app.Origin != "127.0.0.1:4555" {
		t.Errorf("app = %+v", created.app)
	}
	if strings.Join(created.app.Scopes, " ") != "chat live" {
		t.Errorf("app scopes = %v", created.app.Scopes)
	}
	if created.ownerID != "user-owner" || created.createdBy != "user-owner" {
		t.Errorf("owner %q / created by %q", created.ownerID, created.createdBy)
	}
}

// TestBadRequestsNeverRedirectToAnUncheckedAddress ports the JS test of the same
// name plus the response_type and method checks.
func TestBadRequestsNeverRedirectToAnUncheckedAddress(t *testing.T) {
	env := newOAuthEnv(t)
	bad := env.authorize(map[string]string{"redirect_uri": "javascript:alert(1)"})
	if bad.Code != http.StatusFound {
		t.Fatalf("status = %d", bad.Code)
	}
	location := bad.Header().Get("Location")
	if !strings.HasPrefix(location, "/app/?connect_error=") {
		t.Fatalf("location = %q", location)
	}
	if !strings.Contains(location, "redirect_uri+must+be+an+absolute+URL.") && !strings.Contains(location, "redirect_uri%20must%20be%20an%20absolute%20URL.") {
		t.Errorf("location = %q", location)
	}

	noPKCE := env.authorize(map[string]string{"code_challenge": "-"})
	target, err := url.Parse(noPKCE.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse location: %v", err)
	}
	if got := target.Query().Get("error"); got != "invalid_request" {
		t.Errorf("error = %q", got)
	}
	if got := target.Query().Get("state"); got != "xyz" {
		t.Errorf("state = %q", got)
	}

	cases := []struct{ key, value, want string }{
		{"client_id", "-", "invalid_request"},
		{"response_type", "token", "unsupported_response_type"},
		{"code_challenge_method", "plain", "invalid_request"},
	}
	for _, testCase := range cases {
		response := env.authorize(map[string]string{testCase.key: testCase.value})
		target, err := url.Parse(response.Header().Get("Location"))
		if err != nil {
			t.Fatalf("%s: parse location: %v", testCase.key, err)
		}
		if got := target.Query().Get("error"); got != testCase.want {
			t.Errorf("%s = %s: error = %q, want %q", testCase.key, testCase.value, got, testCase.want)
		}
	}

	// A fragment in the callback is refused, because it would run in the app.
	fragmented := env.authorize(map[string]string{"redirect_uri": redirectURI + "#frag"})
	if !strings.HasPrefix(fragmented.Header().Get("Location"), "/app/?connect_error=") {
		t.Errorf("fragment location = %q", fragmented.Header().Get("Location"))
	}
}

// TestDecliningSendsAccessDenied ports the JS test of the same name.
func TestDecliningSendsAccessDenied(t *testing.T) {
	env := newOAuthEnv(t)
	start := env.authorize(nil)
	requestID := env.connectID(start)
	denied := env.postJSON("owner-token", "/api/v1/oauth/apps/requests/"+requestID+"/deny", "")
	if denied.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", denied.Code, denied.Body)
	}
	var result approveResult
	decode(t, denied, &result)
	target, err := url.Parse(*result.Redirect)
	if err != nil {
		t.Fatalf("parse redirect: %v", err)
	}
	if got := target.Query().Get("error"); got != "access_denied" {
		t.Errorf("error = %q", got)
	}
	if got := target.Query().Get("error_description"); got != "The request was declined." {
		t.Errorf("error_description = %q", got)
	}
	if got := target.Query().Get("state"); got != "xyz" {
		t.Errorf("state = %q", got)
	}
	if again := env.get("owner-token", "/api/v1/oauth/apps/requests/"+requestID); again.Code != http.StatusNotFound {
		t.Errorf("answered request status = %d", again.Code)
	}
}

// TestDeviceFlowWorksWithATypedCode ports the JS test of the same name.
func TestDeviceFlowWorksWithATypedCode(t *testing.T) {
	env := newOAuthEnv(t)
	device := env.postJSON("", "/oauth/device/code", `{"client_id":"neoagent-server","client_name":"NeoAgent (home)","scope":"chat transcription"}`)
	if device.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", device.Code, device.Body)
	}
	var code deviceCodeResponse
	decode(t, device, &code)
	if len(code.UserCode) != 9 || code.UserCode[4] != '-' {
		t.Errorf("user_code = %q", code.UserCode)
	}
	for _, r := range strings.ReplaceAll(code.UserCode, "-", "") {
		if !strings.ContainsRune(userCodeAlphabet, r) {
			t.Errorf("user_code %q uses %q", code.UserCode, r)
		}
	}
	if code.ExpiresIn != 600 || code.Interval != 5 {
		t.Errorf("expires_in = %d, interval = %d", code.ExpiresIn, code.Interval)
	}
	if code.VerificationURI != "http://example.com/app/?connect=" {
		t.Errorf("verification_uri = %q", code.VerificationURI)
	}
	if !strings.HasPrefix(code.VerificationURIComplete, "http://example.com/app/?connect=") {
		t.Errorf("verification_uri_complete = %q", code.VerificationURIComplete)
	}

	poll := func() *httptest.ResponseRecorder {
		return env.postForm("/oauth/token", url.Values{
			"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
			"device_code": {code.DeviceCode},
			"client_id":   {"neoagent-server"},
		})
	}
	if got := poll(); !strings.Contains(got.Body.String(), `"error":"authorization_pending"`) {
		t.Fatalf("first poll = %s", got.Body)
	}
	if got := poll(); !strings.Contains(got.Body.String(), `"error":"slow_down"`) {
		t.Fatalf("second poll = %s, want slow_down for polling faster than the interval", got.Body)
	}

	typed := strings.ToLower(code.UserCode)
	typed = strings.Replace(typed, "-", " ", 1)
	shown := env.get("owner-token", "/api/v1/oauth/apps/requests/"+url.PathEscape(typed))
	if shown.Code != http.StatusOK {
		t.Fatalf("typed code status = %d, body %s", shown.Code, shown.Body)
	}
	var described struct {
		Request describeRequest `json:"request"`
	}
	decode(t, shown, &described)
	if described.Request.Kind != "device" {
		t.Errorf("kind = %q", described.Request.Kind)
	}
	if described.Request.Redirect != nil {
		t.Errorf("device request has a redirect: %+v", described.Request.Redirect)
	}

	approved := env.postJSON("owner-token", "/api/v1/oauth/apps/requests/"+described.Request.ID+"/approve",
		`{"allowFree":true,"allowPlan":true,"allowPaid":true,"budgetUsd":5,"budgetPeriod":"monthly"}`)
	if approved.Code != http.StatusOK {
		t.Fatalf("approve status = %d, body %s", approved.Code, approved.Body)
	}
	var approval approveResult
	decode(t, approved, &approval)
	if approval.Redirect != nil {
		t.Errorf("device approval redirected: %v", *approval.Redirect)
	}

	// The interval grew by five seconds while polling too fast.
	env.now = env.now.Add(11 * time.Second)
	token := poll()
	if token.Code != http.StatusOK {
		t.Fatalf("token status = %d, body %s", token.Code, token.Body)
	}
	var issued tokenResponse
	decode(t, token, &issued)
	if issued.ExpiresIn != nil {
		t.Errorf("expires_in = %v, want none without an expiry", *issued.ExpiresIn)
	}
	created := env.keys.only(t)
	if created.request.BudgetUSD == nil || *created.request.BudgetUSD != 5 {
		t.Errorf("budget = %v", created.request.BudgetUSD)
	}
	if created.request.BudgetPeriod == nil || *created.request.BudgetPeriod != "monthly" {
		t.Errorf("budget period = %v", created.request.BudgetPeriod)
	}
	if created.app.Origin != nil {
		t.Errorf("device app origin = %v, want null", *created.app.Origin)
	}
	if strings.Join(created.app.Scopes, " ") != "chat transcription" {
		t.Errorf("device scopes = %v", created.app.Scopes)
	}
	// The device code works once.
	if again := poll(); !strings.Contains(again.Body.String(), `"error":"expired_token"`) {
		t.Errorf("reused device code = %s", again.Body)
	}
}

// TestServerDescribesItselfForOAuthLibraries ports the JS test of the same name.
func TestServerDescribesItselfForOAuthLibraries(t *testing.T) {
	env := newOAuthEnv(t)
	meta := env.get("", "/.well-known/oauth-authorization-server")
	if meta.Code != http.StatusOK {
		t.Fatalf("status = %d", meta.Code)
	}
	var body metadata
	decode(t, meta, &body)
	if !strings.HasSuffix(body.TokenEndpoint, "/oauth/token") {
		t.Errorf("token_endpoint = %q", body.TokenEndpoint)
	}
	if len(body.CodeChallengeMethodsSupported) != 1 || body.CodeChallengeMethodsSupported[0] != "S256" {
		t.Errorf("code_challenge_methods_supported = %v", body.CodeChallengeMethodsSupported)
	}
	if body.Issuer != "http://example.com" {
		t.Errorf("issuer = %q", body.Issuer)
	}
	if meta.Header().Get("Access-Control-Allow-Origin") != "*" || meta.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("headers = %v", meta.Header())
	}
	// The body keeps the key order apps read.
	if !strings.Contains(meta.Body.String(), `"issuer":"http://example.com","authorization_endpoint":"http://example.com/oauth/authorize","token_endpoint"`) {
		t.Errorf("body = %s", meta.Body)
	}
	// A preflight is answered by the CORS wrapper, not the route.
	preflight := env.do("", http.MethodOptions, "/oauth/token", "", "")
	if preflight.Code != http.StatusNoContent || preflight.Header().Get("Access-Control-Allow-Methods") != "GET,POST,OPTIONS" {
		t.Errorf("preflight = %d %v", preflight.Code, preflight.Header())
	}
}

// TestApprovalValidatesTheKeyOptions ports connect.js's zod access object and the
// key service's own checks at approval time.
func TestApprovalValidatesTheKeyOptions(t *testing.T) {
	env := newOAuthEnv(t)
	start := env.authorize(nil)
	requestID := env.connectID(start)
	approve := func(body string) *httptest.ResponseRecorder {
		return env.postJSON("owner-token", "/api/v1/oauth/apps/requests/"+requestID+"/approve", body)
	}
	cases := []string{
		`{"allowPlan":true}`, // allowFree is required
		`{"allowFree":true,"allowPlan":false,"allowPaid":false,"budgetUsd":-1}`,
		`{"allowFree":true,"allowPlan":false,"allowPaid":false,"rpmLimit":1.5}`,
		`{"allowFree":true,"allowPlan":false,"allowPaid":false,"tokenLimit":0}`,
		`{"allowFree":true,"allowPlan":false,"allowPaid":false,"budgetPeriod":"weekly"}`,
		`{"allowFree":true,"allowPlan":false,"allowPaid":false,"expiresAt":"yesterday"}`,
	}
	for _, body := range cases {
		response := approve(body)
		if response.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d", body, response.Code)
			continue
		}
		if !strings.Contains(response.Body.String(), `"message":"Choose what the app may use."`) {
			t.Errorf("%s: body = %s", body, response.Body)
		}
	}

	// Every switch off is the key service's refusal, not the body's.
	none := approve(`{"allowFree":false,"allowPlan":false,"allowPaid":false}`)
	if none.Code != http.StatusBadRequest || !strings.Contains(none.Body.String(), `"code":"NO_MODELS_ALLOWED"`) {
		t.Errorf("status = %d, body %s", none.Code, none.Body)
	}
	// An expiry in the past is refused before the app ever sees a code.
	past := approve(`{"allowFree":true,"allowPlan":false,"allowPaid":false,"expiresAt":"2020-01-01T00:00:00.000Z"}`)
	if past.Code != http.StatusBadRequest || !strings.Contains(past.Body.String(), `"code":"INVALID_KEY_EXPIRY"`) {
		t.Errorf("status = %d, body %s", past.Code, past.Body)
	}
	// Nothing was made for a refused approval.
	if created := env.keys.createdKeys(); len(created) != 0 {
		t.Errorf("keys were made: %d", len(created))
	}
}

// TestOAuthRoutesNeedASessionAndPermission covers the middleware routes/oauth.js
// puts in front of the approval and OpenRouter routes.
func TestOAuthRoutesNeedASessionAndPermission(t *testing.T) {
	env := newOAuthEnv(t)
	paths := []struct{ method, path string }{
		{http.MethodGet, "/api/v1/oauth/apps/requests/abc"},
		{http.MethodPost, "/api/v1/oauth/apps/requests/abc/approve"},
		{http.MethodPost, "/api/v1/oauth/apps/requests/abc/deny"},
		{http.MethodPost, "/api/v1/oauth/openrouter/start"},
	}
	for _, target := range paths {
		anonymous := env.do("", target.method, target.path, "application/json", "{}")
		if anonymous.Code != http.StatusUnauthorized || !strings.Contains(anonymous.Body.String(), `"code":"AUTHENTICATION_REQUIRED"`) {
			t.Errorf("%s %s anonymous = %d %s", target.method, target.path, anonymous.Code, anonymous.Body)
		}
		teamMember := env.do("team-token", target.method, target.path, "application/json", "{}")
		if teamMember.Code != http.StatusForbidden || !strings.Contains(teamMember.Body.String(), `"code":"PERMISSION_DENIED"`) {
			t.Errorf("%s %s as team member = %d %s", target.method, target.path, teamMember.Code, teamMember.Body)
		}
	}
	// The OpenRouter callback is a browser redirect without a session header.
	callback := env.get("", "/api/v1/oauth/openrouter/callback/unknown")
	if callback.Code != http.StatusBadRequest || !strings.Contains(callback.Body.String(), `"code":"OAUTH_EXPIRED"`) {
		t.Errorf("callback = %d %s", callback.Code, callback.Body)
	}
}

// fakeKeys records what the flow asked for and answers like
// router_key_service does.
type fakeKeys struct {
	mu   sync.Mutex
	next int
	keys []createdKey
}

type createdKey struct {
	ownerID   string
	createdBy string
	request   KeyRequest
	app       AppInfo
}

func (f *fakeKeys) Modes(allowFree, allowPlan, allowPaid bool) error {
	if !allowFree && !allowPlan && !allowPaid {
		return httpserver.New(http.StatusBadRequest, "NO_MODELS_ALLOWED",
			"Allow at least one kind of model: free, prepaid plans or paid.")
	}
	return nil
}

func (f *fakeKeys) AssertExpiry(expiresAt *string) error {
	if expiresAt == nil {
		return nil
	}
	at, err := time.Parse(time.RFC3339Nano, *expiresAt)
	if err != nil || !at.After(time.Now()) {
		return httpserver.New(http.StatusBadRequest, "INVALID_KEY_EXPIRY", "Choose an expiry date and time in the future.")
	}
	return nil
}

func (f *fakeKeys) Create(ownerID string, request KeyRequest, createdBy string, app AppInfo) (*IssuedKey, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.next++
	f.keys = append(f.keys, createdKey{ownerID: ownerID, createdBy: createdBy, request: request, app: app})
	// A router key looks like nrt_<prefix>_<token>.
	secret := "nrt_00a1b2c3_" + strings.Repeat("k", 43)
	return &IssuedKey{ID: "key-" + strconv.Itoa(f.next), Secret: secret, ExpiresAt: request.ExpiresAt}, nil
}

func (f *fakeKeys) createdKeys() []createdKey {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]createdKey(nil), f.keys...)
}

func (f *fakeKeys) only(t *testing.T) createdKey {
	t.Helper()
	keys := f.createdKeys()
	if len(keys) != 1 {
		t.Fatalf("keys created = %d, want 1", len(keys))
	}
	return keys[0]
}

type fakeAccounts struct {
	mu       sync.Mutex
	accounts []NewAccount
	owners   []string
	created  []string
}

func (f *fakeAccounts) CreateAccount(_ context.Context, ownerID string, request NewAccount, createdBy string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.accounts = append(f.accounts, request)
	f.owners = append(f.owners, ownerID+"/"+createdBy)
	f.created = append(f.created, "acct-1")
	return "acct-1", nil
}

type fakeCatalog struct {
	mu     sync.Mutex
	synced []string
	fail   bool
}

func (f *fakeCatalog) SyncAccount(_ context.Context, accountID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.synced = append(f.synced, accountID)
	if f.fail {
		return &httpError{message: "no models"}
	}
	return nil
}

type httpError struct{ message string }

func (e *httpError) Error() string { return e.message }

// TestTokenAcceptsAJSONBody covers applyBody's express.json branch: apps built
// with a JSON HTTP client post the same fields.
func TestTokenAcceptsAJSONBody(t *testing.T) {
	env := newOAuthEnv(t)
	start := env.authorize(nil)
	requestID := env.connectID(start)
	approved := env.postJSON("owner-token", "/api/v1/oauth/apps/requests/"+requestID+"/approve", accessBody)
	var approval approveResult
	decode(t, approved, &approval)
	code, err := url.Parse(*approval.Redirect)
	if err != nil {
		t.Fatalf("parse redirect: %v", err)
	}

	token := env.postJSON("", "/oauth/token", `{"grant_type":"authorization_code","code":"`+
		code.Query().Get("code")+`","client_id":"neoagent","redirect_uri":"`+redirectURI+`","code_verifier":"`+pkceVerifier()+`"}`)
	if token.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", token.Code, token.Body)
	}
	var issued tokenResponse
	decode(t, token, &issued)
	if issued.KeyID == "" || issued.AccessToken == "" {
		t.Errorf("token = %s", token.Body)
	}

	// A body over the 16kb applyBody limit is refused before it is parsed.
	huge := env.do("", http.MethodPost, "/oauth/token", "application/x-www-form-urlencoded",
		"grant_type=authorization_code&padding="+strings.Repeat("a", 17<<10))
	if huge.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("oversized body status = %d, body %s", huge.Code, huge.Body)
	}

	// An unknown grant type answers the OAuth shape, not the management one.
	other := env.postJSON("", "/oauth/token", `{"grant_type":"password"}`)
	if other.Code != http.StatusBadRequest || !strings.Contains(other.Body.String(), `"error":"unsupported_grant_type"`) {
		t.Errorf("status = %d, body %s", other.Code, other.Body)
	}
	if !strings.Contains(other.Body.String(), "Use authorization_code or the device_code grant.") {
		t.Errorf("body = %s", other.Body)
	}

	// A device_code grant with an unknown code says the code expired.
	unknown := env.postForm("/oauth/token", url.Values{
		"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
		"device_code": {"nope"},
		"client_id":   {"neoagent-server"},
	})
	if !strings.Contains(unknown.Body.String(), `"error":"expired_token"`) {
		t.Errorf("body = %s", unknown.Body)
	}

	// The wrong client id never gets a key.
	start = env.authorize(nil)
	requestID = env.connectID(start)
	approved = env.postJSON("owner-token", "/api/v1/oauth/apps/requests/"+requestID+"/approve", accessBody)
	decode(t, approved, &approval)
	code, err = url.Parse(*approval.Redirect)
	if err != nil {
		t.Fatalf("parse redirect: %v", err)
	}
	wrongClient := env.postForm("/oauth/token", url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code.Query().Get("code")},
		"client_id":     {"someone-else"},
		"redirect_uri":  {redirectURI},
		"code_verifier": {pkceVerifier()},
	})
	if !strings.Contains(wrongClient.Body.String(), "client_id does not match the authorization request.") {
		t.Errorf("body = %s", wrongClient.Body)
	}
	// And neither does the wrong redirect uri, on a fresh code.
	start = env.authorize(nil)
	requestID = env.connectID(start)
	approved = env.postJSON("owner-token", "/api/v1/oauth/apps/requests/"+requestID+"/approve", accessBody)
	decode(t, approved, &approval)
	code, err = url.Parse(*approval.Redirect)
	if err != nil {
		t.Fatalf("parse redirect: %v", err)
	}
	wrongRedirect := env.postForm("/oauth/token", url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code.Query().Get("code")},
		"client_id":     {"neoagent"},
		"redirect_uri":  {"http://127.0.0.1:4555/other"},
		"code_verifier": {pkceVerifier()},
	})
	if !strings.Contains(wrongRedirect.Body.String(), "redirect_uri does not match the authorization request.") {
		t.Errorf("body = %s", wrongRedirect.Body)
	}
}
