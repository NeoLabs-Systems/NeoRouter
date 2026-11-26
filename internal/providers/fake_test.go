package providers

// Test scaffolding for the provider account service: a real temp SQLite, a
// registry built from a small set of definitions with fake adapters, and fakes
// for the domains the service consumes.

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/dbx"
	"github.com/NeoLabs-Systems/neorouter/internal/config"
	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
	"github.com/NeoLabs-Systems/neorouter/internal/schema"
)

// testDefinitions is the provider set the account tests run against: a custom
// endpoint, one provider that requires a key and a field, a free tier verified
// by headers, one verified by a paid-model probe, a flat-rate plan, the two
// signed-in providers and the pool/local definitions the API filters out.
const testDefinitions = `[
 {"id":"custom_openai","label":"Custom OpenAI-compatible","description":"Any endpoint.","category":"custom",
  "adapter":"openai_compatible","capabilities":["chat"],"media":{},"live":[],
  "baseUrl":"","editableBaseUrl":true,"baseUrlSchemes":["http","https"],
  "auth":{"type":"bearer"},"keyOptional":true,"fields":[],"headers":{},
  "models":{"extraPaths":[],"optIn":[]},"extraModels":[],
  "billing":{"defaultMode":"paid","freeTierAll":true,"freeTierModels":[],"alwaysFree":[],"trustListingFree":true},
  "limits":{"pool":"model"},
  "quota":{"headerPeriods":{"requests":"minute","tokens":"minute"},"plainHeaderMetric":"rpm"},"quirks":{}},
 {"id":"strict","label":"Strict Provider","description":"Needs a key and an account id.","category":"paid",
  "adapter":"openai_compatible","capabilities":["chat"],"media":{},"live":[],
  "baseUrl":"http://127.0.0.1:9/v1","editableBaseUrl":false,"baseUrlSchemes":["http","https"],
  "auth":{"type":"bearer"},"keyOptional":false,
  "fields":[{"key":"accountId","label":"Account ID","required":true}],"headers":{},
  "models":{"extraPaths":[],"optIn":[]},"extraModels":[],
  "billing":{"defaultMode":"paid","freeTierAll":false,"freeTierModels":[],"alwaysFree":[],"trustListingFree":false},
  "limits":{"pool":"model"},
  "quota":{"headerPeriods":{"requests":"minute","tokens":"minute"},"plainHeaderMetric":"rpm"},"quirks":{}},
 {"id":"groq","label":"Groq","description":"Free while on the free plan.","category":"free",
  "adapter":"openai_compatible","capabilities":["chat"],"media":{},"live":[],
  "baseUrl":"http://127.0.0.1:9/groq/v1","editableBaseUrl":false,"baseUrlSchemes":["http","https"],
  "auth":{"type":"bearer"},"keyOptional":false,"fields":[],"headers":{},
  "models":{"extraPaths":[],"optIn":[]},"extraModels":[],
  "billing":{"defaultMode":"free_tier","freeTierAll":true,"freeTierModels":[],"alwaysFree":[],"trustListingFree":true,
   "verify":{"type":"ratelimit_headers","maxFreeRequestsPerDay":14400,"probeModels":["gpt-oss-20b"]}},
  "limits":{"pool":"model"},
  "quota":{"headerPeriods":{"requests":"minute","tokens":"minute"},"plainHeaderMetric":"rpm"},"quirks":{}},
 {"id":"google","label":"Google","description":"Free models silently end with billing.","category":"free",
  "adapter":"openai_compatible","capabilities":["chat"],"media":{},"live":[],
  "baseUrl":"http://127.0.0.1:9/google/v1","editableBaseUrl":false,"baseUrlSchemes":["http","https"],
  "auth":{"type":"bearer"},"keyOptional":false,"fields":[],"headers":{},
  "models":{"extraPaths":[],"optIn":[]},"extraModels":[],
  "billing":{"defaultMode":"free_tier","freeTierAll":true,"freeTierModels":[],"alwaysFree":[],"trustListingFree":true,
   "verify":{"type":"paid_model_probe","paidOnlyModels":["^gemini-pro-latest$"]}},
  "limits":{"pool":"model"},
  "quota":{"headerPeriods":{"requests":"minute","tokens":"minute"},"plainHeaderMetric":"rpm"},"quirks":{}},
 {"id":"flat","label":"Flat Rate","description":"Sold by concurrency plan.","category":"paid",
  "adapter":"openai_compatible","capabilities":["chat"],"media":{},"live":[],
  "baseUrl":"http://127.0.0.1:9/flat/v1","editableBaseUrl":false,"baseUrlSchemes":["http","https"],
  "auth":{"type":"bearer"},"keyOptional":false,"fields":[],"headers":{},
  "models":{"extraPaths":[],"optIn":[]},"extraModels":[],
  "billing":{"defaultMode":"flat_rate","flatRate":{"defaultPlan":"basic","plans":[{"id":"basic","label":"Basic","concurrent":2}]},
   "freeTierAll":false,"freeTierModels":[],"alwaysFree":[],"trustListingFree":true},
  "limits":{"pool":"model"},
  "quota":{"headerPeriods":{"requests":"minute","tokens":"minute"},"plainHeaderMetric":"rpm"},"quirks":{}},
 {"id":"chatgpt","label":"ChatGPT","description":"Your ChatGPT plan.","category":"plan","connect":"codex_device",
  "adapter":"codex","capabilities":["chat"],"media":{},"live":[],
  "baseUrl":"http://127.0.0.1:9/backend-api/codex","editableBaseUrl":false,"baseUrlSchemes":["http","https"],
  "auth":{"type":"bearer"},"keyOptional":true,"fields":[],"headers":{},
  "models":{"extraPaths":[],"optIn":[]},"extraModels":[],
  "billing":{"defaultMode":"plan","plan":{"note":"Uses your ChatGPT plan limits."},
   "freeTierAll":false,"freeTierModels":[],"alwaysFree":[],"trustListingFree":true},
  "limits":{"pool":"model"},
  "quota":{"headerPeriods":{"requests":"minute","tokens":"minute"},"plainHeaderMetric":"rpm"},"quirks":{}},
 {"id":"nous","label":"Nous Portal","description":"Portal credits.","category":"paid","connect":"nous_device",
  "adapter":"nous","capabilities":["chat"],"media":{},"live":[],
  "baseUrl":"http://127.0.0.1:9/v1","editableBaseUrl":false,"baseUrlSchemes":["http","https"],
  "auth":{"type":"bearer"},"keyOptional":true,"fields":[],"headers":{},
  "models":{"extraPaths":[],"optIn":[]},"extraModels":[],
  "billing":{"defaultMode":"paid","freeTierAll":false,"freeTierModels":[],"alwaysFree":[":free$"],"trustListingFree":true},
  "limits":{"pool":"model"},
  "quota":{"headerPeriods":{"requests":"minute","tokens":"minute"},"plainHeaderMetric":"rpm"},"quirks":{}},
 {"id":"neorouter_local","label":"NeoRouter Local","description":"Models on this server.","category":"local",
  "adapter":"local_runtime","capabilities":["chat"],"media":{},"live":[],
  "baseUrl":"http://127.0.0.1:11434/v1","editableBaseUrl":false,"baseUrlSchemes":["http","https"],
  "auth":{"type":"none"},"keyOptional":true,"fields":[],"headers":{},
  "models":{"extraPaths":[],"optIn":[]},"extraModels":[],
  "billing":{"defaultMode":"free_tier","freeTierAll":true,"freeTierModels":[],"alwaysFree":[],"trustListingFree":true},
  "limits":{"pool":"model"},
  "quota":{"headerPeriods":{"requests":"minute","tokens":"minute"},"plainHeaderMetric":"rpm"},"quirks":{}},
 {"id":"neorouter_peer","label":"Pool Partner","description":"A pool member.","category":"pool",
  "adapter":"openai_compatible","capabilities":["chat"],"media":{},"live":[],
  "baseUrl":"relay://peer","editableBaseUrl":false,"baseUrlSchemes":["relay"],
  "auth":{"type":"bearer"},"keyOptional":true,"fields":[],"headers":{},
  "models":{"extraPaths":[],"optIn":[]},"extraModels":[],
  "billing":{"defaultMode":"free_tier","freeTierAll":true,"freeTierModels":[],"alwaysFree":[],"trustListingFree":true},
  "limits":{"pool":"model"},
  "quota":{"headerPeriods":{"requests":"minute","tokens":"minute"},"plainHeaderMetric":"rpm"},"quirks":{}}
]`

