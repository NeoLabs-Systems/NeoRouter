package providers

import (
	"context"
	"errors"
	"testing"
	"time"
)

// A caller that read the secret before the rotation was stored must not spend the
// old refresh token again, and a failed store is retried.
func TestStaleSecretDoesNotSpendARefreshTokenTwice(t *testing.T) {
	server := newCodexAuthServer(t)
	now := time.Now()
	oauth := testOAuth(t, map[string]string{"NEOROUTER_CODEX_ISSUER": server.url}, &now)
	stale := mustJSON(t, CodexCredentials{Access: "access-one", Refresh: "refresh-one", Expires: now.UnixMilli() - 1})

	failing := errors.New("disk full")
	if _, _, err := oauth.FreshCodexCredentials(context.Background(), stale, "acct", func(string) error { return failing }); !errors.Is(err, failing) {
		t.Fatalf("err = %v", err)
	}
	var stored []string
	store := func(secret string) error { stored = append(stored, secret); return nil }
	for range 2 {
		credentials, _, err := oauth.FreshCodexCredentials(context.Background(), stale, "acct", store)
		if err != nil || credentials.Access != "access-two" {
			t.Fatalf("credentials = %+v, %v", credentials, err)
		}
	}
	if server.refreshes != 1 || len(stored) != 1 {
		t.Fatalf("refreshes = %d, stored = %d", server.refreshes, len(stored))
	}
}
