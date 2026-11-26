package gateway

// The /v1 routes and the playground chat the web app uses.

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
)

// maxBodyBytes is the gateway's body limit: config.MaxBodyBytes, set by the
// caller of mountChatRoutes through the Service's config.
func (s *Service) maxBodyBytes() int64 {
	if limit := s.cfg().MaxBodyBytes; limit > 0 {
		return limit
	}
	return 32 << 20
}

// readBody decodes a gateway request body keeping JSON key order, and answers
// failures with 400 INVALID_JSON and 413 BODY_TOO_LARGE, in the OpenAI error
// shape. A body that is not JSON by Content-Type is left unparsed, as
// body-parser skips it.
func (s *Service) readBody(w http.ResponseWriter, r *http.Request) (*Obj, error) {
	body, err := readOrderedBody(w, r, s.maxBodyBytes())
	if err != nil {
		return nil, err
	}
	return body, nil
}

// readOrderedBody is readBody without a Service.
func readOrderedBody(w http.ResponseWriter, r *http.Request, maxBytes int64) (*Obj, error) {
	empty := newObj()
	if r.Body == nil || !jsonContentType(r.Header.Get("Content-Type")) {
		return empty, nil
	}
	reader := io.Reader(r.Body)
	if maxBytes > 0 {
		reader = http.MaxBytesReader(w, r.Body, maxBytes)
	}
	raw, err := io.ReadAll(reader)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeGatewayError(w, http.StatusRequestEntityTooLarge, "BODY_TOO_LARGE", "The request body is too large.")
			return nil, err
		}
		writeGatewayError(w, http.StatusBadRequest, "INVALID_JSON", "The request body is not valid JSON.")
		return nil, err
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return empty, nil
	}
	// body-parser runs in strict mode: only objects and arrays are accepted.
	if trimmed[0] != '{' && trimmed[0] != '[' {
		writeGatewayError(w, http.StatusBadRequest, "INVALID_JSON", "The request body is not valid JSON.")
		return nil, errors.New("gateway: request body is not a JSON object or array")
	}
	parsed, err := parseOrdered(trimmed)
	if err != nil {
		writeGatewayError(w, http.StatusBadRequest, "INVALID_JSON", "The request body is not valid JSON.")
		return nil, err
	}
	object, _ := parsed.(*Obj)
	if object == nil {
		return empty, nil
	}
	return object, nil
}

// jsonContentType reports whether a Content-Type is JSON, as body-parser reads
// it.
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

// mountChatRoutes registers the /v1 endpoints on mux. They are mounted outside
// the compression middleware by internal/app, because they stream.
func (s *Service) mountChatRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/models", s.withKey(dialectOpenAIChat, s.handleModels))
	mux.HandleFunc("GET /v1/models/{model...}", s.withKey(dialectOpenAIChat, s.handleModel))
	mux.HandleFunc("GET /v1/limits", s.withKey(dialectOpenAIChat, s.handleLimits))
	mux.HandleFunc("POST /v1/feedback", s.withKey(dialectOpenAIChat, s.handleFeedback))
	mux.HandleFunc("POST /v1/chat/completions", s.withKey(dialectOpenAIChat, func(w http.ResponseWriter, r *http.Request, key *Key) {
		s.handleChat(w, r, key, dialectOpenAIChat, false)
	}))
	mux.HandleFunc("POST /v1/messages", s.withKey(dialectAnthropicMessages, func(w http.ResponseWriter, r *http.Request, key *Key) {
		s.handleChat(w, r, key, dialectAnthropicMessages, false)
	}))
	mux.HandleFunc("POST /v1/responses", s.withKey(dialectOpenAIResponses, func(w http.ResponseWriter, r *http.Request, key *Key) {
		s.handleChat(w, r, key, dialectOpenAIResponses, false)
	}))
	mux.HandleFunc("POST /v1/messages/count_tokens", s.withKey(dialectOpenAIChat, s.handleCountTokens))
}

// handleChat is one POST of a chat dialect.
func (s *Service) handleChat(w http.ResponseWriter, r *http.Request, key *Key, dialect string, freeOnly bool) {
	body, err := s.readBody(w, r)
	if err != nil {
		return
	}
	s.Execute(w, r, key, dialect, body, freeOnly)
}

// --- playground -------------------------------------------------------------

// Space is the caller's space as the playground route needs it: whose key the
// chat runs on.
type Space struct {
	OwnerID string
}

// mountPlaygroundRoutes registers the web app's playground chat.
func (s *Service) mountPlaygroundRoutes(mux *http.ServeMux) {
	var handler http.Handler = http.HandlerFunc(s.handlePlaygroundChat)
	if s.opts.RequirePermission != nil {
		handler = s.opts.RequirePermission("playground.use")(handler)
	}
	mux.Handle("POST /api/v1/playground/chat", handler)
}

// playgroundRequest is the validated body of POST /api/v1/playground/chat.
type playgroundRequest struct {
	KeyID       string
	Model       string
	Messages    []any
	MaxTokens   *int
	Temperature *float64
	FreeOnly    bool
}

