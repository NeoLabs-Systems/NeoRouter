package providers

import (
	"math"
	"regexp"
	"strconv"
	"strings"
)

// What a listing says a model costs. Prices are USD per 1M tokens; a provider
// that reports nothing is unknown, never free.

// freeModelID marks a model the listing itself prices at zero.
var freeModelID = regexp.MustCompile(`:free$`)

// ToNumber is the listing's number reader: null and "" are unknown; "$1,200"
// and " 3 " are numbers.
func ToNumber(value any) *float64 {
	if value == nil {
		return nil
	}
	if text, ok := value.(string); ok {
		if text == "" {
			return nil
		}
		cleaned := strings.Map(func(r rune) rune {
			if r == '$' || r == ',' || r == ' ' || r == '\t' || r == '\n' || r == '\r' {
				return -1
			}
			return r
		}, text)
		if cleaned == "" {
			return nil
		}
		number, err := strconv.ParseFloat(cleaned, 64)
		if err != nil || math.IsNaN(number) || math.IsInf(number, 0) {
			return nil
		}
		return &number
	}
	if number, ok := numberOf(value); ok {
		return &number
	}
	return nil
}

// perMillion converts OpenRouter-style per-token prices. A per-token price is
// always tiny, so values below 0.01 are treated as such.
func perMillion(value *float64, unitHint string) *float64 {
	if value == nil {
		return nil
	}
	switch unitHint {
	case "token":
		scaled := *value * 1e6
		return &scaled
	case "million":
		return value
	}
	if *value != 0 && *value < 0.01 {
		scaled := *value * 1e6
		return &scaled
	}
	return value
}

// price is a normalized price set.
type price struct {
	in    *float64
	out   *float64
	known bool
	free  bool
}

// ParsePricing reads what a listing says a text model costs.
func ParsePricing(raw map[string]any) (in, out *float64, known, free bool) {
	parsed := parsePricing(raw)
	return parsed.in, parsed.out, parsed.known, parsed.free
}

func parsePricing(raw map[string]any) price {
	if isPaidOnly(raw) {
		return price{}
	}
	// Free markers in a listing: xKiro access_tier, Api.Airforce and KiosAPI
	// tier, BlockRun billing_mode, Blaze and Iteracompute is_free.
	if jsString(raw["price"]) == "free" || jsString(raw["pricing"]) == "free" ||
		raw["is_free"] == true || raw["free"] == true ||
		jsString(raw["access_tier"]) == "free" || jsString(raw["tier"]) == "free" ||
		jsString(raw["billing_mode"]) == "free" {
		zero := 0.0
		return price{in: &zero, out: &zero, known: true, free: true}
	}
	// Requesty: input_price / output_price per token at the top level (its
	// pricing field is a list of context tiers).
	topIn, topOut := ToNumber(raw["input_price"]), ToNumber(raw["output_price"])
	if topIn != nil && topOut != nil && *topIn >= 0 && *topOut >= 0 {
		return price{in: perMillion(topIn, ""), out: perMillion(topOut, ""), known: true, free: *topIn == 0 && *topOut == 0}
	}
	pricing := object(raw["pricing"])
	listed := price{}
	if pricing == nil {
		if listed = parseListedCost(raw["cost"]); listed.known {
			return listed
		}
	}
	if pricing != nil {
		var values []float64
		for _, item := range pricing {
			if number := ToNumber(item); number != nil {
				values = append(values, *number)
			}
		}
		unit := ""
		if jsString(pricing["unit"]) == "token" {
			unit = "token"
		}
		prompt := ToNumber(firstNonNull(pricing["prompt"], pricing["input"], pricing["input_tokens"]))
		completion := ToNumber(firstNonNull(pricing["completion"], pricing["output"], pricing["output_tokens"]))
		// A negative price ("-1") marks a dynamic price only known after the
		// call: unknown, never cheap.
		for _, value := range values {
			if value < 0 {
				return price{}
			}
		}
		if len(values) > 0 && prompt != nil && completion != nil {
			allZero := true
			for _, value := range values {
				if value != 0 {
					allZero = false
					break
				}
			}
			return price{in: perMillion(prompt, unit), out: perMillion(completion, unit), known: true, free: allZero}
		}
	}
	return price{}
}

// parseListedCost reads the models.dev / OpenCode style cost block, which is
// always USD per million tokens.
func parseListedCost(cost any) price {
	text := object(member(cost, "text"))
	if text == nil {
		return price{}
	}
	// Both shapes: { input: { price } } and { input: number }.
	input := ToNumber(firstNonNull(member(member(text, "input"), "price"), text["input"]))
	output := ToNumber(firstNonNull(member(member(text, "output"), "price"), text["output"]))
	if input == nil || output == nil || *input < 0 || *output < 0 {
		return price{}
	}
	return price{in: input, out: output, known: true, free: *input == 0 && *output == 0}
}

// isPaidOnly reports whether the listing says this model needs paid credit
// whatever the plan or price (Pollinations' paid_only, usage_based_only,
// NavyAI's required_plan).
func isPaidOnly(raw map[string]any) bool {
	return raw["paid_only"] == true || raw["usage_based_only"] == true || truthy(raw["required_plan"])
}

// UnitPricing is what a media model is priced by.
type UnitPricing struct {
	Unit  *string
	Price *float64
}

// ParseUnitPricing reads the unit prices of media listings, in USD: OpenRouter's
// video SKUs are cents per output second per resolution; the cheapest
// resolution is the floor.
func ParseUnitPricing(raw map[string]any) UnitPricing {
	if skus := object(raw["pricing_skus"]); skus != nil {
		// Either "cents_per_video_output_second_720p" (cents) or
		// "duration_seconds_720p" (USD).
		var seconds []float64
		for name, value := range skus {
			if !strings.Contains(name, "second") || strings.Contains(name, "reference") || strings.Contains(name, "input") {
				continue
			}
			number := ToNumber(value)
			amount := -1.0
			if number != nil {
				amount = *number
			}
			if strings.HasPrefix(name, "cents") {
				amount /= 100
			}
			if amount >= 0 {
				seconds = append(seconds, amount)
			}
		}
		if len(seconds) > 0 {
			cheapest := seconds[0]
			for _, value := range seconds[1:] {
				cheapest = min(cheapest, value)
			}
			unit := "second"
			return UnitPricing{Unit: &unit, Price: &cheapest}
		}
	}
	for _, candidate := range []struct{ field, unit string }{
		{"price_per_minute", "minute"},
		{"price_per_mchar", "mchar"},
		{"price_per_search", "search"},
		{"price_per_call", "call"},
	} {
		if value := ToNumber(raw[candidate.field]); value != nil && *value >= 0 {
			unit := candidate.unit
			return UnitPricing{Unit: &unit, Price: value}
		}
	}
	if value := ToNumber(firstNonNull(raw["price_per_image"], member(member(raw, "pricing"), "per_image"))); value != nil && *value >= 0 {
		unit := "image"
		return UnitPricing{Unit: &unit, Price: value}
	}
	return UnitPricing{}
}
