package catalog

import (
	"encoding/json"
	"log/slog"
	"math"
	"testing"
)

// newTestMetadata is a metadata service with no cache file and no settings.
func newTestMetadata(t *testing.T) *Metadata {
	t.Helper()
	metadata := &Metadata{log: slog.New(slog.DiscardHandler)}
	metadata.SkillsFromBenchmarks = testSkillsFromBenchmarks
	return metadata
}

// seedModelsDev puts a canonical family of models.dev facts in place.
func seedModelsDev(providers map[string]map[string]*Fact, analysis []*Fact) *metadataState {
	state := newMetadataState()
	state.ModelsDev = providers
	state.ArtificialAnalysis = analysis
	return state
}

func almostEqual(left, right, epsilon float64) bool { return math.Abs(left-right) < epsilon }

func TestIntelligenceUsesBenchmarkDataWhenPresentAndHeuristicsOtherwise(t *testing.T) {
	metadata := newTestMetadata(t)
	metadata.resetForTests(seedModelsDev(map[string]map[string]*Fact{
		"a": {"big-model-405b": devFact("Big", 3, 15, 128000, true, "2025-06-01")},
		"b": {"tiny-model-1b": devFact("Tiny", 0.02, 0.04, 8000, false, "2023-03-01")},
	}, []*Fact{aaFact("bench-model", "Bench Model", 60, nil)}))

	big := metadata.ComputeCanonical(canonicalID("big-model-405b"), []string{"big-model-405b"}, "")
	tiny := metadata.ComputeCanonical(canonicalID("tiny-model-1b"), []string{"tiny-model-1b"}, "")
	bench := metadata.ComputeCanonical(canonicalID("bench-model"), []string{"bench-model"}, "")
	if !(big.Intelligence > tiny.Intelligence+0.3) {
		t.Errorf("big %v is not clearly above tiny %v", big.Intelligence, tiny.Intelligence)
	}
	if big.IntelligenceSource != "heuristic" {
		t.Errorf("big source = %q, want heuristic", big.IntelligenceSource)
	}
	if bench.IntelligenceSource != "artificial_analysis" {
		t.Errorf("bench source = %q, want artificial_analysis", bench.IntelligenceSource)
	}
	if !almostEqual(bench.Intelligence, 1, 1e-9) {
		t.Errorf("the best benchmarked model defines the top of the scale, got %v", bench.Intelligence)
	}
	unknown := metadata.ComputeCanonical("nothingknown", []string{"nothing-known"}, "")
	if unknown.IntelligenceSource != "default" {
		t.Errorf("unknown source = %q, want default", unknown.IntelligenceSource)
	}
}

func TestANewVariantIsScoredFromItsBenchmarkedBaseModelNotItsPrice(t *testing.T) {
	metadata := newTestMetadata(t)
	metadata.resetForTests(seedModelsDev(map[string]map[string]*Fact{
		"a": {"hy4-flash": devFact("Hy4 Flash", 0.04, 0.18, 1048576, true, "2026-11-16")},
	}, []*Fact{aaFact("top", "Top", 60, nil), aaFact("hy4", "Hy4", 57, nil)}))

	base := metadata.ComputeCanonical(canonicalID("tencent/hy4"), []string{"tencent/hy4"}, "")
	flash := metadata.ComputeCanonical(canonicalID("tencent/hy4-flash:free"), []string{"tencent/hy4-flash:free"}, "")
	if flash.IntelligenceSource != "family" {
		t.Errorf("flash source = %q, want family", flash.IntelligenceSource)
	}
	if !almostEqual(flash.Intelligence, base.Intelligence*0.8, 1e-3) {
		t.Errorf("flash %v, want base*0.8 = %v", flash.Intelligence, base.Intelligence*0.8)
	}
	// "mini" inside a word is not a variant.
	gemini := metadata.ComputeCanonical(canonicalID("gemini"), []string{"gemini"}, "")
	if gemini.IntelligenceSource == "family" {
		t.Errorf("gemini source = %q, want anything but family", gemini.IntelligenceSource)
	}
}

