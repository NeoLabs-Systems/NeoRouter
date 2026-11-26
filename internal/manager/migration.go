package manager

// Migrating a Node.js install to this binary: detect it, stop it politely, back
// it up, re-seal the values it encrypted, replace its service and leave a marker
// so none of it runs twice. Pattern: refs/NeoRecall/internal/manager/migration.go.

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/cli"
	"github.com/NeoLabs-Systems/neolabs-go/cli/envfile"
	"github.com/NeoLabs-Systems/neolabs-go/cli/service"
	"github.com/NeoLabs-Systems/neorouter/internal/config"
)

// LegacyJSState is what a Node.js NeoRouter installation left on this computer.
type LegacyJSState struct {
	// HasDB is a database at the install's path.
	HasDB bool `json:"hasDb"`
	// MigratedMarker is data/.go-migrated, written by a finished migration.
	MigratedMarker bool `json:"migratedMarker"`
	// LegacyPlist and LegacySystemd are service files that run server/index.js.
	LegacyPlist   bool `json:"legacyPlist"`
	LegacySystemd bool `json:"legacySystemd"`
	// LegacyNPMBin is an npm-style symlink to bin/neorouter.js on the PATH.
	LegacyNPMBin string `json:"legacyNpmBin,omitempty"`
	// NeedsMigration is why this binary has work to do before it serves.
	NeedsMigration bool `json:"needsMigration"`
}

// Marker file name: data/.go-migrated records the migration and the backup.
const markerName = ".go-migrated"

// MarkerPath is the migration marker of one install.
func MarkerPath(cfg config.Config) string { return filepath.Join(cfg.Paths.Data, markerName) }

// MigratedMarker is what the marker file holds.
type MigratedMarker struct {
	MigratedAt string `json:"migratedAt"`
	Version    string `json:"version"`
	BackupKey  string `json:"backupKey"`
	Note       string `json:"note"`
}

// WriteMigratedMarker records a finished migration: when, by which version,
// which backup was taken and why.
func WriteMigratedMarker(cfg config.Config, version, backupKey, note string) error {
	if err := os.MkdirAll(filepath.Dir(MarkerPath(cfg)), 0o700); err != nil {
		return err
	}
	body, err := json.MarshalIndent(MigratedMarker{
		MigratedAt: isoTimestamp(time.Now()),
		Version:    version,
		BackupKey:  backupKey,
		Note:       note,
	}, "", "  ")
	if err != nil {
		return err
	}
	return envfile.WriteAtomic(MarkerPath(cfg), append(body, '\n'), 0o600)
}

