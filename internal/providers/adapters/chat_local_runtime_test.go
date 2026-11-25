package adapters

import (
	"context"
	"encoding/base64"
	"net/http"
	"testing"

	"github.com/NeoLabs-Systems/neorouter/internal/providers"
)

// The local runtime, ported from providers/adapters/local_runtime.js: a model
// that is not running is a temporary failure, an unshared server is refused, and
// the running llama.cpp server answers the chat call.

// fakeLocalModels stands in for services/local_models.
type fakeLocalModels struct {
	allowed   bool
	served    []LocalModel
	instances map[string]LocalInstance
	decided   *LocalSpeechOutput
	spoken    *LocalSpeechInput
	read      *LocalTranscribeInput
}

func (f *fakeLocalModels) Allowed(ctx context.Context, userID string) bool { return f.allowed }

func (f *fakeLocalModels) Served() []LocalModel { return f.served }

func (f *fakeLocalModels) Running(modelID string) (LocalInstance, bool) {
	instance, ok := f.instances[modelID]
	return instance, ok
}

func (f *fakeLocalModels) speech(ctx context.Context, in LocalSpeechInput) (LocalSpeechOutput, error) {
	f.spoken = &in
	if f.decided != nil {
		return *f.decided, nil
	}
	return LocalSpeechOutput{Samples: []float32{0, 0.5, -0.5}, Rate: 24000}, nil
}

func (f *fakeLocalModels) transcribe(ctx context.Context, in LocalTranscribeInput) (LocalTranscribeOutput, error) {
	f.read = &in
	duration := 1.5
	return LocalTranscribeOutput{Text: "hello", Language: "en", Duration: &duration}, nil
}

// localAdapter builds the dialect for one fake service.
func localAdapter(t *testing.T, service LocalModels) *localRuntime {
	t.Helper()
	return newLocalRuntime(chatDefinition(t, "neorouter_local"), chatDeps{userID: "user-1"}.deps(), service)
}

func TestLocalRuntimeListsItsFreeModels(t *testing.T) {
	service := &fakeLocalModels{
		allowed: true,
		served: []LocalModel{
			{ID: "qwen3-8b", Name: "Qwen3 8B", Kind: providers.KindChat, ContextWindow: 32768, Tools: true, Reasoning: true},
			{ID: "whisper-small", Kind: providers.KindTranscription, Translate: true},
		},
	}
	adapter := localAdapter(t, service)
	models, err := adapter.ListModels(context.Background())
	if err != nil {
		t.Fatalf("list models: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("models = %v", models)
	}
	qwen := models[0]
	if qwen.ID != "qwen3-8b" || qwen.Name != "Qwen3 8B" {
		t.Errorf("model = %+v", qwen)
	}
	if qwen.ContextWindow == nil || *qwen.ContextWindow != 32768 {
		t.Errorf("context window = %v", qwen.ContextWindow)
	}
	if qwen.PriceKnown != true || qwen.FreeHint != true || qwen.PriceIn == nil || *qwen.PriceIn != 0 {
		t.Errorf("local models are free: %+v", qwen)
	}
	if qwen.Kind != providers.KindChat || qwen.Tools == nil || !*qwen.Tools {
		t.Errorf("model = %+v", qwen)
	}
	if models[1].Kind != providers.KindTranscription {
		t.Errorf("kind = %q", models[1].Kind)
	}

	// A person the admin has not shared the server with sees nothing.
	service.allowed = false
	if models, err := adapter.ListModels(context.Background()); err != nil || len(models) != 0 {
		t.Fatalf("models = %v, err = %v", models, err)
	}
}

func TestLocalRuntimeRefusesAnUnsharedServer(t *testing.T) {
	adapter := localAdapter(t, &fakeLocalModels{allowed: false})
	_, err := adapter.Chat(context.Background(), chatRequest(t, `{"messages":[]}`), providers.Call{ModelID: "qwen3-8b"})
	if providers.KindOf(err) != providers.KindAuth {
		t.Fatalf("kind = %q: %v", providers.KindOf(err), err)
	}
	if err.Error() != "An admin has not shared this server’s local models with you." {
		t.Errorf("message = %q", err.Error())
	}
	if classified := providers.AsProviderError(err); classified == nil || !classified.NeedsAction {
		t.Error("only a person can share the models, so the failure needs one")
	}
	verdict, err := adapter.Validate(context.Background())
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if verdict.Status != providers.ValidationInvalid {
		t.Errorf("status = %q", verdict.Status)
	}
}

func TestLocalRuntimeModelNotRunningIsTemporary(t *testing.T) {
	adapter := localAdapter(t, &fakeLocalModels{allowed: true, instances: map[string]LocalInstance{}})
	_, err := adapter.Chat(context.Background(), chatRequest(t, `{"messages":[]}`), providers.Call{ModelID: "qwen3-8b"})
	if providers.KindOf(err) != providers.KindServer {
		t.Fatalf("kind = %q: %v", providers.KindOf(err), err)
	}
	if err.Error() != "This local model is not running right now." {
		t.Errorf("message = %q", err.Error())
	}
	classified := providers.AsProviderError(err)
	if classified == nil || !classified.RetryAfterKnown || classified.RetryAfter != localNotRunningRetry {
		t.Errorf("the caller must be told to try again in 30s: %+v", classified)
	}
}

func TestLocalRuntimeChatReachesTheRunningServer(t *testing.T) {
	upstream := newChatProvider(t, func(w http.ResponseWriter, r *http.Request, call chatCall) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer local-token" {
			t.Errorf("authorization = %q", got)
		}
		chatJSON(w, http.StatusOK, `{"id":"c1","object":"chat.completion","created":1,"model":"qwen3-8b","choices":[{"index":0,"message":{"role":"assistant","content":"local hi"},"finish_reason":"stop"}]}`)
	})
	service := &fakeLocalModels{
		allowed: true,
		instances: map[string]LocalInstance{
			"qwen3-8b": {Endpoint: &LocalEndpoint{BaseURL: upstream.url() + "/v1", Token: "local-token"}},
		},
	}
	adapter := localAdapter(t, service)
	request := chatRequest(t, `{"messages":[{"role":"user","content":"hi"}]}`)
	result, err := adapter.Chat(context.Background(), request, providers.Call{ModelID: "qwen3-8b", SessionID: "s-1"})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	message := chatPath(t, decodedChatResult(t, result.Body), "choices").([]any)[0].(map[string]any)["message"].(map[string]any)
	if message["content"] != "local hi" {
		t.Errorf("content = %v", message["content"])
	}
	if upstream.last(t).body["model"] != "qwen3-8b" {
		t.Errorf("model = %v", upstream.last(t).body["model"])
	}

	// A worker model answers no chat request.
	service.instances["whisper-small"] = LocalInstance{}
	if _, err := adapter.Chat(context.Background(), request, providers.Call{ModelID: "whisper-small"}); providers.KindOf(err) != providers.KindBadRequest {
		t.Fatalf("kind = %q: %v", providers.KindOf(err), err)
	}
}