func TestTwoWeakSignalsCannotProduceATopIntelligenceScore(t *testing.T) {
	metadata := newTestMetadata(t)
	fact := devFact("Mystery", 0, 0, 0, true, "2026-11-25")
	fact.ContextWindow = nil
	metadata.resetForTests(seedModelsDev(map[string]map[string]*Fact{
		"x": {"mystery-v1": fact},
	}, nil))
	record := metadata.ComputeCanonical(canonicalID("mystery-v1"), []string{"mystery-v1"}, "")
	if record.Intelligence >= 0.6 {
		t.Errorf("intelligence = %v, want below 0.6", record.Intelligence)
	}
}

func TestGuessesAreCalibratedOntoTheBenchmarkScale(t *testing.T) {
	metadata := newTestMetadata(t)
	modelsDev := map[string]*Fact{}
	analysis := []*Fact{}
	for i := 0; i < 16; i++ {
		id := "family-" + itoa(i) + "-" + itoa(10*(i+1)) + "b"
		modelsDev[id] = devFact(id, 0.1*float64(i+1), 0.4*float64(i+1), 0, true, "2026-01-01")
		analysis = append(analysis, aaFact(id, id, 15+2.5*float64(i), nil))
	}
	modelsDev["unbenchmarked-400b"] = devFact("Unbenchmarked", 1.6, 6.4, 0, true, "2026-01-01")
	metadata.resetForTests(seedModelsDev(map[string]map[string]*Fact{"a": modelsDev}, analysis))

	guess := metadata.ComputeCanonical(canonicalID("unbenchmarked-400b"), []string{"unbenchmarked-400b"}, "")
	strongest := metadata.ComputeCanonical(canonicalID("family-15-160b"), []string{"family-15-160b"}, "")
	if guess.IntelligenceSource != "calibrated" {
		t.Errorf("source = %q, want calibrated", guess.IntelligenceSource)
	}
	if strongest.Intelligence != 1 {
		t.Errorf("strongest = %v, want 1", strongest.Intelligence)
	}
	// Priced and sized like the strongest benchmarked models, so it lands near
	// them, not above 1.
	if !(guess.Intelligence > 0.8 && guess.Intelligence <= 0.9) {
		t.Errorf("calibrated guess = %v, want (0.8, 0.9]", guess.Intelligence)
	}
}

func TestBenchmarkNamesMatchProviderIDsWithoutBorrowingASiblingScore(t *testing.T) {
	metadata := newTestMetadata(t)
	metadata.resetForTests(seedModelsDev(nil, []*Fact{
		aaFact("nemotron-3-5-super-vl", "Nemotron 3.5 Super VL", 14, nil),
		aaFact("reflection-beam", "Beam 1.0", 18, nil),
		aaFact("deepseek-v4-1-flash-1017", "DeepSeek V4.1 Flash 1017 (Max)", 32, nil),
		aaFact("glm-5-3", "GLM-5.3 (Max)", 31, nil),
		aaFact("glm-5-3-low", "GLM-5.3 (Low)", 23, nil),
		aaFact("qwen3-8-max", "Qwen3.8 Max", 34, nil),
		aaFact("qwen3-8-27b", "Qwen3.8 27B (Xhigh)", 20, nil),
		aaFact("llama-4-70b", "Llama 4 70B", 25, nil),
		aaFact("llama-4-8b", "Llama 4 8B", 10, nil),
	}))
	index := func(id string) *int {
		record := metadata.ComputeCanonical(canonicalID(id), []string{id}, "chat")
		if record.IntelligenceSource != "artificial_analysis" {
			return nil
		}
		value := int(math.Round(record.Intelligence * 34))
		return &value
	}
	cases := []struct {
		id   string
		want *int
	}{
		{"nvidia/nemotron-3.5-super-vl-120b-a12b", ipInt(14)},
		{"reflection/beam-1.0-501b-a23b:free", ipInt(18)},
		{"deepseek-ai/deepseek-v4.1-flash", ipInt(32)},
		{"z-ai/glm-5.3", ipInt(31)},
		{"z-ai/glm-5.3-flash", nil},
		{"qwen/qwen3.8-27b", ipInt(20)},
		{"meta/llama-4-8b-instruct", ipInt(10)},
		{"meta/llama-4", nil},
	}
	for _, testCase := range cases {
		got := index(testCase.id)
		switch {
		case got == nil && testCase.want == nil:
		case got == nil || testCase.want == nil:
			t.Errorf("index(%q) = %v, want %v", testCase.id, got, testCase.want)
		case *got != *testCase.want:
			t.Errorf("index(%q) = %d, want %d", testCase.id, *got, *testCase.want)
		}
	}
}

