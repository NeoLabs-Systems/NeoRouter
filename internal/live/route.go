package live

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
	"github.com/NeoLabs-Systems/neorouter/internal/providers"
	"github.com/NeoLabs-Systems/neorouter/internal/routing"
	"github.com/coder/websocket"
)

// connect claims the key's capacity for the session and starts routing.
func (s *session) connect(requestedModel string, first *object) {
	s.mu.Lock()
	s.requestedModel = requestedModel
	s.first = first
	s.mu.Unlock()
	if s.svc.opts.Concurrency != nil {
		release, err := s.svc.opts.Concurrency.Acquire(s.key)
		if err != nil {
			var httpError *httpserver.HttpError
			code := "rate_limited"
			if errors.As(err, &httpError) && httpError.Code != "" {
				code = strings.ToLower(httpError.Code)
			}
			s.fail(err.Error(), code)
			return
		}
		s.mu.Lock()
		s.releaseKey = release
		s.mu.Unlock()
	}
	rpm := s.svc.opts.Keys.ConsumeRPM(s.key, s.svc.now())
	if !rpm.Allowed {
		s.fail("This router key exceeded its requests-per-minute limit.", "rate_limited")
		return
	}
	plan, err := s.plan(requestedModel)
	if err != nil {
		// When no routing plan can be built, the client is told why instead of
		// waiting for nothing.
		s.fail(err.Error(), "invalid_request")
		return
	}
	s.mu.Lock()
	s.route = plan
	s.lastError = nil
	s.mu.Unlock()
	s.tryNext()
}

// plan builds the session's route through the catalog: routing prices it at zero
// (a live session spends no tokens) and picks the models the key may use.
func (s *session) plan(requestedModel string) (*routing.RoutingPlan, error) {
	remaining := s.svc.remaining(s.key, s.svc.now())
	exhausted := remaining != nil && *remaining <= 0
	return s.svc.opts.Routing.NewPlan(routing.PlanOptions{
		UserID: s.key.UserID,
		Key:    s.router,
		// A live session carries no prompt to read, so routing classifies nothing.
		Request:        routing.RequestInfo{},
		RequestedModel: requestedModel,
		Needs:          routing.Needs{},
		InputTokens:    0,
		FreeOnly:       exhausted,
		Capability:     "live",
		EstimateCost:   func(*routing.Entry) *float64 { return floatPtrOf(0) },
	})
}

// supported reports whether an entry's provider speaks this session's protocol.
func (s *session) supported(entry *routing.Entry) bool {
	connection, ok := s.svc.opts.Accounts.Runtime(entry.AccountID)
	return ok && connection.Speaks(s.dialect.id)
}

// tryNext tries the next candidate until one answers or the attempts run out.
func (s *session) tryNext() {
	for {
		s.mu.Lock()
		attempts := len(s.trail)
		closed := s.closed
		plan := s.route
		s.mu.Unlock()
		if attempts >= maxAttempts || closed {
			break
		}
		choice := plan.Next(routing.NextOptions{Filter: s.supported, Sample: true})
		if choice == nil {
			break
		}
		entry := choice.Entry
		connection, ok := s.svc.opts.Accounts.Runtime(entry.AccountID)
		if !ok {
			// The connection went away between choosing and opening it.
			continue
		}
		attemptStarted := s.svc.now()
		release := s.svc.opts.Routing.Lease(entry)
		s.mu.Lock()
		s.release = release
		s.mu.Unlock()
		s.svc.recordRequest(entry, attemptStarted)
		if s.svc.opts.PlanSent != nil {
			s.svc.opts.PlanSent(entry)
		}
		target := s.dialect.upstream(connection, entry.ModelID, s.request)
		up, err := openUpstream(target, connection.Account.PublicOnly, s.svc.opts.Resolve)
		if err != nil {
			// A refused socket: count the attempt and try the next candidate.
			failure := providerErrorOf(err)
			s.mu.Lock()
			closed := s.closed
			s.mu.Unlock()
			retry := !closed && s.failed(entry, failure, attemptStarted)
			s.releaseLease()
			if !retry {
				break
			}
			continue
		}
		if s.isClosed() {
			up.close()
			return
		}
		go s.readProvider(up)
		elapsed := s.svc.now().Sub(attemptStarted)
		s.pushTrail(trailSuccess{Model: entry.ID, Outcome: "success", MS: elapsed.Milliseconds()})
		s.svc.opts.Routing.OnSuccess(entry, routing.Measurement{
			LatencyMs:    floatPtrOf(float64(elapsed.Milliseconds())),
			OutputTokens: 0,
			GenerationMs: 0,
		}, nil, s.svc.now())
		s.attach(up, entry)
		return
	}
	s.describeFailure()
}

