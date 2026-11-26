package adapters

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/NeoLabs-Systems/neorouter/internal/providers"
)

// Deepgram: /v1/speak with the Aura model a voice names, and /v1/listen with
// the raw audio. Tests of the voice providers.

// TestAuraModelPicksTheSpeaker: a full model id is passed on, an Aura voice
// names its own model, an OpenAI voice maps onto its Aura name, and anything
// else is Aura's default speaker.
func TestAuraModelPicksTheSpeaker(t *testing.T) {
	cases := []struct{ family, voice, want string }{
		{"aura-2", "nova", "aura-2-luna-en"},
		{"aura-2", "nova", "aura-2-luna-en"},
		{"aura-2", "aura-2-julius-de", "aura-2-julius-de"},
		{"aura-2", "unknown", "aura-2-thalia-en"},
		{"aura-2", "", "aura-2-thalia-en"},
		{"aura-2", "Kore", "aura-2-thalia-en"},
		{"aura-2", "SELENE", "aura-2-selene-en"},
		{"aura-2", "onyx", "aura-2-zeus-en"},
		{"aura-2-es", "luna", "aura-2-es-luna-en"},
	}
	for _, item := range cases {
		if got := AuraModel(item.family, item.voice); got != item.want {
			t.Fatalf("AuraModel(%q, %q) = %q, want %q", item.family, item.voice, got, item.want)
		}
	}
}

// mediaDeepgramCalls records what the fake Deepgram saw.
type mediaDeepgramCalls struct {
	speechQuery string
	auth        string
	speechBody  map[string]any
	listenQuery string
	listenType  string
	listenBytes int
}

// mediaDeepgramUpstream answers /v1/speak and /v1/listen.
func mediaDeepgramUpstream(t *testing.T, calls *mediaDeepgramCalls) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/speak":
			calls.speechQuery = r.URL.RawQuery
			calls.auth = r.Header.Get("Authorization")
			calls.speechBody = mediaTestBody(t, r)
			w.Header().Set("Content-Type", "audio/mpeg")
			_, _ = w.Write([]byte("mp3-bytes"))
		case "/v1/listen":
			calls.listenQuery = r.URL.RawQuery
			calls.listenType = r.Header.Get("Content-Type")
			raw, err := io.ReadAll(r.Body)
			if err != nil {
				t.Errorf("read audio: %v", err)
			}
			calls.listenBytes = len(raw)
			mediaTestJSON(t, w, map[string]any{
				"metadata": map[string]any{"duration": 3.3},
				"results": map[string]any{
					"channels": []any{map[string]any{
						"detected_language": "en",
						"alternatives":      []any{map[string]any{"transcript": "Hello from NeoRouter."}},
					}},
					"utterances": []any{
						map[string]any{"start": 0, "end": 1.5, "transcript": "Hello from"},
						map[string]any{"start": 1.5, "end": 3.3, "transcript": "NeoRouter."},
					},
				},
			})
		default:
			t.Errorf("path = %q", r.URL.Path)
		}
	}
}

// TestDeepgramSpeaksWithTheAuraVoiceOfAnOpenAIName: the encoding and container
// of the requested format, and Aura's voice.
func TestDeepgramSpeaksWithTheAuraVoiceOfAnOpenAIName(t *testing.T) {
	calls := &mediaDeepgramCalls{}
	base := mediaTestServer(t, mediaDeepgramUpstream(t, calls))
	adapter := NewDeepgramAdapter(mediaTestDefinition(t, "deepgram"), mediaTestDeps("secret-1", base+"/v1", nil))
	result, err := adapter.Speech(context.Background(), providers.SpeechRequest{
		Input: "Hi", Voice: "nova", Format: "wav",
	}, providers.Call{ModelID: "aura-2"})
	if err != nil {
		t.Fatalf("speech: %v", err)
	}
	if result.Mime != "audio/wav" {
		t.Fatalf("mime = %q", result.Mime)
	}
	if calls.auth != "Token secret-1" {
		t.Fatalf("authorization = %q", calls.auth)
	}
	query := mediaQueryOf(t, "/speak?"+calls.speechQuery)
	for parameter, want := range map[string]string{"model": "aura-2-luna-en", "encoding": "linear16", "container": "wav"} {
		if got := query.Get(parameter); got != want {
			t.Fatalf("%s = %q, want %q", parameter, got, want)
		}
	}
	if query.Get("sample_rate") != "" {
		t.Fatalf("sample_rate = %q", query.Get("sample_rate"))
	}
	mediaTestSame(t, "body", map[string]any{"text": "Hi"}, calls.speechBody)
}

