package adapters

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"mime/multipart"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/llm/transport"
	"github.com/NeoLabs-Systems/neorouter/internal/providers"
)

// cloudflare is Cloudflare Workers AI. Chat is the OpenAI-compatible path (the
// account's base URL carries the account id); the key check, the model listing
// and the media calls are Workers AI's own.
type cloudflare struct {
	openAICompatible
}

// cloudflareAPI is Cloudflare's control-plane API. It is a variable so a test
// can point the key check and the model listing at a fake.
var cloudflareAPI = "https://api.cloudflare.com/client/v4"

// cloudflareTasks are the model tasks the listing walks, with the kind each one
// serves.
var cloudflareTasks = [][2]string{
	{"Text Generation", providers.KindChat},
	{"Text Embeddings", providers.KindEmbedding},
	{"Text-to-Image", providers.KindImage},
	{"Automatic Speech Recognition", providers.KindTranscription},
	{"Text-to-Speech", providers.KindSpeech},
}

// cloudflareVision is the model-id pattern of a vision model.
var cloudflareVision = regexp.MustCompile(`(?i)vision|llava|-vl`)

// newCloudflare builds the Workers AI dialect.
func newCloudflare(def providers.Definition, deps providers.AdapterDeps) *cloudflare {
	adapter := &cloudflare{openAICompatible: openAICompatible{baseAdapter: newBaseAdapter(def, deps)}}
	adapter.self = adapter
	adapter.supported = providers.Of(
		providers.CapChat, providers.CapEmbed, providers.CapGenerateImage,
		providers.CapSpeech, providers.CapTranscribe,
	)
	return adapter
}

// accountAPI is the account's control-plane endpoint.
func (a *cloudflare) accountAPI() string {
	return cloudflareAPI + "/accounts/" + chatEncodeURIComponent(a.optionString("accountId"))
}

// Validate checks the token against Cloudflare's own token endpoint.
func (a *cloudflare) Validate(ctx context.Context) (providers.Validation, error) {
	for _, path := range []string{
		"/user/tokens/verify",
		"/accounts/" + chatEncodeURIComponent(a.optionString("accountId")) + "/tokens/verify",
	} {
		response, err := a.get(ctx, cloudflareAPI+path, validationTimeoutMS*time.Millisecond)
		if err != nil {
			if providers.KindOf(err) != providers.KindAuth {
				return providers.UnknownKey(err.Error()), nil
			}
			continue
		}
		data, _ := response.Data.(map[string]any)
		result, _ := data["result"].(map[string]any)
		status, _ := result["status"].(string)
		if status == "active" {
			return providers.ValidKey(), nil
		}
		if status == "" {
			status = "unknown"
		}
		return providers.InvalidKey("Token status: " + status), nil
	}
	return providers.InvalidKey("Cloudflare rejected this API token."), nil
}

// ListModels walks Workers AI's catalogue per task.
func (a *cloudflare) ListModels(ctx context.Context) ([]providers.Model, error) {
	var models []providers.Model
	for _, task := range cloudflareTasks {
		path := a.accountAPI() + "/ai/models/search?task=" + chatEncodeURIComponent(task[0]) + "&per_page=1000"
		response, err := a.get(ctx, path, listTimeoutMS*time.Millisecond)
		if err != nil {
			return nil, err
		}
		data, _ := response.Data.(map[string]any)
		for _, entry := range chatList(data["result"]) {
			raw := chatPlainObject(entry)
			name, _ := raw["name"].(string)
			normalized := map[string]any{"id": name, "name": name}
			if description, ok := raw["description"].(string); ok {
				normalized["description"] = description
			}
			overrides := providers.ModelOverrides{
				ContextWindow: cloudflareInt(cloudflareProperty(entry, "context_window")),
				Tools:         chatFlag(chatString(cloudflareProperty(entry, "function_calling")) == "true"),
				Vision:        chatFlag(cloudflareVision.MatchString(name)),
			}
			if task[1] != providers.KindChat {
				kind := task[1]
				overrides.Kind = &kind
			}
			models = append(models, providers.NormalizeModel(normalized, overrides))
		}
	}
	return models, nil
}

