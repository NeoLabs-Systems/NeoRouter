package auth_test

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/passhash"
	"github.com/NeoLabs-Systems/neorouter/internal/auth"
	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
)

var requestContext = auth.RequestContext{IPAddress: "192.0.2.1", UserAgent: "test"}

func httpCode(err error) string {
	var failure *httpserver.HttpError
	if errors.As(err, &failure) {
		return failure.Code
	}
	return ""
}

// parallel sends the same request count times at once and tallies the codes.
func (h *harness) parallel(count int, send func() response) map[string]int {
	h.t.Helper()
	results := make([]response, count)
	var wait sync.WaitGroup
	start := make(chan struct{})
	for i := range results {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			results[i] = send()
		}()
	}
	close(start)
	wait.Wait()
	codes := map[string]int{}
	for _, r := range results {
		if r.status < 300 {
			codes["ok"]++
			continue
		}
		codes[r.code(h.t)]++
	}
	return codes
}

// With the clock standing still, only the five tries before the backoff starts
// may be evaluated, however many arrive at once.
func TestParallelGuessesCannotPassTheConfirmLockout(t *testing.T) {
	h := newHarness(t)
	h.registerAdmin()
	change := func(current string) response {
		return h.do("PUT", "/api/v1/auth/password", map[string]any{
			"currentPassword": current, "newPassword": "violet-Harbor-9-lantern",
		}, h.token)
	}
	codes := h.parallel(40, func() response { return change("not-the-password") })
	if codes["INVALID_PASSWORD"] != 5 || codes["CONFIRM_LOCKED"] != 35 {
		t.Fatalf("evaluated guesses: %v", codes)
	}
	if right := change(h.password); right.code(t) != "CONFIRM_LOCKED" {
		t.Fatalf("the right password must wait too: %d %s", right.status, right.body)
	}
}

func TestParallelGuessesCannotPassTheSignInCodeLockout(t *testing.T) {
	h := newHarness(t)
	h.registerAdmin()
	h.enableTwoFactor()
	first := h.do("POST", "/api/v1/auth/login", map[string]any{"account": "admin", "password": h.password}, "")
	challenge := path(t, first.object(t), "challenge").(string)

	var evaluated, locked atomic.Int32
	var wait sync.WaitGroup
	for range 30 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, err := h.service.LoginWithTwoFactor(h.context(), challenge, "999999", requestContext)
			switch httpCode(err) {
			case "INVALID_TWO_FACTOR":
				evaluated.Add(1)
			case "TWO_FACTOR_LOCKED":
				locked.Add(1)
			}
		}()
	}
	wait.Wait()
	if evaluated.Load() != 5 || locked.Load() != 25 {
		t.Fatalf("evaluated %d, locked %d", evaluated.Load(), locked.Load())
	}
}

func TestAuthenticatorCodeWorksOnce(t *testing.T) {
	h := newHarness(t)
	h.registerAdmin()
	secret := h.enableTwoFactorSecret()
	code := h.code(secret)
	signIn := func() response {
		first := h.do("POST", "/api/v1/auth/login", map[string]any{"account": "admin", "password": h.password}, "")
		return h.do("POST", "/api/v1/auth/login/2fa", map[string]any{
			"challenge": path(t, first.object(t), "challenge").(string), "code": code,
		}, "")
	}
	if first := signIn(); first.status != http.StatusOK {
		t.Fatalf("first use: %d %s", first.status, first.body)
	}
	if replay := signIn(); replay.code(t) != "INVALID_TWO_FACTOR" {
		t.Fatalf("replay: %d %s", replay.status, replay.body)
	}
	// An older step than the one already used is refused too.
	h.advance(time.Minute)
	code = h.code(secret)
	if later := signIn(); later.status != http.StatusOK {
		t.Fatalf("a later code: %d %s", later.status, later.body)
	}
}

