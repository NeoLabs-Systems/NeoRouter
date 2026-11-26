package app

// The discovery service's view of the catalog metadata, and the admin/misc
// seams: the oauth approval flow's key issuer, the apps domain's guards, the
// update runner and the status probes.

import (
	"context"
	"net/http"
	"time"

	"github.com/NeoLabs-Systems/neorouter/internal/apps"
	"github.com/NeoLabs-Systems/neorouter/internal/catalog"
	"github.com/NeoLabs-Systems/neorouter/internal/discovery"
	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
	"github.com/NeoLabs-Systems/neorouter/internal/oauth"
	"github.com/NeoLabs-Systems/neorouter/internal/pools"
	"github.com/NeoLabs-Systems/neorouter/internal/providers"
	"github.com/NeoLabs-Systems/neorouter/internal/routerkeys"
	"github.com/NeoLabs-Systems/neorouter/internal/teams"
	"github.com/NeoLabs-Systems/neorouter/internal/updates"
)

// discoveryCatalog is the catalog's public metadata projection as the discovery
// leaderboard and news feed read it.
type discoveryCatalog struct {
	metadata *catalog.Metadata
}

func (c discoveryCatalog) DiscoverySnapshot() discovery.Snapshot {
	return discoverySnapshotOf(c.metadata.DiscoverySnapshot())
}

func (c discoveryCatalog) BenchmarkIdentity(modelID, capability string) string {
	return c.metadata.BenchmarkIdentity(modelID, capability)
}

func (c discoveryCatalog) VariantBase(modelID string) (string, float64, bool) {
	match := c.metadata.VariantBase(modelID)
	if match == nil {
		return "", 0, false
	}
	return match.Base, match.Share, true
}

// discoverySnapshotOf is the catalog's metadata projection in discovery's own
// shape.
func discoverySnapshotOf(snapshot catalog.DiscoverySnapshot) discovery.Snapshot {
	out := discovery.Snapshot{
		FetchedAt:               snapshot.FetchedAt,
		ArtificialAnalysisMedia: map[string][]discovery.ArenaModel{},
		MTEB:                    map[string]discovery.MTEBModel{},
	}
	if out.FetchedAt == nil {
		out.FetchedAt = map[string]string{}
	}
	for _, fact := range snapshot.ArtificialAnalysis {
		if fact == nil {
			continue
		}
		out.ArtificialAnalysis = append(out.ArtificialAnalysis, discovery.AAModel{
			Slug:              firstString(fact.Slug, fact.ID, fact.ModelID),
			Name:              fact.Name,
			ReleaseDate:       fact.ReleaseDate,
			Benchmarks:        fact.Benchmarks,
			IntelligenceIndex: fact.IntelligenceIndex,
			IndexDerived:      fact.IndexDerived,
		})
	}
	for _, fact := range snapshot.OpenRouter {
		if fact == nil {
			continue
		}
		out.OpenRouter = append(out.OpenRouter, discovery.OpenRouterModel{
			ID:            firstString(fact.ID, fact.Slug, fact.ModelID),
			IsFree:        fact.IsFree,
			PriceIn:       fact.PriceIn,
			PriceOut:      fact.PriceOut,
			ContextWindow: intOf(fact.ContextWindow),
		})
	}
	if snapshot.OpenRouterBenchmarks != nil {
		for _, model := range snapshot.OpenRouterBenchmarks.Models {
			out.OpenRouterBenchmarks = append(out.OpenRouterBenchmarks, discovery.BenchmarkModel{
				Slug:       model.Slug,
				Name:       model.Name,
				Benchmarks: model.Benchmarks,
			})
		}
	}
	for capability, arenas := range snapshot.ArtificialAnalysisMedia {
		models := make([]discovery.ArenaModel, 0, len(arenas))
		for _, arena := range arenas {
			releaseDate := ""
			if arena.ReleaseDate != nil {
				releaseDate = *arena.ReleaseDate
			}
			models = append(models, discovery.ArenaModel{Slug: arena.Slug, Name: arena.Name, ReleaseDate: releaseDate, Elo: arena.Elo})
		}
		out.ArtificialAnalysisMedia[capability] = models
	}
	for key, score := range snapshot.MTEB {
		value := 0.0
		if score.Score != nil {
			value = *score.Score
		}
		out.MTEB[key] = discovery.MTEBModel{Model: firstString(score.Model, key), Score: value}
	}
	return out
}

