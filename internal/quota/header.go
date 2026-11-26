package quota

import (
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// periodMetric is how a header family's period maps to a metric.
var periodMetric = map[string]map[string]string{
	"requests": {"minute": RPM, "day": RPD, "hour": RPM},
	"tokens":   {"minute": TPM, "day": TPD, "hour": TPM},
}

var (
	rateLimitField  = regexp.MustCompile(`^x-ratelimit-(limit|remaining|reset)-(requests|tokens)(?:-(minute|day|hour))?$`)
	plainRateLimit  = regexp.MustCompile(`^x-ratelimit-(limit|remaining|reset)$`)
	numericValue    = regexp.MustCompile(`^\d+(\.\d+)?$`)
	dateShapedValue = regexp.MustCompile(`\d{4}-\d{2}-\d{2}|GMT|UTC`)
	durationValue   = regexp.MustCompile(`(\d+(?:\.\d+)?)(ms|h|m|s|d)`)
)

// metricRank fixes the order observations are reported in.
var metricRank = map[string]int{RPM: 0, RPD: 1, TPM: 2, TPD: 3}

var durationUnits = map[string]float64{
	"ms": 1,
	"s":  1000,
	"m":  60_000,
	"h":  3_600_000,
	"d":  86_400_000,
}

var dateLayouts = []string{
	time.RFC3339Nano,
	time.RFC3339,
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05",
	"2006-01-02",
	"Mon, 02 Jan 2006 15:04:05 MST",
	"Mon, 02 Jan 2006 15:04:05 -0700",
}

// ParseReset reads a provider's reset header into an instant, nil when the
// value says nothing usable. It accepts "1m30.5s", "6ms", "2h", plain seconds,
// epoch seconds and epoch milliseconds.
func ParseReset(value string, now time.Time) *int64 {
	raw := strings.TrimSpace(value)
	if raw == "" {
		return nil
	}
	if numericValue.MatchString(raw) {
		number, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return nil
		}
		switch {
		case number > 1e12:
			return intPtr(int64(number))
		case number > 1e9:
			return intPtr(int64(number * 1000))
		default:
			return intPtr(now.UnixMilli() + int64(number*1000))
		}
	}
	if dateShapedValue.MatchString(raw) {
		if parsed, ok := parseDate(raw); ok {
			return intPtr(parsed.UnixMilli())
		}
	}
	var total float64
	matched := false
	for _, match := range durationValue.FindAllStringSubmatch(raw, -1) {
		amount, err := strconv.ParseFloat(match[1], 64)
		if err != nil {
			continue
		}
		total += amount * durationUnits[match[2]]
		matched = true
	}
	if !matched {
		return nil
	}
	return intPtr(now.UnixMilli() + int64(total))
}

func parseDate(raw string) (time.Time, bool) {
	if parsed, err := http.ParseTime(raw); err == nil {
		return parsed, true
	}
	for _, layout := range dateLayouts {
		if parsed, err := time.Parse(layout, raw); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

// ParseRateLimitHeaders reads the rate-limit headers of a response into
// observations, one per metric the headers mention. cfg says how this provider
// spells its periods and what a bare x-ratelimit-limit means.
func ParseRateLimitHeaders(header map[string][]string, cfg HeaderConfig, now time.Time) []Observation {
	periods := map[string]string{"requests": "minute", "tokens": "minute"}
	for name, period := range cfg.HeaderPeriods {
		periods[name] = period
	}
	plainMetric := cfg.PlainHeaderMetric
	if plainMetric == "" {
		plainMetric = RPM
	}

	var order []string
	byMetric := map[string]*Observation{}
	touch := func(metric string) *Observation {
		observation := byMetric[metric]
		if observation == nil {
			observation = &Observation{Metric: metric}
			byMetric[metric] = observation
			order = append(order, metric)
		}
		return observation
	}
	for name, values := range header {
		lower := strings.ToLower(name)
		value := strings.Join(values, ", ")
		var field, metric string
		if match := rateLimitField.FindStringSubmatch(lower); match != nil {
			field = match[1]
			period := match[3]
			if period == "" {
				period = periods[match[2]]
			}
			if period == "" {
				period = "minute"
			}
			metric = periodMetric[match[2]][period]
		} else if match := plainRateLimit.FindStringSubmatch(lower); match != nil {
			field = match[1]
			metric = plainMetric
		} else {
			continue
		}
		if metric == "" {
			continue
		}
		observation := touch(metric)
		if field == "reset" {
			observation.ResetAt = ParseReset(value, now)
			continue
		}
		first, _, _ := strings.Cut(value, ",")
		number, ok := jsNumber(first)
		if !ok {
			continue
		}
		if field == "limit" {
			observation.Limit = floatPtr(number)
		} else {
			observation.Remaining = floatPtr(number)
		}
	}
	out := make([]Observation, 0, len(order))
	for _, metric := range order {
		observation := byMetric[metric]
		if observation.Limit == nil && observation.Remaining == nil {
			continue
		}
		out = append(out, *observation)
	}
	// Headers arrive in a map, so the order is fixed here rather than left to
	// the map: requests before tokens, minute before day.
	sort.SliceStable(out, func(i, j int) bool {
		return metricRank[out[i].Metric] < metricRank[out[j].Metric]
	})
	return out
}

// jsNumber parses text as far as a header value needs it: blank is zero,
// anything unparsable is NaN.
func jsNumber(text string) (float64, bool) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return 0, true
	}
	number, err := strconv.ParseFloat(trimmed, 64)
	if err != nil {
		return 0, false
	}
	return number, true
}

var patternCache sync.Map // pattern → *regexp.Regexp

// MatchesPattern reports whether a definition's model pattern matches a model
// id. An empty pattern matches everything; an unparsable one matches nothing.
func MatchesPattern(pattern, modelID string) bool {
	if pattern == "" {
		return true
	}
	if cached, ok := patternCache.Load(pattern); ok {
		return cached.(*regexp.Regexp).MatchString(modelID)
	}
	compiled, err := regexp.Compile("(?i)" + pattern)
	if err != nil {
		return false
	}
	patternCache.Store(pattern, compiled)
	return compiled.MatchString(modelID)
}
