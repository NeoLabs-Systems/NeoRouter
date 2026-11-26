package adapters

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/NeoLabs-Systems/neorouter/internal/providers"
)

// One dialect per search engine: what each one is sent and how its answer is
// read into the normalized result list.

// mediaPublished reads an optional published date.
func mediaPublished(value any) *string {
	text := mediaString(value)
	if text == "" {
		return nil
	}
	return &text
}

// mediaHits builds result records from a listing.
func mediaHits(items []any, read func(item any) providers.SearchHit) []providers.SearchHit {
	hits := make([]providers.SearchHit, 0, len(items))
	for _, item := range items {
		hits = append(hits, read(item))
	}
	return hits
}

// searchTavily is Tavily's search dialect.
func searchTavily(ctx context.Context, adapter *SearchAdapter, params mediaSearchParams, call providers.Call) (*providers.SearchResult, error) {
	body := map[string]any{
		"query": params.Query, "max_results": params.MaxResults,
		"search_depth": "basic", "include_raw_content": false,
	}
	if len(params.IncludeDomains) > 0 {
		body["include_domains"] = params.IncludeDomains
	}
	if len(params.ExcludeDomains) > 0 {
		body["exclude_domains"] = params.ExcludeDomains
	}
	if params.Recency != "" {
		body["time_range"] = params.Recency
	}
	response, err := adapter.doPost(ctx, "/search", body, call)
	if err != nil {
		return nil, err
	}
	items := mediaList(mediaMember(response.Data, "results"))
	return &providers.SearchResult{Headers: response.Headers, Results: mediaHits(items, func(item any) providers.SearchHit {
		content := mediaString(mediaMember(item, "content"))
		return providers.SearchHit{
			Title:     mediaString(mediaMember(item, "title")),
			URL:       mediaString(mediaMember(item, "url")),
			Snippet:   mediaClip(content, 400),
			Content:   mediaClip(content, mediaSearchMaxContent),
			Published: mediaPublished(mediaMember(item, "published_date")),
		}
	})}, nil
}

// searchExa is Exa's search dialect.
func searchExa(ctx context.Context, adapter *SearchAdapter, params mediaSearchParams, call providers.Call) (*providers.SearchResult, error) {
	body := map[string]any{
		"query": params.Query, "numResults": params.MaxResults, "type": "auto",
		"contents": map[string]any{"text": map[string]any{"maxCharacters": mediaSearchMaxContent}},
	}
	if len(params.IncludeDomains) > 0 {
		body["includeDomains"] = params.IncludeDomains
	}
	if len(params.ExcludeDomains) > 0 {
		body["excludeDomains"] = params.ExcludeDomains
	}
	if params.Recency != "" {
		body["startPublishedDate"] = mediaSinceDate(params.Recency) + "T00:00:00.000Z"
	}
	response, err := adapter.doPost(ctx, "/search", body, call)
	if err != nil {
		return nil, err
	}
	items := mediaList(mediaMember(response.Data, "results"))
	return &providers.SearchResult{Headers: response.Headers, Results: mediaHits(items, func(item any) providers.SearchHit {
		text := mediaString(mediaMember(item, "text"))
		return providers.SearchHit{
			Title:     mediaString(mediaMember(item, "title")),
			URL:       mediaString(mediaMember(item, "url")),
			Snippet:   mediaClip(text, 400),
			Content:   mediaClip(text, mediaSearchMaxContent),
			Published: mediaPublished(mediaMember(item, "publishedDate")),
		}
	})}, nil
}

// searchLinkup is Linkup's search dialect.
func searchLinkup(ctx context.Context, adapter *SearchAdapter, params mediaSearchParams, call providers.Call) (*providers.SearchResult, error) {
	body := map[string]any{"q": params.Query, "depth": "standard", "outputType": "searchResults"}
	if len(params.IncludeDomains) > 0 {
		body["includeDomains"] = params.IncludeDomains
	}
	if len(params.ExcludeDomains) > 0 {
		body["excludeDomains"] = params.ExcludeDomains
	}
	if params.Recency != "" {
		body["fromDate"] = mediaSinceDate(params.Recency)
	}
	response, err := adapter.doPost(ctx, "/search", body, call)
	if err != nil {
		return nil, err
	}
	items := mediaList(mediaMember(response.Data, "results"))
	return &providers.SearchResult{Headers: response.Headers, Results: mediaHits(items, func(item any) providers.SearchHit {
		if mediaString(mediaMember(item, "type")) == "image" {
			return providers.SearchHit{}
		}
		content := mediaString(mediaMember(item, "content"))
		return providers.SearchHit{
			Title:   mediaString(mediaMember(item, "name")),
			URL:     mediaString(mediaMember(item, "url")),
			Snippet: mediaClip(content, 400),
			Content: mediaClip(content, mediaSearchMaxContent),
		}
	})}, nil
}

