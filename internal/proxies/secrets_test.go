package proxies

import (
	"context"
	"net/http"
	"strconv"
	"testing"

	"github.com/NeoLabs-Systems/neorouter/internal/secrets"
	"github.com/google/uuid"
)

// TestLegacyStoredProxyStillWorks writes the row the way earlier releases did
// and makes a request through it, which is the migration promise: no re-seal
// needed before traffic works.
func TestLegacyStoredProxyStillWorks(t *testing.T) {
	h := newHarness(t)
	socks := startSOCKS5(t)
	h.reset()

	legacyKey := []byte(fixtureKey)
	url := "socks5://127.0.0.1:" + strconv.Itoa(socks.port())
	sealed, err := secrets.SealLegacy(url, legacyKey)
	if err != nil {
		t.Fatalf("seal legacy: %v", err)
	}
	if _, err := h.db.W.Exec(
		`INSERT INTO outbound_proxies (id, fingerprint, endpoint, secret_encrypted) VALUES (?, ?, ?, ?)`,
		uuid.NewString(), fingerprint(url), "socks5://127.0.0.1:"+strconv.Itoa(socks.port()), sealed); err != nil {
		t.Fatalf("insert legacy row: %v", err)
	}

	resp, err := h.svc.Fetch(context.Background(), h.request(t, http.MethodGet, "http://"+h.originHost+"/", ""))
	if err != nil {
		t.Fatalf("fetch through a legacy-sealed proxy: %v", err)
	}
	h.closeBody(resp)
	if served := h.list()[0].Served; served != 1 {
		t.Fatalf("served = %d, want 1", served)
	}
}
