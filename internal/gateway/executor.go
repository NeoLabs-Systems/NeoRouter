package gateway

// One chat request, from the client's dialect through routing, quota, budget and
// usage, to the answer.

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
	"github.com/NeoLabs-Systems/neorouter/internal/providers"
	"github.com/NeoLabs-Systems/neorouter/internal/routing"
	"github.com/NeoLabs-Systems/neorouter/internal/routing/task"
	"github.com/google/uuid"
)

const (
	// streamIdleTimeoutMS is how long a stream may go silent before the attempt
	// is abandoned.
	streamIdleTimeoutMS = 120_000
	// hedgeFactor and the hedge bounds decide when a slow free attempt gets a
	// second one alongside it.
	hedgeFactor      = 3
	hedgeMinStreamMs = 2500
	hedgeMinPlainMs  = 4000
	hedgeMaxMs       = 30_000
	hedgeMinSamples  = 3
	hedgeRetryMs     = 2000
	// queuePollMs and queueMinPollMs pace the wait for a busy candidate.
	queuePollMs    = 1000
	queueMinPollMs = 200
	// peerQueueMs is how long a pool partner's request waits for capacity: the
	// owner's own requests come first.
	peerQueueMs = 2000
	// maxConsecutiveBadRequests ends the request when the body itself is the
	// problem.
	maxConsecutiveBadRequests = 3
)

// keyPolicyRejections are the rejections that mean "this key may never use this
// model", which is a permission error rather than a temporary outage.
var keyPolicyRejections = map[string]bool{"not_free": true, "not_allowed_for_key": true, "price_unknown": true}

// permanentRejections are rejections that do not change within the lifetime of
// one request.
var permanentRejections = map[string]bool{
	"needs_action": true, "model_disabled": true, "model_unavailable": true, "account_unavailable": true,
	"no_tools": true, "no_vision": true, "not_available": true, "no_free_quota": true, "incompatible": true,
	"payment_required": true, "auth_failed": true, "model_excluded": true, "provider_excluded": true,
	"provider_not_included": true, "below_min_intelligence": true, "too_expensive": true,
	"budget_exhausted": true, "needs_more_tokens": true,
}

// translatorFor is the translator of one client dialect.
func translatorFor(dialect string) Translator {
	switch dialect {
	case dialectAnthropicMessages:
		return AnthropicTranslator{}
	case dialectOpenAIResponses:
		return ResponsesTranslator{}
	default:
		return ChatTranslator{}
	}
}

// Translator is one client dialect: what a client speaks, as the canonical
// format, and the canonical format back to the client.
type Translator interface {
	ToCanonical(body *Obj) (*Obj, error)
	ResponseFromCanonical(completion *Obj, model string) *Obj
	StreamFromCanonical(chunks ChunkStream, model string, yield func(Event) bool) error
}

// noAnswer is noAnswer: a reasoning model that spent max_tokens on thinking is
// truncated (learned, mild), anything else is empty (a real failure).
func noAnswer(finishReason string) *providers.ProviderError {
	if finishReason == "length" {
		return providers.NewError(providers.KindTruncated, "The model used the whole max_tokens budget before answering.", providers.ErrorOptions{})
	}
	return providers.NewError(providers.KindEmpty, "The provider returned no answer.", providers.ErrorOptions{})
}

// hasAnswer is hasAnswer: only an answer or a tool call is output. Reasoning
// alone gives the client nothing.
func hasAnswer(message *Obj) bool {
	if message == nil {
		return false
	}
	return strings.TrimSpace(TextOf(getValue(message, "content"))) != "" || len(getList(message, "tool_calls")) > 0
}

// writeSSE writes one event and flushes it.
func writeSSE(w http.ResponseWriter, event Event, flush func()) {
	if event.Event != "" {
		_, _ = w.Write([]byte("event: " + event.Event + "\n"))
	}
	var data string
	if text, ok := event.Data.(string); ok {
		data = text
	} else {
		data = marshal(event.Data)
	}
	_, _ = w.Write([]byte("data: " + data + "\n\n"))
	if flush != nil {
		flush()
	}
}

// streamErrorEvent is streamErrorEvent: the error event of a stream that failed
// after it was committed to the client.
func streamErrorEvent(dialect string, err *providers.ProviderError) Event {
	message := "Upstream stream failed."
	kind := providers.KindServer
	if err != nil {
		if err.Message != "" {
			message = err.Message
		}
		if err.Kind != "" {
			kind = err.Kind
		}
	}
	switch dialect {
	case dialectAnthropicMessages:
		return Event{Event: "error", Data: newObjOf("type", "error", "error", newObjOf("type", "api_error", "message", message))}
	case dialectOpenAIResponses:
		return Event{Event: "error", Data: newObjOf("type", "error", "code", string(kind), "message", message)}
	default:
		return Event{Data: newObjOf("error", newObjOf("message", message, "type", "upstream_error", "code", string(kind)))}
	}
}