func TestLocalRuntimeEmbed(t *testing.T) {
	upstream := newChatProvider(t, func(w http.ResponseWriter, r *http.Request, call chatCall) {
		if r.URL.Path != "/v1/embeddings" {
			t.Errorf("path = %q", r.URL.Path)
		}
		chatJSON(w, http.StatusOK, `{"data":[{"index":1,"embedding":[0.3]},{"index":0,"embedding":[0.1]}],"usage":{"prompt_tokens":2}}`)
	})
	service := &fakeLocalModels{
		allowed: true,
		instances: map[string]LocalInstance{
			"bge-m3": {Endpoint: &LocalEndpoint{BaseURL: upstream.url() + "/v1"}},
		},
	}
	adapter := localAdapter(t, service)
	result, err := adapter.Embed(context.Background(), providers.EmbedRequest{Input: []string{"a", "b"}}, providers.Call{ModelID: "bge-m3"})
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	// The rows come back in the order the inputs were sent.
	if len(result.Vectors) != 2 || result.Vectors[0][0] != 0.1 || result.Vectors[1][0] != 0.3 {
		t.Fatalf("vectors = %v", result.Vectors)
	}
	if result.InputTokens == nil || *result.InputTokens != 2 {
		t.Errorf("input tokens = %v", result.InputTokens)
	}
}

