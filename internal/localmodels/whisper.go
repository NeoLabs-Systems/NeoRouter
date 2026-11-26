package localmodels

// whisper.go is the speech recognition the transformers.js worker used to do:
// Whisper's log-mel frontend, its encoder and a greedy decoder over the merged
// decoder graph, natively on ONNX Runtime.
//
// The graph work is split from the arithmetic on purpose: MelSpectrogram and the
// token loop are plain Go and tested without a model, while the ONNX sessions
// are the same whisper-base/small/large-v3-turbo checkpoints the catalog names.

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
)

// WhisperAudio constants, from Whisper's own feature extractor.
const (
	whisperSampleRate = 16000
	whisperFFT        = 400
	whisperHop        = 160
	whisperChunkS     = 30
	whisperStrideS    = 5
	// whisperFrameCount is how many spectrogram frames one 30 second window has.
	whisperFrameCount = whisperChunkS * whisperSampleRate / whisperHop
	maxDecodeTokens   = 448
)

// MelFilterbank is librosa.filters.mel(sr=16000, n_fft=400, n_mels=nMels) in
// Slaney's mel scale, which is the filterbank Whisper ships.
func MelFilterbank(nMels int) [][]float64 {
	frequencies := whisperFFT/2 + 1
	// Slaney's mel scale: linear below 1 kHz, logarithmic above.
	const (
		fMin      = 0.0
		fMax      = 8000.0
		fSp       = 200.0 / 3
		minLogHz  = 1000.0
		minLogMel = minLogHz / fSp
	)
	logStep := math.Log(6.4) / 27.0
	melToHz := func(mel float64) float64 {
		if mel >= minLogMel {
			return minLogHz * math.Exp(logStep*(mel-minLogMel))
		}
		return fSp * mel
	}
	hzToMel := func(hz float64) float64 {
		if hz >= minLogHz {
			return minLogMel + math.Log(hz/minLogHz)/logStep
		}
		return hz / fSp
	}
	_ = fMin
	_ = fMax
	// nMels + 2 band edges, converted to FFT bin positions.
	edges := make([]float64, nMels+2)
	melMin, melMax := hzToMel(fMin), hzToMel(fMax)
	for index := 0; index <= nMels+1; index++ {
		mel := melMin + (melMax-melMin)*float64(index)/float64(nMels+1)
		edges[index] = melToHz(mel)
	}
	binHz := func(bin int) float64 { return float64(bin) * 8000.0 / float64(whisperFFT/2) }
	filters := make([][]float64, nMels)
	for mel := 0; mel < nMels; mel++ {
		filters[mel] = make([]float64, frequencies)
		lower, center, upper := edges[mel], edges[mel+1], edges[mel+2]
		enorm := 2.0 / (upper - lower)
		for bin := 0; bin < frequencies; bin++ {
			hz := binHz(bin)
			switch {
			case hz >= lower && hz <= center:
				filters[mel][bin] = (hz - lower) / (center - lower)
			case hz > center && hz <= upper:
				filters[mel][bin] = (upper - hz) / (upper - center)
			}
			filters[mel][bin] *= enorm
		}
	}
	return filters
}

// fft runs a radix-2 FFT of samples, zero-padding to the next power of two the
// way librosa's STFT does, and returns the power spectrum of the first
// len(samples)/2+1 bins — the n_fft/2+1 bins a mel filterbank is built for.
func fft(samples []float64) []float64 {
	size := 1
	for size < len(samples) {
		size <<= 1
	}
	n := size
	re := make([]float64, n)
	im := make([]float64, n)
	copy(re, samples)
	// Bit-reversal permutation.
	for i, j := 1, 0; i < n; i++ {
		bit := n >> 1
		for ; j&bit != 0; bit >>= 1 {
			j ^= bit
		}
		j ^= bit
		if i < j {
			re[i], re[j] = re[j], re[i]
		}
	}
	for length := 2; length <= n; length <<= 1 {
		angle := -2 * math.Pi / float64(length)
		for start := 0; start < n; start += length {
			for offset := 0; offset < length/2; offset++ {
				cosine := math.Cos(angle * float64(offset))
				sine := math.Sin(angle * float64(offset))
				evenIndex := start + offset
				oddIndex := evenIndex + length/2
				oddRe := re[oddIndex]*cosine - im[oddIndex]*sine
				oddIm := re[oddIndex]*sine + im[oddIndex]*cosine
				re[oddIndex] = re[evenIndex] - oddRe
				im[oddIndex] = im[evenIndex] - oddIm
				re[evenIndex] += oddRe
				im[evenIndex] += oddIm
			}
		}
	}
	bins := len(samples)/2 + 1
	power := make([]float64, bins)
	for bin := 0; bin < bins; bin++ {
		power[bin] = re[bin]*re[bin] + im[bin]*im[bin]
	}
	return power
}

