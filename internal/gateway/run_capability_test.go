package gateway

// The shared pipeline internal/media consumes: one non-chat request through
// routing, quota, budget and usage.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
	"github.com/NeoLabs-Systems/neorouter/internal/providers"
	"github.com/NeoLabs-Systems/neorouter/internal/routerkeys"
	"github.com/NeoLabs-Systems/neorouter/internal/routing"
)

// mediaModel adds one catalog model of another kind, served by an account.
func mediaModel(f *fixture, slug, modelID, capability string) *routing.Entry {
	f.t.Helper()
	accountID := f.accounts[slug]
	if accountID == "" {
		accountID = f.account(slug, "free_tier")
	}
	f.nextRef++
	if _, err := f.db.W.Exec(`INSERT INTO models (account_id, model_id, canonical_id, capability) VALUES (?, ?, ?, ?)`, accountID, modelID, modelID, capability); err != nil {
		f.t.Fatalf("insert media model: %v", err)
	}
	entry := &routing.Entry{
		Ref: f.nextRef, ID: slug + "/" + modelID, ModelID: modelID, Name: modelID, CanonicalID: modelID,
		Capability: capability, AccountID: accountID, UserID: f.userID,
		ProviderSlug: slug, ProviderKey: slug, ProviderName: slug, DefinitionID: "custom_openai",
		BillingMode: "free_tier", OverflowPolicy: "auto", Enabled: true, Available: true,
		AccountEnabled: true, AccountStatus: "healthy", IsFree: true,
	}
	f.catalog.mu.Lock()
	f.catalog.entries = append(f.catalog.entries, entry)
	f.catalog.mu.Unlock()
	return entry
}

