package manager

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/cli"
	"github.com/NeoLabs-Systems/neorouter/internal/config"
)

// cmdHome is `neorouter` with no arguments (lib/manager.js cmdDefault): install
// when nothing is here yet, otherwise the header and the menu.
func (m *Manager) cmdHome(ctx context.Context, cfg config.Config) error {
	if !m.scanForInstalledInstance(cfg).Installed {
		if exists(cfg.Paths.EnvFile) {
			m.UI.Info(fmt.Sprintf("Your existing settings in %s are kept.", cfg.Paths.EnvFile))
		}
		return m.cmdInstall(ctx, nil)
	}
	facts := m.collectStatusFacts(ctx, cfg, m.UI.Interactive())
	// Running state and updates are in the header and the menu already.
	problems := make([]statusCheck, 0, 3)
	for _, check := range statusChecks(facts) {
		if check.ID != "server" && check.ID != "version" && needsAttention(check) {
			problems = append(problems, check)
		}
	}
	m.printHomeHeader(facts, problems)
	m.UI.Blank()
	return m.UI.HomeMenu(AppCommand, m.homeActions(facts))
}

// homeActions is lib/manager.js homeActions on top of cli.HomeActions.
func (m *Manager) homeActions(facts statusFacts) cli.HomeActions {
	return cli.HomeActions{
		AppName:       "NeoRouter",
		Running:       facts.Running,
		ShowUpdate:    facts.Update != nil,
		UpdateVersion: updateVersionLabel(facts.Update),
		Open:          func() error { return openInBrowser(facts.URL) },
		Start:         func() error { return m.cmdStart(context.Background(), nil) },
		Status:        func() error { return m.cmdStatus(context.Background(), []string{"--details"}) },
		Restart:       func() error { return m.cmdRestart(context.Background(), nil) },
		Update:        func() error { return m.cmdUpdate(context.Background(), nil) },
		Doctor:        func() error { return m.cmdDoctor(context.Background(), nil) },
		Help:          func() error { return m.cmdHelp(context.Background(), nil) },
	}
}

// openInBrowser opens url with the desktop's own handler.
func openInBrowser(url string) error {
	opener := map[string]string{"darwin": "open", "windows": "explorer"}[Platform()]
	if opener == "" {
		opener = "xdg-open"
	}
	return exec.Command(opener, url).Start()
}

// cmdVersion prints the installed version and nothing else, so scripts can read
// it: `neorouter version`.
func (m *Manager) cmdVersion(context.Context, []string) error {
	fmt.Fprintln(m.Out, m.versionLabel())
	return nil
}

// versionLabel is the installed version, with the git description in brackets
// when the checkout has moved on from it (lib/manager.js
// currentInstalledVersionLabel).
func (m *Manager) versionLabel() string {
	version := m.Version
	if version == "" {
		version = config.Version
	}
	if git := m.gitVersionLabel(context.Background()); git != "" && git != version {
		return version + " (" + git + ")"
	}
	return version
}

// gitVersionLabel is `git describe --tags --always --dirty`, or "" outside a
// checkout.
func (m *Manager) gitVersionLabel(ctx context.Context) string {
	cfg, err := m.loadConfig()
	if err != nil || !exists(filepath.Join(cfg.AppDir, ".git")) {
		return ""
	}
	out, err := m.run(ctx, cfg.AppDir, "git", "describe", "--tags", "--always", "--dirty")
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(strings.TrimSpace(out), "v")
}

// cmdHelp prints the everyday commands, or every command with --all.
func (m *Manager) cmdHelp(_ context.Context, args []string) error {
	m.printHelp(cli.HasFlag(args, "--all"))
	return nil
}

// cmdHealthcheck exits non-zero when the server does not answer, which is what
// the container health check runs.
func (m *Manager) cmdHealthcheck(ctx context.Context, _ []string) error {
	cfg, err := m.loadConfig()
	if err != nil {
		return err
	}
	host := cfg.Host
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	url := "http://" + net.JoinHostPort(host, strconv.Itoa(m.loadEnvPort())) + "/health"
	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("NeoRouter is not healthy: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("NeoRouter is not healthy: health check returned %s", resp.Status)
	}
	return nil
}
