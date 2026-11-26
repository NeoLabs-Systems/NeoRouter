package media

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"
	"time"

	"github.com/NeoLabs-Systems/neorouter/internal/catalog"
	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
	"github.com/NeoLabs-Systems/neorouter/internal/providers"
)

// upload sends one multipart audio upload.
func (e *testEnv) upload(t *testing.T, server *httptest.Server, path string, fields map[string]string, filename string, data []byte, mime string) (*http.Response, []byte) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	names := make([]string, 0, len(fields))
	for name := range fields {
		names = append(names, name)
	}
	for _, name := range names {
		if err := writer.WriteField(name, fields[name]); err != nil {
			t.Fatalf("write field: %v", err)
		}
	}
	if filename != "" {
		header := textproto.MIMEHeader{}
		header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="%s"`, filename))
		if mime != "" {
			header.Set("Content-Type", mime)
		}
		part, err := writer.CreatePart(header)
		if err != nil {
			t.Fatalf("create part: %v", err)
		}
		if _, err := part.Write(data); err != nil {
			t.Fatalf("write part: %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	return e.do(t, server, "POST", path, &body, writer.FormDataContentType())
}

// transcriptionAdapter serves transcripts the tests describe.
func (e *testEnv) transcriptionAdapter(transcript *providers.Transcript) *fakeAdapter {
	adapter := newFakeAdapter("whisper-ok", providers.CapTranscribe)
	adapter.timestamps = true
	adapter.translate = true
	adapter.transcribeFn = func(request providers.TranscribeRequest) (*providers.Transcript, error) {
		return transcript, nil
	}
	return e.useAdapter("account-1", adapter)
}

func TestTranscriptionsReturnText(t *testing.T) {
	env := newTestEnv(t)
	env.transcriptionAdapter(&providers.Transcript{Text: "hello world", Language: "en", Duration: floatPtr(2.5)})
	server := env.server(t)
	audio := make([]byte, 2048)
	response, payload := env.upload(t, server, "/v1/audio/transcriptions", map[string]string{"model": "auto"}, "a.wav", audio, "audio/wav")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body %s", response.StatusCode, payload)
	}
	if string(payload) != `{"text":"hello world"}` {
		t.Fatalf("body = %s", payload)
	}
	options := env.runner.last()
	if options.Capability != "transcription" || options.Method != providers.CapTranscribe || options.Format != "transcription" {
		t.Fatalf("options = %+v", options)
	}
	if options.Units != estimateAudioMinutes(len(audio)) {
		t.Fatalf("units = %v, want the upload's estimated minutes", options.Units)
	}
	if options.ContextTokens == nil || *options.ContextTokens != 0 {
		t.Fatalf("context tokens = %v, want 0 without a prompt", options.ContextTokens)
	}
	outcome := env.runner.lastOutcome()
	if outcome == nil || outcome.Units == nil || *outcome.Units != 2.5/60 {
		t.Fatalf("billed minutes come from the reported duration: %+v", outcome)
	}
}

func TestTranscriptionsBuildSubtitles(t *testing.T) {
	env := newTestEnv(t)
	adapter := env.transcriptionAdapter(&providers.Transcript{Text: "hello world", Duration: floatPtr(1.2)})
	env.models["transcription"] = []catalog.Entry{{ID: "mock/whisper-ok", ModelID: "whisper-ok", AccountID: "account-1", Capability: "transcription", Enabled: true, Available: true}}
	server := env.server(t)
	audio := make([]byte, 64)

	response, payload := env.upload(t, server, "/v1/audio/transcriptions", map[string]string{"response_format": "srt"}, "a.wav", audio, "audio/wav")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body %s", response.StatusCode, payload)
	}
	if !strings.HasPrefix(string(payload), "1\n00:00:00,000 --> 00:00:01,200\nhello world\n") {
		t.Fatalf("srt = %q", payload)
	}
	if got := response.Header.Get("Content-Type"); got != "application/x-subrip; charset=utf-8" {
		t.Fatalf("Content-Type = %q", got)
	}

	response, payload = env.upload(t, server, "/v1/audio/transcriptions", map[string]string{"response_format": "vtt"}, "a.wav", audio, "audio/wav")
	if response.StatusCode != http.StatusOK || !strings.HasPrefix(string(payload), "WEBVTT\n\n00:00:00.000 --> 00:00:01.200\n") {
		t.Fatalf("vtt = %q (%d)", payload, response.StatusCode)
	}
	if got := response.Header.Get("Content-Type"); got != "text/vtt; charset=utf-8" {
		t.Fatalf("Content-Type = %q", got)
	}

	// Segments the provider reported become cues of their own.
	segments := []providers.Segment{{Start: 0, End: 1.2, Text: "hello"}, {Start: 1.2, End: 2, Text: "world"}}
	adapter.transcribeFn = func(providers.TranscribeRequest) (*providers.Transcript, error) {
		return &providers.Transcript{Text: "hello world", Language: "en", Duration: floatPtr(2), Segments: segments}, nil
	}
	response, payload = env.upload(t, server, "/v1/audio/transcriptions", map[string]string{"response_format": "srt"}, "a.wav", audio, "audio/wav")
	if response.StatusCode != http.StatusOK || !strings.Contains(string(payload), "2\n00:00:01,200 --> 00:00:02,000\nworld\n") {
		t.Fatalf("srt = %q (%d)", payload, response.StatusCode)
	}

	// A model that answers timestamps is preferred while one is in the catalog.
	options := env.runner.last()
	if options.Filter == nil {
		t.Fatal("subtitle formats filter for models that report timestamps")
	}
	if options.Filter(catalog.Entry{ModelID: "x"}, adapter) != true {
		t.Fatal("a timestamping transcriber is a candidate")
	}
	plain := newFakeAdapter("plain", providers.CapTranscribe)
	if options.Filter(catalog.Entry{ModelID: "y"}, plain) != false {
		t.Fatal("a transcriber without timestamps is not a candidate for subtitles")
	}
}

func TestTranscriptionsVerboseJSONAndText(t *testing.T) {
	env := newTestEnv(t)
	env.transcriptionAdapter(&providers.Transcript{
		Text: "hello", Language: "en", Duration: floatPtr(1.2),
		Segments: []providers.Segment{{Start: 0, End: 1.2, Text: "hello"}},
	})
	server := env.server(t)
	audio := make([]byte, 64)

	response, payload := env.upload(t, server, "/v1/audio/transcriptions", map[string]string{"response_format": "verbose_json"}, "a.wav", audio, "audio/wav")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body %s", response.StatusCode, payload)
	}
	want := `{"task":"transcribe","language":"en","duration":1.2,"text":"hello","segments":[{"id":0,"start":0,"end":1.2,"text":"hello"}]}`
	if string(payload) != want {
		t.Fatalf("verbose = %s\nwant      %s", payload, want)
	}

	response, payload = env.upload(t, server, "/v1/audio/translations", map[string]string{"response_format": "text"}, "a.wav", audio, "audio/wav")
	if response.StatusCode != http.StatusOK || string(payload) != "hello" {
		t.Fatalf("text = %q (%d)", payload, response.StatusCode)
	}
	if got := response.Header.Get("Content-Type"); got != "text/plain; charset=utf-8" {
		t.Fatalf("Content-Type = %q", got)
	}
	options := env.runner.last()
	if options.Format != "transcription" {
		t.Fatalf("format = %q", options.Format)
	}
	// Translation only goes to models that can translate (not Deepgram or
	// ElevenLabs).
	if options.Filter == nil {
		t.Fatal("a translation filters for models that can translate")
	}
	if !options.Filter(catalog.Entry{ModelID: "whisper-ok"}, env.accounts.adapters["account-1"]) {
		t.Fatal("a translating model is a candidate")
	}
	plain := newFakeAdapter("deepgram", providers.CapTranscribe)
	if options.Filter(catalog.Entry{ModelID: "nova-3"}, plain) {
		t.Fatal("a model that cannot translate is not a candidate")
	}
}

func TestTranscriptionsRefuseAMissingFile(t *testing.T) {
	env := newTestEnv(t)
	env.transcriptionAdapter(&providers.Transcript{Text: "hello"})
	server := env.server(t)
	response, payload := env.upload(t, server, "/v1/audio/transcriptions", map[string]string{"model": "auto"}, "", nil, "")
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, body %s", response.StatusCode, payload)
	}
	if got := errorMessage(t, payload); got != "file: an audio file is required." {
		t.Fatalf("message = %q", got)
	}
	// A body that is not multipart is refused the way express.raw refuses it.
	response, payload = env.do(t, server, "POST", "/v1/audio/transcriptions", strings.NewReader(`{"model":"auto"}`), "application/json")
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, body %s", response.StatusCode, payload)
	}
	if got := errorMessage(t, payload); got != `Send the audio as multipart/form-data with a "file" field.` {
		t.Fatalf("message = %q", got)
	}
}

func TestTranscriptionsRefuseALargeUpload(t *testing.T) {
	env := newTestEnv(t)
	env.transcriptionAdapter(&providers.Transcript{Text: "hello"})
	request := httptest.NewRequest(http.MethodPost, "/v1/audio/transcriptions", nil)
	c := &call{
		w: httptest.NewRecorder(), r: request, key: env.key,
		fields: map[string]any{"model": "auto"},
		file:   &audioFile{data: make([]byte, maxAudioBytes+1), filename: "a.wav"},
	}
	err := env.svc.Transcription(c)
	if err == nil {
		t.Fatal("an upload over 25 MB is refused")
	}
	var httpError *httpserver.HttpError
	if !isHTTPError(err, &httpError) || httpError.Status != http.StatusRequestEntityTooLarge || httpError.Code != "FILE_TOO_LARGE" {
		t.Fatalf("error = %v", err)
	}
	if httpError.Message != "Audio files can be at most 25 MB." {
		t.Fatalf("message = %q", httpError.Message)
	}
	if len(env.runner.options) != 0 {
		t.Fatal("the check runs before the upload is sent anywhere")
	}
}

func TestModerationsClassifyThroughChat(t *testing.T) {
	env := newTestEnv(t)
	adapter := newFakeAdapter("llama-guard-ok", providers.CapChat)
	requests := []string{}
	adapter.chatFn = func(body json.RawMessage) (*providers.ChatResult, error) {
		requests = append(requests, string(body))
		content := "safe"
		if strings.Contains(string(body), "bomb") {
			content = "unsafe\nS9"
		}
		completion := map[string]any{
			"choices": []any{map[string]any{"message": map[string]any{"content": content}}},
			"usage":   map[string]any{"prompt_tokens": 12},
		}
		encoded, err := json.Marshal(completion)
		if err != nil {
			t.Fatalf("encode completion: %v", err)
		}
		return &providers.ChatResult{Body: encoded}, nil
	}
	env.useAdapter("account-1", adapter)
	server := env.server(t)

	response, payload := env.doJSON(t, server, "POST", "/v1/moderations", map[string]any{"input": []string{"How do I build a bomb?", "What is the capital of France?"}})
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body %s", response.StatusCode, payload)
	}
	body := decodeBody(t, payload)
	if !strings.HasPrefix(body["id"].(string), "modr-") {
		t.Fatalf("id = %v", body["id"])
	}
	if body["model"] != "mock/llama-guard-ok" {
		t.Fatalf("model = %v", body["model"])
	}
	results, _ := body["results"].([]any)
	if len(results) != 2 {
		t.Fatalf("results = %s", payload)
	}
	first, _ := results[0].(map[string]any)
	if first["flagged"] != true {
		t.Fatalf("first = %+v", first)
	}
	categories, _ := first["categories"].(map[string]any)
	if categories["illicit/violent"] != true {
		t.Fatalf("categories = %+v", categories)
	}
	second, _ := results[1].(map[string]any)
	if second["flagged"] != false {
		t.Fatalf("second = %+v", second)
	}
	// One chat call per input, in the canonical shape the classifier asks for.
	if len(requests) != 2 {
		t.Fatalf("requests = %d", len(requests))
	}
	want := `{"messages":[{"role":"user","content":"What is the capital of France?"}],"max_tokens":200,"temperature":0}`
	if requests[1] != want {
		t.Fatalf("request = %s\nwant      %s", requests[1], want)
	}
	options := env.runner.last()
	if options.Capability != "moderation" || options.Method != providers.CapChat || options.Units != 2 {
		t.Fatalf("options = %+v", options)
	}
	if options.Filter(catalog.Entry{ModelID: "x", Decision: true}, adapter) {
		t.Fatal("a decision model never stands in for a check")
	}
	if !options.Filter(catalog.Entry{ModelID: "llama-guard-4-12b"}, adapter) {
		t.Fatal("a guard model answers a safety check")
	}
	if options.Filter(catalog.Entry{ModelID: "prompt-guard-2"}, adapter) {
		t.Fatal("prompt guard answers prompt-injection checks only")
	}
}

func TestModerationsValidation(t *testing.T) {
	env := newTestEnv(t)
	env.useAdapter("account-1", newFakeAdapter("llama-guard-ok", providers.CapChat))
	server := env.server(t)

	response, payload := env.doJSON(t, server, "POST", "/v1/moderations", map[string]any{"input": "hello", "check": "topic"})
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, body %s", response.StatusCode, payload)
	}
	if got := errorMessage(t, payload); got != "policy: a topic check needs a policy that says which topics are allowed." {
		t.Fatalf("message = %q", got)
	}
	response, payload = env.doJSON(t, server, "POST", "/v1/moderations", map[string]any{"input": []any{map[string]any{"type": "text"}}})
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, body %s", response.StatusCode, payload)
	}
	if got := errorMessage(t, payload); got != "input: only text inputs are supported." {
		t.Fatalf("message = %q", got)
	}
}

func TestModerationsUseANativeModerationEndpoint(t *testing.T) {
	env := newTestEnv(t)
	adapter := newFakeAdapter("mistral-moderation-latest", providers.CapChat, providers.CapModerate)
	adapter.moderateFn = func(request providers.ModerateRequest) (*providers.ModerationResult, error) {
		if len(request.Input) != 1 || request.Input[0] != "hello" {
			t.Fatalf("inputs = %v", request.Input)
		}
		return &providers.ModerationResult{Results: []any{map[string]any{
			"flagged":         true,
			"categories":      map[string]any{"violence": true},
			"category_scores": map[string]any{"violence": 0.8},
		}}}, nil
	}
	env.useAdapter("account-1", adapter)
	server := env.server(t)
	response, payload := env.doJSON(t, server, "POST", "/v1/moderations", map[string]any{"model": "mock/mistral-moderation-latest", "input": "hello"})
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body %s", response.StatusCode, payload)
	}
	results, _ := decodeBody(t, payload)["results"].([]any)
	first, _ := results[0].(map[string]any)
	if first["flagged"] != true {
		t.Fatalf("results = %s", payload)
	}
	if recorded := adapter.recorded(); len(recorded) != 1 || recorded[0] != "moderate" {
		t.Fatalf("calls = %v, want one native moderation call", recorded)
	}
}

func TestDecisionsAnswerTypedQuestions(t *testing.T) {
	env := newTestEnv(t)
	adapter := newFakeAdapter("jev-1.13", providers.CapDecide)
	adapter.decision = true
	adapter.decideFn = func(request providers.DecideRequest) (*providers.DecideResult, error) {
		questions, _ := request.Questions.(map[string]any)
		if _, ok := questions["reply_needed"]; !ok {
			t.Fatalf("questions = %v", request.Questions)
		}
		tokens := 9
		return &providers.DecideResult{Answers: map[string]any{"reply_needed": true}, Usage: map[string]any{}, InputTokens: &tokens}, nil
	}
	env.useAdapter("account-1", adapter)
	server := env.server(t)
	response, payload := env.doJSON(t, server, "POST", "/v1/systemone", map[string]any{
		"state":     map[string]any{"message": "hi"},
		"questions": map[string]any{"reply_needed": map[string]any{"type": "boolean"}},
	})
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body %s", response.StatusCode, payload)
	}
	if string(payload) != `{"model":"mock/jev-1.13","answers":{"reply_needed":true},"usage":{}}` {
		t.Fatalf("body = %s", payload)
	}
	options := env.runner.last()
	if options.Capability != "moderation" || options.Method != providers.CapDecide || options.Units != 1 {
		t.Fatalf("options = %+v", options)
	}
	if !options.Filter(catalog.Entry{ModelID: "x", Decision: true}, adapter) {
		t.Fatal("a decision model is the only kind /v1/systemone may use")
	}
	if options.Filter(catalog.Entry{ModelID: "x"}, adapter) {
		t.Fatal("a classifier is not a decision model")
	}
	wantTokens := estimateTokens([]string{`{"message":"hi"}`, `{"reply_needed":{"type":"boolean"}}`})
	if options.InputTokens != wantTokens {
		t.Fatalf("tokens = %d, want %d", options.InputTokens, wantTokens)
	}
}

func TestVideoRoutesCreatePollAndServe(t *testing.T) {
	env := newTestEnv(t)
	adapter := env.videoAdapter("account-1", providers.VideoStatus{Status: "in_progress", Progress: floatPtr(50)})
	server := env.server(t)

	response, payload := env.doJSON(t, server, "POST", "/v1/videos", map[string]any{"prompt": "waves", "seconds": 4})
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body %s", response.StatusCode, payload)
	}
	body := decodeBody(t, payload)
	id, _ := body["id"].(string)
	if !strings.HasPrefix(id, "video_") {
		t.Fatalf("id = %v", id)
	}
	if body["object"] != "video" || body["status"] != "queued" || body["model"] != "mock/sora" {
		t.Fatalf("body = %s", payload)
	}
	if body["seconds"] != "4" || body["prompt"] != "waves" || body["error"] != nil {
		t.Fatalf("body = %s", payload)
	}
	if recorded := adapter.recorded(); len(recorded) != 1 || recorded[0] != "createVideo:4:" {
		t.Fatalf("calls = %v", recorded)
	}
	if options := env.runner.last(); options.Capability != "video" || options.Units != 4 {
		t.Fatalf("options = %+v", options)
	}

	// A fresh job is not polled; one past the interval is.
	response, payload = env.do(t, server, "GET", "/v1/videos/"+id, nil, "")
	if response.StatusCode != http.StatusOK || decodeBody(t, payload)["status"] != "queued" {
		t.Fatalf("body = %s (%d)", payload, response.StatusCode)
	}
	env.clock.Advance(5 * time.Second)
	response, payload = env.do(t, server, "GET", "/v1/videos/"+id, nil, "")
	if response.StatusCode != http.StatusOK || decodeBody(t, payload)["status"] != "in_progress" {
		t.Fatalf("body = %s (%d)", payload, response.StatusCode)
	}

	// The finished file is served once the provider reports it.
	response, payload = env.do(t, server, "GET", "/v1/videos/"+id+"/content", nil, "")
	if response.StatusCode != http.StatusConflict || errorCode(t, payload) != "VIDEO_NOT_READY" {
		t.Fatalf("status = %d, body %s", response.StatusCode, payload)
	}
	env.clock.Advance(5 * time.Second)
	response, payload = env.do(t, server, "GET", "/v1/videos/"+id+"/content", nil, "")
	if response.StatusCode != http.StatusOK || string(payload) != "mp4data" {
		t.Fatalf("status = %d, body %q", response.StatusCode, payload)
	}
	if got := response.Header.Get("Content-Type"); got != "video/mp4" {
		t.Fatalf("Content-Type = %q", got)
	}

	response, payload = env.do(t, server, "GET", "/v1/videos", nil, "")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body %s", response.StatusCode, payload)
	}
	list := decodeBody(t, payload)
	if list["object"] != "list" || list["has_more"] != false {
		t.Fatalf("list = %s", payload)
	}
	if jobs, _ := list["data"].([]any); len(jobs) != 1 || jobs[0].(map[string]any)["id"] != id {
		t.Fatalf("list = %s", payload)
	}
}

func TestSearchRoute(t *testing.T) {
	env := newTestEnv(t)
	adapter := newFakeAdapter("brave", providers.CapSearch)
	adapter.searchFn = func(request providers.SearchRequest) (*providers.SearchResult, error) {
		if request.Query != "current time" || request.MaxResults != 5 || request.Recency != "day" {
			t.Fatalf("request = %+v", request)
		}
		published := "2026-01-01"
		return &providers.SearchResult{Results: []providers.SearchHit{
			{Title: "Time now", URL: "https://example.test/now", Snippet: "snippet", Content: "content", Published: &published},
		}}, nil
	}
	env.useAdapter("account-1", adapter)
	server := env.server(t)
	response, payload := env.doJSON(t, server, "POST", "/v1/search", map[string]any{"query": "current time", "recency": "day"})
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body %s", response.StatusCode, payload)
	}
	want := `{"object":"search","model":"mock/brave","query":"current time","results":[` +
		`{"title":"Time now","url":"https://example.test/now","snippet":"snippet","content":"content","published":"2026-01-01"}]}`
	if string(payload) != want {
		t.Fatalf("body = %s\nwant   %s", payload, want)
	}
	if options := env.runner.last(); options.Capability != "search" || options.Method != providers.CapSearch || options.Format != "search" {
		t.Fatalf("options = %+v", options)
	}
}

func TestWeatherRoute(t *testing.T) {
	env := newTestEnv(t)
	adapter := newFakeAdapter("openweather", providers.CapWeather)
	adapter.weatherFn = func(request providers.WeatherRequest) (*providers.WeatherReport, error) {
		if request.Location != "Paris" || !request.Current || request.Hourly || !request.Daily || request.Days != 3 || request.Units != "metric" {
			t.Fatalf("request = %+v", request)
		}
		return &providers.WeatherReport{
			Location: map[string]any{"name": "Paris"},
			Current:  map[string]any{"temperature": 18.5},
		}, nil
	}
	env.useAdapter("account-1", adapter)
	server := env.server(t)
	response, payload := env.doJSON(t, server, "POST", "/v1/weather", map[string]any{"location": "Paris"})
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body %s", response.StatusCode, payload)
	}
	body := decodeBody(t, payload)
	if body["object"] != "weather" || body["model"] != "mock/openweather" || body["units"] != "metric" {
		t.Fatalf("body = %s", payload)
	}
	if body["location"] == nil || body["current"] == nil {
		t.Fatalf("body = %s", payload)
	}
	if body["hourly"] != nil || body["daily"] != nil {
		t.Fatalf("sections nobody asked for are null: %s", payload)
	}
	if options := env.runner.last(); options.Capability != "weather" || options.Format != "weather" || options.InputTokens != 0 {
		t.Fatalf("options = %+v", options)
	}
}

func TestMediaRoutesRequireAKey(t *testing.T) {
	env := newTestEnv(t)
	server := env.server(t)
	request, err := http.NewRequest(http.MethodPost, server.URL+"/v1/embeddings", strings.NewReader(`{"input":"x"}`))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	defer response.Body.Close()
	payload, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusUnauthorized || errorCode(t, payload) != "INVALID_API_KEY" {
		t.Fatalf("status = %d, body %s", response.StatusCode, payload)
	}
}

func TestRequestValidationReachesTheAPI(t *testing.T) {
	env := newTestEnv(t)
	server := env.server(t)
	response, payload := env.doJSON(t, server, "POST", "/v1/images/generations", map[string]any{})
	if response.StatusCode != http.StatusBadRequest || errorCode(t, payload) != "INVALID_REQUEST" {
		t.Fatalf("status = %d, body %s", response.StatusCode, payload)
	}
	if got := errorMessage(t, payload); got != "prompt: Required" {
		t.Fatalf("message = %q", got)
	}
	if failure := decodeBody(t, payload)["error"].(map[string]any); failure["type"] != "invalid_request_error" {
		t.Fatalf("type = %v", failure["type"])
	}
}

// isHTTPError is errors.As for an *httpserver.HttpError.
func isHTTPError(err error, target **httpserver.HttpError) bool {
	return errors.As(err, target)
}
