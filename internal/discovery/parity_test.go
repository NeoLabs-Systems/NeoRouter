package discovery

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"testing"
	"time"
)

// oracleScript drives the Node server's own discovery services over the same
// fixture the Go test builds, so the two answers can be compared byte for byte.
// The JS server is the behavioral oracle for this conversion.
const oracleScript = `
'use strict';
const fs = require('node:fs');
const path = require('node:path');
const appRoot = process.argv[2];
const home = process.argv[3];
const fetchedAt = process.argv[4];
const now = Number(process.argv[5]);
process.env.NEOROUTER_HOME = home;
process.env.NODE_ENV = 'test';
process.env.NEOROUTER_WORKERS_ENABLED = 'false';
process.env.NEOROUTER_METADATA_SOURCES_ENABLED = 'false';
process.env.NEOROUTER_LOG_LEVEL = 'error';
const database = require(path.join(appRoot, 'server/db/database'));
require(path.join(appRoot, 'server/db/migrate')).migrate();
const metadata = require(path.join(appRoot, 'server/services/catalog/metadata_service'));
const benchmarks = require(path.join(appRoot, 'server/services/discovery/benchmark_service'));
const news = require(path.join(appRoot, 'server/services/discovery/news_service'));

metadata.resetForTests({
  artificialAnalysis: [
    { slug: 'hy4', name: 'Hy4', intelligenceIndex: 66, benchmarks: { gpqa: 0.9, mmlu: 0.8 }, releaseDate: '2026-01-01' },
    { slug: 'weak-model', name: 'Weak model', intelligenceIndex: 20, benchmarks: { gpqa: 0.2 } },
    { slug: 'derived', name: 'Derived', intelligenceIndex: 40, indexDerived: true, benchmarks: { gpqa: 0.5 } },
  ],
  openrouterBenchmarks: { models: [{ slug: 'or-measured', name: 'OR measured', benchmarks: { mmlu: 0.7, gpqa: 0.4 } }] },
  artificialAnalysisMedia: {
    image: [{ slug: 'meow', name: 'Meow', elo: 1200, releaseDate: '2026-02-02' }],
    video: [{ slug: 'vid', name: 'Vid', elo: 1000 }],
  },
  mteb: { 'embed-1': { model: 'Embed One', score: 64.2 } },
  openrouter: [
    { id: 'tencent/hy4-flash', isFree: false, priceIn: 0.035, priceOut: 0.14 },
    { id: 'vendor/weak-model:free', isFree: true, priceIn: 0, priceOut: 0, contextWindow: 32000 },
    { id: 'or/or-measured', isFree: true, priceIn: 0.5, priceOut: 1.5 },
  ],
  fetchedAt: { openrouter: fetchedAt, artificialAnalysis: fetchedAt },
});

const db = database.getDatabase();
db.prepare("INSERT INTO users (id, username, email, password_hash, role) VALUES ('u1', 'oracle', 'oracle@example.test', 'x', 'user')").run();
db.prepare("INSERT INTO provider_accounts (id, user_id, definition_id, slug, label, billing_mode) VALUES ('nous-test', 'u1', 'nous', 'nous', 'Nous', 'free_tier')").run();
db.prepare("INSERT INTO models (account_id, model_id, display_name, canonical_id, is_free, context_window) VALUES ('nous-test', 'tencent/hy4-flash:free', 'Hy4 Flash', 'hy4flash', 1, 128000)").run();

const options = { query: '', capability: 'chat', source: '', free: false, offset: 0, limit: 3, compare: [] };
const page = benchmarks.list(options);
const all = benchmarks.list({ query: '', capability: '', source: '', free: false, offset: 0, limit: 50, compare: ['chat:hy4', 'chat:ormeasured'] });
news.observe(now);
const newsPage = news.list({ query: '', offset: 0, limit: 10, asOf: now + 1000 });
const search = benchmarks.list({ query: 'measured', capability: 'chat', source: 'openrouterBenchmarks', free: true, offset: 0, limit: 2, compare: [] });
process.stdout.write(JSON.stringify({ page, all, news: newsPage, search }));
`

