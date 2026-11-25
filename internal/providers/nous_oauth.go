package providers

// Sign-in for the Nous Portal connection: Portal's device flow
// (POST /api/oauth/device/code), which is the same flow Hermes Agent uses, so it
// works on a server without a browser. The access token it hands out is itself
// the inference JWT, so it is stored as the account secret and sent as the
// bearer. Refresh tokens are single-use and rotate on every refresh: reuse is
// read as theft and revokes the session, so a rotated token is written back to
// the account before the call that needed it returns
// (server/services/providers/nous_oauth.js).

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Nous sign-in facts.
const (
	// NousClientID is the client Portal's own device flow registers; it accepts
	// no other.
	NousClientID = "hermes-cli"
	// NousScope is the only scope this connection needs.
	NousScope = "inference:invoke"
	// NousDefaultPortal is the Portal host; NEOROUTER_NOUS_PORTAL_URL replaces
	// it (tests).
	NousDefaultPortal = "https://portal.nousresearch.com"
	// nousRefreshMargin is how long before expiry a token is renewed.
	nousRefreshMargin = 5 * time.Minute
	// nousDeviceTTL is how long a started sign-in stays valid.
	nousDeviceTTL = 15 * time.Minute
)

// ProviderErrors the Nous sign-in answers with.
const (
	msgNousStartFailed     = "Nous Portal did not start a device sign-in. Try again in a moment."
	msgNousDeclined        = "The sign-in was declined in Nous Portal."
	msgNousNoKey           = "Nous Portal did not hand out a key."
	msgNousBadSecret       = "This Nous Portal connection has no valid sign-in. Connect it again."
	msgNousRefreshRejected = "The Nous Portal sign-in expired or was revoked. Connect it again."
	msgNousRefreshFailed   = "Nous Portal did not refresh the sign-in. Try again in a moment."
)

// nousPending is one started sign-in.
type nousPending struct {
	deviceCode string
	userCode   string
	expiresAt  time.Time
}

// NousCredentials is a signed-in Nous Portal connection (the stored secret).
type NousCredentials struct {
	Access string `json:"access"`
	// Refresh is nil for a key pasted from the Portal's API-keys page, which
	// needs no refreshing.
	Refresh *string `json:"refresh"`
	// Expires is when the access token ends, in unix milliseconds.
	Expires *int64 `json:"expires"`
	UserID  string `json:"userId,omitempty"`
	Email   string `json:"email,omitempty"`
	Tier    string `json:"tier,omitempty"`
}

// secret renders the credentials as they are stored.
func (c NousCredentials) secret() string {
	encoded, err := json.Marshal(c)
	if err != nil {
		return ""
	}
	return string(encoded)
}

// NousPoll is one poll of a Nous Portal sign-in.
type NousPoll struct {
	// Status is pending, slow_down, expired or done.
	Status string
	// Secret is the credentials JSON to store, set when Status is done.
	Secret string
	Email  string
	Tier   string
}

// portalBase is the Portal host, without a trailing slash.
func (o *OAuth) portalBase() string {
	value := strings.TrimSpace(o.getenv("NEOROUTER_NOUS_PORTAL_URL"))
	if value == "" {
		value = NousDefaultPortal
	}
	return strings.TrimRight(value, "/")
}

// StartNousLogin starts a device-code sign-in: the code the user approves in
// Nous Portal.
func (o *OAuth) StartNousLogin(ctx context.Context) (DeviceLogin, error) {
	o.httpClient()
	portal := o.portalBase()
	form := url.Values{"client_id": {NousClientID}, "scope": {NousScope}}
	status, data, err := o.post(ctx, portal+"/api/oauth/device/code", form, true, http.Header{"Accept": {"application/json"}})
	if err != nil {
		return DeviceLogin{}, err
	}
	deviceCode, _ := data["device_code"].(string)
	if status < 200 || status >= 300 || deviceCode == "" {
		message := firstNonEmpty(stringMember(data, "error_description"), stringMember(data, "error"))
		kind := KindNetwork
		if status == http.StatusBadRequest && stringMember(data, "error") == "invalid_client" {
			kind = KindServer
		}
		if message != "" {
			return DeviceLogin{}, NewError(kind, "Nous Portal did not start a sign-in: "+message+".", ErrorOptions{Status: status})
		}
		return DeviceLogin{}, NewError(kind, msgNousStartFailed, ErrorOptions{Status: status})
	}
	userCode, _ := data["user_code"].(string)
	seconds := 600.0
	if value, ok := numberOf(data["expires_in"]); ok && value != 0 {
		seconds = value
	}
	expiresAt := o.now().Add(time.Duration(seconds * float64(time.Second)))
	loginID := deviceCode
	o.mu.Lock()
	o.nous[loginID] = nousPending{deviceCode: loginID, userCode: userCode, expiresAt: expiresAt}
	o.mu.Unlock()
	verification := firstNonEmpty(
		stringMember(data, "verification_uri_complete"),
		stringMember(data, "verification_uri"),
		portal+"/manage-subscription",
	)
	interval := 5
	if value, ok := numberOf(data["interval"]); ok && value != 0 {
		interval = int(value)
	}
	ttl := min(nousDeviceTTL, time.Duration(seconds*float64(time.Second)))
	return DeviceLogin{
		LoginID:          loginID,
		UserCode:         userCode,
		VerificationURL:  verification,
		Interval:         interval,
		ExpiresInSeconds: ttl.Seconds(),
	}, nil
}

