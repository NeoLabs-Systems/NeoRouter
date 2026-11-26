package adapters

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"iter"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/llm/transport"
	"github.com/NeoLabs-Systems/neorouter/internal/providers"
)

// googleGemini is providers/adapters/google_gemini.js: Gemini's Generative
// Language API. Gemini speaks its own dialect for chat, embeddings, images,
// speech and transcription, so every call is translated here.
type googleGemini struct {
	baseAdapter
	signatures geminiSignatures
}

// geminiModelPath is the models segment of a Gemini URL.
const geminiModelPath = "/models/"

// newGoogleGemini builds Gemini's dialect.
func newGoogleGemini(def providers.Definition, deps providers.AdapterDeps) *googleGemini {
	adapter := &googleGemini{baseAdapter: newBaseAdapter(def, deps)}
	adapter.self = adapter
	adapter.auth = adapter.authHeaders
	// Gemini has its own endpoints for these, whatever the definition declares.
	adapter.supported = providers.Of(
		providers.CapChat, providers.CapEmbed, providers.CapGenerateImage,
		providers.CapSpeech, providers.CapTranscribe,
	)
	return adapter
}

// TranscribeTimestamps is false: Gemini's transcripts carry no word timings, so
// subtitles go elsewhere where possible.
func (a *googleGemini) TranscribeTimestamps() bool { return false }

// authHeaders is Google's key header.
func (a *googleGemini) authHeaders() map[string]string {
	if a.secret == "" {
		return nil
	}
	return map[string]string{"x-goog-api-key": a.secret}
}

// modelPath is one model's endpoint, with the path escaped as a path segment.
func geminiModelURL(modelID, method string) string {
	return geminiModelPath + chatEncodeURIComponent(modelID) + ":" + method
}

// ListModels pages through Gemini's listing, keeping the models that can chat,
// embed or speak live voice and leaving out the rest.
func (a *googleGemini) ListModels(ctx context.Context) ([]providers.Model, error) {
	var models []providers.Model
	token := ""
	for page := 0; page < 20; page++ {
		path := "/models?pageSize=1000"
		if token != "" {
			path += "&pageToken=" + chatEncodeURIComponent(token)
		}
		response, err := a.get(ctx, path, listTimeoutMS*time.Millisecond)
		if err != nil {
			return nil, err
		}
		data, _ := response.Data.(map[string]any)
		for _, entry := range chatList(data["models"]) {
			raw := chatPlainObject(entry)
			if raw == nil {
				continue
			}
			name, _ := raw["name"].(string)
			id := strings.TrimPrefix(name, "models/")
			raw["id"] = id
			display, _ := raw["displayName"].(string)
			if display == "" {
				display = id
			}
			raw["name"] = display
			methods := stringList(raw["supportedGenerationMethods"])
			contextWindow := geminiInt(raw["inputTokenLimit"])
			switch {
			case chatInList(methods, "embedContent"):
				raw["type"] = "embedding"
				models = append(models, providers.NormalizeModel(raw, providers.ModelOverrides{ContextWindow: contextWindow}))
			case chatInList(methods, "bidiGenerateContent") && !chatInList(methods, "generateContent"):
				// Gemini Live models only speak the bidirectional WebSocket API.
				raw["type"] = "live"
				models = append(models, providers.NormalizeModel(raw, providers.ModelOverrides{ContextWindow: contextWindow}))
			case !chatInList(methods, "generateContent"):
				continue
			default:
				thinking, _ := raw["thinking"].(bool)
				models = append(models, providers.NormalizeModel(raw, providers.ModelOverrides{
					Tools:     chatFlag(!geminiNoTools.MatchString(id)),
					Vision:    chatFlag(!geminiNoVision.MatchString(id)),
					JSON:      chatFlag(!geminiNoJSON.MatchString(id)),
					Reasoning: chatFlag(thinking || geminiReasoningModel.MatchString(id)),
				}))
			}
		}
		next, _ := data["nextPageToken"].(string)
		if next == "" {
			break
		}
		token = next
	}
	return models, nil
}

