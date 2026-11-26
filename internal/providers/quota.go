package providers

import (
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// QuotaWindow is one limit a provider reports (the quota domain reads them):
// the metric it counts, the limit when the provider states one, what is left,
// and when the window renews.
type QuotaWindow struct {
	Metric string `json:"metric"`
	// Limit is nil when the provider reports only an amount, such as a balance.
	Limit *float64 `json:"limit"`
	// Remaining is nil when the answer says nothing about what is left.
	Remaining *float64 `json:"remaining"`
	// ResetAt is when the window renews, in unix milliseconds.
	ResetAt *int64 `json:"resetAt"`
}

// QuotaParser turns a quota probe's response body into windows.
type QuotaParser func(body any) []QuotaWindow

// quotaParsers maps the parser names written in definitions.json to their
// functions.
var quotaParsers = map[string]QuotaParser{
	"openrouter_credits":  openrouterCredits,
	"deepseek_balance":    deepseekBalance,
	"siliconflow_balance": siliconflowBalance,
	"synthetic_plan":      syntheticPlan,
	"minimax_windows":     minimaxWindows,
	"opencode_go_windows": opencodeGoWindows,
}

// window builds a window; limit and remaining are set when known.
func window(metric string, limit, remaining *float64, resetAt *int64) QuotaWindow {
	return QuotaWindow{Metric: metric, Limit: limit, Remaining: remaining, ResetAt: resetAt}
}

// numberOf converts a value to a number: a missing or unparsable value is not a
// number.
func numberOf(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case string:
		return numberFromString(v)
	}
	return 0, false
}

// numberFromString parses a trimmed number: an empty or blank string is 0,
// anything else must parse.
func numberFromString(text string) (float64, bool) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return 0, true
	}
	value, err := strconv.ParseFloat(trimmed, 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, false
	}
	return value, true
}

// pointer returns a pointer to value, for the windows that know it.
func pointer(value float64) *float64 { return &value }

// millis returns unix milliseconds for a timestamp.
func millis(at time.Time) *int64 {
	value := at.UnixMilli()
	return &value
}

// parseJSDate is Date.parse for the ISO forms providers use; invalid input is
// zero, which callers treat as "no reset time".
func parseJSDate(value any) *int64 {
	text, ok := value.(string)
	if !ok {
		return nil
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05", "2006-01-02 15:04:05", "2006-01-02"} {
		if at, err := time.Parse(layout, text); err == nil {
			return millis(at)
		}
	}
	return nil
}

// openrouterCredits is the OpenRouter probe: the key's credit limit and the
// request rate the account may use.
func openrouterCredits(body any) []QuotaWindow {
	info := object(member(body, "data"))
	var out []QuotaWindow
	if limit, ok := numberOf(info["limit"]); ok {
		var remaining *float64
		if value, present := info["limit_remaining"]; present && value != nil {
			// An unparsable amount is the provider's NaN: nothing is known.
			if amount, known := numberOf(value); known {
				remaining = &amount
			}
		} else {
			used := 0.0
			if amount, known := numberOf(info["usage"]); known {
				used = amount
			}
			rest := limit - used
			remaining = &rest
		}
		out = append(out, window("credits_usd", pointer(limit), remaining, nil))
	}
	if requests, ok := numberOf(member(member(info, "rate_limit"), "requests")); ok && requests != 0 {
		out = append(out, window("rate_requests", pointer(requests), nil, nil))
	}
	return out
}

// deepseekBalance probes the USD balance.
func deepseekBalance(body any) []QuotaWindow {
	var out []QuotaWindow
	for _, info := range list(member(body, "balance_infos")) {
		entry := object(info)
		if currency, _ := entry["currency"].(string); currency != "USD" {
			continue
		}
		remaining, known := numberOf(entry["total_balance"])
		out = append(out, window("balance", nil, nullable(remaining, known), nil))
	}
	return out
}

