package app

// buildEverything: the object graph of one running NeoRouter, in the order
// server/index.js + server/app.js create it. Leaf services come first; a field
// that points forward (the accounts service's catalog, the local-model adapter
// the chat dialects read) is filled in once its owner exists.

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/dbx"
	"github.com/NeoLabs-Systems/neolabs-go/ssrf"
	"github.com/NeoLabs-Systems/neorouter/internal/apps"
	"github.com/NeoLabs-Systems/neorouter/internal/auth"
	"github.com/NeoLabs-Systems/neorouter/internal/budget"
	"github.com/NeoLabs-Systems/neorouter/internal/catalog"
	"github.com/NeoLabs-Systems/neorouter/internal/config"
	"github.com/NeoLabs-Systems/neorouter/internal/discovery"
	"github.com/NeoLabs-Systems/neorouter/internal/gateway"
	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
	"github.com/NeoLabs-Systems/neorouter/internal/live"
	"github.com/NeoLabs-Systems/neorouter/internal/localmodels"
	"github.com/NeoLabs-Systems/neorouter/internal/media"
	"github.com/NeoLabs-Systems/neorouter/internal/oauth"
	"github.com/NeoLabs-Systems/neorouter/internal/pools"
	"github.com/NeoLabs-Systems/neorouter/internal/processing"
	"github.com/NeoLabs-Systems/neorouter/internal/providers"
	"github.com/NeoLabs-Systems/neorouter/internal/providers/adapters"
	"github.com/NeoLabs-Systems/neorouter/internal/proxies"
	"github.com/NeoLabs-Systems/neorouter/internal/quota"
	"github.com/NeoLabs-Systems/neorouter/internal/routerkeys"
	"github.com/NeoLabs-Systems/neorouter/internal/routing"
	"github.com/NeoLabs-Systems/neorouter/internal/routing/task"
	"github.com/NeoLabs-Systems/neorouter/internal/scheduler"
	"github.com/NeoLabs-Systems/neorouter/internal/settings"
	"github.com/NeoLabs-Systems/neorouter/internal/status"
	"github.com/NeoLabs-Systems/neorouter/internal/teams"
	"github.com/NeoLabs-Systems/neorouter/internal/updates"
	"github.com/NeoLabs-Systems/neorouter/internal/usage"
)

// services is every domain of one running server.
type services struct {
	Settings    *settings.Service
	AccountData *settings.AccountData
	Teams       *teams.Service
	Auth        *auth.Service
	Proxies     *proxies.Service
	Updates     *updates.Service
	Status      *status.Service
	Registry    *providers.Registry
	Accounts    *providers.Service
	Catalog     *catalog.Service
	Budget      *budget.Service
	Quota       *quota.Service
	Usage       *usage.Service
	Routing     *routing.Service
	Keys        *routerkeys.Keys
	Concurrency *routerkeys.Concurrency
	Processing  *processing.Service
	Gateway     *gateway.Service
	Media       *media.Service
	Live        *live.Service
	Pools       *pools.Service
	Local       *localmodels.Service
	Discovery   *discovery.Service
	Apps        *apps.Service
	OAuth       *oauth.Service
	Scheduler   *scheduler.Service
}

// deps carries what buildEverything needs while it runs.
type deps struct {
	app        *App
	db         *dbx.DB
	log        *slog.Logger
	getenv     func(string) string
	version    string
	httpClient *http.Client
	secretKey  []byte
	cfg        func() config.Config

	services
	transports    map[string]providers.Transport
	providersHTTP *providers.Client
	sealer        *providers.Sealer
	gatewayMux    *http.ServeMux
	localAdapter  *localModelsAdapter
	shutdownFn    func()
	refCache      *refCache
	startedAt     time.Time
	now           func() time.Time
}

