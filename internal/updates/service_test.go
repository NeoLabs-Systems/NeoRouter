package updates

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/NeoLabs-Systems/neorouter/internal/config"
)

// fixture is one install: a runtime home, an app directory and an env-driven
// configuration, like test/api/updates.test.js builds through createApp.
type fixture struct {
	service *Service
	mux     *http.ServeMux
	env     map[string]string
	cfg     config.Config
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	home := t.TempDir()
	appDir := t.TempDir()
	cfg := config.Config{
		AppDir: appDir,
		Paths: config.Paths{
			Home:             home,
			Data:             filepath.Join(home, "data"),
			Logs:             filepath.Join(home, "logs"),
			Backups:          filepath.Join(home, "backups"),
			EnvFile:          filepath.Join(home, ".env"),
			UpdateStatusFile: filepath.Join(home, "data", "update-status.json"),
		},
	}
	env := map[string]string{}
	service := &Service{
		Cfg:     func() config.Config { return cfg },
		Version: "0.2.1",
		Getenv:  func(k string) string { return env[k] },
		Log:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	mux := http.NewServeMux()
	service.Routes(mux, adminGate)
	return &fixture{service: service, mux: mux, env: env, cfg: cfg}
}

// makeCheckout turns the fixture's app directory into a git install, which is
// what a GitHub-clone install looks like.
func (f *fixture) makeCheckout(t *testing.T) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(f.cfg.AppDir, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(f.cfg.Paths.Data, 0o700); err != nil {
		t.Fatal(err)
	}
}

// adminGate stands in for the auth and teams middleware: the real chain arrives
// with internal/app.
func adminGate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Test-Role") != "admin" {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":{"code":"ADMIN_REQUIRED","message":"Administrator privileges are required."}}`))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (f *fixture) call(t *testing.T, method, path string, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("X-Test-Role", "admin")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	f.mux.ServeHTTP(rec, req)
	return rec
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var value T
	if err := json.Unmarshal(rec.Body.Bytes(), &value); err != nil {
		t.Fatalf("body %s: %v", rec.Body.String(), err)
	}
	return value
}

// TestOnlyAdminsSeeTheUpdateStatus is the first case of
// test/api/updates.test.js: the gate decides, and an idle install reports an
// idle job, a channel and no update.
func TestOnlyAdminsSeeTheUpdateStatus(t *testing.T) {
	f := newFixture(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/update", nil)
	rec := httptest.NewRecorder()
	f.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("member status = %d, want 403", rec.Code)
	}

	rec = f.call(t, http.MethodGet, "/api/v1/admin/update", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("admin status = %d, body = %s", rec.Code, rec.Body.String())
	}
	body := decode[struct {
		Version      *string `json:"version"`
		Channel      string  `json:"channel"`
		ChannelLabel string  `json:"channelLabel"`
		SelfUpdate   bool    `json:"selfUpdate"`
		BlockedReasn *string `json:"blockedReason"`
		CheckedAt    *string `json:"checkedAt"`
		CheckFailed  bool    `json:"checkFailed"`
		Available    any     `json:"available"`
		Job          struct {
			State string `json:"state"`
		} `json:"job"`
	}](t, rec)

	if body.Job.State != "idle" {
		t.Fatalf("job.state = %q", body.Job.State)
	}
	if body.Channel != "stable" && body.Channel != "beta" {
		t.Fatalf("channel = %q", body.Channel)
	}
	if body.ChannelLabel != "Stable" && body.ChannelLabel != "Beta" {
		t.Fatalf("channelLabel = %q", body.ChannelLabel)
	}
	if body.Available != nil {
		t.Fatalf("available = %v, want null", body.Available)
	}
	if body.BlockedReasn == nil || *body.BlockedReasn != blockedNotGit {
		t.Fatalf("blockedReason = %v, want not_git", body.BlockedReasn)
	}
	if body.SelfUpdate {
		t.Fatal("an install without a checkout must not claim self-update")
	}
	// Key order is part of the contract.
	want := []string{`"version"`, `"channel"`, `"channelLabel"`, `"selfUpdate"`, `"blockedReason"`,
		`"checkedAt"`, `"checkFailed"`, `"available"`, `"job"`}
	at := 0
	for _, key := range want {
		i := strings.Index(rec.Body.String()[at:], key)
		if i < 0 {
			t.Fatalf("key %s out of order in %s", key, rec.Body.String())
		}
		at += i + len(key) - 1
	}
}

// TestAContainerCannotUpdateItself is the second case: the container reason is
// reported and the start is refused.
func TestAContainerCannotUpdateItself(t *testing.T) {
	f := newFixture(t)
	f.env["NEOROUTER_CONTAINER"] = "1"

	rec := f.call(t, http.MethodPost, "/api/v1/admin/update", "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	body := decode[struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}](t, rec)
	if body.Error.Code != "UPDATE_CONTAINER" {
		t.Fatalf("code = %q", body.Error.Code)
	}
	if body.Error.Message != "NeoRouter runs from a container image. Pull the new image and recreate the container." {
		t.Fatalf("message = %q", body.Error.Message)
	}
	status := decode[struct {
		BlockedReason *string `json:"blockedReason"`
		SelfUpdate    bool    `json:"selfUpdate"`
	}](t, f.call(t, http.MethodGet, "/api/v1/admin/update", ""))
	if status.BlockedReason == nil || *status.BlockedReason != blockedContainer {
		t.Fatalf("blockedReason = %v", status.BlockedReason)
	}
	if status.SelfUpdate {
		t.Fatal("a container must not claim self-update")
	}
}

// TestAnUnknownChannelIsRefused is the third case.
func TestAnUnknownChannelIsRefused(t *testing.T) {
	f := newFixture(t)
	f.makeCheckout(t)
	rec := f.call(t, http.MethodPut, "/api/v1/admin/update/channel", `{"channel":"nightly"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	body := decode[struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}](t, rec)
	if body.Error.Code != "INVALID_CHANNEL" {
		t.Fatalf("code = %q", body.Error.Code)
	}
	if body.Error.Message != `The release channel must be "stable" or "beta".` {
		t.Fatalf("message = %q", body.Error.Message)
	}
}

