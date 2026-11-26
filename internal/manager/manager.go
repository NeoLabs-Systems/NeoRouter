// Package manager implements NeoRouter's command-line management: install,
// service control, status and diagnostics, backups, settings, updates and the
// migration of an existing Node.js installation.
//
// Terminal work (palette, prompts, step lists), service definitions, release
// channels, verified downloads and the update steps come from neolabs-go.
package manager

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/cli"
	"github.com/NeoLabs-Systems/neolabs-go/cli/envfile"
	"github.com/NeoLabs-Systems/neolabs-go/cli/release"
	"github.com/NeoLabs-Systems/neolabs-go/cli/service"
	"github.com/NeoLabs-Systems/neolabs-go/dbx"
	"github.com/NeoLabs-Systems/neolabs-go/runtimecfg"
	"github.com/NeoLabs-Systems/neolabs-go/ssrf"
	"github.com/NeoLabs-Systems/neorouter/internal/backup"
	"github.com/NeoLabs-Systems/neorouter/internal/config"
	"github.com/NeoLabs-Systems/neorouter/internal/updates"
)

// DefaultPort is the port an install listens on until NEOROUTER_PORT says
// otherwise.
const DefaultPort = 4600

// CommandRunner runs an external command in dir and returns its combined
// output. Tests replace it, so no test ever needs Flutter.
type CommandRunner func(ctx context.Context, dir, name string, args ...string) (string, error)

// Options are what the binary gives the manager: the one thing it cannot do
// itself is run the server in the foreground.
type Options struct {
	// Serve runs the server until the context is cancelled: `neorouter serve`,
	// `neorouter start --foreground` and `neorouter install` in a container.
	Serve func(ctx context.Context) error
	// Getenv, UserHome and Cwd default to the process's own.
	Getenv   func(string) string
	UserHome string
	Cwd      string
}

// Manager is one run of the CLI over one installation.
type Manager struct {
	Getenv   func(string) string
	UserHome string
	Cwd      string
	Version  string
	In       io.Reader
	Out, Err io.Writer
	UI       *cli.UI
	// HTTP fetches release metadata and the prebuilt web client.
	HTTP *http.Client
	// ReleaseAPIBase points the release client at another GitHub API root.
	ReleaseAPIBase string
	// AllowHTTPDownloads permits plain http downloads (tests only).
	AllowHTTPDownloads bool
	// RuntimePublicKey overrides the embedded runtime verification key.
	RuntimePublicKey string
	// RuntimeStartupTimeout and RuntimeRollbackTimeout bound the health checks
	// after an install or update; 0 uses 60s and 30s.
	RuntimeStartupTimeout  time.Duration
	RuntimeRollbackTimeout time.Duration

	// Serve, RunCommand and the factories below are seams: production leaves
	// them nil and the package uses the real thing.
	Serve                func(ctx context.Context) error
	RunCommand           CommandRunner
	ServiceFactory       func(config.Config) (*service.Manager, error)
	ReleaseClientFactory func() *release.Client
}

// New builds a manager for args.
func New(_ []string, opts Options) (*Manager, error) {
	home := opts.UserHome
	if home == "" {
		var err error
		if home, err = os.UserHomeDir(); err != nil {
			return nil, fmt.Errorf("find home directory: %w", err)
		}
	}
	cwd := opts.Cwd
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	getenv := opts.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	ui := cli.NewUI(false)
	ui.In, ui.Out, ui.Err = os.Stdin, os.Stdout, os.Stderr
	version := config.Version
	// An installed runtime is the version this install actually runs; the
	// constant is what the binary itself was built from.
	if cfg, err := LoadConfig(getenv, home, cwd); err == nil {
		if active, err := activeRuntimeVersion(cfg); err == nil && active != "" {
			version = active
		}
	}
	return &Manager{
		Getenv:   getenv,
		UserHome: home,
		Cwd:      cwd,
		Version:  version,
		In:       os.Stdin,
		Out:      os.Stdout,
		Err:      os.Stderr,
		UI:       ui,
		// Runtime archives are large; metadata requests carry their own deadline.
		HTTP:  ssrf.NewClient(ssrf.Options{Timeout: 30 * time.Minute}),
		Serve: opts.Serve,
	}, nil
}

