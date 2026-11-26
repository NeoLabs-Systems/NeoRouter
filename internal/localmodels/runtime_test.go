package localmodels

// The behavioural oracle: the memory sampler, the resumable downloader and the
// runner's restart policy, ported from test/unit/local_models.test.js.

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestMemoryAveragesWhatWasMeasured(t *testing.T) {
	sampler := NewSampler()
	sampler.SetForTests(&SamplerFigures{Others: int64ptr(6000), Models: map[string]int64{"qwen3.5-4b": 3300}})
	if got := sampler.OthersMB(); got == nil || *got != 6000 {
		t.Fatalf("othersMb = %v", got)
	}
	if got := sampler.ModelMB("qwen3.5-4b"); got == nil || *got != 3300 {
		t.Fatalf("modelMb = %v", got)
	}
	if got := sampler.ModelMB("whisper-base"); got != nil {
		t.Fatalf("unmeasured modelMb = %v", got)
	}
	sampler.Forget("qwen3.5-4b")
	if got := sampler.ModelMB("qwen3.5-4b"); got != nil {
		t.Fatalf("forgotten modelMb = %v", got)
	}
	sampler.SetForTests(nil)
}

func TestMemoryMeasuresThisMachine(t *testing.T) {
	sampler := NewSampler()
	sampler.SetForTests(nil)
	if err := sampler.Refresh(context.Background()); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	others := sampler.OthersMB()
	if others == nil {
		t.Fatalf("no sample taken")
	}
	machine, err := NewDetector(t.TempDir()).Detect(context.Background(), true)
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	if *others <= 0 || *others >= machine.Memory.TotalMB {
		t.Fatalf("othersMb = %d MB of %d", *others, machine.Memory.TotalMB)
	}
}

func TestDownloadsResumeWhereTheyStoppedAndAreCheckedAgainstTheirHash(t *testing.T) {
	body := make([]byte, 300_000)
	if _, err := rand.Read(body); err != nil {
		t.Fatalf("random body: %v", err)
	}
	sum := sha256.Sum256(body)
	digest := hex.EncodeToString(sum[:])

	var mu sync.Mutex
	ranges := []int64{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		offset := int64(0)
		if header := r.Header.Get("Range"); header != "" {
			value := strings.TrimSuffix(strings.TrimPrefix(header, "bytes="), "-")
			if parsed, err := parseOptionalInt(value); err == nil {
				offset = parsed
			}
		}
		mu.Lock()
		ranges = append(ranges, offset)
		mu.Unlock()
		if offset > 0 {
			w.Header().Set("Content-Length", itoa(int64(len(body))-offset))
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write(body[offset:])
			return
		}
		w.Header().Set("Content-Length", itoa(int64(len(body))))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
	defer server.Close()

	dir := t.TempDir()
	dest := filepath.Join(dir, "model.bin")
	if err := os.WriteFile(dest+".part", body[:100_000], 0o600); err != nil {
		t.Fatalf("seed part file: %v", err)
	}
	size := int64(len(body))
	file := File{URL: server.URL + "/model.bin", Size: &size, SHA256: digest}
	seen := int64(0)
	err := Download(context.Background(), server.Client(), file, dest, DownloadOptions{
		OnBytes: func(delta int64) { seen += delta },
	})
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	mu.Lock()
	got := append([]int64{}, ranges...)
	mu.Unlock()
	if len(got) != 1 || got[0] != 100_000 {
		t.Fatalf("ranges = %v, want [100000]", got)
	}
	if seen != size {
		t.Fatalf("progress = %d, want %d", seen, size)
	}
	stored, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read downloaded file: %v", err)
	}
	if string(stored) != string(body) {
		t.Fatalf("downloaded file differs")
	}
	if _, err := os.Stat(dest + ".part"); !os.IsNotExist(err) {
		t.Fatalf("part file left behind")
	}
	// A complete file is kept as it is.
	if err := Download(context.Background(), server.Client(), file, dest, DownloadOptions{}); err != nil {
		t.Fatalf("second download: %v", err)
	}
	mu.Lock()
	count := len(ranges)
	mu.Unlock()
	if count != 1 {
		t.Fatalf("second download asked the server again: %d requests", count)
	}
}

// parseOptionalInt and itoa keep the test's helpers local.
func parseOptionalInt(value string) (int64, error) {
	var out int64
	for _, r := range value {
		if r < '0' || r > '9' {
			return 0, os.ErrInvalid
		}
		out = out*10 + int64(r-'0')
	}
	return out, nil
}

