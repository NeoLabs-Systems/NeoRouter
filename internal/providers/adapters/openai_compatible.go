package adapters

import (
	"context"
	"encoding/json"
	"io"
	"iter"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/llm/transport"
	"github.com/NeoLabs-Systems/neorouter/internal/providers"
)

// openAICompatible is every provider that speaks the OpenAI Chat Completions
// API. The canonical request is that same wire format, so a chat call forwards
// the caller's body with the model and stream flags set and the provider's
// answer goes back unchanged.
//
// The media methods of this adapter (embed, images, speech, transcription) are
// defined by the media adapter files of this package.
type openAICompatible struct {
	baseAdapter
	media *MediaMethods
}

// newOpenAICompatible builds the OpenAI-compatible dialect.
func newOpenAICompatible(def providers.Definition, deps providers.AdapterDeps) *openAICompatible {
	adapter := &openAICompatible{baseAdapter: newBaseAdapter(def, deps), media: NewMediaMethods(def, deps)}
	adapter.self = adapter
	adapter.supported = chatMediaCapabilities(def)
	return adapter
}

// chatPath is the chat completions endpoint of the definition.
func (a *openAICompatible) chatPath() string {
	if a.def.ChatPath != "" {
		return a.def.ChatPath
	}
	return "/chat/completions"
}

// buildBody is one canonical request turned into the upstream body: the
// caller's own fields, then this provider's quirks.
func (a *openAICompatible) buildBody(canonical *transport.Obj, modelID string, stream bool) *transport.Obj {
	quirks := a.def.Quirks
	body := canonical.Clone()
	body.Set("model", modelID).Set("stream", stream)
	// streamUsage defaults to on: the usage block of the last chunk is what
	// billing and quota learning read.
	if stream && (quirks.StreamUsage == nil || *quirks.StreamUsage) {
		body.Set("stream_options", transport.NewObj().Set("include_usage", true))
	}
	if _, present := body.Get("max_tokens"); present && quirks.MaxTokensField != "" && quirks.MaxTokensField != "max_tokens" {
		value, _ := body.Get("max_tokens")
		body.Set(quirks.MaxTokensField, value)
		body.Delete("max_tokens")
	}
	if _, ok := body.Get("tools"); ok && providers.IsTrue(quirks.SingleToolCall) {
		body.Set("parallel_tool_calls", false)
	}
	for _, field := range quirks.DropFields {
		body.Delete(field)
	}
	if tools, ok := body.Get("tools"); ok && len(quirks.StripSchemaKeys) > 0 {
		body.Set("tools", chatStripSchemaKeys(tools, quirks.StripSchemaKeys))
	}
	if quirks.MinMaxTokens > 0 {
		if value, ok := chatNumber(body, "max_tokens"); ok {
			if value < float64(quirks.MinMaxTokens) {
				body.Set("max_tokens", quirks.MinMaxTokens)
			}
		}
	}
	return body
}

// Validate checks the key. A provider whose /models is public proves nothing
// with a GET, so those definitions opt into a 1-token chat probe instead.
func (a *openAICompatible) Validate(ctx context.Context) (providers.Validation, error) {
	spec := a.def.Validate
	if spec == nil || !spec.ChatProbe {
		return a.baseAdapter.Validate(ctx)
	}
	listed, err := a.ListModels(ctx)
	if err != nil {
		return validationVerdict(err, true)
	}
	var chat []providers.Model
	for _, model := range listed {
		if model.Kind == providers.KindChat {
			chat = append(chat, model)
		}
	}
	// The cheap models named by the definition, then any that is not free.
	var chosen *providers.Model
	for _, wanted := range spec.ProbeModels {
		for i := range chat {
			if chat[i].ID == wanted {
				chosen = &chat[i]
				break
			}
		}
		if chosen != nil {
			break
		}
	}
	if chosen == nil {
		for i := range chat {
			if !chat[i].FreeHint {
				chosen = &chat[i]
				break
			}
		}
	}
	if chosen == nil && len(chat) > 0 {
		chosen = &chat[0]
	}
	if chosen == nil {
		return providers.UnknownKey("No chat model available to probe the key."), nil
	}
	probe, err := chatMarshal(transport.NewObj().
		Set("messages", []any{transport.NewObj().Set("role", "user").Set("content", "ping")}).
		Set("max_tokens", 1))
	if err != nil {
		return providers.Validation{}, err
	}
	call := providers.Call{ModelID: chosen.ID, Timeout: 30 * time.Second}
	if _, err := a.Chat(ctx, probe, call); err != nil {
		// A missing model is a good key too: this one model is not open to it.
		return validationVerdict(err, true)
	}
	return providers.ValidKey(), nil
}

