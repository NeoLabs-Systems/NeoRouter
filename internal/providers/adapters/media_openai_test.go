package adapters

import (
	"context"
	"net/http"
	"testing"

	"github.com/NeoLabs-Systems/neorouter/internal/providers"
)

// The OpenAI-style media surface: what one definition's dialect sends and what
// it reads back. These drive a real upstream.

// mediaTestMethods builds the surface of one definition against baseURL.
func mediaTestMethods(t *testing.T, id, baseURL string) *MediaMethods {
	t.Helper()
	return NewMediaMethods(mediaTestDefinition(t, id), mediaTestDeps("key", baseURL, nil))
}

// TestMediaSupportsReportsTheDeclaredMethods: a definition opts in per method.
func TestMediaSupportsReportsTheDeclaredMethods(t *testing.T) {
	pollinations := mediaTestMethods(t, "pollinations", "")
	if !pollinations.SupportsMedia(providers.CapChat) || !pollinations.SupportsMedia(providers.CapGenerateImage) || !pollinations.SupportsMedia(providers.CapEmbed) {
		t.Fatal("pollinations should offer chat, image and embed")
	}
	if pollinations.SupportsMedia(providers.CapSpeech) {
		t.Fatal("pollinations should not offer speech")
	}
	deepgram := mediaTestMethods(t, "deepgram", "")
	if deepgram.SupportsMedia(providers.CapSpeech) {
		t.Fatal("a dialect with its own methods should not report chat media paths")
	}
	// A custom endpoint answers every OpenAI path, because the user knows what
	// their server speaks.
	custom := providers.Definition{ID: "custom", Adapter: providers.AdapterOpenAICompatible, EditableBaseURL: true}
	methods := NewMediaMethods(custom, mediaTestDeps("key", "https://example.test/v1", nil))
	for _, method := range []providers.Capability{providers.CapEmbed, providers.CapGenerateImage, providers.CapSpeech, providers.CapTranscribe, providers.CapModerate, providers.CapCreateVideo, providers.CapDecide} {
		if !methods.SupportsMedia(method) {
			t.Fatalf("a custom endpoint should offer %s", method)
		}
	}
}

// TestMediaPathFillsTheModelIntoThePath: NVIDIA's genai host is one URL per
// model.
func TestMediaPathFillsTheModelIntoThePath(t *testing.T) {
	def := mediaTestDefinition(t, "nvidia")
	path, ok := MediaPath(def, providers.CapGenerateImage, "black-forest-labs/flux.2-klein-4b")
	if !ok || path != "https://ai.api.nvidia.com/v1/genai/black-forest-labs/flux.2-klein-4b" {
		t.Fatalf("path = %q ok=%v", path, ok)
	}
	if _, ok := MediaPath(def, providers.CapSpeech, "model"); ok {
		t.Fatal("nvidia declares no speech path")
	}
}

// TestEmbedSendsTheCanonicalRequest orders the rows by index and reports the
// prompt tokens.
func TestEmbedSendsTheCanonicalRequest(t *testing.T) {
	var sent map[string]any
	base := mediaTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" {
			t.Errorf("path = %q", r.URL.Path)
		}
		sent = mediaTestBody(t, r)
		mediaTestJSON(t, w, map[string]any{
			"data": []any{
				map[string]any{"index": 1, "embedding": []any{0.3, 0.4}},
				map[string]any{"index": 0, "embedding": []any{0.1, 0.2}},
			},
			"usage": map[string]any{"prompt_tokens": 2},
		})
	})
	methods := mediaTestMethods(t, "pollinations", base+"/v1")
	result, err := methods.Embed(context.Background(), providers.EmbedRequest{
		Input: []string{"hi", "there"}, Dimensions: mediaTestInt(4),
	}, providers.Call{ModelID: "openai/text-embedding-3-small"})
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	if len(result.Vectors) != 2 || result.Vectors[0][0] != 0.1 || result.Vectors[1][0] != 0.3 {
		t.Fatalf("vectors = %v", result.Vectors)
	}
	if result.InputTokens == nil || *result.InputTokens != 2 {
		t.Fatalf("input tokens = %v", result.InputTokens)
	}
	mediaTestSame(t, "body", map[string]any{
		"model": "openai/text-embedding-3-small", "input": []string{"hi", "there"},
		"encoding_format": "float", "dimensions": 4,
	}, sent)
}

