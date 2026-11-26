package manager

// Installing a verified runtime: the release carries one signed archive per
// platform, the manager unpacks it into <home>/app/versions/<version> and points
// the service at it. A failure puts the previous version back, definition for
// definition the way refs/NeoRecall/internal/manager/runtime_update.go does.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/cli"
	"github.com/NeoLabs-Systems/neolabs-go/cli/envfile"
	"github.com/NeoLabs-Systems/neolabs-go/cli/release"
	"github.com/NeoLabs-Systems/neolabs-go/cli/service"
	"github.com/NeoLabs-Systems/neorouter/internal/config"
)

// releaseClient builds the download client for this run.
func (m *Manager) releaseClient() *release.Client {
	if m.ReleaseClientFactory != nil {
		return m.ReleaseClientFactory()
	}
	return &release.Client{HTTP: m.HTTP, UserAgent: "NeoRouter CLI " + m.Version, APIBase: m.ReleaseAPIBase, AllowHTTP: m.AllowHTTPDownloads}
}

// runtimePublicKey is the key a runtime manifest must be signed with.
func (m *Manager) runtimePublicKey() (string, error) {
	if key := strings.TrimSpace(m.RuntimePublicKey); key != "" {
		return release.NormalizePublicKey(key)
	}
	return RuntimePublicKey()
}

// activeRuntimeVersion is the version this install runs, or an error when it
// runs from the binary that was installed some other way.
func activeRuntimeVersion(cfg config.Config) (string, error) {
	marker, err := RuntimeStore(cfg).Current()
	if err != nil {
		return "", err
	}
	return marker.Version, nil
}

// stageSignedRuntime verifies the signed manifest, downloads this platform's
// archive and installs it next to the versions already there. The active runtime
// is only replaced once the archive is complete.
func (m *Manager) stageSignedRuntime(ctx context.Context, cfg config.Config, rel release.Release, version string) error {
	publicKey, err := m.runtimePublicKey()
	if err != nil {
		return err
	}
	client := m.releaseClient()
	raw, err := client.FetchVerifiedManifest(ctx, rel, publicKey)
	if err != nil {
		return err
	}
	var manifest release.RuntimeManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return fmt.Errorf("invalid runtime manifest: %w", err)
	}
	if manifest.SchemaVersion != 1 || manifest.Version != version {
		return errors.New("the runtime manifest does not match the selected release")
	}
	var chosen *release.Artifact
	for _, artifact := range manifest.Artifacts {
		if artifact.Platform != Platform() || artifact.Architecture != Architecture() {
			continue
		}
		if chosen != nil {
			return errors.New("the runtime manifest has duplicate artifacts for this platform")
		}
		normalized, err := release.NormalizeArtifact(artifact, SupportedTargets())
		if err != nil {
			return err
		}
		chosen = &normalized
	}
	if chosen == nil || rel.Assets[chosen.AssetName] == "" {
		return errors.New("the signed runtime for this computer is missing")
	}
	if err := os.MkdirAll(cfg.Paths.Data, 0o700); err != nil {
		return err
	}
	temp, err := os.MkdirTemp(cfg.Paths.Data, ".runtime-download-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temp)
	archive := filepath.Join(temp, "runtime.zip")
	if err := client.Download(ctx, rel.Assets[chosen.AssetName], archive, chosen.SizeBytes, chosen.SHA256); err != nil {
		return err
	}
	return RuntimeStore(cfg).InstallArchive(version, archive)
}

