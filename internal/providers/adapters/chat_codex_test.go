package adapters

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/NeoLabs-Systems/neorouter/internal/providers"
)

// ChatGPT through the Codex login, ported from providers/adapters/codex.js and
// test/api/chatgpt.test.js: the Responses request goes out in the backend's
// shape and the stream comes back as a canonical answer.

// fakeCodexSignIn stands in for services/providers/codex_oauth.go.
type fakeCodexSignIn struct {
	access    string
	accountID string
	refreshes int
	persisted []string
	err       error
}

func (f *fakeCodexSignIn) FreshCodexCredentials(ctx context.Context, secret, accountID string, persist func(secret string) error) (providers.CodexCredentials, string, error) {
	if f.err != nil {
		return providers.CodexCredentials{}, "", f.err
	}
	f.refreshes++
	rotated := secret
	if persist != nil {
		rotated = secret + "-rotated"
		if err := persist(rotated); err != nil {
			return providers.CodexCredentials{}, "", err
		}
	}
	return providers.CodexCredentials{Access: f.access, AccountID: f.accountID}, rotated, nil
}

func (f *fakeCodexSignIn) CodexClientVersion(ctx context.Context) string { return "1.2.3" }

func codexAdapterFor(t *testing.T, baseURL string, signIn CodexSignIn) *codexAdapter {
	t.Helper()
	return newCodex(chatDefinition(t, "chatgpt"), chatDeps{
		baseURL: baseURL + "/backend-api/codex",
		secret:  `{"tokens":true}`,
	}.deps(), signIn, nil, &codexInstructions{})
}

// codexStream answers the Responses event stream a chat request produces.
func codexStream(toolCall bool) func(w http.ResponseWriter, r *http.Request, call chatCall) {
	return func(w http.ResponseWriter, r *http.Request, call chatCall) {
		var events []string
		if toolCall {
			events = []string{
				`{"type":"response.output_item.added","item":{"id":"fc_1","type":"function_call","call_id":"call_1","name":"weather"}}`,
				`{"type":"response.function_call_arguments.delta","item_id":"fc_1","delta":"{\"city\":"}`,
				`{"type":"response.function_call_arguments.delta","item_id":"fc_1","delta":"\"Berlin\"}"}`,
			}
		} else {
			events = []string{
				`{"type":"response.output_text.delta","delta":"Hello "}`,
				`{"type":"response.output_text.delta","delta":"from ChatGPT"}`,
			}
		}
		events = append(events, `{"type":"response.completed","response":{"usage":{"input_tokens":11,"output_tokens":4}}}`)
		chatSSE(w, events, false)
	}
}

