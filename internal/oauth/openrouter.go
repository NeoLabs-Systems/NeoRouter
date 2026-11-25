package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/cli/oauthcli"
	"github.com/NeoLabs-Systems/neolabs-go/oauthclient"
	"github.com/NeoLabs-Systems/neolabs-go/passhash"
	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
)

// OpenRouter's PKCE flow: the user approves in the browser, OpenRouter redirects
// back with a one-time code, and the server swaps it for an API key that is
// stored like any manually entered key. No key ever passes the UI.
const (
	openRouterAuthURL     = "https://openrouter.ai/auth"
	openRouterExchangeURL = "https://openrouter.ai/api/v1/auth/keys"
	openRouterFlowTTL     = 10 * time.Minute
	openRouterTimeout     = 20 * time.Second
	// maxOAuthErrorLength is how much of a failure the dashboard shows.
	maxOAuthErrorLength = 200
)

// openRouterFlow is one connection started from the dashboard, waiting for the
// browser to come back.
type openRouterFlow struct {
	userID    string
	createdBy string
	verifier  string
	returnTo  string
	expiresAt time.Time
}

// OpenRouterStart is the input of POST /api/v1/oauth/openrouter/start.
type OpenRouterStart struct {
	UserID      string
	CreatedBy   string
	Origin      string
	RequestHost string
	ReturnTo    string
}

// openRouterKey is what the token exchange answers: a key, or why not.
type openRouterKey struct {
	Key   string `json:"key"`
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
}

// pruneFlows forgets expired connection flows. Callers hold the lock.
func (s *Service) pruneFlows(now time.Time) {
	for state, flow := range s.flows {
		if !flow.expiresAt.After(now) {
			delete(s.flows, state)
		}
	}
}

// safeReturnTo only ever redirects back into this app, never to an arbitrary
// site. It returns the normalised URL and whether it was accepted.
func safeReturnTo(returnTo, requestHost string) (string, bool) {
	parsed, err := url.Parse(returnTo)
	if err != nil {
		return "", false
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", false
	}
	if isLoopback(parsed.Hostname()) || parsed.Host == requestHost {
		return parsed.String(), true
	}
	return "", false
}

// StartOpenRouter begins the flow and returns the URL to send the browser to.
// UserID owns the new account; CreatedBy is who connected it (a team member
// working in the owner's space, or the owner).
func (s *Service) StartOpenRouter(start OpenRouterStart) (string, error) {
	createdBy := start.CreatedBy
	if createdBy == "" {
		createdBy = start.UserID
	}
	target, ok := safeReturnTo(start.ReturnTo, start.RequestHost)
	if !ok {
		return "", httpserver.New(http.StatusBadRequest, "INVALID_RETURN_URL", "The return URL must point to this NeoRouter instance.")
	}
	state, err := passhash.RandomToken(18)
	if err != nil {
		return "", fmt.Errorf("oauth: create state: %w", err)
	}
	// The same verifier and S256 challenge openrouter_oauth.js makes: 48 random
	// bytes, base64url, hashed.
	pkce := oauthcli.NewPKCE()
	now := s.now()
	s.mu.Lock()
	s.pruneFlows(now)
	s.flows[state] = &openRouterFlow{
		userID:    start.UserID,
		createdBy: createdBy,
		verifier:  pkce.Verifier,
		returnTo:  target,
		expiresAt: now.Add(openRouterFlowTTL),
	}
	s.mu.Unlock()

	callback := strings.TrimRight(start.Origin, "/") + "/api/v1/oauth/openrouter/callback/" + state
	return s.openRouterAuth + "?callback_url=" + encodeURIComponent(callback) +
		"&code_challenge=" + pkce.Challenge + "&code_challenge_method=S256", nil
}

// CompleteOpenRouter finishes the flow and returns where the browser goes next:
// the dashboard, carrying oauth_status=connected or oauth_error.
func (s *Service) CompleteOpenRouter(ctx context.Context, state, code string) (string, error) {
	now := s.now()
	s.mu.Lock()
	s.pruneFlows(now)
	flow, ok := s.flows[state]
	delete(s.flows, state)
	s.mu.Unlock()

	if !ok {
		return "", httpserver.New(http.StatusBadRequest, "OAUTH_EXPIRED", "This sign-in link expired. Start the connection again.")
	}
	if code == "" {
		return s.withResult(flow.returnTo, "oauth", "openrouter", "oauth_error", "cancelled")
	}
	accountID, err := s.exchangeOpenRouter(ctx, flow, code)
	if err != nil {
		s.log.Warn("OpenRouter connection failed", "error", err)
		return s.withResult(flow.returnTo, "oauth", "openrouter", "oauth_error", shortMessage(err.Error()))
	}
	if s.catalog != nil {
		if err := s.catalog.SyncAccount(ctx, accountID); err != nil {
			s.log.Warn("Initial model sync failed", "error", err)
		}
	}
	return s.withResult(flow.returnTo, "oauth", "openrouter", "oauth_status", "connected")
}

// exchangeOpenRouter swaps the one-time code for an API key and stores it as an
// account of its own.
func (s *Service) exchangeOpenRouter(ctx context.Context, flow *openRouterFlow, code string) (string, error) {
	response, err := s.client.Do(ctx, oauthclient.Request{
		Method: http.MethodPost,
		URL:    s.openRouterExchange,
		JSON: map[string]string{
			"code":                  code,
			"code_verifier":         flow.verifier,
			"code_challenge_method": "S256",
		},
		Timeout: openRouterTimeout,
		NoRetry: true,
	})
	if err != nil {
		return "", err
	}
	var payload openRouterKey
	// A body that is not JSON is "no key", like response.json().catch(() => ({})).
	_ = json.Unmarshal(response.Body, &payload)
	if response.Status < 200 || response.Status >= 300 || payload.Key == "" {
		if payload.Error.Message != "" {
			return "", errors.New(payload.Error.Message)
		}
		return "", fmt.Errorf("OpenRouter returned HTTP %d", response.Status)
	}
	if s.accounts == nil {
		return "", errors.New("oauth: provider accounts are not available")
	}
	return s.accounts.CreateAccount(ctx, flow.userID, NewAccount{
		DefinitionID: "openrouter",
		Secret:       payload.Key,
		BillingMode:  "paid",
	}, flow.createdBy)
}

// withResult sets each parameter on the dashboard URL like openrouter_oauth.js
// withResult: a key already present is replaced, a new one appended.
func (s *Service) withResult(returnTo string, kv ...string) (string, error) {
	var params oauthclient.Query
	for i := 0; i+1 < len(kv); i += 2 {
		params = append(params, [2]string{kv[i], kv[i+1]})
	}
	target, err := oauthclient.SetParams(returnTo, params)
	if err != nil {
		return "", fmt.Errorf("oauth: build return URL: %w", err)
	}
	return target, nil
}

// shortMessage is a failure the dashboard may show, cut to something that fits
// a query parameter.
func shortMessage(message string) string {
	runes := []rune(message)
	if len(runes) > maxOAuthErrorLength {
		return string(runes[:maxOAuthErrorLength])
	}
	return message
}
