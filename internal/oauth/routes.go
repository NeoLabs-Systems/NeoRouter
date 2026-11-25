package oauth

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/NeoLabs-Systems/neolabs-go/httpx"
	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
)

// maxOAuthBody is applyBody's 16kb limit on token and device requests.
const maxOAuthBody = 16 << 10

// Routes registers the OAuth endpoints apps talk to (server/routes/connect.js,
// mounted at the root of server/app.js) and the dashboard endpoints
// (server/routes/oauth.js).
func (s *Service) Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /.well-known/oauth-authorization-server", s.metadataRoute)
	mux.HandleFunc("GET /oauth/authorize", s.authorizeRoute)
	mux.HandleFunc("POST /oauth/device/code", s.deviceCodeRoute)
	mux.HandleFunc("OPTIONS /oauth/device/code", s.preflight)
	mux.HandleFunc("POST /oauth/token", s.tokenRoute)
	mux.HandleFunc("OPTIONS /oauth/token", s.preflight)

	mux.HandleFunc("GET /api/v1/oauth/openrouter/callback/{state}", s.openRouterCallbackRoute)
	mux.Handle("POST /api/v1/oauth/openrouter/start", s.guarded("providers.manage", s.startOpenRouterRoute))
	mux.Handle("GET /api/v1/oauth/apps/requests/{reference}", s.guarded("keys.create", s.describeRoute))
	mux.Handle("POST /api/v1/oauth/apps/requests/{reference}/approve", s.guarded("keys.create", s.approveRoute))
	mux.Handle("POST /api/v1/oauth/apps/requests/{reference}/deny", s.guarded("keys.create", s.denyRoute))
}

// openCORS answers token requests from apps on any origin, which carry no
// cookies, and reports whether it already answered a preflight.
func openCORS(w http.ResponseWriter, r *http.Request) bool {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	w.Header().Set("Access-Control-Allow-Methods", "GET,POST,OPTIONS")
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return true
	}
	return false
}

// preflight is openCORS for the paths a browser preflights.
func (s *Service) preflight(w http.ResponseWriter, r *http.Request) { openCORS(w, r) }

// redirectTo sends the browser on, like res.redirect.
func redirectTo(w http.ResponseWriter, target string) {
	w.Header().Set("Location", target)
	w.WriteHeader(http.StatusFound)
}

func (s *Service) metadataRoute(w http.ResponseWriter, r *http.Request) {
	if openCORS(w, r) {
		return
	}
	httpx.WriteJSON(w, http.StatusOK, s.metadata(s.publicOrigin(r)))
}

func (s *Service) authorizeRoute(w http.ResponseWriter, r *http.Request) {
	target, err := s.startAuthorization(r.URL.Query())
	if err != nil {
		// No trustworthy redirect_uri: show the problem in the app instead.
		var oauthErr *OAuthError
		if errors.As(err, &oauthErr) {
			redirectTo(w, "/app/?connect_error="+encodeURIComponent(oauthErr.Description))
			return
		}
		httpserver.WriteError(w, r, s.log, err)
		return
	}
	redirectTo(w, target)
}

