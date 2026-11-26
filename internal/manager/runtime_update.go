package manager

// Installing a verified runtime: the release carries a signed manifest and one
// archive per platform. The shared updater runs the steps; this file supplies
// NeoRouter's backup, service switch, health check and way back.

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/cli"
	"github.com/NeoLabs-Systems/neolabs-go/cli/envfile"
	"github.com/NeoLabs-Systems/neolabs-go/cli/release"
	"github.com/NeoLabs-Systems/neolabs-go/cli/service"
	"github.com/NeoLabs-Systems/neolabs-go/cli/update"
	"github.com/NeoLabs-Systems/neorouter/internal/backup"
	"github.com/NeoLabs-Systems/neorouter/internal/config"
)

// metadataTimeout bounds each look at the release list and manifest.
const metadataTimeout = time.Minute

// releaseClient builds the download client for this run.
func (m *Manager) releaseClient() *release.Client {
	if m.ReleaseClientFactory != nil {
		return m.ReleaseClientFactory()
	}
	return &release.Client{HTTP: m.HTTP, UserAgent: "NeoRouter CLI " + m.Version, APIBase: m.ReleaseAPIBase, AllowHTTP: m.AllowHTTPDownloads}
}

// runtimeSource is where NeoRouter's signed runtimes are published, with the
// key that verifies them.
func (m *Manager) runtimeSource() (update.Source, error) {
	var publicKey string
	var err error
	if key := strings.TrimSpace(m.RuntimePublicKey); key != "" {
		publicKey, err = release.NormalizePublicKey(key)
	} else {
		publicKey, err = RuntimePublicKey()
	}
	if err != nil {
		return update.Source{}, err
	}
	return update.Source{
		Client: m.releaseClient(), Repository: Repo, ManifestPrefix: RuntimeManifestPrefix,
		PublicKey: publicKey, SchemaVersion: 1, Targets: SupportedTargets(),
	}, nil
}

// findRuntime is the newest verified runtime of the channel for this computer.
// A release without a signed manifest, or without every platform's runtime, is
// refused.
func (m *Manager) findRuntime(ctx context.Context, channel string) (update.Source, update.Candidate, error) {
	source, err := m.runtimeSource()
	if err != nil {
		return source, update.Candidate{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, metadataTimeout)
	defer cancel()
	rel, err := source.Client.ResolveRelease(ctx, Repo, channel, RuntimeManifestPrefix)
	if err != nil {
		return source, update.Candidate{}, err
	}
	candidate, err := source.Select(ctx, rel)
	if err != nil {
		return source, update.Candidate{}, err
	}
	raw, err := source.Client.FetchVerifiedManifest(ctx, rel, source.PublicKey)
	if err != nil {
		return source, update.Candidate{}, err
	}
	var manifest release.RuntimeManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return source, update.Candidate{}, err
	}
	published := map[string]bool{}
	for _, artifact := range manifest.Artifacts {
		if rel.Assets[artifact.AssetName] != "" {
			published[artifact.Platform+"-"+artifact.Architecture] = true
		}
	}
	for platform, architectures := range SupportedTargets() {
		for _, architecture := range architectures {
			if !published[platform+"-"+architecture] {
				return source, update.Candidate{}, cli.Errorf("SETUP_RUNTIME_INCOMPLETE",
					"Release %s is missing the %s %s runtime, so it was not installed.", rel.Tag, platform, architecture)
			}
		}
	}
	return source, candidate, nil
}

// activeRuntimeVersion is the version this install runs, or an error when it
// runs from a binary that was installed some other way.
func activeRuntimeVersion(cfg config.Config) (string, error) {
	marker, err := RuntimeStore(cfg).Current()
	if err != nil {
		return "", err
	}
	return marker.Version, nil
}

