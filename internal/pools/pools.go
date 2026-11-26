// Package pools is NeoRouter's federation domain: pools of people on separate
// NeoRouters who share their spare capacity.
//
// A person may be in several pools; each has its own partners, sharing, caps and
// pause switch. Control goes through the host (who created the pool): it keeps
// the member list and the invites. Every two members of a pool are linked
// (links.go): directly when both have a public address, and through the host
// when one of them has none (relay_*.go: sealed end to end, so the host only
// passes it on). Relaying is an optional extra on protocol v2: NeoRouters
// without it ignore the new fields and leave members that have only a relay
// address out.
//
// Everything that belongs to another domain is a func field or a small
// interface here, so this package imports only the foundation (dbx, httpx,
// httpserver, config, teams, providers' HTTP layer) and the app wiring
// satisfies the rest.
package pools

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/dbx"
	"github.com/NeoLabs-Systems/neorouter/internal/config"
	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
	"github.com/NeoLabs-Systems/neorouter/internal/providers"
)

// Protocol and limit constants.
const (
	// InviteTTL is how long an invite link works.
	InviteTTL = 7 * 24 * time.Hour
	// MaxActiveInvites is how many open invites a pool may have at once.
	MaxActiveInvites = 20
	// MaxMembers is how many people one pool holds.
	MaxMembers = 20
	// MaxPools is how many pools one person may be in.
	MaxPools = 10
	// TicketTTL is how long an introduction ticket stays valid.
	TicketTTL = 10 * time.Minute
	// ProbeTimeout is how long a joiner's own address is tried before the host
	// relays it instead.
	ProbeTimeout = 4 * time.Second
	// RelayReadyTimeout is how long a joiner waits for its connection to the
	// host before introducing itself.
	RelayReadyTimeout = 8 * time.Second
	// PeerTimeout bounds one control call to another NeoRouter.
	PeerTimeout = 15 * time.Second
	// PeerDefinition is the provider definition a partner link's outbound
	// connection uses.
	PeerDefinition = "neorouter_peer"
	// PeerUserAgent is what this NeoRouter calls another with.
	PeerUserAgent = "NeoRouter-Pool"
)

// Patterns the pool code checks values against.
var (
	// tokenText is the shape of an invite token (TOKEN_PATTERN).
	tokenText = regexp.MustCompile(`^[A-Za-z0-9_-]{16,128}$`)
	// memberIDText is the shape of a member id (a lower-case UUID).
	memberIDText = regexp.MustCompile(`(?i)^[0-9a-f-]{36}$`)
	// publicKeyText is the shape of a base64 Ed25519 public key.
	publicKeyText = regexp.MustCompile(`^[A-Za-z0-9+/=]{40,200}$`)
	// peerKeyText is the shape of a partner's router key (nrt_<8 hex>_<secret>).
	peerKeyText = regexp.MustCompile(`^nrt_[a-f0-9]{8}_[A-Za-z0-9_-]{20,100}$`)
)

// HttpError is the error every pool endpoint answers with.
type HttpError = httpserver.HttpError

// features offered on protocol v2: relaying.
var poolFeatures = []string{"relay"}

// mustRegexp compiles a pattern the package relies on.
func mustRegexp(pattern string) *regexp.Regexp { return regexp.MustCompile(pattern) }

// fail is the error every pool endpoint answers with.
func fail(status int, code, message string) *httpserver.HttpError {
	return httpserver.New(status, code, message)
}

// RouterKeyIssuer issues the inbound router key a partner link calls us with.
// The wiring satisfies it with routerkeys.Keys.Create: lowest priority, free
// models only, the "balanced" profile, named "Pool: <partner>".
type RouterKeyIssuer interface {
	CreatePoolKey(ctx context.Context, userID, name string) (keyID string, secret string, err error)
}

// PeerAccount is the outbound connection of one partner link.
type PeerAccount struct {
	Label   string
	Slug    string
	BaseURL string
	Secret  string
}

