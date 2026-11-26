package localmodels

// Tests for the catalog, the fit, the engines, the downloader and the runner —
// the parts that can be checked without a machine that runs models.

import (
	"regexp"
	"testing"
)

// ggufFilePattern matches the single GGUF model file a llama.cpp entry loads.
var ggufFilePattern = regexp.MustCompile(`\.gguf$`)

// testMachine is a 16 GB Linux server without a graphics card.
func testMachine(overrides ...func(*Machine)) Machine {
	machine := Machine{
		Platform: "linux",
		Arch:     "x64",
		CPU:      CPU{Name: "Test CPU", Cores: 8},
		Memory:   Memory{TotalMB: 16384},
		Disk:     Disk{FreeMB: int64ptr(500_000), TotalMB: int64ptr(1_000_000)},
	}
	for _, apply := range overrides {
		apply(&machine)
	}
	return machine
}

// systemOneFamily is the System One family rule the catalog has to satisfy: a
// moderation model's id must name its family. The full function belongs to the
// System One domain (internal/media).
func systemOneFamily(modelID string) string {
	patterns := []struct {
		pattern *regexp.Regexp
		family  string
	}{
		{regexp.MustCompile(`prompt-guard`), "prompt_guard"},
		{regexp.MustCompile(`safeguard`), "safeguard"},
		{regexp.MustCompile(`llama-guard`), "llama_guard"},
		{regexp.MustCompile(`nemoguard|content-safety|safety-guard`), "nemoguard"},
		{regexp.MustCompile(`topic-control`), "topic_control"},
		{regexp.MustCompile(`shieldgemma`), "shieldgemma"},
		{regexp.MustCompile(`moderation`), "native"},
	}
	for _, item := range patterns {
		if item.pattern.MatchString(modelID) {
			return item.family
		}
	}
	return "generic"
}

func TestCatalogIsValid(t *testing.T) {
	entries := Catalog()
	if len(entries) < 20 {
		t.Fatalf("catalog has %d entries, want at least 20", len(entries))
	}
	seen := map[string]bool{}
	for _, entry := range entries {
		if seen[entry.ID] {
			t.Fatalf("duplicate local model id %s", entry.ID)
		}
		seen[entry.ID] = true
		if err := validateEntry(entry); err != nil {
			t.Fatalf("invalid entry: %v", err)
		}
	}
	for _, kind := range []string{KindChat, KindModeration, KindEmbedding, KindTranscription, KindSpeech} {
		recommended := false
		for _, entry := range entries {
			if entry.Kind == kind && entry.Recommended {
				recommended = true
				break
			}
		}
		if !recommended {
			t.Fatalf("no recommended %s model", kind)
		}
	}
	for _, entry := range entries {
		if entry.Engine != EngineLlamaCpp {
			continue
		}
		if len(entry.Source.Include) != 1 {
			t.Fatalf("%s loads exactly one model file", entry.ID)
		}
		if !ggufFilePattern.MatchString(entry.Source.Include[0]) {
			t.Fatalf("%s: %s is not a GGUF file", entry.ID, entry.Source.Include[0])
		}
	}
	for _, entry := range entries {
		if entry.Kind == KindSpeech && len(entry.Voices) == 0 {
			t.Fatalf("%s has no voices", entry.ID)
		}
	}
}

func TestSystemOneModelsKeepTheFamilyNameSystemOneAsksThemBy(t *testing.T) {
	for _, entry := range Catalog() {
		if entry.Kind != KindModeration {
			continue
		}
		if family := systemOneFamily(entry.ID); family == "generic" {
			t.Fatalf("%s has no System One family", entry.ID)
		}
	}
}

func TestFilePatternsStayInsideOneFolder(t *testing.T) {
	cases := []struct {
		pattern string
		path    string
		want    bool
	}{
		{"*.json", "config.json", true},
		{"*.json", "onnx/config.json", false},
		{"onnx/*", "onnx/model.onnx_data", true},
		{"Qwen3.5-4B-Q4_K_M.gguf", "Qwen3.5-4B-Q4_K_M.gguf", true},
		{"Qwen3.5-4B-Q4_K_M.gguf", "Qwen3.5-4BxQ4_K_M.gguf", false},
	}
	for _, tc := range cases {
		if got := GlobToRegex(tc.pattern).MatchString(tc.path); got != tc.want {
			t.Fatalf("GlobToRegex(%q).MatchString(%q) = %v, want %v", tc.pattern, tc.path, got, tc.want)
		}
	}
}

