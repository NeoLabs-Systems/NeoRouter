// Package gateway owns NeoRouter's /v1 gateway: the OpenAI Chat Completions,
// OpenAI Responses and Anthropic Messages endpoints, the shared request
// pipeline the other kinds of model run through (run_capability), the model
// list, limits and feedback, and the playground chat behind the web app.
//
// Everything it needs from another domain is a field on Options: the routing,
// quota, budget, usage and router-key services are used directly, and provider
// accounts, request processing, pools, the search engines and the requests log
// are narrow func fields, so nothing here imports them.
package gateway

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/NeoLabs-Systems/neorouter/internal/budget"
	"github.com/NeoLabs-Systems/neorouter/internal/config"
	"github.com/NeoLabs-Systems/neorouter/internal/providers"
	"github.com/NeoLabs-Systems/neorouter/internal/quota"
	"github.com/NeoLabs-Systems/neorouter/internal/routerkeys"
	"github.com/NeoLabs-Systems/neorouter/internal/routing"
	"github.com/NeoLabs-Systems/neorouter/internal/routing/task"
	"github.com/NeoLabs-Systems/neorouter/internal/usage"
)

// Catalog is what the gateway reads from the model catalog. It is the same
// interface routing consumes, so one adapter satisfies both.
type Catalog interface {
	// ListForUser returns the user's models for one capability, in catalog
	// order.
	ListForUser(userID string, q routing.CatalogQuery) []*routing.Entry
	// CanonicalID normalises a model id to the identity shared across providers.
	CanonicalID(modelID string) string
}

// Adapters is the provider account service as the gateway uses it: which
// adapter speaks for an account, whether that adapter serves a capability, the
// account's upstream timeout, a key re-check after an authentication failure,
// and the live protocols its provider definition speaks.
type Adapters interface {
	// Adapter returns the adapter of an account, nil when the account has no
	// runtime (removed, disabled, or its provider is not connected).
	Adapter(accountID string) providers.Adapter
	// CanServe reports whether the account's adapter serves method.
	CanServe(accountID string, method providers.Capability) bool
	// Timeout is the account's upstream timeout (adapter.timeoutMs), 0 when
	// unknown, in which case the configured upstream timeout applies.
	Timeout(accountID string) time.Duration
	// Validate re-checks an account's key; failures are logged by the caller's
	// callee, never returned here.
	Validate(accountID string)
	// LiveProtocols are the live dialects the account's definition speaks
	// (openai_live, openai_realtime, gemini_live).
	LiveProtocols(accountID string) []string
}

