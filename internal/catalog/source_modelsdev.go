package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// metadataTimeout is the per-request timeout of a metadata source.
const metadataTimeout = 60 * time.Second

// ProviderFacts are the models.dev facts for one provider's model, in USD per
// million tokens. A value the source does not publish is nil.
type ProviderFacts struct {
	Name            string   `json:"name,omitempty"`
	PriceCachedIn   *float64 `json:"priceCachedIn,omitempty"`
	PriceCacheWrite *float64 `json:"priceCacheWrite,omitempty"`
	PriceIn         *float64 `json:"priceIn,omitempty"`
	PriceOut        *float64 `json:"priceOut,omitempty"`
	ContextWindow   *int64   `json:"contextWindow,omitempty"`
	MaxOutput       *int64   `json:"maxOutput,omitempty"`
	Tools           *bool    `json:"tools,omitempty"`
	Vision          *bool    `json:"vision,omitempty"`
	Reasoning       *bool    `json:"reasoning,omitempty"`
	ReleaseDate     string   `json:"releaseDate,omitempty"`
	TextOutput      *bool    `json:"textOutput,omitempty"`
}

// Fact is one metadata record from any source, normalized into the union of
// the fields the indexes read.
type Fact struct {
	ProviderFacts
	// Source and the provider/model identity are set when the record is
	// indexed, not when it is stored.
	Source     string `json:"-"`
	ProviderID string `json:"-"`
	ModelID    string `json:"-"`
	ID         string `json:"id,omitempty"`
	Slug       string `json:"slug,omitempty"`
	Created    string `json:"created,omitempty"`
	// IsFree is the OpenRouter public list's verdict on a model's prices.
	IsFree bool `json:"isFree,omitempty"`
	// Artificial Analysis: the benchmark index, its per-benchmark scores and
	// the measured speed.
	IntelligenceIndex *float64           `json:"intelligenceIndex,omitempty"`
	IndexDerived      bool               `json:"indexDerived,omitempty"`
	Benchmarks        map[string]float64 `json:"benchmarks,omitempty"`
	OutputTps         *float64           `json:"outputTps,omitempty"`
	TtftSeconds       *float64           `json:"ttftSeconds,omitempty"`
}

// modelsDevURL publishes per-provider model facts as one JSON document.
const modelsDevURL = "https://models.dev/api.json"

// modelsDevProviderIDs maps a NeoRouter definition id onto the models.dev
// provider id where the two differ.
var modelsDevProviderIDs = map[string]string{
	"together":     "togetherai",
	"fireworks":    "fireworks-ai",
	"cloudflare":   "cloudflare-workers-ai",
	"moonshot":     "moonshotai",
	"dashscope":    "alibaba",
	"zhipu":        "zhipuai",
	"novita":       "novita-ai",
	"ollama_cloud": "ollama-cloud",
}

// modelsDevProviderKey is the models.dev provider id of a definition id.
func modelsDevProviderKey(definitionID string) string {
	if key, ok := modelsDevProviderIDs[definitionID]; ok {
		return key
	}
	return definitionID
}

type modelsDevSource struct {
	baseURL string
	client  *http.Client
}

type modelsDevModel struct {
	Name        string `json:"name"`
	Attachment  *bool  `json:"attachment"`
	Reasoning   *bool  `json:"reasoning"`
	ToolCall    *bool  `json:"tool_call"`
	ReleaseDate string `json:"release_date"`
	Cost        struct {
		Input      *float64 `json:"input"`
		Output     *float64 `json:"output"`
		CacheRead  *float64 `json:"cache_read"`
		CacheWrite *float64 `json:"cache_write"`
	} `json:"cost"`
	Limit struct {
		Context *float64 `json:"context"`
		Output  *float64 `json:"output"`
	} `json:"limit"`
	Modalities struct {
		Input  []string `json:"input"`
		Output []string `json:"output"`
	} `json:"modalities"`
}

