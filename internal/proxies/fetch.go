package proxies

// Proxy selection and failover: which proxy a request goes through, how the
// weights are drawn, and how a failure is classified as safe to retry. Fetching
// through the proxy pool.

import (
	"context"
	"fmt"
	"math/rand"
	"net/http"
	"time"
)

// Fetch sends req through the rotation, or directly when the policy is off or no
// healthy proxy is left and the policy allows a direct fallback. The caller owns
// the returned response and must close its body, which releases the lease.
func (s *Service) Fetch(ctx context.Context, req *http.Request) (*http.Response, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	policy, err := s.PoolPolicy(ctx)
	if err != nil {
		return nil, err
	}
	if !policy.Enabled {
		return s.Direct.Do(req)
	}
	rows, err := s.enabledRows(ctx)
	if err != nil {
		return nil, err
	}

	tried := make(map[string]bool, len(rows))
	consumed := false
	for attempt := 0; attempt < s.pol.MaxAttempts; attempt++ {
		candidates, weights := s.candidates(rows, tried)
		if len(candidates) == 0 {
			break
		}
		row := pickCandidate(candidates, weights)
		tried[row.ID] = true
		s.beginAttempt(row.ID)

		var (
			leased     bool
			response   *http.Response
			attemptErr error
			stop       bool
		)
		func() {
			defer func() {
				if !leased {
					s.endAttempt(row.ID)
				}
			}()
			dispatcher, err := s.dispatcherFor(row)
			if err != nil {
				s.failProxy(row.ID)
				attemptErr = fmt.Errorf("proxies: %s: %w", row.Endpoint, err)
				stop = true
				return
			}
			outbound := requestForAttempt(req, ctx, attempt)
			if outbound == nil {
				// The body cannot be replayed, so the request cannot be retried.
				attemptErr = ErrTransportFailed
				stop = true
				return
			}
			consumed = true
			resp, err := dispatcher.client.Do(outbound)
			if err != nil {
				if ctxErr := ctx.Err(); ctxErr != nil {
					attemptErr = ctxErr
					stop = true
					return
				}
				s.failProxy(row.ID)
				// Only retry a mutation when the connection failed before the
				// request was sent; anything else may already have run.
				if !idempotent(req.Method) && !retryable(err) {
					attemptErr = fmt.Errorf("proxies: %s: %w", row.Endpoint, ErrTransportFailed)
					stop = true
				}
				return
			}
			s.succeedProxy(row.ID)
			response = s.lease(ctx, row.ID, resp)
			leased = true
		}()
		if stop {
			return nil, attemptErr
		}
		if response != nil {
			return response, nil
		}
	}
	if policy.FallbackDirect {
		outbound := req
		if consumed {
			outbound = requestForAttempt(req, ctx, 1)
			if outbound == nil {
				return nil, ErrTransportFailed
			}
		}
		return s.Direct.Do(outbound)
	}
	return nil, ErrNoHealthyProxy
}

func (s *Service) enabledRows(ctx context.Context) ([]proxyRow, error) {
	rows, err := s.DB.R.QueryContext(ctx,
		`SELECT id, endpoint, secret_encrypted FROM outbound_proxies WHERE enabled = 1`)
	if err != nil {
		return nil, fmt.Errorf("proxies: enabled proxies: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []proxyRow
	for rows.Next() {
		var row proxyRow
		if err := rows.Scan(&row.ID, &row.Endpoint, &row.SecretEncrypted); err != nil {
			return nil, fmt.Errorf("proxies: enabled proxies: scan: %w", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("proxies: enabled proxies: rows: %w", err)
	}
	return out, nil
}

func (s *Service) candidates(rows []proxyRow, tried map[string]bool) ([]proxyRow, []float64) {
	now := time.Now().UnixMilli()
	s.mu.Lock()
	defer s.mu.Unlock()
	var (
		candidates []proxyRow
		weights    []float64
	)
	for _, row := range rows {
		if tried[row.ID] {
			continue
		}
		h := s.stateLocked(row.ID)
		if h.until > now {
			continue
		}
		latency := float64(s.pol.UnmeasuredMs)
		if h.latency != nil {
			latency = *h.latency
		}
		if latency < 1 {
			latency = 1
		}
		candidates = append(candidates, row)
		weights = append(weights, 1/(float64(h.active+1)*latency*float64(h.failures+1)))
	}
	return candidates, weights
}

func pickCandidate(candidates []proxyRow, weights []float64) proxyRow {
	total := 0.0
	for _, weight := range weights {
		total += weight
	}
	draw := rand.Float64() * total
	for i, weight := range weights {
		draw -= weight
		if draw <= 0 {
			return candidates[i]
		}
	}
	return candidates[len(candidates)-1]
}

func requestForAttempt(req *http.Request, ctx context.Context, attempt int) *http.Request {
	next := req.Clone(ctx)
	if attempt == 0 || req.Body == nil || req.Body == http.NoBody {
		return next
	}
	if req.GetBody == nil {
		return nil
	}
	body, err := req.GetBody()
	if err != nil {
		return nil
	}
	next.Body = body
	return next
}

func idempotent(method string) bool {
	switch method {
	case "", http.MethodGet, http.MethodHead:
		return true
	}
	return false
}
