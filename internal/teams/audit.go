package teams

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
)

// Audit event details, written in the key order the JS services built them in
// so a stored detail_json keeps its shape.
type (
	memberJoinDetail struct {
		InviteID string `json:"inviteId"`
	}
	memberPermissionsDetail struct {
		Permissions []string `json:"permissions"`
	}
	inviteCreateDetail struct {
		InviteID    string   `json:"inviteId"`
		Permissions []string `json:"permissions"`
		SingleUse   bool     `json:"singleUse"`
		ExpiresAt   *string  `json:"expiresAt"`
	}
	inviteRevokeDetail struct {
		InviteID string `json:"inviteId"`
	}
)

// emptyDetail is the `{}` the JS services pass when an event carries nothing.
var emptyDetail = json.RawMessage(`{}`)

// mustJSON renders a detail the audit trail stores; the shapes above cannot
// fail to encode.
func mustJSON(detail any) json.RawMessage {
	raw, err := json.Marshal(detail)
	if err != nil {
		return emptyDetail
	}
	return raw
}

// recordAudit appends one team event.
func (s *Service) recordAudit(ctx context.Context, q dbtx, action, actorUserID string, subjectUserID *string, detail json.RawMessage) error {
	var subject any
	if subjectUserID != nil {
		subject = *subjectUserID
	}
	if len(detail) == 0 {
		detail = emptyDetail
	}
	if _, err := q.ExecContext(ctx,
		`INSERT INTO team_audit (action, actor_user_id, subject_user_id, detail_json) VALUES (?, ?, ?, ?)`,
		action, actorUserID, subject, string(detail)); err != nil {
		return fmt.Errorf("teams: record audit: %w", err)
	}
	return nil
}

// AuditEvent is one row of the team audit trail as the admin API returns it.
type AuditEvent struct {
	ID        int64           `json:"id"`
	Action    string          `json:"action"`
	Actor     *string         `json:"actor"`
	Subject   *string         `json:"subject"`
	Detail    json.RawMessage `json:"detail"`
	CreatedAt string          `json:"createdAt"`
}

// AuditList reads the newest team events, 100 by default and at most 500.
func (s *Service) AuditList(ctx context.Context, limit int) ([]AuditEvent, error) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	rows, err := s.DB.R.QueryContext(ctx,
		`SELECT a.id, a.action, actor.username actor_name, subject.username subject_name, a.detail_json, a.created_at
		FROM team_audit a
		LEFT JOIN users actor ON actor.id = a.actor_user_id
		LEFT JOIN users subject ON subject.id = a.subject_user_id
		ORDER BY a.id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("teams: list audit: %w", err)
	}
	defer rows.Close()

	events := make([]AuditEvent, 0)
	for rows.Next() {
		var event AuditEvent
		var actor, subject, detail sql.NullString
		if err := rows.Scan(&event.ID, &event.Action, &actor, &subject, &detail, &event.CreatedAt); err != nil {
			return nil, fmt.Errorf("teams: read audit: %w", err)
		}
		event.Actor = stringPtr(actor)
		event.Subject = stringPtr(subject)
		event.Detail = detailJSON(detail)
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("teams: list audit: %w", err)
	}
	return events, nil
}

// detailJSON passes a stored detail through unchanged so its key order survives,
// the way JSON.parse + res.json round-trips it in the Node server.
func detailJSON(value sql.NullString) json.RawMessage {
	if !value.Valid || value.String == "" || !json.Valid([]byte(value.String)) {
		return emptyDetail
	}
	return json.RawMessage(value.String)
}

// ParseLimit reads the ?limit= query value of the audit endpoint the way the JS
// `Math.min(Math.max(Number(limit)||100,1),500)` did.
func ParseLimit(raw string) int {
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsNaN(value) || value == 0 {
		return 100
	}
	limit := int(value)
	if limit < 1 {
		return 1
	}
	if limit > 500 {
		return 500
	}
	return limit
}