// TestEmbedAsksForTheInputType: NVIDIA's retrieval embedders need to know
// whether they embed a query or a passage.
func TestEmbedAsksForTheInputType(t *testing.T) {
	var sent map[string]any
	base := mediaTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		sent = mediaTestBody(t, r)
		mediaTestJSON(t, w, map[string]any{"data": []any{map[string]any{"index": 0, "embedding": []any{0.5}}}})
	})
	methods := mediaTestMethods(t, "nvidia", base)
	if _, err := methods.Embed(context.Background(), providers.EmbedRequest{Input: []string{"hi"}}, providers.Call{ModelID: "nvidia/llama-3.2-nv-embedqa-1b-v2"}); err != nil {
		t.Fatalf("embed: %v", err)
	}
	if sent["input_type"] != "query" {
		t.Fatalf("input_type = %v", sent["input_type"])
	}
	if _, err := methods.Embed(context.Background(), providers.EmbedRequest{Input: "hi", InputType: "passage"}, providers.Call{ModelID: "nvidia/llama-3.2-nv-embedqa-1b-v2"}); err != nil {
		t.Fatalf("embed: %v", err)
	}
	if sent["input_type"] != "passage" {
		t.Fatalf("input_type = %v", sent["input_type"])
	}
}

// TestEmbedRefusesAnAnswerThatDoesNotMatchTheInput pins the message.
func TestEmbedRefusesAnAnswerThatDoesNotMatchTheInput(t *testing.T) {
	base := mediaTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		mediaTestJSON(t, w, map[string]any{"data": []any{
			map[string]any{"index": 0, "embedding": []any{0.1}},
			map[string]any{"index": 1, "embedding": []any{0.2}},
		}})
	})
	methods := mediaTestMethods(t, "pollinations", base+"/v1")
	_, err := methods.Embed(context.Background(), providers.EmbedRequest{Input: []string{"hi"}}, providers.Call{ModelID: "openai/text-embedding-3-small"})
	if kind := mediaTestErrorKind(t, err); kind != providers.KindInvalidOutput {
		t.Fatalf("kind = %v", kind)
	}
	if got := mediaErrorText(err); got != "The provider returned an unexpected embeddings response." {
		t.Fatalf("message = %q", got)
	}
}

// TestGenerateImageSendsTheOpenAIBody: the size and the b64 response format.
func TestGenerateImageSendsTheOpenAIBody(t *testing.T) {
	var sent map[string]any
	base := mediaTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/images/generations" {
			t.Errorf("path = %q", r.URL.Path)
		}
		sent = mediaTestBody(t, r)
		mediaTestJSON(t, w, map[string]any{"data": []any{map[string]any{"b64_json": "aW1n"}}})
	})
	methods := mediaTestMethods(t, "pollinations", base+"/v1")
	result, err := methods.GenerateImage(context.Background(), providers.ImageRequest{Prompt: "a cat", Size: "512x512"}, providers.Call{ModelID: "tongyi-mai/z-image-turbo"})
	if err != nil {
		t.Fatalf("image: %v", err)
	}
	if len(result.Images) != 1 || result.Images[0].B64 != "aW1n" {
		t.Fatalf("images = %+v", result.Images)
	}
	mediaTestSame(t, "body", map[string]any{
		"model": "tongyi-mai/z-image-turbo", "prompt": "a cat", "n": 1,
		"size": "512x512", "response_format": "b64_json",
	}, sent)
}

