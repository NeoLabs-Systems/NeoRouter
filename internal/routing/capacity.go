package routing

import "sync"

// Whether a user's requests are outrunning what their models can take, from how
// their last requests ended. While they are, waiting rarely gets an answer and
// a client is better off told at once, and a second attempt on the side only
// spends free quota somebody else is queueing for.
const (
	capacityWindow     = 200
	capacityMinSamples = 50
	shortOfCapacity    = 0.3
	// ShortWaitMs is how long a request may wait while the user is short of
	// capacity.
	ShortWaitMs = 5000
	// CapacityMinSamples is how many recent answers feed the estimate.
	CapacityMinSamples = capacityMinSamples
)

type capacityTracker struct {
	mu    sync.Mutex
	users map[string][]bool
}

func newCapacityTracker() *capacityTracker {
	return &capacityTracker{users: map[string][]bool{}}
}

func (c *capacityTracker) record(userID string, gotAnAnswer bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	recent := c.users[userID]
	recent = append(recent, gotAnAnswer)
	if len(recent) > capacityWindow {
		recent = recent[1:]
	}
	c.users[userID] = recent
}

func (c *capacityTracker) isShort(userID string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	recent := c.users[userID]
	if len(recent) < capacityMinSamples {
		return false
	}
	failed := 0
	for _, ok := range recent {
		if !ok {
			failed++
		}
	}
	return float64(failed)/float64(len(recent)) >= shortOfCapacity
}

// queueWaitMs is how long a request may wait for a model to free up. A
// high-priority key still waits in full: lower priorities leave what it waits
// for alone.
func (c *capacityTracker) queueWaitMs(key RouterKey, configured int64) int64 {
	if c.isShort(key.UserID) && key.Priority != "high" {
		if configured > ShortWaitMs {
			return ShortWaitMs
		}
	}
	return configured
}

// RecordAnswered notes how a request ended, for the short-of-capacity
// estimate.
func (s *Service) RecordAnswered(userID string, gotAnAnswer bool) {
	s.capacity.record(userID, gotAnAnswer)
}

// ShortOfCapacity reports whether the user's recent requests mostly failed to
// get an answer.
func (s *Service) ShortOfCapacity(userID string) bool { return s.capacity.isShort(userID) }

// QueueWaitMs is the configured wait, shortened while the user is short of
// capacity.
func (s *Service) QueueWaitMs(key RouterKey, configured int64) int64 {
	return s.capacity.queueWaitMs(key, configured)
}
