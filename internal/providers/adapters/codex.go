package adapters

import (
	"context"
	"encoding/json"
	"iter"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/llm/transport"
	"github.com/NeoLabs-Systems/neorouter/internal/providers"
)

// codexAdapter is providers/adapters/codex.js: ChatGPT through the Codex login,
// so the user's ChatGPT plan answers instead of API credit. The account secret
// holds the OAuth tokens; the sign-in is refreshed before it is used, which the
// CodexSignIn seam does (providers.OAuth in practice).
type codexAdapter struct {
	baseAdapter
	signIn  CodexSignIn
	persist func(accountID, secret string) error
	prompt  *codexInstructions
}

// codexInstructions is the backend's required instructions, fetched once and
// remembered: the JS adapter keeps the same cache for the process.
type codexInstructions struct {
	mu        sync.Mutex
	text      string
	required  bool
	fetchFail error
}

// newCodex builds the ChatGPT/Codex dialect.
func newCodex(def providers.Definition, deps providers.AdapterDeps, signIn CodexSignIn, persist func(accountID, secret string) error, prompt *codexInstructions) *codexAdapter {
	adapter := &codexAdapter{baseAdapter: newBaseAdapter(def, deps), signIn: signIn, persist: persist, prompt: prompt}
	adapter.self = adapter
	adapter.supported = providers.Of(providers.CapChat)
	return adapter
}

// msgCodexNoSignIn is the failure of a connection whose tokens cannot be used.
const msgCodexNoSignIn = "This ChatGPT connection has no valid sign-in. Connect it again."

// credentials is the sign-in this call uses, refreshed when it has expired. A
// rotated secret is stored through the factory's persist hook, so the next call
// keeps working: refresh tokens rotate.
func (a *codexAdapter) credentials(ctx context.Context) (providers.CodexCredentials, error) {
	if a.signIn == nil {
		return providers.CodexCredentials{}, providers.NewError(providers.KindAuth, msgCodexNoSignIn, providers.ErrorOptions{})
	}
	accountID := a.account.ID
	var persist func(string) error
	if a.persist != nil && accountID != "" {
		persist = func(secret string) error { return a.persist(accountID, secret) }
	}
	credentials, _, err := a.signIn.FreshCodexCredentials(ctx, a.account.Secret, accountID, persist)
	if err != nil {
		if classified := providers.AsProviderError(err); classified != nil {
			return providers.CodexCredentials{}, classified
		}
		return providers.CodexCredentials{}, providers.FromNetwork(err)
	}
	if credentials.Access == "" {
		return providers.CodexCredentials{}, providers.NewError(providers.KindAuth, msgCodexNoSignIn, providers.ErrorOptions{})
	}
	return credentials, nil
}

// codexHeaders are the headers the ChatGPT backend expects, including the
// client's originator and a per-call session id.
func (a *codexAdapter) codexHeaders(ctx context.Context, extra map[string]string) (map[string]string, error) {
	credentials, err := a.credentials(ctx)
	if err != nil {
		return nil, err
	}
	headers := map[string]string{
		"Authorization":      "Bearer " + credentials.Access,
		"chatgpt-account-id": credentials.AccountID,
		"OpenAI-Beta":        "responses=experimental",
		// The Codex backend expects the Codex client's originator.
		"originator": "codex_cli_rs",
		"session_id": codexRandomUUID(),
	}
	for name, value := range extra {
		headers[name] = value
	}
	return headers, nil
}

// Validate checks the sign-in and the model listing.
func (a *codexAdapter) Validate(ctx context.Context) (providers.Validation, error) {
	if _, err := a.credentials(ctx); err != nil {
		return validationVerdict(err, false)
	}
	if _, err := a.ListModels(ctx); err != nil {
		return validationVerdict(err, false)
	}
	return providers.ValidKey(), nil
}

// ListModels reads the backend's own listing and leaves out the models it marks
// as hidden.
func (a *codexAdapter) ListModels(ctx context.Context) ([]providers.Model, error) {
	headers, err := a.codexHeaders(ctx, nil)
	if err != nil {
		return nil, err
	}
	path := a.baseURL + "/models"
	if a.signIn != nil {
		path += "?client_version=" + chatEncodeURIComponent(a.signIn.CodexClientVersion(ctx))
	}
	response, err := a.request(ctx, path, providers.Request{Method: http.MethodGet, Headers: httpHeaders(headers)}, listTimeoutMS*time.Millisecond)
	if err != nil {
		return nil, err
	}
	data, _ := response.Data.(map[string]any)
	var models []providers.Model
	for _, entry := range chatList(data["models"]) {
		raw := chatPlainObject(entry)
		slug, _ := raw["slug"].(string)
		if slug == "" {
			continue
		}
		if visibility, present := raw["visibility"]; present && visibility != "list" {
			continue
		}
		name, _ := raw["display_name"].(string)
		if name == "" {
			name = slug
		}
		normalized := map[string]any{"id": slug, "name": name}
		if window := raw["context_window"]; window != nil {
			normalized["context_length"] = window
		}
		models = append(models, providers.NormalizeModel(normalized, providers.ModelOverrides{
			Tools:     chatTrue(),
			Vision:    chatTrue(),
			Reasoning: chatTrue(),
		}))
	}
	return models, nil
}

