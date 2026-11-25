package auth_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/NeoLabs-Systems/neorouter/internal/auth"
)

// TestRegistrationStatusAndGate ports the registration gate of
// auth_service.js: the first account is the bootstrap administrator, and after
// that registration needs the administrator to open it.
func TestRegistrationStatusAndGate(t *testing.T) {
	h := newHarness(t)

	status := h.do("GET", "/api/v1/auth/registration", nil, "")
	if status.status != http.StatusOK {
		t.Fatalf("registration status: %d %s", status.status, status.body)
	}
	equalStrings(t, "status keys", orderedKeys(t, status.body), []string{"open", "bootstrap"})
	if path(t, status.object(t), "open") != true || path(t, status.object(t), "bootstrap") != true {
		t.Fatalf("a fresh instance should be open for the bootstrap account: %s", status.body)
	}

	h.registerAdmin()
	if role := h.roleOf(h.adminID); role != "admin" {
		t.Fatalf("the first account is the administrator, got %q", role)
	}

	closed := h.do("GET", "/api/v1/auth/registration", nil, "")
	if path(t, closed.object(t), "open") != false || path(t, closed.object(t), "bootstrap") != false {
		t.Fatalf("registration should be closed now: %s", closed.body)
	}
	second := h.do("POST", "/api/v1/auth/register", map[string]any{
		"username": "member", "email": "member@example.test", "password": h.password,
	}, "")
	if second.status != http.StatusForbidden || second.code(t) != "REGISTRATION_DISABLED" {
		t.Fatalf("second registration: %d %s", second.status, second.body)
	}
	if message := path(t, second.object(t), "error", "message"); message != "Registration is disabled. Ask an administrator to open it." {
		t.Fatalf("REGISTRATION_DISABLED message: %v", message)
	}

	h.gate.set(true)
	opened := h.do("POST", "/api/v1/auth/register", map[string]any{
		"username": "member", "email": "member@example.test", "password": h.password,
	}, "")
	if opened.status != http.StatusCreated {
		t.Fatalf("registration after opening the gate: %d %s", opened.status, opened.body)
	}
	if role := path(t, opened.object(t), "user", "role"); role != "user" {
		t.Fatalf("later accounts are ordinary users, got %v", role)
	}
}

// TestRegisterResponseJSON pins the response shape and key order of
// auth_service.js register(): {user, session} with publicUser()'s keys.
func TestRegisterResponseJSON(t *testing.T) {
	h := newHarness(t)
	response := h.do("POST", "/api/v1/auth/register", map[string]any{
		"username": "  boot  ", "email": "Boot@Example.Test", "password": h.password,
	}, "")
	if response.status != http.StatusCreated {
		t.Fatalf("register: %d %s", response.status, response.body)
	}
	equalStrings(t, "top-level keys", orderedKeys(t, response.body), []string{"user", "session"})
	equalStrings(t, "user keys", orderedKeys(t, nested(t, response.body, "user")),
		[]string{"id", "username", "email", "displayName", "role", "disabled", "createdAt", "lastLoginAt"})
	equalStrings(t, "session keys", orderedKeys(t, nested(t, response.body, "session")),
		[]string{"id", "token", "expiresAt"})

	user := path(t, response.object(t), "user").(map[string]any)
	if user["username"] != "boot" {
		t.Fatalf("the username is trimmed: %v", user["username"])
	}
	if user["email"] != "boot@example.test" {
		t.Fatalf("the email is trimmed and lower-cased: %v", user["email"])
	}
	if user["displayName"] != "boot" {
		t.Fatalf("displayName falls back to the username: %v", user["displayName"])
	}
	if user["lastLoginAt"] != nil {
		t.Fatalf("a fresh account has never signed in: %v", user["lastLoginAt"])
	}
	if user["disabled"] != false {
		t.Fatalf("a fresh account is enabled: %v", user["disabled"])
	}
	token := path(t, response.object(t), "session", "token").(string)
	if !strings.HasPrefix(token, "nrs_") || len(token) != len("nrs_")+43 {
		t.Fatalf("session token: %q", token)
	}
	if contentType := response.header.Get("Content-Type"); contentType != "application/json; charset=utf-8" {
		t.Fatalf("content type: %q", contentType)
	}
}

