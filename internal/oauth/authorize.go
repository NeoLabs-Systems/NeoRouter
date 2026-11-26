package oauth

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/NeoLabs-Systems/neolabs-go/oauthclient"
	"github.com/NeoLabs-Systems/neolabs-go/passhash"
	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
)

// Lifetimes and pacing of the two provider flows, from app_authorization.js.
const (
	requestTTL            = 10 * time.Minute
	codeTTL               = 2 * time.Minute
	deviceIntervalSeconds = 5
	userCodeAlphabet      = "BCDFGHJKLMNPQRSTVWXZ"
	// maxClientID is how long an app's client_id may be.
	maxClientID = 200
)

// Scopes lists what an app may say it will use: the gateway's capabilities plus
// live voice.
var Scopes = []string{"chat", "embedding", "image", "speech", "transcription", "live", "moderation", "video", "search", "weather"}

// codeChallenge is a PKCE S256 challenge: 43 to 128 base64url characters.
var codeChallenge = regexp.MustCompile(`^[A-Za-z0-9_-]{43,128}$`)

// unsafeSchemes run code (or read this machine) in a browser, so they are never
// a redirect target.
var unsafeSchemes = map[string]bool{"javascript": true, "data": true, "file": true, "vbscript": true, "blob": true, "about": true}

// connectRequest is one authorization the dashboard can approve: the code flow
// (a browser callback) or the device flow (a typed code).
type connectRequest struct {
	id          string
	kind        string // "code" or "device"
	clientID    string
	name        string
	redirectURI string
	redirect    *redirectInfo
	challenge   string
	state       *string
	scopes      []string
	expiresAt   time.Time
	status      string // pending, approved or denied

	// device flow
	deviceCodeHash string
	userCode       string
	interval       int
	lastPoll       time.Time
	approval       *approval
}

// approval is what the person approving decided the key may use.
type approval struct {
	ownerID string
	actorID string
	access  access
}

// issuedCode is an approved code-flow request waiting for its exchange.
type issuedCode struct {
	request   *connectRequest
	approval  *approval
	expiresAt time.Time
}

// redirectInfo is where the key goes, as the approval screen shows it.
type redirectInfo struct {
	Host   string `json:"host"`
	Secure bool   `json:"secure"`
}

// describeRequest is what the approval screen shows.
type describeRequest struct {
	ID        string        `json:"id"`
	Kind      string        `json:"kind"`
	Name      string        `json:"name"`
	ClientID  string        `json:"clientId"`
	Redirect  *redirectInfo `json:"redirect"`
	Scopes    []string      `json:"scopes"`
	ExpiresAt string        `json:"expiresAt"`
}

// approveResult is where the browser goes next; the device flow answers null.
type approveResult struct {
	Redirect *string `json:"redirect"`
}

// deviceCodeResponse is what an app without a browser callback receives.
type deviceCodeResponse struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int64  `json:"expires_in"`
	Interval                int    `json:"interval"`
}

// tokenResponse is the key an app receives for its code.
type tokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   *int64 `json:"expires_in,omitempty"`
	Scope       string `json:"scope"`
	BaseURL     string `json:"base_url"`
	KeyID       string `json:"key_id"`
}

// metadata is GET /.well-known/oauth-authorization-server.
type metadata struct {
	Issuer                            string   `json:"issuer"`
	AuthorizationEndpoint             string   `json:"authorization_endpoint"`
	TokenEndpoint                     string   `json:"token_endpoint"`
	DeviceAuthorizationEndpoint       string   `json:"device_authorization_endpoint"`
	ResponseTypesSupported            []string `json:"response_types_supported"`
	GrantTypesSupported               []string `json:"grant_types_supported"`
	CodeChallengeMethodsSupported     []string `json:"code_challenge_methods_supported"`
	TokenEndpointAuthMethodsSupported []string `json:"token_endpoint_auth_methods_supported"`
	ScopesSupported                   []string `json:"scopes_supported"`
	APIBaseURL                        string   `json:"api_base_url"`
}

// prune forgets expired requests, codes and flows.
func (s *Service) prune() {
	now := s.now()
	for id, item := range s.requests {
		if !item.expiresAt.After(now) {
			delete(s.requests, id)
		}
	}
	for code, item := range s.codes {
		if !item.expiresAt.After(now) {
			delete(s.codes, code)
		}
	}
	s.pruneFlows(now)
}

