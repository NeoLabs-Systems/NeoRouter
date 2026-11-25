package adapters

import (
	"bytes"
	"context"
	"math"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/NeoLabs-Systems/neorouter/internal/providers"
)

// ElevenLabs: text-to-speech (/v1/text-to-speech/{voice_id}) and Scribe
// speech-to-text (/v1/speech-to-text). /v1/models lists the speech models;
// Scribe is not listed there, so the definition adds it.

// ElevenLabsDefaultVoice is Eldrin, one of ElevenLabs' current default voices;
// it is only used when the account's voice lists cannot be read. (The older
// premade voices such as George exist only on accounts from before March 2026
// and stop working at the end of 2026, so they are never assumed.)
const ElevenLabsDefaultVoice = "6WwXjDDEMyNmFG95zycZ"

// elevenLabsOpenAIGender maps OpenAI voice names onto one of the account's
// premade voices with this voice type. Fixed voice ids are not used: older
// premade voices are Voice Library voices on newer accounts, which free
// accounts cannot use through the API.
var elevenLabsOpenAIGender = map[string]string{
	"alloy": "female", "echo": "male", "fable": "male", "onyx": "male", "nova": "female",
	"shimmer": "female", "ash": "male", "coral": "female", "sage": "female", "ballad": "male",
	"verse": "male",
}

// elevenLabsFormat is what one requested format becomes upstream.
type elevenLabsFormat struct {
	outputFormat string
	mime         string
}

// elevenLabsFormats are the requested formats ElevenLabs can produce.
var elevenLabsFormats = map[string]elevenLabsFormat{
	"mp3":  {outputFormat: "mp3_44100_128", mime: "audio/mpeg"},
	"opus": {outputFormat: "opus_48000_64", mime: "audio/ogg"},
	"wav":  {outputFormat: "wav_24000", mime: "audio/wav"},
	"pcm":  {outputFormat: "pcm_24000", mime: "audio/pcm"},
}

// elevenLabsVoiceCacheTTL is how long the account's voice lists are trusted.
const elevenLabsVoiceCacheTTL = 10 * time.Minute

// elevenLabsWordsPerCue is how many words a subtitle cue holds at most.
const elevenLabsWordsPerCue = 14

// elevenLabsFullVoiceID matches a complete voice id, which is used as given.
var elevenLabsFullVoiceID = regexp.MustCompile(`^[A-Za-z0-9]{20}$`)

// elevenLabsLibraryVoice is how ElevenLabs refuses a Voice Library voice.
var elevenLabsLibraryVoice = regexp.MustCompile(`(?i)library voice`)

// elevenLabsSentenceEnd ends a subtitle cue.
var elevenLabsSentenceEnd = regexp.MustCompile(`[.!?]$`)

// elevenLabsVoice is one voice of an account: the parts NeoRouter reads.
type elevenLabsVoice struct {
	ID       string
	Name     string
	Category string
	Gender   string
}

// usable reports whether every plan can speak with this voice: default and
// premade voices, and the ones the account made itself. Voice Library voices
// need a paid plan.
func (v elevenLabsVoice) usable() bool {
	return v.Category == "premade" || v.Category == "cloned" || v.Category == "generated"
}

// ElevenLabsWord is one word timestamp of a transcript.
type ElevenLabsWord struct {
	Start float64
	End   float64
	Text  string
	Type  string
}

// Cues builds subtitle cues from word timestamps: a new cue after a sentence
// end, a pause of more than a second, or fourteen words.
func Cues(words []ElevenLabsWord) []providers.Segment {
	segments := make([]providers.Segment, 0, len(words)/elevenLabsWordsPerCue+1)
	type cue struct {
		segment providers.Segment
		count   int
	}
	current := cue{}
	open := false
	for _, word := range words {
		if word.Type == "spacing" || word.Type == "audio_event" {
			continue
		}
		if !open || word.Start-current.segment.End > 1 || current.count >= elevenLabsWordsPerCue || elevenLabsSentenceEnd.MatchString(current.segment.Text) {
			if open {
				segments = append(segments, current.segment)
			}
			current = cue{segment: providers.Segment{Start: word.Start, End: word.End, Text: word.Text}, count: 1}
			open = true
			continue
		}
		current.segment.Text += " " + word.Text
		current.segment.End = word.End
		current.count++
	}
	if open {
		segments = append(segments, current.segment)
	}
	return segments
}