// testConfig is the configuration the account tests run with.
func testConfig() config.Config {
	return config.Config{
		AllowPrivateProviderURLs: true,
		Routing: config.Routing{
			SlotQueueWait: 45 * time.Second,
			SlotMaxWait:   300 * time.Second,
		},
	}
}

// testDB is a migrated temp database with one admin and one member.
func testDB(t *testing.T) *dbx.DB {
	t.Helper()
	db, err := dbx.Open(context.Background(), filepath.Join(t.TempDir(), "neorouter.sqlite3"), dbx.Options{})
	if err != nil {
		t.Fatalf("dbx.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := schema.Ensure(context.Background(), db.W); err != nil {
		t.Fatalf("schema.Ensure: %v", err)
	}
	for _, user := range []struct{ id, role string }{{"admin", "admin"}, {"member", "user"}} {
		if _, err := db.W.ExecContext(context.Background(),
			`INSERT INTO users (id, username, email, password_hash, role) VALUES (?, ?, ?, 'x', ?)`,
			user.id, user.id, user.id+"@example.test", user.role); err != nil {
			t.Fatalf("insert user: %v", err)
		}
	}
	return db
}

// fakeAdapter is one protocol dialect with scripted answers.
type fakeAdapter struct {
	definition Definition
	account    Account
	factory    *fakeFactory
}

func (a *fakeAdapter) Definition() Definition { return a.definition }

func (a *fakeAdapter) Validate(ctx context.Context) (Validation, error) {
	a.factory.mu.Lock()
	validate := a.factory.validate
	a.factory.mu.Unlock()
	if validate != nil {
		return validate(a.account)
	}
	return ValidKey(), nil
}

func (a *fakeAdapter) ListModels(ctx context.Context) ([]Model, error) {
	a.factory.mu.Lock()
	defer a.factory.mu.Unlock()
	if a.factory.listErr != nil {
		return nil, a.factory.listErr
	}
	return a.factory.models, nil
}

func (a *fakeAdapter) FetchQuota(ctx context.Context) ([]QuotaWindow, error) {
	a.factory.mu.Lock()
	defer a.factory.mu.Unlock()
	return a.factory.quota, a.factory.quotaErr
}

func (a *fakeAdapter) Supports(method Capability) bool { return method == CapChat }

func (a *fakeAdapter) TranscribeTranslate() bool { return false }

func (a *fakeAdapter) TranscribeTimestamps() bool { return false }

// fakeFactory builds the adapters of every definition and records the accounts
// they were built with.
type fakeFactory struct {
	mu       sync.Mutex
	validate func(Account) (Validation, error)
	models   []Model
	listErr  error
	quota    []QuotaWindow
	quotaErr error
	built    []Account
}

func (f *fakeFactory) factory(def Definition, deps AdapterDeps) (Adapter, error) {
	f.mu.Lock()
	f.built = append(f.built, deps.Account)
	f.mu.Unlock()
	return &fakeAdapter{definition: def, account: deps.Account, factory: f}, nil
}

// builtAccounts is what the factory was asked to build, oldest first.
func (f *fakeFactory) builtAccounts() []Account {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]Account, len(f.built))
	copy(out, f.built)
	return out
}