func TestCodexChatIsAFoldedStream(t *testing.T) {
	upstream := newChatProvider(t, func(w http.ResponseWriter, r *http.Request, call chatCall) {
		if r.URL.Path == "/backend-api/codex/responses" {
			codexStream(false)(w, r, call)
			return
		}
		chatJSON(w, http.StatusNotFound, `{"error":{"message":"no such path"}}`)
	})
	signIn := &fakeCodexSignIn{access: "access-1", accountID: "acct-123"}
	adapter := codexAdapterFor(t, upstream.url(), signIn)

	request := chatRequest(t, `{"messages":[{"role":"system","content":"Be brief."},{"role":"user","content":"Hi"}]}`)
	result, err := adapter.Chat(context.Background(), request, providers.Call{ModelID: "gpt-test"})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	sent := upstream.last(t)
	if sent.headers.Get("chatgpt-account-id") != "acct-123" {
		t.Errorf("chatgpt-account-id = %q", sent.headers.Get("chatgpt-account-id"))
	}
	if sent.headers.Get("Authorization") != "Bearer access-1" {
		t.Errorf("authorization = %q", sent.headers.Get("Authorization"))
	}
	if sent.headers.Get("OpenAI-Beta") != "responses=experimental" || sent.headers.Get("originator") != "codex_cli_rs" {
		t.Errorf("headers = %v", sent.headers)
	}
	if sent.headers.Get("session_id") == "" {
		t.Error("every call needs its own session id")
	}
	if sent.body["store"] != false || sent.body["stream"] != true {
		t.Errorf("the backend is always streamed and stateless: %v", sent.body)
	}
	include, _ := sent.body["include"].([]any)
	if len(include) != 1 || include[0] != "reasoning.encrypted_content" {
		t.Errorf("include = %v", sent.body["include"])
	}
	if sent.body["instructions"] != codexNeutralInstructions {
		t.Errorf("instructions = %v", sent.body["instructions"])
	}
	input := sent.body["input"].([]any)
	if len(input) != 2 {
		t.Fatalf("input = %v", input)
	}
	if input[0].(map[string]any)["role"] != "developer" || input[1].(map[string]any)["role"] != "user" {
		t.Errorf("roles = %v, %v", input[0], input[1])
	}

	completion := decodedChatResult(t, result.Body)
	message := chatPath(t, completion, "choices").([]any)[0].(map[string]any)["message"].(map[string]any)
	if message["content"] != "Hello from ChatGPT" {
		t.Errorf("content = %v", message["content"])
	}
	if chatNumberAt(t, completion["usage"].(map[string]any)["prompt_tokens"]) != 11 {
		t.Errorf("usage = %v", completion["usage"])
	}
	choice := chatPath(t, completion, "choices").([]any)[0].(map[string]any)
	if choice["finish_reason"] != "stop" {
		t.Errorf("finish_reason = %v", choice["finish_reason"])
	}
}

func TestCodexToolCallsTranslateBothWays(t *testing.T) {
	upstream := newChatProvider(t, func(w http.ResponseWriter, r *http.Request, call chatCall) {
		codexStream(true)(w, r, call)
	})
	adapter := codexAdapterFor(t, upstream.url(), &fakeCodexSignIn{access: "a", accountID: "acct-1"})
	request := chatRequest(t, `{"messages":[{"role":"user","content":"Weather?"}],"tools":[{"type":"function","function":{"name":"weather","parameters":{"type":"object","properties":{"city":{"type":"string"}}}}}],"tool_choice":"auto","parallel_tool_calls":false}`)
	result, err := adapter.Chat(context.Background(), request, providers.Call{ModelID: "gpt-test"})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	sent := upstream.last(t).body
	tools := sent["tools"].([]any)
	tool := tools[0].(map[string]any)
	if tool["type"] != "function" || tool["name"] != "weather" || tool["strict"] != false {
		t.Errorf("tool = %v", tool)
	}
	if sent["tool_choice"] != "auto" || sent["parallel_tool_calls"] != false {
		t.Errorf("tool settings = %v / %v", sent["tool_choice"], sent["parallel_tool_calls"])
	}

	completion := decodedChatResult(t, result.Body)
	choice := chatPath(t, completion, "choices").([]any)[0].(map[string]any)
	if choice["finish_reason"] != "tool_calls" {
		t.Errorf("finish_reason = %v", choice["finish_reason"])
	}
	call := choice["message"].(map[string]any)["tool_calls"].([]any)[0].(map[string]any)
	if call["id"] != "call_1" || call["function"].(map[string]any)["name"] != "weather" {
		t.Errorf("tool call = %v", call)
	}
	if call["function"].(map[string]any)["arguments"] != `{"city":"Berlin"}` {
		t.Errorf("arguments = %v", call["function"])
	}
}