// The capabilities Gemini's own listing does not publish, read from the id.
var (
	geminiNoTools  = regexp.MustCompile(`(?i)gemma|tts|image|embedding|aqa`)
	geminiNoVision = regexp.MustCompile(`(?i)tts|embedding|aqa`)
	geminiNoJSON   = regexp.MustCompile(`(?i)gemma|tts`)
)

// Chat sends one canonical request as a generateContent call.
func (a *googleGemini) Chat(ctx context.Context, req json.RawMessage, call providers.Call) (*providers.ChatResult, error) {
	canonical, err := chatParseBody(req)
	if err != nil {
		return nil, err
	}
	body, err := a.geminiRequest(canonical, call.ModelID)
	if err != nil {
		return nil, err
	}
	response, err := a.post(ctx, geminiModelURL(call.ModelID, "generateContent"), body, a.timeoutFor(call))
	if err != nil {
		return nil, err
	}
	data := chatObj(chatPlainValue(response.Data))
	if failure := geminiCandidateCheck(data); failure != nil {
		return nil, failure
	}
	var candidate *transport.Obj
	if candidates := chatList(chatGet(data, "candidates")); len(candidates) > 0 {
		candidate = chatObj(candidates[0])
	}
	if candidate == nil {
		return nil, providers.EmptyResponseError()
	}
	counter := 0
	delta := a.geminiDelta(chatList(chatGet(chatObj(chatGet(candidate, "content")), "parts")), &counter)
	finish := "stop"
	if len(chatList(chatGet(delta, "tool_calls"))) > 0 {
		finish = "tool_calls"
	} else if mapped, ok := geminiFinish[chatField(candidate, "finishReason")]; ok {
		finish = mapped
	}
	id := chatField(data, "responseId")
	completion := chatBuildResponse(id, call.ModelID, geminiMessage(delta), finish, geminiUsage(chatObj(chatGet(data, "usageMetadata"))))
	encoded, err := chatMarshal(completion)
	if err != nil {
		return nil, providers.NewError(providers.KindInvalidOutput, "The answer could not be encoded.", providers.ErrorOptions{})
	}
	return &providers.ChatResult{Body: encoded, Headers: response.Headers}, nil
}

// Stream sends one canonical request as a streamed generateContent call.
func (a *googleGemini) Stream(ctx context.Context, req json.RawMessage, call providers.Call) iter.Seq2[json.RawMessage, error] {
	return func(yield func(json.RawMessage, error) bool) {
		canonical, err := chatParseBody(req)
		if err != nil {
			yield(nil, err)
			return
		}
		body, err := a.geminiRequest(canonical, call.ModelID)
		if err != nil {
			yield(nil, err)
			return
		}
		headers := a.headers(map[string]string{"Accept": "text/event-stream"})
		response, callErr := a.requestRaw(ctx, geminiModelURL(call.ModelID, "streamGenerateContent")+"?alt=sse", providers.Request{
			Method:  http.MethodPost,
			Headers: httpHeaders(headers),
			JSON:    body,
		}, a.timeoutFor(call))
		if callErr != nil {
			yield(nil, callErr)
			return
		}
		defer response.Body.Close()
		if call.OnHeaders != nil {
			call.OnHeaders(response.Header)
		}
		id := chatCompletionID()
		created := chatNowSeconds()
		counter := 0
		var usage *transport.Obj
		var finish any
		sawTools := false
		for event, streamErr := range providers.SSE(response.Body) {
			if streamErr != nil {
				yield(nil, streamErr)
				return
			}
			decoded, decodeErr := chatDecodeJSON([]byte(event.Data))
			if decodeErr != nil {
				continue
			}
			payload := chatObj(decoded)
			if failure := geminiCandidateCheck(payload); failure != nil {
				yield(nil, failure)
				return
			}
			if metadata := chatObj(chatGet(payload, "usageMetadata")); metadata != nil {
				usage = geminiUsage(metadata)
			}
			candidates := chatList(chatGet(payload, "candidates"))
			if len(candidates) == 0 {
				continue
			}
			candidate := chatObj(candidates[0])
			delta := a.geminiDelta(chatList(chatGet(chatObj(chatGet(candidate, "content")), "parts")), &counter)
			if len(chatList(chatGet(delta, "tool_calls"))) > 0 {
				sawTools = true
			}
			if len(delta.Keys()) > 0 {
				if !yield(chatEmit(chatBuildChunk(id, call.ModelID, created, delta, nil, nil))) {
					return
				}
			}
			if reason := chatField(candidate, "finishReason"); reason != "" {
				finish = geminiFinish[reason]
			}
		}
		finalFinish := finish
		if sawTools {
			finalFinish = "tool_calls"
		}
		yield(chatEmit(chatBuildChunk(id, call.ModelID, created, transport.NewObj(), chatFinishOr(finalFinish, "stop"), usage)))
	}
}