// LoadConfig reads the install's .env without overriding the real environment,
// validates it the way the server does, and points it at the active runtime
// when one is installed.
func LoadConfig(getenv func(string) string, home, cwd string) (config.Config, error) {
	if getenv == nil {
		getenv = os.Getenv
	}
	prelim := config.ResolvePaths(getenv, home)
	if err := runtimecfg.LoadEnvFile(prelim.EnvFile, os.LookupEnv, os.Setenv); err != nil {
		return config.Config{}, fmt.Errorf("load %s: %w", prelim.EnvFile, err)
	}
	cfg, err := config.Load(getenv, home, cwd)
	if err != nil {
		return config.Config{}, err
	}
	return applyRuntimeMarker(cfg), nil
}

// applyRuntimeMarker points the install at the runtime version that is active.
// Nothing changes until an update has installed one.
func applyRuntimeMarker(cfg config.Config) config.Config {
	dir, err := activeRuntimeDir(cfg)
	if err != nil {
		return cfg
	}
	cfg.AppDir = runtimeAppDir(dir)
	cfg.WebDir = config.WebClientDir(cfg.AppDir)
	cfg.Landing = config.LandingDir(cfg.AppDir)
	return cfg
}

// activeRuntimeDir is the directory of the active runtime version, or an error
// when no complete runtime is active.
func activeRuntimeDir(cfg config.Config) (string, error) {
	store := RuntimeStore(cfg)
	marker, err := store.Current()
	if err != nil {
		return "", err
	}
	return store.RuntimeDir(marker.Version)
}

// loadConfig is LoadConfig for this manager.
func (m *Manager) loadConfig() (config.Config, error) {
	return LoadConfig(m.Getenv, m.UserHome, m.Cwd)
}

// configWithPaths loads the configuration and makes sure the install's private
// directories exist.
func (m *Manager) configWithPaths() (config.Config, error) {
	cfg, err := m.loadConfig()
	if err != nil {
		return cfg, err
	}
	return cfg, cfg.Paths.Ensure()
}

// withDB opens the install's database for one command: the schema is created or
// verified first, exactly as the server does on start.
func (m *Manager) withDB(ctx context.Context, fn func(context.Context, *dbx.DB, config.Config) error) error {
	cfg, err := m.configWithPaths()
	if err != nil {
		return err
	}
	if m.legacyInstall(cfg).NeedsMigration {
		return errUpgradeFirst
	}
	db, _, err := m.openDB(ctx, cfg)
	if err != nil {
		return err
	}
	defer db.Close()
	return fn(ctx, db, cfg)
}

// openDB opens the database, taking a snapshot first when its schema is about
// to change; the snapshot is nil otherwise.
func (m *Manager) openDB(ctx context.Context, cfg config.Config) (*dbx.DB, *backup.Artifact, error) {
	return backup.OpenDatabase(ctx, backup.InstallOf(cfg.Paths), dbx.Options{})
}

// errUserNotFound is the text every account command prints for an unknown user.
var errUserNotFound = errors.New("User not found.")

// userByName finds an account by username or email, case-insensitively, the way
// `reset-password` and `providers import` do.
func userByName(ctx context.Context, db *dbx.DB, account string) (id, username string, err error) {
	row := db.R.QueryRowContext(ctx,
		`SELECT id, username FROM users WHERE username=? COLLATE NOCASE OR email=? COLLATE NOCASE`, account, account)
	switch err := row.Scan(&id, &username); {
	case errors.Is(err, sql.ErrNoRows):
		return "", "", errUserNotFound
	case err != nil:
		return "", "", fmt.Errorf("manager: find user: %w", err)
	}
	return id, username, nil
}

// commands is the CLI surface; `serve` is left to the binary because it owns
// the server's run loop.
func (m *Manager) commands() []cli.Command {
	return []cli.Command{
		{Name: "install", Run: m.cmdInstall},
		{Name: "setup", Run: m.cmdSetup},
		{Name: "start", Run: m.cmdStart},
		{Name: "stop", Run: m.cmdStop},
		{Name: "restart", Run: m.cmdRestart},
		{Name: "status", Run: m.cmdStatus},
		{Name: "doctor", Run: m.cmdDoctor},
		{Name: "logs", Run: m.cmdLogs},
		{Name: "backup", Run: m.cmdBackup},
		{Name: "restore", Run: m.cmdRestore},
		{Name: "rebuild-web", Run: m.cmdRebuildWeb},
		{Name: "repair", Aliases: []string{"fix"}, Run: m.cmdRepair},
		{Name: "uninstall", Run: m.cmdUninstall},
		{Name: "channel", Run: m.cmdChannel},
		{Name: "update", Run: m.cmdUpdate},
		{Name: "env", Run: m.cmdEnv},
		{Name: "reset-password", Run: m.cmdResetPassword},
		{Name: "reset-2fa", Run: m.cmdReset2FA},
		{Name: "providers", Run: m.cmdProviders},
		{Name: "serve", Aliases: []string{"run"}},
		{Name: "healthcheck", Run: m.cmdHealthcheck},
		{Name: "version", Aliases: []string{"--version", "-V", "-v"}, Run: m.cmdVersion},
		{Name: "help", Aliases: []string{"--help", "-h"}, Run: m.cmdHelp},
	}
}

