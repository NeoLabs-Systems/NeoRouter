package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func env(values map[string]string) func(string) string {
	return func(k string) string { return values[k] }
}

func TestLoadDefaults(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	cfg, err := Load(env(nil), home, cwd)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Host != "127.0.0.1" {
		t.Fatalf("host = %q", cfg.Host)
	}
	if cfg.Port != 4600 {
		t.Fatalf("port = %d", cfg.Port)
	}
	if cfg.PublicURL != "" || cfg.WebDevURL != "" {
		t.Fatalf("urls = %q %q", cfg.PublicURL, cfg.WebDevURL)
	}
	if cfg.TrustProxy || cfg.AllowPrivateProviderURLs {
		t.Fatal("booleans must default to false")
	}
	if cfg.LogLevel != "info" {
		t.Fatalf("log level = %q", cfg.LogLevel)
	}
	if cfg.SessionTTL != 30*24*time.Hour {
		t.Fatalf("session ttl = %v", cfg.SessionTTL)
	}
	if !cfg.WorkersEnabled || !cfg.MetadataSourcesEnabled {
		t.Fatal("workers and metadata sources default on outside tests")
	}
	if cfg.MaxBodyBytes != 32<<20 {
		t.Fatalf("max body bytes = %d", cfg.MaxBodyBytes)
	}
	if cfg.UpstreamTimeout != 120*time.Second {
		t.Fatalf("upstream timeout = %v", cfg.UpstreamTimeout)
	}
	if cfg.Paths.Home != filepath.Join(home, ".neorouter") {
		t.Fatalf("home = %q", cfg.Paths.Home)
	}
	if cfg.Paths.Database != filepath.Join(home, ".neorouter", "data", "neorouter.sqlite3") {
		t.Fatalf("database = %q", cfg.Paths.Database)
	}
	if cfg.Paths.EnvFile != filepath.Join(home, ".neorouter", ".env") {
		t.Fatalf("env file = %q", cfg.Paths.EnvFile)
	}
	if cfg.Paths.SecretKey != filepath.Join(home, ".neorouter", "data", "secret.key") {
		t.Fatalf("secret key = %q", cfg.Paths.SecretKey)
	}
	if cfg.Paths.PIDFile != filepath.Join(home, ".neorouter", "data", "neorouter.pid") {
		t.Fatalf("pid file = %q", cfg.Paths.PIDFile)
	}
	if cfg.Paths.UpdateStatusFile != filepath.Join(home, ".neorouter", "data", "update-status.json") {
		t.Fatalf("update status file = %q", cfg.Paths.UpdateStatusFile)
	}
	if cfg.AppDir != cwd {
		t.Fatalf("app dir = %q, want %q", cfg.AppDir, cwd)
	}
	if cfg.WebDir != filepath.Join(cwd, "flutter_app", "build", "web") {
		t.Fatalf("web dir = %q", cfg.WebDir)
	}
	if cfg.Landing != filepath.Join(cwd, "landing") {
		t.Fatalf("landing = %q", cfg.Landing)
	}
}

