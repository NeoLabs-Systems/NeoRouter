package providers

import (
	"context"
	"encoding/json"
	"iter"
	"net/http"
	"time"
)

// Capability is one thing an adapter can do. The names are the adapter method
// names, used unchanged by definitions' media entries, routing candidates and
// the account API.
type Capability string

const (
	CapChat          Capability = "chat"
	CapEmbed         Capability = "embed"
	CapGenerateImage Capability = "generateImage"
	CapSpeech        Capability = "speech"
	CapTranscribe    Capability = "transcribe"
	CapModerate      Capability = "moderate"
	CapClassify      Capability = "classify"
	CapCreateVideo   Capability = "createVideo"
	CapDecide        Capability = "decide"
	CapSearch        Capability = "search"
	CapWeather       Capability = "weather"
	CapVoices        Capability = "voices"
)

// Capabilities is the set of methods one adapter serves. Adapters embed it in
// the value they return from Supports.
type Capabilities map[Capability]bool

// Supports reports whether the set contains method.
func (c Capabilities) Supports(method Capability) bool { return c[method] }

// Of builds a Capabilities set from method names.
func Of(methods ...Capability) Capabilities {
	set := make(Capabilities, len(methods))
	for _, method := range methods {
		set[method] = true
	}
	return set
}

// Adapter is one provider dialect bound to one account. It answers the
// questions every connection has to answer — is the key good, what does the
// provider list, what is left — and the methods it can perform are the
// capability interfaces below.
//
// A caller that wants to speak, embed or search asserts the interface it needs
// and asks Supports first:
//
//	if speaker, ok := adapter.(providers.Speaker); ok && adapter.Supports(providers.CapSpeech) {
//		... speaker.Speech(ctx, providers.SpeechRequest{...}, call)
//	}
type Adapter interface {
	// Definition is the provider this adapter was built from.
	Definition() Definition
	// Validate checks the key without saving anything: healthy, invalid (the
	// provider refused the key) or unknown (it answered something unexpected).
	Validate(ctx context.Context) (Validation, error)
	// ListModels reads the provider's listing, normalized by NormalizeModel. An
	// adapter that cannot list returns an empty slice.
	ListModels(ctx context.Context) ([]Model, error)
	// FetchQuota reads the definition's quota probe. It returns nil when the
	// provider reports nothing.
	FetchQuota(ctx context.Context) ([]QuotaWindow, error)
	// Supports reports whether the adapter can perform a capability. Chat and
	// everything the adapter implements answer true.
	Supports(method Capability) bool
	// TranscribeTranslate reports whether transcribe can translate to English
	// (deepgram and elevenlabs cannot).
	TranscribeTranslate() bool
	// TranscribeTimestamps reports whether transcribe returns word timings.
	TranscribeTimestamps() bool
}

// Chatter is an adapter that answers chat completions.
type Chatter interface {
	// Chat sends one canonical request and returns the canonical completion
	// (both are the OpenAI Chat Completions JSON bodies of
	// capabilities/chat/canonical.go) with the upstream headers.
	Chat(ctx context.Context, req json.RawMessage, call Call) (*ChatResult, error)
}

// ChatResult is a canonical completion: the OpenAI wire body plus the upstream
// response headers, which adapters keep for quota learning.
type ChatResult struct {
	Body    json.RawMessage
	Headers http.Header
}

// Streamer is an adapter that streams chat completions.
type Streamer interface {
	// Stream sends one canonical request and yields the canonical stream events
	// (`chat.completion.chunk` JSON objects) as they arrive. The final event
	// carries the provider's usage block when it reports one.
	Stream(ctx context.Context, req json.RawMessage, call Call) iter.Seq2[json.RawMessage, error]
}

// Embedder is an adapter that turns text into vectors.
type Embedder interface {
	Embed(ctx context.Context, req EmbedRequest, call Call) (*EmbedResult, error)
}

// ImageGenerator is an adapter that generates images.
type ImageGenerator interface {
	GenerateImage(ctx context.Context, req ImageRequest, call Call) (*ImageResult, error)
}

