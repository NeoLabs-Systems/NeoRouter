package updates

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/NeoLabs-Systems/neorouter/internal/config"
	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
)

const (
	// fetchRemoteTimeout bounds the network fetch the admin UI triggers.
	fetchRemoteTimeout = 20 * time.Second
	// blockedContainer and blockedNotGit are the two reasons an install cannot
	// update itself.
	blockedContainer = "container"
	blockedNotGit    = "not_git"
)

// Status is the body of GET /api/v1/admin/update, in update_service.js order.
type Status struct {
	Version       any              `json:"version"`
	Channel       string           `json:"channel"`
	ChannelLabel  string           `json:"channelLabel"`
	SelfUpdate    bool             `json:"selfUpdate"`
	BlockedReason any              `json:"blockedReason"`
	CheckedAt     any              `json:"checkedAt"`
	CheckFailed   bool             `json:"checkFailed"`
	Available     *AvailableUpdate `json:"available"`
	Job           Job              `json:"job"`
}

// lastCheck is what the last successful check answered, kept so an offline
// check does not hide an update banner.
type lastCheck struct {
	checkedAt *string
	checked   bool
	update    *AvailableUpdate
	reason    string
	channel   string
}

// Service is the update side of the admin API: checking for a newer release and
// handing the update itself to a detached runner.
type Service struct {
	// Cfg is read per call so a reload is picked up.
	Cfg func() config.Config
	// Version is the running build's version.
	Version string
	// Getenv reads the environment; nil means the process environment.
	Getenv func(string) string
	// Log receives check warnings and runner failures; nil uses slog's default.
	Log *slog.Logger
	// RunnerCommand is the detached update runner; nil means this binary's own
	// `update` command.
	RunnerCommand []string
	// Now overrides the clock in tests.
	Now func() time.Time

	mu       sync.Mutex
	last     lastCheck
	override string
}

func (s *Service) cfg() config.Config {
	if s.Cfg != nil {
		return s.Cfg()
	}
	return config.Config{}
}

func (s *Service) getenv() func(string) string {
	if s.Getenv != nil {
		return s.Getenv
	}
	return os.Getenv
}

func (s *Service) log() *slog.Logger {
	if s.Log != nil {
		return s.Log
	}
	return slog.Default()
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// StatusFile is the update status file of this install.
func (s *Service) StatusFile() *StatusFile {
	file := NewStatusFile(s.cfg().Paths.UpdateStatusFile)
	file.Now = s.Now
	return file
}

// Channel is the release channel this install follows.
func (s *Service) Channel() string {
	s.mu.Lock()
	override := s.override
	s.mu.Unlock()
	if override != "" {
		return override
	}
	return ReadConfiguredChannel(s.getenv(), s.cfg().Paths.EnvFile)
}

// InstalledVersion is the running build's version, or nil when it is unknown.
func (s *Service) InstalledVersion() any {
	if s.Version == "" {
		return nil
	}
	return s.Version
}

// BlockedReason is why this install cannot update itself, or nil.
func (s *Service) BlockedReason() *string {
	if DetectContainer(s.getenv()) {
		reason := blockedContainer
		return &reason
	}
	if _, err := os.Stat(filepath.Join(s.cfg().AppDir, ".git")); err != nil {
		reason := blockedNotGit
		return &reason
	}
	return nil
}

// Status is update_service.js status.
func (s *Service) Status() Status {
	channel := s.Channel()
	blocked := s.BlockedReason()

	s.mu.Lock()
	last := s.last
	s.mu.Unlock()

	var checkedAt any
	checkFailed := false
	var available *AvailableUpdate
	// A check for another channel says nothing about this one.
	if last.channel == channel {
		if last.checkedAt != nil {
			checkedAt = *last.checkedAt
			checkFailed = !last.checked
		}
		available = last.update
	}
	var blockedReason any
	if blocked != nil {
		blockedReason = *blocked
	}
	status := Status{
		Version:       s.InstalledVersion(),
		Channel:       channel,
		ChannelLabel:  ChannelLabel(channel),
		SelfUpdate:    blocked == nil,
		BlockedReason: blockedReason,
		CheckedAt:     checkedAt,
		CheckFailed:   checkFailed,
		Available:     available,
		Job:           s.StatusFile().Job(),
	}
	return status
}

// Check asks the channel's branch for a newer version and reports the status.
// A check that fails keeps the last good answer.
func (s *Service) Check(ctx context.Context) Status {
	channel := s.Channel()
	if s.BlockedReason() != nil {
		return s.Status()
	}
	result := CheckForUpdate(ctx, ExecRunner(s.cfg().AppDir), CheckOptions{
		Channel:     channel,
		FetchRemote: s.fetchRemote,
		OnFallback:  func(message string) { s.log().Warn(message) },
	})
	s.mu.Lock()
	if result.Checked || !s.last.checked || s.last.channel != channel {
		checkedAt := isoMillis(s.now())
		s.last = lastCheck{
			checkedAt: &checkedAt,
			checked:   result.Checked,
			update:    result.Update,
			reason:    result.Reason,
			channel:   channel,
		}
	}
	s.mu.Unlock()
	return s.Status()
}

// fetchRemote fetches origin with the timeout the admin path allows.
func (s *Service) fetchRemote(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, fetchRemoteTimeout)
	defer cancel()
	return NewGit(ExecRunner(s.cfg().AppDir)).FetchRemote(ctx)
}

