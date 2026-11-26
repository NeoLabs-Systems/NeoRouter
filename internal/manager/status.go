package manager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/cli"
	"github.com/NeoLabs-Systems/neolabs-go/cli/release"
	"github.com/NeoLabs-Systems/neorouter/internal/config"
	"github.com/NeoLabs-Systems/neorouter/internal/updates"
)

// labelWidth is the column the status labels line up in.
const labelWidth = 24

// statusCheck is one plain-language health check: a tone, a label, what it
// found, and the command that fixes it.
type statusCheck struct {
	ID     string
	Tone   string
	Label  string
	Detail string
	Fix    string
}

// statusUpdate is a newer release waiting on the channel.
type statusUpdate struct {
	Version string
}

// statusFacts is what `neorouter status` gathers before it says anything.
type statusFacts struct {
	AppDir            string
	EnvFile           string
	Port              int
	URL               string
	Running           bool
	InContainer       bool
	Version           string
	VersionLabel      string
	RunningVersion    string
	ChannelSummary    string
	ServiceActive     bool
	ConfigPresent     bool
	WebClientPresent  bool
	Update            *statusUpdate
	UpdateChecked     bool
	RuntimeIncomplete bool
}

// updateVersionLabel is the hint the home menu shows beside "Get the latest
// version".
func updateVersionLabel(update *statusUpdate) string {
	if update == nil {
		return ""
	}
	return update.Version
}

// statusChecks lists the checks in the order the CLI prints them.
func statusChecks(facts statusFacts) []statusCheck {
	checks := []statusCheck{{
		ID: "server", Tone: "ok", Label: "NeoRouter", Detail: "Running at " + facts.URL,
	}}
	if !facts.Running {
		checks[0] = statusCheck{ID: "server", Tone: "error", Label: "NeoRouter", Detail: "Not running", Fix: "neorouter start"}
	}
	if facts.RunningVersion != "" && facts.RunningVersion != facts.Version {
		checks = append(checks, statusCheck{
			Tone: "warn", Label: "Restart needed",
			Detail: fmt.Sprintf("%s is installed, but %s is still running", facts.Version, facts.RunningVersion),
			Fix:    "neorouter restart",
		})
	}
	switch {
	case facts.InContainer:
		checks = append(checks, statusCheck{Tone: "ok", Label: "Starts automatically", Detail: "The container looks after it"})
	case facts.ServiceActive:
		checks = append(checks, statusCheck{Tone: "ok", Label: "Starts automatically", Detail: "Turns on when your computer does"})
	default:
		checks = append(checks, statusCheck{
			Tone: "warn", Label: "Starts automatically", Detail: "Not set up on this computer", Fix: "neorouter install",
		})
	}
	if facts.ConfigPresent {
		checks = append(checks, statusCheck{Tone: "ok", Label: "Settings", Detail: "Saved"})
	} else {
		checks = append(checks, statusCheck{Tone: "error", Label: "Settings", Detail: "Not set up yet", Fix: "neorouter setup"})
	}
	if facts.WebClientPresent {
		checks = append(checks, statusCheck{Tone: "ok", Label: "App in your browser", Detail: "Installed"})
	} else {
		checks = append(checks, statusCheck{Tone: "error", Label: "App in your browser", Detail: "Missing", Fix: "neorouter repair"})
	}
	if facts.Update != nil {
		detail := "A newer version is ready"
		if facts.Update.Version != "" {
			detail = fmt.Sprintf("A newer version (%s) is ready", facts.Update.Version)
		}
		checks = append(checks, statusCheck{
			ID: "version", Tone: "warn", Label: "Version " + facts.Version, Detail: detail, Fix: "neorouter update",
		})
	} else {
		detail := ""
		if facts.UpdateChecked {
			detail = "Up to date"
		}
		checks = append(checks, statusCheck{ID: "version", Tone: "ok", Label: "Version " + facts.Version, Detail: detail})
	}
	return checks
}

// needsAttention reports whether a check is a warning or an error.
func needsAttention(check statusCheck) bool { return check.Tone == "warn" || check.Tone == "error" }