// Speaker is an adapter that speaks text.
type Speaker interface {
	Speech(ctx context.Context, req SpeechRequest, call Call) (*SpeechResult, error)
}

// Transcriber is an adapter that transcribes audio.
type Transcriber interface {
	Transcribe(ctx context.Context, req TranscribeRequest, call Call) (*Transcript, error)
}

// Moderator is an adapter with a native moderation endpoint.
type Moderator interface {
	Moderate(ctx context.Context, req ModerateRequest, call Call) (*ModerationResult, error)
}

// Decider is an adapter that answers System One decision questions.
type Decider interface {
	Decide(ctx context.Context, req DecideRequest, call Call) (*DecideResult, error)
}

// VideoCreator is an adapter that generates videos from a prompt.
type VideoCreator interface {
	CreateVideo(ctx context.Context, req VideoRequest, call Call) (*VideoJob, error)
	GetVideo(ctx context.Context, upstreamID string) (*VideoStatus, error)
	VideoContent(ctx context.Context, upstreamID string) (*http.Response, error)
}

// Searcher is an adapter for a web search engine.
type Searcher interface {
	Search(ctx context.Context, req SearchRequest) (*SearchResult, error)
}

// Weatherer is an adapter for a weather service.
type Weatherer interface {
	Weather(ctx context.Context, req WeatherRequest) (*WeatherReport, error)
}

// Voicer is an adapter that lists the voices a speech model offers.
type Voicer interface {
	Voices(ctx context.Context, modelID string) ([]Voice, error)
}

// Translator is implemented by adapters whose translation support depends on
// the model (local runtimes). Everything else reports it through
// Adapter.TranscribeTranslate.
type Translator interface {
	CanTranslate(modelID string) bool
}

// CanTranslate reports whether modelID can translate a transcription to
// English.
func CanTranslate(adapter Adapter, modelID string) bool {
	if translator, ok := adapter.(Translator); ok {
		return translator.CanTranslate(modelID)
	}
	return adapter.TranscribeTranslate()
}

// Call is what one upstream call is made with: the model to use, the caller's
// conversation id, the timeout and the hook that receives the response headers
// before the body. The caller's context carries cancellation.
type Call struct {
	ModelID string
	// SessionID is the conversation id the client sent, when it sent one; hosts
	// that route by it (OpenCode) receive it as a header.
	SessionID string
	// Timeout bounds this call. Zero means the definition's quirks.timeoutMs,
	// else AdapterDeps.UpstreamTimeout.
	Timeout time.Duration
	// OnHeaders receives the upstream response headers as soon as they are
	// read, for calls whose result comes from headers (a stream's usage block).
	OnHeaders func(http.Header)
}

// ValidationStatus is what a key check concluded.
type ValidationStatus string

const (
	// ValidationHealthy means the provider accepted the key.
	ValidationHealthy ValidationStatus = "healthy"
	// ValidationInvalid means the provider refused the key itself.
	ValidationInvalid ValidationStatus = "invalid"
	// ValidationUnknown means the check could not decide (an HTML page, an
	// unexpected answer, a provider that cannot be checked cheaply).
	ValidationUnknown ValidationStatus = "unknown"
)

// Validation is a key check's verdict.
type Validation struct {
	Status  ValidationStatus `json:"status"`
	Message *string          `json:"message"`
}

// ValidKey is the healthy verdict.
func ValidKey() Validation { return Validation{Status: ValidationHealthy} }

// InvalidKey is the verdict for a key the provider refused.
func InvalidKey(message string) Validation {
	return Validation{Status: ValidationInvalid, Message: &message}
}

// UnknownKey is the verdict for a check that could not decide.
func UnknownKey(message string) Validation {
	return Validation{Status: ValidationUnknown, Message: &message}
}

// Model kinds: what a listed model is for, from model_info.detectKind.
const (
	KindChat          = "chat"
	KindEmbedding     = "embedding"
	KindRerank        = "rerank"
	KindTranscription = "transcription"
	KindSpeech        = "speech"
	KindLive          = "live"
	KindImage         = "image"
	KindVideo         = "video"
	KindRouter        = "router"
	KindModeration    = "moderation"
	KindSearch        = "search"
	KindWeather       = "weather"
	KindOther         = "other"
)

