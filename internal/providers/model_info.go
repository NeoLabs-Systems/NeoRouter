package providers

import (
	"math"
	"strings"
)

// Model is one normalized model listing record. Prices are USD per 1M tokens.
// PriceKnown is only true when the provider itself reported a price; FreeHint
// is only true when every reported price field is exactly zero or the provider
// labels the model free. Unknown never means free.
type Model struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Kind          string   `json:"kind"`
	ContextWindow *int     `json:"contextWindow"`
	MaxOutput     *int     `json:"maxOutput"`
	PriceIn       *float64 `json:"priceIn"`
	PriceOut      *float64 `json:"priceOut"`
	PriceKnown    bool     `json:"priceKnown"`
	FreeHint      bool     `json:"freeHint"`
	PriceCachedIn *float64 `json:"priceCachedIn"`
	// PriceCacheWrite is what writing to the prompt cache costs.
	PriceCacheWrite *float64 `json:"priceCacheWrite"`
	// PriceUnit is the unit a media model is priced by: second, minute, mchar,
	// search, call or image.
	PriceUnit *string  `json:"priceUnit"`
	UnitPrice *float64 `json:"unitPrice"`

	Tools     *bool `json:"tools"`
	Vision    *bool `json:"vision"`
	JSON      *bool `json:"json"`
	Reasoning *bool `json:"reasoning"`
	// Decision marks a System One decision model.
	Decision bool `json:"decision"`
	// PaidOnly marks a model the listing says needs paid credit whatever the plan.
	PaidOnly bool `json:"paidOnly"`
	// Raw is the listing entry the record was built from.
	Raw map[string]any `json:"raw"`
}

// ModelOverrides replace normalized values an adapter knows better (a listing
// that does not publish them, or a dialect with fixed capabilities). A nil
// field leaves the normalized value alone.
type ModelOverrides struct {
	Kind          *string
	ContextWindow *int
	MaxOutput     *int
	Tools         *bool
	Vision        *bool
	JSON          *bool
	Reasoning     *bool
	PriceIn       *float64
	PriceOut      *float64
	PriceKnown    *bool
	FreeHint      *bool
}

// NormalizeModel turns one listing entry into a model record.
func NormalizeModel(raw map[string]any, overrides ModelOverrides) Model {
	if raw == nil {
		raw = map[string]any{}
	}
	id := strings.TrimSpace(jsString(firstNonNull(raw["id"], raw["name"], raw["model"], "")))
	params := supportedParameters(raw)
	inputs := firstTruthy(
		member(member(raw, "architecture"), "input_modalities"),
		raw["input_modalities"],
		member(member(raw, "modalities"), "input"),
	)
	contextWindow := ToNumber(firstNonNull(
		raw["context_length"], raw["context_window"], raw["max_context_length"],
		member(raw["top_provider"], "context_length"), raw["max_model_len"], raw["inputTokenLimit"],
		member(raw["limit"], "context"),
	))
	maxOutput := ToNumber(firstNonNull(
		raw["max_output_tokens"], member(raw["top_provider"], "max_completion_tokens"),
		raw["max_completion_tokens"], raw["outputTokenLimit"], member(raw["limit"], "output"),
	))
	pricing := parsePricing(raw)
	if freeModelID.MatchString(id) {
		zero := 0.0
		pricing = price{in: &zero, out: &zero, known: true, free: true}
	}
	cachedIn := ToNumber(raw["priceCachedIn"])
	if cachedIn == nil {
		if value := ToNumber(member(raw["pricing"], "input_cache_read")); value != nil {
			scaled := *value * 1e6
			cachedIn = &scaled
		}
	}
	cacheWrite := ToNumber(raw["priceCacheWrite"])
	if cacheWrite == nil {
		if value := ToNumber(member(raw["pricing"], "input_cache_write")); value != nil {
			scaled := *value * 1e6
			cacheWrite = &scaled
		}
	}
	units := ParseUnitPricing(raw)
	capabilities := member(raw, "capabilities")
	model := Model{
		ID:              id,
		Name:            jsString(firstNonNull(raw["display_name"], raw["displayName"], raw["name"], id)),
		Kind:            DetectKind(id, raw),
		ContextWindow:   positiveInt(contextWindow),
		MaxOutput:       positiveInt(maxOutput),
		PriceIn:         pricing.in,
		PriceOut:        pricing.out,
		PriceKnown:      pricing.known,
		FreeHint:        pricing.free,
		PriceCachedIn:   cachedIn,
		PriceCacheWrite: cacheWrite,
		PriceUnit:       units.Unit,
		UnitPrice:       units.Price,
		Tools: capability(params, flagValue(supportsValue(firstNonNull(
			raw["supports_tools"],
			member(capabilities, "function_calling"),
			member(capabilities, "tools"),
		))), "tools"),
		Vision:    visionSupport(inputs, raw),
		JSON:      parameterOnly(params, "response_format", "structured_outputs"),
		Reasoning: capability(params, flagValue(supportsValue(member(capabilities, "reasoning"))), "reasoning", "include_reasoning"),
		Decision:  containsString(stringList(member(member(raw, "architecture"), "output_modalities")), "decisions"),
		PaidOnly:  isPaidOnly(raw),
		Raw:       raw,
	}
	if overrides.Kind != nil {
		model.Kind = *overrides.Kind
	}
	if overrides.ContextWindow != nil {
		model.ContextWindow = overrides.ContextWindow
	}
	if overrides.MaxOutput != nil {
		model.MaxOutput = overrides.MaxOutput
	}
	if overrides.Tools != nil {
		model.Tools = overrides.Tools
	}
	if overrides.Vision != nil {
		model.Vision = overrides.Vision
	}
	if overrides.JSON != nil {
		model.JSON = overrides.JSON
	}
	if overrides.Reasoning != nil {
		model.Reasoning = overrides.Reasoning
	}
	if overrides.PriceIn != nil {
		model.PriceIn = overrides.PriceIn
	}
	if overrides.PriceOut != nil {
		model.PriceOut = overrides.PriceOut
	}
	if overrides.PriceKnown != nil {
		model.PriceKnown = *overrides.PriceKnown
	}
	if overrides.FreeHint != nil {
		model.FreeHint = *overrides.FreeHint
	}
	return model
}