// TestChannelBodyValidation keeps the admin router's zod schema: one string of
// at most 16 characters.
func TestChannelBodyValidation(t *testing.T) {
	f := newFixture(t)
	f.makeCheckout(t)
	cases := []struct {
		body    string
		message string
	}{
		{`{}`, "channel: Required"},
		{`{"channel":5}`, "channel: Expected string, received number"},
		{`{"channel":"` + strings.Repeat("x", 17) + `"}`, "channel: String must contain at most 16 character(s)"},
	}
	for _, c := range cases {
		rec := f.call(t, http.MethodPut, "/api/v1/admin/update/channel", c.body)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("body %s status = %d", c.body, rec.Code)
		}
		body := decode[struct {
			Error struct {
				Code    string         `json:"code"`
				Message string         `json:"message"`
				Details map[string]any `json:"details"`
			} `json:"error"`
		}](t, rec)
		if body.Error.Code != "VALIDATION_ERROR" || body.Error.Message != c.message {
			t.Fatalf("body %s error = %+v", c.body, body.Error)
		}
		if body.Error.Details == nil {
			t.Fatalf("body %s has no details", c.body)
		}
	}
}

// TestSetChannelStoresAndChecks writes the channel to the .env file and reports
// the new channel.
func TestSetChannelStoresAndChecks(t *testing.T) {
	f := newFixture(t)
	f.makeCheckout(t)
	rec := f.call(t, http.MethodPut, "/api/v1/admin/update/channel", `{"channel":"stable"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	body := decode[struct {
		Channel      string `json:"channel"`
		ChannelLabel string `json:"channelLabel"`
	}](t, rec)
	if body.Channel != "stable" || body.ChannelLabel != "Stable" {
		t.Fatalf("body = %+v", body)
	}
	raw, err := os.ReadFile(f.cfg.Paths.EnvFile)
	if err != nil {
		t.Fatalf("env file: %v", err)
	}
	if strings.TrimSpace(string(raw)) != "NEOROUTER_RELEASE_CHANNEL=stable" {
		t.Fatalf("env file = %q", raw)
	}
	// The check that follows the switch cannot reach a remote here, which is
	// reported as a failed check rather than a broken response.
	status := decode[struct {
		CheckFailed bool    `json:"checkFailed"`
		CheckedAt   *string `json:"checkedAt"`
	}](t, f.call(t, http.MethodGet, "/api/v1/admin/update", ""))
	if !status.CheckFailed || status.CheckedAt == nil {
		t.Fatalf("status = %+v, want a failed check", status)
	}
}

// TestStartRunsADetachedRunner starts a real (harmless) runner, reports the job
// as running, and refuses a second start.
func TestStartRunsADetachedRunner(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the detached runner fixture is a POSIX shell")
	}
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("no /bin/sh to fake the update runner")
	}
	f := newFixture(t)
	f.makeCheckout(t)
	f.service.RunnerCommand = []string{"/bin/sh", "-c", "sleep 30"}

	rec := f.call(t, http.MethodPost, "/api/v1/admin/update", "")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	body := decode[struct {
		Job struct {
			State         string  `json:"state"`
			Phase         string  `json:"phase"`
			Progress      float64 `json:"progress"`
			Message       string  `json:"message"`
			VersionBefore *string `json:"versionBefore"`
			RunnerAlive   bool    `json:"-"`
		} `json:"job"`
	}](t, rec)
	if body.Job.State != "running" || body.Job.Phase != "starting" || body.Job.Progress != 1 {
		t.Fatalf("job = %+v", body.Job)
	}
	if body.Job.Message != "Starting the update" {
		t.Fatalf("message = %q", body.Job.Message)
	}
	if body.Job.VersionBefore == nil || *body.Job.VersionBefore != "0.2.1" {
		t.Fatalf("versionBefore = %v", body.Job.VersionBefore)
	}
	t.Cleanup(func() {
		if pid := pidOf(f.service.StatusFile().Read()["runnerPid"]); pid > 0 {
			if process, err := os.FindProcess(pid); err == nil {
				_ = process.Kill()
			}
		}
	})

	second := f.call(t, http.MethodPost, "/api/v1/admin/update", "")
	if second.Code != http.StatusConflict {
		t.Fatalf("second start status = %d, body = %s", second.Code, second.Body.String())
	}
	if code := decode[struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}](t, second).Error.Code; code != "UPDATE_RUNNING" {
		t.Fatalf("code = %q, want UPDATE_RUNNING", code)
	}
	logged, err := os.ReadFile(filepath.Join(f.cfg.Paths.Logs, "update.log"))
	if err != nil {
		t.Fatalf("update log: %v", err)
	}
	if len(logged) != 0 {
		t.Fatalf("update log = %q", logged)
	}
}

// TestFailedCheckKeepsTheLastAnswer covers the offline rule: a good answer
// survives, and the channel it belongs to is remembered.
func TestFailedCheckKeepsTheLastAnswer(t *testing.T) {
	f := newFixture(t)
	f.makeCheckout(t)
	version := "0.3.0"
	f.service.mu.Lock()
	f.service.last = lastCheck{
		checked: true,
		update:  &AvailableUpdate{Version: &version, Branch: "beta", Changes: []string{"Add a thing"}},
		channel: DefaultChannel,
	}
	f.service.mu.Unlock()
	checkedAt := isoMillis(f.service.now())
	f.service.mu.Lock()
	f.service.last.checkedAt = &checkedAt
	f.service.mu.Unlock()

	status := f.service.Status()
	if status.Available == nil || status.Available.Version == nil || *status.Available.Version != "0.3.0" {
		t.Fatalf("available = %+v", status.Available)
	}
	if status.CheckFailed {
		t.Fatal("a good check must not read as failed")
	}

	// A check for another channel says nothing about this one.
	f.service.mu.Lock()
	f.service.last.channel = "stable"
	f.service.mu.Unlock()
	if status := f.service.Status(); status.Available != nil || status.CheckedAt != nil || status.CheckFailed {
		t.Fatalf("a foreign channel leaked into the status: %+v", status)
	}
}

// TestCheckOnARealRepository exercises the git path end to end: a temp clone
// with its own origin answers "no update" for the branch it is on.
func TestCheckOnARealRepository(t *testing.T) {
	if _, err := execLookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	origin := t.TempDir()
	runGitCommand(t, origin, "init", "--bare", "-q")
	work := t.TempDir()
	runGitCommand(t, work, "init", "-q", "-b", "beta")
	writeFile(t, filepath.Join(work, "package.json"), `{"version":"0.2.1"}`)
	runGitCommand(t, work, "add", ".")
	runGitCommand(t, work, "-c", "user.email=t@example.test", "-c", "user.name=t", "commit", "-q", "-m", "init")
	runGitCommand(t, work, "remote", "add", "origin", origin)
	runGitCommand(t, work, "push", "-q", "origin", "beta")

	f := newFixture(t)
	cfg := f.cfg
	cfg.AppDir = work
	f.service.Cfg = func() config.Config { return cfg }
	if err := os.MkdirAll(cfg.Paths.Data, 0o700); err != nil {
		t.Fatal(err)
	}

	status := f.service.Check(context.Background())
	if status.BlockedReason != nil {
		t.Fatalf("blockedReason = %v", status.BlockedReason)
	}
	if status.Available != nil {
		t.Fatalf("available = %+v, want none", status.Available)
	}
	if status.CheckFailed {
		t.Fatalf("checkFailed after a successful check")
	}
	if status.Channel != DefaultChannel {
		t.Fatalf("channel = %q", status.Channel)
	}
	if status.CheckedAt == nil {
		t.Fatal("checkedAt is missing after a check")
	}
}

// execLookPath is exec.LookPath for the git guard above.
func execLookPath(name string) (string, error) { return exec.LookPath(name) }

// runGitCommand runs one git command in dir, failing the test on error.
func runGitCommand(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// writeFile writes one file in a test.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestStatusBytesMatchTheNodeServer pins the exact body the Node admin endpoint
// returns for an idle install (captured from server/services/updates on the
// beta branch): same keys, same order, same nulls and empty arrays.
func TestStatusBytesMatchTheNodeServer(t *testing.T) {
	f := newFixture(t)
	f.makeCheckout(t)
	const oracle = `{"version":"0.2.1","channel":"beta","channelLabel":"Beta","selfUpdate":true,` +
		`"blockedReason":null,"checkedAt":null,"checkFailed":false,"available":null,` +
		`"job":{"state":"idle","phase":"idle","progress":0,"message":"","versionBefore":null,` +
		`"versionAfter":null,"changelog":[],"rolledBack":false,"logs":[],"startedAt":null,"completedAt":null}}`
	rec := f.call(t, http.MethodGet, "/api/v1/admin/update", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := rec.Body.String(); got != oracle {
		t.Fatalf("body mismatch\n want %s\n  got %s", oracle, got)
	}
}
