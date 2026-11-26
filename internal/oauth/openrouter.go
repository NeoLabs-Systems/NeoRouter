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
	// result is the dashboard redirect once the callback has been answered. A
	// browser can reach the callback twice (link prefetch, refresh, back); the
	// code is exchanged once and a repeat hit replays this same outcome.
	result    string
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
	s.log.Info("OpenRouter sign-in link issued", "state", state, "user", start.UserID)

	callback := strings.TrimRight(start.Origin, "/") + "/api/v1/oauth/openrouter/callback/" + state
	return s.openRouterAuth + "?callback_url=" + encodeURIComponent(callback) +
		"&code_challenge=" + pkce.Challenge + "&code_challenge_method=S256", nil
}

// CompleteOpenRouter finishes the flow and returns where the browser goes next:
// the dashboard, carrying oauth_status=connected or oauth_error. The one-time
// code is exchanged once; a repeat callback (link prefetch, refresh, back)
// replays the same outcome instead of reporting the link expired.
func (s *Service) CompleteOpenRouter(ctx context.Context, state, code string) (string, error) {
	now := s.now()
	s.mu.Lock()
	s.pruneFlows(now)
	flow, ok := s.flows[state]
	s.mu.Unlock()
	s.log.Info("OpenRouter callback", "state", state, "known", ok)

	if !ok {
		return "", httpserver.New(http.StatusBadRequest, "OAUTH_EXPIRED", "This sign-in link expired. Start the connection again.")
	}
	if flow.result != "" {
		return flow.result, nil
	}

	var target string
	var err error
	if code == "" {
		target, err = s.withResult(flow.returnTo, "oauth", "openrouter", "oauth_error", "cancelled")
	} else if accountID, exchangeErr := s.exchangeOpenRouter(ctx, flow, code); exchangeErr != nil {
		s.log.Warn("OpenRouter connection failed", "error", exchangeErr)
		target, err = s.withResult(flow.returnTo, "oauth", "openrouter", "oauth_error", shortMessage(exchangeErr.Error()))
	} else {
		if s.catalog != nil {
			if syncErr := s.catalog.SyncAccount(ctx, accountID); syncErr != nil {
				s.log.Warn("Initial model sync failed", "error", syncErr)
			}
		}
		target, err = s.withResult(flow.returnTo, "oauth", "openrouter", "oauth_status", "connected")
	}
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	if still, ok := s.flows[state]; ok {
		still.result = target
	}
	s.mu.Unlock()
	return target, nil
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