// Options build a Service. Routing, Catalog, Keys and Concurrency are required;
// every other dependency is optional and its absence disables the part that
// needs it (tests supply fakes).
type Options struct {
	// Cfg returns the current configuration (read per request).
	Cfg func() config.Config
	Log *slog.Logger

	// Routing is the learning router: plans, learning, capacity, live events.
	Routing *routing.Service
	// Catalog lists the models a key may use (GET /v1/models).
	Catalog Catalog
	// Quota counts requests and tokens against providers' allowances.
	Quota *quota.Service
	// Budget reserves, settles and prices requests for a key.
	Budget *budget.Service
	// Usage records one row per request (metadata only, never prompts).
	Usage *usage.Service
	// Keys authenticates router keys and enforces their per-minute limit.
	Keys *routerkeys.Keys
	// Concurrency enforces a key's concurrency cap.
	Concurrency *routerkeys.Concurrency
	// Adapters is the provider account service.
	Adapters Adapters

	// --- optional cross-domain hooks (nil disables the behaviour) -----------

	// ApplyProcessing is request processing's apply (compression and the
	// privacy shield): it takes the canonical request and returns the one that
	// may go upstream, an opaque vault for putting values back, and what it did.
	ApplyProcessing func(ctx context.Context, userID string, canonical *Obj, opts ProcessingOptions) (ProcessingResult, error)
	// RestoreResponse puts shielded values back into a canonical completion.
	RestoreResponse func(completion *Obj, vault any) *Obj
	// NewStreamRestorer returns a restorer that puts shielded values back into a
	// stream, or nil when there is no vault.
	NewStreamRestorer func(vault any) StreamRestorer

	// PoolAccess reports whether the key may use pool partners and whether the
	// owner keeps very sensitive requests private (peer_privacy).
	PoolAccess func(key *Key) PoolPermission
	// AssessSensitivity judges how sensitive a request is (processing/
	// sensitivity); only used when the owner keeps such requests private.
	AssessSensitivity func(canonical *Obj) (level string, reasons []string)
	// ShareAdmit admits a pool partner's request within its share, or refuses it
	// with the error the partner is answered; nil refuses and reserves nothing.
	ShareAdmit func(key *Key) (*Share, error)
	// SharingWith reports whether this pool is offered to a partner's key
	// (share_policy.sharingWith); nil offers what is listed.
	SharingWith func(key *Key) bool
	// PoolOffers reports whether a partner's pool offers one model
	// (share_policy.offers); nil offers every model in reach.
	PoolOffers func(key *Key, entry *routing.Entry) bool
	// IsPeerEntry reports whether a catalog entry belongs to a pool partner.
	IsPeerEntry func(entry *routing.Entry) bool
	// RecordPoolTraffic counts a finished request of a pool partner.
	RecordPoolTraffic func(key *Key, accountID string, success bool, tokens int64)
	// NotePeerReachability reports a partner connection's health to the pool's
	// reachability tracker.
	NotePeerReachability func(accountID string, success bool, kind string)

	// ObserveCost records what a provider says it charged for an answer.
	ObserveCost func(entry *routing.Entry, cost float64)

	// SearchWeb runs one web search through the search engines the owner
	// connected (capabilities/media handlers.searchWeb): routing, fallback and
	// quotas over the "search" kind.
	SearchWeb func(ctx context.Context, call SearchCall) (*providers.SearchResult, error)

	// AnswerForRequest finds a successful answer of this key (the requests row
	// /v1/feedback names).
	AnswerForRequest func(requestID, keyID string) (*Answer, error)
	// RecordFeedback teaches routing how a model did on a kind of task.
	RecordFeedback func(modelRef int64, task string, good bool)
	// RecordKeyFailure records why a request with a valid key failed early
	// (apps/key_activity), for the app's "waiting for you" screen.
	RecordKeyFailure func(keyID string, status int, code, message, model string)

	// --- management routes (the web app) ------------------------------------

	// RequirePermission gates a management route (middleware/space). Optional:
	// without it the playground route is mounted ungated, which only tests want.
	RequirePermission func(permission string) func(http.Handler) http.Handler
	// Space is the caller's space (X-NeoRouter-Space), whose owner's key the
	// playground uses.
	Space func(ctx context.Context) (Space, bool)
}

// Service is the gateway: one value per running server, no globals.
type Service struct {
	opts Options
	// retries remembers the last answer to a conversation, so the same question
	// right after an answer teaches routing that the answer was no good.
	retries *task.RetryTracker
}

// New builds the gateway.
func New(opts Options) *Service {
	if opts.Log == nil {
		opts.Log = slog.Default()
	}
	return &Service{opts: opts, retries: task.NewRetryTracker()}
}

// cfg is the current configuration.
func (s *Service) cfg() config.Config {
	if s.opts.Cfg == nil {
		return config.Config{}
	}
	return s.opts.Cfg()
}

// Key is one router key as a gateway request sees it: the stored row plus the
// routing view of it.
type Key struct {
	Row     *routerkeys.Row
	Routing routing.RouterKey
}

// ID is the key's id.
func (k *Key) ID() string {
	if k == nil || k.Row == nil {
		return ""
	}
	return k.Row.ID
}

// UserID is the key owner's id.
func (k *Key) UserID() string {
	if k == nil || k.Row == nil {
		return ""
	}
	return k.Row.UserID
}

// Name is the key's name.
func (k *Key) Name() string {
	if k == nil || k.Row == nil {
		return ""
	}
	return k.Row.Name
}