// hannWindow is the periodic Hann window Whisper's STFT applies.
func hannWindow(length int) []float64 {
	window := make([]float64, length)
	for index := range window {
		window[index] = 0.5 - 0.5*math.Cos(2*math.Pi*float64(index)/float64(length))
	}
	return window
}

// MelSpectrogram is Whisper's log-mel spectrogram of one window of samples:
// nMels rows of whisperFrameCount columns, normalized the way the model
// expects. Samples longer than the window are cut; shorter ones are padded with
// silence.
func MelSpectrogram(samples []float32, nMels int) [][]float32 {
	filters := MelFilterbank(nMels)
	window := hannWindow(whisperFFT)
	// The last frame reaches past the window, so the buffer carries one window
	// of slack, the way the centered STFT's padding does.
	needed := whisperFrameCount*whisperHop + whisperFFT
	if len(samples) < needed {
		padded := make([]float32, needed)
		copy(padded, samples)
		samples = padded
	}
	out := make([][]float32, nMels)
	for mel := range out {
		out[mel] = make([]float32, whisperFrameCount)
	}
	frame := make([]float64, whisperFFT)
	power := make([]float64, whisperFFT/2+1)
	for column := 0; column < whisperFrameCount; column++ {
		start := column * whisperHop
		for index := 0; index < whisperFFT; index++ {
			frame[index] = float64(samples[start+index]) * window[index]
		}
		copy(power, fft(frame))
		for mel := 0; mel < nMels; mel++ {
			sum := 0.0
			for bin := range power {
				sum += filters[mel][bin] * power[bin]
			}
			out[mel][column] = float32(sum)
		}
	}
	// log10 with a floor, then the max-8 clamp and the -1..1 scale Whisper uses.
	maximum := math.Inf(-1)
	for mel := range out {
		for column := range out[mel] {
			value := float64(out[mel][column])
			if value < 1e-10 {
				value = 1e-10
			}
			out[mel][column] = float32(math.Log10(value))
			if float64(out[mel][column]) > maximum {
				maximum = float64(out[mel][column])
			}
		}
	}
	floor := maximum - 8.0
	for mel := range out {
		for column := range out[mel] {
			value := float64(out[mel][column])
			if value < floor {
				value = floor
			}
			out[mel][column] = float32((value + 4.0) / 4.0)
		}
	}
	return out
}

// whisperConfig is the model's config.json, or the parts of it read here.
type whisperConfig struct {
	ModelType         string `json:"model_type"`
	NumMelBins        int    `json:"num_mel_bins"`
	DecoderStartToken int    `json:"decoder_start_token_id"`
	MaxLength         int    `json:"max_length"`
}

// whisperModel is one loaded speech recognition model.
type whisperModel struct {
	dir       string
	encoder   Session
	decoder   Session
	tokens    *Tokenizer
	config    whisperConfig
	nMels     int
	isWhisper bool
}

// SpeechSegment is one timed piece of a transcription.
type SpeechSegment struct {
	Start float64  `json:"start"`
	End   *float64 `json:"end"`
	Text  string   `json:"text"`
}

// Transcription is what the engine returns for one recording.
type Transcription struct {
	Text     string          `json:"text"`
	Segments []SpeechSegment `json:"segments"`
	Duration float64         `json:"duration"`
	Language string          `json:"language"`
}

