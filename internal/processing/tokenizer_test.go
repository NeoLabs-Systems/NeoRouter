package processing

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/NeoLabs-Systems/neorouter/internal/processing/onnxrt"
)

// writeTokenizer lays a small GPT-style tokenizer.json in a temp directory: a Split
// pre-tokenizer, byte-level BPE with two merges and a handful of tokens.
func writeTokenizer(t *testing.T) string {
	t.Helper()
	// The privacy filter's own split pattern, reduced to ASCII letters and spaces so
	// the fixture stays readable.
	source := `{
	  "pre_tokenizer": {"type": "Sequence", "pretokenizers": [
	    {"type": "Split", "pattern": {"Regex": "[A-Za-z]+| ?[^\\sA-Za-z]+|\\s+"}, "behavior": "Isolated"},
	    {"type": "ByteLevel", "add_prefix_space": false, "use_regex": false}
	  ]},
	  "post_processor": {"type": "ByteLevel", "add_prefix_space": true},
	  "model": {"type": "BPE", "ignore_merges": true,
	    "vocab": {" ": 0, "h": 1, "e": 2, "l": 3, "o": 4, "he": 5, "llo": 6, "hello": 7, "w": 8, "orld": 9, "world": 10, "Ġ": 11, "Ġhello": 12, "Ġworld": 13},
	    "merges": [["h", "e"], ["l", "l"], ["l", "o"], ["he", "llo"], ["w", "orld"]]}
	}`
	path := filepath.Join(t.TempDir(), "tokenizer.json")
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestTokenizerEncodes covers the BPE path the privacy filter needs: the bytes to
// characters mapping, the split pre-tokenizer, the merges and the offsets.
func TestTokenizerEncodes(t *testing.T) {
	tokenizer, err := LoadTokenizer(writeTokenizer(t))
	if err != nil {
		t.Fatal(err)
	}
	text := "hello world"
	tokens := tokenizer.Encode(text)
	if len(tokens) == 0 {
		t.Fatal("no tokens")
	}
	// The post-processor's leading space is a token of its own; its offsets are
	// clamped to the start of the text, like the JS trim_offsets.
	if got := tokens[0]; got.Start != 0 || got.End != 0 {
		t.Fatalf("leading space token covers %d:%d, want 0:0", got.Start, got.End)
	}
	covered := ""
	last := 0
	for _, token := range tokens {
		if token.Start < 0 || token.End > len(text) || token.Start > token.End || token.Start < last {
			t.Fatalf("token offsets %d:%d are not ordered inside the text", token.Start, token.End)
		}
		last = token.End
		covered += text[token.Start:token.End]
	}
	if covered != text {
		t.Fatalf("tokens cover %q, want %q", covered, text)
	}
}

// TestTokenizerRejectsUnsupportedModels proves a tokenizer the port cannot read
// fails loudly instead of producing nonsense.
func TestTokenizerRejectsUnsupportedModels(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tokenizer.json")
	if err := os.WriteFile(path, []byte(`{"model":{"type":"WordPiece"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadTokenizer(path); err == nil {
		t.Fatal("WordPiece must be refused")
	}
}

// TestAggregateEntities covers aggregation_strategy "simple": B- starts, I-
// continues, E- closes, S- is a single token, O breaks a group.
func TestAggregateEntities(t *testing.T) {
	text := "Anna Schmidt works at Acme in Berlin"
	tokens := []Token{{Start: 0, End: 4}, {Start: 5, End: 12}, {Start: 13, End: 18}, {Start: 22, End: 26}, {Start: 30, End: 36}}
	labels := []int{1, 2, 0, 3, 0}
	names := []string{"O", "B-private_person", "I-private_person", "S-private_person"}
	scores := []float64{0.9, 0.9, 0.9, 0.8, 0.8}
	entities := aggregateEntities(text, tokens, labels, scores, names)
	if len(entities) != 2 {
		t.Fatalf("%d entities: %+v", len(entities), entities)
	}
	if entities[0].EntityGroup != "private_person" || entities[0].Word != "Anna Schmidt" {
		t.Fatalf("first entity %+v", entities[0])
	}
	if entities[0].Score < 0.89 || entities[0].Score > 0.91 {
		t.Fatalf("averaged score %v", entities[0].Score)
	}
	if entities[1].Word != "Acme" || entities[1].EntityGroup != "private_person" {
		t.Fatalf("second entity %+v", entities[1])
	}
}

// TestONNXSmokeLoadsTheRealModel runs the real loader only where the runtime
// library and the weights are installed; everywhere else it says why it is skipped.
func TestONNXSmokeLoadsTheRealModel(t *testing.T) {
	if !onnxrt.HasRuntime() {
		t.Skip("skip: the ONNX Runtime shared library is not installed")
	}
	service, _ := testService(t)
	dir := service.Privacy.ModelDir()
	for _, name := range privacyModelFiles() {
		if info, err := os.Stat(filepath.Join(dir, filepath.FromSlash(name))); err != nil || info.Size() == 0 {
			t.Skipf("skip: the privacy filter weights are not installed (%s)", name)
		}
	}
	if _, err := service.Privacy.SetEnabled(t.Context(), true); err != nil {
		t.Fatal(err)
	}
	defer service.Privacy.SetEnabled(t.Context(), false)
	if _, err := service.Privacy.load(t.Context()); err != nil {
		t.Fatalf("load the real privacy filter: %v", err)
	}
	items, err := service.Privacy.Detect(t.Context(), "Call Anna Schmidt on 0170 1234567.")
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	t.Logf("the real model found %+v", items)
}
