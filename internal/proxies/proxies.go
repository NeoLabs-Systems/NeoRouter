// Package proxies is NeoRouter's outbound proxy pool: the admin API that manages
// SOCKS5/HTTP proxies, and the rotation, health and cooldown logic that sends
// public provider HTTP traffic through them.
//
// It is a port of server/services/proxies/{pool,transport,stream,import,policy}.js
// and server/routes/proxies.js. Local/private destinations and relayed or
// WebSocket traffic keep their own transport; only the public provider HTTP path
// (including streams) reroutes through the pool, which is the caller's decision
// in server/providers/http.js.
package proxies

import (
	"errors"
	"time"
)

// Policy holds the shared bounds for transport, import and adaptive selection.
// Field names carry the JS unit (milliseconds) so the arithmetic ports line for
// line; server/services/proxies/policy.js is the source of truth.
type Policy struct {
	ConnectMs        int     // connect/tunnel and probe timeout
	RestMs           int     // first cooldown after a failure
	MaxRestMs        int     // cooldown ceiling
	MaxAttempts      int     // proxies tried per request
	MaxImport        int     // lines accepted by one import
	Connections      int     // connections kept per proxy
	Smoothing        float64 // latency moving average weight
	UnmeasuredMs     int     // latency assumed for a proxy nobody measured yet
	MaxDispatchers   int     // retained per-proxy transports
	IdleMs           int     // a transport unused this long is released
	CheckURL         string  // tiny public page every proxy should reach
	CheckEveryMs     int     // automatic check interval
	CheckConcurrency int     // proxies checked at a time
}

// DefaultPolicy returns policy.js's POLICY object.
func DefaultPolicy() Policy {
	return Policy{
		ConnectMs:        8000,
		RestMs:           30000,
		MaxRestMs:        900000,
		MaxAttempts:      3,
		MaxImport:        5000,
		Connections:      8,
		Smoothing:        0.8,
		UnmeasuredMs:     100,
		MaxDispatchers:   64,
		IdleMs:           60000,
		CheckURL:         "https://www.cloudflare.com/cdn-cgi/trace",
		CheckEveryMs:     60 * 60_000,
		CheckConcurrency: 16,
	}
}

// withDefaults fills unset fields from DefaultPolicy so a partially written
// Policy in wiring or a test stays usable.
func (p Policy) withDefaults() Policy {
	d := DefaultPolicy()
	if p.ConnectMs <= 0 {
		p.ConnectMs = d.ConnectMs
	}
	if p.RestMs <= 0 {
		p.RestMs = d.RestMs
	}
	if p.MaxRestMs <= 0 {
		p.MaxRestMs = d.MaxRestMs
	}
	if p.MaxAttempts <= 0 {
		p.MaxAttempts = d.MaxAttempts
	}
	if p.MaxImport <= 0 {
		p.MaxImport = d.MaxImport
	}
	if p.Connections <= 0 {
		p.Connections = d.Connections
	}
	if p.Smoothing <= 0 {
		p.Smoothing = d.Smoothing
	}
	if p.UnmeasuredMs <= 0 {
		p.UnmeasuredMs = d.UnmeasuredMs
	}
	if p.MaxDispatchers <= 0 {
		p.MaxDispatchers = d.MaxDispatchers
	}
	if p.IdleMs <= 0 {
		p.IdleMs = d.IdleMs
	}
	if p.CheckURL == "" {
		p.CheckURL = d.CheckURL
	}
	if p.CheckEveryMs <= 0 {
		p.CheckEveryMs = d.CheckEveryMs
	}
	if p.CheckConcurrency <= 0 {
		p.CheckConcurrency = d.CheckConcurrency
	}
	return p
}

func (p Policy) connectDuration() time.Duration { return time.Duration(p.ConnectMs) * time.Millisecond }
func (p Policy) idleDuration() time.Duration    { return time.Duration(p.IdleMs) * time.Millisecond }

// Errors whose texts are part of the API contract (pool.js throws the first two
// as plain Errors and routes/proxies.js turns them into 500s or 404s).
var (
	// ErrNoHealthyProxy is returned when every enabled proxy is resting and the
	// policy forbids a direct fallback. pool.js: 'No healthy outbound proxy is available.'
	ErrNoHealthyProxy = errors.New("No healthy outbound proxy is available.")
	// ErrTransportFailed is returned for a mutation whose request may already
	// have reached the origin, so replaying it is not safe. pool.js:
	// 'Outbound proxy transport failed.'
	ErrTransportFailed = errors.New("Outbound proxy transport failed.")
	// ErrConnect wraps every tunnel/handshake failure; pool.js tags them with
	// code PROXY_CONNECT_FAILED, which marks the attempt safe to retry.
	ErrConnect = errors.New("Proxy connection failed")
)
