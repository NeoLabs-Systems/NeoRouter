package gateway

// Port of the chat-owned cases of test/api/search.test.js: the built-in web
// search loop, in every dialect a client can ask for it.

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/NeoLabs-Systems/neorouter/internal/providers"
	"github.com/NeoLabs-Systems/neorouter/internal/routing"
)

// searchEngine is the fake search engine: one result as the JS mock returns it.
func searchResult(query string) *providers.SearchResult {
	return &providers.SearchResult{Results: []providers.SearchHit{{
		Title:   "Result for " + query,
		URL:     "https://example.com/a",
		Content: "Something about " + query + ".",
	}}}
}

// searcherAdapter is the mock provider's searcher-* behavior: it calls the
// web_search tool once, then answers from the result it was given.
func searcherAdapter(modelID string) func(ctx context.Context, request *Obj, call providers.Call) (*providers.ChatResult, error) {
	return func(ctx context.Context, request *Obj, call providers.Call) (*providers.ChatResult, error) {
		hasTool := false
		for _, item := range getList(request, "tools") {
			if getString(getObj(asObj(item), "function"), "name") == "web_search" {
				hasTool = true
			}
		}
		var toolMessage *Obj
		for _, item := range getList(request, "messages") {
			if getString(asObj(item), "role") == "tool" {
				toolMessage = asObj(item)
			}
		}
		if hasTool && toolMessage == nil {
			call := newObjOf("id", "call_s1", "type", "function", "function", newObjOf("name", "web_search", "arguments", `{"query":"neorouter release"}`))
			message := newObjOf("role", "assistant", "content", nil, "tool_calls", []any{call})
			return &providers.ChatResult{Body: []byte(marshal(BuildResponse(ResponseOptions{
				Model:        modelID,
				Message:      message,
				FinishReason: "tool_calls",
				Usage:        newObjOf("prompt_tokens", float64(10), "completion_tokens", float64(5), "total_tokens", float64(15)),
			})))}, nil
		}
		answer := "Answer from: nothing"
		if toolMessage != nil {
			first := strings.SplitN(getString(toolMessage, "content"), "\n", 2)[0]
			answer = "Answer from: " + first
		}
		return &providers.ChatResult{Body: []byte(marshal(BuildResponse(ResponseOptions{
			Model:   modelID,
			Message: newObjOf("role", "assistant", "content", answer),
			Usage:   newObjOf("prompt_tokens", float64(10), "completion_tokens", float64(5), "total_tokens", float64(15)),
		})))}, nil
	}
}

// searcherStream is the same answer as a stream.
func searcherStream(modelID string) func(ctx context.Context, request *Obj, call providers.Call, yield func(*Obj) bool) error {
	return func(ctx context.Context, request *Obj, call providers.Call, yield func(*Obj) bool) error {
		var toolMessage *Obj
		for _, item := range getList(request, "messages") {
			if getString(asObj(item), "role") == "tool" {
				toolMessage = asObj(item)
			}
		}
		answer := "Answer from: nothing"
		if toolMessage != nil {
			answer = "Answer from: " + strings.SplitN(getString(toolMessage, "content"), "\n", 2)[0]
		}
		yield(BuildChunk("chunk-1", modelID, 1, newObjOf("role", "assistant", "content", ""), nil, nil))
		yield(BuildChunk("chunk-2", modelID, 1, newObjOf("content", answer), nil, nil))
		yield(BuildChunk("chunk-3", modelID, 1, newObj(), "stop", newObjOf("prompt_tokens", float64(10), "completion_tokens", float64(5), "total_tokens", float64(15))))
		return nil
	}
}