// TestDeepgramPCMAsksForTheRate: PCM has no container, so the rate is sent.
func TestDeepgramPCMAsksForTheRate(t *testing.T) {
	calls := &mediaDeepgramCalls{}
	base := mediaTestServer(t, mediaDeepgramUpstream(t, calls))
	adapter := NewDeepgramAdapter(mediaTestDefinition(t, "deepgram"), mediaTestDeps("secret-1", base+"/v1", nil))
	result, err := adapter.Speech(context.Background(), providers.SpeechRequest{Input: "Hi", Format: "pcm", Speed: mediaTestFloat(1.25)}, providers.Call{ModelID: "aura-2"})
	if err != nil {
		t.Fatalf("speech: %v", err)
	}
	query := mediaQueryOf(t, "/speak?"+calls.speechQuery)
	if query.Get("sample_rate") != "24000" || query.Get("container") != "none" || query.Get("speed") != "1.25" {
		t.Fatalf("query = %v", query)
	}
	if result.Mime != "audio/pcm" {
		t.Fatalf("mime = %q", result.Mime)
	}
}

// TestDeepgramTranscribesRawAudio: the audio goes up as it is, and the answer's
// text, language and utterances come back.
func TestDeepgramTranscribesRawAudio(t *testing.T) {
	calls := &mediaDeepgramCalls{}
	base := mediaTestServer(t, mediaDeepgramUpstream(t, calls))
	adapter := NewDeepgramAdapter(mediaTestDefinition(t, "deepgram"), mediaTestDeps("secret-1", base+"/v1", nil))
	file := bytes.Repeat([]byte{1}, 64)
	result, err := adapter.Transcribe(context.Background(), providers.TranscribeRequest{File: file, Mime: "audio/wav"}, providers.Call{ModelID: "nova-3"})
	if err != nil {
		t.Fatalf("transcribe: %v", err)
	}
	if result.Text != "Hello from NeoRouter." || result.Language != "en" {
		t.Fatalf("transcript = %+v", result)
	}
	if result.Duration == nil || *result.Duration != 3.3 {
		t.Fatalf("duration = %v", result.Duration)
	}
	if len(result.Segments) != 2 || result.Segments[1].Text != "NeoRouter." {
		t.Fatalf("segments = %+v", result.Segments)
	}
	if calls.listenType != "audio/wav" || calls.listenBytes != 64 {
		t.Fatalf("upload = %q %d", calls.listenType, calls.listenBytes)
	}
	query := mediaQueryOf(t, "/listen?"+calls.listenQuery)
	for parameter, want := range map[string]string{
		"model": "nova-3", "smart_format": "true", "utterances": "true", "punctuate": "true",
		"detect_language": "true",
	} {
		if got := query.Get(parameter); got != want {
			t.Fatalf("%s = %q, want %q", parameter, got, want)
		}
	}
	if query.Get("language") != "" {
		t.Fatalf("language = %q", query.Get("language"))
	}
}

// TestDeepgramSendsALanguageAsGiven: a language is a fact, and only its absence
// asks for detection.
func TestDeepgramSendsALanguageAsGiven(t *testing.T) {
	calls := &mediaDeepgramCalls{}
	base := mediaTestServer(t, mediaDeepgramUpstream(t, calls))
	adapter := NewDeepgramAdapter(mediaTestDefinition(t, "deepgram"), mediaTestDeps("secret-1", base+"/v1", nil))
	if _, err := adapter.Transcribe(context.Background(), providers.TranscribeRequest{File: []byte{1}, Mime: "audio/wav", Language: "de"}, providers.Call{ModelID: "nova-3"}); err != nil {
		t.Fatalf("transcribe: %v", err)
	}
	query := mediaQueryOf(t, "/listen?"+calls.listenQuery)
	if query.Get("language") != "de" || query.Get("detect_language") != "" {
		t.Fatalf("query = %v", query)
	}
}

// TestDeepgramTurnsAPromptIntoKeyTermsForNova3: Nova-3 takes the prompt's words
// as hints, at most twenty.
func TestDeepgramTurnsAPromptIntoKeyTermsForNova3(t *testing.T) {
	calls := &mediaDeepgramCalls{}
	base := mediaTestServer(t, mediaDeepgramUpstream(t, calls))
	adapter := NewDeepgramAdapter(mediaTestDefinition(t, "deepgram"), mediaTestDeps("secret-1", base+"/v1", nil))
	prompt := strings.TrimSpace(strings.Repeat("NeoRouter, karucode ", 13))
	if _, err := adapter.Transcribe(context.Background(), providers.TranscribeRequest{File: []byte{1}, Mime: "audio/wav", Prompt: prompt}, providers.Call{ModelID: "nova-3"}); err != nil {
		t.Fatalf("transcribe: %v", err)
	}
	query := mediaQueryOf(t, "/listen?"+calls.listenQuery)
	terms := query["keyterm"]
	if len(terms) != 20 {
		t.Fatalf("key terms = %d, want 20", len(terms))
	}
	if terms[0] != "NeoRouter" {
		t.Fatalf("first key term = %q", terms[0])
	}
	// A model that takes no key terms is not sent any.
	calls.listenQuery = ""
	if _, err := adapter.Transcribe(context.Background(), providers.TranscribeRequest{File: []byte{1}, Mime: "audio/wav", Prompt: prompt}, providers.Call{ModelID: "nova-2"}); err != nil {
		t.Fatalf("transcribe: %v", err)
	}
	if got := mediaQueryOf(t, "/listen?"+calls.listenQuery)["keyterm"]; len(got) != 0 {
		t.Fatalf("key terms = %v", got)
	}
}