// cloudflareProperty reads one property's value out of a listing entry.
func cloudflareProperty(entry any, id string) any {
	raw, _ := entry.(map[string]any)
	for _, item := range chatList(raw["properties"]) {
		property, _ := item.(map[string]any)
		if property["property_id"] == id {
			return property["value"]
		}
	}
	return nil
}

// cloudflareInt reads a listing property as a positive integer.
func cloudflareInt(value any) *int {
	number, ok := chatToNumber(value)
	if !ok || number <= 0 {
		return nil
	}
	out := int(number)
	return &out
}

// cloudflareRun is one /ai/run answer: JSON with a result, or raw bytes.
type cloudflareRun struct {
	result  any
	bytes   []byte
	mime    string
	headers http.Header
}

// run calls a Workers AI model: /ai/run answers JSON ({ result }) or raw bytes
// depending on the model.
func (a *cloudflare) run(ctx context.Context, modelID string, body any, contentType string, timeout time.Duration, extra map[string]string) (*cloudflareRun, error) {
	headers := a.headers(extra)
	request := providers.Request{Method: http.MethodPost, Headers: httpHeaders(headers)}
	switch value := body.(type) {
	case io.Reader:
		request.Body = value
		request.ContentType = contentType
	case []byte:
		request.Body = bytes.NewReader(value)
		request.ContentType = contentType
	default:
		request.JSON = body
	}
	response, err := a.requestRaw(ctx, a.accountAPI()+"/ai/run/"+modelID, request, timeout)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, providers.FromNetwork(err)
	}
	mime := response.Header.Get("Content-Type")
	if !strings.Contains(mime, "json") {
		return &cloudflareRun{bytes: raw, mime: mime, headers: response.Header}, nil
	}
	decoded, decodeErr := chatDecodeJSON(raw)
	if decodeErr != nil {
		return nil, providers.NewError(providers.KindInvalidOutput, "Workers AI returned a non-JSON response.", providers.ErrorOptions{Headers: response.Header})
	}
	data := chatObj(decoded)
	if success, present := data.Get("success"); present && success == false {
		message := "Workers AI rejected the request."
		if errors := chatList(chatGet(data, "errors")); len(errors) > 0 {
			if text := chatField(chatObj(errors[0]), "message"); text != "" {
				message = text
			}
		}
		return nil, providers.NewError(providers.KindBadRequest, message, providers.ErrorOptions{Headers: response.Header})
	}
	result := chatGet(data, "result")
	if result == nil {
		result = decoded
	}
	return &cloudflareRun{result: result, mime: mime, headers: response.Header}, nil
}

// GenerateImage makes images with a Workers AI image model.
func (a *cloudflare) GenerateImage(ctx context.Context, req providers.ImageRequest, call providers.Call) (*providers.ImageResult, error) {
	width, height := cloudflareSize(req.Size)
	count := req.N
	if count <= 0 {
		count = 1
	}
	var images []providers.Image
	var headers http.Header
	for index := 0; index < min(count, 4); index++ {
		output, err := a.imageCall(ctx, call.ModelID, req.Prompt, width, height, a.timeoutFor(call))
		if err != nil {
			return nil, err
		}
		headers = output.headers
		if len(output.bytes) > 0 {
			mime := output.mime
			if mime == "" {
				mime = "image/png"
			}
			images = append(images, providers.Image{B64: base64.StdEncoding.EncodeToString(output.bytes), Mime: mime})
			continue
		}
		if image := chatField(chatObj(output.result), "image"); image != "" {
			images = append(images, providers.Image{B64: image, Mime: "image/jpeg"})
		}
	}
	if len(images) == 0 {
		return nil, providers.NewError(providers.KindEmpty, "Workers AI returned no image.", providers.ErrorOptions{Headers: headers})
	}
	return &providers.ImageResult{Images: images, Headers: headers}, nil
}

