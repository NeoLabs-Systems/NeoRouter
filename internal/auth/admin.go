package auth

import (
	"context"
	"fmt"
	"net/http"

	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
)

// ListUsers returns every account, oldest first, for the admin user list.
func (s *Service) ListUsers(ctx context.Context) ([]PublicUser, error) {
	rows, err := s.DB.R.QueryContext(ctx, `SELECT `+userColumns+` FROM users ORDER BY created_at`)
	if err != nil {
		return nil, fmt.Errorf("auth: list users: %w", err)
	}
	defer rows.Close()

	users := make([]PublicUser, 0)
	for rows.Next() {
		user, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, publicUser(user))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("auth: list users: %w", err)
	}
	return users, nil
}

// SetUserDisabled disables or re-enables an account. A disabled account cannot
// sign in and its sessions are revoked; nobody can disable themselves.
func (s *Service) SetUserDisabled(ctx context.Context, actorID, userID string, disabled bool) error {
	if actorID == userID {
		return httpserver.New(http.StatusBadRequest, "CANNOT_DISABLE_SELF", "You cannot disable your own account.")
	}
	tx, err := s.DB.W.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("auth: begin: %w", err)
	}
	defer tx.Rollback()

	flag := 0
	if disabled {
		flag = 1
	}
	result, err := tx.ExecContext(ctx,
		`UPDATE users SET disabled_at = CASE WHEN ? THEN strftime('%Y-%m-%dT%H:%M:%fZ','now') ELSE NULL END WHERE id = ?`,
		flag, userID)
	if err != nil {
		return fmt.Errorf("auth: disable user: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("auth: disable user: %w", err)
	}
	if changed == 0 {
		return httpserver.New(http.StatusNotFound, "USER_NOT_FOUND", "This account does not exist.")
	}
	if disabled {
		if err := s.logoutAll(ctx, tx, userID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// SetUserRole makes an account an administrator or an ordinary user.
func (s *Service) SetUserRole(ctx context.Context, actorID, userID, role string) error {
	if actorID == userID {
		return httpserver.New(http.StatusBadRequest, "CANNOT_CHANGE_OWN_ROLE", "You cannot change your own role.")
	}
	result, err := s.DB.W.ExecContext(ctx, `UPDATE users SET role = ? WHERE id = ?`, role, userID)
	if err != nil {
		return fmt.Errorf("auth: set role: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("auth: set role: %w", err)
	}
	if changed == 0 {
		return httpserver.New(http.StatusNotFound, "USER_NOT_FOUND", "This account does not exist.")
	}
	return nil
}

// DeleteUser removes an account and everything that cascades from it.
func (s *Service) DeleteUser(ctx context.Context, actorID, userID string) error {
	if actorID == userID {
		return httpserver.New(http.StatusBadRequest, "CANNOT_DELETE_SELF", "You cannot delete your own account.")
	}
	result, err := s.DB.W.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, userID)
	if err != nil {
		return fmt.Errorf("auth: delete user: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("auth: delete user: %w", err)
	}
	if changed == 0 {
		return httpserver.New(http.StatusNotFound, "USER_NOT_FOUND", "This account does not exist.")
	}
	return nil
}