func TestCodexStreamYieldsCanonicalChunks(t *testing.T) {
	upstream := newChatProvider(t, func(w http.ResponseWriter, r *http.Request, call chatCall) {
		codexStream(false)(w, r, call)
	})
	adapter := codexAdapterFor(t, upstream.url(), &fakeCodexSignIn{access: "a", accountID: "acct-1"})
	var headers http.Header
	chunks := chatChunks(t, adapter.Stream(context.Background(), chatRequest(t, `{"messages":[{"role":"user","content":"hi"}]}`), providers.Call{
		ModelID:   "gpt-test",
		OnHeaders: func(received http.Header) { headers = received },
	}))
	if len(chunks) != 5 {
		t.Fatalf("chunks = %d: %v", len(chunks), chunks)
	}
	// The first chunk opens the answer, as a client of OpenAI's stream expects.
	first := chatPath(t, chunks[0], "choices").([]any)[0].(map[string]any)["delta"].(map[string]any)
	if first["role"] != "assistant" {
		t.Errorf("first delta = %v", first)
	}
	second := chatPath(t, chunks[1], "choices").([]any)[0].(map[string]any)["delta"].(map[string]any)
	if second["content"] != "Hello " {
		t.Errorf("content = %v", second)
	}
	finish := chatPath(t, chunks[3], "choices").([]any)[0].(map[string]any)
	if finish["finish_reason"] != "stop" {
		t.Errorf("finish_reason = %v", finish["finish_reason"])
	}
	usage := chunks[4]["usage"].(map[string]any)
	if chatNumberAt(t, usage["prompt_tokens"]) != 11 || chatNumberAt(t, usage["completion_tokens"]) != 4 {
		t.Errorf("usage = %v", usage)
	}
	if headers == nil {
		t.Error("the stream headers must reach the caller")
	}
}

func TestCodexStreamErrorEvent(t *testing.T) {
	upstream := newChatProvider(t, func(w http.ResponseWriter, r *http.Request, call chatCall) {
		chatSSE(w, []string{`{"type":"response.failed","response":{"error":{"code":"rate_limit_exceeded","message":"Rate limit reached"}}}`}, false)
	})
	adapter := codexAdapterFor(t, upstream.url(), &fakeCodexSignIn{access: "a", accountID: "acct-1"})
	failure := chatStreamFailure(t, adapter.Stream(context.Background(), chatRequest(t, `{"messages":[]}`), providers.Call{ModelID: "gpt-test"}))
	if providers.KindOf(failure) != providers.KindRateLimit {
		t.Fatalf("kind = %q: %v", providers.KindOf(failure), failure)
	}
	if failure.Error() != "Rate limit reached" {
		t.Errorf("message = %q", failure.Error())
	}
}

func TestCodexListsModelsItMayUse(t *testing.T) {
	upstream := newChatProvider(t, func(w http.ResponseWriter, r *http.Request, call chatCall) {
		if r.URL.Path != "/backend-api/codex/models" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if call.query != "client_version=1.2.3" {
			t.Errorf("query = %q", call.query)
		}
		chatJSON(w, http.StatusOK, `{"models":[
			{"slug":"gpt-test","display_name":"GPT Test","visibility":"list","context_window":272000},
			{"slug":"gpt-hidden","visibility":"hide"},
			{"display_name":"no slug"}
		]}`)
	})
	adapter := codexAdapterFor(t, upstream.url(), &fakeCodexSignIn{access: "a", accountID: "acct-1"})
	models, err := adapter.ListModels(context.Background())
	if err != nil {
		t.Fatalf("list models: %v", err)
	}
	if len(models) != 1 || models[0].ID != "gpt-test" {
		t.Fatalf("models = %v", models)
	}
	if models[0].Name != "GPT Test" {
		t.Errorf("name = %q", models[0].Name)
	}
	if models[0].ContextWindow == nil || *models[0].ContextWindow != 272000 {
		t.Errorf("context window = %v", models[0].ContextWindow)
	}
	if models[0].Tools == nil || !*models[0].Tools || models[0].Reasoning == nil || !*models[0].Reasoning {
		t.Errorf("ChatGPT models take tools and reason: %+v", models[0])
	}
}

