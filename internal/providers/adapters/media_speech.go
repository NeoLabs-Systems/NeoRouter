package adapters

import (
	"context"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/NeoLabs-Systems/neorouter/internal/providers"
)

// Speech and the voice lists a provider offers (adapters/openai_media.js
// voices/speech).

// OpenAIVoices are OpenAI's voice names. A provider that lists none of its own
// answers these, because they are what an OpenAI-compatible client sends.
var OpenAIVoices = []string{
	"alloy", "ash", "ballad", "coral", "echo", "fable", "nova", "onyx", "sage", "shimmer", "verse",
}

// VoiceList turns voice ids into the records the playground and the catalog
// report, capitalized the way OpenAI names its voices.
func VoiceList(ids []string) []providers.Voice {
	voices := make([]providers.Voice, 0, len(ids))
	for _, id := range ids {
		voices = append(voices, providers.Voice{ID: id, Name: mediaCapitalize(id)})
	}
	return voices
}

// mediaCapitalize upper-cases the first character, as charAt(0).toUpperCase()
// plus the rest does.
func mediaCapitalize(text string) string {
	if text == "" {
		return text
	}
	runes := []rune(text)
	return string(unicode.ToUpper(runes[0])) + string(runes[1:])
}

// Voices lists the voices this provider speaks for a model: the ones its model
// listing names, its own fixed list, or OpenAI's names.
func (m *MediaMethods) Voices(ctx context.Context, modelID string) ([]providers.Voice, error) {
	if m.def.Quirks.SpeechAPI == "riva" {
		return VoiceList(RivaVoices(modelID)), nil
	}
	voices, _ := m.speechVoices(ctx, providers.Call{ModelID: modelID})
	switch {
	case len(voices) > 0:
		return VoiceList(voices), nil
	case m.def.Quirks.SpeechVoicesPath != "":
		return VoiceList(nil), nil
	default:
		return VoiceList(OpenAIVoices), nil
	}
}

// Speech synthesizes one text. A provider that caps the text of a request gets
// it in sentence-sized pieces, joined back into one WAV; raw PCM is wrapped into
// a WAV file unless PCM was asked for.
func (m *MediaMethods) Speech(ctx context.Context, req providers.SpeechRequest, call providers.Call) (*providers.SpeechResult, error) {
	quirks := m.def.Quirks
	path, ok := MediaPath(m.def, providers.CapSpeech, call.ModelID)
	if !ok {
		return nil, mediaNoEndpoint(providers.CapSpeech)
	}
	format := req.Format
	if format == "" {
		format = "mp3"
	}
	if quirks.SpeechAPI == "riva" {
		return rivaSpeech(ctx, m.mediaBase, path, req, call)
	}
	// A voice the model does not have becomes its default voice. A model whose
	// listing names no voices keeps the requested voice (or none), never an
	// OpenAI name the provider may not know.
	voices, defaultVoice := m.speechVoices(ctx, call)
	voice := ""
	if len(voices) > 0 {
		voice = mediaKnownVoice(voices, req.Voice)
		if voice == "" {
			voice = defaultVoice
		}
		if voice == "" {
			voice = voices[0]
		}
	} else if req.Voice != "" {
		voice = req.Voice
	} else if defaultVoice != "" {
		voice = defaultVoice
	} else if quirks.SpeechVoicesPath == "" {
		voice = "alloy"
	}
	// A format the provider can produce (the gateway converts when it can).
	chosenFormat := format
	if len(quirks.SpeechFormats) > 0 && !mediaContains(quirks.SpeechFormats, format) {
		chosenFormat = quirks.SpeechFormats[0]
	}
	// Hosts that cap the text of one request get it in sentence-sized parts.
	pieces := []string{req.Input}
	if quirks.MaxSpeechChars > 0 && mediaLength(req.Input) > quirks.MaxSpeechChars {
		pieces = SpeechChunks(req.Input, quirks.MaxSpeechChars)
	}
	if len(pieces) == 0 {
		// Text with nothing to say (only whitespace) is still one request.
		pieces = []string{req.Input}
	}
	parts := make([]mediaSpeechPart, 0, len(pieces))
	for _, piece := range pieces {
		part, err := m.synthesize(ctx, path, piece, voice, req.Speed, chosenFormat, call)
		if err != nil {
			return nil, err
		}
		parts = append(parts, part)
	}
	first := parts[0]
	if mediaRawPCM(first) {
		// Raw PCM (OpenRouter's default) has no header: wrapped into WAV unless
		// PCM was asked for.
		pcm := make([]byte, 0, len(first.Data)*len(parts))
		for _, part := range parts {
			pcm = append(pcm, part.Data...)
		}
		if format == "pcm" {
			return &providers.SpeechResult{Audio: pcm, Mime: first.Mime, Headers: first.Headers}, nil
		}
		return &providers.SpeechResult{Audio: PCMToWAV(pcm, mediaPCMRate(first.Mime)), Mime: "audio/wav", Headers: first.Headers}, nil
	}
	if len(parts) == 1 {
		return &providers.SpeechResult{Audio: first.Data, Mime: first.Mime, Headers: first.Headers}, nil
	}
	buffers := make([][]byte, 0, len(parts))
	for _, part := range parts {
		buffers = append(buffers, part.Data)
	}
	joined, err := JoinWAV(buffers)
	if err != nil {
		return nil, err
	}
	return &providers.SpeechResult{Audio: joined, Mime: "audio/wav", Headers: first.Headers}, nil
}

