// Package media owns NeoRouter's non-chat capabilities: embeddings, image
// generation, speech, transcription, System One (moderation and decision
// models), asynchronous video jobs, web search and weather. Their OpenAI-shaped
// endpoints live under /v1 and the dashboard playground reaches the same
// handlers with one of the person's stored keys, so both behave identically.
//
// Every request goes through the shared pipeline (Runner): routing, fallback
// across providers, learning, quotas, budget and the usage log are the same
// machinery chat uses. What this package adds is the request validation, the
// per-kind glue (embedding pins, voice fallback, subtitles, audio transcoding)
// and the response shapes.
package media

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/dbx"
	"github.com/NeoLabs-Systems/neorouter/internal/catalog"
	"github.com/NeoLabs-Systems/neorouter/internal/config"
	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
	"github.com/NeoLabs-Systems/neorouter/internal/providers"
	"github.com/NeoLabs-Systems/neorouter/internal/routerkeys"
)

// Space is the request's resolved space (internal/teams): the account whose
// keys, providers and usage the playground works on.
type Space struct {
	OwnerID string
	ActorID string
}

// Options are the dependencies of the media capability.
type Options struct {
	DB  *dbx.DB
	Log *slog.Logger
	Now func() time.Time
	// Config reports the server configuration (public URL for generated media
	// links, body limits).
	Config func() config.Config
	// Catalog lists one kind of model a person can route to (internal/catalog
	// ListForUser); includeUnavailable keeps the models switched off right now.
	Catalog func(ctx context.Context, userID, capability string, includeUnavailable bool) ([]catalog.Entry, error)
	// Accounts resolves the adapter of a provider account.
	Accounts Accounts
	// Runner is the shared pipeline every request goes through.
	Runner Runner
	// Shield is the privacy shield text leaving for a provider passes; nil lets
	// every text through.
	Shield Shield
	// KeyRow reads one of a person's router keys (internal/routerkeys Row), for
	// the playground's stored-key modes.
	KeyRow func(userID, keyID string) (*routerkeys.Row, error)
	// KeyAuth is the router-key middleware the /v1 routes sit behind.
	KeyAuth KeyAuth
	// Require wraps a handler with the permission check (internal/teams).
	Require func(permission string) func(http.Handler) http.Handler
	// Space resolves the request's space (internal/teams).
	Space func(ctx context.Context) (Space, bool)
	// HTTP fetches a generated image the provider only returned a URL for; nil
	// uses the default client.
	HTTP *http.Client
}

// Service is the media capability.
type Service struct {
	db       *dbx.DB
	log      *slog.Logger
	now      func() time.Time
	config   func() config.Config
	catalog  func(ctx context.Context, userID, capability string, includeUnavailable bool) ([]catalog.Entry, error)
	accounts Accounts
	runner   Runner
	shield   Shield
	keyRow   func(userID, keyID string) (*routerkeys.Row, error)
	keyAuth  KeyAuth
	require  func(permission string) func(http.Handler) http.Handler
	space    func(ctx context.Context) (Space, bool)
	http     *http.Client
	store    *mediaStore
}

// New builds the media capability.
func New(options Options) *Service {
	service := &Service{
		db:       options.DB,
		log:      options.Log,
		now:      options.Now,
		config:   options.Config,
		catalog:  options.Catalog,
		accounts: options.Accounts,
		runner:   options.Runner,
		shield:   options.Shield,
		keyRow:   options.KeyRow,
		keyAuth:  options.KeyAuth,
		require:  options.Require,
		space:    options.Space,
		http:     options.HTTP,
		store:    newMediaStore(),
	}
	if service.log == nil {
		service.log = slog.Default()
	}
	if service.now == nil {
		service.now = time.Now
	}
	service.store.now = func() int64 { return service.now().UnixMilli() }
	return service
}

// SetClock replaces the clock (tests).
func (s *Service) SetClock(now func() time.Time) {
	if now != nil {
		s.now = now
	}
}

// settings is the server configuration, zero when no accessor was wired.
func (s *Service) settings() config.Config {
	if s.config == nil {
		return config.Config{}
	}
	return s.config()
}

// models lists one kind of model a person can route to.
func (s *Service) models(ctx context.Context, userID, capability string) ([]catalog.Entry, error) {
	if s.catalog == nil {
		return nil, nil
	}
	return s.catalog(ctx, userID, capability, false)
}

// adapterFor is the adapter of one provider account, or nil.
func (s *Service) adapterFor(accountID string) providers.Adapter {
	if s.accounts == nil {
		return nil
	}
	return s.accounts.Adapter(accountID)
}

// client is the HTTP client used for a URL a provider handed back.
func (s *Service) client() *http.Client {
	if s.http != nil {
		return s.http
	}
	return http.DefaultClient
}

// shieldTexts runs the texts through the privacy shield. A shield failure stops
// the request: text that was not checked does not leave.
func (s *Service) shieldTexts(userID string, texts []string) (Shielded, error) {
	if s.shield == nil {
		return Shielded{Texts: texts}, nil
	}
	shielded, err := s.shield(userID, texts)
	if err != nil {
		return Shielded{}, httpserver.New(http.StatusInternalServerError, "PRIVACY_SHIELD_FAILED",
			"The privacy shield could not check this request, so it was not sent.")
	}
	return shielded, nil
}

// requestSpace is the space a playground request works on.
func (s *Service) requestSpace(r *http.Request) (Space, error) {
	if s.space == nil {
		return Space{}, httpserver.New(http.StatusUnauthorized, "AUTHENTICATION_REQUIRED", "A valid session is required.")
	}
	space, ok := s.space(r.Context())
	if !ok {
		return Space{}, httpserver.New(http.StatusUnauthorized, "AUTHENTICATION_REQUIRED", "A valid session is required.")
	}
	return space, nil
}

// permission wraps the playground routes with their permission check.
func (s *Service) permission(handler http.HandlerFunc) http.Handler {
	if s.require == nil {
		return handler
	}
	return s.require("playground.use")(handler)
}

// invalid is the 400 INVALID_REQUEST the API reports for a request body that
// does not satisfy its schema.
func invalid(message string) error {
	return httpserver.New(http.StatusBadRequest, "INVALID_REQUEST", message)
}
