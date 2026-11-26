package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/runtimecfg"
)

// Version is the NeoRouter release version (mirrors package.json).
const Version = "0.2.2-beta.1"

// maxSafeInteger is Number.MAX_SAFE_INTEGER, the upper bound
// server/config/env.js uses when no maximum is given. It appears verbatim in
// the "must be an integer between" error message.
const maxSafeInteger = 9007199254740991

// Routing is the routing block of server/config/env.js.
type Routing struct {
	SlotOverflowReserve   time.Duration // NEOROUTER_SLOT_OVERFLOW_RESERVE_MS (1000, min 0)
	SlotMaxWait           time.Duration // fixed 300000
	SlotQueueWait         time.Duration // NEOROUTER_SLOT_QUEUE_WAIT_MS (45000, 0..300000)
	SlotComparableGap     float64       // fixed 0.1
	SlotUsageBucket       time.Duration // fixed 60000
	SlotUsageWindow       time.Duration // fixed 86400000
	MaxAttempts           int           // NEOROUTER_ROUTING_MAX_ATTEMPTS (8, 1..30)
	TotalBudget           time.Duration // NEOROUTER_ROUTING_TOTAL_BUDGET_MS (300000, min 1000)
	CacheAffinityTTL      time.Duration // NEOROUTER_CACHE_AFFINITY_TTL_MS (1800000, min 1000)
	CacheAffinitySessions int           // NEOROUTER_CACHE_AFFINITY_MAX_SESSIONS (10000, min 1)
	CacheLearningWindow   time.Duration // NEOROUTER_CACHE_LEARNING_WINDOW_MS (604800000, min 1000)
	CacheAffinityScale    int           // NEOROUTER_CACHE_AFFINITY_TOKEN_SCALE (1024, min 1)
	CacheAffinityMaxBonus float64       // fixed 0.2
	TopK                  int           // NEOROUTER_ROUTING_TOP_K (5, 1..50)
	Hedging               bool          // NEOROUTER_ROUTING_HEDGING (true)
	MaxParallel           int           // NEOROUTER_ROUTING_MAX_PARALLEL (3, 1..8)
	QueueWait             time.Duration // NEOROUTER_ROUTING_QUEUE_WAIT_MS (45000, min 0)
	HedgeUnmeasured       time.Duration // NEOROUTER_ROUTING_HEDGE_UNMEASURED_MS (10000, min 100)
}

// Sync is the background sync cadence block of server/config/env.js.
type Sync struct {
	ValidateInterval   time.Duration // NEOROUTER_VALIDATE_INTERVAL_MS (300000, min 30000)
	ModelSyncInterval  time.Duration // NEOROUTER_MODEL_SYNC_INTERVAL_MS (7200000, min 60000)
	MetadataInterval   time.Duration // NEOROUTER_METADATA_INTERVAL_MS (3600000, min 60000)
	QuotaProbeInterval time.Duration // NEOROUTER_QUOTA_PROBE_INTERVAL_MS (900000, min 60000)
}

// Config is the validated server configuration.
type Config struct {
	Paths   Paths
	AppDir  string
	WebDir  string
	Landing string

	Host                     string
	Port                     int
	PublicURL                string
	WebDevURL                string
	TrustProxy               bool
	AllowPrivateProviderURLs bool
	LogLevel                 string
	SessionTTL               time.Duration
	WorkersEnabled           bool
	MaxBodyBytes             int64
	UpstreamTimeout          time.Duration

	Routing                Routing
	Sync                   Sync
	MetadataSourcesEnabled bool
}

type reader struct {
	getenv func(string) string
	err    error
}

func (r *reader) str(name, def string) string {
	if v := r.getenv(name); v != "" {
		return v
	}
	return def
}

// integer mirrors server/config/env.js integer(): an empty value takes the
// fallback, anything else must parse as an integer inside [min, max].
func (r *reader) integer(name string, def, min, max int64) int64 {
	if r.err != nil {
		return def
	}
	raw := r.getenv(name)
	if raw == "" {
		return def
	}
	v, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || v < min || v > max {
		r.err = fmt.Errorf("%s must be an integer between %d and %d.", name, min, max)
		return def
	}
	return v
}

func (r *reader) ms(name string, def time.Duration, minMs, maxMs int64) time.Duration {
	return time.Duration(r.integer(name, int64(def/time.Millisecond), minMs, maxMs)) * time.Millisecond
}

// boolean mirrors server/config/env.js boolean().
func (r *reader) boolean(name string, def bool) bool {
	if r.err != nil {
		return def
	}
	raw := r.getenv(name)
	if raw == "" {
		return def
	}
	if b := runtimecfg.Bool(raw, true); b != runtimecfg.Bool(raw, false) {
		r.err = fmt.Errorf("%s must be true or false.", name)
		return def
	}
	return runtimecfg.Bool(raw, def)
}

func resolveAppDir(envDir, cwd, userHome string) string {
	if v := strings.TrimSpace(envDir); v != "" {
		return v
	}
	hasAssets := func(dir string) bool {
		if dir == "" {
			return false
		}
		if _, err := os.Stat(filepath.Join(dir, "flutter_app", "build", "web", "index.html")); err == nil {
			return true
		}
		if _, err := os.Stat(filepath.Join(dir, "landing", "index.html")); err == nil {
			return true
		}
		return false
	}
	var exeDir string
	if exe, err := os.Executable(); err == nil {
		exeDir = filepath.Dir(exe)
	}
	for _, cand := range []string{
		cwd,
		exeDir,
		filepath.Join(userHome, "NeoRouter"),
	} {
		if hasAssets(cand) {
			return cand
		}
	}
	return cwd
}

