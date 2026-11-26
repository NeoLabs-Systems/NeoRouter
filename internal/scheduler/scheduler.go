// Package scheduler runs NeoRouter's background workers: key validation,
// billing checks, model sync, metadata refresh, quota and plan probes, update
// checks, log pruning, proxy health and pool sync. It is a faithful port of
// server/workers/scheduler.js: jittered interval chains (0.8x-1.2x), one run at
// a time per task, failures logged and survived, and a quota flush every ten
// seconds even when workers are disabled.
package scheduler

import (
	"context"
	"log/slog"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"github.com/NeoLabs-Systems/neorouter/internal/config"
)

const (
	quotaFlushInterval = 10 * time.Second
	planProbeInterval  = 5 * time.Minute
	billingInterval    = 6 * time.Hour
	updateInterval     = 6 * time.Hour
	pruneInterval      = 24 * time.Hour
	poolSyncInterval   = 10 * time.Minute
)

// Service owns the timers. Every task is a func field the wiring satisfies by
// closing over the owning domain (the JS forEachAccount loops live there).
type Service struct {
	Cfg func() config.Config
	Log *slog.Logger

	QuotaFlush      func()
	ValidateKeys    func(ctx context.Context) error
	VerifyBilling   func(ctx context.Context) error
	SyncModels      func(ctx context.Context) error
	RefreshMetadata func(ctx context.Context) error
	ProbeQuotas     func(ctx context.Context) error
	ProbePlans      func(ctx context.Context) error
	CheckUpdates    func(ctx context.Context) error
	PruneLogs       func(ctx context.Context) error
	CheckProxies    func(ctx context.Context) error
	SyncPools       func(ctx context.Context) error

	// ProxyCheckInterval mirrors proxies.POLICY.checkEveryMs.
	ProxyCheckInterval func() time.Duration

	stopOnce sync.Once
	stopCh   chan struct{}
	wg       sync.WaitGroup
	started  atomic.Bool
}

func (s *Service) log() *slog.Logger {
	if s.Log != nil {
		return s.Log
	}
	return slog.Default()
}

func jitter(interval time.Duration) time.Duration {
	return time.Duration(float64(interval) * (0.8 + rand.Float64()*0.4))
}

// Start begins the workers. The quota flush always runs; the rest only when
// workers are enabled (NEOROUTER_WORKERS_ENABLED, off under NODE_ENV=test in JS).
func (s *Service) Start(ctx context.Context) {
	if !s.started.CompareAndSwap(false, true) {
		return
	}
	s.stopCh = make(chan struct{})
	cfg := s.Cfg()

	if s.QuotaFlush != nil {
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			ticker := time.NewTicker(quotaFlushInterval)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-s.stopCh:
					return
				case <-ticker.C:
					s.QuotaFlush()
				}
			}
		}()
	}
	if !cfg.WorkersEnabled {
		return
	}

	s.every(ctx, "validate-keys", cfg.Sync.ValidateInterval, s.ValidateKeys, false)
	s.every(ctx, "verify-billing", billingInterval, s.VerifyBilling, true)
	s.every(ctx, "sync-models", cfg.Sync.ModelSyncInterval, s.SyncModels, false)
	if cfg.MetadataSourcesEnabled {
		s.every(ctx, "refresh-metadata", cfg.Sync.MetadataInterval, s.RefreshMetadata, true)
	}
	s.every(ctx, "probe-quotas", cfg.Sync.QuotaProbeInterval, s.ProbeQuotas, false)
	// Plan allowances move with every request, so plans are checked more often.
	s.every(ctx, "probe-plans", planProbeInterval, s.ProbePlans, false)
	s.every(ctx, "check-updates", updateInterval, s.CheckUpdates, true)
	s.every(ctx, "prune-logs", pruneInterval, s.PruneLogs, false)
	proxyInterval := 15 * time.Minute
	if s.ProxyCheckInterval != nil {
		proxyInterval = s.ProxyCheckInterval()
	}
	s.every(ctx, "check-proxies", proxyInterval, s.CheckProxies, true)
	s.every(ctx, "sync-pools", poolSyncInterval, s.SyncPools, true)
}

// every runs task on a jittered chain, skipping a tick while the previous run
// is still going, logging failures without stopping the chain.
func (s *Service) every(ctx context.Context, name string, interval time.Duration, task func(context.Context) error, runAtStart bool) {
	if task == nil || interval <= 0 {
		return
	}
	var running atomic.Bool
	run := func() {
		if !running.CompareAndSwap(false, true) {
			return
		}
		defer running.Store(false)
		if err := task(ctx); err != nil {
			s.log().Warn("Scheduled task failed", "task", name, "error", err)
		}
	}
	if runAtStart {
		go run()
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		for {
			timer := time.NewTimer(jitter(interval))
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-s.stopCh:
				timer.Stop()
				return
			case <-timer.C:
			}
			run()
		}
	}()
}

// Stop ends every chain and flushes quota one last time, as scheduler.stop() did.
func (s *Service) Stop() {
	s.stopOnce.Do(func() {
		if s.stopCh != nil {
			close(s.stopCh)
		}
	})
	s.wg.Wait()
	if s.QuotaFlush != nil {
		s.QuotaFlush()
	}
}
