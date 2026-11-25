package live

import (
	"context"
	"math"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/NeoLabs-Systems/neorouter/internal/budget"
	"github.com/NeoLabs-Systems/neorouter/internal/providers"
	"github.com/NeoLabs-Systems/neorouter/internal/routerkeys"
	"github.com/NeoLabs-Systems/neorouter/internal/routing"
	"github.com/coder/websocket"
	"github.com/google/uuid"
)

// trailMessageLimit is the 200 characters the Node code keeps of a failure
// message in the attempt trail and the log.
const trailMessageLimit = 200

// liveTokens is what the provider reported, in tokens.
type liveTokens struct {
	input  float64
	output float64
}

// liveTraffic is the messages each way, for the log: a call where the client
// sent nothing points at the app, one where the provider said nothing at the
// model.
type liveTraffic struct {
	fromClient   int
	fromProvider int
}

// trailSuccess is one attempt that opened a provider socket.
type trailSuccess struct {
	Model   string `json:"model"`
	Outcome string `json:"outcome"`
	MS      int64  `json:"ms"`
}

// trailFailure is one attempt a provider refused, with the kind it failed with.
type trailFailure struct {
	Model   string `json:"model"`
	Outcome string `json:"outcome"`
	// Status is the HTTP status the provider answered, null when it answered
	// with no HTTP at all.
	Status  *int   `json:"status"`
	MS      int64  `json:"ms"`
	Message string `json:"message"`
}

// session is one live voice session: a client's WebSocket relayed to a
// provider's live model (class LiveSession in gateway.js).
//
// Lock order: an upstream's lock may be taken while holding the session's, never
// the other way round (the provider's read path takes the upstream's lock and
// then the session's to relay).
type session struct {
	svc     *Service
	client  *websocket.Conn
	request *http.Request
	url     *url.URL
	dialect *dialect
	key     *routerkeys.Row
	router  routing.RouterKey

	id        string
	startedAt time.Time
	done      chan struct{}
	endOnce   sync.Once
	failMu    sync.Mutex

	mu     sync.Mutex
	closed bool
	// ending marks a deliberate end (a failure, a provider close): the client's
	// own close that follows is not the reason the session ended.
	ending         bool
	answered       bool
	awaitingFirst  bool
	urlModel       bool
	upstream       *upstream
	entry          *routing.Entry
	queue          []pendingMessage
	tokens         liveTokens
	traffic        liveTraffic
	first          *object
	attachedAt     time.Time
	release        func()
	releaseKey     func()
	trail          []any
	lastError      *providers.ProviderError
	requestedModel string
	route          *routing.RoutingPlan
	timer          *time.Timer
}

// newSession builds a session for one accepted client socket.
func (s *Service) newSession(client *websocket.Conn, r *http.Request, d *dialect, key *routerkeys.Row) *session {
	return &session{
		svc:       s,
		client:    client,
		request:   r,
		url:       r.URL,
		dialect:   d,
		key:       key,
		router:    routerKeyOf(key),
		id:        uuid.NewString(),
		startedAt: s.now(),
		done:      make(chan struct{}),
	}
}

// run serves the session until it ends. The model comes from the URL or from the
// opening message.
func (s *session) run() {
	defer s.client.CloseNow()
	model, fromURL := s.dialect.modelFromURL(s.url)
	s.mu.Lock()
	s.urlModel = fromURL
	s.mu.Unlock()
	if fromURL {
		go s.connect(model, nil)
	} else {
		// The model is named in the opening message.
		s.mu.Lock()
		s.awaitingFirst = true
		s.timer = time.AfterFunc(firstMessageTimeout, func() {
			s.fail("The session did not start in time.", "session_timeout")
		})
		s.mu.Unlock()
	}
	s.readClient()
	<-s.done
}

// readClient relays what the client sends until its socket ends.
func (s *session) readClient() {
	for {
		kind, data, err := s.client.Read(context.Background())
		if err != nil {
			s.clientGone(err)
			return
		}
		s.clientMessage(kind == websocket.MessageBinary, data)
	}
}