// assertIdle refuses an update on an install that cannot take one, or while one
// is already running.
func (s *Service) assertIdle() error {
	switch reason := s.BlockedReason(); {
	case reason != nil && *reason == blockedContainer:
		return httpserver.New(http.StatusConflict, "UPDATE_CONTAINER",
			"NeoRouter runs from a container image. Pull the new image and recreate the container.")
	case reason != nil:
		return httpserver.New(http.StatusConflict, "UPDATE_NOT_GIT",
			"This install did not come from Git, so it cannot update itself.")
	}
	if s.StatusFile().Read()["state"] == "running" {
		return httpserver.New(http.StatusConflict, "UPDATE_RUNNING", "An update is already running.")
	}
	return nil
}

// Start hands the update to a detached runner and reports the status of the job
// it just started.
func (s *Service) Start() (Status, error) {
	if err := s.assertIdle(); err != nil {
		return Status{}, err
	}
	cfg := s.cfg()
	if err := cfg.Paths.Ensure(); err != nil {
		return Status{}, fmt.Errorf("updates: prepare runtime directories: %w", err)
	}
	file := s.StatusFile()
	// A runner in the service's own cgroup would be stopped by the service stop
	// the update needs, so the JS updater moves it out with systemd-run.
	command := s.runnerCommand()
	s.mu.Lock()
	last := s.last
	s.mu.Unlock()
	versionAfter := any(nil)
	changelog := any([]string{})
	if last.update != nil {
		versionAfter = last.update.Version
		changelog = last.update.Changes
	}
	if _, err := file.Write(map[string]any{
		"state": "running", "progress": 1, "phase": "starting", "message": "Starting the update",
		"startedAt": isoMillis(s.now()), "completedAt": nil, "versionBefore": s.InstalledVersion(),
		"versionAfter": versionAfter, "rolledBack": false, "changelog": changelog,
		"logs": []any{}, "runnerPid": os.Getpid(),
	}); err != nil {
		return Status{}, err
	}

	logPath := filepath.Join(cfg.Paths.Logs, "update.log")
	output, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return Status{}, fmt.Errorf("updates: open update log: %w", err)
	}
	defer output.Close()

	cmd := exec.Command(command[0], command[1:]...)
	cmd.Dir = cfg.AppDir
	cmd.Stdout = output
	cmd.Stderr = output
	cmd.Stdin = nil
	detach(cmd)
	if err := cmd.Start(); err != nil {
		_, _ = file.Write(map[string]any{
			"state": "failed", "phase": "failed", "runnerPid": nil, "completedAt": isoMillis(s.now()),
			"message": "The update could not start: " + err.Error(),
		})
		s.log().Error("The update runner could not start", "error", err)
		return Status{}, fmt.Errorf("updates: start update runner: %w", err)
	}
	pid := cmd.Process.Pid
	if _, err := file.Write(map[string]any{"runnerPid": pid}); err != nil {
		return Status{}, err
	}
	_ = cmd.Process.Release()
	s.log().Info("Update started", "runner", command[0], "pid", pid)
	return s.Status(), nil
}

// runnerCommand is the detached update runner: systemd-run moves it out of the
// service's cgroup when the server itself runs under systemd.
func (s *Service) runnerCommand() []string {
	command := s.RunnerCommand
	if len(command) == 0 {
		exe, err := os.Executable()
		if err != nil {
			exe = "neorouter"
		}
		command = []string{exe, "update"}
	}
	if runtime.GOOS == "linux" && s.getenv()("INVOCATION_ID") != "" {
		wrapped := []string{"systemd-run", "--user", "--scope", "--quiet", "--collect"}
		return append(wrapped, command...)
	}
	return command
}

// SetChannel stores the channel, then checks it.
func (s *Service) SetChannel(ctx context.Context, requested string) (Status, error) {
	if err := s.assertIdle(); err != nil {
		return Status{}, err
	}
	channel := ParseChannel(requested)
	if channel == "" {
		return Status{}, httpserver.New(http.StatusBadRequest, "INVALID_CHANNEL",
			`The release channel must be "stable" or "beta".`)
	}
	if _, err := WriteChannelToEnvFile(channel, s.cfg().Paths.EnvFile); err != nil {
		return Status{}, fmt.Errorf("updates: store release channel: %w", err)
	}
	s.mu.Lock()
	s.override = channel
	s.mu.Unlock()
	return s.Check(ctx), nil
}
