package teams

import (
	"net/http"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/httpx"
	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
)

// Routes registers the team endpoints under /api/v1/teams. Every one of them
// acts on the caller's own space, so each is wrapped with RequireOwnSpace, and
// the tree is mounted behind SpaceMiddleware, which is what requires the session
// in the first place: `auth.Middleware(teams.SpaceMiddleware(auth.Actor, mux))`,
// or the space middleware alone when the session runs outside it.
func (s *Service) Routes(mux *http.ServeMux) {
	inviteLimit := httpserver.NewSlidingWindow(httpserver.SlidingWindowOptions{
		Window: 15 * time.Minute, Limit: 20, Key: s.rateLimitKey, Log: s.Log,
	})
	changeLimit := httpserver.NewSlidingWindow(httpserver.SlidingWindowOptions{
		Window: 15 * time.Minute, Limit: 120, Key: s.rateLimitKey, Log: s.Log,
	})

	mux.Handle("GET /api/v1/teams", s.guard(http.HandlerFunc(s.summaryHandler)))
	mux.Handle("POST /api/v1/teams/invites/preview", s.guard(inviteLimit.Middleware(http.HandlerFunc(s.previewInviteHandler))))
	mux.Handle("POST /api/v1/teams/invites/redeem", s.guard(inviteLimit.Middleware(http.HandlerFunc(s.redeemInviteHandler))))
	mux.Handle("POST /api/v1/teams/invites", s.guard(changeLimit.Middleware(http.HandlerFunc(s.createInviteHandler))))
	mux.Handle("DELETE /api/v1/teams/invites/{id}", s.guard(changeLimit.Middleware(http.HandlerFunc(s.revokeInviteHandler))))
	mux.Handle("PUT /api/v1/teams/members/{userId}/permissions", s.guard(changeLimit.Middleware(http.HandlerFunc(s.setPermissionsHandler))))
	mux.Handle("DELETE /api/v1/teams/members/{userId}", s.guard(changeLimit.Middleware(http.HandlerFunc(s.releaseMemberHandler))))
	mux.Handle("POST /api/v1/teams/leave", s.guard(changeLimit.Middleware(http.HandlerFunc(s.leaveHandler))))
}

// guard applies the own-space rule to one endpoint.
func (s *Service) guard(next http.Handler) http.Handler {
	return RequireOwnSpace(next)
}

// rateLimitKey is the account, else the address, so a member cannot spread its
// tries over addresses.
func (s *Service) rateLimitKey(r *http.Request) string {
	if space, ok := SpaceFromContext(r.Context()); ok {
		return space.ActorID
	}
	return httpx.ClientIP(r, s.cfg().TrustProxy)
}

