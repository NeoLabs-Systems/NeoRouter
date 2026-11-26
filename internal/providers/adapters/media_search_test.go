package adapters

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/NeoLabs-Systems/neorouter/internal/providers"
)

// The web search engines: one dialect each behind one method, and the domain
// filtering the caller asked for. The SearXNG cases, driven against the
// adapter.

// mediaSearchAdapter builds the adapter of one definition against baseURL.
func mediaSearchAdapter(t *testing.T, id, baseURL string) *SearchAdapter {
	t.Helper()
	return NewSearchAdapter(mediaTestDefinition(t, id), mediaTestDeps("k", baseURL, nil))
}

// TestWithSiteOperators: engines without a domain filter get operators instead.
func TestWithSiteOperators(t *testing.T) {
	cases := []struct {
		query    string
		include  []string
		exclude  []string
		expected string
	}{
		{"neorouter", nil, nil, "neorouter"},
		{"neorouter", []string{"example.com"}, nil, "neorouter (site:example.com)"},
		{"neorouter", nil, []string{"example.com"}, "neorouter -site:example.com"},
		{"neorouter", []string{"a.test", "b.test"}, []string{"c.test"}, "neorouter (site:a.test OR site:b.test) -site:c.test"},
		{"neorouter", []string{"1", "2", "3", "4", "5", "6"}, nil, "neorouter (site:1 OR site:2 OR site:3 OR site:4 OR site:5)"},
	}
	for _, item := range cases {
		if got := mediaWithSiteOperators(item.query, item.include, item.exclude); got != item.expected {
			t.Fatalf("mediaWithSiteOperators = %q, want %q", got, item.expected)
		}
	}
}

// TestOnDomain matches a domain and its subdomains, ignoring the www prefix.
func TestOnDomain(t *testing.T) {
	if !mediaOnDomain("https://www.example.com/a", []string{"example.com"}) {
		t.Fatal("www.example.com is on example.com")
	}
	if !mediaOnDomain("https://api.example.com/a", []string{"www.example.com"}) {
		t.Fatal("a subdomain is on its domain")
	}
	if mediaOnDomain("https://notexample.com/a", []string{"example.com"}) {
		t.Fatal("a different domain matched")
	}
	if mediaOnDomain("not a url", []string{"example.com"}) {
		t.Fatal("an unparsable link matched")
	}
}

// TestClipBoundsTheText: whitespace is collapsed and a long text is cut.
func TestClipBoundsTheText(t *testing.T) {
	if got := mediaClip("  a\n\tb  ", 100); got != "a b" {
		t.Fatalf("clip = %q", got)
	}
	got := mediaClip(strings.Repeat("x", 10), 4)
	if got != "xxxx…" {
		t.Fatalf("clip = %q", got)
	}
}

// TestSearXNGSearchFiltersTheDomains: the mock provider's answer, the operators
// it was asked with, and the filtering the adapter does itself.
func TestSearXNGSearchFiltersTheDomains(t *testing.T) {
	var queries []string
	base := mediaTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search" || r.URL.Query().Get("format") != "json" {
			t.Errorf("request = %s", r.URL.String())
		}
		queries = append(queries, r.URL.Query().Get("q"))
		mediaTestJSON(t, w, map[string]any{"results": []any{
			map[string]any{"title": "Result for neorouter", "url": "https://example.com/a", "content": "First result text."},
			map[string]any{"title": "Second", "url": "https://www.other.org/b", "content": "Second result text."},
		}})
	})
	adapter := mediaSearchAdapter(t, "searxng", base)
	first, err := adapter.Search(context.Background(), providers.SearchRequest{Query: "neorouter", MaxResults: 1})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(first.Results) != 1 || first.Results[0].URL != "https://example.com/a" {
		t.Fatalf("results = %+v", first.Results)
	}
	if first.Results[0].Snippet != "First result text." || first.Results[0].Content != "First result text." {
		t.Fatalf("result = %+v", first.Results[0])
	}
	filtered, err := adapter.Search(context.Background(), providers.SearchRequest{Query: "neorouter", ExcludeDomains: []string{"example.com"}})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(filtered.Results) != 1 || filtered.Results[0].URL != "https://www.other.org/b" {
		t.Fatalf("results = %+v", filtered.Results)
	}
	if !strings.Contains(queries[len(queries)-1], "-site:example.com") {
		t.Fatalf("query = %q", queries[len(queries)-1])
	}
	// A recency window is a parameter of the engine.
	if _, err := adapter.Search(context.Background(), providers.SearchRequest{Query: "neorouter", Recency: "week"}); err != nil {
		t.Fatalf("search: %v", err)
	}
}

