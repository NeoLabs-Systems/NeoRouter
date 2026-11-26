// Package live serves NeoRouter's live (speech-to-speech) voice sessions: a
// WebSocket from a client relayed to a provider's live model, with the same key
// policy, routing, learning and usage log as every other request
// (server/capabilities/live).
//
// The model is chosen when the session opens; if the provider refuses the
// socket, the next candidate is tried, and once the provider has answered the
// session stays with it (a conversation cannot move mid-way). Usage is logged
// when the session ends: the tokens the provider reported, and the session
// length as units (seconds).
//
// Wiring: New(...).Routes(mux) registers the three protocol paths. A request for
// any other path is not this gateway's, so an upgrade the mux does not route is
// refused by the server's fallback handler exactly as index.js destroys the
// socket of an unclaimed upgrade; Claimed reports which paths this package
// serves, for a wiring layer that wants to decide that itself.
package live

import (
	"bytes"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
	"github.com/NeoLabs-Systems/neorouter/internal/providers"
	"github.com/NeoLabs-Systems/neorouter/internal/routing"
)

// Service serves live sessions on the HTTP server's WebSocket upgrades.
type Service struct {
	opts Options
}

// New builds a Service.
func New(opts Options) *Service {
	return &Service{opts: opts}
}

// Options build a Service. Routing, Keys and Accounts are required; every other
// consumer is optional and its absence disables the part that needs it.
type Options struct {
	// Routing ranks the catalog for a session and records what it learned.
	Routing Routing
	// Keys authenticates the router key a session carries and counts its
	// per-minute allowance.
	Keys RouterKeys
	// Accounts resolves a catalog entry's provider connection.
	Accounts Accounts
	// Concurrency counts the key's sessions in flight against its limit
	// (router_keys/key_concurrency.js).
	Concurrency KeySlots
	// Quota counts what a session used against a connection's allowance.
	Quota Quota
	// Budget reads what a key may still spend and books the session's cost.
	Budget Budget
	// Usage writes the request log row.
	Usage Usage
	// PlanSent records a request sent to a prepaid plan
	// (routing plans.onSent). Optional.
	PlanSent func(entry *routing.Entry)
	// ValidateAccount re-checks a connection whose provider refused it
	// (account_service.validateAccount). Optional: called without waiting for it.
	ValidateAccount func(accountID string)
	// Resolve is the resolver the private-host guard uses; nil reads the system
	// resolver. Tests pass a stub.
	Resolve providers.Resolver
	// Log records what an operator needs; prompts, keys and tokens are never
	// logged.
	Log *slog.Logger
	// Now is the clock, time.Now when nil.
	Now func() time.Time
}

// Routes registers the live WebSocket endpoints on mux. The Gemini path is the
// provider's own, so a Gemini client needs no configuration beyond the router's
// address.
func (s *Service) Routes(mux *http.ServeMux) {
	mux.HandleFunc(openAILivePath, s.handle)
	mux.HandleFunc(openAIRealtimePath, s.handle)
	mux.HandleFunc("/ws/google.ai.generativelanguage.v1alpha.GenerativeService.BidiGenerateContent", s.handle)
	mux.HandleFunc("/ws/google.ai.generativelanguage.v1beta.GenerativeService.BidiGenerateContent", s.handle)
}

// Claimed reports whether a path is one this gateway serves. A server that
// routes upgrades itself uses it to close the ones nobody claimed, as
// server/index.js does for an upgrade no handler claimed.
func Claimed(path string) bool { return knownPath(path) }

func (s *Service) log() *slog.Logger {
	if s.opts.Log == nil {
		return slog.Default()
	}
	return s.opts.Log
}

func (s *Service) now() time.Time {
	if s.opts.Now == nil {
		return time.Now()
	}
	return s.opts.Now()
}