// Embed turns text into vectors with a batch embed call.
func (a *googleGemini) Embed(ctx context.Context, req providers.EmbedRequest, call providers.Call) (*providers.EmbedResult, error) {
	input := chatInputTexts(req.Input)
	// Only the first-generation model takes a task type.
	taskType := ""
	if strings.HasSuffix(call.ModelID, "embedding-001") {
		if req.InputType == "passage" {
			taskType = "RETRIEVAL_DOCUMENT"
		} else {
			taskType = "RETRIEVAL_QUERY"
		}
	}
	requests := make([]any, 0, len(input))
	for _, text := range input {
		entry := transport.NewObj().
			Set("model", "models/"+call.ModelID).
			Set("content", transport.NewObj().Set("parts", []any{transport.NewObj().Set("text", text)}))
		if req.Dimensions != nil && *req.Dimensions > 0 {
			entry.Set("outputDimensionality", *req.Dimensions)
		}
		if taskType != "" {
			entry.Set("taskType", taskType)
		}
		requests = append(requests, entry)
	}
	response, err := a.post(ctx, geminiModelURL(call.ModelID, "batchEmbedContents"), transport.NewObj().Set("requests", requests), a.timeoutFor(call))
	if err != nil {
		return nil, err
	}
	data := chatObj(chatPlainValue(response.Data))
	if failure := geminiCandidateCheck(data); failure != nil {
		return nil, failure
	}
	var vectors [][]float64
	for _, raw := range chatList(chatGet(data, "embeddings")) {
		values := chatList(chatGet(chatObj(raw), "values"))
		vector := make([]float64, 0, len(values))
		valid := true
		for _, item := range values {
			number, ok := chatToNumber(item)
			if !ok {
				valid = false
				break
			}
			vector = append(vector, number)
		}
		if !valid {
			vectors = nil
			break
		}
		vectors = append(vectors, vector)
	}
	if len(vectors) != len(input) || len(input) == 0 {
		return nil, providers.NewError(providers.KindInvalidOutput, "Gemini returned an unexpected embeddings response.", providers.ErrorOptions{Headers: response.Headers})
	}
	return &providers.EmbedResult{Vectors: vectors, Headers: response.Headers}, nil
}

// GenerateImage asks an image model for pictures: Gemini image models ("Nano
// Banana") answer generateContent with image parts.
func (a *googleGemini) GenerateImage(ctx context.Context, req providers.ImageRequest, call providers.Call) (*providers.ImageResult, error) {
	count := req.N
	if count <= 0 {
		count = 1
	}
	var images []providers.Image
	var headers http.Header
	for index := 0; index < min(count, 4); index++ {
		config := transport.NewObj().Set("responseModalities", []any{"TEXT", "IMAGE"})
		if req.Size != "" {
			config.Set("imageConfig", transport.NewObj().Set("aspectRatio", chatAspectRatio(req.Size)))
		}
		body := transport.NewObj().
			Set("contents", []any{transport.NewObj().Set("parts", []any{transport.NewObj().Set("text", req.Prompt)})}).
			Set("generationConfig", config)
		response, err := a.post(ctx, geminiModelURL(call.ModelID, "generateContent"), body, a.timeoutFor(call))
		if err != nil {
			return nil, err
		}
		headers = response.Headers
		data := chatObj(chatPlainValue(response.Data))
		if failure := geminiCandidateCheck(data); failure != nil {
			return nil, failure
		}
		for _, raw := range geminiCandidateParts(data) {
			part := chatObj(raw)
			inline := chatObj(chatGet(part, "inlineData"))
			mimeType := chatField(inline, "mimeType")
			payload := chatField(inline, "data")
			if payload == "" || !strings.HasPrefix(mimeType, "image/") {
				continue
			}
			images = append(images, providers.Image{B64: payload, Mime: mimeType})
		}
	}
	if len(images) == 0 {
		return nil, providers.NewError(providers.KindEmpty, "Gemini returned no image.", providers.ErrorOptions{Headers: headers})
	}
	return &providers.ImageResult{Images: images, Headers: headers}, nil
}