// TestSearXNGReportsAnHTMLAnswer pins the message: the format has to be turned
// on in its own settings.
func TestSearXNGReportsAnHTMLAnswer(t *testing.T) {
	base := mediaTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><body>not json</body></html>"))
	})
	adapter := mediaSearchAdapter(t, "searxng", base)
	_, err := adapter.Search(context.Background(), providers.SearchRequest{Query: "neorouter"})
	if kind := mediaTestErrorKind(t, err); kind != providers.KindBadRequest {
		t.Fatalf("kind = %v", kind)
	}
	want := "SearXNG answered with HTML: enable the json format in its settings.yml (search.formats)."
	if got := mediaErrorText(err); got != want {
		t.Fatalf("message = %q", got)
	}
}

// TestSearchAsksForRoomToFilter: a few more results are requested when domains
// narrow the answer, because filtering may drop some.
func TestSearchAsksForRoomToFilter(t *testing.T) {
	var sent map[string]any
	base := mediaTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		sent = mediaTestBody(t, r)
		mediaTestJSON(t, w, map[string]any{"results": []any{
			map[string]any{"title": "a", "url": "https://example.com/a", "content": "text", "published_date": "2026-10-30"},
		}})
	})
	adapter := mediaSearchAdapter(t, "tavily", base)
	result, err := adapter.Search(context.Background(), providers.SearchRequest{
		Query: "neorouter", MaxResults: 5, IncludeDomains: []string{"example.com"}, Recency: "day",
	})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if sent["max_results"] != float64(10) {
		t.Fatalf("max_results = %v", sent["max_results"])
	}
	if sent["time_range"] != "day" {
		t.Fatalf("time_range = %v", sent["time_range"])
	}
	if !mediaSameStrings(mediaStrings(sent["include_domains"]), []string{"example.com"}) {
		t.Fatalf("include_domains = %v", sent["include_domains"])
	}
	if result.Results[0].Published == nil || *result.Results[0].Published != "2026-10-30" {
		t.Fatalf("published = %v", result.Results[0].Published)
	}
	if result.Results[0].Title != "a" {
		t.Fatalf("title = %q", result.Results[0].Title)
	}
}