// TestGenerateImageReadsEveryShapeAProviderUses: bare base64, a link, a data
// URL and an image_url object.
func TestGenerateImageReadsEveryShapeAProviderUses(t *testing.T) {
	base := mediaTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		mediaTestJSON(t, w, map[string]any{"data": []any{
			map[string]any{"b64_json": "abc", "revised_prompt": "a better cat"},
			map[string]any{"url": "https://images.test/cat.png"},
			map[string]any{"url": "data:image/webp;base64,d2VicA=="},
			map[string]any{"image_url": map[string]any{"url": "https://images.test/other.png"}},
			map[string]any{"image_url": "https://images.test/third.png"},
			map[string]any{},
		}})
	})
	methods := mediaTestMethods(t, "pollinations", base+"/v1")
	result, err := methods.GenerateImage(context.Background(), providers.ImageRequest{Prompt: "a cat"}, providers.Call{ModelID: "flux"})
	if err != nil {
		t.Fatalf("image: %v", err)
	}
	if len(result.Images) != 5 {
		t.Fatalf("images = %+v", result.Images)
	}
	if result.Images[0].B64 != "abc" || result.Images[0].RevisedPrompt == nil || *result.Images[0].RevisedPrompt != "a better cat" {
		t.Fatalf("first image = %+v", result.Images[0])
	}
	if result.Images[1].URL != "https://images.test/cat.png" {
		t.Fatalf("second image = %+v", result.Images[1])
	}
	if result.Images[2].B64 != "d2VicA==" || result.Images[2].Mime != "image/webp" {
		t.Fatalf("third image = %+v", result.Images[2])
	}
	if result.Images[3].URL != "https://images.test/other.png" || result.Images[4].URL != "https://images.test/third.png" {
		t.Fatalf("image_url shapes = %+v %+v", result.Images[3], result.Images[4])
	}
}

// TestGenerateImageReportsAnEmptyAnswer pins the message.
func TestGenerateImageReportsAnEmptyAnswer(t *testing.T) {
	base := mediaTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		mediaTestJSON(t, w, map[string]any{"data": []any{}})
	})
	methods := mediaTestMethods(t, "pollinations", base+"/v1")
	_, err := methods.GenerateImage(context.Background(), providers.ImageRequest{Prompt: "a cat"}, providers.Call{ModelID: "flux"})
	if kind := mediaTestErrorKind(t, err); kind != providers.KindEmpty {
		t.Fatalf("kind = %v", kind)
	}
	if got := mediaErrorText(err); got != "The provider returned no image." {
		t.Fatalf("message = %q", got)
	}
}

// TestGenerateImageSendsAnAspectRatioOnOpenRouter: OpenRouter takes a ratio and
// no response format.
func TestGenerateImageSendsAnAspectRatioOnOpenRouter(t *testing.T) {
	var sent map[string]any
	base := mediaTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		sent = mediaTestBody(t, r)
		mediaTestJSON(t, w, map[string]any{"data": []any{map[string]any{"b64_json": "aW1n"}}})
	})
	methods := mediaTestMethods(t, "openrouter", base+"/api/v1")
	if _, err := methods.GenerateImage(context.Background(), providers.ImageRequest{Prompt: "a cat", Size: "1024x1024"}, providers.Call{ModelID: "google/gemini-image"}); err != nil {
		t.Fatalf("image: %v", err)
	}
	mediaTestSame(t, "body", map[string]any{
		"model": "google/gemini-image", "prompt": "a cat", "n": 1, "aspect_ratio": "1:1",
	}, sent)
	if got := AspectRatio("1280x720"); got != "16:9" {
		t.Fatalf("aspect ratio = %q", got)
	}
	if got := AspectRatio("wide"); got != "wide" {
		t.Fatalf("aspect ratio = %q", got)
	}
}

// TestNvidiaGenai: one image per call, base64 JPEG in artifacts[], no size.
func TestNvidiaGenai(t *testing.T) {
	def := providers.Definition{
		ID: "nvidia", Adapter: providers.AdapterOpenAICompatible,
		Media:  providers.MediaSpec{GenerateImage: "/genai/{model}"},
		Quirks: providers.Quirks{ImageAPI: "nvidia_genai"},
	}
	calls := 0
	var lastAccept string
	base := mediaTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		lastAccept = r.Header.Get("Accept")
		if r.URL.Path != "/genai/flux.2-klein-4b" {
			t.Errorf("path = %q", r.URL.Path)
		}
		sent := mediaTestBody(t, r)
		if sent["width"] != float64(1024) || sent["height"] != float64(1024) {
			t.Errorf("size = %v x %v", sent["width"], sent["height"])
		}
		if _, found := sent["size"]; found {
			t.Error("the requested size must not be passed on")
		}
		mediaTestJSON(t, w, map[string]any{"artifacts": []any{map[string]any{"base64": "aW1n", "finishReason": "SUCCESS"}}})
	})
	methods := NewMediaMethods(def, mediaTestDeps("key", base, nil))
	result, err := methods.GenerateImage(context.Background(), providers.ImageRequest{Prompt: "a cat", N: 2, Size: "512x512"}, providers.Call{ModelID: "flux.2-klein-4b"})
	if err != nil {
		t.Fatalf("image: %v", err)
	}
	if calls != 2 || len(result.Images) != 2 {
		t.Fatalf("calls = %d, images = %d", calls, len(result.Images))
	}
	if result.Images[0].Mime != "image/jpeg" {
		t.Fatalf("mime = %q", result.Images[0].Mime)
	}
	if lastAccept != "application/json" {
		t.Fatalf("accept = %q", lastAccept)
	}
}

