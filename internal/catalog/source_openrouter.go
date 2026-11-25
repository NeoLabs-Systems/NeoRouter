package catalog

import (
	"context"
	"net/http"
	"time"
)

// openRouterModelsURL is OpenRouter's public model list: a broad market price
// reference for the same open models across hosts.
const openRouterModelsURL = "https://openrouter.ai/api/v1/models"

type openRouterPublicSource struct {
	baseURL string
	client  *http.Client
}

type openRouterModel struct {
	ID                  string   `json:"id"`
	ContextLength       *float64 `json:"context_length"`
	Created             *int64   `json:"created"`
	SupportedParameters []string `json:"supported_parameters"`
	Architecture        struct {
		InputModalities []string `json:"input_modalities"`
	} `json:"architecture"`
	Pricing map[string]any `json:"pricing"`
}

// fetch reads the public model list.
func (s openRouterPublicSource) fetch(ctx context.Context) ([]*Fact, error) {
	var body struct {
		Data []openRouterModel `json:"data"`
	}
	if err := fetchJSON(ctx, s.client, s.baseURL, nil, &body); err != nil {
		return nil, sourceError(err, "OpenRouter")
	}
	records := make([]*Fact, 0, len(body.Data))
	for _, model := range body.Data {
		records = append(records, toOpenRouterFact(model))
	}
	return records, nil
}

// toOpenRouterFact normalizes one public listing entry. Per-token prices are
// converted to USD per million tokens.
func toOpenRouterFact(model openRouterModel) *Fact {
	record := &Fact{ID: model.ID}

	// All published charges must be known and exactly zero; null or empty is
	// never free.
	if _, ok := model.Pricing["prompt"]; ok && model.Pricing["prompt"] != nil {
		if completion, ok := model.Pricing["completion"]; ok && completion != nil {
			allZero := true
			for _, value := range model.Pricing {
				number, finite := finiteNumber(value)
				if value == nil || !finite || number == nil || *number != 0 {
					allZero = false
					break
				}
				if text, isText := value.(string); isText && text == "" {
					allZero = false
					break
				}
			}
			record.IsFree = allZero
		}
	}
	if value, ok := finiteNumber(model.Pricing["input_cache_read"]); ok && value != nil {
		number := *value * 1e6
		record.PriceCachedIn = &number
	}
	if value, ok := finiteNumber(model.Pricing["input_cache_write"]); ok && value != nil {
		number := *value * 1e6
		record.PriceCacheWrite = &number
	}
	if value, ok := finiteNumber(model.Pricing["prompt"]); ok && value != nil {
		number := *value * 1e6
		record.PriceIn = &number
	}
	if value, ok := finiteNumber(model.Pricing["completion"]); ok && value != nil {
		number := *value * 1e6
		record.PriceOut = &number
	}
	record.ContextWindow = floatToIntPtr(model.ContextLength)
	if model.Created != nil && *model.Created != 0 {
		record.Created = time.Unix(*model.Created, 0).UTC().Format("2006-01-02")
	}
	reasoning := containsString(model.SupportedParameters, "reasoning")
	record.Reasoning = &reasoning
	tools := containsString(model.SupportedParameters, "tools")
	record.Tools = &tools
	vision := containsString(model.Architecture.InputModalities, "image")
	record.Vision = &vision
	return record
}

// toFloatOrNil keeps a decoded JSON number that a source may publish as a
// string, null when it is missing or not finite.
func toFloatOrNil(raw any) *float64 {
	number, ok := finiteNumber(raw)
	if !ok {
		return nil
	}
	return number
}