// updateSignedRuntime installs and activates version, restarts the service on it
// and rolls back to before when anything fails.
func (m *Manager) updateSignedRuntime(ctx context.Context, cfg config.Config, rel release.Release, before, version string, job *updateJob) (bool, error) {
	store := RuntimeStore(cfg)
	if current, err := store.Current(); err == nil && current.Version == version {
		return false, errors.New("the selected runtime is already active")
	}
	m.UI.Title("Updating NeoRouter", before+" → "+version+" · "+m.currentChannel(cfg))
	m.UI.Blank()
	list := m.UI.Steps([]cli.Label{
		{Doing: "Saving a backup of your settings and data", Done: "Saved a backup of your settings and data"},
		{Doing: "Downloading the new version", Done: "Downloaded the new version"},
		{Doing: "Installing it", Done: "Installed it"},
		{Doing: "Restarting NeoRouter", Done: "Restarted NeoRouter"},
		{Doing: "Making sure it all works", Done: "Made sure it all works"},
	}, true)
	defer list.Close()
	phases := []string{"backup", "downloading", "installing", "restarting", "verifying"}
	progress := []int{15, 31, 47, 63, 79}
	run := func(index int, fn func() error) error {
		if job != nil {
			job.record(phases[index], progress[index], list.Doing(index), "running", "", false)
		}
		return list.Run(index, fn)
	}

	if err := run(0, func() error {
		if !exists(cfg.Paths.Database) {
			return nil
		}
		_, err := m.makeBackup(cfg)
		return err
	}); err != nil {
		return false, err
	}
	if err := run(1, func() error { return m.stageSignedRuntime(ctx, cfg, rel, version) }); err != nil {
		return false, err
	}

	sm, err := m.serviceManager(cfg)
	if err != nil {
		return false, err
	}
	wasInstalled := sm.Installed()
	wasRunning := cli.PortOpen(m.loadEnvPort()) || m.RestartAfterLegacyMigration
	snapshot, oldSpec := m.snapshotService(), sm.Spec
	oldSpec.Env = append([]service.EnvVar(nil), sm.Spec.Env...)
	markerPath := filepath.Join(store.Root, "current.json")
	oldMarker, markerErr := os.ReadFile(markerPath)
	if markerErr != nil && !os.IsNotExist(markerErr) {
		return false, markerErr
	}
	previous := ""
	if current, err := store.Current(); err == nil {
		previous = current.Version
	}
	dir, err := store.RuntimeDir(version)
	if err != nil {
		return false, err
	}
	appDir := dir
	binary := runtimeBinary(dir)
	rollback := func(cause error) (bool, error) {
		if wasInstalled || wasRunning {
			m.stopManagedService(cfg, sm)
		}
		restoreErr := restoreRuntimeState(markerPath, oldMarker, snapshot)
		sm.Spec = oldSpec
		if wasInstalled || wasRunning {
			var rerr error
			if wasInstalled {
				if snapshot.path == "" {
					rerr = sm.Install()
				} else {
					rerr = sm.Launch()
				}
			} else {
				rerr = sm.StartFallback()
			}
			if rerr != nil {
				restoreErr = errors.Join(restoreErr, rerr)
			} else {
				timeout := m.RuntimeRollbackTimeout
				if timeout <= 0 {
					timeout = 30 * time.Second
				}
				rollbackCtx, cancel := context.WithTimeout(context.Background(), timeout+5*time.Second)
				rerr = m.waitForServerVersion(rollbackCtx, m.loadEnvPort(), before, timeout)
				cancel()
				if rerr != nil {
					restoreErr = errors.Join(restoreErr, rerr)
				}
			}
		}
		if restoreErr != nil {
			return false, errors.Join(cause, fmt.Errorf("rollback failed: %w", restoreErr))
		}
		return true, fmt.Errorf("%w; restored NeoRouter %s", cause, before)
	}

	if err := run(2, func() error {
		if wasInstalled || wasRunning {
			m.stopManagedService(cfg, sm)
		}
		if err := store.Activate(version); err != nil {
			return err
		}
		sm.Spec.Program, sm.Spec.WorkDir = binary, appDir
		setSpecEnv(&sm.Spec, "NEOROUTER_APP_DIR", appDir)
		setSpecEnv(&sm.Spec, "PATH", ServicePath(m.Getenv("PATH"), binary))
		return nil
	}); err != nil {
		return rollback(err)
	}

	if wasInstalled || wasRunning {
		if err := run(3, func() error {
			if wasInstalled {
				return sm.Install()
			}
			return sm.StartFallback()
		}); err != nil {
			return rollback(err)
		}
		timeout := m.RuntimeStartupTimeout
		if timeout <= 0 {
			timeout = 60 * time.Second
		}
		if err := run(4, func() error {
			return m.waitForServerVersion(ctx, m.loadEnvPort(), version, timeout)
		}); err != nil {
			return rollback(err)
		}
	}

	if err := m.refreshLauncher(binary); err != nil {
		m.UI.Warn("NeoRouter updated, but the neorouter command could not be refreshed: " + err.Error())
	}
	if err := store.Prune(version, previous); err != nil {
		m.UI.Warn("Old runtime cleanup failed: " + err.Error())
	}
	return false, nil
}

// setSpecEnv replaces or appends one environment variable of a service.
func setSpecEnv(spec *service.Spec, key, value string) {
	for i := range spec.Env {
		if spec.Env[i].Key == key {
			spec.Env[i].Value = value
			return
		}
	}
	spec.Env = append(spec.Env, service.EnvVar{Key: key, Value: value})
}

// stopManagedService stops the service (and anything detached) before the
// runtime it runs is replaced.
func (m *Manager) stopManagedService(cfg config.Config, sm *service.Manager) {
	m.stopRunningProcesses(cfg, sm)
}

// restoreRuntimeState puts the runtime marker and the service definition back the
// way they were before an update replaced them.
func restoreRuntimeState(markerPath string, marker []byte, snapshot serviceSnapshot) error {
	var result error
	if marker != nil {
		result = envfile.WriteAtomic(markerPath, marker, 0o600)
	} else if err := os.Remove(markerPath); err != nil && !os.IsNotExist(err) {
		result = err
	}
	if snapshot.path != "" && len(snapshot.content) != 0 {
		result = errors.Join(result, envfile.WriteAtomic(snapshot.path, snapshot.content, 0o644))
	}
	return result
}

// refreshLauncher points the `neorouter` command at the binary the service now
// runs.
func (m *Manager) refreshLauncher(binary string) error {
	path := filepath.Join(BinDir(m.UserHome), AppCommand)
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	if !strings.Contains(string(raw), CommandMarker) {
		return nil
	}
	link, err := ensureCommandLink(m.UserHome, binary, map[string]string{"PATH": m.Getenv("PATH"), "SHELL": m.Getenv("SHELL")})
	if err != nil {
		return err
	}
	if link.Status == LinkForeign || link.Status == LinkUnsupported {
		return errors.New("the neorouter command is not this installation's")
	}
	return nil
}

// waitForServerVersion waits until the server reports version on port.
func (m *Manager) waitForServerVersion(ctx context.Context, port int, version string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if health, ok := fetchHealth(ctx, port); ok {
			if got, _ := health["version"].(string); strings.TrimPrefix(got, "v") == strings.TrimPrefix(version, "v") {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
	return fmt.Errorf("NeoRouter did not report version %s on port %d", version, port)
}
