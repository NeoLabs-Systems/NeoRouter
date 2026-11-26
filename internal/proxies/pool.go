package proxies

// The proxy pool itself: the list the admin API returns, the adaptive selection
// weighted by load, latency and failures, the cooldown that removes a failing
// proxy from the rotation, and the automatic health check.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"

	"github.com/NeoLabs-Systems/neolabs-go/dbx"
	"github.com/NeoLabs-Systems/neolabs-go/ssrf"
	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
	"github.com/NeoLabs-Systems/neorouter/internal/secrets"
)

// policySettingKey is the global settings row holding PoolPolicy.
const policySettingKey = "outbound_proxy_policy"

// Settings is the narrow slice of the settings domain the pool needs: the global
// outbound proxy policy as raw JSON. The method set is internal/settings'
// GetGlobal/SetGlobalRaw, so *settings.Service satisfies it without an adapter.
type Settings interface {
	// GetGlobal returns the stored JSON value of key and whether it exists.
	GetGlobal(ctx context.Context, key string) (value json.RawMessage, ok bool, err error)
	// SetGlobalRaw stores an already-encoded JSON value under key.
	SetGlobalRaw(ctx context.Context, key string, value json.RawMessage) error
}

// PoolPolicy is the outbound proxy policy the admin toggles.
type PoolPolicy struct {
	Enabled        bool `json:"enabled"`
	FallbackDirect bool `json:"fallbackDirect"`
}

// Proxy is one row of GET /api/v1/admin/proxies, health included.
type Proxy struct {
	ID        string   `json:"id"`
	Endpoint  string   `json:"endpoint"`
	Enabled   bool     `json:"enabled"`
	Failures  int      `json:"failures"`
	Until     int64    `json:"until"`
	LatencyMs *float64 `json:"latencyMs"`
	Active    int      `json:"active"`
	Served    int      `json:"served"`
	CheckedAt *int64   `json:"checkedAt,omitempty"`
	CheckOk   *bool    `json:"checkOk,omitempty"`
}

// ProbeResult is the body of POST /api/v1/admin/proxies/{id}/test.
type ProbeResult struct {
	OK        bool     `json:"ok"`
	Status    int      `json:"status,omitempty"`
	LatencyMs *float64 `json:"latencyMs,omitempty"`
}

// CheckResult is the body of POST /api/v1/admin/proxies/check and the lastCheck
// field of the list. At is epoch milliseconds, like Date.now().
type CheckResult struct {
	At      int64 `json:"at"`
	Checked int   `json:"checked"`
	OK      int   `json:"ok"`
}

// Options configure New.
type Options struct {
	DB       *dbx.DB
	Log      *slog.Logger
	Sealer   *secrets.Sealer
	Settings Settings
	// Policy is the rotation policy; zero values fall back to DefaultPolicy().
	Policy Policy
	// Direct is the client used when the pool is disabled or a direct fallback
	// runs. Nil uses a plain client.
	Direct *http.Client
	// IsPrivateDestination reports whether a URL resolves to a local or private
	// address. Nil uses the ssrf-based default; internal/providers' net guard
	// may replace it.
	IsPrivateDestination func(ctx context.Context, rawURL string) (bool, error)
}

// Service is the outbound proxy pool.
type Service struct {
	DB       *dbx.DB
	Log      *slog.Logger
	Sealer   *secrets.Sealer
	Settings Settings
	Direct   *http.Client

	IsPrivateDestination func(ctx context.Context, rawURL string) (bool, error)

	pol Policy

	mu        sync.Mutex
	health    map[string]*health
	agents    map[string]*dispatcher
	lastCheck *CheckResult
}

// New builds the pool. DB, Sealer and Settings are required; the remaining
// options have defaults.
func New(o Options) (*Service, error) {
	if o.DB == nil {
		return nil, errors.New("proxies: DB is required")
	}
	if o.Sealer == nil {
		return nil, errors.New("proxies: Sealer is required")
	}
	if o.Settings == nil {
		return nil, errors.New("proxies: Settings is required")
	}
	log := o.Log
	if log == nil {
		log = slog.Default()
	}
	direct := o.Direct
	if direct == nil {
		direct = directClient()
	}
	isPrivate := o.IsPrivateDestination
	if isPrivate == nil {
		isPrivate = DefaultIsPrivateDestination
	}
	return &Service{
		DB:                   o.DB,
		Log:                  log,
		Sealer:               o.Sealer,
		Settings:             o.Settings,
		Direct:               direct,
		IsPrivateDestination: isPrivate,
		pol:                  o.Policy.withDefaults(),
		health:               map[string]*health{},
		agents:               map[string]*dispatcher{},
	}, nil
}

// Policy returns the rotation policy in force.
func (s *Service) Policy() Policy { return s.pol }