// clientGone ends the session the way the Node socket events do: a close says
// who closed it, a failure says the socket broke.
func (s *session) clientGone(err error) {
	s.mu.Lock()
	ending := s.ending
	s.mu.Unlock()
	if ending {
		// The session is being ended on purpose; its own reason wins.
		return
	}
	code, reason := closeError(err)
	if code == -1 {
		s.end("client_error", "", 0, "")
		return
	}
	s.end("client_closed", "", code, reason)
}

// clientMessage forwards a client message, or opens the session when it is the
// opening one.
func (s *session) clientMessage(binary bool, data []byte) {
	s.mu.Lock()
	awaitingFirst := s.awaitingFirst
	if awaitingFirst {
		s.awaitingFirst = false
	}
	s.mu.Unlock()
	if awaitingFirst {
		s.beginFirstMessage(data)
		return
	}
	s.forward(binary, data)
}

// beginFirstMessage reads the model out of the opening message and connects.
func (s *session) beginFirstMessage(data []byte) {
	s.mu.Lock()
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
	s.traffic.fromClient++
	s.mu.Unlock()
	// An unparseable message names no session.
	first, _ := parseObject(data)
	model, opens := s.dialect.modelFromFirst(first)
	if !opens {
		s.fail("The first message must open the session and name the model.", "invalid_request")
		return
	}
	go s.connect(model, first)
}

// forward sends a client message to the provider and keeps a copy until the
// provider answers, so a provider that hangs up right away can be replaced by the
// next one.
func (s *session) forward(binary bool, data []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.traffic.fromClient++
	if s.upstream != nil {
		s.writeUpstream(binary, data)
	}
	if !s.answered {
		s.queue = append(s.queue, pendingMessage{binary: binary, data: data})
	}
}

// relay passes one provider message to the client, counting what it reports.
func (s *session) relay(binary bool, data []byte) {
	s.mu.Lock()
	if !s.answered {
		s.answered = true
		s.queue = nil
	}
	s.traffic.fromProvider++
	input, output, reported := countUsage(binary, data)
	if reported {
		s.tokens.input += input
		s.tokens.output += output
	}
	entry := s.entry
	closed := s.closed
	s.mu.Unlock()
	if reported {
		s.svc.recordTokens(entry, int64(input+output), s.svc.now())
	}
	if !closed {
		s.writeClient(binary, data)
	}
}

// countUsage reads the token counts a provider message reports. Gemini sends its
// JSON as binary frames; audio-only binary frames do not parse and are skipped.
func countUsage(binary bool, data []byte) (float64, float64, bool) {
	if binary && (len(data) == 0 || data[0] != '{') {
		return 0, 0, false
	}
	event, ok := parseObject(data)
	if !ok {
		return 0, 0, false
	}
	return usageOf(event)
}

// fail tells the client what went wrong and ends the session: the protocol's own
// error event, then a close with the message as its reason.
func (s *session) fail(message, code string) {
	s.failMu.Lock()
	defer s.failMu.Unlock()
	if !s.claimEnding() {
		return
	}
	s.mu.Lock()
	closed := s.closed
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
	s.mu.Unlock()
	if !closed {
		if s.dialect.errorEvent != nil {
			if encoded, err := marshalObject(s.dialect.errorEvent(message, code)); err == nil {
				s.writeClient(false, encoded)
			}
		}
		s.closeClient(websocket.StatusInternalError, closeReason(message))
	}
	s.end(code, message, 0, "")
}

