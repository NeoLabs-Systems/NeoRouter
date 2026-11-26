package providers

// The two device-code sign-ins against fake auth servers.

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// testJWT builds a token whose payload is claims (no signature: the flows only
// read the claims).
func testJWT(claims map[string]any) string {
	payload, err := json.Marshal(claims)
	if err != nil {
		panic(err)
	}
	return "x." + base64RawURL(payload) + ".y"
}

// codexTokens is the token answer of the fake OpenAI auth server.
func codexTokens(suffix string, expiresIn int) map[string]any {
	return map[string]any{
		"access_token":  "access-" + suffix,
		"refresh_token": "refresh-" + suffix,
		"expires_in":    float64(expiresIn),
		"id_token": testJWT(map[string]any{
			codexAccountClaim: map[string]any{"chatgpt_account_id": "acct-123", "chatgpt_plan_type": "plus"},
		}),
	}
}

// nousTokens is the token answer of the fake Nous Portal.
func nousTokens(suffix string, expiresIn int) map[string]any {
	return map[string]any{
		"access_token":  testJWT(map[string]any{"sub": "user-1", "email": "portal@example.test", "account_tier": "paid"}),
		"refresh_token": "refresh-" + suffix,
		"expires_in":    float64(expiresIn),
		"scope":         NousScope,
	}
}

// base64RawURL encodes without padding, as a JWT segment is written.
func base64RawURL(raw []byte) string {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	var out strings.Builder
	for i := 0; i < len(raw); i += 3 {
		var chunk [3]byte
		n := copy(chunk[:], raw[i:])
		value := uint32(chunk[0])<<16 | uint32(chunk[1])<<8 | uint32(chunk[2])
		out.WriteByte(alphabet[(value>>18)&0x3f])
		out.WriteByte(alphabet[(value>>12)&0x3f])
		if n > 1 {
			out.WriteByte(alphabet[(value>>6)&0x3f])
		}
		if n > 2 {
			out.WriteByte(alphabet[value&0x3f])
		}
	}
	return out.String()
}

// codexAuthServer is the fake OpenAI auth server: two polls before the sign-in
// is approved, and one short-lived token answer.
type codexAuthServer struct {
	mu        sync.Mutex
	polls     int
	refreshes int
	shortTTL  int
	startCode int
	startBody map[string]any
	noAccount bool
	noTokens  bool
	refuse    bool
	url       string
}

