package media

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/dbx"
	"github.com/NeoLabs-Systems/neorouter/internal/catalog"
	"github.com/NeoLabs-Systems/neorouter/internal/config"
	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
	"github.com/NeoLabs-Systems/neorouter/internal/providers"
	"github.com/NeoLabs-Systems/neorouter/internal/routerkeys"
	"github.com/NeoLabs-Systems/neorouter/internal/schema"
)

// The tests drive the package the way the API does: a real temporary SQLite
// database, a real HTTP server over Routes and PlaygroundRoutes, and fakes for
// the three things media does not own (the shared pipeline, the provider
// accounts, the privacy shield).

const testSecret = "nrt_test_secret"

// testEnv is one media service with its fakes.
type testEnv struct {
	svc      *Service
	db       *dbx.DB
	runner   *fakeRunner
	accounts *fakeAccounts
	shield   fakeShield
	key      *routerkeys.Row
	clock    *testClock
	// videoRef is the id of the seeded video model row.
	videoRef int64
	// models is what the catalog lists per capability.
	models map[string][]catalog.Entry
}

// testClock is the service clock the tests move.
type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) Advance(delta time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(delta)
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	ctx := context.Background()
	db, err := dbx.Open(ctx, t.TempDir()+"/neorouter.sqlite3", dbx.Options{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := schema.Ensure(ctx, db.W); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	env := &testEnv{
		db:       db,
		runner:   &fakeRunner{accounts: map[string]*fakeAdapter{}, order: []string{}},
		accounts: &fakeAccounts{adapters: map[string]providers.Adapter{}},
		clock:    &testClock{now: time.UnixMilli(1_700_000_000_000)},
		models:   map[string][]catalog.Entry{},
	}
	env.seedUser(t, "user-1")
	env.key = env.seedKey(t, "key-1")
	env.videoRef = env.seedModel(t, "account-1", "sora-ok", "sora", "video")
	env.runner.ref = env.videoRef
	env.svc = New(Options{
		DB:  db,
		Log: slog.New(slog.DiscardHandler),
		Now: env.clock.Now,
		Config: func() config.Config {
			return config.Config{MaxBodyBytes: 2 << 20}
		},
		Catalog: func(_ context.Context, _ string, capability string, _ bool) ([]catalog.Entry, error) {
			return env.models[capability], nil
		},
		Accounts: env.accounts,
		Runner:   env.runner,
		Shield:   env.shield.shield,
		KeyRow:   env.keyRow,
		KeyAuth:  &fakeKeyAuth{key: env.key},
		Space: func(context.Context) (Space, bool) {
			return Space{OwnerID: "user-1", ActorID: "user-1"}, true
		},
	})
	return env
}

// seedUser inserts the user row the key and pin foreign keys need.
func (e *testEnv) seedUser(t *testing.T, id string) {
	t.Helper()
	if _, err := e.db.W.Exec(`INSERT INTO users (id, username, email, password_hash, role) VALUES (?, ?, ?, ?, 'user')`,
		id, "user-"+id, id+"@example.test", "x"); err != nil {
		t.Fatalf("seed user: %v", err)
	}
}

// seedKey inserts one router key row and returns it.
func (e *testEnv) seedKey(t *testing.T, id string) *routerkeys.Row {
	t.Helper()
	if _, err := e.db.W.Exec(`INSERT INTO router_keys (id, user_id, name, prefix, key_hash, mode, allowed_models_json)
		VALUES (?, 'user-1', ?, ?, ?, 'free_only', '[]')`, id, "key "+id, "nrt_"+id, "hash_"+id); err != nil {
		t.Fatalf("seed key: %v", err)
	}
	row, err := routerkeys.New(e.db).Row("user-1", id)
	if err != nil {
		t.Fatalf("read key: %v", err)
	}
	return row
}

// seedModel inserts one provider account and one model row, the way a synced
// connection has them (media_jobs.model_ref points at models.id).
func (e *testEnv) seedModel(t *testing.T, accountID, modelID, canonicalID, capability string) int64 {
	t.Helper()
	if _, err := e.db.W.Exec(`INSERT INTO provider_accounts (id, user_id, definition_id, slug, label, options_json, billing_mode, status)
		VALUES (?, 'user-1', 'custom_openai', ?, ?, '{}', 'free_tier', 'healthy')`, accountID, accountID, accountID); err != nil {
		t.Fatalf("seed account: %v", err)
	}
	result, err := e.db.W.Exec(`INSERT INTO models (account_id, model_id, canonical_id, capability, is_free)
		VALUES (?, ?, ?, ?, 1)`, accountID, modelID, canonicalID, capability)
	if err != nil {
		t.Fatalf("seed model: %v", err)
	}
	ref, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("model ref: %v", err)
	}
	return ref
}

// keyRow is the router key store the playground reads.
func (e *testEnv) keyRow(userID, keyID string) (*routerkeys.Row, error) {
	return routerkeys.New(e.db).Row(userID, keyID)
}

// server mounts the gateway and playground routes on a real HTTP server.
func (e *testEnv) server(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	e.svc.Routes(mux)
	e.svc.PlaygroundRoutes(mux)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

// do sends one request with the test router key and returns the response body.
func (e *testEnv) do(t *testing.T, server *httptest.Server, method, path string, body io.Reader, contentType string) (*http.Response, []byte) {
	t.Helper()
	request, err := http.NewRequest(method, server.URL+path, body)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	request.Header.Set("Authorization", "Bearer "+testSecret)
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatalf("send request: %v", err)
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	return response, payload
}

// doJSON sends one JSON request.
func (e *testEnv) doJSON(t *testing.T, server *httptest.Server, method, path string, body any) (*http.Response, []byte) {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("encode body: %v", err)
	}
	return e.do(t, server, method, path, stringsReader(string(encoded)), "application/json")
}

// fakeKeyAuth is the gateway's router-key middleware: one known bearer token.
type fakeKeyAuth struct {
	key *routerkeys.Row
}

type keyContextKey struct{}

func (f *fakeKeyAuth) Require(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+testSecret {
			httpserver.WriteGatewayError(w, http.StatusUnauthorized, "INVALID_API_KEY", "A valid NeoRouter key (nrt_…) is required.")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), keyContextKey{}, f.key)))
	})
}

