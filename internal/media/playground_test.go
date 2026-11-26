package media

import (
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/dbx"
	"github.com/NeoLabs-Systems/neorouter/internal/catalog"
	"github.com/NeoLabs-Systems/neorouter/internal/config"
	"github.com/NeoLabs-Systems/neorouter/internal/providers"
)

// The playground's media modes (server/routes/playground_media.js): the same
// handlers as /v1, reached with one of the person's stored keys and base64 audio.

// play sends one playground request with the test key.
func (e *testEnv) play(t *testing.T, server *httptest.Server, kind string, body map[string]any) (*http.Response, []byte) {
	t.Helper()
	payload := map[string]any{"keyId": e.key.ID}
	for name, value := range body {
		payload[name] = value
	}
	return e.doJSON(t, server, "POST", "/api/v1/playground/media/"+kind, payload)
}

func TestPlaygroundMediaKinds(t *testing.T) {
	env := newTestEnv(t)
	adapter := newFakeAdapter("any", providers.CapSpeech, providers.CapGenerateImage, providers.CapTranscribe)
	adapter.speechFn = func(request providers.SpeechRequest) (*providers.SpeechResult, error) {
		return &providers.SpeechResult{Audio: []byte("audio-bytes"), Mime: "audio/mpeg"}, nil
	}
	adapter.imageFn = func(request providers.ImageRequest) (*providers.ImageResult, error) {
		return &providers.ImageResult{Images: []providers.Image{{B64: base64.StdEncoding.EncodeToString([]byte("png")), Mime: "image/png"}}}, nil
	}
	adapter.transcribeFn = func(request providers.TranscribeRequest) (*providers.Transcript, error) {
		if string(request.File) != "audio!" {
			t.Fatalf("uploaded audio = %q", request.File)
		}
		if request.Filename != "x.wav" {
			t.Fatalf("filename = %q", request.Filename)
		}
		return &providers.Transcript{Text: "hello world", Language: "en", Duration: floatPtr(1.5)}, nil
	}
	env.useAdapter("account-1", adapter)
	server := env.server(t)

	response, payload := env.play(t, server, "speech", map[string]any{"input": "hi", "freeOnly": true})
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body %s", response.StatusCode, payload)
	}
	speech := decodeBody(t, payload)
	if speech["audio"] != base64.StdEncoding.EncodeToString([]byte("audio-bytes")) {
		t.Fatalf("speech = %s", payload)
	}
	if speech["mime"] != "audio/mpeg" || speech["format"] != "mp3" || speech["model"] != "mock/any" {
		t.Fatalf("speech = %s", payload)
	}
	if options := env.runner.last(); !options.FreeOnly {
		t.Fatal("the playground's free toggle reaches the pipeline")
	}

	response, payload = env.play(t, server, "transcription", map[string]any{
		"audio": base64.StdEncoding.EncodeToString([]byte("audio!")), "filename": "x.wav",
	})
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body %s", response.StatusCode, payload)
	}
	if string(payload) != `{"text":"hello world","language":"en","duration":1.5,"model":"mock/any"}` {
		t.Fatalf("transcription = %s", payload)
	}

	response, payload = env.play(t, server, "image", map[string]any{"prompt": "a cat"})
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body %s", response.StatusCode, payload)
	}
	data, _ := decodeBody(t, payload)["data"].([]any)
	if len(data) != 1 || data[0].(map[string]any)["b64_json"] == nil {
		t.Fatalf("images default to base64 in the playground: %s", payload)
	}

	response, payload = env.play(t, server, "nope", map[string]any{})
	if response.StatusCode != http.StatusNotFound || !strings.Contains(errorMessage(t, payload), `Unknown kind "nope".`) {
		t.Fatalf("status = %d, body %s", response.StatusCode, payload)
	}

	// A transcription without audio is refused before any provider is called.
	response, payload = env.play(t, server, "transcription", map[string]any{})
	if response.StatusCode != http.StatusBadRequest || !strings.Contains(errorMessage(t, payload), "audio: base64 audio is required.") {
		t.Fatalf("status = %d, body %s", response.StatusCode, payload)
	}
}

