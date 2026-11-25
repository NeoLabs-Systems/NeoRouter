package oauth

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/NeoLabs-Systems/neolabs-go/httpx"
	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
)

// OAuthError is an RFC 6749 error: the body an app's OAuth library expects,
// {"error","error_description"}, instead of the management envelope.
type OAuthError struct {
	Status      int    // 400 unless the flow says otherwise
	Code        string // invalid_request, invalid_grant, ...
	Description string
}

func (e *OAuthError) Error() string { return e.Description }

// oauthBody is the JSON body of an OAuth error.
type oauthBody struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

// newOAuthError builds the 400 error app_authorization.js sends.
func newOAuthError(code, description string) *OAuthError {
	return &OAuthError{Status: http.StatusBadRequest, Code: code, Description: description}
}

// writeOAuthError answers with the OAuth shape for an *OAuthError, and with the
// management envelope for anything else (connect.js oauthErrors).
func writeOAuthError(w http.ResponseWriter, r *http.Request, log *slog.Logger, err error) {
	var oauthErr *OAuthError
	if errors.As(err, &oauthErr) {
		status := oauthErr.Status
		if status == 0 {
			status = http.StatusBadRequest
		}
		httpx.WriteJSON(w, status, oauthBody{Error: oauthErr.Code, ErrorDescription: oauthErr.Description})
		return
	}
	httpserver.WriteError(w, r, log, err)
}