// isLoopback reports whether a host name only this computer can reach.
func isLoopback(hostname string) bool {
	switch hostname {
	case "localhost", "127.0.0.1", "::1", "[::1]":
		return true
	}
	return false
}

// parseRedirect accepts any absolute URL without a fragment, except schemes that
// run code. Custom schemes (mobile apps) are allowed; PKCE makes an intercepted
// code useless.
func parseRedirect(value string) (*url.URL, bool) {
	if strings.TrimSpace(value) == "" {
		return nil, false
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" {
		return nil, false
	}
	if unsafeSchemes[strings.ToLower(parsed.Scheme)] || parsed.Fragment != "" {
		return nil, false
	}
	return parsed, true
}

// describeRedirect says where a key will go, in the app's own terms. Plain
// http beyond this machine can be read on the network.
func describeRedirect(parsed *url.URL) *redirectInfo {
	web := parsed.Scheme == "http" || parsed.Scheme == "https"
	info := &redirectInfo{
		Host:   parsed.Scheme,
		Secure: parsed.Scheme != "http" || isLoopback(parsed.Hostname()),
	}
	if web {
		info.Host = parsed.Host
	}
	return info
}

// parseScopes keeps the scopes an app asked for that NeoRouter knows, once each.
func parseScopes(value string) []string {
	asked := strings.FieldsFunc(value, func(r rune) bool { return r == ',' || unicode.IsSpace(r) })
	scopes := make([]string, 0, len(asked))
	seen := make(map[string]bool, len(asked))
	for _, scope := range asked {
		if !knownScope(scope) || seen[scope] {
			continue
		}
		seen[scope] = true
		scopes = append(scopes, scope)
	}
	return scopes
}

func knownScope(scope string) bool {
	for _, known := range Scopes {
		if known == scope {
			return true
		}
	}
	return false
}

// appName is the name the approval screen shows: the app's own, else its id.
func appName(clientID, clientName string) string {
	name := clientName
	if name == "" {
		name = clientID
	}
	name = strings.Join(strings.Fields(name), " ")
	if len(name) > 80 {
		name = string([]rune(name)[:80])
	}
	return name
}

// userCode is the short code a person types: four characters, a dash, four.
func userCode() (string, error) {
	raw := make([]byte, 8)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	chars := make([]byte, 8)
	for i := 0; i < 8; i++ {
		chars[i] = userCodeAlphabet[int(raw[i])%len(userCodeAlphabet)]
	}
	return string(chars[:4]) + "-" + string(chars[4:]), nil
}