// build constructs every domain and its seams.
func (d *deps) build(ctx context.Context) error {
	cfg := d.cfg
	d.startedAt = time.Now()
	d.now = time.Now
	d.transports = map[string]providers.Transport{}
	d.refCache = newRefCache()
	d.gatewayMux = http.NewServeMux()
	d.localAdapter = &localModelsAdapter{}

	sealer, err := newSealer(d.secretKey)
	if err != nil {
		return err
	}
	d.sealer = sealer
	authSecrets, err := auth.NewSecrets(d.secretKey)
	if err != nil {
		return fmt.Errorf("app: auth secrets: %w", err)
	}

	// ---- settings and teams: the stores everything else reads ------------
	settingsSvc := &settings.Service{DB: d.db, Cfg: cfg, Log: d.log, Sealer: sealer}
	d.Settings = settingsSvc
	teamsSvc := &teams.Service{DB: d.db, Cfg: cfg, Log: d.log, Actor: auth.Actor}
	d.Teams = teamsSvc

	// ---- auth ------------------------------------------------------------
	authSvc := &auth.Service{
		DB:       d.db,
		Cfg:      cfg,
		Log:      d.log,
		Settings: registrationGate{svc: settingsSvc},
		Secrets:  authSecrets,
	}
	d.Auth = authSvc

	// ---- providers: proxies, HTTP layer, registry, accounts --------------
	proxiesSvc, err := proxies.New(proxies.Options{
		DB:       d.db,
		Log:      d.log,
		Sealer:   proxySealer(d.secretKey),
		Settings: settingsSvc,
		Direct:   d.httpClient,
		IsPrivateDestination: func(ctx context.Context, rawURL string) (bool, error) {
			return providers.IsPrivateDestination(ctx, rawURL, nil), nil
		},
	})
	if err != nil {
		return fmt.Errorf("app: build proxies: %w", err)
	}
	d.Proxies = proxiesSvc

	// Admin-owned accounts may reach local and private destinations (the JS
	// net_guard decision); everyone else goes through the public-only client.
	providerDirect := ssrf.NewClient(ssrf.Options{AllowPrivate: true, Timeout: cfg().UpstreamTimeout})
	providersHTTP := providers.NewClient(providers.Options{
		Direct:     providerDirect,
		Public:     d.httpClient,
		Proxy:      proxyPool{svc: proxiesSvc},
		Transports: d.transports,
		Timeout:    cfg().UpstreamTimeout,
	})
	d.providersHTTP = providersHTTP

	accounts := &providers.Service{
		DB:              d.db,
		Cfg:             cfg,
		Log:             d.log,
		Secrets:         sealer,
		HTTP:            providersHTTP,
		UpstreamTimeout: cfg().UpstreamTimeout,
		Getenv:          d.getenv,
		LocalModels:     localPolicy{adapter: d.localAdapter},
	}
	d.Accounts = accounts

	oauthFlows := providers.NewOAuth(providersHTTP, nil, d.getenv)
	// One sign-in machinery for both the routes and the adapters: Nous refresh
	// tokens are single-use, so two instances would rotate one twice and Portal
	// would read the reuse as theft and revoke the connection.
	accounts.OAuth = oauthFlows
	chatFactory := &adapters.Factory{
		Codex:              oauthFlows,
		PersistCodexSecret: d.persistSecret,
		Nous:               oauthFlows,
		PersistNousSecret:  d.persistSecret,
		NousIsSignedIn:     providers.NousIsSignIn,
		NousPortalURL:      d.getenv("NEOROUTER_NOUS_PORTAL_URL"),
		Local:              d.localAdapter,
	}
	registry, err := providers.Load(mergedFactory{factory: chatFactory}.build, d.getenv)
	if err != nil {
		return fmt.Errorf("app: load provider definitions: %w", err)
	}
	d.Registry = registry
	accounts.Registry = registry

	// ---- catalog ---------------------------------------------------------
	discoverySvc := &discovery.Service{Log: func(err error) { d.log.Warn("discovery", "error", err) }}
	catalogSvc := catalog.New(catalog.Options{
		DB:          d.db,
		Log:         d.log,
		Definitions: definitionsOf{registry: registry},
		Models:      accountModels{accounts: accounts, registry: registry},
		Settings:    catalogSettings{svc: settingsSvc},
		Config:      cfg,
		DataDir:     cfg().Paths.Data,
		HTTPClient:  d.httpClient,
		ObserveNews: func() { _ = discoverySvc.News.Observe(context.Background()) },
	})
	catalogSvc.Metadata.SkillsFromBenchmarks = task.SkillsFromBenchmarks
	d.Catalog = catalogSvc
	accounts.Catalog = accountCatalog{svc: catalogSvc}

	// ---- budget, quota, usage --------------------------------------------
	d.Budget = budget.New(d.db)
	d.Budget.Now = d.now
	quotaSvc := quota.New(d.db, quotaDefinitions{registry: registry})
	quotaSvc.SetCatalog(quotaCatalogSource{svc: catalogSvc})
	d.Quota = quotaSvc
	accounts.Limits = quotaSvc.AccountLimits()
	accounts.Quota = quotaRecorder{svc: quotaSvc}

	usageSvc := usage.New(d.db)
	usageSvc.Log = d.log
	usageSvc.Now = d.now
	usageSvc.ProviderName = usageProviderName(registry)
	usageSvc.Catalog = usageCatalogLookup(catalogSvc)
	usageSvc.AllowanceUsedUp = providers.AllowanceUsedUp
	usageSvc.Require = requirePermissionFunc()
	usageSvc.Space = usageSpace
	d.Usage = usageSvc

	// ---- router keys ------------------------------------------------------
	keys := routerkeys.New(d.db)
	keys.Log = d.log
	keys.Now = d.now
	d.Concurrency = routerkeys.NewConcurrency()
	keys.Concurrency = d.Concurrency
	d.Keys = keys

	// ---- processing (the shield and compression) -------------------------
	procSvc, err := processing.New(cfg, settingsSvc, d.log)
	if err != nil {
		return fmt.Errorf("app: build processing: %w", err)
	}
	procSvc.Space = processingSpace
	procSvc.Require = requirePermissionFunc()
	procSvc.PoolMinimum = poolMinimum
	d.Processing = procSvc
	procSvc.StatsQuery = d.processingStats

	// routing is built after pools (its share policy comes from there), while the
	// pool seams call back into it.
	var routingSvc *routing.Service

	// ---- pools -----------------------------------------------------------
	poolsSvc := pools.New(d.db)
	poolsSvc.Log = d.log
	poolsSvc.Seal = sealer.Seal
	poolsSvc.Unseal = sealer.Open
	poolsSvc.Peers = providersHTTP
	poolsSvc.Accounts = poolPeerAccounts{accounts: accounts}
	poolsSvc.RouterKeys = poolKeyIssuer{keys: keys}
	poolsSvc.Catalog = func(ctx context.Context, accountID string) error {
		_, err := catalogSvc.SyncAccount(ctx, accountID)
		return err
	}
	poolsSvc.Quota = func(model pools.Model, estimatedTokens float64, now time.Time) (pools.QuotaState, error) {
		evaluation, err := quotaSvc.Evaluate(quotaEntryOf(poolsCatalogEntry(model)), estimatedTokens, now)
		if err != nil {
			return pools.QuotaState{}, err
		}
		state := pools.QuotaState{Headroom: evaluation.Headroom}
		for _, detail := range evaluation.Details {
			state.Details = append(state.Details, pools.QuotaDetail{Metric: detail.Metric, Remaining: &detail.Remaining})
		}
		return state, nil
	}
	poolsSvc.ForgetQuota = func(accountID string, refs []int64) error {
		return quotaSvc.ForgetObserved(accountID, refs)
	}
	poolsSvc.MarkFixed = func(accountID string, now time.Time) { routingSvc.MarkAccountFixed(accountID, now) }
	poolsSvc.Concurrency = func(model pools.Model, now time.Time) (int, bool) {
		limit := routingSvc.LimitFor(poolsRoutingEntry(model), now)
		if limit == nil {
			return 0, false
		}
		return limit.Limit, true
	}
	poolsSvc.Inflight = func(model pools.Model) int {
		return routingSvc.Inflight(poolsRoutingEntry(model)).Account
	}
	poolsSvc.GetSetting = func(ctx context.Context, userID, key string) (json.RawMessage, bool, error) {
		return settingsSvc.GetUserSetting(ctx, userID, key)
	}
	poolsSvc.SetSetting = func(ctx context.Context, userID, key string, value any) error {
		return settingsSvc.SetUserSetting(ctx, userID, key, value)
	}
	d.transports["relay:"] = poolsSvc.RelayTransport
	d.Pools = poolsSvc

	// ---- routing ---------------------------------------------------------
	routingSvc = routing.New(routing.Options{
		DB:            d.db,
		Cfg:           cfg,
		Log:           d.log,
		Catalog:       routingCatalogView{svc: catalogSvc, cache: d.refCache},
		Quota:         routingQuota{svc: quotaSvc},
		Budget:        routingBudget{svc: d.Budget},
		Billing:       routingBilling{accounts: accounts},
		Pools:         routingPools{svc: poolsSvc},
		Settings:      routingSettings{svc: settingsSvc},
		AccountLimits: routingAccountLimits{limits: quotaSvc.AccountLimits()},
		Providers:     routingDefinitions{registry: registry},
		Keys:          routingKeys{keys: keys},
		PublicUser:    d.publicUserOf,
		Accounts:      d.accountCardsOf,
	})
	d.Routing = routingSvc
	accounts.Fixed = func(accountID string) providers.FixedResult {
		fixed := routingSvc.MarkAccountFixed(accountID, d.now())
		return providers.FixedResult{Models: fixed.Models, Chances: fixed.Chances}
	}
	accounts.Concurrency = func(accountID, definitionID, slotPlan string) *providers.ConcurrencyLimit {
		limit := routingSvc.LimitFor(&routing.Entry{AccountID: accountID, DefinitionID: definitionID, SlotPlan: slotPlan}, d.now())
		if limit == nil {
			return nil
		}
		return &providers.ConcurrencyLimit{Limit: limit.Limit, Source: limit.Source}
	}
	accounts.SlotWatch = func(accountID string) { routingSvc.SlotSnapshot(accountID, d.now()) }
	// A stated limit change takes a utilization snapshot around the write
	// (routing's slot_utilization.watch).
	quotaSvc.AccountLimits().Watch = func(accountID string) { routingSvc.SlotSnapshot(accountID, d.now()) }
	usageSvc.Routing = usageRouting{svc: routingSvc}
	usageSvc.CapacityView = func(userID string, now time.Time) (any, error) {
		return quotaSvc.FreeCapacity(userID, now)
	}
	usageSvc.QuotasView = d.quotasView
	keys.Budget = routerkeysBudget{svc: d.Budget}
	keys.Profiles = func(userID, profile string) bool { return routingSvc.ResolveProfile(userID, profile) != nil }
	keys.RequireGroup = func(userID, group string) error {
		_, err := routingSvc.RequireGroup(userID, group)
		return err
	}
	keys.PublicUser = d.publicUserOf
	keys.Levels = routing.PriorityLevels
	keys.Require = requirePermissionFunc()
	keys.Space = keysSpace

	// ---- gateway, media, live --------------------------------------------
	// The media capability is built after the gateway (it runs through the
	// gateway's pipeline), so the web-search hook reads it through this slot.
	var mediaRef *media.Service
	gatewaySvc := gateway.New(gateway.Options{
		Cfg:                  cfg,
		Log:                  d.log,
		Routing:              routingSvc,
		Catalog:              routingCatalogView{svc: catalogSvc, cache: d.refCache},
		Quota:                quotaSvc,
		Budget:               d.Budget,
		Usage:                usageSvc,
		Keys:                 keys,
		Concurrency:          d.Concurrency,
		Adapters:             gatewayAccounts{accounts: accounts, registry: registry},
		PoolAccess:           poolHooks{pools: poolsSvc, now: d.now}.PoolAccess,
		ShareAdmit:           poolHooks{pools: poolsSvc, now: d.now}.ShareAdmit,
		SharingWith:          poolHooks{pools: poolsSvc, now: d.now}.SharingWith,
		PoolOffers:           poolHooks{pools: poolsSvc, now: d.now}.Offers,
		IsPeerEntry:          routingPools{svc: poolsSvc}.IsPeerEntry,
		RecordPoolTraffic:    poolHooks{pools: poolsSvc, now: d.now}.RecordTraffic,
		NotePeerReachability: poolHooks{pools: poolsSvc, now: d.now}.NoteReachability,
		AssessSensitivity:    assessSensitivity(procSvc),
		ApplyProcessing:      processingBridge{svc: procSvc}.Apply,
		RestoreResponse:      processingBridge{svc: procSvc}.Restore,
		NewStreamRestorer:    processingBridge{svc: procSvc}.StreamRestorer,
		SearchWeb: func(ctx context.Context, call gateway.SearchCall) (*providers.SearchResult, error) {
			if mediaRef == nil {
				return nil, httpserver.New(http.StatusServiceUnavailable, "NO_SEARCH", "No search engine is connected.")
			}
			return mediaSearch(mediaRef)(ctx, call)
		},
		AnswerForRequest:  d.answerForRequest,
		RecordFeedback:    d.recordFeedback,
		RecordKeyFailure:  d.recordKeyFailure,
		RequirePermission: requirePermissionFunc(),
		Space:             gatewaySpace,
	})
	d.Gateway = gatewaySvc

	mediaSvc := media.New(media.Options{
		DB:       d.db,
		Log:      d.log,
		Now:      d.now,
		Config:   cfg,
		Catalog:  catalogLookup(catalogSvc),
		Accounts: accountsAdapter{accounts: accounts},
		Runner:   mediaRunner{gateway: gatewaySvc},
		Shield:   mediaShield(procSvc),
		KeyRow:   keys.Row,
		KeyAuth:  mediaKeyAuth{gateway: gatewaySvc},
		Require:  requirePermissionFunc(),
		Space:    mediaSpace,
		HTTP:     d.httpClient,
	})
	d.Media = mediaSvc
	mediaRef = mediaSvc

	liveSvc := live.New(live.Options{
		Routing:         routingSvc,
		Keys:            keys,
		Accounts:        liveAccounts{accounts: accounts, registry: registry},
		Concurrency:     d.Concurrency,
		Quota:           quotaSvc,
		Budget:          d.Budget,
		Usage:           usageSvc,
		PlanSent:        routingSvc.PlanSent,
		ValidateAccount: func(accountID string) { _, _ = accounts.ValidateAccount(context.Background(), accountID) },
		Log:             d.log,
		Now:             d.now,
	})
	d.Live = liveSvc

	// ---- local models ----------------------------------------------------
	privacy := procSvc.Privacy
	localSvc, err := localmodels.New(localmodels.Options{
		DB:       d.db,
		Cfg:      cfg,
		Log:      d.log,
		Settings: settingsSvc,
		PrivacyStatus: func() localmodels.PrivacyStatus {
			return privacyStatusOf(privacy.Status(context.Background()))
		},
		SetPrivacyEnabled: func(enabled bool) {
			_, _ = privacy.SetEnabled(context.Background(), enabled)
		},
		ConnectAccount: func(ctx context.Context, userID string) error {
			return connectLocalAccount(ctx, accounts, userID)
		},
		SyncAccounts: func(ctx context.Context, accountID string) error {
			_, err := catalogSvc.SyncAccount(ctx, accountID)
			return err
		},
		HTTP:    d.httpClient,
		DataDir: cfg().Paths.Data,
	})
	if err != nil {
		return fmt.Errorf("app: build local models: %w", err)
	}
	d.Local = localSvc
	d.localAdapter.svc = localSvc
	procSvc.Privacy = privacy

	// ---- discovery -------------------------------------------------------
	discoveryCatalogView := discoveryCatalog{metadata: catalogSvc.Metadata}
	benchmarks := &discovery.Benchmarks{DB: d.db, Catalog: discoveryCatalogView}
	discoverySvc.Benchmarks = benchmarks
	discoverySvc.News = &discovery.News{DB: d.db, Benchmarks: benchmarks, Catalog: discoveryCatalogView}
	d.Discovery = discoverySvc

	// ---- updates, status -------------------------------------------------
	updatesSvc := &updates.Service{
		Cfg:           cfg,
		Version:       d.version,
		Getenv:        d.getenv,
		Log:           d.log,
		RunnerCommand: updateRunnerCommand(executablePath()),
		Now:           d.now,
		// A signed-runtime install (the manager's <home>/app store) updates
		// itself without a git checkout.
		RuntimeSelfUpdate: func() bool {
			_, err := os.Stat(filepath.Join(cfg().Paths.Home, "app", "current.json"))
			return err == nil
		},
	}
	d.Updates = updatesSvc
	statusSvc := status.New(d.db, cfg().Paths.Data)
	statusSvc.Catalog = func(ctx context.Context) error {
		_, err := catalogSvc.Metadata.Status()
		return err
	}
	d.Status = statusSvc

	// ---- apps, oauth -----------------------------------------------------
	d.Apps = apps.New(apps.Options{
		DB:      d.db,
		Config:  cfg,
		Getenv:  d.getenv,
		Log:     d.log,
		Guard:   appGuard(),
		Restart: d.restart,
	})
	d.OAuth = oauth.New(oauth.Options{
		Config:   cfg,
		Log:      d.log,
		Keys:     oauthKeys{keys: keys},
		Accounts: oauthAccounts{accounts: accounts},
		Catalog:  oauthCatalog{catalog: catalogSvc},
		HTTP:     d.httpClient,
		Guard:    oauthGuard(),
		Now:      d.now,
	})

	// ---- the account data surface auth serves ----------------------------
	d.AccountData = d.accountDataSeams()
	authSvc.AccountData = authAccountData{svc: d.AccountData}
	d.settingsSeams(settingsSvc, catalogSvc.Metadata, procSvc)
	// ---- scheduler -------------------------------------------------------
	d.Scheduler = d.buildScheduler()
	return nil
}

// connectLocalAccount creates a person's "NeoRouter Local" connection.
func connectLocalAccount(ctx context.Context, accounts *providers.Service, userID string) error {
	definition := "neorouter_local"
	in := providers.Input{
		DefinitionID: providers.Optional[string]{Set: true, Value: &definition},
	}
	_, err := accounts.Create(ctx, userID, in, "", nil)
	return err
}

// restart stops this process so the service manager starts it again
// (network.js sends SIGTERM to itself).
func (d *deps) restart() {
	if d.shutdownFn != nil {
		d.shutdownFn()
	}
	d.app.cancel()
}

// shutdown is the Close sequence: scheduler.stop, localModels.shutdown, pools
// and the samplers, then the database (Close does that last).
func (d *deps) shutdown() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if d.Scheduler != nil {
		d.Scheduler.Stop()
	}
	if d.Local != nil {
		d.Local.Shutdown(ctx)
	}
	if d.Pools != nil {
		d.Pools.Stop()
	}
	if d.Proxies != nil {
		_ = d.Proxies.Close()
	}
}