// hedgeDelay is hedgeDelay: how long to wait for an attempt before hedging with
// another free model — a multiple of what this model usually needs, or a default
// when unmeasured.
func (s *Service) hedgeDelay(entry *routing.Entry, stream bool) time.Duration {
	snapshot := s.opts.Routing.ModelStats(entry.Ref, time.Now())
	usual := snapshot.LatencyMs
	minimum := float64(hedgeMinPlainMs)
	if stream {
		usual = snapshot.TTFTMs
		minimum = hedgeMinStreamMs
	}
	if snapshot.Samples < hedgeMinSamples || usual == nil || *usual <= 0 {
		return s.cfg().Routing.HedgeUnmeasured
	}
	delay := math.Min(hedgeMaxMs, math.Max(minimum, *usual*hedgeFactor))
	return time.Duration(delay) * time.Millisecond
}

// trailItem is trailItem: one line of the trail an answer carries.
func trailItem(entry *routing.Entry, outcome string, extra *Obj) *Obj {
	item := newObj()
	item.Set("group", nil)
	if entry.RoutingGroup != "" {
		item.Set("group", entry.RoutingGroup)
	}
	item.Set("model", entry.ID)
	item.Set("ref", entry.Ref)
	item.Set("outcome", outcome)
	if entry.RoutingReason != "" {
		item.Set("routingReason", entry.RoutingReason)
	}
	if entry.TryingLuck {
		item.Set("luck", true)
	}
	if entry.LastResort {
		item.Set("lastResort", true)
	}
	setAll(item, extra)
	return item
}

// toolCallRequired is toolCallRequired: tool_choice "required" or a named
// function obliges the model to call a tool.
func toolCallRequired(canonical *Obj) bool {
	if len(getList(canonical, "tools")) == 0 {
		return false
	}
	choice, ok := get(canonical, "tool_choice")
	if !ok {
		return false
	}
	if text, isText := choice.(string); isText {
		return text == "required"
	}
	return asObj(choice) != nil
}

// missingToolCall is missingToolCall: the model ignored a required tool.
func missingToolCall() *providers.ProviderError {
	return providers.NewError(providers.KindInvalidOutput, "The model ignored tool_choice and answered without a tool call.", providers.ErrorOptions{})
}

// --- upstream streams ------------------------------------------------------

// streamStep is one step of an upstream chunk stream: a chunk or the failure
// that ended it.
type streamStep struct {
	chunk *Obj
	err   error
}

// streamReader drives one upstream chunk stream, so a silent stream can be
// abandoned: the adapter's iterator runs in its own goroutine and every read has
// a deadline.
type streamReader struct {
	steps  chan streamStep
	ctx    context.Context
	cancel context.CancelFunc
}

// openStream starts reading stream.
func openStream(parent context.Context, stream providers.Streamer, request json.RawMessage, call providers.Call) *streamReader {
	ctx, cancel := context.WithCancel(parent)
	reader := &streamReader{steps: make(chan streamStep), ctx: ctx, cancel: cancel}
	sequence := stream.Stream(ctx, request, call)
	go func() {
		defer close(reader.steps)
		sequence(func(raw json.RawMessage, err error) bool {
			if err != nil {
				select {
				case reader.steps <- streamStep{err: err}:
				case <-ctx.Done():
				}
				return false
			}
			chunk, parseErr := parseObject(raw)
			if parseErr != nil {
				// A chunk the adapter could not produce as JSON is skipped.
				return true
			}
			select {
			case reader.steps <- streamStep{chunk: chunk}:
				return true
			case <-ctx.Done():
				return false
			}
		})
	}()
	return reader
}

// next reads the next chunk. The stream is abandoned when expired fires first;
// a nil channel waits as long as the caller's context allows.
func (r *streamReader) next(expired <-chan time.Time, stalled string) (*Obj, error) {
	select {
	case step, open := <-r.steps:
		if !open {
			return nil, nil
		}
		return step.chunk, step.err
	case <-expired:
		r.cancel()
		return nil, providers.NewError(providers.KindTimeout, stalled, providers.ErrorOptions{})
	case <-r.ctx.Done():
		return nil, providers.FromNetwork(r.ctx.Err())
	}
}

// nextWithin is next with a deadline of its own: how long the stream may stay
// silent.
func (r *streamReader) nextWithin(idle time.Duration, stalled string) (*Obj, error) {
	timer := time.NewTimer(idle)
	defer timer.Stop()
	return r.next(timer.C, stalled)
}

// close stops the reader.
func (r *streamReader) close() {
	if r != nil && r.cancel != nil {
		r.cancel()
	}
}

// attemptResult is what one successful attempt produced.
type attemptResult struct {
	// response is the canonical completion of a plain attempt.
	response *Obj
	// headers are the upstream response headers.
	headers http.Header
	// latencyMs is how long the whole attempt took.
	latencyMs float64
	// buffered are the stream chunks read before the answer was committed.
	buffered []*Obj
	// reader continues a stream after the buffered chunks.
	reader *streamReader
	// accumulator is the stream folded so far.
	accumulator *Accumulator
	// ttftMs is when the first answer token arrived.
	ttftMs *float64
	// started is when the attempt started.
	started time.Time
}

