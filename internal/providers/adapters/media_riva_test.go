package adapters

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/NeoLabs-Systems/neorouter/internal/providers"
)

// OVHcloud's Riva text-to-speech: one host per model, a voice derived from the
// model's language, and the fallback to the other voice when Riva does not know
// the first one.

// TestRivaLanguageReadsTheModelID: the language and region of the model id,
// which may be a full host name too.
func TestRivaLanguageReadsTheModelID(t *testing.T) {
	cases := []struct {
		model    string
		language string
		region   string
		ok       bool
	}{
		{"nvr-tts-en-us", "en", "US", true},
		{"nvr-tts-de-de", "de", "DE", true},
		{"nvr-tts-es-es", "es", "ES", true},
		{"endpoints/nvr-tts-de-de", "de", "DE", true},
		{"meta/llama-3.1-8b", "", "", false},
		{"https://nvr-tts-de-de.endpoints.kepler.ai.cloud.ovh.net/api/v1/tts/text_to_audio", "", "", false},
		{"", "", "", false},
	}
	for _, item := range cases {
		language, region, ok := MediaRivaLanguage(item.model)
		if ok != item.ok || language != item.language || region != item.region {
			t.Fatalf("MediaRivaLanguage(%q) = %q %q %v", item.model, language, region, ok)
		}
	}
}

// TestRivaVoicesAreDerivedFromTheLanguage: English is Riva's multi-speaker
// model, the other languages are one voice per model.
func TestRivaVoicesAreDerivedFromTheLanguage(t *testing.T) {
	mediaTestSame(t, "english", []string{"English-US.Female-1", "English-US.Male-1"}, RivaVoices("nvr-tts-en-us"))
	mediaTestSame(t, "german", []string{"German-DE-Female-1", "German-DE-Male-1"}, RivaVoices("nvr-tts-de-de"))
	mediaTestSame(t, "spanish", []string{"Spanish-ES-Female-1", "Spanish-ES-Male-1"}, RivaVoices("nvr-tts-es-es"))
	if voices := RivaVoices("meta/llama-3.1-8b"); len(voices) != 0 {
		t.Fatalf("voices = %v", voices)
	}
}

// mediaRivaAdapter points the real OVH definition's speech path at a fake host:
// the dialect, the quirks and the request shape stay the ones the definition
// carries.
func mediaRivaAdapter(t *testing.T, baseURL string) *MediaMethods {
	t.Helper()
	def := mediaTestDefinition(t, "ovh")
	def.Media.Speech = baseURL + "/api/v1/tts/text_to_audio"
	return NewMediaMethods(def, mediaTestDeps("secret-1", "", nil))
}

// mediaRivaCalls records what the fake Riva saw.
type mediaRivaCalls struct {
	urls   []string
	bodies []map[string]any
	// unknown is the voice name Riva refuses, as OVHcloud does for a language
	// without that voice.
	unknown string
}

