package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/lockout"
	"github.com/NeoLabs-Systems/neolabs-go/passhash"
	"github.com/NeoLabs-Systems/neolabs-go/totp"
	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
)

// TOTP two-factor authentication, following NeoAgent: an authenticator-app code
// (6 digits, 30 s, one step of clock drift) or a one-time recovery code.
const (
	totpIssuer        = "NeoRouter"
	totpStep          = 30 * time.Second
	recoveryCodeCount = 10
	recoveryHashCost  = 10
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
}

// readTwoFactor reads an account's factor row, or nil when it has none.
func (s *Service) readTwoFactor(ctx context.Context, q dbtx, userID string) (*twoFactorRow, error) {
	var factor twoFactorRow
	err := q.QueryRowContext(ctx,
		`SELECT user_id, secret_encrypted, pending_secret_encrypted, enabled, enabled_at
		FROM user_two_factor WHERE user_id = ?`, userID).
		Scan(&factor.UserID, &factor.SecretEncrypted, &factor.PendingSecret, &factor.Enabled, &factor.EnabledAt)
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

// twoFactorEnabled reports whether the account signs in with a code. A read
// failure is returned: it must never pass for "off".
func (s *Service) twoFactorEnabled(ctx context.Context, userID string) (bool, error) {
	factor, err := s.readTwoFactor(ctx, s.DB.R, userID)
	if err != nil {
		return false, err
	}
	return factor != nil && factor.Enabled == 1, nil
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
	right, err := s.throttled(ctx, lockout.ConfirmKey(userID), "password", confirmLocked, func() (bool, error) {
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
	right, err := s.throttled(ctx, lockout.ConfirmKey(userID), "code", confirmLocked, func() (bool, error) {
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

// matchTotp checks an authenticator code (six digits, one step of drift) and
// returns the time step it belongs to.
func matchTotp(secret, code string, now time.Time) (int64, bool) {
	if !totp.Verify(secret, code, now) {
		return 0, false
	}
	normalized := totp.NormalizeCode(code)
	for _, at := range []time.Time{now, now.Add(-totpStep), now.Add(totpStep)} {
		if want, err := totp.Code(secret, at); err == nil && passhash.Equal(want, normalized) {
			return at.Unix() / int64(totpStep/time.Second), true
		}
	}
	return 0, false
}

// useTotp accepts an authenticator code once: its time step has to be newer
// than the last one the account used.
func (s *Service) useTotp(ctx context.Context, userID, secret, code string) (bool, error) {
	step, ok := matchTotp(secret, code, s.now())
	if !ok {
		return false, nil
	}
	result, err := s.DB.W.ExecContext(ctx,
		`UPDATE user_two_factor SET last_totp_step = ? WHERE user_id = ? AND last_totp_step < ?`, step, userID, step)
	if err != nil {
		return false, fmt.Errorf("auth: use code: %w", err)
	}
	changed, err := result.RowsAffected()
	return changed == 1, err
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
	used, err := s.useTotp(ctx, userID, secret, code)
	if err != nil || used {
		return used, err
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
		result, err := s.DB.W.ExecContext(ctx,
			`UPDATE user_recovery_codes SET used_at = strftime('%Y-%m-%dT%H:%M:%fZ','now')
			WHERE id = ? AND used_at IS NULL`, item.id)
		if err != nil {
			return false, fmt.Errorf("auth: use recovery code: %w", err)
		}
		changed, err := result.RowsAffected()
		return changed == 1, err
	}
	return false, nil
}

// newRecoveryCodes mints a set of one-time codes and their bcrypt hashes.
func (s *Service) newRecoveryCodes() (codes, hashes []string, err error) {
	for i := 0; i < recoveryCodeCount; i++ {
		code, err := totp.NewRecoveryCode()
		if err != nil {
			return nil, nil, err
		}
		hash, err := passhash.Hash(totp.NormalizeRecoveryCode(code), s.hashCost(recoveryHashCost))
		if err != nil {
			return nil, nil, err
		}
		codes = append(codes, code)
		hashes = append(hashes, hash)
	}
	return codes, hashes, nil
}

// replaceRecoveryCodes swaps an account's codes for a new set.
func replaceRecoveryCodes(ctx context.Context, q dbtx, userID string, hashes []string) error {
	if _, err := q.ExecContext(ctx, `DELETE FROM user_recovery_codes WHERE user_id = ?`, userID); err != nil {
		return fmt.Errorf("auth: clear recovery codes: %w", err)
	}
	for _, hash := range hashes {
		if _, err := q.ExecContext(ctx,
			`INSERT INTO user_recovery_codes (user_id, code_hash) VALUES (?, ?)`, userID, hash); err != nil {
			return fmt.Errorf("auth: store recovery code: %w", err)
		}
	}
	return nil
}

// storeRecoveryCodes replaces the codes and returns the new ones, of which only
// the hashes are stored.
func (s *Service) storeRecoveryCodes(ctx context.Context, userID string) ([]string, error) {
	codes, hashes, err := s.newRecoveryCodes()
	if err != nil {
		return nil, err
	}
	tx, err := s.DB.W.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("auth: begin: %w", err)
	}
	defer tx.Rollback()
	if err := replaceRecoveryCodes(ctx, tx, userID, hashes); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("auth: commit recovery codes: %w", err)
	}
	return codes, nil
}

// verifyCode is the sign-in code step: an authenticator or recovery code, under
// the account's sign-in lockout so codes cannot be brute-forced. An account
// without a factor never asks for a code.
func (s *Service) verifyCode(ctx context.Context, userID, code string) (bool, error) {
	enabled, err := s.twoFactorEnabled(ctx, userID)
	if err != nil {
		return false, err
	}
	if !enabled {
		return true, nil
	}
	return s.throttled(ctx, signInKey(userID), "code", signInLocked, func() (bool, error) {
		return s.matchCode(ctx, userID, code)
	})
}

// Confirm is what a step that matters (turning 2FA off, exporting data, deleting
// the account) asks for: the password again, and a code when 2FA is on.
// requireFactor refuses outright when 2FA is off.
func (s *Service) Confirm(ctx context.Context, userID, currentPassword, code string, requireFactor bool) error {
	if err := s.assertPassword(ctx, userID, currentPassword); err != nil {
		return err
	}
	enabled, err := s.twoFactorEnabled(ctx, userID)
	if err != nil {
		return err
	}
	if !enabled {
		if requireFactor {
			return httpserver.New(http.StatusForbidden, "TWO_FACTOR_REQUIRED", "Turn on two-factor authentication first.")
		}
		return nil
	}
	return s.assertCode(ctx, userID, code)
}