func TestFitRatings(t *testing.T) {
	tiny, _ := CatalogEntry("qwen3.5-0.8b")
	big, _ := CatalogEntry("gpt-oss-120b")
	mid, _ := CatalogEntry("qwen3.5-9b")

	if got := Assess(tiny, testMachine(), AssessOptions{}).Rating; got != RatingGood {
		t.Fatalf("tiny rating = %q", got)
	}
	if got := Assess(big, testMachine(), AssessOptions{}).Rating; got != RatingTooBig {
		t.Fatalf("big rating = %q", got)
	}
	if got := Assess(mid, testMachine(), AssessOptions{}).Rating; got != RatingSlow {
		t.Fatalf("mid rating = %q", got)
	}
	withGPU := testMachine(func(machine *Machine) {
		machine.GPU = &GPU{Type: "cuda", Name: "RTX", MemoryMB: 24576, CUDA: "13.0"}
	})
	if got := Assess(mid, withGPU, AssessOptions{}).Rating; got != RatingGreat {
		t.Fatalf("mid on a graphics card = %q", got)
	}
	// Running models leave less room for the next one.
	if Assess(mid, testMachine(), AssessOptions{UsedMB: 8000}).FitsNow {
		t.Fatalf("mid still fits beside 8000 MB")
	}
	small := testMachine(func(machine *Machine) {
		machine.Disk = Disk{FreeMB: int64ptr(2000), TotalMB: int64ptr(10_000)}
	})
	if Assess(mid, small, AssessOptions{}).DiskOK {
		t.Fatalf("mid reports enough disk space")
	}
}

func TestFitUsesMeasuredMemoryAndWhatElseRuns(t *testing.T) {
	mid, _ := CatalogEntry("qwen3.5-9b")
	estimate := Assess(mid, testMachine(), AssessOptions{}).MemoryMB
	measured := int64(5000)
	assessed := Assess(mid, testMachine(), AssessOptions{MeasuredMB: &measured})
	if assessed.MemoryMB != 5000 || !assessed.Measured {
		t.Fatalf("measured = %+v", assessed)
	}
	if estimate <= 5000 {
		t.Fatalf("estimate = %d, want above 5000", estimate)
	}
	// Without measurements only the fixed reserve is kept.
	if got := RoomMB(testMachine()); got != 16384-ReserveMB {
		t.Fatalf("roomMb = %d", got)
	}
	// Other programs using 9 GB on average leave 16 - 9 - 1 GB of headroom.
	busy := testMachine(func(machine *Machine) { machine.Memory.OthersMB = int64ptr(9216) })
	if got := RoomMB(busy); got != 16384-9216-HeadroomMB {
		t.Fatalf("busy roomMb = %d", got)
	}
	tight := Assess(mid, busy, AssessOptions{})
	if tight.Rating != RatingTight || tight.FitsNow {
		t.Fatalf("tight = %+v", tight)
	}
	if got := Assess(catalogMust(t, "gpt-oss-120b"), busy, AssessOptions{}).Rating; got != RatingTooBig {
		t.Fatalf("big on a busy machine = %q", got)
	}
	if got := Assess(catalogMust(t, "qwen3.5-0.8b"), busy, AssessOptions{}).Rating; got != RatingGood {
		t.Fatalf("tiny on a busy machine = %q", got)
	}
}

func catalogMust(t *testing.T, id string) *Entry {
	t.Helper()
	entry, ok := CatalogEntry(id)
	if !ok {
		t.Fatalf("catalog has no %s", id)
	}
	return entry
}

func TestFitSpeechEverywhereTextOnlyWhereLlamaCppRuns(t *testing.T) {
	windows := testMachine(func(machine *Machine) { machine.Platform = "win32" })
	if got := Assess(catalogMust(t, "whisper-base"), windows, AssessOptions{}).Rating; got != RatingGreat {
		t.Fatalf("whisper on windows = %q", got)
	}
	if got := Assess(catalogMust(t, "qwen3.5-0.8b"), windows, AssessOptions{}).Rating; got != RatingUnsupported {
		t.Fatalf("chat model on windows = %q", got)
	}
}