// TestSearchUsesTheHostAsATitle: a result without a title is named by its host.
func TestSearchUsesTheHostAsATitle(t *testing.T) {
	base := mediaTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		mediaTestJSON(t, w, map[string]any{"results": []any{
			map[string]any{"title": "", "url": "https://www.example.com/a", "content": ""},
			map[string]any{"title": "dup", "url": "https://www.example.com/a", "content": ""},
			map[string]any{"title": "not a link", "url": "ftp://example.com/a", "content": ""},
		}})
	})
	adapter := mediaSearchAdapter(t, "searxng", base)
	result, err := adapter.Search(context.Background(), providers.SearchRequest{Query: "x"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(result.Results) != 1 || result.Results[0].Title != "example.com" {
		t.Fatalf("results = %+v", result.Results)
	}
}

// TestSearchValidatesWithARealSearch: the only reliable check costs one credit,
// and only a refused key is a definite verdict.
func TestSearchValidatesWithARealSearch(t *testing.T) {
	var queries []string
	base := mediaTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.Query().Get("q"))
		mediaTestJSON(t, w, map[string]any{"results": []any{}})
	})
	adapter := mediaSearchAdapter(t, "searxng", base)
	validation, err := adapter.Validate(context.Background())
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if validation.Status != providers.ValidationHealthy || len(queries) != 1 || queries[0] != "NeoRouter" {
		t.Fatalf("validation = %+v queries = %v", validation, queries)
	}
	limited := mediaSearchAdapter(t, "searxng", mediaTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":"quota used up"}`))
	}))
	if verdict, err := limited.Validate(context.Background()); err != nil || verdict.Status != providers.ValidationHealthy {
		t.Fatalf("a used-up allowance is not a broken connection: %+v %v", verdict, err)
	}
	refused := mediaSearchAdapter(t, "searxng", mediaTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"Invalid API key"}`))
	}))
	if verdict, err := refused.Validate(context.Background()); err != nil || verdict.Status != providers.ValidationInvalid {
		t.Fatalf("validation = %+v %v", verdict, err)
	}
	paid := mediaSearchAdapter(t, "searxng", mediaTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusPaymentRequired)
		_, _ = w.Write([]byte(`{"error":"Payment required"}`))
	}))
	if verdict, err := paid.Validate(context.Background()); err != nil || verdict.Status != providers.ValidationInvalid {
		t.Fatalf("validation = %+v %v", verdict, err)
	}
	broken := mediaSearchAdapter(t, "searxng", mediaTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"search exploded"}`))
	}))
	if verdict, err := broken.Validate(context.Background()); err != nil || verdict.Status != providers.ValidationUnknown {
		t.Fatalf("validation = %+v %v", verdict, err)
	}
}

// TestSearchAdapterSurface: one method, one static model, no quota endpoint.
func TestSearchAdapterSurface(t *testing.T) {
	adapter := mediaSearchAdapter(t, "searxng", "https://searx.test")
	if !adapter.Supports(providers.CapSearch) || adapter.Supports(providers.CapWeather) || adapter.Supports(providers.CapChat) {
		t.Fatal("a search engine serves search only")
	}
	models, err := adapter.ListModels(context.Background())
	if err != nil {
		t.Fatalf("models: %v", err)
	}
	if len(models) != 1 || models[0].Kind != providers.KindSearch {
		t.Fatalf("models = %+v", models)
	}
	if quota, err := adapter.FetchQuota(context.Background()); err != nil || quota != nil {
		t.Fatalf("quota = %v %v", quota, err)
	}
	unknown := NewSearchAdapter(providers.Definition{ID: "custom"}, mediaTestDeps("k", "", nil))
	_, err = unknown.Search(context.Background(), providers.SearchRequest{Query: "x"})
	if kind := mediaTestErrorKind(t, err); kind != providers.KindBadRequest {
		t.Fatalf("kind = %v", kind)
	}
	if got := mediaErrorText(err); got != "Unknown search engine ." {
		t.Fatalf("message = %q", got)
	}
}

// TestBraveSearchSendsItsOwnParameters: the count cap, the freshness code and
// the extra snippets the content is built from.
func TestBraveSearchSendsItsOwnParameters(t *testing.T) {
	var query = map[string]string{}
	base := mediaTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		for name, values := range r.URL.Query() {
			query[name] = values[0]
		}
		if r.Header.Get("X-Subscription-Token") != "k" {
			t.Errorf("key header = %q", r.Header.Get("X-Subscription-Token"))
		}
		mediaTestJSON(t, w, map[string]any{"web": map[string]any{"results": []any{map[string]any{
			"title": "Brave result", "url": "https://example.com/a",
			"description": "one", "extra_snippets": []any{"two", "three"}, "page_age": "2026-10-30",
		}}}})
	})
	adapter := mediaSearchAdapter(t, "brave_search", base+"/res/v1")
	result, err := adapter.Search(context.Background(), providers.SearchRequest{
		Query: "neorouter", MaxResults: 30, ExcludeDomains: []string{"example.org"}, Recency: "month",
	})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if query["count"] != "20" {
		t.Fatalf("count = %q", query["count"])
	}
	if query["freshness"] != "pm" {
		t.Fatalf("freshness = %q", query["freshness"])
	}
	if query["extra_snippets"] != "true" {
		t.Fatalf("extra_snippets = %q", query["extra_snippets"])
	}
	if !strings.Contains(query["q"], "-site:example.org") {
		t.Fatalf("q = %q", query["q"])
	}
	if result.Results[0].Content != "one two three" {
		t.Fatalf("content = %q", result.Results[0].Content)
	}
	if result.Results[0].Published == nil || *result.Results[0].Published != "2026-10-30" {
		t.Fatalf("published = %v", result.Results[0].Published)
	}
}

