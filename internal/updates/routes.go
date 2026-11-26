package updates

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"unicode/utf16"

	"github.com/NeoLabs-Systems/neolabs-go/httpx"
	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
)

// maxChannelBodyBytes is the management JSON limit (2 MB).
const maxChannelBodyBytes = 2 << 20

// Gate wraps a handler with the admin chain: a session, the caller's own space,
// and the admin role. internal/app supplies the auth and teams middleware.
type Gate func(http.Handler) http.Handler

// Routes registers the admin update endpoints. gate may be nil only in tests.
func (s *Service) Routes(mux *http.ServeMux, gate Gate) {
	if gate == nil {
		gate = func(next http.Handler) http.Handler { return next }
	}
	handle := func(pattern string, handler http.HandlerFunc) {
		mux.Handle(pattern, gate(handler))
	}
	handle("GET /api/v1/admin/update", s.handleStatus)
	handle("POST /api/v1/admin/update/check", s.handleCheck)
	handle("POST /api/v1/admin/update", s.handleStart)
	handle("PUT /api/v1/admin/update/channel", s.handleChannel)
}

func (s *Service) handleStatus(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, s.Status())
}

func (s *Service) handleCheck(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, s.Check(r.Context()))
}

func (s *Service) handleStart(w http.ResponseWriter, r *http.Request) {
	status, err := s.Start()
	if err != nil {
		httpserver.WriteError(w, r, s.Log, err)
		return
	}
	httpx.WriteJSON(w, http.StatusAccepted, status)
}

func (s *Service) handleChannel(w http.ResponseWriter, r *http.Request) {
	channel, err := readChannel(w, r)
	if err != nil {
		httpserver.WriteError(w, r, s.Log, err)
		return
	}
	status, err := s.SetChannel(r.Context(), channel)
	if err != nil {
		httpserver.WriteError(w, r, s.Log, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, status)
}

// readChannel validates the body: one string field of at most 16 characters.
func readChannel(w http.ResponseWriter, r *http.Request) (string, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxChannelBodyBytes+1))
	if err != nil || len(body) > maxChannelBodyBytes {
		return "", validationError("", "The request is invalid.")
	}
	values := map[string]any{}
	if trimmed := strings.TrimSpace(string(body)); trimmed != "" {
		if err := json.Unmarshal([]byte(trimmed), &values); err != nil {
			var typeErr *json.UnmarshalTypeError
			if errors.As(err, &typeErr) && strings.HasPrefix(typeErr.Value, "array") {
				return "", validationError("", "Expected object, received array")
			}
			return "", validationError("", "Expected object, received "+receivedType(trimmed))
		}
	}
	value, ok := values["channel"]
	if !ok {
		return "", validationError("channel", "Required")
	}
	channel, ok := value.(string)
	if !ok {
		return "", validationError("channel", "Expected string, received "+receivedTypeOf(value))
	}
	if len(utf16.Encode([]rune(channel))) > 16 {
		return "", validationError("channel", "String must contain at most 16 character(s)")
	}
	return channel, nil
}

// receivedType names what a body held when it was not an object.
func receivedType(trimmed string) string {
	switch {
	case trimmed == "":
		return "undefined"
	case trimmed == "null":
		return "null"
	case strings.HasPrefix(trimmed, "["):
		return "array"
	case strings.HasPrefix(trimmed, `"`):
		return "string"
	case trimmed == "true" || trimmed == "false":
		return "boolean"
	}
	return "number"
}

// receivedTypeOf names a decoded JSON value.
func receivedTypeOf(value any) string {
	switch value.(type) {
	case nil:
		return "null"
	case string:
		return "string"
	case bool:
		return "boolean"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	}
	return "number"
}

// validationError is a 400 VALIDATION_ERROR with the issue flattened into
// details.
func validationError(field, message string) error {
	text := message
	details := map[string]any{
		"formErrors":  []string{},
		"fieldErrors": map[string][]string{},
	}
	if field != "" {
		text = field + ": " + message
		details["fieldErrors"] = map[string][]string{field: {message}}
	}
	return httpserver.New(http.StatusBadRequest, "VALIDATION_ERROR", text, details)
}
