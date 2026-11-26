package manager

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/cli"
	"github.com/NeoLabs-Systems/neorouter/internal/config"
)

// waitForServer is lib/manager.js waitForServerReady: the port answers and
// /health says the server is up, or the command fails with SERVER_NOT_READY.
func (m *Manager) waitForServer(ctx context.Context, port int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if cli.PortOpen(port) {
			if _, ok := fetchHealth(ctx, port); ok {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(400 * time.Millisecond):
		}
	}
	return serverNotReadyError(port)
}

// launchService starts whichever service manager this installation uses
// (lib/manager.js launchService).
func (m *Manager) launchService(cfg config.Config) error {
	sm, err := m.serviceManager(cfg)
	if err != nil {
		return err
	}
	return sm.Launch()
}

// stopService asks the service manager and any detached process to stop, and
// reports whether there was anything to stop. Both are asked: an install can
// have a unit on disk and a detached process left over from before it.
func (m *Manager) stopService(cfg config.Config) bool {
	sm, err := m.serviceManager(cfg)
	if err != nil {
		return stopDetached(cfg)
	}
	stopped := sm.Installed() || exists(cfg.Paths.PIDFile)
	sm.Stop()
	if stopDetached(cfg) {
		stopped = true
	}
	return stopped
}

// stopDetached ends the process named by the PID file, if there is one.
func stopDetached(cfg config.Config) bool {
	raw, err := os.ReadFile(cfg.Paths.PIDFile)
	if err != nil {
		return false
	}
	if pid, err := strconv.Atoi(strings.TrimSpace(string(raw))); err == nil && pid > 0 && pid != os.Getpid() {
		if process, ferr := os.FindProcess(pid); ferr == nil {
			_ = process.Signal(syscall.SIGTERM)
			waitGone(process, 8*time.Second)
		}
	}
	_ = os.Remove(cfg.Paths.PIDFile)
	return true
}

// stopAndWait stops NeoRouter and waits for the port to close, so the next
// start's readiness check cannot be answered by the server that is shutting
// down.
func (m *Manager) stopAndWait(ctx context.Context, cfg config.Config) bool {
	stopped := m.stopService(cfg)
	port := cfg.Port
	if port <= 0 {
		port = DefaultPort
	}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) && cli.PortOpen(port) {
		select {
		case <-ctx.Done():
			return stopped
		case <-time.After(250 * time.Millisecond):
		}
	}
	return stopped
}

// printRunning is the one line after a successful start.
func (m *Manager) printRunning(headline string) {
	m.UI.Blank()
	m.UI.Item("ok", m.UI.Bold(headline), "")
	m.UI.Print("    " + m.UI.Accent(appURL(m.loadEnvPort())))
}

// printContainerLifecycle reminds people that the container runtime owns the
// process.
func (m *Manager) printContainerLifecycle(verb string) {
	m.UI.Blank()
	m.UI.Info(fmt.Sprintf("NeoRouter runs in a container: %s it with `docker %s <container>`", verb, verb))
	m.UI.Blank()
}

// cmdStart is `neorouter start`: make sure the app is there, start the service
// and wait for the server to answer.
func (m *Manager) cmdStart(ctx context.Context, args []string) error {
	cfg, err := m.ensureDefaultEnvFile()
	if err != nil {
		return err
	}
	if cli.HasFlag(args, "--foreground") || m.inContainer() {
		// A container has exactly one foreground process; "starting" means
		// running it here instead of handing off to a service manager.
		if m.inContainer() {
			m.UI.Info("Container detected: running in the foreground")
		}
		if err := m.ensureWebClient(ctx, cfg, true); err != nil {
			return err
		}
		return m.serve(ctx)
	}
	port := cfg.Port
	if port <= 0 {
		port = DefaultPort
	}
	err = m.UI.RunSteps(true,
		cli.Step{Label: cli.Label{Doing: "Starting NeoRouter", Done: "Started NeoRouter"}, Run: func() error {
			if err := m.ensureWebClient(ctx, cfg, true); err != nil {
				return err
			}
			return m.launchService(cfg)
		}},
		cli.Step{Label: cli.Label{Doing: "Checking it's answering", Done: "It's answering"}, Run: func() error {
			return m.waitForServer(ctx, port, 30*time.Second)
		}},
	)
	if err != nil {
		return err
	}
	m.printRunning("NeoRouter is running.")
	m.UI.Blank()
	return nil
}