func TestCodexValidate(t *testing.T) {
	healthy := newChatProvider(t, func(w http.ResponseWriter, r *http.Request, call chatCall) {
		chatJSON(w, http.StatusOK, `{"models":[{"slug":"gpt-test"}]}`)
	})
	adapter := codexAdapterFor(t, healthy.url(), &fakeCodexSignIn{access: "a", accountID: "acct-1"})
	verdict, err := adapter.Validate(context.Background())
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if verdict.Status != providers.ValidationHealthy {
		t.Fatalf("verdict = %+v", verdict)
	}

	refused := newChatProvider(t, func(w http.ResponseWriter, r *http.Request, call chatCall) {
		chatJSON(w, http.StatusUnauthorized, `{"error":{"message":"Invalid access token"}}`)
	})
	adapter = codexAdapterFor(t, refused.url(), &fakeCodexSignIn{access: "a", accountID: "acct-1"})
	verdict, err = adapter.Validate(context.Background())
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if verdict.Status != providers.ValidationInvalid {
		t.Fatalf("verdict = %+v", verdict)
	}
	if verdict.Message == nil || *verdict.Message != "Invalid access token" {
		t.Errorf("message = %v", verdict.Message)
	}
}

func TestCodexAsksForTheCodexInstructionsWhenTheBackendInsists(t *testing.T) {
	// The backend refuses the neutral instructions once, and the adapter then
	// fetches Codex's own prompt and retries.
	prompt := newChatProvider(t, func(w http.ResponseWriter, r *http.Request, call chatCall) {
		_, _ = w.Write([]byte("You are Codex."))
	})
	previous := codexPromptURL
	codexPromptURL = prompt.url()
	t.Cleanup(func() { codexPromptURL = previous })

	var attempts int
	upstream := newChatProvider(t, func(w http.ResponseWriter, r *http.Request, call chatCall) {
		attempts++
		if attempts == 1 {
			chatJSON(w, http.StatusBadRequest, `{"error":{"message":"Instructions are required"}}`)
			return
		}
		codexStream(false)(w, r, call)
	})
	adapter := codexAdapterFor(t, upstream.url(), &fakeCodexSignIn{access: "a", accountID: "acct-1"})
	request := chatRequest(t, `{"messages":[{"role":"user","content":"hi"}]}`)
	if _, err := adapter.Chat(context.Background(), request, providers.Call{ModelID: "gpt-test", Timeout: 5 * time.Second}); err != nil {
		t.Fatalf("chat: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want the request retried with the Codex instructions", attempts)
	}
	sent := upstream.last(t).body
	if sent["instructions"] != "You are Codex." {
		t.Errorf("instructions = %v", sent["instructions"])
	}
	// With Codex's own instructions a bridge message tells it this is an API call.
	input := sent["input"].([]any)
	bridge := input[0].(map[string]any)
	if bridge["role"] != "developer" {
		t.Fatalf("the bridge message = %v", bridge)
	}
	text := bridge["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "not working in a terminal") {
		t.Errorf("bridge = %q", text)
	}

	// The next call remembers that the backend requires them.
	if _, err := adapter.Chat(context.Background(), request, providers.Call{ModelID: "gpt-test"}); err != nil {
		t.Fatalf("chat: %v", err)
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d, want no second refusal", attempts)
	}
	if upstream.last(t).body["instructions"] != "You are Codex." {
		t.Error("the required instructions must be used straight away afterwards")
	}
	if len(prompt.all()) > 1 {
		t.Errorf("the instructions must be fetched once: %d", len(prompt.all()))
	}
}

func TestCodexStoresARotatedSecret(t *testing.T) {
	// A saved connection's refresh token rotates on every refresh, so the rotated
	// secret has to be stored or the next call finds a dead token.
	upstream := newChatProvider(t, func(w http.ResponseWriter, r *http.Request, call chatCall) {
		codexStream(false)(w, r, call)
	})
	signIn := &fakeCodexSignIn{access: "access-2", accountID: "acct-1"}
	var stored []string
	adapter := newCodex(chatDefinition(t, "chatgpt"), chatDeps{baseURL: upstream.url() + "/backend-api/codex", secret: "tokens-1"}.deps(),
		signIn, func(accountID, secret string) error {
			stored = append(stored, accountID+"="+secret)
			return nil
		}, &codexInstructions{})
	if _, err := adapter.Chat(context.Background(), chatRequest(t, `{"messages":[]}`), providers.Call{ModelID: "gpt-test"}); err != nil {
		t.Fatalf("chat: %v", err)
	}
	if len(stored) != 1 || stored[0] != "acct-1=tokens-1-rotated" {
		t.Fatalf("stored = %v", stored)
	}
}

