package live

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"

	"github.com/NeoLabs-Systems/neorouter/internal/providers"
)

// The socket dial is checked as well as the name lookup before it.
func TestPublicOnlySocketsCannotBeRebound(t *testing.T) {
	reached := false
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true }))
	defer server.Close()
	target := upstreamTarget{url: "ws://localhost" + strings.TrimPrefix(server.URL, "http://127.0.0.1")}
	public := func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
	}
	_, err := openUpstream(target, true, public)
	if providers.KindOf(err) != providers.KindBadRequest || reached {
		t.Fatalf("err = %v, reached = %v", err, reached)
	}
}