// fakeCatalog records the catalog calls a service makes.
type fakeCatalog struct {
	mu           sync.Mutex
	synced       []string
	reclassified []string
	flagged      []int64
	syncResult   any
	syncErr      error
}

func (c *fakeCatalog) SyncAccount(ctx context.Context, accountID string) (any, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.synced = append(c.synced, accountID)
	if c.syncErr != nil {
		return nil, c.syncErr
	}
	if c.syncResult != nil {
		return c.syncResult, nil
	}
	return map[string]any{"models": 1}, nil
}

func (c *fakeCatalog) ReclassifyAccount(ctx context.Context, accountID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.reclassified = append(c.reclassified, accountID)
	return nil
}

func (c *fakeCatalog) FlagModel(ctx context.Context, ref int64, flags map[string]any) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.flagged = append(c.flagged, ref)
	return true, nil
}

// fakeLimits is the stated-limits store.
type fakeLimits struct {
	mu     sync.Mutex
	values map[string]map[string]float64
	sets   []map[string]*float64
}

func newFakeLimits() *fakeLimits { return &fakeLimits{values: map[string]map[string]float64{}} }

func (l *fakeLimits) Get(accountID string) map[string]float64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := map[string]float64{}
	for metric, value := range l.values[accountID] {
		out[metric] = value
	}
	return out
}

func (l *fakeLimits) Set(accountID string, patch map[string]*float64, now time.Time) (map[string]float64, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.sets = append(l.sets, patch)
	if l.values[accountID] == nil {
		l.values[accountID] = map[string]float64{}
	}
	for metric, value := range patch {
		if value == nil {
			delete(l.values[accountID], metric)
			continue
		}
		l.values[accountID][metric] = *value
	}
	out := map[string]float64{}
	for metric, value := range l.values[accountID] {
		out[metric] = value
	}
	return out, nil
}

// fakeQuota records the probe results a service stores.
type fakeQuota struct {
	mu     sync.Mutex
	states []quotaStateRecord
}

type quotaStateRecord struct {
	Scope   string
	Metric  string
	Limit   *float64
	Remain  *float64
	ResetAt *int64
	Source  string
}

