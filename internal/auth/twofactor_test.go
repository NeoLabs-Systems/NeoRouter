package auth_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/totp"
)

// totpCode is the current authenticator code for a secret, the equivalent of
// otplib's generateSync in the Node tests.
func totpCode(t *testing.T, secret string) string {
	t.Helper()
	code, err := totp.Code(secret, time.Now())
	if err != nil {
		t.Fatalf("generate code: %v", err)
	}
	return code
}

// enableTwoFactor walks the enrolment of the harness administrator and returns
// the secret and the recovery codes it handed out.
func (h *harness) enableTwoFactor() (string, []string) {
	h.t.Helper()
	setup := h.do("POST", "/api/v1/auth/2fa/setup", map[string]any{"currentPassword": h.password}, h.token)
	if setup.status != http.StatusOK {
		h.t.Fatalf("2fa setup: %d %s", setup.status, setup.body)
	}
	secret := path(h.t, setup.object(h.t), "manualKey").(string)
	enabled := h.do("POST", "/api/v1/auth/2fa/enable", map[string]any{"code": totpCode(h.t, secret)}, h.token)
	if enabled.status != http.StatusOK {
		h.t.Fatalf("2fa enable: %d %s", enabled.status, enabled.body)
	}
	return secret, anyStrings(h.t, path(h.t, enabled.object(h.t), "recoveryCodes"))
}

// enableTwoFactorSecret is enableTwoFactor without the recovery codes.
func (h *harness) enableTwoFactorSecret() string {
	h.t.Helper()
	secret, _ := h.enableTwoFactor()
	return secret
}

// TestTwoFactorSetupAndEnable ports the enrolment half of
// test/api/two_factor.test.js.
func TestTwoFactorSetupAndEnable(t *testing.T) {
	h := newHarness(t)
	h.registerAdmin()

	denied := h.do("POST", "/api/v1/auth/2fa/setup", map[string]any{"currentPassword": "SI_SECRET_PASSWORD_GTDT9RJV"}, h.token)
	if denied.status != http.StatusUnauthorized || denied.code(t) != "INVALID_PASSWORD" {
		t.Fatalf("setup without the password: %d %s", denied.status, denied.body)
	}

	setup := h.do("POST", "/api/v1/auth/2fa/setup", map[string]any{"currentPassword": h.password}, h.token)
	if setup.status != http.StatusOK {
		t.Fatalf("setup: %d %s", setup.status, setup.body)
	}
	equalStrings(t, "setup keys", orderedKeys(t, setup.body), []string{"otpauthUrl", "manualKey", "status"})
	url := path(t, setup.object(t), "otpauthUrl").(string)
	if !strings.HasPrefix(url, "otpauth://totp/NeoRouter") {
		t.Fatalf("otpauth url: %q", url)
	}
	if !strings.Contains(url, "issuer=NeoRouter") || !strings.Contains(url, "secret=") {
		t.Fatalf("otpauth url should name the issuer and carry the secret: %q", url)
	}
	secret := path(t, setup.object(t), "manualKey").(string)
	if pending := path(t, setup.object(t), "status", "pending"); pending != true {
		t.Fatalf("the setup is pending until a code confirms it: %s", setup.body)
	}

	wrong := h.do("POST", "/api/v1/auth/2fa/enable", map[string]any{"code": "000000"}, h.token)
	if wrong.status != http.StatusUnauthorized || wrong.code(t) != "INVALID_TWO_FACTOR" {
		t.Fatalf("a wrong code must not enable the factor: %d %s", wrong.status, wrong.body)
	}
	enabled := h.do("POST", "/api/v1/auth/2fa/enable", map[string]any{"code": totpCode(t, secret)}, h.token)
	if enabled.status != http.StatusOK {
		t.Fatalf("enable: %d %s", enabled.status, enabled.body)
	}
	equalStrings(t, "enable keys", orderedKeys(t, enabled.body), []string{"recoveryCodes", "status"})
	codes := anyStrings(t, path(t, enabled.object(t), "recoveryCodes"))
	if len(codes) != 10 {
		t.Fatalf("expected ten recovery codes, got %d", len(codes))
	}
	for _, code := range codes {
		if len(code) != 11 || code[5] != '-' {
			t.Fatalf("recovery code shape: %q", code)
		}
	}
	if value := path(t, enabled.object(t), "status", "enabled"); value != true {
		t.Fatalf("status after enabling: %s", enabled.body)
	}
	if remaining := path(t, enabled.object(t), "status", "recoveryCodesRemaining"); remaining != float64(10) {
		t.Fatalf("recovery codes remaining: %v", remaining)
	}
}

