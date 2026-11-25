package media

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/NeoLabs-Systems/neorouter/internal/catalog"
	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
	"github.com/NeoLabs-Systems/neorouter/internal/providers"
	"github.com/NeoLabs-Systems/neorouter/internal/routerkeys"
)

// Video generation is asynchronous: the provider that accepted a job is the only
// one that knows it, so a job is bound to that account for its whole life.
// NeoRouter hands out its own ids (video_…) and polls the provider when the
// client asks for the status, OpenAI Videos API style
// (server/capabilities/media/video_jobs.js).

// videoMinPollMS is how often a job may be polled upstream: asking more often
// only spends the provider's rate limit.
const videoMinPollMS = 3000

// videoPollTimeout bounds one upstream status poll.
const videoPollTimeout = 30 * time.Second

// videoTerminal reports whether a status is final.
func videoTerminal(status string) bool {
	return status == "completed" || status == "failed"
}

// videoJob is one media_jobs row.
type videoJob struct {
	ID           string
	UserID       string
	RouterKeyID  *string
	RequestID    *string
	Capability   string
	ModelRef     *int64
	ModelLabel   *string
	AccountID    *string
	UpstreamID   string
	Status       string
	Progress     *float64
	RequestJSON  string
	ResultJSON   *string
	ErrorMessage *string
	CreatedAt    int64
	UpdatedAt    int64
}

// videoRequest is the request a job was created from, kept for its listing.
type videoRequest struct {
	Model   string   `json:"model,omitempty"`
	Prompt  *string  `json:"prompt,omitempty"`
	Seconds *float64 `json:"seconds,omitempty"`
	Size    *string  `json:"size,omitempty"`
}

// videoObject is the OpenAI Videos response body.
type videoObject struct {
	ID        string       `json:"id"`
	Object    string       `json:"object"`
	Model     *string      `json:"model"`
	Status    string       `json:"status"`
	Progress  float64      `json:"progress"`
	CreatedAt int64        `json:"created_at"`
	Seconds   *string      `json:"seconds"`
	Size      *string      `json:"size"`
	Prompt    *string      `json:"prompt"`
	Error     *videoFailed `json:"error"`
}

// videoFailed is a job's failure, in the API's shape.
type videoFailed struct {
	Message string `json:"message"`
}

// newVideoJobID is video_<24 hex characters>, as crypto.randomBytes(12) renders.
func newVideoJobID() string {
	buffer := make([]byte, 12)
	if _, err := rand.Read(buffer); err != nil {
		return ""
	}
	return "video_" + hex.EncodeToString(buffer)
}

// createVideoJob records a job the provider just accepted.
func (s *Service) createVideoJob(ctx context.Context, key *routerkeys.Row, requestID string, entry catalog.Entry, job *providers.VideoJob, request videoRequest) (*videoJob, error) {
	now := s.now().UnixMilli()
	encoded, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("media: encode video request: %w", err)
	}
	created := &videoJob{
		ID:          newVideoJobID(),
		UserID:      key.UserID,
		RouterKeyID: &key.ID,
		Capability:  "video",
		ModelRef:    &entry.Ref,
		ModelLabel:  &entry.ID,
		AccountID:   &entry.AccountID,
		UpstreamID:  job.UpstreamID,
		Status:      job.Status,
		Progress:    job.Progress,
		RequestJSON: string(encoded),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if created.ID == "" {
		return nil, errors.New("media: generate video job id")
	}
	if requestID != "" {
		created.RequestID = &requestID
	}
	if _, err := s.db.W.ExecContext(ctx,
		`INSERT INTO media_jobs (id, user_id, router_key_id, request_id, capability, model_ref, model_label, account_id, upstream_id,
			status, progress, request_json, result_json, error_message, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, NULL, ?, ?)`,
		created.ID, created.UserID, created.RouterKeyID, created.RequestID, created.Capability, created.ModelRef,
		created.ModelLabel, created.AccountID, created.UpstreamID, created.Status, created.Progress,
		created.RequestJSON, created.CreatedAt, created.UpdatedAt); err != nil {
		return nil, fmt.Errorf("media: create video job: %w", err)
	}
	return created, nil
}

// videoJobRow reads one media_jobs row.
const videoJobColumns = `id, user_id, router_key_id, request_id, capability, model_ref, model_label, account_id,
	upstream_id, status, progress, request_json, result_json, error_message, created_at, updated_at`

func scanVideoJob(scan func(...any) error) (*videoJob, error) {
	var job videoJob
	var routerKeyID, requestID, modelLabel, accountID, resultJSON, errorMessage sql.NullString
	var modelRef sql.NullInt64
	var progress sql.NullFloat64
	if err := scan(&job.ID, &job.UserID, &routerKeyID, &requestID, &job.Capability, &modelRef, &modelLabel,
		&accountID, &job.UpstreamID, &job.Status, &progress, &job.RequestJSON, &resultJSON, &errorMessage,
		&job.CreatedAt, &job.UpdatedAt); err != nil {
		return nil, err
	}
	job.RouterKeyID = nullStringPtr(routerKeyID)
	job.RequestID = nullStringPtr(requestID)
	job.ModelLabel = nullStringPtr(modelLabel)
	job.AccountID = nullStringPtr(accountID)
	job.ResultJSON = nullStringPtr(resultJSON)
	job.ErrorMessage = nullStringPtr(errorMessage)
	if modelRef.Valid {
		value := modelRef.Int64
		job.ModelRef = &value
	}
	if progress.Valid {
		value := progress.Float64
		job.Progress = &value
	}
	return &job, nil
}