func (q *fakeQuota) SetState(scope, metric string, obs QuotaObservation, source string, now time.Time) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.states = append(q.states, quotaStateRecord{Scope: scope, Metric: metric, Limit: obs.Limit, Remain: obs.Remaining, ResetAt: obs.ResetAt, Source: source})
	return nil
}

// fakeLocalModels is the local-model sharing policy.
type fakeLocalModels struct {
	allowed bool
	offered bool
}

func (f fakeLocalModels) Allowed(string) bool { return f.allowed }
func (f fakeLocalModels) Offered(string) bool { return f.offered }

// testService builds a provider service over a temp database, with every
// consumed domain faked and recorded.
func testService(t *testing.T) (*Service, *fakeFactory) {
	t.Helper()
	return testServiceWith(t, nil)
}

// testServiceWith builds a service and lets a test adjust it before use.
func testServiceWith(t *testing.T, adjust func(*Service)) (*Service, *fakeFactory) {
	t.Helper()
	db := testDB(t)
	factory := &fakeFactory{}
	registry, err := Parse([]byte(testDefinitions), factory.factory, func(string) string { return "" })
	if err != nil {
		t.Fatalf("Parse definitions: %v", err)
	}
	sealer, err := NewSealerFromKey(bytes.Repeat([]byte{7}, SecretKeySize))
	if err != nil {
		t.Fatalf("NewSealerFromKey: %v", err)
	}
	service := &Service{
		DB:              db,
		Registry:        registry,
		Cfg:             testConfig,
		Log:             slog.New(slog.NewTextHandler(io.Discard, nil)),
		Secrets:         sealer,
		HTTP:            NewClient(Options{}),
		UpstreamTimeout: 10 * time.Second,
		Getenv:          func(string) string { return "" },
		Catalog:         &fakeCatalog{},
		Limits:          newFakeLimits(),
		Quota:           &fakeQuota{},
		LocalModels:     fakeLocalModels{allowed: true, offered: true},
	}
	if adjust != nil {
		adjust(service)
	}
	return service, factory
}

// createAccount adds a connection the terse way most tests need.
func createAccount(t *testing.T, service *Service, userID, definitionID string, fields ...func(*Input)) Created {
	t.Helper()
	definition := definitionID
	in := Input{DefinitionID: Optional[string]{Set: true, Value: &definition}}
	if definition == "custom_openai" {
		base := "http://127.0.0.1:9/v1"
		secret := "sk-test"
		in.BaseURL = Optional[string]{Set: true, Value: &base}
		in.Secret = Optional[string]{Set: true, Value: &secret}
	}
	for _, field := range fields {
		field(&in)
	}
	created, err := service.Create(context.Background(), userID, in, userID, nil)
	if err != nil {
		t.Fatalf("Create(%s): %v", definitionID, err)
	}
	return created
}

// withSecret sets the account key of a create input.
func withSecret(secret string) func(*Input) {
	return func(in *Input) { in.Secret = Optional[string]{Set: true, Value: &secret} }
}

// withBilling sets the billing mode of a create or update input.
func withBilling(mode string) func(*Input) {
	return func(in *Input) { in.BillingMode = Optional[string]{Set: true, Value: &mode} }
}

// withOption sets one account option.
func withOption(name string, value any) func(*Input) {
	return func(in *Input) {
		if !in.Options.Set {
			options := map[string]any{}
			in.Options = Optional[map[string]any]{Set: true, Value: &options}
		}
		(*in.Options.Value)[name] = value
	}
}

// ptrFloat is a pointer to a limit value.
func ptrFloat(value float64) *float64 { return &value }

// accountRow reads one stored row regardless of who owns it.
func accountRow(t *testing.T, service *Service, accountID string) Row {
	t.Helper()
	row, err := service.queryRow(context.Background(), `SELECT `+rowColumns+` FROM provider_accounts WHERE id = ?`, accountID)
	if err != nil {
		t.Fatalf("read account row: %v", err)
	}
	return row
}

// countRows counts the rows of a table.
func countRows(t *testing.T, db *sql.DB, query string, args ...any) int {
	t.Helper()
	var value int
	if err := db.QueryRow(query, args...).Scan(&value); err != nil {
		t.Fatalf("count: %v", err)
	}
	return value
}

// errorCode is the machine code of an API error.
func errorCode(t *testing.T, err error) string {
	t.Helper()
	var httpErr *httpserver.HttpError
	if !errors.As(err, &httpErr) {
		t.Fatalf("not an API error: %v", err)
	}
	return httpErr.Code
}

// mustJSON encodes a value, failing the test when it cannot.
func mustJSON(t *testing.T, value any) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	return string(encoded)
}
