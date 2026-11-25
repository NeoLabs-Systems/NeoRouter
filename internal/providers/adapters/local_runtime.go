package adapters

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"iter"
	"sort"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/llm/transport"
	"github.com/NeoLabs-Systems/neorouter/internal/providers"
)

// localRuntime is providers/adapters/local_runtime.js: the models running on
// this server. Chat, safety and embedding models are llama.cpp servers on
// loopback that speak OpenAI's API, so those calls go to an OpenAI-compatible
// adapter pointed at the model's own port; speech and transcription models run
// in a worker process. A model that is not running right now is a temporary
// server error, so the router tries something else.
type localRuntime struct {
	baseAdapter
	local LocalModels
}

// localNotRunningRetry is how long to wait before trying a model that is not
// running again.
const localNotRunningRetry = 30 * time.Second

// localUpstreamMedia is what llama.cpp serves besides chat.
var localUpstreamMedia = providers.MediaSpec{Embed: "/embeddings"}

// newLocalRuntime builds the local model dialect.
func newLocalRuntime(def providers.Definition, deps providers.AdapterDeps, local LocalModels) *localRuntime {
	adapter := &localRuntime{baseAdapter: newBaseAdapter(def, deps), local: local}
	adapter.self = adapter
	adapter.supported = providers.Of(providers.CapChat, providers.CapEmbed, providers.CapSpeech, providers.CapTranscribe)
	return adapter
}

// CanTranslate reports whether a recogniser translates into English (Whisper
// does, Moonshine does not).
func (a *localRuntime) CanTranslate(modelID string) bool {
	if a.local == nil {
		return false
	}
	for _, model := range a.local.Served() {
		if model.ID == modelID {
			return model.Translate
		}
	}
	return false
}

// allowed reports whether this person may use the server's local models.
func (a *localRuntime) allowed(ctx context.Context) bool {
	return a.local != nil && a.local.Allowed(ctx, a.userID())
}

// instance is the running model, or the failure a caller retries later.
func (a *localRuntime) instance(ctx context.Context, modelID string) (LocalInstance, error) {
	if !a.allowed(ctx) {
		return LocalInstance{}, providers.NewError(providers.KindAuth, "An admin has not shared this server’s local models with you.", providers.ErrorOptions{NeedsAction: true})
	}
	instance, running := a.local.Running(modelID)
	if !running {
		retryAfter := localNotRunningRetry
		return LocalInstance{}, providers.NewError(providers.KindServer, "This local model is not running right now.", providers.ErrorOptions{RetryAfter: &retryAfter})
	}
	return instance, nil
}

// upstream is the running llama.cpp server of a model, as an OpenAI-compatible
// connection.
func (a *localRuntime) upstream(ctx context.Context, modelID string) (*openAICompatible, error) {
	instance, err := a.instance(ctx, modelID)
	if err != nil {
		return nil, err
	}
	if instance.Endpoint == nil {
		return nil, providers.NewError(providers.KindBadRequest, "This local model does not answer this kind of request.", providers.ErrorOptions{})
	}
	definition := a.def
	definition.Media = localUpstreamMedia
	deps := a.deps
	deps.Account = providers.Account{
		BaseURL: instance.Endpoint.BaseURL,
		Secret:  instance.Endpoint.Token,
	}
	return newOpenAICompatible(definition, deps), nil
}

// Validate checks that this person may use the server's models and reports how
// many are running. A connection being created has no owner yet; who may add one
// is decided by the route.
func (a *localRuntime) Validate(ctx context.Context) (providers.Validation, error) {
	if a.userID() != "" && !a.allowed(ctx) {
		return providers.InvalidKey("An admin has not shared this server’s local models with you."), nil
	}
	count := 0
	if a.local != nil {
		count = len(a.local.Served())
	}
	if count == 0 {
		return providers.Validation{Status: providers.ValidationHealthy, Message: chatText("No local model is running right now.")}, nil
	}
	if count == 1 {
		return providers.Validation{Status: providers.ValidationHealthy, Message: chatText("1 local model running.")}, nil
	}
	return providers.Validation{Status: providers.ValidationHealthy, Message: chatText(chatItoa(count) + " local models running.")}, nil
}