// updateSignedRuntime installs and activates candidate, restarts the service on
// it and goes back to before when it does not answer. It reports whether that
// way back was taken.
func (m *Manager) updateSignedRuntime(ctx context.Context, cfg config.Config, source update.Source,
	candidate update.Candidate, before, channel string, job updateJob) (bool, error) {
	store := RuntimeStore(cfg)
	version := candidate.Version
	previous := ""
	if current, err := store.Current(); err == nil {
		previous = current.Version
	}
	snap, err := m.snapshotInstall(cfg, LegacyJSState{})
	if err != nil {
		return false, err
	}
	sm := snap.service
	wasInstalled := sm.Installed()
	oldSpec := sm.Spec
	oldSpec.Env = append([]service.EnvVar(nil), sm.Spec.Env...)
	dir, err := store.RuntimeDir(version)
	if err != nil {
		return false, err
	}
	appDir, binary := runtimeAppDir(dir), runtimeBinary(dir)
	start := func(install bool) error {
		switch {
		case !wasInstalled && !snap.wasRunning:
			return nil
		case !wasInstalled:
			return sm.StartFallback()
		case install:
			return sm.Install()
		}
		return sm.Launch()
	}
	stop := func(ctx context.Context) {
		if wasInstalled || snap.wasRunning {
			m.stopAndWait(ctx, cfg)
		}
	}

	var saved *backup.Artifact
	var schemaBefore backup.Schema
	var takeBackup func(context.Context) error
	if exists(cfg.Paths.Database) {
		takeBackup = func(ctx context.Context) error {
			if schemaBefore, err = backup.ReadSchema(ctx, cfg.Paths.Database); err != nil {
				return err
			}
			item, err := backup.Create(ctx, backup.InstallOf(cfg.Paths), backup.KindUpdate)
			saved = &item
			return err
		}
	}
	var archive string
	err = update.Apply(ctx, m.UI, update.Plan{
		App: "NeoRouter", From: before, To: version, Channel: channel,
		Backup: takeBackup,
		Progress: func(phase string, percent int, message string) {
			job.record(phase, percent, message, "running", "", false)
		},
		Download: func(ctx context.Context) (err error) {
			archive, err = source.Download(ctx, store, candidate)
			return err
		},
		Install: func(context.Context) error { return update.Unpack(store, version, archive, false) },
		Restart: func(ctx context.Context) error {
			stop(ctx)
			if err := store.Activate(version); err != nil {
				return err
			}
			sm.Spec.Program, sm.Spec.WorkDir = binary, appDir
			setSpecEnv(&sm.Spec, "NEOROUTER_APP_DIR", appDir)
			setSpecEnv(&sm.Spec, "PATH", ServicePath(m.Getenv("PATH"), binary))
			return start(true)
		},
		Verify: func(ctx context.Context) error {
			if !wasInstalled && !snap.wasRunning {
				return nil
			}
			return m.waitForServerVersion(ctx, cfg.Port, version, m.startupTimeout())
		},
		RollbackTimeout: m.rollbackTimeout() + 30*time.Second,
		Rollback: func(ctx context.Context) error {
			stop(ctx)
			// The old server refuses a database the new one has already changed.
			if saved != nil {
				now, err := backup.ReadSchema(ctx, cfg.Paths.Database)
				if err != nil || now.Version != schemaBefore.Version {
					if err := backup.RestoreDatabase(*saved); err != nil {
						return err
					}
				}
			}
			if err := restoreRuntimeState(filepath.Join(store.Root, "current.json"), snap); err != nil {
				return err
			}
			sm.Spec = oldSpec
			if !wasInstalled && !snap.wasRunning {
				return nil
			}
			// Without a saved service file the old definition is written again.
			if err := start(snap.unit == nil); err != nil {
				return err
			}
			return m.waitForServerVersion(ctx, cfg.Port, before, m.rollbackTimeout())
		},
	})
	if err != nil {
		return update.RolledBack(err), err
	}
	m.refreshLaunchers(binary, store.Root)
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

// restoreRuntimeState puts the runtime marker and the service definition back
// the way they were before an update replaced them.
func restoreRuntimeState(markerPath string, snap installSnapshot) error {
	var result error
	if snap.marker != nil {
		result = envfile.WriteAtomic(markerPath, snap.marker, 0o600)
	} else if err := os.Remove(markerPath); err != nil && !os.IsNotExist(err) {
		result = err
	}
	if snap.unit != nil {
		result = errors.Join(result, envfile.WriteAtomic(snap.unitPath, snap.unit, 0o644))
	}
	return result
}
