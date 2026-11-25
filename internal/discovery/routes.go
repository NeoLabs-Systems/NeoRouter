package discovery

import (
	"net/http"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/NeoLabs-Systems/neolabs-go/httpx"
	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
)

// maxSafeInteger is Number.MAX_SAFE_INTEGER, the upper bound of asOf.
const maxSafeInteger = 9007199254740991

// Permission gates the discovery endpoints on a teammate permission, the way
// server/routes/index.js applies requireSession and
// requirePermission('providers.view') before them. internal/app supplies the
// auth and teams middleware.
type Permission func(permission string, next http.Handler) http.Handler

// Routes registers the discovery endpoints. permission may be nil only in tests.
func (s *Service) Routes(mux *http.ServeMux, permission Permission) {
	if permission == nil {
		permission = func(_ string, next http.Handler) http.Handler { return next }
	}
	const providersView = "providers.view"
	mux.Handle("GET /api/v1/discovery/benchmarks", permission(providersView, http.HandlerFunc(s.handleBenchmarks)))
	mux.Handle("GET /api/v1/discovery/news", permission(providersView, http.HandlerFunc(s.handleNews)))
}

// Service is the discovery API: the leaderboard and the news feed.
type Service struct {
	Benchmarks *Benchmarks
	News       *News
	// Log receives unexpected failures; nil uses slog's default.
	Log func(err error)
}

func (s *Service) handleBenchmarks(w http.ResponseWriter, r *http.Request) {
	options, err := parseQuery(r)
	if err != nil {
		httpserver.WriteError(w, r, nil, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, s.Benchmarks.List(ListOptions{
		Query: options.query, Capability: options.capability, Source: options.source,
		Free: options.free, Offset: options.offset, Limit: options.limit, Compare: options.compare,
	}))
}

func (s *Service) handleNews(w http.ResponseWriter, r *http.Request) {
	options, err := parseQuery(r)
	if err != nil {
		httpserver.WriteError(w, r, nil, err)
		return
	}
	page, err := s.News.List(r.Context(), NewsOptions{
		Query: options.query, Offset: options.offset, Limit: options.limit, AsOf: options.asOf,
	})
	if err != nil {
		if s.Log != nil {
			s.Log(err)
		}
		httpserver.WriteError(w, r, nil, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, page)
}

// options are the validated query parameters of server/routes/discovery.js.
type options struct {
	query      string
	capability string
	source     string
	free       bool
	offset     int
	limit      int
	asOf       *int64
	compare    []string
}

// invalidQuery is the route's answer to any query the schema rejects.
func invalidQuery() error {
	return httpserver.New(http.StatusBadRequest, "INVALID_QUERY", "Invalid discovery filters.")
}

// parseQuery applies the route's zod schema: bounded strings, an enumerated free
// flag, and integer bounds on the page window.
func parseQuery(r *http.Request) (options, error) {
	values := r.URL.Query()
	for _, key := range []string{"query", "capability", "source", "free", "offset", "limit", "asOf", "compare"} {
		if len(values[key]) > 1 {
			return options{}, invalidQuery() // an array is not the string the schema wants
		}
	}
	out := options{capability: "chat", limit: PageSize, free: false}
	var err error
	if out.query, err = boundedString(values.Get("query"), 200); err != nil {
		return options{}, err
	}
	if out.capability, err = boundedString(values.Get("capability"), 50); err != nil {
		return options{}, err
	}
	if out.capability == "" && !values.Has("capability") {
		out.capability = "chat"
	}
	if out.source, err = boundedString(values.Get("source"), 80); err != nil {
		return options{}, err
	}
	switch free := values.Get("free"); free {
	case "", "0":
	case "1":
		out.free = true
	default:
		return options{}, invalidQuery()
	}
	// An absent parameter takes its default; a present one is coerced, so an
	// empty limit is 0 and out of range, exactly like z.coerce.number().
	if values.Has("offset") {
		offset, ok := coerceInt(values.Get("offset"), 0, 1000000)
		if !ok {
			return options{}, invalidQuery()
		}
		out.offset = offset
	}
	if values.Has("limit") {
		limit, ok := coerceInt(values.Get("limit"), 1, MaxPageSize)
		if !ok {
			return options{}, invalidQuery()
		}
		out.limit = limit
	}
	if values.Has("asOf") {
		asOf, ok := coerceInt(values.Get("asOf"), 0, maxSafeInteger)
		if !ok {
			return options{}, invalidQuery()
		}
		value := int64(asOf)
		out.asOf = &value
	}
	compare, err := boundedString(values.Get("compare"), 1000)
	if err != nil {
		return options{}, err
	}
	for _, id := range strings.Split(compare, ",") {
		if id != "" {
			out.compare = append(out.compare, id)
		}
	}
	return out, nil
}

// boundedString applies z.string().max(n); a missing value is the empty string.
func boundedString(value string, max int) (string, error) {
	if len(utf16.Encode([]rune(value))) > max {
		return "", invalidQuery()
	}
	return value, nil
}

// coerceInt applies z.coerce.number().int().min(min).max(max): the query text is
// coerced the way Number() does, then must be a whole number inside the bounds.
func coerceInt(raw string, min, max int) (int, bool) {
	text := strings.TrimSpace(raw)
	value := 0.0
	if text != "" {
		parsed, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return 0, false
		}
		value = parsed
	}
	if value != float64(int64(value)) {
		return 0, false
	}
	integer := int64(value)
	if integer < int64(min) || integer > int64(max) {
		return 0, false
	}
	return int(integer), true
}
