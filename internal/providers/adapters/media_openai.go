package adapters

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"math/rand/v2"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/NeoLabs-Systems/neorouter/internal/providers"
)

// The OpenAI-style endpoints for the non-chat kinds: a definition opts in per
// method with `media: { embed: '/embeddings', … }`, and a custom
// OpenAI-compatible endpoint supports every method, because the user knows what
// their server speaks. Every method returns a normalized result the gateway
// turns back into the OpenAI response shape, so providers with other dialects
// (Gemini, Cloudflare) can return the same shapes.
//
// The openai_compatible adapter owns one MediaMethods and delegates these
// methods to it.

// mediaDefaultPaths are the OpenAI paths a definition may leave out.
var mediaDefaultPaths = map[providers.Capability]string{
	providers.CapEmbed:         "/embeddings",
	providers.CapGenerateImage: "/images/generations",
	providers.CapSpeech:        "/audio/speech",
	providers.CapTranscribe:    "/audio/transcriptions",
	providers.CapModerate:      "/moderations",
	providers.CapCreateVideo:   "/videos",
	providers.CapDecide:        "/systemone",
}

// MediaPath is the endpoint a definition declares for method, or the OpenAI
// default for an endpoint the account may point anywhere (mediaPath).
func MediaPath(def providers.Definition, method providers.Capability, modelID string) (string, bool) {
	path, declared := def.Media.Endpoint(method)
	if declared && path != "" {
		// A path may carry the model: NVIDIA's genai host is one URL per model.
		return strings.Replace(path, "{model}", modelID, 1), true
	}
	if def.EditableBaseURL {
		if fallback, ok := mediaDefaultPaths[method]; ok {
			return fallback, true
		}
	}
	return "", false
}

// MediaMethods is the OpenAI-style media surface of one account.
type MediaMethods struct {
	*mediaBase
}

// NewMediaMethods builds the media surface of an account, for an adapter that
// speaks these paths (the OpenAI-compatible dialects and their relatives).
func NewMediaMethods(def providers.Definition, deps providers.AdapterDeps) *MediaMethods {
	return &MediaMethods{mediaBase: newMediaBase(def, deps)}
}

// SupportsMedia reports whether the definition offers method: chat always
// answers true, and a media method does when the definition names a path for
// it.
func (m *MediaMethods) SupportsMedia(method providers.Capability) bool {
	if method == providers.CapChat {
		return true
	}
	_, ok := MediaPath(m.def, method, "")
	return ok
}

// mediaNoEndpoint is the failure for a method the definition does not declare.
func mediaNoEndpoint(method providers.Capability) error {
	return providers.NewError(providers.KindBadRequest,
		fmt.Sprintf("This provider does not offer %s.", method), providers.ErrorOptions{})
}

// mediaInvalidOutput is the failure for an answer that cannot be used.
func mediaInvalidOutput(message string) error {
	return providers.NewError(providers.KindInvalidOutput, message, providers.ErrorOptions{})
}

// Embed sends one embedding request: one vector per input, in order.
func (m *MediaMethods) Embed(ctx context.Context, req providers.EmbedRequest, call providers.Call) (*providers.EmbedResult, error) {
	path, ok := MediaPath(m.def, providers.CapEmbed, call.ModelID)
	if !ok {
		return nil, mediaNoEndpoint(providers.CapEmbed)
	}
	body := map[string]any{"model": call.ModelID, "input": req.Input, "encoding_format": "float"}
	if req.Dimensions != nil && *req.Dimensions != 0 {
		field := m.def.Quirks.EmbeddingDimensionsField
		if field == "" {
			field = "dimensions"
		}
		body[field] = *req.Dimensions
	}
	// NVIDIA's retrieval embedders need to know whether they embed a query or
	// a passage.
	if providers.IsTrue(m.def.Quirks.EmbeddingInputType) {
		inputType := req.InputType
		if inputType == "" {
			inputType = "query"
		}
		body["input_type"] = inputType
	}
	response, err := m.doPost(ctx, path, body, call)
	if err != nil {
		return nil, err
	}
	rows := mediaSortByIndex(mediaList(mediaMember(response.Data, "data")))
	if len(rows) != mediaInputCount(req.Input) {
		return nil, mediaInvalidOutput("The provider returned an unexpected embeddings response.")
	}
	vectors := make([][]float64, len(rows))
	for i, row := range rows {
		embedding := mediaMember(row, "embedding")
		if mediaList(embedding) == nil {
			return nil, mediaInvalidOutput("The provider returned an unexpected embeddings response.")
		}
		vectors[i] = mediaFloats(embedding)
	}
	var inputTokens *int
	if tokens := mediaNumber(mediaMember(mediaMember(response.Data, "usage"), "prompt_tokens")); tokens != nil {
		value := int(*tokens)
		inputTokens = &value
	}
	return &providers.EmbedResult{Vectors: vectors, InputTokens: inputTokens, Headers: response.Headers}, nil
}

