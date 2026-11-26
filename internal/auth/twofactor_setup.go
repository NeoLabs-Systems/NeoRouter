package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"

	"github.com/NeoLabs-Systems/neolabs-go/totp"
	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
)

// BeginSetup starts enrolment: it needs the password again, and refuses to
// replace a factor that is already on.
func (s *Service) BeginSetup(ctx context.Context, userID, currentPassword string) (TwoFactorSetup, error) {
	if err := s.assertPassword(ctx, userID, currentPassword); err != nil {
		return TwoFactorSetup{}, err
	}
	enabled, err := s.twoFactorEnabled(ctx, userID)
	if err != nil {
		return TwoFactorSetup{}, err
	}
	if enabled {
		return TwoFactorSetup{}, httpserver.New(http.StatusConflict, "TWO_FACTOR_ENABLED",
			"Two-factor is already on. Turn it off first to set up another app.")
	}
	var username, email string
	err = s.DB.R.QueryRowContext(ctx, `SELECT username, email FROM users WHERE id = ?`, userID).Scan(&username, &email)
	if errors.Is(err, sql.ErrNoRows) {
		return TwoFactorSetup{}, httpserver.New(http.StatusNotFound, "USER_NOT_FOUND", "This account does not exist.")
	}
	if err != nil {
		return TwoFactorSetup{}, fmt.Errorf("auth: read user for setup: %w", err)
	}
	secret, err := totp.GenerateSecret()
	if err != nil {
		return TwoFactorSetup{}, err
	}
	label := email
	if label == "" {
		label = username
	}
	sealed, err := s.sealSecret(secret)
	if err != nil {
		return TwoFactorSetup{}, err
	}
	if _, err := s.DB.W.ExecContext(ctx,
		`INSERT INTO user_two_factor (user_id, pending_secret_encrypted) VALUES (?, ?)
		ON CONFLICT(user_id) DO UPDATE SET pending_secret_encrypted = excluded.pending_secret_encrypted,
			updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now')`, userID, sealed); err != nil {
		return TwoFactorSetup{}, fmt.Errorf("auth: store pending secret: %w", err)
	}
	status, err := s.TwoFactorStatus(ctx, userID)
	if err != nil {
		return TwoFactorSetup{}, err
	}
	return TwoFactorSetup{OTPAuthURL: totp.URI(totpIssuer, label, secret), ManualKey: secret, Status: status}, nil
}

// Enable turns the factor on once a code from the pending secret proves the app
// was set up, and hands out the recovery codes.
func (s *Service) Enable(ctx context.Context, userID, code string) (TwoFactorCodes, error) {
	factor, err := s.readTwoFactor(ctx, s.DB.R, userID)
	if err != nil {
		return TwoFactorCodes{}, err
	}
	if factor == nil || !factor.PendingSecret.Valid || factor.PendingSecret.String == "" {
		return TwoFactorCodes{}, httpserver.New(http.StatusBadRequest, "NO_PENDING_SETUP",
			"Start the two-factor setup first.")
	}
	secret, err := s.openSecret(factor.PendingSecret)
	if err != nil {
		return TwoFactorCodes{}, err
	}
	step, ok := matchTotp(secret, code, s.now())
	if !ok {
		return TwoFactorCodes{}, httpserver.New(http.StatusUnauthorized, "INVALID_TWO_FACTOR",
			"The code from your authenticator app is not valid.")
	}
	codes, hashes, err := s.newRecoveryCodes()
	if err != nil {
		return TwoFactorCodes{}, err
	}
	tx, err := s.DB.W.BeginTx(ctx, nil)
	if err != nil {
		return TwoFactorCodes{}, fmt.Errorf("auth: begin: %w", err)
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx,
		`UPDATE user_two_factor SET secret_encrypted = pending_secret_encrypted, pending_secret_encrypted = NULL, enabled = 1,
		enabled_at = strftime('%Y-%m-%dT%H:%M:%fZ','now'), last_totp_step = ?,
		updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE user_id = ? AND pending_secret_encrypted = ?`,
		step, userID, factor.PendingSecret.String)
	if err != nil {
		return TwoFactorCodes{}, fmt.Errorf("auth: enable two-factor: %w", err)
	}
	if changed, err := result.RowsAffected(); err != nil || changed != 1 {
		return TwoFactorCodes{}, httpserver.New(http.StatusBadRequest, "NO_PENDING_SETUP",
			"Start the two-factor setup first.")
	}
	if err := replaceRecoveryCodes(ctx, tx, userID, hashes); err != nil {
		return TwoFactorCodes{}, err
	}
	if err := tx.Commit(); err != nil {
		return TwoFactorCodes{}, fmt.Errorf("auth: commit two-factor: %w", err)
	}
	status, err := s.TwoFactorStatus(ctx, userID)
	if err != nil {
		return TwoFactorCodes{}, err
	}
	return TwoFactorCodes{RecoveryCodes: codes, Status: status}, nil
}

// Disable turns the factor off, which needs the password and a code.
func (s *Service) Disable(ctx context.Context, userID, currentPassword, code string) (TwoFactorChange, error) {
	if err := s.assertPassword(ctx, userID, currentPassword); err != nil {
		return TwoFactorChange{}, err
	}
	enabled, err := s.twoFactorEnabled(ctx, userID)
	if err != nil {
		return TwoFactorChange{}, err
	}
	if !enabled {
		status, err := s.TwoFactorStatus(ctx, userID)
		if err != nil {
			return TwoFactorChange{}, err
		}
		return TwoFactorChange{Status: status}, nil
	}
	if err := s.assertCode(ctx, userID, code); err != nil {
		return TwoFactorChange{}, err
	}
	tx, err := s.DB.W.BeginTx(ctx, nil)
	if err != nil {
		return TwoFactorChange{}, fmt.Errorf("auth: begin: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM user_two_factor WHERE user_id = ?`, userID); err != nil {
		return TwoFactorChange{}, fmt.Errorf("auth: delete two-factor: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM user_recovery_codes WHERE user_id = ?`, userID); err != nil {
		return TwoFactorChange{}, fmt.Errorf("auth: delete recovery codes: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return TwoFactorChange{}, fmt.Errorf("auth: commit disable: %w", err)
	}
	status, err := s.TwoFactorStatus(ctx, userID)
	if err != nil {
		return TwoFactorChange{}, err
	}
	return TwoFactorChange{Status: status}, nil
}

// RegenerateRecoveryCodes replaces the recovery codes, which needs the password
// and a code.
func (s *Service) RegenerateRecoveryCodes(ctx context.Context, userID, currentPassword, code string) (TwoFactorCodes, error) {
	if err := s.assertPassword(ctx, userID, currentPassword); err != nil {
		return TwoFactorCodes{}, err
	}
	enabled, err := s.twoFactorEnabled(ctx, userID)
	if err != nil {
		return TwoFactorCodes{}, err
	}
	if !enabled {
		return TwoFactorCodes{}, httpserver.New(http.StatusBadRequest, "TWO_FACTOR_DISABLED",
			"Two-factor authentication is not enabled.")
	}
	if err := s.assertCode(ctx, userID, code); err != nil {
		return TwoFactorCodes{}, err
	}
	codes, err := s.storeRecoveryCodes(ctx, userID)
	if err != nil {
		return TwoFactorCodes{}, err
	}
	status, err := s.TwoFactorStatus(ctx, userID)
	if err != nil {
		return TwoFactorCodes{}, err
	}
	return TwoFactorCodes{RecoveryCodes: codes, Status: status}, nil
}
