package apps

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// maxFailureKeys is how many keys keep their last failure in memory.
const maxFailureKeys = 2000

// Activity is what last happened with a key, for the "waiting for your app"
// check. Times are unix milliseconds, like Date.now().
type Activity struct {
	LastUsedAt *int64       `json:"lastUsedAt"`
	Request    *LastRequest `json:"request"`
}

// LastRequest is the newest request with the key, whether it succeeded, and
// when it failed before reaching a model, why.
type LastRequest struct {
	At           int64   `json:"at"`
	OK           bool    `json:"ok"`
	Model        *string `json:"model"`
	Requested    *string `json:"requested"`
	ErrorCode    *string `json:"errorCode"`
	ErrorMessage *string `json:"errorMessage"`
	LatencyMs    *int64  `json:"latencyMs"`
}

// Failure is a request with a valid key that failed before it reached a model
// (an unknown model name, a bad request). Called by the gateway's error handler.
type Failure struct {
	Status  int
	Code    string
	Message string
	Model   string
}

// failure is the last failure of one key. status is kept, though the approval
// screen does not show it.
type failure struct {
	at      int64
	status  int
	code    *string
	message string
	model   *string
}

// RecordFailure remembers why the newest request with keyID failed. Only the
// newest failure per key is kept, and never the prompt.
func (s *Service) RecordFailure(keyID string, f Failure) {
	if keyID == "" {
		return
	}
	code := f.Code
	message := f.Message
	if len(message) > 300 {
		message = message[:300]
	}
	model := f.Model
	if len(model) > 200 {
		model = model[:200]
	}
	record := failure{at: nowMillis(s.now), status: f.Status, message: message}
	if code != "" {
		record.code = &code
	}
	if model != "" {
		record.model = &model
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, seen := s.failures[keyID]; seen {
		for i, id := range s.order {
			if id == keyID {
				s.order = append(s.order[:i], s.order[i+1:]...)
				break
			}
		}
	}
	s.failures[keyID] = record
	s.order = append(s.order, keyID)
	if len(s.order) > maxFailureKeys {
		oldest := s.order[0]
		s.order = s.order[1:]
		delete(s.failures, oldest)
	}
}

// ResetForTests forgets every recorded failure.
func (s *Service) ResetForTests() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failures = make(map[string]failure)
	s.order = nil
}

// Activity reports what last happened with a key of this owner. It returns
// (nil, nil) when the owner has no such key.
func (s *Service) Activity(ctx context.Context, ownerID, keyID string) (*Activity, error) {
	var (
		keyIDColumn string
		lastUsedAt  sql.NullString
	)
	err := s.db.R.QueryRowContext(ctx,
		`SELECT id, last_used_at FROM router_keys WHERE id = ? AND user_id = ?`, keyID, ownerID).
		Scan(&keyIDColumn, &lastUsedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("apps: read router key: %w", err)
	}

	var (
		createdAt     int64
		status        string
		modelLabel    sql.NullString
		requested     string
		errorCode     sql.NullString
		errorMessage  sql.NullString
		latencyMillis sql.NullInt64
	)
	err = s.db.R.QueryRowContext(ctx,
		`SELECT created_at, status, model_label, requested_model, error_code, error_message, latency_ms
		   FROM requests WHERE router_key_id = ? ORDER BY created_at DESC LIMIT 1`, keyID).
		Scan(&createdAt, &status, &modelLabel, &requested, &errorCode, &errorMessage, &latencyMillis)
	hasRow := err == nil
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("apps: read last request: %w", err)
	}

	activity := &Activity{}
	if lastUsedAt.Valid {
		if at, ok := parseMillis(lastUsedAt.String); ok {
			activity.LastUsedAt = &at
		}
	}
	if hasRow {
		last := &LastRequest{At: createdAt, OK: status == "success", Requested: &requested}
		model := modelLabel.String
		if model == "" {
			model = requested
		}
		last.Model = &model
		if errorCode.Valid {
			last.ErrorCode = &errorCode.String
		}
		if errorMessage.Valid {
			last.ErrorMessage = &errorMessage.String
		}
		if latencyMillis.Valid {
			last.LatencyMs = &latencyMillis.Int64
		}
		activity.Request = last
	}

	s.mu.Lock()
	recorded, seen := s.failures[keyID]
	s.mu.Unlock()
	if seen && (activity.Request == nil || recorded.at > activity.Request.At) {
		activity.Request = &LastRequest{
			At:           recorded.at,
			OK:           false,
			Requested:    recorded.model,
			ErrorCode:    recorded.code,
			ErrorMessage: &recorded.message,
		}
	}
	return activity, nil
}

// parseMillis is Date.parse for the ISO millisecond timestamps the database
// stores; an unreadable value is null, as NaN serialises to null in JSON.
func parseMillis(value string) (int64, bool) {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05"} {
		if at, err := time.Parse(layout, value); err == nil {
			return at.UnixMilli(), true
		}
	}
	return 0, false
}

// nowMillis is Date.now() for the injected clock.
func nowMillis(now func() time.Time) int64 { return now().UnixMilli() }