// imageCall sends one image request: FLUX.2 models only take multipart form
// data (their step count is fixed), the other image models take JSON.
func (a *cloudflare) imageCall(ctx context.Context, modelID, prompt string, width, height int, timeout time.Duration) (*cloudflareRun, error) {
	if !cloudflareFlux2.MatchString(modelID) {
		body := transport.NewObj().Set("prompt", prompt)
		if width > 0 {
			body.Set("width", width).Set("height", height)
		}
		return a.run(ctx, modelID, body, "", timeout, nil)
	}
	var form bytes.Buffer
	writer := multipart.NewWriter(&form)
	if err := writer.WriteField("prompt", prompt); err != nil {
		return nil, providers.NewError(providers.KindBadRequest, "The request could not be encoded.", providers.ErrorOptions{})
	}
	if width > 0 {
		if err := writer.WriteField("width", strconv.Itoa(width)); err != nil {
			return nil, providers.NewError(providers.KindBadRequest, "The request could not be encoded.", providers.ErrorOptions{})
		}
		if err := writer.WriteField("height", strconv.Itoa(height)); err != nil {
			return nil, providers.NewError(providers.KindBadRequest, "The request could not be encoded.", providers.ErrorOptions{})
		}
	}
	if err := writer.Close(); err != nil {
		return nil, providers.NewError(providers.KindBadRequest, "The request could not be encoded.", providers.ErrorOptions{})
	}
	return a.run(ctx, modelID, &form, writer.FormDataContentType(), timeout, nil)
}

// cloudflareFlux2 is the model family that only takes a form body.
var cloudflareFlux2 = regexp.MustCompile(`(?i)flux-2`)

// cloudflareSize reads a "WxH" size.
func cloudflareSize(size string) (int, int) {
	match := aspectSize.FindStringSubmatch(size)
	if match == nil {
		return 0, 0
	}
	return chatAtoi(match[1], 0), chatAtoi(match[2], 0)
}

// Speech synthesizes audio: MeloTTS takes a prompt and a language, Deepgram
// Aura takes text and a speaker.
func (a *cloudflare) Speech(ctx context.Context, req providers.SpeechRequest, call providers.Call) (*providers.SpeechResult, error) {
	isAura := cloudflareAura.MatchString(call.ModelID)
	var body *transport.Obj
	if isAura {
		body = transport.NewObj().Set("text", req.Input)
		if speaker := cloudflareSpeaker(call.ModelID, req.Voice); speaker != "" {
			body.Set("speaker", speaker)
		}
	} else {
		body = transport.NewObj().Set("prompt", req.Input).Set("lang", "en")
	}
	output, err := a.run(ctx, call.ModelID, body, "", a.timeoutFor(call), nil)
	if err != nil {
		return nil, err
	}
	if len(output.bytes) > 0 {
		mime := output.mime
		if mime == "" {
			mime = "audio/mpeg"
		}
		return &providers.SpeechResult{Audio: output.bytes, Mime: mime, Headers: output.headers}, nil
	}
	audio := chatField(chatObj(output.result), "audio")
	if audio == "" {
		return nil, providers.NewError(providers.KindEmpty, "Workers AI returned no audio.", providers.ErrorOptions{Headers: output.headers})
	}
	decoded, decodeErr := base64.StdEncoding.DecodeString(audio)
	if decodeErr != nil {
		return nil, providers.NewError(providers.KindInvalidOutput, "Workers AI returned audio that is not base64.", providers.ErrorOptions{Headers: output.headers})
	}
	return &providers.SpeechResult{Audio: decoded, Mime: "audio/mpeg", Headers: output.headers}, nil
}

// cloudflareAura is the Deepgram Aura model family on Workers AI.
var cloudflareAura = regexp.MustCompile(`(?i)aura`)

// cloudflareAuraVoices are Aura's speakers per model. Aura refuses any other
// name (another provider's voice such as Gemini's "Kore"), so those get its
// default voice.
var cloudflareAuraVoices = map[string][]string{
	"aura-2-en": {
		"amalthea", "andromeda", "apollo", "arcas", "aries", "asteria", "athena", "atlas", "aurora",
		"callista", "cora", "cordelia", "delia", "draco", "electra", "harmonia", "helena", "hera",
		"hermes", "hyperion", "iris", "janus", "juno", "jupiter", "luna", "mars", "minerva",
		"neptune", "odysseus", "ophelia", "orion", "orpheus", "pandora", "phoebe", "pluto",
		"saturn", "thalia", "theia", "vesta", "zeus",
	},
	"aura-2-es": {"aquila", "alvaro", "carina", "celeste", "diana", "estrella", "javier", "nestor", "selena", "sirio"},
	"aura-1":    {"angus", "arcas", "asteria", "athena", "helios", "hera", "luna", "orion", "orpheus", "perseus", "stella", "zeus"},
}