// mediaKnownVoice is the voice of the list that matches name, ignoring case.
func mediaKnownVoice(voices []string, name string) string {
	lower := strings.ToLower(name)
	for _, voice := range voices {
		if strings.ToLower(voice) == lower {
			return voice
		}
	}
	return ""
}

// mediaContains reports whether list holds value.
func mediaContains(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}

// mediaLength is a string's length in Unicode code points, the unit the speech
// cap and the chunker count in.
func mediaLength(text string) int { return len([]rune(text)) }

// mediaSpeechPart is one synthesized piece of a spoken text.
type mediaSpeechPart struct {
	Data    []byte
	Mime    string
	Headers http.Header
	Format  string
}

// synthesize speaks one piece. A host that can produce several formats but
// refuses the chosen one for this model (OpenRouter: HeyGen only PCM, MiniMax
// only MP3) is asked once more for its other format.
func (m *MediaMethods) synthesize(ctx context.Context, path, input, voice string, speed *float64, format string, call providers.Call) (mediaSpeechPart, error) {
	send := func(responseFormat string) (mediaSpeechPart, error) {
		body := map[string]any{"model": call.ModelID, "input": input, "response_format": responseFormat}
		if voice != "" {
			body["voice"] = voice
		}
		if speed != nil && *speed != 0 {
			body["speed"] = *speed
		}
		audio, err := m.fetchBytes(ctx, path, providers.Request{Method: http.MethodPost, JSON: body}, call)
		if err != nil {
			return mediaSpeechPart{}, err
		}
		if len(audio.Data) == 0 {
			return mediaSpeechPart{}, providers.NewError(providers.KindEmpty, "The provider returned no audio.", providers.ErrorOptions{})
		}
		return mediaSpeechPart{Data: audio.Data, Mime: audio.Mime, Headers: audio.Headers, Format: responseFormat}, nil
	}
	other := ""
	for _, candidate := range m.def.Quirks.SpeechFormats {
		if candidate != format {
			other = candidate
			break
		}
	}
	part, err := send(format)
	if other == "" || providers.KindOf(err) != providers.KindBadRequest {
		return part, err
	}
	return send(other)
}

// mediaPCMPattern is audio a provider sends without a container.
var (
	mediaPCMMime  = regexp.MustCompile(`(?i)pcm|l16`)
	mediaNotRaw   = regexp.MustCompile(`(?i)wav|mpeg|mp3|ogg|opus|aac|flac`)
	mediaRatePart = regexp.MustCompile(`(?i)rate=(\d+)`)
)

// mediaPCMSampleRate is what PCM is assumed to be sampled at when the content
// type does not say: OpenAI and OpenRouter send 24 kHz.
const mediaPCMSampleRate = 24000

// mediaRawPCM reports whether a piece is headerless PCM.
func mediaRawPCM(part mediaSpeechPart) bool {
	if mediaPCMMime.MatchString(part.Mime) {
		return true
	}
	return part.Format == "pcm" && !mediaNotRaw.MatchString(part.Mime)
}

// mediaPCMRate reads the sample rate a PCM content type names.
func mediaPCMRate(mime string) int {
	if match := mediaRatePart.FindStringSubmatch(mime); match != nil {
		rate := 0
		for _, digit := range match[1] {
			rate = rate*10 + int(digit-'0')
		}
		if rate > 0 {
			return rate
		}
	}
	return mediaPCMSampleRate
}

// mediaListedVoicesTTL is how long a provider's voice listing is trusted, and
// the shorter retry after a listing that could not be read.
const (
	mediaListedVoicesTTL      = time.Hour
	mediaListedVoicesRetryTTL = time.Minute
)

// mediaVoiceListings caches per-model voices by listing URL, as the JS module
// did with a module-level map: one listing serves every account that points at
// it, and a listing that failed is retried sooner.
var mediaVoiceListings = struct {
	sync.Mutex
	entries map[string]mediaVoiceListing
}{entries: make(map[string]mediaVoiceListing)}

// mediaVoiceListing is one cached listing.
type mediaVoiceListing struct {
	until   time.Time
	byModel map[string][]string
}

