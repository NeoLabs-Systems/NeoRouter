package media

import (
	"context"
	"net/http"
	"strconv"

	"github.com/NeoLabs-Systems/neolabs-go/httpx"
	"github.com/NeoLabs-Systems/neorouter/internal/catalog"
	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
	"github.com/NeoLabs-Systems/neorouter/internal/providers"
	"github.com/NeoLabs-Systems/neorouter/internal/routerkeys"
)

// CreateVideo is POST /v1/videos: one video job, bound to the provider that
// accepted it.
func (s *Service) CreateVideo(c *call) error {
	input, err := parseVideoInput(c.body)
	if err != nil {
		return err
	}
	shielded, err := s.shieldTexts(c.key.UserID, []string{input.Prompt})
	if err != nil {
		return err
	}
	prompt := input.Prompt
	if len(shielded.Texts) > 0 {
		prompt = shielded.Texts[0]
	}
	seconds := float64(input.Seconds)
	size := ""
	if input.Size != nil {
		size = *input.Size
	}
	request := videoRequest{Model: input.Model, Prompt: &prompt, Seconds: &seconds, Size: input.Size}
	options := &RunOptions{
		Capability: "video", Method: providers.CapCreateVideo, RequestedModel: input.Model,
		FreeOnly: c.freeOnly, InputTokens: estimateTokens([]string{prompt}), Units: seconds,
		RedactedCount: shielded.Redacted,
		EstimateCost:  func(entry catalog.Entry) *float64 { return unitCost(entry, "second", seconds) },
		Invoke: func(ctx context.Context, attempt Attempt) (*Outcome, error) {
			creator, ok := attempt.Adapter.(providers.VideoCreator)
			if !ok {
				return nil, providers.NewError(providers.KindInvalidOutput, "This provider cannot generate video.", providers.ErrorOptions{})
			}
			job, err := creator.CreateVideo(ctx, providers.VideoRequest{
				Prompt: prompt, Seconds: strconv.Itoa(input.Seconds), Size: size,
			}, providers.Call{ModelID: attempt.Entry.ModelID, Timeout: attempt.Timeout})
			if err != nil {
				return nil, err
			}
			return &Outcome{Value: job, Headers: job.Headers}, nil
		},
		CostOf: func(entry catalog.Entry, _ *Outcome) *float64 {
			if cost := unitCost(entry, "second", seconds); cost != nil {
				return cost
			}
			return zeroCost()
		},
		Respond: func(w http.ResponseWriter, outcome *Outcome, entry catalog.Entry) error {
			job, _ := outcome.Value.(*providers.VideoJob)
			if job == nil {
				return httpserver.New(http.StatusBadGateway, "VIDEO_FAILED", "The provider accepted no video job.")
			}
			created, err := s.createVideoJob(c.r.Context(), c.key, httpserver.RequestIDFrom(c.r.Context()), entry, job, request)
			if err != nil {
				return err
			}
			httpx.WriteJSON(w, http.StatusOK, created.object())
			return nil
		},
	}
	_, err = s.run(c, options)
	return err
}

// SearchHit is one web search result, in the shape the API returns it.
type SearchHit struct {
	Title     string  `json:"title"`
	URL       string  `json:"url"`
	Snippet   string  `json:"snippet"`
	Content   string  `json:"content"`
	Published *string `json:"published"`
}

// SearchAnswer is one web search's answer.
type SearchAnswer struct {
	Model   string
	Results []SearchHit
}

// searchResponse is the /v1/search body.
type searchResponse struct {
	Object  string      `json:"object"`
	Model   string      `json:"model"`
	Query   string      `json:"query"`
	Results []SearchHit `json:"results"`
}

// Search is POST /v1/search.
func (s *Service) Search(c *call) error {
	input, err := parseSearchInput(c.body)
	if err != nil {
		return err
	}
	shielded, err := s.shieldTexts(c.key.UserID, []string{input.Query})
	if err != nil {
		return err
	}
	if len(shielded.Texts) > 0 {
		input.Query = shielded.Texts[0]
	}
	answer, err := s.searchThrough(c, input, shielded.Redacted, "search")
	if err != nil {
		return err
	}
	httpx.WriteJSON(c.w, http.StatusOK, searchResponse{
		Object: "search", Model: answer.Model, Query: input.Query, Results: answer.Results,
	})
	return nil
}

// SearchWeb runs one web search through the shared pipeline without writing a
// response, for chat's built-in web-search tool, which shields its own query.
// format names the api_format the usage log records ('web_search_tool' for that
// caller).
func (s *Service) SearchWeb(ctx context.Context, r *http.Request, key *routerkeys.Row, query SearchInput, freeOnly bool, format string) (SearchAnswer, error) {
	c := &call{r: r, key: key, freeOnly: freeOnly, body: nil}
	return s.searchThrough(c, query, 0, format)
}