func ipInt(value int) *int { return &value }

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	digits := []byte{}
	for value > 0 {
		digits = append([]byte{byte('0' + value%10)}, digits...)
		value /= 10
	}
	return string(digits)
}

func TestOpenRouterScoresWorkWithoutAnArtificialAnalysisKey(t *testing.T) {
	metadata := newTestMetadata(t)
	state := newMetadataState()
	state.OpenRouterBenchmarks = &BenchmarkSnapshot{SnapshotDate: "2026-11-04", Models: []BenchmarkModel{
		{Slug: "lab/strong-1", Name: "Strong", Benchmarks: map[string]float64{"gpqa": 0.95, "tau": 0.8}},
		{Slug: "lab/weak-1", Name: "Weak", Benchmarks: map[string]float64{"gpqa": 0.475, "tau": 0.4}},
	}}
	metadata.resetForTests(state)

	strong := metadata.ComputeCanonical("strong1", []string{"lab/strong-1"}, "chat")
	weak := metadata.ComputeCanonical("weak1", []string{"lab/weak-1"}, "chat")
	if strong.IntelligenceSource != "openrouter_benchmarks" {
		t.Errorf("strong source = %q", strong.IntelligenceSource)
	}
	if strong.Intelligence != 1 {
		t.Errorf("strong = %v, want 1", strong.Intelligence)
	}
	if weak.Intelligence != 0.5 {
		t.Errorf("weak = %v, want 0.5", weak.Intelligence)
	}
	if weak.SkillsJSON == nil {
		t.Fatal("weak has no skills")
	}
	skills := map[string]float64{}
	if err := json.Unmarshal([]byte(*weak.SkillsJSON), &skills); err != nil {
		t.Fatalf("skills json: %v", err)
	}
	if skills["reasoning"] != 0.5 || skills["agentic"] != 0.5 {
		t.Errorf("weak skills = %v, want reasoning 0.5 and agentic 0.5", skills)
	}
}

func TestABenchmarkThatRanksModelsUnlikeTheFullerIndexGetsNoSay(t *testing.T) {
	metadata := newTestMetadata(t)
	state := newMetadataState()
	analysis := []*Fact{}
	models := []BenchmarkModel{}
	for i := 0; i < 12; i++ {
		slug := "lab/model-" + itoa(i)
		// The benchmark slug carries the model without its vendor prefix.
		analysis = append(analysis, aaFact(slug[4:], slug, 10+float64(i)*5, nil))
		models = append(models, BenchmarkModel{Slug: slug, Name: slug, Benchmarks: map[string]float64{
			"gpqa": 0.3 + float64(i)*0.05,
			"tau":  0.5 + float64((i*7)%12)*0.01,
		}})
	}
	state.ArtificialAnalysis = analysis
	state.OpenRouterBenchmarks = &BenchmarkSnapshot{SnapshotDate: "2026-11-04", Models: models}
	metadata.resetForTests(state)

	agreement := metadata.OpenRouterAgreement()
	if !(agreement["gpqa"] > 0.9) {
		t.Errorf("gpqa agreement = %v, want above 0.9", agreement["gpqa"])
	}
	record := metadata.ComputeCanonical("model11", []string{"lab/model-11"}, "chat")
	if record.SkillsJSON == nil {
		t.Fatal("model11 has no skills")
	}
	skills := map[string]float64{}
	if err := json.Unmarshal([]byte(*record.SkillsJSON), &skills); err != nil {
		t.Fatalf("skills json: %v", err)
	}
	if !(skills["reasoning"] > 0.9) {
		t.Errorf("reasoning = %v, want above 0.9", skills["reasoning"])
	}
	if tauAgreement, known := agreement["tau"]; known && tauAgreement < 0.3 {
		if _, present := skills["agentic"]; present {
			t.Errorf("agentic = %v, want absent for a benchmark that disagrees", skills["agentic"])
		}
	}
}

func TestAVariantBaseIsWhatItsNameSays(t *testing.T) {
	match := variantBase("vendor/hy4-flash:free")
	if match == nil || match.Base != "vendor/hy4" || match.Share != 0.8 {
		t.Fatalf("variantBase = %+v, want vendor/hy4 share 0.8", match)
	}
	if variantBase("vendor/hy4") != nil {
		t.Error("a plain model is not a variant")
	}
}