// mediaInputCount is how many inputs one embedding request carried.
func mediaInputCount(input any) int {
	switch value := input.(type) {
	case []string:
		return len(value)
	case []any:
		return len(value)
	case string:
		return 1
	}
	return -1
}

// mediaSortByIndex orders embedding rows by their index, absent meaning 0.
func mediaSortByIndex(rows []any) []any {
	out := append([]any(nil), rows...)
	sort.SliceStable(out, func(i, j int) bool {
		return mediaIndex(out[i]) < mediaIndex(out[j])
	})
	return out
}

// mediaIndex reads one row's index.
func mediaIndex(row any) float64 {
	if index := mediaNumber(mediaMember(row, "index")); index != nil {
		return *index
	}
	return 0
}

// GenerateImage asks for n images. The response_format is b64_json unless the
// provider takes an aspect ratio instead, and NVIDIA's genai host speaks a
// dialect of its own.
func (m *MediaMethods) GenerateImage(ctx context.Context, req providers.ImageRequest, call providers.Call) (*providers.ImageResult, error) {
	path, ok := MediaPath(m.def, providers.CapGenerateImage, call.ModelID)
	if !ok {
		return nil, mediaNoEndpoint(providers.CapGenerateImage)
	}
	count := req.N
	if count == 0 {
		count = 1
	}
	if m.def.Quirks.ImageAPI == "nvidia_genai" {
		return m.nvidiaGenai(ctx, path, req.Prompt, count, call)
	}
	body := map[string]any{"model": call.ModelID, "prompt": req.Prompt, "n": count}
	if providers.IsTrue(m.def.Quirks.NoImageResponseFormat) {
		// OpenRouter takes an aspect ratio instead of pixel sizes.
		if req.Size != "" {
			body["aspect_ratio"] = AspectRatio(req.Size)
		}
	} else {
		if req.Size != "" {
			body["size"] = req.Size
		}
		body["response_format"] = "b64_json"
	}
	if req.Quality != "" {
		body["quality"] = req.Quality
	}
	response, err := m.doPost(ctx, path, body, call)
	if err != nil {
		return nil, err
	}
	rows := mediaList(mediaFirstTruthy(mediaMember(response.Data, "data"), mediaMember(response.Data, "images")))
	images := make([]providers.Image, 0, len(rows))
	for _, item := range rows {
		value := mediaFirstNonNull(
			mediaMember(item, "b64_json"),
			mediaMember(item, "url"),
			mediaMember(mediaMember(item, "image_url"), "url"),
			mediaMember(item, "image_url"),
		)
		image := ImageItem(value, mediaRevisedPrompt(item))
		if image.B64 != "" || image.URL != "" {
			images = append(images, image)
		}
	}
	if len(images) == 0 {
		return nil, providers.NewError(providers.KindEmpty, "The provider returned no image.", providers.ErrorOptions{})
	}
	return &providers.ImageResult{Images: images, Headers: response.Headers}, nil
}

// mediaRevisedPrompt reads an image's revised prompt.
func mediaRevisedPrompt(item any) *string {
	if text, ok := mediaMember(item, "revised_prompt").(string); ok {
		return &text
	}
	return nil
}

// mediaDataURL is a base64 image in a data URL.
var mediaDataURL = regexp.MustCompile(`(?s)^data:([^;]+);base64,(.*)$`)

// ImageItem reads one image the way a provider reported it: a data URL, a
// link, or bare base64 (an image/png unless it says otherwise).
func ImageItem(value any, revisedPrompt *string) providers.Image {
	text := mediaString(value)
	if match := mediaDataURL.FindStringSubmatch(text); match != nil {
		return providers.Image{B64: match[2], Mime: match[1], RevisedPrompt: revisedPrompt}
	}
	if mediaAbsoluteURL.MatchString(text) {
		return providers.Image{URL: text, RevisedPrompt: revisedPrompt}
	}
	return providers.Image{B64: text, Mime: "image/png", RevisedPrompt: revisedPrompt}
}

// mediaSizePattern is a pixel size such as 1024x1024.
var mediaSizePattern = regexp.MustCompile(`^(\d+)x(\d+)$`)

