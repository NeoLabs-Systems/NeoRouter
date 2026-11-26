package auth

import (
	"net/http"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/httpx"
	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
)

// Routes registers the auth endpoints under /api/v1/auth. The sign-in endpoints
// are public, so the wiring mounts them before the API's session middleware;
// everything from /me on is wrapped here with Middleware.
//
// The account data endpoints (/auth/account/export, /export-keys, /delete) are
// registered here and delegate to the settings domain's AccountData surface.
func (s *Service) Routes(mux *http.ServeMux) {
	signIn := func() *httpserver.SlidingWindow {
		return httpserver.NewSlidingWindow(httpserver.SlidingWindowOptions{
			Window: time.Minute, Limit: 10, Key: s.clientIP, Log: s.Log,
		})
	}
	registerLimit, loginLimit, loginTwoFactorLimit := signIn(), signIn(), signIn()
	// One limiter shared by the four second-factor endpoints, keyed by account.
	twoFactorLimit := httpserver.NewSlidingWindow(httpserver.SlidingWindowOptions{
		Window: time.Minute, Limit: 10, Key: s.userKey, Log: s.Log,
	})

	mux.Handle("GET /api/v1/auth/registration", http.HandlerFunc(s.registrationHandler))
	mux.Handle("POST /api/v1/auth/register", registerLimit.Middleware(http.HandlerFunc(s.registerHandler)))
	mux.Handle("POST /api/v1/auth/login", loginLimit.Middleware(http.HandlerFunc(s.loginHandler)))
	mux.Handle("POST /api/v1/auth/login/2fa", loginTwoFactorLimit.Middleware(http.HandlerFunc(s.loginTwoFactorHandler)))

	s.signInRoutes(mux)

	mux.Handle("GET /api/v1/auth/me", s.Middleware(http.HandlerFunc(s.meHandler)))
	mux.Handle("GET /api/v1/auth/2fa", s.Middleware(http.HandlerFunc(s.twoFactorStatusHandler)))
	mux.Handle("POST /api/v1/auth/2fa/setup", s.Middleware(twoFactorLimit.Middleware(http.HandlerFunc(s.setupHandler))))
	mux.Handle("POST /api/v1/auth/2fa/enable", s.Middleware(twoFactorLimit.Middleware(http.HandlerFunc(s.enableHandler))))
	mux.Handle("POST /api/v1/auth/2fa/disable", s.Middleware(twoFactorLimit.Middleware(http.HandlerFunc(s.disableHandler))))
	mux.Handle("POST /api/v1/auth/2fa/recovery-codes", s.Middleware(twoFactorLimit.Middleware(http.HandlerFunc(s.recoveryCodesHandler))))
	mux.Handle("POST /api/v1/auth/logout", s.Middleware(http.HandlerFunc(s.logoutHandler)))
	mux.Handle("POST /api/v1/auth/logout-all", s.Middleware(http.HandlerFunc(s.logoutAllHandler)))
	mux.Handle("PUT /api/v1/auth/password", s.Middleware(http.HandlerFunc(s.changePasswordHandler)))
	mux.Handle("PATCH /api/v1/auth/profile", s.Middleware(http.HandlerFunc(s.profileHandler)))

	// Your own account: export it, export the saved keys, delete it. Each route
	// gets its own 5-per-minute limiter.
	accountLimit := func() *httpserver.SlidingWindow {
		return httpserver.NewSlidingWindow(httpserver.SlidingWindowOptions{
			Window: time.Minute, Limit: 5, Key: s.userKey, Log: s.Log,
		})
	}
	mux.Handle("POST /api/v1/auth/account/export", s.Middleware(accountLimit().Middleware(s.accountHandler("export"))))
	mux.Handle("POST /api/v1/auth/account/export-keys", s.Middleware(accountLimit().Middleware(s.accountHandler("export-keys"))))
	mux.Handle("POST /api/v1/auth/account/delete", s.Middleware(accountLimit().Middleware(s.accountHandler("delete"))))
}

// userKey is the rate limiter's default key, req.auth?.userId || req.ip.
func (s *Service) userKey(r *http.Request) string {
	if caller, ok := FromContext(r.Context()); ok && caller != nil {
		return caller.UserID
	}
	return s.clientIP(r)
}

type meResponse struct {
	User      PublicUser      `json:"user"`
	TwoFactor TwoFactorStatus `json:"twoFactor"`
}

type profileResponse struct {
	User PublicUser `json:"user"`
}

type twoFactorRequiredResponse struct {
	TwoFactorRequired bool   `json:"twoFactorRequired"`
	Challenge         string `json:"challenge"`
}

