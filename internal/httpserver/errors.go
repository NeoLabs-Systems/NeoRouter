// Package httpserver is NeoRouter's HTTP shell: the JSON error envelope,
// request ids, CORS, sliding-window rate limiting, response compression, body
// limits, and the Flutter web client + landing static handlers.
//
// Everything here matches the Express server (server/app.js and
// server/middleware) byte for byte where the Node code is observable: same
// status codes, same error texts, same header strings.
package httpserver

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"strconv"

	"github.com/NeoLabs-Systems/neolabs-go/httpx"
	"github.com/google/uuid"
)

// HttpError is an error that carries the HTTP status, machine code, message
// and optional details the API returns, like server/middleware/error_handler.js.
type HttpError struct {
	Status     int
	Code       string
	Message    string
	Details    any
	RetryAfter int // seconds; sets Retry-After when non-zero
}

func (e *HttpError) Error() string { return e.Message }

// New builds an *HttpError; details is optional.
func New(status int, code, message string, details ...any) *HttpError {
	e := &HttpError{Status: status, Code: code, Message: message}
	if len(details) > 0 {
		e.Details = details[0]
	}
	return e
}

type ctxKey int

const requestIDKey ctxKey = iota

// RequestIDFrom returns the request id RequestID attached, or "".
func RequestIDFrom(ctx context.Context) string {
	if v, ok := ctx.Value(requestIDKey).(string); ok {
		return v
	}
	return ""
}

// RequestID mirrors server/middleware/request_id.js: the caller's X-Request-Id
// is kept, otherwise a UUID is generated; the id is echoed and stored in the
// request context.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-Id")
		if id == "" {
			id = uuid.NewString()
		}
		w.Header().Set("X-Request-Id", id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey, id)))
	})
}

type errorDetail struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Details   any    `json:"details,omitempty"`
	RequestID string `json:"requestId,omitempty"`
}

type errorEnvelope struct {
	Error errorDetail `json:"error"`
}

// WriteError writes the management error envelope
// {"error":{"code","message","details"?,"requestId"}} of
// server/middleware/error_handler.js: status defaults to 500, code to
// INTERNAL_ERROR, an unknown error's message is masked when the status is 5xx,
// 5xx failures are logged once, and Retry-After is set when the error carries it.
func WriteError(w http.ResponseWriter, r *http.Request, log *slog.Logger, err error) {
	status := http.StatusInternalServerError
	code := "INTERNAL_ERROR"
	msg := "An internal error occurred."
	var details any
	retryAfter := 0

	var he *HttpError
	isHTTPError := errors.As(err, &he)
	if isHTTPError {
		if he.Status != 0 {
			status = he.Status
		}
		if he.Code != "" {
			code = he.Code
		}
		msg = he.Message
		details = he.Details
		retryAfter = he.RetryAfter
	}
	if status >= 500 {
		if !isHTTPError {
			msg = "An internal error occurred."
		}
		if log == nil {
			log = slog.Default()
		}
		var requestID string
		if r != nil {
			requestID = RequestIDFrom(r.Context())
			log.Error("Request failed", "requestId", requestID, "method", r.Method, "path", r.URL.Path, "error", err)
		} else {
			log.Error("Request failed", "error", err)
		}
	}
	if retryAfter > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
	}
	body := errorEnvelope{Error: errorDetail{Code: code, Message: msg, Details: details}}
	if r != nil {
		body.Error.RequestID = RequestIDFrom(r.Context())
	}
	httpx.WriteJSON(w, status, body)
}

// NotFound writes server/middleware/error_handler.js notFound: a 404 with no
// requestId, because the JS handler answers before the error handler runs.
func NotFound(w http.ResponseWriter) {
	httpx.WriteJSON(w, http.StatusNotFound, errorEnvelope{Error: errorDetail{
		Code:    "NOT_FOUND",
		Message: "The requested resource was not found.",
	}})
}

// getenvOr returns a getenv defaulting to the process environment.
func getenvOr(getenv func(string) string) func(string) string {
	if getenv != nil {
		return getenv
	}
	return os.Getenv
}