// PeerAccounts keeps the connections partner links use, in both directions.
// The wiring satisfies it with the providers-accounts service.
type PeerAccounts interface {
	// AddPoolPeer adds the partner as a connection and returns its id.
	AddPoolPeer(ctx context.Context, userID string, peer PeerAccount) (accountID string, err error)
	// Remove deletes a connection this person owns.
	Remove(ctx context.Context, userID, accountID string) error
	// Validate checks a connection's key and reachability again.
	Validate(ctx context.Context, accountID string) error
	// PublicOnly reports whether this person's calls may only reach public
	// addresses (the instance admin's own accounts may reach a private network).
	PublicOnly(ctx context.Context, userID string) bool
}

// QuotaState is one model's quota picture (quota.Service.Evaluate), reduced to
// what the share policy reads.
type QuotaState struct {
	Headroom float64
	Details  []QuotaDetail
}

// QuotaDetail is one window of the quota picture.
type QuotaDetail struct {
	Metric    string
	Remaining *float64
}

// Model is one catalog entry as the share policy judges it.
type Model struct {
	Ref          int64
	AccountID    string
	UserID       string
	DefinitionID string
	IsFree       bool
	IsPlan       bool
}

// IsPeer reports whether the model belongs to a pool partner's connection:
// partner work never goes on to another partner.
func (m Model) IsPeer() bool { return m.DefinitionID == PeerDefinition }

// Service owns NeoRouter's pools: the pool page, the federation endpoints other
// NeoRouters call, and the relay both sides of it keep open.
type Service struct {
	DB  *dbx.DB
	Cfg func() config.Config
	Log *slog.Logger

	// Seal and Unseal protect the pool identity keys at rest (the app's
	// secretbox).
	Seal   func(plain string) (string, error)
	Unseal func(sealed string) (string, error)

	// Peers is the HTTP layer other NeoRouters are called through.
	Peers *providers.Client
	// Accounts adds, removes and validates a partner's connection.
	Accounts PeerAccounts
	// RouterKeys issues the key a partner calls us with.
	RouterKeys RouterKeyIssuer
	// Catalog refreshes one connection's models (catalog.Service.SyncAccount).
	Catalog func(ctx context.Context, accountID string) error
	// Quota reports one model's quota picture.
	Quota func(model Model, estimatedTokens float64, now time.Time) (QuotaState, error)
	// ForgetQuota drops what was observed about one connection's models.
	ForgetQuota func(accountID string, refs []int64) error
	// MarkFixed clears a connection's failure state (routing's fixed marks).
	MarkFixed func(accountID string, now time.Time)
	// Concurrency reports a model's known parallel limit; ok is false when it is
	// not known.
	Concurrency func(model Model, now time.Time) (limit int, ok bool)
	// Inflight reports how many requests are running on a model's account.
	Inflight func(model Model) int
	// GetSetting and SetSetting read and write one of a person's settings.
	GetSetting func(ctx context.Context, userID, key string) (json.RawMessage, bool, error)
	SetSetting func(ctx context.Context, userID, key string, value any) error

	// Loopback is the app relayed requests are answered by, exactly as if they
	// had come in over the network. The wiring sets it to the same handler the
	// HTTP server serves.
	Loopback http.Handler

	// Process-wide state.
	mu                sync.Mutex
	clock             func() time.Time
	hub               *relayHub
	relays            *connectorSet
	identities        map[string]*identity
	sealMu            sync.Mutex
	sealPairs         map[string][]byte
	nonceMu           sync.Mutex
	nonces            map[string]int64
	shareMu           sync.Mutex
	inflight          map[string]int64
	kindsCache        map[string]shareKindsEntry
	reserveMu         sync.Mutex
	reserved          map[string]int64
	reachMu           sync.Mutex
	down              map[string]int64
	lookupMu          sync.Mutex
	lookups           map[string]lookupEntry
	syncs             map[string]chan struct{}
	refreshedAt       map[string]int64
	federationLimiter *httpserver.SlidingWindow
}

// New builds the pools service on an open database. The app sets the dependency
// fields before serving.
func New(db *dbx.DB) *Service {
	s := &Service{
		DB:          db,
		identities:  map[string]*identity{},
		sealPairs:   map[string][]byte{},
		nonces:      map[string]int64{},
		inflight:    map[string]int64{},
		kindsCache:  map[string]shareKindsEntry{},
		reserved:    map[string]int64{},
		down:        map[string]int64{},
		lookups:     map[string]lookupEntry{},
		syncs:       map[string]chan struct{}{},
		refreshedAt: map[string]int64{},
	}
	s.hub = newRelayHub(s)
	s.relays = newConnectorSet(s)
	return s
}