func newCodexAuthServer(t *testing.T) *codexAuthServer {
	t.Helper()
	server := &codexAuthServer{shortTTL: 1}
	server.url = newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		server.mu.Lock()
		defer server.mu.Unlock()
		switch r.URL.Path {
		case "/api/accounts/deviceauth/usercode":
			if server.startCode != 0 {
				w.WriteHeader(server.startCode)
				writeTestJSON(w, server.startBody)
				return
			}
			writeTestJSON(w, map[string]any{"device_auth_id": "dev-1", "user_code": "ABCD-1234", "interval": 1})
		case "/api/accounts/deviceauth/token":
			server.polls++
			if server.polls < 2 {
				w.WriteHeader(http.StatusForbidden)
				writeTestJSON(w, map[string]any{"error": "authorization_pending"})
				return
			}
			if server.refuse {
				w.WriteHeader(http.StatusBadRequest)
				writeTestJSON(w, map[string]any{"error": "bad_exchange"})
				return
			}
			writeTestJSON(w, map[string]any{"authorization_code": "code-1", "code_verifier": "verifier-1"})
		case "/oauth/token":
			if err := r.ParseForm(); err != nil {
				t.Errorf("parse form: %v", err)
			}
			if r.PostForm.Get("grant_type") == "authorization_code" {
				if r.PostForm.Get("redirect_uri") != server.url+"/deviceauth/callback" || r.PostForm.Get("code_verifier") != "verifier-1" {
					w.WriteHeader(http.StatusBadRequest)
					writeTestJSON(w, map[string]any{"error": "bad_exchange"})
					return
				}
				if server.noTokens {
					writeTestJSON(w, map[string]any{})
					return
				}
				tokens := codexTokens("one", server.shortTTL)
				if server.noAccount {
					delete(tokens, "id_token")
					tokens["access_token"] = "access-one"
				}
				writeTestJSON(w, tokens)
				return
			}
			server.refreshes++
			writeTestJSON(w, codexTokens("two", 3600))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	return server
}

// nousPortalServer is the fake Nous Portal.
type nousPortalServer struct {
	mu         sync.Mutex
	polls      int
	refreshes  int
	shortTTL   int
	decline    bool
	badClient  bool
	refreshErr string
	wrongToken bool
	url        string
}

func newNousPortalServer(t *testing.T) *nousPortalServer {
	t.Helper()
	server := &nousPortalServer{shortTTL: 1}
	server.url = newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		server.mu.Lock()
		defer server.mu.Unlock()
		switch r.URL.Path {
		case "/api/oauth/device/code":
			if err := r.ParseForm(); err != nil {
				t.Errorf("parse form: %v", err)
			}
			if server.badClient || r.PostForm.Get("client_id") != NousClientID {
				w.WriteHeader(http.StatusBadRequest)
				writeTestJSON(w, map[string]any{"error": "invalid_client", "error_description": "Unsupported OAuth client_id: " + r.PostForm.Get("client_id")})
				return
			}
			if r.PostForm.Get("scope") != NousScope {
				t.Errorf("scope = %q", r.PostForm.Get("scope"))
			}
			writeTestJSON(w, map[string]any{
				"device_code": "dc-1", "user_code": "ABCD-1234", "expires_in": float64(600), "interval": float64(1),
				"verification_uri":          "https://portal.test/manage-subscription",
				"verification_uri_complete": "https://portal.test/manage-subscription?user_code=ABCD-1234",
			})
		case "/api/oauth/token":
			if err := r.ParseForm(); err != nil {
				t.Errorf("parse form: %v", err)
			}
			switch r.PostForm.Get("grant_type") {
			case "urn:ietf:params:oauth:grant-type:device_code":
				if server.decline {
					w.WriteHeader(http.StatusBadRequest)
					writeTestJSON(w, map[string]any{"error": "access_denied"})
					return
				}
				server.polls++
				if server.polls < 2 {
					w.WriteHeader(http.StatusBadRequest)
					writeTestJSON(w, map[string]any{"error": "authorization_pending"})
					return
				}
				writeTestJSON(w, nousTokens("one", server.shortTTL))
			case "refresh_token":
				server.refreshes++
				// The refresh token goes in the header, and only the current one works.
				if server.refreshErr != "" || (server.wrongToken && r.Header.Get("x-nous-refresh-token") != "refresh-one") {
					w.WriteHeader(http.StatusBadRequest)
					errorCode := server.refreshErr
					if errorCode == "" {
						errorCode = "invalid_grant"
					}
					writeTestJSON(w, map[string]any{"error": errorCode})
					return
				}
				writeTestJSON(w, nousTokens("two", 3600))
			default:
				w.WriteHeader(http.StatusBadRequest)
				writeTestJSON(w, map[string]any{"error": "unsupported_grant_type"})
			}
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	return server
}

// writeTestJSON answers with a JSON body.
func writeTestJSON(w http.ResponseWriter, body any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}

// testOAuth builds the sign-in machinery against an environment.
func testOAuth(t *testing.T, env map[string]string, now *time.Time) *OAuth {
	t.Helper()
	clock := func() time.Time {
		if now == nil {
			return time.Now()
		}
		return *now
	}
	return NewOAuth(NewClient(Options{}), clock, func(name string) string { return env[name] })
}

func TestCodexDeviceSignIn(t *testing.T) {
	server := newCodexAuthServer(t)
	oauth := testOAuth(t, map[string]string{"NEOROUTER_CODEX_ISSUER": server.url}, nil)

	login, err := oauth.StartCodexLogin(context.Background())
	if err != nil {
		t.Fatalf("StartCodexLogin: %v", err)
	}
	if login.LoginID != "dev-1" || login.UserCode != "ABCD-1234" || login.Interval != 1 {
		t.Fatalf("login = %+v", login)
	}
	if !strings.HasSuffix(login.VerificationURL, "/codex/device") {
		t.Fatalf("verification url = %s", login.VerificationURL)
	}
	if login.ExpiresInSeconds != codexDeviceTTL.Seconds() {
		t.Fatalf("expires in = %v", login.ExpiresInSeconds)
	}
	poll, err := oauth.PollCodexLogin(context.Background(), login.LoginID)
	if err != nil {
		t.Fatalf("PollCodexLogin: %v", err)
	}
	if poll.Status != "pending" {
		t.Fatalf("first poll = %+v", poll)
	}
	poll, err = oauth.PollCodexLogin(context.Background(), login.LoginID)
	if err != nil {
		t.Fatalf("PollCodexLogin: %v", err)
	}
	if poll.Status != "done" {
		t.Fatalf("second poll = %+v", poll)
	}
	credentials, err := ParseCodexSecret(poll.Secret)
	if err != nil {
		t.Fatalf("ParseCodexSecret: %v", err)
	}
	if credentials.Access != "access-one" || credentials.Refresh != "refresh-one" {
		t.Fatalf("credentials = %+v", credentials)
	}
	if credentials.AccountID != "acct-123" || credentials.Plan == nil || *credentials.Plan != "plus" {
		t.Fatalf("account = %+v", credentials)
	}
	// The code is gone once it was used.
	again, err := oauth.PollCodexLogin(context.Background(), login.LoginID)
	if err != nil || again.Status != "expired" {
		t.Fatalf("poll after the sign-in finished = %+v, %v", again, err)
	}
}

func TestCodexSignInFailures(t *testing.T) {
	server := newCodexAuthServer(t)
	oauth := testOAuth(t, map[string]string{"NEOROUTER_CODEX_ISSUER": server.url}, nil)

	server.startCode = http.StatusNotFound
	if _, err := oauth.StartCodexLogin(context.Background()); err == nil || KindOf(err) != KindNotFound {
		t.Fatalf("err = %v", err)
	} else if err.Error() != msgCodexStartFailed {
		t.Fatalf("message = %q", err.Error())
	}
	server.startCode = http.StatusInternalServerError
	if _, err := oauth.StartCodexLogin(context.Background()); err == nil || KindOf(err) != KindServer {
		t.Fatalf("err = %v", err)
	}
	server.startCode = 0
	if poll, err := oauth.PollCodexLogin(context.Background(), "nobody"); err != nil || poll.Status != "expired" {
		t.Fatalf("unknown sign-in = %+v, %v", poll, err)
	}
	// The sign-in is refused at the token exchange.
	login, err := oauth.StartCodexLogin(context.Background())
	if err != nil {
		t.Fatalf("StartCodexLogin: %v", err)
	}
	server.refuse = true
	server.polls = 1
	if _, err := oauth.PollCodexLogin(context.Background(), login.LoginID); err == nil || KindOf(err) != KindAuth {
		t.Fatalf("err = %v", err)
	} else if err.Error() != msgCodexRefused {
		t.Fatalf("message = %q", err.Error())
	}
	// A login with no ChatGPT account behind it.
	server.refuse, server.noAccount = false, true
	server.polls = 1
	login, _ = oauth.StartCodexLogin(context.Background())
	if _, err := oauth.PollCodexLogin(context.Background(), login.LoginID); err == nil || err.Error() != msgCodexNoAccount {
		t.Fatalf("err = %v", err)
	}
	// Tokens that never arrived.
	server.noAccount, server.noTokens = false, true
	server.polls = 1
	login, _ = oauth.StartCodexLogin(context.Background())
	if _, err := oauth.PollCodexLogin(context.Background(), login.LoginID); err == nil || err.Error() != msgCodexNoTokens {
		t.Fatalf("err = %v", err)
	}
}

func TestCodexRefreshRotatesOnceAndPersists(t *testing.T) {
	server := newCodexAuthServer(t)
	now := time.Now()
	oauth := testOAuth(t, map[string]string{"NEOROUTER_CODEX_ISSUER": server.url}, &now)
	// A sign-in whose access token ends in a moment.
	secret := mustJSON(t, CodexCredentials{Access: "access-one", Refresh: "refresh-one", Expires: now.UnixMilli() + 1000})

	var persisted []string
	var mu sync.Mutex
	persist := func(value string) error {
		mu.Lock()
		defer mu.Unlock()
		persisted = append(persisted, value)
		return nil
	}
	var wait sync.WaitGroup
	for i := 0; i < 4; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			credentials, fresh, err := oauth.FreshCodexCredentials(context.Background(), secret, "acct-id", persist)
			if err != nil {
				t.Errorf("FreshCodexCredentials: %v", err)
				return
			}
			if credentials.Access != "access-two" || !strings.Contains(fresh, "access-two") {
				t.Errorf("credentials = %+v", credentials)
			}
		}()
	}
	wait.Wait()
	// One refresh however many callers asked: a refresh token is spent once.
	if server.refreshes != 1 {
		t.Fatalf("refreshes = %d", server.refreshes)
	}
	if len(persisted) != 1 || !strings.Contains(persisted[0], "refresh-two") {
		t.Fatalf("persisted = %+v", persisted)
	}
	// A token that is still good is used as it is.
	server.refreshes = 0
	healthy := mustJSON(t, CodexCredentials{Access: "access-one", Refresh: "refresh-one", Expires: now.Add(time.Hour).UnixMilli()})
	if _, fresh, err := oauth.FreshCodexCredentials(context.Background(), healthy, "acct-id", persist); err != nil || fresh != healthy {
		t.Fatalf("fresh = %q, %v", fresh, err)
	}
	if server.refreshes != 0 {
		t.Fatalf("a valid token was refreshed")
	}
	// An unsaved connection keeps the token it was given, even when it expired.
	if _, fresh, err := oauth.FreshCodexCredentials(context.Background(), secret, "", persist); err != nil || fresh != secret {
		t.Fatalf("unsaved = %q, %v", fresh, err)
	}
}

