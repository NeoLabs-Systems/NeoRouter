package auth_test

import (
	"context"
	"net/http"
	"testing"
)

// TestAdminUserOperations ports the admin user endpoints of auth_service.js.
func TestAdminUserOperations(t *testing.T) {
	h := newHarness(t)
	h.registerAdmin()
	_, memberID := h.register("member")
	ctx := context.Background()

	self := h.service.SetUserDisabled(ctx, h.adminID, h.adminID, true)
	assertHTTPError(t, self, http.StatusBadRequest, "CANNOT_DISABLE_SELF")
	selfRole := h.service.SetUserRole(ctx, h.adminID, h.adminID, "user")
	assertHTTPError(t, selfRole, http.StatusBadRequest, "CANNOT_CHANGE_OWN_ROLE")
	selfDelete := h.service.DeleteUser(ctx, h.adminID, h.adminID)
	assertHTTPError(t, selfDelete, http.StatusBadRequest, "CANNOT_DELETE_SELF")
	missing := h.service.SetUserRole(ctx, h.adminID, "does-not-exist", "admin")
	assertHTTPError(t, missing, http.StatusNotFound, "USER_NOT_FOUND")

	memberToken := h.signIn("member")
	if err := h.service.SetUserDisabled(ctx, h.adminID, memberID, true); err != nil {
		t.Fatalf("disable member: %v", err)
	}
	if me := h.do("GET", "/api/v1/auth/me", nil, memberToken); me.status != http.StatusUnauthorized {
		t.Fatalf("disabling an account revokes its sessions: %d %s", me.status, me.body)
	}
	if err := h.service.SetUserDisabled(ctx, h.adminID, memberID, false); err != nil {
		t.Fatalf("re-enable member: %v", err)
	}
	if err := h.service.SetUserRole(ctx, h.adminID, memberID, "admin"); err != nil {
		t.Fatalf("promote member: %v", err)
	}
	if role := h.roleOf(memberID); role != "admin" {
		t.Fatalf("role after promotion: %q", role)
	}
	users, err := h.service.ListUsers(ctx)
	if err != nil {
		t.Fatalf("list users: %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("expected two accounts, got %d", len(users))
	}
	if err := h.service.DeleteUser(ctx, h.adminID, memberID); err != nil {
		t.Fatalf("delete member: %v", err)
	}
	if users, err = h.service.ListUsers(ctx); err != nil || len(users) != 1 {
		t.Fatalf("after deleting the member: %d users, %v", len(users), err)
	}
}

// TestAdminOnlyMiddleware covers the requireAdmin guard.
func TestAdminOnlyMiddleware(t *testing.T) {
	h := newHarness(t)
	h.registerAdmin()
	_, memberID := h.register("plain")
	memberToken := h.signIn("plain")

	reached := false
	guarded := h.service.AdminOnly(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	}))
	deny := doGuarded(t, guarded, memberToken, h.service)
	if deny.status != http.StatusForbidden || deny.code(t) != "ADMIN_REQUIRED" {
		t.Fatalf("a plain user: %d %s", deny.status, deny.body)
	}
	if message := path(t, deny.object(t), "error", "message"); message != "Administrator privileges are required." {
		t.Fatalf("ADMIN_REQUIRED message: %v", message)
	}
	if reached {
		t.Fatal("the guarded handler must not run")
	}
	allow := doGuarded(t, guarded, h.token, h.service)
	if allow.status != http.StatusOK || !reached {
		t.Fatalf("the administrator: %d %s", allow.status, allow.body)
	}
	if memberID == "" {
		t.Fatal("the member account should exist")
	}
}

// TestAuthenticateTokenDigest checks the digest lookup against the service
// directly, including the nrs_ prefix rule.
func TestAuthenticateTokenDigest(t *testing.T) {
	h := newHarness(t)
	h.registerAdmin()
	ctx := context.Background()
	if caller := h.service.AuthenticateToken(ctx, h.token); caller == nil || caller.UserID != h.adminID {
		t.Fatalf("authenticate: %+v", caller)
	}
	for _, token := range []string{"", "nrs_", "not-a-token", "nrs_" + h.token, h.token + "x"} {
		if caller := h.service.AuthenticateToken(ctx, token); caller != nil {
			t.Fatalf("token %q must not authenticate", token)
		}
	}
}