// AspectRatio turns a pixel size into the ratio OpenRouter asks for.
func AspectRatio(size string) string {
	match := mediaSizePattern.FindStringSubmatch(size)
	if match == nil {
		return size
	}
	width, _ := strconv.Atoi(match[1])
	height, _ := strconv.Atoi(match[2])
	divisor := mediaGCD(width, height)
	if divisor == 0 {
		return size
	}
	return fmt.Sprintf("%d:%d", width/divisor, height/divisor)
}

// mediaGCD is Euclid's greatest common divisor.
func mediaGCD(a, b int) int {
	for b != 0 {
		a, b = b, a%b
	}
	return a
}

// nvidiaGenai is NVIDIA's genai host: one image per call, base64 JPEG in
// artifacts[]. Its FLUX.2 models only make 1024x1024 images and refuse any
// other size, so the requested size is not passed on.
func (m *MediaMethods) nvidiaGenai(ctx context.Context, path, prompt string, count int, call providers.Call) (*providers.ImageResult, error) {
	images := make([]providers.Image, 0, count)
	var headers http.Header
	for index := 0; index < min(count, 4); index++ {
		body := map[string]any{
			"prompt": prompt, "width": 1024, "height": 1024,
			"seed": rand.IntN(1_000_000_000),
		}
		response, err := m.do(ctx, path, providers.Request{
			Method: http.MethodPost, JSON: body, Headers: mediaHeader("Accept", "application/json"),
		}, call)
		if err != nil {
			return nil, err
		}
		headers = response.Headers
		artifact := mediaFirst(mediaMember(response.Data, "artifacts"))
		if reason, ok := mediaMember(artifact, "finishReason").(string); ok && reason != "" && reason != "SUCCESS" {
			return nil, providers.NewError(providers.KindBadRequest, "The image was blocked ("+reason+").", providers.ErrorOptions{})
		}
		if encoded, ok := mediaMember(artifact, "base64").(string); ok && encoded != "" {
			images = append(images, providers.Image{B64: encoded, Mime: "image/jpeg"})
		}
	}
	if len(images) == 0 {
		return nil, providers.NewError(providers.KindEmpty, "The provider returned no image.", providers.ErrorOptions{})
	}
	return &providers.ImageResult{Images: images, Headers: headers}, nil
}

// Decide asks a System One decision model typed questions about a state.
func (m *MediaMethods) Decide(ctx context.Context, req providers.DecideRequest, call providers.Call) (*providers.DecideResult, error) {
	path, ok := MediaPath(m.def, providers.CapDecide, call.ModelID)
	if !ok {
		return nil, mediaNoEndpoint(providers.CapDecide)
	}
	response, err := m.doPost(ctx, path, map[string]any{
		"model": call.ModelID, "state": req.State, "questions": req.Questions,
	}, call)
	if err != nil {
		return nil, err
	}
	answers := mediaMember(response.Data, "answers")
	if !mediaTruthy(answers) || (mediaObject(answers) == nil && mediaList(answers) == nil) {
		return nil, mediaInvalidOutput("The decision model returned no answers.")
	}
	usage := mediaFirstTruthy(mediaMember(response.Data, "usage"), map[string]any{})
	var inputTokens *int
	for _, field := range []string{"prompt_tokens", "input_tokens"} {
		if tokens := mediaNumber(mediaMember(usage, field)); tokens != nil {
			value := int(*tokens)
			inputTokens = &value
			break
		}
	}
	return &providers.DecideResult{Answers: answers, Usage: usage, InputTokens: inputTokens, Headers: response.Headers}, nil
}

// Moderate runs a provider's own moderation endpoint.
func (m *MediaMethods) Moderate(ctx context.Context, req providers.ModerateRequest, call providers.Call) (*providers.ModerationResult, error) {
	path, ok := MediaPath(m.def, providers.CapModerate, call.ModelID)
	if !ok {
		return nil, mediaNoEndpoint(providers.CapModerate)
	}
	response, err := m.doPost(ctx, path, map[string]any{"model": call.ModelID, "input": req.Input}, call)
	if err != nil {
		return nil, err
	}
	results := mediaList(mediaMember(response.Data, "results"))
	if results == nil {
		return nil, mediaInvalidOutput("The provider returned an unexpected moderation response.")
	}
	return &providers.ModerationResult{Results: results, Headers: response.Headers}, nil
}