func TestLlamaCppBuildsGPUFirstWithCPUFallback(t *testing.T) {
	ids := func(machine Machine) []string {
		out := []string{}
		for _, variant := range LlamaVariants(machine) {
			out = append(out, variant.ID)
		}
		return out
	}
	cases := []struct {
		name    string
		machine Machine
		want    []string
	}{
		{"apple silicon", testMachine(func(m *Machine) { m.Platform = "darwin"; m.Arch = "arm64" }), []string{"macos-arm64"}},
		{"plain linux", testMachine(), []string{"ubuntu-x64"}},
		{"cuda 13", testMachine(func(m *Machine) { m.GPU = &GPU{Type: "cuda", CUDA: "13.0"} }), []string{"ubuntu-cuda-13.5-x64", "ubuntu-x64"}},
		{"cuda 12", testMachine(func(m *Machine) { m.GPU = &GPU{Type: "cuda", CUDA: "12.4"} }), []string{"ubuntu-cuda-12.8-x64", "ubuntu-x64"}},
		{"linux arm", testMachine(func(m *Machine) { m.Arch = "arm64" }), []string{"ubuntu-arm64"}},
		{"windows", testMachine(func(m *Machine) { m.Platform = "win32" }), []string{}},
	}
	for _, tc := range cases {
		got := ids(tc.machine)
		if len(got) != len(tc.want) {
			t.Fatalf("%s: variants = %v, want %v", tc.name, got, tc.want)
		}
		for index := range got {
			if got[index] != tc.want[index] {
				t.Fatalf("%s: variants = %v, want %v", tc.name, got, tc.want)
			}
		}
	}
	cuda := LlamaVariants(testMachine(func(m *Machine) { m.GPU = &GPU{Type: "cuda", CUDA: "13.0"} }))[0]
	if len(cuda.Assets) != 2 {
		t.Fatalf("CUDA builds bring their runtime libraries: %v", cuda.Assets)
	}
}

func TestContextGrowsWithMemoryAndEmbeddingsKeepTheirSize(t *testing.T) {
	chat := catalogMust(t, "qwen3.5-4b")
	if got := ContextSize(chat, testMachine()); got != 8192 {
		t.Fatalf("16 GB context = %d", got)
	}
	if got := ContextSize(chat, testMachine(func(m *Machine) { m.Memory.TotalMB = 32768 })); got != 16384 {
		t.Fatalf("32 GB context = %d", got)
	}
	if got := ContextSize(chat, testMachine(func(m *Machine) { m.Memory.TotalMB = 131072 })); got != 32768 {
		t.Fatalf("128 GB context = %d", got)
	}
	if got := ContextSize(catalogMust(t, "nomic-embed-text-v1.5"), testMachine()); got != 8192 {
		t.Fatalf("embedding context = %d", got)
	}
}

func TestStarterSetFitsTogether(t *testing.T) {
	fits := func(machine Machine) []StarterSuggestion {
		set := SuggestStarter(machine, nil)
		budget := RoomMB(machine)
		total := int64(0)
		for _, item := range set {
			total += item.MemoryMB
		}
		if total > budget {
			t.Fatalf("starter set takes %d MB of %d", total, budget)
		}
		return set
	}
	mac := fits(testMachine(func(machine *Machine) {
		machine.Platform = "darwin"
		machine.Arch = "arm64"
		machine.GPU = &GPU{Type: "metal", Name: "Apple GPU", MemoryMB: 11468}
	}))
	kinds := []string{}
	for _, item := range mac {
		kinds = append(kinds, item.Kind)
	}
	if len(kinds) != 3 || kinds[0] != "chat" || kinds[1] != "transcription" || kinds[2] != "speech" {
		t.Fatalf("mac starter kinds = %v", kinds)
	}
	if mac[0].ID != "qwen3.5-4b" {
		t.Fatalf("mac starter chat model = %s", mac[0].ID)
	}
	// Without a graphics card the recommended model is slow here, so a smaller
	// one is chosen.
	cpu := fits(testMachine())
	if cpu[0].Kind != "chat" || cpu[0].ID == "qwen3.5-4b" {
		t.Fatalf("cpu starter = %+v", cpu[0])
	}
	// A tiny machine still gets what fits; Windows gets speech only.
	if got := fits(testMachine(func(machine *Machine) { machine.Memory = Memory{TotalMB: 4096} })); len(got) < 2 {
		t.Fatalf("tiny starter = %+v", got)
	}
	windows := fits(testMachine(func(machine *Machine) { machine.Platform = "win32" }))
	windowsKinds := []string{}
	for _, item := range windows {
		windowsKinds = append(windowsKinds, item.Kind)
	}
	if len(windowsKinds) != 2 || windowsKinds[0] != "transcription" || windowsKinds[1] != "speech" {
		t.Fatalf("windows starter = %v", windowsKinds)
	}
}

func TestOnlyWhisperModelsAreOfferedForTranslation(t *testing.T) {
	// The adapter answers canTranslate with the catalog's feature, so the
	// catalog carries the answer.
	whisper := catalogMust(t, "whisper-small")
	moonshine := catalogMust(t, "moonshine-base")
	if !whisper.Features.Translate {
		t.Fatalf("whisper-small cannot translate")
	}
	if moonshine.Features.Translate {
		t.Fatalf("moonshine-base claims to translate")
	}
}