// TestTwoFactorLogin ports the second step of sign-in: the password only earns a
// challenge, and the challenge works once.
func TestTwoFactorLogin(t *testing.T) {
	h := newHarness(t)
	h.registerAdmin()
	secret, _ := h.enableTwoFactor()

	first := h.do("POST", "/api/v1/auth/login", map[string]any{"account": "admin", "password": h.password}, "")
	if first.status != http.StatusOK {
		t.Fatalf("login: %d %s", first.status, first.body)
	}
	if required := path(t, first.object(t), "twoFactorRequired"); required != true {
		t.Fatalf("the second step must be required: %s", first.body)
	}
	if _, ok := first.object(t)["session"]; ok {
		t.Fatalf("no session before the code: %s", first.body)
	}
	challenge := path(t, first.object(t), "challenge").(string)

	bad := h.do("POST", "/api/v1/auth/login/2fa", map[string]any{"challenge": challenge, "code": "123456"}, "")
	if bad.status != http.StatusUnauthorized || bad.code(t) != "INVALID_TWO_FACTOR" {
		t.Fatalf("a wrong code: %d %s", bad.status, bad.body)
	}
	good := h.do("POST", "/api/v1/auth/login/2fa", map[string]any{"challenge": challenge, "code": totpCode(t, secret)}, "")
	if good.status != http.StatusOK {
		t.Fatalf("the code: %d %s", good.status, good.body)
	}
	if token := path(t, good.object(t), "session", "token").(string); !strings.HasPrefix(token, "nrs_") {
		t.Fatalf("session token: %q", token)
	}
	reused := h.do("POST", "/api/v1/auth/login/2fa", map[string]any{"challenge": challenge, "code": totpCode(t, secret)}, "")
	if reused.status != http.StatusUnauthorized || reused.code(t) != "TWO_FACTOR_EXPIRED" {
		t.Fatalf("a challenge works only once: %d %s", reused.status, reused.body)
	}

	unknown := h.do("POST", "/api/v1/auth/login/2fa", map[string]any{"challenge": "not-a-real-challenge", "code": "123456"}, "")
	if unknown.status != http.StatusUnauthorized || unknown.code(t) != "TWO_FACTOR_EXPIRED" {
		t.Fatalf("an unknown challenge: %d %s", unknown.status, unknown.body)
	}
}

// TestEnabledFactorCannotBeSwapped pins the rule that replacing an enabled
// factor needs the factor itself, not just the password.
func TestEnabledFactorCannotBeSwapped(t *testing.T) {
	h := newHarness(t)
	h.registerAdmin()
	secret, _ := h.enableTwoFactor()

	swap := h.do("POST", "/api/v1/auth/2fa/setup", map[string]any{"currentPassword": h.password}, h.token)
	if swap.status != http.StatusConflict || swap.code(t) != "TWO_FACTOR_ENABLED" {
		t.Fatalf("swap: %d %s", swap.status, swap.body)
	}
	if message := path(t, swap.object(t), "error", "message"); message != "Two-factor is already on. Turn it off first to set up another app." {
		t.Fatalf("TWO_FACTOR_ENABLED message: %v", message)
	}

	first := h.do("POST", "/api/v1/auth/login", map[string]any{"account": "admin", "password": h.password}, "")
	still := h.do("POST", "/api/v1/auth/login/2fa", map[string]any{
		"challenge": path(t, first.object(t), "challenge").(string),
		"code":      totpCode(t, secret),
	}, "")
	if still.status != http.StatusOK {
		t.Fatalf("the original authenticator still works: %d %s", still.status, still.body)
	}
}

// TestRecoveryCodeWorksOnce ports the recovery-code rule: a code signs in once,
// whatever case it was typed in, and the count goes down.
func TestRecoveryCodeWorksOnce(t *testing.T) {
	h := newHarness(t)
	h.registerAdmin()
	_, codes := h.enableTwoFactor()

	first := h.do("POST", "/api/v1/auth/login", map[string]any{"account": "admin", "password": h.password}, "")
	ok := h.do("POST", "/api/v1/auth/login/2fa", map[string]any{
		"challenge": path(t, first.object(t), "challenge").(string),
		"code":      strings.ToLower(codes[0]),
	}, "")
	if ok.status != http.StatusOK {
		t.Fatalf("a recovery code signs in: %d %s", ok.status, ok.body)
	}

	again := h.do("POST", "/api/v1/auth/login", map[string]any{"account": "admin", "password": h.password}, "")
	reuse := h.do("POST", "/api/v1/auth/login/2fa", map[string]any{
		"challenge": path(t, again.object(t), "challenge").(string),
		"code":      codes[0],
	}, "")
	if reuse.status != http.StatusUnauthorized || reuse.code(t) != "INVALID_TWO_FACTOR" {
		t.Fatalf("a recovery code works once: %d %s", reuse.status, reuse.body)
	}

	status := h.do("GET", "/api/v1/auth/2fa", nil, h.token)
	if remaining := path(t, status.object(t), "recoveryCodesRemaining"); remaining != float64(9) {
		t.Fatalf("recovery codes remaining: %s", status.body)
	}
}