func TestParseCodexSecret(t *testing.T) {
	if _, err := ParseCodexSecret("not json"); err == nil || err.Error() != msgCodexBadSecret {
		t.Fatalf("err = %v", err)
	}
	if _, err := ParseCodexSecret(`{"access":"a"}`); err == nil || err.Error() != msgCodexBadSecret {
		t.Fatalf("a sign-in without a refresh token = %v", err)
	}
	credentials, err := ParseCodexSecret(`{"access":"a","refresh":"r","expires":5}`)
	if err != nil || credentials.Refresh != "r" {
		t.Fatalf("credentials = %+v, %v", credentials, err)
	}
}

func TestCodexClientVersion(t *testing.T) {
	releases := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Accept"); got != "application/vnd.github+json" {
			t.Errorf("accept = %q", got)
		}
		writeTestJSON(w, map[string]any{"tag_name": "rust-v0.170.2"})
	})
	now := time.Now()
	oauth := testOAuth(t, nil, &now)
	oauth.ReleasesURL = releases
	if version := oauth.CodexClientVersion(context.Background()); version != "0.170.2" {
		t.Fatalf("version = %s", version)
	}
	// The lookup is cached for a day.
	before := oauth.CodexClientVersion(context.Background())
	now = now.Add(23 * time.Hour)
	if version := oauth.CodexClientVersion(context.Background()); version != before {
		t.Fatalf("version = %s", version)
	}
	// Offline is not a failure: the documented fallback is recent enough.
	offline := testOAuth(t, nil, nil)
	offline.ReleasesURL = "http://127.0.0.1:9/nothing"
	if version := offline.CodexClientVersion(context.Background()); version != CodexFallbackVersion {
		t.Fatalf("fallback = %s", version)
	}
}

