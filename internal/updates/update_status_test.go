package updates

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestStatusFileDefaultsAndMerge keeps the status file shape the CLI and the UI
// both read.
func TestStatusFileDefaultsAndMerge(t *testing.T) {
	path := filepath.Join(t.TempDir(), "update-status.json")
	file := NewStatusFile(path)
	status := file.Read()
	for key, want := range map[string]any{
		"state": "idle", "phase": "idle", "progress": 0, "message": "",
		"startedAt": nil, "completedAt": nil, "versionBefore": nil, "versionAfter": nil,
		"targetBranch": nil, "runnerPid": nil,
	} {
		if got, ok := status[key]; !ok || got != want {
			t.Fatalf("%s = %v, want %v", key, got, want)
		}
	}
	if logs, ok := status["logs"].([]any); !ok || len(logs) != 0 {
		t.Fatalf("logs = %v", status["logs"])
	}

	// A live runner keeps the job running.
	written, err := file.Write(map[string]any{
		"state": "running", "phase": "installing", "progress": 42, "runnerPid": os.Getpid(),
	})
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if written["state"] != "running" || written["phase"] != "installing" {
		t.Fatalf("written = %+v", written)
	}
	if _, ok := written["updatedAt"].(string); !ok {
		t.Fatalf("updatedAt missing: %+v", written)
	}
	if job := file.Job(); job.State != "running" || job.Phase != "installing" || job.RolledBack {
		t.Fatalf("job = %+v", job)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("mode = %v, want 0600", mode)
	}
	var stored map[string]any
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &stored); err != nil {
		t.Fatalf("status file %s: %v", raw, err)
	}
	if stored["state"] != "running" {
		t.Fatalf("stored state = %v", stored["state"])
	}
}

// TestStatusFileFailsADeadRunner keeps the rule that a runner which died without
// saying so must not leave the UI waiting.
func TestStatusFileFailsADeadRunner(t *testing.T) {
	path := filepath.Join(t.TempDir(), "update-status.json")
	deadPID := finishedPID(t)
	body, err := json.Marshal(map[string]any{
		"state": "running", "phase": "installing", "progress": 40, "runnerPid": deadPID,
		"changelog": []string{"Add a thing"}, "logs": []string{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	job := NewStatusFile(path).Job()
	if job.State != "failed" || job.Phase != "failed" {
		t.Fatalf("job = %+v", job)
	}
	if message, _ := job.Message.(string); message != "The update stopped unexpectedly. Nothing was lost; you can try again." {
		t.Fatalf("message = %v", job.Message)
	}
	if job.Progress != float64(100) && job.Progress != 100 {
		t.Fatalf("progress = %v", job.Progress)
	}
	if job.RolledBack {
		t.Fatal("rolledBack must stay false")
	}
	if logs, ok := job.Logs.([]any); !ok || len(logs) != 0 {
		t.Fatalf("logs = %v", job.Logs)
	}
}

// finishedPID returns the pid of a process that has already exited.
func finishedPID(t *testing.T) int {
	t.Helper()
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no shell to start a short-lived process")
	}
	cmd := exec.Command("sh", "-c", "exit 0")
	if err := cmd.Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	return cmd.Process.Pid
}

// TestStatusFileKeepsALiveRunner leaves a running job with a live runner alone.
func TestStatusFileKeepsALiveRunner(t *testing.T) {
	path := filepath.Join(t.TempDir(), "update-status.json")
	// The test process is the live runner.
	if _, err := NewStatusFile(path).Write(map[string]any{"state": "running", "runnerPid": os.Getpid()}); err != nil {
		t.Fatalf("write: %v", err)
	}
	if job := NewStatusFile(path).Job(); job.State != "running" {
		t.Fatalf("job = %+v", job)
	}
}

// TestAppendLogKeepsTheNewestLines covers the bounded log.
func TestAppendLogKeepsTheNewestLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "update-status.json")
	file := NewStatusFile(path)
	file.Now = func() time.Time { return time.Date(2026, 1, 3, 12, 0, 0, 0, time.UTC) }
	for i := 0; i < MaxLogLines+5; i++ {
		if _, err := file.AppendLog("line"); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	logs, ok := file.Read()["logs"].([]any)
	if !ok || len(logs) != MaxLogLines {
		t.Fatalf("logs = %d", len(logs))
	}
	first, _ := logs[0].(string)
	if first != "2026-01-03T12:00:00.000Z line" {
		t.Fatalf("first log = %q", first)
	}
}

// TestProcessAliveKeepsNodeSemantics keeps the dead-runner rule.
func TestProcessAliveKeepsNodeSemantics(t *testing.T) {
	if ProcessAlive(0) || ProcessAlive(-1) {
		t.Fatal("a non-positive pid is not a process")
	}
	if !ProcessAlive(os.Getpid()) {
		t.Fatal("this process must count as alive")
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no shell to start a short-lived process")
	}
	cmd := exec.Command("sh", "-c", "exit 0")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	if ProcessAlive(cmd.Process.Pid) {
		t.Fatalf("a finished process (pid %d) still counts as alive", cmd.Process.Pid)
	}
}
