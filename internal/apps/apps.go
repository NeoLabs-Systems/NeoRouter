// Package apps is Settings → Connect your apps: the app catalog, the address an
// app on this machine, in a container or on the network should use, the live
// check of a router key, and (admins) who may reach NeoRouter.
//
// It serves the app catalog, the places apps run, network access and key
// activity. The OAuth surface apps talk to lives in internal/oauth.
package apps

import (
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/dbx"
	"github.com/NeoLabs-Systems/neorouter/internal/config"
)

// Service is the apps domain: the catalog, the places an app can run and the
// key activity check.
type Service struct {
	db      *dbx.DB
	cfg     func() config.Config
	getenv  func(string) string
	log     *slog.Logger
	guard   Guard
	restart func()
	now     func() time.Time

	mu       sync.Mutex
	failures map[string]failure
	order    []string // key ids in arrival order, oldest first
}

// Options configure a Service.
type Options struct {
	// DB is the running database; New keeps the reader pool for the key check.
	DB *dbx.DB
	// Config returns the live configuration (port, host, public URL).
	Config func() config.Config
	// Getenv reads the process environment; nil uses os.Getenv.
	Getenv func(string) string
	// Log is the server logger; nil uses slog.Default().
	Log *slog.Logger
	// Guard resolves the session and space of a request. internal/app supplies
	// it from internal/auth and internal/teams.
	Guard Guard
	// Restart stops this process so the service manager starts it again
	// (launchd, systemd). nil sends SIGTERM to this process.
	Restart func()
}

// New builds the apps domain.
func New(opts Options) *Service {
	s := &Service{
		db:       opts.DB,
		cfg:      opts.Config,
		getenv:   opts.Getenv,
		log:      opts.Log,
		guard:    opts.Guard,
		restart:  opts.Restart,
		now:      time.Now,
		failures: make(map[string]failure),
	}
	if s.cfg == nil {
		s.cfg = func() config.Config { return config.Config{} }
	}
	if s.getenv == nil {
		s.getenv = os.Getenv
	}
	if s.log == nil {
		s.log = slog.Default()
	}
	if s.restart == nil {
		s.restart = stopProcess
	}
	return s
}

// stopProcess sends SIGTERM to this process so launchd or systemd starts it
// again. Windows has no service manager to come back, so it is never reached
// there.
func stopProcess() {
	process, err := os.FindProcess(os.Getpid())
	if err != nil {
		return
	}
	_ = process.Signal(syscall.SIGTERM)
}

// requestProtocol is https when the request arrived over TLS, or else the first
// X-Forwarded-Proto value from a trusted proxy.
func requestProtocol(r *http.Request, trustProxy bool) string {
	if trustProxy {
		if forwarded := r.Header.Get("X-Forwarded-Proto"); forwarded != "" {
			if proto := strings.TrimSpace(strings.Split(forwarded, ",")[0]); proto != "" {
				return proto
			}
		}
	}
	if r.TLS != nil {
		return "https"
	}
	return "http"
}
