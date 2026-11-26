package manager

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/cli"
	"github.com/NeoLabs-Systems/neorouter/internal/config"
	"github.com/NeoLabs-Systems/neorouter/internal/updates"
)

// cmdInstall is `neorouter install`: prepare the install, migrate an existing
// Node.js one, start the service and wait until the server answers.
func (m *Manager) cmdInstall(ctx context.Context, _ []string) error {
	m.printSetupProgress(0)
	m.UI.Title("Welcome to NeoRouter", "Let's get it running on this computer. It can take a few minutes.")
	m.UI.Blank()
	cfg, err := m.ensureDefaultEnvFile()
	if err != nil {
		return err
	}
	if cfg, err = m.migrateLegacyIfNeeded(ctx, cfg); err != nil {
		return err
	}
	port := m.loadEnvPort()
	steps := []cli.Step{
		{Label: cli.Label{Doing: "Preparing NeoRouter's files", Done: "Prepared NeoRouter's files"}, Run: func() error {
			return m.prepareServerFiles(cfg)
		}},
		{Label: cli.Label{Doing: "Preparing the app", Done: "Prepared the app"}, Run: func() error {
			return m.ensureWebClient(ctx, cfg, true)
		}},
		{Label: cli.Label{Doing: "Updating the database", Done: "Updated the database"}, Run: func() error {
			return m.migrateDatabase(ctx)
		}},
	}
	var link *CommandLink
	if m.needsCommandLink(cfg) {
		steps = append(steps, cli.Step{
			Label: cli.Label{Doing: "Adding the neorouter command", Done: "Added the neorouter command"},
			Run:   func() error { link = m.linkCommand(); return nil },
		})
	}
	if m.inContainer() {
		// Nothing to install: the image already holds the code and the client,
		// and the container runtime supervises the process.
		if err := m.UI.RunSteps(true, steps...); err != nil {
			return err
		}
		m.UI.OK("Container detected: starting the server in the foreground")
		return m.serve(ctx)
	}
	var serviceKind string
	steps = append(steps,
		cli.Step{Label: cli.Label{Doing: "Starting NeoRouter", Done: "Started NeoRouter"}, Run: func() error {
			kind, ierr := m.installService(cfg)
			serviceKind = kind
			return ierr
		}},
		cli.Step{Label: cli.Label{Doing: "Checking it's answering", Done: "It's answering"}, Run: func() error {
			return m.waitForServer(ctx, port, 30*time.Second)
		}},
	)
	if err := m.UI.RunSteps(true, steps...); err != nil {
		return err
	}
	url := appURL(port)
	m.printSetupProgress(2)
	notes := make([]string, 0, 3)
	notes = append(notes, "Add your providers and create a router key in the app.")
	notes = append(notes, fmt.Sprintf("Point your apps at %s, the OpenAI-compatible address.",
		m.UI.Accent(fmt.Sprintf("http://localhost:%d/v1", port))))
	if note := m.commandNote(link); note != "" {
		notes = append(notes, note)
	}
	m.printSetupFinished(url, serviceKind != "detached", m.hasAccount(ctx), notes)
	if !m.UI.Interactive() {
		return nil
	}
	m.UI.Keys([2]string{"enter", "open it"}, [2]string{"q", "done"})
	pressed, err := m.UI.WaitForKey("return", "q", "escape")
	if err != nil {
		return err
	}
	if pressed == "return" {
		return openInBrowser(url)
	}
	return nil
}

// prepareServerFiles is the install's first step: the files the service runs
// must be complete. A Go install carries them in the binary, so only an
// installed runtime can be incomplete.
func (m *Manager) prepareServerFiles(cfg config.Config) error {
	return m.runtimeProblem(cfg)
}