// ElevenLabsAdapter is one ElevenLabs account: speech and transcription.
type ElevenLabsAdapter struct {
	*mediaBase
	mu       sync.Mutex
	own      *elevenLabsVoiceCache
	defaults *elevenLabsVoiceCache
}

// elevenLabsVoiceCache is one cached listing of the account's voices.
type elevenLabsVoiceCache struct {
	at     time.Time
	voices []elevenLabsVoice
}

// NewElevenLabsAdapter builds the ElevenLabs adapter.
func NewElevenLabsAdapter(def providers.Definition, deps providers.AdapterDeps) *ElevenLabsAdapter {
	return &ElevenLabsAdapter{mediaBase: newMediaBase(def, deps)}
}

// Validate checks the key against the models listing.
func (e *ElevenLabsAdapter) Validate(ctx context.Context) (providers.Validation, error) {
	return e.validateDefault(ctx)
}

// ListModels lists the speech models, plus the ones the definition adds
// (Scribe, which /v1/models does not name).
func (e *ElevenLabsAdapter) ListModels(ctx context.Context) ([]providers.Model, error) {
	response, err := e.doGet(ctx, "/models", providers.Call{Timeout: 30 * time.Second})
	if err != nil {
		return nil, err
	}
	speech := make([]providers.Model, 0, len(mediaList(response.Data)))
	for _, entry := range mediaList(response.Data) {
		if value, ok := mediaMember(entry, "can_do_text_to_speech").(bool); ok && !value {
			continue
		}
		kind := providers.KindSpeech
		speech = append(speech, providers.NormalizeModel(map[string]any{
			"id":   mediaMember(entry, "model_id"),
			"name": mediaMember(entry, "name"),
		}, providers.ModelOverrides{Kind: &kind}))
	}
	for _, extra := range e.def.ExtraModels {
		kind := extra.Type
		if kind == "" {
			kind = providers.KindOther
		}
		speech = append(speech, providers.NormalizeModel(map[string]any{
			"id": extra.ID, "name": extra.Name,
		}, providers.ModelOverrides{Kind: &kind}))
	}
	return speech, nil
}

// FetchQuota reports nothing: ElevenLabs publishes no quota endpoint.
func (e *ElevenLabsAdapter) FetchQuota(context.Context) ([]providers.QuotaWindow, error) {
	return nil, nil
}

// Supports reports the two methods ElevenLabs serves.
func (e *ElevenLabsAdapter) Supports(method providers.Capability) bool {
	return method == providers.CapSpeech || method == providers.CapTranscribe
}

// TranscribeTranslate is false: Scribe does not translate.
func (e *ElevenLabsAdapter) TranscribeTranslate() bool { return false }

// TranscribeTimestamps is true: Scribe answers with word timings.
func (e *ElevenLabsAdapter) TranscribeTimestamps() bool { return true }

// voicesOf reads the voices of one listing answer.
func (e *ElevenLabsAdapter) voicesOf(response *providers.Response) []elevenLabsVoice {
	entries := mediaList(mediaMember(response.Data, "voices"))
	voices := make([]elevenLabsVoice, 0, len(entries))
	for _, entry := range entries {
		voices = append(voices, elevenLabsVoice{
			ID:       mediaString(mediaMember(entry, "voice_id")),
			Name:     mediaString(mediaMember(entry, "name")),
			Category: mediaString(mediaMember(entry, "category")),
			Gender:   mediaString(mediaMember(mediaMember(entry, "labels"), "gender")),
		})
	}
	return voices
}

// accountVoices is the account's own voices, cached for a while.
func (e *ElevenLabsAdapter) accountVoices(ctx context.Context) []elevenLabsVoice {
	e.mu.Lock()
	cached := e.own
	e.mu.Unlock()
	if cached != nil && cached.at.After(time.Now().Add(-elevenLabsVoiceCacheTTL)) {
		return cached.voices
	}
	var voices []elevenLabsVoice
	if response, err := e.doGet(ctx, "/voices", providers.Call{Timeout: 15 * time.Second}); err == nil {
		voices = e.voicesOf(response)
	}
	e.mu.Lock()
	e.own = &elevenLabsVoiceCache{at: time.Now(), voices: voices}
	e.mu.Unlock()
	return voices
}

