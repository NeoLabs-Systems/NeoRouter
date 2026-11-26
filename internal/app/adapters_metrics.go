package app

// The routing numbers the models API adds to each row: reliability, speed and
// price scores plus the availability reason. The scores need the routing entry,
// and the catalog hands the adapter only a ref, so the entries the catalog just
// listed are kept for the duration of the request.

import (
	"context"
	"sync"
	"time"

	"github.com/NeoLabs-Systems/neorouter/internal/catalog"
	"github.com/NeoLabs-Systems/neorouter/internal/routing"
)

// refCache is the most recently listed catalog entries by ref.
type refCache struct {
	mu      sync.RWMutex
	entries map[int64]catalog.Entry
}

func newRefCache() *refCache {
	return &refCache{entries: map[int64]catalog.Entry{}}
}

func (c *refCache) put(entry catalog.Entry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[entry.Ref] = entry
}

func (c *refCache) get(ref int64) (catalog.Entry, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	entry, ok := c.entries[ref]
	return entry, ok
}

// catalogMetrics is the routing domain's scores in the shape the models routes
// read them.
type catalogMetrics struct {
	svc   *routing.Service
	cache *refCache
	now   func() time.Time
}

func (m catalogMetrics) ModelMetrics(ctx context.Context, ref int64) catalog.ModelMetrics {
	now := time.Now
	if m.now != nil {
		now = m.now
	}
	at := now()
	row := m.svc.ModelStats(ref, at)
	availability := m.svc.ModelAvailability(ref, at)
	// stats_store.availability's exact JSON: {available:false, reason, until}
	// while cooling down or open, {available:true, halfOpen} otherwise.
	var availabilityJSON any
	if availability.Available {
		availabilityJSON = map[string]any{"available": true, "halfOpen": availability.HalfOpen}
	} else {
		availabilityJSON = map[string]any{"available": false, "reason": availability.Reason, "until": availability.Until}
	}

	out := catalog.ModelMetrics{
		Reliability:  routing.Reliability(row, false, nil),
		Availability: availabilityJSON,
		Penalty:      row.Penalty,
		Successes:    row.Successes,
		Failures:     row.Failures,
	}
	entry := &routing.Entry{Ref: ref}
	if cached, ok := m.cache.get(ref); ok {
		entry = routingEntryOf(cached)
	}
	out.SpeedScore = routing.SpeedOf(entry, row)
	out.CostScore = m.svc.CostScore(entry, at)
	return out
}

// ClearCooldown tries a model again right away and closes its breaker.
func (m catalogMetrics) ClearCooldown(_ context.Context, ref int64) {
	m.svc.ClearCooldown(ref, time.Now())
}
