package catalog

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestArtificialAnalysisRecordsReadTheCurrentResponseShape(t *testing.T) {
	item := aaItem{
		ID: "id-1", Slug: "claude-sonnet-5-5-medium", Name: "Claude Sonnet 5.5", ReleaseDate: "2026-09-28",
		Evaluations: map[string]any{
			"artificial_analysis_intelligence_index": 30.8,
			"artificial_analysis_coding_index":       nil,
		},
	}
	item.Performance.MedianOutputTokensPerSecond = 88.82
	item.Performance.MedianTimeToFirstTokenSeconds = 1.47
	record := toArtificialAnalysisFact(item)
	if record.ID != "id-1" || record.Slug != "claude-sonnet-5-5-medium" || record.Name != "Claude Sonnet 5.5" {
		t.Fatalf("identity = %+v", record)
	}
	if record.IntelligenceIndex == nil || *record.IntelligenceIndex != 30.8 {
		t.Errorf("intelligenceIndex = %v, want 30.8", record.IntelligenceIndex)
	}
	if len(record.Benchmarks) != 0 {
		t.Errorf("benchmarks = %v, want none", record.Benchmarks)
	}
	if record.OutputTps == nil || *record.OutputTps != 88.82 {
		t.Errorf("outputTps = %v, want 88.82", record.OutputTps)
	}
	if record.TtftSeconds == nil || *record.TtftSeconds != 1.47 {
		t.Errorf("ttftSeconds = %v, want 1.47", record.TtftSeconds)
	}
	if record.ReleaseDate != "2026-09-28" {
		t.Errorf("releaseDate = %q", record.ReleaseDate)
	}
}

func TestArtificialAnalysisProBenchmarkNamesMapOntoTheSkillNames(t *testing.T) {
	record := toArtificialAnalysisFact(aaItem{Slug: "m", Name: "M", Evaluations: map[string]any{
		"gpqa_diamond": 0.917, "aa_lcr": 0.797, "terminalbench_v4_0": 0.419, "hle": 0.423,
		"scicode": nil, "critpt": 0.191,
	}})
	want := map[string]float64{"gpqa": 0.917, "lcr": 0.797, "terminalbench_hard": 0.419, "hle": 0.423}
	if len(record.Benchmarks) != len(want) {
		t.Fatalf("benchmarks = %v, want %v", record.Benchmarks, want)
	}
	for name, value := range want {
		if record.Benchmarks[name] != value {
			t.Errorf("benchmark %s = %v, want %v", name, record.Benchmarks[name], value)
		}
	}
}

func TestArtificialAnalysisMergeKeepsScoresAndDropsOldIndexValues(t *testing.T) {
	merged := mergeKnown(
		[]*Fact{{ID: "x", Slug: "kept", Name: "Kept", IntelligenceIndex: fp(30), Benchmarks: map[string]float64{}}},
		[]*Fact{
			{Slug: "kept", Name: "Kept", IntelligenceIndex: fp(70), Benchmarks: map[string]float64{"gpqa": 0.9}},
			{Slug: "dropped", Name: "Dropped", IntelligenceIndex: fp(60), IndexDerived: true, Benchmarks: map[string]float64{"hle": 0.2}},
			{Slug: "nothing", Name: "Nothing", IntelligenceIndex: fp(50), Benchmarks: map[string]float64{}},
		},
	)
	if len(merged) != 2 || merged[0].Slug != "kept" || merged[1].Slug != "dropped" {
		slugs := []string{}
		for _, record := range merged {
			slugs = append(slugs, record.Slug)
		}
		t.Fatalf("slugs = %v, want [kept dropped]", slugs)
	}
	if merged[0].IntelligenceIndex == nil || *merged[0].IntelligenceIndex != 30 {
		t.Errorf("kept index = %v, want 30", merged[0].IntelligenceIndex)
	}
	if merged[0].Benchmarks["gpqa"] != 0.9 {
		t.Errorf("kept benchmarks = %v, want gpqa 0.9", merged[0].Benchmarks)
	}
	if merged[1].IntelligenceIndex != nil {
		t.Errorf("dropped index = %v, want nil (on the old scale)", *merged[1].IntelligenceIndex)
	}
	if merged[1].Benchmarks["hle"] != 0.2 {
		t.Errorf("dropped benchmarks = %v, want hle 0.2", merged[1].Benchmarks)
	}
}