// PollNousLogin is one poll of a started sign-in: pending (or slow_down, when
// the Portal asks for a longer interval) until it is approved.
func (o *OAuth) PollNousLogin(ctx context.Context, loginID string) (NousPoll, error) {
	o.mu.Lock()
	pending, ok := o.nous[loginID]
	if ok && !pending.expiresAt.After(o.now()) {
		delete(o.nous, loginID)
		ok = false
	}
	o.mu.Unlock()
	if !ok {
		return NousPoll{Status: "expired"}, nil
	}
	form := url.Values{
		"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
		"client_id":   {NousClientID},
		"device_code": {pending.deviceCode},
	}
	status, data, err := o.post(ctx, o.portalBase()+"/api/oauth/token", form, true, http.Header{"Accept": {"application/json"}})
	if err != nil {
		return NousPoll{}, err
	}
	if access, _ := data["access_token"].(string); status >= 200 && status < 300 && access != "" {
		o.mu.Lock()
		delete(o.nous, loginID)
		o.mu.Unlock()
		credentials := nousCredentialsFrom(data, NousCredentials{}, o.now())
		return NousPoll{Status: "done", Secret: credentials.secret(), Email: credentials.Email, Tier: credentials.Tier}, nil
	}
	code := stringMember(data, "error")
	switch code {
	case "authorization_pending", "network":
		return NousPoll{Status: "pending"}, nil
	case "slow_down":
		return NousPoll{Status: "slow_down"}, nil
	}
	o.mu.Lock()
	delete(o.nous, loginID)
	o.mu.Unlock()
	switch code {
	case "access_denied":
		return NousPoll{}, NewError(KindAuth, msgNousDeclined, ErrorOptions{Status: status})
	case "expired_token":
		return NousPoll{Status: "expired"}, nil
	}
	message := firstNonEmpty(stringMember(data, "error_description"), msgNousNoKey)
	return NousPoll{}, NewError(KindAuth, message, ErrorOptions{Status: status})
}

// nousCredentialsFrom reads a token answer, keeping what the previous
// credentials know when the answer is silent.
func nousCredentialsFrom(tokens map[string]any, previous NousCredentials, now time.Time) NousCredentials {
	access, _ := tokens["access_token"].(string)
	userID, email, tier := nousAccountOf(access)
	var expires *int64
	seconds, ok := numberOf(tokens["expires_in"])
	if !ok || seconds == 0 {
		seconds = 3600
	}
	value := now.UnixMilli() + int64(seconds*1000)
	expires = &value
	next := NousCredentials{
		Access:  access,
		Refresh: previous.Refresh,
		Expires: expires,
		UserID:  firstNonEmpty(userID, previous.UserID),
		Email:   firstNonEmpty(email, previous.Email),
		Tier:    firstNonEmpty(tier, previous.Tier),
	}
	// Portal rotates refresh tokens; none in the answer means keep the old one.
	if refresh, ok := tokens["refresh_token"].(string); ok && refresh != "" {
		next.Refresh = &refresh
	}
	return next
}

// nousAccountOf reads the account behind a Portal token from its claims.
func nousAccountOf(token string) (userID, email, tier string) {
	claims := oauthJWTClaims(token)
	if claims == nil {
		return "", "", ""
	}
	userID, _ = claims["sub"].(string)
	email, _ = claims["email"].(string)
	tier, _ = claims["account_tier"].(string)
	return userID, email, tier
}

