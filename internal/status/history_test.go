package status

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// TestHistoryKeepsWorstPerHour: an hour keeps its worst observation, gaps stay
// unknown, and old hours are dropped.
func TestHistoryKeepsWorstPerHour(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 1, 3, 12, 15, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	store := NewHistory(dir)
	store.Now = clock

	for _, sample := range store.History() {
		if sample.Status != nil {
			t.Fatalf("fresh history has status %v", *sample.Status)
		}
	}
	store.Record("operational")
	store.Record("degraded")
	store.Record("operational")
	now = now.Add(2 * time.Hour)
	store.Record("operational")

	reopened := NewHistory(dir)
	reopened.Now = clock
	history := reopened.History()
	if len(history) != 48 {
		t.Fatalf("history length = %d, want 48", len(history))
	}
	last := history[len(history)-3:]
	want := []string{"degraded", "", "operational"}
	for i, entry := range last {
		got := ""
		if entry.Status != nil {
			got = *entry.Status
		}
		if got != want[i] {
			t.Fatalf("entry %d = %q, want %q", i, got, want[i])
		}
	}

	now = now.Add(49 * time.Hour)
	store.Record("operational")
	raw, err := os.ReadFile(filepath.Join(dir, HistoryFileName))
	if err != nil {
		t.Fatalf("read history: %v", err)
	}
	var stored []entry
	if err := json.Unmarshal(raw, &stored); err != nil {
		t.Fatalf("history %s: %v", raw, err)
	}
	if len(stored) != 1 {
		t.Fatalf("stored entries = %d, want 1", len(stored))
	}
	if fileInfo, err := os.Stat(filepath.Join(dir, HistoryFileName)); err == nil {
		if mode := fileInfo.Mode().Perm(); mode != 0o600 {
			t.Fatalf("history mode = %v, want 0600", mode)
		}
	}
}

// TestHistoryUnknownStatusIgnored keeps record() to the three reported states.
func TestHistoryUnknownStatusIgnored(t *testing.T) {
	store := NewHistory(t.TempDir())
	store.Now = func() time.Time { return time.Date(2026, 1, 3, 12, 15, 0, 0, time.UTC) }
	store.Record("maintenance")
	if _, err := os.Stat(filepath.Join(store.Directory, HistoryFileName)); !os.IsNotExist(err) {
		t.Fatalf("an unknown status must not be stored (err = %v)", err)
	}
}

// TestHistoryUnwritableDoesNotInterrupt: an unwritable history never breaks
// sampling.
func TestHistoryUnwritableDoesNotInterrupt(t *testing.T) {
	store := NewHistory("/dev/null/status-history")
	store.Record("operational")
	for _, sample := range store.History() {
		if sample.Status != nil {
			t.Fatalf("unwritable history has status %v", *sample.Status)
		}
	}
}

// TestHistorySampleTimesAreHourly pins the ISO shape and the hour alignment the
// status page renders.
func TestHistorySampleTimesAreHourly(t *testing.T) {
	store := NewHistory(t.TempDir())
	store.Now = func() time.Time { return time.Date(2026, 1, 3, 12, 59, 59, 0, time.UTC) }
	store.Record("degraded")
	history := store.History()
	if history[47].Time != "2026-01-03T12:00:00.000Z" {
		t.Fatalf("last sample = %q", history[47].Time)
	}
	if history[0].Time != "2026-01-01T13:00:00.000Z" {
		t.Fatalf("first sample = %q", history[0].Time)
	}
}

// TestStartSamplesAndStops drives the sampler: it records what the sample
// function returns, an error leaves the hour alone, and a cancelled context
// stops it.
func TestStartSamplesAndStops(t *testing.T) {
	store := NewHistory(t.TempDir())
	store.Now = func() time.Time { return time.Date(2026, 1, 3, 12, 15, 0, 0, time.UTC) }
	store.SampleAfter = 5 * time.Millisecond
	store.Interval = 5 * time.Millisecond

	var calls atomic.Int32
	ctx, cancel := context.WithCancel(context.Background())
	store.Start(ctx, func(context.Context) (string, error) {
		calls.Add(1)
		return "degraded", nil
	})
	deadline := time.Now().Add(2 * time.Second)
	for calls.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	cancel()
	if calls.Load() < 2 {
		t.Fatalf("sampler calls = %d, want at least 2", calls.Load())
	}
	if got := store.History()[47]; got.Status == nil || *got.Status != "degraded" {
		t.Fatalf("history = %+v", got)
	}
	// A sample already under way when cancel ran may still finish.
	time.Sleep(30 * time.Millisecond)
	stopped := calls.Load()
	time.Sleep(30 * time.Millisecond)
	if got := calls.Load(); got != stopped {
		t.Fatalf("sampler kept running after cancel: %d -> %d", stopped, got)
	}

	// A failing sample leaves the hour as it was.
	store.collect(context.Background(), func(context.Context) (string, error) { return "", errNoCatalog })
	if got := store.History()[47]; got.Status == nil || *got.Status != "degraded" {
		t.Fatalf("a failed sample must leave the hour alone: %+v", got)
	}
}