func TestArtificialAnalysisAFreeKeyReadsEveryPageOfTheFreeList(t *testing.T) {
	asked := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.RequestURI(), "/")
		asked = append(asked, path)
		if r.Header.Get("x-api-key") != "test-key" {
			t.Errorf("x-api-key = %q, want test-key", r.Header.Get("x-api-key"))
		}
		if strings.HasSuffix(r.URL.Path, "/language/models") {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		page := r.URL.Query().Get("page")
		if page == "2" {
			w.Write([]byte(`{"data":[{"id":"b","slug":"two"}],"pagination":{"page":2,"total_pages":2,"has_more":false}}`))
			return
		}
		w.Write([]byte(`{"data":[{"id":"a","slug":"one"}],"pagination":{"page":1,"total_pages":2,"has_more":true}}`))
	}))
	defer server.Close()

	source := &artificialAnalysisSource{baseURL: server.URL, client: server.Client()}
	items, err := source.fetchList(context.Background(), "language/models", "test-key", time.Now())
	if err != nil {
		t.Fatalf("fetchList: %v", err)
	}
	if len(items) != 2 || items[0].Slug != "one" || items[1].Slug != "two" {
		t.Fatalf("items = %+v", items)
	}
	want := []string{"language/models", "language/models/free", "language/models/free?page=2"}
	if len(asked) != len(want) {
		t.Fatalf("asked = %v, want %v", asked, want)
	}
	for i, path := range want {
		if asked[i] != path {
			t.Errorf("asked[%d] = %q, want %q", i, asked[i], path)
		}
	}
	// The tier is remembered, saving a request on the next list.
	asked = asked[:0]
	if _, err := source.fetchList(context.Background(), "media/text-to-image/models", "test-key", time.Now()); err != nil {
		t.Fatalf("fetchList media: %v", err)
	}
	if len(asked) == 0 || asked[0] != "media/text-to-image/models/free" {
		t.Errorf("asked = %v, want the free list first", asked)
	}
}

func TestArtificialAnalysisAFailedPageKeepsTheListWeAlreadyHave(t *testing.T) {
	call := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call++
		if call == 1 {
			w.Write([]byte(`{"data":[{"slug":"one"}],"pagination":{"has_more":true}}`))
			return
		}
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	source := &artificialAnalysisSource{baseURL: server.URL, client: server.Client()}
	_, err := source.fetch(context.Background(), "k", nil)
	if err == nil {
		t.Fatal("a failed page must fail the whole list")
	}
	if !strings.Contains(err.Error(), "429") || !strings.Contains(err.Error(), "page 2") {
		t.Errorf("error = %q, want page 2 and HTTP 429", err.Error())
	}
}

func TestArtificialAnalysisMediaListsUseTheNewPaths(t *testing.T) {
	asked := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.RequestURI(), "/")
		asked = append(asked, path)
		if !strings.HasSuffix(r.URL.Path, "/free") {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Write([]byte(`{"data":[{"slug":"m","name":"M","elo":1200,"model_creator":{"name":"Lab"}},{"slug":"no-elo","name":"N","elo":null}]}`))
	}))
	defer server.Close()

	source := &artificialAnalysisSource{baseURL: server.URL, client: server.Client()}
	result, err := source.fetchMedia(context.Background(), "k")
	if err != nil {
		t.Fatalf("fetchMedia: %v", err)
	}
	want := []string{"media/text-to-image/models", "media/text-to-image/models/free",
		"media/text-to-speech/models/free", "media/text-to-video/models/free"}
	if len(asked) != len(want) {
		t.Fatalf("asked = %v, want %v", asked, want)
	}
	for i, path := range want {
		if asked[i] != path {
			t.Errorf("asked[%d] = %q, want %q", i, asked[i], path)
		}
	}
	image := result["image"]
	if len(image) != 1 {
		t.Fatalf("image = %+v, want the one record with an ELO", image)
	}
	if image[0].Slug != "m" || image[0].Name != "M" || image[0].Elo != 1200 {
		t.Errorf("image record = %+v", image[0])
	}
	if image[0].Creator == nil || *image[0].Creator != "Lab" {
		t.Errorf("creator = %v, want Lab", image[0].Creator)
	}
	if image[0].ReleaseDate != nil {
		t.Errorf("releaseDate = %v, want null", *image[0].ReleaseDate)
	}
	for _, path := range asked {
		if strings.Contains(path, "data/") {
			t.Errorf("legacy path requested: %s", path)
		}
	}
}