// siliconflowBalance probes the account balance.
func siliconflowBalance(body any) []QuotaWindow {
	data := object(member(body, "data"))
	balance, known := numberOf(data["totalBalance"])
	if !known {
		balance, known = numberOf(data["balance"])
	}
	if !known {
		return nil
	}
	return []QuotaWindow{window("balance", nil, pointer(balance), nil)}
}

// syntheticPlan probes how much of the weekly request allowance is left and
// when it renews.
func syntheticPlan(body any) []QuotaWindow {
	plan := object(member(body, "subscription"))
	if plan == nil {
		return nil
	}
	limit, known := numberOf(plan["limit"])
	if !known {
		return nil
	}
	used, usedKnown := numberOf(plan["requests"])
	if !usedKnown {
		used = 0
	}
	remaining := max(0, limit-used)
	return []QuotaWindow{{
		Metric:    "plan_requests",
		Limit:     pointer(limit),
		Remaining: pointer(remaining),
		ResetAt:   parseJSDate(plan["renewsAt"]),
	}}
}

// minimaxWindows reads MiniMax's per-model windows, reported as percentages
// left. Its count fields are unreliable, so only the percentages are read, and
// the tightest model decides each window.
func minimaxWindows(body any) []QuotaWindow {
	var models []map[string]any
	for _, item := range list(member(body, "model_remains")) {
		entry := object(item)
		if entry == nil {
			continue
		}
		name, _ := entry["model_name"].(string)
		if nonModelWindow.MatchString(name) {
			continue
		}
		models = append(models, entry)
	}
	tightest := func(percentField, endField string) (float64, *int64, bool) {
		best, bestReset, found := 0.0, (*int64)(nil), false
		for _, entry := range models {
			value, known := numberOf(entry[percentField])
			if !known {
				continue
			}
			// The reset field is a unix timestamp in seconds, stored as the
			// provider reported it (MiniMax's own unit).
			var resetAt *int64
			if seconds, ok := numberOf(entry[endField]); ok && seconds != 0 {
				value := int64(seconds)
				resetAt = &value
			}
			if !found || value < best {
				best, bestReset, found = value, resetAt, true
			}
		}
		return best, bestReset, found
	}
	var out []QuotaWindow
	if value, resetAt, ok := tightest("current_interval_remaining_percent", "end_time"); ok {
		out = append(out, window("plan_5h", pointer(100), pointer(value), resetAt))
	}
	if value, resetAt, ok := tightest("current_weekly_remaining_percent", "weekly_end_time"); ok {
		out = append(out, window("plan_week", pointer(100), pointer(value), resetAt))
	}
	return out
}

// nonModelWindow is the models a MiniMax plan window does not cover.
var nonModelWindow = regexp.MustCompile(`(?i)video|hailuo|speech|image|music`)

// opencodeGoWindows reads the console's three meters, each reported as the
// share used.
func opencodeGoWindows(body any) []QuotaWindow {
	meters := object(member(body, "usage"))
	if meters == nil {
		meters = object(body)
	}
	windows := []struct{ name, metric string }{
		{"rolling", "plan_5h"},
		{"weekly", "plan_week"},
		{"monthly", "plan_month"},
	}
	var out []QuotaWindow
	for _, candidate := range windows {
		meter := object(meters[candidate.name])
		if meter == nil {
			continue
		}
		used, known := numberOf(meter["percent"])
		if !known {
			used, known = numberOf(meter["usagePercent"])
		}
		if !known {
			continue
		}
		resetAt := parseJSDate(meter["resetsAt"])
		if resetAt == nil {
			if seconds, ok := numberOf(meter["resetInSec"]); ok && seconds != 0 {
				resetAt = millis(time.Now().Add(time.Duration(seconds * float64(time.Second))))
			}
		}
		remaining := max(0, min(100, round(100-used)))
		out = append(out, window(candidate.metric, pointer(100), pointer(remaining), resetAt))
	}
	return out
}

// round rounds half up, for the values a percentage takes.
func round(value float64) float64 {
	return float64(int64(value + 0.5))
}

// nullable is a pointer to value when it is known, else nil.
func nullable(value float64, known bool) *float64 {
	if !known {
		return nil
	}
	return &value
}
