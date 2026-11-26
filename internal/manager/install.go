package manager

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/cli"
	"github.com/NeoLabs-Systems/neolabs-go/cli/release"
	"github.com/NeoLabs-Systems/neolabs-go/cli/update"
	"github.com/NeoLabs-Systems/neorouter/internal/backup"
	"github.com/NeoLabs-Systems/neorouter/internal/config"
	"github.com/NeoLabs-Systems/neorouter/internal/updates"
)

// cmdInstall is `neorouter install [--channel stable|beta]`: prepare the
// install, upgrade a Node.js one, start the service and wait until the server
// answers.
func (m *Manager) cmdInstall(ctx context.Context, args []string) error {
	requested, err := channelArg("install", args)
	if err != nil {
		return err
	}
	if m.inContainer() {
		// Nothing to install: the image already holds the code and the app, and
		// the container runtime supervises the process.
		if err := m.ensureEnvFile(requested); err != nil {
			return err
		}
		m.UI.OK("Container detected: starting the server in the foreground")
		return m.serve(ctx)
	}
	m.printSetupProgress(0)
	m.UI.Title("Welcome to NeoRouter", "Let's get it running on this computer. It can take a few minutes.")
	m.UI.Blank()
	serviceKind, link, err := m.switchOver(ctx, requested)
	if err != nil {
		return err
	}
	port := m.loadEnvPort()
	url := appURL(port)
	m.printSetupProgress(2)
	notes := []string{
		"Add your providers and create a router key in the app.",
		fmt.Sprintf("Point your apps at %s, the OpenAI-compatible address.",
			m.UI.Accent(fmt.Sprintf("http://localhost:%d/v1", port))),
	}
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

// switchOver makes the service run this install: it downloads the app first,
// then stops the old server, saves what is about to change, and starts the new
// one. When anything fails after that, everything is put back and the old server
// started again.
func (m *Manager) switchOver(ctx context.Context, requested string) (serviceKind string, link *CommandLink, err error) {
	cfg, err := m.loadConfig()
	if err != nil {
		return "", nil, err
	}
	legacy := m.legacyInstall(cfg)
	channel := requested
	if channel == "" {
		channel = m.currentChannel(cfg)
		if recorded, rerr := m.channelRecorded(cfg); rerr != nil {
			return "", nil, rerr
		} else if legacy.NeedsMigration && !recorded {
			channel = nodeDefaultChannel
			requested = channel
		}
	}
	if err := cfg.Paths.Ensure(); err != nil {
		return "", nil, err
	}
	// Nothing about the running install changes until the app is downloaded.
	staged, err := m.stageRuntime(ctx, cfg, channel, legacy)
	if err != nil {
		return "", nil, err
	}
	snap, err := m.snapshotInstall(cfg, legacy)
	if err != nil {
		return "", nil, err
	}
	if legacy.NeedsMigration {
		m.UI.Info("Detected an existing NeoRouter installation; keeping your data, keys and settings.")
	}
	steps := []cli.Step{
		{Label: cli.Label{Doing: "Saving your settings and data", Done: "Saved your settings and data"}, Run: func() error {
			if err := m.freePort(ctx, cfg); err != nil {
				return err
			}
			if !legacy.NeedsMigration {
				return nil
			}
			item, err := backup.Create(ctx, backup.InstallOf(cfg.Paths), backup.KindMigration)
			if err != nil {
				return err
			}
			snap.data = &item
			return nil
		}},
		{Label: cli.Label{Doing: "Preparing NeoRouter's files", Done: "Prepared NeoRouter's files"}, Run: func() error {
			if err := m.ensureEnvFile(channel); err != nil {
				return err
			}
			if requested != "" {
				if err := m.setChannel(requested); err != nil {
					return err
				}
			}
			if legacy.NeedsMigration {
				if err := m.clearNodeAppDir(cfg); err != nil {
					return err
				}
			}
			if staged != "" {
				if err := RuntimeStore(cfg).Activate(staged); err != nil {
					return err
				}
			}
			if cfg, err = m.loadConfig(); err != nil {
				return err
			}
			return m.runtimeProblem(cfg)
		}},
		{Label: cli.Label{Doing: "Updating the database", Done: "Updated the database"}, Run: func() error {
			if legacy.NeedsMigration {
				return m.upgradeDatabase(ctx, cfg)
			}
			saved, err := m.migrateDatabase(ctx, cfg)
			snap.database = saved
			return err
		}},
		{Label: cli.Label{Doing: "Adding the neorouter command", Done: "Added the neorouter command"}, Run: func() error {
			link = m.installCommand(cfg, legacy)
			return nil
		}},
		{Label: cli.Label{Doing: "Starting NeoRouter", Done: "Started NeoRouter"}, Run: func() error {
			serviceKind, err = m.installService(cfg)
			return err
		}},
		{Label: cli.Label{Doing: "Checking it's answering", Done: "It's answering"}, Run: func() error {
			return m.waitForServerVersion(ctx, cfg.Port, m.expectedVersion(cfg), m.startupTimeout())
		}},
	}
	if err := m.UI.RunSteps(true, steps...); err != nil {
		m.UI.Warn("Putting everything back the way it was")
		if rerr := m.restoreInstall(snap); rerr != nil {
			hint := ""
			if snap.data != nil {
				hint = " Your data from before is in " + snap.data.Path + "."
			}
			return "", nil, fmt.Errorf("%w\nPutting it back also failed: %w.%s", err, rerr, hint)
		}
		return "", nil, err
	}
	if legacy.NeedsMigration {
		if err := WriteMigratedMarker(cfg, m.expectedVersion(cfg), snap.data.Key); err != nil {
			return "", nil, err
		}
	}
	m.Version = m.expectedVersion(cfg)
	return serviceKind, link, nil
}

// channelRecorded reports whether the install says which channel it follows.
func (m *Manager) channelRecorded(cfg config.Config) (bool, error) {
	if m.Getenv(updates.ChannelEnvKey) != "" {
		return true, nil
	}
	values, err := config.EnvFileValues(cfg.Paths.EnvFile)
	return values[updates.ChannelEnvKey] != "", err
}

// stageRuntime downloads the signed runtime an install without one needs: the
// app, the landing page and the server binary. It does not activate it.
func (m *Manager) stageRuntime(ctx context.Context, cfg config.Config, channel string, legacy LegacyJSState) (string, error) {
	store := RuntimeStore(cfg)
	if _, err := store.Current(); err == nil {
		return "", nil
	}
	marker := filepath.Join(store.Root, "current.json")
	broken := exists(marker)
	if !broken {
		if config.IsSourceCheckout(cfg.AppDir) {
			return "", nil
		}
		// An app folder someone pointed NeoRouter at is used as it is.
		if !legacy.NeedsMigration && !isNodeCheckout(cfg.AppDir) && HasBundledWebClient(cfg.WebDir) {
			return "", nil
		}
	}
	source, candidate, err := m.findRuntime(ctx, channel)
	if err != nil {
		return "", err
	}
	err = m.UI.Step("Downloading the verified NeoRouter release", "Downloaded the verified NeoRouter release", func() error {
		archive, err := source.Download(ctx, store, candidate)
		if err != nil {
			return err
		}
		// A runtime with missing files cannot be replaced while it is active.
		if broken {
			if err := os.Remove(marker); err != nil {
				return err
			}
		}
		return update.Unpack(store, candidate.Version, archive, false)
	})
	if err != nil {
		return "", err
	}
	return candidate.Version, nil
}

// expectedVersion is the version the service answers with.
func (m *Manager) expectedVersion(cfg config.Config) string {
	if active, err := activeRuntimeVersion(cfg); err == nil {
		return active
	}
	return config.Version
}

// migrateDatabase prepares the database, unless an installed runtime of another
// version serves it: that server does it itself, and this binary's schema may
// be one it cannot read.
func (m *Manager) migrateDatabase(ctx context.Context, cfg config.Config) (*backup.Artifact, error) {
	if active, err := activeRuntimeVersion(cfg); err == nil && release.CompareVersions(active, config.Version) != 0 {
		return nil, nil
	}
	db, saved, err := m.openDB(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return saved, db.Close()
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
	db, _, err := m.openDB(ctx, cfg)
	if err != nil {
		return false
	}
	defer db.Close()
	var one int
	return db.R.QueryRowContext(ctx, `SELECT 1 FROM users LIMIT 1`).Scan(&one) == nil
}

// cmdSetup is `neorouter setup [--channel stable|beta]`: prepare the settings,
// the app and the database, without installing or starting the service.
func (m *Manager) cmdSetup(ctx context.Context, args []string) error {
	requested, err := channelArg("setup", args)
	if err != nil {
		return err
	}
	cfg, err := m.loadConfig()
	if err != nil {
		return err
	}
	// An upgrade is only finished once the new server answers, which setup never
	// starts.
	legacy := m.legacyInstall(cfg)
	if legacy.NeedsMigration {
		return errUpgradeFirst
	}
	if cfg, err = m.ensureDefaultEnvFile(); err != nil {
		return err
	}
	if requested != "" {
		if err := m.setChannel(requested); err != nil {
			return err
		}
	}
	channel := m.currentChannel(cfg)
	m.UI.Title("Setting up NeoRouter", fmt.Sprintf("Updates come from the %s channel.", channel))
	m.UI.Blank()
	staged, err := m.stageRuntime(ctx, cfg, channel, legacy)
	if err != nil {
		return err
	}
	err = m.UI.RunSteps(false,
		cli.Step{Label: cli.Label{Doing: "Preparing NeoRouter's files", Done: "Prepared NeoRouter's files"}, Run: func() error {
			if staged != "" {
				if err := RuntimeStore(cfg).Activate(staged); err != nil {
					return err
				}
				if cfg, err = m.loadConfig(); err != nil {
					return err
				}
			}
			return m.runtimeProblem(cfg)
		}},
		cli.Step{Label: cli.Label{Doing: "Updating the database", Done: "Updated the database"}, Run: func() error {
			_, err := m.migrateDatabase(ctx, cfg)
			return err
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

// cmdRepair is `neorouter repair` (alias `fix`): download what is missing,
// reinstall the service and restart.
func (m *Manager) cmdRepair(ctx context.Context, _ []string) error {
	m.UI.Title("Repairing NeoRouter", "Your data stays where it is. This can take a few minutes.")
	m.UI.Blank()
	if m.inContainer() {
		cfg, err := m.ensureDefaultEnvFile()
		if err != nil {
			return err
		}
		if _, err := m.migrateDatabase(ctx, cfg); err != nil {
			return err
		}
		m.UI.OK("Repair complete. Restart the container to pick up the changes.")
		m.UI.Blank()
		return nil
	}
	_, link, err := m.switchOver(ctx, "")
	if err != nil {
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

// waitForServerVersion waits until the server reports version on port.
func (m *Manager) waitForServerVersion(ctx context.Context, port int, version string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if health, ok := fetchHealth(ctx, port); ok {
			if got, _ := health["version"].(string); release.CompareVersions(got, version) == 0 {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
	return cli.Errorf("SERVER_NOT_READY", "NeoRouter did not report version %s on port %d.", version, port)
}