// TestRegisterValidatesItsInput covers the zod body rules of POST /register.
func TestRegisterValidatesItsInput(t *testing.T) {
	h := newHarness(t)
	cases := []struct {
		name    string
		body    map[string]any
		message string
	}{
		{"empty", map[string]any{}, "username: Required"},
		{"short username", map[string]any{"username": "ab", "email": "a@b.test", "password": h.password},
			"username: String must contain at least 3 character(s)"},
		{"bad email", map[string]any{"username": "abc", "email": "nope", "password": h.password},
			"email: Invalid email"},
		{"short password", map[string]any{"username": "abc", "email": "a@b.test", "password": "short"},
			"password: String must contain at least 12 character(s)"},
		{"wrong type", map[string]any{"username": 5, "email": "a@b.test", "password": h.password},
			"username: Expected string, received number"},
		{"long display name", map[string]any{"username": "abc", "email": "a@b.test", "password": h.password,
			"displayName": strings.Repeat("y", 81)}, "displayName: String must contain at most 80 character(s)"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			response := h.do("POST", "/api/v1/auth/register", testCase.body, "")
			if response.status != http.StatusBadRequest || response.code(t) != "VALIDATION_ERROR" {
				t.Fatalf("status %d body %s", response.status, response.body)
			}
			if message := path(t, response.object(t), "error", "message"); message != testCase.message {
				t.Fatalf("message %q, want %q", message, testCase.message)
			}
			details := path(t, response.object(t), "error", "details").(map[string]any)
			if _, ok := details["formErrors"]; !ok {
				t.Fatalf("details should flatten like zod: %v", details)
			}
		})
	}
}

// TestRegisterServiceGuards covers the rules the JS service keeps behind the
// route validation, called directly the way the settings domain calls them.
func TestRegisterServiceGuards(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	cases := []struct {
		name   string
		input  auth.RegisterInput
		status int
		code   string
	}{
		{"short username", auth.RegisterInput{Username: "ab", Email: "a@b.test", Password: h.password},
			http.StatusBadRequest, "INVALID_USERNAME"},
		{"no email", auth.RegisterInput{Username: "abc", Email: "   ", Password: h.password},
			http.StatusBadRequest, "EMAIL_REQUIRED"},
		{"weak password", auth.RegisterInput{Username: "abc", Email: "a@b.test", Password: "short"},
			http.StatusBadRequest, "WEAK_PASSWORD"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := h.service.Register(ctx, testCase.input, auth.RequestContext{})
			assertHTTPError(t, err, testCase.status, testCase.code)
		})
	}
}

// TestBootstrapRegistrationIsRaceSafe ports the transactional registration gate:
// two first sign-ups cannot both become the administrator.
func TestBootstrapRegistrationIsRaceSafe(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	type outcome struct {
		result auth.LoginResult
		err    error
	}
	results := make(chan outcome, 2)
	var wait sync.WaitGroup
	for _, name := range []string{"first", "second"} {
		wait.Add(1)
		go func(username string) {
			defer wait.Done()
			result, err := h.service.Register(ctx, auth.RegisterInput{
				Username: username,
				Email:    username + "@example.test",
				Password: testPassword,
			}, auth.RequestContext{})
			results <- outcome{result: result, err: err}
		}(name)
	}
	wait.Wait()
	close(results)

	created, admins := 0, 0
	for item := range results {
		if item.err != nil {
			assertHTTPError(t, item.err, http.StatusForbidden, "REGISTRATION_DISABLED")
			continue
		}
		created++
		if item.result.User.Role == "admin" {
			admins++
		}
	}
	if created != 1 || admins != 1 {
		t.Fatalf("expected one bootstrap administrator, got %d accounts and %d admins", created, admins)
	}
	var stored int
	if err := h.db.R.QueryRow(`SELECT COUNT(*) FROM users WHERE role = 'admin'`).Scan(&stored); err != nil {
		t.Fatalf("count admins: %v", err)
	}
	if stored != 1 {
		t.Fatalf("the database has %d administrators", stored)
	}
}

