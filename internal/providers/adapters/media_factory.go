package adapters

import (
	"fmt"

	"github.com/NeoLabs-Systems/neorouter/internal/providers"
)

// The media dialects this package builds. The chat dialects are the chat_*
// files' business; the two sets never build each other's definitions.

// NewMediaAdapter builds the adapter for a media-only definition: deepgram,
// elevenlabs, search and weather. It reports false for a definition whose
// adapter is one of the chat dialects, so the package's NewAdapter can hand it
// to the chat constructors instead.
func NewMediaAdapter(def providers.Definition, deps providers.AdapterDeps) (providers.Adapter, bool, error) {
	switch def.Adapter {
	case providers.AdapterDeepgram:
		return NewDeepgramAdapter(def, deps), true, nil
	case providers.AdapterElevenLabs:
		return NewElevenLabsAdapter(def, deps), true, nil
	case providers.AdapterSearch:
		return NewSearchAdapter(def, deps), true, nil
	case providers.AdapterWeather:
		return NewWeatherAdapter(def, deps), true, nil
	}
	return nil, false, nil
}

// MediaFactory is NewMediaAdapter as the registry's AdapterFactory, for a
// registry that only needs the media dialects (a test, or a deployment without
// chat providers).
func MediaFactory(def providers.Definition, deps providers.AdapterDeps) (providers.Adapter, error) {
	adapter, ok, err := NewMediaAdapter(def, deps)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("adapters: %q is not a media adapter", def.Adapter)
	}
	return adapter, nil
}

// NewOpenAIMedia is the media surface of an adapter that speaks the OpenAI
// paths (openai_compatible and the dialects built on it, such as cloudflare).
// The chat adapter holds it and delegates embed, generateImage, voices, speech,
// transcribe, moderate, decide and the video methods to it; cloudflare keeps
// its own generateImage, speech and transcribe and takes the rest from here.
func NewOpenAIMedia(def providers.Definition, deps providers.AdapterDeps) *MediaMethods {
	return NewMediaMethods(def, deps)
}

// The capability interfaces each type of this package satisfies, checked at
// compile time so a signature drift in internal/providers fails here.
var (
	_ providers.Embedder       = (*MediaMethods)(nil)
	_ providers.ImageGenerator = (*MediaMethods)(nil)
	_ providers.Speaker        = (*MediaMethods)(nil)
	_ providers.Transcriber    = (*MediaMethods)(nil)
	_ providers.Moderator      = (*MediaMethods)(nil)
	_ providers.Decider        = (*MediaMethods)(nil)
	_ providers.VideoCreator   = (*MediaMethods)(nil)
	_ providers.Voicer         = (*MediaMethods)(nil)

	_ providers.Adapter     = (*DeepgramAdapter)(nil)
	_ providers.Speaker     = (*DeepgramAdapter)(nil)
	_ providers.Transcriber = (*DeepgramAdapter)(nil)

	_ providers.Adapter     = (*ElevenLabsAdapter)(nil)
	_ providers.Speaker     = (*ElevenLabsAdapter)(nil)
	_ providers.Transcriber = (*ElevenLabsAdapter)(nil)

	_ providers.Adapter  = (*SearchAdapter)(nil)
	_ providers.Searcher = (*SearchAdapter)(nil)

	_ providers.Adapter   = (*WeatherAdapter)(nil)
	_ providers.Weatherer = (*WeatherAdapter)(nil)
)
