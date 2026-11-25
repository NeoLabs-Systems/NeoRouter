package gateway

// The test fixture: a real routing service over a temp SQLite database, a real
// router-key service, and fake adapters, so the API-level oracles run against the
// real pipeline without a network or a parallel wave's code.

import (
	"context"
	"encoding/json"
	"iter"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/dbx"
	"github.com/NeoLabs-Systems/neorouter/internal/budget"
	"github.com/NeoLabs-Systems/neorouter/internal/config"
	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
	"github.com/NeoLabs-Systems/neorouter/internal/providers"
	"github.com/NeoLabs-Systems/neorouter/internal/quota"
	"github.com/NeoLabs-Systems/neorouter/internal/routerkeys"
	"github.com/NeoLabs-Systems/neorouter/internal/routing"
	"github.com/NeoLabs-Systems/neorouter/internal/schema"
	"github.com/NeoLabs-Systems/neorouter/internal/usage"
)

// --- catalog ---------------------------------------------------------------

type fakeCatalog struct {
	mu      sync.Mutex
	entries []*routing.Entry
}

func (c *fakeCatalog) ListForUser(userID string, q routing.CatalogQuery) []*routing.Entry {
	capability := q.Capability
	if capability == "" {
		capability = "chat"
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]*routing.Entry, 0, len(c.entries))
	for _, entry := range c.entries {
		if entry.UserID != userID || entry.Capability != capability {
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

// --- adapters --------------------------------------------------------------

// fakeAdapter is one provider dialect bound to one account. Its answer is a
// function so a test can hang, fail or stream as the case needs.
type fakeAdapter struct {
	mu     sync.Mutex
	chat   func(ctx context.Context, request *Obj, call providers.Call) (*providers.ChatResult, error)
	stream func(ctx context.Context, request *Obj, call providers.Call, yield func(*Obj) bool) error
	calls  []string
	// media answers one non-chat call (run_capability).
	media func(ctx context.Context, request *Obj, call providers.Call) (*CapabilityResult, error)
}

func (a *fakeAdapter) Definition() providers.Definition {
	return providers.Definition{ID: "custom_openai"}
}

func (a *fakeAdapter) Validate(ctx context.Context) (providers.Validation, error) {
	return providers.ValidKey(), nil
}

func (a *fakeAdapter) ListModels(ctx context.Context) ([]providers.Model, error) { return nil, nil }

func (a *fakeAdapter) FetchQuota(ctx context.Context) ([]providers.QuotaWindow, error) {
	return nil, nil
}

func (a *fakeAdapter) Supports(method providers.Capability) bool {
	return method == providers.CapChat || method == providers.CapEmbed
}

func (a *fakeAdapter) TranscribeTranslate() bool  { return false }
func (a *fakeAdapter) TranscribeTimestamps() bool { return false }

func (a *fakeAdapter) Chat(ctx context.Context, request json.RawMessage, call providers.Call) (*providers.ChatResult, error) {
	parsed, err := parseObject(request)
	if err != nil {
		return nil, err
	}
	a.mu.Lock()
	a.calls = append(a.calls, call.ModelID)
	handler := a.chat
	a.mu.Unlock()
	if handler == nil {
		return nil, providers.NewError(providers.KindServer, "no chat handler", providers.ErrorOptions{})
	}
	return handler(ctx, parsed, call)
}

func (a *fakeAdapter) Stream(ctx context.Context, request json.RawMessage, call providers.Call) iter.Seq2[json.RawMessage, error] {
	parsed, err := parseObject(request)
	if err != nil {
		return func(yield func(json.RawMessage, error) bool) { yield(nil, err) }
	}
	a.mu.Lock()
	a.calls = append(a.calls, call.ModelID)
	handler := a.stream
	a.mu.Unlock()
	return func(yield func(json.RawMessage, error) bool) {
		if handler == nil {
			yield(nil, providers.NewError(providers.KindServer, "no stream handler", providers.ErrorOptions{}))
			return
		}
		_ = handler(ctx, parsed, call, func(chunk *Obj) bool {
			return yield(json.RawMessage(marshal(chunk)), nil)
		})
	}
}

func (a *fakeAdapter) callCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.calls)
}

// --- fixture ---------------------------------------------------------------