// searchThrough runs one search and returns the answer instead of writing it.
func (s *Service) searchThrough(c *call, input SearchInput, redacted int, format string) (SearchAnswer, error) {
	model := input.Model
	if model == "" {
		model = "auto"
	}
	maxResults := input.MaxResults
	if maxResults == 0 {
		maxResults = 5
	}
	includeDomains := input.IncludeDomains
	if includeDomains == nil {
		includeDomains = []string{}
	}
	excludeDomains := input.ExcludeDomains
	if excludeDomains == nil {
		excludeDomains = []string{}
	}
	recency := ""
	if input.Recency != nil {
		recency = *input.Recency
	}
	query := input.Query
	options := &RunOptions{
		Capability: "search", Method: providers.CapSearch, RequestedModel: model, Format: format,
		FreeOnly: c.freeOnly, InputTokens: estimateTokens([]string{query}), Units: 1,
		RedactedCount: redacted,
		EstimateCost:  func(entry catalog.Entry) *float64 { return unitCost(entry, "search", 1) },
		Invoke: func(ctx context.Context, attempt Attempt) (*Outcome, error) {
			searcher, ok := attempt.Adapter.(providers.Searcher)
			if !ok {
				return nil, providers.NewError(providers.KindInvalidOutput, "This provider cannot search.", providers.ErrorOptions{})
			}
			result, err := searcher.Search(ctx, providers.SearchRequest{
				Query: query, MaxResults: maxResults,
				IncludeDomains: includeDomains, ExcludeDomains: excludeDomains, Recency: recency,
			})
			if err != nil {
				return nil, err
			}
			return &Outcome{Value: result, Headers: result.Headers}, nil
		},
		CostOf: func(entry catalog.Entry, _ *Outcome) *float64 {
			if cost := unitCost(entry, "search", 1); cost != nil {
				return cost
			}
			return zeroCost()
		},
	}
	run, err := s.run(c, options)
	if err != nil {
		return SearchAnswer{}, err
	}
	answer := SearchAnswer{Model: run.Entry.ID, Results: []SearchHit{}}
	if result, ok := run.Outcome.Value.(*providers.SearchResult); ok {
		for _, hit := range result.Results {
			answer.Results = append(answer.Results, SearchHit{
				Title: hit.Title, URL: hit.URL, Snippet: hit.Snippet, Content: hit.Content, Published: hit.Published,
			})
		}
	}
	return answer, nil
}

// weatherResponse is the /v1/weather body.
type weatherResponse struct {
	Object   string `json:"object"`
	Model    string `json:"model"`
	Location any    `json:"location"`
	Units    string `json:"units"`
	Current  any    `json:"current"`
	Hourly   any    `json:"hourly"`
	Daily    any    `json:"daily"`
}

// Weather is POST /v1/weather.
func (s *Service) Weather(c *call) error {
	input, err := parseWeatherInput(c.body)
	if err != nil {
		return err
	}
	redacted := 0
	if place, ok := input.Location.(string); ok {
		shielded, err := s.shieldTexts(c.key.UserID, []string{place})
		if err != nil {
			return err
		}
		if len(shielded.Texts) > 0 {
			input.Location = shielded.Texts[0]
		}
		redacted = shielded.Redacted
	}
	location, units, days := input.Location, input.Units, input.Days
	current, hourly, daily := input.Current, input.Hourly, input.Daily
	options := &RunOptions{
		Capability: "weather", Method: providers.CapWeather, RequestedModel: input.Model, Format: "weather",
		FreeOnly: c.freeOnly, Units: 1, RedactedCount: redacted,
		EstimateCost: func(entry catalog.Entry) *float64 { return unitCost(entry, "call", 1) },
		Invoke: func(ctx context.Context, attempt Attempt) (*Outcome, error) {
			service, ok := attempt.Adapter.(providers.Weatherer)
			if !ok {
				return nil, providers.NewError(providers.KindInvalidOutput, "This provider cannot report weather.", providers.ErrorOptions{})
			}
			report, err := service.Weather(ctx, providers.WeatherRequest{
				Location: location, Current: current, Hourly: hourly, Daily: daily, Days: days, Units: units,
			})
			if err != nil {
				return nil, err
			}
			return &Outcome{Value: report, Headers: report.Headers}, nil
		},
		CostOf: func(entry catalog.Entry, _ *Outcome) *float64 {
			if cost := unitCost(entry, "call", 1); cost != nil {
				return cost
			}
			return zeroCost()
		},
	}
	run, err := s.run(c, options)
	if err != nil {
		return err
	}
	report, _ := run.Outcome.Value.(*providers.WeatherReport)
	body := weatherResponse{Object: "weather", Model: run.Entry.ID, Units: units}
	if report != nil {
		body.Location = report.Location
		body.Current = report.Current
		body.Hourly = report.Hourly
		body.Daily = report.Daily
	}
	httpx.WriteJSON(c.w, http.StatusOK, body)
	return nil
}