func (s *Service) registrationHandler(w http.ResponseWriter, r *http.Request) {
	status, err := s.RegistrationStatus(r.Context())
	if err != nil {
		httpserver.WriteError(w, r, s.log(), err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, status)
}

func (s *Service) registerHandler(w http.ResponseWriter, r *http.Request) {
	decoded, ok := readBody(w, r)
	if !ok {
		return
	}
	v := &validation{body: decoded}
	input := RegisterInput{
		Username:    v.str("username", 3, 64),
		Email:       v.email("email"),
		Password:    v.str("password", MinPassword, 1024),
		DisplayName: v.strOptional("displayName", 80),
	}
	if err := v.err(); err != nil {
		httpserver.WriteError(w, r, s.log(), err)
		return
	}
	result, err := s.Register(r.Context(), input, s.requestContext(r))
	if err != nil {
		httpserver.WriteError(w, r, s.log(), err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, result)
}

func (s *Service) loginHandler(w http.ResponseWriter, r *http.Request) {
	decoded, ok := readBody(w, r)
	if !ok {
		return
	}
	// The shared NeoLabs client sends the account as "username".
	if _, ok := decoded["account"]; !ok {
		if alias, ok := decoded["username"]; ok {
			decoded["account"] = alias
		}
	}
	v := &validation{body: decoded}
	account := v.str("account", 1, 320)
	password := v.str("password", 1, 1024)
	if err := v.err(); err != nil {
		httpserver.WriteError(w, r, s.log(), err)
		return
	}
	outcome, err := s.Login(r.Context(), account, password, s.requestContext(r))
	if err != nil {
		httpserver.WriteError(w, r, s.log(), err)
		return
	}
	if outcome.TwoFactorRequired {
		httpx.WriteJSON(w, http.StatusOK, twoFactorRequiredResponse{
			TwoFactorRequired: true,
			Challenge:         outcome.Challenge,
		})
		return
	}
	httpx.WriteJSON(w, http.StatusOK, outcome.Result)
}

func (s *Service) loginTwoFactorHandler(w http.ResponseWriter, r *http.Request) {
	decoded, ok := readBody(w, r)
	if !ok {
		return
	}
	v := &validation{body: decoded}
	challenge := v.str("challenge", 10, 200)
	code := v.str("code", 6, 64)
	if err := v.err(); err != nil {
		httpserver.WriteError(w, r, s.log(), err)
		return
	}
	result, err := s.LoginWithTwoFactor(r.Context(), challenge, code, s.requestContext(r))
	if err != nil {
		httpserver.WriteError(w, r, s.log(), err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, result)
}

func (s *Service) meHandler(w http.ResponseWriter, r *http.Request) {
	caller, ok := FromContext(r.Context())
	if !ok {
		httpserver.WriteError(w, r, s.log(), httpserver.New(http.StatusUnauthorized,
			"AUTHENTICATION_REQUIRED", "A valid session is required."))
		return
	}
	status, err := s.TwoFactorStatus(r.Context(), caller.UserID)
	if err != nil {
		httpserver.WriteError(w, r, s.log(), err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, meResponse{User: caller.User, TwoFactor: status})
}

func (s *Service) twoFactorStatusHandler(w http.ResponseWriter, r *http.Request) {
	caller, ok := FromContext(r.Context())
	if !ok {
		httpserver.WriteError(w, r, s.log(), httpserver.New(http.StatusUnauthorized,
			"AUTHENTICATION_REQUIRED", "A valid session is required."))
		return
	}
	status, err := s.TwoFactorStatus(r.Context(), caller.UserID)
	if err != nil {
		httpserver.WriteError(w, r, s.log(), err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, status)
}

func (s *Service) setupHandler(w http.ResponseWriter, r *http.Request) {
	decoded, ok := readBody(w, r)
	if !ok {
		return
	}
	v := &validation{body: decoded}
	currentPassword := v.str("currentPassword", 1, 1024)
	if err := v.err(); err != nil {
		httpserver.WriteError(w, r, s.log(), err)
		return
	}
	caller, ok := FromContext(r.Context())
	if !ok {
		httpserver.WriteError(w, r, s.log(), httpserver.New(http.StatusUnauthorized,
			"AUTHENTICATION_REQUIRED", "A valid session is required."))
		return
	}
	setup, err := s.BeginSetup(r.Context(), caller.UserID, currentPassword)
	if err != nil {
		httpserver.WriteError(w, r, s.log(), err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, setup)
}

func (s *Service) enableHandler(w http.ResponseWriter, r *http.Request) {
	decoded, ok := readBody(w, r)
	if !ok {
		return
	}
	v := &validation{body: decoded}
	code := v.str("code", 6, 16)
	if err := v.err(); err != nil {
		httpserver.WriteError(w, r, s.log(), err)
		return
	}
	caller, ok := FromContext(r.Context())
	if !ok {
		httpserver.WriteError(w, r, s.log(), httpserver.New(http.StatusUnauthorized,
			"AUTHENTICATION_REQUIRED", "A valid session is required."))
		return
	}
	result, err := s.Enable(r.Context(), caller.UserID, code)
	if err != nil {
		httpserver.WriteError(w, r, s.log(), err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, result)
}

func (s *Service) disableHandler(w http.ResponseWriter, r *http.Request) {
	decoded, ok := readBody(w, r)
	if !ok {
		return
	}
	v := &validation{body: decoded}
	currentPassword := v.str("currentPassword", 1, 1024)
	code := v.str("code", 6, 64)
	if err := v.err(); err != nil {
		httpserver.WriteError(w, r, s.log(), err)
		return
	}
	caller, ok := FromContext(r.Context())
	if !ok {
		httpserver.WriteError(w, r, s.log(), httpserver.New(http.StatusUnauthorized,
			"AUTHENTICATION_REQUIRED", "A valid session is required."))
		return
	}
	result, err := s.Disable(r.Context(), caller.UserID, currentPassword, code)
	if err != nil {
		httpserver.WriteError(w, r, s.log(), err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, result)
}

func (s *Service) recoveryCodesHandler(w http.ResponseWriter, r *http.Request) {
	decoded, ok := readBody(w, r)
	if !ok {
		return
	}
	v := &validation{body: decoded}
	currentPassword := v.str("currentPassword", 1, 1024)
	code := v.str("code", 6, 64)
	if err := v.err(); err != nil {
		httpserver.WriteError(w, r, s.log(), err)
		return
	}
	caller, ok := FromContext(r.Context())
	if !ok {
		httpserver.WriteError(w, r, s.log(), httpserver.New(http.StatusUnauthorized,
			"AUTHENTICATION_REQUIRED", "A valid session is required."))
		return
	}
	result, err := s.RegenerateRecoveryCodes(r.Context(), caller.UserID, currentPassword, code)
	if err != nil {
		httpserver.WriteError(w, r, s.log(), err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, result)
}

func (s *Service) logoutHandler(w http.ResponseWriter, r *http.Request) {
	caller, ok := FromContext(r.Context())
	if !ok {
		httpserver.WriteError(w, r, s.log(), httpserver.New(http.StatusUnauthorized,
			"AUTHENTICATION_REQUIRED", "A valid session is required."))
		return
	}
	if err := s.Logout(r.Context(), caller.SessionID); err != nil {
		httpserver.WriteError(w, r, s.log(), err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) logoutAllHandler(w http.ResponseWriter, r *http.Request) {
	if err := s.LogoutAll(r.Context(), caller(r).UserID); err != nil {
		httpserver.WriteError(w, r, s.log(), err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) changePasswordHandler(w http.ResponseWriter, r *http.Request) {
	decoded, ok := readBody(w, r)
	if !ok {
		return
	}
	v := &validation{body: decoded}
	currentPassword := v.str("currentPassword", -1, -1)
	newPassword := v.str("newPassword", MinPassword, 1024)
	if err := v.err(); err != nil {
		httpserver.WriteError(w, r, s.log(), err)
		return
	}
	caller, ok := FromContext(r.Context())
	if !ok {
		httpserver.WriteError(w, r, s.log(), httpserver.New(http.StatusUnauthorized,
			"AUTHENTICATION_REQUIRED", "A valid session is required."))
		return
	}
	if err := s.ChangePassword(r.Context(), caller.UserID, currentPassword, newPassword); err != nil {
		httpserver.WriteError(w, r, s.log(), err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) profileHandler(w http.ResponseWriter, r *http.Request) {
	decoded, ok := readBody(w, r)
	if !ok {
		return
	}
	v := &validation{body: decoded}
	update := ProfileUpdate{
		DisplayName: v.strOptional("displayName", 80),
		Email:       v.emailOptional("email"),
	}
	if err := v.err(); err != nil {
		httpserver.WriteError(w, r, s.log(), err)
		return
	}
	caller, ok := FromContext(r.Context())
	if !ok {
		httpserver.WriteError(w, r, s.log(), httpserver.New(http.StatusUnauthorized,
			"AUTHENTICATION_REQUIRED", "A valid session is required."))
		return
	}
	user, err := s.UpdateProfile(r.Context(), caller.UserID, update)
	if err != nil {
		httpserver.WriteError(w, r, s.log(), err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, profileResponse{User: user})
}
