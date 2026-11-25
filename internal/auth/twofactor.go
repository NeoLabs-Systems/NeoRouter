package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/passhash"
	"github.com/NeoLabs-Systems/neolabs-go/totp"
	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
)

// TOTP two-factor authentication, following NeoAgent: an authenticator-app code
// (6 digits, 30 s, one step of clock drift) or a one-time recovery code.
const (
	totpIssuer        = "NeoRouter"
	recoveryCodeCount = 10
	recoveryHashCost  = 10
	maxFailedAttempts = 5
	lockoutDuration   = 5 * time.Minute
)

// TwoFactorStatus is the state of one account's second factor.
type TwoFactorStatus struct {
	Enabled                bool    `json:"enabled"`
	Pending                bool    `json:"pending"`
	EnabledAt              *string `json:"enabledAt"`
	RecoveryCodesRemaining int     `json:"recoveryCodesRemaining"`
}

// TwoFactorSetup is the enrolment answer: the address an authenticator app
// scans, the key to type in by hand, and the state of the factor.
type TwoFactorSetup struct {
	OTPAuthURL string          `json:"otpauthUrl"`
	ManualKey  string          `json:"manualKey"`
	Status     TwoFactorStatus `json:"status"`
}

// TwoFactorCodes is the answer of the endpoints that hand out recovery codes.
type TwoFactorCodes struct {
	RecoveryCodes []string        `json:"recoveryCodes"`
	Status        TwoFactorStatus `json:"status"`
}

// TwoFactorChange is the answer of the endpoints that only change the state.
type TwoFactorChange struct {
	Status TwoFactorStatus `json:"status"`
}

type twoFactorRow struct {
	UserID          string
	SecretEncrypted sql.NullString
	PendingSecret   sql.NullString
	Enabled         int
	EnabledAt       sql.NullString
	FailedAttempts  int
	LockedUntil     int64
}

// readTwoFactor reads an account's factor row, or nil when it has none.
func (s *Service) readTwoFactor(ctx context.Context, q dbtx, userID string) (*twoFactorRow, error) {
	var factor twoFactorRow
	err := q.QueryRowContext(ctx,
		`SELECT user_id, secret_encrypted, pending_secret_encrypted, enabled, enabled_at, failed_attempts, locked_until
		FROM user_two_factor WHERE user_id = ?`, userID).
		Scan(&factor.UserID, &factor.SecretEncrypted, &factor.PendingSecret, &factor.Enabled,
			&factor.EnabledAt, &factor.FailedAttempts, &factor.LockedUntil)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("auth: read two-factor: %w", err)
	}
	return &factor, nil
}

