package providers

// Sign-in for the ChatGPT (Codex) connection, following the Codex CLI's own
// OAuth flow (openai/codex, codex-rs/login): a device code the user approves at
// auth.openai.com/codex/device, so it works on a server without a browser.
// Tokens are stored as the account secret and refreshed shortly before they
// expire (server/services/providers/codex_oauth.js).

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/llm/oauth"
)

// Codex sign-in facts.
const (
	// CodexClientID is the Codex CLI's own OAuth client.
	CodexClientID = "app_EMoamEEZ73f0CkXaXp7hrann"
	// codexAccountClaim is the JWT claim that names the ChatGPT account.
	codexAccountClaim = "https://api.openai.com/auth"
	// CodexDefaultIssuer is the OpenAI auth host; NEOROUTER_CODEX_ISSUER
	// replaces it (tests).
	CodexDefaultIssuer = "https://auth.openai.com"
	// codexRefreshMargin is how long before expiry a token is renewed.
	codexRefreshMargin = 5 * time.Minute
	// codexDeviceTTL is how long a started sign-in stays valid.
	codexDeviceTTL = 15 * time.Minute
	// CodexFallbackVersion is the Codex release used when GitHub cannot be read.
	CodexFallbackVersion = "0.165.0"
	// codexVersionTTL is how long the Codex release lookup is trusted.
	codexVersionTTL = 24 * time.Hour
	// oauthTimeout bounds one sign-in request.
	oauthTimeout = 20 * time.Second
)

// CodexReleasesURL is where the current Codex release is looked up.
const CodexReleasesURL = "https://api.github.com/repos/openai/codex/releases/latest"

// ProviderErrors the sign-in flows answer with.
const (
	msgCodexStartFailed     = "OpenAI did not start a device sign-in. Try again in a moment."
	msgCodexRefused         = "OpenAI refused the sign-in."
	msgCodexNoTokens        = "OpenAI did not issue tokens for this sign-in."
	msgCodexNoAccount       = "This OpenAI login has no ChatGPT account. Sign in with the account that has your ChatGPT plan."
	msgCodexBadSecret       = "This ChatGPT connection has no valid sign-in. Connect it again."
	msgCodexRefreshRejected = "The ChatGPT sign-in expired or was revoked. Connect it again."
)

// OAuth runs the device-code sign-ins of the signed-in providers and keeps
// their tokens fresh. It holds the pending-login and in-flight-refresh state the
// JS modules kept at module scope: one per server.
type OAuth struct {
	// Client is the HTTP layer the flows call through; nil builds a plain one.
	Client *Client
	// Now is the clock; nil means time.Now.
	Now func() time.Time
	// Getenv reads the issuer overrides; nil means os.Getenv.
	Getenv func(string) string
	// ReleasesURL is where the Codex release is looked up.
	ReleasesURL string

	mu           sync.Mutex
	codex        map[string]codexPending
	nous         map[string]nousPending
	codexFlights map[string]*flight[codexRefresh]
	nousFlights  map[string]*flight[nousRefresh]
	version      *codexVersion
}

// NewOAuth builds the sign-in machinery for one server.
func NewOAuth(client *Client, now func() time.Time, getenv func(string) string) *OAuth {
	return &OAuth{Client: client, Now: now, Getenv: getenv, ReleasesURL: CodexReleasesURL}
}

type codexPending struct {
	userCode  string
	expiresAt time.Time
}

type codexVersion struct {
	value string
	at    time.Time
}

// flight is one in-flight refresh a second caller waits for.
type flight[T any] struct {
	done  chan struct{}
	value T
	err   error
}

// flightDo runs fn once per key: a caller that arrives while a refresh is
// running waits for its result instead of spending the refresh token again.
func flightDo[T any](mu *sync.Mutex, calls map[string]*flight[T], key string, fn func() (T, error)) (T, error) {
	mu.Lock()
	if call, ok := calls[key]; ok {
		mu.Unlock()
		<-call.done
		return call.value, call.err
	}
	call := &flight[T]{done: make(chan struct{})}
	calls[key] = call
	mu.Unlock()

	call.value, call.err = fn()

	mu.Lock()
	delete(calls, key)
	mu.Unlock()
	close(call.done)
	return call.value, call.err
}

// now is the sign-in clock.
func (o *OAuth) now() time.Time {
	if o.Now != nil {
		return o.Now()
	}
	return time.Now()
}