// runAttempt is one attempt at one model. A stream is only committed to the
// client once real output arrived, so a provider that fails or stays silent can
// still be replaced transparently. timeout bounds a plain call, and for a stream
// the wait for the first answer; a committed stream has no total limit.
func runAttempt(ctx context.Context, entry *routing.Entry, adapter providers.Adapter, canonical *Obj, stream bool, timeout time.Duration, sessionID string) (*attemptResult, error) {
	started := time.Now()
	mustCallTool := toolCallRequired(canonical)
	request := json.RawMessage(marshal(canonical))
	call := providers.Call{ModelID: entry.ModelID, SessionID: sessionID, Timeout: timeout}
	if !stream {
		chatter, ok := adapter.(providers.Chatter)
		if !ok {
			return nil, providers.NewError(providers.KindServer, "This connection cannot answer chat requests.", providers.ErrorOptions{})
		}
		result, err := chatter.Chat(ctx, request, call)
		if err != nil {
			return nil, restartable(err)
		}
		response, err := parseObject(result.Body)
		if err != nil {
			return nil, providers.NewError(providers.KindInvalidOutput, "The provider returned a body that is not JSON.", providers.ErrorOptions{Headers: result.Headers})
		}
		choice := asObj(firstItem(getList(response, "choices")))
		message := getObj(choice, "message")
		// A reasoning model can spend a small max_tokens entirely on thinking;
		// that is a truncated answer, not a provider failure.
		if !hasAnswer(message) {
			return nil, noAnswer(getString(choice, "finish_reason"))
		}
		if InvalidToolCalls(getList(message, "tool_calls")) {
			return nil, providers.NewError(providers.KindInvalidOutput, "The provider returned malformed tool-call arguments.", providers.ErrorOptions{Headers: result.Headers})
		}
		if mustCallTool && len(getList(message, "tool_calls")) == 0 {
			return nil, missingToolCall()
		}
		return &attemptResult{response: response, headers: result.Headers, latencyMs: millis(time.Since(started)), started: started}, nil
	}
	streamer, ok := adapter.(providers.Streamer)
	if !ok {
		return nil, providers.NewError(providers.KindServer, "This connection cannot stream chat answers.", providers.ErrorOptions{})
	}
	var headers http.Header
	call.OnHeaders = func(value http.Header) { headers = value }
	reader := openStream(ctx, streamer, request, call)
	accumulator := &Accumulator{}
	buffered := make([]*Obj, 0, 4)
	var ttftMs *float64
	// One deadline for the whole wait: a model that only keeps reasoning is
	// replaced too.
	firstAnswer := time.NewTimer(timeout)
	defer firstAnswer.Stop()
	for {
		chunk, err := reader.next(firstAnswer.C, "No output before the first-token timeout.")
		if err != nil {
			reader.close()
			if classified := classify(err); classified != nil && classified.Kind == providers.KindAborted {
				return nil, classified
			}
			return nil, err
		}
		if chunk == nil {
			break
		}
		accumulator.Push(chunk)
		buffered = append(buffered, chunk)
		// Commit on the first real answer token (reasoning is buffered), and with
		// a required tool call only once the call starts.
		if accumulator.HasAnswer() && ttftMs == nil {
			ttft := millis(time.Since(started))
			ttftMs = &ttft
		}
		if accumulator.HasAnswer() && (!mustCallTool || len(accumulator.ToolCalls) > 0) {
			break
		}
	}
	if !accumulator.HasAnswer() {
		reader.close()
		return nil, noAnswer(accumulator.FinishReason)
	}
	if mustCallTool && len(accumulator.ToolCalls) == 0 {
		reader.close()
		return nil, missingToolCall()
	}
	return &attemptResult{
		headers:     headers,
		buffered:    buffered,
		reader:      reader,
		accumulator: accumulator,
		ttftMs:      ttftMs,
		started:     started,
	}, nil
}

// restartable keeps the classified failure when the error already is one.
func restartable(err error) *providers.ProviderError {
	if classified := providers.AsProviderError(err); classified != nil {
		return classified
	}
	return providers.FromNetwork(err)
}

// --- one request -----------------------------------------------------------

