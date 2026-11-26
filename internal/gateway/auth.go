package gateway

// server/middleware/router_key_auth.js: gateway clients send the router key the
// way their SDK does.

import (
	"net/http"
	"strings"

	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
)

// partnerPaths are the only paths a pool partner's key may reach.
var partnerPaths = map[string]bool{"/models": true, "/chat/completions": true}

// extractToken is extractToken: the router key from the header the client's SDK
// sends it in — OpenAI's Authorization, Anthropic's x-api-key, Gemini tooling's
// x-goog-api-key.
func extractToken(r *http.Request) string {
	if authorization := r.Header.Get("Authorization"); strings.HasPrefix(authorization, "Bearer ") {
		token := strings.TrimSpace(authorization[len("Bearer "):])
		if token != "" {
			return token
		}
	}
	for _, header := range []string{"x-api-key", "x-goog-api-key"} {
		if token := strings.TrimSpace(r.Header.Get(header)); token != "" {
			return token
		}
	}
	return ""
}

// Authenticate is requireRouterKey: the key a request carries, or the error the
// client is answered with. A pool partner's key reaches chat and the model list
// only (for now).
func (s *Service) Authenticate(r *http.Request) (*Key, error) {
	if s.opts.Keys == nil {
		return nil, httpserver.New(http.StatusUnauthorized, "INVALID_API_KEY", "A valid NeoRouter key (nrt_…) is required.")
	}
	row, err := s.opts.Keys.Authenticate(extractToken(r))
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, httpserver.New(http.StatusUnauthorized, "INVALID_API_KEY", "A valid NeoRouter key (nrt_…) is required.")
	}
	key := keyFromRow(row)
	if key.IsPeer() && !partnerPaths[strings.TrimPrefix(r.URL.Path, "/v1")] {
		return nil, httpserver.New(http.StatusForbidden, "POOL_CHAT_ONLY", "Pool partners can only use chat models.")
	}
	return key, nil
}

// withKey wraps a handler that needs a router key.
func (s *Service) withKey(dialect string, handler func(w http.ResponseWriter, r *http.Request, key *Key)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key, err := s.Authenticate(r)
		if err != nil {
			s.WriteError(w, r, dialect, nil, "", err)
			return
		}
		handler(w, r, key)
	}
}
