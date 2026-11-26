package live

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/NeoLabs-Systems/neolabs-go/dbx"
	"github.com/NeoLabs-Systems/neolabs-go/passhash"
	"github.com/NeoLabs-Systems/neorouter/internal/budget"
	"github.com/NeoLabs-Systems/neorouter/internal/config"
	"github.com/NeoLabs-Systems/neorouter/internal/providers"
	"github.com/NeoLabs-Systems/neorouter/internal/quota"
	"github.com/NeoLabs-Systems/neorouter/internal/routerkeys"
	"github.com/NeoLabs-Systems/neorouter/internal/routing"
	"github.com/NeoLabs-Systems/neorouter/internal/schema"
	"github.com/NeoLabs-Systems/neorouter/internal/usage"
	"github.com/google/uuid"
)

const (
	mockSecret  = "good-key"
	mockLiveV1  = "gpt-live-1"
	testUserID  = "live"
	testAccount = "acct-live"
)

// fakeCatalog is routing's catalog: the user's live models.
type fakeCatalog struct {
	mu      sync.Mutex
	entries []*routing.Entry
}

func (c *fakeCatalog) ListForUser(userID string, q routing.CatalogQuery) []*routing.Entry {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []*routing.Entry
	for _, entry := range c.entries {
		if entry.UserID != userID || entry.Capability != q.Capability {
			continue
		}
		if !q.IncludeUnavailable && !entry.Available {
			continue
		}
		copied := *entry
		out = append(out, &copied)
	}
	return out
}

func (c *fakeCatalog) CanonicalID(modelID string) string { return modelID }

func (c *fakeCatalog) add(entry *routing.Entry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = append(c.entries, entry)
}

// setFree flips every live model of an account between free and needing
// credits.
func (c *fakeCatalog) setFree(accountID string, free bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, entry := range c.entries {
		if entry.AccountID != accountID {
			continue
		}
		entry.IsFree = free
		if free {
			entry.FreeReason = ""
		} else {
			entry.FreeReason = "not_in_free_tier"
		}
	}
}

// fakeAccounts is the provider accounts domain: one connection per account.
type fakeAccounts struct {
	mu        sync.Mutex
	conns     map[string]Connection
	validated []string
}

func newFakeAccounts() *fakeAccounts {
	return &fakeAccounts{conns: map[string]Connection{}}
}

func (a *fakeAccounts) Runtime(accountID string) (Connection, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	connection, ok := a.conns[accountID]
	return connection, ok
}

func (a *fakeAccounts) validate(accountID string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.validated = append(a.validated, accountID)
}

func (a *fakeAccounts) validatedAccounts() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string{}, a.validated...)
}

// add registers an OpenAI-compatible connection (definition custom_openai, which
// speaks openai_live and openai_realtime) at baseURL.
func (a *fakeAccounts) add(accountID, userID, baseURL, secret string, publicOnly bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.conns[accountID] = Connection{
		Definition: providers.Definition{
			ID:           "custom_openai",
			Label:        "Custom OpenAI",
			Auth:         providers.Auth{Type: providers.AuthBearer},
			Live:         []providers.LiveDialect{providers.LiveOpenAI, providers.LiveRealtime},
			Capabilities: []string{"chat", "live"},
		},
		Account: providers.Account{
			ID:           accountID,
			UserID:       userID,
			DefinitionID: "custom_openai",
			BaseURL:      baseURL,
			Secret:       secret,
			PublicOnly:   publicOnly,
		},
	}
}

// addGemini registers a Gemini connection, which speaks gemini_live only.
func (a *fakeAccounts) addGemini(accountID, userID, baseURL, secret string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.conns[accountID] = Connection{
		Definition: providers.Definition{
			ID:           "google",
			Label:        "Google",
			Auth:         providers.Auth{Type: providers.AuthBearer},
			Live:         []providers.LiveDialect{providers.LiveGemini},
			BaseURL:      baseURL,
			Capabilities: []string{"chat", "live"},
		},
		Account: providers.Account{
			ID:           accountID,
			UserID:       userID,
			DefinitionID: "google",
			Secret:       secret,
		},
	}
}