// handle serves one upgrade request: authenticate the key, choose the protocol
// and relay the session.
func (s *Service) handle(w http.ResponseWriter, r *http.Request) {
	dialect := dialectFor(r.URL.Path)
	// A plain HTTP request to a live path is not a route the Node server has: it
	// answers 404 like any unknown path.
	if dialect == nil || !isUpgrade(r) {
		httpserver.NotFound(w)
		return
	}
	key, err := s.opts.Keys.Authenticate(tokenOf(r))
	if err != nil {
		// The Node gateway throws here and the session never starts; the client is
		// told the same thing an unknown key is told.
		s.log().Error("Live session: authenticate key", "error", err)
		rejectSocket(w, http.StatusUnauthorized, missingKeyText)
		return
	}
	if key == nil {
		rejectSocket(w, http.StatusUnauthorized, missingKeyText)
		return
	}
	// Pool partners reach chat only.
	if key.PeerLinkID != nil && *key.PeerLinkID != "" {
		rejectSocket(w, http.StatusForbidden, "Pool partners can only use chat models.")
		return
	}
	client, err := acceptClient(w, r)
	if err != nil {
		// acceptClient already answered the request.
		s.log().Warn("Live session: upgrade refused", "path", r.URL.Path, "error", err)
		return
	}
	session := s.newSession(client, r, dialect, key)
	session.run()
}

const missingKeyText = "A valid NeoRouter key (nrt_…) is required."

// tokenOf is tokenOf(request, url): the router key a session carries. Browsers
// cannot set headers on a WebSocket, so the key may also come as ?key= (Gemini
// style) or as the "openai-insecure-api-key.<key>" subprotocol.
func tokenOf(r *http.Request) string {
	if authorization := r.Header.Get("Authorization"); strings.HasPrefix(authorization, "Bearer ") {
		return strings.TrimSpace(authorization[len("Bearer "):])
	}
	header := r.Header.Get("x-api-key")
	if header == "" {
		header = r.Header.Get("x-goog-api-key")
	}
	if header != "" {
		return strings.TrimSpace(header)
	}
	// A query value is taken as written (no trimming), as URLSearchParams does.
	if key := r.URL.Query().Get("key"); key != "" {
		return key
	}
	for _, protocol := range headerTokens(r, "Sec-WebSocket-Protocol") {
		if carried, found := strings.CutPrefix(protocol, insecureKeyProtocol); found {
			return carried
		}
	}
	return ""
}

// rejectSocket is reject(): a raw HTTP answer with a JSON body, then the socket
// is closed without a WebSocket handshake. The Node code writes the status line
// itself, so a 403 carries "Bad Request" as its reason phrase; hijacking keeps
// that byte for byte.
func rejectSocket(w http.ResponseWriter, status int, message string) {
	reason := "Bad Request"
	switch status {
	case http.StatusUnauthorized:
		reason = "Unauthorized"
	case http.StatusTooManyRequests:
		reason = "Too Many Requests"
	}
	body := errorBody(message)
	raw := "HTTP/1.1 " + itoa(status) + " " + reason + "\r\nContent-Type: application/json\r\nConnection: close\r\n\r\n" + body
	if hijacker, ok := w.(http.Hijacker); ok {
		if conn, buffer, err := hijacker.Hijack(); err == nil {
			_, _ = buffer.WriteString(raw)
			_ = buffer.Flush()
			_ = conn.Close()
			return
		}
	}
	// HTTP/2 and test recorders cannot take the connection over: answer the same
	// status and body through the normal writer.
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Connection", "close")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}

// errorBody is JSON.stringify({ error: { message } }).
func errorBody(message string) string {
	var b bytes.Buffer
	b.WriteString(`{"error":{"message":`)
	writeJSONValue(&b, message)
	b.WriteString("}}")
	return b.String()
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	var digits [4]byte
	index := len(digits)
	for value > 0 {
		index--
		digits[index] = byte('0' + value%10)
		value /= 10
	}
	return string(digits[index:])
}

// isUpgrade reports whether the request asks for a WebSocket handshake.
func isUpgrade(r *http.Request) bool {
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		return false
	}
	for _, token := range headerTokens(r, "Connection") {
		if strings.EqualFold(token, "upgrade") {
			return true
		}
	}
	return false
}

// headerTokens is the comma-separated, trimmed values of a header across every
// line it was sent on.
func headerTokens(r *http.Request, name string) []string {
	var tokens []string
	for _, line := range r.Header.Values(name) {
		for _, raw := range strings.Split(line, ",") {
			if token := strings.TrimSpace(raw); token != "" {
				tokens = append(tokens, token)
			}
		}
	}
	return tokens
}