// toModelsDevFact normalizes one models.dev entry.
func toModelsDevFact(model modelsDevModel) ProviderFacts {
	facts := ProviderFacts{
		Name:            model.Name,
		PriceCachedIn:   model.Cost.CacheRead,
		PriceCacheWrite: model.Cost.CacheWrite,
		PriceIn:         model.Cost.Input,
		PriceOut:        model.Cost.Output,
		ContextWindow:   floatToIntPtr(model.Limit.Context),
		MaxOutput:       floatToIntPtr(model.Limit.Output),
		Tools:           model.ToolCall,
		ReleaseDate:     model.ReleaseDate,
	}
	textOutput := true
	if len(model.Modalities.Output) > 0 {
		textOutput = containsString(model.Modalities.Output, "text")
	}
	facts.TextOutput = &textOutput
	switch {
	case len(model.Modalities.Input) > 0:
		vision := containsString(model.Modalities.Input, "image")
		facts.Vision = &vision
	case model.Attachment != nil:
		vision := *model.Attachment
		facts.Vision = &vision
	}
	if model.Reasoning != nil {
		reasoning := *model.Reasoning
		facts.Reasoning = &reasoning
	}
	return facts
}

// fetch reads every provider's models from models.dev.
func (s modelsDevSource) fetch(ctx context.Context) (map[string]map[string]*Fact, error) {
	var data map[string]struct {
		Models map[string]modelsDevModel `json:"models"`
	}
	if err := fetchJSON(ctx, s.client, s.baseURL, nil, &data); err != nil {
		return nil, sourceError(err, "models.dev")
	}
	byProvider := make(map[string]map[string]*Fact, len(data))
	for providerID, provider := range data {
		models := make(map[string]*Fact, len(provider.Models))
		for modelID, model := range provider.Models {
			facts := toModelsDevFact(model)
			models[modelID] = &Fact{ProviderFacts: facts}
		}
		byProvider[providerID] = models
	}
	return byProvider, nil
}

// fetchJSON GETs a URL and decodes its JSON body. Callers pass the headers a
// source needs; a non-2xx status is reported as *httpStatusError so each
// source can use its own error wording.
func fetchJSON(ctx context.Context, client *http.Client, url string, headers map[string]string, dst any) error {
	status, body, err := httpGet(ctx, client, url, headers)
	if err != nil {
		return err
	}
	if status < 200 || status > 299 {
		return &httpStatusError{Status: status}
	}
	if err := json.Unmarshal(body, dst); err != nil {
		return fmt.Errorf("catalog: metadata response: %w", err)
	}
	return nil
}

// httpGet performs one GET and returns the status and body.
func httpGet(ctx context.Context, client *http.Client, url string, headers map[string]string) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, nil, fmt.Errorf("catalog: metadata request: %w", err)
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("catalog: metadata request: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, fmt.Errorf("catalog: metadata response: %w", err)
	}
	return resp.StatusCode, body, nil
}

// httpStatusError carries the status a metadata source reports in its error
// text.
type httpStatusError struct{ Status int }

func (e *httpStatusError) Error() string { return "HTTP " + strconv.Itoa(e.Status) }

// sourceError renders a failed fetch with a source's own error wording.
func sourceError(err error, source string) error {
	var status *httpStatusError
	if errors.As(err, &status) {
		return fmt.Errorf("%s returned HTTP %d", source, status.Status)
	}
	return err
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

// floatToIntPtr rounds a source count into the integer column it is stored in.
func floatToIntPtr(value *float64) *int64 {
	if value == nil {
		return nil
	}
	number := int64(*value)
	return &number
}

// jsNumber converts the shapes a JSON document can hold to a number. ok is
// false when there is none.
func jsNumber(value any) (float64, bool) {
	switch v := value.(type) {
	case nil:
		return 0, true
	case bool:
		if v {
			return 1, true
		}
		return 0, true
	case float64:
		return v, true
	case string:
		trimmed := strings.TrimSpace(v)
		if trimmed == "" {
			return 0, true
		}
		number, err := strconv.ParseFloat(trimmed, 64)
		if err != nil {
			return 0, false
		}
		return number, true
	}
	return 0, false
}

// finiteNumber returns the value as a number when it is finite.
func finiteNumber(value any) (*float64, bool) {
	number, ok := jsNumber(value)
	if !ok {
		return nil, false
	}
	return &number, true
}

// truthyNumber is the value as a number, with zero as nil.
func truthyNumber(value any) *float64 {
	number, ok := finiteNumber(value)
	if !ok || *number == 0 {
		return nil
	}
	return number
}