// searcherProvider is the fixture's search-capable chat account.
func searcherProvider(f *fixture) {
	accountID := f.account("llm", "free_tier")
	adapter := f.adapters[accountID]
	adapter.chat = func(ctx context.Context, request *Obj, call providers.Call) (*providers.ChatResult, error) {
		return searcherAdapter(call.ModelID)(ctx, request, call)
	}
	adapter.stream = func(ctx context.Context, request *Obj, call providers.Call, yield func(*Obj) bool) error {
		return searcherStream(call.ModelID)(ctx, request, call, yield)
	}
	f.model("llm", "searcher-one")
	// The engine is a catalog entry of the "search" kind.
	engineRef := f.nextRef + 1
	f.catalog.mu.Lock()
	f.catalog.entries = append(f.catalog.entries, &routing.Entry{
		Ref: engineRef, ID: "searx/searxng", ModelID: "searxng", Name: "searxng",
		CanonicalID: "searxng", Capability: "search", AccountID: accountID, UserID: f.userID,
		ProviderSlug: "searx", ProviderKey: "searx", ProviderName: "searx", DefinitionID: "searxng",
		BillingMode: "free_tier", Enabled: true, Available: true, AccountEnabled: true, IsFree: true,
	})
	f.catalog.mu.Unlock()
	f.nextRef = engineRef
}

func TestWithoutASearchEngineTheMarkerIsIgnored(t *testing.T) {
	f := newFixture(t, nil)
	f.mockProvider("mock", "free_tier", "ok-alpha")
	secret := freeKey(t, f)
	response := f.chat(secret, newObjOf(
		"model", "auto",
		"web_search_options", newObj(),
		"messages", []any{newObjOf("role", "user", "content", "hi")},
	))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
	if searches := response.Header().Get("X-NeoRouter-Web-Searches"); searches != "" {
		t.Errorf("X-NeoRouter-Web-Searches = %q", searches)
	}
}

func TestChatWithWebSearchOptionsSearchesAndAnswers(t *testing.T) {
	f := newFixture(t, nil)
	searcherProvider(f)
	var queries []string
	f.search = func(ctx context.Context, call SearchCall) (*providers.SearchResult, error) {
		queries = append(queries, call.Query)
		if call.MaxResults != resultsPerSearch {
			t.Errorf("max results = %d", call.MaxResults)
		}
		return searchResult(call.Query), nil
	}
	secret := freeKey(t, f)
	response := f.chat(secret, newObjOf(
		"model", "auto",
		"web_search_options", newObj(),
		"messages", []any{newObjOf("role", "user", "content", "What is new in NeoRouter?")},
	))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
	if searches := response.Header().Get("X-NeoRouter-Web-Searches"); searches != "1" {
		t.Errorf("X-NeoRouter-Web-Searches = %q", searches)
	}
	body := decode(t, response)
	message := getObj(asObj(firstItem(getList(body, "choices"))), "message")
	if content := getString(message, "content"); content != "Answer from: [1] Result for neorouter release" {
		t.Errorf("content = %q", content)
	}
	if _, ok := message.Get("tool_calls"); ok {
		t.Error("the internal search call reached the client")
	}
	if len(queries) != 1 || queries[0] != "neorouter release" {
		t.Errorf("queries = %v", queries)
	}
}

func TestAnthropicSearchToolAndOnlineModelSearchToo(t *testing.T) {
	f := newFixture(t, nil)
	searcherProvider(f)
	f.search = func(ctx context.Context, call SearchCall) (*providers.SearchResult, error) {
		return searchResult(call.Query), nil
	}
	secret := freeKey(t, f)
	anthropic := f.do(http.MethodPost, "/v1/messages", secret, newObjOf(
		"model", "auto",
		"max_tokens", float64(200),
		"tools", []any{newObjOf("type", "web_search_20250305", "name", "web_search", "max_uses", float64(2), "allowed_domains", []any{"example.com"})},
		"messages", []any{newObjOf("role", "user", "content", "Search please")},
	), map[string]string{"anthropic-version": "2023-06-01"})
	if anthropic.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", anthropic.Code, anthropic.Body.String())
	}
	if searches := anthropic.Header().Get("X-NeoRouter-Web-Searches"); searches != "1" {
		t.Errorf("X-NeoRouter-Web-Searches = %q", searches)
	}
	body := decode(t, anthropic)
	text := getString(asObj(firstItem(getList(body, "content"))), "text")
	if !strings.HasPrefix(text, "Answer from: [1]") {
		t.Errorf("text = %q", text)
	}
	streamed := f.chat(secret, newObjOf(
		"model", "llm/searcher-one:online",
		"stream", true,
		"messages", []any{newObjOf("role", "user", "content", "news?")},
	))
	if streamed.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", streamed.Code, streamed.Body.String())
	}
	if searches := streamed.Header().Get("X-NeoRouter-Web-Searches"); searches != "1" {
		t.Errorf("X-NeoRouter-Web-Searches = %q", searches)
	}
	text = streamed.Body.String()
	if !strings.Contains(text, "Answer from: [1] Result for neorouter release") {
		t.Errorf("stream = %q", text)
	}
	if !strings.Contains(text, "data: [DONE]") {
		t.Errorf("stream has no [DONE]: %q", text)
	}
}