// handler answers text_to_audio, refusing one voice name.
func (c *mediaRivaCalls) handler(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/tts/text_to_audio" {
			t.Errorf("path = %q", r.URL.Path)
		}
		body := mediaTestBody(t, r)
		c.urls = append(c.urls, r.URL.String())
		c.bodies = append(c.bodies, body)
		if c.unknown != "" && mediaString(body["voice_name"]) == c.unknown {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"message":"Model is not available on server: Voice ` + c.unknown + ` for language de-DE not found."}`))
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(mediaWAVSamples(16000, []byte{1, 0, 2, 0}))
	}
}

// TestRivaSpeaksWithADerivedVoice: the body Riva takes, the WAV it answers with,
// and the fallback to the model's other voice.
func TestRivaSpeaksWithADerivedVoice(t *testing.T) {
	// German has no female voice on OVHcloud: Riva names the voice it does not
	// know.
	calls := &mediaRivaCalls{unknown: "German-DE-Female-1"}
	base := mediaTestServer(t, calls.handler(t))
	methods := mediaRivaAdapter(t, base)
	first, err := methods.Speech(context.Background(), providers.SpeechRequest{
		Input: "Hallo.", Voice: "alloy",
	}, providers.Call{ModelID: "nvr-tts-de-de"})
	if err != nil {
		t.Fatalf("speech: %v", err)
	}
	if first.Mime != "audio/wav" {
		t.Fatalf("mime = %q", first.Mime)
	}
	if len(calls.bodies) != 2 {
		t.Fatalf("calls = %d", len(calls.bodies))
	}
	mediaTestSame(t, "body", map[string]any{
		"encoding": 1, "language_code": "de-DE", "sample_rate_hz": 16000,
		"text": "Hallo.", "voice_name": "German-DE-Female-1",
	}, calls.bodies[0])
	if got := mediaString(calls.bodies[1]["voice_name"]); got != "German-DE-Male-1" {
		t.Fatalf("fallback voice = %q", got)
	}
	// The voice that worked is remembered for that host.
	if _, err := methods.Speech(context.Background(), providers.SpeechRequest{Input: "Noch einmal."}, providers.Call{ModelID: "nvr-tts-de-de"}); err != nil {
		t.Fatalf("speech: %v", err)
	}
	if len(calls.bodies) != 3 || mediaString(calls.bodies[2]["voice_name"]) != "German-DE-Male-1" {
		t.Fatalf("remembered voice = %v", calls.bodies)
	}
	// A full Riva voice name is passed on as it is.
	if _, err := methods.Speech(context.Background(), providers.SpeechRequest{Input: "Hi.", Voice: "English-US.Male-1"}, providers.Call{ModelID: "nvr-tts-en-us"}); err != nil {
		t.Fatalf("speech: %v", err)
	}
	if got := mediaString(calls.bodies[3]["voice_name"]); got != "English-US.Male-1" {
		t.Fatalf("voice = %q", got)
	}
	if got := mediaString(calls.bodies[3]["language_code"]); got != "en-US" {
		t.Fatalf("language = %q", got)
	}
	// The host labels the WAV as octet-stream; the adapter still reports audio.
	if first.Headers.Get("Content-Type") != "application/octet-stream" {
		t.Fatalf("headers = %v", first.Headers)
	}
}

// TestRivaSpeaksWithTheModelHostOnTheRealDefinition: the definition's own path
// has the model in the host, so a request goes to that model's endpoint.
func TestRivaSpeaksWithTheModelHostOnTheRealDefinition(t *testing.T) {
	def := mediaTestDefinition(t, "ovh")
	path, ok := MediaPath(def, providers.CapSpeech, "nvr-tts-en-us")
	if !ok {
		t.Fatal("ovh declares no speech path")
	}
	if path != "https://nvr-tts-en-us.endpoints.kepler.ai.cloud.ovh.net/api/v1/tts/text_to_audio" {
		t.Fatalf("path = %q", path)
	}
	if def.Quirks.SpeechAPI != "riva" {
		t.Fatalf("speech api = %q", def.Quirks.SpeechAPI)
	}
}

// TestRivaListsTheDerivedVoices: the HTTP host has no voice listing, so the
// voices come from the model id.
func TestRivaListsTheDerivedVoices(t *testing.T) {
	methods := mediaRivaAdapter(t, "https://example.test")
	voices, err := methods.Voices(context.Background(), "nvr-tts-en-us")
	if err != nil {
		t.Fatalf("voices: %v", err)
	}
	mediaTestSame(t, "voices", []map[string]string{
		{"id": "English-US.Female-1", "name": "English-US.Female-1"},
		{"id": "English-US.Male-1", "name": "English-US.Male-1"},
	}, voices)
	if voices := RivaVoices("nvr-tts-fr-fr"); voices[0] != "French-FR-Female-1" {
		t.Fatalf("voices = %v", voices)
	}
}

// TestRivaReportsAModelWithoutALanguage pins the message.
func TestRivaReportsAModelWithoutALanguage(t *testing.T) {
	methods := mediaRivaAdapter(t, "https://example.test")
	_, err := methods.Speech(context.Background(), providers.SpeechRequest{Input: "Hi"}, providers.Call{ModelID: "meta/llama-3.1-8b"})
	if kind := mediaTestErrorKind(t, err); kind != providers.KindBadRequest {
		t.Fatalf("kind = %v", kind)
	}
	if got := mediaErrorText(err); got != "The language of the Riva model meta/llama-3.1-8b is not known." {
		t.Fatalf("message = %q", got)
	}
}

// TestRivaNeverRetriesAVoiceRivaKnows: a refusal that is not about the voice is
// the caller's problem, and it is reported as it came.
func TestRivaNeverRetriesAVoiceRivaKnows(t *testing.T) {
	base := mediaTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"message":"Too many requests"}`))
	})
	methods := mediaRivaAdapter(t, base)
	_, err := methods.Speech(context.Background(), providers.SpeechRequest{Input: "Hallo."}, providers.Call{ModelID: "nvr-tts-de-de"})
	if kind := mediaTestErrorKind(t, err); kind != providers.KindRateLimit {
		t.Fatalf("kind = %v", kind)
	}
	if !strings.Contains(mediaErrorText(err), "Too many requests") {
		t.Fatalf("message = %q", mediaErrorText(err))
	}
}