// cmdStop is `neorouter stop`.
func (m *Manager) cmdStop(ctx context.Context, _ []string) error {
	if m.inContainer() {
		m.printContainerLifecycle("stop")
		return nil
	}
	cfg, err := m.configWithPaths()
	if err != nil {
		return err
	}
	stopped := false
	err = m.UI.RunSteps(false, cli.Step{
		Label: cli.Label{Doing: "Stopping NeoRouter", Done: "Stopped NeoRouter"},
		Run:   func() error { stopped = m.stopAndWait(ctx, cfg); return nil },
	})
	if err != nil {
		return err
	}
	m.UI.Blank()
	if stopped {
		m.UI.OK("NeoRouter stopped")
	} else {
		m.UI.Warn("NeoRouter wasn't running")
	}
	m.UI.Blank()
	return nil
}

// cmdRestart is `neorouter restart`: rebuild the app if that is possible, stop,
// start again and wait.
func (m *Manager) cmdRestart(ctx context.Context, _ []string) error {
	if m.inContainer() {
		m.printContainerLifecycle("restart")
		return nil
	}
	startedAt := time.Now()
	cfg, err := m.ensureDefaultEnvFile()
	if err != nil {
		return err
	}
	port := cfg.Port
	if port <= 0 {
		port = DefaultPort
	}
	err = m.UI.RunSteps(true,
		cli.Step{Label: cli.Label{Doing: "Getting the app ready", Done: "Got the app ready"}, Run: func() error {
			return m.ensureWebClient(ctx, cfg, false)
		}},
		cli.Step{Label: cli.Label{Doing: "Stopping NeoRouter", Done: "Stopped NeoRouter"}, Run: func() error {
			m.stopAndWait(ctx, cfg)
			return nil
		}},
		cli.Step{Label: cli.Label{Doing: "Starting it again", Done: "Started it again"}, Run: func() error {
			return m.launchService(cfg)
		}},
		cli.Step{Label: cli.Label{Doing: "Checking it's answering", Done: "It's answering"}, Run: func() error {
			return m.waitForServer(ctx, port, 30*time.Second)
		}},
	)
	if err != nil {
		return err
	}
	m.printRunning("Back online in " + cli.FormatDuration(time.Since(startedAt)) + ".")
	m.UI.Blank()
	return nil
}

// cmdUninstall removes the service definition and the command link, and keeps
// every setting and every byte of data.
func (m *Manager) cmdUninstall(ctx context.Context, _ []string) error {
	if m.inContainer() {
		m.printContainerLifecycle("stop")
		return nil
	}
	cfg, err := m.configWithPaths()
	if err != nil {
		return err
	}
	if m.UI.Interactive() {
		confirmed, cerr := m.UI.Confirm("Stop NeoRouter and stop it starting with your computer? Your data stays.", false)
		if cerr != nil {
			return cerr
		}
		if !confirmed {
			return nil
		}
	}
	m.stopService(cfg)
	sm, err := m.serviceManager(cfg)
	if err != nil {
		return err
	}
	sm.Uninstall()
	_, _ = m.run(ctx, cfg.AppDir, "systemctl", "--user", "daemon-reload")
	unlinked := RemoveCommandLink(m.UserHome)
	m.UI.Blank()
	m.UI.OK("NeoRouter no longer starts with your computer")
	if unlinked {
		m.UI.OK("Removed the neorouter command")
	}
	m.UI.Note(fmt.Sprintf("Your settings and data are still in %s. Turn it back on with %s.",
		cfg.Paths.Home, m.UI.Command("neorouter install")))
	m.UI.Blank()
	return nil
}

// serviceUnitPath is the service definition this platform installs.
func serviceUnitPath(userHome string) string {
	if Platform() == "macos" {
		return config.LaunchAgentPlistPath(userHome)
	}
	return config.SystemdUnitPath(userHome)
}

// serviceUnitContent reads the installed service definition, for the migration
// so it can be put back when an update fails.
func serviceUnitContent(userHome string) (string, []byte) {
	path := serviceUnitPath(userHome)
	raw, err := os.ReadFile(path)
	if err != nil {
		return path, nil
	}
	return path, raw
}