func nullStringPtr(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	text := value.String
	return &text
}

// findVideoJob reads one of a person's jobs, 404 when it is not there.
func (s *Service) findVideoJob(ctx context.Context, userID, id string) (*videoJob, error) {
	job, err := scanVideoJob(s.db.R.QueryRowContext(ctx,
		`SELECT `+videoJobColumns+` FROM media_jobs WHERE id = ? AND user_id = ?`, id, userID).Scan)
	if err == sql.ErrNoRows {
		return nil, httpserver.New(http.StatusNotFound, "VIDEO_NOT_FOUND", fmt.Sprintf("No video job %s.", id))
	}
	if err != nil {
		return nil, fmt.Errorf("media: read video job: %w", err)
	}
	return job, nil
}

// listVideoJobs is one person's jobs, newest first.
func (s *Service) listVideoJobs(ctx context.Context, userID string, limit int) ([]videoJob, error) {
	if limit > 100 {
		limit = 100
	}
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.R.QueryContext(ctx,
		`SELECT `+videoJobColumns+` FROM media_jobs WHERE user_id = ? ORDER BY created_at DESC LIMIT ?`, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("media: list video jobs: %w", err)
	}
	defer rows.Close()
	jobs := []videoJob{}
	for rows.Next() {
		job, err := scanVideoJob(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("media: scan video job: %w", err)
		}
		jobs = append(jobs, *job)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("media: list video jobs: %w", err)
	}
	return jobs, nil
}

// videoAdapter is the adapter of the account that made a job, 410 when that
// account is gone.
func (s *Service) videoAdapter(job *videoJob) (providers.VideoCreator, error) {
	accountID := ""
	if job.AccountID != nil {
		accountID = *job.AccountID
	}
	if adapter := s.adapterFor(accountID); adapter != nil {
		if creator, ok := adapter.(providers.VideoCreator); ok {
			return creator, nil
		}
	}
	return nil, httpserver.New(http.StatusGone, "VIDEO_PROVIDER_GONE", "The provider account that made this video was removed.")
}

// refreshVideoJob asks the provider for a job's progress, at most once every few
// seconds. A failed poll is not a failed job: the last known state is reported.
func (s *Service) refreshVideoJob(ctx context.Context, job *videoJob) (*videoJob, error) {
	if videoTerminal(job.Status) || s.now().UnixMilli()-job.UpdatedAt < videoMinPollMS {
		return job, nil
	}
	creator, err := s.videoAdapter(job)
	if err != nil {
		return nil, err
	}
	pollCtx, cancel := context.WithTimeout(ctx, videoPollTimeout)
	defer cancel()
	state, err := creator.GetVideo(pollCtx, job.UpstreamID)
	if err != nil {
		var httpError *httpserver.HttpError
		if errors.As(err, &httpError) {
			return nil, err
		}
		return job, nil
	}
	next := *job
	next.Status = state.Status
	if state.Progress != nil {
		next.Progress = state.Progress
	}
	next.ErrorMessage = state.Err
	next.UpdatedAt = s.now().UnixMilli()
	if _, err := s.db.W.ExecContext(ctx,
		`UPDATE media_jobs SET status = ?, progress = ?, error_message = ?, updated_at = ? WHERE id = ?`,
		next.Status, next.Progress, next.ErrorMessage, next.UpdatedAt, next.ID); err != nil {
		return nil, fmt.Errorf("media: update video job: %w", err)
	}
	return &next, nil
}

// videoContent is the finished video's bytes, 409 while it is not ready.
func (s *Service) videoContent(ctx context.Context, job *videoJob) (*http.Response, error) {
	if job.Status != "completed" {
		return nil, httpserver.New(http.StatusConflict, "VIDEO_NOT_READY", fmt.Sprintf("The video is %s.", job.Status))
	}
	creator, err := s.videoAdapter(job)
	if err != nil {
		return nil, err
	}
	response, err := creator.VideoContent(ctx, job.UpstreamID)
	if err != nil {
		return nil, err
	}
	return response, nil
}

// object is the job as the API lists it.
func (job *videoJob) object() videoObject {
	var request videoRequest
	if err := json.Unmarshal([]byte(job.RequestJSON), &request); err != nil {
		request = videoRequest{}
	}
	progress := 0.0
	switch {
	case job.Progress != nil:
		progress = *job.Progress
	case job.Status == "completed":
		progress = 100
	}
	out := videoObject{
		ID:        job.ID,
		Object:    "video",
		Model:     job.ModelLabel,
		Status:    job.Status,
		Progress:  progress,
		CreatedAt: job.CreatedAt / 1000,
		Size:      request.Size,
		Prompt:    request.Prompt,
	}
	if request.Seconds != nil && *request.Seconds != 0 {
		seconds := strconv.FormatFloat(*request.Seconds, 'f', -1, 64)
		out.Seconds = &seconds
	}
	if job.ErrorMessage != nil {
		out.Error = &videoFailed{Message: *job.ErrorMessage}
	}
	return out
}