func TestAdapterForRenewsAndStoresASignedInSecret(t *testing.T) {
	server := newCodexAuthServer(t)
	now := time.Now()
	service, factory := testServiceWith(t, func(service *Service) {
		service.Getenv = func(name string) string {
			if name == "NEOROUTER_CODEX_ISSUER" {
				return server.url
			}
			return ""
		}
		service.Now = func() time.Time { return now }
	})
	expired := mustJSON(t, CodexCredentials{Access: "access-one", Refresh: "refresh-one", Expires: now.UnixMilli() - 1000})
	created := createAccount(t, service, "admin", "chatgpt", withSecret(expired))

	runtime, err := service.Runtime(context.Background(), created.Account.ID)
	if err != nil || runtime == nil {
		t.Fatalf("Runtime: %v", err)
	}
	if _, err := service.AdapterFor(context.Background(), runtime); err != nil {
		t.Fatalf("AdapterFor: %v", err)
	}
	built := factory.builtAccounts()
	// The last adapter built is the one AdapterFor made; the first was the key
	// check during Create.
	if len(built) < 2 || !strings.Contains(built[len(built)-1].Secret, "access-two") {
		t.Fatalf("the adapter was built with %+v", built)
	}
	row := accountRow(t, service, created.Account.ID)
	opened, err := service.Secrets.Open(*row.SecretEncrypted)
	if err != nil || !strings.Contains(opened, "refresh-two") {
		t.Fatalf("the rotated secret was not stored: %v", err)
	}
	if server.refreshes != 1 {
		t.Fatalf("refreshes = %d", server.refreshes)
	}
}

