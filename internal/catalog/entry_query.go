package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
)

// ListForUser returns one row per model of one capability for a user, ordered
// by provider slug and model id.
func (s *Service) ListForUser(ctx context.Context, userID, capability string, includeUnavailable bool) ([]Entry, error) {
	query := selectEntry + ` WHERE a.user_id = ? AND m.capability = ?`
	if !includeUnavailable {
		query += ` AND m.available = 1`
	}
	query += ` ORDER BY a.slug, m.model_id`
	rows, err := s.DB.R.QueryContext(ctx, query, userID, capability)
	if err != nil {
		return nil, fmt.Errorf("catalog: list models: %w", err)
	}
	defer rows.Close()
	entries := []Entry{}
	for rows.Next() {
		var row entryRow
		if err := row.scan(rows); err != nil {
			return nil, fmt.Errorf("catalog: scan model: %w", err)
		}
		entries = append(entries, s.toEntry(&row))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("catalog: list models: %w", err)
	}
	return entries, nil
}

// GetModel returns one catalog row of a user's space, and a 404
// MODEL_NOT_FOUND error when it is not there.
func (s *Service) GetModel(ctx context.Context, userID string, ref int64) (Entry, error) {
	var row entryRow
	err := row.scan(s.DB.R.QueryRowContext(ctx, selectEntry+` WHERE m.id = ? AND a.user_id = ?`, ref, userID))
	if err == sql.ErrNoRows {
		return Entry{}, modelNotFound()
	}
	if err != nil {
		return Entry{}, fmt.Errorf("catalog: get model: %w", err)
	}
	return s.toEntry(&row), nil
}

func modelNotFound() error {
	return httpserver.New(404, "MODEL_NOT_FOUND", "This model does not exist.")
}

// parseJSONObject decodes a JSON object column, treating an empty or invalid
// value as an empty object.
func parseJSONObject(text string) map[string]any {
	object := map[string]any{}
	if strings.TrimSpace(text) == "" {
		return object
	}
	if err := json.Unmarshal([]byte(text), &object); err != nil || object == nil {
		return map[string]any{}
	}
	return object
}

func parseSkills(text sql.NullString) map[string]float64 {
	if !text.Valid || strings.TrimSpace(text.String) == "" {
		return nil
	}
	skills := map[string]float64{}
	if err := json.Unmarshal([]byte(text.String), &skills); err != nil {
		return nil
	}
	return skills
}

func stringPtrIfValid(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return stringPtrOf(value.String)
}

func stringPtrOf(value string) *string { return &value }

func floatPtrIfValid(value sql.NullFloat64) *float64 {
	if !value.Valid {
		return nil
	}
	number := value.Float64
	return &number
}

func int64PtrIfValid(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	number := value.Int64
	return &number
}

func boolPtrIfValid(value sql.NullInt64) *bool {
	if !value.Valid {
		return nil
	}
	flag := value.Int64 != 0
	return &flag
}

func floatOr0(value sql.NullFloat64) float64 {
	if !value.Valid {
		return 0
	}
	return value.Float64
}

func int64Or0(value sql.NullInt64) int64 {
	if !value.Valid {
		return 0
	}
	return value.Int64
}

// toFloatPtr converts a JSON value to a number the way JavaScript keeps one:
// numbers as they are, numeric strings parsed, anything else dropped.
func toFloatPtr(value any) *float64 {
	switch v := value.(type) {
	case nil:
		return nil
	case float64:
		return &v
	case float32:
		number := float64(v)
		return &number
	case int:
		number := float64(v)
		return &number
	case int64:
		number := float64(v)
		return &number
	case json.Number:
		if number, err := v.Float64(); err == nil {
			return &number
		}
	}
	return nil
}

// toIntPtr converts a JSON value to an integer, dropping anything else.
func toIntPtr(value any) *int64 {
	number := toFloatPtr(value)
	if number == nil {
		return nil
	}
	truncated := int64(*number)
	return &truncated
}
