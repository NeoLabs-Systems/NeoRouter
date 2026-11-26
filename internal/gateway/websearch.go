package gateway

// Built-in web search for chat. Clients ask for it in their own dialect;
// NeoRouter offers the routed model a plain `web_search` function, runs its
// calls through the owner's search engines (routing, fallback and quotas like
// any other kind) and repeats until the model answers. Inner turns are
// non-streaming; the answer is delivered in the client's format.

import (
	"context"
	"net/http"
	"regexp"
	"strings"

	"github.com/NeoLabs-Systems/neolabs-go/httpx"
	"github.com/NeoLabs-Systems/neorouter/internal/providers"
	"github.com/NeoLabs-Systems/neorouter/internal/routing"
)

const (
	// webSearchToolName is the function the model is offered.
	webSearchToolName = "web_search"
	// defaultMaxUses is how many searches a request may run by default, and the
	// most it may ask for.
	defaultMaxUses = 5
	maxUsesCeiling = 20
	// resultsPerSearch is how many results one search returns to the model.
	resultsPerSearch = 5
	// extractChars is how much of each result's text the model sees.
	extractChars = 1200
)

// webSearchInstruction tells the model how to use the tool.
const webSearchInstruction = "You can search the web with the web_search tool. Search when the question needs current or specific facts, " +
	"then answer from the results and cite the sources you used as markdown links."

// webSearchTool is the function definition offered to the model.
var webSearchTool = newObjOf(
	"type", "function",
	"function", newObjOf(
		"name", webSearchToolName,
		"description", "Search the web for current information. Returns the top results with title, URL and an extract.",
		"parameters", newObjOf(
			"type", "object",
			"properties", newObjOf("query", newObjOf(
				"type", "string",
				"description", "A focused search query, like one typed into a search engine.",
			)),
			"required", []any{"query"},
		),
	),
)

// webSearchSpellings match the tool types each dialect uses to ask for search.
var (
	responsesSearchTool = regexp.MustCompile(`^web_search(_preview)?(_\d+)?$`)
	anthropicSearchTool = regexp.MustCompile(`^web_search_\d+$`)
)

// webSearchSpec is what the client asked for.
type webSearchSpec struct {
	MaxUses        int
	IncludeDomains []string
	ExcludeDomains []string
	// Body is the request without the search marker.
	Body *Obj
}

// clampUses is clampUses: 1..maxUsesCeiling, or the default.
func clampUses(value any) int {
	number, ok := number(value)
	if !ok || number < 1 {
		return defaultMaxUses
	}
	uses := int(number)
	if uses > maxUsesCeiling {
		return maxUsesCeiling
	}
	return uses
}

// stringList is a JSON array of strings.
func stringList(value any) []string {
	items := asList(value)
	out := make([]string, 0, len(items))
	for _, item := range items {
		if text, ok := item.(string); ok {
			out = append(out, text)
		}
	}
	return out
}

// webSearchRequested is requested: what the client asked for, or nil. The body it
// returns has the marker taken out.
func webSearchRequested(dialect string, body *Obj) *webSearchSpec {
	if body == nil {
		return nil
	}
	if dialect == dialectOpenAIChat {
		var plugin *Obj
		for _, item := range getList(body, "plugins") {
			candidate := asObj(item)
			if getString(candidate, "id") == "web" {
				plugin = candidate
				break
			}
		}
		model := getString(body, "model")
		online := strings.HasSuffix(model, ":online")
		if !truthy(getValue(body, "web_search_options")) && plugin == nil && !online {
			return nil
		}
		stripped := clone(body)
		stripped.Delete("web_search_options")
		if plugins, ok := get(body, "plugins"); ok {
			kept := make([]any, 0, len(asList(plugins)))
			for _, item := range asList(plugins) {
				if getString(asObj(item), "id") == "web" {
					continue
				}
				kept = append(kept, item)
			}
			stripped.Set("plugins", kept)
		}
		if online {
			stripped.Set("model", strings.TrimSuffix(model, ":online"))
		}
		return &webSearchSpec{MaxUses: defaultMaxUses, Body: stripped}
	}
	tools := getList(body, "tools")
	isSearch := func(tool *Obj) bool {
		kind := getString(tool, "type")
		if dialect == dialectOpenAIResponses {
			return responsesSearchTool.MatchString(kind)
		}
		return anthropicSearchTool.MatchString(kind)
	}
	var search *Obj
	for _, item := range tools {
		tool := asObj(item)
		if isSearch(tool) {
			search = tool
			break
		}
	}
	if search == nil {
		return nil
	}
	include := stringList(getValue(search, "allowed_domains"))
	if len(include) == 0 {
		include = stringList(getValue(getObj(search, "filters"), "allowed_domains"))
	}
	exclude := stringList(getValue(search, "blocked_domains"))
	if len(exclude) == 0 {
		exclude = stringList(getValue(getObj(search, "filters"), "blocked_domains"))
	}
	kept := make([]any, 0, len(tools))
	for _, item := range tools {
		if isSearch(asObj(item)) {
			continue
		}
		kept = append(kept, item)
	}
	stripped := clone(body)
	stripped.Set("tools", kept)
	return &webSearchSpec{
		MaxUses:        clampUses(getValue(search, "max_uses")),
		IncludeDomains: include,
		ExcludeDomains: exclude,
		Body:           stripped,
	}
}

