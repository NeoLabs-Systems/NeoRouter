package adapters

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/NeoLabs-Systems/neorouter/internal/providers"
)

// ElevenLabs: the voices a plan can use, the voice a name resolves to, and the
// Voice Library refusal a free account meets. Tests of the ElevenLabs voices
// and speech.

// mediaElevenLabsUpstream is a fake account: two current default voices and two
// of its own, one of them a Voice Library voice.
type mediaElevenLabsUpstream struct {
	spoken []string
	// refuse decides which voice ids the fake host refuses as a library voice.
	refuse func(voiceID string) bool
	// listing is what /v1/models answers.
	models []any
	// words is what a transcription answers with.
	words []any
}

// handler answers the four endpoints ElevenLabs is called on.
func (u *mediaElevenLabsUpstream) handler(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v2/voices":
			if r.URL.Query().Get("voice_type") != "default" || r.URL.Query().Get("page_size") != "100" {
				t.Errorf("default voices query = %s", r.URL.RawQuery)
			}
			mediaTestJSON(t, w, map[string]any{"voices": []any{
				map[string]any{"voice_id": "DefaultFemale0000001", "name": "Talia", "category": "premade", "labels": map[string]any{"gender": "female"}},
				map[string]any{"voice_id": "DefaultMale000000001", "name": "Eldrin", "category": "premade", "labels": map[string]any{"gender": "male"}},
			}})
		case r.URL.Path == "/v1/voices":
			mediaTestJSON(t, w, map[string]any{"voices": []any{
				map[string]any{"voice_id": "LibraryVoice00000001", "name": "Narrator", "category": "professional"},
				map[string]any{"voice_id": "MyOwnClone0000000001", "name": "Me", "category": "cloned"},
			}})
		case r.URL.Path == "/v1/models":
			// /v1/models answers a bare array, as the real API does.
			mediaTestJSON(t, w, u.models)
		case len(r.URL.Path) > len("/v1/text-to-speech/") && r.URL.Path[:len("/v1/text-to-speech/")] == "/v1/text-to-speech/":
			voiceID := r.URL.Path[len("/v1/text-to-speech/"):]
			u.spoken = append(u.spoken, voiceID)
			if r.URL.Query().Get("output_format") == "" {
				t.Error("no output format was asked for")
			}
			if r.Header.Get("xi-api-key") != "secret-1" {
				t.Errorf("key header = %q", r.Header.Get("xi-api-key"))
			}
			if u.refuse != nil && u.refuse(voiceID) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusPaymentRequired)
				_, _ = w.Write([]byte(`{"detail":{"code":"paid_plan_required","message":"Free users cannot use library voices via the API. Please upgrade your subscription to use this voice."}}`))
				return
			}
			w.Header().Set("Content-Type", "audio/mpeg")
			_, _ = w.Write([]byte("eleven-mp3"))
		case r.URL.Path == "/v1/speech-to-text":
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Errorf("parse form: %v", err)
			}
			if got := r.MultipartForm.Value["timestamps_granularity"]; len(got) == 0 || got[0] != "word" {
				t.Errorf("granularity = %v", got)
			}
			if got := r.MultipartForm.Value["model_id"]; len(got) == 0 || got[0] != "scribe_v2" {
				t.Errorf("model = %v", got)
			}
			mediaTestJSON(t, w, map[string]any{"language_code": "eng", "text": "Hi there. Bye now.", "words": u.words})
		default:
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
	}
}

// mediaElevenLabsAdapter builds the adapter against baseURL.
func mediaElevenLabsAdapter(t *testing.T, baseURL string) *ElevenLabsAdapter {
	t.Helper()
	return NewElevenLabsAdapter(mediaTestDefinition(t, "elevenlabs"), mediaTestDeps("secret-1", baseURL+"/v1", nil))
}

