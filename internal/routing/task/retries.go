package task

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"sync"
	"time"
)

// A signal about quality that needs nobody to rate anything: a client sending
// the very same conversation again right after getting an answer did not like
// that answer. Only a hash of the conversation is kept, in memory, for minutes.
const (
	withinMs      = 3 * 60 * 1000
	maxRemembered = 5000
)

// Answer is the answer a repeated request repeats: the model and its task.
type Answer struct {
	Ref  int64
	Task string
	At   int64
}

// RetryTracker remembers the answers requests got, by conversation fingerprint.
type RetryTracker struct {
	mu      sync.Mutex
	answers map[string]Answer
	order   []string
}

// NewRetryTracker builds the tracker.
func NewRetryTracker() *RetryTracker {
	return &RetryTracker{answers: map[string]Answer{}}
}

// Fingerprint is a hash of the key and the conversation (sha256, base64url
// without padding).
func Fingerprint(keyID string, messages []Message) string {
	encoded, err := json.Marshal(messages)
	if err != nil {
		encoded = []byte("[]")
	}
	sum := sha256.Sum256(append(append([]byte(keyID), '|'), encoded...))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// RepeatedAnswer is the answer this request repeats, if any: once per answer.
func (t *RetryTracker) RepeatedAnswer(id string, now time.Time) *Answer {
	t.mu.Lock()
	defer t.mu.Unlock()
	earlier, ok := t.answers[id]
	if !ok {
		return nil
	}
	delete(t.answers, id)
	if now.UnixMilli()-earlier.At <= withinMs {
		return &earlier
	}
	return nil
}

// RememberAnswer keeps an answer for a few minutes.
func (t *RetryTracker) RememberAnswer(id string, answer Answer, now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.answers) >= maxRemembered {
		// The first key is the oldest (a Map keeps insertion order); keys
		// already answered are skipped.
		for len(t.order) > 0 {
			oldest := t.order[0]
			t.order = t.order[1:]
			if _, ok := t.answers[oldest]; ok {
				delete(t.answers, oldest)
				break
			}
		}
	}
	answer.At = now.UnixMilli()
	if _, ok := t.answers[id]; !ok {
		t.order = append(t.order, id)
	}
	t.answers[id] = answer
}

// ResetForTests forgets every remembered answer.
func (t *RetryTracker) ResetForTests() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.answers = map[string]Answer{}
	t.order = nil
}