// TestExaAndFirecrawlSendTheirOwnBodies: the two dialects with a body of their
// own, including Firecrawl's either-or domain lists.
func TestExaAndFirecrawlSendTheirOwnBodies(t *testing.T) {
	var exaBody, firecrawlBody map[string]any
	exa := mediaTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		exaBody = mediaTestBody(t, r)
		mediaTestJSON(t, w, map[string]any{"results": []any{map[string]any{
			"title": "Exa", "url": "https://example.com/a", "text": "page text", "publishedDate": "2026-10-30",
		}}})
	})
	adapter := mediaSearchAdapter(t, "exa", exa)
	result, err := adapter.Search(context.Background(), providers.SearchRequest{
		Query: "neorouter", MaxResults: 3, IncludeDomains: []string{"example.com"}, Recency: "month",
	})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if exaBody["numResults"] != float64(8) || exaBody["type"] != "auto" {
		t.Fatalf("body = %+v", exaBody)
	}
	start := mediaString(exaBody["startPublishedDate"])
	if !strings.HasSuffix(start, "T00:00:00.000Z") || len(start) != len("2026-10-30T00:00:00.000Z") {
		t.Fatalf("startPublishedDate = %q", start)
	}
	if result.Results[0].Content != "page text" {
		t.Fatalf("result = %+v", result.Results[0])
	}

	firecrawl := mediaTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		firecrawlBody = mediaTestBody(t, r)
		mediaTestJSON(t, w, map[string]any{"data": map[string]any{"web": []any{map[string]any{
			"title": "Fire", "url": "https://example.com/a", "description": "short", "markdown": "long text",
		}}}})
	})
	adapter = mediaSearchAdapter(t, "firecrawl", firecrawl+"/v2")
	result, err = adapter.Search(context.Background(), providers.SearchRequest{
		Query: "neorouter", MaxResults: 2, IncludeDomains: []string{"example.com"}, ExcludeDomains: []string{"example.org"}, Recency: "day",
	})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if _, found := firecrawlBody["excludeDomains"]; found {
		t.Fatal("Firecrawl takes either list, not both")
	}
	if firecrawlBody["tbs"] != "qdr:d" {
		t.Fatalf("tbs = %v", firecrawlBody["tbs"])
	}
	if result.Results[0].Snippet != "short" || result.Results[0].Content != "long text" {
		t.Fatalf("result = %+v", result.Results[0])
	}
}

// mediaSameStrings compares two string lists.
func mediaSameStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// TestSearchRecencyWindowsAreDates: the engines that take a date get the start of
// the window, not the name of it.
func TestSearchRecencyWindowsAreDates(t *testing.T) {
	today := time.Now().UTC()
	cases := map[string]int{"day": 1, "week": 7, "month": 31, "year": 366}
	for recency, days := range cases {
		want := today.Add(-time.Duration(days) * 24 * time.Hour).Format("2006-01-02")
		if got := mediaSinceDate(recency); got != want {
			t.Fatalf("mediaSinceDate(%q) = %q, want %q", recency, got, want)
		}
	}
	if got := mediaSinceDate(""); got != "" {
		t.Fatalf("mediaSinceDate = %q", got)
	}
}
