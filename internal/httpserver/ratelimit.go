package httpserver

import (
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/httpx"
)

// SlidingWindowOptions configures a SlidingWindow rate limiter.
type SlidingWindowOptions struct {
	Window time.Duration
	Limit  int
	// Key identifies the caller; nil means the client IP.
	Key  func(*http.Request) string
	Code string // defaults to RATE_LIMITED
	Log  *slog.Logger
	Now  func() time.Time // for tests; defaults to time.Now
}

// SlidingWindow is the port of server/middleware/rate_limit.js: a per-key
// sliding window that rejects with 429 once Limit requests happened inside
// Window. A Limit of 0 disables the operation with 429 "This operation is
// disabled.", exactly like the JS middleware.
type SlidingWindow struct {
	window time.Duration
	limit  int
	key    func(*http.Request) string
	code   string
	log    *slog.Logger
	now    func() time.Time

	mu      sync.Mutex
	entries map[string][]time.Time
}

// NewSlidingWindow builds a limiter. It keeps at most 10k keys; expired ones
// are pruned when that bound is crossed, as the JS middleware does.
func NewSlidingWindow(opts SlidingWindowOptions) *SlidingWindow {
	if opts.Code == "" {
		opts.Code = "RATE_LIMITED"
	}
	if opts.Key == nil {
		opts.Key = func(r *http.Request) string { return httpx.ClientIP(r, false) }
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &SlidingWindow{
		window:  opts.Window,
		limit:   opts.Limit,
		key:     opts.Key,
		code:    opts.Code,
		log:     opts.Log,
		now:     opts.Now,
		entries: make(map[string][]time.Time),
	}
}

// Clear forgets every recorded request, like the JS middleware's clear().
func (l *SlidingWindow) Clear() {
	l.mu.Lock()
	l.entries = make(map[string][]time.Time)
	l.mu.Unlock()
}

// Middleware wraps next with the limiter. A rejected request is answered here
// with the management error envelope and a Retry-After header.
func (l *SlidingWindow) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !l.Allow(w, r) {
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Allow records one request for the caller's key and reports whether it may
// proceed. When it may not, the 429 response has already been written.
func (l *SlidingWindow) Allow(w http.ResponseWriter, r *http.Request) bool {
	if l.limit == 0 {
		WriteError(w, r, l.log, New(http.StatusTooManyRequests, l.code, "This operation is disabled."))
		return false
	}
	now := l.now()
	cutoff := now.Add(-l.window)
	id := l.key(r)

	l.mu.Lock()
	defer l.mu.Unlock()
	recent := make([]time.Time, 0, len(l.entries[id])+1)
	for _, ts := range l.entries[id] {
		if ts.After(cutoff) {
			recent = append(recent, ts)
		}
	}
	if len(recent) >= l.limit {
		retryAfter := int((recent[0].Add(l.window).Sub(now) + time.Second - 1) / time.Second)
		if retryAfter < 1 {
			retryAfter = 1
		}
		l.entries[id] = recent
		herr := New(http.StatusTooManyRequests, l.code, "Too many requests. Please try again later.", map[string]int{"retryAfterSeconds": retryAfter})
		herr.RetryAfter = retryAfter
		WriteError(w, r, l.log, herr)
		return false
	}
	recent = append(recent, now)
	l.entries[id] = recent
	if len(l.entries) > 10_000 {
		for entryKey, timestamps := range l.entries {
			live := false
			for _, ts := range timestamps {
				if ts.After(cutoff) {
					live = true
					break
				}
			}
			if !live {
				delete(l.entries, entryKey)
			}
		}
	}
	return true
}