// installService installs and starts the background service, and reports which
// kind it installed: launchd, systemd or a detached process.
func (m *Manager) installService(cfg config.Config) (string, error) {
	if m.inContainer() {
		return "", errors.New("NeoRouter runs inside a container. Start it with `neorouter serve` " +
			"(the image default) and let Docker, Compose or Kubernetes manage the lifecycle.")
	}
	sm, err := m.serviceManager(cfg)
	if err != nil {
		return "", err
	}
	sm.Logf = func(level, message string) {
		if level == "warn" {
			m.UI.Warn(message)
			return
		}
		m.UI.Info(message)
	}
	if err := sm.Install(); err != nil {
		if sm.Installed() {
			// The unit is on disk but nothing runs it; a detached process still
			// keeps NeoRouter up.
			m.UI.Warn("Couldn't set up the system service, so NeoRouter won't restart by itself")
			m.UI.Paragraph(err.Error(), 4, m.UI.Muted)
			if ferr := sm.StartFallback(); ferr != nil {
				return "", ferr
			}
			return "detached", nil
		}
		return "", err
	}
	return m.serviceKind(), nil
}

// serviceKind names the service manager this platform installs.
func (m *Manager) serviceKind() string {
	switch {
	case Platform() == "macos" && m.commandExists("launchctl"):
		return "launchd"
	case Platform() == "linux" && m.commandExists("systemctl"):
		return "systemd"
	}
	return "detached"
}

// hasAccount reports whether the install has any user yet, so the last install
// screen can say "sign in" instead of "create your account".
func (m *Manager) hasAccount(ctx context.Context) bool {
	cfg, err := m.loadConfig()
	if err != nil {
		return false
	}
	db, err := m.openDB(ctx, cfg)
	if err != nil {
		return false
	}
	defer db.Close()
	var one int
	return db.R.QueryRowContext(ctx, `SELECT 1 FROM users LIMIT 1`).Scan(&one) == nil
}

// cmdSetup is `neorouter setup [--channel stable|beta]`: prepare the database,
// the settings and the app, without installing or starting the service.
func (m *Manager) cmdSetup(ctx context.Context, args []string) error {
	cfg, err := m.ensureDefaultEnvFile()
	if err != nil {
		return err
	}
	requested, err := requestedChannel(args)
	if err != nil {
		return err
	}
	channel := requested
	if channel == "" {
		channel = m.currentChannel(cfg)
	}
	if requested != "" {
		if _, werr := updates.WriteChannelToEnvFile(requested, cfg.Paths.EnvFile); werr != nil {
			return werr
		}
		_ = os.Setenv(updates.ChannelEnvKey, requested)
	}
	if cfg, err = m.migrateLegacyIfNeeded(ctx, cfg); err != nil {
		return err
	}
	m.UI.Title("Setting up NeoRouter", fmt.Sprintf("Updates come from the %s channel.", channel))
	m.UI.Blank()
	err = m.UI.RunSteps(false,
		cli.Step{Label: cli.Label{Doing: "Preparing NeoRouter's files", Done: "Prepared NeoRouter's files"}, Run: func() error {
			return m.prepareServerFiles(cfg)
		}},
		cli.Step{Label: cli.Label{Doing: "Updating the database", Done: "Updated the database"}, Run: func() error {
			return m.migrateDatabase(ctx)
		}},
		cli.Step{Label: cli.Label{Doing: "Preparing the app", Done: "Prepared the app"}, Run: func() error {
			return m.ensureWebClient(ctx, cfg, true)
		}},
	)
	if err != nil {
		return err
	}
	m.UI.Blank()
	m.UI.Item("ok", m.UI.Bold("Setup complete"), cfg.Paths.Home)
	if m.inContainer() {
		m.UI.Note(fmt.Sprintf("Next, run %s to start the server.", m.UI.Command("neorouter serve")))
	} else {
		m.UI.Note(fmt.Sprintf("Next, run %s to start NeoRouter on this computer.", m.UI.Command("neorouter install")))
	}
	m.UI.Blank()
	return nil
}