func TestSearchResultsAreFormattedForTheModel(t *testing.T) {
	formatted := formatSearchResults("query", nil)
	if formatted != "No results for \"query\"." {
		t.Errorf("empty = %q", formatted)
	}
	published := "2026-01-01"
	formatted = formatSearchResults("q", []providers.SearchHit{
		{Title: "One", URL: "https://a.test", Snippet: "snippet", Published: &published},
		{Title: "Two", URL: "https://b.test", Content: strings.Repeat("x", 2000)},
	})
	if !strings.HasPrefix(formatted, "[1] One\nhttps://a.test\nPublished: 2026-01-01\nsnippet") {
		t.Errorf("formatted = %q", formatted)
	}
	if !strings.Contains(formatted, "[2] Two") {
		t.Errorf("second result missing: %q", formatted)
	}
	if strings.Contains(formatted, strings.Repeat("x", extractChars+1)) {
		t.Error("a result's text was not cut to the extract length")
	}
}

func TestSearchRequestDetection(t *testing.T) {
	cases := []struct {
		dialect string
		body    string
		want    bool
	}{
		{dialectOpenAIChat, `{"web_search_options": {}}`, true},
		{dialectOpenAIChat, `{"plugins": [{"id": "web"}]}`, true},
		{dialectOpenAIChat, `{"model": "x:online"}`, true},
		{dialectOpenAIChat, `{"messages": []}`, false},
		{dialectOpenAIResponses, `{"tools": [{"type": "web_search"}]}`, true},
		{dialectOpenAIResponses, `{"tools": [{"type": "web_search_preview_2025"}]}`, true},
		{dialectOpenAIResponses, `{"tools": [{"type": "function"}]}`, false},
		{dialectAnthropicMessages, `{"tools": [{"type": "web_search_20250305"}]}`, true},
		{dialectAnthropicMessages, `{"tools": [{"type": "web_search"}]}`, false},
	}
	for _, testCase := range cases {
		spec := webSearchRequested(testCase.dialect, parseBody(t, testCase.body))
		if got := spec != nil; got != testCase.want {
			t.Errorf("%s %s: detected = %v", testCase.dialect, testCase.body, got)
		}
	}
	// The marker is taken out of the body that is sent on.
	spec := webSearchRequested(dialectOpenAIChat, parseBody(t, `{"model": "llama:online", "web_search_options": {}, "plugins": [{"id": "web"}, {"id": "other"}], "messages": []}`))
	if model := getString(spec.Body, "model"); model != "llama" {
		t.Errorf("model = %q", model)
	}
	if _, ok := spec.Body.Get("web_search_options"); ok {
		t.Error("web_search_options was kept")
	}
	plugins := getList(spec.Body, "plugins")
	if len(plugins) != 1 || getString(asObj(plugins[0]), "id") != "other" {
		t.Errorf("plugins = %s", marshal(spec.Body))
	}
	// max_uses is clamped.
	spec = webSearchRequested(dialectAnthropicMessages, parseBody(t, `{"tools": [{"type": "web_search_1", "max_uses": 500}]}`))
	if spec.MaxUses != maxUsesCeiling {
		t.Errorf("maxUses = %d", spec.MaxUses)
	}
	spec = webSearchRequested(dialectAnthropicMessages, parseBody(t, `{"tools": [{"type": "web_search_1", "max_uses": 0}]}`))
	if spec.MaxUses != defaultMaxUses {
		t.Errorf("maxUses = %d", spec.MaxUses)
	}
}