// cloudflareSpeaker is the Aura speaker for a model and requested voice.
func cloudflareSpeaker(modelID, voice string) string {
	name := strings.ToLower(voice)
	for model, voices := range cloudflareAuraVoices {
		if !strings.Contains(modelID, model) {
			continue
		}
		if chatInList(voices, name) {
			return name
		}
	}
	return ""
}

// Transcribe reads a transcript out of an audio request.
func (a *cloudflare) Transcribe(ctx context.Context, req providers.TranscribeRequest, call providers.Call) (*providers.Transcript, error) {
	if cloudflareNova.MatchString(call.ModelID) {
		return a.transcribeNova(ctx, req, call)
	}
	body := transport.NewObj()
	if cloudflareWhisper.MatchString(call.ModelID) {
		body.Set("audio", base64.StdEncoding.EncodeToString(req.File))
		if req.Language != "" {
			body.Set("language", req.Language)
		}
		if req.Translate {
			body.Set("task", "translate")
		}
	} else {
		bytes := make([]any, 0, len(req.File))
		for _, value := range req.File {
			bytes = append(bytes, value)
		}
		body.Set("audio", bytes)
	}
	output, err := a.run(ctx, call.ModelID, body, "", a.timeoutFor(call), nil)
	if err != nil {
		return nil, err
	}
	result := chatObj(output.result)
	text := chatField(result, "text")
	if text == "" {
		return nil, providers.NewError(providers.KindInvalidOutput, "Workers AI returned no transcript.", providers.ErrorOptions{Headers: output.headers})
	}
	transcript := &providers.Transcript{Text: text, Language: req.Language, Headers: output.headers}
	info := chatObj(chatGet(result, "transcription_info"))
	if language := chatField(info, "language"); language != "" {
		transcript.Language = language
	}
	if duration, ok := chatNumber(info, "duration"); ok {
		transcript.Duration = &duration
	}
	for _, raw := range chatList(chatGet(result, "segments")) {
		segment := chatObj(raw)
		start, _ := chatNumber(segment, "start")
		end, _ := chatNumber(segment, "end")
		transcript.Segments = append(transcript.Segments, providers.Segment{Start: start, End: end, Text: chatField(segment, "text")})
	}
	return transcript, nil
}

// cloudflareWhisper is the Whisper model family, which takes base64 audio.
var cloudflareWhisper = regexp.MustCompile(`(?i)turbo|large-v3`)

// cloudflareNova is Deepgram Nova on Workers AI.
var cloudflareNova = regexp.MustCompile(`deepgram/nova`)

// transcribeNova calls Nova's schema: `audio: { body, contentType }`, which over
// the REST API means the raw audio as the request body with its content type.
func (a *cloudflare) transcribeNova(ctx context.Context, req providers.TranscribeRequest, call providers.Call) (*providers.Transcript, error) {
	mime := req.Mime
	if mime == "" {
		mime = "application/octet-stream"
	}
	output, err := a.run(ctx, call.ModelID, req.File, mime, a.timeoutFor(call), nil)
	if err != nil {
		return nil, err
	}
	channels := chatList(chatGet(chatObj(chatGet(chatObj(output.result), "results")), "channels"))
	if len(channels) == 0 {
		return nil, providers.NewError(providers.KindInvalidOutput, "Workers AI returned no transcript.", providers.ErrorOptions{Headers: output.headers})
	}
	channel := chatObj(channels[0])
	alternatives := chatList(chatGet(channel, "alternatives"))
	if len(alternatives) == 0 {
		return nil, providers.NewError(providers.KindInvalidOutput, "Workers AI returned no transcript.", providers.ErrorOptions{Headers: output.headers})
	}
	text := chatField(chatObj(alternatives[0]), "transcript")
	if text == "" {
		return nil, providers.NewError(providers.KindInvalidOutput, "Workers AI returned no transcript.", providers.ErrorOptions{Headers: output.headers})
	}
	transcript := &providers.Transcript{Text: text, Headers: output.headers}
	if language := chatField(channel, "detected_language"); language != "" {
		transcript.Language = language
	}
	return transcript, nil
}
