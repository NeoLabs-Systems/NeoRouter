package usage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

// LogQuery filters the request log.
type LogQuery struct {
	Limit  int    // at most 500
	Before *int64 // only requests older than this instant
	KeyID  string
	KeyIDs []string
	Status string
}

// LogRow is one request of the log, metadata only.
type LogRow struct {
	ID                string          `json:"id"`
	CreatedAt         int64           `json:"createdAt"`
	KeyID             *string         `json:"keyId"`
	KeyName           *string         `json:"keyName"`
	APIFormat         string          `json:"apiFormat"`
	RequestedModel    string          `json:"requestedModel"`
	Model             *string         `json:"model"`
	Status            string          `json:"status"`
	HTTPStatus        *int64          `json:"httpStatus"`
	ErrorCode         *string         `json:"errorCode"`
	ErrorMessage      *string         `json:"errorMessage"`
	RoutingGroup      *string         `json:"routingGroup"`
	CachedInputTokens int64           `json:"cachedInputTokens"`
	InputTokens       int64           `json:"inputTokens"`
	OutputTokens      int64           `json:"outputTokens"`
	CostUSD           float64         `json:"costUsd"`
	IsFree            bool            `json:"isFree"`
	LatencyMS         *int64          `json:"latencyMs"`
	TTFTMS            *int64          `json:"ttftMs"`
	Attempts          int64           `json:"attempts"`
	Stream            bool            `json:"stream"`
	Trail             json.RawMessage `json:"trail"`
	QueuedMS          int64           `json:"queuedMs"`
	Capability        string          `json:"capability"`
	Task              *string         `json:"task"`
	Units             float64         `json:"units"`
	KeptPrivate       *string         `json:"keptPrivate"`
}

// Logs is a person's request log, newest first.
func (s *Service) Logs(userID string, query LogQuery) ([]LogRow, error) {
	keyIDs := query.KeyIDs
	if query.KeyID != "" {
		keyIDs = append([]string{query.KeyID}, keyIDs...)
	}
	filter, ids := keyFilter(keyIDs)
	limit := query.Limit
	if limit == 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	args := []any{userID, nil, nil}
	if query.Before != nil {
		args[1], args[2] = *query.Before, *query.Before
	}
	args = append(args, ids...)
	args = append(args, nil, nil)
	if query.Status != "" {
		args[len(args)-2], args[len(args)-1] = query.Status, query.Status
	}
	args = append(args, limit)
	rows, err := s.DB.R.QueryContext(context.Background(), `SELECT r.id, r.created_at, r.router_key_id, k.name key_name, r.api_format,
		r.requested_model, r.model_label, r.status, r.http_status, r.error_code, r.error_message, r.routing_group,
		r.cached_input_tokens, r.input_tokens, r.output_tokens, r.cost_usd, r.is_free, r.latency_ms, r.ttft_ms, r.attempts,
		r.stream, r.trail_json, r.queued_ms, r.capability, r.task, r.units, r.kept_private
		FROM requests r LEFT JOIN router_keys k ON k.id = r.router_key_id
		WHERE r.user_id = ? AND k.peer_link_id IS NULL AND (? IS NULL OR r.created_at < ?)`+filter+` AND (? IS NULL OR r.status = ?)
		ORDER BY r.created_at DESC LIMIT ?`, args...)
	if err != nil {
		return nil, fmt.Errorf("usage: read log: %w", err)
	}
	defer rows.Close()
	out := []LogRow{}
	for rows.Next() {
		var row LogRow
		var keyID, keyName, model, errorCode, errorMessage, routingGroup, task, keptPrivate sql.NullString
		var httpStatus, latency, ttft sql.NullInt64
		var isFree, stream int64
		var trail string
		if err := rows.Scan(&row.ID, &row.CreatedAt, &keyID, &keyName, &row.APIFormat, &row.RequestedModel, &model,
			&row.Status, &httpStatus, &errorCode, &errorMessage, &routingGroup, &row.CachedInputTokens, &row.InputTokens,
			&row.OutputTokens, &row.CostUSD, &isFree, &latency, &ttft, &row.Attempts, &stream, &trail, &row.QueuedMS,
			&row.Capability, &task, &row.Units, &keptPrivate); err != nil {
			return nil, fmt.Errorf("usage: read log: %w", err)
		}
		row.KeyID, row.KeyName, row.Model = nullString(keyID), nullString(keyName), nullString(model)
		row.ErrorCode, row.ErrorMessage = nullString(errorCode), nullString(errorMessage)
		row.RoutingGroup, row.Task, row.KeptPrivate = nullString(routingGroup), nullString(task), nullString(keptPrivate)
		if httpStatus.Valid {
			row.HTTPStatus = &httpStatus.Int64
		}
		if latency.Valid {
			row.LatencyMS = &latency.Int64
		}
		if ttft.Valid {
			row.TTFTMS = &ttft.Int64
		}
		row.IsFree, row.Stream = isFree != 0, stream != 0
		if row.Capability == "" {
			row.Capability = "chat"
		}
		if trail == "" {
			trail = "[]"
		}
		// The trail is passed through as stored, so its shape never changes.
		row.Trail = json.RawMessage(trail)
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("usage: read log: %w", err)
	}
	return out, nil
}