// open starts one Responses call: the neutral instructions first, the Codex
// ones when the backend insists on them.
func (a *codexAdapter) open(ctx context.Context, canonical *transport.Obj, modelID string, timeout time.Duration) (*http.Response, error) {
	send := func(instructions string) (*http.Response, error) {
		headers, err := a.codexHeaders(ctx, map[string]string{"Accept": "text/event-stream"})
		if err != nil {
			return nil, err
		}
		return a.requestRaw(ctx, a.baseURL+"/responses", providers.Request{
			Method:  http.MethodPost,
			Headers: httpHeaders(headers),
			JSON:    codexBody(canonical, modelID, instructions),
		}, timeout)
	}
	if a.prompt != nil {
		if required, cached := a.prompt.current(); cached {
			if !required {
				return send(codexNeutralInstructions)
			}
			instructions, err := a.officialInstructions(ctx)
			if err != nil {
				return nil, err
			}
			return send(instructions)
		}
	}
	response, err := send(codexNeutralInstructions)
	if err == nil {
		return response, nil
	}
	if providers.KindOf(err) != providers.KindBadRequest || !strings.Contains(strings.ToLower(err.Error()), "instruction") {
		return nil, err
	}
	if a.prompt != nil {
		a.prompt.require()
	}
	instructions, promptErr := a.officialInstructions(ctx)
	if promptErr != nil {
		return nil, promptErr
	}
	return send(instructions)
}

// officialInstructions fetches the Codex client's instructions the backend
// requires.
func (a *codexAdapter) officialInstructions(ctx context.Context) (string, error) {
	if a.prompt != nil {
		a.prompt.mu.Lock()
		cached := a.prompt.text
		a.prompt.mu.Unlock()
		if cached != "" {
			return cached, nil
		}
	}
	response, err := a.client.Do(ctx, codexPromptURL, providers.Request{Method: http.MethodGet, Timeout: 15 * time.Second})
	if err != nil {
		return "", providers.NewError(providers.KindServer, "Could not load the Codex instructions the ChatGPT backend requires.", providers.ErrorOptions{})
	}
	defer response.Body.Close()
	body, err := readAllString(response)
	if err != nil {
		return "", providers.NewError(providers.KindServer, "Could not load the Codex instructions the ChatGPT backend requires.", providers.ErrorOptions{})
	}
	if a.prompt != nil {
		a.prompt.mu.Lock()
		a.prompt.text = body
		a.prompt.mu.Unlock()
	}
	return body, nil
}

// Stream sends one canonical request and yields canonical chunks.
func (a *codexAdapter) Stream(ctx context.Context, req json.RawMessage, call providers.Call) iter.Seq2[json.RawMessage, error] {
	return func(yield func(json.RawMessage, error) bool) {
		canonical, err := chatParseBody(req)
		if err != nil {
			yield(nil, err)
			return
		}
		response, callErr := a.open(ctx, canonical, call.ModelID, a.timeoutFor(call))
		if callErr != nil {
			yield(nil, callErr)
			return
		}
		defer response.Body.Close()
		if call.OnHeaders != nil {
			call.OnHeaders(response.Header)
		}
		for chunk, streamErr := range codexChunks(response.Body, call.ModelID) {
			if !yield(chunk, streamErr) {
				return
			}
		}
	}
}

// Chat is a plain request over the streamed backend: the backend only streams,
// so the answer is folded back into one completion.
func (a *codexAdapter) Chat(ctx context.Context, req json.RawMessage, call providers.Call) (*providers.ChatResult, error) {
	canonical, err := chatParseBody(req)
	if err != nil {
		return nil, err
	}
	response, err := a.open(ctx, canonical, call.ModelID, a.timeoutFor(call))
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	accumulator := &chatStreamAccumulator{}
	for chunk, streamErr := range codexChunks(response.Body, call.ModelID) {
		if streamErr != nil {
			return nil, streamErr
		}
		decoded, decodeErr := chatDecodeJSON(chunk)
		if decodeErr != nil {
			continue
		}
		accumulator.push(chatObj(decoded))
	}
	if !accumulator.hasAnswer() {
		return nil, providers.NewError(providers.KindEmpty, "ChatGPT returned an empty response.", providers.ErrorOptions{})
	}
	completion := chatBuildResponse("", call.ModelID, accumulator.message(), accumulator.finishReason(), accumulator.usage)
	body, err := chatMarshal(completion)
	if err != nil {
		return nil, providers.NewError(providers.KindInvalidOutput, "The answer could not be encoded.", providers.ErrorOptions{})
	}
	return &providers.ChatResult{Body: body, Headers: response.Header}, nil
}

// current reports whether the required instructions are known and whether they
// are required at all.
func (c *codexInstructions) current() (required, known bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.required, c.required || c.text != ""
}

// require records that the backend insists on the Codex instructions.
func (c *codexInstructions) require() {
	c.mu.Lock()
	c.required = true
	c.mu.Unlock()
}

// readAllString reads a response body as text.
func readAllString(response *http.Response) (string, error) {
	builder := &strings.Builder{}
	buf := make([]byte, 32<<10)
	for {
		read, err := response.Body.Read(buf)
		if read > 0 {
			builder.Write(buf[:read])
		}
		if err != nil {
			if err.Error() == "EOF" {
				return builder.String(), nil
			}
			return builder.String(), err
		}
	}
}