// ListModels lists the models serving right now; local models are free.
func (a *localRuntime) ListModels(ctx context.Context) ([]providers.Model, error) {
	if !a.allowed(ctx) {
		return nil, nil
	}
	served := a.local.Served()
	models := make([]providers.Model, 0, len(served))
	zero := 0.0
	for _, model := range served {
		raw := map[string]any{"id": model.ID, "name": model.Name}
		kind := model.Kind
		overrides := providers.ModelOverrides{
			Tools:      chatFlag(model.Tools),
			Vision:     chatFlag(model.Vision),
			Reasoning:  chatFlag(model.Reasoning),
			PriceKnown: chatTrue(),
			PriceIn:    &zero,
			PriceOut:   &zero,
			FreeHint:   chatTrue(),
		}
		if kind != "" {
			overrides.Kind = &kind
		}
		if model.ContextWindow > 0 {
			window := model.ContextWindow
			overrides.ContextWindow = &window
		}
		models = append(models, providers.NormalizeModel(raw, overrides))
	}
	return models, nil
}

// Chat answers with the model's own llama.cpp server.
func (a *localRuntime) Chat(ctx context.Context, req json.RawMessage, call providers.Call) (*providers.ChatResult, error) {
	upstream, err := a.upstream(ctx, call.ModelID)
	if err != nil {
		return nil, err
	}
	return upstream.Chat(ctx, req, call)
}

// Stream answers with the model's own llama.cpp server.
func (a *localRuntime) Stream(ctx context.Context, req json.RawMessage, call providers.Call) iter.Seq2[json.RawMessage, error] {
	return func(yield func(json.RawMessage, error) bool) {
		upstream, err := a.upstream(ctx, call.ModelID)
		if err != nil {
			yield(nil, err)
			return
		}
		for chunk, streamErr := range upstream.Stream(ctx, req, call) {
			if !yield(chunk, streamErr) {
				return
			}
		}
	}
}

// Embed embeds with the model's own llama.cpp server, on the path llama.cpp
// serves embeddings from.
func (a *localRuntime) Embed(ctx context.Context, req providers.EmbedRequest, call providers.Call) (*providers.EmbedResult, error) {
	upstream, err := a.upstream(ctx, call.ModelID)
	if err != nil {
		return nil, err
	}
	input := chatInputTexts(req.Input)
	body := transport.NewObj().
		Set("model", call.ModelID).
		Set("input", input).
		Set("encoding_format", "float")
	if req.Dimensions != nil && *req.Dimensions > 0 {
		body.Set("dimensions", *req.Dimensions)
	}
	response, err := upstream.post(ctx, "/embeddings", body, upstream.timeoutFor(call))
	if err != nil {
		return nil, err
	}
	data, _ := response.Data.(map[string]any)
	rows := chatList(data["data"])
	sort.SliceStable(rows, func(i, j int) bool {
		left, _ := chatToNumber(chatMember(rows[i], "index"))
		right, _ := chatToNumber(chatMember(rows[j], "index"))
		return left < right
	})
	vectors := make([][]float64, 0, len(rows))
	for _, row := range rows {
		values := chatList(chatMember(row, "embedding"))
		if values == nil {
			return nil, providers.NewError(providers.KindInvalidOutput, "The provider returned an unexpected embeddings response.", providers.ErrorOptions{Headers: response.Headers})
		}
		vector := make([]float64, 0, len(values))
		for _, item := range values {
			number, ok := chatToNumber(item)
			if !ok {
				return nil, providers.NewError(providers.KindInvalidOutput, "The provider returned an unexpected embeddings response.", providers.ErrorOptions{Headers: response.Headers})
			}
			vector = append(vector, number)
		}
		vectors = append(vectors, vector)
	}
	if len(vectors) != len(input) || len(input) == 0 {
		return nil, providers.NewError(providers.KindInvalidOutput, "The provider returned an unexpected embeddings response.", providers.ErrorOptions{Headers: response.Headers})
	}
	result := &providers.EmbedResult{Vectors: vectors, Headers: response.Headers}
	if usage, ok := chatMember(data, "usage").(map[string]any); ok {
		if value, known := chatToNumber(usage["prompt_tokens"]); known {
			tokens := int(value)
			result.InputTokens = &tokens
		}
	}
	return result, nil
}

// Voices are the speakers a local speech model offers.
func (a *localRuntime) Voices(ctx context.Context, modelID string) ([]providers.Voice, error) {
	if a.local == nil {
		return nil, nil
	}
	for _, model := range a.local.Served() {
		if model.ID == modelID {
			return model.Voices, nil
		}
	}
	return nil, nil
}