// getenv reads an issuer override.
func (o *OAuth) getenv(name string) string {
	if o.Getenv != nil {
		return o.Getenv(name)
	}
	return os.Getenv(name)
}

// httpClient is the HTTP layer the flows use; a sign-in endpoint is a fixed,
// public address, so these calls carry no private-destination guard. It also
// brings up the pending-login and refresh state, so every flow starts here.
func (o *OAuth) httpClient() *Client {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.Client == nil {
		o.Client = NewClient(Options{Timeout: oauthTimeout})
	}
	if len(o.codex) == 0 {
		o.codex = map[string]codexPending{}
	}
	if len(o.nous) == 0 {
		o.nous = map[string]nousPending{}
	}
	if o.codexFlights == nil {
		o.codexFlights = map[string]*flight[codexRefresh]{}
	}
	if o.nousFlights == nil {
		o.nousFlights = map[string]*flight[nousRefresh]{}
	}
	return o.Client
}

// codexIssuer is the OpenAI auth host.
func (o *OAuth) codexIssuer() string {
	if value := strings.TrimSpace(o.getenv("NEOROUTER_CODEX_ISSUER")); value != "" {
		return value
	}
	return CodexDefaultIssuer
}

// post performs one sign-in request. Every answer is returned, because a
// refusal is what a poll reads; only a transport failure is an error.
func (o *OAuth) post(ctx context.Context, rawURL string, body any, form bool, headers http.Header) (int, map[string]any, error) {
	request := Request{Method: http.MethodPost, Headers: headers.Clone(), Timeout: oauthTimeout}
	if form {
		values, _ := body.(url.Values)
		request.Body = strings.NewReader(values.Encode())
		request.ContentType = "application/x-www-form-urlencoded"
	} else {
		request.JSON = body
	}
	response, err := o.httpClient().Request(ctx, rawURL, request)
	if err == nil {
		return response.Status, object(response.Data), nil
	}
	if classified := AsProviderError(err); classified != nil && classified.Status != 0 {
		return classified.Status, object(classified.Body), nil
	}
	return 0, nil, err
}

// CodexCredentials is a signed-in ChatGPT connection (the stored secret).
type CodexCredentials struct {
	Access  string `json:"access"`
	Refresh string `json:"refresh"`
	// Expires is when the access token ends, in unix milliseconds.
	Expires int64 `json:"expires"`
	// AccountID is the ChatGPT account the login belongs to.
	AccountID string `json:"accountId,omitempty"`
	// Plan is the ChatGPT plan the account is on, when the token names one.
	Plan *string `json:"plan"`
}

// secret renders the credentials as they are stored.
func (c CodexCredentials) secret() string {
	encoded, err := json.Marshal(c)
	if err != nil {
		return ""
	}
	return string(encoded)
}

// DeviceLogin is a started sign-in: the code the person enters at the
// verification URL.
type DeviceLogin struct {
	LoginID          string  `json:"loginId"`
	UserCode         string  `json:"userCode"`
	VerificationURL  string  `json:"verificationUrl"`
	Interval         int     `json:"interval"`
	ExpiresInSeconds float64 `json:"expiresInSeconds"`
}

// CodexPoll is one poll of a ChatGPT sign-in.
type CodexPoll struct {
	// Status is pending, expired or done.
	Status string
	// Secret is the credentials JSON to store, set when Status is done.
	Secret string
	// Plan is the ChatGPT plan, when the token names one.
	Plan *string
}

// StartCodexLogin starts a device-code sign-in: it returns the code the user
// enters at the verification URL.
func (o *OAuth) StartCodexLogin(ctx context.Context) (DeviceLogin, error) {
	o.httpClient()
	issuer := o.codexIssuer()
	status, data, err := o.post(ctx, issuer+"/api/accounts/deviceauth/usercode",
		map[string]any{"client_id": CodexClientID}, false, http.Header{})
	if err != nil {
		return DeviceLogin{}, err
	}
	deviceAuthID, _ := data["device_auth_id"].(string)
	if status < 200 || status >= 300 || deviceAuthID == "" {
		kind := KindServer
		if status == http.StatusNotFound {
			kind = KindNotFound
		}
		return DeviceLogin{}, NewError(kind, msgCodexStartFailed, ErrorOptions{Status: status})
	}
	userCode, _ := data["user_code"].(string)
	if userCode == "" {
		userCode, _ = data["usercode"].(string)
	}
	interval := 5
	if value, ok := numberOf(data["interval"]); ok && value != 0 {
		interval = int(value)
	}
	o.mu.Lock()
	o.codex[deviceAuthID] = codexPending{userCode: userCode, expiresAt: o.now().Add(codexDeviceTTL)}
	o.mu.Unlock()
	return DeviceLogin{
		LoginID:          deviceAuthID,
		UserCode:         userCode,
		VerificationURL:  issuer + "/codex/device",
		Interval:         interval,
		ExpiresInSeconds: codexDeviceTTL.Seconds(),
	}, nil
}