// headline is the one line at the top of the status screen.
func headline(facts statusFacts, checks []statusCheck) statusCheck {
	switch {
	case !facts.Running:
		return statusCheck{Tone: "error", Label: "NeoRouter isn't running"}
	case hasTone(checks, "error"):
		return statusCheck{Tone: "error", Label: "NeoRouter needs attention"}
	}
	suggestions := countTone(checks, "warn")
	switch suggestions {
	case 0:
		return statusCheck{Tone: "ok", Label: "Everything's working"}
	case 1:
		return statusCheck{Tone: "warn", Label: "Running — 1 thing needs a look"}
	}
	return statusCheck{Tone: "warn", Label: fmt.Sprintf("Running — %d things need a look", suggestions)}
}

func hasTone(checks []statusCheck, tone string) bool { return countTone(checks, tone) > 0 }

func countTone(checks []statusCheck, tone string) int {
	n := 0
	for _, check := range checks {
		if check.Tone == tone {
			n++
		}
	}
	return n
}

// printChecks prints the checks side by side, or the detail under its label
// when the window is narrow. A failed check says how to fix it.
func (m *Manager) printChecks(checks []statusCheck) {
	u := m.UI
	narrow := u.IsNarrow()
	indent := labelWidth + 6
	if narrow {
		indent = 4
	}
	u.Blank()
	for _, check := range checks {
		if narrow {
			u.Item(check.Tone, u.Bold(check.Label), "")
			if check.Detail != "" {
				u.Paragraph(check.Detail, indent, u.Muted)
			}
		} else {
			u.ItemWidth(check.Tone, u.Bold(check.Label), check.Detail, labelWidth)
		}
		if check.Fix != "" {
			u.Print(strings.Repeat(" ", indent) + u.Faint("run") + " " + u.Command(check.Fix))
		}
	}
}

// printStatus prints the status screen.
func (m *Manager) printStatus(facts statusFacts, details bool) {
	u := m.UI
	checks := statusChecks(facts)
	top := headline(facts, checks)
	u.Blank()
	u.Print("  " + u.InTone(top.Tone, cli.Tones[top.Tone].Mark) + " " + u.Bold(top.Label))
	if facts.Running {
		u.Print("    " + u.Accent(facts.URL))
	} else {
		u.Print("    " + u.Faint("run") + " " + u.Command("neorouter start"))
	}
	shown := checks
	if !details {
		shown = nil
		for _, check := range checks {
			if check.ID != "server" && needsAttention(check) {
				shown = append(shown, check)
			}
		}
	}
	if len(shown) > 0 {
		m.printChecks(shown)
	}
	if details {
		u.Section("Details")
		runningVersion := facts.RunningVersion
		if runningVersion == "" {
			runningVersion = "not running"
		}
		rows := [][2]string{
			{"Installed in", facts.AppDir},
			{"Version", facts.VersionLabel},
			{"Running version", runningVersion},
			{"Release channel", facts.ChannelSummary},
			{"Settings file", facts.EnvFile},
			{"Port", strconv.Itoa(facts.Port)},
		}
		for _, row := range rows {
			u.Print("  " + cli.PadEnd(u.Faint(row[0]), 18) + row[1])
		}
	}
	u.Blank()
}

// collectStatusFacts gathers everything the status screen and the menu show.
func (m *Manager) collectStatusFacts(ctx context.Context, cfg config.Config, checkForUpdate bool) statusFacts {
	port := cfg.Port
	if port <= 0 {
		port = DefaultPort
	}
	facts := statusFacts{
		AppDir:           cfg.AppDir,
		EnvFile:          cfg.Paths.EnvFile,
		Port:             port,
		URL:              appURL(port),
		Running:          cli.PortOpen(port),
		InContainer:      m.inContainer(),
		Version:          m.Version,
		VersionLabel:     m.versionLabel(),
		ChannelSummary:   updates.DescribeChannelPolicy(m.currentChannel(cfg)),
		ServiceActive:    m.serviceActive(cfg),
		ConfigPresent:    exists(cfg.Paths.EnvFile),
		WebClientPresent: HasBundledWebClient(cfg.WebDir),
	}
	if facts.Running {
		facts.RunningVersion = m.fetchRunningVersion(ctx, port)
	}
	if checkForUpdate && !facts.InContainer {
		facts.Update, facts.UpdateChecked = m.findPendingUpdate(ctx, cfg)
	}
	return facts
}

