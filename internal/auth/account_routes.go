package auth

import (
	"context"
	"errors"
	"net/http"

	"github.com/NeoLabs-Systems/neolabs-go/httpx"
	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
)

// AccountData is the settings domain's export/delete surface (the JS
// account_data_service). The wiring adapts *settings.AccountData onto it;
// a nil field answers a masked 500, never a partial export.
type AccountData interface {
	ExportData(ctx context.Context, userID, currentPassword, code string) (any, error)
	ExportKeys(ctx context.Context, userID, currentPassword, code string) (any, error)
	DeleteAccount(ctx context.Context, userID, currentPassword, code string) error
}

// accountHandler serves POST /api/v1/auth/account/{export,export-keys,delete}.
// Each asks for the password again (and a 2FA code where the settings domain
// requires one); the route carries its own 5-per-minute limiter, exactly like
// the three accountLimit() instances in server/routes/auth.js.
func (s *Service) accountHandler(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		caller, ok := FromContext(r.Context())
		if !ok || caller == nil {
			httpserver.WriteError(w, r, s.log(), httpserver.New(http.StatusUnauthorized, "AUTHENTICATION_REQUIRED", "A valid session is required."))
			return
		}
		decoded, ok := readBody(w, r)
		if !ok {
			return
		}
		v := &validation{body: decoded}
		password := v.str("currentPassword", 1, 1024)
		code := v.strOptional("code", 64)
		if code != nil && jsLen(*code) < 6 {
			v.add([]string{"code"}, "String must contain at least 6 character(s)")
		}
		if err := v.err(); err != nil {
			httpserver.WriteError(w, r, s.log(), err)
			return
		}
		if s.AccountData == nil {
			httpserver.WriteError(w, r, s.log(), errors.New("auth: account data is not wired"))
			return
		}
		codeText := ""
		if code != nil {
			codeText = *code
		}
		switch kind {
		case "export":
			document, err := s.AccountData.ExportData(r.Context(), caller.UserID, password, codeText)
			if err != nil {
				httpserver.WriteError(w, r, s.log(), err)
				return
			}
			w.Header().Set("Cache-Control", "no-store")
			httpx.WriteJSON(w, http.StatusOK, document)
		case "export-keys":
			document, err := s.AccountData.ExportKeys(r.Context(), caller.UserID, password, codeText)
			if err != nil {
				httpserver.WriteError(w, r, s.log(), err)
				return
			}
			w.Header().Set("Cache-Control", "no-store")
			httpx.WriteJSON(w, http.StatusOK, document)
		case "delete":
			if err := s.AccountData.DeleteAccount(r.Context(), caller.UserID, password, codeText); err != nil {
				httpserver.WriteError(w, r, s.log(), err)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		}
	}
}
