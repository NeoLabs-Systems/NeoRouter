package quota

import (
	"testing"
	"time"
)

func TestParseReset(t *testing.T) {
	now := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	nowMS := now.UnixMilli()

	cases := []struct {
		name  string
		value string
		want  *int64
	}{
		{"empty", "", nil},
		{"seconds from now", "45", intPtr(nowMS + 45_000)},
		{"epoch seconds", "1780000000", intPtr(1780000000 * 1000)},
		{"epoch milliseconds", "1780000000000", intPtr(1780000000000)},
		{"duration with minutes and seconds", "1m30.5s", intPtr(nowMS + 90_500)},
		{"milliseconds", "6ms", intPtr(nowMS + 6)},
		{"hours", "2h", intPtr(nowMS + 7_200_000)},
		{"days", "1d", intPtr(nowMS + 86_400_000)},
		{"http date", "Tue, 10 Mar 2026 13:00:00 GMT", intPtr(time.Date(2026, 3, 10, 13, 0, 0, 0, time.UTC).UnixMilli())},
		{"iso date", "2026-03-10T13:00:00Z", intPtr(time.Date(2026, 3, 10, 13, 0, 0, 0, time.UTC).UnixMilli())},
		{"nonsense", "soon", nil},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			got := ParseReset(test.value, now)
			if test.want == nil {
				if got != nil {
					t.Fatalf("ParseReset(%q) = %d, want nil", test.value, *got)
				}
				return
			}
			if got == nil || *got != *test.want {
				t.Fatalf("ParseReset(%q) = %v, want %d", test.value, got, *test.want)
			}
		})
	}
}

func TestParseRateLimitHeaders(t *testing.T) {
	now := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	nowMS := now.UnixMilli()

	t.Run("openai style", func(t *testing.T) {
		observations := ParseRateLimitHeaders(map[string][]string{
			"X-ratelimit-limit-requests":     {"500"},
			"X-ratelimit-remaining-requests": {"499"},
			"X-ratelimit-reset-requests":     {"1m30s"},
			"X-ratelimit-limit-tokens":       {"20000"},
			"X-ratelimit-remaining-tokens":   {"19500"},
			"X-ratelimit-reset-tokens":       {"120"},
		}, HeaderConfig{}, now)
		if len(observations) != 2 {
			t.Fatalf("observations = %+v", observations)
		}
		if observations[0].Metric != RPM || *observations[0].Limit != 500 || *observations[0].Remaining != 499 {
			t.Fatalf("requests observation = %+v", observations[0])
		}
		if observations[0].ResetAt == nil || *observations[0].ResetAt != nowMS+90_000 {
			t.Fatalf("reset = %v", observations[0].ResetAt)
		}
		if observations[1].Metric != TPM || *observations[1].Limit != 20000 {
			t.Fatalf("tokens observation = %+v", observations[1])
		}
	})

	t.Run("cerebras day periods", func(t *testing.T) {
		observations := ParseRateLimitHeaders(map[string][]string{
			"X-ratelimit-limit-requests-day":   {"14400"},
			"X-ratelimit-limit-tokens-minute":  {"60000"},
			"X-ratelimit-remaining-tokens-day": {"1"},
		}, HeaderConfig{}, now)
		byMetric := map[string]Observation{}
		for _, observation := range observations {
			byMetric[observation.Metric] = observation
		}
		if byMetric[RPD].Limit == nil || *byMetric[RPD].Limit != 14400 {
			t.Fatalf("rpd = %+v", byMetric[RPD])
		}
		if byMetric[TPM].Limit == nil || *byMetric[TPM].Limit != 60000 {
			t.Fatalf("tpm = %+v", byMetric[TPM])
		}
		if byMetric[TPD].Remaining == nil || *byMetric[TPD].Remaining != 1 {
			t.Fatalf("tpd = %+v", byMetric[TPD])
		}
	})

	t.Run("definition periods", func(t *testing.T) {
		observations := ParseRateLimitHeaders(map[string][]string{
			"x-ratelimit-limit-requests": {"1000"},
			"x-ratelimit-limit-tokens":   {"500000"},
		}, HeaderConfig{HeaderPeriods: map[string]string{"requests": "day", "tokens": "day"}}, now)
		byMetric := map[string]Observation{}
		for _, observation := range observations {
			byMetric[observation.Metric] = observation
		}
		if byMetric[RPD].Limit == nil || *byMetric[RPD].Limit != 1000 {
			t.Fatalf("rpd = %+v", byMetric[RPD])
		}
		if byMetric[TPD].Limit == nil || *byMetric[TPD].Limit != 500000 {
			t.Fatalf("tpd = %+v", byMetric[TPD])
		}
	})

	t.Run("plain headers", func(t *testing.T) {
		observations := ParseRateLimitHeaders(map[string][]string{
			"x-ratelimit-limit":     {"200"},
			"x-ratelimit-remaining": {"150"},
		}, HeaderConfig{PlainHeaderMetric: TPM}, now)
		if len(observations) != 1 || observations[0].Metric != TPM || *observations[0].Limit != 200 {
			t.Fatalf("observations = %+v", observations)
		}
		plain := ParseRateLimitHeaders(map[string][]string{"x-ratelimit-limit": {"200"}}, HeaderConfig{}, now)
		if len(plain) != 1 || plain[0].Metric != RPM {
			t.Fatalf("plain default metric = %+v", plain)
		}
	})

	t.Run("reset only is not an observation", func(t *testing.T) {
		if observations := ParseRateLimitHeaders(map[string][]string{"x-ratelimit-reset-requests": {"30"}}, HeaderConfig{}, now); len(observations) != 0 {
			t.Fatalf("observations = %+v", observations)
		}
	})

	t.Run("joined values and blanks", func(t *testing.T) {
		observations := ParseRateLimitHeaders(map[string][]string{
			"x-ratelimit-limit-requests":     {"500, 500"},
			"x-ratelimit-remaining-requests": {""},
			"x-ratelimit-limit-tokens":       {"lots"},
		}, HeaderConfig{}, now)
		if len(observations) != 1 {
			t.Fatalf("observations = %+v", observations)
		}
		if *observations[0].Limit != 500 || *observations[0].Remaining != 0 {
			t.Fatalf("observation = %+v", observations[0])
		}
	})
}

func TestMatchesPattern(t *testing.T) {
	if !MatchesPattern("", "anything") {
		t.Fatal("an empty pattern matches everything")
	}
	if !MatchesPattern(":free$", "meta/llama:free") {
		t.Fatal("suffix patterns match")
	}
	if !MatchesPattern("^FREE/", "free/model") {
		t.Fatal("patterns are case insensitive")
	}
	if MatchesPattern(":free$", "meta/llama") {
		t.Fatal("a non-matching model is not matched")
	}
	if MatchesPattern("(unclosed", "x") {
		t.Fatal("an unparsable pattern matches nothing")
	}
}