// ListModels reads the provider's listing, its extra listings and the models
// the definition adds itself, then drops the ones the definition excludes.
func (a *openAICompatible) ListModels(ctx context.Context) ([]providers.Model, error) {
	spec := a.def.Models
	if len(spec.Static) > 0 {
		models := make([]providers.Model, 0, len(spec.Static))
		for _, entry := range spec.Static {
			models = append(models, providers.NormalizeModel(chatPlainObject(entry), providers.ModelOverrides{}))
		}
		return models, nil
	}
	// Models the user typed in for endpoints whose /models is missing or
	// incomplete.
	var manual []providers.Model
	for _, id := range a.account.OptionStrings("manualModels") {
		manual = append(manual, providers.NormalizeModel(map[string]any{"id": id}, providers.ModelOverrides{}))
	}
	var listed []providers.Model
	path := a.optionString("modelsPath")
	if path == "" {
		path = spec.Path
	}
	if path == "" {
		path = "/models"
	}
	response, err := a.get(ctx, path, listTimeoutMS*time.Millisecond)
	if err != nil {
		if len(manual) == 0 {
			return nil, err
		}
	} else {
		for _, entry := range chatModelList(response.Data) {
			raw := chatPlainObject(entry)
			if text, ok := entry.(string); ok {
				raw = map[string]any{"id": text}
			}
			model := providers.NormalizeModel(raw, providers.ModelOverrides{})
			if model.ID != "" {
				listed = append(listed, model)
			}
		}
	}
	for _, extra := range spec.ExtraPaths {
		response, err := a.get(ctx, extra, listTimeoutMS*time.Millisecond)
		if err != nil {
			// A missing secondary listing must not hide the chat models.
			continue
		}
		for _, entry := range chatModelList(response.Data) {
			model := providers.NormalizeModel(chatPlainObject(entry), providers.ModelOverrides{})
			if model.ID != "" {
				listed = append(listed, model)
			}
		}
	}
	for _, extra := range a.def.ExtraModels {
		raw := map[string]any{"id": extra.ID}
		if extra.Name != "" {
			raw["name"] = extra.Name
		}
		listed = append(listed, providers.NormalizeModel(raw, providers.ModelOverrides{}))
	}
	var optIn []*regexp.Regexp
	for _, pattern := range spec.OptIn {
		compiled, err := regexp.Compile("(?i)" + pattern)
		if err != nil {
			continue
		}
		optIn = append(optIn, compiled)
	}
	kept := make([]providers.Model, 0, len(listed))
	for _, model := range listed {
		if spec.ExcludeFree && model.PriceKnown && model.FreeHint {
			continue
		}
		for _, pattern := range optIn {
			if pattern.MatchString(model.ID) {
				// A model that starts switched off. Model carries no field for it,
				// so the listing entry keeps the fact for the catalog to read.
				if model.Raw == nil {
					model.Raw = map[string]any{}
				}
				model.Raw["optIn"] = true
				break
			}
		}
		kept = append(kept, model)
	}
	// One entry per id, the listed models before the manual ones.
	seen := make(map[string]bool, len(kept)+len(manual))
	out := make([]providers.Model, 0, len(kept)+len(manual))
	for _, model := range append(kept, manual...) {
		if seen[model.ID] {
			continue
		}
		seen[model.ID] = true
		out = append(out, model)
	}
	return out, nil
}

// sessionHeaders is the conversation id for hosts that route by it: the
// client's own, else one derived from the conversation.
func (a *openAICompatible) sessionHeaders(canonical *transport.Obj, sessionID string) map[string]string {
	name := a.def.SessionHeader
	if name == "" {
		return nil
	}
	value := sessionID
	if value == "" {
		salt := a.account.ID
		raw, err := chatMarshal(canonical)
		if err != nil {
			raw = nil
		}
		value = providers.DerivedSession(raw, salt)
	}
	return map[string]string{name: value}
}

// Chat sends one canonical request and returns the provider's answer unchanged.
func (a *openAICompatible) Chat(ctx context.Context, req json.RawMessage, call providers.Call) (*providers.ChatResult, error) {
	canonical, err := chatParseBody(req)
	if err != nil {
		return nil, err
	}
	headers := a.headers(a.sessionHeaders(canonical, call.SessionID))
	raw, responseHeaders, err := a.requestBody(ctx, a.chatPath(), providers.Request{
		Method:  http.MethodPost,
		Headers: httpHeaders(headers),
		JSON:    a.buildBody(canonical, call.ModelID, false),
	}, a.timeoutFor(call))
	if err != nil {
		return nil, err
	}
	decoded, decodeErr := chatDecodeJSON(raw)
	if decodeErr != nil || chatObj(decoded) == nil {
		return nil, providers.NewError(providers.KindInvalidOutput, "The provider returned a non-JSON response.", providers.ErrorOptions{Headers: responseHeaders})
	}
	if err := providers.AssertNoEmbeddedError(decoded); err != nil {
		return nil, err
	}
	return &providers.ChatResult{Body: raw, Headers: responseHeaders}, nil
}