// PollCodexLogin is one poll of a started sign-in: pending until it is
// approved, then the credentials to store.
func (o *OAuth) PollCodexLogin(ctx context.Context, loginID string) (CodexPoll, error) {
	o.mu.Lock()
	pending, ok := o.codex[loginID]
	if ok && !pending.expiresAt.After(o.now()) {
		delete(o.codex, loginID)
		ok = false
	}
	o.mu.Unlock()
	if !ok {
		return CodexPoll{Status: "expired"}, nil
	}
	issuer := o.codexIssuer()
	status, data, err := o.post(ctx, issuer+"/api/accounts/deviceauth/token",
		map[string]any{"device_auth_id": loginID, "user_code": pending.userCode}, false, http.Header{})
	if err != nil {
		return CodexPoll{}, err
	}
	if status == http.StatusForbidden || status == http.StatusNotFound {
		return CodexPoll{Status: "pending"}, nil
	}
	authorizationCode, _ := data["authorization_code"].(string)
	if status < 200 || status >= 300 || authorizationCode == "" {
		return CodexPoll{}, NewError(KindAuth, msgCodexRefused, ErrorOptions{Status: status})
	}
	codeVerifier, _ := data["code_verifier"].(string)
	exchange := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {authorizationCode},
		"redirect_uri":  {issuer + "/deviceauth/callback"},
		"client_id":     {CodexClientID},
		"code_verifier": {codeVerifier},
	}
	status, tokens, err := o.post(ctx, issuer+"/oauth/token", exchange, true, http.Header{})
	o.mu.Lock()
	delete(o.codex, loginID)
	o.mu.Unlock()
	if err != nil {
		return CodexPoll{}, err
	}
	if access, _ := tokens["access_token"].(string); status < 200 || status >= 300 || access == "" {
		return CodexPoll{}, NewError(KindAuth, msgCodexNoTokens, ErrorOptions{Status: status})
	}
	credentials := codexCredentialsFrom(tokens, CodexCredentials{}, o.now())
	if credentials.AccountID == "" {
		return CodexPoll{}, NewError(KindAuth, msgCodexNoAccount, ErrorOptions{Status: status})
	}
	return CodexPoll{Status: "done", Secret: credentials.secret(), Plan: credentials.Plan}, nil
}

// codexCredentialsFrom reads a token answer, keeping what the previous
// credentials know when the answer is silent.
func codexCredentialsFrom(tokens map[string]any, previous CodexCredentials, now time.Time) CodexCredentials {
	accountID, plan := codexAccountOf(tokens)
	access, _ := tokens["access_token"].(string)
	refresh, _ := tokens["refresh_token"].(string)
	seconds, ok := numberOf(tokens["expires_in"])
	if !ok || seconds == 0 {
		seconds = 3600
	}
	next := CodexCredentials{
		Access:  access,
		Refresh: firstNonEmpty(refresh, previous.Refresh),
		Expires: now.UnixMilli() + int64(seconds*1000),
	}
	next.AccountID = firstNonEmpty(accountID, previous.AccountID)
	if plan != nil {
		next.Plan = plan
	} else {
		next.Plan = previous.Plan
	}
	return next
}

// codexAccountOf reads the ChatGPT account and plan out of the login's tokens.
func codexAccountOf(tokens map[string]any) (string, *string) {
	for _, name := range []string{"id_token", "access_token"} {
		token, _ := tokens[name].(string)
		claims := object(oauthJWTClaims(token)[codexAccountClaim])
		if claims == nil {
			continue
		}
		if accountID, ok := claims["chatgpt_account_id"].(string); ok && accountID != "" {
			var plan *string
			if value, ok := claims["chatgpt_plan_type"].(string); ok && value != "" {
				plan = &value
			}
			return accountID, plan
		}
	}
	return "", nil
}

