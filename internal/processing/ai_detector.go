package processing

import (
	"container/list"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// Optional second opinion for the privacy shield: the privacy filter running on
// this server lists names, addresses and account numbers that patterns cannot
// catch. It only reads texts it has not seen yet, so long conversations do not pay
// for their history again, and any failure simply falls back to the pattern
// detectors.

const (
	// entityCacheLimit is how many texts' entities are remembered.
	entityCacheLimit = 1000
	// entityMinText skips texts too short to hold a name.
	entityMinText = 12
	// entityDeadline is generous: the first request after idling loads the model.
	entityDeadline = 30 * time.Second
)

// EntityDetector runs the privacy filter over a request's user and tool messages.
type EntityDetector struct {
	Model *PrivacyModel
	Log   *slog.Logger
	// Timeout overrides the deadline (tests).
	Timeout time.Duration

	mu    sync.Mutex
	Cache map[string][]Entity
	order *list.List
}

func (d *EntityDetector) log() *slog.Logger {
	if d.Log != nil {
		return d.Log
	}
	return slog.Default()
}

func (d *EntityDetector) deadline() time.Duration {
	if d.Timeout > 0 {
		return d.Timeout
	}
	return entityDeadline
}

// DetectEntities returns, per message index, the entities the privacy filter found.
// Text it has seen before comes from the cache.
func (d *EntityDetector) DetectEntities(ctx context.Context, canonical Canonical) func(index int) []Entity {
	deadline := time.Now().Add(d.deadline())
	results := map[int][]Entity{}
	var mu sync.Mutex
	var jobs sync.WaitGroup
	messages := canonical.Messages()
	for index, message := range messages {
		role := messageRole(message)
		if role == "system" || role == "assistant" {
			continue
		}
		text := entityText(message)
		if len([]rune(strings.TrimSpace(text))) < entityMinText {
			continue
		}
		key := cacheKeyOf(text)
		if cached, ok := d.cached(key); ok {
			results[index] = cached
			continue
		}
		jobs.Add(1)
		go func(index int, text, key string) {
			defer jobs.Done()
			items, err := d.Model.Detect(ctx, text)
			if err != nil {
				d.log().Warn("AI detection failed", "error", err)
				mu.Lock()
				results[index] = []Entity{}
				mu.Unlock()
				return
			}
			if items != nil {
				d.remember(key, items)
			} else {
				items = []Entity{}
			}
			mu.Lock()
			results[index] = items
			mu.Unlock()
		}(index, text, key)
	}
	done := make(chan struct{})
	go func() {
		jobs.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Until(deadline) + 50*time.Millisecond):
	case <-ctx.Done():
	}
	return func(index int) []Entity {
		mu.Lock()
		defer mu.Unlock()
		if items, ok := results[index]; ok {
			return items
		}
		return []Entity{}
	}
}

// entityText is the text the filter reads for one message.
func entityText(message map[string]any) string {
	switch content := message["content"].(type) {
	case string:
		return content
	case []any:
		parts := []string{}
		for _, raw := range content {
			part, ok := raw.(map[string]any)
			if !ok || part["type"] != "text" {
				continue
			}
			text, _ := part["text"].(string)
			parts = append(parts, text)
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

func cacheKeyOf(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

func (d *EntityDetector) cached(key string) ([]Entity, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.Cache == nil {
		return nil, false
	}
	items, ok := d.Cache[key]
	return items, ok
}

// remember stores one text's entities, forgetting the oldest past the limit.
func (d *EntityDetector) remember(key string, items []Entity) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.Cache == nil {
		d.Cache = map[string][]Entity{}
	}
	if d.order == nil {
		d.order = list.New()
	}
	if _, exists := d.Cache[key]; !exists {
		d.order.PushBack(key)
	}
	d.Cache[key] = items
	for len(d.Cache) > entityCacheLimit {
		oldest := d.order.Front()
		if oldest == nil {
			break
		}
		d.order.Remove(oldest)
		delete(d.Cache, oldest.Value.(string))
	}
}

// ClearCache forgets every remembered text (the tests start empty).
func (d *EntityDetector) ClearCache() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.Cache = map[string][]Entity{}
	d.order = list.New()
}
