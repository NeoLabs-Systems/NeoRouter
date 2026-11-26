package updates

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// MaxLogLines is how many update log lines the status file keeps.
const MaxLogLines = 220

// DefaultUpdateStatus is the status of an install that is not updating: every
// key the admin UI reads is present.
func DefaultUpdateStatus() map[string]any {
	return map[string]any{
		"state":         "idle",
		"progress":      0,
		"phase":         "idle",
		"message":       "",
		"startedAt":     nil,
		"completedAt":   nil,
		"versionBefore": nil,
		"versionAfter":  nil,
		"targetBranch":  nil,
		"runnerPid":     nil,
		"changelog":     []any{},
		"logs":          []any{},
	}
}

// StatusFile is data/update-status.json, the file the admin UI and the update
// runner talk through: the runner is a separate process because it has to
// outlive the server it restarts.
type StatusFile struct {
	Path string
	// Now overrides the clock in tests.
	Now func() time.Time
}

// NewStatusFile is the status file at path.
func NewStatusFile(path string) *StatusFile { return &StatusFile{Path: path} }

func (f *StatusFile) nowText() string {
	now := time.Now()
	if f.Now != nil {
		now = f.Now()
	}
	return isoMillis(now)
}

// Read returns the status, with a runner that died without saying so read as
// failed so the UI never waits forever.
func (f *StatusFile) Read() map[string]any {
	status := DefaultUpdateStatus()
	raw, err := os.ReadFile(f.Path)
	if err == nil {
		var saved map[string]any
		if json.Unmarshal(raw, &saved) == nil {
			for key, value := range saved {
				status[key] = value
			}
		}
	}
	return f.normalize(status)
}

// Write merges patch into the status and saves it atomically (0600), returning
// the stored status.
func (f *StatusFile) Write(patch map[string]any) (map[string]any, error) {
	status := f.Read()
	for key, value := range patch {
		status[key] = value
	}
	status["updatedAt"] = f.nowText()
	status = f.normalize(status)
	body, err := json.MarshalIndent(status, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("updates: encode update status: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(f.Path), 0o700); err != nil {
		return nil, fmt.Errorf("updates: create status directory: %w", err)
	}
	tmp := fmt.Sprintf("%s.%d.tmp", f.Path, os.Getpid())
	if err := os.WriteFile(tmp, body, 0o600); err != nil {
		return nil, fmt.Errorf("updates: write update status: %w", err)
	}
	if err := os.Rename(tmp, f.Path); err != nil {
		_ = os.Remove(tmp)
		return nil, fmt.Errorf("updates: replace update status: %w", err)
	}
	return status, nil
}

// AppendLog adds a timestamped line to the status log, keeping the newest
// MaxLogLines.
func (f *StatusFile) AppendLog(message string) (map[string]any, error) {
	logs := anySlice(f.Read()["logs"])
	logs = append(logs, f.nowText()+" "+message)
	if len(logs) > MaxLogLines {
		logs = logs[len(logs)-MaxLogLines:]
	}
	return f.Write(map[string]any{"logs": logs})
}

// normalize fills in a status and turns a runner that is gone into a failure.
func (f *StatusFile) normalize(status map[string]any) map[string]any {
	if status["state"] != "running" || processAlive(pidOf(status["runnerPid"])) {
		return status
	}
	status["state"], status["progress"], status["phase"] = "failed", 100, "failed"
	status["message"] = "The update stopped unexpectedly. Nothing was lost; you can try again."
	if status["completedAt"] == nil {
		status["completedAt"] = f.nowText()
	}
	status["runnerPid"] = nil
	return status
}

// Job is the status as the admin UI reads it.
type Job struct {
	State         string `json:"state"`
	Phase         string `json:"phase"`
	Progress      any    `json:"progress"`
	Message       any    `json:"message"`
	VersionBefore any    `json:"versionBefore"`
	VersionAfter  any    `json:"versionAfter"`
	Changelog     any    `json:"changelog"`
	RolledBack    bool   `json:"rolledBack"`
	Logs          any    `json:"logs"`
	StartedAt     any    `json:"startedAt"`
	CompletedAt   any    `json:"completedAt"`
}

// Job reads the status file as the admin UI's job object: same keys in the same
// order, missing values as null, and rolledBack always present.
func (f *StatusFile) Job() Job {
	status := f.Read()
	return Job{
		State:         text(status["state"]),
		Phase:         text(status["phase"]),
		Progress:      status["progress"],
		Message:       status["message"],
		VersionBefore: status["versionBefore"],
		VersionAfter:  status["versionAfter"],
		Changelog:     status["changelog"],
		RolledBack:    truthy(status["rolledBack"]),
		Logs:          status["logs"],
		StartedAt:     status["startedAt"],
		CompletedAt:   status["completedAt"],
	}
}

// ProcessAlive reports whether pid is a running process. A process owned by
// somebody else still counts as alive.
func ProcessAlive(pid int) bool { return processAlive(pid) }

// pidOf reads a pid out of a decoded JSON value.
func pidOf(value any) int {
	switch n := value.(type) {
	case float64:
		if n != float64(int(n)) {
			return 0
		}
		return int(n)
	case int:
		return n
	case string:
		if pid, err := strconv.Atoi(n); err == nil {
			return pid
		}
	}
	return 0
}

func text(value any) string {
	if s, ok := value.(string); ok {
		return s
	}
	return ""
}

// truthy is truthiness of the values a status file holds.
func truthy(value any) bool {
	switch v := value.(type) {
	case nil:
		return false
	case bool:
		return v
	case float64:
		return v != 0
	case string:
		return v != ""
	}
	return true
}

// anySlice reads a decoded JSON array, or an empty one.
func anySlice(value any) []any {
	if values, ok := value.([]any); ok {
		return values
	}
	return []any{}
}

// isoMillis formats ISO 8601 UTC with millisecond precision.
func isoMillis(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000Z")
}