// Speech synthesizes audio with Gemini's TTS models.
func (a *googleGemini) Speech(ctx context.Context, req providers.SpeechRequest, call providers.Call) (*providers.SpeechResult, error) {
	body := transport.NewObj().
		Set("contents", []any{transport.NewObj().Set("parts", []any{transport.NewObj().Set("text", req.Input)})}).
		Set("generationConfig", transport.NewObj().
			Set("responseModalities", []any{"AUDIO"}).
			Set("speechConfig", transport.NewObj().Set("voiceConfig", transport.NewObj().
				Set("prebuiltVoiceConfig", transport.NewObj().Set("voiceName", geminiVoice(req.Voice))))))
	response, err := a.post(ctx, geminiModelURL(call.ModelID, "generateContent"), body, a.timeoutFor(call))
	if err != nil {
		return nil, err
	}
	data := chatObj(chatPlainValue(response.Data))
	if failure := geminiCandidateCheck(data); failure != nil {
		return nil, failure
	}
	for _, raw := range geminiCandidateParts(data) {
		inline := chatObj(chatGet(chatObj(raw), "inlineData"))
		payload := chatField(inline, "data")
		if payload == "" {
			continue
		}
		audio, decodeErr := base64.StdEncoding.DecodeString(payload)
		if decodeErr != nil {
			return nil, providers.NewError(providers.KindInvalidOutput, "Gemini returned audio that is not base64.", providers.ErrorOptions{Headers: response.Headers})
		}
		mime := chatField(inline, "mimeType")
		if mime == "" {
			mime = "audio/wav"
		}
		if geminiPCM.MatchString(mime) {
			// Raw 16-bit PCM gets a WAV header so players accept it.
			audio = geminiWAVFromPCM(audio, mime)
			mime = "audio/wav"
		}
		return &providers.SpeechResult{Audio: audio, Mime: mime, Headers: response.Headers}, nil
	}
	return nil, providers.EmptyResponseError()
}

// geminiPCM is the MIME type of raw PCM audio.
var geminiPCM = regexp.MustCompile(`(?i)l16|pcm`)

// Transcribe reads a transcript out of an audio request.
func (a *googleGemini) Transcribe(ctx context.Context, req providers.TranscribeRequest, call providers.Call) (*providers.Transcript, error) {
	instruction := "Transcribe this audio exactly"
	if req.Translate {
		instruction = "Translate the speech in this audio into English. Reply with the English text only."
	} else if req.Language != "" {
		instruction = "Transcribe this audio exactly (language: " + req.Language + "). Reply with the transcript only."
	} else {
		instruction += ". Reply with the transcript only."
	}
	if req.Prompt != "" {
		instruction += " Context: " + req.Prompt
	}
	mime := req.Mime
	if mime == "" {
		mime = "audio/wav"
	}
	body := transport.NewObj().Set("contents", []any{transport.NewObj().Set("parts", []any{
		transport.NewObj().Set("text", instruction),
		transport.NewObj().Set("inlineData", transport.NewObj().
			Set("mimeType", mime).
			Set("data", base64.StdEncoding.EncodeToString(req.File))),
	})})
	response, err := a.post(ctx, geminiModelURL(call.ModelID, "generateContent"), body, a.timeoutFor(call))
	if err != nil {
		return nil, err
	}
	data := chatObj(chatPlainValue(response.Data))
	if failure := geminiCandidateCheck(data); failure != nil {
		return nil, failure
	}
	var transcript strings.Builder
	for _, raw := range geminiCandidateParts(data) {
		part := chatObj(raw)
		if text := chatField(chatObj(chatGet(part, "audioTranscription")), "text"); text != "" {
			transcript.WriteString(text)
			continue
		}
		if text, ok := chatGet(part, "text").(string); ok {
			transcript.WriteString(text)
		}
	}
	text := strings.TrimSpace(transcript.String())
	if text == "" {
		return nil, providers.EmptyResponseError()
	}
	return &providers.Transcript{Text: text, Language: req.Language, Headers: response.Headers}, nil
}

