package manager

// Upgrading a Node.js install to this binary: detect it, and while it is
// switched over keep everything needed to put it back — the data, the settings,
// the service definition, the `neorouter` command — until the new server answers.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/cli"
	"github.com/NeoLabs-Systems/neolabs-go/cli/envfile"
	"github.com/NeoLabs-Systems/neolabs-go/cli/service"
	"github.com/NeoLabs-Systems/neorouter/internal/backup"
	"github.com/NeoLabs-Systems/neorouter/internal/config"
)

// nodeDefaultChannel is the channel a Node.js install without a recorded one
// was following.
const nodeDefaultChannel = "beta"

// errUpgradeFirst is what a command says that cannot run beside the Node.js
// server.
var errUpgradeFirst = inputError("This computer still has the Node.js NeoRouter. Run `neorouter install` to upgrade it; your data is backed up first.")

// LegacyJSState is what a Node.js NeoRouter installation left on this computer.
type LegacyJSState struct {
	// HasDB is a database at the install's path, NodeDB one that only the
	// Node.js server has migrated.
	HasDB  bool
	NodeDB bool
	// MigratedMarker is data/.go-migrated, written by a finished upgrade.
	MigratedMarker bool
	// LegacyPlist and LegacySystemd are service files that run server/index.js.
	LegacyPlist   bool
	LegacySystemd bool
	// LegacyNPMBin is an npm-style symlink to bin/neorouter.js on the PATH.
	LegacyNPMBin string
	// NeedsMigration is why this binary has work to do before it serves.
	NeedsMigration bool
}

// Marker file name: data/.go-migrated records the upgrade and its backup.
const markerName = ".go-migrated"

// MarkerPath is the migration marker of one install.
func MarkerPath(cfg config.Config) string { return filepath.Join(cfg.Paths.Data, markerName) }

// MigratedMarker is what the marker file holds.
type MigratedMarker struct {
	MigratedAt string `json:"migratedAt"`
	Version    string `json:"version"`
	BackupKey  string `json:"backupKey"`
}

// WriteMigratedMarker records a finished upgrade.
func WriteMigratedMarker(cfg config.Config, version, backupKey string) error {
	body, err := json.MarshalIndent(MigratedMarker{
		MigratedAt: time.Now().UTC().Format("2006-01-02T15:04:05.000Z"),
		Version:    version,
		BackupKey:  backupKey,
	}, "", "  ")
	if err != nil {
		return err
	}
	return envfile.WriteAtomic(MarkerPath(cfg), append(body, '\n'), 0o600)
}

// containsNodeServerRef reports whether a service file still runs the Node.js
// server.
func containsNodeServerRef(raw []byte) bool {
	return strings.Contains(strings.ToLower(string(raw)), "server/index.js")
}

// findLegacyNPMSymlink finds the `neorouter` symlink an npm install put on the
// PATH, pointing at bin/neorouter.js.
func findLegacyNPMSymlink(pathEnv string) string {
	for _, dir := range filepath.SplitList(pathEnv) {
		if dir == "" {
			continue
		}
		candidate := filepath.Join(dir, AppCommand)
		target, err := os.Readlink(candidate)
		if err != nil {
			continue
		}
		if strings.HasSuffix(filepath.ToSlash(target), "bin/neorouter.js") {
			return candidate
		}
	}
	return ""
}

// InspectLegacyJS looks for a Node.js installation. It changes nothing. A
// database this binary created or upgraded never counts as one.
func InspectLegacyJS(cfg config.Config, userHome string, getenv func(string) string) LegacyJSState {
	if getenv == nil {
		getenv = os.Getenv
	}
	var state LegacyJSState
	state.HasDB = exists(cfg.Paths.Database)
	state.MigratedMarker = exists(MarkerPath(cfg))
	if current, err := backup.ReadSchema(context.Background(), cfg.Paths.Database); err == nil {
		state.NodeDB = current.Node
	}
	switch Platform() {
	case "macos":
		raw, err := os.ReadFile(config.LaunchAgentPlistPath(userHome))
		state.LegacyPlist = err == nil && containsNodeServerRef(raw)
	case "linux":
		raw, err := os.ReadFile(config.SystemdUnitPath(userHome))
		state.LegacySystemd = err == nil && containsNodeServerRef(raw)
	}
	state.LegacyNPMBin = findLegacyNPMSymlink(getenv("PATH"))
	state.NeedsMigration = state.LegacyPlist || state.LegacySystemd || state.NodeDB ||
		(state.LegacyNPMBin != "" && state.HasDB && !state.MigratedMarker)
	return state
}