func TestRoutingAndSyncDefaults(t *testing.T) {
	cfg, err := Load(env(map[string]string{"NODE_ENV": "test"}), t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r := cfg.Routing
	if r.SlotOverflowReserve != time.Second || r.SlotMaxWait != 300*time.Second || r.SlotQueueWait != 45*time.Second {
		t.Fatalf("slot settings = %v %v %v", r.SlotOverflowReserve, r.SlotMaxWait, r.SlotQueueWait)
	}
	if r.SlotComparableGap != 0.1 || r.SlotUsageBucket != time.Minute || r.SlotUsageWindow != 24*time.Hour {
		t.Fatalf("slot comparables = %v %v %v", r.SlotComparableGap, r.SlotUsageBucket, r.SlotUsageWindow)
	}
	if r.MaxAttempts != 8 || r.TotalBudget != 300*time.Second || r.TopK != 5 || r.MaxParallel != 3 {
		t.Fatalf("routing numbers = %+v", r)
	}
	if r.CacheAffinityTTL != 30*time.Minute || r.CacheAffinitySessions != 10000 || r.CacheLearningWindow != 168*time.Hour {
		t.Fatalf("cache affinity = %+v", r)
	}
	if r.CacheAffinityScale != 1024 || r.CacheAffinityMaxBonus != 0.2 || !r.Hedging || r.QueueWait != 45*time.Second || r.HedgeUnmeasured != 10*time.Second {
		t.Fatalf("routing extras = %+v", r)
	}
	s := cfg.Sync
	if s.ValidateInterval != 5*time.Minute || s.ModelSyncInterval != 2*time.Hour || s.MetadataInterval != time.Hour || s.QuotaProbeInterval != 15*time.Minute {
		t.Fatalf("sync = %+v", s)
	}
	if cfg.WorkersEnabled {
		t.Fatal("workers must default off when NODE_ENV=test")
	}
	if cfg.MetadataSourcesEnabled {
		t.Fatal("metadata sources must default off when NODE_ENV=test")
	}
}

func TestLoadEnvOverrides(t *testing.T) {
	home := t.TempDir()
	cfg, err := Load(env(map[string]string{
		"NEOROUTER_HOME":                        home,
		"NEOROUTER_HOST":                        "0.0.0.0",
		"NEOROUTER_PORT":                        "8080",
		"NEOROUTER_PUBLIC_URL":                  "https://router.example.com",
		"NEOROUTER_WEB_DEV_URL":                 "http://localhost:3000",
		"NEOROUTER_TRUST_PROXY":                 "TRUE",
		"NEOROUTER_ALLOW_PRIVATE_PROVIDER_URLS": "yes",
		"NEOROUTER_LOG_LEVEL":                   "debug",
		"NEOROUTER_SESSION_TTL_MS":              "60000",
		"NEOROUTER_WORKERS_ENABLED":             "0",
		"NEOROUTER_MAX_BODY_BYTES":              "4096",
		"NEOROUTER_UPSTREAM_TIMEOUT_MS":         "5000",
		"NEOROUTER_ROUTING_TOP_K":               "9",
		"NEOROUTER_ROUTING_HEDGING":             "off",
		"NEOROUTER_ROUTING_MAX_PARALLEL":        "8",
		"NEOROUTER_ROUTING_MAX_ATTEMPTS":        "30",
		"NEOROUTER_ROUTING_QUEUE_WAIT_MS":       "0",
		"NEOROUTER_ROUTING_HEDGE_UNMEASURED_MS": "100",
		"NEOROUTER_SLOT_QUEUE_WAIT_MS":          "300000",
		"NEOROUTER_VALIDATE_INTERVAL_MS":        "30000",
		"NEOROUTER_METADATA_SOURCES_ENABLED":    "on",
		"NEOROUTER_CACHE_AFFINITY_TTL_MS":       "1000",
		"NEOROUTER_CACHE_AFFINITY_MAX_SESSIONS": "1",
		"NEOROUTER_CACHE_LEARNING_WINDOW_MS":    "1000",
		"NEOROUTER_CACHE_AFFINITY_TOKEN_SCALE":  "1",
		"NEOROUTER_METADATA_INTERVAL_MS":        "60000",
		"NEOROUTER_QUOTA_PROBE_INTERVAL_MS":     "60000",
		"NEOROUTER_MODEL_SYNC_INTERVAL_MS":      "60000",
		"NEOROUTER_SLOT_OVERFLOW_RESERVE_MS":    "0",
		"NEOROUTER_ROUTING_TOTAL_BUDGET_MS":     "1000",
	}), home, home)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Host != "0.0.0.0" || cfg.Port != 8080 {
		t.Fatalf("host/port = %q/%d", cfg.Host, cfg.Port)
	}
	if cfg.PublicURL != "https://router.example.com" || cfg.WebDevURL != "http://localhost:3000" {
		t.Fatalf("urls = %q %q", cfg.PublicURL, cfg.WebDevURL)
	}
	if !cfg.TrustProxy || !cfg.AllowPrivateProviderURLs {
		t.Fatal("booleans not parsed")
	}
	if cfg.LogLevel != "debug" {
		t.Fatalf("log level = %q", cfg.LogLevel)
	}
	if cfg.SessionTTL != time.Minute {
		t.Fatalf("session ttl = %v", cfg.SessionTTL)
	}
	if cfg.WorkersEnabled {
		t.Fatal("workers should be disabled")
	}
	if cfg.MaxBodyBytes != 4096 || cfg.UpstreamTimeout != 5*time.Second {
		t.Fatalf("limits = %d %v", cfg.MaxBodyBytes, cfg.UpstreamTimeout)
	}
	if cfg.Routing.TopK != 9 || cfg.Routing.Hedging || cfg.Routing.MaxParallel != 8 || cfg.Routing.MaxAttempts != 30 {
		t.Fatalf("routing = %+v", cfg.Routing)
	}
	if cfg.Routing.QueueWait != 0 || cfg.Routing.HedgeUnmeasured != 100*time.Millisecond {
		t.Fatalf("routing waits = %+v", cfg.Routing)
	}
	if cfg.Routing.SlotQueueWait != 300*time.Second || cfg.Routing.SlotOverflowReserve != 0 || cfg.Routing.TotalBudget != time.Second {
		t.Fatalf("slot settings = %+v", cfg.Routing)
	}
	if !cfg.MetadataSourcesEnabled {
		t.Fatal("metadata sources should be enabled")
	}
	if cfg.Sync.ValidateInterval != 30*time.Second || cfg.Sync.QuotaProbeInterval != time.Minute {
		t.Fatalf("sync = %+v", cfg.Sync)
	}
}

func TestPathResolution(t *testing.T) {
	home := "/home/tester"
	cases := []struct {
		name string
		env  map[string]string
		want string
	}{
		{name: "default", want: filepath.Join(home, ".neorouter")},
		{name: "tilde", env: map[string]string{"NEOROUTER_HOME": "~"}, want: home},
		{name: "tilde slash", env: map[string]string{"NEOROUTER_HOME": "~/custom"}, want: filepath.Join(home, "custom")},
		{name: "absolute", env: map[string]string{"NEOROUTER_HOME": "/opt/neorouter"}, want: "/opt/neorouter"},
		{name: "relative", env: map[string]string{"NEOROUTER_HOME": "relative/dir"}, want: mustAbs(t, "relative/dir")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			paths := ResolvePaths(env(tc.env), home)
			if paths.Home != tc.want {
				t.Fatalf("home = %q, want %q", paths.Home, tc.want)
			}
		})
	}

	paths := ResolvePaths(env(map[string]string{
		"NEOROUTER_HOME":          "/srv/nr",
		"NEOROUTER_DATABASE_PATH": "/tmp/custom.sqlite3",
	}), home)
	if paths.Database != "/tmp/custom.sqlite3" {
		t.Fatalf("database override = %q", paths.Database)
	}
}

