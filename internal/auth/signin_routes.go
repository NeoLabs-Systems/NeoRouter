package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/apperr"
	"github.com/NeoLabs-Systems/neolabs-go/authpages"
	"github.com/NeoLabs-Systems/neolabs-go/httpx"
	"github.com/NeoLabs-Systems/neolabs-go/qrlogin"
	"github.com/NeoLabs-Systems/neolabs-go/ratelimit"
	"github.com/NeoLabs-Systems/neolabs-go/webauthnx"
	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
)

// forgotMinimumLatency covers the difference between looking an account up and
// not finding one.
const forgotMinimumLatency = 600 * time.Millisecond

// The routes for QR sign-in, password reset and passkeys. The paths are the
// shared NeoLabs ones under /api/v1.
func (s *Service) signInRoutes(mux *http.ServeMux) {
	limit := func(window time.Duration, max int) func(http.HandlerFunc) http.Handler {
		limiter := ratelimit.New(ratelimit.Config{
			Window: window, Max: max, Key: s.clientIP,
			Message: map[string]any{"error": map[string]string{
				"code": "RATE_LIMITED", "message": "Too many requests. Please try again later.",
			}},
		})
		return func(h http.HandlerFunc) http.Handler { return limiter.Middleware(h) }
	}
	passkey := limit(time.Minute, 20)
	qrCreate := limit(time.Minute, 20)
	qrPoll := limit(time.Minute, 120)
	reset := limit(15*time.Minute, 10)
	session := func(h http.HandlerFunc) http.Handler { return s.Middleware(h) }

	mux.HandleFunc("GET /api/v1/auth/options", s.optionsHandler)
	mux.Handle("POST /api/v1/auth/password/forgot", reset(s.forgotHandler))
	mux.HandleFunc("GET "+passwordResetPath, s.resetPageHandler)
	mux.Handle("POST "+passwordResetPath, reset(s.resetSubmitHandler))

	mux.Handle("POST /api/v1/auth/webauthn/options", passkey(s.passkeyOptionsHandler))
	mux.Handle("POST /api/v1/auth/webauthn/verify", passkey(s.passkeyVerifyHandler))
	mux.Handle("GET /api/v1/settings/security-keys", session(s.listKeysHandler))
	mux.Handle("POST /api/v1/settings/security-keys/options", s.Middleware(passkey(s.addKeyOptionsHandler)))
	mux.Handle("POST /api/v1/settings/security-keys", s.Middleware(passkey(s.addKeyHandler)))
	mux.Handle("PUT /api/v1/settings/security-keys/{id}", session(s.renameKeyHandler))
	mux.Handle("DELETE /api/v1/settings/security-keys/{id}", session(s.deleteKeyHandler))

	mux.Handle("POST /api/v1/auth/qr-login/challenge", qrCreate(s.qrChallengeHandler))
	mux.Handle("POST /api/v1/auth/qr-login/challenge/{id}/status", qrPoll(s.qrStatusHandler))
	mux.Handle("POST /api/v1/auth/qr-login/challenge/{id}/cancel", qrPoll(s.qrCancelHandler))
	mux.Handle("POST /api/v1/auth/qr-login/challenge/{id}/claim", qrPoll(s.qrClaimHandler))
	mux.Handle("POST /api/v1/account/qr-login/resolve", session(s.qrResolveHandler))
	mux.Handle("POST /api/v1/account/qr-login/approve", session(s.qrApproveHandler))
	mux.Handle("POST /api/v1/account/qr-login/decline", session(s.qrDeclineHandler))
}

// fail writes an error of this package or of the shared sign-in packages.
func (s *Service) fail(w http.ResponseWriter, r *http.Request, err error) {
	if failure, ok := apperr.As(err); ok {
		err = httpserver.New(failure.Status, "QR_LOGIN_ERROR", failure.Message)
	}
	httpserver.WriteError(w, r, s.log(), ceremonyError(err))
}

// decodeInto reads a JSON body into a struct; a failure has been answered.
func decodeInto(w http.ResponseWriter, r *http.Request, dst any) bool {
	return httpserver.ReadJSONManagement(w, r, dst, httpserver.DefaultJSONBodyLimit) == nil
}

func invalid(message string) error {
	return httpserver.New(http.StatusBadRequest, "VALIDATION_ERROR", message)
}

