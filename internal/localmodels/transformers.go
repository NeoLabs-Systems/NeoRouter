package localmodels

// transformers.go runs the speech and transcription models. It runs the ONNX
// graphs natively, in process, answering `ping`, `speech` and `transcribe`.

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"sync"
)

// SpeechRequest is one text-to-speech request.
type SpeechRequest struct {
	Text string `json:"text"`
	// Voice is the chosen voice id; empty or unknown means the first one.
	Voice string `json:"voice"`
	// Speed is the speaking rate, 0 for the model's own.
	Speed float64 `json:"speed"`
}

// SpeechResult is the audio a voice produced: float samples and their rate.
type SpeechResult struct {
	Samples []float32 `json:"samples"`
	Rate    int       `json:"rate"`
}

// TranscribeRequest is one transcription request: 16 kHz mono samples.
type TranscribeRequest struct {
	Samples   []float32 `json:"samples"`
	Language  string    `json:"language"`
	Translate bool      `json:"translate"`
}

// TransformersEngine runs the catalog's ONNX speech models.
type TransformersEngine struct {
	Paths Paths
	Log   *slog.Logger
	// Open loads one ONNX graph; nil uses ONNX Runtime.
	Open func(path string) (Session, error)
}

// speechModel is what one started model can do.
type speechModel struct {
	whisper *whisperModel
	vits    *vitsModel

	entry *Entry
	files ModelFiles
}

// Support is transformers.js's answer: the engine itself always runs; a machine
// that lacks the ONNX runtime fails when a model starts.
func (e *TransformersEngine) Support(machine Machine) Support { return supported() }

// Prepare has nothing to install: the runtime is found on the machine.
func (e *TransformersEngine) Prepare(ctx context.Context, machine Machine, opts PrepareOptions) error {
	return nil
}

// open loads an ONNX graph, through ONNX Runtime unless a test wired its own.
func (e *TransformersEngine) open(path string) (Session, error) {
	if e.Open != nil {
		return e.Open(path)
	}
	return loadONNXSession(path)
}

// Start loads one model's graphs and returns the instance its requests reach.
func (e *TransformersEngine) Start(ctx context.Context, in StartInput) (*Instance, error) {
	if in.Entry.Transformers == nil {
		return nil, &StartError{Message: "The model " + in.Entry.ID + " has no task."}
	}
	dir := in.Files.Model
	model := &speechModel{entry: in.Entry, files: in.Files}
	process := newInProcess()
	var cleanup func()
	switch in.Entry.Transformers.Task {
	case "automatic-speech-recognition":
		whisper, err := loadWhisperModel(dir, e.open)
		if err != nil {
			return nil, &StartError{Message: "The model could not load: " + err.Error()}
		}
		model.whisper = whisper
		cleanup = func() {
			whisper.encoder.Close()
			whisper.decoder.Close()
		}
	case "text-to-speech":
		if isMultiModelVoice(dir) {
			return nil, &StartError{Message: "The model could not load: The model " + in.Entry.ID + " is not supported by this build."}
		}
		vits, err := loadVITSModel(dir, e.open)
		if err != nil {
			return nil, &StartError{Message: "The model could not load: " + err.Error()}
		}
		model.vits = vits
		cleanup = func() { vits.Close() }
	default:
		return nil, &StartError{Message: "The model could not load: unknown task " + in.Entry.Transformers.Task}
	}
	process.onStop = cleanup
	instance := &Instance{
		Proc:          process,
		ContextWindow: nil,
		GPU:           false,
		Invoke: func(ctx context.Context, method string, payload any) (any, error) {
			return model.invoke(ctx, method, payload)
		},
		Health: func(ctx context.Context) bool { return model != nil },
	}
	return instance, nil
}

// invoke answers one request, with the worker's method names.
func (m *speechModel) invoke(ctx context.Context, method string, payload any) (any, error) {
	switch method {
	case "ping":
		return map[string]any{"ok": true}, nil
	case "speech":
		request, ok := payload.(SpeechRequest)
		if !ok {
			return nil, fmt.Errorf("Unknown request %s.", method)
		}
		return m.speech(ctx, request)
	case "transcribe":
		request, ok := payload.(TranscribeRequest)
		if !ok {
			return nil, fmt.Errorf("Unknown request %s.", method)
		}
		return m.transcribe(ctx, request)
	default:
		return nil, fmt.Errorf("Unknown request %s.", method)
	}
}