func (f *fakeKeyAuth) Key(r *http.Request) (*routerkeys.Row, bool) {
	key, ok := r.Context().Value(keyContextKey{}).(*routerkeys.Row)
	return key, ok
}

// fakeAccounts resolves the adapter of one account.
type fakeAccounts struct {
	adapters map[string]providers.Adapter
}

func (f *fakeAccounts) Adapter(accountID string) providers.Adapter { return f.adapters[accountID] }

// fakeShield is the privacy shield: it hides "<secret>" and nothing else.
type fakeShield struct {
	fail bool
	seen *[]string
}

func (f *fakeShield) shield(_ string, texts []string) (Shielded, error) {
	if f.seen != nil {
		*f.seen = append(*f.seen, texts...)
	}
	if f.fail {
		return Shielded{}, fmt.Errorf("shield unavailable")
	}
	out := make([]string, 0, len(texts))
	hidden := 0
	for _, text := range texts {
		if text == "<secret>" {
			out = append(out, "<REDACTED>")
			hidden++
			continue
		}
		out = append(out, text)
	}
	return Shielded{Texts: out, Redacted: hidden}, nil
}

// fakeRunner is the shared pipeline: it records the options it was handed and
// runs the attempt against the adapters the test wired.
type fakeRunner struct {
	mu       sync.Mutex
	options  []*RunOptions
	outcomes []*Outcome
	// accounts are the adapters one of which the pipeline may attempt, in
	// registration order.
	accounts map[string]*fakeAdapter
	order    []string
	// ref is the models.id the candidate entry carries.
	ref int64
	// err fails the request instead of running an attempt.
	err error
}

func (f *fakeRunner) Run(ctx context.Context, options *RunOptions) (*Result, error) {
	f.mu.Lock()
	f.options = append(f.options, options)
	err := f.err
	f.mu.Unlock()
	if err != nil {
		return nil, err
	}
	entry, adapter, ok := f.candidate(options)
	if !ok {
		if options.NoCandidate != nil {
			if err := options.NoCandidate(); err != nil {
				return nil, err
			}
		}
		return nil, httpserver.New(http.StatusServiceUnavailable, "NO_MODEL_AVAILABLE", "No model is available for this request right now.")
	}
	outcome, err := options.Invoke(ctx, Attempt{Entry: entry, Adapter: adapter, Timeout: 5 * time.Second})
	if err != nil {
		// Like the pipeline: a failed attempt becomes the request's error.
		return nil, httpserver.New(http.StatusBadGateway, "ALL_ATTEMPTS_FAILED",
			"All 1 attempt(s) failed. Last error: "+err.Error())
	}
	f.mu.Lock()
	f.outcomes = append(f.outcomes, outcome)
	f.mu.Unlock()
	if options.CostOf != nil {
		options.CostOf(entry, outcome)
	}
	if options.Respond != nil && options.Response != nil {
		if err := options.Respond(options.Response, outcome, entry); err != nil {
			return nil, err
		}
	}
	return &Result{Outcome: outcome, Entry: entry}, nil
}

