package media

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/NeoLabs-Systems/neorouter/internal/catalog"
	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
	"github.com/NeoLabs-Systems/neorouter/internal/providers"
)

// videoEntry is the catalog entry a fake video provider serves.
func (e *testEnv) videoEntry(accountID string) catalog.Entry {
	return catalog.Entry{Ref: e.videoRef, ID: "mock/sora-ok", ModelID: "sora-ok", CanonicalID: "sora", AccountID: accountID}
}

// videoAdapter is a provider that accepts a video job and then reports progress.
func (e *testEnv) videoAdapter(accountID string, statuses ...providers.VideoStatus) *fakeAdapter {
	adapter := newFakeAdapter("sora", providers.CapCreateVideo)
	adapter.contentFn = func(string) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"video/mp4"}, "Content-Length": []string{"7"}},
			Body:       io.NopCloser(strings.NewReader("mp4data")),
		}, nil
	}
	adapter.videoFn = func(request providers.VideoRequest) (*providers.VideoJob, error) {
		return &providers.VideoJob{UpstreamID: "upstream-1", Status: "queued"}, nil
	}
	index := 0
	adapter.statusFn = func(string) (*providers.VideoStatus, error) {
		if index >= len(statuses) {
			return &providers.VideoStatus{Status: "completed", Progress: floatPtr(100)}, nil
		}
		status := statuses[index]
		index++
		return &status, nil
	}
	return e.useAdapter(accountID, adapter)
}

func TestVideoJobsRoundTrip(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	adapter := env.videoAdapter("account-1")
	job, err := env.svc.createVideoJob(ctx, env.key, "request-1", env.videoEntry("account-1"),
		&providers.VideoJob{UpstreamID: "upstream-1", Status: "queued"}, videoRequest{
			Model: "mock/sora-ok", Prompt: stringPtr("waves"), Seconds: floatPtr(4), Size: stringPtr("1280x720"),
		})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if !strings.HasPrefix(job.ID, "video_") || len(job.ID) != len("video_")+24 {
		t.Fatalf("id = %q, want video_ and 24 hex characters", job.ID)
	}
	found, err := env.svc.findVideoJob(ctx, "user-1", job.ID)
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	object := found.object()
	if object.Object != "video" || object.Status != "queued" || object.Progress != 0 {
		t.Fatalf("object = %+v", object)
	}
	if object.Seconds == nil || *object.Seconds != "4" || object.Size == nil || *object.Size != "1280x720" {
		t.Fatalf("object = %+v", object)
	}
	if object.Prompt == nil || *object.Prompt != "waves" {
		t.Fatalf("prompt = %+v", object.Prompt)
	}
	if object.Model == nil || *object.Model != "mock/sora-ok" {
		t.Fatalf("model = %+v", object.Model)
	}
	if object.CreatedAt != env.clock.Now().Unix() {
		t.Fatalf("created_at = %d, want seconds", object.CreatedAt)
	}
	if object.Error != nil {
		t.Fatalf("a queued job has no error: %+v", object.Error)
	}
	if _, err := env.svc.findVideoJob(ctx, "user-2", job.ID); err == nil {
		t.Fatal("another person's job is not found")
	} else if !strings.Contains(err.Error(), "No video job "+job.ID+".") {
		t.Fatalf("error = %v", err)
	}
	if len(adapter.recorded()) != 0 {
		t.Fatalf("creating a job does not poll: %v", adapter.recorded())
	}
}

func TestVideoJobsRefreshRespectsThePollInterval(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	adapter := env.videoAdapter("account-1", providers.VideoStatus{Status: "in_progress", Progress: floatPtr(40)})
	job, err := env.svc.createVideoJob(ctx, env.key, "", env.videoEntry("account-1"),
		&providers.VideoJob{UpstreamID: "upstream-1", Status: "queued"}, videoRequest{Prompt: stringPtr("waves")})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// Polling sooner than the interval only spends the provider's rate limit.
	polled, err := env.svc.refreshVideoJob(ctx, job)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if polled.Status != "queued" || len(adapter.recorded()) != 0 {
		t.Fatalf("status = %q, calls = %v", polled.Status, adapter.recorded())
	}
	env.clock.Advance(3 * time.Second)
	polled, err = env.svc.refreshVideoJob(ctx, job)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if polled.Status != "in_progress" || polled.Progress == nil || *polled.Progress != 40 {
		t.Fatalf("polled = %+v", polled)
	}
	// The new state is what a later read reports.
	stored, err := env.svc.findVideoJob(ctx, "user-1", job.ID)
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if stored.Status != "in_progress" || stored.UpdatedAt != env.clock.Now().UnixMilli() {
		t.Fatalf("stored = %+v", stored)
	}
	// A terminal job is never polled again.
	env.clock.Advance(time.Minute)
	completed, err := env.svc.refreshVideoJob(ctx, stored)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if completed.Status != "completed" {
		t.Fatalf("status = %q", completed.Status)
	}
	env.clock.Advance(time.Minute)
	again, err := env.svc.refreshVideoJob(ctx, completed)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if again.Status != "completed" || len(adapter.recorded()) != 2 {
		t.Fatalf("status = %q, calls = %v", again.Status, adapter.recorded())
	}
	if object := again.object(); object.Progress != 100 {
		t.Fatalf("a completed job is 100%%: %+v", object)
	}
}