// end finishes the session once: it releases what the session held, books what it
// cost and writes the request log row.
func (s *session) end(reason, errorMessage string, code int, closeReason string) {
	s.endOnce.Do(func() {
		s.mu.Lock()
		s.closed = true
		if s.timer != nil {
			s.timer.Stop()
			s.timer = nil
		}
		release, releaseKey := s.release, s.releaseKey
		s.release, s.releaseKey = nil, nil
		up := s.upstream
		entry := s.entry
		tokens, traffic := s.tokens, s.traffic
		trail := s.trail
		requestedModel := s.requestedModel
		s.mu.Unlock()

		now := s.svc.now()
		seconds := int64(math.Round(now.Sub(s.startedAt).Seconds()))
		fields := []any{
			"sessionId", s.id, "model", nil, "by", reason, "seconds", seconds,
		}
		if entry != nil {
			fields[3] = entry.ID
		}
		if code != 0 {
			fields = append(fields, "code", code, "closeReason", truncateText(closeReason, trailMessageLimit))
		}
		if errorMessage != "" {
			fields = append(fields, "error", truncateText(errorMessage, trailMessageLimit))
		}
		// Field names with "token" in them are redacted by the Node logger.
		fields = append(fields, "usageIn", tokens.input, "usageOut", tokens.output,
			"fromClient", traffic.fromClient, "fromProvider", traffic.fromProvider)
		s.svc.log().Info("Live session ended", fields...)

		if release != nil {
			release()
		}
		if releaseKey != nil {
			releaseKey()
		}
		if up != nil {
			up.close()
		}
		cost := 0.0
		if entry != nil {
			cost = budget.CostOf(priceOf(entry), &budget.Usage{
				PromptTokens:     int64(tokens.input),
				CompletionTokens: int64(tokens.output),
			})
			s.svc.settle(s.key, cost, s.id)
		}
		s.recordUsage(entry, reason, errorMessage, requestedModel, trail, tokens, cost, seconds, now)
		close(s.done)
	})
}

// releaseLease gives the attempt's in-flight slot back.
func (s *session) releaseLease() {
	s.mu.Lock()
	release := s.release
	s.release = nil
	s.mu.Unlock()
	if release != nil {
		release()
	}
}

// pushTrail appends one attempt to the trail.
func (s *session) pushTrail(entry any) {
	s.mu.Lock()
	s.trail = append(s.trail, entry)
	s.mu.Unlock()
}

// claimEnding marks that the session is being ended on purpose, so the client's
// close that follows is not taken for the reason. False when another path is
// already ending the session.
func (s *session) claimEnding() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ending || s.closed {
		return false
	}
	s.ending = true
	return true
}

// isClosed reports whether the session has ended.
func (s *session) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

// writeUpstream sends one message to the provider; the caller holds the session's
// lock so the opening message and the client's messages keep their order.
func (s *session) writeUpstream(binary bool, data []byte) {
	up := s.upstream
	if up == nil {
		return
	}
	if err := up.write(binary, data); err != nil {
		s.svc.log().Debug("Live session: write to provider", "sessionId", s.id, "error", err)
	}
}

// writeClient sends one message to the client.
func (s *session) writeClient(binary bool, data []byte) {
	kind := websocket.MessageText
	if binary {
		kind = websocket.MessageBinary
	}
	ctx, cancel := writeTimeoutContext()
	defer cancel()
	if err := s.client.Write(ctx, kind, data); err != nil {
		s.svc.log().Debug("Live session: write to client", "sessionId", s.id, "error", err)
	}
}

// closeClient closes the client's socket with a code and reason.
func (s *session) closeClient(code websocket.StatusCode, reason string) {
	_ = s.client.Close(code, closeReason(reason))
}

// statusOf is the HTTP status a failure carried, nil when it had none (the Node
// error's status is null then).
func statusOf(failure *providers.ProviderError) *int {
	if failure.Status == 0 {
		return nil
	}
	status := failure.Status
	return &status
}

// closeCodeFor is the code the client is closed with: a socket that ended with
// no code of its own ends the client's session abnormally (1005 and 1006 are not
// close codes that may be sent).
func closeCodeFor(code int) websocket.StatusCode {
	switch code {
	case 1000, 1001, 1002, 1003, 1007, 1008, 1009, 1010, 1011:
		return websocket.StatusCode(code)
	}
	if code >= 3000 && code <= 4999 {
		return websocket.StatusCode(code)
	}
	return websocket.StatusInternalError
}

// closeReason keeps a close reason inside the 123 bytes the protocol allows and
// valid UTF-8, as Node's Buffer slice produces replacement characters.
func closeReason(message string) string {
	if len(message) > closeReasonLimit {
		message = message[:closeReasonLimit]
	}
	return strings.ToValidUTF8(message, "\uFFFD")
}

// truncateText keeps the first limit characters of a message (String.slice).
func truncateText(message string, limit int) string {
	runes := []rune(message)
	if len(runes) <= limit {
		return message
	}
	return string(runes[:limit])
}