// CreateVideo submits a video job: the answer's id is the only name the
// provider knows it by.
func (m *MediaMethods) CreateVideo(ctx context.Context, req providers.VideoRequest, call providers.Call) (*providers.VideoJob, error) {
	path, ok := MediaPath(m.def, providers.CapCreateVideo, call.ModelID)
	if !ok {
		return nil, mediaNoEndpoint(providers.CapCreateVideo)
	}
	body := map[string]any{"model": call.ModelID, "prompt": req.Prompt}
	if req.Seconds != "" {
		if providers.IsTrue(m.def.Quirks.VideoDuration) {
			if seconds, err := strconv.ParseFloat(req.Seconds, 64); err == nil {
				body["duration"] = math.Round(seconds)
			} else {
				body["duration"] = nil
			}
		} else {
			body["seconds"] = req.Seconds
		}
	}
	if req.Size != "" {
		body["size"] = req.Size
	}
	response, err := m.doPost(ctx, path, body, call)
	if err != nil {
		return nil, err
	}
	id := mediaMember(response.Data, "id")
	if !mediaTruthy(id) {
		return nil, mediaInvalidOutput("The provider returned no video job id.")
	}
	return &providers.VideoJob{
		UpstreamID: mediaString(id),
		Status:     NormalizeVideoStatus(mediaString(mediaMember(response.Data, "status"))),
		Progress:   mediaNumber(mediaMember(response.Data, "progress")),
		Headers:    response.Headers,
	}, nil
}

// mediaVideoPath is the job path for an id the provider gave us.
func (m *MediaMethods) mediaVideoPath(upstreamID string) (string, bool) {
	path, ok := MediaPath(m.def, providers.CapCreateVideo, "")
	if !ok {
		return "", false
	}
	return path + "/" + mediaEncodeURIComponent(upstreamID), true
}

// GetVideo reads a video job's progress.
func (m *MediaMethods) GetVideo(ctx context.Context, upstreamID string) (*providers.VideoStatus, error) {
	path, ok := m.mediaVideoPath(upstreamID)
	if !ok {
		return nil, mediaNoEndpoint(providers.CapCreateVideo)
	}
	response, err := m.doGet(ctx, path, providers.Call{Timeout: 30 * time.Second})
	if err != nil {
		return nil, err
	}
	status := &providers.VideoStatus{
		Status:   NormalizeVideoStatus(mediaString(mediaMember(response.Data, "status"))),
		Progress: mediaNumber(mediaMember(response.Data, "progress")),
	}
	if message, ok := mediaMember(mediaMember(response.Data, "error"), "message").(string); ok && message != "" {
		status.Err = &message
	}
	return status, nil
}

// VideoContent streams a finished video back.
func (m *MediaMethods) VideoContent(ctx context.Context, upstreamID string) (*http.Response, error) {
	path, ok := m.mediaVideoPath(upstreamID)
	if !ok {
		return nil, mediaNoEndpoint(providers.CapCreateVideo)
	}
	return m.doRaw(ctx, path+"/content", providers.Request{Method: http.MethodGet}, providers.Call{Timeout: 300 * time.Second})
}

// NormalizeVideoStatus folds a provider's job status into the four the gateway
// reports.
func NormalizeVideoStatus(status string) string {
	switch strings.ToLower(status) {
	case "completed", "succeeded", "success", "done":
		return "completed"
	case "failed", "error", "cancelled", "canceled":
		return "failed"
	case "in_progress", "running", "processing":
		return "in_progress"
	}
	return "queued"
}

// mediaForm builds a multipart body: the field order and the file part's type
// matter to some providers, so the body is written explicitly.
type mediaForm struct {
	body   bytes.Buffer
	writer *multipart.Writer
}

// newMediaForm starts a multipart body.
func newMediaForm() *mediaForm {
	form := &mediaForm{}
	form.writer = multipart.NewWriter(&form.body)
	return form
}

// field adds one text field.
func (f *mediaForm) field(name, value string) {
	_ = f.writer.WriteField(name, value)
}

// file adds the uploaded audio.
func (f *mediaForm) file(name, filename, contentType string, data []byte) {
	part, err := f.writer.CreatePart(textproto.MIMEHeader{
		"Content-Disposition": {fmt.Sprintf(`form-data; name=%q; filename=%q`, name, filename)},
		"Content-Type":        {contentType},
	})
	if err != nil {
		return
	}
	_, _ = part.Write(data)
}

// close finishes the body and returns it with its content type.
func (f *mediaForm) close() ([]byte, string) {
	_ = f.writer.Close()
	return f.body.Bytes(), f.writer.FormDataContentType()
}
