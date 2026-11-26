package status

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"time"
)

const (
	// HistoryHours is how many hourly samples the status page shows.
	HistoryHours = 48
	// HistoryFileName is the file the samples are kept in, inside the data dir.
	HistoryFileName = "public-status-history.json"
	// SampleInterval is how often a running server records a sample.
	SampleInterval = 5 * time.Minute
	// firstSampleDelay is how long the server waits before its first sample.
	firstSampleDelay = 15 * time.Second

	hourMillis = int64(time.Hour / time.Millisecond)
)

// severity ranks the three reported states so an hour keeps its worst
// observation, like server/status_history.js.
var severity = map[string]int{"operational": 0, "degraded": 1, "unavailable": 2}

// KnownStatus reports whether the status page records this status.
func KnownStatus(s string) bool {
	_, ok := severity[s]
	return ok
}

// Sample is one hour of the history: Time is the whole hour in UTC and Status is
// nil for an hour without an observation.
type Sample struct {
	Time   string  `json:"time"`
	Status *string `json:"status"`
}

// entry is one stored observation: the hour it belongs to, in epoch
// milliseconds, as status_history.js stores it.
type entry struct {
	Time   float64 `json:"time"`
	Status string  `json:"status"`
}

// History is the file-backed ring of hourly status samples behind the public
// status page, the Go port of server/status_history.js. It is safe to share
// between the request path and the sampler.
type History struct {
	// Directory holds the history file; empty means the file is never written.
	Directory string
	// Now overrides the clock in tests.
	Now func() time.Time
	// SampleAfter delays the first sample (default firstSampleDelay).
	SampleAfter time.Duration
	// Interval is the sampling cadence (default SampleInterval).
	Interval time.Duration
}

// NewHistory builds the history kept in dir.
func NewHistory(dir string) *History { return &History{Directory: dir} }

// Path is the history file this History reads and writes.
func (h *History) Path() string {
	if h.Directory == "" {
		return ""
	}
	return filepath.Join(h.Directory, HistoryFileName)
}

func (h *History) now() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now()
}

// hourStart floors t to the hour, in epoch milliseconds.
func (h *History) hourStart(t time.Time) int64 {
	return t.UnixMilli() / hourMillis * hourMillis
}

// History returns the last 48 hours, one entry per hour, oldest first. An hour
// without an observation has a null status.
func (h *History) History() []Sample {
	current := h.hourStart(h.now())
	byHour := make(map[float64]string, HistoryHours)
	for _, e := range h.read() {
		byHour[e.Time] = e.Status
	}
	out := make([]Sample, 0, HistoryHours)
	for i := range HistoryHours {
		at := current - int64(HistoryHours-1-i)*hourMillis
		sample := Sample{Time: isoMillis(time.UnixMilli(at))}
		if st, ok := byHour[float64(at)]; ok {
			sample.Status = &st
		}
		out = append(out, sample)
	}
	return out
}

// Record stores one observation for the current hour, keeping the worst status
// that hour has seen. An unknown status is ignored and a write failure is
// swallowed: sampling must never disrupt the server.
func (h *History) Record(status string) {
	if !KnownStatus(status) {
		return
	}
	current := h.hourStart(h.now())
	entries := h.read()
	kept := make([]entry, 0, len(entries)+1)
	for _, e := range entries {
		if e.Time > float64(current-HistoryHours*hourMillis) && e.Time <= float64(current) {
			kept = append(kept, e)
		}
	}
	found := false
	for i := range kept {
		if kept[i].Time == float64(current) {
			found = true
			if severity[status] > severity[kept[i].Status] {
				kept[i].Status = status
			}
		}
	}
	if !found {
		kept = append(kept, entry{Time: float64(current), Status: status})
	}
	_ = h.write(kept)
}

// Start samples the server every Interval after an initial delay, until ctx is
// cancelled. A failing sample leaves that hour unknown.
func (h *History) Start(ctx context.Context, sample func(context.Context) (string, error)) {
	if sample == nil {
		return
	}
	after := h.SampleAfter
	if after <= 0 {
		after = firstSampleDelay
	}
	interval := h.Interval
	if interval <= 0 {
		interval = SampleInterval
	}
	go func() {
		timer := time.NewTimer(after)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		h.collect(ctx, sample)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				h.collect(ctx, sample)
			}
		}
	}()
}

// collect records one sample; a sample that fails, or a sample function that
// panics, leaves the hour unknown.
func (h *History) collect(ctx context.Context, sample func(context.Context) (string, error)) {
	defer func() { _ = recover() }()
	if s, err := sample(ctx); err == nil {
		h.Record(s)
	}
}

// read returns the stored observations, dropping anything a hand-edited file may
// hold that the page cannot show.
func (h *History) read() []entry {
	path := h.Path()
	if path == "" {
		return nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var value []entry
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil
	}
	out := make([]entry, 0, len(value))
	for _, e := range value {
		if math.IsNaN(e.Time) || math.IsInf(e.Time, 0) || !KnownStatus(e.Status) {
			continue
		}
		out = append(out, e)
	}
	return out
}

// write replaces the file atomically through a temporary file in the same
// directory, mode 0600.
func (h *History) write(entries []entry) error {
	path := h.Path()
	if path == "" {
		return fmt.Errorf("status: write history: no directory")
	}
	if err := os.MkdirAll(h.Directory, 0o700); err != nil {
		return fmt.Errorf("status: create history directory: %w", err)
	}
	if entries == nil {
		entries = []entry{}
	}
	body, err := json.Marshal(entries)
	if err != nil {
		return fmt.Errorf("status: encode history: %w", err)
	}
	tmp := fmt.Sprintf("%s.%d.tmp", path, os.Getpid())
	if err := os.WriteFile(tmp, body, 0o600); err != nil {
		return fmt.Errorf("status: write history: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("status: replace history: %w", err)
	}
	return nil
}