// mediaListedVoices reads a provider's model listing for the voices each model
// supports (OpenRouter's `supported_voices`), cached per listing URL.
func (m *MediaMethods) mediaListedVoices(ctx context.Context, path string) map[string][]string {
	url := m.url(path)
	mediaVoiceListings.Lock()
	cached, found := mediaVoiceListings.entries[url]
	mediaVoiceListings.Unlock()
	if found && cached.until.After(time.Now()) {
		return cached.byModel
	}
	byModel := make(map[string][]string)
	response, err := m.doGet(ctx, path, providers.Call{Timeout: 30 * time.Second})
	until := time.Now().Add(mediaListedVoicesRetryTTL)
	if err == nil {
		for _, entry := range mediaList(mediaMember(response.Data, "data")) {
			id, _ := mediaMember(entry, "id").(string)
			voices := mediaList(mediaMember(entry, "supported_voices"))
			if id == "" || voices == nil {
				continue
			}
			byModel[id] = mediaStrings(voices)
		}
		until = time.Now().Add(mediaListedVoicesTTL)
	}
	mediaVoiceListings.Lock()
	mediaVoiceListings.entries[url] = mediaVoiceListing{until: until, byModel: byModel}
	mediaVoiceListings.Unlock()
	return byModel
}

// mediaSpeechVoices is a model's voices and default voice (empty: the first
// one): from the provider's listing, the definition's per-model voices, or its
// fixed list.
func (m *MediaMethods) speechVoices(ctx context.Context, call providers.Call) ([]string, string) {
	quirks := m.def.Quirks
	if quirks.SpeechVoicesPath != "" {
		byModel := m.mediaListedVoices(ctx, quirks.SpeechVoicesPath)
		voices := byModel[call.ModelID]
		if voices == nil {
			voices = byModel[strings.TrimSuffix(call.ModelID, ":free")]
		}
		if len(voices) > 0 {
			return voices, ""
		}
		return nil, ""
	}
	for _, entry := range quirks.ModelVoices {
		pattern, err := regexp.Compile("(?i)" + entry.Match)
		if err != nil || !pattern.MatchString(call.ModelID) {
			continue
		}
		voices := entry.Voices
		if len(voices) == 0 {
			voices = quirks.Voices
		}
		defaultVoice := entry.DefaultVoice
		if defaultVoice == "" {
			defaultVoice = quirks.DefaultVoice
		}
		return voices, defaultVoice
	}
	return quirks.Voices, quirks.DefaultVoice
}

// SpeechChunks splits text into sentence-sized pieces of at most max
// characters. Sentences are packed together while they fit (fewer requests); a
// longer sentence is split between words, and a longer word wherever it must.
func SpeechChunks(text string, max int) []string {
	if max <= 0 {
		return []string{text}
	}
	units := make([]string, 0, 8)
	for _, sentence := range mediaSentences(strings.TrimSpace(text)) {
		if mediaLength(sentence) <= max {
			units = append(units, sentence)
			continue
		}
		current := ""
		for _, word := range strings.Fields(sentence) {
			parts := []string{word}
			if mediaLength(word) > max {
				parts = mediaSplitEvery(word, max)
			}
			for _, part := range parts {
				if current != "" && mediaLength(current)+1+mediaLength(part) > max {
					units = append(units, current)
					current = ""
				}
				if current == "" {
					current = part
				} else {
					current += " " + part
				}
			}
		}
		if current != "" {
			units = append(units, current)
		}
	}
	chunks := make([]string, 0, len(units))
	for _, unit := range units {
		if unit == "" {
			continue
		}
		last := len(chunks) - 1
		if last >= 0 && mediaLength(chunks[last])+1+mediaLength(unit) <= max {
			chunks[last] += " " + unit
			continue
		}
		chunks = append(chunks, unit)
	}
	return chunks
}

// mediaSplitEvery cuts text into pieces of at most max code points.
func mediaSplitEvery(text string, max int) []string {
	runes := []rune(text)
	pieces := make([]string, 0, len(runes)/max+1)
	for start := 0; start < len(runes); start += max {
		end := start + max
		if end > len(runes) {
			end = len(runes)
		}
		pieces = append(pieces, string(runes[start:end]))
	}
	return pieces
}

// mediaSentenceEnd are the marks a sentence is ended with.
const mediaSentenceEnd = ".!?…;:"

// mediaSentences splits text after a sentence-ending mark followed by
// whitespace, the way the JS lookbehind split does. RE2 has no lookbehind, so
// the boundary is found by hand.
func mediaSentences(text string) []string {
	runes := []rune(text)
	sentences := make([]string, 0, 8)
	start := 0
	for i := 0; i < len(runes); i++ {
		if !strings.ContainsRune(mediaSentenceEnd, runes[i]) {
			continue
		}
		next := i + 1
		for next < len(runes) && unicode.IsSpace(runes[next]) {
			next++
		}
		if next == i+1 {
			// No whitespace after the mark: it is not a sentence boundary.
			continue
		}
		sentences = append(sentences, string(runes[start:i+1]))
		start = next
		i = next - 1
	}
	if start < len(runes) {
		sentences = append(sentences, string(runes[start:]))
	}
	return sentences
}
