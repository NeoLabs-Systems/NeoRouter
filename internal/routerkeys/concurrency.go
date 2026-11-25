package routerkeys

import (
	"net/http"
	"sync"

	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
)

// Concurrency counts the client requests in flight per router key, including
// streams and internal retries, and refuses the ones past the key's limit.
// Uncapped keys are counted too, so a cap added mid-flight applies at once.
type Concurrency struct {
	mu     sync.Mutex
	active map[string]int64
}

// NewConcurrency builds an empty counter.
func NewConcurrency() *Concurrency {
	return &Concurrency{active: map[string]int64{}}
}

// InFlight is the requests currently running under a key.
func (c *Concurrency) InFlight(keyID string) int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.active[keyID]
}

// ErrConcurrencyLimited is what Acquire refuses with past the key's limit.
func ErrConcurrencyLimited() error {
	return httpserver.New(http.StatusTooManyRequests, "KEY_CONCURRENCY_LIMITED",
		"This router key has reached its limit for simultaneous requests.")
}

// Acquire takes one slot for the key and returns the release function. It
// fails with a 429 KEY_CONCURRENCY_LIMITED *HttpError when the key is at its
// limit.
func (c *Concurrency) Acquire(key *Row) (func(), error) {
	c.mu.Lock()
	count := c.active[key.ID]
	// A limit of zero (or none) is no limit at all.
	if key.ConcurrentLimit != nil && *key.ConcurrentLimit > 0 && count >= *key.ConcurrentLimit {
		c.mu.Unlock()
		return nil, ErrConcurrencyLimited()
	}
	c.active[key.ID] = count + 1
	c.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			c.mu.Lock()
			defer c.mu.Unlock()
			remaining := c.active[key.ID] - 1
			if remaining > 0 {
				c.active[key.ID] = remaining
				return
			}
			delete(c.active, key.ID)
		})
	}, nil
}

// Run takes a slot for the key around fn.
func (c *Concurrency) Run(key *Row, fn func() error) error {
	release, err := c.Acquire(key)
	if err != nil {
		return err
	}
	defer release()
	return fn()
}

// Reset drops the counters (tests).
func (c *Concurrency) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	clear(c.active)
}