// TestLoginAndSessionLifecycle ports the sign-in flow: a username or email
// signs in, /me shows the caller, and logout revokes the session.
func TestLoginAndSessionLifecycle(t *testing.T) {
	h := newHarness(t)
	h.registerAdmin()

	for _, account := range []string{"admin", "admin@example.test"} {
		login := h.do("POST", "/api/v1/auth/login", map[string]any{"account": account, "password": h.password}, "")
		if login.status != http.StatusOK {
			t.Fatalf("login as %s: %d %s", account, login.status, login.body)
		}
		token := path(t, login.object(t), "session", "token").(string)
		me := h.doAs("GET", "/api/v1/auth/me", nil, token, "")
		if me.status != http.StatusOK {
			t.Fatalf("me: %d %s", me.status, me.body)
		}
		equalStrings(t, "me keys", orderedKeys(t, me.body), []string{"user", "twoFactor"})
		equalStrings(t, "twoFactor keys", orderedKeys(t, nested(t, me.body, "twoFactor")),
			[]string{"enabled", "pending", "enabledAt", "recoveryCodesRemaining"})
		if path(t, me.object(t), "user", "username") != "admin" {
			t.Fatalf("me: %s", me.body)
		}
		if path(t, me.object(t), "user", "lastLoginAt") == nil {
			t.Fatalf("the sign-in timestamp is stamped before the session is issued: %s", me.body)
		}
	}

	wrong := h.do("POST", "/api/v1/auth/login", map[string]any{"account": "admin", "password": "wrong-password"}, "")
	if wrong.status != http.StatusUnauthorized || wrong.code(t) != "INVALID_CREDENTIALS" {
		t.Fatalf("wrong password: %d %s", wrong.status, wrong.body)
	}
	unknown := h.do("POST", "/api/v1/auth/login", map[string]any{"account": "nobody", "password": h.password}, "")
	if unknown.status != http.StatusUnauthorized || unknown.code(t) != "INVALID_CREDENTIALS" {
		t.Fatalf("unknown account: %d %s", unknown.status, unknown.body)
	}

	logout := h.do("POST", "/api/v1/auth/logout", nil, h.token)
	if logout.status != http.StatusNoContent {
		t.Fatalf("logout: %d %s", logout.status, logout.body)
	}
	after := h.do("GET", "/api/v1/auth/me", nil, h.token)
	if after.status != http.StatusUnauthorized || after.code(t) != "AUTHENTICATION_REQUIRED" {
		t.Fatalf("a revoked session must not work: %d %s", after.status, after.body)
	}
	if message := path(t, after.object(t), "error", "message"); message != "A valid session is required." {
		t.Fatalf("AUTHENTICATION_REQUIRED message: %v", message)
	}
}

// TestSessionTokensAreStoredHashed pins the nrs_ token contract: only a SHA-256
// digest is stored, and expired or revoked sessions stop working.
func TestSessionTokensAreStoredHashed(t *testing.T) {
	h := newHarness(t)
	h.registerAdmin()

	var stored, expires string
	if err := h.db.R.QueryRow(`SELECT token_hash, expires_at FROM user_sessions`).Scan(&stored, &expires); err != nil {
		t.Fatalf("read session: %v", err)
	}
	sum := sha256.Sum256([]byte(h.token))
	if stored != hex.EncodeToString(sum[:]) {
		t.Fatalf("stored digest %q, want %q", stored, hex.EncodeToString(sum[:]))
	}
	if !strings.HasSuffix(expires, "Z") || len(expires) != len("2026-01-01T00:00:00.000Z") {
		t.Fatalf("expires_at is ISO milliseconds: %q", expires)
	}
	moment, err := time.Parse(time.RFC3339, expires)
	if err != nil {
		t.Fatalf("parse expires_at: %v", err)
	}
	if moment.Before(time.Now().Add(29 * 24 * time.Hour)) {
		t.Fatalf("the session should live for the configured TTL: %s", expires)
	}

	if _, err := h.db.W.Exec(`UPDATE user_sessions SET expires_at = '2000-01-01T00:00:00.000Z'`); err != nil {
		t.Fatalf("expire session: %v", err)
	}
	if response := h.do("GET", "/api/v1/auth/me", nil, h.token); response.status != http.StatusUnauthorized {
		t.Fatalf("an expired session must not work: %d %s", response.status, response.body)
	}
	var lastUsed sql.NullString
	if err := h.db.R.QueryRow(`SELECT last_used_at FROM user_sessions`).Scan(&lastUsed); err != nil {
		t.Fatalf("read last_used_at: %v", err)
	}
	if _, err := h.db.W.Exec(`UPDATE user_sessions SET expires_at = '2100-01-01T00:00:00.000Z', revoked_at = '2026-01-01T00:00:00.000Z'`); err != nil {
		t.Fatalf("revoke session: %v", err)
	}
	if response := h.do("GET", "/api/v1/auth/me", nil, h.token); response.status != http.StatusUnauthorized {
		t.Fatalf("a revoked session must not work: %d %s", response.status, response.body)
	}
}