// searchParallel is Parallel's search dialect.
func searchParallel(ctx context.Context, adapter *SearchAdapter, params mediaSearchParams, call providers.Call) (*providers.SearchResult, error) {
	sourcePolicy := map[string]any{}
	if len(params.IncludeDomains) > 0 {
		sourcePolicy["include_domains"] = params.IncludeDomains
	}
	if len(params.ExcludeDomains) > 0 {
		sourcePolicy["exclude_domains"] = params.ExcludeDomains
	}
	if params.Recency != "" {
		sourcePolicy["after_date"] = mediaSinceDate(params.Recency)
	}
	advanced := map[string]any{"max_results": params.MaxResults}
	if len(sourcePolicy) > 0 {
		advanced["source_policy"] = sourcePolicy
	}
	body := map[string]any{
		"objective": params.Query, "search_queries": []string{params.Query},
		"mode": "fast", "advanced_settings": advanced,
	}
	response, err := adapter.doPost(ctx, "/search", body, call)
	if err != nil {
		return nil, err
	}
	items := mediaList(mediaMember(response.Data, "results"))
	return &providers.SearchResult{Headers: response.Headers, Results: mediaHits(items, func(item any) providers.SearchHit {
		text := strings.Join(mediaStrings(mediaMember(item, "excerpts")), "\n")
		return providers.SearchHit{
			Title:     mediaString(mediaMember(item, "title")),
			URL:       mediaString(mediaMember(item, "url")),
			Snippet:   mediaClip(text, 400),
			Content:   mediaClip(text, mediaSearchMaxContent),
			Published: mediaPublished(mediaMember(item, "publish_date")),
		}
	})}, nil
}

// searchFirecrawl is Firecrawl's search dialect.
func searchFirecrawl(ctx context.Context, adapter *SearchAdapter, params mediaSearchParams, call providers.Call) (*providers.SearchResult, error) {
	body := map[string]any{
		"query": params.Query, "limit": params.MaxResults,
		"sources": []any{map[string]any{"type": "web"}},
	}
	// Firecrawl takes either list, not both.
	if len(params.IncludeDomains) > 0 {
		body["includeDomains"] = params.IncludeDomains
	} else if len(params.ExcludeDomains) > 0 {
		body["excludeDomains"] = params.ExcludeDomains
	}
	if params.Recency != "" {
		body["tbs"] = "qdr:" + params.Recency[:1]
	}
	response, err := adapter.doPost(ctx, "/search", body, call)
	if err != nil {
		return nil, err
	}
	items := mediaList(mediaMember(mediaMember(response.Data, "data"), "web"))
	return &providers.SearchResult{Headers: response.Headers, Results: mediaHits(items, func(item any) providers.SearchHit {
		description := mediaString(mediaMember(item, "description"))
		content := mediaFirstString(mediaString(mediaMember(item, "markdown")), description)
		return providers.SearchHit{
			Title:   mediaString(mediaMember(item, "title")),
			URL:     mediaString(mediaMember(item, "url")),
			Snippet: mediaClip(description, 400),
			Content: mediaClip(content, mediaSearchMaxContent),
		}
	})}, nil
}

// searchSerper is Serper's search dialect.
func searchSerper(ctx context.Context, adapter *SearchAdapter, params mediaSearchParams, call providers.Call) (*providers.SearchResult, error) {
	body := map[string]any{
		"q":   mediaWithSiteOperators(params.Query, params.IncludeDomains, params.ExcludeDomains),
		"num": params.MaxResults,
	}
	if params.Recency != "" {
		body["tbs"] = "qdr:" + params.Recency[:1]
	}
	response, err := adapter.doPost(ctx, "/search", body, call)
	if err != nil {
		return nil, err
	}
	items := mediaList(mediaMember(response.Data, "organic"))
	return &providers.SearchResult{Headers: response.Headers, Results: mediaHits(items, func(item any) providers.SearchHit {
		snippet := mediaString(mediaMember(item, "snippet"))
		return providers.SearchHit{
			Title:     mediaString(mediaMember(item, "title")),
			URL:       mediaString(mediaMember(item, "link")),
			Snippet:   mediaClip(snippet, 400),
			Content:   mediaClip(snippet, mediaSearchMaxContent),
			Published: mediaPublished(mediaMember(item, "date")),
		}
	})}, nil
}