func mustAbs(t *testing.T, p string) string {
	t.Helper()
	abs, err := filepath.Abs(p)
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

func TestPathsEnsureCreatesPrivateDirs(t *testing.T) {
	home := t.TempDir()
	paths := ResolvePaths(env(map[string]string{"NEOROUTER_HOME": filepath.Join(home, "nr")}), home)
	if err := paths.Ensure(); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	for _, dir := range []string{paths.Home, paths.Data, paths.Logs, paths.Backups} {
		st, err := os.Stat(dir)
		if err != nil {
			t.Fatalf("missing %s: %v", dir, err)
		}
		if !st.IsDir() {
			t.Fatalf("%s is not a directory", dir)
		}
		if perm := st.Mode().Perm(); perm != 0o700 {
			t.Fatalf("%s mode = %o, want 700", dir, perm)
		}
	}
}

func TestValidationErrorTexts(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want string
	}{
		{name: "port not a number", env: map[string]string{"NEOROUTER_PORT": "abc"}, want: "NEOROUTER_PORT must be an integer between 1 and 65535."},
		{name: "port out of range", env: map[string]string{"NEOROUTER_PORT": "70000"}, want: "NEOROUTER_PORT must be an integer between 1 and 65535."},
		{name: "port zero", env: map[string]string{"NEOROUTER_PORT": "0"}, want: "NEOROUTER_PORT must be an integer between 1 and 65535."},
		{name: "port decimal", env: map[string]string{"NEOROUTER_PORT": "46.5"}, want: "NEOROUTER_PORT must be an integer between 1 and 65535."},
		{name: "boolean", env: map[string]string{"NEOROUTER_TRUST_PROXY": "maybe"}, want: "NEOROUTER_TRUST_PROXY must be true or false."},
		{name: "session ttl too small", env: map[string]string{"NEOROUTER_SESSION_TTL_MS": "1000"}, want: "NEOROUTER_SESSION_TTL_MS must be an integer between 60000 and 9007199254740991."},
		{name: "body bytes too small", env: map[string]string{"NEOROUTER_MAX_BODY_BYTES": "10"}, want: "NEOROUTER_MAX_BODY_BYTES must be an integer between 1024 and 9007199254740991."},
		{name: "top k", env: map[string]string{"NEOROUTER_ROUTING_TOP_K": "51"}, want: "NEOROUTER_ROUTING_TOP_K must be an integer between 1 and 50."},
		{name: "max parallel", env: map[string]string{"NEOROUTER_ROUTING_MAX_PARALLEL": "9"}, want: "NEOROUTER_ROUTING_MAX_PARALLEL must be an integer between 1 and 8."},
		{name: "hedging", env: map[string]string{"NEOROUTER_ROUTING_HEDGING": "sometimes"}, want: "NEOROUTER_ROUTING_HEDGING must be true or false."},
		{name: "queue wait above ceiling", env: map[string]string{"NEOROUTER_SLOT_QUEUE_WAIT_MS": "300001"}, want: "NEOROUTER_SLOT_QUEUE_WAIT_MS must be an integer between 0 and 300000."},
		{name: "hedge unmeasured floor", env: map[string]string{"NEOROUTER_ROUTING_HEDGE_UNMEASURED_MS": "99"}, want: "NEOROUTER_ROUTING_HEDGE_UNMEASURED_MS must be an integer between 100 and 9007199254740991."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Load(env(tc.env), t.TempDir(), t.TempDir())
			if err == nil {
				t.Fatal("Load accepted an invalid value")
			}
			if err.Error() != tc.want {
				t.Fatalf("error = %q, want %q", err.Error(), tc.want)
			}
		})
	}
}

