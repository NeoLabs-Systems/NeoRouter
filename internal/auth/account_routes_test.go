package auth_test

import (
	"context"
	"net/http"
	"sync"
	"testing"

	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
)

// fakeAccountData stands in for the settings domain's export/delete surface.
type fakeAccountData struct {
	mu    sync.Mutex
	calls []string
	doc   any
	err   error
}

func (f *fakeAccountData) record(kind, userID, password, code string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, kind+":"+userID+":"+password+":"+code)
}

func (f *fakeAccountData) ExportData(_ context.Context, userID, password, code string) (any, error) {
	f.record("export", userID, password, code)
	return f.doc, f.err
}

func (f *fakeAccountData) ExportKeys(_ context.Context, userID, password, code string) (any, error) {
	f.record("export-keys", userID, password, code)
	return f.doc, f.err
}

func (f *fakeAccountData) DeleteAccount(_ context.Context, userID, password, code string) error {
	f.record("delete", userID, password, code)
	return f.err
}

func TestAccountExportReturnsTheDocumentWithNoStore(t *testing.T) {
	h := newHarness(t)
	h.registerAdmin()
	fake := &fakeAccountData{doc: map[string]any{"format": 1, "profile": map[string]any{"id": h.adminID}}}
	h.service.AccountData = fake

	response := h.do("POST", "/api/v1/auth/account/export", map[string]any{"currentPassword": h.password}, h.token)
	if response.status != http.StatusOK {
		t.Fatalf("status = %d body %s", response.status, response.body)
	}
	if got := response.header.Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
	if got := path(t, response.object(t), "format"); got != float64(1) {
		t.Fatalf("document = %s", response.body)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.calls) != 1 || fake.calls[0] != "export:"+h.adminID+":"+h.password+":" {
		t.Fatalf("calls = %v", fake.calls)
	}
}

func TestAccountExportKeysPassesTheTwoFactorCode(t *testing.T) {
	h := newHarness(t)
	h.registerAdmin()
	fake := &fakeAccountData{doc: map[string]any{"keys": []any{}}}
	h.service.AccountData = fake

	response := h.do("POST", "/api/v1/auth/account/export-keys", map[string]any{"currentPassword": h.password, "code": "123456"}, h.token)
	if response.status != http.StatusOK {
		t.Fatalf("status = %d body %s", response.status, response.body)
	}
	if got := response.header.Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q", got)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.calls) != 1 || fake.calls[0] != "export-keys:"+h.adminID+":"+h.password+":123456" {
		t.Fatalf("calls = %v", fake.calls)
	}
}

func TestAccountDeleteAnswers204(t *testing.T) {
	h := newHarness(t)
	h.registerAdmin()
	fake := &fakeAccountData{}
	h.service.AccountData = fake

	response := h.do("POST", "/api/v1/auth/account/delete", map[string]any{"currentPassword": h.password}, h.token)
	if response.status != http.StatusNoContent || len(response.body) != 0 {
		t.Fatalf("status = %d body %q", response.status, response.body)
	}
}

func TestAccountRoutesValidateTheConfirmation(t *testing.T) {
	h := newHarness(t)
	h.registerAdmin()
	h.service.AccountData = &fakeAccountData{doc: map[string]any{}}

	missing := h.do("POST", "/api/v1/auth/account/export", map[string]any{}, h.token)
	if missing.status != http.StatusBadRequest || missing.code(t) != "VALIDATION_ERROR" {
		t.Fatalf("missing password: %d %s", missing.status, missing.body)
	}
	shortCode := h.do("POST", "/api/v1/auth/account/export", map[string]any{"currentPassword": h.password, "code": "123"}, h.token)
	if shortCode.status != http.StatusBadRequest {
		t.Fatalf("short code: %d %s", shortCode.status, shortCode.body)
	}
	if got := path(t, shortCode.object(t), "error", "message"); got != "code: String must contain at least 6 character(s)" {
		t.Fatalf("message = %v", got)
	}
}

func TestAccountRoutesRequireASession(t *testing.T) {
	h := newHarness(t)
	h.registerAdmin()
	h.service.AccountData = &fakeAccountData{}
	response := h.do("POST", "/api/v1/auth/account/export", map[string]any{"currentPassword": h.password}, "")
	if response.status != http.StatusUnauthorized || response.code(t) != "AUTHENTICATION_REQUIRED" {
		t.Fatalf("status = %d %s", response.status, response.body)
	}
}

func TestAccountRoutesThrottleFivePerMinute(t *testing.T) {
	h := newHarness(t)
	h.registerAdmin()
	h.service.AccountData = &fakeAccountData{doc: map[string]any{}}
	for i := 0; i < 5; i++ {
		response := h.do("POST", "/api/v1/auth/account/export", map[string]any{"currentPassword": h.password}, h.token)
		if response.status != http.StatusOK {
			t.Fatalf("call %d: %d %s", i, response.status, response.body)
		}
	}
	response := h.do("POST", "/api/v1/auth/account/export", map[string]any{"currentPassword": h.password}, h.token)
	if response.status != http.StatusTooManyRequests || response.code(t) != "RATE_LIMITED" {
		t.Fatalf("sixth call: %d %s", response.status, response.body)
	}
	if response.header.Get("Retry-After") == "" {
		t.Fatal("Retry-After missing")
	}
	// The delete route has its own limiter.
	del := h.do("POST", "/api/v1/auth/account/delete", map[string]any{"currentPassword": h.password}, h.token)
	if del.status != http.StatusNoContent {
		t.Fatalf("delete after export throttle: %d %s", del.status, del.body)
	}
}

func TestAccountRoutesWithoutWiringAnswerAMasked500(t *testing.T) {
	h := newHarness(t)
	h.registerAdmin()
	response := h.do("POST", "/api/v1/auth/account/export", map[string]any{"currentPassword": h.password}, h.token)
	if response.status != http.StatusInternalServerError || response.code(t) != "INTERNAL_ERROR" {
		t.Fatalf("status = %d %s", response.status, response.body)
	}
	if got := path(t, response.object(t), "error", "message"); got != "An internal error occurred." {
		t.Fatalf("message = %v", got)
	}
}

func TestAccountRoutesKeepDomainErrors(t *testing.T) {
	h := newHarness(t)
	h.registerAdmin()
	h.service.AccountData = &fakeAccountData{err: httpserver.New(http.StatusUnauthorized, "INVALID_CREDENTIALS", "Password is incorrect.")}
	response := h.do("POST", "/api/v1/auth/account/export", map[string]any{"currentPassword": "wrong"}, h.token)
	if response.status != http.StatusUnauthorized || response.code(t) != "INVALID_CREDENTIALS" {
		t.Fatalf("status = %d %s", response.status, response.body)
	}
}
