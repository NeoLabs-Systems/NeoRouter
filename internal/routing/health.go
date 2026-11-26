package routing

import (
	"sync"
	"time"
)

// A connection that keeps failing, whatever the model: a provider asking for
// phone verification, a free tier that refuses this account. Each of its models
// has its own cooldown, so a provider with a hundred models would otherwise use
// up every request's attempts. After enough failures in a row without one
// success the whole connection rests, longer each time; one success ends it.
// Kept in memory: a restart simply tries again.
const (
	failuresToRest = 30
	firstRestMs    = 5 * minuteMs
	longestRestMs  = hourMs
	// After a rest this many failures send it back, so it is only probed
	// lightly.
	probesAfterRest = 3
	// "Marked as fixed" by a person: the next few failures of the connection
	// are forgiven (no cooldown, no rest, no penalty) so it gets a fair new
	// start.
	chancesAfterFixed = 6
)

type accountRest struct {
	failures int
	rests    int
	until    int64
}

type accountHealth struct {
	mu       sync.Mutex
	accounts map[string]*accountRest
	// fixed holds the chances a person's "this works again" left.
	fixed map[string]int
}

func newAccountHealth() *accountHealth {
	return &accountHealth{accounts: map[string]*accountRest{}, fixed: map[string]int{}}
}

func (h *accountHealth) onFailure(accountID string, now time.Time) {
	h.mu.Lock()
	defer h.mu.Unlock()
	item, ok := h.accounts[accountID]
	if !ok {
		item = &accountRest{}
		h.accounts[accountID] = item
	}
	item.failures++
	if item.failures >= failuresToRest {
		rest := firstRestMs << uint(item.rests)
		if rest > longestRestMs {
			rest = longestRestMs
		}
		item.until = now.UnixMilli() + rest
		item.rests++
		item.failures = failuresToRest - probesAfterRest
	}
}

// onSuccess clears a connection's record.
func (h *accountHealth) onSuccess(accountID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.accounts, accountID)
	delete(h.fixed, accountID)
}

// markFixed forgets the rest and gives new chances.
func (h *accountHealth) markFixed(accountID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.accounts, accountID)
	h.fixed[accountID] = chancesAfterFixed
}

// takeChance uses one of the new chances; true while the failure should be
// forgiven.
func (h *accountHealth) takeChance(accountID string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	left := h.fixed[accountID]
	if left <= 0 {
		return false
	}
	if left == 1 {
		delete(h.fixed, accountID)
	} else {
		h.fixed[accountID] = left - 1
	}
	return true
}

// chancesLeft is how many forgiven failures the connection has left.
func (h *accountHealth) chancesLeft(accountID string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.fixed[accountID]
}

// restingUntil is when the connection may be tried again, 0 when it is not
// resting.
func (h *accountHealth) restingUntil(accountID string, now time.Time) int64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	item, ok := h.accounts[accountID]
	if !ok {
		return 0
	}
	if item.until > now.UnixMilli() {
		return item.until
	}
	return 0
}

// Constants exported for the routes that report what a "fixed" connection got.
const (
	FailuresToRest    = failuresToRest
	FirstRestMs       = firstRestMs
	ChancesAfterFixed = chancesAfterFixed
)
