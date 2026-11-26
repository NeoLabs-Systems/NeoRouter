package manager

// `neorouter update`: move this install to the newest release on its channel.
// The update replaces the runtime the service runs (a signed, verified archive),
// never the code inside a running image, and it rolls back when the new version
// does not answer.

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/cli"
	"github.com/NeoLabs-Systems/neolabs-go/cli/release"
	"github.com/NeoLabs-Systems/neorouter/internal/config"
	"github.com/NeoLabs-Systems/neorouter/internal/updates"
)

// cmdUpdate is `neorouter update [stable|beta] [--notes]`.
func (m *Manager) cmdUpdate(ctx context.Context, args []string) error {
	cfg, err := m.ensureDefaultEnvFile()
	if err != nil {
		return err
	}
	if m.inContainer() {
		// Updating the code in place inside a running image is not a thing: the
		// image is replaced.
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

	notesOnly := cli.HasFlag(args, "--notes")
	requested := ""
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			continue
		}
		requested = updates.ParseChannel(arg)
		if requested == "" {
			return inputError("Usage: neorouter update [stable|beta] [--notes]")
		}
		break
	}
	channel := requested
	if channel == "" {
		channel = m.currentChannel(cfg)
	}
	if requested != "" && !notesOnly {
		if _, err := updates.WriteChannelToEnvFile(channel, cfg.Paths.EnvFile); err != nil {
			return err
		}
		_ = os.Setenv(updates.ChannelEnvKey, channel)
	}

	versionBefore := m.Version
	if current, err := activeRuntimeVersion(cfg); err == nil {
		versionBefore = current
	}
	job := m.updateJob(cfg, versionBefore)
	if notesOnly {
		return m.printUpdateNotes(ctx, cfg, channel, versionBefore)
	}

	// An install that still has a Node.js database or service finishes its
	// migration before the update restarts anything.
	if legacy := InspectLegacyJS(cfg, m.UserHome, m.Getenv); legacy.NeedsMigration {
		if cfg, err = m.migrateLegacyIfNeeded(ctx, cfg); err != nil {
			return job.fail(err, false)
		}
		m.RestartAfterLegacyMigration = true
		versionBefore = m.Version
		job = m.updateJob(cfg, versionBefore)
	}

	job.record("checking", 15, "Checking the latest "+channel+" release on GitHub", "running", "", false)
	rel, err := m.releaseClient().ResolveRelease(ctx, Repo, channel, RuntimeManifestPrefix)
	if err != nil {
		return job.fail(err, false)
	}
	targetVersion := strings.TrimPrefix(rel.Tag, "v")
	if release.CompareVersions(targetVersion, versionBefore) <= 0 && !cli.HasFlag(args, "--force") {
		job.record("completed", 100, fmt.Sprintf("NeoRouter is up to date (%s on %s).", versionBefore, channel), "completed", versionBefore, false)
		m.UI.Blank()
		m.UI.OK(fmt.Sprintf("You have the latest version (%s)", versionBefore))
		m.UI.Blank()
		return nil
	}
	if !hasManifest(rel.Assets, targetVersion) {
		return job.fail(fmt.Errorf("the selected release does not contain a signed runtime manifest"), false)
	}
	rolledBack, err := m.updateSignedRuntime(ctx, cfg, rel, versionBefore, targetVersion, &job)
	if err != nil {
		return job.fail(err, rolledBack)
	}
	m.Version = targetVersion
	job.record("completed", 100, fmt.Sprintf("Updated NeoRouter to %s.", targetVersion), "completed", targetVersion, false)
	m.UI.Blank()
	m.UI.Item("ok", m.UI.Bold("NeoRouter "+m.versionLabel()+" is ready"), appURL(m.loadEnvPort()))
	m.printChanges("What's new", m.pendingChanges(ctx, cfg, channel))
	m.UI.Blank()
	m.UI.Note("Your settings, keys and usage were kept.")
	m.UI.Blank()
	return nil
}

// printUpdateNotes is `update --notes`: what is new, without touching anything.
func (m *Manager) printUpdateNotes(ctx context.Context, cfg config.Config, channel, versionBefore string) error {
	m.UI.Blank()
	if !m.isGitCheckout() {
		m.UI.Info("This install didn't come from Git, so there are no notes to show.")
		m.UI.Blank()
		return nil
	}
	git := updates.NewGit(updates.ExecRunner(cfg.AppDir))
	if !git.FetchRemote(ctx) {
		m.UI.Warn("NeoRouter couldn't reach GitHub to look for updates. Check the internet connection and try again.")
		m.UI.Blank()
		return nil
	}
	branch, err := updates.ResolvePreferredBranch(ctx, channel, updates.ExecRunner(cfg.AppDir), func(note string) { m.UI.Warn(note) })
	if err != nil {
		return err
	}
	pending, ok := git.PendingUpdate(ctx, branch, false)
	if !ok {
		m.UI.OK(fmt.Sprintf("You have the latest version (%s)", versionBefore))
		m.UI.Blank()
		return nil
	}
	next := versionBefore
	if pending.Version != nil {
		next = *pending.Version
	}
	m.UI.Item("warn", m.UI.Bold(next+" is ready"), "you have "+versionBefore)
	m.printChanges("What's new", git.ChangesBetween(ctx, git.Head(ctx).SHA, pending.RemoteRef))
	m.UI.Blank()
	m.UI.Note(fmt.Sprintf("Get it with %s.", m.UI.Command("neorouter update")))
	m.UI.Blank()
	return nil
}

// pendingChanges is the "What's new" list of a finished update: the commits the
// channel's branch gained since this checkout's HEAD, when there is one.
func (m *Manager) pendingChanges(ctx context.Context, cfg config.Config, channel string) []string {
	if !m.isGitCheckout() {
		return nil
	}
	git := updates.NewGit(updates.ExecRunner(cfg.AppDir))
	branch, err := updates.ResolvePreferredBranch(ctx, channel, updates.ExecRunner(cfg.AppDir), nil)
	if err != nil {
		return nil
	}
	return git.ChangesBetween(ctx, git.Head(ctx).SHA, "origin/"+branch)
}

// printChanges prints a titled list of changes, and nothing when there are none.
func (m *Manager) printChanges(title string, changes []string) {
	if len(changes) == 0 {
		return
	}
	m.UI.Section(title)
	for _, change := range changes {
		m.UI.Item("info", change, "")
	}
}

// updateJob writes the update status file the admin UI polls. It only reports
// when this process is the update runner the server started (or when --apply
// asked for it), so a plain `neorouter update` in a terminal stays quiet.
type updateJob struct {
	file     *updates.StatusFile
	version  string
	recorder bool
}

// updateJob decides whether this run reports through the status file.
func (m *Manager) updateJob(cfg config.Config, versionBefore string) updateJob {
	file := updates.NewStatusFile(cfg.Paths.UpdateStatusFile)
	state, _ := file.Read()["state"].(string)
	return updateJob{file: file, version: versionBefore, recorder: state == "running"}
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
		patch["completedAt"] = isoTimestamp(time.Now())
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
