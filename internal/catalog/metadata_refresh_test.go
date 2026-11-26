package catalog

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// metadataServer serves the four metadata sources of the refresh.
type metadataServer struct {
	*httptest.Server
	artificialAnalysisCalls int
}

func newMetadataServer(t *testing.T) *metadataServer {
	t.Helper()
	server := &metadataServer{}
	server.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/modelsdev"):
			w.Write([]byte(`{"g":{"models":{"llama-3.1-8b-instant":{"name":"Llama 3.1 8B",
				"cost":{"input":0.05,"output":0.08},"limit":{"context":131072,"output":8192},
				"reasoning":true,"tool_call":true,"release_date":"2024-09-25",
				"modalities":{"input":["text"],"output":["text"]}}}}}`))
		case strings.HasPrefix(r.URL.Path, "/openrouter"):
			w.Write([]byte(`{"data":[{"id":"llama-3.1-8b-instant","pricing":{"prompt":"0.00000005","completion":"0.00000008"},
				"context_length":131072,"supported_parameters":["tools","reasoning"],"architecture":{"input_modalities":["text"]}}]}`))
		case strings.HasPrefix(r.URL.Path, "/benchmarks"):
			w.Write([]byte(`{"snapshot_date":"2026-11-04","rows":[{"benchmark":"gpqa_diamond","model_slug":"lab/model-a",
				"model_name":"Model A","model_permaslug":"lab/model-a-1","accuracy":0.6,"total_questions":100,"last_run_timestamp":"2026-11-01"}]}`))
		case strings.HasPrefix(r.URL.Path, "/aa/media/"):
			w.Write([]byte(`{"data":[{"slug":"flux_flux-2-klein-4b","name":"FLUX.2 klein 4B","elo":1150,"model_creator":{"name":"BFL"}}]}`))
		case strings.HasPrefix(r.URL.Path, "/aa/language/models"):
			server.artificialAnalysisCalls++
			w.Write([]byte(`{"data":[{"id":"aa-1","slug":"llama-3-1-8b-instant","name":"Llama 3.1 8B",
				"evaluations":{"artificial_analysis_intelligence_index":24,"gpqa_diamond":0.41},
				"performance":{"median_output_tokens_per_second":120.5}}]}`))
		case strings.HasPrefix(r.URL.Path, "/mteb"):
			w.Write([]byte(`{"rows":[]}`))
		default:
			t.Errorf("unexpected metadata request: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func TestRefreshSourcesFillsTheIndexesAndTheCatalog(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	server := newMetadataServer(t)
	metadata := env.svc.Metadata
	metadata.modelsDev = modelsDevSource{baseURL: server.URL + "/modelsdev", client: server.Client()}
	metadata.openRouter = openRouterPublicSource{baseURL: server.URL + "/openrouter", client: server.Client()}
	metadata.benchmarks = benchmarkSource{baseURL: server.URL + "/benchmarks", client: server.Client()}
	metadata.analysis = artificialAnalysisSource{baseURL: server.URL + "/aa", client: server.Client()}
	metadata.mteb = mtebSource{baseURL: server.URL + "/mteb", client: server.Client()}
	if err := env.settings.SetGlobal(aaSetting, "sealed:test-key"); err != nil {
		t.Fatalf("set key: %v", err)
	}

	env.seedAccount(t, "acc-1", "g", "g-main", "paid", nil)
	env.models.models["acc-1"] = []ListedModel{{ID: "llama-3.1-8b-instant", Kind: "chat"}}
	env.syncAccount(t, "acc-1")

	results, err := metadata.RefreshSources(ctx)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	for _, source := range []string{"modelsDev", "openrouter", "openrouterBenchmarks", "artificialAnalysis", "artificialAnalysisMedia", "mteb"} {
		if results[source] != "ok" {
			t.Errorf("%s = %q, want ok", source, results[source])
		}
	}

	facts := metadata.ProviderFacts("g", "llama-3.1-8b-instant")
	if facts.PriceIn == nil || *facts.PriceIn != 0.05 {
		t.Errorf("facts priceIn = %v, want 0.05", facts.PriceIn)
	}
	if facts.Name != "Llama 3.1 8B" {
		t.Errorf("facts name = %q", facts.Name)
	}

	status, err := metadata.Status()
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !status.ArtificialAnalysisConfigured || !status.SourcesEnabled {
		t.Errorf("status = %+v, want a configured key and enabled sources", status)
	}
	if status.FetchedAt["modelsDev"] == "" {
		t.Errorf("status.fetchedAt = %v, want modelsDev recorded", status.FetchedAt)
	}

	// The canonical record of the synced model now carries the benchmark index.
	entries, err := env.svc.ListForUser(ctx, "user-1", "chat", false)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	entry := entries[0]
	if entry.Intelligence == nil || *entry.Intelligence <= 0 {
		t.Errorf("intelligence = %v, want a computed score", entry.Intelligence)
	}
	if entry.SpeedPriorTps == nil || *entry.SpeedPriorTps != 120.5 {
		t.Errorf("speedPriorTps = %v, want 120.5", entry.SpeedPriorTps)
	}
	if entry.IntelligenceSource == nil || *entry.IntelligenceSource != "artificial_analysis" {
		t.Errorf("intelligenceSource = %v, want artificial_analysis", entry.IntelligenceSource)
	}
	var sources string
	if err := env.db.R.QueryRow(`SELECT sources_json FROM model_metadata WHERE canonical_id = 'llama318b'`).Scan(&sources); err != nil {
		t.Fatalf("read metadata: %v", err)
	}
	if !strings.Contains(sources, "models.dev") || !strings.Contains(sources, "artificial_analysis") {
		t.Errorf("sources = %s, want models.dev and artificial_analysis", sources)
	}

	// The cache file is written for the next start.
	if _, err := os.Stat(filepath.Join(metadata.dataDir, "metadata_cache.json")); err != nil {
		t.Errorf("cache file: %v", err)
	}

	// A second refresh inside the Artificial Analysis interval keeps its quota.
	calls := server.artificialAnalysisCalls
	if _, err := metadata.RefreshSources(ctx); err != nil {
		t.Fatalf("second refresh: %v", err)
	}
	if server.artificialAnalysisCalls != calls {
		t.Errorf("Artificial Analysis was refreshed again (%d → %d calls)", calls, server.artificialAnalysisCalls)
	}
}

func TestRefreshSourcesKeepsGoingWhenASourceFails(t *testing.T) {
	env := newTestEnv(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/modelsdev") {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Write([]byte(`{"data":[]}`))
	}))
	defer server.Close()
	metadata := env.svc.Metadata
	metadata.modelsDev = modelsDevSource{baseURL: server.URL + "/modelsdev", client: server.Client()}
	metadata.openRouter = openRouterPublicSource{baseURL: server.URL + "/openrouter", client: server.Client()}
	metadata.benchmarks = benchmarkSource{baseURL: server.URL + "/benchmarks", client: server.Client()}
	metadata.mteb = mtebSource{baseURL: server.URL + "/mteb", client: server.Client()}

	results, err := metadata.RefreshSources(context.Background())
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if results["modelsDev"] != "models.dev returned HTTP 500" {
		t.Errorf("modelsDev = %q, want the source error text", results["modelsDev"])
	}
	if results["openrouter"] != "ok" {
		t.Errorf("openrouter = %q, want ok despite the other source failing", results["openrouter"])
	}
	if _, present := results["artificialAnalysis"]; present {
		t.Error("without a key the Artificial Analysis sources must not be refreshed")
	}
}

func TestSetArtificialAnalysisKeySealsTheKey(t *testing.T) {
	env := newTestEnv(t)
	metadata := env.svc.Metadata
	if err := metadata.SetArtificialAnalysisKey("secret-key"); err != nil {
		t.Fatalf("set key: %v", err)
	}
	stored, ok, err := env.settings.Global(aaSetting)
	if err != nil || !ok {
		t.Fatalf("stored key: %v %v", ok, err)
	}
	var sealed string
	if err := json.Unmarshal(stored, &sealed); err != nil {
		t.Fatalf("stored value: %v", err)
	}
	if sealed != "sealed:secret-key" {
		t.Errorf("stored = %q, want the sealed key", sealed)
	}
	status, err := metadata.Status()
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !status.ArtificialAnalysisConfigured {
		t.Error("the key was not reported as configured")
	}
	if err := metadata.SetArtificialAnalysisKey(""); err != nil {
		t.Fatalf("clear key: %v", err)
	}
	status, err = metadata.Status()
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if status.ArtificialAnalysisConfigured {
		t.Error("the cleared key is still reported as configured")
	}
}