func (s *Service) deviceCodeRoute(w http.ResponseWriter, r *http.Request) {
	if openCORS(w, r) {
		return
	}
	body, err := readOAuthBody(w, r)
	if err != nil {
		return
	}
	response, err := s.startDevice(body, s.publicOrigin(r))
	if err != nil {
		writeOAuthError(w, r, s.log, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, response)
}

func (s *Service) tokenRoute(w http.ResponseWriter, r *http.Request) {
	if openCORS(w, r) {
		return
	}
	body, err := readOAuthBody(w, r)
	if err != nil {
		return
	}
	token, err := s.exchange(r.Context(), body, s.publicOrigin(r))
	if err != nil {
		writeOAuthError(w, r, s.log, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, token)
}

func (s *Service) openRouterCallbackRoute(w http.ResponseWriter, r *http.Request) {
	target, err := s.CompleteOpenRouter(r.Context(), r.PathValue("state"), r.URL.Query().Get("code"))
	if err != nil {
		httpserver.WriteError(w, r, s.log, err)
		return
	}
	redirectTo(w, target)
}

// startResponse is where the browser goes to approve at OpenRouter.
type startResponse struct {
	URL string `json:"url"`
}

func (s *Service) startOpenRouterRoute(w http.ResponseWriter, r *http.Request, caller Caller) {
	raw, err := readJSONBody(w, r, httpserver.DefaultJSONBodyLimit)
	if err != nil {
		return
	}
	returnTo, err := parseReturnTo(raw)
	if err != nil {
		httpserver.WriteError(w, r, s.log, err)
		return
	}
	target, err := s.StartOpenRouter(OpenRouterStart{
		UserID:      caller.Space.OwnerID,
		CreatedBy:   caller.Space.ActorID,
		Origin:      s.publicOrigin(r),
		RequestHost: r.Host,
		ReturnTo:    returnTo,
	})
	if err != nil {
		httpserver.WriteError(w, r, s.log, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, startResponse{URL: target})
}

// requestedResponse is one connection request as the approval screen sees it.
type requestedResponse struct {
	Request *describeRequest `json:"request"`
}

func (s *Service) describeRoute(w http.ResponseWriter, r *http.Request, _ Caller) {
	request, err := s.describe(r.PathValue("reference"))
	if err != nil {
		httpserver.WriteError(w, r, s.log, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, requestedResponse{Request: request})
}

func (s *Service) approveRoute(w http.ResponseWriter, r *http.Request, caller Caller) {
	raw, err := readJSONBody(w, r, httpserver.DefaultJSONBodyLimit)
	if err != nil {
		return
	}
	approved, err := parseAccess(raw)
	if err != nil {
		httpserver.WriteError(w, r, s.log, err)
		return
	}
	result, err := s.approve(r.PathValue("reference"), caller.Space.OwnerID, caller.Space.ActorID, approved)
	if err != nil {
		httpserver.WriteError(w, r, s.log, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, result)
}

func (s *Service) denyRoute(w http.ResponseWriter, r *http.Request, _ Caller) {
	result, err := s.deny(r.PathValue("reference"))
	if err != nil {
		httpserver.WriteError(w, r, s.log, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, result)
}

// readJSONBody reads a management request body and returns its raw JSON, so the
// caller can tell a missing field from a wrong one.
func readJSONBody(w http.ResponseWriter, r *http.Request, limit int64) ([]byte, error) {
	var raw json.RawMessage
	if err := httpserver.ReadJSONManagement(w, r, &raw, limit); err != nil {
		return nil, err
	}
	return raw, nil
}

// readOAuthBody parses a token or device request body the way
// express.urlencoded({extended:false}) followed by express.json({limit:'16kb'})
// does: form-encoded when it says so, JSON when it says so.
func readOAuthBody(w http.ResponseWriter, r *http.Request) (map[string]string, error) {
	body := make(map[string]string)
	if r.Body == nil {
		return body, nil
	}
	contentType := r.Header.Get("Content-Type")
	if isFormContentType(contentType) {
		raw, err := readCapped(w, r, maxOAuthBody)
		if err != nil {
			return nil, err
		}
		values, err := url.ParseQuery(string(raw))
		if err != nil {
			return nil, err
		}
		for key, items := range values {
			if len(items) > 0 {
				body[key] = items[0]
			}
		}
		return body, nil
	}
	raw, err := readJSONBody(w, r, maxOAuthBody)
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &fields); err != nil {
			httpserver.WriteError(w, r, nil, httpserver.New(http.StatusBadRequest, "INVALID_JSON", "The request body is not valid JSON."))
			return nil, err
		}
	}
	for key, value := range fields {
		body[key] = jsonScalar(value)
	}
	return body, nil
}

// isFormContentType reports whether the body is application/x-www-form-urlencoded.
func isFormContentType(contentType string) bool {
	for i := 0; i < len(contentType); i++ {
		if contentType[i] == ';' {
			contentType = contentType[:i]
			break
		}
	}
	return strings.EqualFold(strings.TrimSpace(contentType), "application/x-www-form-urlencoded")
}

// readCapped reads at most limit bytes of the body.
func readCapped(w http.ResponseWriter, r *http.Request, limit int64) ([]byte, error) {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, limit))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			httpserver.WriteError(w, r, nil, httpserver.New(http.StatusRequestEntityTooLarge, "BODY_TOO_LARGE", "The request body is too large."))
			return nil, err
		}
		httpserver.WriteError(w, r, nil, fmt.Errorf("oauth: read request body: %w", err))
		return nil, err
	}
	return raw, nil
}

// jsonScalar is String(value) for the JSON values OAuth requests carry; a
// nested object or array never matches anything, so it reads as empty.
func jsonScalar(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	switch raw[0] {
	case '"':
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			return ""
		}
		return value
	case '{', '[':
		return ""
	}
	return string(raw)
}
