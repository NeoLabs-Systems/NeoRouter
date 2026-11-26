package adapters

import (
	"context"
	"encoding/json"
	"iter"
	"time"

	"github.com/NeoLabs-Systems/neorouter/internal/providers"
)

// anthropic is providers/adapters/anthropic.js: Anthropic's Messages API, and
// every service that answers like it (custom_anthropic).
type anthropic struct {
	baseAdapter
}

// newAnthropic builds Anthropic's dialect.
func newAnthropic(def providers.Definition, deps providers.AdapterDeps) *anthropic {
	adapter := &anthropic{baseAdapter: newBaseAdapter(def, deps)}
	adapter.self = adapter
	adapter.auth = adapter.authHeaders
	adapter.supported = providers.Of(providers.CapChat)
	return adapter
}

// authHeaders is Anthropic's key header plus the API version it requires.
func (a *anthropic) authHeaders() map[string]string {
	if a.secret == "" {
		return nil
	}
	return map[string]string{"x-api-key": a.secret, "anthropic-version": anthropicAPIVersion}
}

// ListModels pages through Anthropic's listing.
func (a *anthropic) ListModels(ctx context.Context) ([]providers.Model, error) {
	var models []providers.Model
	after := ""
	for page := 0; page < 20; page++ {
		path := "/models?limit=1000"
		if after != "" {
			path += "&after_id=" + chatEncodeURIComponent(after)
		}
		response, err := a.get(ctx, path, listTimeoutMS*time.Millisecond)
		if err != nil {
			return nil, err
		}
		data, _ := response.Data.(map[string]any)
		for _, entry := range chatList(data["data"]) {
			raw := chatPlainObject(entry)
			if raw == nil {
				continue
			}
			if value, present := raw["max_input_tokens"]; present {
				raw["context_window"] = value
			}
			if value, present := raw["max_tokens"]; present {
				raw["max_output_tokens"] = value
			}
			models = append(models, providers.NormalizeModel(raw, providers.ModelOverrides{
				Tools:  chatTrue(),
				Vision: chatTrue(),
			}))
		}
		hasMore, _ := data["has_more"].(bool)
		lastID, _ := data["last_id"].(string)
		if !hasMore || lastID == "" {
			break
		}
		after = lastID
	}
	return models, nil
}

// Chat sends one canonical request as an Anthropic message and reads the answer
// back into a canonical completion.
func (a *anthropic) Chat(ctx context.Context, req json.RawMessage, call providers.Call) (*providers.ChatResult, error) {
	canonical, err := chatParseBody(req)
	if err != nil {
		return nil, err
	}
	body := anthropicRequest(canonical, anthropicDefaultMaxTokens())
	body.Set("model", call.ModelID).Set("stream", false)
	response, err := a.post(ctx, "/messages", body, a.timeoutFor(call))
	if err != nil {
		return nil, err
	}
	message := chatObj(chatPlainValue(response.Data))
	if chatField(message, "type") == "error" {
		return nil, providers.ClassifyHTTP(502, response.Data, response.Headers)
	}
	completion, err := chatMarshal(anthropicResponse(message, call.ModelID))
	if err != nil {
		return nil, providers.NewError(providers.KindInvalidOutput, "The answer could not be encoded.", providers.ErrorOptions{})
	}
	return &providers.ChatResult{Body: completion, Headers: response.Headers}, nil
}

// Stream sends one canonical request as an Anthropic stream and yields canonical
// chunks.
func (a *anthropic) Stream(ctx context.Context, req json.RawMessage, call providers.Call) iter.Seq2[json.RawMessage, error] {
	return func(yield func(json.RawMessage, error) bool) {
		canonical, err := chatParseBody(req)
		if err != nil {
			yield(nil, err)
			return
		}
		body := anthropicRequest(canonical, anthropicDefaultMaxTokens())
		body.Set("model", call.ModelID).Set("stream", true)
		headers := a.headers(map[string]string{"Accept": "text/event-stream"})
		response, callErr := a.requestRaw(ctx, "/messages", providers.Request{
			Method:  "POST",
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
		for chunk, streamErr := range anthropicStream(response.Body, call.ModelID) {
			if !yield(chunk, streamErr) {
				return
			}
		}
	}
}

// anthropicDefaultMaxTokens is the output limit Anthropic is asked for when the
// caller names none: the model's own limit clamped to what the JS adapter sends.
func anthropicDefaultMaxTokens() float64 { return 8192 }