// Load parses and validates the NeoRouter configuration the way
// server/config/env.js getConfig() does, and resolves the runtime paths.
func Load(getenv func(string) string, userHome, cwd string) (Config, error) {
	if getenv == nil {
		getenv = os.Getenv
	}
	r := &reader{getenv: getenv}
	testMode := getenv("NODE_ENV") == "test"

	var c Config
	c.Paths = ResolvePaths(getenv, userHome)
	c.AppDir = resolveAppDir(getenv("NEOROUTER_APP_DIR"), cwd, userHome)
	c.WebDir = WebClientDir(c.AppDir)
	c.Landing = LandingDir(c.AppDir)

	c.Host = r.str("NEOROUTER_HOST", "127.0.0.1")
	c.Port = int(r.integer("NEOROUTER_PORT", 4600, 1, 65535))
	c.PublicURL = getenv("NEOROUTER_PUBLIC_URL")
	c.WebDevURL = getenv("NEOROUTER_WEB_DEV_URL")
	c.TrustProxy = r.boolean("NEOROUTER_TRUST_PROXY", false)
	c.AllowPrivateProviderURLs = r.boolean("NEOROUTER_ALLOW_PRIVATE_PROVIDER_URLS", false)
	c.LogLevel = r.str("NEOROUTER_LOG_LEVEL", "info")
	c.SessionTTL = time.Duration(r.integer("NEOROUTER_SESSION_TTL_MS", 30*24*60*60*1000, 60_000, maxSafeInteger)) * time.Millisecond
	c.WorkersEnabled = r.boolean("NEOROUTER_WORKERS_ENABLED", !testMode)
	c.MaxBodyBytes = r.integer("NEOROUTER_MAX_BODY_BYTES", 32*1024*1024, 1024, maxSafeInteger)
	c.UpstreamTimeout = time.Duration(r.integer("NEOROUTER_UPSTREAM_TIMEOUT_MS", 120_000, 1000, maxSafeInteger)) * time.Millisecond

	c.Routing = Routing{
		SlotOverflowReserve:   r.ms("NEOROUTER_SLOT_OVERFLOW_RESERVE_MS", 1000*time.Millisecond, 0, maxSafeInteger),
		SlotMaxWait:           300000 * time.Millisecond,
		SlotQueueWait:         r.ms("NEOROUTER_SLOT_QUEUE_WAIT_MS", 45000*time.Millisecond, 0, 300000),
		SlotComparableGap:     0.1,
		SlotUsageBucket:       60000 * time.Millisecond,
		SlotUsageWindow:       86400000 * time.Millisecond,
		MaxAttempts:           int(r.integer("NEOROUTER_ROUTING_MAX_ATTEMPTS", 8, 1, 30)),
		TotalBudget:           r.ms("NEOROUTER_ROUTING_TOTAL_BUDGET_MS", 300_000*time.Millisecond, 1000, maxSafeInteger),
		CacheAffinityTTL:      r.ms("NEOROUTER_CACHE_AFFINITY_TTL_MS", 30*60_000*time.Millisecond, 1000, maxSafeInteger),
		CacheAffinitySessions: int(r.integer("NEOROUTER_CACHE_AFFINITY_MAX_SESSIONS", 10000, 1, maxSafeInteger)),
		CacheLearningWindow:   r.ms("NEOROUTER_CACHE_LEARNING_WINDOW_MS", 7*86_400_000*time.Millisecond, 1000, maxSafeInteger),
		CacheAffinityScale:    int(r.integer("NEOROUTER_CACHE_AFFINITY_TOKEN_SCALE", 1024, 1, maxSafeInteger)),
		CacheAffinityMaxBonus: 0.2,
		TopK:                  int(r.integer("NEOROUTER_ROUTING_TOP_K", 5, 1, 50)),
		Hedging:               r.boolean("NEOROUTER_ROUTING_HEDGING", true),
		MaxParallel:           int(r.integer("NEOROUTER_ROUTING_MAX_PARALLEL", 3, 1, 8)),
		QueueWait:             r.ms("NEOROUTER_ROUTING_QUEUE_WAIT_MS", 45_000*time.Millisecond, 0, maxSafeInteger),
		HedgeUnmeasured:       r.ms("NEOROUTER_ROUTING_HEDGE_UNMEASURED_MS", 10_000*time.Millisecond, 100, maxSafeInteger),
	}
	c.Sync = Sync{
		ValidateInterval:   r.ms("NEOROUTER_VALIDATE_INTERVAL_MS", 5*60_000*time.Millisecond, 30_000, maxSafeInteger),
		ModelSyncInterval:  r.ms("NEOROUTER_MODEL_SYNC_INTERVAL_MS", 2*60*60_000*time.Millisecond, 60_000, maxSafeInteger),
		MetadataInterval:   r.ms("NEOROUTER_METADATA_INTERVAL_MS", 60*60_000*time.Millisecond, 60_000, maxSafeInteger),
		QuotaProbeInterval: r.ms("NEOROUTER_QUOTA_PROBE_INTERVAL_MS", 15*60_000*time.Millisecond, 60_000, maxSafeInteger),
	}
	c.MetadataSourcesEnabled = r.boolean("NEOROUTER_METADATA_SOURCES_ENABLED", !testMode)

	if r.err != nil {
		return c, r.err
	}
	return c, nil
}

// AllowedOrigins is NEOROUTER_ALLOWED_ORIGINS split into trimmed entries.
// middleware/cors.js reads it on every request, so callers pass getenv.
func AllowedOrigins(getenv func(string) string) []string {
	if getenv == nil {
		getenv = os.Getenv
	}
	var out []string
	for _, item := range strings.Split(getenv("NEOROUTER_ALLOWED_ORIGINS"), ",") {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
