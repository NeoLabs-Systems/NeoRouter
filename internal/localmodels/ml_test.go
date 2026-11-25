package localmodels

// The native ONNX machinery: the tokenizer, the log-mel frontend and the
// decoder loop, plus one real inference when this machine has ONNX Runtime and a
// model to run. These replace the transformers.js worker, so they are tested
// without it.

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// writeByteLevelTokenizer writes a tokenizer.json whose vocabulary is every
// single byte in byte-level form, plus the Whisper special tokens.
func writeByteLevelTokenizer(t *testing.T, dir string, extra map[string]int64) string {
	t.Helper()
	table := gpt2BytesToUnicode()
	vocab := map[string]int{}
	for b := 0; b < 256; b++ {
		vocab[string(table[b])] = b
	}
	next := 256
	type added struct {
		ID      int32  `json:"id"`
		Content string `json:"content"`
	}
	specials := []added{}
	for name, id := range extra {
		vocab[name] = int(id)
		specials = append(specials, added{ID: int32(id), Content: name})
		next = int(id) + 1
	}
	_ = next
	document := map[string]any{
		"model":        map[string]any{"type": "BPE", "vocab": vocab, "merges": []any{}},
		"added_tokens": specials,
	}
	data, err := json.Marshal(document)
	if err != nil {
		t.Fatalf("encode tokenizer: %v", err)
	}
	path := filepath.Join(dir, "tokenizer.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write tokenizer: %v", err)
	}
	return path
}

func TestTokenizerEncodesAndDecodesByteLevelText(t *testing.T) {
	dir := t.TempDir()
	path := writeByteLevelTokenizer(t, dir, map[string]int64{
		"<|startoftranscript|>": 50258,
		"<|endoftext|>":         50257,
		"<|en|>":                50259,
		"<|transcribe|>":        50359,
		"<|translate|>":         50358,
		"<|notimestamps|>":      50363,
		"<|0.00|>":              50364,
	})
	tokens, err := LoadTokenizer(path)
	if err != nil {
		t.Fatalf("load tokenizer: %v", err)
	}
	if got := tokens.Encode("hi"); len(got) != 2 || got[0] != 'h' || got[1] != 'i' {
		t.Fatalf("encode(hi) = %v", got)
	}
	if got := tokens.Decode([]int32{'h', 'i'}); got != "hi" {
		t.Fatalf("decode = %q", got)
	}
	// A space is its own byte-level token, which decodes back to a space.
	encoded := tokens.Encode(" a")
	if len(encoded) != 2 {
		t.Fatalf("encode( a) = %v", encoded)
	}
	if got := tokens.Decode(encoded); got != " a" {
		t.Fatalf("decode of %v = %q", encoded, got)
	}
	if id, ok := tokens.SpecialID("<|startoftranscript|>"); !ok || id != 50258 {
		t.Fatalf("special token = %v %v", id, ok)
	}
}

func TestTokenizerAppliesMerges(t *testing.T) {
	dir := t.TempDir()
	document := map[string]any{
		"model": map[string]any{
			"type":  "BPE",
			"vocab": map[string]int{"h": 0, "i": 1, "hi": 2},
			"merges": []any{
				[]any{"h", "i"},
			},
		},
	}
	data, _ := json.Marshal(document)
	path := filepath.Join(dir, "tokenizer.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write tokenizer: %v", err)
	}
	tokens, err := LoadTokenizer(path)
	if err != nil {
		t.Fatalf("load tokenizer: %v", err)
	}
	// "hi" is a byte-level vocabulary's own token here, so the merge is what
	// joins the two pieces the text produces.
	if got := tokens.Encode("hi"); len(got) != 1 || got[0] != 2 {
		t.Fatalf("encode(hi) = %v, want the merged token", got)
	}
}