// geminiCandidateParts is the first candidate's parts.
func geminiCandidateParts(data *transport.Obj) []any {
	candidates := chatList(chatGet(data, "candidates"))
	if len(candidates) == 0 {
		return nil
	}
	return chatList(chatGet(chatObj(chatGet(chatObj(candidates[0]), "content")), "parts"))
}

// geminiImagePart turns a canonical image part into Gemini's inline data: a data
// URL is decoded, a remote one is fetched.
func (a *googleGemini) geminiImagePart(part *transport.Obj) (*transport.Obj, error) {
	url := chatImageURL(part)
	if mimeType, data, ok := chatDataURI(url); ok {
		return transport.NewObj().Set("inlineData", transport.NewObj().
			Set("mimeType", mimeType).
			Set("data", data)), nil
	}
	response, err := a.client.Do(context.Background(), url, providers.Request{
		Method:     http.MethodGet,
		Timeout:    15 * time.Second,
		PublicOnly: a.account.PublicOnly,
		Owner:      a.account.UserID,
	})
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, providers.FromNetwork(err)
	}
	mimeType := response.Header.Get("Content-Type")
	if mimeType == "" {
		mimeType = "image/png"
	}
	return transport.NewObj().Set("inlineData", transport.NewObj().
		Set("mimeType", mimeType).
		Set("data", base64.StdEncoding.EncodeToString(payload))), nil
}

// chatInputTexts is an embedding input as a list of strings.
func chatInputTexts(input any) []string {
	switch value := input.(type) {
	case string:
		return []string{value}
	case []string:
		return value
	case []any:
		out := make([]string, 0, len(value))
		for _, item := range value {
			out = append(out, chatString(item))
		}
		return out
	}
	return nil
}

// chatAspectRatio is openai_media.aspectRatio: "1280x720" as "16:9".
func chatAspectRatio(size string) string {
	match := aspectSize.FindStringSubmatch(size)
	if match == nil {
		return size
	}
	width := chatAtoi(match[1], 0)
	height := chatAtoi(match[2], 0)
	if width == 0 || height == 0 {
		return size
	}
	divisor := chatGCD(width, height)
	return chatItoa(width/divisor) + ":" + chatItoa(height/divisor)
}

// aspectSize is the WxH shape a requested image size has.
var aspectSize = regexp.MustCompile(`^(\d+)x(\d+)$`)

// stringList reads a decoded JSON array as strings.
func stringList(value any) []string {
	items := chatList(value)
	if items == nil {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if text, ok := item.(string); ok {
			out = append(out, text)
		}
	}
	return out
}

// geminiInt reads a listing's positive integer.
func geminiInt(value any) *int {
	number, ok := chatToNumber(value)
	if !ok || number <= 0 {
		return nil
	}
	out := int(number)
	return &out
}

// boolValue is a definition override that asserts or denies a capability.
func chatFlag(value bool) *bool { return &value }

// atoiOr parses a decimal, the fallback when it is not one.
func chatAtoi(text string, fallback int) int {
	if value, err := strconv.Atoi(text); err == nil {
		return value
	}
	return fallback
}

// itoa renders a decimal.
func chatItoa(value int) string { return strconv.Itoa(value) }

// chatGCD is the greatest common divisor two image dimensions share.
func chatGCD(a, b int) int {
	for b != 0 {
		a, b = b, a%b
	}
	if a < 0 {
		return -a
	}
	return a
}