// TestElevenLabsPicksVoicesThePlanCanUse: defaults, OpenAI names by voice type,
// and the account's own voices by name.
func TestElevenLabsPicksVoicesThePlanCanUse(t *testing.T) {
	upstream := &mediaElevenLabsUpstream{}
	base := mediaTestServer(t, upstream.handler(t))
	adapter := mediaElevenLabsAdapter(t, base)
	ctx := context.Background()
	cases := []struct{ voice, want string }{
		{"", "DefaultFemale0000001"},
		{"onyx", "DefaultMale000000001"},
		{"Kore", "DefaultFemale0000001"},
		{"Me", "MyOwnClone0000000001"},
		{"6WwXjDDEMyNmFG95zycZ", "6WwXjDDEMyNmFG95zycZ"},
		{"Narrator", "LibraryVoice00000001"},
	}
	for _, item := range cases {
		if got := adapter.VoiceID(ctx, item.voice); got != item.want {
			t.Fatalf("VoiceID(%q) = %q, want %q", item.voice, got, item.want)
		}
	}
}

// TestElevenLabsRetriesAVoiceLibraryRefusal: a library voice refused on a free
// plan is retried with a default voice, and never read as "needs payment".
func TestElevenLabsRetriesAVoiceLibraryRefusal(t *testing.T) {
	upstream := &mediaElevenLabsUpstream{refuse: func(voiceID string) bool { return voiceID == "LibraryVoice00000001" }}
	base := mediaTestServer(t, upstream.handler(t))
	adapter := mediaElevenLabsAdapter(t, base)
	result, err := adapter.Speech(context.Background(), providers.SpeechRequest{
		Input: "Hello there.", Voice: "LibraryVoice00000001",
	}, providers.Call{ModelID: "eleven_flash_v2_5"})
	if err != nil {
		t.Fatalf("speech: %v", err)
	}
	if len(result.Audio) == 0 {
		t.Fatal("no audio came back")
	}
	if len(upstream.spoken) != 2 || upstream.spoken[0] != "LibraryVoice00000001" || upstream.spoken[1] != "DefaultFemale0000001" {
		t.Fatalf("voices spoken = %v", upstream.spoken)
	}
}

// TestElevenLabsReportsAStubbornVoiceLibraryRefusal: a plan that refuses even
// the account's own voice gets a bad request, not a payment failure.
func TestElevenLabsReportsAStubbornVoiceLibraryRefusal(t *testing.T) {
	upstream := &mediaElevenLabsUpstream{refuse: func(string) bool { return true }}
	base := mediaTestServer(t, upstream.handler(t))
	adapter := mediaElevenLabsAdapter(t, base)
	_, err := adapter.Speech(context.Background(), providers.SpeechRequest{
		Input: "Hi", Voice: "LibraryVoice00000001",
	}, providers.Call{ModelID: "eleven_flash_v2_5"})
	if kind := mediaTestErrorKind(t, err); kind != providers.KindBadRequest {
		t.Fatalf("kind = %v", kind)
	}
	if got := mediaErrorText(err); got != "ElevenLabs refused its own premade voice as a Voice Library voice; pick one of your voices." {
		t.Fatalf("message = %q", got)
	}
}

// TestElevenLabsTranscribesIntoSubtitleCues: word timings become cues, and the
// transcript's duration is the last word's end.
func TestElevenLabsTranscribesIntoSubtitleCues(t *testing.T) {
	upstream := &mediaElevenLabsUpstream{words: []any{
		map[string]any{"text": "Hi", "start": 0, "end": 0.3, "type": "word"},
		map[string]any{"text": " ", "start": 0.3, "end": 0.35, "type": "spacing"},
		map[string]any{"text": "there.", "start": 0.35, "end": 0.8, "type": "word"},
		map[string]any{"text": "Bye", "start": 2.5, "end": 2.8, "type": "word"},
		map[string]any{"text": "now.", "start": 2.8, "end": 3.1, "type": "word"},
	}}
	base := mediaTestServer(t, upstream.handler(t))
	adapter := mediaElevenLabsAdapter(t, base)
	result, err := adapter.Transcribe(context.Background(), providers.TranscribeRequest{
		File: []byte("wav"), Filename: "a.wav", Mime: "audio/wav",
	}, providers.Call{ModelID: "scribe_v2"})
	if err != nil {
		t.Fatalf("transcribe: %v", err)
	}
	if result.Text != "Hi there. Bye now." || result.Language != "eng" {
		t.Fatalf("transcript = %+v", result)
	}
	mediaTestSame(t, "cues", []map[string]any{
		{"start": 0, "end": 0.8, "text": "Hi there."},
		{"start": 2.5, "end": 3.1, "text": "Bye now."},
	}, result.Segments)
	if result.Duration == nil || *result.Duration != 3.1 {
		t.Fatalf("duration = %v", result.Duration)
	}
	if cues := Cues(nil); len(cues) != 0 {
		t.Fatalf("cues = %v", cues)
	}
}