func (s *Service) summaryHandler(w http.ResponseWriter, r *http.Request) {
	userID, err := spaceActorID(r)
	if err != nil {
		httpserver.WriteError(w, r, s.log(), err)
		return
	}
	summary, err := s.BuildSummary(r.Context(), userID)
	if err != nil {
		httpserver.WriteError(w, r, s.log(), err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, summary)
}

// writeSummary answers with the Team page state after a change, as every
// mutating team endpoint does.
func (s *Service) writeSummary(w http.ResponseWriter, r *http.Request) {
	userID, err := spaceActorID(r)
	if err != nil {
		httpserver.WriteError(w, r, s.log(), err)
		return
	}
	summary, err := s.BuildSummary(r.Context(), userID)
	if err != nil {
		httpserver.WriteError(w, r, s.log(), err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, summary)
}

func (s *Service) previewInviteHandler(w http.ResponseWriter, r *http.Request) {
	decoded, ok := readBody(w, r)
	if !ok {
		return
	}
	v := &validation{body: decoded}
	link := v.str("link", 1, 2048)
	if err := v.err(); err != nil {
		httpserver.WriteError(w, r, s.log(), err)
		return
	}
	userID, err := spaceActorID(r)
	if err != nil {
		httpserver.WriteError(w, r, s.log(), err)
		return
	}
	preview, err := s.PreviewInvite(r.Context(), userID, link)
	if err != nil {
		httpserver.WriteError(w, r, s.log(), err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, preview)
}

func (s *Service) redeemInviteHandler(w http.ResponseWriter, r *http.Request) {
	decoded, ok := readBody(w, r)
	if !ok {
		return
	}
	v := &validation{body: decoded}
	link := v.str("link", 1, 2048)
	if err := v.err(); err != nil {
		httpserver.WriteError(w, r, s.log(), err)
		return
	}
	userID, err := spaceActorID(r)
	if err != nil {
		httpserver.WriteError(w, r, s.log(), err)
		return
	}
	if err := s.RedeemInvite(r.Context(), userID, link); err != nil {
		httpserver.WriteError(w, r, s.log(), err)
		return
	}
	s.writeSummary(w, r)
}

// createInviteResponse is the 201 of a new invite link: the invite and the one
// address that carries its token.
type createInviteResponse struct {
	Invite Invite `json:"invite"`
	Link   string `json:"link"`
}

func (s *Service) createInviteHandler(w http.ResponseWriter, r *http.Request) {
	decoded, ok := readBody(w, r)
	if !ok {
		return
	}
	v := &validation{body: decoded}
	label := v.strOptional("label", 80)
	granted := v.enumArray("permissions", PERMISSIONS, len(PERMISSIONS))
	expiresInHours := v.intOptionalNullable("expiresInHours", 1, 24*90)
	singleUse := v.boolOptional("singleUse")
	if err := v.err(); err != nil {
		httpserver.WriteError(w, r, s.log(), err)
		return
	}
	userID, err := spaceActorID(r)
	if err != nil {
		httpserver.WriteError(w, r, s.log(), err)
		return
	}
	inviteLabel := ""
	if label != nil {
		inviteLabel = *label
	}
	invite, token, err := s.CreateInvite(r.Context(), userID, inviteLabel, granted, expiresInHours,
		singleUse != nil && *singleUse)
	if err != nil {
		httpserver.WriteError(w, r, s.log(), err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, createInviteResponse{
		Invite: invite,
		Link:   BuildLink(s.publicOrigin(r), token),
	})
}

func (s *Service) revokeInviteHandler(w http.ResponseWriter, r *http.Request) {
	userID, err := spaceActorID(r)
	if err != nil {
		httpserver.WriteError(w, r, s.log(), err)
		return
	}
	if err := s.RevokeInvite(r.Context(), userID, r.PathValue("id")); err != nil {
		httpserver.WriteError(w, r, s.log(), err)
		return
	}
	s.writeSummary(w, r)
}

func (s *Service) setPermissionsHandler(w http.ResponseWriter, r *http.Request) {
	decoded, ok := readBody(w, r)
	if !ok {
		return
	}
	v := &validation{body: decoded}
	granted := v.enumArray("permissions", PERMISSIONS, len(PERMISSIONS))
	if err := v.err(); err != nil {
		httpserver.WriteError(w, r, s.log(), err)
		return
	}
	userID, err := spaceActorID(r)
	if err != nil {
		httpserver.WriteError(w, r, s.log(), err)
		return
	}
	if err := s.SetPermissions(r.Context(), userID, r.PathValue("userId"), granted); err != nil {
		httpserver.WriteError(w, r, s.log(), err)
		return
	}
	s.writeSummary(w, r)
}

func (s *Service) releaseMemberHandler(w http.ResponseWriter, r *http.Request) {
	userID, err := spaceActorID(r)
	if err != nil {
		httpserver.WriteError(w, r, s.log(), err)
		return
	}
	if err := s.ReleaseMember(r.Context(), userID, r.PathValue("userId")); err != nil {
		httpserver.WriteError(w, r, s.log(), err)
		return
	}
	s.writeSummary(w, r)
}

func (s *Service) leaveHandler(w http.ResponseWriter, r *http.Request) {
	userID, err := spaceActorID(r)
	if err != nil {
		httpserver.WriteError(w, r, s.log(), err)
		return
	}
	if err := s.Leave(r.Context(), userID); err != nil {
		httpserver.WriteError(w, r, s.log(), err)
		return
	}
	s.writeSummary(w, r)
}
