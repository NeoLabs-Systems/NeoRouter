package localmodels

// vits.go is text-to-speech for the ONNX voices of the catalog: Meta's MMS
// models, a single VITS graph that turns token ids into a waveform. The
// transformers.js worker ran the same graph through the same library.

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
)

// vitsConfig is the part of a VITS model's config.json this package reads.
type vitsConfig struct {
	SamplingRate int `json:"sampling_rate"`
}

// vitsModel is one loaded text-to-speech model.
type vitsModel struct {
	dir        string
	session    Session
	tokens     *Tokenizer
	sampleRate int
	padID      int32
}

// loadVITSModel opens a text-to-speech model directory.
func loadVITSModel(dir string, open func(string) (Session, error)) (*vitsModel, error) {
	path, ok := modelFile(dir, "onnx/model_quantized.onnx", "onnx/model.onnx")
	if !ok {
		return nil, fmt.Errorf("The model has no ONNX graph.")
	}
	session, err := open(path)
	if err != nil {
		return nil, err
	}
	tokens, err := loadTokenizerFor(dir)
	if err != nil {
		session.Close()
		return nil, err
	}
	model := &vitsModel{dir: dir, session: session, tokens: tokens, sampleRate: 16000}
	if data, err := os.ReadFile(filepath.Join(dir, "config.json")); err == nil {
		var config vitsConfig
		if json.Unmarshal(data, &config) == nil && config.SamplingRate > 0 {
			model.sampleRate = config.SamplingRate
		}
	}
	// The VITS tokenizer separates words with the padding token.
	if id, ok := tokens.SpecialID("<pad>"); ok {
		model.padID = id
	}
	return model, nil
}

// Speech speaks one part of a text: the token ids in, the samples out.
func (m *vitsModel) Speech(ctx context.Context, text string, speaker []float32) ([]float32, int, error) {
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	ids := m.tokenize(text)
	if len(ids) == 0 {
		return []float32{}, m.sampleRate, nil
	}
	mask := make([]int64, len(ids))
	values := make([]int64, len(ids))
	for index, id := range ids {
		values[index] = int64(id)
		mask[index] = 1
	}
	inputs := map[string]*Tensor{
		"input_ids":      Int64Tensor([]int64{1, int64(len(ids))}, values),
		"attention_mask": Int64Tensor([]int64{1, int64(len(ids))}, mask),
		"input_values":   Int64Tensor([]int64{1, int64(len(ids))}, values),
	}
	if len(speaker) > 0 && m.hasInput("speaker_embeddings") {
		inputs["speaker_embeddings"] = FloatTensor([]int64{1, int64(len(speaker))}, speaker)
	}
	outputs, err := m.session.Run(inputs)
	if err != nil {
		return nil, 0, err
	}
	for _, name := range m.session.OutputNames() {
		tensor := outputs[name]
		if tensor == nil || len(tensor.Floats) == 0 {
			continue
		}
		return tensor.Floats, m.sampleRate, nil
	}
	return nil, 0, fmt.Errorf("The model produced no audio.")
}

// hasInput reports whether the graph takes a named input.
func (m *vitsModel) hasInput(name string) bool {
	for _, candidate := range m.session.InputNames() {
		if candidate == name {
			return true
		}
	}
	return false
}

// tokenize is the word-separated tokenization a VITS graph expects.
func (m *vitsModel) tokenize(text string) []int32 {
	out := []int32{}
	for index, word := range strings.Fields(text) {
		if index > 0 && m.padID >= 0 {
			out = append(out, m.padID)
		}
		out = append(out, m.tokens.Encode(word)...)
	}
	return out
}

// Close releases the graph.
func (m *vitsModel) Close() {
	if m != nil && m.session != nil {
		m.session.Close()
	}
}

// voiceEmbedding reads a voice file: raw float32 samples, as the Supertonic
// voices are stored.
func voiceEmbedding(path string) ([]float32, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	count := len(data) / 4
	out := make([]float32, count)
	for index := 0; index < count; index++ {
		bits := uint32(data[index*4]) | uint32(data[index*4+1])<<8 | uint32(data[index*4+2])<<16 | uint32(data[index*4+3])<<24
		out[index] = math.Float32frombits(bits)
	}
	return out, nil
}