// candidate is the first account adapter the request's filter accepts.
func (f *fakeRunner) candidate(options *RunOptions) (catalog.Entry, providers.Adapter, bool) {
	for _, accountID := range f.order {
		adapter := f.accounts[accountID]
		entry := catalog.Entry{
			Ref: f.ref, ID: "mock/" + adapter.Definition().ID, ModelID: adapter.Definition().ID,
			CanonicalID: adapter.Definition().ID, AccountID: accountID,
			Capability: options.Capability, Enabled: true, Available: true, AccountEnabled: true,
			Decision: adapter.decision,
		}
		if options.Filter != nil && !options.Filter(entry, adapter) {
			continue
		}
		return entry, adapter, true
	}
	return catalog.Entry{}, nil, false
}

// lastOutcome is the most recent answer the pipeline produced.
func (f *fakeRunner) lastOutcome() *Outcome {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.outcomes) == 0 {
		return nil
	}
	return f.outcomes[len(f.outcomes)-1]
}

// last is the most recent options the pipeline was handed.
func (f *fakeRunner) last() *RunOptions {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.options) == 0 {
		return nil
	}
	return f.options[len(f.options)-1]
}

// useAdapter registers one adapter for the account the pipeline may pick.
func (e *testEnv) useAdapter(accountID string, adapter *fakeAdapter) *fakeAdapter {
	e.accounts.adapters[accountID] = adapter
	if _, known := e.runner.accounts[accountID]; !known {
		e.runner.order = append(e.runner.order, accountID)
	}
	e.runner.accounts[accountID] = adapter
	return adapter
}

// fakeAdapter is one provider adapter whose answers the tests decide.
type fakeAdapter struct {
	id         string
	caps       providers.Capabilities
	timestamps bool
	translate  bool
	// decision marks a System One decision model (the catalog's decision flag).
	decision bool

	mu           sync.Mutex
	calls        []string
	embedFn      func(providers.EmbedRequest) (*providers.EmbedResult, error)
	imageFn      func(providers.ImageRequest) (*providers.ImageResult, error)
	speechFn     func(providers.SpeechRequest) (*providers.SpeechResult, error)
	transcribeFn func(providers.TranscribeRequest) (*providers.Transcript, error)
	chatFn       func(json.RawMessage) (*providers.ChatResult, error)
	moderateFn   func(providers.ModerateRequest) (*providers.ModerationResult, error)
	decideFn     func(providers.DecideRequest) (*providers.DecideResult, error)
	videoFn      func(providers.VideoRequest) (*providers.VideoJob, error)
	statusFn     func(string) (*providers.VideoStatus, error)
	contentFn    func(string) (*http.Response, error)
	searchFn     func(providers.SearchRequest) (*providers.SearchResult, error)
	weatherFn    func(providers.WeatherRequest) (*providers.WeatherReport, error)
	voicesFn     func(string) ([]providers.Voice, error)
}

func newFakeAdapter(id string, caps ...providers.Capability) *fakeAdapter {
	return &fakeAdapter{id: id, caps: providers.Of(caps...)}
}

func (f *fakeAdapter) Definition() providers.Definition {
	return providers.Definition{ID: f.id, Label: f.id}
}

func (f *fakeAdapter) Validate(context.Context) (providers.Validation, error) {
	return providers.ValidKey(), nil
}

func (f *fakeAdapter) ListModels(context.Context) ([]providers.Model, error) { return nil, nil }

func (f *fakeAdapter) FetchQuota(context.Context) ([]providers.QuotaWindow, error) { return nil, nil }

func (f *fakeAdapter) Supports(method providers.Capability) bool { return f.caps.Supports(method) }

func (f *fakeAdapter) TranscribeTranslate() bool { return f.translate }

func (f *fakeAdapter) TranscribeTimestamps() bool { return f.timestamps }

func (f *fakeAdapter) record(call string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call)
}

func (f *fakeAdapter) recorded() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func (f *fakeAdapter) Embed(_ context.Context, req providers.EmbedRequest, _ providers.Call) (*providers.EmbedResult, error) {
	f.record("embed")
	if f.embedFn == nil {
		return nil, fmt.Errorf("no embed")
	}
	return f.embedFn(req)
}