// speech speaks the text in parts, so a long text never exceeds what the
// tokenizer can hold, and returns one continuous recording.
func (m *speechModel) speech(ctx context.Context, request SpeechRequest) (SpeechResult, error) {
	if m.vits == nil {
		return SpeechResult{}, fmt.Errorf("This local model does not speak.")
	}
	speaker, err := m.voiceEmbedding(request.Voice)
	if err != nil {
		return SpeechResult{}, err
	}
	samples := []float32{}
	rate := m.vits.sampleRate
	for _, part := range SpeechParts(request.Text, MaxSpeechChars) {
		if err := ctx.Err(); err != nil {
			return SpeechResult{}, err
		}
		chunk, chunkRate, err := m.vits.Speech(ctx, part, speaker)
		if err != nil {
			return SpeechResult{}, err
		}
		if chunkRate > 0 {
			rate = chunkRate
		}
		samples = append(samples, chunk...)
	}
	if request.Speed > 0 && math.Abs(request.Speed-1) > 0.001 {
		samples = resampleBySpeed(samples, request.Speed)
	}
	return SpeechResult{Samples: samples, Rate: rate}, nil
}

// voiceEmbedding reads the chosen voice file, empty when the model has none.
func (m *speechModel) voiceEmbedding(voice string) ([]float32, error) {
	entry := m.entry
	if len(entry.Voices) == 0 {
		return nil, nil
	}
	chosen := entry.Voices[0]
	for _, candidate := range entry.Voices {
		if candidate.ID == voice {
			chosen = candidate
			break
		}
	}
	if chosen.File == "" {
		return nil, nil
	}
	path, err := Paths{}.Inside(m.files.Model, chosen.File)
	if err != nil {
		return nil, err
	}
	return voiceEmbedding(path)
}

// transcribe turns samples into text.
func (m *speechModel) transcribe(ctx context.Context, request TranscribeRequest) (Transcription, error) {
	if m.whisper == nil {
		return Transcription{}, fmt.Errorf("This local model does not transcribe.")
	}
	result, err := m.whisper.Transcribe(ctx, request.Samples, request.Language, request.Translate)
	if err != nil {
		return Transcription{}, err
	}
	if result.Language == "" {
		result.Language = request.Language
	}
	result.Duration = float64(len(request.Samples)) / whisperSampleRate
	return result, nil
}

// isMultiModelVoice reports a voice whose repository holds several graphs
// (Supertonic: a text encoder, a duration predictor and a vocoder). Only the
// single-graph VITS voices are supported, so this one is refused by name
// instead of failing obscurely.
func isMultiModelVoice(dir string) bool {
	entries, err := os.ReadDir(filepath.Join(dir, "onnx"))
	if err != nil {
		return false
	}
	count := 0
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".onnx" {
			count++
		}
	}
	return count > 1
}

// resampleBySpeed shortens or lengthens a recording without changing its pitch
// class: a plain linear interpolation, which is what a speed multiplier does to
// the sample count.
func resampleBySpeed(samples []float32, speed float64) []float32 {
	if speed <= 0 || len(samples) == 0 {
		return samples
	}
	count := int(float64(len(samples)) / speed)
	if count <= 1 {
		return samples
	}
	out := make([]float32, count)
	for index := 0; index < count; index++ {
		position := float64(index) * speed
		left := int(position)
		if left >= len(samples)-1 {
			out[index] = samples[len(samples)-1]
			continue
		}
		fraction := float32(position - float64(left))
		out[index] = samples[left]*(1-fraction) + samples[left+1]*fraction
	}
	return out
}

// inProcess is a model that runs inside NeoRouter: it has no pid, so the memory
// sampler measures the machine rather than the model.
type inProcess struct {
	done   chan struct{}
	once   sync.Once
	onStop func()
}

func newInProcess() *inProcess { return &inProcess{done: make(chan struct{})} }

// PID is 0: the model runs in NeoRouter's own process.
func (p *inProcess) PID() int { return 0 }

// Done is closed once the model has stopped.
func (p *inProcess) Done() <-chan struct{} { return p.done }

// Exit is a clean exit.
func (p *inProcess) Exit() Exit {
	code := 0
	return Exit{Code: &code}
}

// Stop releases the model's graphs.
func (p *inProcess) Stop() {
	p.once.Do(func() {
		if p.onStop != nil {
			p.onStop()
		}
		close(p.done)
	})
}

// Output is empty: there is no child process to print anything.
func (p *inProcess) Output() string { return "" }

// Ready does nothing: there is no output to forget.
func (p *inProcess) Ready() {}