// defaultVoices are ElevenLabs' current default voices (GET /v2/voices
// ?voice_type=default), cached.
func (e *ElevenLabsAdapter) defaultVoices(ctx context.Context) []elevenLabsVoice {
	e.mu.Lock()
	cached := e.defaults
	e.mu.Unlock()
	if cached != nil && cached.at.After(time.Now().Add(-elevenLabsVoiceCacheTTL)) {
		return cached.voices
	}
	root := strings.TrimSuffix(e.baseURL, "/v1")
	var voices []elevenLabsVoice
	path := root + "/v2/voices?voice_type=default&page_size=100"
	if response, err := e.doGet(ctx, path, providers.Call{Timeout: 15 * time.Second}); err == nil {
		voices = e.voicesOf(response)
	}
	e.mu.Lock()
	e.defaults = &elevenLabsVoiceCache{at: time.Now(), voices: voices}
	e.mu.Unlock()
	return voices
}

// usableVoices are the voices every plan can use: the defaults first, then the
// account's own premade and made voices.
func (e *ElevenLabsAdapter) usableVoices(ctx context.Context) []elevenLabsVoice {
	defaults := e.defaultVoices(ctx)
	own := e.accountVoices(ctx)
	voices := make([]elevenLabsVoice, 0, len(defaults)+len(own))
	seen := make(map[string]bool, len(defaults)+len(own))
	add := func(voice elevenLabsVoice) {
		if voice.ID == "" || seen[voice.ID] {
			return
		}
		seen[voice.ID] = true
		voices = append(voices, voice)
	}
	for _, voice := range defaults {
		add(voice)
	}
	for _, voice := range own {
		if voice.usable() {
			add(voice)
		}
	}
	return voices
}

// defaultVoiceID is the account's first usable voice, or ElevenLabs' default.
func (e *ElevenLabsAdapter) defaultVoiceID(ctx context.Context) string {
	if voices := e.usableVoices(ctx); len(voices) > 0 && voices[0].ID != "" {
		return voices[0].ID
	}
	return ElevenLabsDefaultVoice
}

// VoiceID resolves the voice a request names: voice ids and names of the
// account's voices are used as given, OpenAI voice names get a premade voice of
// the same type, and anything else the default.
func (e *ElevenLabsAdapter) VoiceID(ctx context.Context, voice string) string {
	value := strings.TrimSpace(voice)
	if value == "" {
		return e.defaultVoiceID(ctx)
	}
	if elevenLabsFullVoiceID.MatchString(value) {
		return value
	}
	own := e.accountVoices(ctx)
	usable := e.usableVoices(ctx)
	lower := strings.ToLower(value)
	for _, candidate := range append(append([]elevenLabsVoice(nil), usable...), own...) {
		name := strings.ToLower(candidate.Name)
		if name == lower || strings.HasPrefix(name, lower+" ") {
			return candidate.ID
		}
	}
	if gender, found := elevenLabsOpenAIGender[lower]; found {
		for _, candidate := range usable {
			if strings.ToLower(candidate.Gender) == gender {
				return candidate.ID
			}
		}
	}
	if len(usable) > 0 && usable[0].ID != "" {
		return usable[0].ID
	}
	return ElevenLabsDefaultVoice
}

// Voices lists the account's voices, premade and its own.
func (e *ElevenLabsAdapter) Voices(ctx context.Context, _ string) ([]providers.Voice, error) {
	response, err := e.doGet(ctx, "/voices", providers.Call{Timeout: 15 * time.Second})
	if err != nil {
		return nil, err
	}
	voices := make([]providers.Voice, 0, len(mediaList(mediaMember(response.Data, "voices"))))
	for _, voice := range e.voicesOf(response) {
		voices = append(voices, providers.Voice{ID: voice.ID, Name: voice.Name})
	}
	return voices, nil
}

