// Package app constructs NeoRouter's database and HTTP handler and is the only
// place where the domains are wired together. It mirrors the shape of the
// NeoRecall/NeoAgent apps: Options in, Open builds everything, Close releases it.
package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/dbx"
	"github.com/NeoLabs-Systems/neolabs-go/httpx"
	"github.com/NeoLabs-Systems/neolabs-go/ssrf"
	"github.com/NeoLabs-Systems/neorouter/internal/config"
	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
	"github.com/NeoLabs-Systems/neorouter/internal/schema"
)

// Options configure Open. Either StaticConfig or ConfigLoader supplies the
// configuration; the loader is kept for live reloads.
type Options struct {
	ConfigLoader func() (config.Config, error)
	StaticConfig *config.Config
	Getenv       func(string) string
	Logger       *slog.Logger
	HTTPClient   *http.Client
	AppDir       string
	Version      string
}

// App is one running NeoRouter instance.
type App struct {
	mu     sync.RWMutex
	cfg    config.Config
	loader func() (config.Config, error)

	DB         *dbx.DB
	Log        *slog.Logger
	HTTPClient *http.Client
	Version    string
	Handler    http.Handler
}

// Open loads the configuration, creates the runtime directories, opens the
// SQLite database, applies the schema and builds the HTTP handler.
func Open(ctx context.Context, opts Options) (*App, error) {
	getenv := opts.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	log := opts.Logger
	if log == nil {
		log = slog.Default()
	}
	initial, err := loadConfig(opts, getenv)
	if err != nil {
		return nil, err
	}
	if err := initial.Paths.Ensure(); err != nil {
		return nil, err
	}
	db, err := dbx.Open(ctx, initial.Paths.Database, dbx.Options{BusyTimeout: 10 * time.Second})
	if err != nil {
		return nil, err
	}
	if err := schema.Ensure(ctx, db.W); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("app: prepare schema: %w", err)
	}

	httpClient := opts.HTTPClient
	if httpClient == nil {
		httpClient = ssrf.NewClient(ssrf.Options{Timeout: initial.UpstreamTimeout})
	}
	version := opts.Version
	if version == "" {
		version = config.Version
	}
	if opts.AppDir != "" {
		initial.AppDir = opts.AppDir
		initial.WebDir = config.WebClientDir(opts.AppDir)
		initial.Landing = config.LandingDir(opts.AppDir)
	}

	a := &App{
		cfg:        initial,
		loader:     opts.ConfigLoader,
		DB:         db,
		Log:        log,
		HTTPClient: httpClient,
		Version:    version,
	}
	cfgFn := a.Config

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteJSON(w, http.StatusOK, healthResponse{Status: "ok", Process: "http", Version: version})
	})
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		httpserver.NotFound(w)
	})
	mux.HandleFunc("GET /ready", func(w http.ResponseWriter, r *http.Request) {
		var one int
		if err := a.DB.R.QueryRowContext(r.Context(), `SELECT 1`).Scan(&one); err != nil {
			httpserver.WriteError(w, r, log, fmt.Errorf("app: database not ready: %w", err))
			return
		}
		httpx.WriteJSON(w, http.StatusOK, readyResponse{Status: "ready", Database: true})
	})
	mux.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
		httpserver.NotFound(w)
	})
	a.mountDomains(mux)

	mux.Handle("/", httpserver.Static(httpserver.StaticOptions{
		WebDir:     func() string { return cfgFn().WebDir },
		LandingDir: func() string { return cfgFn().Landing },
		WebDevURL:  func() string { return cfgFn().WebDevURL },
	}))

	a.Handler = httpserver.RequestID(httpserver.CORS(getenv, httpserver.Gzip(mux)))
	return a, nil
}

func loadConfig(opts Options, getenv func(string) string) (config.Config, error) {
	switch {
	case opts.StaticConfig != nil:
		return *opts.StaticConfig, nil
	case opts.ConfigLoader != nil:
		cfg, err := opts.ConfigLoader()
		if err != nil {
			return config.Config{}, err
		}
		return cfg, nil
	default:
		userHome, err := os.UserHomeDir()
		if err != nil {
			return config.Config{}, fmt.Errorf("app: find home directory: %w", err)
		}
		cwd, err := os.Getwd()
		if err != nil {
			return config.Config{}, fmt.Errorf("app: find working directory: %w", err)
		}
		return config.Load(getenv, userHome, cwd)
	}
}

// mountDomains is where later waves register their routes on the API mux.
func (a *App) mountDomains(mux *http.ServeMux) {
	_ = mux
}

// Config returns the configuration the app is running with.
func (a *App) Config() config.Config {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.cfg
}

// ReloadConfig re-runs the config loader, if one was supplied.
func (a *App) ReloadConfig() error {
	if a.loader == nil {
		return nil
	}
	next, err := a.loader()
	if err != nil {
		return err
	}
	a.mu.Lock()
	a.cfg = next
	a.mu.Unlock()
	return nil
}

// Close releases the database.
func (a *App) Close() error {
	if a.DB != nil {
		return a.DB.Close()
	}
	return nil
}

type healthResponse struct {
	Status  string `json:"status"`
	Process string `json:"process"`
	Version string `json:"version"`
}

type readyResponse struct {
	Status   string `json:"status"`
	Database bool   `json:"database"`
}
