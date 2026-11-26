package adapters

import (
	"context"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/NeoLabs-Systems/neorouter/internal/providers"
)

// Web search engines behind one method: search({ query, maxResults,
// includeDomains, excludeDomains, recency }) → one normalized result list.
// Every engine speaks its own dialect; the definition names it
// (`search.engine`).

// mediaSearchMaxContent is how much of a page's text a result may carry.
const mediaSearchMaxContent = 4000

// mediaSearchRecencyDays is how far back each recency window reaches.
var mediaSearchRecencyDays = map[string]int{"day": 1, "week": 7, "month": 31, "year": 366}

// mediaClip collapses whitespace and bounds a text.
func mediaClip(text string, max int) string {
	value := strings.Join(strings.Fields(text), " ")
	if mediaUTFLength(value) <= max {
		return value
	}
	return mediaSliceUTF(value, max) + "…"
}

// mediaUTFLength is a string's length in UTF-16 units.
func mediaUTFLength(text string) int { return len(utf16.Encode([]rune(text))) }

// mediaSliceUTF cuts a string to at most max UTF-16 units.
func mediaSliceUTF(text string, max int) string {
	units := utf16.Encode([]rune(text))
	if len(units) <= max {
		return text
	}
	return string(utf16.Decode(units[:max]))
}

// mediaSinceDate is the YYYY-MM-DD date a recency window starts at.
func mediaSinceDate(recency string) string {
	days := mediaSearchRecencyDays[recency]
	if days == 0 {
		return ""
	}
	return time.Now().UTC().Add(-time.Duration(days) * 24 * time.Hour).Format("2006-01-02")
}

// mediaHostOf is a URL's host without the www prefix, or "" when it has none.
func mediaHostOf(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(parsed.Hostname(), "www.")
}

// mediaOnDomain reports whether a URL is on one of the domains (or a
// subdomain of one).
func mediaOnDomain(rawURL string, domains []string) bool {
	host := mediaHostOf(rawURL)
	for _, domain := range domains {
		wanted := strings.ToLower(strings.TrimPrefix(domain, "www."))
		if host == wanted || strings.HasSuffix(host, "."+wanted) {
			return true
		}
	}
	return false
}

// mediaWithSiteOperators adds site: and -site: operators for engines that have
// no domain filters of their own.
func mediaWithSiteOperators(query string, includeDomains, excludeDomains []string) string {
	include := make([]string, 0, 5)
	for _, domain := range includeDomains {
		if len(include) >= 5 {
			break
		}
		include = append(include, "site:"+domain)
	}
	exclude := make([]string, 0, 10)
	for _, domain := range excludeDomains {
		if len(exclude) >= 10 {
			break
		}
		exclude = append(exclude, "-site:"+domain)
	}
	parts := make([]string, 0, 3)
	if query != "" {
		parts = append(parts, query)
	}
	if len(include) > 0 {
		parts = append(parts, "("+strings.Join(include, " OR ")+")")
	}
	if len(exclude) > 0 {
		parts = append(parts, strings.Join(exclude, " "))
	}
	return strings.Join(parts, " ")
}

// mediaSearchParams is one engine call's arguments.
type mediaSearchParams struct {
	Query          string
	MaxResults     int
	IncludeDomains []string
	ExcludeDomains []string
	Recency        string
}

// mediaSearchEngine is one engine's dialect.
type mediaSearchEngine func(ctx context.Context, adapter *SearchAdapter, params mediaSearchParams, call providers.Call) (*providers.SearchResult, error)

// mediaSearchEngines are the dialects a definition may name.
var mediaSearchEngines = map[string]mediaSearchEngine{
	"tavily":    searchTavily,
	"exa":       searchExa,
	"linkup":    searchLinkup,
	"parallel":  searchParallel,
	"firecrawl": searchFirecrawl,
	"serper":    searchSerper,
	"brave":     searchBrave,
	"jina":      searchJina,
	"searxng":   searchSearxng,
}

// mediaResultURL matches a result link worth keeping.
var mediaResultURL = regexp.MustCompile(`^https?://`)

// SearchAdapter is one search engine account.
type SearchAdapter struct {
	*mediaBase
}

