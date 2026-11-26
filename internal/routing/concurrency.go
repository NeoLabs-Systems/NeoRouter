package routing

import (
	"regexp"
	"strconv"
	"sync"
	"time"
)

// How many requests a provider account takes at once. Providers rarely document
// it and nobody knows it for their own key, so it is learned: a 429 while
// several requests run on the account shows the limit was passed (fewer than
// that many fit), and steady success at the learned limit probes one higher
// again. A provider definition may state a known limit
// (limits.account.concurrent). Learned limits fade after an hour without new
// evidence, so a single burst never caps an account for good.
const (
	forgetAfterMs      = hourMs
	successesToProbeUp = 10
)

// concurrencyPattern matches error texts that name concurrency outright (as
// opposed to a rate per minute).
var concurrencyPattern = regexp.MustCompile(`(?i)concurren|parallel requests|simultaneous|in.?flight|too many (?:open|active) (?:requests|connections)`)

// namedLimitPatterns find the limit when the provider names it ("concurrency
// limit: 4 in-flight requests", "max 4 concurrent requests"): exact, unlike a
// guess from how many were running here (other clients may share the key).
var namedLimitPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)concurren\w*\s+limit\D{0,12}(\d{1,4})`),
	regexp.MustCompile(`(?i)(\d{1,4})\s+(?:in.?flight|concurrent|parallel|simultaneous)`),
}

// ConcurrencyLimit is a known account concurrency and where it came from.
type ConcurrencyLimit struct {
	Limit  int    `json:"limit"`
	Source string `json:"source"` // manual | learned | documented
}

type learnedConcurrency struct {
	limit     int
	at        int64
	successes int
}

type concurrencyTracker struct {
	limits    AccountLimits
	providers ProviderDefinitions

	mu      sync.Mutex
	learned map[string]*learnedConcurrency
}

func newConcurrencyTracker(limits AccountLimits, providers ProviderDefinitions) *concurrencyTracker {
	return &concurrencyTracker{limits: limits, providers: providers, learned: map[string]*learnedConcurrency{}}
}

// documented is a provider definition's stated concurrency for a model.
func (c *concurrencyTracker) documented(entry *Entry) int {
	if c.providers == nil {
		return 0
	}
	return c.providers.DocumentedConcurrency(entry)
}

func (c *concurrencyTracker) current(accountID string, now time.Time) *learnedConcurrency {
	item, ok := c.learned[accountID]
	if !ok {
		return nil
	}
	if now.UnixMilli()-item.at > forgetAfterMs {
		delete(c.learned, accountID)
		return nil
	}
	return item
}

// limitFor is the account's concurrency limit, or nil when unknown.
func (c *concurrencyTracker) limitFor(entry *Entry, now time.Time) *ConcurrencyLimit {
	if entry == nil {
		return nil
	}
	// A number the person stated is exact: it never fades and is never learned
	// over.
	if c.limits != nil {
		if stated := c.limits.Stated(entry.AccountID).Concurrent; stated > 0 {
			return &ConcurrencyLimit{Limit: stated, Source: "manual"}
		}
	}
	c.mu.Lock()
	item := c.current(entry.AccountID, now)
	c.mu.Unlock()
	known := c.documented(entry)
	if item != nil {
		limit := item.limit
		if known > 0 && known < limit {
			limit = known
		}
		return &ConcurrencyLimit{Limit: limit, Source: "learned"}
	}
	if known > 0 {
		return &ConcurrencyLimit{Limit: known, Source: "documented"}
	}
	return nil
}

// isConcurrencyError reports a message that names concurrency.
func isConcurrencyError(message string) bool { return concurrencyPattern.MatchString(message) }

// namedLimit reads the limit out of an error message.
func namedLimit(message string) int {
	for _, pattern := range namedLimitPatterns {
		match := pattern.FindStringSubmatch(message)
		if match == nil {
			continue
		}
		value, err := strconv.Atoi(match[1])
		if err == nil && value >= 1 {
			return value
		}
	}
	return 0
}

// onRateLimited is the reaction to a rate-limit error. running counts the
// account's requests in flight, the failed one included. It returns true when
// the error looked like a concurrency limit rather than a per-minute rate.
func (c *concurrencyTracker) onRateLimited(entry *Entry, failure Failure, running *int, now time.Time) bool {
	explicit := isConcurrencyError(failure.Message)
	if c.limits != nil && c.limits.Stated(entry.AccountID).Concurrent > 0 {
		return explicit
	}
	inFlight := 0
	if running != nil {
		inFlight = *running
	}
	if !explicit && inFlight < 2 {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	item := c.current(entry.AccountID, now)
	if named := namedLimit(failure.Message); named > 0 {
		c.learned[entry.AccountID] = &learnedConcurrency{limit: named, at: now.UnixMilli()}
		return explicit
	}
	limit := inFlight - 1
	if limit < 1 {
		limit = 1
	}
	if item == nil || limit < item.limit || explicit {
		applied := limit
		if item != nil && item.limit < applied {
			applied = item.limit
		}
		c.learned[entry.AccountID] = &learnedConcurrency{limit: applied, at: now.UnixMilli()}
	} else {
		item.at = now.UnixMilli()
	}
	return explicit
}

// onSuccess is the reaction to an answer; running includes the request that
// just finished.
func (c *concurrencyTracker) onSuccess(entry *Entry, running *int, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	item := c.current(entry.AccountID, now)
	if item == nil {
		return
	}
	inFlight := 0
	if running != nil {
		inFlight = *running
	}
	if inFlight < item.limit {
		return
	}
	item.successes++
	if item.successes >= successesToProbeUp {
		item.limit++
		item.successes = 0
		item.at = now.UnixMilli()
	}
}

// forget drops what was learned about an account (a person says it works
// again).
func (c *concurrencyTracker) forget(accountID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.learned, accountID)
}