func TestCodexWithoutASignInReportsIt(t *testing.T) {
	adapter := codexAdapterFor(t, "http://127.0.0.1:1", nil)
	_, err := adapter.Chat(context.Background(), chatRequest(t, `{"messages":[]}`), providers.Call{ModelID: "gpt-test"})
	if providers.KindOf(err) != providers.KindAuth {
		t.Fatalf("kind = %q: %v", providers.KindOf(err), err)
	}
	if err.Error() != "This ChatGPT connection has no valid sign-in. Connect it again." {
		t.Errorf("message = %q", err.Error())
	}
}

func TestCodexEmptyAnswerIsAFailure(t *testing.T) {
	upstream := newChatProvider(t, func(w http.ResponseWriter, r *http.Request, call chatCall) {
		chatSSE(w, []string{`{"type":"response.completed","response":{"usage":{"input_tokens":1,"output_tokens":0}}}`}, false)
	})
	adapter := codexAdapterFor(t, upstream.url(), &fakeCodexSignIn{access: "a", accountID: "acct-1"})
	_, err := adapter.Chat(context.Background(), chatRequest(t, `{"messages":[]}`), providers.Call{ModelID: "gpt-test"})
	if providers.KindOf(err) != providers.KindEmpty {
		t.Fatalf("kind = %q: %v", providers.KindOf(err), err)
	}
	if err.Error() != "ChatGPT returned an empty response." {
		t.Errorf("message = %q", err.Error())
	}
}

func TestCodexRequestInputParts(t *testing.T) {
	// The canonical messages become Responses input items, images included.
	var body map[string]any
	upstream := newChatProvider(t, func(w http.ResponseWriter, r *http.Request, call chatCall) {
		body = call.body
		codexStream(false)(w, r, call)
	})
	adapter := codexAdapterFor(t, upstream.url(), &fakeCodexSignIn{access: "a", accountID: "acct-1"})
	request := chatRequest(t, `{
		"prompt_cache_key":"cache-1",
		"reasoning_effort":"high",
		"messages":[
			{"role":"user","content":[{"type":"text","text":"look"},{"type":"image_url","image_url":{"url":"data:image/png;base64,AAAA"}}]},
			{"role":"assistant","content":"calling","tool_calls":[{"id":"call_1","type":"function","function":{"name":"get","arguments":"{\"a\":1}"}}]},
			{"role":"tool","tool_call_id":"call_1","content":"done"}
		]}`)
	if _, err := adapter.Chat(context.Background(), request, providers.Call{ModelID: "gpt-test"}); err != nil {
		t.Fatalf("chat: %v", err)
	}
	if body["prompt_cache_key"] != "cache-1" {
		t.Errorf("prompt_cache_key = %v", body["prompt_cache_key"])
	}
	reasoning := body["reasoning"].(map[string]any)
	if reasoning["effort"] != "high" || reasoning["summary"] != "auto" {
		t.Errorf("reasoning = %v", reasoning)
	}
	input := body["input"].([]any)
	parts := input[0].(map[string]any)["content"].([]any)
	if parts[1].(map[string]any)["type"] != "input_image" {
		t.Errorf("an image part must be input_image: %v", parts[1])
	}
	// The assistant's text is its own message and the call another item.
	if input[1].(map[string]any)["role"] != "assistant" {
		t.Fatalf("the assistant turn = %v", input[1])
	}
	functionCall := input[2].(map[string]any)
	if functionCall["type"] != "function_call" || functionCall["call_id"] != "call_1" {
		t.Errorf("the assistant's call = %v", functionCall)
	}
	output := input[3].(map[string]any)
	if output["type"] != "function_call_output" || output["output"] != "done" {
		t.Errorf("the tool answer = %v", output)
	}
}