// TestParityWithTheNodeServer compares the Go benchmark and news pages with the
// Node server's own output for the same snapshot: same keys in the same order,
// same values. It skips where the JS oracle cannot run (no Node, no npm deps).
func TestParityWithTheNodeServer(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed: the JS oracle cannot run")
	}
	appRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(appRoot, "node_modules", "better-sqlite3")); err != nil {
		t.Skip("the JS oracle's npm dependencies are not installed")
	}

	const fetchedAt = "2026-01-03T12:00:00.000Z"
	const now = int64(1767441600000) // 2026-01-03T12:00:00.000Z
	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(dir, "oracle.js")
	if err := os.WriteFile(script, []byte(oracleScript), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	cmd := exec.Command(node, script, appRoot, home, fetchedAt, "1767441600000")
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("JS oracle failed: %v\n%s", err, stderr.String())
	}
	var oracle struct {
		Page   json.RawMessage `json:"page"`
		All    json.RawMessage `json:"all"`
		News   json.RawMessage `json:"news"`
		Search json.RawMessage `json:"search"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &oracle); err != nil {
		t.Fatalf("oracle output %q: %v", stdout.String(), err)
	}

	catalog := &fakeCatalog{
		snapshot: Snapshot{
			ArtificialAnalysis: []AAModel{
				{Slug: "hy4", Name: "Hy4", IntelligenceIndex: ptr(66.0), Benchmarks: map[string]float64{"gpqa": 0.9, "mmlu": 0.8}, ReleaseDate: "2026-01-01"},
				{Slug: "weak-model", Name: "Weak model", IntelligenceIndex: ptr(20.0), Benchmarks: map[string]float64{"gpqa": 0.2}},
				{Slug: "derived", Name: "Derived", IntelligenceIndex: ptr(40.0), IndexDerived: true, Benchmarks: map[string]float64{"gpqa": 0.5}},
			},
			OpenRouterBenchmarks: []BenchmarkModel{
				{Slug: "or-measured", Name: "OR measured", Benchmarks: map[string]float64{"mmlu": 0.7, "gpqa": 0.4}},
			},
			ArtificialAnalysisMedia: map[string][]ArenaModel{
				"image": {{Slug: "meow", Name: "Meow", Elo: 1200, ReleaseDate: "2026-02-02"}},
				"video": {{Slug: "vid", Name: "Vid", Elo: 1000}},
			},
			MTEB: map[string]MTEBModel{"embed-1": {Model: "Embed One", Score: 64.2}},
			OpenRouter: []OpenRouterModel{
				{ID: "tencent/hy4-flash", PriceIn: ptr(0.035), PriceOut: ptr(0.14)},
				{ID: "vendor/weak-model:free", IsFree: true, PriceIn: ptr(0.0), PriceOut: ptr(0.0), ContextWindow: ptr(32000)},
				{ID: "or/or-measured", IsFree: true, PriceIn: ptr(0.5), PriceOut: ptr(1.5)},
			},
			FetchedAt: map[string]string{"openrouter": fetchedAt, "artificialAnalysis": fetchedAt},
		},
		variants: map[string]variant{"tencent/hy4-flash:free": {base: "tencent/hy4", share: 0.8}},
	}
	f := newFixture(t, catalog)
	insertUser(t, f.db, "u1", "oracle")
	if _, err := f.db.W.Exec(`INSERT INTO provider_accounts (id, user_id, definition_id, slug, label, billing_mode)
		VALUES ('nous-test', 'u1', 'nous', 'nous', 'Nous', 'free_tier')`); err != nil {
		t.Fatalf("insert account: %v", err)
	}
	if _, err := f.db.W.Exec(`INSERT INTO models (account_id, model_id, display_name, canonical_id, is_free, context_window)
		VALUES ('nous-test', 'tencent/hy4-flash:free', 'Hy4 Flash', 'hy4flash', 1, 128000)`); err != nil {
		t.Fatalf("insert model: %v", err)
	}

	mustJSON := func(v any) string {
		t.Helper()
		body, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		return string(body)
	}
	page := f.benchmark.List(ListOptions{Capability: "chat", Limit: 3})
	all := f.benchmark.List(ListOptions{Capability: "", Limit: 50, Compare: []string{"chat:hy4", "chat:ormeasured"}})
	search := f.benchmark.List(ListOptions{Query: "measured", Capability: "chat", Source: "openrouterBenchmarks", Free: true, Limit: 2})
	f.news.Now = func() time.Time { return time.UnixMilli(now) }
	if err := f.news.Observe(t.Context()); err != nil {
		t.Fatalf("observe: %v", err)
	}
	const asOf = now + 1000
	newsPage, err := f.news.List(t.Context(), NewsOptions{Limit: 10, AsOf: ptr(asOf)})
	if err != nil {
		t.Fatalf("news: %v", err)
	}

	compareJSON(t, "benchmarks", string(oracle.Page), mustJSON(page))
	compareJSON(t, "all capabilities", string(oracle.All), mustJSON(all))
	compareJSON(t, "search", string(oracle.Search), mustJSON(search))
	compareJSON(t, "news", string(oracle.News), mustJSON(newsPage))
}

// scoresArray matches one model's scores array. The entries are flat objects, so
// no nested bracket can appear inside it.
var scoresArray = regexp.MustCompile(`"scores":\[[^\]]*\]`)

// compareJSON checks one page against the oracle twice: every value through a
// structural comparison with each scores array sorted by metric, and every byte
// outside the scores arrays. The order of a model's scores follows the order the
// source published its metrics in, which the Go port reports sorted instead (see
// the report: metric order inside a source is not a contract, and the JS order
// depends on the remote JSON).
func compareJSON(t *testing.T, name, want, got string) {
	t.Helper()
	if want == got {
		return
	}
	var wantValue, gotValue any
	if err := json.Unmarshal([]byte(want), &wantValue); err != nil {
		t.Fatalf("%s: oracle json: %v", name, err)
	}
	if err := json.Unmarshal([]byte(got), &gotValue); err != nil {
		t.Fatalf("%s: go json: %v", name, err)
	}
	sortScores(wantValue)
	sortScores(gotValue)
	if !reflect.DeepEqual(wantValue, gotValue) {
		t.Fatalf("%s: mismatch\n want %s\n  got %s", name, want, got)
	}
	if blankScores(want) != blankScores(got) {
		t.Fatalf("%s: same values, different bytes\n want %s\n  got %s", name, blankScores(want), blankScores(got))
	}
}

// sortScores puts every model's scores in metric-id order, on both sides.
func sortScores(value any) {
	switch node := value.(type) {
	case map[string]any:
		if scores, ok := node["scores"].([]any); ok {
			sort.SliceStable(scores, func(i, j int) bool {
				left, _ := scores[i].(map[string]any)["id"].(string)
				right, _ := scores[j].(map[string]any)["id"].(string)
				return left < right
			})
			node["scores"] = scores
		}
		for _, child := range node {
			sortScores(child)
		}
	case []any:
		for _, child := range node {
			sortScores(child)
		}
	}
}

// blankScores removes the scores arrays, leaving the bytes that must match.
func blankScores(page string) string {
	return scoresArray.ReplaceAllString(page, `"scores":[]`)
}