// Execute is execute(): runs one chat request end to end. It writes the response
// itself; every failure is written in the client's dialect.
func (s *Service) Execute(w http.ResponseWriter, r *http.Request, key *Key, dialect string, body *Obj, freeOnly bool) {
	if strings.TrimSpace(r.Header.Get("X-NeoRouter-Hop")) != "" && !key.IsPeer() {
		s.WriteError(w, r, dialect, key, getString(body, "model"), httpserver.New(http.StatusForbidden, "POOL_HOP_REFUSED", "Requests passed on by a pool partner are only accepted with a partner key."))
		return
	}
	if s.opts.Concurrency != nil {
		release, err := s.opts.Concurrency.Acquire(key.Row)
		if err != nil {
			s.WriteError(w, r, dialect, key, getString(body, "model"), err)
			return
		}
		defer release()
	}
	run := executeRun{
		service:  s,
		w:        w,
		r:        r,
		key:      key,
		dialect:  dialect,
		body:     body,
		freeOnly: freeOnly,
	}
	if key.IsPeer() {
		run.peer = true
		if s.opts.ShareAdmit != nil {
			share, err := s.opts.ShareAdmit(key)
			if err != nil {
				// Paused, used up or busy: the partner learns why and when to
				// come back.
				setRetryAfter(w, retryAfterSeconds(err))
				s.WriteError(w, r, dialect, key, getString(body, "model"), err)
				return
			}
			run.share = share
		}
	}
	if share := run.share; share != nil {
		setHeaders(w, share.Headers)
		if share.Release != nil {
			// Only a request this side answered counts against the day's share.
			defer func() { share.Release(run.answered) }()
		}
	}
	if err := run.execute(); err != nil {
		if run.management {
			httpserver.WriteError(w, r, s.log(), err)
			return
		}
		if run.peer {
			err = peerSafeError(err)
			setRetryAfter(w, retryAfterSeconds(err))
		}
		s.WriteError(w, r, dialect, key, run.requestedModel, err)
	}
}

// peerSafeError is what a partner learns when its request fails here: a code, a
// plain message and when to retry, never which connections or models this
// instance has.
func peerSafeError(err error) error {
	var httpErr *httpserver.HttpError
	if !errors.As(err, &httpErr) {
		return httpserver.New(http.StatusBadGateway, "POOL_PARTNER_FAILED", "The partner could not answer this request.")
	}
	status, code, message := httpErr.Status, httpErr.Code, httpErr.Message
	if status >= 500 || code == "NO_MODEL_AVAILABLE" || code == "ALL_ATTEMPTS_FAILED" {
		if status < 500 {
			status = http.StatusServiceUnavailable
		}
		if code == "" {
			code = "POOL_PARTNER_FAILED"
		}
		message = "No shared model can take this request right now."
	}
	safe := httpserver.New(status, code, message)
	if seconds := retryAfterSeconds(err); seconds > 0 {
		safe.RetryAfter = seconds
		safe.Details = map[string]any{"retryAfterSeconds": seconds}
	}
	return safe
}

// retryAfterSeconds is when the failure says to come back, 0 when it does not.
func retryAfterSeconds(err error) int {
	var httpErr *httpserver.HttpError
	if !errors.As(err, &httpErr) {
		return 0
	}
	if httpErr.RetryAfter > 0 {
		return httpErr.RetryAfter
	}
	details, _ := httpErr.Details.(map[string]any)
	switch value := details["retryAfterSeconds"].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	}
	return 0
}

func setRetryAfter(w http.ResponseWriter, seconds int) {
	if seconds > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(seconds))
	}
}

// secondsUntil is the whole seconds until a unix millisecond time, at least 1.
func secondsUntil(unixMs int64) int {
	return maxInt(1, int(math.Ceil(float64(unixMs-time.Now().UnixMilli())/1000)))
}

// queueWindows are how long a request may wait for capacity. A pool partner
// waits only briefly. A model that works and is only full right now is worth the
// configured wait even while the user is short of capacity (busyUntil): its
// slots free as it answers. Neither goes past deadline.
func (s *Service) queueWindows(plan *routing.RoutingPlan, key *Key, now, deadline time.Time) (waitUntil, busyUntil time.Time) {
	until := func(wait time.Duration) time.Time {
		if end := now.Add(wait); end.Before(deadline) {
			return end
		}
		return deadline
	}
	if key.IsPeer() {
		end := until(peerQueueMs * time.Millisecond)
		return end, end
	}
	configured := plan.QueueWaitMs(s.cfg().Routing.QueueWait.Milliseconds())
	capped := s.opts.Routing.QueueWaitMs(key.Routing, configured)
	return until(time.Duration(capped) * time.Millisecond), until(time.Duration(configured) * time.Millisecond)
}

// executeRun is one request's state.
type executeRun struct {
	service *Service
	w       http.ResponseWriter
	r       *http.Request
	key     *Key
	dialect string
	body    *Obj
	// freeOnly restricts the request to free models (the playground toggle).
	freeOnly bool
	// peer marks a pool partner's own key.
	peer bool
	// share is the partner's admitted share of this instance, when pools are
	// wired.
	share *Share

	requestedModel string
	answered       bool
	// queuedMs is how long the request waited for capacity.
	queuedMs float64
	// canonical is the request as it goes upstream (after processing).
	canonical *Obj
	// plan is this request's route through the catalog.
	plan *routing.RoutingPlan
	// streaming is whether the client asked for a stream.
	streaming bool
	// internal marks a turn of a request that already passed the key's rate
	// limit (the web-search loop), which records its own usage but takes no
	// concurrency slot and does not count against the limit again.
	internal bool
	// preset is an already translated canonical request (a web-search turn).
	preset *Obj
	// management reports failures in the management envelope (the playground
	// route lives under /api/v1, not on the gateway).
	management bool
}