// searchBrave is Brave's search dialect.
func searchBrave(ctx context.Context, adapter *SearchAdapter, params mediaSearchParams, call providers.Call) (*providers.SearchResult, error) {
	count := params.MaxResults
	if count > 20 {
		count = 20
	}
	query := url.Values{
		"q":              {mediaWithSiteOperators(params.Query, params.IncludeDomains, params.ExcludeDomains)},
		"count":          {strconv.Itoa(count)},
		"extra_snippets": {"true"},
	}
	if params.Recency != "" {
		freshness := map[string]string{"day": "pd", "week": "pw", "month": "pm", "year": "py"}
		query.Set("freshness", freshness[params.Recency])
	}
	response, err := adapter.do(ctx, "/web/search?"+query.Encode(), providers.Request{
		Method: http.MethodGet, Headers: mediaHeader("Accept", "application/json"),
	}, call)
	if err != nil {
		return nil, err
	}
	items := mediaList(mediaMember(mediaMember(response.Data, "web"), "results"))
	return &providers.SearchResult{Headers: response.Headers, Results: mediaHits(items, func(item any) providers.SearchHit {
		parts := append([]string{mediaString(mediaMember(item, "description"))}, mediaStrings(mediaMember(item, "extra_snippets"))...)
		return providers.SearchHit{
			Title:     mediaString(mediaMember(item, "title")),
			URL:       mediaString(mediaMember(item, "url")),
			Snippet:   mediaClip(mediaString(mediaMember(item, "description")), 400),
			Content:   mediaClip(strings.Join(parts, " "), mediaSearchMaxContent),
			Published: mediaPublished(mediaMember(item, "page_age")),
		}
	})}, nil
}

// searchJina is Jina's search dialect: the site filter has a parameter of its
// own when exactly one domain was asked for.
func searchJina(ctx context.Context, adapter *SearchAdapter, params mediaSearchParams, call providers.Call) (*providers.SearchResult, error) {
	headers := mediaHeader("Accept", "application/json", "X-Respond-With", "no-content")
	include := params.IncludeDomains
	if len(include) == 1 {
		headers.Set("X-Site", include[0])
		include = nil
	}
	path := "/?q=" + mediaEncodeURIComponent(mediaWithSiteOperators(params.Query, include, params.ExcludeDomains))
	response, err := adapter.do(ctx, path, providers.Request{Method: http.MethodGet, Headers: headers}, call)
	if err != nil {
		return nil, err
	}
	items := mediaList(mediaMember(response.Data, "data"))
	return &providers.SearchResult{Headers: response.Headers, Results: mediaHits(items, func(item any) providers.SearchHit {
		description := mediaString(mediaMember(item, "description"))
		content := mediaString(mediaMember(item, "content"))
		return providers.SearchHit{
			Title:     mediaString(mediaMember(item, "title")),
			URL:       mediaString(mediaMember(item, "url")),
			Snippet:   mediaClip(mediaFirstString(description, content), 400),
			Content:   mediaClip(mediaFirstString(content, description), mediaSearchMaxContent),
			Published: mediaPublished(mediaFirstNonNull(mediaMember(item, "date"), mediaMember(item, "publishedTime"))),
		}
	})}, nil
}

// searchSearxng is SearXNG's search dialect: it needs the json format turned on
// in its own settings.
func searchSearxng(ctx context.Context, adapter *SearchAdapter, params mediaSearchParams, call providers.Call) (*providers.SearchResult, error) {
	query := url.Values{
		"q":      {mediaWithSiteOperators(params.Query, params.IncludeDomains, params.ExcludeDomains)},
		"format": {"json"},
	}
	if params.Recency != "" {
		query.Set("time_range", params.Recency)
	}
	response, err := adapter.do(ctx, "/search?"+query.Encode(), providers.Request{
		Method: http.MethodGet, Headers: mediaHeader("Accept", "application/json"),
	}, call)
	if err != nil {
		return nil, err
	}
	if _, isText := response.Data.(string); isText {
		return nil, providers.NewError(providers.KindBadRequest,
			"SearXNG answered with HTML: enable the json format in its settings.yml (search.formats).", providers.ErrorOptions{})
	}
	items := mediaList(mediaMember(response.Data, "results"))
	return &providers.SearchResult{Headers: response.Headers, Results: mediaHits(items, func(item any) providers.SearchHit {
		content := mediaString(mediaMember(item, "content"))
		return providers.SearchHit{
			Title:     mediaString(mediaMember(item, "title")),
			URL:       mediaString(mediaMember(item, "url")),
			Snippet:   mediaClip(content, 400),
			Content:   mediaClip(content, mediaSearchMaxContent),
			Published: mediaPublished(mediaMember(item, "publishedDate")),
		}
	})}, nil
}