// Voice is one voice a speech model offers.
type Voice struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// EmbedRequest is one embedding call. Input is a string or a list of strings.
type EmbedRequest struct {
	Input      any
	Dimensions *int
	// InputType is the caller's hint (search_document, search_query), sent by
	// providers that ask for one.
	InputType string
}

// EmbedResult is one embedding answer.
type EmbedResult struct {
	Vectors [][]float64
	// InputTokens is the prompt tokens the provider reported.
	InputTokens *int
	Headers     http.Header
}

// ImageRequest is one image generation call. Zero values are the adapter's
// defaults (one image).
type ImageRequest struct {
	Prompt  string
	N       int
	Size    string
	Quality string
}

// Image is one generated image: base64 data, a URL, or both.
type Image struct {
	B64           string
	URL           string
	Mime          string
	RevisedPrompt *string
}

// ImageResult is one image generation answer.
type ImageResult struct {
	Images  []Image
	Headers http.Header
}

// SpeechRequest is one speech call.
type SpeechRequest struct {
	Input string
	Voice string
	// Format is the requested container (mp3, wav, pcm, opus); empty means the
	// provider's default.
	Format string
	Speed  *float64
}

// SpeechResult is synthesized audio.
type SpeechResult struct {
	Audio   []byte
	Mime    string
	Headers http.Header
}

// TranscribeRequest is one transcription call.
type TranscribeRequest struct {
	File     []byte
	Filename string
	Mime     string
	Language string
	Prompt   string
	// Translate asks for English whatever the source language.
	Translate bool
}

// Segment is one timed piece of a transcript.
type Segment struct {
	Start float64 `json:"start"`
	End   float64 `json:"end"`
	Text  string  `json:"text"`
}

// Transcript is one transcription answer. Language and Duration are empty when
// the provider did not report them.
type Transcript struct {
	Text     string
	Language string
	Duration *float64
	Segments []Segment
	Headers  http.Header
}

// ModerateRequest is one moderation call.
type ModerateRequest struct {
	Input []string
}

// ModerationResult carries the provider's moderation records unchanged: the
// media API returns them as they came.
type ModerationResult struct {
	Results []any
	Headers http.Header
}

// DecideRequest is one System One decision question.
type DecideRequest struct {
	State     any
	Questions any
}

// DecideResult is one decision answer.
type DecideResult struct {
	Answers     any
	Usage       any
	InputTokens *int
	Headers     http.Header
}

// VideoRequest is one video generation call. Seconds is the length as the API
// asked for it (a number in a string).
type VideoRequest struct {
	Prompt  string
	Seconds string
	Size    string
}

// VideoJob is a submitted video.
type VideoJob struct {
	UpstreamID string
	Status     string
	Progress   *float64
	Headers    http.Header
}

// VideoStatus is a video's progress.
type VideoStatus struct {
	Status   string
	Progress *float64
	Err      *string
}

// SearchRequest is one web search.
type SearchRequest struct {
	Query          string
	MaxResults     int
	IncludeDomains []string
	ExcludeDomains []string
	Recency        string
}

// SearchHit is one search result.
type SearchHit struct {
	Title     string
	URL       string
	Snippet   string
	Content   string
	Published *string
}

// SearchResult is one search answer.
type SearchResult struct {
	Results []SearchHit
	Headers http.Header
}

// WeatherRequest is one weather lookup. Location is a place name or
// {latitude, longitude}.
type WeatherRequest struct {
	Location any
	Current  bool
	Hourly   bool
	Daily    bool
	Units    string
	Days     int
}

// WeatherReport is what a weather dialect builds: the resolved place and the
// sections the caller asked for (nil when not asked for), in the canonical
// weather shape of capabilities/media/handlers.go.
type WeatherReport struct {
	Location any
	Current  any
	Hourly   any
	Daily    any
	Headers  http.Header
}