// capability reads one capability the way normalizeModel does: a listing that
// publishes supported_parameters answers from that list, and one that does not
// answers from the flag the listing gave (nil when it gave none).
func capability(params []string, flag *bool, names ...string) *bool {
	if params == nil {
		return flag
	}
	found := false
	for _, name := range names {
		found = found || containsString(params, name)
	}
	return &found
}

// parameterOnly is a capability a listing only declares through
// supported_parameters; without that list it is unknown.
func parameterOnly(params []string, names ...string) *bool {
	if params == nil {
		return nil
	}
	found := false
	for _, name := range names {
		found = found || containsString(params, name)
	}
	return &found
}

// flagValue is nil when nothing is said, otherwise the value's truthiness.
func flagValue(value any) *bool {
	if value == nil {
		return nil
	}
	flagged := truthy(value)
	return &flagged
}

// supportsValue: a capability written as { supported: false } answers with its
// supported member.
func supportsValue(value any) any {
	if fields := object(value); fields != nil {
		if supported, ok := fields["supported"]; ok {
			return supported
		}
	}
	return value
}

// visionSupport reads whether a model takes images: the modalities if the
// listing declares them, else its vision flag.
func visionSupport(inputs any, raw map[string]any) *bool {
	if modalities := stringList(inputs); modalities != nil {
		found := false
		for _, modality := range modalities {
			if modality == "image" || strings.HasPrefix(strings.ToLower(modality), "image/") {
				found = true
				break
			}
		}
		return &found
	}
	return flagValue(firstNonNull(raw["supports_vision"], member(member(raw, "capabilities"), "vision")))
}

// supportedParameters reads the listing's supported_parameters list.
func supportedParameters(raw map[string]any) []string {
	if list, ok := raw["supported_parameters"].([]any); ok {
		return stringList(list)
	}
	if list, ok := raw["supported_parameters"].([]string); ok {
		return list
	}
	return nil
}

// positiveInt rounds a positive size and reports an unknown one as nil.
func positiveInt(value *float64) *int {
	if value == nil || *value <= 0 {
		return nil
	}
	rounded := int(math.Round(*value))
	return &rounded
}

// stringList reads a JSON array of strings ("" entries are kept as written).
func stringList(value any) []string {
	items, ok := value.([]any)
	if !ok {
		if list, ok := value.([]string); ok {
			return list
		}
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if text, ok := item.(string); ok {
			out = append(out, text)
		}
	}
	return out
}

// containsString reports whether list holds value.
func containsString(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}