// TestCuesBreaksOnPausesSentenceEndsAndFourteenWords: the three reasons a new
// cue starts.
func TestCuesBreaksOnPausesSentenceEndsAndFourteenWords(t *testing.T) {
	// A pause of more than a second starts a new cue.
	paused := Cues([]ElevenLabsWord{
		{Text: "one", Start: 0, End: 0.5},
		{Text: "two", Start: 2, End: 2.5},
	})
	if len(paused) != 2 || paused[1].Text != "two" {
		t.Fatalf("cues = %+v", paused)
	}
	// A sentence end closes the cue even without a pause.
	sentence := Cues([]ElevenLabsWord{
		{Text: "end.", Start: 0, End: 0.5},
		{Text: "next", Start: 0.6, End: 1},
	})
	if len(sentence) != 2 || sentence[1].Text != "next" {
		t.Fatalf("cues = %+v", sentence)
	}
	// Fourteen words make a cue; a fifteenth starts the next one, and spacing
	// and audio events never count.
	words := make([]ElevenLabsWord, 0, 16)
	for index := 0; index < 16; index++ {
		words = append(words, ElevenLabsWord{Text: "w", Start: float64(index) / 10, End: float64(index)/10 + 0.05})
		if index == 3 {
			words = append(words, ElevenLabsWord{Text: " ", Type: "spacing", Start: 0.3, End: 0.35})
		}
	}
	cues := Cues(words)
	if len(cues) != 2 || cues[0].End != 1.35 {
		t.Fatalf("cues = %+v", cues)
	}
}

// TestElevenLabsSpeaksWithTheFormatItWasAsked: the output format and the clamped
// speed.
func TestElevenLabsSpeaksWithTheFormatItWasAsked(t *testing.T) {
	var sent map[string]any
	var format string
	upstream := &mediaElevenLabsUpstream{}
	base := mediaTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/v1/text-to-speech/") {
			format = r.URL.Query().Get("output_format")
			sent = mediaTestBody(t, r)
			w.Header().Set("Content-Type", "audio/ogg")
			_, _ = w.Write([]byte("ogg"))
			return
		}
		upstream.handler(t)(w, r)
	})
	adapter := mediaElevenLabsAdapter(t, base)
	result, err := adapter.Speech(context.Background(), providers.SpeechRequest{
		Input: "Hi", Voice: "Me", Format: "opus", Speed: mediaTestFloat(3),
	}, providers.Call{ModelID: "eleven_multilingual_v2"})
	if err != nil {
		t.Fatalf("speech: %v", err)
	}
	if format != "opus_48000_64" {
		t.Fatalf("output format = %q", format)
	}
	if result.Mime != "audio/ogg" {
		t.Fatalf("mime = %q", result.Mime)
	}
	settings := mediaObject(sent["voice_settings"])
	if settings == nil || settings["speed"] != float64(1.2) {
		t.Fatalf("voice settings = %v", sent["voice_settings"])
	}
	if mediaString(sent["model_id"]) != "eleven_multilingual_v2" || mediaString(sent["text"]) != "Hi" {
		t.Fatalf("body = %+v", sent)
	}
}