// legacyInstall is InspectLegacyJS for this run; a container has no service or
// command to upgrade.
func (m *Manager) legacyInstall(cfg config.Config) LegacyJSState {
	if m.inContainer() {
		return LegacyJSState{}
	}
	return InspectLegacyJS(cfg, m.UserHome, m.Getenv)
}

// isNodeCheckout reports whether dir is a checkout of the Node.js server.
func isNodeCheckout(dir string) bool {
	return exists(filepath.Join(dir, "server", "index.js")) || exists(filepath.Join(dir, "bin", "neorouter.js"))
}

// clearNodeAppDir drops a NEOROUTER_APP_DIR that points at the Node.js
// checkout, so the installed runtime is served instead.
func (m *Manager) clearNodeAppDir(cfg config.Config) error {
	values, err := envfile.Load(cfg.Paths.EnvFile)
	if err != nil {
		return err
	}
	dir := strings.TrimSpace(values["NEOROUTER_APP_DIR"])
	if dir == "" || (!isNodeCheckout(dir) && exists(filepath.Join(config.WebClientDir(dir), "index.html"))) {
		return nil
	}
	if _, err := envfile.Remove(cfg.Paths.EnvFile, "NEOROUTER_APP_DIR"); err != nil {
		return err
	}
	return os.Unsetenv("NEOROUTER_APP_DIR")
}

// installSnapshot is an install as it was before it was switched over.
type installSnapshot struct {
	cfg config.Config
	// service is the manager whose definition file is saved in unit; nil unit
	// means there was none.
	service  *service.Manager
	unitPath string
	unit     []byte
	env      []byte
	marker   []byte
	// npmLink and npmTarget are the Node.js command link; launcher the
	// `neorouter` script in ~/.local/bin, when it is NeoRouter's.
	npmLink, npmTarget string
	launcher           []byte
	wasRunning         bool
	// detached is the command line of a server that ran without a service.
	detached string
	// data is the whole install before a Node.js upgrade; database the copy
	// taken before a schema change of an existing install.
	data     *backup.Artifact
	database *backup.Artifact
}

func readOptional(path string) ([]byte, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err == nil && raw == nil {
		raw = []byte{}
	}
	return raw, err
}

func (m *Manager) launcherPath() string { return filepath.Join(BinDir(m.UserHome), AppCommand) }

// snapshotInstall records what switching over is about to change.
func (m *Manager) snapshotInstall(cfg config.Config, legacy LegacyJSState) (installSnapshot, error) {
	snap := installSnapshot{cfg: cfg}
	sm, err := m.serviceManager(cfg)
	if err != nil {
		return snap, err
	}
	snap.service = sm
	switch sm.Platform {
	case "macos":
		snap.unitPath = sm.PlistPath
	case "linux":
		snap.unitPath = sm.UnitPath
	}
	if snap.unitPath != "" {
		if snap.unit, err = readOptional(snap.unitPath); err != nil {
			return snap, err
		}
	}
	if snap.env, err = readOptional(cfg.Paths.EnvFile); err != nil {
		return snap, err
	}
	if snap.marker, err = readOptional(filepath.Join(RuntimeStore(cfg).Root, "current.json")); err != nil {
		return snap, err
	}
	if legacy.LegacyNPMBin != "" {
		snap.npmLink = legacy.LegacyNPMBin
		if snap.npmTarget, err = os.Readlink(legacy.LegacyNPMBin); err != nil {
			return snap, err
		}
	}
	if raw, rerr := os.ReadFile(m.launcherPath()); rerr == nil && isOwnLauncher(raw) {
		snap.launcher = raw
	}
	snap.wasRunning = cli.PortOpen(cfg.Port) || sm.Active()
	if snap.unit == nil && snap.wasRunning {
		snap.detached = processCommand(cfg.Paths.PIDFile)
	}
	return snap, nil
}