func TestNousDeviceSignIn(t *testing.T) {
	server := newNousPortalServer(t)
	oauth := testOAuth(t, map[string]string{"NEOROUTER_NOUS_PORTAL_URL": server.url + "/"}, nil)

	login, err := oauth.StartNousLogin(context.Background())
	if err != nil {
		t.Fatalf("StartNousLogin: %v", err)
	}
	if login.UserCode != "ABCD-1234" || login.LoginID != "dc-1" {
		t.Fatalf("login = %+v", login)
	}
	if !strings.HasSuffix(login.VerificationURL, "user_code=ABCD-1234") {
		t.Fatalf("verification url = %s", login.VerificationURL)
	}
	if login.ExpiresInSeconds != 600 {
		t.Fatalf("expires in = %v", login.ExpiresInSeconds)
	}
	poll, err := oauth.PollNousLogin(context.Background(), login.LoginID)
	if err != nil || poll.Status != "pending" {
		t.Fatalf("first poll = %+v, %v", poll, err)
	}
	poll, err = oauth.PollNousLogin(context.Background(), login.LoginID)
	if err != nil || poll.Status != "done" {
		t.Fatalf("second poll = %+v, %v", poll, err)
	}
	if poll.Email != "portal@example.test" || poll.Tier != "paid" {
		t.Fatalf("account = %+v", poll)
	}
	credentials, err := ParseNousSecret(poll.Secret)
	if err != nil {
		t.Fatalf("ParseNousSecret: %v", err)
	}
	if credentials.UserID != "user-1" || credentials.Email != "portal@example.test" || credentials.Tier != "paid" {
		t.Fatalf("credentials = %+v", credentials)
	}
	if credentials.Refresh == nil || *credentials.Refresh != "refresh-one" || credentials.Expires == nil {
		t.Fatalf("credentials = %+v", credentials)
	}
	if !NousIsSignIn(poll.Secret) {
		t.Fatalf("a signed-in connection is a sign-in")
	}
}

func TestNousSignInFailures(t *testing.T) {
	server := newNousPortalServer(t)
	oauth := testOAuth(t, map[string]string{"NEOROUTER_NOUS_PORTAL_URL": server.url}, nil)

	server.badClient = true
	_, err := oauth.StartNousLogin(context.Background())
	if err == nil || KindOf(err) != KindServer {
		t.Fatalf("err = %v", err)
	}
	if want := "Nous Portal did not start a sign-in: Unsupported OAuth client_id: hermes-cli."; err.Error() != want {
		t.Fatalf("message = %q", err.Error())
	}
	server.badClient = false
	if poll, err := oauth.PollNousLogin(context.Background(), "dc-unknown"); err != nil || poll.Status != "expired" {
		t.Fatalf("unknown sign-in = %+v, %v", poll, err)
	}
	// A sign-in declined in the Portal is gone afterwards.
	server.decline = true
	login, err := oauth.StartNousLogin(context.Background())
	if err != nil {
		t.Fatalf("StartNousLogin: %v", err)
	}
	if _, err := oauth.PollNousLogin(context.Background(), login.LoginID); err == nil || KindOf(err) != KindAuth {
		t.Fatalf("err = %v", err)
	} else if err.Error() != msgNousDeclined {
		t.Fatalf("message = %q", err.Error())
	}
	if poll, err := oauth.PollNousLogin(context.Background(), login.LoginID); err != nil || poll.Status != "expired" {
		t.Fatalf("a declined sign-in is gone: %+v, %v", poll, err)
	}
	server.decline = false

	// The Portal asks for a longer wait, then hands out the key.
	slow := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/oauth/device/code":
			_ = r.ParseForm()
			writeTestJSON(w, map[string]any{"device_code": "dc-2", "user_code": "WXYZ-1", "expires_in": float64(600)})
		case "/api/oauth/token":
			_ = r.ParseForm()
			if r.PostForm.Get("grant_type") == "refresh_token" {
				writeTestJSON(w, map[string]any{})
				return
			}
			w.WriteHeader(http.StatusBadRequest)
			writeTestJSON(w, map[string]any{"error": "slow_down"})
		}
	})
	slowOAuth := testOAuth(t, map[string]string{"NEOROUTER_NOUS_PORTAL_URL": slow}, nil)
	slowLogin, err := slowOAuth.StartNousLogin(context.Background())
	if err != nil {
		t.Fatalf("StartNousLogin: %v", err)
	}
	if !strings.HasSuffix(slowLogin.VerificationURL, "/manage-subscription") {
		t.Fatalf("verification url = %s", slowLogin.VerificationURL)
	}
	if poll, err := slowOAuth.PollNousLogin(context.Background(), slowLogin.LoginID); err != nil || poll.Status != "slow_down" {
		t.Fatalf("poll = %+v, %v", poll, err)
	}
}

