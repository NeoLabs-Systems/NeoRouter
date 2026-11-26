package catalog

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/NeoLabs-Systems/neolabs-go/dbx"
	"github.com/NeoLabs-Systems/neolabs-go/ssrf"
	"github.com/NeoLabs-Systems/neorouter/internal/config"
)

// Settings is the slice of internal/settings the catalog consumes: the global
// setting holding the Artificial Analysis key, the per-user setting holding
// model presets, and the sealing of the key at rest.
type Settings interface {
	// Global returns a global setting; ok is false when it is not set.
	Global(key string) (value json.RawMessage, ok bool, err error)
	// SetGlobal stores (or, with nil, removes) a global setting.
	SetGlobal(key string, value any) error
	// UserSetting returns one user's setting; ok is false when it is not set.
	UserSetting(userID, key string) (value json.RawMessage, ok bool, err error)
	// SetUserSetting stores one user's setting.
	SetUserSetting(userID, key string, value any) error
	// Seal encrypts a secret for storage with the install's key material.
	Seal(plain string) (string, error)
	// Open decrypts a sealed secret, legacy values included.
	Open(sealed string) (string, error)
}

// ListedModel is one entry of a provider's live model list, normalized by the
// provider adapter (providers/model_info.js normalizeModel). Prices are USD
// per million tokens; PriceKnown is true only when the provider itself
// reported a price, FreeHint only when every reported price is exactly zero.
type ListedModel struct {
	ID              string
	Name            string
	Kind            string
	ContextWindow   *int64
	MaxOutput       *int64
	PriceKnown      bool
	PriceIn         *float64
	PriceOut        *float64
	PriceCachedIn   *float64
	PriceCacheWrite *float64
	FreeHint        bool
	PaidOnly        bool
	Tools           *bool
	Vision          *bool
	JSON            *bool
	Reasoning       *bool
	Decision        bool
	PriceUnit       string
	UnitPrice       *float64
	// OptIn marks a model the provider does not expose by default; a new
	// catalog row for it starts switched off.
	OptIn bool
}

// ModelLister lists the live models of one provider account through that
// account's adapter (services/providers account_service.adapterFor).
type ModelLister interface {
	// ListModels returns the account's models. ok is false when the account
	// has no runtime or no adapter, which turns a sync into a no-op exactly as
	// accounts.getRuntime returning null does.
	ListModels(ctx context.Context, accountID string) (models []ListedModel, ok bool, err error)
}

// Options are the dependencies of a catalog service. Everything the catalog
// does not own is injected; nothing here imports another domain.
type Options struct {
	DB          *dbx.DB
	Log         *slog.Logger
	Definitions Definitions
	Models      ModelLister
	Settings    Settings
	// Config returns the current configuration (metadataSourcesEnabled).
	Config func() config.Config
	// DataDir holds metadata_cache.json.
	DataDir string
	// HTTPClient fetches the public metadata sources. It must refuse private
	// destinations; a ssrf client is used when it is nil.
	HTTPClient *http.Client
	// ObserveNews lets a metadata refresh feed the discovery news service.
	ObserveNews func()
}

// Service is the model catalog.
type Service struct {
	DB     *dbx.DB
	Log    *slog.Logger
	Defs   Definitions
	Models ModelLister
	// Settings stores the Artificial Analysis key and model presets.
	Settings Settings
	// Cfg returns the current configuration.
	Cfg      func() config.Config
	Metadata *Metadata
}

// New builds the catalog service and its metadata indexes.
func New(opts Options) *Service {
	client := opts.HTTPClient
	if client == nil {
		client = ssrf.NewClient(ssrf.Options{Timeout: metadataTimeout})
	}
	svc := &Service{
		DB:       opts.DB,
		Log:      opts.Log,
		Defs:     opts.Definitions,
		Models:   opts.Models,
		Settings: opts.Settings,
		Cfg:      opts.Config,
	}
	if svc.Log == nil {
		svc.Log = slog.Default()
	}
	svc.Metadata = &Metadata{
		db:          opts.DB,
		client:      client,
		dataDir:     opts.DataDir,
		settings:    opts.Settings,
		cfg:         opts.Config,
		log:         svc.Log,
		observeNews: opts.ObserveNews,
	}
	return svc
}

// definition returns a provider definition, and false when it is unknown.
func (s *Service) definition(id string) (Definition, bool) {
	if s.Defs == nil {
		return Definition{}, false
	}
	return s.Defs.Definition(id)
}