// ParseCodexSecret reads a stored ChatGPT secret.
func ParseCodexSecret(secret string) (CodexCredentials, error) {
	var credentials CodexCredentials
	if err := json.Unmarshal([]byte(secret), &credentials); err == nil {
		if credentials.Access != "" && credentials.Refresh != "" {
			return credentials, nil
		}
	}
	return CodexCredentials{}, NewError(KindAuth, msgCodexBadSecret, ErrorOptions{})
}

// FreshCodexCredentials is a connection's valid credentials and the secret to
// use for them: the stored secret unless the tokens were rotated. accountID is
// empty for a connection that is not saved yet, whose tokens must not be
// refreshed (refresh tokens rotate, so a refresh that is not stored would leave
// the saved connection with a dead one). persist stores the rotated secret.
func (o *OAuth) FreshCodexCredentials(ctx context.Context, secret, accountID string, persist func(secret string) error) (CodexCredentials, string, error) {
	o.httpClient()
	current, err := ParseCodexSecret(secret)
	if err != nil {
		return CodexCredentials{}, "", err
	}
	if time.UnixMilli(current.Expires).Sub(o.now()) > codexRefreshMargin || accountID == "" {
		return current, secret, nil
	}
	flight, err := flightDo(&o.mu, o.codexFlights, accountID, func() (codexRefresh, error) {
		next, refreshErr := o.refreshCodex(ctx, current)
		if refreshErr != nil {
			return codexRefresh{}, refreshErr
		}
		rotated := next.secret()
		if persist != nil {
			if persistErr := persist(rotated); persistErr != nil {
				return codexRefresh{}, persistErr
			}
		}
		return codexRefresh{credentials: next, secret: rotated}, nil
	})
	if err != nil {
		return CodexCredentials{}, "", err
	}
	return flight.credentials, flight.secret, nil
}

// codexRefresh is one finished ChatGPT token refresh.
type codexRefresh struct {
	credentials CodexCredentials
	secret      string
}

// refreshCodex spends the refresh token.
func (o *OAuth) refreshCodex(ctx context.Context, current CodexCredentials) (CodexCredentials, error) {
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {current.Refresh},
		"client_id":     {CodexClientID},
	}
	status, data, err := o.post(ctx, o.codexIssuer()+"/oauth/token", form, true, http.Header{})
	if err != nil {
		return CodexCredentials{}, err
	}
	access, _ := data["access_token"].(string)
	if status < 200 || status >= 300 || access == "" {
		return CodexCredentials{}, NewError(KindAuth, msgCodexRefreshRejected, ErrorOptions{Status: status})
	}
	return codexCredentialsFrom(data, current, o.now()), nil
}

// CodexClientVersion is the Codex release a client reports. The models list
// depends on it (an old one sees almost nothing), so the current release is
// used, checked daily.
func (o *OAuth) CodexClientVersion(ctx context.Context) string {
	o.mu.Lock()
	cached := o.version
	o.mu.Unlock()
	if cached != nil && o.now().Sub(cached.at) < codexVersionTTL {
		return cached.value
	}
	value := CodexFallbackVersion
	releases := o.ReleasesURL
	if releases == "" {
		releases = CodexReleasesURL
	}
	response, err := o.httpClient().Request(ctx, releases, Request{
		Headers: http.Header{"Accept": {"application/vnd.github+json"}},
		Timeout: 10 * time.Second,
	})
	if err == nil {
		// Offline or rate-limited keeps the fallback, which is recent enough.
		if tag, ok := object(response.Data)["tag_name"].(string); ok {
			if match := versionPattern.FindStringSubmatch(tag); match != nil {
				value = match[1]
			}
		}
	}
	o.mu.Lock()
	o.version = &codexVersion{value: value, at: o.now()}
	o.mu.Unlock()
	return value
}

// versionPattern is the x.y.z of a release tag.
var versionPattern = regexp.MustCompile(`(\d+\.\d+\.\d+)`)

// oauthJWTClaims decodes a JWT payload, nil when the token is not one.
func oauthJWTClaims(token string) map[string]any {
	if token == "" {
		return nil
	}
	return oauth.JWTClaims(token)
}
