package localmodels

// The engine wiring and the small pieces around it: path safety, the llama.cpp
// build bookkeeping, the installer's progress, the boot sequence, and the
// native speech engine end to end with stand-in graphs.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestInsideRefusesPathsOutsideTheModelDirectory(t *testing.T) {
	paths := NewPaths(t.TempDir())
	dir := paths.ModelDir("whisper-base")
	inside, err := paths.Inside(dir, "onnx/encoder_model_quantized.onnx")
	if err != nil {
		t.Fatalf("inside: %v", err)
	}
	if inside != filepath.Join(dir, "onnx", "encoder_model_quantized.onnx") {
		t.Fatalf("inside = %s", inside)
	}
	for _, relative := range []string{"../escape", "/etc/passwd", "onnx/../../escape"} {
		if _, err := paths.Inside(dir, relative); err == nil {
			t.Fatalf("%q was accepted", relative)
		} else if !strings.Contains(err.Error(), "Refusing a path outside") {
			t.Fatalf("%q error = %v", relative, err)
		}
	}
}

func TestLlamaRuntimeMarksBrokenBuildsAndReportsWhatIsInstalled(t *testing.T) {
	paths := NewPaths(t.TempDir())
	if err := paths.EnsureDirs(); err != nil {
		t.Fatalf("ensure dirs: %v", err)
	}
	runtime := NewLlamaRuntime(paths)
	machine := testMachine()

	if support := llamaCppSupport(machine); !support.OK {
		t.Fatalf("linux x64 is not supported: %+v", support)
	}
	windows := testMachine(func(m *Machine) { m.Platform = "win32" })
	if support := llamaCppSupport(windows); support.OK || support.Reason == nil || *support.Reason != "unsupported_system" {
		t.Fatalf("windows support = %+v", support)
	}
	status := runtime.Status(machine)
	if status.Build != LlamaCppBuild || status.Installed != nil || status.GPU {
		t.Fatalf("status = %+v", status)
	}
	// Installing a build and marking it broken are the two states the page shows.
	variantDir := filepath.Join(paths.RuntimesDir, "llama.cpp", LlamaCppBuild+"-ubuntu-x64")
	if err := os.MkdirAll(variantDir, 0o700); err != nil {
		t.Fatalf("create variant dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(variantDir, LlamaCppBinaryName), nil, 0o700); err != nil {
		t.Fatalf("create binary: %v", err)
	}
	status = runtime.Status(machine)
	if status.Installed == nil || *status.Installed != "ubuntu-x64" {
		t.Fatalf("installed = %v", status.Installed)
	}
	runtime.MarkBroken("ubuntu-x64")
	status = runtime.Status(machine)
	if status.Installed != nil {
		t.Fatalf("a broken build is still reported installed")
	}
	if len(status.Broken) != 1 || status.Broken[0] != "ubuntu-x64" {
		t.Fatalf("broken = %v", status.Broken)
	}
	if _, err := os.Stat(variantDir); !os.IsNotExist(err) {
		t.Fatalf("a broken build was not removed")
	}
	runtime.ClearBroken()
	if status := runtime.Status(machine); len(status.Broken) != 0 {
		t.Fatalf("broken after clearing = %v", status.Broken)
	}
}

func TestHuggingFaceFileListIsResolvedThroughGlobs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/models/onnx-community/whisper-base/tree/main" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("recursive") != "1" {
			t.Errorf("recursive query missing: %s", r.URL.RawQuery)
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"type": "file", "path": "config.json", "size": 10},
			{"type": "file", "path": "tokenizer.json", "size": 20},
			{"type": "file", "path": "merges.txt", "size": 25},
			{"type": "file", "path": "onnx/encoder_model_quantized.onnx", "size": 30},
			{"type": "file", "path": "onnx/decoder_model_merged_quantized.onnx", "size": 40},
			{"type": "directory", "path": "onnx"},
			{"type": "file", "path": "big.bin", "size": 1, "lfs": map[string]any{"size": 99, "oid": "abc123"}},
		})
	}))
	defer server.Close()

	client := &HFClient{HTTP: server.Client(), Base: server.URL}
	entry := catalogMust(t, "whisper-base")
	files, err := client.Resolve(context.Background(), entry)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	paths := []string{}
	for _, file := range files {
		paths = append(paths, file.Path)
	}
	want := []string{"config.json", "tokenizer.json", "merges.txt", "onnx/encoder_model_quantized.onnx", "onnx/decoder_model_merged_quantized.onnx"}
	if strings.Join(paths, ",") != strings.Join(want, ",") {
		t.Fatalf("files = %v, want %v", paths, want)
	}
	if files[0].URL != server.URL+"/onnx-community/whisper-base/resolve/main/config.json" {
		t.Fatalf("url = %s", files[0].URL)
	}
	if files[0].Size == nil || *files[0].Size != 10 {
		t.Fatalf("size = %v", files[0].Size)
	}

	// A pattern that matches nothing is an error that names it.
	broken := *entry
	broken.Source = Source{Repo: "onnx-community/whisper-base", Include: []string{"missing/*.onnx"}}
	if _, err := client.Resolve(context.Background(), &broken); err == nil {
		t.Fatalf("missing pattern was accepted")
	} else if !strings.Contains(err.Error(), "has no file matching missing/*.onnx") {
		t.Fatalf("error = %v", err)
	}
}