// Close releases every retained proxy transport.
func (s *Service) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id := range s.agents {
		s.disposeLocked(id)
	}
	return nil
}

// PoolPolicy reads the stored policy; an unset row means "off, fall back direct".
func (s *Service) PoolPolicy(ctx context.Context) (PoolPolicy, error) {
	raw, ok, err := s.Settings.GetGlobal(ctx, policySettingKey)
	if err != nil {
		return PoolPolicy{}, fmt.Errorf("proxies: read policy: %w", err)
	}
	if !ok || len(raw) == 0 {
		return PoolPolicy{FallbackDirect: true}, nil
	}
	var policy PoolPolicy
	if err := json.Unmarshal([]byte(raw), &policy); err != nil {
		return PoolPolicy{}, fmt.Errorf("proxies: decode policy: %w", err)
	}
	return policy, nil
}

// SetPoolPolicy stores the policy the admin sent.
func (s *Service) SetPoolPolicy(ctx context.Context, policy PoolPolicy) error {
	raw, err := json.Marshal(policy)
	if err != nil {
		return fmt.Errorf("proxies: encode policy: %w", err)
	}
	if err := s.Settings.SetGlobalRaw(ctx, policySettingKey, raw); err != nil {
		return fmt.Errorf("proxies: write policy: %w", err)
	}
	return nil
}

// LastCheck returns the most recent automatic check, or nil before the first one.
func (s *Service) LastCheck() *CheckResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lastCheck == nil {
		return nil
	}
	check := *s.lastCheck
	return &check
}

// List returns every proxy ordered by endpoint, with its health.
func (s *Service) List(ctx context.Context) ([]Proxy, error) {
	rows, err := s.DB.R.QueryContext(ctx, `SELECT id, endpoint, enabled FROM outbound_proxies ORDER BY endpoint`)
	if err != nil {
		return nil, fmt.Errorf("proxies: list: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := []Proxy{}
	for rows.Next() {
		var (
			id       string
			endpoint string
			enabled  int
		)
		if err := rows.Scan(&id, &endpoint, &enabled); err != nil {
			return nil, fmt.Errorf("proxies: list: scan: %w", err)
		}
		proxy := Proxy{ID: id, Endpoint: endpoint, Enabled: enabled != 0}
		s.mu.Lock()
		snapshot := *s.stateLocked(id)
		s.mu.Unlock()
		proxy.Failures = snapshot.failures
		proxy.Until = snapshot.until
		proxy.LatencyMs = snapshot.latency
		proxy.Active = snapshot.active
		proxy.Served = snapshot.served
		proxy.CheckedAt = snapshot.checkedAt
		proxy.CheckOk = snapshot.checkOk
		out = append(out, proxy)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("proxies: list: rows: %w", err)
	}
	return out, nil
}

// Change enables or disables one proxy and drops its health, like pool.change.
func (s *Service) Change(ctx context.Context, id string, enabled bool) error {
	res, err := s.DB.W.ExecContext(ctx, `UPDATE outbound_proxies SET enabled = ? WHERE id = ?`, boolInt(enabled), id)
	if err != nil {
		return fmt.Errorf("proxies: change proxy: %w", err)
	}
	changes, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("proxies: change proxy: rows: %w", err)
	}
	if changes == 0 {
		return httpserver.New(http.StatusNotFound, "PROXY_NOT_FOUND", "Proxy not found.")
	}
	s.mu.Lock()
	s.disposeLocked(id)
	delete(s.health, id)
	s.mu.Unlock()
	return nil
}

// Remove deletes one proxy; removing an unknown id is not an error.
func (s *Service) Remove(ctx context.Context, id string) error {
	if _, err := s.DB.W.ExecContext(ctx, `DELETE FROM outbound_proxies WHERE id = ?`, id); err != nil {
		return fmt.Errorf("proxies: remove proxy: %w", err)
	}
	s.mu.Lock()
	s.disposeLocked(id)
	delete(s.health, id)
	s.mu.Unlock()
	return nil
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

// DefaultIsPrivateDestination resolves rawURL and reports whether any address
// is local or private: an unparsable URL counts as private, an unresolvable
// host does not.
func DefaultIsPrivateDestination(ctx context.Context, rawURL string) (bool, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return true, nil
	}
	host := strings.TrimSuffix(strings.TrimPrefix(parsed.Hostname(), "["), "]")
	if host == "" {
		return true, nil
	}
	if addr, err := netip.ParseAddr(host); err == nil {
		return ssrf.CheckAddr(addr) != nil, nil
	}
	addrs, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return false, nil
	}
	for _, addr := range addrs {
		if ssrf.CheckAddr(addr) != nil {
			return true, nil
		}
	}
	return false, nil
}