// TestNvidiaGenaiReportsABlockedImage pins the message.
func TestNvidiaGenaiReportsABlockedImage(t *testing.T) {
	def := providers.Definition{
		ID: "nvidia", Adapter: providers.AdapterOpenAICompatible,
		Media:  providers.MediaSpec{GenerateImage: "/genai/{model}"},
		Quirks: providers.Quirks{ImageAPI: "nvidia_genai"},
	}
	base := mediaTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		mediaTestJSON(t, w, map[string]any{"artifacts": []any{map[string]any{"finishReason": "CONTENT_FILTERED"}}})
	})
	methods := NewMediaMethods(def, mediaTestDeps("key", base, nil))
	_, err := methods.GenerateImage(context.Background(), providers.ImageRequest{Prompt: "a cat"}, providers.Call{ModelID: "flux.2-klein-4b"})
	if kind := mediaTestErrorKind(t, err); kind != providers.KindBadRequest {
		t.Fatalf("kind = %v", kind)
	}
	if got := mediaErrorText(err); got != "The image was blocked (CONTENT_FILTERED)." {
		t.Fatalf("message = %q", got)
	}
}

// TestVoiceListCapitalizes: the playground shows a name beside every id.
func TestVoiceListCapitalizes(t *testing.T) {
	voices := VoiceList([]string{"alloy", "Kore", ""})
	mediaTestSame(t, "voices", []map[string]string{
		{"id": "alloy", "name": "Alloy"},
		{"id": "Kore", "name": "Kore"},
		{"id": "", "name": ""},
	}, voices)
}

// TestVoicesComeFromTheProvidersListing: OpenRouter's per-model voices.
func TestVoicesComeFromTheProvidersListing(t *testing.T) {
	base := mediaTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/models" || r.URL.Query().Get("output_modalities") != "speech" {
			t.Errorf("listing = %s", r.URL.String())
		}
		mediaTestJSON(t, w, map[string]any{"data": []any{
			map[string]any{"id": "heygen/heygen-voice-1-turbo:free", "supported_voices": []any{"nadine", "tabitha"}},
			map[string]any{"id": "google/gemini-3.8-flash-tts", "supported_voices": []any{"Zephyr", "Kore"}},
			map[string]any{"id": "fish-audio/s2.1-pro-free:free", "supported_voices": nil},
		}})
	})
	methods := mediaTestMethods(t, "openrouter", base+"/api/v1")
	voices, err := methods.Voices(context.Background(), "google/gemini-3.8-flash-tts")
	if err != nil {
		t.Fatalf("voices: %v", err)
	}
	mediaTestSame(t, "voices", []map[string]string{
		{"id": "Zephyr", "name": "Zephyr"}, {"id": "Kore", "name": "Kore"},
	}, voices)
	// A model that lists no voices is left without any, not given OpenAI's.
	voiceless, err := methods.Voices(context.Background(), "fish-audio/s2.1-pro-free:free")
	if err != nil {
		t.Fatalf("voices: %v", err)
	}
	if len(voiceless) != 0 {
		t.Fatalf("voices = %v", voiceless)
	}
}

// TestVoicesFallBackToOpenAINames: a host with no listing answers OpenAI's.
func TestVoicesFallBackToOpenAINames(t *testing.T) {
	methods := mediaTestMethods(t, "custom_openai", "https://example.test/v1")
	voices, err := methods.Voices(context.Background(), "tts-1")
	if err != nil {
		t.Fatalf("voices: %v", err)
	}
	if len(voices) != len(OpenAIVoices) || voices[0].ID != "alloy" {
		t.Fatalf("voices = %v", voices)
	}
}