func TestPlaygroundDispatchesEveryKind(t *testing.T) {
	env := newTestEnv(t)
	adapter := newFakeAdapter("llama-guard-ok", providers.CapEmbed, providers.CapGenerateImage, providers.CapSpeech,
		providers.CapTranscribe, providers.CapChat, providers.CapCreateVideo, providers.CapSearch, providers.CapWeather)
	adapter.embedFn = func(providers.EmbedRequest) (*providers.EmbedResult, error) {
		return &providers.EmbedResult{Vectors: [][]float64{{1}}}, nil
	}
	adapter.imageFn = func(providers.ImageRequest) (*providers.ImageResult, error) {
		return &providers.ImageResult{Images: []providers.Image{{B64: "cG5n", Mime: "image/png"}}}, nil
	}
	adapter.speechFn = func(providers.SpeechRequest) (*providers.SpeechResult, error) {
		return &providers.SpeechResult{Audio: []byte("mp3"), Mime: "audio/mpeg"}, nil
	}
	adapter.transcribeFn = func(providers.TranscribeRequest) (*providers.Transcript, error) {
		return &providers.Transcript{Text: "hello"}, nil
	}
	adapter.chatFn = func(json.RawMessage) (*providers.ChatResult, error) {
		return &providers.ChatResult{Body: []byte(`{"choices":[{"message":{"content":"safe"}}]}`)}, nil
	}
	adapter.videoFn = func(providers.VideoRequest) (*providers.VideoJob, error) {
		return &providers.VideoJob{UpstreamID: "upstream-1", Status: "queued"}, nil
	}
	adapter.searchFn = func(providers.SearchRequest) (*providers.SearchResult, error) {
		return &providers.SearchResult{Results: []providers.SearchHit{{Title: "t", URL: "u"}}}, nil
	}
	adapter.weatherFn = func(providers.WeatherRequest) (*providers.WeatherReport, error) {
		return &providers.WeatherReport{Location: map[string]any{"name": "Paris"}}, nil
	}
	env.useAdapter("account-1", adapter)
	decider := newFakeAdapter("jev-1.13", providers.CapDecide)
	decider.decision = true
	decider.decideFn = func(providers.DecideRequest) (*providers.DecideResult, error) {
		return &providers.DecideResult{Answers: map[string]any{"ok": true}}, nil
	}
	env.useAdapter("account-2", decider)
	server := env.server(t)

	cases := []struct {
		kind string
		body map[string]any
	}{
		{"embedding", map[string]any{"input": "x"}},
		{"image", map[string]any{"prompt": "a cat"}},
		{"speech", map[string]any{"input": "hi"}},
		{"moderation", map[string]any{"model": "mock/llama-guard-ok", "input": "hi"}},
		{"decision", map[string]any{"state": map[string]any{"a": 1}, "questions": map[string]any{"ok": map[string]any{"type": "boolean"}}}},
		{"search", map[string]any{"query": "x"}},
		{"weather", map[string]any{"location": "Paris"}},
		{"video", map[string]any{"prompt": "waves"}},
		{"transcription", map[string]any{"audio": base64.StdEncoding.EncodeToString([]byte("audio"))}},
	}
	for _, testCase := range cases {
		t.Run(testCase.kind, func(t *testing.T) {
			response, payload := env.play(t, server, testCase.kind, testCase.body)
			if response.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, body %s", response.StatusCode, payload)
			}
		})
	}
}

