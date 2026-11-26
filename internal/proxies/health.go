package proxies

// Proxy health: the in-memory cooldown, latency and probe state of pool.js's
// `health` Map, the probe and the automatic check that rests the dead.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
)

// health is the in-memory state of one proxy, keyed by row id like pool.js's
// `health` Map. It is lost on restart, as in JS.
type health struct {
	failures  int
	until     int64 // epoch ms; the proxy rests until then
	latency   *float64
	active    int
	served    int
	checkedAt *int64
	checkOk   *bool
}

// proxyRow is one outbound_proxies row.
type proxyRow struct {
	ID              string
	Endpoint        string
	SecretEncrypted string
}

// Probe checks one proxy against url through that proxy only. A missing proxy is
// a 404; any failure is reported as {ok:false} and rests the proxy.
func (s *Service) Probe(ctx context.Context, id, rawURL string) (ProbeResult, error) {
	row, err := s.rowByID(ctx, id)
	if err != nil {
		return ProbeResult{}, err
	}
	if row == nil {
		return ProbeResult{}, httpserver.New(http.StatusNotFound, "PROXY_NOT_FOUND", "Proxy not found.")
	}

	var (
		began   = time.Now()
		status  int
		failErr error
	)
	dispatcher, err := s.dispatcherFor(*row)
	if err != nil {
		failErr = err
	} else {
		client := &http.Client{
			Transport: dispatcher.transport,
			Timeout:   s.pol.connectDuration(),
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
		req, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if reqErr != nil {
			failErr = reqErr
		} else if resp, doErr := client.Do(req); doErr != nil {
			failErr = doErr
		} else {
			_ = resp.Body.Close()
			status = resp.StatusCode
			if status == http.StatusProxyAuthRequired {
				failErr = errors.New("Proxy authentication failed")
			}
		}
	}
	now := time.Now().UnixMilli()
	if failErr != nil {
		s.failProxy(id)
		s.markChecked(id, now, false)
		return ProbeResult{OK: false}, nil
	}
	latency := float64(time.Since(began).Microseconds()) / 1000
	s.probeSucceeded(id, latency, now)
	return ProbeResult{OK: true, Status: status, LatencyMs: &latency}, nil
}

// CheckAll probes every enabled proxy, a few at a time, and records the result.
func (s *Service) CheckAll(ctx context.Context, rawURL string) (CheckResult, error) {
	if rawURL == "" {
		rawURL = s.pol.CheckURL
	}
	rows, err := s.DB.R.QueryContext(ctx, `SELECT id FROM outbound_proxies WHERE enabled = 1`)
	if err != nil {
		return CheckResult{}, fmt.Errorf("proxies: check all: %w", err)
	}
	defer func() { _ = rows.Close() }()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return CheckResult{}, fmt.Errorf("proxies: check all: scan: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return CheckResult{}, fmt.Errorf("proxies: check all: rows: %w", err)
	}

	var next, ok int64
	workers := s.pol.CheckConcurrency
	if workers > len(ids) {
		workers = len(ids)
	}
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				index := int(atomic.AddInt64(&next, 1)) - 1
				if index >= len(ids) {
					return
				}
				if result, err := s.Probe(ctx, ids[index], rawURL); err == nil && result.OK {
					atomic.AddInt64(&ok, 1)
				}
			}
		}()
	}
	wg.Wait()

	result := CheckResult{At: time.Now().UnixMilli(), Checked: len(ids), OK: int(ok)}
	s.mu.Lock()
	s.lastCheck = &result
	s.mu.Unlock()
	return result, nil
}

func (s *Service) rowByID(ctx context.Context, id string) (*proxyRow, error) {
	var row proxyRow
	err := s.DB.R.QueryRowContext(ctx,
		`SELECT id, endpoint, secret_encrypted FROM outbound_proxies WHERE id = ?`, id).
		Scan(&row.ID, &row.Endpoint, &row.SecretEncrypted)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("proxies: read proxy: %w", err)
	}
	return &row, nil
}

// --- health bookkeeping -------------------------------------------------

func (s *Service) stateLocked(id string) *health {
	h, ok := s.health[id]
	if !ok {
		h = &health{}
		s.health[id] = h
	}
	return h
}

func (s *Service) beginAttempt(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	h := s.stateLocked(id)
	h.active++
	h.served++
}

func (s *Service) endAttempt(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if h, ok := s.health[id]; ok && h.active > 0 {
		h.active--
	}
}

func (s *Service) succeedProxy(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	h := s.stateLocked(id)
	h.failures = 0
	h.until = 0
}

// failProxy rests a proxy for an exponentially growing cooldown and drops its
// retained transport, mirroring pool.failed.
func (s *Service) failProxy(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	h := s.stateLocked(id)
	h.failures++
	exponent := h.failures - 1
	if exponent > 8 {
		exponent = 8
	}
	rest := int64(s.pol.RestMs) << exponent
	if rest > int64(s.pol.MaxRestMs) || rest < 0 {
		rest = int64(s.pol.MaxRestMs)
	}
	h.until = time.Now().UnixMilli() + rest
	s.disposeLocked(id)
}

func (s *Service) probeSucceeded(id string, latency float64, now int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	h := s.stateLocked(id)
	h.latency = &latency
	h.failures = 0
	h.until = 0
	h.checkedAt = &now
	checkOk := true
	h.checkOk = &checkOk
}

func (s *Service) markChecked(id string, now int64, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	h := s.stateLocked(id)
	h.checkedAt = &now
	h.checkOk = &ok
}

func (s *Service) recordLatency(id string, ms float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	h, ok := s.health[id]
	if !ok {
		return
	}
	if h.latency == nil {
		h.latency = &ms
		return
	}
	smoothed := *h.latency*s.pol.Smoothing + ms*(1-s.pol.Smoothing)
	h.latency = &smoothed
}

// lease keeps the proxy's active count until the response body is done: the
// whole stream, including SSE, holds its slot (server/services/proxies/stream.js).
func (s *Service) lease(ctx context.Context, id string, resp *http.Response) *http.Response {
	if resp.Body == nil {
		s.endAttempt(id)
		return resp
	}
	resp.Body = &leasedBody{
		rc:      resp.Body,
		release: func() { s.endAttempt(id) },
		failed: func() {
			if ctx.Err() == nil {
				s.failProxy(id)
			}
		},
	}
	return resp
}

// candidates returns the proxies still eligible for this request with their
// selection weights: busy, slow and failing proxies lose weight, a resting one
// is skipped entirely.
