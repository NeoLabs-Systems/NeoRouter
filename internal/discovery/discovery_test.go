package discovery

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/dbx"
	"github.com/NeoLabs-Systems/neorouter/internal/schema"
)

// fakeCatalog is the catalog projection the tests drive, standing in for
// internal/catalog's metadata service.
type fakeCatalog struct {
	snapshot Snapshot
	variants map[string]variant
}

type variant struct {
	base  string
	share float64
}

func (f *fakeCatalog) DiscoverySnapshot() Snapshot { return f.snapshot }

// BenchmarkIdentity is the catalog's canonicalization (see identity_double_test.go).
func (f *fakeCatalog) BenchmarkIdentity(modelID, capability string) string {
	return canonicalID(modelID)
}

func (f *fakeCatalog) VariantBase(modelID string) (string, float64, bool) {
	found, ok := f.variants[modelID]
	if !ok {
		return "", 0, false
	}
	return found.base, found.share, true
}

// fixture is one discovery install: a real database and the two services.
type fixture struct {
	service   *Service
	benchmark *Benchmarks
	news      *News
	catalog   *fakeCatalog
	mux       *http.ServeMux
	db        *dbx.DB
}

// withSession stands in for requireSession and requirePermission: the real chain
// arrives with internal/app.
func withSession(permission string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"code":"AUTHENTICATION_REQUIRED","message":"A valid session is required."}}`))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func newFixture(t *testing.T, catalog *fakeCatalog) *fixture {
	t.Helper()
	dir := t.TempDir()
	db, err := dbx.Open(context.Background(), filepath.Join(dir, "neorouter.sqlite3"), dbx.Options{})
	if err != nil {
		t.Fatalf("dbx.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := schema.Ensure(context.Background(), db.W); err != nil {
		t.Fatalf("schema.Ensure: %v", err)
	}
	benchmarks := &Benchmarks{DB: db, Catalog: catalog}
	news := &News{DB: db, Benchmarks: benchmarks, Catalog: catalog}
	service := &Service{Benchmarks: benchmarks, News: news}
	mux := http.NewServeMux()
	service.Routes(mux, withSession)
	return &fixture{service: service, benchmark: benchmarks, news: news, catalog: catalog, mux: mux, db: db}
}

func (f *fixture) get(t *testing.T, target string, authenticated bool) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	if authenticated {
		req.Header.Set("Authorization", "Bearer nrs_test")
	}
	rec := httptest.NewRecorder()
	f.mux.ServeHTTP(rec, req)
	return rec
}

// TestBenchmarksAreBoundedAndSearchable: the benchmark list is paged and
// searchable.
func TestBenchmarksAreBoundedAndSearchable(t *testing.T) {
	catalog := &fakeCatalog{snapshot: Snapshot{
		ArtificialAnalysis: []AAModel{{
			Slug: "measured-model", Name: "Measured model",
			IntelligenceIndex: ptr(50.0), Benchmarks: map[string]float64{"gpqa": 0.8},
		}},
		FetchedAt: map[string]string{"artificialAnalysis": "2026-01-03T12:00:00.000Z"},
	}}
	f := newFixture(t, catalog)

	if rec := f.get(t, "/api/v1/discovery/benchmarks", false); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous status = %d, want 401", rec.Code)
	}

	rec := f.get(t, "/api/v1/discovery/benchmarks?query=measured&limit=1", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	body := decode[struct {
		Models []struct {
			Name        string   `json:"name"`
			IsFree      bool     `json:"isFree"`
			Price       *float64 `json:"price"`
			Vendor      *string  `json:"vendor"`
			CanonicalID string   `json:"canonicalId"`
			Score       float64  `json:"score"`
		} `json:"models"`
		Total        int      `json:"total"`
		Offset       int      `json:"offset"`
		Limit        int      `json:"limit"`
		Capabilities []string `json:"capabilities"`
		Sources      []struct {
			ID         string  `json:"id"`
			UpdatedAt  *string `json:"updatedAt"`
			ModelCount int     `json:"modelCount"`
		} `json:"sources"`
	}](t, rec)
	if body.Total != 1 || len(body.Models) != 1 {
		t.Fatalf("body = %+v", body)
	}
	if body.Models[0].Name != "Measured model" || body.Models[0].IsFree {
		t.Fatalf("model = %+v", body.Models[0])
	}
	if body.Models[0].Price != nil || body.Models[0].Vendor != nil {
		t.Fatalf("model = %+v", body.Models[0])
	}
	if body.Limit != 1 {
		t.Fatalf("limit = %d", body.Limit)
	}
	if len(body.Capabilities) != 1 || body.Capabilities[0] != "chat" {
		t.Fatalf("capabilities = %v", body.Capabilities)
	}
	if len(body.Sources) != 4 || body.Sources[0].ModelCount != 1 {
		t.Fatalf("sources = %+v", body.Sources)
	}
	if body.Sources[0].UpdatedAt == nil || *body.Sources[0].UpdatedAt != "2026-01-03T12:00:00.000Z" {
		t.Fatalf("source updatedAt = %v", body.Sources[0].UpdatedAt)
	}
	if body.Sources[1].UpdatedAt != nil {
		t.Fatalf("a source without a fetch time must be null: %v", body.Sources[1].UpdatedAt)
	}

	for _, target := range []string{
		"/api/v1/discovery/benchmarks?limit=100000",
		"/api/v1/discovery/benchmarks?offset=-1",
		"/api/v1/discovery/benchmarks?free=yes",
		"/api/v1/discovery/benchmarks?capability=" + strings.Repeat("x", 51),
	} {
		rec := f.get(t, target, true)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s status = %d, want 400", target, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), `"code":"INVALID_QUERY"`) {
			t.Fatalf("%s body = %s", target, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "Invalid discovery filters.") {
			t.Fatalf("%s body = %s", target, rec.Body.String())
		}
	}

	empty := decode[struct {
		Total int `json:"total"`
	}](t, f.get(t, "/api/v1/discovery/benchmarks?query=absent", true))
	if empty.Total != 0 {
		t.Fatalf("absent query total = %d", empty.Total)
	}
	news := decode[struct {
		Total int `json:"total"`
	}](t, f.get(t, "/api/v1/discovery/news", true))
	if news.Total != 0 {
		t.Fatalf("news total = %d", news.Total)
	}
	if strings.Contains(rec.Body.String(), "secret_encrypted") {
		t.Fatal("the page leaks an encrypted column")
	}
}

// TestNewsPicksUpAFreeVariantOfAMeasuredModel is the second case: a free variant
// on a connected provider scores from its base, scaled down, while a weak free
// model stays below the median.
func TestNewsPicksUpAFreeVariantOfAMeasuredModel(t *testing.T) {
	fetchedAt := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	catalog := &fakeCatalog{
		snapshot: Snapshot{
			ArtificialAnalysis: []AAModel{
				{Slug: "hy4", Name: "Hy4", IntelligenceIndex: ptr(66.0), Benchmarks: map[string]float64{"gpqa": 0.9}},
				{Slug: "weak-model", Name: "Weak model", IntelligenceIndex: ptr(20.0), Benchmarks: map[string]float64{"gpqa": 0.2}},
			},
			// OpenRouter sells the variant; only the weak model is free there.
			OpenRouter: []OpenRouterModel{
				{ID: "tencent/hy4-flash", IsFree: false, PriceIn: ptr(0.035), PriceOut: ptr(0.14)},
				{ID: "vendor/weak-model:free", IsFree: true, PriceIn: ptr(0.0), PriceOut: ptr(0.0)},
			},
			FetchedAt: map[string]string{"openrouter": fetchedAt},
		},
		variants: map[string]variant{"tencent/hy4-flash:free": {base: "tencent/hy4", share: 0.8}},
	}
	f := newFixture(t, catalog)

	insertUser(t, f.db, "user-1", "reader")
	if _, err := f.db.W.Exec(`INSERT INTO provider_accounts (id, user_id, definition_id, slug, label, billing_mode)
		VALUES ('nous-test', 'user-1', 'nous', 'nous', 'Nous', 'free_tier')`); err != nil {
		t.Fatalf("insert account: %v", err)
	}
	if _, err := f.db.W.Exec(`INSERT INTO models (account_id, model_id, display_name, canonical_id, is_free, context_window)
		VALUES ('nous-test', 'tencent/hy4-flash:free', 'Hy4 Flash', 'hy4-flash', 1, 128000)`); err != nil {
		t.Fatalf("insert model: %v", err)
	}
	if err := f.news.Observe(context.Background()); err != nil {
		t.Fatalf("observe: %v", err)
	}

	rec := f.get(t, "/api/v1/discovery/news", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	page := decode[struct {
		Items []struct {
			ModelID       string  `json:"modelId"`
			Name          string  `json:"name"`
			Score         float64 `json:"score"`
			ContextWindow *int    `json:"contextWindow"`
			Available     bool    `json:"available"`
			URL           string  `json:"url"`
		} `json:"items"`
		Total     int     `json:"total"`
		AsOf      int64   `json:"asOf"`
		UpdatedAt *string `json:"updatedAt"`
	}](t, rec)
	var hy4 *struct {
		ModelID       string  `json:"modelId"`
		Name          string  `json:"name"`
		Score         float64 `json:"score"`
		ContextWindow *int    `json:"contextWindow"`
		Available     bool    `json:"available"`
		URL           string  `json:"url"`
	}
	for i, item := range page.Items {
		if item.ModelID == "tencent/hy4-flash:free" {
			hy4 = &page.Items[i]
		}
		if item.Name == "Weak model" {
			t.Fatalf("a below-median model made the news: %+v", item)
		}
	}
	if hy4 == nil {
		t.Fatalf("the free variant is missing: %+v", page.Items)
	}
	if hy4.Score <= 40 || hy4.Score >= 100 {
		t.Fatalf("score = %v, want it scaled down from its base", hy4.Score)
	}
	if hy4.ContextWindow == nil || *hy4.ContextWindow != 128000 {
		t.Fatalf("contextWindow = %v", hy4.ContextWindow)
	}
	if !hy4.Available {
		t.Fatal("the variant is still free, so it must be available")
	}
	if hy4.URL != "https://openrouter.ai/tencent/hy4-flash%3Afree" {
		t.Fatalf("url = %q", hy4.URL)
	}
	if page.UpdatedAt == nil || *page.UpdatedAt != fetchedAt {
		t.Fatalf("updatedAt = %v, want %q", page.UpdatedAt, fetchedAt)
	}
	if page.AsOf <= 0 {
		t.Fatalf("asOf = %d", page.AsOf)
	}
}

// TestNewsMarksAModelUnavailable keeps the persistent detection event: a model
// that is no longer free stays in the feed but reads as unavailable.
func TestNewsMarksAModelUnavailable(t *testing.T) {
	fetchedAt := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	catalog := &fakeCatalog{snapshot: Snapshot{
		ArtificialAnalysis: []AAModel{{Slug: "solo", Name: "Solo", Benchmarks: map[string]float64{"gpqa": 0.5}}},
		FetchedAt:          map[string]string{"openrouter": fetchedAt},
	}}
	f := newFixture(t, catalog)
	if err := f.news.Observe(context.Background()); err != nil {
		t.Fatalf("observe: %v", err)
	}
	if total := f.newsTotal(t); total != 0 {
		t.Fatalf("total = %d, want no news", total)
	}
}

func (f *fixture) newsTotal(t *testing.T) int {
	t.Helper()
	page := decode[struct {
		Total int `json:"total"`
	}](t, f.get(t, "/api/v1/discovery/news", true))
	return page.Total
}

// TestObserveWithoutASourceFetchIsANoOp keeps a never-fetched source from
// creating detections.
func TestObserveWithoutASourceFetchIsANoOp(t *testing.T) {
	catalog := &fakeCatalog{snapshot: Snapshot{
		ArtificialAnalysis: []AAModel{{Slug: "solo", Name: "Solo", Benchmarks: map[string]float64{"gpqa": 0.5}}},
	}}
	f := newFixture(t, catalog)
	if err := f.news.Observe(context.Background()); err != nil {
		t.Fatalf("observe: %v", err)
	}
	if total := f.newsTotal(t); total != 0 {
		t.Fatalf("total = %d", total)
	}
}

// TestFreeListingsAndPrices covers the annotations the page adds.
func TestFreeListingsAndPrices(t *testing.T) {
	catalog := &fakeCatalog{snapshot: Snapshot{
		OpenRouter: []OpenRouterModel{
			{ID: "a/first", IsFree: true, PriceIn: ptr(0.0), PriceOut: ptr(0.0), ContextWindow: ptr(1000)},
			{ID: "b/second", IsFree: false, PriceIn: ptr(1.0), PriceOut: ptr(2.0)},
		},
	}}
	f := newFixture(t, catalog)
	listings := f.benchmark.FreeListings()
	if !listings.Has("chat:first") {
		t.Fatalf("free listing missing: %+v", listings.All())
	}
	prices := f.benchmark.Prices()
	// (3 * 1 + 2) / 4
	if price, ok := prices["chat:second"]; !ok || price != 1.25 {
		t.Fatalf("prices = %+v", prices)
	}
	if _, ok := prices["chat:first"]; ok {
		t.Fatal("a zero price is not a price point")
	}
}

// TestQueryCoercion covers the coercion of the page window.
func TestQueryCoercion(t *testing.T) {
	cases := []struct {
		target string
		ok     bool
	}{
		{"?offset=&limit=24", true},        // Number('') is 0 for the offset
		{"?offset=0&limit=1", true},        // the smallest page
		{"?offset=1000000&limit=50", true}, // the bounds themselves
		{"?limit=", false},                 // a coerced empty limit is 0, out of range
		{"?asOf=", true},                   // a coerced empty asOf is 0, in range
		{"?offset=1000001", false},
		{"?limit=0", false},
		{"?limit=1.5", false},
		{"?offset=abc", false},
		{"?free=0&free=1", false},
	}
	for _, c := range cases {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/discovery/benchmarks"+c.target, nil)
		_, err := parseQuery(req)
		if c.ok && err != nil {
			t.Fatalf("%s rejected: %v", c.target, err)
		}
		if !c.ok && err == nil {
			t.Fatalf("%s accepted", c.target)
		}
	}
}

// TestEncodeURIComponentPath pins the encoding of the OpenRouter link.
func TestEncodeURIComponentPath(t *testing.T) {
	cases := map[string]string{
		"tencent/hy4-flash:free": "tencent/hy4-flash%3Afree",
		"vendor/model":           "vendor/model",
		"a b+c":                  "a%20b%2Bc",
		"x~y_z!w*v'q(r)":         "x~y_z!w*v'q(r)",
	}
	for in, want := range cases {
		if got := encodeURIComponentPath(in); got != want {
			t.Fatalf("encode(%q) = %q, want %q", in, got, want)
		}
	}
}

// insertUser adds a user row; the discovery pages only need the foreign key.
func insertUser(t *testing.T, db *dbx.DB, id, username string) {
	t.Helper()
	if _, err := db.W.Exec(`INSERT INTO users (id, username, email, password_hash, role)
		VALUES (?, ?, ?, 'x', 'user')`, id, username, username+"@example.test"); err != nil {
		t.Fatalf("insert user: %v", err)
	}
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var value T
	if err := json.Unmarshal(rec.Body.Bytes(), &value); err != nil {
		t.Fatalf("body %s: %v", rec.Body.String(), err)
	}
	return value
}

func ptr[T any](v T) *T { return &v }