// isoTimestamp is JavaScript's Date#toISOString.
func isoTimestamp(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000Z") }

// containsNodeServerRef reports whether a service file starts the Node server.
func containsNodeServerRef(data []byte) bool {
	text := strings.ToLower(string(data))
	for _, needle := range []string{"server/index.js", "/node<", "execstart=/usr/bin/node", "/bin/node "} {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}

// findLegacyNPMSymlink finds a `neorouter` on the PATH that links to the Node
// CLI (an npm install or install.sh's old launcher).
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

// InspectLegacyJS looks for a Node.js installation that has not been migrated:
// its database, its service files, its command on the PATH.
func InspectLegacyJS(cfg config.Config, userHome string, getenv func(string) string) LegacyJSState {
	var state LegacyJSState
	state.HasDB = exists(cfg.Paths.Database)
	state.MigratedMarker = exists(MarkerPath(cfg))
	if runtime.GOOS == "darwin" {
		if raw, err := os.ReadFile(config.LaunchAgentPlistPath(userHome)); err == nil && containsNodeServerRef(raw) {
			state.LegacyPlist = true
		}
	}
	if runtime.GOOS == "linux" {
		if raw, err := os.ReadFile(config.SystemdUnitPath(userHome)); err == nil && containsNodeServerRef(raw) {
			state.LegacySystemd = true
		}
	}
	if getenv != nil {
		state.LegacyNPMBin = findLegacyNPMSymlink(getenv("PATH"))
	}
	state.NeedsMigration = state.LegacyPlist || state.LegacySystemd || state.LegacyNPMBin != "" ||
		(state.HasDB && !state.MigratedMarker)
	return state
}

// serviceSnapshot is a service definition as it was before a migration or an
// update touched it, so a failure can put it back.
type serviceSnapshot struct {
	path    string
	content []byte
}

// snapshotService reads this platform's service definition.
func (m *Manager) snapshotService() serviceSnapshot {
	path, content := serviceUnitContent(m.UserHome)
	return serviceSnapshot{path: path, content: content}
}

// stopRunningProcesses stops the service, the detached process in the PID file,
// whoever holds the port and whoever has the database open — politely (SIGTERM,
// then SIGKILL for something that does not let go).
func (m *Manager) stopRunningProcesses(cfg config.Config, sm *service.Manager) {
	if sm != nil {
		sm.Stop()
	}
	if raw, err := os.ReadFile(cfg.Paths.PIDFile); err == nil {
		if pid, perr := strconv.Atoi(strings.TrimSpace(string(raw))); perr == nil && pid > 0 && pid != os.Getpid() {
			if process, ferr := os.FindProcess(pid); ferr == nil {
				_ = process.Signal(syscall.SIGTERM)
				waitGone(process, 8*time.Second)
			}
		}
		_ = os.Remove(cfg.Paths.PIDFile)
	}
	port := cfg.Port
	if port <= 0 {
		port = DefaultPort
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) && cli.PortOpen(port) {
		terminateLegacyPortOwner(port, syscall.SIGTERM)
		time.Sleep(250 * time.Millisecond)
	}
	if cli.PortOpen(port) {
		terminateLegacyPortOwner(port, syscall.SIGKILL)
		time.Sleep(250 * time.Millisecond)
	}
	terminateProcessesHoldingDB(cfg.Paths.Database)
}

// waitGone waits until the process is gone or the deadline passes.
func waitGone(process *os.Process, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if process.Signal(syscall.Signal(0)) != nil {
			return
		}
		time.Sleep(150 * time.Millisecond)
	}
}

// isNeoRouterProcessCmd reports whether a command line belongs to NeoRouter,
// whichever version of it.
func isNeoRouterProcessCmd(cmdLine string) bool {
	text := strings.ToLower(cmdLine)
	for _, needle := range []string{"neorouter", "server/index.js", "lib/manager.js", "lib/cli/update_runner.js"} {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}

// terminateLegacyPortOwner signals the NeoRouter process that holds port.
func terminateLegacyPortOwner(port int, sig syscall.Signal) {
	if runtime.GOOS == "windows" {
		return
	}
	out, err := exec.Command("lsof", "-ti", ":"+strconv.Itoa(port)).Output()
	if err != nil {
		return
	}
	for _, field := range strings.Fields(string(out)) {
		pid, perr := strconv.Atoi(field)
		if perr != nil || pid <= 0 || pid == os.Getpid() {
			continue
		}
		cmdOut, cerr := exec.Command("ps", "-p", field, "-o", "command=").Output()
		if cerr != nil || !isNeoRouterProcessCmd(string(cmdOut)) {
			continue
		}
		if process, ferr := os.FindProcess(pid); ferr == nil {
			_ = process.Signal(sig)
		}
	}
}

// terminateProcessesHoldingDB stops whatever still has the database open, so the
// file can be copied and re-sealed.
func terminateProcessesHoldingDB(dbPath string) {
	if runtime.GOOS == "windows" || dbPath == "" {
		return
	}
	var targets []string
	for _, path := range []string{dbPath, dbPath + "-wal", dbPath + "-shm"} {
		if exists(path) {
			targets = append(targets, path)
		}
	}
	if len(targets) == 0 {
		return
	}
	out, err := exec.Command("lsof", append([]string{"-t"}, targets...)...).Output()
	if err != nil {
		return
	}
	seen := map[int]bool{}
	var processes []*os.Process
	for _, field := range strings.Fields(string(out)) {
		pid, perr := strconv.Atoi(field)
		if perr != nil || pid <= 0 || pid == os.Getpid() || seen[pid] {
			continue
		}
		seen[pid] = true
		cmdOut, cerr := exec.Command("ps", "-p", field, "-o", "command=").Output()
		if cerr != nil || !isNeoRouterProcessCmd(string(cmdOut)) {
			continue
		}
		if process, ferr := os.FindProcess(pid); ferr == nil {
			_ = process.Signal(syscall.SIGTERM)
			processes = append(processes, process)
		}
	}
	if len(processes) == 0 {
		return
	}
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		alive := false
		for _, process := range processes {
			if process.Signal(syscall.Signal(0)) == nil {
				alive = true
				break
			}
		}
		if !alive {
			return
		}
		time.Sleep(150 * time.Millisecond)
	}
	for _, process := range processes {
		if process.Signal(syscall.Signal(0)) == nil {
			_ = process.Signal(syscall.SIGKILL)
		}
	}
	time.Sleep(250 * time.Millisecond)
}

// migrateLegacyJSState does everything that has to happen while the old server
// is stopped: drop its command, clean a stale app directory, back the data up
// and re-seal the database.
func (m *Manager) migrateLegacyJSState(ctx context.Context, cfg config.Config, state LegacyJSState) (string, error) {
	if state.LegacyNPMBin != "" {
		if err := os.Remove(state.LegacyNPMBin); err == nil {
			m.UI.OK("Removed the legacy Node.js command " + state.LegacyNPMBin)
		}
	}
	if err := m.clearStaleAppDir(cfg); err != nil {
		return "", err
	}
	backupKey := ""
	if exists(cfg.Paths.Database) && !state.MigratedMarker {
		if err := m.UI.Step("Backing up your settings and data before the upgrade", "Saved the pre-upgrade backup", func() error {
			artifact, berr := m.makeBackup(cfg)
			if berr != nil {
				return berr
			}
			backupKey = artifact.Key
			return nil
		}); err != nil {
			return "", err
		}
	}
	resealed := 0
	if exists(cfg.Paths.Database) {
		if err := m.UI.Step("Re-sealing the keys the Node.js server encrypted", "Re-sealed the keys", func() error {
			var serr error
			resealed, serr = m.resealLegacySecrets(ctx, cfg)
			return serr
		}); err != nil {
			return "", err
		}
		if resealed > 0 {
			m.UI.OK("Re-sealed " + strconv.Itoa(resealed) + " encrypted value(s) the Node.js server wrote")
		}
	}
	return backupKey, nil
}

// clearStaleAppDir drops NEOROUTER_APP_DIR from .env when it names a Node.js
// checkout or a directory that is gone, so the install finds its own app again.
func (m *Manager) clearStaleAppDir(cfg config.Config) error {
	values, err := envfile.Load(cfg.Paths.EnvFile)
	if err != nil {
		return err
	}
	appDir := strings.TrimSpace(values["NEOROUTER_APP_DIR"])
	if appDir == "" {
		return nil
	}
	hasWeb := exists(filepath.Join(config.WebClientDir(appDir), "index.html"))
	hasLegacyServer := exists(filepath.Join(appDir, "server", "index.js"))
	hasLegacyBin := exists(filepath.Join(appDir, "bin", "neorouter.js"))
	if hasWeb && !hasLegacyServer && !hasLegacyBin {
		return nil
	}
	if _, err := envfile.Remove(cfg.Paths.EnvFile, "NEOROUTER_APP_DIR"); err != nil {
		return err
	}
	return os.Unsetenv("NEOROUTER_APP_DIR")
}