// fixture is one running live gateway over a temp database.
type fixture struct {
	t        *testing.T
	db       *dbx.DB
	service  *Service
	gateway  *httptest.Server
	base     string
	upstream *mockUpstream
	catalog  *fakeCatalog
	accounts *fakeAccounts
	usage    *usage.Service
	budget   *budget.Service
	token    string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctx := context.Background()
	home := t.TempDir()
	db, err := dbx.Open(ctx, filepath.Join(home, "test.sqlite3"), dbx.Options{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := schema.Ensure(ctx, db.W); err != nil {
		t.Fatalf("schema: %v", err)
	}
	if _, err := db.W.Exec(`INSERT INTO users (id, username, email, password_hash, role)
		VALUES (?, 'live', 'live@example.test', 'x', 'admin')`, testUserID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	cfg, err := config.Load(func(string) string { return "" }, home, home)
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	upstream := newMockUpstream(t)
	catalog := &fakeCatalog{}
	accounts := newFakeAccounts()
	accounts.add(testAccount, testUserID, upstream.URL(), mockSecret, false)
	catalog.add(liveEntry(1, "custom_openai/gpt-live-1", mockLiveV1, testAccount))
	catalog.add(liveEntry(2, "custom_openai/fail-realtime", "fail-realtime", testAccount))
	keys := routerkeys.New(db)
	usageService := usage.New(db)
	budgetService := budget.New(db)
	router := routing.New(routing.Options{
		DB:      db,
		Cfg:     func() config.Config { return cfg },
		Catalog: catalog,
		Random:  func() float64 { return 0.5 },
	})
	service := New(Options{
		Routing:         router,
		Keys:            keys,
		Accounts:        accounts,
		Concurrency:     routerkeys.NewConcurrency(),
		Quota:           quota.New(db, nil),
		Budget:          budgetService,
		Usage:           usageService,
		ValidateAccount: accounts.validate,
		Log:             testLogger(t),
	})
	mux := http.NewServeMux()
	service.Routes(mux)
	gateway := httptest.NewServer(mux)
	t.Cleanup(gateway.Close)
	return &fixture{
		t:        t,
		db:       db,
		service:  service,
		gateway:  gateway,
		base:     "ws" + strings.TrimPrefix(gateway.URL, "http"),
		upstream: upstream,
		catalog:  catalog,
		accounts: accounts,
		usage:    usageService,
		budget:   budgetService,
		token:    insertKey(t, db, testUserID, "free_only", nil),
	}
}

// liveEntry is one live model of an account, the way the catalog presents it.
func liveEntry(ref int64, id, modelID, accountID string) *routing.Entry {
	intelligence := 0.5
	return &routing.Entry{
		Ref:            ref,
		ID:             id,
		ModelID:        modelID,
		Name:           modelID,
		CanonicalID:    modelID,
		Capability:     "live",
		AccountID:      accountID,
		UserID:         testUserID,
		ProviderSlug:   "custom_openai",
		ProviderName:   "Custom OpenAI",
		ProviderKey:    "openai_compatible",
		DefinitionID:   "custom_openai",
		BillingMode:    "free_tier",
		Enabled:        true,
		Available:      true,
		AccountEnabled: true,
		AccountStatus:  "active",
		ContextWindow:  32768,
		MaxOutput:      4096,
		IsFree:         true,
		Intelligence:   &intelligence,
	}
}

// geminiEntry is one live model of a Gemini connection.
func geminiEntry(ref int64, id, modelID, accountID string) *routing.Entry {
	entry := liveEntry(ref, id, modelID, accountID)
	entry.ProviderSlug = "google"
	entry.ProviderName = "Google"
	entry.ProviderKey = "google_gemini"
	entry.DefinitionID = "google"
	return entry
}

// insertKey stores a router key and returns its secret.
func insertKey(t *testing.T, db *dbx.DB, userID, mode string, budgetUSD *float64) string {
	t.Helper()
	token := routerkeys.Prefix + "_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	id := uuid.NewString()
	prefix := token[:len(routerkeys.Prefix)+9]
	if _, err := db.W.Exec(`INSERT INTO router_keys (id, user_id, name, prefix, key_hash, mode, budget_usd)
		VALUES (?, ?, 'live', ?, ?, ?, ?)`, id, userID, prefix, passhash.DigestToken(token), mode, budgetUSD); err != nil {
		t.Fatalf("insert router key: %v", err)
	}
	return token
}

// bearer is the Authorization header a session carries.
func bearer(token string) http.Header {
	return http.Header{"Authorization": []string{"Bearer " + token}}
}

// requestRow is one row of the request log as the tests read it.
type requestRow struct {
	apiFormat      string
	requestedModel string
	modelLabel     string
	accountID      string
	status         string
	httpStatus     int64
	errorCode      string
	errorMessage   string
	inputTokens    int64
	outputTokens   int64
	attempts       int64
	stream         int64
	trailJSON      string
	capability     string
	units          float64
	costUSD        float64
	isFree         int64
}

// requests reads the request log rows the gateway wrote, newest first.
func (f *fixture) requests() []requestRow {
	f.t.Helper()
	rows, err := f.db.R.Query(`SELECT api_format, requested_model, model_label, account_id, status, http_status,
		error_code, error_message, input_tokens, output_tokens, attempts, stream, trail_json, capability, units, cost_usd, is_free
		FROM requests ORDER BY created_at DESC`)
	if err != nil {
		f.t.Fatalf("read requests: %v", err)
	}
	defer rows.Close()
	var out []requestRow
	for rows.Next() {
		var row requestRow
		var modelLabel, accountID, errorCode, errorMessage *string
		var httpStatus *int64
		if err := rows.Scan(&row.apiFormat, &row.requestedModel, &modelLabel, &accountID, &row.status, &httpStatus,
			&errorCode, &errorMessage, &row.inputTokens, &row.outputTokens, &row.attempts, &row.stream,
			&row.trailJSON, &row.capability, &row.units, &row.costUSD, &row.isFree); err != nil {
			f.t.Fatalf("scan request: %v", err)
		}
		row.modelLabel = textOf(modelLabel)
		row.accountID = textOf(accountID)
		row.errorCode = textOf(errorCode)
		row.errorMessage = textOf(errorMessage)
		row.httpStatus = numberOrZero(httpStatus)
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		f.t.Fatalf("read requests: %v", err)
	}
	return out
}

func textOf(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func numberOrZero(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}