// execute is execute(): the request, with the built-in web search in front of it
// when the client asked for search and the owner has a search engine.
func (run *executeRun) execute() error {
	s := run.service
	translator := translatorFor(run.dialect)
	// Built-in web search runs the request as several turns (web_search.go).
	if !run.peer && run.preset == nil && s.opts.SearchWeb != nil {
		if spec := webSearchRequested(run.dialect, run.body); spec != nil && s.webSearchAvailable(run.key.UserID()) {
			if err := s.consumeRPM(run); err != nil {
				return err
			}
			return s.executeWithSearch(run, spec, translator)
		} else if spec != nil {
			// Without a search engine the marker is dropped, as before.
			run.body = spec.Body
		}
	}
	return run.run(translator)
}

// run is run(): the request itself.
func (run *executeRun) run(translator Translator) error {
	s := run.service
	key := run.key
	res := run.w
	req := run.r
	dialect := run.dialect
	body := run.body
	if body == nil {
		body = newObj()
	}
	requestID := httpserver.RequestIDFrom(req.Context())
	if requestID == "" {
		requestID = uuid.NewString()
	}
	startedAt := time.Now()
	requestedModel := getString(body, "model")
	if requestedModel == "" {
		requestedModel = "auto"
	}
	run.requestedModel = requestedModel
	var canonical *Obj
	if run.preset != nil {
		canonical = clone(run.preset)
	} else {
		translated, err := translator.ToCanonical(body)
		if err != nil {
			var httpErr *httpserver.HttpError
			if errors.As(err, &httpErr) {
				return err
			}
			return httpserver.New(http.StatusBadRequest, "INVALID_REQUEST", "The request body could not be read: "+err.Error())
		}
		canonical = translated
	}
	if len(getList(canonical, "messages")) == 0 {
		return httpserver.New(http.StatusBadRequest, "INVALID_REQUEST", "The request contains no messages.")
	}
	sessionID := providers.ClientSession(req)
	if sessionID == "" {
		sessionID = providers.DerivedSession(json.RawMessage(marshal(canonical)), key.ID())
	}
	if _, ok := get(canonical, "prompt_cache_key"); !ok {
		canonical.Set("prompt_cache_key", sessionID)
	}

	if !run.internal {
		if err := s.consumeRPM(run); err != nil {
			return err
		}
	}
	// What the key may still send, so clients pace themselves (not for partners).
	if !run.peer && !run.internal {
		setHeaders(res, routing.BudgetHeaders(s.requestBudget(key)))
	}
	budgetExhausted := s.budgetExhausted(key)
	if budgetExhausted && key.Row.Mode == "paid" && !key.Row.AllowPlan {
		return httpserver.New(http.StatusPaymentRequired, "BUDGET_EXHAUSTED",
			"This router key has used its "+key.Row.BudgetPeriod+" budget of "+jsString(*key.Row.BudgetUSD)+" USD.")
	}

	// A key that may use the pool: very sensitive requests stay on own
	// connections (when the owner wants that), judged on the original text.
	keepPrivate := false
	var keptPrivateReasons []string
	if !run.peer && s.opts.PoolAccess != nil {
		permission := s.opts.PoolAccess(key)
		if permission.MayUse && permission.KeepSensitivePrivate && s.opts.AssessSensitivity != nil {
			level, reasons := s.opts.AssessSensitivity(canonical)
			if level == "high" {
				keepPrivate = true
				keptPrivateReasons = reasons
			}
		}
	}

	// Compression and the privacy shield run before routing, so the router sees
	// the real request size and providers never see hidden values.
	processing := ProcessingResult{Canonical: canonical}
	if !run.peer && s.opts.ApplyProcessing != nil {
		poolMinimum := s.opts.PoolAccess != nil && s.opts.PoolAccess(key).MayUse
		applied, err := s.opts.ApplyProcessing(req.Context(), key.UserID(), canonical, ProcessingOptions{
			CompressionLevel: req.Header.Get("X-NeoRouter-Compression"),
			PoolMinimum:      poolMinimum,
		})
		if err != nil {
			return err
		}
		processing = applied
		canonical = applied.Canonical
	}
	run.canonical = canonical

	stream := getBool(canonical, "stream")
	run.streaming = stream
	needs := Requirements(canonical)
	inputTokens := EstimateInputTokens(canonical)
	planOpts := routing.PlanOptions{
		UserID:         key.UserID(),
		Key:            key.Routing,
		Request:        requestInfoOf(canonical),
		RequestedModel: requestedModel,
		Needs:          routing.Needs{Tools: needs.Tools, Vision: needs.Vision, JSON: needs.JSON, Stream: needs.Stream},
		InputTokens:    inputTokens,
		FreeOnly:       run.freeOnly || budgetExhausted,
		Capability:     "chat",
		SessionID:      sessionID,
		KeepPrivate:    keepPrivate,
	}
	if !run.peer {
		// Apps that know what they ask for can say so (one of the task kinds).
		planOpts.Workload = req.Header.Get("X-NeoRouter-Workload")
		planOpts.TaskHint = req.Header.Get("X-NeoRouter-Task")
		planOpts.RoutingGroup = req.Header.Get("X-NeoRouter-Group")
	}
	if run.share != nil {
		planOpts.ShareReserve = run.share.Reserve
		planOpts.SharePoolID = run.share.PoolID
	}
	plan, err := s.opts.Routing.NewPlan(planOpts)
	if err != nil {
		return err
	}
	run.plan = plan
	if s.opts.Routing != nil {
		s.opts.Routing.LiveStarted(key.UserID(), requestID, keyNameForLive(run), planTask(plan))
	}
	trail := make([]*Obj, 0, 4)
	var lastError *providers.ProviderError
	badRequests := 0
	var freesUpAt *int64
	finish := s.finisher(run, requestID, requestedModel, startedAt, processing, keepPrivate, keptPrivateReasons, plan, &trail)

	// The same conversation again right after an answer: that answer was no good.
	retryID := ""
	if !run.peer && !run.internal {
		retryID = task.Fingerprint(key.ID(), planOpts.Request.Messages)
		if retryID != "" {
			if repeated := s.retries.RepeatedAnswer(retryID, time.Now()); repeated != nil && s.opts.RecordFeedback != nil {
				s.opts.RecordFeedback(repeated.Ref, repeated.Task, false)
			}
		}
	}
	if pacing := s.pacingState(key); pacing != "" && !run.peer {
		res.Header().Set("X-NeoRouter-Budget-Pace", pacing)
	}
	if plan.Task != nil && !run.peer {
		res.Header().Set("X-NeoRouter-Task", plan.Task.Primary+"; difficulty="+trimFloat(plan.Task.Difficulty))
	}

	config := s.cfg()
	clientCtx := req.Context()
	trail = trail[:0]
	var attemptsStarted int
	var nextHedgeAt time.Time
	var overBudget bool
	running := map[*attempt]bool{}
	results := make(chan attemptOutcome, maxInt(1, config.Routing.MaxParallel+1))
	totalBudget := config.Routing.TotalBudget

	launch := func(entry *routing.Entry) bool {
		if s.opts.Adapters == nil {
			return false
		}
		adapter := s.opts.Adapters.Adapter(entry.AccountID)
		if adapter == nil {
			return false
		}
		attemptCtx, cancel := context.WithCancel(clientCtx)
		boost := s.opts.Routing.BoostedMaxTokens(entry, planOpts.Request, time.Now())
		sent := canonical
		if boost != nil {
			sent = clone(canonical)
			sent.Set("max_tokens", float64(*boost))
		}
		maxTokens := 0
		if value, ok := getNumber(sent, "max_tokens"); ok {
			maxTokens = int(value)
		}
		maxOutput := routing.MaxOutputFor(planOpts.Request, entry)
		timeout := config.UpstreamTimeout
		if s.opts.Adapters != nil {
			if own := s.opts.Adapters.Timeout(entry.AccountID); own > 0 {
				timeout = own
			}
		}
		if remaining := totalBudget - time.Since(startedAt); remaining < timeout {
			timeout = remaining
		}
		if timeout < time.Second {
			timeout = time.Second
		}
		reservation, affordable := s.reserve(key, entry, inputTokens, maxOutput)
		if !affordable {
			// Concurrent requests took what was left since the plan looked.
			overBudget = true
			cancel()
			return false
		}
		item := &attempt{
			entry:       entry,
			maxTokens:   maxTokens,
			boosted:     boost != nil,
			started:     time.Now(),
			cancel:      cancel,
			release:     s.opts.Routing.Lease(entry),
			reservation: reservation,
		}
		s.opts.Routing.PlanSent(entry)
		if s.opts.Quota != nil {
			_ = s.opts.Quota.RecordRequest(quotaEntryOf(entry), int64(inputTokens), time.Now())
		}
		s.opts.Routing.LiveAttempt(key.UserID(), requestID, slugOf(entry), entry.ID)
		attemptsStarted++
		running[item] = true
		go func() {
			result, err := runAttempt(attemptCtx, entry, adapter, sent, stream, timeout, sessionID)
			results <- attemptOutcome{attempt: item, result: result, err: err}
		}()
		return true
	}

	startNext := func(filter func(*routing.Entry) bool) bool {
		for attemptsStarted < config.Routing.MaxAttempts && time.Since(startedAt) < totalBudget && clientCtx.Err() == nil {
			choice := plan.Next(routing.NextOptions{Filter: filter, Sample: true})
			if choice == nil {
				return false
			}
			if launch(choice.Entry) {
				return true
			}
		}
		return false
	}

	// ensureRunning starts an attempt, or waits for temporarily blocked
	// candidates to free up (bursts beyond every provider's rate limit queue
	// instead of failing).
	ensureRunning := func() bool {
		waitUntil, busyUntil := s.queueWindows(plan, key, time.Now(), startedAt.Add(totalBudget))
		defer plan.LeaveQueue()
		for len(running) == 0 {
			if startNext(nil) {
				return true
			}
			if clientCtx.Err() != nil || attemptsStarted >= config.Routing.MaxAttempts {
				return false
			}
			now := time.Now()
			freesUpAt = plan.FreesUpAt(now, nil)
			until := waitUntil
			if busyUntil.After(waitUntil) && plan.WaitingOnBusy(now, nil) {
				until = busyUntil
			}
			if freesUpAt == nil || time.UnixMilli(*freesUpAt).After(until) {
				// Better to try what may work again than to answer "no model
				// available".
				if plan.EnableLastResort(now, nil) {
					continue
				}
				if plan.EnableLongShots(now, nil) {
					continue
				}
				return false
			}
			plan.JoinQueue(now, nil)
			pause := time.Until(until)
			free := time.Until(time.UnixMilli(*freesUpAt))
			poll := math.Max(queueMinPollMs, math.Min(millis(free), queuePollMs)) * (0.5 + rand.Float64())
			if wait := time.Duration(poll) * time.Millisecond; wait < pause {
				pause = wait
			}
			select {
			case <-time.After(pause):
			case <-clientCtx.Done():
				return false
			}
			run.queuedMs += millis(time.Since(now))
		}
		return true
	}

	settle := func(item *attempt) {
		delete(running, item)
		if item.release != nil {
			item.release()
		}
	}

	commit := s.committer(run, plan, requestID, requestedModel, sessionID, inputTokens, stream, dialect, translator, processing, keepPrivate, keptPrivateReasons, startedAt, &attemptsStarted, &trail, func() { run.answered = true })

	cleanup := func() {
		for item := range running {
			if item.cancel != nil {
				item.cancel()
			}
			settle(item)
			if item.reservation != nil {
				item.reservation.Release()
			}
		}
	}

	ensureRunning()
	for len(running) > 0 {
		newest := newestAttempt(running)
		parallel := config.Routing.MaxParallel
		if s.opts.Routing.ShortOfCapacity(key.UserID()) {
			parallel = minInt(2, parallel)
		}
		canHedge := config.Routing.Hedging && len(running) < parallel && attemptsStarted < config.Routing.MaxAttempts && allFree(running)
		var hedgeTimer *time.Timer
		var hedgeChannel <-chan time.Time
		if canHedge {
			delay := s.hedgeDelay(newest.entry, stream) - time.Since(newest.started)
			if next := time.Until(nextHedgeAt); next > delay {
				delay = next
			}
			if delay < 0 {
				delay = 0
			}
			hedgeTimer = time.NewTimer(delay)
			hedgeChannel = hedgeTimer.C
		}
		var outcome attemptOutcome
		select {
		case outcome = <-results:
		case <-hedgeChannel:
			// Hedges only go to free models, so a slow attempt never costs money
			// twice.
			if !startNext(func(entry *routing.Entry) bool { return entry.IsFree }) {
				nextHedgeAt = time.Now().Add(hedgeRetryMs * time.Millisecond)
			}
			continue
		case <-clientCtx.Done():
			cleanup()
			goto finished
		}
		if hedgeTimer != nil {
			hedgeTimer.Stop()
		}
		item := outcome.attempt
		if outcome.err != nil {
			// The provider rejected the attempt while it still occupied a slot.
			inFlight := s.opts.Routing.Inflight(item.entry).Account
			settle(item)
			s.opts.Routing.LiveFailed(key.UserID(), requestID, slugOf(item.entry))
			if item.reservation != nil {
				item.reservation.Release()
			}
			classified := restartable(outcome.err)
			if classified.Kind == providers.KindAborted && clientCtx.Err() != nil {
				cleanup()
				break
			}
			lastError = classified
			trail = append(trail, trailItem(item.entry, string(classified.Kind), newObjOf(
				"status", classified.Status,
				"ms", millis(time.Since(item.started)),
				"message", truncateText(classified.Message, 200),
			)))
			policy := s.opts.Routing.OnFailure(item.entry, routingFailureOf(classified), routing.FailureOptions{
				EstimatedTokens: inputTokens,
				MaxTokens:       intPtr(item.maxTokens),
				Boosted:         item.boosted,
				Running:         intPtr(inFlight),
				Now:             time.Now(),
			})
			if s.isPeerEntry(item.entry) {
				if s.opts.Quota != nil {
					_ = s.opts.Quota.RefundRequest(quotaEntryOf(item.entry), int64(inputTokens), time.Now())
				}
				if s.opts.NotePeerReachability != nil {
					s.opts.NotePeerReachability(item.entry.AccountID, false, string(classified.Kind))
				}
			}
			plan.NoteFailure(item.entry, routing.Kind(classified.Kind))
			if classified.Kind == providers.KindAuth && s.opts.Adapters != nil {
				go s.opts.Adapters.Validate(item.entry.AccountID)
			}
			if classified.Kind == providers.KindBadRequest {
				badRequests++
			} else {
				badRequests = 0
			}
			if !policy.Fallback || badRequests >= maxConsecutiveBadRequests {
				cleanup()
				break
			}
			if len(running) == 0 {
				ensureRunning()
			}
			continue
		}
		for other := range running {
			if other == item {
				continue
			}
			if other.cancel != nil {
				other.cancel()
			}
			settle(other)
			if other.reservation != nil {
				other.reservation.Release()
			}
			trail = append(trail, trailItem(other.entry, "hedge_lost", newObjOf("ms", millis(time.Since(other.started)))))
			s.opts.Routing.LiveFailed(key.UserID(), requestID, slugOf(other.entry))
			s.opts.Routing.OnSlow(other.entry, millis(time.Since(other.started)), time.Now())
		}
		delete(running, item)
		s.opts.Routing.RecordAnswered(key.UserID(), true)
		if s.isPeerEntry(item.entry) && s.opts.NotePeerReachability != nil {
			s.opts.NotePeerReachability(item.entry.AccountID, true, "")
		}
		if retryID != "" && plan.Task != nil {
			s.retries.RememberAnswer(retryID, task.Answer{Ref: item.entry.Ref, Task: plan.Task.Primary}, time.Now())
		}
		err := commit(item, outcome.result)
		item.cancel()
		if item.release != nil {
			item.release()
		}
		return err
	}
	cleanup()

finished:
	if clientCtx.Err() != nil {
		finish(usageFields{status: "error", httpStatus: statusClientClosed, errorCode: "client_closed", errorMessage: "The client disconnected."})
		return nil
	}
	rejected := plan.RejectionSummary(time.Now(), nil)
	if overBudget && rejected["budget_exhausted"] == 0 {
		rejected["budget_exhausted"] = 1
	}
	reasons := make([]string, 0, len(rejected))
	for reason := range rejected {
		reasons = append(reasons, reason)
	}
	// An explicitly named model this key may never use is a permission error,
	// not a temporary outage.
	if !routing.ParseModel(requestedModel).Auto && len(trail) == 0 && len(reasons) > 0 && allIn(reasons, keyPolicyRejections) {
		why := "is not allowed for this key"
		if rejected["not_free"] > 0 {
			why = "is not free and this key only allows free models"
		}
		message := "The model \"" + requestedModel + "\" " + why + "."
		finish(usageFields{status: "error", httpStatus: http.StatusForbidden, errorCode: "MODEL_NOT_ALLOWED", errorMessage: message})
		return httpserver.New(http.StatusForbidden, "MODEL_NOT_ALLOWED", message, map[string]any{"rejected": rejected})
	}
	if len(trail) == 0 && len(reasons) > 0 && allContextOrPermanent(reasons) {
		message := "The request (~" + jsString(float64(inputTokens)) + " tokens) is larger than the context window of every model available to this key."
		finish(usageFields{status: "error", httpStatus: http.StatusBadRequest, errorCode: "CONTEXT_TOO_LARGE", errorMessage: message})
		return httpserver.New(http.StatusBadRequest, "CONTEXT_TOO_LARGE", message, map[string]any{"rejected": rejected})
	}
	status := http.StatusBadGateway
	code := "ALL_ATTEMPTS_FAILED"
	message := ""
	if len(trail) == 0 {
		switch {
		case lastError != nil && lastError.Kind == providers.KindBadRequest && badRequests >= maxConsecutiveBadRequests:
			status = http.StatusBadRequest
		case budgetExhausted:
			status = http.StatusPaymentRequired
		case rejected["token_limit"] > 0:
			status = http.StatusTooManyRequests
		default:
			status = http.StatusServiceUnavailable
		}
		if rejected["token_limit"] > 0 {
			code = "TOKEN_LIMIT_REACHED"
		} else {
			code = "NO_MODEL_AVAILABLE"
		}
		message = routing.ExplainNoModel(rejected, "chat")
	} else {
		if lastError != nil && lastError.Kind == providers.KindBadRequest && badRequests >= maxConsecutiveBadRequests {
			status = http.StatusBadRequest
		} else if lastError != nil && lastError.Kind == providers.KindContextLength {
			status = http.StatusBadRequest
		}
		last := "unknown"
		if lastError != nil {
			last = lastError.Message
		}
		message = "All " + jsString(float64(len(trail))) + " attempt(s) failed. Last error: " + last
	}
	finish(usageFields{status: "error", httpStatus: status, errorCode: code, errorMessage: message})
	s.opts.Routing.RecordAnswered(key.UserID(), false)
	// When a model frees up later, say when to come back.
	details := map[string]any{"trail": trail, "rejected": rejected}
	httpErr := httpserver.New(status, code, message, details)
	if freesUpAt != nil {
		httpErr.RetryAfter = secondsUntil(*freesUpAt)
		details["retryAfterSeconds"] = httpErr.RetryAfter
		setRetryAfter(res, httpErr.RetryAfter)
	}
	return httpErr
}
