// Package oauth is "Connect with NeoRouter": NeoRouter acting as the OAuth 2.0
// provider apps talk to (authorization code with PKCE, and the device flow), so
// an app gets a router key without copy-paste, plus the OpenRouter PKCE flow the
// dashboard runs to connect an OpenRouter account.
//
// It serves app authorization (code and device flows) and the OpenRouter
// connect flow.
package oauth

import (
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/oauthclient"
	"github.com/NeoLabs-Systems/neorouter/internal/config"
)

// Service is the OAuth domain: the provider-side app authorization, the
// approval endpoints the dashboard calls, and the OpenRouter connection flow.
type Service struct {
	cfg      func() config.Config
	log      *slog.Logger
	keys     Keys
	accounts Accounts
	catalog  Catalog
	client   *oauthclient.Client
	guard    Guard
	now      func() time.Time

	// openRouterAuth is https://openrouter.ai/auth and openRouterExchange is
	// its /api/v1/auth/keys; tests point them at a fake provider.
	openRouterAuth     string
	openRouterExchange string

	mu       sync.Mutex
	requests map[string]*connectRequest
	codes    map[string]*issuedCode
	flows    map[string]*openRouterFlow
}

// Options configure a Service.
type Options struct {
	// Config returns the live configuration (public URL).
	Config func() config.Config
	// Log is the server logger; nil uses slog.Default().
	Log *slog.Logger
	// Keys makes the router keys an app receives; internal/routerkeys supplies it.
	Keys Keys
	// Accounts connects provider accounts; internal/providers supplies it.
	Accounts Accounts
	// Catalog refreshes a provider account's models; internal/catalog supplies it.
	Catalog Catalog
	// HTTP is the client for the OpenRouter token exchange; nil uses
	// http.DefaultClient.
	HTTP *http.Client
	// Guard resolves the session and space of a request. internal/app supplies
	// it from internal/auth and internal/teams.
	Guard Guard
	// Now is the clock; nil uses time.Now.
	Now func() time.Time
}

// New builds the OAuth domain.
func New(opts Options) *Service {
	s := &Service{
		cfg:                opts.Config,
		log:                opts.Log,
		keys:               opts.Keys,
		accounts:           opts.Accounts,
		catalog:            opts.Catalog,
		guard:              opts.Guard,
		now:                opts.Now,
		openRouterAuth:     openRouterAuthURL,
		openRouterExchange: openRouterExchangeURL,
		requests:           make(map[string]*connectRequest),
		codes:              make(map[string]*issuedCode),
		flows:              make(map[string]*openRouterFlow),
	}
	if s.cfg == nil {
		s.cfg = func() config.Config { return config.Config{} }
	}
	if s.log == nil {
		s.log = slog.Default()
	}
	if s.now == nil {
		s.now = time.Now
	}
	client := opts.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	s.client = &oauthclient.Client{HTTP: client, Service: "OpenRouter"}
	return s
}

// publicOrigin is this server's address as people reach it: the configured
// public URL, else the request's host. No trailing slash.
func (s *Service) publicOrigin(r *http.Request) string {
	cfg := s.cfg()
	origin := cfg.PublicURL
	if origin == "" {
		origin = requestProtocol(r, cfg.TrustProxy) + "://" + r.Host
	}
	return strings.TrimRight(origin, "/")
}

// requestProtocol is https when the request arrived over TLS, or else the first
// X-Forwarded-Proto value from a trusted proxy.
func requestProtocol(r *http.Request, trustProxy bool) string {
	if trustProxy {
		if forwarded := r.Header.Get("X-Forwarded-Proto"); forwarded != "" {
			if proto := strings.TrimSpace(strings.Split(forwarded, ",")[0]); proto != "" {
				return proto
			}
		}
	}
	if r.TLS != nil {
		return "https"
	}
	return "http"
}