// describeFailure says what stopped the session. Models of other protocols are
// not counted as rejected.
func (s *session) describeFailure() {
	s.mu.Lock()
	attempts := len(s.trail)
	lastError := s.lastError
	plan := s.route
	requestedModel := s.requestedModel
	s.mu.Unlock()
	if attempts > 0 {
		message := "unknown"
		if lastError != nil && lastError.Message != "" {
			message = lastError.Message
		}
		s.fail(fmt.Sprintf("All %d attempt(s) failed. Last error: %s", attempts, message), "all_attempts_failed")
		return
	}
	rejected := plan.RejectionSummary(time.Time{}, nil)
	if len(rejected) > 0 {
		s.fail(routing.ExplainNoModel(rejected, "live"), "no_model_available")
		return
	}
	s.fail(fmt.Sprintf("No live model in your catalog can serve \"%s\" over this protocol.", requestedModel), "no_model_available")
}

// failed records a failed attempt and reports whether another candidate makes
// sense.
func (s *session) failed(entry *routing.Entry, failure *providers.ProviderError, attemptStarted time.Time) bool {
	elapsed := s.svc.now().Sub(attemptStarted)
	s.mu.Lock()
	s.lastError = failure
	s.trail = append(s.trail, trailFailure{
		Model:   entry.ID,
		Outcome: string(failure.Kind),
		Status:  statusOf(failure),
		MS:      elapsed.Milliseconds(),
		Message: truncateText(failure.Message, trailMessageLimit),
	})
	plan := s.route
	s.mu.Unlock()
	policy := s.svc.opts.Routing.OnFailure(entry, routingFailure(failure), routing.FailureOptions{EstimatedTokens: 0})
	plan.NoteFailure(entry, routing.Kind(failure.Kind))
	if failure.Kind == providers.KindAuth && s.svc.opts.ValidateAccount != nil {
		accountID := entry.AccountID
		go s.svc.opts.ValidateAccount(accountID)
	}
	return policy.Fallback
}

// attach wires the session to a provider socket that accepted it: the opening
// message with the chosen model, then what the client sent meanwhile.
func (s *session) attach(up *upstream, entry *routing.Entry) {
	s.mu.Lock()
	s.upstream = up
	s.entry = entry
	s.attachedAt = s.svc.now()
	first := s.first
	queue := s.queue
	if first != nil {
		if err := up.writeJSON(s.dialect.rewriteFirst(first, entry.ModelID)); err != nil {
			s.svc.log().Debug("Live session: write opening message", "model", entry.ID, "error", err)
		}
	}
	for _, message := range queue {
		s.writeUpstream(message.binary, message.data)
	}
	s.mu.Unlock()
	up.attach(func(binary bool, data []byte) { s.relay(binary, data) })
	// A provider that hung up before the session was wired is replaced, as one
	// that hangs up right after it.
	if closed, code, reason := up.closedState(); closed {
		s.providerClosed(up, code, reason)
	}
}

// providerClosed handles a provider socket that ended.
func (s *session) providerClosed(up *upstream, code int, reason string) {
	s.mu.Lock()
	current := s.upstream
	answered := s.answered
	closed := s.closed
	entry := s.entry
	s.mu.Unlock()
	if current != up {
		return
	}
	if !answered && !closed {
		s.hungUp(up, entry, code, reason)
		return
	}
	if !closed {
		if !s.claimEnding() {
			return
		}
		s.closeClient(closeCodeFor(code), reason)
	}
	s.end("provider_closed", "", code, reason)
}

// readProvider relays what the provider sends until its socket ends.
func (s *session) readProvider(up *upstream) {
	for {
		kind, data, err := up.conn.Read(context.Background())
		if err != nil {
			code, reason := closeError(err)
			up.noteClosed(code, reason)
			s.providerClosed(up, code, reason)
			return
		}
		up.deliver(kind == websocket.MessageBinary, data)
	}
}

// hungUp replaces a provider that closed before saying anything (Gemini does
// this for a model it cannot serve): the session moves on to the next candidate.
func (s *session) hungUp(up *upstream, entry *routing.Entry, code int, reason string) {
	s.mu.Lock()
	if s.upstream == up {
		s.upstream = nil
		s.entry = nil
	}
	attachedAt := s.attachedAt
	if len(s.trail) > 0 {
		s.trail = s.trail[:len(s.trail)-1]
	}
	s.mu.Unlock()
	s.releaseLease()
	kind := providers.KindServer
	if code == int(websocket.StatusPolicyViolation) {
		kind = providers.KindNotFound
	}
	message := reason
	if message == "" {
		message = fmt.Sprintf("The provider closed the session (code %d).", code)
	}
	failure := providers.NewError(kind, message, providers.ErrorOptions{})
	if s.failed(entry, failure, attachedAt) {
		go s.tryNext()
		return
	}
	s.fail("The provider closed the session: "+message, "provider_closed")
}