func TestNousRefreshSpendsTheRotatedToken(t *testing.T) {
	server := newNousPortalServer(t)
	server.wrongToken = true
	now := time.Now()
	oauth := testOAuth(t, map[string]string{"NEOROUTER_NOUS_PORTAL_URL": server.url}, &now)
	refresh := "refresh-one"
	expired := now.UnixMilli() - 1000
	secret := mustJSON(t, NousCredentials{Access: "access-old", Refresh: &refresh, Expires: &expired, Email: "portal@example.test"})

	var persisted string
	credentials, fresh, err := oauth.FreshNousCredentials(context.Background(), secret, "acct", func(value string) error {
		persisted = value
		return nil
	})
	if err != nil {
		t.Fatalf("FreshNousCredentials: %v", err)
	}
	if credentials.Refresh == nil || *credentials.Refresh != "refresh-two" || !strings.Contains(fresh, "refresh-two") {
		t.Fatalf("credentials = %+v", credentials)
	}
	if persisted != fresh {
		t.Fatalf("the rotated token was not stored: %q", persisted)
	}
	if credentials.Email != "portal@example.test" {
		t.Fatalf("the account was lost: %+v", credentials)
	}
	// A revoked sign-in is terminal, a broken Portal is not.
	server.refreshErr = "refresh_token_reused"
	if _, _, err := oauth.FreshNousCredentials(context.Background(), secret, "other", nil); err == nil || KindOf(err) != KindAuth {
		t.Fatalf("err = %v", err)
	} else if err.Error() != msgNousRefreshRejected {
		t.Fatalf("message = %q", err.Error())
	}
	server.refreshErr = "boom"
	if _, _, err := oauth.FreshNousCredentials(context.Background(), secret, "other", nil); err == nil || KindOf(err) != KindServer {
		t.Fatalf("err = %v", err)
	} else if err.Error() != msgNousRefreshFailed {
		t.Fatalf("message = %q", err.Error())
	}
}

func TestNousPastedKeyIsNeverRefreshed(t *testing.T) {
	server := newNousPortalServer(t)
	oauth := testOAuth(t, map[string]string{"NEOROUTER_NOUS_PORTAL_URL": server.url}, nil)
	credentials, fresh, err := oauth.FreshNousCredentials(context.Background(), "  sk-pasted-key  ", "acct", func(string) error {
		t.Fatalf("a pasted key is not stored again")
		return nil
	})
	if err != nil {
		t.Fatalf("FreshNousCredentials: %v", err)
	}
	if credentials.Access != "sk-pasted-key" || fresh != "  sk-pasted-key  " {
		t.Fatalf("credentials = %+v, secret = %q", credentials, fresh)
	}
	if NousIsSignIn("sk-pasted-key") {
		t.Fatalf("a pasted key is not a sign-in")
	}
	if server.refreshes != 0 {
		t.Fatalf("a pasted key was refreshed")
	}
	if _, err := ParseNousSecret("{}"); err == nil || err.Error() != msgNousBadSecret {
		t.Fatalf("err = %v", err)
	}
}

func TestOAuthPostReadsRefusalsAsAnswers(t *testing.T) {
	url := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		writeTestJSON(w, map[string]any{"error": "invalid_grant", "error_description": "nope"})
	})
	oauth := testOAuth(t, nil, nil)
	status, data, err := oauth.post(context.Background(), url, map[string]any{"a": 1}, false, http.Header{})
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	if status != http.StatusBadRequest || data["error"] != "invalid_grant" {
		t.Fatalf("status = %d, data = %+v", status, data)
	}
	if _, _, err := oauth.post(context.Background(), "http://127.0.0.1:9/nothing", map[string]any{}, false, http.Header{}); err == nil {
		t.Fatalf("a transport failure is an error")
	}
}