func TestMelSpectrogramHasTheShapeWhisperExpects(t *testing.T) {
	silence := make([]float32, whisperSampleRate)
	mel := MelSpectrogram(silence, 80)
	if len(mel) != 80 {
		t.Fatalf("bands = %d", len(mel))
	}
	for band := range mel {
		if len(mel[band]) != whisperFrameCount {
			t.Fatalf("band %d has %d frames, want %d", band, len(mel[band]), whisperFrameCount)
		}
	}
	// Silence is the log floor everywhere, so every band is the same value.
	first := mel[0][0]
	for band := range mel {
		for _, value := range mel[band] {
			if value != first {
				t.Fatalf("silence is not flat: %v and %v", value, first)
			}
		}
	}
	// A tone in the middle of the spectrum lifts the bands that cover it.
	tone := make([]float32, whisperSampleRate)
	for index := range tone {
		tone[index] = float32(0.5 * math.Sin(2*math.Pi*1000*float64(index)/whisperSampleRate))
	}
	loud := MelSpectrogram(tone, 80)
	maximum := math.Inf(-1)
	for band := range loud {
		for _, value := range loud[band] {
			if float64(value) > maximum {
				maximum = float64(value)
			}
		}
	}
	if maximum <= float64(first) {
		t.Fatalf("a tone does not lift the spectrogram: %v <= %v", maximum, first)
	}
}

// fakeSession is an ONNX graph that answers from a function.
type fakeSession struct {
	inputs  []string
	outputs []string
	run     func(inputs map[string]*Tensor) (map[string]*Tensor, error)
	closed  bool
}

func (f *fakeSession) InputNames() []string  { return f.inputs }
func (f *fakeSession) OutputNames() []string { return f.outputs }
func (f *fakeSession) Run(inputs map[string]*Tensor) (map[string]*Tensor, error) {
	return f.run(inputs)
}
func (f *fakeSession) Close() { f.closed = true }

func TestWhisperDecoderLoopProducesText(t *testing.T) {
	dir := t.TempDir()
	writeByteLevelTokenizer(t, dir, map[string]int64{
		"<|startoftranscript|>": 50258,
		"<|endoftext|>":         50257,
		"<|en|>":                50259,
		"<|transcribe|>":        50359,
		"<|translate|>":         50358,
		"<|notimestamps|>":      50363,
		"<|0.00|>":              50364,
	})
	// The decoder answers with "hi" and then the end token.
	script := []int32{'h', 'i', 50257}
	step := 0
	vocabulary := 60000
	const hidden = 4
	encoder := &fakeSession{
		inputs:  []string{"input_features"},
		outputs: []string{"last_hidden_state"},
		run: func(inputs map[string]*Tensor) (map[string]*Tensor, error) {
			features := inputs["input_features"]
			if len(features.Shape) != 3 || features.Shape[0] != 1 || features.Shape[1] != 80 || features.Shape[2] != whisperFrameCount {
				return nil, fmt.Errorf("unexpected input shape %v", features.Shape)
			}
			return map[string]*Tensor{
				"last_hidden_state": FloatTensor([]int64{1, 1500, hidden}, make([]float32, 1500*hidden)),
			}, nil
		},
	}
	decoder := &fakeSession{
		inputs:  []string{"input_ids", "encoder_hidden_states", "past_key_values.0.key"},
		outputs: []string{"logits", "present.0.key"},
		run: func(inputs map[string]*Tensor) (map[string]*Tensor, error) {
			wanted := script[len(script)-1]
			if step < len(script) {
				wanted = script[step]
			}
			step++
			logits := make([]float32, vocabulary)
			logits[wanted] = 10
			return map[string]*Tensor{
				"logits":        FloatTensor([]int64{1, 1, int64(vocabulary)}, logits),
				"present.0.key": FloatTensor([]int64{1, 1, 1}, []float32{float32(step)}),
			}, nil
		},
	}
	model := &whisperModel{dir: dir, encoder: encoder, decoder: decoder, nMels: 80}
	tokens, err := loadTokenizerFor(dir)
	if err != nil {
		t.Fatalf("tokenizer: %v", err)
	}
	model.tokens = tokens

	text, segments, err := model.TranscribeWindow(context.Background(), make([]float32, 16000), "en", false, 0)
	if err != nil {
		t.Fatalf("transcribe: %v", err)
	}
	if text != "hi" {
		t.Fatalf("text = %q, want %q", text, "hi")
	}
	if len(segments) != 1 || segments[0].Text != "hi" {
		t.Fatalf("segments = %+v", segments)
	}
	if segments[0].End == nil || *segments[0].End <= segments[0].Start {
		t.Fatalf("segment timing = %+v", segments[0])
	}
}

