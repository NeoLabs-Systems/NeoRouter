package localmodels

// store.go is the local_models table — which models are installed or
// downloading, and whether the admin wants them running.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/NeoLabs-Systems/neolabs-go/dbx"
)

// StoreRow is one row of local_models.
type StoreRow struct {
	ID        string
	Status    string
	Enabled   bool
	CreatedAt string
}

// Store reads and writes local_models.
type Store struct {
	DB *dbx.DB
}

// List returns every installed or downloading model, oldest first.
func (s *Store) List(ctx context.Context) ([]StoreRow, error) {
	rows, err := s.DB.R.QueryContext(ctx, `SELECT id, status, enabled, created_at FROM local_models ORDER BY created_at`)
	if err != nil {
		return nil, fmt.Errorf("localmodels: list installed models: %w", err)
	}
	defer rows.Close()
	out := []StoreRow{}
	for rows.Next() {
		var row StoreRow
		var enabled int
		if err := rows.Scan(&row.ID, &row.Status, &enabled, &row.CreatedAt); err != nil {
			return nil, fmt.Errorf("localmodels: read installed model: %w", err)
		}
		row.Enabled = enabled != 0
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("localmodels: list installed models: %w", err)
	}
	return out, nil
}

// Get returns one row, or nil when the model is not installed.
func (s *Store) Get(ctx context.Context, id string) (*StoreRow, error) {
	var row StoreRow
	var enabled int
	err := s.DB.R.QueryRowContext(ctx,
		`SELECT id, status, enabled, created_at FROM local_models WHERE id = ?`, id).
		Scan(&row.ID, &row.Status, &enabled, &row.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("localmodels: read installed model %s: %w", id, err)
	}
	row.Enabled = enabled != 0
	return &row, nil
}

// Add records a model as downloading.
func (s *Store) Add(ctx context.Context, id, userID string) error {
	if _, err := s.DB.W.ExecContext(ctx,
		`INSERT INTO local_models (id, status, installed_by_user_id) VALUES (?, 'downloading', ?)`, id, userID); err != nil {
		return fmt.Errorf("localmodels: add model %s: %w", id, err)
	}
	return nil
}

// StoreUpdate is the set of fields an update may change; nil leaves a field
// alone.
type StoreUpdate struct {
	Status  *string
	Enabled *bool
}

// Update writes the given fields and the row's timestamp.
func (s *Store) Update(ctx context.Context, id string, update StoreUpdate) error {
	sets := []string{}
	values := []any{}
	if update.Status != nil {
		sets = append(sets, `status = ?`)
		values = append(values, *update.Status)
	}
	if update.Enabled != nil {
		sets = append(sets, `enabled = ?`)
		if *update.Enabled {
			values = append(values, 1)
		} else {
			values = append(values, 0)
		}
	}
	if len(sets) == 0 {
		return nil
	}
	query := `UPDATE local_models SET ` + joinComma(sets) +
		`, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id = ?`
	if _, err := s.DB.W.ExecContext(ctx, query, append(values, id)...); err != nil {
		return fmt.Errorf("localmodels: update model %s: %w", id, err)
	}
	return nil
}

// Remove deletes a model's row.
func (s *Store) Remove(ctx context.Context, id string) error {
	if _, err := s.DB.W.ExecContext(ctx, `DELETE FROM local_models WHERE id = ?`, id); err != nil {
		return fmt.Errorf("localmodels: remove model %s: %w", id, err)
	}
	return nil
}

// HasInstalled reports whether any model is installed; that is what decides
// whether the "NeoRouter Local" connection is offered.
func (s *Store) HasInstalled(ctx context.Context) (bool, error) {
	var found int
	err := s.DB.R.QueryRowContext(ctx, `SELECT 1 FROM local_models WHERE status = 'installed'`).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("localmodels: read installed models: %w", err)
	}
	return true, nil
}

// joinComma joins SQL fragments.
func joinComma(values []string) string {
	out := ""
	for index, value := range values {
		if index > 0 {
			out += ", "
		}
		out += value
	}
	return out
}
