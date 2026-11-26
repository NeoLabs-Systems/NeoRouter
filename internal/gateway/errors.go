package gateway

// Errors use the dialect of the endpoint, so SDKs surface them properly.

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/NeoLabs-Systems/neolabs-go/httpx"
	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
)

// dialectOpenAIChat and friends are the client dialects the gateway speaks: an
// error is reported in the dialect of the endpoint that failed.
const (
	dialectOpenAIChat        = "openai_chat"
	dialectAnthropicMessages = "anthropic_messages"
	dialectOpenAIResponses   = "openai_responses"
)

// anthropicErrorTypes maps a status to Anthropic's error type.
var anthropicErrorTypes = map[int]string{
	400: "invalid_request_error",
	401: "authentication_error",
	402: "billing_error",
	403: "permission_error",
	404: "not_found_error",
	429: "rate_limit_error",
	529: "overloaded_error",
}

// gatewayError is the error details a gateway response may carry.
type gatewayError struct {
	status  int
	code    string
	message string
	details any
}

// errorOf reads the status, code, message and details of an error the way the
// error handler reads error.status, error.code, error.message and
// error.details.
func errorOf(err error) gatewayError {
	out := gatewayError{status: http.StatusInternalServerError, message: "An internal error occurred."}
	if err == nil {
		return out
	}
	var httpErr *httpserver.HttpError
	if errors.As(err, &httpErr) {
		if httpErr.Status != 0 {
			out.status = httpErr.Status
		}
		out.code = httpErr.Code
		out.message = httpErr.Message
		out.details = httpErr.Details
		return out
	}
	out.message = err.Error()
	return out
}

// openAIErrorType is the OpenAI error type for a status.
func openAIErrorType(status int) string {
	switch {
	case status == http.StatusUnauthorized:
		return "invalid_request_error"
	case status == http.StatusTooManyRequests:
		return "rate_limit_error"
	case status >= 500:
		return "server_error"
	default:
		return "invalid_request_error"
	}
}

// errorBody is {"error":{"message","type","code","details"?}}.
type errorBody struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    any    `json:"code"`
	Details any    `json:"details,omitempty"`
}

type openAIErrorEnvelope struct {
	Error errorBody `json:"error"`
}

type anthropicErrorBody struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

type anthropicErrorEnvelope struct {
	Type      string             `json:"type"`
	Error     anthropicErrorBody `json:"error"`
	RequestID string             `json:"request_id"`
}

// WriteError is gatewayErrorHandler(format): it reports err in the dialect of
// the endpoint, records the failure against the router key (so "waiting for your
// app" can say why a request with a valid key failed early) and logs a 5xx once.
func (s *Service) WriteError(w http.ResponseWriter, r *http.Request, dialect string, key *Key, model string, err error) {
	info := errorOf(err)
	message := info.message
	if info.status >= 500 && info.code == "" {
		message = "An internal error occurred."
		s.log().Error("Gateway request failed", "requestId", httpserver.RequestIDFrom(r.Context()), "error", err)
	}
	if key != nil && s.opts.RecordKeyFailure != nil {
		s.opts.RecordKeyFailure(key.ID(), info.status, info.code, message, model)
	}
	requestID := httpserver.RequestIDFrom(r.Context())
	if dialect == dialectAnthropicMessages {
		kind := anthropicErrorTypes[info.status]
		if kind == "" {
			kind = "api_error"
		}
		httpx.WriteJSON(w, info.status, anthropicErrorEnvelope{
			Type:      "error",
			Error:     anthropicErrorBody{Type: kind, Message: message},
			RequestID: requestID,
		})
		return
	}
	body := errorBody{Message: message, Type: openAIErrorType(info.status), Code: nil}
	if info.code != "" {
		body.Code = info.code
	}
	if info.details != nil {
		body.Details = info.details
	}
	httpx.WriteJSON(w, info.status, openAIErrorEnvelope{Error: body})
}

// writeGatewayError is the plain OpenAI-shaped error the routes write directly
// (validation failures, unsupported parameters).
func writeGatewayError(w http.ResponseWriter, status int, code, message string) {
	body := errorBody{Message: message, Type: openAIErrorType(status), Code: nil}
	if code != "" {
		body.Code = code
	}
	httpx.WriteJSON(w, status, openAIErrorEnvelope{Error: body})
}

// log is the gateway's logger.
func (s *Service) log() *slog.Logger {
	if s.opts.Log != nil {
		return s.opts.Log
	}
	return slog.Default()
}