// TwoFactorStatus reads the state of an account's second factor for the API.
func (s *Service) TwoFactorStatus(ctx context.Context, userID string) (TwoFactorStatus, error) {
	factor, err := s.readTwoFactor(ctx, s.DB.R, userID)
	if err != nil {
		return TwoFactorStatus{}, err
	}
	var remaining int
	if err := s.DB.R.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM user_recovery_codes WHERE user_id = ? AND used_at IS NULL`, userID).
		Scan(&remaining); err != nil {
		return TwoFactorStatus{}, fmt.Errorf("auth: count recovery codes: %w", err)
	}
	status := TwoFactorStatus{RecoveryCodesRemaining: remaining}
	if factor != nil {
		status.Enabled = factor.Enabled == 1
		status.Pending = factor.PendingSecret.Valid && factor.PendingSecret.String != ""
		status.EnabledAt = stringPtr(factor.EnabledAt)
	}
	return status, nil
}

// twoFactorEnabled reports whether the account signs in with a code.
func (s *Service) twoFactorEnabled(ctx context.Context, userID string) bool {
	factor, err := s.readTwoFactor(ctx, s.DB.R, userID)
	if err != nil {
		s.log().Error("auth: read two-factor", "error", err, "userId", userID)
		return false
	}
	return factor != nil && factor.Enabled == 1
}

// sealSecret seals a factor secret for storage.
func (s *Service) sealSecret(secret string) (string, error) {
	sealer, err := s.sealer()
	if err != nil {
		return "", err
	}
	return sealer.Seal(secret)
}

// openSecret reads a stored factor secret.
func (s *Service) openSecret(value sql.NullString) (string, error) {
	if !value.Valid {
		return "", errors.New("auth: two-factor secret is missing")
	}
	sealer, err := s.sealer()
	if err != nil {
		return "", err
	}
	return sealer.Open(value.String)
}

// AssertPassword asks a signed-in account for its password again, under the
// account's confirm throttle.
func (s *Service) AssertPassword(ctx context.Context, userID, password string) error {
	return s.assertPassword(ctx, userID, password)
}

func (s *Service) assertPassword(ctx context.Context, userID, password string) error {
	right, err := s.guardConfirm(ctx, userID, "password", func() (bool, error) {
		hash, found, err := s.passwordHash(ctx, userID)
		if err != nil {
			return false, err
		}
		return found && passhash.Verify(hash, password), nil
	})
	if err != nil {
		return err
	}
	if !right {
		return httpserver.New(http.StatusUnauthorized, "INVALID_PASSWORD", "The current password is incorrect.")
	}
	return nil
}

// assertCode asks a signed-in account for a code, counted apart from the
// sign-in code lockout so a stolen session cannot lock sign-in.
func (s *Service) assertCode(ctx context.Context, userID, code string) error {
	if strings.TrimSpace(code) == "" {
		return httpserver.New(http.StatusUnauthorized, "INVALID_TWO_FACTOR", "The code is not valid.")
	}
	right, err := s.guardConfirm(ctx, userID, "code", func() (bool, error) {
		return s.matchCode(ctx, userID, code)
	})
	if err != nil {
		return err
	}
	if !right {
		return httpserver.New(http.StatusUnauthorized, "INVALID_TWO_FACTOR", "The code is not valid.")
	}
	return nil
}

// verifyTotp checks an authenticator code: six digits, one step of drift.
func verifyTotp(secret, code string) bool {
	return totp.Verify(secret, code, time.Now())
}

// matchCode accepts an authenticator code, or a recovery code it then uses up.
func (s *Service) matchCode(ctx context.Context, userID, code string) (bool, error) {
	factor, err := s.readTwoFactor(ctx, s.DB.R, userID)
	if err != nil {
		return false, err
	}
	if factor == nil || factor.Enabled != 1 {
		return true, nil
	}
	secret, err := s.openSecret(factor.SecretEncrypted)
	if err != nil {
		return false, err
	}
	if verifyTotp(secret, code) {
		return true, nil
	}
	return s.consumeRecoveryCode(ctx, userID, code)
}

// consumeRecoveryCode uses up a one-time recovery code if it matches.
func (s *Service) consumeRecoveryCode(ctx context.Context, userID, code string) (bool, error) {
	normalized := totp.NormalizeRecoveryCode(code)
	if jsLen(normalized) < 6 {
		return false, nil
	}
	rows, err := s.DB.R.QueryContext(ctx,
		`SELECT id, code_hash FROM user_recovery_codes WHERE user_id = ? AND used_at IS NULL ORDER BY id`, userID)
	if err != nil {
		return false, fmt.Errorf("auth: read recovery codes: %w", err)
	}
	type entry struct {
		id   int64
		hash string
	}
	var entries []entry
	for rows.Next() {
		var item entry
		if err := rows.Scan(&item.id, &item.hash); err != nil {
			rows.Close()
			return false, fmt.Errorf("auth: read recovery code: %w", err)
		}
		entries = append(entries, item)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("auth: read recovery codes: %w", err)
	}
	for _, item := range entries {
		if !passhash.Verify(item.hash, normalized) {
			continue
		}
		if _, err := s.DB.W.ExecContext(ctx,
			`UPDATE user_recovery_codes SET used_at = strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id = ?`, item.id); err != nil {
			return false, fmt.Errorf("auth: use recovery code: %w", err)
		}
		return true, nil
	}
	return false, nil
}

// storeRecoveryCodes mints a fresh set of ten one-time codes and stores only
// their bcrypt hashes.
func (s *Service) storeRecoveryCodes(ctx context.Context, userID string) ([]string, error) {
	codes := make([]string, 0, recoveryCodeCount)
	hashes := make([]string, 0, recoveryCodeCount)
	for i := 0; i < recoveryCodeCount; i++ {
		code, err := totp.NewRecoveryCode()
		if err != nil {
			return nil, err
		}
		hash, err := passhash.Hash(totp.NormalizeRecoveryCode(code), recoveryHashCost)
		if err != nil {
			return nil, err
		}
		codes = append(codes, code)
		hashes = append(hashes, hash)
	}
	tx, err := s.DB.W.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("auth: begin: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM user_recovery_codes WHERE user_id = ?`, userID); err != nil {
		return nil, fmt.Errorf("auth: clear recovery codes: %w", err)
	}
	for _, hash := range hashes {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO user_recovery_codes (user_id, code_hash) VALUES (?, ?)`, userID, hash); err != nil {
			return nil, fmt.Errorf("auth: store recovery code: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("auth: commit recovery codes: %w", err)
	}
	return codes, nil
}

// verifyCode is the sign-in code step: an authenticator or recovery code, with a
// short lockout after repeated failures so codes cannot be brute-forced. An
// account without a factor never asks for a code.
func (s *Service) verifyCode(ctx context.Context, userID, code string) (bool, error) {
	factor, err := s.readTwoFactor(ctx, s.DB.R, userID)
	if err != nil {
		return false, err
	}
	if factor == nil || factor.Enabled != 1 {
		return true, nil
	}
	now := time.Now()
	if factor.LockedUntil > now.UnixMilli() {
		return false, httpserver.New(http.StatusTooManyRequests, "TWO_FACTOR_LOCKED",
			"Too many wrong codes. Try again in a few minutes.")
	}
	valid, err := s.matchCode(ctx, userID, code)
	if err != nil {
		return false, err
	}
	if valid {
		if _, err := s.DB.W.ExecContext(ctx,
			`UPDATE user_two_factor SET failed_attempts = 0, locked_until = 0 WHERE user_id = ?`, userID); err != nil {
			return false, fmt.Errorf("auth: reset failed attempts: %w", err)
		}
		return true, nil
	}
	attempts := factor.FailedAttempts + 1
	failed, lockedUntil := attempts, int64(0)
	if attempts >= maxFailedAttempts {
		failed, lockedUntil = 0, now.Add(lockoutDuration).UnixMilli()
	}
	if _, err := s.DB.W.ExecContext(ctx,
		`UPDATE user_two_factor SET failed_attempts = ?, locked_until = ? WHERE user_id = ?`,
		failed, lockedUntil, userID); err != nil {
		return false, fmt.Errorf("auth: record failed attempt: %w", err)
	}
	return false, nil
}

// Confirm is what a step that matters (turning 2FA off, exporting data, deleting
// the account) asks for: the password again, and a code when 2FA is on.
// requireFactor refuses outright when 2FA is off.
func (s *Service) Confirm(ctx context.Context, userID, currentPassword, code string, requireFactor bool) error {
	if err := s.assertPassword(ctx, userID, currentPassword); err != nil {
		return err
	}
	if !s.twoFactorEnabled(ctx, userID) {
		if requireFactor {
			return httpserver.New(http.StatusForbidden, "TWO_FACTOR_REQUIRED", "Turn on two-factor authentication first.")
		}
		return nil
	}
	return s.assertCode(ctx, userID, code)
}