// SetLoopback wires the app relayed requests are answered by: the wiring passes
// the same handler the HTTP server serves.
func (s *Service) SetLoopback(handler http.Handler) {
	s.mu.Lock()
	s.Loopback = handler
	s.mu.Unlock()
}

// loopbackHandler is the app relayed requests are answered by.
func (s *Service) loopbackHandler() http.Handler {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Loopback
}

func (s *Service) log() *slog.Logger {
	if s.Log != nil {
		return s.Log
	}
	return slog.Default()
}

func (s *Service) now() time.Time {
	s.mu.Lock()
	clock := s.clock
	s.mu.Unlock()
	if clock != nil {
		return clock()
	}
	return time.Now()
}

// SetClock replaces the clock tests read.
func (s *Service) SetClock(clock func() time.Time) {
	s.mu.Lock()
	s.clock = clock
	s.mu.Unlock()
}

// nowMS is Date.now().
func (s *Service) nowMS() int64 { return s.now().UnixMilli() }

// isoMillis is new Date(ms).toISOString().
func isoMillis(ms int64) string {
	return time.UnixMilli(ms).UTC().Format("2006-01-02T15:04:05.000Z")
}

// publicOrigin is this server's address as people reach it: the configured
// public URL, else the request's own scheme and host, without a trailing slash.
func (s *Service) publicOrigin(r *http.Request) string {
	cfg := config.Config{}
	if s.Cfg != nil {
		cfg = s.Cfg()
	}
	origin := cfg.PublicURL
	if origin == "" {
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		if forwarded := r.Header.Get("X-Forwarded-Proto"); forwarded != "" && cfg.TrustProxy {
			scheme = strings.TrimSpace(strings.Split(forwarded, ",")[0])
		}
		origin = scheme + "://" + r.Host
	}
	return strings.TrimRight(origin, "/")
}

// StartRelays opens a connection to the host of every pool that relays. It
// starts the relays, called at boot.
func (s *Service) StartRelays(ctx context.Context) {
	s.hub.start()
	for _, membership := range s.poolMemberships(ctx) {
		s.ensureRelay(ctx, membership.UserID, membership.PoolID)
	}
}

// Stop ends every relay connection and stops the heartbeat. It is the graceful
// shutdown of the relay.
func (s *Service) Stop() {
	if s == nil {
		return
	}
	s.relays.stopAll()
	s.hub.stopHub()
}

// RelayTransport answers a relay:// address from the providers HTTP layer. The
// wiring registers it as providers.Options.Transports["relay:"].
func (s *Service) RelayTransport(ctx context.Context, rawURL string, req *http.Request, info providers.TransportInfo) (*http.Response, error) {
	target, ok := parseRelayAddress(rawURL)
	var endpoint *relayEndpoint
	if ok && info.Owner != "" {
		endpoint = s.endpointFor(ctx, info.Owner, target.PoolID)
	}
	if endpoint == nil {
		err := fail(http.StatusBadGateway, "RELAY_UNREACHABLE",
			"Not connected to the pool host, so this partner cannot be reached right now.")
		err.RetryAfter = 0
		return nil, err
	}
	return endpoint.request(ctx, target.MemberID, relayRequest{
		Method:  req.Method,
		Path:    target.Path,
		Headers: req.Header,
		Body:    req.Body,
	})
}

// cleanName keeps a person-given name to one printable line of at most 60
// characters, with a fallback when nothing is left.
func cleanName(value, fallback string) string {
	cleaned := strings.Map(func(r rune) rune {
		if r < 0x20 {
			return -1
		}
		return r
	}, value)
	cleaned = strings.Join(strings.Fields(cleaned), " ")
	cleaned = strings.TrimSpace(cleaned)
	if len(cleaned) > 60 {
		cleaned = strings.TrimSpace(cleaned[:60])
	}
	if cleaned == "" {
		return fallback
	}
	return cleaned
}
