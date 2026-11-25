package httpserver

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/NeoLabs-Systems/neolabs-go/httpx"
)

// Body limits from server/app.js. Express reads "2mb" and "36mb" through
// bytes(), which counts 1024-byte kilobytes.
const (
	// DefaultJSONBodyLimit is app.js express.json({limit:'2mb'}) for /api/v1.
	DefaultJSONBodyLimit int64 = 2 << 20
	// PlaygroundMediaBodyLimit is app.js express.json({limit:'36mb'}) for
	// /api/v1/playground/media (base64 audio uploads).
	PlaygroundMediaBodyLimit int64 = 36 << 20
)

type openAIErrorDetail struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    string `json:"code"`
}

type openAIErrorEnvelope struct {
	Error openAIErrorDetail `json:"error"`
}

// WriteGatewayError writes the OpenAI-shaped error body the gateway SDKs parse:
// {"error":{"message","type","code"}}.
func WriteGatewayError(w http.ResponseWriter, status int, code, message string) {
	httpx.WriteJSON(w, status, openAIErrorEnvelope{Error: openAIErrorDetail{
		Message: message,
		Type:    "invalid_request_error",
		Code:    code,
	}})
}

// ReadJSON decodes a gateway (/v1) request body and answers failures in the
// OpenAI error shape: 400 INVALID_JSON "The request body is not valid JSON."
// and 413 BODY_TOO_LARGE "The request body is too large.".
//
// It returns nil when the body was accepted, and the written error otherwise,
// so a handler is `if err := httpserver.ReadJSON(...); err != nil { return }`.
// A body that is not JSON by Content-Type is left unparsed, exactly as
// body-parser skips it, and nil is returned.
func ReadJSON(w http.ResponseWriter, r *http.Request, dst any, maxBytes int64) error {
	err := decodeBody(w, r, dst, maxBytes)
	if err == nil {
		return nil
	}
	if errors.Is(err, errBodyTooLarge) {
		WriteGatewayError(w, http.StatusRequestEntityTooLarge, "BODY_TOO_LARGE", "The request body is too large.")
		return err
	}
	WriteGatewayError(w, http.StatusBadRequest, "INVALID_JSON", "The request body is not valid JSON.")
	return err
}

// ReadJSONManagement decodes a management request body (the routes behind
// app.js express.json) and answers failures in the management envelope
// {"error":{"code","message","requestId"}}. The status codes and codes match
// the gateway; the message of a parse failure is the fixed text because the
// JS server forwarded V8's parser message, which has no Go equivalent.
func ReadJSONManagement(w http.ResponseWriter, r *http.Request, dst any, maxBytes int64) error {
	err := decodeBody(w, r, dst, maxBytes)
	if err == nil {
		return nil
	}
	if errors.Is(err, errBodyTooLarge) {
		WriteError(w, r, nil, New(http.StatusRequestEntityTooLarge, "BODY_TOO_LARGE", "The request body is too large."))
		return err
	}
	WriteError(w, r, nil, New(http.StatusBadRequest, "INVALID_JSON", "The request body is not valid JSON."))
	return err
}

var errBodyTooLarge = errors.New("httpserver: request body too large")

// decodeBody reads and decodes a JSON request body the way body-parser does:
// an absent or empty body is accepted, a body whose Content-Type is not JSON is
// skipped, an oversized body is errBodyTooLarge, and malformed JSON is an error.
func decodeBody(w http.ResponseWriter, r *http.Request, dst any, maxBytes int64) error {
	if r.Body == nil || !jsonContentType(r.Header.Get("Content-Type")) {
		return nil
	}
	reader := io.Reader(r.Body)
	if maxBytes > 0 {
		reader = http.MaxBytesReader(w, r.Body, maxBytes)
	}
	raw, err := io.ReadAll(reader)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return errBodyTooLarge
		}
		return err
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil
	}
	// body-parser runs in strict mode: only objects and arrays are accepted.
	if trimmed[0] != '{' && trimmed[0] != '[' {
		return errors.New("httpserver: request body is not a JSON object or array")
	}
	if err := json.Unmarshal(trimmed, dst); err != nil {
		return err
	}
	return nil
}

func jsonContentType(contentType string) bool {
	ct := strings.ToLower(strings.TrimSpace(contentType))
	if i := strings.Index(ct, ";"); i >= 0 {
		ct = strings.TrimSpace(ct[:i])
	}
	switch ct {
	case "", "application/json", "text/json":
		return true
	}
	return strings.HasSuffix(ct, "+json")
}