// serviceActive asks the service manager whether NeoRouter starts by itself.
func (m *Manager) serviceActive(cfg config.Config) bool {
	sm, err := m.serviceManager(cfg)
	if err != nil {
		return false
	}
	return sm.Active()
}

// fetchRunningVersion is the version the running server reports.
func (m *Manager) fetchRunningVersion(ctx context.Context, port int) string {
	health, ok := fetchHealth(ctx, port)
	if !ok {
		return ""
	}
	version, _ := health["version"].(string)
	return version
}

// findPendingUpdate asks whether the channel has a newer release.
func (m *Manager) findPendingUpdate(ctx context.Context, cfg config.Config) (*statusUpdate, bool) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	latest, err := updates.Latest(ctx, m.releaseClient(), m.currentChannel(cfg))
	if err != nil {
		return nil, false
	}
	if release.CompareVersions(latest, m.Version) <= 0 {
		return nil, true
	}
	return &statusUpdate{Version: latest}, true
}

// fetchHealth reads /health from the local server.
func fetchHealth(ctx context.Context, port int) (map[string]any, bool) {
	client := &http.Client{Timeout: 2 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/health", port), nil)
	if err != nil {
		return nil, false
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, false
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, false
	}
	var out map[string]any
	if json.NewDecoder(resp.Body).Decode(&out) != nil {
		return nil, false
	}
	return out, true
}

// cmdStatus is `neorouter status [--details]`.
func (m *Manager) cmdStatus(ctx context.Context, args []string) error {
	cfg, err := m.configWithPaths()
	if err != nil {
		return err
	}
	facts := m.collectStatusFacts(ctx, cfg, m.UI.Interactive())
	m.printStatus(facts, cli.HasFlag(args, "--details"))
	return nil
}

// doctorCheck is one read-only diagnostic.
type doctorCheck struct {
	ok      bool
	label   string
	message string
	action  string
}

// cmdDoctor is `neorouter doctor`: it changes nothing and offers to repair what
// it finds.
func (m *Manager) cmdDoctor(ctx context.Context, _ []string) error {
	cfg, err := m.configWithPaths()
	if err != nil {
		return err
	}
	var checks []doctorCheck
	_, writeErr := os.OpenFile(filepath.Join(cfg.Paths.Home, ".doctor-probe"), os.O_CREATE|os.O_WRONLY, 0o600)
	if writeErr == nil {
		_ = os.Remove(filepath.Join(cfg.Paths.Home, ".doctor-probe"))
	}
	checks = append(checks, doctorCheck{
		ok: writeErr == nil, label: "Data folder",
		message: messageIf(writeErr == nil, "Can be read and written", "Can't write to "+cfg.Paths.Home+": check its permissions"),
	})
	_, envErr := os.Stat(cfg.Paths.EnvFile)
	checks = append(checks, doctorCheck{
		ok: envErr == nil, label: "Settings",
		message: messageIf(envErr == nil, "Saved", "Not set up yet"), action: "neorouter setup",
	})
	runtimeErr := m.runtimeProblem(cfg)
	checks = append(checks, doctorCheck{
		ok: runtimeErr == nil, label: "Server files",
		message: messageIf(runtimeErr == nil, "Complete", runtimeErrText(runtimeErr)), action: "neorouter repair",
	})
	web := HasBundledWebClient(cfg.WebDir)
	checks = append(checks, doctorCheck{
		ok: web, label: "App in your browser",
		message: messageIf(web, "Installed", "Missing"), action: "neorouter repair",
	})
	if m.legacyInstall(cfg).NeedsMigration {
		checks = append(checks, doctorCheck{label: "Upgrade", message: "The Node.js NeoRouter is still installed", action: "neorouter install"})
	}
	running := cli.PortOpen(cfg.Port)
	checks = append(checks, doctorCheck{
		ok: running, label: "NeoRouter",
		message: messageIf(running, fmt.Sprintf("Answering on port %d", cfg.Port), fmt.Sprintf("Not answering on port %d", cfg.Port)),
		action:  "neorouter start",
	})
	if m.needsCommandLink(cfg) {
		reachable := commandReachable(m.Getenv("PATH"))
		checks = append(checks, doctorCheck{
			ok: reachable, label: "Command",
			message: messageIf(reachable, "neorouter works in your terminal", "neorouter is not found in your terminal"),
			action:  "neorouter repair",
		})
	}
	m.UI.Title("Checking NeoRouter", "Nothing is changed while this runs.")
	allOK := true
	shown := make([]statusCheck, 0, len(checks))
	for _, check := range checks {
		tone := "error"
		fix := check.action
		if check.ok {
			tone, fix = "ok", ""
		} else {
			allOK = false
		}
		shown = append(shown, statusCheck{Tone: tone, Label: check.label, Detail: check.message, Fix: fix})
	}
	m.printChecks(shown)
	m.UI.Blank()
	switch {
	case allOK:
		m.UI.Item("ok", m.UI.Bold("No problems found"), "")
		m.UI.Blank()
	case !m.inContainer() && m.UI.Interactive() && !m.legacyInstall(cfg).NeedsMigration:
		confirm, cerr := m.UI.Confirm("Fix these for you now? Your data stays where it is.", true)
		if cerr != nil {
			return cerr
		}
		if confirm {
			return m.cmdRepair(ctx, nil)
		}
		m.UI.Note("Most problems are fixed by " + m.UI.Command("neorouter repair") + ".")
		m.UI.Blank()
	default:
		m.UI.Note("Most problems are fixed by " + m.UI.Command("neorouter repair") + ".")
		m.UI.Blank()
	}
	return nil
}

// runtimeProblem reports what is missing from the active runtime, or nil when
// this install runs from its own binary.
func (m *Manager) runtimeProblem(cfg config.Config) error {
	store := RuntimeStore(cfg)
	marker, err := store.Current()
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return store.Validate(marker.Version)
}

// runtimeErrText is the one-line reason the runtime check failed.
func runtimeErrText(err error) string {
	if err == nil {
		return ""
	}
	return "Some are missing: " + strings.SplitN(err.Error(), "\n", 2)[0]
}

func messageIf(ok bool, good, bad string) string {
	if ok {
		return good
	}
	return bad
}

// cmdLogs is `neorouter logs`: follow the service's two log files, or print
// them when there is no tail to follow them with.
func (m *Manager) cmdLogs(ctx context.Context, _ []string) error {
	cfg, err := m.configWithPaths()
	if err != nil {
		return err
	}
	var files []string
	for _, name := range []string{AppCommand + ".log", AppCommand + ".error.log"} {
		if file := filepath.Join(cfg.Paths.Logs, name); exists(file) {
			files = append(files, file)
		}
	}
	if len(files) == 0 {
		m.UI.Blank()
		m.UI.Item("info", "No logs yet.", "")
		m.UI.Blank()
		return nil
	}
	if _, err := exec.LookPath("tail"); err == nil {
		m.UI.Title("What NeoRouter is doing", "Press Ctrl+C to stop watching.")
		m.UI.Blank()
		cmd := exec.CommandContext(ctx, "tail", append([]string{"-n", "80", "-f"}, files...)...)
		cmd.Stdout, cmd.Stderr = m.Out, m.Err
		err := cmd.Run()
		// Ctrl+C reaches tail and this process alike; either is how it ends.
		if ctx.Err() != nil || interrupted(err) {
			return nil
		}
		return err
	}
	for _, file := range files {
		m.UI.Section(file)
		raw, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		fmt.Fprint(m.Out, string(raw))
	}
	return nil
}

// interrupted reports a child that Ctrl+C or a stop request ended.
func interrupted(err error) bool {
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		return false
	}
	status, ok := exit.Sys().(syscall.WaitStatus)
	return ok && status.Signaled() && (status.Signal() == syscall.SIGINT || status.Signal() == syscall.SIGTERM)
}
