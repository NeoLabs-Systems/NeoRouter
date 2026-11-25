package adapters

import (
	"context"
	"net/http"

	"github.com/NeoLabs-Systems/neorouter/internal/providers"
)

// The OpenAI-compatible dialect carries the OpenAI-style media surface, the
// way openai_media.js installs those methods on the JS class. Supports() still
// gates what the definition actually offers, and dialects with their own media
// calls (cloudflare) override these through method shadowing.
var (
	_ providers.Embedder       = (*openAICompatible)(nil)
	_ providers.ImageGenerator = (*openAICompatible)(nil)
	_ providers.Speaker        = (*openAICompatible)(nil)
	_ providers.Transcriber    = (*openAICompatible)(nil)
	_ providers.Moderator      = (*openAICompatible)(nil)
	_ providers.Decider        = (*openAICompatible)(nil)
	_ providers.VideoCreator   = (*openAICompatible)(nil)
	_ providers.Voicer         = (*openAICompatible)(nil)
)

func (a *openAICompatible) Embed(ctx context.Context, req providers.EmbedRequest, call providers.Call) (*providers.EmbedResult, error) {
	return a.media.Embed(ctx, req, call)
}

func (a *openAICompatible) GenerateImage(ctx context.Context, req providers.ImageRequest, call providers.Call) (*providers.ImageResult, error) {
	return a.media.GenerateImage(ctx, req, call)
}

func (a *openAICompatible) Speech(ctx context.Context, req providers.SpeechRequest, call providers.Call) (*providers.SpeechResult, error) {
	return a.media.Speech(ctx, req, call)
}

func (a *openAICompatible) Transcribe(ctx context.Context, req providers.TranscribeRequest, call providers.Call) (*providers.Transcript, error) {
	return a.media.Transcribe(ctx, req, call)
}

func (a *openAICompatible) Moderate(ctx context.Context, req providers.ModerateRequest, call providers.Call) (*providers.ModerationResult, error) {
	return a.media.Moderate(ctx, req, call)
}

func (a *openAICompatible) Decide(ctx context.Context, req providers.DecideRequest, call providers.Call) (*providers.DecideResult, error) {
	return a.media.Decide(ctx, req, call)
}

func (a *openAICompatible) CreateVideo(ctx context.Context, req providers.VideoRequest, call providers.Call) (*providers.VideoJob, error) {
	return a.media.CreateVideo(ctx, req, call)
}

func (a *openAICompatible) GetVideo(ctx context.Context, upstreamID string) (*providers.VideoStatus, error) {
	return a.media.GetVideo(ctx, upstreamID)
}

func (a *openAICompatible) VideoContent(ctx context.Context, upstreamID string) (*http.Response, error) {
	return a.media.VideoContent(ctx, upstreamID)
}

func (a *openAICompatible) Voices(ctx context.Context, modelID string) ([]providers.Voice, error) {
	return a.media.Voices(ctx, modelID)
}