// NewSearchAdapter builds the search adapter.
func NewSearchAdapter(def providers.Definition, deps providers.AdapterDeps) *SearchAdapter {
	return &SearchAdapter{mediaBase: newMediaBase(def, deps)}
}

// engineName is the dialect this account speaks.
func (s *SearchAdapter) engineName() string {
	if s.def.Search == nil {
		return ""
	}
	return s.def.Search.Engine
}

// Supports reports the one method a search engine serves.
func (s *SearchAdapter) Supports(method providers.Capability) bool {
	return method == providers.CapSearch
}

// TranscribeTranslate is true: the adapter never transcribes, and the default
// is "can translate".
func (s *SearchAdapter) TranscribeTranslate() bool { return true }

// TranscribeTimestamps is true for a provider that never transcribes.
func (s *SearchAdapter) TranscribeTimestamps() bool { return true }

// Validate runs a real one-result search: the only reliable check, at the cost
// of one credit.
func (s *SearchAdapter) Validate(ctx context.Context) (providers.Validation, error) {
	call := providers.Call{Timeout: 20 * time.Second}
	if _, err := s.run(ctx, providers.SearchRequest{Query: "NeoRouter", MaxResults: 1}, call); err != nil {
		classified := providers.AsProviderError(err)
		if classified == nil {
			return providers.UnknownKey(err.Error()), nil
		}
		switch classified.Kind {
		case providers.KindAuth, providers.KindPayment:
			return providers.InvalidKey(classified.Message), nil
		// The key was accepted and the allowance is only used up until it
		// renews (a free plan's monthly searches): no problem with the
		// connection.
		case providers.KindQuotaExhausted, providers.KindRateLimit:
			return providers.ValidKey(), nil
		}
		return providers.UnknownKey(classified.Message), nil
	}
	return providers.ValidKey(), nil
}

// ListModels is the definition's fixed listing: one search model.
func (s *SearchAdapter) ListModels(context.Context) ([]providers.Model, error) {
	return s.staticModels(nil), nil
}

// FetchQuota reports nothing: the engines publish no quota endpoint.
func (s *SearchAdapter) FetchQuota(context.Context) ([]providers.QuotaWindow, error) {
	return nil, nil
}

// Search runs one web search.
func (s *SearchAdapter) Search(ctx context.Context, req providers.SearchRequest) (*providers.SearchResult, error) {
	return s.run(ctx, req, providers.Call{})
}

// run is Search with the call's own timeout, for Validate.
func (s *SearchAdapter) run(ctx context.Context, req providers.SearchRequest, call providers.Call) (*providers.SearchResult, error) {
	engine, found := mediaSearchEngines[s.engineName()]
	if !found {
		return nil, providers.NewError(providers.KindBadRequest, "Unknown search engine "+s.engineName()+".", providers.ErrorOptions{})
	}
	wanted := req.MaxResults
	if wanted == 0 {
		wanted = 5
	}
	if wanted < 1 {
		wanted = 1
	}
	if wanted > 20 {
		wanted = 20
	}
	// Ask for a few more than needed: domain filtering below may drop some.
	asked := wanted
	if len(req.IncludeDomains) > 0 || len(req.ExcludeDomains) > 0 {
		asked += 5
	}
	if asked > 20 {
		asked = 20
	}
	result, err := engine(ctx, s, mediaSearchParams{
		Query: req.Query, MaxResults: asked,
		IncludeDomains: req.IncludeDomains, ExcludeDomains: req.ExcludeDomains,
		Recency: req.Recency,
	}, call)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool, len(result.Results))
	filtered := make([]providers.SearchHit, 0, len(result.Results))
	for _, hit := range result.Results {
		switch {
		case !mediaResultURL.MatchString(hit.URL):
			continue
		case len(req.IncludeDomains) > 0 && !mediaOnDomain(hit.URL, req.IncludeDomains):
			continue
		case len(req.ExcludeDomains) > 0 && mediaOnDomain(hit.URL, req.ExcludeDomains):
			continue
		case seen[hit.URL]:
			continue
		}
		seen[hit.URL] = true
		hit.Title = mediaClip(mediaFirstString(hit.Title, mediaHostOf(hit.URL)), 300)
		filtered = append(filtered, hit)
		if len(filtered) >= wanted {
			break
		}
	}
	return &providers.SearchResult{Results: filtered, Headers: result.Headers}, nil
}