// ParseNousSecret reads a stored Nous secret: the credentials of a signed-in
// connection, or a key pasted from the Portal's API-keys page (a plain string
// that is a bearer as it is, with nothing to refresh).
func ParseNousSecret(secret string) (NousCredentials, error) {
	trimmed := strings.TrimSpace(secret)
	if trimmed != "" && !strings.HasPrefix(trimmed, "{") {
		return NousCredentials{Access: trimmed}, nil
	}
	var credentials NousCredentials
	if err := json.Unmarshal([]byte(secret), &credentials); err == nil && credentials.Access != "" {
		return credentials, nil
	}
	return NousCredentials{}, NewError(KindAuth, msgNousBadSecret, ErrorOptions{})
}

// NousIsSignIn reports whether a stored secret is a Portal sign-in (refreshable
// tokens) rather than a key pasted from the API-keys page.
func NousIsSignIn(secret string) bool {
	credentials, err := ParseNousSecret(secret)
	return err == nil && credentials.Refresh != nil && *credentials.Refresh != ""
}

// FreshNousCredentials is a connection's valid credentials and the secret to
// use for them: the stored secret unless the tokens were rotated. accountID is
// empty for a connection that is not saved yet, whose tokens must not be
// refreshed (refresh tokens rotate, so a refresh that is not stored would leave
// the saved connection with a dead one). persist stores the rotated secret.
func (o *OAuth) FreshNousCredentials(ctx context.Context, secret, accountID string, persist func(secret string) error) (NousCredentials, string, error) {
	o.httpClient()
	current, err := ParseNousSecret(secret)
	if err != nil {
		return NousCredentials{}, "", err
	}
	if current.Refresh == nil || *current.Refresh == "" {
		return current, secret, nil
	}
	if current.Expires != nil && time.UnixMilli(*current.Expires).Sub(o.now()) > nousRefreshMargin {
		return current, secret, nil
	}
	if accountID == "" {
		return current, secret, nil
	}
	flight, err := flightDo(&o.mu, o.nousFlights, accountID, func() (nousRefresh, error) {
		next, refreshErr := o.refreshNous(ctx, current)
		if refreshErr != nil {
			return nousRefresh{}, refreshErr
		}
		rotated := next.secret()
		if persist != nil {
			if persistErr := persist(rotated); persistErr != nil {
				return nousRefresh{}, persistErr
			}
		}
		return nousRefresh{credentials: next, secret: rotated}, nil
	})
	if err != nil {
		return NousCredentials{}, "", err
	}
	return flight.credentials, flight.secret, nil
}

// nousRefresh is one finished Nous Portal token refresh.
type nousRefresh struct {
	credentials NousCredentials
	secret      string
}

// refreshNous spends the refresh token. Portal takes it in a header, not the
// body, and reads reuse of an old one as theft.
func (o *OAuth) refreshNous(ctx context.Context, current NousCredentials) (NousCredentials, error) {
	form := url.Values{"grant_type": {"refresh_token"}, "client_id": {NousClientID}}
	headers := http.Header{"Accept": {"application/json"}}
	if current.Refresh != nil {
		headers.Set("x-nous-refresh-token", *current.Refresh)
	}
	status, data, err := o.post(ctx, o.portalBase()+"/api/oauth/token", form, true, headers)
	if err != nil {
		return NousCredentials{}, err
	}
	if access, _ := data["access_token"].(string); status >= 200 && status < 300 && access != "" {
		return nousCredentialsFrom(data, current, o.now()), nil
	}
	code := stringMember(data, "error")
	switch code {
	case "invalid_grant", "refresh_token_reused", "revoked", "revoked_token", "expired_token":
		return NousCredentials{}, NewError(KindAuth, msgNousRefreshRejected, ErrorOptions{Status: status})
	}
	return NousCredentials{}, NewError(KindServer, msgNousRefreshFailed, ErrorOptions{Status: status})
}

// stringMember reads one string member of a decoded answer.
func stringMember(value map[string]any, name string) string {
	text, _ := value[name].(string)
	return text
}

// firstString is the first non-empty of two string members.
func firstString(values ...any) (string, bool) {
	for _, value := range values {
		if text, ok := value.(string); ok && text != "" {
			return text, true
		}
	}
	return "", false
}