// TestDisabledAccountCannotSignIn ports the disabled_at rule.
func TestDisabledAccountCannotSignIn(t *testing.T) {
	h := newHarness(t)
	h.registerAdmin()
	if _, err := h.db.W.Exec(`UPDATE users SET disabled_at = '2026-01-01T00:00:00.000Z' WHERE id = ?`, h.adminID); err != nil {
		t.Fatalf("disable account: %v", err)
	}
	login := h.do("POST", "/api/v1/auth/login", map[string]any{"account": "admin", "password": h.password}, "")
	if login.status != http.StatusForbidden || login.code(t) != "ACCOUNT_DISABLED" {
		t.Fatalf("disabled account: %d %s", login.status, login.body)
	}
	if message := path(t, login.object(t), "error", "message"); message != "This account is disabled." {
		t.Fatalf("ACCOUNT_DISABLED message: %v", message)
	}
	if me := h.do("GET", "/api/v1/auth/me", nil, h.token); me.status != http.StatusUnauthorized {
		t.Fatalf("a disabled account's session must not work: %d %s", me.status, me.body)
	}
}

// TestChangePassword ports PUT /auth/password: the current password is asked
// again, and a change signs the account out everywhere.
func TestChangePassword(t *testing.T) {
	h := newHarness(t)
	h.registerAdmin()

	wrong := h.do("PUT", "/api/v1/auth/password", map[string]any{
		"currentPassword": "not-the-password", "newPassword": "SI_SECRET_PASSWORD_GY0T7QQV",
	}, h.token)
	if wrong.status != http.StatusUnauthorized || wrong.code(t) != "INVALID_PASSWORD" {
		t.Fatalf("wrong password: %d %s", wrong.status, wrong.body)
	}
	short := h.do("PUT", "/api/v1/auth/password", map[string]any{
		"currentPassword": h.password, "newPassword": "short",
	}, h.token)
	if short.status != http.StatusBadRequest || short.code(t) != "VALIDATION_ERROR" {
		t.Fatalf("short password: %d %s", short.status, short.body)
	}
	changed := h.do("PUT", "/api/v1/auth/password", map[string]any{
		"currentPassword": h.password, "newPassword": "SI_SECRET_PASSWORD_GY0T7QQV",
	}, h.token)
	if changed.status != http.StatusNoContent {
		t.Fatalf("change password: %d %s", changed.status, changed.body)
	}
	if me := h.do("GET", "/api/v1/auth/me", nil, h.token); me.status != http.StatusUnauthorized {
		t.Fatalf("a password change signs every session out: %d %s", me.status, me.body)
	}
	login := h.do("POST", "/api/v1/auth/login", map[string]any{
		"account": "admin", "password": "SI_SECRET_PASSWORD_GY0T7QQV",
	}, "")
	if login.status != http.StatusOK {
		t.Fatalf("login with the new password: %d %s", login.status, login.body)
	}
}

// TestUpdateProfile ports PATCH /auth/profile.
func TestUpdateProfile(t *testing.T) {
	h := newHarness(t)
	h.registerAdmin()
	_, otherID := h.register("other")

	renamed := h.do("PATCH", "/api/v1/auth/profile", map[string]any{
		"displayName": "  Ada Lovelace  ", "email": "ADA@Example.Test",
	}, h.token)
	if renamed.status != http.StatusOK {
		t.Fatalf("update profile: %d %s", renamed.status, renamed.body)
	}
	equalStrings(t, "profile keys", orderedKeys(t, renamed.body), []string{"user"})
	if name := path(t, renamed.object(t), "user", "displayName"); name != "Ada Lovelace" {
		t.Fatalf("display name: %v", name)
	}
	if email := path(t, renamed.object(t), "user", "email"); email != "ada@example.test" {
		t.Fatalf("email: %v", email)
	}

	taken := h.do("PATCH", "/api/v1/auth/profile", map[string]any{"email": "other@example.test"}, h.token)
	if taken.status != http.StatusConflict || taken.code(t) != "EMAIL_TAKEN" {
		t.Fatalf("email taken: %d %s", taken.status, taken.body)
	}
	if message := path(t, taken.object(t), "error", "message"); message != "This email is already in use." {
		t.Fatalf("EMAIL_TAKEN message: %v", message)
	}
	if otherID == "" {
		t.Fatal("the second account should exist")
	}
	blank := h.do("PATCH", "/api/v1/auth/profile", map[string]any{"displayName": "   "}, h.token)
	if name := path(t, blank.object(t), "user", "displayName"); name != "admin" {
		t.Fatalf("an empty display name falls back to the username: %v", name)
	}
}