// Speech synthesizes audio in the model's worker process, as WAV: the gateway
// converts it to the format the client asked for.
func (a *localRuntime) Speech(ctx context.Context, req providers.SpeechRequest, call providers.Call) (*providers.SpeechResult, error) {
	instance, err := a.instance(ctx, call.ModelID)
	if err != nil {
		return nil, err
	}
	if instance.Speech == nil {
		return nil, providers.NewError(providers.KindBadRequest, "This local model does not answer this kind of request.", providers.ErrorOptions{})
	}
	known, _ := a.Voices(ctx, call.ModelID)
	voice := ""
	for _, candidate := range known {
		if candidate.ID == req.Voice {
			voice = candidate.ID
			break
		}
	}
	if voice == "" && len(known) > 0 {
		voice = known[0].ID
	}
	output, invokeErr := instance.Speech(ctx, LocalSpeechInput{
		Text:    req.Input,
		Voice:   voice,
		Speed:   req.Speed,
		Timeout: a.timeoutFor(call),
	})
	if invokeErr != nil {
		return nil, localFailure(ctx, invokeErr)
	}
	return &providers.SpeechResult{Audio: chatWAVFromSamples(output.Samples, output.Rate), Mime: "audio/wav"}, nil
}

// Transcribe reads audio in the model's worker process.
func (a *localRuntime) Transcribe(ctx context.Context, req providers.TranscribeRequest, call providers.Call) (*providers.Transcript, error) {
	instance, err := a.instance(ctx, call.ModelID)
	if err != nil {
		return nil, err
	}
	if instance.Transcribe == nil {
		return nil, providers.NewError(providers.KindBadRequest, "This local model does not answer this kind of request.", providers.ErrorOptions{})
	}
	output, invokeErr := instance.Transcribe(ctx, LocalTranscribeInput{
		File:      req.File,
		Filename:  req.Filename,
		Mime:      req.Mime,
		Language:  req.Language,
		Translate: req.Translate,
		Timeout:   a.timeoutFor(call),
	})
	if invokeErr != nil {
		return nil, localFailure(ctx, invokeErr)
	}
	return &providers.Transcript{
		Text:     output.Text,
		Language: output.Language,
		Duration: output.Duration,
		Segments: output.Segments,
	}, nil
}

// localFailure reads a worker failure: a cancelled call aborted, a classified
// failure as it is, anything else the model failed.
func localFailure(ctx context.Context, err error) error {
	if ctx.Err() != nil || providers.KindOf(err) == providers.KindAborted {
		return providers.NewError(providers.KindAborted, "The request was cancelled.", providers.ErrorOptions{})
	}
	if classified := providers.AsProviderError(err); classified != nil {
		return classified
	}
	return providers.NewError(providers.KindServer, err.Error(), providers.ErrorOptions{})
}

// chatWAVFromSamples wraps samples in a 16-bit mono WAV file.
func chatWAVFromSamples(samples []float32, rate int) []byte {
	if rate <= 0 {
		rate = 24000
	}
	data := make([]byte, len(samples)*2)
	for i, sample := range samples {
		value := float64(sample)
		if value > 1 {
			value = 1
		}
		if value < -1 {
			value = -1
		}
		binary.LittleEndian.PutUint16(data[i*2:], uint16(int16(value*32767)))
	}
	header := make([]byte, 44)
	copy(header[0:], "RIFF")
	binary.LittleEndian.PutUint32(header[4:], uint32(36+len(data)))
	copy(header[8:], "WAVE")
	copy(header[12:], "fmt ")
	binary.LittleEndian.PutUint32(header[16:], 16)
	binary.LittleEndian.PutUint16(header[20:], 1)
	binary.LittleEndian.PutUint16(header[22:], 1)
	binary.LittleEndian.PutUint32(header[24:], uint32(rate))
	binary.LittleEndian.PutUint32(header[28:], uint32(rate*2))
	binary.LittleEndian.PutUint16(header[32:], 2)
	binary.LittleEndian.PutUint16(header[34:], 16)
	copy(header[36:], "data")
	binary.LittleEndian.PutUint32(header[40:], uint32(len(data)))
	return append(header, data...)
}

// chatText is a pointer to the message of a verdict.
func chatText(message string) *string { return &message }
