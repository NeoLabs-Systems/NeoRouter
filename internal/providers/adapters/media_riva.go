package adapters

import (
	"context"
	"net/http"
	"regexp"
	"strings"
	"sync"

	"github.com/NeoLabs-Systems/neorouter/internal/providers"
)

// NVIDIA Riva text-to-speech as OVHcloud AI Endpoints hosts it
// (adapters/riva_speech.js): one host per model (`media.speech` names it with
// {model}), JSON in, 16-bit WAV out.
//
// Riva lists its voices only over gRPC; this HTTP API has no voice listing
// (GET /api/v1/tts/list_voices answers 404), so the voices are derived from the
// language in the model id (nvr-tts-de-de → German, DE):
//   - English is Riva's multi-speaker model, "English-US.Female-1" (the default
//     in OVHcloud's docs) or "English-US.Male-1".
//   - The other languages have one model per voice, "German-DE-Male-1",
//     "Spanish-ES-Female-1", and not every language has both.
//
// A derived voice Riva does not know falls back to the other one, and the voice
// that worked is remembered per model.

// mediaRivaSampleRate is the rate Riva's WAV answers carry.
const mediaRivaSampleRate = 16000

// mediaRivaLinearPCM is Riva's AudioEncoding.LINEAR_PCM.
const mediaRivaLinearPCM = 1

// mediaRivaLanguages is the English name of a language code, as
// Intl.DisplayNames(['en'], { type: 'language' }) gives it. A code that is not
// here keeps its own spelling, which is what Intl does too.
var mediaRivaLanguages = map[string]string{
	"ar": "Arabic", "bg": "Bulgarian", "ca": "Catalan", "cs": "Czech", "cy": "Welsh",
	"da": "Danish", "de": "German", "el": "Greek", "en": "English", "es": "Spanish",
	"et": "Estonian", "fa": "Persian", "fi": "Finnish", "fr": "French", "he": "Hebrew",
	"hi": "Hindi", "hr": "Croatian", "hu": "Hungarian", "id": "Indonesian", "it": "Italian",
	"ja": "Japanese", "ko": "Korean", "lt": "Lithuanian", "lv": "Latvian", "ms": "Malay",
	"nl": "Dutch", "no": "Norwegian", "nb": "Norwegian Bokmål", "pl": "Polish", "pt": "Portuguese",
	"ro": "Romanian", "ru": "Russian", "sk": "Slovak", "sl": "Slovenian", "sr": "Serbian",
	"sv": "Swedish", "th": "Thai", "tr": "Turkish", "uk": "Ukrainian", "vi": "Vietnamese",
	"zh": "Chinese",
}

// mediaRivaModel is a Riva model id's language and region.
var mediaRivaModel = regexp.MustCompile(`(?i)(?:^|[-_/])([a-z]{2,3})-([a-z]{2})$`)

// MediaRivaLanguage reads { language, region } from a Riva model id such as
// nvr-tts-de-de, or reports false.
func MediaRivaLanguage(modelID string) (language string, region string, ok bool) {
	match := mediaRivaModel.FindStringSubmatch(modelID)
	if match == nil {
		return "", "", false
	}
	return strings.ToLower(match[1]), strings.ToUpper(match[2]), true
}

// RivaVoices are the voices derived for a model: female first, as OVHcloud's
// docs default to it.
func RivaVoices(modelID string) []string {
	language, region, ok := MediaRivaLanguage(modelID)
	if !ok {
		return nil
	}
	name, found := mediaRivaLanguages[language]
	if !found {
		name = language
	}
	separator := "-"
	if language == "en" {
		separator = "."
	}
	voices := make([]string, 0, 2)
	for _, speaker := range []string{"Female-1", "Male-1"} {
		voices = append(voices, name+"-"+region+separator+speaker)
	}
	return voices
}

// mediaRivaVoiceName matches a full Riva voice name such as "English-US.Male-1",
// which is passed on as it is. Anything else (an OpenAI or Gemini voice name)
// gets the derived voices.
var mediaRivaVoiceName = regexp.MustCompile(`^[A-Z][a-z]+-[A-Z]{2}[.-]`)

// mediaRivaUnknownVoice is how Riva refuses a voice it does not have.
var mediaRivaUnknownVoice = regexp.MustCompile(`(?i)voice .* not found`)

// mediaRivaWorkingVoices remembers, per endpoint, the voice that worked there.
var mediaRivaWorkingVoices = struct {
	sync.Mutex
	byURL map[string]string
}{byURL: make(map[string]string)}

// mediaRivaVoice returns the remembered voice for an endpoint.
func mediaRivaVoice(url string) string {
	mediaRivaWorkingVoices.Lock()
	defer mediaRivaWorkingVoices.Unlock()
	return mediaRivaWorkingVoices.byURL[url]
}

// mediaRememberRivaVoice records the voice that worked on an endpoint.
func mediaRememberRivaVoice(url, voice string) {
	mediaRivaWorkingVoices.Lock()
	mediaRivaWorkingVoices.byURL[url] = voice
	mediaRivaWorkingVoices.Unlock()
}

// rivaSpeech speaks one text with a Riva voice, falling back to the model's
// other voice when the first one is not available.
func rivaSpeech(ctx context.Context, base *mediaBase, path string, req providers.SpeechRequest, call providers.Call) (*providers.SpeechResult, error) {
	language, region, known := MediaRivaLanguage(call.ModelID)
	if !known {
		return nil, providers.NewError(providers.KindBadRequest,
			"The language of the Riva model "+call.ModelID+" is not known.", providers.ErrorOptions{})
	}
	url := base.url(path)
	say := func(voiceName string) (*providers.SpeechResult, error) {
		body := map[string]any{
			"encoding":       mediaRivaLinearPCM,
			"language_code":  language + "-" + region,
			"sample_rate_hz": mediaRivaSampleRate,
			"text":           req.Input,
			"voice_name":     voiceName,
		}
		audio, err := base.fetchBytes(ctx, path, providers.Request{Method: http.MethodPost, JSON: body}, call)
		if err != nil {
			return nil, err
		}
		if len(audio.Data) == 0 {
			return nil, providers.NewError(providers.KindEmpty, "The provider returned no audio.", providers.ErrorOptions{})
		}
		// The host labels the WAV as application/octet-stream.
		return &providers.SpeechResult{Audio: audio.Data, Mime: "audio/wav", Headers: audio.Headers}, nil
	}
	if mediaRivaVoiceName.MatchString(req.Voice) {
		return say(req.Voice)
	}
	candidates := []string{mediaRivaVoice(url)}
	if candidates[0] == "" {
		candidates = RivaVoices(call.ModelID)
	}
	for index, candidate := range candidates {
		result, err := say(candidate)
		if err == nil {
			mediaRememberRivaVoice(url, candidate)
			return result, nil
		}
		classified := providers.AsProviderError(err)
		unknown := classified != nil && classified.Kind == providers.KindBadRequest &&
			mediaRivaUnknownVoice.MatchString(classified.Message)
		if !unknown || index == len(candidates)-1 {
			return nil, err
		}
	}
	return nil, providers.NewError(providers.KindBadRequest,
		"No voice is known for the Riva model "+call.ModelID+".", providers.ErrorOptions{})
}