type fixture struct {
	t           *testing.T
	db          *dbx.DB
	cfg         config.Config
	routing     *routing.Service
	catalog     *fakeCatalog
	keys        *routerkeys.Keys
	quota       *quota.Service
	budget      *budget.Service
	usage       *usage.Service
	concurrency *routerkeys.Concurrency
	service     *Service
	mux         *http.ServeMux
	userID      string
	adapters    map[string]*fakeAdapter
	nextRef     int64
	accounts    map[string]string
	// search is the fake search engine run_capability/web search calls.
	search func(ctx context.Context, call SearchCall) (*providers.SearchResult, error)
	// feedback is what /v1/feedback recorded.
	feedback []feedbackRecord
}

// testConfig is the default configuration with a few keys overridden.
func testConfig(overrides map[string]string) config.Config {
	cfg, err := config.Load(func(key string) string { return overrides[key] }, "/tmp", "/tmp")
	if err != nil {
		panic(err)
	}
	return cfg
}

// newFixture builds a gateway over a real routing service and temp database.
func newFixture(t *testing.T, overrides map[string]string) *fixture {
	t.Helper()
	ctx := context.Background()
	db, err := dbx.Open(ctx, t.TempDir()+"/test.sqlite3", dbx.Options{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := schema.Ensure(ctx, db.W); err != nil {
		t.Fatalf("schema: %v", err)
	}
	if _, err := db.W.Exec(`INSERT INTO users (id, username, email, password_hash, role) VALUES ('u', 'u', 'u@test.local', 'x', 'admin')`); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	cfg := testConfig(overrides)
	f := &fixture{
		t:        t,
		db:       db,
		cfg:      cfg,
		catalog:  &fakeCatalog{},
		userID:   "u",
		adapters: map[string]*fakeAdapter{},
		accounts: map[string]string{},
	}
	f.routing = routing.New(routing.Options{
		DB:      db,
		Cfg:     func() config.Config { return f.cfg },
		Catalog: f.catalog,
		Random:  func() float64 { return 0.5 },
		// internal/app satisfies this in the real wiring; the fixture delegates
		// to the same router-key service.
		Keys: f,
	})
	f.quota = quota.New(db, nil)
	f.budget = budget.New(db)
	f.usage = usage.New(db)
	f.keys = routerkeys.New(db)
	f.concurrency = routerkeys.NewConcurrency()
	f.service = New(Options{
		Cfg:         func() config.Config { return f.cfg },
		Routing:     f.routing,
		Catalog:     f.catalog,
		Quota:       f.quota,
		Budget:      f.budget,
		Usage:       f.usage,
		Keys:        f.keys,
		Concurrency: f.concurrency,
		Adapters:    f,
		SearchWeb: func(ctx context.Context, call SearchCall) (*providers.SearchResult, error) {
			if f.search == nil {
				return nil, httpserver.New(http.StatusServiceUnavailable, "NO_MODEL_AVAILABLE", "No search engine answered.")
			}
			return f.search(ctx, call)
		},
		AnswerForRequest: func(requestID, keyID string) (*Answer, error) {
			var ref int64
			var label, task *string
			err := db.R.QueryRowContext(ctx, `SELECT model_ref, model_label, task FROM requests WHERE id = ? AND router_key_id = ? AND status = 'success'`, requestID, keyID).Scan(&ref, &label, &task)
			if err != nil {
				return nil, nil
			}
			answer := &Answer{ModelRef: ref}
			if label != nil {
				answer.ModelLabel = *label
			}
			if task != nil {
				answer.Task = *task
			}
			return answer, nil
		},
		RecordFeedback: func(modelRef int64, task string, good bool) {
			f.feedback = append(f.feedback, feedbackRecord{modelRef, task, good})
		},
	})
	f.mux = http.NewServeMux()
	f.service.Routes(f.mux)
	return f
}

// feedbackRecord is one recorded piece of feedback.
type feedbackRecord struct {
	ref  int64
	task string
	good bool
}

// Adapter implements the gateway's Adapters dependency.
func (f *fixture) Adapter(accountID string) providers.Adapter {
	adapter := f.adapters[accountID]
	if adapter == nil {
		return nil
	}
	return adapter
}

func (f *fixture) CanServe(accountID string, method providers.Capability) bool {
	adapter := f.adapters[accountID]
	return adapter != nil && adapter.Supports(method)
}

func (f *fixture) Timeout(accountID string) time.Duration { return 8 * time.Second }

func (f *fixture) Validate(accountID string) {}

func (f *fixture) LiveProtocols(accountID string) []string { return nil }

// RPMState is routing's view of a key's per-minute allowance.
func (f *fixture) RPMState(key routing.RouterKey, now time.Time) *routing.RPMState {
	row, err := f.keys.Row(key.UserID, key.ID)
	if err != nil || row == nil {
		return nil
	}
	state := f.keys.RPMStateOf(row, now)
	if state == nil {
		return nil
	}
	return &routing.RPMState{Limit: int(state.Limit), Remaining: int(state.Remaining), ResetSeconds: state.ResetSeconds}
}

// InFlight is routing's view of a key's requests in flight.
func (f *fixture) InFlight(key routing.RouterKey) int {
	return int(f.concurrency.InFlight(key.ID))
}

// account inserts a provider account and its adapter.
func (f *fixture) account(slug, billingMode string) string {
	f.t.Helper()
	id := "acc-" + slug
	_, err := f.db.W.Exec(`INSERT INTO provider_accounts (id, user_id, definition_id, slug, label, billing_mode, enabled, status, prepaid)
		VALUES (?, 'u', 'custom_openai', ?, ?, ?, 1, 'healthy', 0)`, id, slug, slug, billingMode)
	if err != nil {
		f.t.Fatalf("insert account %s: %v", slug, err)
	}
	if f.adapters[id] == nil {
		f.adapters[id] = &fakeAdapter{}
	}
	f.accounts[slug] = id
	return id
}

// model adds one catalog model served by an account's fake adapter.
func (f *fixture) model(slug, modelID string, overrides ...func(*routing.Entry)) *routing.Entry {
	f.t.Helper()
	accountID := f.accounts[slug]
	if accountID == "" {
		f.t.Fatalf("unknown account %s", slug)
	}
	f.nextRef++
	canonical := modelID
	result, err := f.db.W.Exec(`INSERT INTO models (account_id, model_id, canonical_id, capability) VALUES (?, ?, ?, 'chat')`, accountID, modelID, canonical)
	if err != nil {
		f.t.Fatalf("insert model: %v", err)
	}
	ref, err := result.LastInsertId()
	if err != nil {
		f.t.Fatalf("model id: %v", err)
	}
	entry := &routing.Entry{
		Ref:            ref,
		ID:             slug + "/" + modelID,
		ModelID:        modelID,
		Name:           modelID,
		CanonicalID:    canonical,
		Capability:     "chat",
		AccountID:      accountID,
		UserID:         f.userID,
		ProviderSlug:   slug,
		ProviderKey:    slug,
		ProviderName:   slug,
		DefinitionID:   "custom_openai",
		BillingMode:    "free_tier",
		OverflowPolicy: "auto",
		Enabled:        true,
		Available:      true,
		AccountEnabled: true,
		AccountStatus:  "healthy",
		ContextWindow:  128000,
		PriceIn:        float64Ptr(1),
		PriceOut:       float64Ptr(2),
		Intelligence:   float64Ptr(0.5),
		IsFree:         true,
	}
	for _, override := range overrides {
		override(entry)
	}
	f.catalog.mu.Lock()
	f.catalog.entries = append(f.catalog.entries, entry)
	f.catalog.mu.Unlock()
	return entry
}

// key creates a router key and returns its secret.
func (f *fixture) key(options func(*routerkeys.Input)) (string, *routerkeys.PublicKey) {
	f.t.Helper()
	input := routerkeys.Input{}
	name := "k"
	input.Name = routerkeys.Field[string]{Set: true, Value: &name}
	if options != nil {
		options(&input)
	}
	public, secret, err := f.keys.Create(f.userID, input, f.userID, nil)
	if err != nil {
		f.t.Fatalf("create key: %v", err)
	}
	return secret, public
}

// freeKeyMode sets a key to free_only.
func freeKeyMode(input *routerkeys.Input) {
	mode := routerkeys.ModeFreeOnly
	input.Mode = routerkeys.Field[string]{Set: true, Value: &mode}
}

// do runs one request against the gateway mux.
func (f *fixture) do(method, target, token string, body any, headers map[string]string) *httptest.ResponseRecorder {
	f.t.Helper()
	var reader *strings.Reader
	switch value := body.(type) {
	case nil:
		reader = strings.NewReader("")
	case string:
		reader = strings.NewReader(value)
	default:
		reader = strings.NewReader(marshalAny(body))
	}
	request := httptest.NewRequest(method, target, reader)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	recorder := httptest.NewRecorder()
	f.mux.ServeHTTP(recorder, request)
	return recorder
}

// chat posts a chat completion.
func (f *fixture) chat(token string, body *Obj) *httptest.ResponseRecorder {
	return f.do(http.MethodPost, "/v1/chat/completions", token, body, nil)
}

// decode parses a response body keeping key order.
func decode(t *testing.T, recorder *httptest.ResponseRecorder) *Obj {
	t.Helper()
	body, err := parseObject(recorder.Body.Bytes())
	if err != nil {
		t.Fatalf("response is not a JSON object: %v (%s)", err, recorder.Body.String())
	}
	return body
}

// errorCode is the error code of a failed response.
func errorCode(t *testing.T, recorder *httptest.ResponseRecorder) string {
	t.Helper()
	return getString(getObj(decode(t, recorder), "error"), "code")
}

// --- the mock provider -----------------------------------------------------

// okResponse is the canonical completion the mock provider answers with: "hello
// from <model>" and the usage block the JS mock reports.
func okResponse(modelID string) *Obj {
	message := newObjOf("role", "assistant", "content", "hello from "+strings.TrimPrefix(modelID, ""))
	usage := newObjOf("prompt_tokens", float64(4), "completion_tokens", float64(6), "total_tokens", float64(10))
	return BuildResponse(ResponseOptions{Model: modelID, Message: message, Usage: usage})
}

// mockChat is the mock provider's plain answer for one model id.
func mockChat(modelID string) func(ctx context.Context, request *Obj, call providers.Call) (*providers.ChatResult, error) {
	return func(ctx context.Context, request *Obj, call providers.Call) (*providers.ChatResult, error) {
		switch {
		case strings.HasPrefix(modelID, "fail-"):
			return nil, providers.NewError(providers.KindServer, "The provider failed.", providers.ErrorOptions{Status: 500})
		case strings.HasPrefix(modelID, "rl-"):
			return nil, providers.NewError(providers.KindRateLimit, "Rate limit reached", providers.ErrorOptions{Status: 429})
		case strings.HasPrefix(modelID, "empty-"):
			return &providers.ChatResult{Body: json.RawMessage(marshal(BuildResponse(ResponseOptions{
				Model:   modelID,
				Message: newObjOf("role", "assistant", "content", ""),
				Usage:   newObjOf("prompt_tokens", float64(4), "completion_tokens", float64(0), "total_tokens", float64(4)),
			})))}, nil
		case strings.HasPrefix(modelID, "tools-"):
			call := newObjOf("id", "call_1", "type", "function", "function", newObjOf("name", "lookup", "arguments", `{"q":"x"}`))
			message := newObjOf("role", "assistant", "content", nil, "tool_calls", []any{call})
			return &providers.ChatResult{Body: json.RawMessage(marshal(BuildResponse(ResponseOptions{
				Model:        modelID,
				Message:      message,
				FinishReason: "tool_calls",
				Usage:        newObjOf("prompt_tokens", float64(4), "completion_tokens", float64(6), "total_tokens", float64(10)),
			})))}, nil
		case modelID == "think-only":
			return &providers.ChatResult{Body: json.RawMessage(marshal(BuildResponse(ResponseOptions{
				Model:   modelID,
				Message: newObjOf("role", "assistant", "content", "", "reasoning_content", "thinking hard"),
				Usage:   newObjOf("prompt_tokens", float64(4), "completion_tokens", float64(6), "total_tokens", float64(10)),
			})))}, nil
		case modelID == "trunc-thinker" || modelID == "room-thinker":
			maxTokens := 0
			if value, ok := getNumber(request, "max_tokens"); ok {
				maxTokens = int(value)
			}
			if modelID == "trunc-thinker" || maxTokens < 100 {
				return &providers.ChatResult{Body: json.RawMessage(marshal(BuildResponse(ResponseOptions{
					Model:        modelID,
					Message:      newObjOf("role", "assistant", "content", "", "reasoning_content", "thinking"),
					FinishReason: "length",
					Usage:        newObjOf("prompt_tokens", float64(4), "completion_tokens", float64(50), "total_tokens", float64(54)),
				})))}, nil
			}
			return &providers.ChatResult{Body: json.RawMessage(marshal(BuildResponse(ResponseOptions{
				Model:   modelID,
				Message: newObjOf("role", "assistant", "content", "hello with 100"),
				Usage:   newObjOf("prompt_tokens", float64(4), "completion_tokens", float64(6), "total_tokens", float64(10)),
			})))}, nil
		case strings.HasPrefix(modelID, "hang-"):
			select {
			case <-time.After(3 * time.Second):
				return &providers.ChatResult{Body: json.RawMessage(marshal(okResponse(modelID)))}, nil
			case <-ctx.Done():
				return nil, providers.FromNetwork(ctx.Err())
			}
		}
		return &providers.ChatResult{Body: json.RawMessage(marshal(okResponse(modelID)))}, nil
	}
}

// mockStream is the mock provider's stream for one model id: the same answer in
// chunks.
func mockStream(modelID string) func(ctx context.Context, request *Obj, call providers.Call, yield func(*Obj) bool) error {
	return func(ctx context.Context, request *Obj, call providers.Call, yield func(*Obj) bool) error {
		switch {
		case strings.HasPrefix(modelID, "fail-"):
			return providers.NewError(providers.KindServer, "The provider failed.", providers.ErrorOptions{Status: 500})
		case strings.HasPrefix(modelID, "rl-"):
			return providers.NewError(providers.KindRateLimit, "Rate limit reached", providers.ErrorOptions{Status: 429})
		case strings.HasPrefix(modelID, "empty-"):
			// A stream that ends without output.
			yield(BuildChunk("chunk-1", modelID, 1, newObj(), nil, nil))
			return nil
		case strings.HasPrefix(modelID, "hang-"):
			<-ctx.Done()
			return providers.FromNetwork(ctx.Err())
		case strings.HasPrefix(modelID, "tools-"):
			first := BuildChunk("chunk-1", modelID, 1, newObjOf("tool_calls", []any{newObjOf(
				"index", float64(0), "id", "call_1", "function", newObjOf("name", "lookup", "arguments", `{"q":`),
			)}), nil, nil)
			second := BuildChunk("chunk-2", modelID, 1, newObjOf("tool_calls", []any{newObjOf(
				"index", float64(0), "function", newObjOf("arguments", `"x"}`),
			)}), nil, nil)
			last := BuildChunk("chunk-3", modelID, 1, newObj(), "tool_calls", newObjOf("prompt_tokens", float64(4), "completion_tokens", float64(6), "total_tokens", float64(10)))
			if !yield(first) || !yield(second) {
				return nil
			}
			yield(last)
			return nil
		}
		first := BuildChunk("chunk-1", modelID, 1, newObjOf("role", "assistant", "content", ""), nil, nil)
		second := BuildChunk("chunk-2", modelID, 1, newObjOf("content", "hello "), nil, nil)
		third := BuildChunk("chunk-3", modelID, 1, newObjOf("content", "from "+modelID), nil, nil)
		last := BuildChunk("chunk-4", modelID, 1, newObj(), "stop", newObjOf("prompt_tokens", float64(4), "completion_tokens", float64(6), "total_tokens", float64(10)))
		for _, chunk := range []*Obj{first, second, third, last} {
			if !yield(chunk) {
				return nil
			}
		}
		return nil
	}
}

// mockProvider wires one account's adapter to the mock provider.
func (f *fixture) mockProvider(slug, billingMode string, models ...string) {
	f.t.Helper()
	id := f.account(slug, billingMode)
	adapter := f.adapters[id]
	adapter.chat = func(ctx context.Context, request *Obj, call providers.Call) (*providers.ChatResult, error) {
		return mockChat(call.ModelID)(ctx, request, call)
	}
	adapter.stream = func(ctx context.Context, request *Obj, call providers.Call, yield func(*Obj) bool) error {
		return mockStream(call.ModelID)(ctx, request, call, yield)
	}
	for _, modelID := range models {
		f.model(slug, modelID)
	}
}
