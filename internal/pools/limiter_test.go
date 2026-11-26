package pools

import (
	"net/http/httptest"
	"testing"

	"github.com/NeoLabs-Systems/neorouter/internal/config"
)

// Behind a trusted proxy each partner has its own federation allowance.
func TestFederationClientFollowsTrustProxy(t *testing.T) {
	request := httptest.NewRequest("POST", "/federation/v1/ticket", nil)
	request.RemoteAddr = "10.0.0.1:1000"
	request.Header.Set("X-Forwarded-For", "203.0.113.9")
	for trust, want := range map[bool]string{true: "203.0.113.9", false: "10.0.0.1"} {
		service := &Service{Cfg: func() config.Config { return config.Config{TrustProxy: trust} }}
		if got := service.clientOf(request); got != want {
			t.Errorf("trust=%v: %q", trust, got)
		}
	}
}