func TestRunCapabilityAnswersAndRecords(t *testing.T) {
	f := newFixture(t, nil)
	mediaModel(f, "embed", "text-embedding-3", "embedding")
	secret, _ := f.key(freeKeyMode)
	row, err := f.keys.Authenticate(secret)
	if err != nil || row == nil {
		t.Fatalf("authenticate: %v", err)
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/embeddings", nil)
	responded := false
	result, entry, runErr := f.service.RunCapability(&CapabilityRun{
		W:              recorder,
		R:              request,
		Key:            keyFromRow(row),
		Capability:     "embedding",
		Method:         providers.CapEmbed,
		RequestedModel: "auto",
		InputTokens:    12,
		Units:          1,
		Invoke: func(ctx context.Context, entry *routing.Entry, adapter providers.Adapter, timeout time.Duration) (*CapabilityResult, error) {
			if entry.ID != "embed/text-embedding-3" {
				t.Errorf("entry = %q", entry.ID)
			}
			return &CapabilityResult{Value: newObjOf("vectors", 1)}, nil
		},
		CostOf: func(entry *routing.Entry, result *CapabilityResult) float64 { return 0 },
		EstimateCost: func(entry *routing.Entry) *float64 {
			return float64Ptr(0)
		},
		Respond: func(result *CapabilityResult, entry *routing.Entry) error {
			responded = true
			return nil
		},
	})
	if runErr != nil {
		t.Fatalf("RunCapability: %v", runErr)
	}
	if !responded {
		t.Error("Respond was not called")
	}
	if entry == nil || entry.ID != "embed/text-embedding-3" {
		t.Errorf("entry = %v", entry)
	}
	if result == nil || result.Value == nil {
		t.Error("no result")
	}
	if model := recorder.Header().Get("X-NeoRouter-Model"); model != "embed/text-embedding-3" {
		t.Errorf("X-NeoRouter-Model = %q", model)
	}
	if trail := recorder.Header().Get("X-NeoRouter-Trail"); trail != "embed/text-embedding-3" {
		t.Errorf("X-NeoRouter-Trail = %q", trail)
	}
	var capability, status string
	var units, inputTokens int64
	if err := f.db.R.QueryRow(`SELECT capability, status, units, input_tokens FROM requests LIMIT 1`).
		Scan(&capability, &status, &units, &inputTokens); err != nil {
		t.Fatalf("usage row: %v", err)
	}
	if capability != "embedding" || status != "success" || units != 1 || inputTokens != 12 {
		t.Errorf("row = %q %q units=%d tokens=%d", capability, status, units, inputTokens)
	}
}

func TestRunCapabilityFallsBackAcrossProviders(t *testing.T) {
	f := newFixture(t, nil)
	broken := mediaModel(f, "broken", "text-embedding-3", "embedding")
	working := mediaModel(f, "good", "text-embedding-3", "embedding")
	secret, _ := f.key(freeKeyMode)
	row, _ := f.keys.Authenticate(secret)
	var tried []string
	result, entry, runErr := f.service.RunCapability(&CapabilityRun{
		R:              httptest.NewRequest(http.MethodPost, "/v1/embeddings", nil),
		Key:            keyFromRow(row),
		Capability:     "embedding",
		Method:         providers.CapEmbed,
		RequestedModel: "text-embedding-3",
		InputTokens:    5,
		Invoke: func(ctx context.Context, attempt *routing.Entry, adapter providers.Adapter, timeout time.Duration) (*CapabilityResult, error) {
			tried = append(tried, attempt.ID)
			if attempt.Ref == broken.Ref {
				return nil, providers.NewError(providers.KindServer, "The provider failed.", providers.ErrorOptions{Status: 500})
			}
			return &CapabilityResult{Value: "ok"}, nil
		},
		CostOf: func(entry *routing.Entry, result *CapabilityResult) float64 { return 0 },
	})
	if runErr != nil {
		t.Fatalf("RunCapability: %v", runErr)
	}
	if entry == nil || entry.Ref != working.Ref {
		t.Fatalf("entry = %v", entry)
	}
	if result == nil || result.Value != "ok" {
		t.Error("no result")
	}
	if len(tried) < 2 {
		t.Errorf("tried = %v", tried)
	}
	var trailJSON string
	if err := f.db.R.QueryRow(`SELECT trail_json FROM requests WHERE status = 'success' LIMIT 1`).Scan(&trailJSON); err != nil {
		t.Fatalf("trail: %v", err)
	}
	if !contains(trailJSON, `"outcome":"server"`) {
		t.Errorf("trail = %q", trailJSON)
	}
	if !contains(trailJSON, `"outcome":"success"`) {
		t.Errorf("no success step in %q", trailJSON)
	}
}

func TestRunCapabilityExplainsAnEmptyCandidateList(t *testing.T) {
	f := newFixture(t, nil)
	secret, _ := f.key(freeKeyMode)
	row, _ := f.keys.Authenticate(secret)
	_, _, runErr := f.service.RunCapability(&CapabilityRun{
		W:              httptest.NewRecorder(),
		R:              httptest.NewRequest(http.MethodPost, "/v1/embeddings", nil),
		Key:            keyFromRow(row),
		Capability:     "embedding",
		Method:         providers.CapEmbed,
		RequestedModel: "auto",
		OnNoCandidate: func() error {
			return httpserver.New(http.StatusServiceUnavailable, "NO_EMBEDDING_MODEL", "No embedding model is connected.")
		},
	})
	if runErr == nil {
		t.Fatal("expected an error")
	}
	problem := errorOf(runErr)
	if problem.status != http.StatusServiceUnavailable || problem.code != "NO_EMBEDDING_MODEL" {
		t.Errorf("error = %+v", problem)
	}
}

func TestRunCapabilityRefusesAModelTheKeyMayNotUse(t *testing.T) {
	f := newFixture(t, nil)
	entry := mediaModel(f, "paid", "text-embedding-3", "embedding")
	entry.IsFree = false
	entry.BillingMode = "paid"
	secret, _ := f.key(freeKeyMode)
	row, _ := f.keys.Authenticate(secret)
	_, _, runErr := f.service.RunCapability(&CapabilityRun{
		W:              httptest.NewRecorder(),
		R:              httptest.NewRequest(http.MethodPost, "/v1/embeddings", nil),
		Key:            keyFromRow(row),
		Capability:     "embedding",
		Method:         providers.CapEmbed,
		RequestedModel: "paid/text-embedding-3",
	})
	if runErr == nil {
		t.Fatal("expected an error")
	}
	problem := errorOf(runErr)
	if problem.status != http.StatusForbidden || problem.code != "MODEL_NOT_ALLOWED" {
		t.Errorf("error = %+v", problem)
	}
	if problem.message != `The model "paid/text-embedding-3" is not free and this key only allows free models.` {
		t.Errorf("message = %q", problem.message)
	}
}

func TestRunCapabilityRateLimitsAKey(t *testing.T) {
	f := newFixture(t, nil)
	mediaModel(f, "embed", "text-embedding-3", "embedding")
	rpm := int64(1)
	secret, _ := f.key(func(input *routerkeys.Input) {
		freeKeyMode(input)
		input.RPMLimit = routerkeys.Field[int64]{Set: true, Value: &rpm}
	})
	row, _ := f.keys.Authenticate(secret)
	run := func() error {
		_, _, err := f.service.RunCapability(&CapabilityRun{
			W:              httptest.NewRecorder(),
			R:              httptest.NewRequest(http.MethodPost, "/v1/embeddings", nil),
			Key:            keyFromRow(row),
			Capability:     "embedding",
			Method:         providers.CapEmbed,
			RequestedModel: "auto",
			Invoke: func(ctx context.Context, entry *routing.Entry, adapter providers.Adapter, timeout time.Duration) (*CapabilityResult, error) {
				return &CapabilityResult{}, nil
			},
			CostOf: func(entry *routing.Entry, result *CapabilityResult) float64 { return 0 },
		})
		return err
	}
	if err := run(); err != nil {
		t.Fatalf("first request: %v", err)
	}
	second := run()
	if second == nil {
		t.Fatal("the second request was allowed")
	}
	problem := errorOf(second)
	if problem.status != http.StatusTooManyRequests || problem.code != "KEY_RATE_LIMITED" {
		t.Errorf("error = %+v", problem)
	}
}

func TestRunCapabilityKeepsAnHttpErrorFromTheCaller(t *testing.T) {
	f := newFixture(t, nil)
	mediaModel(f, "embed", "text-embedding-3", "embedding")
	secret, _ := f.key(freeKeyMode)
	row, _ := f.keys.Authenticate(secret)
	_, _, runErr := f.service.RunCapability(&CapabilityRun{
		W:              httptest.NewRecorder(),
		R:              httptest.NewRequest(http.MethodPost, "/v1/embeddings", nil),
		Key:            keyFromRow(row),
		Capability:     "embedding",
		Method:         providers.CapEmbed,
		RequestedModel: "auto",
		Invoke: func(ctx context.Context, entry *routing.Entry, adapter providers.Adapter, timeout time.Duration) (*CapabilityResult, error) {
			return nil, httpserver.New(http.StatusBadRequest, "EMBEDDING_PIN_MISMATCH", "The dimensions do not match the pinned model.")
		},
		CostOf: func(entry *routing.Entry, result *CapabilityResult) float64 { return 0 },
	})
	if runErr == nil {
		t.Fatal("expected an error")
	}
	problem := errorOf(runErr)
	if problem.status != http.StatusBadRequest || problem.code != "EMBEDDING_PIN_MISMATCH" {
		t.Errorf("error = %+v", problem)
	}
}

func TestRequireHandsTheKeyToTheHandler(t *testing.T) {
	f := newFixture(t, nil)
	f.mockProvider("mock", "free_tier", "ok-alpha")
	secret := freeKey(t, f)
	seen := false
	handler := f.service.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		row, ok := f.service.KeyRowOfRequest(r)
		if !ok || row == nil {
			t.Error("no key in the context")
		}
		seen = true
		w.WriteHeader(http.StatusNoContent)
	}))
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/embeddings", nil)
	request.Header.Set("x-api-key", secret)
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNoContent || !seen {
		t.Fatalf("status = %d seen = %v", recorder.Code, seen)
	}
	// A request without a key is answered in the gateway's error shape.
	blocked := httptest.NewRecorder()
	handler.ServeHTTP(blocked, httptest.NewRequest(http.MethodPost, "/v1/embeddings", nil))
	if blocked.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", blocked.Code)
	}
	if code := errorCode(t, blocked); code != "INVALID_API_KEY" {
		t.Errorf("code = %q", code)
	}
}