// Run runs one neorouter command over a fresh manager, or the home screen when
// there is none.
func Run(ctx context.Context, args []string, opts Options) error {
	m, err := New(args, opts)
	if err != nil {
		return err
	}
	return m.Run(ctx, args)
}

// Run runs one neorouter command, or the home screen when there is none. Each
// command loads what it needs, so a bad value in .env cannot stop `env`, `help`
// or `version`.
func (m *Manager) Run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		cfg, err := m.configWithPaths()
		if err != nil {
			return err
		}
		return m.cmdHome(ctx, cfg)
	}
	// `serve` and `run` start the server, which only the binary can do.
	command := cli.Find(m.commands(), args[0])
	if command == nil || (command.Run == nil && command.Name != "serve") {
		return inputError(fmt.Sprintf("There's no neorouter %s command.", args[0]))
	}
	if helpRequested(command.Name, args[1:]) {
		m.printHelp(true)
		return nil
	}
	if err := checkArgs(command.Name, args[1:]); err != nil {
		return err
	}
	if command.Name == "serve" {
		return m.serve(ctx)
	}
	return command.Run(ctx, args[1:])
}

// serve runs the server in the foreground.
func (m *Manager) serve(ctx context.Context) error {
	if err := m.ensureEnvFile(""); err != nil {
		return err
	}
	if m.Serve == nil {
		return errors.New("manager: this build cannot run the server in the foreground")
	}
	m.UI.Title("NeoRouter is starting", "Press Ctrl+C to stop it.")
	m.UI.Blank()
	return m.Serve(ctx)
}

// ensureEnvFile creates the install's .env when there is none, recording the
// release channel. An existing file is left exactly as it is.
func (m *Manager) ensureEnvFile(channel string) error {
	paths := m.installPaths()
	if err := paths.Ensure(); err != nil {
		return err
	}
	if exists(paths.EnvFile) {
		return nil
	}
	content := []byte(fmt.Sprintf("# NeoRouter local configuration\nNEOROUTER_HOST=127.0.0.1\nNEOROUTER_PORT=%d\n", DefaultPort))
	if err := envfile.WriteAtomic(paths.EnvFile, content, 0o600); err != nil {
		return err
	}
	if channel == "" {
		channel = updates.NormalizeChannel(m.Getenv(updates.ChannelEnvKey))
	}
	return m.setChannel(channel)
}

// setChannel records the release channel for this and later runs.
func (m *Manager) setChannel(channel string) error {
	if _, err := updates.WriteChannelToEnvFile(channel, m.installPaths().EnvFile); err != nil {
		return err
	}
	return os.Setenv(updates.ChannelEnvKey, channel)
}

// ensureDefaultEnvFile is ensureEnvFile followed by loading the configuration.
func (m *Manager) ensureDefaultEnvFile() (config.Config, error) {
	if err := m.ensureEnvFile(""); err != nil {
		return config.Config{}, err
	}
	return m.configWithPaths()
}

// currentChannel is the channel this install follows right now.
func (m *Manager) currentChannel(cfg config.Config) string {
	return updates.ReadConfiguredChannel(m.Getenv, cfg.Paths.EnvFile)
}

// loadEnvPort is the port the install listens on.
func (m *Manager) loadEnvPort() int {
	cfg, err := m.loadConfig()
	if err == nil && cfg.Port > 0 {
		return cfg.Port
	}
	return DefaultPort
}

// appURL is where people open the app.
func appURL(port int) string { return fmt.Sprintf("http://localhost:%d/app", port) }

// inContainer reports whether this process runs inside a container, where the
// image is the unit of deployment and there is nothing to install or update.
func (m *Manager) inContainer() bool { return updates.DetectContainer(m.Getenv) }