// loadWhisperModel opens a Whisper model directory: the encoder and the merged
// decoder graph, its tokenizer and its config.
func loadWhisperModel(dir string, open func(string) (Session, error)) (*whisperModel, error) {
	encoderPath, ok := modelFile(dir, "onnx/encoder_model_quantized.onnx", "onnx/encoder_model.onnx")
	if !ok {
		return nil, fmt.Errorf("The model has no ONNX encoder.")
	}
	decoderPath, ok := modelFile(dir, "onnx/decoder_model_merged_quantized.onnx", "onnx/decoder_model_merged.onnx", "onnx/decoder_model.onnx")
	if !ok {
		return nil, fmt.Errorf("The model has no ONNX decoder.")
	}
	encoder, err := open(encoderPath)
	if err != nil {
		return nil, err
	}
	decoder, err := open(decoderPath)
	if err != nil {
		encoder.Close()
		return nil, err
	}
	tokens, err := loadTokenizerFor(dir)
	if err != nil {
		encoder.Close()
		decoder.Close()
		return nil, err
	}
	model := &whisperModel{dir: dir, encoder: encoder, decoder: decoder, tokens: tokens, nMels: 80}
	if data, err := os.ReadFile(filepath.Join(dir, "config.json")); err == nil {
		_ = json.Unmarshal(data, &model.config)
	}
	if model.config.NumMelBins > 0 {
		model.nMels = model.config.NumMelBins
	}
	model.isWhisper = model.config.ModelType == "whisper" || model.config.ModelType == ""
	return model, nil
}

// loadTokenizerFor finds the tokenizer.json beside a model.
func loadTokenizerFor(dir string) (*Tokenizer, error) {
	path, ok := modelFile(dir, "tokenizer.json")
	if !ok {
		return nil, fmt.Errorf("The model has no tokenizer.json.")
	}
	return LoadTokenizer(path)
}

// decoderInputFor builds one decoder step's inputs, feeding the cache back.
func (m *whisperModel) decoderInputs(tokens []int32, hidden *Tensor, cache map[string]*Tensor) map[string]*Tensor {
	ids := make([]int64, len(tokens))
	for index, id := range tokens {
		ids[index] = int64(id)
	}
	inputs := map[string]*Tensor{
		"input_ids":             Int64Tensor([]int64{1, int64(len(ids))}, ids),
		"encoder_hidden_states": hidden,
	}
	for name, tensor := range cache {
		inputs[name] = tensor
	}
	return inputs
}

// Transcribe runs a whole recording through the model.
func (m *whisperModel) Transcribe(ctx context.Context, samples []float32, language string, translate bool) (Transcription, error) {
	result := Transcription{Segments: []SpeechSegment{}, Duration: float64(len(samples)) / whisperSampleRate}
	texts := []string{}
	chunk := whisperChunkS * whisperSampleRate
	step := (whisperChunkS - whisperStrideS) * whisperSampleRate
	for start := 0; start < len(samples) || start == 0; start += step {
		if err := ctx.Err(); err != nil {
			return Transcription{}, err
		}
		window := samples[start:]
		offset := float64(start) / whisperSampleRate
		text, segments, err := m.TranscribeWindow(ctx, window, language, translate, offset)
		if err != nil {
			return Transcription{}, err
		}
		if text != "" {
			texts = append(texts, text)
		}
		result.Segments = append(result.Segments, segments...)
		if len(window) <= chunk {
			break
		}
	}
	result.Text = strings.TrimSpace(strings.Join(texts, " "))
	if language != "" {
		result.Language = language
	}
	return result, nil
}

// TranscribeWindow decodes one 30 second window.
func (m *whisperModel) TranscribeWindow(ctx context.Context, samples []float32, language string, translate bool, offset float64) (string, []SpeechSegment, error) {
	spectrogram := MelSpectrogram(samples, m.nMels)
	features := make([]float32, 0, m.nMels*whisperFrameCount)
	for band := 0; band < m.nMels; band++ {
		features = append(features, spectrogram[band]...)
	}
	encoderOut, err := m.encoder.Run(map[string]*Tensor{
		"input_features": FloatTensor([]int64{1, int64(m.nMels), whisperFrameCount}, features),
	})
	if err != nil {
		return "", nil, err
	}
	hidden := encoderOut["last_hidden_state"]
	if hidden == nil {
		for name, tensor := range encoderOut {
			if strings.Contains(name, "hidden") {
				hidden = tensor
				break
			}
		}
	}
	if hidden == nil {
		return "", nil, fmt.Errorf("The model's encoder returned no hidden state.")
	}
	tokens, err := m.startTokens(language, translate)
	if err != nil {
		return "", nil, err
	}
	cache := map[string]*Tensor{}
	decoded := []int32{}
	for step := 0; step < maxDecodeTokens; step++ {
		if err := ctx.Err(); err != nil {
			return "", nil, err
		}
		feed := tokens
		if step > 0 {
			feed = tokens[len(tokens)-1:]
		}
		outputs, err := m.decoder.Run(m.decoderInputs(feed, hidden, cache))
		if err != nil {
			return "", nil, err
		}
		// Keep every present.* output as this step's cache.
		for name, tensor := range outputs {
			if strings.HasPrefix(name, "present") {
				cache[name] = tensor
			}
		}
		logits := outputs["logits"]
		if logits == nil {
			for name, tensor := range outputs {
				if strings.Contains(name, "logits") {
					logits = tensor
					break
				}
			}
		}
		if logits == nil {
			return "", nil, fmt.Errorf("The model's decoder returned no logits.")
		}
		next := argmaxLast(logits)
		endID, _ := m.tokens.SpecialID("<|endoftext|>")
		if next == endID {
			break
		}
		tokens = append(tokens, next)
		decoded = append(decoded, next)
	}
	text, segments := m.textOf(decoded, offset)
	return text, segments, nil
}