func TestBooleanForms(t *testing.T) {
	for _, value := range []string{"1", "TRUE", "true", "yes", "Yes", "on", "ON"} {
		cfg, err := Load(env(map[string]string{"NEOROUTER_TRUST_PROXY": value}), t.TempDir(), t.TempDir())
		if err != nil {
			t.Fatalf("%q: %v", value, err)
		}
		if !cfg.TrustProxy {
			t.Fatalf("%q parsed as false", value)
		}
	}
	for _, value := range []string{"0", "FALSE", "false", "no", "No", "off", "OFF"} {
		cfg, err := Load(env(map[string]string{"NEOROUTER_TRUST_PROXY": value}), t.TempDir(), t.TempDir())
		if err != nil {
			t.Fatalf("%q: %v", value, err)
		}
		if cfg.TrustProxy {
			t.Fatalf("%q parsed as true", value)
		}
	}
}

func TestEmptyValueTakesDefault(t *testing.T) {
	cfg, err := Load(env(map[string]string{"NEOROUTER_PORT": "", "NEOROUTER_HOST": "", "NEOROUTER_TRUST_PROXY": ""}), t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Port != 4600 || cfg.Host != "127.0.0.1" || cfg.TrustProxy {
		t.Fatalf("defaults not applied: %+v", cfg)
	}
}

func TestAppDirAndWebPaths(t *testing.T) {
	home := t.TempDir()
	appDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(appDir, "landing"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "landing", "index.html"), []byte("landing"), 0o600); err != nil {
		t.Fatal(err)
	}
	cwd := t.TempDir()
	cfg, err := Load(env(map[string]string{"NEOROUTER_HOME": home}), home, cwd)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AppDir != cwd {
		t.Fatalf("cwd without assets should win, got %q", cfg.AppDir)
	}

	// A directory with assets is picked up, and NEOROUTER_APP_DIR overrides it.
	cfg, err = Load(env(map[string]string{"NEOROUTER_HOME": home}), home, appDir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AppDir != appDir {
		t.Fatalf("app dir = %q, want %q", cfg.AppDir, appDir)
	}
	override := t.TempDir()
	cfg, err = Load(env(map[string]string{"NEOROUTER_HOME": home, "NEOROUTER_APP_DIR": override}), home, appDir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AppDir != override {
		t.Fatalf("app dir = %q, want %q", cfg.AppDir, override)
	}
	if cfg.WebDir != filepath.Join(override, "flutter_app", "build", "web") {
		t.Fatalf("web dir = %q", cfg.WebDir)
	}
	if cfg.Landing != filepath.Join(override, "landing") {
		t.Fatalf("landing = %q", cfg.Landing)
	}
}

func TestAllowedOrigins(t *testing.T) {
	got := AllowedOrigins(env(map[string]string{"NEOROUTER_ALLOWED_ORIGINS": " https://a.example.com , https://b.example.com ,, "}))
	want := []string{"https://a.example.com", "https://b.example.com"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("origins = %v", got)
	}
	if got := AllowedOrigins(env(nil)); len(got) != 0 {
		t.Fatalf("origins = %v", got)
	}
}

func TestServicePaths(t *testing.T) {
	home := "/home/tester"
	if got := SystemdUnitPath(home); got != "/home/tester/.config/systemd/user/neorouter.service" {
		t.Fatalf("systemd unit = %q", got)
	}
	if got := LaunchAgentPlistPath(home); got != "/home/tester/Library/LaunchAgents/systems.neolabs.neorouter.plist" {
		t.Fatalf("plist = %q", got)
	}
	if got := WebClientDir("/opt/app"); got != "/opt/app/flutter_app/build/web" {
		t.Fatalf("web client dir = %q", got)
	}
	if got := LandingDir("/opt/app"); got != "/opt/app/landing" {
		t.Fatalf("landing dir = %q", got)
	}
	if ServiceLabel != "systems.neolabs.neorouter" {
		t.Fatalf("service label = %q", ServiceLabel)
	}
	if Version != "0.2.1" {
		t.Fatalf("version = %q", Version)
	}
}

func TestLoadEnvironmentAndEnvFile(t *testing.T) {
	home := t.TempDir()
	paths := ResolvePaths(env(map[string]string{"NEOROUTER_HOME": filepath.Join(home, "nr")}), home)
	cwd := t.TempDir()
	if err := os.WriteFile(filepath.Join(cwd, ".env"), []byte("NEOROUTER_TEST_FROM_CWD=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := LoadEnvironment(paths, cwd); err != nil {
		t.Fatalf("LoadEnvironment: %v", err)
	}
	t.Cleanup(func() {
		os.Unsetenv("NEOROUTER_TEST_FROM_CWD")
		os.Unsetenv("NEOROUTER_TEST_KEY")
	})
	if os.Getenv("NEOROUTER_TEST_FROM_CWD") != "1" {
		t.Fatal("cwd .env was not loaded")
	}

	if err := UpsertEnvValue(paths.EnvFile, "NEOROUTER_TEST_KEY", "value"); err != nil {
		t.Fatalf("UpsertEnvValue: %v", err)
	}
	if err := UpsertEnvValue(paths.EnvFile, "NEOROUTER_TEST_KEY", "second"); err != nil {
		t.Fatalf("UpsertEnvValue: %v", err)
	}
	values, err := EnvFileValues(paths.EnvFile)
	if err != nil {
		t.Fatal(err)
	}
	if values["NEOROUTER_TEST_KEY"] != "second" {
		t.Fatalf("env value = %q", values["NEOROUTER_TEST_KEY"])
	}
	st, err := os.Stat(paths.EnvFile)
	if err != nil {
		t.Fatal(err)
	}
	if perm := st.Mode().Perm(); perm != 0o600 {
		t.Fatalf("env file mode = %o", perm)
	}
	removed, err := RemoveEnvValue(paths.EnvFile, "NEOROUTER_TEST_KEY")
	if err != nil || !removed {
		t.Fatalf("RemoveEnvValue = %v, %v", removed, err)
	}
	if ValidEnvKey("not a key") {
		t.Fatal("invalid key accepted")
	}
}