func (f *fakeAdapter) GenerateImage(_ context.Context, req providers.ImageRequest, _ providers.Call) (*providers.ImageResult, error) {
	f.record("generateImage")
	if f.imageFn == nil {
		return nil, fmt.Errorf("no image")
	}
	return f.imageFn(req)
}

func (f *fakeAdapter) Speech(_ context.Context, req providers.SpeechRequest, _ providers.Call) (*providers.SpeechResult, error) {
	f.record("speech:" + req.Voice)
	if f.speechFn == nil {
		return nil, fmt.Errorf("no speech")
	}
	return f.speechFn(req)
}

func (f *fakeAdapter) Transcribe(_ context.Context, req providers.TranscribeRequest, _ providers.Call) (*providers.Transcript, error) {
	f.record("transcribe")
	if f.transcribeFn == nil {
		return nil, fmt.Errorf("no transcribe")
	}
	return f.transcribeFn(req)
}

func (f *fakeAdapter) Chat(_ context.Context, req json.RawMessage, _ providers.Call) (*providers.ChatResult, error) {
	f.record("chat")
	if f.chatFn == nil {
		return nil, fmt.Errorf("no chat")
	}
	return f.chatFn(req)
}

func (f *fakeAdapter) Moderate(_ context.Context, req providers.ModerateRequest, _ providers.Call) (*providers.ModerationResult, error) {
	f.record("moderate")
	if f.moderateFn == nil {
		return nil, fmt.Errorf("no moderate")
	}
	return f.moderateFn(req)
}

func (f *fakeAdapter) Decide(_ context.Context, req providers.DecideRequest, _ providers.Call) (*providers.DecideResult, error) {
	f.record("decide")
	if f.decideFn == nil {
		return nil, fmt.Errorf("no decide")
	}
	return f.decideFn(req)
}

func (f *fakeAdapter) CreateVideo(_ context.Context, req providers.VideoRequest, _ providers.Call) (*providers.VideoJob, error) {
	f.record("createVideo:" + req.Seconds + ":" + req.Size)
	if f.videoFn == nil {
		return nil, fmt.Errorf("no video")
	}
	return f.videoFn(req)
}

func (f *fakeAdapter) GetVideo(_ context.Context, upstreamID string) (*providers.VideoStatus, error) {
	f.record("getVideo")
	if f.statusFn == nil {
		return nil, fmt.Errorf("no status")
	}
	return f.statusFn(upstreamID)
}

func (f *fakeAdapter) VideoContent(_ context.Context, upstreamID string) (*http.Response, error) {
	f.record("videoContent")
	if f.contentFn == nil {
		return nil, fmt.Errorf("no content")
	}
	return f.contentFn(upstreamID)
}

func (f *fakeAdapter) Search(_ context.Context, req providers.SearchRequest) (*providers.SearchResult, error) {
	f.record("search")
	if f.searchFn == nil {
		return nil, fmt.Errorf("no search")
	}
	return f.searchFn(req)
}

func (f *fakeAdapter) Weather(_ context.Context, req providers.WeatherRequest) (*providers.WeatherReport, error) {
	f.record("weather")
	if f.weatherFn == nil {
		return nil, fmt.Errorf("no weather")
	}
	return f.weatherFn(req)
}

func (f *fakeAdapter) Voices(_ context.Context, modelID string) ([]providers.Voice, error) {
	f.record("voices")
	if f.voicesFn == nil {
		return nil, fmt.Errorf("no voices")
	}
	return f.voicesFn(modelID)
}

// stringsReader is a string body reader.
func stringsReader(value string) io.Reader { return strings.NewReader(value) }

// decodeBody decodes a JSON response body, failing the test when it is not JSON.
func decodeBody(t *testing.T, payload []byte) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(payload, &body); err != nil {
		t.Fatalf("decode body %q: %v", payload, err)
	}
	return body
}

// errorCode is the code of a gateway error body.
func errorCode(t *testing.T, payload []byte) string {
	t.Helper()
	body := decodeBody(t, payload)
	failure, _ := body["error"].(map[string]any)
	code, _ := failure["code"].(string)
	return code
}

// errorMessage is the message of a gateway error body.
func errorMessage(t *testing.T, payload []byte) string {
	t.Helper()
	body := decodeBody(t, payload)
	failure, _ := body["error"].(map[string]any)
	message, _ := failure["message"].(string)
	return message
}
