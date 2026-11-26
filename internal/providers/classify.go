package providers

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Classification: what a provider's failure means, read from its status, its
// error text and its headers, exactly as provider_error.js reads it. The kinds
// this produces drive routing, so the texts and the precedence are part of the
// contract.

var (
	quotaPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)per.?day`), regexp.MustCompile(`(?i)daily`), regexp.MustCompile(`(?i)quota`),
		regexp.MustCompile(`(?i)exhaust`), regexp.MustCompile(`(?i)\brpd\b`), regexp.MustCompile(`(?i)\btpd\b`),
		regexp.MustCompile(`(?i)monthly`), regexp.MustCompile(`(?i)(per|/) ?month\b`),
		regexp.MustCompile(`(?i)insufficient.?(credit|balance|quota)`),
	}
	usageLimit      = regexp.MustCompile(`(?i)exceeds? your plan'?s (set )?usage limit|(usage|quota|credit) limit (exceeded|reached)|out of (search )?credits`)
	contextPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)context.?(length|window)`), regexp.MustCompile(`(?i)maximum.?context`),
		regexp.MustCompile(`(?i)too many tokens`), regexp.MustCompile(`(?i)prompt is too long`),
		regexp.MustCompile(`(?i)reduce the length`), regexp.MustCompile(`(?i)input.{0,20}too long`),
		regexp.MustCompile(`(?i)exceeds.{0,30}(token|context)`),
	}
	// accessRefused: a setting only a person can change (a workspace's region, a
	// consent, a phone verification).
	accessRefused = regexp.MustCompile(`(?i)not available on the .{0,30}plan|model agreement|not allowed to access|needs your consent|phone verification|verify your (phone|account)|upgrade your plan|requires? global regions?|objected to passing your inputs|data (policy|retention) settings?`)
	// termsRefusal: a model this account may not use until the terms are accepted.
	termsRefusal = regexp.MustCompile(`(?i)terms acceptance|accept the terms|requires? (you to )?accept`)
	// allowanceUsedUp: the plan's included usage is used up and returns when the
	// window renews.
	allowanceUsedUp  = regexp.MustCompile(`(?i)add (usage )?credits or (subscribe|upgrade)|(session|hourly|daily|weekly|monthly) usage limit`)
	needsPayment     = regexp.MustCompile(`(?i)(active|valid).{0,40}subscription is required|insufficient (account )?(funds|balance|credits?)`)
	regionRefusal    = regexp.MustCompile(`(?i)region|country|location|not available in your|not (yet )?available to you|being rolled out|no access to (this )?model`)
	learnedLimit     = regexp.MustCompile(`(?i)limit[^0-9]{0,20}(\d[\d,]*)`)
	announcedMinutes = regexp.MustCompile(`(?i)back in (?:about |around |~)?(\d{1,3}) ?min`)
	announcedClock   = regexp.MustCompile(`(?i)back (?:at|by|from) (\d{1,2}):(\d{2}) ?UTC`)
)

// AllowanceUsedUp reports whether a provider's error text says only that the
// plan's included usage is used up.
func AllowanceUsedUp(message string) bool { return allowanceUsedUp.MatchString(message) }

// ParseRetryAfter reads retry-after-ms, then retry-after (seconds or an HTTP
// date), and reports how long to wait.
func ParseRetryAfter(headers http.Header) (time.Duration, bool) {
	if headers == nil {
		return 0, false
	}
	if raw := strings.TrimSpace(headers.Get("retry-after")); raw != "" {
		if seconds, ok := numberFromString(raw); ok {
			return time.Duration(max(0, seconds*1000)) * time.Millisecond, true
		}
		if at, err := http.ParseTime(raw); err == nil {
			return time.Duration(max(0, float64(time.Until(at).Milliseconds()))) * time.Millisecond, true
		}
	}
	if ms, ok := numberFromString(strings.TrimSpace(headers.Get("retry-after-ms"))); ok && ms > 0 {
		return time.Duration(ms * float64(time.Millisecond)), true
	}
	return 0, false
}

// ExtractMessage reads the human message out of a provider's error body, in
// the many shapes providers use, and keeps it to 2000 units.
func ExtractMessage(body any) string {
	if body == nil {
		return ""
	}
	if text, ok := body.(string); ok {
		return truncate(text)
	}
	candidates := []any{
		member(member(body, "error"), "message"),
		member(member(body, "error"), "msg"),
		stringCandidate(member(member(body, "error"), "detail")),
		member(body, "message"),
		member(member(body, "detail"), "message"),
		member(member(body, "detail"), "error"),
		member(body, "detail"),
		member(first(member(body, "errors")), "message"),
		member(body, "error"),
	}
	for _, candidate := range candidates {
		if !truthy(candidate) {
			continue
		}
		if text, ok := candidate.(string); ok {
			return truncate(text)
		}
		return truncate(compactJSON(body))
	}
	// Nothing readable inside: the body itself is the message, as
	// JSON.stringify(body) does in provider_error.js.
	return truncate(compactJSON(body))
}

// stringCandidate keeps a value only when it is a string, as the JS
// `typeof x === 'string' && x` test does.
func stringCandidate(value any) any {
	if text, ok := value.(string); ok {
		return text
	}
	return nil
}

// extractMessage is the message classify uses, with the HTTP fallback.
func extractMessage(body any, status int) string {
	if message := ExtractMessage(body); message != "" {
		return message
	}
	return "HTTP " + strconv.Itoa(status)
}

// LearnFromMessage pulls a "limit: 1000"-style number out of a rate-limit
// message, so the quota tracker can learn limits a provider does not publish.
func LearnFromMessage(message string) *LearnedLimit {
	match := learnedLimit.FindStringSubmatch(message)
	if match == nil {
		return nil
	}
	value, err := strconv.ParseFloat(strings.ReplaceAll(match[1], ",", ""), 64)
	if err != nil || value < 0 {
		return nil
	}
	// "limit: 0" means this model has no free quota at all on this account.
	if value == 0 {
		return &LearnedLimit{Metric: "rpd", Value: 0}
	}
	tokens := regexp.MustCompile(`(?i)token`).MatchString(message)
	switch {
	case regexp.MustCompile(`(?i)per.?minute|\bpm\b|rpm|tpm`).MatchString(message):
		return &LearnedLimit{Metric: ternary(tokens, "tpm", "rpm"), Value: value}
	case regexp.MustCompile(`(?i)per.?day|daily|rpd|tpd`).MatchString(message):
		return &LearnedLimit{Metric: ternary(tokens, "tpd", "rpd"), Value: value}
	}
	return nil
}

// AnnouncedReturn reads a planned outage that says when it ends: "back in
// about 11 min", "back at 12:00 UTC". A time already past today means
// tomorrow.
func AnnouncedReturn(message string, now time.Time) (time.Duration, bool) {
	if minutes := announcedMinutes.FindStringSubmatch(message); minutes != nil {
		value, err := strconv.Atoi(minutes[1])
		if err == nil {
			return time.Duration(value) * time.Minute, true
		}
	}
	clock := announcedClock.FindStringSubmatch(message)
	if clock == nil {
		return 0, false
	}
	hour, errHour := strconv.Atoi(clock[1])
	minute, errMinute := strconv.Atoi(clock[2])
	if errHour != nil || errMinute != nil {
		return 0, false
	}
	utc := now.UTC()
	at := time.Date(utc.Year(), utc.Month(), utc.Day(), hour, minute, 0, 0, time.UTC)
	if !at.After(now) {
		at = at.AddDate(0, 0, 1)
	}
	return at.Sub(now), true
}

// ClassifyHTTP turns a failed HTTP answer into a classified failure, with the
// texts provider_error.js produces.
func ClassifyHTTP(status int, body any, headers http.Header) *ProviderError {
	message := extractMessage(body, status)
	opts := ErrorOptions{Status: status, Body: body, Headers: headers}
	if wait, ok := ParseRetryAfter(headers); ok {
		opts.RetryAfter = &wait
	}
	if status == 400 || status == 413 || status == 422 {
		for _, pattern := range contextPatterns {
			if pattern.MatchString(message) {
				return NewError(KindContextLength, message, opts)
			}
		}
	}
	if (status == 402 || status == 429 || status == 403) && AllowanceUsedUp(message) {
		return NewError(KindQuotaExhausted, message, opts)
	}
	// A model this account may not use yet (Groq: "requires terms acceptance")
	// will keep failing the same way: unavailable, not a bad request.
	if (status == 400 || status == 403) && termsRefusal.MatchString(message) {
		opts.NeedsAction = true
		return NewError(KindNotFound, message, opts)
	}
	if (status == 400 || status == 401 || status == 403) && needsPayment.MatchString(message) {
		return NewError(KindPayment, message, opts)
	}
	// A setting only a person can change (a workspace's region): this model, not
	// the key.
	if status == 400 && accessRefused.MatchString(message) {
		opts.NeedsAction = true
		return NewError(KindNotFound, message, opts)
	}
	if status == 401 || status == 403 {
		if status == 403 && regionRefusal.MatchString(message) {
			return NewError(KindNotFound, message, opts)
		}
		if accessRefused.MatchString(message) {
			opts.NeedsAction = true
			return NewError(KindNotFound, message, opts)
		}
		return NewError(KindAuth, message, opts)
	}
	if status == 402 {
		return NewError(KindPayment, message, opts)
	}
	// The plan's allowance is used up until it renews (Tavily answers 432 for its
	// plan limit and 433 for a pay-as-you-go cap).
	if status == 432 || status == 433 || (status != 429 && usageLimit.MatchString(message)) {
		return NewError(KindQuotaExhausted, message, opts)
	}
	if status == 404 {
		return NewError(KindNotFound, message, opts)
	}
	if status == 408 {
		return NewError(KindTimeout, message, opts)
	}
	if status == 429 {
		kind := KindRateLimit
		for _, pattern := range quotaPatterns {
			if pattern.MatchString(message) {
				kind = KindQuotaExhausted
				break
			}
		}
		opts.Learned = LearnFromMessage(message)
		return NewError(kind, message, opts)
	}
	if status >= 500 {
		if opts.RetryAfter == nil {
			if wait, ok := AnnouncedReturn(message, time.Now()); ok {
				opts.RetryAfter = &wait
			}
		}
		return NewError(KindServer, message, opts)
	}
	return NewError(KindBadRequest, message, opts)
}

// ternary is the conditional expression for value choices.
func ternary[T any](condition bool, whenTrue, whenFalse T) T {
	if condition {
		return whenTrue
	}
	return whenFalse
}
