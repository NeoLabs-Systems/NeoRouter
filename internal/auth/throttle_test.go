package auth_test

import (
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/totp"
)

// TestConfirmThrottlePerAccount ports
// test/api/confirm_throttle.test.js: asking a signed-in account for its password
// again is throttled per account, apart from signing in.
func TestConfirmThrottlePerAccount(t *testing.T) {
	h := newHarness(t)
	// Freeze the throttle clock so the backoff window does not depend on how
	// long a bcrypt compare takes (which -race makes much slower).
	h.service.Now = func() time.Time { return time.UnixMilli(1700000000000) }
	h.registerAdmin()
	token, id := h.register("confirmpw")

	change := func(currentPassword string) response {
		return h.do("PUT", "/api/v1/auth/password", map[string]any{
			"currentPassword": currentPassword, "newPassword": "SI_SECRET_PASSWORD_Q8C1PS2S",
		}, token)
	}
	for attempt := 0; attempt < 5; attempt++ {
		wrong := change("wrong-" + string(rune('0'+attempt)))
		if wrong.status != http.StatusUnauthorized || wrong.code(t) != "INVALID_PASSWORD" {
			t.Fatalf("wrong password %d: %d %s", attempt, wrong.status, wrong.body)
		}
	}

	// From the fifth wrong answer the next try waits, even with the right
	// password.
	locked := change(h.password)
	if locked.status != http.StatusTooManyRequests || locked.code(t) != "CONFIRM_LOCKED" {
		t.Fatalf("the account should be throttled: %d %s", locked.status, locked.body)
	}
	retryAfter, _ := path(t, locked.object(t), "error", "details", "retryAfter").(float64)
	if retryAfter < 1 {
		t.Fatalf("retryAfter should be at least a second: %s", locked.body)
	}
	if header := locked.header.Get("Retry-After"); header != strconv.Itoa(int(retryAfter)) {
		t.Fatalf("Retry-After %q should match details.retryAfter %v", header, retryAfter)
	}
	if message := path(t, locked.object(t), "error", "message"); message == nil {
		t.Fatalf("CONFIRM_LOCKED carries the wait: %s", locked.body)
	}

	// Counted apart from sign-in: the owner can still sign in.
	login := h.do("POST", "/api/v1/auth/login", map[string]any{"account": "confirmpw", "password": h.password}, "")
	if login.status != http.StatusOK {
		t.Fatalf("signing in is not throttled by confirmations: %d %s", login.status, login.body)
	}

	// Once the wait is over the right password goes through and clears the count.
	if _, err := h.db.W.Exec(`UPDATE confirm_failures SET failed_at_ms = failed_at_ms - 60000 WHERE user_id = ?`, id); err != nil {
		t.Fatalf("age the failures: %v", err)
	}
	setup := h.do("POST", "/api/v1/auth/2fa/setup", map[string]any{"currentPassword": h.password}, token)
	if setup.status != http.StatusOK {
		t.Fatalf("after the wait: %d %s", setup.status, setup.body)
	}
	if failures := h.confirmFailures(id, "password"); failures != 0 {
		t.Fatalf("a right answer clears that kind's failures, got %d", failures)
	}
}

// TestConfirmThrottleLocksForTheWindow ports the ten-failure lock.
func TestConfirmThrottleLocksForTheWindow(t *testing.T) {
	h := newHarness(t)
	h.registerAdmin()
	token, id := h.register("confirmlock")

	now := time.Now().UnixMilli()
	insert, err := h.db.W.Prepare(`INSERT INTO confirm_failures (user_id, kind, failed_at_ms) VALUES (?, ?, ?)`)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	defer insert.Close()
	for attempt := 0; attempt < 10; attempt++ {
		if _, err := insert.Exec(id, "password", now-10*60_000+int64(attempt)); err != nil {
			t.Fatalf("insert failure: %v", err)
		}
	}
	locked := h.do("POST", "/api/v1/auth/2fa/setup", map[string]any{"currentPassword": h.password}, token)
	if locked.status != http.StatusTooManyRequests || locked.code(t) != "CONFIRM_LOCKED" {
		t.Fatalf("ten failures lock confirmations: %d %s", locked.status, locked.body)
	}
	retryAfter, _ := path(t, locked.object(t), "error", "details", "retryAfter").(float64)
	if retryAfter <= 200 {
		t.Fatalf("locked until the oldest failure leaves the fifteen minutes, got %v", retryAfter)
	}
}

// TestConfirmThrottleCountsCodesApart ports the per-kind counting: a session's
// wrong codes do not lock signing in.
func TestConfirmThrottleCountsCodesApart(t *testing.T) {
	h := newHarness(t)
	h.service.Now = func() time.Time { return time.UnixMilli(1700000000000) }
	h.registerAdmin()
	token, id := h.register("confirmcode")

	setup := h.do("POST", "/api/v1/auth/2fa/setup", map[string]any{"currentPassword": h.password}, token)
	if setup.status != http.StatusOK {
		t.Fatalf("setup: %d %s", setup.status, setup.body)
	}
	secret := path(t, setup.object(t), "manualKey").(string)
	code := func() string {
		value, err := totp.Code(secret, time.Now())
		if err != nil {
			t.Fatalf("generate code: %v", err)
		}
		return value
	}
	if enabled := h.do("POST", "/api/v1/auth/2fa/enable", map[string]any{"code": code()}, token); enabled.status != http.StatusOK {
		t.Fatalf("enable: %d %s", enabled.status, enabled.body)
	}

	for attempt := 0; attempt < 5; attempt++ {
		wrong := h.do("POST", "/api/v1/auth/2fa/recovery-codes", map[string]any{
			"currentPassword": h.password, "code": "000000",
		}, token)
		if wrong.status != http.StatusUnauthorized || wrong.code(t) != "INVALID_TWO_FACTOR" {
			t.Fatalf("wrong code %d: %d %s", attempt, wrong.status, wrong.body)
		}
	}
	if failures := h.confirmFailures(id, "code"); failures != 5 {
		t.Fatalf("expected five counted code failures, got %d", failures)
	}
	locked := h.do("POST", "/api/v1/auth/2fa/recovery-codes", map[string]any{
		"currentPassword": h.password, "code": code(),
	}, token)
	if locked.status != http.StatusTooManyRequests || locked.code(t) != "CONFIRM_LOCKED" {
		t.Fatalf("the code step should be throttled: %d %s", locked.status, locked.body)
	}

	// Signing in with a code still works: the session's guesses did not lock it.
	first := h.do("POST", "/api/v1/auth/login", map[string]any{"account": "confirmcode", "password": h.password}, "")
	second := h.do("POST", "/api/v1/auth/login/2fa", map[string]any{
		"challenge": path(t, first.object(t), "challenge").(string), "code": code(),
	}, "")
	if second.status != http.StatusOK {
		t.Fatalf("signing in with a code: %d %s", second.status, second.body)
	}
}

// confirmFailures counts an account's recorded wrong confirmations.
func (h *harness) confirmFailures(userID, kind string) int {
	h.t.Helper()
	var count int
	if err := h.db.R.QueryRow(`SELECT COUNT(*) FROM confirm_failures WHERE user_id = ? AND kind = ?`, userID, kind).
		Scan(&count); err != nil {
		h.t.Fatalf("count confirm failures: %v", err)
	}
	return count
}