func itoa(value int64) string {
	if value == 0 {
		return "0"
	}
	digits := []byte{}
	for value > 0 {
		digits = append([]byte{byte('0' + value%10)}, digits...)
		value /= 10
	}
	return string(digits)
}

// settle waits long enough for the runner's goroutines to act.
func settle() { time.Sleep(120 * time.Millisecond) }

// waitForState waits for the runner to reach a state; the runner's exit handling
// runs in its own goroutine, where the JS emit was synchronous.
func waitForState(t *testing.T, runner *Runner, state string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if runner.State() == state {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("the runner never became %s (it is %s)", state, runner.State())
}

func runnerMachine() Machine { return testMachine() }

func newTestRunner(t *testing.T, engine Engine, backoff []int) (*Runner, *atomic.Bool) {
	t.Helper()
	gaveUp := &atomic.Bool{}
	runner := NewRunner("fake", func(ctx context.Context) (RunnerContext, error) {
		entry, _ := CatalogEntry("qwen3.5-0.8b")
		return RunnerContext{Entry: entry, Machine: runnerMachine()}, nil
	}, RunnerOptions{
		Engine:    func(string) Engine { return engine },
		BackoffMs: backoff,
		OnGaveUp:  func(*Runner) { gaveUp.Store(true) },
	})
	return runner, gaveUp
}

func TestACrashedModelIsStartedAgainAndRepeatedCrashesGiveUp(t *testing.T) {
	engine := &fakeEngine{}
	// A short backoff, as in the JS test: long enough that the restarting state
	// is observable, short enough that the test stays quick.
	runner, gaveUp := newTestRunner(t, engine, []int{50, 50, 50})
	runner.Start()
	settle()
	if state := runner.State(); state != StateRunning {
		t.Fatalf("state = %q, want running", state)
	}

	engine.last().Crash()
	waitForState(t, runner, StateRestarting)
	if problem := runner.Problem(); problem == nil || problem.Code != "crashed" {
		t.Fatalf("problem = %+v", problem)
	}
	waitForState(t, runner, StateRunning)
	if engine.count() != 2 {
		t.Fatalf("started %d models, want 2", engine.count())
	}
	for index := 0; index < 3; index++ {
		engine.last().Crash()
		settle()
		settle()
	}
	if state := runner.State(); state != StateFailed {
		t.Fatalf("state = %q, want failed", state)
	}
	if !gaveUp.Load() {
		t.Fatalf("the runner never gave up")
	}
	runner.Stop()
	if state := runner.State(); state != StateStopped {
		t.Fatalf("state after stop = %q", state)
	}
}

func TestStoppingAModelEndsItsProcessAndDoesNotRestartIt(t *testing.T) {
	engine := &fakeEngine{}
	runner, _ := newTestRunner(t, engine, []int{5})
	runner.Start()
	settle()
	runner.Stop()
	settle()
	if state := runner.State(); state != StateStopped {
		t.Fatalf("state = %q", state)
	}
	if engine.count() != 1 {
		t.Fatalf("started %d models, want 1", engine.count())
	}
	select {
	case <-engine.started[0].Done():
	default:
		t.Fatalf("the model process was not ended")
	}
}

func TestRefusedStartIsAFailedModel(t *testing.T) {
	engine := &fakeEngine{}
	runner, _ := newTestRunner(t, engine, []int{5})
	runner.Refuse(&Problem{Code: "not_enough_memory", Message: "Not enough free memory. Stop another model first."})
	if state := runner.State(); state != StateFailed {
		t.Fatalf("state = %q, want failed", state)
	}
	if engine.count() != 0 {
		t.Fatalf("a refused model started anyway")
	}
}

func TestLongSpeechIsSplitIntoPartsAtSentenceEnds(t *testing.T) {
	text := strings.Repeat("This is a sentence. ", 40) + strings.Repeat("word ", 120)
	parts := SpeechParts(text, 300)
	if len(parts) <= 3 {
		t.Fatalf("parts = %d, want more than 3", len(parts))
	}
	for _, part := range parts {
		if len([]rune(part)) > 300 {
			t.Fatalf("part of %d characters: %q", len([]rune(part)), part)
		}
	}
	joined := strings.Join(parts, " ")
	collapse := func(value string) string { return strings.Join(strings.Fields(value), " ") }
	if collapse(joined) != collapse(text) {
		t.Fatalf("parts do not reassemble the text:\n%q\n%q", collapse(joined), collapse(text))
	}
}
