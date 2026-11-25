// Command neorouter runs the NeoRouter backend server (`neorouter serve`) and,
// in later releases, manages the local installation (`neorouter help`).
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

	"github.com/NeoLabs-Systems/neolabs-go/runtimecfg"
	"github.com/NeoLabs-Systems/neorouter/internal/app"
	"github.com/NeoLabs-Systems/neorouter/internal/config"
	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
)

func main() {
	args := os.Args[1:]
	cmd := ""
	if len(args) > 0 {
		cmd = args[0]
	}
	switch {
	case cmd == "serve" || (cmd == "start" && len(args) > 1 && args[1] == "--foreground"):
		if err := serve(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %s\n", err)
			os.Exit(1)
		}
	case cmd == "version":
		fmt.Println(config.Version)
	case cmd == "healthcheck":
		if err := healthcheck(); err != nil {
			os.Exit(1)
		}
	case cmd == "":
		fmt.Println("Usage: neorouter <command>")
		fmt.Println("Commands: serve, version, healthcheck")
	default:
		fmt.Fprintf(os.Stderr, "Error: unknown command: %s\n", cmd)
		os.Exit(1)
	}
}

// loadConfig reads the install's .env (without overriding the real
// environment) and validates it, exactly like runtime/env.js plus
// server/config/env.js do on Node start.
func loadConfig() (config.Config, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return config.Config{}, fmt.Errorf("find home directory: %w", err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return config.Config{}, fmt.Errorf("find working directory: %w", err)
	}
	prelim := config.ResolvePaths(os.Getenv, home)
	if err := runtimecfg.LoadEnvFile(prelim.EnvFile, os.LookupEnv, os.Setenv); err != nil {
		return config.Config{}, fmt.Errorf("load %s: %w", prelim.EnvFile, err)
	}
	return config.Load(os.Getenv, home, cwd)
}

func serve() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

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

// healthcheck probes GET /health on the configured port, for the service
// manager and the container health check.
func healthcheck() error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	host := cfg.Host
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	url := "http://" + net.JoinHostPort(host, strconv.Itoa(cfg.Port)) + "/health"
	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "NeoRouter is not healthy: %s\n", err)
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		err := fmt.Errorf("health check returned %s", resp.Status)
		fmt.Fprintf(os.Stderr, "NeoRouter is not healthy: %s\n", err)
		return err
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

// logLevel maps NEOROUTER_LOG_LEVEL (shared/logger.js levels) to slog.
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