// Stream sends one canonical request and yields the provider's own stream
// chunks as they arrive.
func (a *openAICompatible) Stream(ctx context.Context, req json.RawMessage, call providers.Call) iter.Seq2[json.RawMessage, error] {
	canonical, err := chatParseBody(req)
	body := (*transport.Obj)(nil)
	if err == nil {
		body = canonical
	}
	return func(yield func(json.RawMessage, error) bool) {
		if err != nil {
			yield(nil, err)
			return
		}
		headers := a.headers(chatMerge(map[string]string{"Accept": "text/event-stream"}, a.sessionHeaders(canonical, call.SessionID)))
		response, callErr := a.requestStream(ctx, a.chatPath(), providers.Request{
			Method:  http.MethodPost,
			Headers: httpHeaders(headers),
			JSON:    a.buildBody(body, call.ModelID, true),
		})
		if callErr != nil {
			yield(nil, callErr)
			return
		}
		defer response.Body.Close()
		if call.OnHeaders != nil {
			call.OnHeaders(response.Header)
		}
		if strings.Contains(response.Header.Get("Content-Type"), "application/json") {
			// Some upstreams ignore stream=true and answer with one JSON body.
			raw, readErr := io.ReadAll(response.Body)
			if readErr != nil {
				yield(nil, providers.FromNetwork(readErr))
				return
			}
			decoded, decodeErr := chatDecodeJSON(raw)
			if decodeErr != nil {
				yield(nil, providers.NewError(providers.KindInvalidOutput, "The provider returned a non-JSON response.", providers.ErrorOptions{}))
				return
			}
			if embedded := providers.AssertNoEmbeddedError(decoded); embedded != nil {
				yield(nil, embedded)
				return
			}
			for _, chunk := range chatCompletionAsChunks(chatObj(decoded)) {
				if !yield(chunk, nil) {
					return
				}
			}
			return
		}
		for event, streamErr := range providers.SSE(response.Body) {
			if streamErr != nil {
				yield(nil, streamErr)
				return
			}
			if event.Data == "[DONE]" {
				return
			}
			decoded, decodeErr := chatDecodeJSON([]byte(event.Data))
			if decodeErr != nil {
				continue
			}
			if embedded := providers.AssertNoEmbeddedError(decoded); embedded != nil {
				yield(nil, embedded)
				return
			}
			if !yield(json.RawMessage(event.Data), nil) {
				return
			}
		}
	}
}

// FetchQuota reads the definition's quota endpoint.
func (a *openAICompatible) FetchQuota(ctx context.Context) ([]providers.QuotaWindow, error) {
	probe := a.def.Quota.Probe
	if probe == nil {
		return nil, nil
	}
	response, err := a.get(ctx, probe.Path, validationTimeoutMS*time.Millisecond)
	if err != nil {
		return nil, err
	}
	if probe.Parse == nil {
		return nil, nil
	}
	return probe.Parse(response.Data), nil
}

// requestBody performs one call and returns the answer's bytes unchanged, which
// is how the provider's own JSON reaches the client.
func (a *baseAdapter) requestBody(ctx context.Context, pathname string, req providers.Request, timeout time.Duration) ([]byte, http.Header, error) {
	response, err := a.requestRaw(ctx, pathname, req, timeout)
	if err != nil {
		return nil, nil, err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, nil, providers.FromNetwork(err)
	}
	return raw, response.Header, nil
}

// chatCompletionAsChunks folds a whole completion into the two stream chunks a
// client of a streamed answer expects.
func chatCompletionAsChunks(completion *transport.Obj) []json.RawMessage {
	var choice *transport.Obj
	if choices := chatList(chatGet(completion, "choices")); len(choices) > 0 {
		choice = chatObj(choices[0])
	}
	message := chatObj(chatGet(choice, "message"))
	base := func() *transport.Obj {
		return transport.NewObj().
			Set("id", chatGet(completion, "id")).
			Set("object", "chat.completion.chunk").
			Set("created", chatGet(completion, "created")).
			Set("model", chatGet(completion, "model"))
	}
	delta := transport.NewObj().Set("role", "assistant")
	if content := chatGet(message, "content"); chatTruthy(content) {
		delta.Set("content", content)
	}
	if reasoning := chatGet(message, "reasoning_content"); chatTruthy(reasoning) {
		delta.Set("reasoning_content", reasoning)
	}
	if calls := chatList(chatGet(message, "tool_calls")); len(calls) > 0 {
		indexed := make([]any, 0, len(calls))
		for index, call := range calls {
			body := chatObj(call)
			if body == nil {
				continue
			}
			indexed = append(indexed, body.Clone().Set("index", index))
		}
		delta.Set("tool_calls", indexed)
	}
	first := base().Set("choices", []any{transport.NewObj().Set("index", 0).Set("delta", delta).Set("finish_reason", nil)})
	last := base().Set("choices", []any{transport.NewObj().
		Set("index", 0).
		Set("delta", transport.NewObj()).
		Set("finish_reason", chatFinishOr(chatGet(choice, "finish_reason"), "stop"))})
	if usage := chatGet(completion, "usage"); chatTruthy(usage) {
		last.Set("usage", usage)
	}
	out := make([]json.RawMessage, 0, 2)
	for _, chunk := range []*transport.Obj{first, last} {
		encoded, err := chatMarshal(chunk)
		if err != nil {
			continue
		}
		out = append(out, encoded)
	}
	return out
}

