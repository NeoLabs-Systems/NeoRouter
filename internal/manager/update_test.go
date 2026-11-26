package manager

// The status file the admin UI polls, and the health check an update waits on.

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/NeoLabs-Systems/neorouter/internal/updates"
)

func TestUpdateStatusIsWrittenOnlyWhenTheServerStartedTheUpdate(t *testing.T) {
	s := newTestSetup(t)
	cfg := s.config(t)
	quiet := s.m.updateJob(cfg)
	quiet.record("backup", 15, "Saving a backup", "running", "", false)
	if exists(cfg.Paths.UpdateStatusFile) {
		t.Error("a plain CLI update wrote the update status file")
	}
	file := updates.NewStatusFile(cfg.Paths.UpdateStatusFile)
	// The server records its own pid as the runner's when it starts one.
	if _, err := file.Write(map[string]any{"state": "running", "phase": "starting", "runnerPid": os.Getpid()}); err != nil {
		t.Fatalf("seed status file: %v", err)
	}
	runner := s.m.updateJob(cfg)
	runner.record("downloading", 35, "Downloading the new version", "running", "", false)
	status := file.Read()
	if status["phase"] != "downloading" || status["progress"] != float64(35) {
		t.Errorf("status = %v, want the download phase", status)
	}
	if logs, ok := status["logs"].([]any); !ok || len(logs) != 1 {
		t.Errorf("logs = %v, want the phase in them", status["logs"])
	}
	runner.record("completed", 100, "Updated NeoRouter to 0.3.0.", "completed", "0.3.0", false)
	status = file.Read()
	if status["state"] != "completed" || status["completedAt"] == nil || status["runnerPid"] != nil {
		t.Errorf("finished status = %v", status)
	}
}

func TestWaitForServerVersionWaitsForItsOwnVersion(t *testing.T) {
	s := newTestSetup(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"version":"0.2.1"}`)
	}))
	defer server.Close()
	port := 0
	if _, err := fmt.Sscanf(strings.TrimPrefix(server.URL, "http://127.0.0.1:"), "%d", &port); err != nil {
		t.Fatalf("port from %s: %v", server.URL, err)
	}
	err := s.m.waitForServerVersion(s.ctx, port, "0.3.0", 300e6)
	if err == nil || !strings.Contains(err.Error(), "did not report version 0.3.0") {
		t.Fatalf("err = %v, want a version mismatch", err)
	}
	if err := s.m.waitForServerVersion(s.ctx, port, "0.2.1", 300e6); err != nil {
		t.Fatalf("waiting for the running version: %v", err)
	}
}
