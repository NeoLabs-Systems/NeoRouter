package manager

// `neorouter update`: move this install to the newest release on its channel.
// The update replaces the runtime the service runs (a signed, verified archive),
// never the code inside a running image.

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/cli"
	"github.com/NeoLabs-Systems/neolabs-go/cli/release"
	"github.com/NeoLabs-Systems/neorouter/internal/config"
	"github.com/NeoLabs-Systems/neorouter/internal/updates"
)

// cmdUpdate is `neorouter update [stable|beta] [--notes|--check]`.
func (m *Manager) cmdUpdate(ctx context.Context, args []string) error {
	requested, err := channelArg("update", args)
	if err != nil {
		return err
	}
	if m.inContainer() {
		return m.printContainerUpdate()
	}
	cfg, err := m.loadConfig()
	if err != nil {
		return err
	}
	channel := requested
	if channel == "" {
		channel = m.currentChannel(cfg)
	}
	before := m.Version
	if current, err := activeRuntimeVersion(cfg); err == nil {
		before = current
	}
	// Looking changes nothing, not even the saved channel.
	if cli.HasFlag(args, "--notes") || cli.HasFlag(args, "--check") {
		return m.printAvailable(ctx, channel, before, requested)
	}

	job := m.updateJob(cfg)
	if m.legacyInstall(cfg).NeedsMigration {
		var chosen []string
		if requested != "" {
			chosen = []string{"--channel", requested}
		}
		if err := m.cmdInstall(ctx, chosen); err != nil {
			return job.fail(err, false)
		}
		job.record("completed", 100, "NeoRouter was upgraded.", "completed", m.Version, false)
		return nil
	}
	if cfg, err = m.ensureDefaultEnvFile(); err != nil {
		return job.fail(err, false)
	}
	if requested != "" {
		if err := m.setChannel(requested); err != nil {
			return job.fail(err, false)
		}
	}

	job.record("checking", 10, "Checking the latest "+channel+" release on GitHub", "running", "", false)
	source, candidate, err := m.findRuntime(ctx, channel)
	if err != nil {
		return job.fail(err, false)
	}
	if !wantsVersion(candidate.Version, before, requested) {
		job.record("completed", 100, fmt.Sprintf("NeoRouter is up to date (%s on %s).", before, channel), "completed", before, false)
		m.printLatest(before)
		return nil
	}
	rolledBack, err := m.updateSignedRuntime(ctx, cfg, source, candidate, before, channel, job)
	if err != nil {
		return job.fail(err, rolledBack)
	}
	m.Version = candidate.Version
	job.record("completed", 100, fmt.Sprintf("Updated NeoRouter to %s.", candidate.Version), "completed", candidate.Version, false)
	m.UI.Blank()
	m.UI.Item("ok", m.UI.Bold("NeoRouter "+candidate.Version+" is ready"), appURL(m.loadEnvPort()))
	m.UI.Blank()
	m.UI.Note("Your settings, keys and usage were kept.")
	m.UI.Blank()
	return nil
}

// wantsVersion reports whether an install on current moves to latest: when it
// is newer, or when a channel was named and its release differs, which is how
// someone goes back from beta to stable.
func wantsVersion(latest, current, requested string) bool {
	order := release.CompareVersions(latest, current)
	return order > 0 || (requested != "" && order != 0)
}

func (m *Manager) printLatest(version string) {
	m.UI.Blank()
	m.UI.OK(fmt.Sprintf("You have the latest version (%s)", version))
	m.UI.Blank()
}

// printAvailable is `update --notes` and `--check`: what is waiting.
func (m *Manager) printAvailable(ctx context.Context, channel, before, requested string) error {
	ctx, cancel := context.WithTimeout(ctx, metadataTimeout)
	defer cancel()
	latest, err := updates.Latest(ctx, m.releaseClient(), channel)
	if cli.CodeOf(err, "") == "SETUP_DOWNLOAD_FAILED" {
		return fmt.Errorf("NeoRouter couldn't reach GitHub to look for updates. Check the internet connection and try again.\n%w", err)
	}
	if err != nil {
		return err
	}
	if !wantsVersion(latest, before, requested) {
		m.printLatest(before)
		return nil
	}
	command := "neorouter update"
	if requested != "" {
		command += " " + requested
	}
	m.UI.Blank()
	m.UI.Item("warn", m.UI.Bold(latest+" is ready"), "you have "+before)
	m.UI.Blank()
	m.UI.Note(fmt.Sprintf("Get it with %s. What changed: https://github.com/%s/releases/tag/v%s", m.UI.Command(command), Repo, latest))
	m.UI.Blank()
	return nil
}

// printContainerUpdate explains how a container install is updated.
func (m *Manager) printContainerUpdate() error {
	cfg, err := m.loadConfig()
	if err != nil {
		return err
	}
	m.UI.Title("NeoRouter runs from a container image", "Update the image, not the code inside it:")
	m.UI.Blank()
	m.UI.Print("  " + m.UI.Command("docker compose pull && docker compose up -d"))
	m.UI.Print("    " + m.UI.Muted("for the published image"))
	m.UI.Print("  " + m.UI.Command("docker compose build --pull && docker compose up -d"))
	m.UI.Print("    " + m.UI.Muted("when built from this checkout"))
	m.UI.Blank()
	m.UI.Note(fmt.Sprintf("Data in %s is on a volume and survives the update.", cfg.Paths.Home))
	m.UI.Blank()
	return nil
}

// updateJob writes the update status file the admin UI polls. It only reports
// when the server started this update, so a plain `neorouter update` in a
// terminal stays quiet.
type updateJob struct {
	file     *updates.StatusFile
	recorder bool
}

// updateJob decides whether this run reports through the status file.
func (m *Manager) updateJob(cfg config.Config) updateJob {
	file := updates.NewStatusFile(cfg.Paths.UpdateStatusFile)
	state, _ := file.Read()["state"].(string)
	return updateJob{file: file, recorder: state == "running"}
}

// record writes one phase of the update.
func (j updateJob) record(phase string, progress int, message, state, versionAfter string, rolledBack bool) {
	if !j.recorder {
		return
	}
	patch := map[string]any{
		"phase": phase, "progress": progress, "message": message, "rolledBack": rolledBack,
		"runnerPid": os.Getpid(),
	}
	if state != "" {
		patch["state"] = state
	}
	if versionAfter != "" {
		patch["versionAfter"] = versionAfter
	}
	if state == "completed" || state == "failed" {
		patch["completedAt"] = time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
		patch["runnerPid"] = nil
	}
	_, _ = j.file.Write(patch)
	_, _ = j.file.AppendLog(message)
}

// fail records a failed update and returns the error unchanged.
func (j updateJob) fail(err error, rolledBack bool) error {
	if err != nil {
		j.record("failed", 100, err.Error(), "failed", "", rolledBack)
	}
	return err
}