func TestLocalRuntimeSpeechIsWAV(t *testing.T) {
	service := &fakeLocalModels{
		allowed: true,
		served:  []LocalModel{{ID: "kokoro", Kind: providers.KindSpeech, Voices: []providers.Voice{{ID: "af_bella"}, {ID: "am_adam"}}}},
	}
	service.instances = map[string]LocalInstance{
		"kokoro": {Speech: service.speech},
	}
	adapter := localAdapter(t, service)
	result, err := adapter.Speech(context.Background(), providers.SpeechRequest{Input: "hi", Voice: "am_adam"}, providers.Call{ModelID: "kokoro"})
	if err != nil {
		t.Fatalf("speech: %v", err)
	}
	if result.Mime != "audio/wav" || string(result.Audio[0:4]) != "RIFF" || string(result.Audio[8:12]) != "WAVE" {
		t.Fatalf("audio = %d bytes, mime = %q", len(result.Audio), result.Mime)
	}
	// Three samples of 16 bits, plus the header.
	if len(result.Audio) != 44+6 {
		t.Errorf("audio = %d bytes", len(result.Audio))
	}
	if service.spoken == nil || service.spoken.Voice != "am_adam" || service.spoken.Text != "hi" {
		t.Errorf("the worker was asked for %+v", service.spoken)
	}
	// A voice the model does not have falls back to its first.
	if _, err := adapter.Speech(context.Background(), providers.SpeechRequest{Input: "hi", Voice: "unknown"}, providers.Call{ModelID: "kokoro"}); err != nil {
		t.Fatalf("speech: %v", err)
	}
	if service.spoken.Voice != "af_bella" {
		t.Errorf("voice = %q, want the model's first", service.spoken.Voice)
	}

	// A model that only transcribes answers no speech request.
	service.instances["whisper"] = LocalInstance{}
	if _, err := adapter.Speech(context.Background(), providers.SpeechRequest{Input: "hi"}, providers.Call{ModelID: "whisper"}); providers.KindOf(err) != providers.KindBadRequest {
		t.Fatalf("kind = %q: %v", providers.KindOf(err), err)
	}
}

func TestLocalRuntimeTranscribe(t *testing.T) {
	service := &fakeLocalModels{allowed: true}
	upstreamTranscript := base64.StdEncoding.EncodeToString([]byte("x"))
	_ = upstreamTranscript
	instance := LocalInstance{
		Transcribe: func(ctx context.Context, in LocalTranscribeInput) (LocalTranscribeOutput, error) {
			service.read = &in
			duration := 2.5
			return LocalTranscribeOutput{Text: "hello there", Language: "en", Duration: &duration}, nil
		},
	}
	service.instances = map[string]LocalInstance{"whisper-small": instance}
	service.served = []LocalModel{{ID: "whisper-small", Kind: providers.KindTranscription, Translate: true}}
	adapter := localAdapter(t, service)
	transcript, err := adapter.Transcribe(context.Background(), providers.TranscribeRequest{File: []byte("wav"), Mime: "audio/wav", Language: "de", Translate: true}, providers.Call{ModelID: "whisper-small"})
	if err != nil {
		t.Fatalf("transcribe: %v", err)
	}
	if transcript.Text != "hello there" || transcript.Language != "en" {
		t.Errorf("transcript = %+v", transcript)
	}
	if transcript.Duration == nil || *transcript.Duration != 2.5 {
		t.Errorf("duration = %v", transcript.Duration)
	}
	if service.read == nil || string(service.read.File) != "wav" || service.read.Language != "de" || !service.read.Translate {
		t.Errorf("the worker was asked for %+v", service.read)
	}
	if !adapter.CanTranslate("whisper-small") {
		t.Error("Whisper translates")
	}
	if adapter.CanTranslate("another-model") {
		t.Error("an unknown model claims nothing")
	}
}

func TestLocalRuntimeValidateReportsWhatRuns(t *testing.T) {
	none := localAdapter(t, &fakeLocalModels{allowed: true})
	// A connection being created has no owner yet.
	none.account.UserID = ""
	verdict, err := none.Validate(context.Background())
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if verdict.Message == nil || *verdict.Message != "No local model is running right now." {
		t.Errorf("message = %v", verdict.Message)
	}

	one := localAdapter(t, &fakeLocalModels{allowed: true, served: []LocalModel{{ID: "a"}}})
	verdict, _ = one.Validate(context.Background())
	if verdict.Message == nil || *verdict.Message != "1 local model running." {
		t.Errorf("message = %v", verdict.Message)
	}

	two := localAdapter(t, &fakeLocalModels{allowed: true, served: []LocalModel{{ID: "a"}, {ID: "b"}}})
	verdict, _ = two.Validate(context.Background())
	if verdict.Message == nil || *verdict.Message != "2 local models running." {
		t.Errorf("message = %v", verdict.Message)
	}
}

func TestLocalRuntimeCapabilities(t *testing.T) {
	adapter := localAdapter(t, &fakeLocalModels{allowed: true})
	for _, capability := range []providers.Capability{providers.CapChat, providers.CapEmbed, providers.CapSpeech, providers.CapTranscribe} {
		if !adapter.Supports(capability) {
			t.Errorf("%s should be supported", capability)
		}
	}
	if adapter.Supports(providers.CapGenerateImage) {
		t.Error("a local runtime makes no images")
	}
	if _, ok := any(adapter).(providers.Translator); !ok {
		t.Error("a local runtime decides translation per model")
	}
}