// Speech speaks a text with the resolved voice, retrying once with the
// account's own premade voice when a Voice Library voice is refused on a free
// plan.
func (e *ElevenLabsAdapter) Speech(ctx context.Context, req providers.SpeechRequest, call providers.Call) (*providers.SpeechResult, error) {
	format := req.Format
	if format == "" {
		format = "mp3"
	}
	target, found := elevenLabsFormats[format]
	if !found {
		target = elevenLabsFormats["mp3"]
	}
	body := map[string]any{"text": req.Input, "model_id": call.ModelID}
	if req.Speed != nil && *req.Speed != 0 {
		body["voice_settings"] = map[string]any{
			"speed": math.Min(1.2, math.Max(0.7, *req.Speed)),
		}
	}
	say := func(voiceID string) (mediaBytes, error) {
		path := "/text-to-speech/" + mediaEncodeURIComponent(voiceID) + "?output_format=" + target.outputFormat
		return e.fetchBytes(ctx, path, providers.Request{
			Method: http.MethodPost, JSON: body, Headers: mediaHeader("Accept", target.mime),
		}, call)
	}
	voiceID := e.VoiceID(ctx, req.Voice)
	audio, err := say(voiceID)
	if err != nil {
		if !elevenLabsRefusedLibraryVoice(err) {
			return nil, err
		}
		// A Voice Library voice on a free plan: the account's own premade voice
		// instead. Still refused means this voice, not the account, is the problem.
		fallback := e.defaultVoiceID(ctx)
		if fallback == voiceID {
			return nil, providers.NewError(providers.KindBadRequest,
				"This ElevenLabs voice is from the Voice Library, which free accounts cannot use through the API.", providers.ErrorOptions{})
		}
		audio, err = say(fallback)
		if err != nil {
			if elevenLabsRefusedLibraryVoice(err) {
				return nil, providers.NewError(providers.KindBadRequest,
					"ElevenLabs refused its own premade voice as a Voice Library voice; pick one of your voices.", providers.ErrorOptions{})
			}
			return nil, err
		}
	}
	if len(audio.Data) == 0 {
		return nil, providers.NewError(providers.KindEmpty, "ElevenLabs returned no audio.", providers.ErrorOptions{})
	}
	return &providers.SpeechResult{Audio: audio.Data, Mime: target.mime, Headers: audio.Headers}, nil
}

// elevenLabsRefusedLibraryVoice reports a payment refusal that names a Voice
// Library voice: a plan limit on the voice, not on the account.
func elevenLabsRefusedLibraryVoice(err error) bool {
	classified := providers.AsProviderError(err)
	return classified != nil && classified.Kind == providers.KindPayment &&
		elevenLabsLibraryVoice.MatchString(classified.Message)
}

// Transcribe uploads the audio to Scribe and builds subtitle cues from the word
// timings.
func (e *ElevenLabsAdapter) Transcribe(ctx context.Context, req providers.TranscribeRequest, call providers.Call) (*providers.Transcript, error) {
	filename := req.Filename
	if filename == "" {
		filename = "audio"
	}
	mime := req.Mime
	if mime == "" {
		mime = "application/octet-stream"
	}
	form := newMediaForm()
	form.field("model_id", call.ModelID)
	form.file("file", filename, mime, req.File)
	form.field("timestamps_granularity", "word")
	if req.Language != "" {
		form.field("language_code", req.Language)
	}
	body, contentType := form.close()
	response, err := e.do(ctx, "/speech-to-text", providers.Request{
		Method: http.MethodPost, Body: bytes.NewReader(body), ContentType: contentType,
	}, call)
	if err != nil {
		return nil, err
	}
	text, ok := mediaMember(response.Data, "text").(string)
	if !ok {
		return nil, mediaInvalidOutput("ElevenLabs returned no transcript.")
	}
	words := make([]ElevenLabsWord, 0, len(mediaList(mediaMember(response.Data, "words"))))
	for _, entry := range mediaList(mediaMember(response.Data, "words")) {
		word := ElevenLabsWord{Text: mediaString(mediaMember(entry, "text")), Type: mediaString(mediaMember(entry, "type"))}
		if start := mediaNumber(mediaMember(entry, "start")); start != nil {
			word.Start = *start
		}
		if end := mediaNumber(mediaMember(entry, "end")); end != nil {
			word.End = *end
		}
		words = append(words, word)
	}
	var duration *float64
	if len(words) > 0 {
		end := words[len(words)-1].End
		duration = &end
	}
	segments := Cues(words)
	if len(segments) == 0 {
		segments = nil
	}
	return &providers.Transcript{
		Text:     text,
		Language: mediaFirstString(mediaString(mediaMember(response.Data, "language_code")), req.Language),
		Duration: duration,
		Segments: segments,
		Headers:  response.Headers,
	}, nil
}