// TestONNXRuntimeRunsARealModel is the one test that touches the real library: it
// loads ONNX Runtime and runs a model this machine happens to have, which proves
// the binding (api table slots, tensor creation, output reading) works. It skips
// when either is missing.
func TestONNXRuntimeRunsARealModel(t *testing.T) {
	// The model is this machine's business: point the test at any ONNX graph and
	// its tokenizer.json, e.g. a transformers.js model cache.
	modelPath := os.Getenv("NEOROUTER_TEST_ONNX_MODEL")
	tokenizerPath := os.Getenv("NEOROUTER_TEST_ONNX_TOKENIZER")
	libraryDir := os.Getenv("NEOROUTER_TEST_ONNX_LIBRARY_DIR")
	if modelPath == "" || tokenizerPath == "" {
		t.Skip("set NEOROUTER_TEST_ONNX_MODEL and NEOROUTER_TEST_ONNX_TOKENIZER to run a real model")
	}
	if _, err := os.Stat(modelPath); err != nil {
		t.Skipf("no ONNX model at %s", modelPath)
	}
	if libraryDir == "" {
		libraryDir = filepath.Join("..", "..", "node_modules", "onnxruntime-node", "bin", "napi-v6", runtime.GOOS, nodeArch(runtime.GOARCH))
	}
	absoluteLibrary, err := filepath.Abs(libraryDir)
	if err != nil {
		t.Fatalf("resolve library dir: %v", err)
	}
	if _, err := os.Stat(absoluteLibrary); err != nil {
		t.Skip("no ONNX Runtime library in this workspace")
	}
	t.Setenv("NEOROUTER_ORT_DIR", absoluteLibrary)
	if !ORTAvailable() {
		t.Skip("ONNX Runtime is not loadable here")
	}
	tokens, err := LoadTokenizer(tokenizerPath)
	if err != nil {
		t.Fatalf("load tokenizer: %v", err)
	}
	session, err := loadONNXSession(modelPath)
	if err != nil {
		t.Fatalf("load session: %v", err)
	}
	defer session.Close()

	ids := tokens.Encode("hello world")
	if len(ids) == 0 {
		t.Fatalf("tokenizer produced no ids")
	}
	inputIDs := make([]int64, len(ids))
	mask := make([]int64, len(ids))
	for index, id := range ids {
		inputIDs[index] = int64(id)
		mask[index] = 1
	}
	shape := []int64{1, int64(len(ids))}
	inputs := map[string]*Tensor{
		"input_ids":      Int64Tensor(shape, inputIDs),
		"attention_mask": Int64Tensor(shape, mask),
	}
	// Encoder-style graphs also take token_type_ids; feed zeros when they ask.
	for _, name := range session.InputNames() {
		if name == "token_type_ids" {
			inputs[name] = Int64Tensor(shape, make([]int64, len(ids)))
		}
	}
	outputs, err := session.Run(inputs)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	var hidden *Tensor
	for _, tensor := range outputs {
		if len(tensor.Shape) == 3 {
			hidden = tensor
			break
		}
	}
	if hidden == nil {
		t.Fatalf("no hidden state in %v", session.OutputNames())
	}
	if hidden.Shape[0] != 1 || hidden.Shape[1] != int64(len(ids)) {
		t.Fatalf("hidden shape = %v", hidden.Shape)
	}
	dimensions := int(hidden.Shape[2])
	if dimensions < 64 {
		t.Fatalf("hidden size = %d", dimensions)
	}
	// Every value is a real number, and the mean-pooled vector has a usable
	// length: the same pooling the embeddings reference does.
	sum := 0.0
	for _, value := range hidden.Floats {
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
			t.Fatalf("the model produced %v", value)
		}
		sum += float64(value) * float64(value)
	}
	if sum <= 0 {
		t.Fatalf("the model produced an all-zero hidden state")
	}
}