// requestedChannel reads `setup --channel beta`, `setup --channel=beta` or a
// bare `setup beta`.
func requestedChannel(args []string) (string, error) {
	value, has := cli.FlagValue(args, "--channel")
	if has && value == "" {
		return "", inputError("Usage: neorouter setup [--channel stable|beta]")
	}
	if !has {
		for _, arg := range cli.Positionals(args, "--channel") {
			value = arg
			has = true
			break
		}
	}
	if !has {
		return "", nil
	}
	channel := updates.ParseChannel(value)
	if channel == "" {
		return "", inputError("Usage: neorouter setup [--channel stable|beta]")
	}
	return channel, nil
}

// cmdRepair is `neorouter repair` (alias `fix`): reinstall what is missing, redo
// the database and restart.
func (m *Manager) cmdRepair(ctx context.Context, _ []string) error {
	m.UI.Title("Repairing NeoRouter", "Your data stays where it is. This can take a few minutes.")
	m.UI.Blank()
	cfg, err := m.ensureDefaultEnvFile()
	if err != nil {
		return err
	}
	if cfg, err = m.migrateLegacyIfNeeded(ctx, cfg); err != nil {
		return err
	}
	steps := []cli.Step{
		{Label: cli.Label{Doing: "Reinstalling what it needs", Done: "Reinstalled what it needs"}, Run: func() error {
			return m.prepareServerFiles(cfg)
		}},
		{Label: cli.Label{Doing: "Preparing the app", Done: "Prepared the app"}, Run: func() error {
			return m.ensureWebClient(ctx, cfg, true)
		}},
		{Label: cli.Label{Doing: "Updating the database", Done: "Updated the database"}, Run: func() error {
			return m.migrateDatabase(ctx)
		}},
	}
	var link *CommandLink
	if m.needsCommandLink(cfg) {
		steps = append(steps, cli.Step{
			Label: cli.Label{Doing: "Adding the neorouter command", Done: "Added the neorouter command"},
			Run:   func() error { link = m.linkCommand(); return nil },
		})
	}
	if m.inContainer() {
		// No service to reinstall; the container runtime restarts the process.
		if err := m.UI.RunSteps(true, steps...); err != nil {
			return err
		}
		m.UI.Blank()
		m.UI.OK("Repair complete. Restart the container to pick up the changes.")
		m.UI.Blank()
		return nil
	}
	steps = append(steps,
		cli.Step{Label: cli.Label{Doing: "Starting NeoRouter", Done: "Started NeoRouter"}, Run: func() error {
			_, ierr := m.installService(cfg)
			return ierr
		}},
		cli.Step{Label: cli.Label{Doing: "Checking it's answering", Done: "It's answering"}, Run: func() error {
			return m.waitForServer(ctx, m.loadEnvPort(), 30*time.Second)
		}},
	)
	if err := m.UI.RunSteps(true, steps...); err != nil {
		return err
	}
	m.printRunning("Repair complete.")
	if note := m.commandNote(link); note != "" {
		m.UI.Item("info", note, "")
	}
	m.UI.Note(fmt.Sprintf("Check on it any time with %s.", m.UI.Command("neorouter status")))
	m.UI.Blank()
	return nil
}

// migrateLegacyIfNeeded finishes migrating a Node.js install before anything
// starts. It is the one place `install`, `setup`, `repair` and `update` agree
// on: stop the old server, back the data up, re-seal the database, write the
// marker.
func (m *Manager) migrateLegacyIfNeeded(ctx context.Context, cfg config.Config) (config.Config, error) {
	legacy := InspectLegacyJS(cfg, m.UserHome, m.Getenv)
	// The marker is the contract: a migration runs once, however many commands
	// run afterwards. A legacy service file is replaced by installService.
	if !legacy.NeedsMigration || legacy.MigratedMarker {
		return cfg, nil
	}
	m.UI.Info("Detected an existing NeoRouter installation; keeping your data, keys and settings.")
	sm, _ := m.serviceManager(cfg)
	m.stopRunningProcesses(cfg, sm)
	backupKey, err := m.migrateLegacyJSState(ctx, cfg, legacy)
	if err != nil {
		return cfg, err
	}
	note := "migrated from the Node.js installation"
	if err := WriteMigratedMarker(cfg, m.Version, backupKey, note); err != nil {
		return cfg, err
	}
	return m.loadConfig()
}