// PeerLinkID is the pool partner link the key belongs to, "" for an own key.
func (k *Key) PeerLinkID() string {
	if k == nil || k.Row == nil || k.Row.PeerLinkID == nil {
		return ""
	}
	return *k.Row.PeerLinkID
}

// IsPeer reports whether the key belongs to a pool partner.
func (k *Key) IsPeer() bool { return k.PeerLinkID() != "" }

// keyFromRow makes a Key from a stored row.
func keyFromRow(row *routerkeys.Row) *Key {
	if row == nil {
		return nil
	}
	return &Key{Row: row, Routing: routerKeyOf(row)}
}

// routerKeyOf is the routing view of a stored router key.
func routerKeyOf(row *routerkeys.Row) routing.RouterKey {
	key := routing.RouterKey{
		ID:                 row.ID,
		UserID:             row.UserID,
		Name:               row.Name,
		Mode:               row.Mode,
		BudgetUSD:          row.BudgetUSD,
		BudgetPeriod:       row.BudgetPeriod,
		AllowedModelsJSON:  row.AllowedModels,
		Profile:            row.Profile,
		TokenPeriod:        row.TokenPeriod,
		Priority:           row.Priority,
		MaxPricePerMillion: row.MaxPricePerMil,
		PoolAccess:         row.PoolAccess,
		RoutingGroup:       stringOrEmpty(row.RoutingGroup),
	}
	if row.RPMLimit != nil {
		limit := int(*row.RPMLimit)
		key.RPMLimit = &limit
	}
	if row.ConcurrentLimit != nil {
		limit := int(*row.ConcurrentLimit)
		key.ConcurrentLimit = &limit
	}
	if row.TokenLimit != nil {
		limit := float64(*row.TokenLimit)
		key.TokenLimit = &limit
	}
	if row.PeerLinkID != nil {
		key.PeerLinkID = *row.PeerLinkID
	}
	// The stored flags are 1/0; nil means "allowed".
	allowFree := row.AllowFree
	allowPlan := row.AllowPlan
	key.AllowFree = &allowFree
	key.AllowPlan = &allowPlan
	return key
}

// stringOrEmpty is a *string's value, "" when nil.
func stringOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// ProcessingOptions are what request processing needs to know about a request.
type ProcessingOptions struct {
	// CompressionLevel is the X-NeoRouter-Compression request header, "" when
	// the request did not name one.
	CompressionLevel string
	// PoolMinimum asks for at least the pool's shield when the request may reach
	// a partner.
	PoolMinimum bool
}

// ProcessingResult is what request processing did.
type ProcessingResult struct {
	// Canonical is the request that may go upstream.
	Canonical *Obj
	// Vault puts the shielded values back; opaque to the gateway.
	Vault any
	// Redacted is how many values the shield hid.
	Redacted int64
	// CompressionLevel is the level applied ("" and "off" both mean none).
	CompressionLevel string
	// SavedTokens is what compression saved.
	SavedTokens int64
}

// StreamRestorer puts shielded values back into a stream of chunks.
type StreamRestorer interface {
	// Process rewrites one chunk.
	Process(chunk *Obj) *Obj
	// Flush returns the chunk that releases held-back values, nil when none.
	Flush(template *Obj) *Obj
}

// PoolPermission is what a key may do with the pool.
type PoolPermission struct {
	MayUse bool
	// KeepSensitivePrivate keeps very sensitive requests on own connections.
	KeepSensitivePrivate bool
}

// Share is one pool partner's admitted share of this instance.
type Share struct {
	Headers map[string]string
	Reserve *float64
	PoolID  string
	// Release ends the request: served says whether this instance answered it.
	Release func(served bool)
}

// SearchCall is one web search the gateway runs for a chat request.
type SearchCall struct {
	Key            *Key
	FreeOnly       bool
	Format         string
	Query          string
	MaxResults     int
	IncludeDomains []string
	ExcludeDomains []string
}

// Answer is a successful answer a request id names (/v1/feedback).
type Answer struct {
	ModelRef   int64
	ModelLabel string
	Task       string
}
