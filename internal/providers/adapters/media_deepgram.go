package adapters

import (
	"bytes"
	"context"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/NeoLabs-Systems/neorouter/internal/providers"
)

// Deepgram: Nova speech-to-text (/v1/listen, raw audio upload) and Aura
// text-to-speech (/v1/speak). Aura voices are separate model ids
// ("aura-2-thalia-en"); the catalog lists one Aura model and the request's
// voice picks the speaker.

// AuraVoices are the voices Deepgram's Aura models speak with.
var AuraVoices = []string{
	"thalia", "andromeda", "helena", "apollo", "arcas", "aries", "asteria", "athena", "atlas", "aurora",
	"callista", "cora", "cordelia", "delia", "draco", "electra", "harmonia", "hera", "hermes", "hyperion",
	"iris", "janus", "juno", "jupiter", "luna", "mars", "minerva", "neptune", "odysseus", "ophelia",
	"orion", "orpheus", "pandora", "phoebe", "pluto", "saturn", "selene", "theia", "vesta", "zeus",
	"amalthea",
}

// auraVoices is AuraVoices as a set.
var auraVoices = func() map[string]bool {
	set := make(map[string]bool, len(AuraVoices))
	for _, voice := range AuraVoices {
		set[voice] = true
	}
	return set
}()

// auraFromOpenAI maps OpenAI's voice names onto Aura's speakers.
var auraFromOpenAI = map[string]string{
	"alloy": "thalia", "echo": "orion", "fable": "apollo", "onyx": "zeus", "nova": "luna",
	"shimmer": "asteria", "ash": "arcas", "coral": "cora", "sage": "athena", "ballad": "helena",
	"verse": "hermes",
}

// auraEncoding is what one requested format becomes upstream.
type auraEncoding struct {
	encoding   string
	container  string
	sampleRate string
	mime       string
}

// auraEncodings are the requested formats mapped to Deepgram's encoding and
// container.
var auraEncodings = map[string]auraEncoding{
	"mp3":  {encoding: "mp3", mime: "audio/mpeg"},
	"opus": {encoding: "opus", container: "ogg", mime: "audio/ogg"},
	"wav":  {encoding: "linear16", container: "wav", mime: "audio/wav"},
	"pcm":  {encoding: "linear16", container: "none", sampleRate: "24000", mime: "audio/pcm"},
	"flac": {encoding: "flac", mime: "audio/flac"},
	"aac":  {encoding: "aac", mime: "audio/aac"},
}

// auraFullModel matches a complete Aura model id such as aura-2-julius-de.
var auraFullModel = regexp.MustCompile(`^aura-`)

// AuraModel is the model a voice speaks with: a full model id is passed on, an
// Aura voice names its own model, an OpenAI voice maps onto the Aura voice of
// that name, and anything else is Aura's default speaker.
func AuraModel(family, voice string) string {
	value := strings.ToLower(voice)
	if auraFullModel.MatchString(value) {
		return value
	}
	name := "thalia"
	if auraVoices[value] {
		name = value
	} else if mapped, found := auraFromOpenAI[value]; found {
		name = mapped
	}
	return family + "-" + name + "-en"
}

// novaKeyTerm matches the Nova-3 model ids that take key terms.
var novaKeyTerm = regexp.MustCompile(`nova-3`)

// keyTermSplit splits a prompt into the words a Nova-3 request can carry.
var keyTermSplit = regexp.MustCompile(`[\s,]+`)

// DeepgramAdapter is one Deepgram account: speech and transcription.
type DeepgramAdapter struct {
	*mediaBase
}

// NewDeepgramAdapter builds the Deepgram adapter. Deepgram wants its key in a
// Token header, not a Bearer one.
func NewDeepgramAdapter(def providers.Definition, deps providers.AdapterDeps) *DeepgramAdapter {
	base := newMediaBase(def, deps)
	base.authPrefix = "Token"
	return &DeepgramAdapter{mediaBase: base}
}

// Validate checks the key against Deepgram's projects listing.
func (d *DeepgramAdapter) Validate(ctx context.Context) (providers.Validation, error) {
	return d.validateDefault(ctx)
}

// ListModels is the definition's fixed listing.
func (d *DeepgramAdapter) ListModels(context.Context) ([]providers.Model, error) {
	return d.staticModels(func(entry map[string]any) providers.ModelOverrides {
		kind := mediaString(entry["kind"])
		return providers.ModelOverrides{Kind: &kind}
	}), nil
}

// FetchQuota reports nothing: Deepgram has no quota endpoint NeoRouter reads.
func (d *DeepgramAdapter) FetchQuota(context.Context) ([]providers.QuotaWindow, error) {
	return nil, nil
}