// normalizeUserCode is what a typed code looks like after stripping everything
// that is not a letter.
func normalizeUserCode(value string) string {
	upper := strings.ToUpper(value)
	var b strings.Builder
	for _, r := range upper {
		if r >= 'A' && r <= 'Z' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// startAuthorization validates an authorization request (GET /oauth/authorize).
// Errors the app should hear about come back as the redirect that carries them;
// a bad redirect_uri never redirects to the app at all.
func (s *Service) startAuthorization(query url.Values) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prune()

	redirect, ok := parseRedirect(query.Get("redirect_uri"))
	if !ok {
		return "", newOAuthError("invalid_request", "redirect_uri must be an absolute URL.")
	}
	clientID := strings.TrimSpace(query.Get("client_id"))
	fail := func(code, description string) (string, error) {
		params := []param{{"error", &code}, {"error_description", &description}}
		if state := query.Get("state"); state != "" {
			params = append(params, param{"state", &state})
		}
		target, err := withParams(redirect.String(), params...)
		if err != nil {
			return "", fmt.Errorf("oauth: build error redirect: %w", err)
		}
		return target, nil
	}
	if clientID == "" || len(clientID) > maxClientID {
		return fail("invalid_request", "client_id is required.")
	}
	if query.Get("response_type") != "code" {
		return fail("unsupported_response_type", "Only response_type=code is supported.")
	}
	if !codeChallenge.MatchString(query.Get("code_challenge")) {
		return fail("invalid_request", "A PKCE code_challenge is required.")
	}
	method := query.Get("code_challenge_method")
	if method == "" {
		method = "S256"
	}
	if method != "S256" {
		return fail("invalid_request", "Only code_challenge_method=S256 is supported.")
	}

	id, err := passhash.RandomToken(18)
	if err != nil {
		return "", fmt.Errorf("oauth: create request id: %w", err)
	}
	request := &connectRequest{
		id:          id,
		kind:        "code",
		clientID:    clientID,
		name:        appName(clientID, query.Get("client_name")),
		redirectURI: redirect.String(),
		redirect:    describeRedirect(redirect),
		challenge:   query.Get("code_challenge"),
		scopes:      parseScopes(query.Get("scope")),
		expiresAt:   s.now().Add(requestTTL),
		status:      "pending",
	}
	if state := query.Get("state"); state != "" {
		if len(state) > 1024 {
			state = string([]rune(state)[:1024])
		}
		request.state = &state
	}
	s.requests[id] = request
	return "/app/?connect=" + encodeURIComponent(id), nil
}

// startDevice starts a device-flow request (POST /oauth/device/code).
func (s *Service) startDevice(body map[string]string, origin string) (*deviceCodeResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prune()

	clientID := strings.TrimSpace(body["client_id"])
	if clientID == "" || len(clientID) > maxClientID {
		return nil, newOAuthError("invalid_request", "client_id is required.")
	}
	id, err := passhash.RandomToken(18)
	if err != nil {
		return nil, fmt.Errorf("oauth: create request id: %w", err)
	}
	deviceCode, err := passhash.RandomToken(32)
	if err != nil {
		return nil, fmt.Errorf("oauth: create device code: %w", err)
	}
	code, err := userCode()
	if err != nil {
		return nil, fmt.Errorf("oauth: create user code: %w", err)
	}
	s.requests[id] = &connectRequest{
		id:             id,
		kind:           "device",
		clientID:       clientID,
		name:           appName(clientID, body["client_name"]),
		deviceCodeHash: passhash.DigestToken(deviceCode),
		userCode:       normalizeUserCode(code),
		scopes:         parseScopes(body["scope"]),
		expiresAt:      s.now().Add(requestTTL),
		interval:       deviceIntervalSeconds,
		status:         "pending",
	}
	app := origin + "/app/"
	return &deviceCodeResponse{
		DeviceCode:              deviceCode,
		UserCode:                code,
		VerificationURI:         app + "?connect=",
		VerificationURIComplete: app + "?connect=" + encodeURIComponent(id),
		ExpiresIn:               int64(requestTTL / time.Second),
		Interval:                deviceIntervalSeconds,
	}, nil
}

// find looks a request up by its id (from the link) or by the code a user typed.
func (s *Service) find(reference string) *connectRequest {
	if item, ok := s.requests[reference]; ok {
		return item
	}
	typed := normalizeUserCode(reference)
	if len(typed) != 8 {
		return nil
	}
	for _, item := range s.requests {
		if item.kind == "device" && item.userCode == typed {
			return item
		}
	}
	return nil
}

// pending is the request a reference names, if it can still be answered.
func (s *Service) pending(reference string) (*connectRequest, error) {
	item := s.find(reference)
	if item == nil {
		return nil, httpserver.New(404, "CONNECT_REQUEST_NOT_FOUND",
			"This connection request expired or does not exist. Start it again in the app.")
	}
	if item.status != "pending" {
		return nil, httpserver.New(409, "CONNECT_REQUEST_ANSWERED", "This connection request was already answered.")
	}
	return item, nil
}

// describe reports what the approval screen shows for a reference.
func (s *Service) describe(reference string) (*describeRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prune()

	item, err := s.pending(reference)
	if err != nil {
		return nil, err
	}
	return &describeRequest{
		ID:        item.id,
		Kind:      item.kind,
		Name:      item.name,
		ClientID:  item.clientID,
		Redirect:  item.redirect,
		Scopes:    item.scopes,
		ExpiresAt: oauthclient.ISO(item.expiresAt),
	}, nil
}

// approve accepts a request: the code flow gets its redirect, and the key is
// only made when the app exchanges the code, so abandoned approvals leave
// nothing behind.
func (s *Service) approve(reference, ownerID, actorID string, access access) (*approveResult, error) {
	if s.keys == nil {
		return nil, errors.New("oauth: router keys are not available")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prune()

	item, err := s.pending(reference)
	if err != nil {
		return nil, err
	}
	// Validates the options now, so the app never receives a failing code.
	if err := s.keys.Modes(access.AllowFree, access.AllowPlan, access.AllowPaid); err != nil {
		return nil, err
	}
	if err := s.keys.AssertExpiry(access.ExpiresAt); err != nil {
		return nil, err
	}
	grant := &approval{ownerID: ownerID, actorID: actorID, access: access}
	if item.kind == "device" {
		item.status = "approved"
		item.approval = grant
		return &approveResult{}, nil
	}
	delete(s.requests, item.id)
	code, err := passhash.RandomToken(32)
	if err != nil {
		return nil, fmt.Errorf("oauth: create code: %w", err)
	}
	s.codes[code] = &issuedCode{request: item, approval: grant, expiresAt: s.now().Add(codeTTL)}
	target, err := withParams(item.redirectURI, param{"code", &code}, param{"state", item.state})
	if err != nil {
		return nil, fmt.Errorf("oauth: build approve redirect: %w", err)
	}
	return &approveResult{Redirect: &target}, nil
}

// deny refuses a request.
func (s *Service) deny(reference string) (*approveResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prune()

	item, err := s.pending(reference)
	if err != nil {
		return nil, err
	}
	if item.kind == "device" {
		item.status = "denied"
		return &approveResult{}, nil
	}
	delete(s.requests, item.id)
	description := "The request was declined."
	errorCode := "access_denied"
	target, err := withParams(item.redirectURI,
		param{"error", &errorCode}, param{"error_description", &description}, param{"state", item.state})
	if err != nil {
		return nil, fmt.Errorf("oauth: build deny redirect: %w", err)
	}
	return &approveResult{Redirect: &target}, nil
}

// exchange answers POST /oauth/token.
func (s *Service) exchange(ctx context.Context, body map[string]string, origin string) (*tokenResponse, error) {
	switch body["grant_type"] {
	case "authorization_code":
		return s.exchangeCode(body, origin)
	case "urn:ietf:params:oauth:grant-type:device_code":
		return s.exchangeDevice(body, origin)
	default:
		return nil, newOAuthError("unsupported_grant_type", "Use authorization_code or the device_code grant.")
	}
}

func (s *Service) exchangeCode(body map[string]string, origin string) (*tokenResponse, error) {
	s.mu.Lock()
	code := body["code"]
	entry, ok := s.codes[code]
	delete(s.codes, code)
	now := s.now()
	s.mu.Unlock()

	if !ok || !entry.expiresAt.After(now) {
		return nil, newOAuthError("invalid_grant", "The code is invalid or expired.")
	}
	request := entry.request
	if body["client_id"] != request.clientID {
		return nil, newOAuthError("invalid_grant", "client_id does not match the authorization request.")
	}
	if body["redirect_uri"] != request.redirectURI {
		return nil, newOAuthError("invalid_grant", "redirect_uri does not match the authorization request.")
	}
	verifier := body["code_verifier"]
	if verifier == "" || !passhash.Equal(oauthclient.CodeChallenge(verifier), request.challenge) {
		return nil, newOAuthError("invalid_grant", "The code_verifier does not match.")
	}
	return s.issueKey(request, entry.approval, origin)
}

func (s *Service) exchangeDevice(body map[string]string, origin string) (*tokenResponse, error) {
	s.mu.Lock()
	s.prune()

	hash := passhash.DigestToken(body["device_code"])
	var item *connectRequest
	for _, entry := range s.requests {
		if entry.kind == "device" && entry.deviceCodeHash == hash {
			item = entry
			break
		}
	}
	if item == nil {
		s.mu.Unlock()
		return nil, newOAuthError("expired_token", "The device code expired. Start again.")
	}
	if body["client_id"] != item.clientID {
		s.mu.Unlock()
		return nil, newOAuthError("invalid_grant", "client_id does not match the device request.")
	}
	switch item.status {
	case "denied":
		delete(s.requests, item.id)
		s.mu.Unlock()
		return nil, newOAuthError("access_denied", "The request was declined.")
	case "pending":
		now := s.now()
		early := !item.lastPoll.IsZero() && now.Sub(item.lastPoll) < time.Duration(item.interval)*time.Second
		item.lastPoll = now
		if early {
			item.interval += 5
			s.mu.Unlock()
			return nil, newOAuthError("slow_down", "Polling too often.")
		}
		s.mu.Unlock()
		return nil, newOAuthError("authorization_pending", "Waiting for the user to approve.")
	}
	delete(s.requests, item.id)
	approval := item.approval
	s.mu.Unlock()
	return s.issueKey(item, approval, origin)
}

// issueKey makes the router key an approved request asked for.
func (s *Service) issueKey(request *connectRequest, grant *approval, origin string) (*tokenResponse, error) {
	if s.keys == nil {
		return nil, errors.New("oauth: router keys are not available")
	}
	access := grant.access
	keyRequest := KeyRequest{
		Name:         request.name,
		AllowFree:    access.AllowFree,
		AllowPlan:    access.AllowPlan,
		AllowPaid:    access.AllowPaid,
		BudgetPeriod: access.BudgetPeriod,
		RPMLimit:     access.RPMLimit,
		TokenPeriod:  access.TokenPeriod,
		ExpiresAt:    access.ExpiresAt,
	}
	if access.AllowPaid {
		keyRequest.BudgetUSD = access.BudgetUSD
		keyRequest.MaxPricePerMillion = access.MaxPricePerMillion
	}
	if access.AllowFree || access.AllowPlan {
		keyRequest.TokenLimit = access.TokenLimit
	}
	var appOrigin *string
	if request.redirect != nil {
		host := request.redirect.Host
		appOrigin = &host
	}
	key, err := s.keys.Create(grant.ownerID, keyRequest, grant.actorID, AppInfo{
		Name:   request.name,
		Origin: appOrigin,
		Scopes: request.scopes,
	})
	if err != nil {
		return nil, err
	}
	response := &tokenResponse{
		AccessToken: key.Secret,
		TokenType:   "Bearer",
		Scope:       strings.Join(request.scopes, " "),
		BaseURL:     origin + "/v1",
		KeyID:       key.ID,
	}
	if key.ExpiresAt != nil && *key.ExpiresAt != "" {
		expiresIn := expiresInSeconds(*key.ExpiresAt, s.now())
		response.ExpiresIn = &expiresIn
	}
	return response, nil
}

// expiresInSeconds is how long the key still works, never negative.
func expiresInSeconds(expiresAt string, now time.Time) int64 {
	at, err := time.Parse(time.RFC3339Nano, expiresAt)
	if err != nil {
		return 0
	}
	seconds := at.Sub(now) / time.Second
	if seconds < 0 {
		return 0
	}
	return int64(seconds)
}

// metadata is what the server says about itself to OAuth libraries.
func (s *Service) metadata(origin string) *metadata {
	return &metadata{
		Issuer:                            origin,
		AuthorizationEndpoint:             origin + "/oauth/authorize",
		TokenEndpoint:                     origin + "/oauth/token",
		DeviceAuthorizationEndpoint:       origin + "/oauth/device/code",
		ResponseTypesSupported:            []string{"code"},
		GrantTypesSupported:               []string{"authorization_code", "urn:ietf:params:oauth:grant-type:device_code"},
		CodeChallengeMethodsSupported:     []string{"S256"},
		TokenEndpointAuthMethodsSupported: []string{"none"},
		ScopesSupported:                   Scopes,
		// NeoRouter extension: where issued keys are used.
		APIBaseURL: origin + "/v1",
	}
}

// param is one query parameter to set on a redirect. A nil value is left out,
// as app_authorization.js skips null and undefined.
type param struct {
	name  string
	value *string
}

// withParams sets each non-nil parameter on uri like URL.searchParams.set.
func withParams(uri string, params ...param) (string, error) {
	var query oauthclient.Query
	for _, item := range params {
		if item.value != nil {
			query = append(query, [2]string{item.name, *item.value})
		}
	}
	return oauthclient.SetParams(uri, query)
}

// encodeURIComponent is JavaScript's encodeURIComponent, which the dashboard
// links use: a space is %20, and !'()*-._~ stay as they are.
func encodeURIComponent(value string) string {
	const unreserved = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_.!~*'()"
	var b strings.Builder
	for i := 0; i < len(value); i++ {
		c := value[i]
		if strings.IndexByte(unreserved, c) >= 0 {
			b.WriteByte(c)
			continue
		}
		const hex = "0123456789ABCDEF"
		b.WriteByte('%')
		b.WriteByte(hex[c>>4])
		b.WriteByte(hex[c&15])
	}
	return b.String()
}