// TestWrongCodesLockTheSecondStep ports the lockout after repeated wrong codes.
func TestWrongCodesLockTheSecondStep(t *testing.T) {
	h := newHarness(t)
	h.registerAdmin()
	h.enableTwoFactor()

	first := h.do("POST", "/api/v1/auth/login", map[string]any{"account": "admin", "password": h.password}, "")
	challenge := path(t, first.object(t), "challenge").(string)
	var last response
	for attempt := 0; attempt < 6; attempt++ {
		last = h.do("POST", "/api/v1/auth/login/2fa", map[string]any{"challenge": challenge, "code": "999999"}, "")
	}
	if last.status != http.StatusTooManyRequests || last.code(t) != "TWO_FACTOR_LOCKED" {
		t.Fatalf("the second step should be locked: %d %s", last.status, last.body)
	}
	if message := path(t, last.object(t), "error", "message"); message != "Too many wrong codes. Try again in a few minutes." {
		t.Fatalf("TWO_FACTOR_LOCKED message: %v", message)
	}
}

// TestDisableNeedsPasswordAndCode ports turning the factor off again.
func TestDisableNeedsPasswordAndCode(t *testing.T) {
	h := newHarness(t)
	h.registerAdmin()
	secret, _ := h.enableTwoFactor()

	denied := h.do("POST", "/api/v1/auth/2fa/disable", map[string]any{
		"currentPassword": h.password, "code": "000000",
	}, h.token)
	if denied.status != http.StatusUnauthorized || denied.code(t) != "INVALID_TWO_FACTOR" {
		t.Fatalf("a wrong code must not disable the factor: %d %s", denied.status, denied.body)
	}
	disabled := h.do("POST", "/api/v1/auth/2fa/disable", map[string]any{
		"currentPassword": h.password, "code": totpCode(t, secret),
	}, h.token)
	if disabled.status != http.StatusOK {
		t.Fatalf("disable: %d %s", disabled.status, disabled.body)
	}
	if enabled := path(t, disabled.object(t), "status", "enabled"); enabled != false {
		t.Fatalf("status after disabling: %s", disabled.body)
	}
	login := h.do("POST", "/api/v1/auth/login", map[string]any{"account": "admin", "password": h.password}, "")
	if login.status != http.StatusOK {
		t.Fatalf("sign-in without a code again: %d %s", login.status, login.body)
	}
	if token := path(t, login.object(t), "session", "token"); token == nil {
		t.Fatalf("the password alone should sign in now: %s", login.body)
	}
}

// TestRegenerateRecoveryCodes ports POST /auth/2fa/recovery-codes.
func TestRegenerateRecoveryCodes(t *testing.T) {
	h := newHarness(t)
	h.registerAdmin()
	secret, codes := h.enableTwoFactor()

	regenerated := h.do("POST", "/api/v1/auth/2fa/recovery-codes", map[string]any{
		"currentPassword": h.password, "code": totpCode(t, secret),
	}, h.token)
	if regenerated.status != http.StatusOK {
		t.Fatalf("regenerate: %d %s", regenerated.status, regenerated.body)
	}
	fresh := anyStrings(t, path(t, regenerated.object(t), "recoveryCodes"))
	if len(fresh) != 10 {
		t.Fatalf("expected ten fresh codes, got %d", len(fresh))
	}
	if fresh[0] == codes[0] {
		t.Fatalf("the recovery codes should be replaced, not kept")
	}

	h2 := newHarness(t)
	h2.registerAdmin()
	off := h2.do("POST", "/api/v1/auth/2fa/recovery-codes", map[string]any{
		"currentPassword": h2.password, "code": "123456",
	}, h2.token)
	if off.status != http.StatusBadRequest || off.code(t) != "TWO_FACTOR_DISABLED" {
		t.Fatalf("without a factor: %d %s", off.status, off.body)
	}
	if message := path(t, off.object(t), "error", "message"); message != "Two-factor authentication is not enabled." {
		t.Fatalf("TWO_FACTOR_DISABLED message: %v", message)
	}
}

// TestConfirmRequiresFactor covers the rule the account-data endpoints rely on:
// a step that matters can demand a factor that is switched on.
func TestConfirmRequiresFactor(t *testing.T) {
	h := newHarness(t)
	h.registerAdmin()
	err := h.service.Confirm(h.context(), h.adminID, h.password, "", true)
	assertHTTPError(t, err, http.StatusForbidden, "TWO_FACTOR_REQUIRED")
	if err := h.service.Confirm(h.context(), h.adminID, h.password, "", false); err != nil {
		t.Fatalf("without requireFactor a password is enough: %v", err)
	}
	secret := h.enableTwoFactorSecret()
	if err := h.service.Confirm(h.context(), h.adminID, h.password, totpCode(t, secret), false); err != nil {
		t.Fatalf("with 2FA on the code is required: %v", err)
	}
	err = h.service.Confirm(h.context(), h.adminID, h.password, "000000", false)
	assertHTTPError(t, err, http.StatusUnauthorized, "INVALID_TWO_FACTOR")
}
