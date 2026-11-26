package pools

// What other NeoRouters call to join a pool and keep it in order (routes/
// federation.js). Nothing here uses a session: an invite token, a ticket signed by
// the pool's host, or a signature by a known member authorises each call. Bodies
// are small and calls are rate-limited per address.

import (
	"io"
	"net/http"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/httpx"
	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
)

// federationBodyLimit is the JS express.json({limit:'64kb'}) for these routes.
const federationBodyLimit = 64 << 10

// FederationLimiter is the per-address limit of the federation endpoints (the JS
// routes/federation.js limiter, which the tests clear between cases).
type FederationLimiter = httpserver.SlidingWindow

// FederationLimiterOf is the limiter, so a test or the app can clear it.
func (s *Service) FederationLimiterOf() *FederationLimiter {
	if s.federationLimiter == nil {
		s.federationLimiter = httpserver.NewSlidingWindow(httpserver.SlidingWindowOptions{
			Window: time.Minute,
			Limit:  60,
			Code:   "POOL_RATE_LIMITED",
			Log:    s.log(),
		})
	}
	return s.federationLimiter
}

// FederationRoutes registers the federation API at /federation/v1 and the relay
// upgrade path.
func (s *Service) FederationRoutes(mux *http.ServeMux) {
	limiter := s.FederationLimiterOf()
	for _, name := range []string{"preview", "join", "introduce", "members", "ticket", "leave", "remove", "dissolve", "shared_changed"} {
		path := name
		mux.Handle("POST /federation/v1/"+path, limiter.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			s.federationCall(w, r, path)
		})))
	}
	mux.Handle("GET "+RelayPath, s.hub.handler())
}

// federationCall reads one call and answers it.
func (s *Service) federationCall(w http.ResponseWriter, r *http.Request, name string) {
	raw, readErr := io.ReadAll(io.LimitReader(r.Body, federationBodyLimit+1))
	if readErr != nil || len(raw) > federationBodyLimit {
		httpserver.WriteError(w, r, s.log(),
			httpserver.New(http.StatusRequestEntityTooLarge, "BODY_TOO_LARGE", "The request body is too large."))
		return
	}
	ctx := r.Context()
	var (
		answer any
		err    error
	)
	switch name {
	case "preview":
		body, decodeErr := decodeJSON(raw)
		object, _ := body.(map[string]any)
		if decodeErr != nil || object == nil {
			object = map[string]any{}
		}
		answer, err = s.handlePreview(ctx, stringOf(object["token"]))
	default:
		var message *peerMessage
		message, err = newPeerMessage(raw)
		if err != nil {
			s.writeError(w, r, err)
			return
		}
		switch name {
		case "join":
			answer, err = s.handleJoin(ctx, message)
		case "introduce":
			answer, err = s.handleIntroduce(ctx, message)
		case "members":
			answer, err = s.handleMemberList(ctx, message)
		case "ticket":
			answer, err = s.handleTicket(ctx, message)
		case "leave":
			answer, err = s.handleLeave(ctx, message)
		case "remove":
			answer, err = s.handleRemove(ctx, message)
		case "dissolve":
			answer, err = s.handleDissolve(ctx, message)
		case "shared_changed":
			answer, err = s.handleSharedChanged(ctx, message)
		}
	}
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, answer)
}