// TestDeepgramReportsNoTranscript pins the message.
func TestDeepgramReportsNoTranscript(t *testing.T) {
	base := mediaTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		mediaTestJSON(t, w, map[string]any{"results": map[string]any{"channels": []any{}}})
	})
	adapter := NewDeepgramAdapter(mediaTestDefinition(t, "deepgram"), mediaTestDeps("secret-1", base+"/v1", nil))
	_, err := adapter.Transcribe(context.Background(), providers.TranscribeRequest{File: []byte{1}, Mime: "audio/wav"}, providers.Call{ModelID: "nova-3"})
	if kind := mediaTestErrorKind(t, err); kind != providers.KindInvalidOutput {
		t.Fatalf("kind = %v", kind)
	}
	if got := mediaErrorText(err); got != "Deepgram returned no transcript." {
		t.Fatalf("message = %q", got)
	}
}

// TestDeepgramSurface: speech and transcription only, no translation.
func TestDeepgramSurface(t *testing.T) {
	adapter := NewDeepgramAdapter(mediaTestDefinition(t, "deepgram"), mediaTestDeps("secret-1", "https://api.deepgram.com/v1", nil))
	if !adapter.Supports(providers.CapSpeech) || !adapter.Supports(providers.CapTranscribe) {
		t.Fatal("Deepgram speaks and transcribes")
	}
	if adapter.Supports(providers.CapChat) || adapter.Supports(providers.CapEmbed) {
		t.Fatal("Deepgram has no chat or embeddings")
	}
	if adapter.TranscribeTranslate() {
		t.Fatal("Deepgram cannot translate speech into English")
	}
	if !adapter.TranscribeTimestamps() {
		t.Fatal("Deepgram answers with utterances")
	}
	models, err := adapter.ListModels(context.Background())
	if err != nil {
		t.Fatalf("models: %v", err)
	}
	kinds := map[string]string{}
	for _, model := range models {
		kinds[model.ID] = model.Kind
	}
	if kinds["nova-3"] != providers.KindTranscription || kinds["aura-2"] != providers.KindSpeech {
		t.Fatalf("kinds = %v", kinds)
	}
	voices, err := adapter.Voices(context.Background(), "aura-2")
	if err != nil {
		t.Fatalf("voices: %v", err)
	}
	if len(voices) != len(AuraVoices) || voices[0].ID != "thalia" || voices[0].Name != "Thalia" {
		t.Fatalf("voices = %v", voices)
	}
	if quota, err := adapter.FetchQuota(context.Background()); err != nil || quota != nil {
		t.Fatalf("quota = %v %v", quota, err)
	}
}

// TestDeepgramValidatesAgainstItsProjects: the check is an authenticated GET of
// /projects, not /models.
func TestDeepgramValidatesAgainstItsProjects(t *testing.T) {
	var path string
	base := mediaTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		mediaTestJSON(t, w, map[string]any{"projects": []any{}})
	})
	adapter := NewDeepgramAdapter(mediaTestDefinition(t, "deepgram"), mediaTestDeps("secret-1", base+"/v1", nil))
	validation, err := adapter.Validate(context.Background())
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if path != "/v1/projects" {
		t.Fatalf("path = %q", path)
	}
	if validation.Status != providers.ValidationHealthy {
		t.Fatalf("validation = %+v", validation)
	}
}

// TestDeepgramUnknownFormatFallsBackToMP3: the mime the caller gets is the
// format that was sent, and an unknown format is sent as mp3.
func TestDeepgramUnknownFormatFallsBackToMP3(t *testing.T) {
	calls := &mediaDeepgramCalls{}
	base := mediaTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls.speechQuery = r.URL.RawQuery
		calls.speechBody = mediaTestBody(t, r)
		w.Header().Set("Content-Type", "audio/wav")
		_, _ = w.Write(mediaWAVSamples(16000, []byte{1, 0}))
	})
	adapter := NewDeepgramAdapter(mediaTestDefinition(t, "deepgram"), mediaTestDeps("secret-1", base+"/v1", nil))
	result, err := adapter.Speech(context.Background(), providers.SpeechRequest{Input: "Hallo.", Voice: "unknown", Format: "mp3"}, providers.Call{ModelID: "aura-2"})
	if err != nil {
		t.Fatalf("speech: %v", err)
	}
	if query := mediaQueryOf(t, "/speak?"+calls.speechQuery); query.Get("encoding") != "mp3" {
		t.Fatalf("encoding = %q", query.Get("encoding"))
	}
	if result.Mime != "audio/mpeg" {
		t.Fatalf("mime = %q", result.Mime)
	}
	if got := mediaQueryOf(t, "/speak?"+calls.speechQuery).Get("model"); got != "aura-2-thalia-en" {
		t.Fatalf("model = %q", got)
	}
}