func TestToCatalogEntryCarriesWhatAnAnswerNames(t *testing.T) {
	f := newFixture(t, nil)
	entry := mediaModel(f, "embed", "text-embedding-3", "embedding")
	converted := ToCatalogEntry(entry)
	if converted.ID != entry.ID || converted.ModelID != entry.ModelID || converted.AccountID != entry.AccountID {
		t.Errorf("entry = %+v", converted)
	}
	if converted.ContextWindow != nil {
		t.Errorf("unknown context window = %v", *converted.ContextWindow)
	}
	if converted.ProviderIdentity.ProviderName != "embed" {
		t.Errorf("provider name = %+v", converted.ProviderIdentity)
	}
	computed := &routing.Entry{Ref: 7, ID: "a/b", ModelID: "b", Name: "B", Capability: "chat",
		ContextWindow: 128000, MaxOutput: 4096, PriceSource: "manual", FreeReason: "free_tier_account",
		ProviderSlug: "a", ProviderKey: "a", ProviderName: "A"}
	view := ToCatalogEntry(computed)
	if view.ContextWindow == nil || *view.ContextWindow != 128000 {
		t.Errorf("context window = %v", view.ContextWindow)
	}
	if view.MaxOutput == nil || *view.MaxOutput != 4096 {
		t.Errorf("max output = %v", view.MaxOutput)
	}
	if view.PriceSource == nil || *view.PriceSource != "manual" {
		t.Errorf("price source = %v", view.PriceSource)
	}
	if view.FreeReason == nil || *view.FreeReason != "free_tier_account" {
		t.Errorf("free reason = %v", view.FreeReason)
	}
}