// processCommand is the command line of the process in the PID file.
func processCommand(pidFile string) string {
	raw, err := os.ReadFile(pidFile)
	if err != nil {
		return ""
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil || pid <= 0 || pid == os.Getpid() {
		return ""
	}
	out, err := exec.Command("ps", "-o", "command=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// restoreInstall puts the install back the way the snapshot found it and starts
// the old server again.
func (m *Manager) restoreInstall(snap installSnapshot) error {
	// The context of the failed command may be what failed it.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cfg := snap.cfg
	m.stopAndWait(ctx, cfg)
	var failed []error
	note := func(err error) {
		if err != nil {
			failed = append(failed, err)
		}
	}
	restoreFile := func(path string, raw []byte, mode os.FileMode) {
		if raw != nil {
			note(envfile.WriteAtomic(path, raw, mode))
		} else if err := os.Remove(path); !errors.Is(err, fs.ErrNotExist) {
			note(err)
		}
	}
	switch {
	case snap.data != nil:
		note(backup.Restore(backup.InstallOf(cfg.Paths), snap.data.Key))
	case snap.database != nil:
		note(backup.RestoreDatabase(*snap.database))
	}
	restoreFile(cfg.Paths.EnvFile, snap.env, 0o600)
	restoreFile(filepath.Join(RuntimeStore(cfg).Root, "current.json"), snap.marker, 0o600)
	if snap.unit != nil {
		note(envfile.WriteAtomic(snap.unitPath, snap.unit, 0o644))
	} else if snap.unitPath != "" && exists(snap.unitPath) {
		snap.service.Uninstall()
	}
	if snap.npmTarget != "" {
		if _, err := os.Lstat(snap.npmLink); errors.Is(err, fs.ErrNotExist) {
			note(os.Symlink(snap.npmTarget, snap.npmLink))
		}
	}
	if snap.launcher != nil {
		note(envfile.WriteAtomic(m.launcherPath(), snap.launcher, 0o755))
	}
	if snap.wasRunning {
		note(m.startPrevious(snap))
		if err := m.waitForServer(ctx, cfg.Port, m.rollbackTimeout()); err != nil {
			note(err)
		}
	}
	return errors.Join(failed...)
}

// startPrevious starts whatever served this install before.
func (m *Manager) startPrevious(snap installSnapshot) error {
	if snap.unit != nil {
		return snap.service.Launch()
	}
	sm, err := m.serviceManager(snap.cfg)
	if err != nil {
		return err
	}
	// `node /path/server/index.js`, started by the Node.js CLI as a detached
	// process: the interpreter has no spaces, the script's path may.
	if program, script, ok := strings.Cut(snap.detached, " "); ok && containsNodeServerRef([]byte(script)) {
		sm.Spec.Program, sm.Spec.Args = program, []string{script}
		sm.Spec.WorkDir = filepath.Dir(filepath.Dir(script))
		sm.Spec.Env = []service.EnvVar{{Key: "NEOROUTER_HOME", Value: snap.cfg.Paths.Home}}
	}
	return sm.StartFallback()
}

func (m *Manager) startupTimeout() time.Duration {
	if m.RuntimeStartupTimeout > 0 {
		return m.RuntimeStartupTimeout
	}
	return 60 * time.Second
}

func (m *Manager) rollbackTimeout() time.Duration {
	if m.RuntimeRollbackTimeout > 0 {
		return m.RuntimeRollbackTimeout
	}
	return 30 * time.Second
}

// freePort stops the server and checks that nothing else holds its port: a new
// server could not start, and the old one would answer its health check.
func (m *Manager) freePort(ctx context.Context, cfg config.Config) error {
	m.stopAndWait(ctx, cfg)
	if cli.PortOpen(cfg.Port) {
		return fmt.Errorf("Something is still using port %d. Stop it and run the command again.", cfg.Port)
	}
	return nil
}

// waitGone waits until a signalled process is no longer there.
func waitGone(process *os.Process, within time.Duration) {
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if err := process.Signal(syscall.Signal(0)); err != nil {
			return
		}
		time.Sleep(150 * time.Millisecond)
	}
}
