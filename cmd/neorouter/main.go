// Command neorouter runs the NeoRouter backend server (`neorouter serve`) and
// manages the local installation (`neorouter help`).
//
// The server's run loop lives here; every other command is the manager's, so a
// single binary is all an install needs.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/NeoLabs-Systems/neorouter/internal/app"
	"github.com/NeoLabs-Systems/neorouter/internal/config"
	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
	"github.com/NeoLabs-Systems/neorouter/internal/manager"
)

func main() {
	args := os.Args[1:]
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	err := manager.Run(ctx, args, manager.Options{Serve: serve})
	if err != nil {
		manager.PrintError(os.Stderr, err, manager.DetailsRequested(args))
		os.Exit(1)
	}
}

// loadConfig reads the install's .env (without overriding the real
// environment), validates it, and points it at the active runtime when an
// update has installed one.
func loadConfig() (config.Config, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return config.Config{}, fmt.Errorf("find home directory: %w", err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return config.Config{}, fmt.Errorf("find working directory: %w", err)
	}
	return manager.LoadConfig(os.Getenv, home, cwd)
}

// serve runs the server until the context is cancelled.
func serve(ctx context.Context) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel(cfg.LogLevel)}))

	a, err := app.Open(ctx, app.Options{
		StaticConfig: &cfg,
		Getenv:       os.Getenv,
		Logger:       log,
		Version:      config.Version,
	})
	if err != nil {
		return err
	}
	defer a.Close()

	cfg = a.Config()
	if cfg.Paths.PIDFile != "" {
		pid := strconv.Itoa(os.Getpid()) + "\n"
		if err := os.WriteFile(cfg.Paths.PIDFile, []byte(pid), 0o600); err != nil {
			log.Warn("could not write PID file", "path", cfg.Paths.PIDFile, "error", err)
		} else {
			defer os.Remove(cfg.Paths.PIDFile)
		}
	}

	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	if cfg.Host == "" {
		addr = ":" + strconv.Itoa(cfg.Port)
	}
	srv := httpserver.NewHTTPServer(addr, a.Handler, log)
	errc := make(chan error, 1)
	go func() {
		log.Info("NeoRouter listening", "url", listenURL(cfg), "version", config.Version)
		errc <- srv.ListenAndServe()
	}()

	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		shutCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutCtx); err != nil {
			return err
		}
		log.Info("NeoRouter stopped")
	}
	return nil
}

func listenURL(cfg config.Config) string {
	host := cfg.Host
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, strconv.Itoa(cfg.Port))
}

// logLevel maps NEOROUTER_LOG_LEVEL to slog.
func logLevel(name string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