// handlePlaygroundChat is POST /api/v1/playground/chat: a chat through the real
// gateway pipeline with one of the user's keys, so the dashboard can test routing
// without ever holding a key secret. Its errors use the management envelope.
func (s *Service) handlePlaygroundChat(w http.ResponseWriter, r *http.Request) {
	body, err := readOrderedBody(w, r, httpserver.DefaultJSONBodyLimit)
	if err != nil {
		return
	}
	request, validationErr := validatePlayground(body)
	if validationErr != nil {
		httpserver.WriteError(w, r, s.log(), validationErr)
		return
	}
	if s.opts.Space == nil || s.opts.Keys == nil {
		httpserver.WriteError(w, r, s.log(), httpserver.New(http.StatusInternalServerError, "INTERNAL_ERROR", "An internal error occurred."))
		return
	}
	space, ok := s.opts.Space(r.Context())
	if !ok {
		httpserver.WriteError(w, r, s.log(), httpserver.New(http.StatusUnauthorized, "UNAUTHENTICATED", "Sign in to use the playground."))
		return
	}
	row, err := s.opts.Keys.Row(space.OwnerID, request.KeyID)
	if err != nil {
		httpserver.WriteError(w, r, s.log(), httpserver.New(http.StatusNotFound, "NOT_FOUND", "The requested resource was not found."))
		return
	}
	if row == nil {
		httpserver.WriteError(w, r, s.log(), httpserver.New(http.StatusNotFound, "NOT_FOUND", "The requested resource was not found."))
		return
	}
	if row.RevokedAt != nil {
		httpserver.WriteError(w, r, s.log(), httpserver.New(http.StatusBadRequest, "KEY_REVOKED", "This router key is revoked."))
		return
	}
	chat := newObjOf(
		"model", request.Model,
		"messages", request.Messages,
		"stream", false,
	)
	if request.MaxTokens != nil {
		chat.Set("max_tokens", float64(*request.MaxTokens))
	}
	if request.Temperature != nil {
		chat.Set("temperature", *request.Temperature)
	}
	run := executeRun{
		service:    s,
		w:          w,
		r:          r,
		key:        keyFromRow(row),
		dialect:    dialectOpenAIChat,
		body:       chat,
		freeOnly:   request.FreeOnly,
		management: true,
	}
	if err := run.execute(); err != nil {
		httpserver.WriteError(w, r, s.log(), err)
	}
}

// validatePlayground is the route's schema: the fields, their limits and the
// first problem, worded as the API words a validation failure.
func validatePlayground(body *Obj) (playgroundRequest, error) {
	out := playgroundRequest{Model: "auto"}
	keyID, ok := get(body, "keyId")
	if !ok {
		return out, validationError("keyId", "Required")
	}
	text, isText := keyID.(string)
	if !isText {
		return out, validationError("keyId", "Expected string, received "+jsTypeName(keyID))
	}
	if len(text) < 1 {
		return out, validationError("keyId", "String must contain at least 1 character(s)")
	}
	out.KeyID = text
	if model, ok := get(body, "model"); ok {
		modelText, isString := model.(string)
		if !isString {
			return out, validationError("model", "Expected string, received "+jsTypeName(model))
		}
		if utf16Len(modelText) > 300 {
			return out, validationError("model", "String must contain at most 300 character(s)")
		}
		out.Model = modelText
	}
	messages := getList(body, "messages")
	if _, ok := get(body, "messages"); !ok {
		return out, validationError("messages", "Required")
	}
	if _, isList := getValue(body, "messages").([]any); !isList {
		return out, validationError("messages", "Expected array, received "+jsTypeName(getValue(body, "messages")))
	}
	if len(messages) < 1 {
		return out, validationError("messages", "Array must contain at least 1 element(s)")
	}
	if len(messages) > 200 {
		return out, validationError("messages", "Array must contain at most 200 element(s)")
	}
	out.Messages = messages
	for index, item := range messages {
		message := asObj(item)
		prefix := "messages." + jsString(float64(index)) + "."
		role := getString(message, "role")
		if role != "system" && role != "user" && role != "assistant" {
			return out, validationError(prefix+"role", "Invalid enum value. Expected 'system' | 'user' | 'assistant', received "+jsQuote(role))
		}
		content, ok := get(message, "content")
		if !ok {
			return out, validationError(prefix+"content", "Required")
		}
		contentText, isString := content.(string)
		if !isString {
			return out, validationError(prefix+"content", "Expected string, received "+jsTypeName(content))
		}
		if utf16Len(contentText) > 200000 {
			return out, validationError(prefix+"content", "String must contain at most 200000 character(s)")
		}
	}
	if value, ok := getNumber(body, "max_tokens"); ok {
		if value != float64(int(value)) {
			return out, validationError("max_tokens", "Expected integer, received float")
		}
		if value <= 0 {
			return out, validationError("max_tokens", "Number must be greater than 0")
		}
		if value > 32000 {
			return out, validationError("max_tokens", "Number must be less than or equal to 32000")
		}
		tokens := int(value)
		out.MaxTokens = &tokens
	}
	if value, ok := getNumber(body, "temperature"); ok {
		if value < 0 {
			return out, validationError("temperature", "Number must be greater than or equal to 0")
		}
		if value > 2 {
			return out, validationError("temperature", "Number must be less than or equal to 2")
		}
		temperature := value
		out.Temperature = &temperature
	}
	out.FreeOnly = getBool(body, "freeOnly")
	return out, nil
}

// validationError is the route's 400 VALIDATION_ERROR, worded as the API words
// it: "path: message".
func validationError(path, message string) error {
	return httpserver.New(http.StatusBadRequest, "VALIDATION_ERROR", path+": "+message)
}

// jsTypeName is the word validation messages use for a value's type.
func jsTypeName(value any) string {
	switch value.(type) {
	case nil:
		return "null"
	case string:
		return "string"
	case bool:
		return "boolean"
	case float64:
		return "number"
	case []any:
		return "array"
	case *Obj:
		return "object"
	}
	return "unknown"
}

// jsQuote is a value in the quotes validation messages use.
func jsQuote(text string) string { return "'" + text + "'" }