func TestRecoveryCodeCannotBeSpentTwiceAtOnce(t *testing.T) {
	h := newHarness(t)
	h.registerAdmin()
	_, codes := h.enableTwoFactor()
	challenges := make([]string, 4)
	for i := range challenges {
		first := h.do("POST", "/api/v1/auth/login", map[string]any{"account": "admin", "password": h.password}, "")
		challenges[i] = path(t, first.object(t), "challenge").(string)
	}
	var next atomic.Int32
	got := h.parallel(len(challenges), func() response {
		return h.do("POST", "/api/v1/auth/login/2fa", map[string]any{
			"challenge": challenges[next.Add(1)-1], "code": codes[0],
		}, "")
	})
	if got["ok"] != 1 {
		t.Fatalf("one recovery code signed in %d times: %v", got["ok"], got)
	}
}

// A second factor that cannot be read must never count as switched off.
func TestTwoFactorReadFailureFailsClosed(t *testing.T) {
	h := newHarness(t)
	h.registerAdmin()
	h.enableTwoFactor()
	if _, err := h.db.W.Exec(`ALTER TABLE user_two_factor RENAME TO user_two_factor_gone`); err != nil {
		t.Fatal(err)
	}
	login := h.do("POST", "/api/v1/auth/login", map[string]any{"account": "admin", "password": h.password}, "")
	if login.status != http.StatusInternalServerError {
		t.Fatalf("login: %d %s", login.status, login.body)
	}
	if err := h.service.Confirm(h.context(), h.adminID, h.password, "", false); err == nil {
		t.Fatal("a step that needs the code went through")
	}
}

func TestSignOutEverywhereDisableAndDelete(t *testing.T) {
	h := newHarness(t)
	h.registerAdmin()
	works := func(token string) bool { return h.do("GET", "/api/v1/auth/me", nil, token).status == http.StatusOK }
	count := func(userID string) (n int) {
		if err := h.db.R.QueryRow(`SELECT COUNT(*) FROM sessions WHERE json_extract(sess, '$.userId') = ?`, userID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	first, id := h.register("member")
	second := h.signIn("member")
	if all := h.do("POST", "/api/v1/auth/logout-all", nil, first); all.status != http.StatusNoContent {
		t.Fatalf("logout-all: %d %s", all.status, all.body)
	}
	if works(first) || works(second) || !works(h.token) {
		t.Fatal("logout-all ends this account's sessions and no others")
	}

	token := h.signIn("member")
	if err := h.service.SetUserDisabled(h.context(), h.adminID, id, true); err != nil {
		t.Fatal(err)
	}
	if works(token) || count(id) != 0 {
		t.Fatal("disabling ends the sessions")
	}
	if err := h.service.SetUserDisabled(h.context(), h.adminID, id, false); err != nil {
		t.Fatal(err)
	}

	h.signIn("member")
	var left []string
	h.service.BeforeDelete = func(_ context.Context, userID string) { left = append(left, userID) }
	if err := h.service.DeleteUser(h.context(), h.adminID, id); err != nil {
		t.Fatal(err)
	}
	if count(id) != 0 || len(left) != 1 || left[0] != id {
		t.Fatalf("deleting: %d sessions, left %v", count(id), left)
	}
}

func TestPasswordRulesApplyToNewPasswordsOnly(t *testing.T) {
	h := newHarness(t)
	for _, weak := range []string{"aaaaaaaaaaaa", "password12345", "administrator99"} {
		response := h.do("POST", "/api/v1/auth/register", map[string]any{
			"username": "admin", "email": "admin@example.test", "password": weak,
		}, "")
		if response.code(t) != "WEAK_PASSWORD" {
			t.Fatalf("%q: %d %s", weak, response.status, response.body)
		}
	}
	h.registerAdmin()
	// A password stored before these rules keeps signing in.
	hash, err := passhash.Hash("aaaaaaaaaaaa", 4)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.db.W.Exec(`UPDATE users SET password_hash = ?`, hash); err != nil {
		t.Fatal(err)
	}
	login := h.do("POST", "/api/v1/auth/login", map[string]any{"username": "admin", "password": "aaaaaaaaaaaa"}, "")
	if login.status != http.StatusOK {
		t.Fatalf("existing password: %d %s", login.status, login.body)
	}
}

func TestRoleIsValidated(t *testing.T) {
	h := newHarness(t)
	h.registerAdmin()
	_, id := h.register("member")
	assertHTTPError(t, h.service.SetUserRole(h.context(), h.adminID, id, "root"), http.StatusBadRequest, "VALIDATION_ERROR")
}