func TestVideoJobsRefreshKeepsTheLastStateWhenThePollFails(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	adapter := env.videoAdapter("account-1")
	adapter.statusFn = func(string) (*providers.VideoStatus, error) {
		return nil, io.ErrUnexpectedEOF
	}
	job, err := env.svc.createVideoJob(ctx, env.key, "", env.videoEntry("account-1"),
		&providers.VideoJob{UpstreamID: "upstream-1", Status: "queued"}, videoRequest{Prompt: stringPtr("waves")})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	env.clock.Advance(4 * time.Second)
	polled, err := env.svc.refreshVideoJob(ctx, job)
	if err != nil {
		t.Fatalf("a failed poll is not a failed job: %v", err)
	}
	if polled.Status != "queued" {
		t.Fatalf("status = %q", polled.Status)
	}
}

func TestVideoJobsReportAProviderThatIsGone(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	job, err := env.svc.createVideoJob(ctx, env.key, "", env.videoEntry("account-gone"),
		&providers.VideoJob{UpstreamID: "upstream-1", Status: "queued"}, videoRequest{Prompt: stringPtr("waves")})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	env.clock.Advance(4 * time.Second)
	if _, err := env.svc.refreshVideoJob(ctx, job); err == nil {
		t.Fatal("a removed account is reported")
	} else if !strings.Contains(err.Error(), "The provider account that made this video was removed.") {
		t.Fatalf("error = %v", err)
	}
	contentErr := videoContentError(env, ctx, job)
	if contentErr == nil {
		t.Fatal("content of an unfinished job is refused")
	}
	if !strings.Contains(contentErr.Error(), "The video is queued.") {
		t.Fatalf("error = %v", contentErr)
	}
	var httpError *httpserver.HttpError
	if !errors.As(contentErr, &httpError) || httpError.Status != http.StatusConflict {
		t.Fatalf("error = %v, want 409 VIDEO_NOT_READY", contentErr)
	}
}

func TestVideoJobsContent(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	env.videoAdapter("account-1")
	job, err := env.svc.createVideoJob(ctx, env.key, "", env.videoEntry("account-1"),
		&providers.VideoJob{UpstreamID: "upstream-1", Status: "completed"}, videoRequest{Prompt: stringPtr("waves")})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	response, err := env.svc.videoContent(ctx, job)
	if err != nil {
		t.Fatalf("content: %v", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil || !bytes.Equal(body, []byte("mp4data")) {
		t.Fatalf("body = %q (%v)", body, err)
	}
}

func TestVideoJobsList(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	env.videoAdapter("account-1")
	first, err := env.svc.createVideoJob(ctx, env.key, "", env.videoEntry("account-1"),
		&providers.VideoJob{UpstreamID: "a", Status: "queued"}, videoRequest{Prompt: stringPtr("one")})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	env.clock.Advance(time.Second)
	second, err := env.svc.createVideoJob(ctx, env.key, "", env.videoEntry("account-1"),
		&providers.VideoJob{UpstreamID: "b", Status: "queued"}, videoRequest{Prompt: stringPtr("two")})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	jobs, err := env.svc.listVideoJobs(ctx, "user-1", 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(jobs) != 2 || jobs[0].ID != second.ID || jobs[1].ID != first.ID {
		t.Fatalf("jobs = %+v, want newest first", jobs)
	}
	if limited, _ := env.svc.listVideoJobs(ctx, "user-1", 1); len(limited) != 1 {
		t.Fatalf("limit = %d", len(limited))
	}
	if other, _ := env.svc.listVideoJobs(ctx, "user-2", 20); len(other) != 0 {
		t.Fatalf("another person's list has %d jobs", len(other))
	}
}

// videoContentError is the error reading one job's file returned.
func videoContentError(env *testEnv, ctx context.Context, job *videoJob) error {
	_, err := env.svc.videoContent(ctx, job)
	return err
}
