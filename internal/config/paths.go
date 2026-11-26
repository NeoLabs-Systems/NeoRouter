// Package config loads, validates, and exposes NeoRouter's runtime
// configuration and filesystem layout. Environment variable names, defaults,
// bounds, and error texts match runtime/paths.js, runtime/env.js and
// server/config/env.js so existing .env files and installs keep working.
package config

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/NeoLabs-Systems/neolabs-go/runtimecfg"
)

// ServiceLabel is the launchd/systemd service name of a NeoRouter install.
const ServiceLabel = "systems.neolabs.neorouter"

// Paths holds the private runtime directories (0700) and files (0600) of one
// NeoRouter install, the Go equivalent of runtime/paths.js.
type Paths struct {
	Home             string
	Data             string
	Logs             string
	Backups          string
	Database         string
	EnvFile          string
	SecretKey        string
	PIDFile          string
	UpdateStatusFile string
}

// expandHome resolves NEOROUTER_HOME: empty means ~/.neorouter, "~" is the
// user's home, "~/x" is home/x, and anything else is made absolute.
func expandHome(value, userHome string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return filepath.Join(userHome, ".neorouter")
	}
	if value == "~" {
		return userHome
	}
	if strings.HasPrefix(value, "~"+string(filepath.Separator)) {
		return filepath.Join(userHome, value[2:])
	}
	abs, err := filepath.Abs(value)
	if err != nil {
		return value
	}
	return abs
}

// ResolvePaths computes every runtime path without creating anything.
func ResolvePaths(getenv func(string) string, userHome string) Paths {
	if getenv == nil {
		getenv = os.Getenv
	}
	home := expandHome(getenv("NEOROUTER_HOME"), userHome)
	dbPath := getenv("NEOROUTER_DATABASE_PATH")
	if dbPath == "" {
		dbPath = filepath.Join(home, "data", "neorouter.sqlite3")
	}
	data := filepath.Join(home, "data")
	return Paths{
		Home:             home,
		Data:             data,
		Logs:             filepath.Join(home, "logs"),
		Backups:          filepath.Join(home, "backups"),
		Database:         dbPath,
		EnvFile:          filepath.Join(home, ".env"),
		SecretKey:        filepath.Join(data, "secret.key"),
		PIDFile:          filepath.Join(data, "neorouter.pid"),
		UpdateStatusFile: filepath.Join(data, "update-status.json"),
	}
}

// Ensure creates the private runtime directories with 0700 permissions.
func (p Paths) Ensure() error {
	for _, dir := range []string{p.Home, p.Data, p.Logs, p.Backups} {
		if err := runtimecfg.EnsurePrivateDir(dir); err != nil {
			return err
		}
	}
	return nil
}

// SystemdUnitPath is where a user-level systemd unit for this install lives.
func SystemdUnitPath(home string) string {
	return filepath.Join(home, ".config", "systemd", "user", "neorouter.service")
}

// LaunchAgentPlistPath is where the launchd agent of this install lives.
func LaunchAgentPlistPath(home string) string {
	return filepath.Join(home, "Library", "LaunchAgents", ServiceLabel+".plist")
}

// FlutterAppDir is the Flutter project directory shipped next to the server.
func FlutterAppDir(appDir string) string {
	return filepath.Join(appDir, "flutter_app")
}

// WebClientDir is the built Flutter web client served at /app.
func WebClientDir(appDir string) string {
	return filepath.Join(appDir, "flutter_app", "build", "web")
}

// LandingDir is the static landing site served at /.
func LandingDir(appDir string) string {
	return filepath.Join(appDir, "landing")
}