// webSearchAvailable is available: the user has a connected search engine.
func (s *Service) webSearchAvailable(userID string) bool {
	if s.opts.Catalog == nil {
		return false
	}
	for _, entry := range s.opts.Catalog.ListForUser(userID, routing.CatalogQuery{Capability: "search"}) {
		if entry.Enabled && entry.AccountEnabled {
			return true
		}
	}
	return false
}

// formatSearchResults is formatResults: the results as the model reads them.
func formatSearchResults(query string, results []providers.SearchHit) string {
	if len(results) == 0 {
		return "No results for \"" + query + "\"."
	}
	blocks := make([]string, 0, len(results))
	for index, item := range results {
		lines := make([]string, 0, 4)
		lines = append(lines, "["+jsString(float64(index+1))+"] "+item.Title)
		lines = append(lines, item.URL)
		if item.Published != nil && *item.Published != "" {
			lines = append(lines, "Published: "+*item.Published)
		}
		content := item.Content
		if content == "" {
			content = item.Snippet
		}
		lines = append(lines, truncateText(content, extractChars))
		kept := make([]string, 0, len(lines))
		for _, line := range lines {
			if line != "" {
				kept = append(kept, line)
			}
		}
		blocks = append(blocks, strings.Join(kept, "\n"))
	}
	return strings.Join(blocks, "\n\n")
}

// shieldQuery is shieldQuery: the placeholders the model saw are put back before
// the query goes to a search engine, then hidden again, so a rebuilt query never
// leaves with real values in it.
func (s *Service) shieldQuery(userID, query string) (string, bool) {
	if s.opts.ApplyProcessing == nil {
		return query, true
	}
	canonical := newObjOf("messages", []any{newObjOf("role", "user", "content", query)})
	processed, err := s.opts.ApplyProcessing(context.Background(), userID, canonical, ProcessingOptions{CompressionLevel: "off"})
	if err != nil {
		return "", false
	}
	messages := getList(processed.Canonical, "messages")
	for index := len(messages) - 1; index >= 0; index-- {
		message := asObj(messages[index])
		if getString(message, "role") != "user" {
			continue
		}
		if text := TextOf(getValue(message, "content")); text != "" {
			return text, true
		}
	}
	return query, true
}

// runSearchTool is runSearch: one tool call answered from the search engines.
func (s *Service) runSearchTool(run *executeRun, spec *webSearchSpec, call *Obj) (string, bool) {
	var arguments *Obj
	if parsed, err := parseOrdered([]byte(getString(getObj(call, "function"), "arguments"))); err == nil {
		arguments = asObj(parsed)
	}
	query := strings.TrimSpace(getString(arguments, "query"))
	if query == "" || utf16Len(query) > 400 {
		return "Search skipped: the query must be 1–400 characters.", false
	}
	safeQuery, ok := s.shieldQuery(run.key.UserID(), query)
	if !ok || safeQuery == "" {
		return "Search unavailable: the privacy shield could not check the query.", false
	}
	if s.opts.SearchWeb == nil {
		return "Search unavailable right now.", true
	}
	result, err := s.opts.SearchWeb(run.r.Context(), SearchCall{
		Key:            run.key,
		FreeOnly:       run.freeOnly,
		Format:         "web_search_tool",
		Query:          safeQuery,
		MaxResults:     resultsPerSearch,
		IncludeDomains: spec.IncludeDomains,
		ExcludeDomains: spec.ExcludeDomains,
	})
	if err != nil {
		if info := errorOf(err); info.status == http.StatusTooManyRequests {
			return "Search unavailable: too many requests right now.", true
		}
		return "Search unavailable right now.", true
	}
	if result == nil {
		return formatSearchResults(safeQuery, nil), true
	}
	return formatSearchResults(safeQuery, result.Results), true
}

