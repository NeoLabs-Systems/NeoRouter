package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"

	"github.com/NeoLabs-Systems/neolabs-go/lockout"
	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
)

// Wrong passwords and codes are counted per account with neolabs-go/lockout,
// so spreading tries over addresses does not help. A signed-in account's
// confirmations and the sign-in code step count under separate keys: a stolen
// session cannot lock its owner out of signing in.

func signInKey(userID string) string { return "signin:user:" + userID }

// confirmLocked is the 429 a throttled confirmation gets, with the seconds to
// wait in both the details and the Retry-After header.
func confirmLocked(locked *lockout.LockedError) error {
	err := httpserver.New(http.StatusTooManyRequests, "CONFIRM_LOCKED", locked.Error(),
		map[string]int{"retryAfter": locked.RetryAfter})
	err.RetryAfter = locked.RetryAfter
	return err
}

func signInLocked(locked *lockout.LockedError) error {
	err := httpserver.New(http.StatusTooManyRequests, "TWO_FACTOR_LOCKED",
		"Too many wrong codes. Try again in a few minutes.",
		map[string]int{"retryAfter": locked.RetryAfter})
	err.RetryAfter = locked.RetryAfter
	return err
}

// throttled runs check (true when the password or code is right) under the
// key's count. The try counts as wrong before it is checked; checking the count
// and recording the try happen as one step, so parallel guesses cannot all slip
// in under the limit. A right answer takes back that kind's failures.
func (s *Service) throttled(ctx context.Context, key, kind string, refuse func(*lockout.LockedError) error, check func() (bool, error)) (bool, error) {
	store := lockout.Store{DB: s.DB.W, Now: s.now}
	s.attempts.Lock()
	err := store.Check(ctx, key)
	if err == nil {
		err = store.Record(ctx, key, kind)
	}
	s.attempts.Unlock()
	var locked *lockout.LockedError
	if errors.As(err, &locked) {
		return false, refuse(locked)
	}
	if err != nil {
		return false, err
	}
	right, err := check()
	if err != nil || !right {
		return false, err
	}
	if err := store.Clear(ctx, key, kind); err != nil {
		return false, fmt.Errorf("auth: clear failures: %w", err)
	}
	return true, nil
}

// passwordHash is the stored hash the password confirmation checks against.
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