// startTokens are the forced decoder prefix: transcript start, language, task
// and no timestamps.
func (m *whisperModel) startTokens(language string, translate bool) ([]int32, error) {
	start, ok := m.tokens.SpecialID("<|startoftranscript|>")
	if !ok {
		return nil, fmt.Errorf("The model's tokenizer has no <|startoftranscript|>.")
	}
	tokens := []int32{start}
	code := language
	if code == "" {
		code = "en"
	}
	if id, ok := m.tokens.SpecialID("<|" + code + "|>"); ok {
		tokens = append(tokens, id)
	}
	task := "<|transcribe|>"
	if translate {
		task = "<|translate|>"
	}
	if id, ok := m.tokens.SpecialID(task); ok {
		tokens = append(tokens, id)
	}
	if id, ok := m.tokens.SpecialID("<|notimestamps|>"); ok {
		tokens = append(tokens, id)
	}
	return tokens, nil
}

// timestampBegin is the first timestamp token id of a Whisper vocabulary.
func (m *whisperModel) timestampBegin() (int32, bool) {
	if id, ok := m.tokens.SpecialID("<|0.00|>"); ok {
		return id, true
	}
	// Older vocabularies start timestamps right after <|notimestamps|>.
	if id, ok := m.tokens.SpecialID("<|notimestamps|>"); ok {
		return id + 1, true
	}
	return 0, false
}

// textOf turns decoded tokens into text and timed segments.
func (m *whisperModel) textOf(ids []int32, offset float64) (string, []SpeechSegment) {
	timestampBegin, hasTimestamps := m.timestampBegin()
	eotID, _ := m.tokens.SpecialID("<|endoftext|>")
	segments := []SpeechSegment{}
	pending := []int32{}
	start := offset
	flush := func(end float64) {
		text := strings.TrimSpace(m.tokens.Decode(pending))
		pending = nil
		if text == "" {
			return
		}
		finish := end
		segments = append(segments, SpeechSegment{Start: start, End: &finish, Text: text})
	}
	for _, id := range ids {
		if hasTimestamps && id >= timestampBegin {
			stamp := offset + float64(id-timestampBegin)*0.02
			flush(stamp)
			start = stamp
			continue
		}
		if id == eotID || id == m.tokens.unkID {
			continue
		}
		pending = append(pending, id)
	}
	flush(offset + whisperChunkS)
	texts := make([]string, 0, len(segments))
	for _, segment := range segments {
		texts = append(texts, segment.Text)
	}
	return strings.Join(texts, " "), segments
}

// argmaxLast picks the highest scoring token of the last position.
func argmaxLast(logits *Tensor) int32 {
	if logits == nil || len(logits.Floats) == 0 {
		return 0
	}
	vocabulary := 1
	if len(logits.Shape) >= 1 {
		vocabulary = int(logits.Shape[len(logits.Shape)-1])
	}
	if vocabulary <= 0 || vocabulary > len(logits.Floats) {
		vocabulary = len(logits.Floats)
	}
	offset := len(logits.Floats) - vocabulary
	best := 0
	bestScore := math.Inf(-1)
	for index := 0; index < vocabulary; index++ {
		if score := float64(logits.Floats[offset+index]); score > bestScore {
			bestScore = score
			best = index
		}
	}
	return int32(best)
}