// commandExists reports whether name is on PATH.
func (m *Manager) commandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// run runs an external command through the manager's runner.
func (m *Manager) run(ctx context.Context, dir, name string, args ...string) (string, error) {
	if m.RunCommand != nil {
		return m.RunCommand(ctx, dir, name, args...)
	}
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// serviceManager builds the launchd agent / systemd user unit / detached
// process of this install.
func (m *Manager) serviceManager(cfg config.Config) (*service.Manager, error) {
	if m.ServiceFactory != nil {
		return m.ServiceFactory(cfg)
	}
	return NewServiceManager(cfg, m.currentBinary(), m.Getenv("PATH"))
}

// currentBinary is the binary a service runs: the active runtime's, or the one
// currently executing.
func (m *Manager) currentBinary() string {
	if cfg, err := m.loadConfig(); err == nil {
		if dir, err := activeRuntimeDir(cfg); err == nil {
			if binary := runtimeBinary(dir); exists(binary) {
				return binary
			}
		}
	}
	if exe, err := os.Executable(); err == nil {
		return exe
	}
	return AppCommand
}

// NewServiceManager describes the service of one install.
func NewServiceManager(cfg config.Config, binary, currentPath string) (*service.Manager, error) {
	workDir := cfg.AppDir
	if workDir == "" {
		workDir = cfg.Paths.Home
	}
	env := []service.EnvVar{
		{Key: "NEOROUTER_HOME", Value: cfg.Paths.Home},
		{Key: "PATH", Value: ServicePath(currentPath, binary)},
	}
	// The app directory carries the web client and the landing page, and after an
	// update it is the runtime version the service runs from.
	if cfg.AppDir != "" {
		env = append(env, service.EnvVar{Key: "NEOROUTER_APP_DIR", Value: cfg.AppDir})
	}
	spec := service.Spec{
		Label:       config.ServiceLabel,
		Name:        AppCommand,
		TaskName:    "NeoRouter",
		Description: "NeoRouter inference router",
		Program:     binary,
		Args:        []string{"serve"},
		WorkDir:     workDir,
		Env:         env,
		EnvFile:     cfg.Paths.EnvFile,
		LogDir:      cfg.Paths.Logs,
		LogName:     AppCommand,
		PIDFile:     cfg.Paths.PIDFile,
	}
	return service.New(spec, Platform())
}

// ServicePath: the service starts with the bare PATH of launchd or systemd, so the binary's own directory and the
// usual install locations are appended to what is there.
func ServicePath(current, binary string) string {
	var dirs []string
	for _, dir := range filepath.SplitList(current) {
		if dir != "" {
			dirs = append(dirs, dir)
		}
	}
	wanted := []string{filepath.Dir(binary)}
	if Platform() != "windows" {
		wanted = append(wanted, "/opt/homebrew/bin", "/usr/local/bin", "/usr/bin", "/bin")
	}
	seen := make(map[string]bool, len(dirs))
	for _, dir := range dirs {
		seen[dir] = true
	}
	for _, dir := range wanted {
		if dir != "" && dir != "." && !seen[dir] {
			seen[dir] = true
			dirs = append(dirs, dir)
		}
	}
	return strings.Join(dirs, string(os.PathListSeparator))
}

// exists reports whether path exists.
func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// evidence is one sign that NeoRouter already lives on this computer.
type evidence struct{ Kind, Path string }

// installedInstance is what was found of an installation.
type installedInstance struct {
	Installed bool
	Evidence  []evidence
}

func (m *Manager) scanForInstalledInstance(cfg config.Config) installedInstance {
	var out installedInstance
	add := func(kind, path string) { out.Evidence = append(out.Evidence, evidence{Kind: kind, Path: path}) }
	if exists(cfg.Paths.EnvFile) {
		add("config", cfg.Paths.EnvFile)
	}
	if exists(config.LaunchAgentPlistPath(m.UserHome)) {
		add("launchd", config.LaunchAgentPlistPath(m.UserHome))
	}
	if exists(config.SystemdUnitPath(m.UserHome)) {
		add("systemd", config.SystemdUnitPath(m.UserHome))
	}
	if exists(cfg.Paths.PIDFile) {
		add("pid", cfg.Paths.PIDFile)
	}
	if HasBundledWebClient(cfg.WebDir) {
		add("web", cfg.WebDir)
	}
	out.Installed = len(out.Evidence) > 0
	return out
}