// baseURL is how clients reach this server: the public URL when configured,
// otherwise the address the request came in on.
func (s *Service) baseURL(r *http.Request) string {
	c := s.cfg()
	if c.PublicURL != "" {
		return strings.TrimRight(c.PublicURL, "/")
	}
	scheme := "http"
	if r.TLS != nil || c.TrustProxy && strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

type optionsResponse struct {
	Registration      bool `json:"registration"`
	Bootstrap         bool `json:"bootstrap"`
	PasswordReset     bool `json:"passwordReset"`
	MinPasswordLength int  `json:"minPasswordLength"`
}

func (s *Service) optionsHandler(w http.ResponseWriter, r *http.Request) {
	status, err := s.RegistrationStatus(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, optionsResponse{
		Registration: status.Open, Bootstrap: status.Bootstrap,
		PasswordReset: s.PasswordResetEnabled(), MinPasswordLength: MinPassword,
	})
}

func (s *Service) forgotHandler(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Account string `json:"account"`
	}
	if !decodeInto(w, r, &body) {
		return
	}
	if len(body.Account) > 320 {
		s.fail(w, r, invalid("account is too long."))
		return
	}
	started := time.Now()
	s.sendResetInBackground(r.Context(), body.Account)
	time.Sleep(time.Until(started.Add(forgotMinimumLatency)))
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"message": "If that account has an email address, NeoRouter will send a password reset link.",
	})
}

// resetPage renders the shared reset form. The token is in the address, so the
// page is neither cached nor named as a referrer.
func resetPage(w http.ResponseWriter, token, problem string, done bool) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	authpages.PasswordReset(w, "NeoRouter", passwordResetPath, token, problem, done)
}

const openFullLink = "Open the full password reset link from your NeoRouter email."

func (s *Service) resetPageHandler(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSpace(r.URL.Query().Get("token"))
	if token == "" {
		resetPage(w, "", openFullLink, false)
		return
	}
	resetPage(w, token, "", false)
}

func (s *Service) resetSubmitHandler(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	token, password := strings.TrimSpace(r.PostFormValue("token")), r.PostFormValue("password")
	switch {
	case token == "":
		resetPage(w, "", openFullLink, false)
	case password != r.PostFormValue("confirmPassword"):
		resetPage(w, token, "Passwords do not match.", false)
	case len(password) > 1024:
		resetPage(w, token, "The password is too long.", false)
	default:
		if err := s.resetPassword(r.Context(), token, password); err != nil {
			message := "The password could not be changed. Please try again."
			if known, ok := err.(*httpserver.HttpError); ok {
				message = known.Message
			} else {
				s.log().Error("auth: password reset failed", "error", err)
			}
			resetPage(w, token, message, false)
			return
		}
		resetPage(w, "", "", true)
	}
}

type challengeResponse struct {
	ChallengeID string `json:"challengeId"`
	Options     any    `json:"options"`
}

func (s *Service) passkeyOptionsHandler(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Account string `json:"account"`
	}
	if !decodeInto(w, r, &body) {
		return
	}
	id, options, err := s.passkeySignInOptions(r.Context(), r, body.Account)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, challengeResponse{ChallengeID: id, Options: options})
}

type assertionBody struct {
	ChallengeID   string          `json:"challengeId"`
	Response      json.RawMessage `json:"response"`
	TwoFactorCode string          `json:"twoFactorCode"`
	Label         string          `json:"label"`
}

func (s *Service) passkeyVerifyHandler(w http.ResponseWriter, r *http.Request) {
	var body assertionBody
	if !decodeInto(w, r, &body) {
		return
	}
	if len(body.TwoFactorCode) > 64 {
		s.fail(w, r, invalid("twoFactorCode is too long."))
		return
	}
	result, err := s.passkeySignIn(r.Context(), r, body.ChallengeID, body.Response, body.TwoFactorCode, s.requestContext(r))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, result)
}

type keysResponse struct {
	Credentials []webauthnx.Payload `json:"credentials"`
}

// writeKeys answers with the account's keys, as every key endpoint does.
func (s *Service) writeKeys(w http.ResponseWriter, r *http.Request, status int, keys []webauthnx.Payload, err error) {
	if err != nil {
		s.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, status, keysResponse{Credentials: keys})
}

// caller is the account of a request behind Middleware.
func caller(r *http.Request) *Auth {
	value, _ := FromContext(r.Context())
	return value
}

func (s *Service) listKeysHandler(w http.ResponseWriter, r *http.Request) {
	keys, err := s.webauthn().List(r.Context(), caller(r).UserID)
	s.writeKeys(w, r, http.StatusOK, keys, err)
}

func (s *Service) addKeyOptionsHandler(w http.ResponseWriter, r *http.Request) {
	var body struct {
		CurrentPassword string `json:"currentPassword"`
	}
	if !decodeInto(w, r, &body) {
		return
	}
	id, options, err := s.passkeyOptions(r.Context(), r, caller(r).UserID, body.CurrentPassword)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, challengeResponse{ChallengeID: id, Options: options})
}

func (s *Service) addKeyHandler(w http.ResponseWriter, r *http.Request) {
	var body assertionBody
	if !decodeInto(w, r, &body) {
		return
	}
	keys, err := s.addPasskey(r.Context(), r, caller(r).UserID, body.ChallengeID, body.Response, body.Label)
	s.writeKeys(w, r, http.StatusCreated, keys, err)
}