// executeWithSearch is executeWithSearch: runs the chat with server-side web
// search, turn by turn, then answers in the client's format.
func (s *Service) executeWithSearch(outer *executeRun, spec *webSearchSpec, translator Translator) error {
	original, err := translator.ToCanonical(spec.Body)
	if err != nil {
		return err
	}
	clientTools := getList(original, "tools")
	messages := make([]any, 0, len(getList(original, "messages"))+4)
	messages = append(messages, newObjOf("role", "system", "content", webSearchInstruction))
	messages = append(messages, getList(original, "messages")...)
	searches := 0
	var final *Obj
	var lastHeaders http.Header
	for turn := 0; turn <= spec.MaxUses; turn++ {
		allowSearch := searches < spec.MaxUses
		inner := newCaptureWriter()
		preset := clone(original)
		preset.Set("messages", messages)
		preset.Set("stream", false)
		switch {
		case allowSearch:
			preset.Set("tools", append(append([]any{}, clientTools...), webSearchTool))
		case len(clientTools) > 0:
			preset.Set("tools", clientTools)
		default:
			preset.Delete("tools")
		}
		innerRun := &executeRun{
			service:  s,
			w:        inner,
			r:        outer.r,
			key:      outer.key,
			dialect:  dialectOpenAIChat,
			freeOnly: outer.freeOnly,
			internal: true,
			preset:   preset,
		}
		if err := innerRun.execute(); err != nil {
			return err
		}
		lastHeaders = inner.header
		body, parseErr := parseObject(inner.body)
		if parseErr != nil {
			return parseErr
		}
		message := getObj(asObj(firstItem(getList(body, "choices"))), "message")
		calls := getList(message, "tool_calls")
		searchCalls := make([]any, 0, len(calls))
		for _, item := range calls {
			if getString(getObj(asObj(item), "function"), "name") != webSearchToolName {
				continue
			}
			searchCalls = append(searchCalls, item)
		}
		// An answer, or a call to the client's own tools: the next turn is the
		// client's.
		if len(searchCalls) == 0 || len(searchCalls) != len(calls) {
			if len(searchCalls) == 0 {
				final = body
			}
			break
		}
		assistant := newObjOf("role", "assistant")
		if content, ok := get(message, "content"); ok {
			assistant.Set("content", content)
		} else {
			assistant.Set("content", nil)
		}
		assistant.Set("tool_calls", searchCalls)
		messages = append(messages, assistant)
		for _, item := range searchCalls {
			call := asObj(item)
			text := "Search limit reached: answer with the results you already have."
			searched := false
			if searches < spec.MaxUses {
				text, searched = s.runSearchTool(outer, spec, call)
			}
			if searched {
				searches++
			}
			messages = append(messages, newObjOf("role", "tool", "tool_call_id", getString(call, "id"), "content", text))
		}
	}
	outer.w.Header().Set("X-NeoRouter-Web-Searches", jsString(float64(searches)))
	// A plain chat answer is already final; other formats (and streams) get one
	// last turn in their own format, with the search results in context.
	if final != nil && outer.dialect == dialectOpenAIChat && !getBool(original, "stream") {
		for name, values := range lastHeaders {
			if !strings.HasPrefix(strings.ToLower(name), "x-neorouter-") {
				continue
			}
			for _, value := range values {
				outer.w.Header().Set(name, value)
			}
		}
		writeCaptured(outer.w, final)
		return nil
	}
	preset := clone(original)
	preset.Set("messages", messages)
	if len(clientTools) > 0 {
		preset.Set("tools", clientTools)
	} else {
		preset.Delete("tools")
	}
	lastRun := &executeRun{
		service:  s,
		w:        outer.w,
		r:        outer.r,
		key:      outer.key,
		dialect:  outer.dialect,
		freeOnly: outer.freeOnly,
		internal: true,
		preset:   preset,
	}
	outer.answered = lastRun.answered
	return lastRun.execute()
}

// captureWriter is captureResponse: it collects what an internal turn would have
// sent, so it can be folded into the next turn.
type captureWriter struct {
	header http.Header
	body   []byte
	status int
}

// newCaptureWriter is an empty captureWriter.
func newCaptureWriter() *captureWriter {
	return &captureWriter{header: http.Header{}, status: http.StatusOK}
}

// Header implements http.ResponseWriter.
func (c *captureWriter) Header() http.Header { return c.header }

// WriteHeader implements http.ResponseWriter.
func (c *captureWriter) WriteHeader(status int) { c.status = status }

// Write implements http.ResponseWriter.
func (c *captureWriter) Write(data []byte) (int, error) {
	c.body = append(c.body, data...)
	return len(data), nil
}

// Flush satisfies http.Flusher, so a streaming inner turn is harmless.
func (c *captureWriter) Flush() {}

// writeCaptured writes a captured JSON body to the client.
func writeCaptured(w http.ResponseWriter, body *Obj) {
	httpx.WriteJSON(w, http.StatusOK, body)
}
