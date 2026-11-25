package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
)

// Asking a signed-in account for its password or a code again is throttled per
// account, not only per address, so a stolen session cannot guess the password
// faster by spreading its tries over addresses. From the fifth wrong answer in
// the window each next try waits twice as long (2 s, 4 s, ...); the tenth locks
// confirmations until the oldest of those leaves the window. The count is the
// account's own: signing in is not affected, so a stolen session cannot lock the
// owner out.
const (
	// ConfirmWindow is how long a wrong confirmation keeps counting.
	ConfirmWindow = 15 * time.Minute

	failuresBeforeDelay = 5
	failuresBeforeLock  = 10
	baseDelay           = 2000 * time.Millisecond
)

// confirmLocked builds the 429 an over-eager caller gets, with the seconds it
// has to wait in both the details and the Retry-After header.
func confirmLocked(retryAfterMs int64) error {
	retryAfter := int((retryAfterMs + 999) / 1000)
	if retryAfter < 1 {
		retryAfter = 1
	}
	wait := fmt.Sprintf("%d seconds", retryAfter)
	if retryAfter >= 90 {
		wait = fmt.Sprintf("%d minutes", (retryAfter+59)/60)
	}
	err := httpserver.New(http.StatusTooManyRequests, "CONFIRM_LOCKED",
		"Too many wrong passwords or codes for this account. Try again in "+wait+".",
		map[string]int{"retryAfter": retryAfter})
	err.RetryAfter = retryAfter
	return err
}

// confirmFailures reads the account's wrong confirmations inside the window,
// oldest first.
func (s *Service) confirmFailures(ctx context.Context, q dbtx, userID string, now time.Time) ([]int64, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT failed_at_ms FROM confirm_failures WHERE user_id = ? AND failed_at_ms > ? ORDER BY failed_at_ms`,
		userID, now.Add(-ConfirmWindow).UnixMilli())
	if err != nil {
		return nil, fmt.Errorf("auth: read confirm failures: %w", err)
	}
	defer rows.Close()

	var failures []int64
	for rows.Next() {
		var failedAt int64
		if err := rows.Scan(&failedAt); err != nil {
			return nil, fmt.Errorf("auth: read confirm failure: %w", err)
		}
		failures = append(failures, failedAt)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("auth: read confirm failures: %w", err)
	}
	return failures, nil
}

// assertConfirmAllowed refuses a try while the account is throttled.
func (s *Service) assertConfirmAllowed(ctx context.Context, q dbtx, userID string, now time.Time) error {
	failures, err := s.confirmFailures(ctx, q, userID, now)
	if err != nil {
		return err
	}
	windowMs := ConfirmWindow.Milliseconds()
	if len(failures) >= failuresBeforeLock {
		return confirmLocked(failures[len(failures)-failuresBeforeLock] + windowMs - now.UnixMilli())
	}
	if len(failures) >= failuresBeforeDelay {
		delay := baseDelay.Milliseconds() << (len(failures) - failuresBeforeDelay)
		next := failures[len(failures)-1] + delay
		if now.UnixMilli() < next {
			return confirmLocked(next - now.UnixMilli())
		}
	}
	return nil
}

// guardConfirm runs check (true when the password or code is right) under the
// account's count. The try counts as wrong before it is checked, so parallel
// guesses count too; a right answer takes back that kind's failures.
func (s *Service) guardConfirm(ctx context.Context, userID, kind string, check func() (bool, error)) (bool, error) {
	now := time.Now()
	if err := s.assertConfirmAllowed(ctx, s.DB.R, userID, now); err != nil {
		return false, err
	}
	if _, err := s.DB.W.ExecContext(ctx,
		`DELETE FROM confirm_failures WHERE failed_at_ms <= ?`, now.Add(-ConfirmWindow).UnixMilli()); err != nil {
		return false, fmt.Errorf("auth: prune confirm failures: %w", err)
	}
	if _, err := s.DB.W.ExecContext(ctx,
		`INSERT INTO confirm_failures (user_id, kind, failed_at_ms) VALUES (?, ?, ?)`,
		userID, kind, now.UnixMilli()); err != nil {
		return false, fmt.Errorf("auth: record confirm failure: %w", err)
	}
	right, err := check()
	if err != nil {
		return false, err
	}
	if right {
		if _, err := s.DB.W.ExecContext(ctx,
			`DELETE FROM confirm_failures WHERE user_id = ? AND kind = ?`, userID, kind); err != nil {
			return false, fmt.Errorf("auth: clear confirm failures: %w", err)
		}
	}
	return right, nil
}

// passwordTable is the users row the throttle's password check reads.
func (s *Service) passwordHash(ctx context.Context, userID string) (string, bool, error) {
	var hash string
	err := s.DB.R.QueryRowContext(ctx, `SELECT password_hash FROM users WHERE id = ?`, userID).Scan(&hash)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("auth: read password hash: %w", err)
	}
	return hash, true, nil
}