// Supports reports the two methods Deepgram serves.
func (d *DeepgramAdapter) Supports(method providers.Capability) bool {
	return method == providers.CapSpeech || method == providers.CapTranscribe
}

// TranscribeTranslate is false: Deepgram cannot translate speech into English.
func (d *DeepgramAdapter) TranscribeTranslate() bool { return false }

// TranscribeTimestamps is true: Aura answers with utterances.
func (d *DeepgramAdapter) TranscribeTimestamps() bool { return true }

// Voices lists Aura's speakers.
func (d *DeepgramAdapter) Voices(context.Context, string) ([]providers.Voice, error) {
	return VoiceList(AuraVoices), nil
}

// Speech speaks a text with the Aura voice the request names.
func (d *DeepgramAdapter) Speech(ctx context.Context, req providers.SpeechRequest, call providers.Call) (*providers.SpeechResult, error) {
	format := req.Format
	if format == "" {
		format = "mp3"
	}
	target, found := auraEncodings[format]
	if !found {
		target = auraEncodings["mp3"]
	}
	params := url.Values{"model": {AuraModel(call.ModelID, req.Voice)}, "encoding": {target.encoding}}
	if target.container != "" {
		params.Set("container", target.container)
	}
	if target.sampleRate != "" {
		params.Set("sample_rate", target.sampleRate)
	}
	if req.Speed != nil && *req.Speed != 0 {
		params.Set("speed", mediaString(*req.Speed))
	}
	path := "/speak?" + params.Encode()
	audio, err := d.fetchBytes(ctx, path, providers.Request{
		Method: http.MethodPost, JSON: map[string]any{"text": req.Input},
	}, call)
	if err != nil {
		return nil, err
	}
	if len(audio.Data) == 0 {
		return nil, providers.NewError(providers.KindEmpty, "Deepgram returned no audio.", providers.ErrorOptions{})
	}
	return &providers.SpeechResult{Audio: audio.Data, Mime: target.mime, Headers: audio.Headers}, nil
}

// Transcribe uploads the audio as it is and reads Deepgram's transcript.
func (d *DeepgramAdapter) Transcribe(ctx context.Context, req providers.TranscribeRequest, call providers.Call) (*providers.Transcript, error) {
	params := url.Values{
		"model":        {call.ModelID},
		"smart_format": {"true"},
		"utterances":   {"true"},
		"punctuate":    {"true"},
	}
	if req.Language != "" {
		params.Set("language", req.Language)
	} else {
		params.Set("detect_language", "true")
	}
	// Nova-3 takes up to 100 key terms as hints; the prompt's words serve as such.
	if req.Prompt != "" && novaKeyTerm.MatchString(call.ModelID) {
		terms := make([]string, 0, 20)
		for _, term := range keyTermSplit.Split(req.Prompt, -1) {
			if term == "" || len(terms) >= 20 {
				continue
			}
			terms = append(terms, term)
		}
		for _, term := range terms {
			params.Add("keyterm", term)
		}
	}
	mime := req.Mime
	if mime == "" {
		mime = "application/octet-stream"
	}
	response, err := d.do(ctx, "/listen?"+params.Encode(), providers.Request{
		Method: http.MethodPost, Body: bytes.NewReader(req.File), ContentType: mime,
	}, call)
	if err != nil {
		return nil, err
	}
	results := mediaMember(response.Data, "results")
	channel := mediaFirst(mediaMember(results, "channels"))
	transcript, ok := mediaMember(mediaFirst(mediaMember(channel, "alternatives")), "transcript").(string)
	if !ok {
		return nil, mediaInvalidOutput("Deepgram returned no transcript.")
	}
	utterances := mediaList(mediaMember(results, "utterances"))
	segments := make([]providers.Segment, 0, len(utterances))
	for _, item := range utterances {
		segment := providers.Segment{Text: mediaString(mediaMember(item, "transcript"))}
		if start := mediaNumber(mediaMember(item, "start")); start != nil {
			segment.Start = *start
		}
		if end := mediaNumber(mediaMember(item, "end")); end != nil {
			segment.End = *end
		}
		segments = append(segments, segment)
	}
	if len(segments) == 0 {
		segments = nil
	}
	return &providers.Transcript{
		Text:     transcript,
		Language: mediaFirstString(mediaString(mediaMember(channel, "detected_language")), req.Language),
		Duration: mediaNumber(mediaMember(mediaMember(response.Data, "metadata"), "duration")),
		Segments: segments,
		Headers:  response.Headers,
	}, nil
}
