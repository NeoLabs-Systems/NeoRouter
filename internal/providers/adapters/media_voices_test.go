package adapters

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/NeoLabs-Systems/neorouter/internal/providers"
)

// Groq's Orpheus caps one request at 200 characters and answers WAV; OpenRouter
// takes the audio as base64 JSON, sends PCM and refuses the format a model
// cannot make. Tests of the speech paths.

// mediaSpeechUpstream records what one speech request sent.
type mediaSpeechUpstream struct {
	bodies []map[string]any
	// refuse decides what the fake host answers with a 400.
	refuse func(body map[string]any) bool
	// mime is the content type the audio comes back as.
	mime string
	// audio is the body of a successful answer.
	audio []byte
}

// handler answers /v1/audio/speech like a provider.
func (u *mediaSpeechUpstream) handler(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/audio/speech") {
			t.Errorf("path = %q", r.URL.Path)
		}
		body := mediaTestBody(t, r)
		u.bodies = append(u.bodies, body)
		if u.refuse != nil && u.refuse(body) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"This model only returns pcm."}}`))
			return
		}
		w.Header().Set("Content-Type", u.mime)
		_, _ = w.Write(u.audio)
	}
}

// TestGroqSpeaksALongTextInPartsJoinedIntoOneWAV: no part is over 200
// characters, no text is lost and the samples are in order.
func TestGroqSpeaksALongTextInPartsJoinedIntoOneWAV(t *testing.T) {
	var inputs []string
	base := mediaTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/openai/v1/audio/speech" {
			t.Errorf("path = %q", r.URL.Path)
		}
		body := mediaTestBody(t, r)
		inputs = append(inputs, mediaString(body["input"]))
		w.Header().Set("Content-Type", "audio/wav")
		_, _ = w.Write(mediaWAVSamples(24000, []byte{byte(len(inputs)), byte(len(inputs))}))
	})
	methods := mediaTestMethods(t, "groq", base+"/openai/v1")
	text := ""
	for i := 0; i < 8; i++ {
		text += "This sentence has a few words in it. "
	}
	for i := 0; i < 250; i++ {
		text += "x"
	}
	text += "."
	result, err := methods.Speech(context.Background(), providers.SpeechRequest{Input: text, Voice: "troy"}, providers.Call{ModelID: "canopylabs/orpheus-v1-english"})
	if err != nil {
		t.Fatalf("speech: %v", err)
	}
	if len(inputs) <= 2 {
		t.Fatalf("parts = %d", len(inputs))
	}
	for _, input := range inputs {
		if mediaLength(input) > 200 {
			t.Fatalf("a part of %d characters was sent", mediaLength(input))
		}
	}
	if result.Mime != "audio/wav" {
		t.Fatalf("mime = %q", result.Mime)
	}
	joined, ok := ParseWAV(result.Audio)
	if !ok {
		t.Fatal("the answer is not a WAV file")
	}
	if joined.Format.SampleRate != 24000 {
		t.Fatalf("sample rate = %d", joined.Format.SampleRate)
	}
	samples := make([]byte, 0, len(inputs)*2)
	for index := range inputs {
		samples = append(samples, byte(index+1), byte(index+1))
	}
	if !bytes.Equal(joined.Data, samples) {
		t.Fatalf("samples = %v, want %v", joined.Data, samples)
	}
	if got := uint32(result.Audio[4]) | uint32(result.Audio[5])<<8 | uint32(result.Audio[6])<<16 | uint32(result.Audio[7])<<24; int(got) != len(result.Audio)-8 {
		t.Fatalf("RIFF size = %d, length %d", got, len(result.Audio)-8)
	}
	// Short text stays one request.
	inputs = nil
	if _, err := methods.Speech(context.Background(), providers.SpeechRequest{Input: "Hi.", Voice: "troy"}, providers.Call{ModelID: "canopylabs/orpheus-v1-english"}); err != nil {
		t.Fatalf("speech: %v", err)
	}
	if len(inputs) != 1 || inputs[0] != "Hi." {
		t.Fatalf("parts = %v", inputs)
	}
}

// TestGroqPicksAVoiceOfTheModel: an Arabic Orpheus gets an Arabic voice, the
// English one an English voice.
func TestGroqPicksAVoiceOfTheModel(t *testing.T) {
	upstream := &mediaSpeechUpstream{mime: "audio/wav", audio: mediaWAVSamples(24000, []byte{1, 0})}
	base := mediaTestServer(t, upstream.handler(t))
	methods := mediaTestMethods(t, "groq", base+"/openai/v1")
	cases := []struct{ model, voice, want string }{
		{"canopylabs/orpheus-arabic-saudi", "alloy", "fahad"},
		{"canopylabs/orpheus-v1-english", "alloy", "troy"},
		{"canopylabs/orpheus-arabic-saudi", "noura", "noura"},
	}
	for _, item := range cases {
		if _, err := methods.Speech(context.Background(), providers.SpeechRequest{Input: "Hi", Voice: item.voice}, providers.Call{ModelID: item.model}); err != nil {
			t.Fatalf("speech: %v", err)
		}
	}
	for index, item := range cases {
		if got := mediaString(upstream.bodies[index]["voice"]); got != item.want {
			t.Fatalf("voice %d = %q, want %q", index, got, item.want)
		}
	}
}

// TestOpenRouterSpeechTakesEachModelsVoices: an unknown voice gets the first one
// the model lists, and a model with no list gets no voice name at all.
func TestOpenRouterSpeechTakesEachModelsVoices(t *testing.T) {
	upstream := &mediaSpeechUpstream{mime: "audio/pcm", audio: []byte{1, 0, 2, 0}}
	base := mediaTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/models":
			mediaTestJSON(t, w, map[string]any{"data": []any{
				map[string]any{"id": "heygen/heygen-voice-1-turbo:free", "supported_voices": []any{"nadine", "tabitha"}},
				map[string]any{"id": "google/gemini-3.8-flash-tts", "supported_voices": []any{"Zephyr", "Kore"}},
				map[string]any{"id": "fish-audio/s2.1-pro-free:free", "supported_voices": nil},
			}})
		case r.URL.Path == "/api/v1/audio/speech":
			body := mediaTestBody(t, r)
			upstream.bodies = append(upstream.bodies, body)
			w.Header().Set("Content-Type", "audio/pcm")
			_, _ = w.Write([]byte{1, 0, 2, 0})
		default:
			t.Errorf("path = %q", r.URL.Path)
		}
	})
	methods := mediaTestMethods(t, "openrouter", base+"/api/v1")
	calls := []struct {
		model string
		voice string
	}{
		{"heygen/heygen-voice-1-turbo:free", "alloy"},
		{"google/gemini-3.8-flash-tts", "kore"},
		{"fish-audio/s2.1-pro-free:free", ""},
	}
	for _, call := range calls {
		if _, err := methods.Speech(context.Background(), providers.SpeechRequest{Input: "Hi", Voice: call.voice, Format: "pcm"}, providers.Call{ModelID: call.model}); err != nil {
			t.Fatalf("speech: %v", err)
		}
	}
	want := []string{"nadine", "Kore", ""}
	for index, voice := range want {
		got := mediaString(upstream.bodies[index]["voice"])
		if got != voice {
			t.Fatalf("voice %d = %q, want %q", index, got, voice)
		}
	}
	if _, found := upstream.bodies[2]["voice"]; found {
		t.Fatal("a model without a voice list must not be sent an OpenAI voice name")
	}
}

// TestOpenRouterPCMComesBackAsWAV: PCM has no header, so it is wrapped unless
// PCM was asked for.
func TestOpenRouterPCMComesBackAsWAV(t *testing.T) {
	upstream := &mediaSpeechUpstream{
		mime:  "audio/pcm",
		audio: []byte{1, 0, 2, 0},
		refuse: func(body map[string]any) bool {
			return mediaString(body["response_format"]) == "mp3"
		},
	}
	base := mediaTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/models" {
			mediaTestJSON(t, w, map[string]any{"data": []any{
				map[string]any{"id": "heygen/heygen-voice-1-turbo:free", "supported_voices": []any{"nadine"}},
			}})
			return
		}
		upstream.handler(t)(w, r)
	})
	methods := mediaTestMethods(t, "openrouter", base+"/api/v1")
	result, err := methods.Speech(context.Background(), providers.SpeechRequest{Input: "Hi", Voice: "nadine", Format: "mp3"}, providers.Call{ModelID: "heygen/heygen-voice-1-turbo:free"})
	if err != nil {
		t.Fatalf("speech: %v", err)
	}
	formats := make([]string, 0, len(upstream.bodies))
	for _, body := range upstream.bodies {
		formats = append(formats, mediaString(body["response_format"]))
	}
	if len(formats) != 2 || formats[0] != "mp3" || formats[1] != "pcm" {
		t.Fatalf("formats = %v", formats)
	}
	if result.Mime != "audio/wav" {
		t.Fatalf("mime = %q", result.Mime)
	}
	parsed, ok := ParseWAV(result.Audio)
	if !ok {
		t.Fatal("the answer is not a WAV file")
	}
	if parsed.Format.SampleRate != 24000 {
		t.Fatalf("sample rate = %d", parsed.Format.SampleRate)
	}
	if !bytes.Equal(parsed.Data, []byte{1, 0, 2, 0}) {
		t.Fatalf("samples = %v", parsed.Data)
	}
	// PCM asked for is passed on as it is.
	raw, err := methods.Speech(context.Background(), providers.SpeechRequest{Input: "Hi", Voice: "nadine", Format: "pcm"}, providers.Call{ModelID: "heygen/heygen-voice-1-turbo:free"})
	if err != nil {
		t.Fatalf("speech: %v", err)
	}
	if raw.Mime != "audio/pcm" || !bytes.Equal(raw.Audio, []byte{1, 0, 2, 0}) {
		t.Fatalf("raw = %q %v", raw.Mime, raw.Audio)
	}
	// A format the model does not produce is asked for as its first one, then
	// wrapped into WAV because the answer is PCM.
	wrapped, err := methods.Speech(context.Background(), providers.SpeechRequest{Input: "Hi", Voice: "nadine", Format: "wav"}, providers.Call{ModelID: "heygen/heygen-voice-1-turbo:free"})
	if err != nil {
		t.Fatalf("speech: %v", err)
	}
	if wrapped.Mime != "audio/wav" {
		t.Fatalf("mime = %q", wrapped.Mime)
	}
}

// TestSpeechReportsAnEmptyAnswer pins the message.
func TestSpeechReportsAnEmptyAnswer(t *testing.T) {
	base := mediaTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/mpeg")
	})
	methods := mediaTestMethods(t, "groq", base+"/openai/v1")
	_, err := methods.Speech(context.Background(), providers.SpeechRequest{Input: "Hi", Voice: "troy"}, providers.Call{ModelID: "canopylabs/orpheus-v1-english"})
	if kind := mediaTestErrorKind(t, err); kind != providers.KindEmpty {
		t.Fatalf("kind = %v", kind)
	}
	if got := mediaErrorText(err); got != "The provider returned no audio." {
		t.Fatalf("message = %q", got)
	}
}

// mediaMultipart reads the parts of an uploaded form.
func mediaMultipart(t *testing.T, r *http.Request) map[string]string {
	t.Helper()
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		t.Fatalf("parse form: %v", err)
	}
	parts := map[string]string{}
	for name, values := range r.MultipartForm.Value {
		parts[name] = values[0]
	}
	for name, files := range r.MultipartForm.File {
		file, err := files[0].Open()
		if err != nil {
			t.Fatalf("open %s: %v", name, err)
		}
		raw, err := io.ReadAll(file)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		parts[name] = string(raw)
		parts[name+".filename"] = files[0].Filename
		parts[name+".type"] = files[0].Header.Get("Content-Type")
	}
	return parts
}

// TestTranscribeUploadsAVerboseJSONForm: the OpenAI transcription form.
func TestTranscribeUploadsAVerboseJSONForm(t *testing.T) {
	var seen map[string]string
	var path string
	base := mediaTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		seen = mediaMultipart(t, r)
		mediaTestJSON(t, w, map[string]any{
			"text": "hello world", "language": "english", "duration": 2.5,
			"segments": []any{map[string]any{"id": 0, "start": 0, "end": 1.2, "text": " hello"}},
		})
	})
	methods := mediaTestMethods(t, "groq", base+"/openai/v1")
	result, err := methods.Transcribe(context.Background(), providers.TranscribeRequest{
		File: []byte("mp3-bytes"), Filename: "a.mp3", Mime: "audio/mpeg", Language: "en",
	}, providers.Call{ModelID: "whisper-large-v3"})
	if err != nil {
		t.Fatalf("transcribe: %v", err)
	}
	if path != "/openai/v1/audio/transcriptions" {
		t.Fatalf("path = %q", path)
	}
	if seen["file"] != "mp3-bytes" || seen["file.filename"] != "a.mp3" || seen["file.type"] != "audio/mpeg" {
		t.Fatalf("file = %+v", seen)
	}
	mediaTestSame(t, "form", map[string]string{
		"model": "whisper-large-v3", "response_format": "verbose_json", "language": "en",
	}, map[string]string{"model": seen["model"], "response_format": seen["response_format"], "language": seen["language"]})
	if result.Text != "hello world" || result.Language != "english" {
		t.Fatalf("transcript = %+v", result)
	}
	if result.Duration == nil || *result.Duration != 2.5 {
		t.Fatalf("duration = %v", result.Duration)
	}
	if len(result.Segments) != 1 || result.Segments[0].Text != " hello" {
		t.Fatalf("segments = %+v", result.Segments)
	}
}

// TestTranscribeTranslateUsesTheTranslationsPath: a translation is the same
// form at another endpoint, and it drops the language.
func TestTranscribeTranslateUsesTheTranslationsPath(t *testing.T) {
	var path string
	var seen map[string]string
	base := mediaTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		seen = mediaMultipart(t, r)
		mediaTestJSON(t, w, map[string]any{"text": "hello"})
	})
	methods := mediaTestMethods(t, "groq", base+"/openai/v1")
	result, err := methods.Transcribe(context.Background(), providers.TranscribeRequest{
		File: []byte("mp3-bytes"), Filename: "a.mp3", Mime: "audio/mpeg", Language: "de", Translate: true,
	}, providers.Call{ModelID: "whisper-large-v3"})
	if err != nil {
		t.Fatalf("transcribe: %v", err)
	}
	if path != "/openai/v1/audio/translations" {
		t.Fatalf("path = %q", path)
	}
	if _, found := seen["language"]; found {
		t.Fatal("a translation must not carry a language")
	}
	if result.Text != "hello" || result.Language != "de" {
		t.Fatalf("transcript = %+v", result)
	}
}

// TestTranscribeJSONSendsBase64Audio: OpenRouter takes the audio as JSON.
func TestTranscribeJSONSendsBase64Audio(t *testing.T) {
	var sent map[string]any
	base := mediaTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		sent = mediaTestBody(t, r)
		mediaTestJSON(t, w, map[string]any{"text": "hello there", "language": "en", "duration": 1.5})
	})
	methods := mediaTestMethods(t, "openrouter", base+"/api/v1")
	result, err := methods.Transcribe(context.Background(), providers.TranscribeRequest{
		File: []byte("audio"), Filename: "clip.mp3", Mime: "audio/mpeg", Prompt: "a hint",
	}, providers.Call{ModelID: "openai/whisper-1"})
	if err != nil {
		t.Fatalf("transcribe: %v", err)
	}
	input := mediaObject(sent["input_audio"])
	if mediaString(input["data"]) != "YXVkaW8=" || mediaString(input["format"]) != "mp3" {
		t.Fatalf("input_audio = %+v", input)
	}
	if mediaString(sent["prompt"]) != "a hint" {
		t.Fatalf("prompt = %v", sent["prompt"])
	}
	if result.Text != "hello there" || result.Language != "en" {
		t.Fatalf("transcript = %+v", result)
	}
}

// TestTranscribeReportsNoTranscript pins the message.
func TestTranscribeReportsNoTranscript(t *testing.T) {
	base := mediaTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		mediaTestJSON(t, w, map[string]any{"text": nil})
	})
	methods := mediaTestMethods(t, "openrouter", base+"/api/v1")
	_, err := methods.Transcribe(context.Background(), providers.TranscribeRequest{File: []byte("audio")}, providers.Call{ModelID: "openai/whisper-1"})
	if kind := mediaTestErrorKind(t, err); kind != providers.KindInvalidOutput {
		t.Fatalf("kind = %v", kind)
	}
	if got := mediaErrorText(err); got != "The provider returned no transcript." {
		t.Fatalf("message = %q", got)
	}
}

// TestAudioFormatReadsTheFilenameThenTheMIME: what OpenRouter's JSON wants.
func TestAudioFormatReadsTheFilenameThenTheMIME(t *testing.T) {
	cases := []struct{ filename, mime, want string }{
		{"a.mp3", "audio/mpeg", "mp3"},
		{"a.mpeg", "audio/mpeg", "mp3"},
		{"", "audio/mpeg", "mp3"},
		{"", "audio/x-wav", "wav"},
		{"", "", "wav"},
		{"clip.WAV", "", "wav"},
	}
	for _, item := range cases {
		if got := mediaAudioFormat(item.filename, item.mime); got != item.want {
			t.Fatalf("mediaAudioFormat(%q, %q) = %q, want %q", item.filename, item.mime, got, item.want)
		}
	}
}

// TestDecideSendsTypeSafesShape: model, state and questions, and the answers.
func TestDecideSendsTypeSafesShape(t *testing.T) {
	var sent map[string]any
	base := mediaTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/systemone" {
			t.Errorf("path = %q", r.URL.Path)
		}
		sent = mediaTestBody(t, r)
		mediaTestJSON(t, w, map[string]any{
			"model": "jev", "answers": map[string]any{"is_spam": true},
			"usage": map[string]any{"prompt_tokens": 20},
		})
	})
	methods := mediaTestMethods(t, "custom_openai", base+"/v1")
	result, err := methods.Decide(context.Background(), providers.DecideRequest{
		State: map[string]any{"text": "hi"}, Questions: map[string]any{"is_spam": "bool?"},
	}, providers.Call{ModelID: "jev"})
	if err != nil {
		t.Fatalf("decide: %v", err)
	}
	if mediaMember(result.Answers, "is_spam") != true {
		t.Fatalf("answers = %v", result.Answers)
	}
	if result.InputTokens == nil || *result.InputTokens != 20 {
		t.Fatalf("input tokens = %v", result.InputTokens)
	}
	if mediaString(sent["model"]) != "jev" {
		t.Fatalf("body = %+v", sent)
	}
}

// TestDecideReportsMissingAnswers pins the message.
func TestDecideReportsMissingAnswers(t *testing.T) {
	base := mediaTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		mediaTestJSON(t, w, map[string]any{"model": "jev"})
	})
	methods := mediaTestMethods(t, "custom_openai", base+"/v1")
	_, err := methods.Decide(context.Background(), providers.DecideRequest{}, providers.Call{ModelID: "jev"})
	if got := mediaErrorText(err); got != "The decision model returned no answers." {
		t.Fatalf("message = %q", got)
	}
	if kind := mediaTestErrorKind(t, err); kind != providers.KindInvalidOutput {
		t.Fatalf("kind = %v", kind)
	}
}

// TestModerateReturnsTheProvidersRecords: the media API returns them unchanged.
func TestModerateReturnsTheProvidersRecords(t *testing.T) {
	var sent map[string]any
	base := mediaTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/moderations" {
			t.Errorf("path = %q", r.URL.Path)
		}
		sent = mediaTestBody(t, r)
		mediaTestJSON(t, w, map[string]any{"results": []any{map[string]any{
			"flagged": true, "categories": map[string]any{"hate": true},
		}}})
	})
	methods := mediaTestMethods(t, "custom_openai", base+"/v1")
	result, err := methods.Moderate(context.Background(), providers.ModerateRequest{Input: []string{"text"}}, providers.Call{ModelID: "omni-moderation-latest"})
	if err != nil {
		t.Fatalf("moderate: %v", err)
	}
	if len(result.Results) != 1 || mediaMember(result.Results[0], "flagged") != true {
		t.Fatalf("results = %v", result.Results)
	}
	mediaTestSame(t, "body", map[string]any{"model": "omni-moderation-latest", "input": []string{"text"}}, sent)
}

// TestModerateReportsAnUnexpectedAnswer pins the message.
func TestModerateReportsAnUnexpectedAnswer(t *testing.T) {
	base := mediaTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		mediaTestJSON(t, w, map[string]any{"results": "none"})
	})
	methods := mediaTestMethods(t, "custom_openai", base+"/v1")
	_, err := methods.Moderate(context.Background(), providers.ModerateRequest{Input: []string{"text"}}, providers.Call{ModelID: "omni-moderation-latest"})
	if got := mediaErrorText(err); got != "The provider returned an unexpected moderation response." {
		t.Fatalf("message = %q", got)
	}
}

// TestVideoJobsAreNamedByTheProvider: create, poll and fetch the file.
func TestVideoJobsAreNamedByTheProvider(t *testing.T) {
	polls := 0
	var created map[string]any
	base := mediaTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/videos":
			created = mediaTestBody(t, r)
			mediaTestJSON(t, w, map[string]any{"id": "vid_1", "object": "video", "status": "queued", "progress": 0})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/videos/vid_1":
			polls++
			status, progress := "in_progress", 50
			if polls >= 2 {
				status, progress = "completed", 100
			}
			mediaTestJSON(t, w, map[string]any{"id": "vid_1", "status": status, "progress": progress})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/videos/vid_1/content":
			w.Header().Set("Content-Type", "video/mp4")
			_, _ = w.Write([]byte("fake-mp4"))
		default:
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
	})
	methods := mediaTestMethods(t, "custom_openai", base+"/v1")
	job, err := methods.CreateVideo(context.Background(), providers.VideoRequest{Prompt: "a cat", Seconds: "8", Size: "1280x720"}, providers.Call{ModelID: "video-1"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if job.UpstreamID != "vid_1" || job.Status != "queued" {
		t.Fatalf("job = %+v", job)
	}
	mediaTestSame(t, "body", map[string]any{
		"model": "video-1", "prompt": "a cat", "seconds": "8", "size": "1280x720",
	}, created)
	first, err := methods.GetVideo(context.Background(), "vid_1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if first.Status != "in_progress" || first.Progress == nil || *first.Progress != 50 {
		t.Fatalf("status = %+v", first)
	}
	second, err := methods.GetVideo(context.Background(), "vid_1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if second.Status != "completed" {
		t.Fatalf("status = %+v", second)
	}
	response, err := methods.VideoContent(context.Background(), "vid_1")
	if err != nil {
		t.Fatalf("content: %v", err)
	}
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if body := mediaString(string(raw)); body != "fake-mp4" {
		t.Fatalf("content = %q", body)
	}
	if got := response.Header.Get("Content-Type"); got != "video/mp4" {
		t.Fatalf("content type = %q", got)
	}
}

// TestVideoDurationIsAWholeNumberOnOpenRouter: quirks.videoDuration sends
// duration instead of OpenAI's seconds.
func TestVideoDurationIsAWholeNumberOnOpenRouter(t *testing.T) {
	var sent map[string]any
	base := mediaTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		sent = mediaTestBody(t, r)
		mediaTestJSON(t, w, map[string]any{"id": "vid_1", "status": "queued"})
	})
	methods := mediaTestMethods(t, "openrouter", base+"/api/v1")
	if _, err := methods.CreateVideo(context.Background(), providers.VideoRequest{Prompt: "a cat", Seconds: "5"}, providers.Call{ModelID: "google/veo"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if sent["duration"] != float64(5) {
		t.Fatalf("duration = %v", sent["duration"])
	}
	if _, found := sent["seconds"]; found {
		t.Fatal("seconds must not be sent when the provider takes a duration")
	}
}

// TestNormalizeVideoStatus folds a provider's job status into the four the
// gateway reports.
func TestNormalizeVideoStatus(t *testing.T) {
	cases := map[string]string{
		"completed": "completed", "succeeded": "completed", "SUCCESS": "completed", "done": "completed",
		"failed": "failed", "error": "failed", "cancelled": "failed", "canceled": "failed",
		"in_progress": "in_progress", "running": "in_progress", "processing": "in_progress",
		"queued": "queued", "": "queued", "something else": "queued",
	}
	for status, want := range cases {
		if got := NormalizeVideoStatus(status); got != want {
			t.Fatalf("NormalizeVideoStatus(%q) = %q, want %q", status, got, want)
		}
	}
}

// TestMediaNoEndpoint: a method the definition does not declare is refused
// without an upstream call.
func TestMediaNoEndpoint(t *testing.T) {
	methods := mediaTestMethods(t, "pollinations", "https://example.test/v1")
	_, err := methods.Speech(context.Background(), providers.SpeechRequest{Input: "hi"}, providers.Call{ModelID: "tts-1"})
	if kind := mediaTestErrorKind(t, err); kind != providers.KindBadRequest {
		t.Fatalf("kind = %v", kind)
	}
	if got := mediaErrorText(err); got != "This provider does not offer speech." {
		t.Fatalf("message = %q", got)
	}
}

// mediaQueryOf is a URL's query, for the tests that check what was asked for.
func mediaQueryOf(t *testing.T, rawURL string) url.Values {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse %q: %v", rawURL, err)
	}
	return parsed.Query()
}