// chatStripSchemaKeys removes the JSON-schema keys a provider rejects, from
// every level of a tool definition.
func chatStripSchemaKeys(value any, keys []string) any {
	switch v := value.(type) {
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = chatStripSchemaKeys(item, keys)
		}
		return out
	case *transport.Obj:
		out := transport.NewObj()
		for _, name := range v.Keys() {
			if chatInList(keys, name) {
				continue
			}
			inner, _ := v.Get(name)
			out.Set(name, chatStripSchemaKeys(inner, keys))
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(v))
		for name, inner := range v {
			if chatInList(keys, name) {
				continue
			}
			out[name] = chatStripSchemaKeys(inner, keys)
		}
		return out
	}
	return value
}

// chatModelList reads the model array out of a listing shape.
func chatModelList(data any) []any {
	if items, ok := data.([]any); ok {
		return items
	}
	for _, field := range []string{"data", "models", "result"} {
		if items := chatList(chatMember(data, field)); items != nil {
			return items
		}
	}
	return nil
}

// chatMember reads one member of a decoded JSON value.
func chatMember(value any, name string) any {
	if obj, ok := value.(map[string]any); ok {
		return obj[name]
	}
	return nil
}

// chatPlainObject turns a listing entry into the plain map NormalizeModel reads.
func chatPlainObject(value any) map[string]any {
	switch v := value.(type) {
	case map[string]any:
		return v
	case *transport.Obj:
		out := make(map[string]any, len(v.Keys()))
		for _, name := range v.Keys() {
			inner, _ := v.Get(name)
			out[name] = chatPlainValue(inner)
		}
		return out
	}
	return nil
}

// chatPlainValue converts ordered values to the plain shapes the parent package
// reads.
func chatPlainValue(value any) any {
	switch v := value.(type) {
	case *transport.Obj:
		return chatPlainObject(v)
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = chatPlainValue(item)
		}
		return out
	}
	return value
}

// chatParseBody decodes a canonical request, keeping the caller's key order.
func chatParseBody(raw json.RawMessage) (*transport.Obj, error) {
	decoded, err := transport.ParseOrdered(raw)
	if err != nil {
		return nil, providers.NewError(providers.KindBadRequest, "The request body is not a JSON object.", providers.ErrorOptions{})
	}
	body := chatObj(decoded)
	if body == nil {
		return nil, providers.NewError(providers.KindBadRequest, "The request body is not a JSON object.", providers.ErrorOptions{})
	}
	return body, nil
}

// chatDecodeJSON decodes one provider answer for the checks the adapters make of
// it; the bytes themselves are what the client receives.
func chatDecodeJSON(raw []byte) (any, error) {
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, err
	}
	return decoded, nil
}

// chatMerge merges header maps, the later one winning.
func chatMerge(maps ...map[string]string) map[string]string {
	out := map[string]string{}
	for _, values := range maps {
		for name, value := range values {
			out[name] = value
		}
	}
	return out
}

// contains reports whether list holds value.
func chatInList(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}

// chatMediaCapabilities is the capability set of an OpenAI-compatible adapter:
// chat, plus every media endpoint the definition opts into or the OpenAI
// default an editable endpoint falls back to.
func chatMediaCapabilities(def providers.Definition) providers.Capabilities {
	set := providers.Of(providers.CapChat)
	for _, method := range []providers.Capability{
		providers.CapEmbed, providers.CapGenerateImage, providers.CapSpeech, providers.CapTranscribe,
		providers.CapModerate, providers.CapClassify, providers.CapCreateVideo, providers.CapDecide,
		providers.CapSearch, providers.CapWeather,
	} {
		if _, ok := MediaPath(def, method, ""); ok {
			set[method] = true
		}
	}
	return set
}
