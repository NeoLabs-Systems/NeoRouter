package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

// CapabilityCount is how many models of one kind a user has, and how many of
// them are free.
type CapabilityCount struct {
	ID    string `json:"id"`
	Total int64  `json:"total"`
	Free  int64  `json:"free"`
}

// CapabilitySummary counts the user's models per capability (chat,
// embedding, image, …) so the UI can show kinds that are not routable yet.
func (s *Service) CapabilitySummary(ctx context.Context, userID string) ([]CapabilityCount, error) {
	rows, err := s.DB.R.QueryContext(ctx, `SELECT m.capability, COUNT(*) total, SUM(m.is_free) free FROM models m
		JOIN provider_accounts a ON a.id = m.account_id WHERE a.user_id = ? AND m.available = 1 AND a.enabled = 1
		GROUP BY m.capability ORDER BY total DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("catalog: capability summary: %w", err)
	}
	defer rows.Close()
	summary := []CapabilityCount{}
	for rows.Next() {
		var item CapabilityCount
		var free sql.NullFloat64
		if err := rows.Scan(&item.ID, &item.Total, &free); err != nil {
			return nil, fmt.Errorf("catalog: capability summary: %w", err)
		}
		item.Free = int64(floatOr0(free))
		summary = append(summary, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("catalog: capability summary: %w", err)
	}
	return summary, nil
}

// ModelUpdate is a PATCH /api/v1/models/{ref} body.
type ModelUpdate struct {
	// Enabled is nil when the request does not change it.
	Enabled   *bool
	Overrides map[string]any
}

// UpdateModel applies one model's enabled flag and overrides, reclassifies the
// account and returns the updated entry.
//
// UpdateModel applies a change to one model.
func (s *Service) UpdateModel(ctx context.Context, userID string, ref int64, update ModelUpdate) (Entry, error) {
	var accountID string
	var enabled int64
	var overridesJSON string
	err := s.DB.R.QueryRowContext(ctx, `SELECT m.account_id, m.enabled, m.overrides_json FROM models m
		JOIN provider_accounts a ON a.id = m.account_id WHERE m.id = ? AND a.user_id = ?`, ref, userID).Scan(
		&accountID, &enabled, &overridesJSON)
	if err == sql.ErrNoRows {
		return Entry{}, modelNotFound()
	}
	if err != nil {
		return Entry{}, fmt.Errorf("catalog: update model: %w", err)
	}
	next := parseJSONObject(overridesJSON)
	if update.Overrides != nil {
		for _, key := range overrideKeys {
			value, present := update.Overrides[key]
			if !present {
				continue
			}
			if value == nil {
				delete(next, key)
				continue
			}
			next[key] = value
		}
	}
	encoded, err := json.Marshal(next)
	if err != nil {
		return Entry{}, fmt.Errorf("catalog: update model: %w", err)
	}
	nextEnabled := enabled
	if update.Enabled != nil {
		nextEnabled = 0
		if *update.Enabled {
			nextEnabled = 1
		}
	}
	if _, err := s.DB.W.ExecContext(ctx, `UPDATE models SET enabled = ?, overrides_json = ? WHERE id = ?`,
		nextEnabled, string(encoded), ref); err != nil {
		return Entry{}, fmt.Errorf("catalog: update model: %w", err)
	}
	if err := s.ReclassifyAccount(ctx, accountID); err != nil {
		return Entry{}, err
	}
	return s.GetModel(ctx, userID, ref)
}

// BulkResult reports how many model rows a bulk switch touched.
type BulkResult struct {
	Updated int64 `json:"updated"`
}

// BulkSetEnabled switches every model of an account on or off, optionally
// only the free ones.
//
// BulkSetEnabled enables or disables many models at once.
func (s *Service) BulkSetEnabled(ctx context.Context, userID string, accountID *string, enabled, onlyFree bool) (BulkResult, error) {
	var account sql.NullString
	if accountID != nil && *accountID != "" {
		account = sql.NullString{String: *accountID, Valid: true}
	}
	onlyFreeFlag := int64(0)
	if onlyFree {
		onlyFreeFlag = 1
	}
	enabledFlag := int64(0)
	if enabled {
		enabledFlag = 1
	}
	result, err := s.DB.W.ExecContext(ctx, `UPDATE models SET enabled = ? WHERE account_id IN
		(SELECT id FROM provider_accounts WHERE user_id = ? AND (? IS NULL OR id = ?)) AND (? = 0 OR is_free = 1)`,
		enabledFlag, userID, account, account, onlyFreeFlag)
	if err != nil {
		return BulkResult{}, fmt.Errorf("catalog: bulk set enabled: %w", err)
	}
	changes, err := result.RowsAffected()
	if err != nil {
		return BulkResult{}, fmt.Errorf("catalog: bulk set enabled: %w", err)
	}
	return BulkResult{Updated: changes}, nil
}
