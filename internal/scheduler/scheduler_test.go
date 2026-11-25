package scheduler

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/NeoLabs-Systems/neorouter/internal/config"
)

func testConfig(workers bool) config.Config {
	return config.Config{
		WorkersEnabled:         workers,
		MetadataSourcesEnabled: workers,
		Sync: config.Sync{
			ValidateInterval:   15 * time.Millisecond,
			ModelSyncInterval:  15 * time.Millisecond,
			MetadataInterval:   15 * time.Millisecond,
			QuotaProbeInterval: 15 * time.Millisecond,
		},
	}
}

func quietLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestWorkersRunRepeatedlyAndStop(t *testing.T) {
	var runs atomic.Int64
	s := &Service{
		Cfg:          func() config.Config { return testConfig(true) },
		Log:          quietLog(),
		ValidateKeys: func(context.Context) error { runs.Add(1); return nil },
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx)
	time.Sleep(120 * time.Millisecond)
	s.Stop()
	first := runs.Load()
	if first < 2 {
		t.Fatalf("validate-keys ran %d times in 120ms at 15ms interval", first)
	}
	time.Sleep(60 * time.Millisecond)
	if runs.Load() != first {
		t.Fatal("worker kept running after Stop")
	}
}

func TestRunAtStartTasksFireImmediately(t *testing.T) {
	var billing, updates, pools atomic.Int64
	s := &Service{
		Cfg:           func() config.Config { return testConfig(true) },
		Log:           quietLog(),
		VerifyBilling: func(context.Context) error { billing.Add(1); return nil },
		CheckUpdates:  func(context.Context) error { updates.Add(1); return nil },
		SyncPools:     func(context.Context) error { pools.Add(1); return nil },
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx)
	time.Sleep(30 * time.Millisecond)
	s.Stop()
	if billing.Load() == 0 || updates.Load() == 0 || pools.Load() == 0 {
		t.Fatalf("runAtStart tasks did not fire: billing=%d updates=%d pools=%d", billing.Load(), updates.Load(), pools.Load())
	}
}

func TestQuotaFlushRunsEvenWithWorkersDisabled(t *testing.T) {
	var flushes atomic.Int64
	var validate atomic.Int64
	s := &Service{
		Cfg:          func() config.Config { return testConfig(false) },
		Log:          quietLog(),
		QuotaFlush:   func() { flushes.Add(1) },
		ValidateKeys: func(context.Context) error { validate.Add(1); return nil },
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx)
	time.Sleep(30 * time.Millisecond)
	s.Stop() // Stop flushes once more, as scheduler.stop() did.
	if flushes.Load() == 0 {
		t.Fatal("quota never flushed")
	}
	if validate.Load() != 0 {
		t.Fatal("workers ran while disabled")
	}
}

func TestFailuresAreSurvivedAndOverlapsSkipped(t *testing.T) {
	var runs atomic.Int64
	s := &Service{
		Cfg: func() config.Config { return testConfig(true) },
		Log: quietLog(),
		ValidateKeys: func(context.Context) error {
			runs.Add(1)
			time.Sleep(40 * time.Millisecond) // longer than the interval
			return errors.New("boom")
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx)
	time.Sleep(150 * time.Millisecond)
	s.Stop()
	got := runs.Load()
	if got < 2 {
		t.Fatalf("chain died after the first failure: %d runs", got)
	}
	if got > 6 {
		t.Fatalf("overlapping runs were not skipped: %d runs in 150ms with a 40ms task", got)
	}
}

func TestMetadataRefreshRespectsTheSourceSwitch(t *testing.T) {
	var refresh atomic.Int64
	cfg := testConfig(true)
	cfg.MetadataSourcesEnabled = false
	s := &Service{
		Cfg:             func() config.Config { return cfg },
		Log:             quietLog(),
		RefreshMetadata: func(context.Context) error { refresh.Add(1); return nil },
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx)
	time.Sleep(40 * time.Millisecond)
	s.Stop()
	if refresh.Load() != 0 {
		t.Fatal("metadata refreshed while sources are disabled")
	}
}