func keyID(r *http.Request) int64 {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		return 0 // no key has row 0, so this reads as not found
	}
	return id
}

func (s *Service) renameKeyHandler(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Label string `json:"label"`
	}
	if !decodeInto(w, r, &body) {
		return
	}
	keys, err := s.webauthn().Rename(r.Context(), caller(r).UserID, keyID(r), body.Label)
	s.writeKeys(w, r, http.StatusOK, keys, err)
}

func (s *Service) deleteKeyHandler(w http.ResponseWriter, r *http.Request) {
	keys, err := s.webauthn().Delete(r.Context(), caller(r).UserID, keyID(r))
	s.writeKeys(w, r, http.StatusOK, keys, err)
}

func (s *Service) qrChallengeHandler(w http.ResponseWriter, r *http.Request) {
	var body struct {
		RequestMetadata any `json:"requestMetadata"`
	}
	if !decodeInto(w, r, &body) {
		return
	}
	rc := s.requestContext(r)
	challenge, err := s.createQRChallenge(r.Context(), s.baseURL(r), qrlogin.Request{
		UserAgent: truncRunes(rc.UserAgent, 500), IPAddress: rc.IPAddress, Metadata: body.RequestMetadata,
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, challenge)
}

// pollToken reads the token only the signing-in screen holds.
func (s *Service) pollToken(w http.ResponseWriter, r *http.Request) (token, known string, ok bool) {
	var body struct {
		Token       string `json:"token"`
		KnownStatus string `json:"knownStatus"`
	}
	if !decodeInto(w, r, &body) {
		return "", "", false
	}
	token = strings.TrimSpace(body.Token)
	if token == "" {
		s.fail(w, r, invalid("QR login poll token is required."))
		return "", "", false
	}
	return token, strings.TrimSpace(body.KnownStatus), true
}

func (s *Service) qrStatusHandler(w http.ResponseWriter, r *http.Request) {
	token, known, ok := s.pollToken(w, r)
	if !ok {
		return
	}
	var poll *qrlogin.Poll
	var err error
	if known == "" {
		poll, err = s.qr().PollStatus(r.Context(), r.PathValue("id"), token)
	} else {
		// Held open until the status changes, so the screen hears of a scan or
		// approval at once.
		poll, err = s.qr().WaitForChange(r.Context(), r.PathValue("id"), token, known, qrlogin.LongPollMax)
	}
	if r.Context().Err() != nil {
		return
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, poll)
}

type successResponse struct {
	Success bool `json:"success"`
}

func (s *Service) qrCancelHandler(w http.ResponseWriter, r *http.Request) {
	token, _, ok := s.pollToken(w, r)
	if !ok {
		return
	}
	if err := s.qr().Cancel(r.Context(), r.PathValue("id"), token); err != nil {
		s.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, successResponse{Success: true})
}

func (s *Service) qrClaimHandler(w http.ResponseWriter, r *http.Request) {
	token, _, ok := s.pollToken(w, r)
	if !ok {
		return
	}
	// A claim is spent once, so finish it even if the screen goes away.
	result, err := s.claimQRLogin(context.WithoutCancel(r.Context()), r.PathValue("id"), token, s.requestContext(r))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, result)
}

type scanBody struct {
	ChallengeID      string `json:"challengeId"`
	Secret           string `json:"secret"`
	ScannerMetadata  any    `json:"scannerMetadata"`
	ApprovalMetadata any    `json:"approvalMetadata"`
}

// scanned reads what the approving device took from the code.
func (s *Service) scanned(w http.ResponseWriter, r *http.Request) (scanBody, bool) {
	var body scanBody
	if !decodeInto(w, r, &body) {
		return body, false
	}
	body.ChallengeID, body.Secret = strings.TrimSpace(body.ChallengeID), strings.TrimSpace(body.Secret)
	if body.ChallengeID == "" || body.Secret == "" {
		s.fail(w, r, invalid("Challenge id and QR secret are required."))
		return body, false
	}
	return body, true
}

func (s *Service) qrResolveHandler(w http.ResponseWriter, r *http.Request) {
	body, ok := s.scanned(w, r)
	if !ok {
		return
	}
	challenge, err := s.qr().MarkScanned(r.Context(), body.ChallengeID, body.Secret, body.ScannerMetadata)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, challenge)
}

func (s *Service) qrApproveHandler(w http.ResponseWriter, r *http.Request) {
	body, ok := s.scanned(w, r)
	if !ok {
		return
	}
	challenge, err := s.approveQRLogin(r.Context(), caller(r), body.ChallengeID, body.Secret, body.ApprovalMetadata)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, challenge)
}

func (s *Service) qrDeclineHandler(w http.ResponseWriter, r *http.Request) {
	body, ok := s.scanned(w, r)
	if !ok {
		return
	}
	if err := s.qr().Decline(r.Context(), body.ChallengeID, body.Secret); err != nil {
		s.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, successResponse{Success: true})
}