func TestResumeFinishesDownloadsAndStartsWhatTheAdminLeftOn(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	// One model already installed and wanted, one whose download was interrupted.
	if _, err := h.db.W.ExecContext(ctx,
		`INSERT INTO local_models (id, status, enabled) VALUES ('qwen3.5-0.8b', 'installed', 1)`); err != nil {
		t.Fatalf("insert installed row: %v", err)
	}
	if _, err := h.db.W.ExecContext(ctx,
		`INSERT INTO local_models (id, status, enabled) VALUES ('whisper-base', 'downloading', 1)`); err != nil {
		t.Fatalf("insert downloading row: %v", err)
	}
	h.svc.Resume(ctx)
	h.waitFor("qwen3.5-0.8b", StateRunning)
	// The interrupted download finished; the model is installed but not started
	// by resume (it was downloading, not enabled-and-installed).
	for attempt := 0; attempt < 200; attempt++ {
		row, err := h.svc.Store.Get(ctx, "whisper-base")
		if err != nil {
			t.Fatalf("read row: %v", err)
		}
		if row != nil && row.Status == "installed" {
			return
		}
		settleShort()
	}
	t.Fatalf("the interrupted download never finished")
}

func settleShort() { time.Sleep(10 * time.Millisecond) }

// TestTransformersEngineSpeaksAndTranscribes runs the native speech engine with
// stand-in graphs: the same requests and the same answer shapes the
// transformers.js worker produced.
func TestTransformersEngineSpeaksAndTranscribes(t *testing.T) {
	dir := t.TempDir()
	writeByteLevelTokenizer(t, dir, map[string]int64{
		"<|startoftranscript|>": 50258,
		"<|endoftext|>":         50257,
		"<|en|>":                50259,
		"<|transcribe|>":        50359,
		"<|translate|>":         50358,
		"<|notimestamps|>":      50363,
		"<|0.00|>":              50364,
		"<pad>":                 0,
	})
	if err := os.MkdirAll(filepath.Join(dir, "onnx"), 0o700); err != nil {
		t.Fatalf("create onnx dir: %v", err)
	}
	for _, name := range []string{"encoder_model_quantized.onnx", "decoder_model_merged_quantized.onnx"} {
		if err := os.WriteFile(filepath.Join(dir, "onnx", name), nil, 0o600); err != nil {
			t.Fatalf("create graph: %v", err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"model_type":"whisper","num_mel_bins":80}`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	script := []int32{'h', 'i', 50257}
	step := 0
	open := func(path string) (Session, error) {
		if strings.Contains(path, "encoder") {
			return &fakeSession{
				inputs:  []string{"input_features"},
				outputs: []string{"last_hidden_state"},
				run: func(inputs map[string]*Tensor) (map[string]*Tensor, error) {
					return map[string]*Tensor{"last_hidden_state": FloatTensor([]int64{1, 1500, 4}, make([]float32, 6000))}, nil
				},
			}, nil
		}
		return &fakeSession{
			inputs:  []string{"input_ids", "encoder_hidden_states"},
			outputs: []string{"logits"},
			run: func(inputs map[string]*Tensor) (map[string]*Tensor, error) {
				wanted := script[len(script)-1]
				if step < len(script) {
					wanted = script[step]
				}
				step++
				logits := make([]float32, 60000)
				logits[wanted] = 5
				return map[string]*Tensor{"logits": FloatTensor([]int64{1, 1, 60000}, logits)}, nil
			},
		}, nil
	}
	engine := &TransformersEngine{Paths: NewPaths(t.TempDir()), Open: open}
	entry := catalogMust(t, "whisper-base")
	instance, err := engine.Start(context.Background(), StartInput{
		Entry:   entry,
		Files:   ModelFiles{Model: dir},
		Machine: testMachine(),
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	result, err := instance.Invoke(context.Background(), "transcribe", TranscribeRequest{
		Samples:  make([]float32, 16000),
		Language: "en",
	})
	if err != nil {
		t.Fatalf("transcribe: %v", err)
	}
	transcription, ok := result.(Transcription)
	if !ok {
		t.Fatalf("transcribe result = %T", result)
	}
	if transcription.Text != "hi" {
		t.Fatalf("text = %q", transcription.Text)
	}
	if transcription.Duration != 1 || transcription.Language != "en" || len(transcription.Segments) != 1 {
		t.Fatalf("transcription = %+v", transcription)
	}
	if !instance.Health(context.Background()) {
		t.Fatalf("the model reports unhealthy")
	}
	if _, err := instance.Invoke(context.Background(), "ping", nil); err != nil {
		t.Fatalf("ping: %v", err)
	}
	instance.Proc.Stop()
	if instance.Proc.PID() != 0 {
		t.Fatalf("an in-process model reports a pid")
	}
}

func TestVITSSpeechReturnsSamplesAndARate(t *testing.T) {
	dir := t.TempDir()
	writeByteLevelTokenizer(t, dir, map[string]int64{"<pad>": 0})
	if err := os.MkdirAll(filepath.Join(dir, "onnx"), 0o700); err != nil {
		t.Fatalf("create onnx dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "onnx", "model_quantized.onnx"), nil, 0o600); err != nil {
		t.Fatalf("create graph: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"sampling_rate":16000}`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	open := func(string) (Session, error) {
		return &fakeSession{
			inputs:  []string{"input_ids", "attention_mask"},
			outputs: []string{"waveform"},
			run: func(inputs map[string]*Tensor) (map[string]*Tensor, error) {
				ids := inputs["input_ids"]
				count := int(ids.Shape[len(ids.Shape)-1]) * 100
				return map[string]*Tensor{"waveform": FloatTensor([]int64{1, 1, int64(count)}, make([]float32, count))}, nil
			},
		}, nil
	}
	engine := &TransformersEngine{Paths: NewPaths(t.TempDir()), Open: open}
	entry := &Entry{
		ID:           "mms-tts-eng",
		Name:         "MMS English",
		Kind:         KindSpeech,
		Engine:       EngineTransformers,
		Summary:      Text{En: "test", De: "test"},
		Source:       Source{Repo: "Xenova/mms-tts-eng", Include: []string{"onnx/model_quantized.onnx"}},
		DownloadMB:   37,
		Languages:    CodesOf("en"),
		License:      "CC-BY-NC-4.0",
		Transformers: &TransformersTask{Task: "text-to-speech", Dtype: "q8"},
		Voices:       []Voice{{ID: "default", Name: "English"}},
	}
	instance, err := engine.Start(context.Background(), StartInput{Entry: entry, Files: ModelFiles{Model: dir}, Machine: testMachine()})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	result, err := instance.Invoke(context.Background(), "speech", SpeechRequest{Text: "hi there", Voice: "default"})
	if err != nil {
		t.Fatalf("speech: %v", err)
	}
	speech, ok := result.(SpeechResult)
	if !ok {
		t.Fatalf("speech result = %T", result)
	}
	if len(speech.Samples) == 0 || speech.Rate != 16000 {
		t.Fatalf("speech = %d samples at %d Hz", len(speech.Samples), speech.Rate)
	}
	// The WAV the router returns is a real WAV file of those samples.
	wav := ToWav(speech.Samples, speech.Rate)
	if len(wav) <= 44 || string(wav[0:4]) != "RIFF" {
		t.Fatalf("wav header = %q", wav[0:4])
	}
	samples, ok := ToSamples(wav)
	if !ok || len(samples) == 0 {
		t.Fatalf("the WAV could not be read back")
	}
}

// TestTheEngineRefusesAModelItCannotRun keeps an unsupported model a clear error
// instead of a half-started one.
func TestTheEngineRefusesAModelItCannotRun(t *testing.T) {
	dir := t.TempDir()
	writeByteLevelTokenizer(t, dir, nil)
	if err := os.MkdirAll(filepath.Join(dir, "onnx"), 0o700); err != nil {
		t.Fatalf("create onnx dir: %v", err)
	}
	for _, name := range []string{"text_encoder.onnx", "vocoder.onnx"} {
		if err := os.WriteFile(filepath.Join(dir, "onnx", name), nil, 0o600); err != nil {
			t.Fatalf("create graph: %v", err)
		}
	}
	engine := &TransformersEngine{Paths: NewPaths(t.TempDir()), Open: func(string) (Session, error) {
		return nil, fmt.Errorf("should not load")
	}}
	entry := catalogMust(t, "supertonic-tts")
	_, err := engine.Start(context.Background(), StartInput{Entry: entry, Files: ModelFiles{Model: dir}, Machine: testMachine()})
	if err == nil {
		t.Fatalf("a multi-graph voice started")
	}
	var startErr *StartError
	if !asStartError(err, &startErr) || !strings.Contains(startErr.Message, "not supported by this build") {
		t.Fatalf("error = %v", err)
	}
}