// TestElevenLabsListsSpeechModelsAndScribe: /v1/models omits Scribe, so the
// definition adds it.
func TestElevenLabsListsSpeechModelsAndScribe(t *testing.T) {
	upstream := &mediaElevenLabsUpstream{models: []any{
		map[string]any{"model_id": "eleven_multilingual_v2", "name": "Multilingual v2"},
		map[string]any{"model_id": "eleven_voice_changer", "name": "Voice Changer", "can_do_text_to_speech": false},
	}}
	base := mediaTestServer(t, upstream.handler(t))
	models, err := mediaElevenLabsAdapter(t, base).ListModels(context.Background())
	if err != nil {
		t.Fatalf("models: %v", err)
	}
	kinds := map[string]string{}
	for _, model := range models {
		kinds[model.ID] = model.Kind
	}
	if kinds["eleven_multilingual_v2"] != providers.KindSpeech {
		t.Fatalf("kinds = %v", kinds)
	}
	if _, listed := kinds["eleven_voice_changer"]; listed {
		t.Fatal("a model that cannot speak was listed")
	}
	if kinds["scribe_v2"] != providers.KindTranscription {
		t.Fatalf("Scribe is missing: %v", kinds)
	}
}

// TestElevenLabsVoicesAndSurface: the account's voices, and the methods it
// serves.
func TestElevenLabsVoicesAndSurface(t *testing.T) {
	upstream := &mediaElevenLabsUpstream{}
	base := mediaTestServer(t, upstream.handler(t))
	adapter := mediaElevenLabsAdapter(t, base)
	voices, err := adapter.Voices(context.Background(), "eleven_multilingual_v2")
	if err != nil {
		t.Fatalf("voices: %v", err)
	}
	mediaTestSame(t, "voices", []map[string]string{
		{"id": "LibraryVoice00000001", "name": "Narrator"},
		{"id": "MyOwnClone0000000001", "name": "Me"},
	}, voices)
	if !adapter.Supports(providers.CapSpeech) || !adapter.Supports(providers.CapTranscribe) || adapter.Supports(providers.CapChat) {
		t.Fatal("ElevenLabs speaks and transcribes only")
	}
	if adapter.TranscribeTranslate() {
		t.Fatal("Scribe does not translate")
	}
	if !adapter.TranscribeTimestamps() {
		t.Fatal("Scribe answers with word timings")
	}
}

// TestElevenLabsFallsBackToItsStaticDefaultVoice: an account whose voice lists
// cannot be read still speaks, with a current default voice.
func TestElevenLabsFallsBackToItsStaticDefaultVoice(t *testing.T) {
	var spoken []string
	base := mediaTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/voices" || r.URL.Path == "/v2/voices":
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"detail":{"status":"invalid_api_key"}}`))
		case strings.HasPrefix(r.URL.Path, "/v1/text-to-speech/"):
			spoken = append(spoken, r.URL.Path[len("/v1/text-to-speech/"):])
			w.Header().Set("Content-Type", "audio/mpeg")
			_, _ = w.Write([]byte("eleven-mp3"))
		default:
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
	})
	adapter := mediaElevenLabsAdapter(t, base)
	result, err := adapter.Speech(context.Background(), providers.SpeechRequest{Input: "Hi", Voice: "alloy"}, providers.Call{ModelID: "eleven_multilingual_v2"})
	if err != nil {
		t.Fatalf("speech: %v", err)
	}
	if string(result.Audio) != "eleven-mp3" {
		t.Fatalf("audio = %q", result.Audio)
	}
	if len(spoken) != 1 || spoken[0] != ElevenLabsDefaultVoice {
		t.Fatalf("voice = %v", spoken)
	}
	if got := adapter.VoiceID(context.Background(), "Kore"); got != ElevenLabsDefaultVoice {
		t.Fatalf("voice id = %q", got)
	}
}

// TestElevenLabsValidatesAgainstItsModels: the definition's own path.
func TestElevenLabsValidatesAgainstItsModels(t *testing.T) {
	upstream := &mediaElevenLabsUpstream{models: []any{}}
	base := mediaTestServer(t, upstream.handler(t))
	validation, err := mediaElevenLabsAdapter(t, base).Validate(context.Background())
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if validation.Status != providers.ValidationHealthy {
		t.Fatalf("validation = %+v", validation)
	}
	refusing := mediaElevenLabsAdapter(t, mediaTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"detail":{"status":"invalid_api_key"}}`))
	}))
	invalid, err := refusing.Validate(context.Background())
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if invalid.Status != providers.ValidationInvalid {
		t.Fatalf("validation = %+v", invalid)
	}
}