func firstString(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func intOf(value *int64) *int {
	if value == nil {
		return nil
	}
	converted := int(*value)
	return &converted
}

// appGuard resolves who an apps request runs as, from internal/auth and
// internal/teams.
func appGuard() apps.Guard {
	return apps.Guard{
		Authenticate: func(r *http.Request) (*apps.Session, error) {
			user, ok := actorOf(r)
			if !ok {
				return nil, apps.ErrAuthenticationRequired()
			}
			return &apps.Session{UserID: user.id, Role: user.role}, nil
		},
		Space: func(r *http.Request, session *apps.Session) (*apps.Space, error) {
			space, err := spaceOf(r)
			if err != nil {
				return nil, err
			}
			return &apps.Space{
				OwnerID:     space.OwnerID,
				ActorID:     space.ActorID,
				IsOwner:     space.IsOwner,
				Permissions: space.Permissions,
			}, nil
		},
	}
}

// oauthGuard is the same pair in the shape the oauth domain reads.
func oauthGuard() oauth.Guard {
	return oauth.Guard{
		Authenticate: func(r *http.Request) (*oauth.Session, error) {
			user, ok := actorOf(r)
			if !ok {
				return nil, httpserver.New(http.StatusUnauthorized, "AUTHENTICATION_REQUIRED", "A valid session is required.")
			}
			return &oauth.Session{UserID: user.id, Role: user.role}, nil
		},
		Space: func(r *http.Request, session *oauth.Session) (*oauth.Space, error) {
			space, err := spaceOf(r)
			if err != nil {
				return nil, err
			}
			return &oauth.Space{
				OwnerID:     space.OwnerID,
				ActorID:     space.ActorID,
				IsOwner:     space.IsOwner,
				Permissions: space.Permissions,
			}, nil
		},
	}
}

// spaceOf is the space the space middleware resolved for a request.
func spaceOf(r *http.Request) (*teams.Space, error) {
	space, ok := teams.SpaceFromContext(r.Context())
	if !ok || space == nil {
		return nil, httpserver.New(http.StatusNotFound, "SPACE_NOT_FOUND", "This space does not exist.")
	}
	return space, nil
}

// oauthKeys is the router-key store as the oauth approval flow issues keys.
type oauthKeys struct {
	keys *routerkeys.Keys
}

func (k oauthKeys) Modes(allowFree, allowPlan, allowPaid bool) error {
	_, err := routerkeys.ResolveKinds(routerkeys.Input{
		AllowFree: routerkeys.Field[bool]{Set: true, Value: &allowFree},
		AllowPlan: routerkeys.Field[bool]{Set: true, Value: &allowPlan},
		AllowPaid: routerkeys.Field[bool]{Set: true, Value: &allowPaid},
	}, nil)
	return err
}

func (k oauthKeys) AssertExpiry(expiresAt *string) error {
	return routerkeys.AssertExpiry(expiresAt, nil, time.Now())
}

func (k oauthKeys) Create(ownerID string, request oauth.KeyRequest, createdBy string, app oauth.AppInfo) (*oauth.IssuedKey, error) {
	in := routerkeys.Input{
		Name:       routerkeys.Field[string]{Set: true, Value: &request.Name},
		AllowFree:  routerkeys.Field[bool]{Set: true, Value: &request.AllowFree},
		AllowPlan:  routerkeys.Field[bool]{Set: true, Value: &request.AllowPlan},
		AllowPaid:  routerkeys.Field[bool]{Set: true, Value: &request.AllowPaid},
		BudgetUSD:  routerkeys.Field[float64]{Set: request.BudgetUSD != nil, Value: request.BudgetUSD},
		RPMLimit:   routerkeys.Field[int64]{Set: request.RPMLimit != nil, Value: request.RPMLimit},
		TokenLimit: routerkeys.Field[int64]{Set: request.TokenLimit != nil, Value: request.TokenLimit},
		MaxPricePerMillion: routerkeys.Field[float64]{
			Set:   request.MaxPricePerMillion != nil,
			Value: request.MaxPricePerMillion,
		},
	}
	if request.BudgetPeriod != nil {
		in.BudgetPeriod = routerkeys.Field[string]{Set: true, Value: request.BudgetPeriod}
	}
	if request.TokenPeriod != nil {
		in.TokenPeriod = routerkeys.Field[string]{Set: true, Value: request.TokenPeriod}
	}
	if request.ExpiresAt != nil {
		in.ExpiresAt = routerkeys.Field[string]{Set: true, Value: request.ExpiresAt}
	}
	var keyApp *routerkeys.App
	if app.Name != "" || app.Origin != nil || len(app.Scopes) > 0 {
		keyApp = &routerkeys.App{Name: app.Name, Origin: app.Origin, Scopes: app.Scopes}
	}
	public, secret, err := k.keys.Create(ownerID, in, createdBy, keyApp)
	if err != nil {
		return nil, err
	}
	return &oauth.IssuedKey{ID: public.ID, Secret: secret, ExpiresAt: public.ExpiresAt}, nil
}

// oauthAccounts connects a provider account for the OpenRouter sign-in flow.
type oauthAccounts struct {
	accounts *providers.Service
}

func (a oauthAccounts) CreateAccount(ctx context.Context, ownerID string, request oauth.NewAccount, createdBy string) (string, error) {
	definition := request.DefinitionID
	secret := request.Secret
	in := providers.Input{
		DefinitionID: providers.Optional[string]{Set: true, Value: &definition},
		Secret:       providers.Optional[string]{Set: true, Value: &secret},
	}
	if request.BillingMode != "" {
		mode := request.BillingMode
		in.BillingMode = providers.Optional[string]{Set: true, Value: &mode}
	}
	created, err := a.accounts.Create(ctx, ownerID, in, createdBy, nil)
	if err != nil {
		return "", err
	}
	return created.Account.ID, nil
}

// oauthCatalog refreshes a newly connected account's models.
type oauthCatalog struct {
	catalog *catalog.Service
}

func (c oauthCatalog) SyncAccount(ctx context.Context, accountID string) error {
	_, err := c.catalog.SyncAccount(ctx, accountID)
	return err
}

// poolPeerAccounts keeps the connections pool links use, both directions.
type poolPeerAccounts struct {
	accounts *providers.Service
}

func (p poolPeerAccounts) AddPoolPeer(ctx context.Context, userID string, peer pools.PeerAccount) (string, error) {
	definition := pools.PeerDefinition
	label := peer.Label
	slug := peer.Slug
	secret := peer.Secret
	baseURL := peer.BaseURL
	in := providers.Input{
		DefinitionID: providers.Optional[string]{Set: true, Value: &definition},
		Label:        providers.Optional[string]{Set: true, Value: &label},
		Slug:         providers.Optional[string]{Set: true, Value: &slug},
		Secret:       providers.Optional[string]{Set: true, Value: &secret},
		BaseURL:      providers.Optional[string]{Set: true, Value: &baseURL},
	}
	created, err := p.accounts.Create(ctx, userID, in, "", nil)
	if err != nil {
		return "", err
	}
	return created.Account.ID, nil
}

func (p poolPeerAccounts) Remove(ctx context.Context, userID, accountID string) error {
	return p.accounts.Remove(ctx, userID, accountID)
}

func (p poolPeerAccounts) Validate(ctx context.Context, accountID string) error {
	_, err := p.accounts.ValidateAccount(ctx, accountID)
	return err
}

func (p poolPeerAccounts) PublicOnly(ctx context.Context, userID string) bool {
	return p.accounts.PublicOnly(ctx, userID, nil)
}

// updateRunnerCommand is the detached update runner this binary starts
// (update_service.js spawns `neorouter update`).
func updateRunnerCommand(exe string) []string {
	if exe == "" {
		return nil
	}
	return []string{exe, "update"}
}

var _ = updates.Service{}
var _ = apps.Service{}
var _ = discovery.Service{}
var _ = oauth.Service{}
