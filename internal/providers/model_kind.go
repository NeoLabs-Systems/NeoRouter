package providers

import (
	"regexp"
	"strings"
)

// What a listed model is for. A listing's declared type is asked first, then
// the model's name, which is all many providers give: a name decides whether a
// model is offered for chat, embeddings, speech, images or video.

// nonChatPatterns detect what a listing entry is for, in order: the first match
// wins. The image entry needs a matcher of its own: "diffusion" alone also
// names text models such as diffusiongemma.
var nonChatPatterns = []struct {
	kind    string
	pattern *regexp.Regexp
	match   func(string) bool
}{
	{kind: KindEmbedding, pattern: regexp.MustCompile(`(?i)embed|bge-|e5-|gte-|nomic-embed|text-similarity|(^|/)voyage-`)},
	{kind: KindRerank, pattern: regexp.MustCompile(`(?i)rerank`)},
	// Audio is split by direction: speech-to-text and text-to-speech.
	{kind: KindTranscription, pattern: regexp.MustCompile(`(?i)whisper|transcri|(^|[-_/])asr([-_]|$)|parakeet|canary-`)},
	{kind: KindSpeech, pattern: regexp.MustCompile(`(?i)tts|text-to-speech|orpheus|playai|melotts|kokoro|voice-design`)},
	// Speech-to-speech sessions over a WebSocket (GPT-Live, Realtime, Gemini Live).
	{kind: KindLive, pattern: regexp.MustCompile(`(?i)(^|[-_/.])live([-_/.]|$)|realtime|native-audio`)},
	{kind: KindOther, pattern: regexp.MustCompile(`(?i)lyria|music|audio`)},
	// Editing models need an input video, which /v1/videos does not take.
	{kind: KindOther, pattern: regexp.MustCompile(`(?i)video-(?:edit|upscale)|(^|/)aleph|detector`)},
	{kind: KindImage, match: matchesDiffusionImage},
	{kind: KindVideo, pattern: regexp.MustCompile(`(?i)\bvideo\b|veo-|sora|wan2|(^|[/-])kling|hailuo`)},
	// Other routers ("openrouter/auto", "kilo-auto/frontier", Jev Router) pick a
	// model themselves at an unpredictable price; NeoRouter does the routing.
	{kind: KindRouter, pattern: regexp.MustCompile(`(?i)(^|/)auto(?:-beta)?$|(^|/)kilo-auto/|jev-router|(^|/)openrouter/free$|(^|/)router$`)},
	{kind: KindModeration, pattern: regexp.MustCompile(`(?i)moderation|llama-guard|shieldgemma|prompt-guard|safeguard|content-safety|nemoguard|safety-guard|topic-control|qwen[\d.]*guard`)},
	// Agents, tool-only and legacy multimodal endpoints that do not accept plain chat.
	{kind: KindOther, pattern: regexp.MustCompile(`(?i)deep-research|computer-use|robotics|antigravity|deplot|kosmos|fuyu|calibration|reward|embedqa|parse|translat|\bocr\b|-ocr|retriever`)},
}

// imagePatterns are the image models besides diffusion ones.
var imagePatterns = regexp.MustCompile(`(?i)dall-e|sdxl|flux|imagen|image-gen|gpt-image|kolors|seedream|nano-banana|-image(-|$)|animerge|lucid-origin|(^|/)phoenix-\d`)

// diffusion names image models, except in diffusiongemma.
const diffusionName = "diffusion"

// DetectKind reports what a listed model is for: chat unless its declared type
// or its name says otherwise.
func DetectKind(id string, raw map[string]any) string {
	// `category` is Pollinations' kind field (text, image, video, embedding, audio, …).
	declared := strings.ToLower(jsString(firstNonNull(
		raw["type"], raw["object_type"], raw["task"], raw["category"], "",
	)))
	switch {
	case strings.Contains(declared, "embed"):
		return KindEmbedding
	case declared == "video":
		return KindVideo
	case declared == "search":
		return KindSearch
	case declared == "weather":
		return KindWeather
	case declared == "live":
		return KindLive
	case strings.Contains(declared, "image") && !strings.Contains(declared, "text"):
		return KindImage
	}
	if truthy(raw["supported_durations"]) || truthy(raw["supported_resolutions"]) {
		return KindVideo
	}
	outputs := stringList(member(member(raw, "architecture"), "output_modalities"))
	if len(outputs) > 0 && !containsString(outputs, "text") {
		switch {
		case containsString(outputs, "embeddings") || containsString(outputs, "embedding"):
			return KindEmbedding
		case containsString(outputs, "video"):
			return KindVideo
		// Decision models answer typed questions: a System One kind.
		case containsString(outputs, "decisions"):
			return KindModeration
		}
		// OpenRouter names text-to-speech output "speech", other listings "audio".
		switch {
		case containsString(outputs, "image"):
			return KindImage
		case containsString(outputs, "audio"), containsString(outputs, "speech"):
			return KindSpeech
		}
		return KindOther
	}
	for _, candidate := range nonChatPatterns {
		if candidate.match != nil {
			if candidate.match(id) {
				return candidate.kind
			}
			continue
		}
		if candidate.pattern.MatchString(id) {
			return candidate.kind
		}
	}
	return KindChat
}

// matchesDiffusionImage reports whether the id names a diffusion image model.
func matchesDiffusionImage(id string) bool {
	if imagePatterns.MatchString(id) {
		return true
	}
	lower := strings.ToLower(id)
	for offset := 0; offset < len(lower); {
		index := strings.Index(lower[offset:], diffusionName)
		if index < 0 {
			return false
		}
		after := offset + index + len(diffusionName)
		if !strings.HasPrefix(lower[after:], "gemma") {
			return true
		}
		offset = after
	}
	return false
}