func TestOpenRouterBenchmarksUseTheNewestVersionAndTheMiddleHost(t *testing.T) {
	row := func(permaslug, provider string, accuracy float64, questions float64, at string) benchmarkRow {
		return benchmarkRow{Benchmark: "gpqa_diamond", ModelSlug: "lab/model-a", ModelName: "Model A",
			ModelPermaslug: permaslug, ProviderName: provider, Accuracy: accuracy, TotalQuestions: questions,
			LastRunTimestamp: at}
	}
	models := toBenchmarkModels([]benchmarkRow{
		row("lab/model-a-old", "Old", 0.2, 200, "2026-09-01"),
		row("lab/model-a-new", "Good", 0.9, 200, "2026-11-01"),
		row("lab/model-a-new", "Fine", 0.88, 200, "2026-11-01"),
		row("lab/model-a-new", "Broken", 0.01, 200, "2026-11-01"),
		row("lab/model-a-new", "Too few", 1, 5, "2026-11-01"),
		{Benchmark: "unknown", ModelSlug: "lab/model-a", ModelPermaslug: "lab/model-a-new", Accuracy: 0},
	})
	if len(models) != 1 {
		t.Fatalf("models = %+v, want one", models)
	}
	if models[0].Benchmarks["gpqa"] != 0.88 {
		t.Errorf("gpqa = %v, want the median of the newest version's hosts (0.88)", models[0].Benchmarks["gpqa"])
	}
	if _, present := models[0].Benchmarks["tau"]; present {
		t.Errorf("benchmarks = %v, want no tau", models[0].Benchmarks)
	}
}

func TestTheOpenRouterSnapshotOfTheDayFallsBackToTheOneBefore(t *testing.T) {
	asked := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := r.URL.Path[strings.LastIndexByte(r.URL.Path, '/')+1:]
		asked = append(asked, name)
		if name == "2026-11-04.json" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Write([]byte(`{"snapshot_date":"2026-11-03","rows":[{"benchmark":"gpqa_diamond","model_slug":"a/b","model_permaslug":"a/b-1","accuracy":0.5,"total_questions":100,"last_run_timestamp":"2026-11-01"}]}`))
	}))
	defer server.Close()

	source := benchmarkSource{baseURL: server.URL, client: server.Client()}
	now := time.Date(2026, 11, 4, 1, 0, 0, 0, time.UTC)
	snapshot, err := source.fetch(context.Background(), nil, now)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if snapshot.SnapshotDate != "2026-11-03" {
		t.Errorf("snapshotDate = %q, want 2026-11-03", snapshot.SnapshotDate)
	}
	if len(asked) != 2 || asked[0] != "2026-11-04.json" || asked[1] != "2026-11-03.json" {
		t.Errorf("asked = %v, want [2026-11-04.json 2026-11-03.json]", asked)
	}
	// A dated file never changes.
	previous := &BenchmarkSnapshot{SnapshotDate: "2026-11-04", Models: []BenchmarkModel{}}
	again, err := source.fetch(context.Background(), previous, now)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if again != previous {
		t.Error("a snapshot for today must be reused, not re-fetched")
	}
	if len(asked) != 2 {
		t.Errorf("asked = %v, want no further request", asked)
	}
}

func TestMTEBScoresUseOnlyTheFixedEnglishTasks(t *testing.T) {
	rows := []mtebRow{{TaskName: "ArguAna", Score: fp(0.5)}, {TaskName: "SciFact", Score: fp(0.6), Subset: "en"},
		{TaskName: "NFCorpus", Score: fp(0.7)}, {TaskName: "FiQA2018", Score: fp(0.8)}, {TaskName: "SCIDOCS", Score: fp(0.9)},
		{TaskName: "TRECCOVID", Score: fp(0.4)}, {TaskName: "STSBenchmark", Score: fp(0.5), Subset: "pl"}}
	score := mtebScoreOf(rows)
	if score == nil {
		t.Fatal("six English tasks are enough for a score")
	}
	if score.Tasks != 6 {
		t.Errorf("tasks = %d, want 6 (the Polish subset is not counted)", score.Tasks)
	}
	if !almostEqual(*score.Score, (0.5+0.6+0.7+0.8+0.9+0.4)/6, 1e-9) {
		t.Errorf("score = %v", *score.Score)
	}
	if mtebScoreOf(rows[:5]) != nil {
		t.Error("five tasks are not enough for a score")
	}
	if got := mtebIntelligence(0.75); got != 1 {
		t.Errorf("mtebIntelligence(0.75) = %v, want 1", got)
	}
}

func TestMTEBModelNamesFollowTheProviderPrefixes(t *testing.T) {
	names := mtebNames("text-embedding-3-small")
	if !containsString(names, "openai/text-embedding-3-small") {
		t.Errorf("names = %v, want the OpenAI name", names)
	}
	if names := mtebNames("@cf/baai/bge-large-en-v1.5"); !containsString(names, "baai/bge-large-en-v1.5") {
		t.Errorf("names = %v, want the host-prefixed name", names)
	}
}