func TestPlaygroundKeyEnvelope(t *testing.T) {
	env := newTestEnv(t)
	env.useAdapter("account-1", newFakeAdapter("any", providers.CapSpeech))
	server := env.server(t)

	response, payload := env.doJSON(t, server, "POST", "/api/v1/playground/media/speech", map[string]any{"input": "hi"})
	if response.StatusCode != http.StatusBadRequest || errorMessage(t, payload) != "keyId is required." {
		t.Fatalf("status = %d, body %s", response.StatusCode, payload)
	}
	response, payload = env.doJSON(t, server, "POST", "/api/v1/playground/media/speech", map[string]any{"keyId": "nope", "input": "hi"})
	if response.StatusCode != http.StatusNotFound || errorCode(t, payload) != "KEY_NOT_FOUND" {
		t.Fatalf("status = %d, body %s", response.StatusCode, payload)
	}
	if _, err := env.db.W.Exec(`UPDATE router_keys SET revoked_at = '2026-01-01T00:00:00.000Z' WHERE id = ?`, env.key.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	response, payload = env.doJSON(t, server, "POST", "/api/v1/playground/media/speech", map[string]any{"keyId": env.key.ID, "input": "hi"})
	if response.StatusCode != http.StatusBadRequest || errorCode(t, payload) != "KEY_REVOKED" {
		t.Fatalf("status = %d, body %s", response.StatusCode, payload)
	}
}

func TestPlaygroundVoices(t *testing.T) {
	env := newTestEnv(t)
	adapter := newFakeAdapter("tts-ok", providers.CapSpeech, providers.CapVoices)
	env.useAdapter("account-1", adapter)
	server := env.server(t)

	response, payload := env.do(t, server, "GET", "/api/v1/playground/voices?model=auto", nil, "")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body %s", response.StatusCode, payload)
	}
	body := decodeBody(t, payload)
	voices, _ := body["voices"].([]any)
	if len(voices) != len(openAIVoices) {
		t.Fatalf("voices = %s", payload)
	}
	if voices[0].(map[string]any)["id"] != "alloy" || voices[0].(map[string]any)["name"] != "Alloy" {
		t.Fatalf("first voice = %+v", voices[0])
	}

	// A named model asks its provider, and the catalog supplies the entry.
	env.models["speech"] = []catalog.Entry{{ID: "mock/tts-ok", ModelID: "tts-ok", AccountID: "account-1", Capability: "speech", Enabled: true, Available: true}}
	adapter.voicesFn = func(modelID string) ([]providers.Voice, error) {
		return []providers.Voice{{ID: "en-US-1", Name: "English"}}, nil
	}
	response, payload = env.do(t, server, "GET", "/api/v1/playground/voices?model=mock/tts-ok", nil, "")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body %s", response.StatusCode, payload)
	}
	voices, _ = decodeBody(t, payload)["voices"].([]any)
	if len(voices) != 1 || voices[0].(map[string]any)["id"] != "en-US-1" {
		t.Fatalf("voices = %s", payload)
	}

	// A provider that cannot list them leaves OpenAI's names in place.
	adapter.voicesFn = func(string) ([]providers.Voice, error) { return nil, http.ErrHandlerTimeout }
	_, payload = env.do(t, server, "GET", "/api/v1/playground/voices?model=mock/tts-ok", nil, "")
	voices, _ = decodeBody(t, payload)["voices"].([]any)
	if len(voices) != len(openAIVoices) {
		t.Fatalf("voices = %s", payload)
	}
}

func TestPlaygroundVideos(t *testing.T) {
	env := newTestEnv(t)
	env.videoAdapter("account-1")
	server := env.server(t)
	job, err := env.svc.createVideoJob(t.Context(), env.key, "", env.videoEntry("account-1"),
		&providers.VideoJob{UpstreamID: "upstream-1", Status: "completed"}, videoRequest{Prompt: stringPtr("waves"), Seconds: floatPtr(4)})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	response, payload := env.do(t, server, "GET", "/api/v1/playground/videos/"+job.ID, nil, "")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body %s", response.StatusCode, payload)
	}
	if decodeBody(t, payload)["id"] != job.ID {
		t.Fatalf("body = %s", payload)
	}
	response, payload = env.do(t, server, "GET", "/api/v1/playground/videos/"+job.ID+"/content", nil, "")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body %s", response.StatusCode, payload)
	}
	body := decodeBody(t, payload)
	if body["video"] != base64.StdEncoding.EncodeToString([]byte("mp4data")) || body["mime"] != "video/mp4" {
		t.Fatalf("content = %s", payload)
	}
}

func TestPlaygroundRoutesSitBehindTheirPermission(t *testing.T) {
	env := newTestEnv(t)
	refused := false
	env.svc.require = func(permission string) func(http.Handler) http.Handler {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if permission != "playground.use" {
					t.Errorf("permission = %q", permission)
				}
				refused = true
				w.WriteHeader(http.StatusForbidden)
			})
		}
	}
	server := env.server(t)
	response, _ := env.do(t, server, "GET", "/api/v1/playground/voices?model=auto", nil, "")
	if response.StatusCode != http.StatusForbidden || !refused {
		t.Fatalf("status = %d, refused = %v", response.StatusCode, refused)
	}
	// The gateway routes are not behind the playground permission.
	env.useAdapter("account-1", newFakeAdapter("small", providers.CapEmbed))
	response, _ = env.do(t, server, "GET", "/v1/embeddings/pin", nil, "")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", response.StatusCode)
	}
}

// newBareService is a service with no dependencies wired, to prove the mounts
// answer instead of panicking.
func newBareService(t *testing.T) *Service {
	t.Helper()
	db, err := dbx.Open(t.Context(), t.TempDir()+"/neorouter.sqlite3", dbx.Options{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return New(Options{DB: db, Log: slog.New(slog.DiscardHandler), Now: time.Now,
		Config: func() config.Config { return config.Config{} }})
}

func TestUnwiredRoutesAnswer(t *testing.T) {
	service := newBareService(t)
	mux := http.NewServeMux()
	service.Routes(mux)
	service.PlaygroundRoutes(mux)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	response, err := http.Post(server.URL+"/v1/embeddings", "application/json", strings.NewReader(`{"input":"x"}`))
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want the router-key refusal", response.StatusCode)
	}
}
