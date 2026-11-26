package gateway

// The bookkeeping around the attempt loop: the headers a key's allowance shows,
// the finish() that records one row per request, and the commit() that answers
// the client and teaches routing.

import (
	"context"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/httpx"
	"github.com/NeoLabs-Systems/neorouter/internal/budget"
	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
	"github.com/NeoLabs-Systems/neorouter/internal/providers"
	"github.com/NeoLabs-Systems/neorouter/internal/routing"
	"github.com/NeoLabs-Systems/neorouter/internal/usage"
)

// statusClientClosed is the status recorded for a request whose client went
// away (nginx's 499).
const statusClientClosed = 499

// attempt is one running attempt at one model.
type attempt struct {
	entry       *routing.Entry
	maxTokens   int
	boosted     bool
	started     time.Time
	cancel      context.CancelFunc
	release     func()
	reservation *budget.Reservation
}

// attemptOutcome is how one attempt ended.
type attemptOutcome struct {
	attempt *attempt
	result  *attemptResult
	err     error
}

// setHeaders writes a header map.
func setHeaders(w http.ResponseWriter, headers map[string]string) {
	for name, value := range headers {
		w.Header().Set(name, value)
	}
}

// requestBudget is requestBudget.forKey: what the key may send right now.
func (s *Service) requestBudget(key *Key) routing.RequestBudget {
	if s.opts.Routing == nil {
		return routing.RequestBudget{}
	}
	return s.opts.Routing.RequestBudgetForKey(key.Routing, time.Now())
}

// consumeRPM is routerKeys.consumeRpm: one request against the key's per-minute
// limit, with the window headers a client paces itself by.
func (s *Service) consumeRPM(run *executeRun) error {
	if s.opts.Keys == nil {
		return nil
	}
	result := s.opts.Keys.ConsumeRPM(run.key.Row, time.Now())
	if window := s.opts.Keys.RPMStateOf(run.key.Row, time.Now()); window != nil {
		run.w.Header().Set("x-ratelimit-limit-requests", jsString(float64(window.Limit)))
		run.w.Header().Set("x-ratelimit-remaining-requests", jsString(float64(window.Remaining)))
		run.w.Header().Set("x-ratelimit-reset-requests", jsString(float64(window.ResetSeconds))+"s")
	}
	if !result.Allowed {
		run.w.Header().Set("Retry-After", jsString(float64(result.RetryAfterSeconds)))
		err := httpserver.New(http.StatusTooManyRequests, "KEY_RATE_LIMITED", "This router key exceeded its requests-per-minute limit.")
		err.RetryAfter = result.RetryAfterSeconds
		return err
	}
	return nil
}

// budgetExhausted reports a key whose budget is used up: remaining is not null
// and not above zero.
func (s *Service) budgetExhausted(key *Key) bool {
	if s.opts.Budget == nil {
		return false
	}
	remaining, err := s.opts.Budget.Remaining(budgetKeyOf(key), time.Now())
	if err != nil || remaining == nil {
		return false
	}
	return *remaining <= 0
}

// pacingState is plan.pacing.state: how far ahead of its budget the key is.
func (s *Service) pacingState(key *Key) string {
	if s.opts.Budget == nil {
		return ""
	}
	pace, err := s.opts.Budget.Pacing(budgetKeyOf(key), time.Now())
	if err != nil || pace == nil {
		return ""
	}
	return pace.State
}

// reserve holds one attempt's worst-case cost; false means the key's budget no
// longer covers it.
func (s *Service) reserve(key *Key, entry *routing.Entry, inputTokens, maxOutputTokens int) (*budget.Reservation, bool) {
	if s.opts.Budget == nil {
		return nil, true
	}
	return s.tryReserve(key, budget.EstimateMaxCost(priceOf(entry), int64(inputTokens), int64(maxOutputTokens)))
}

// tryReserve is reserve for an already estimated cost; nil reserves nothing.
func (s *Service) tryReserve(key *Key, estimate *float64) (*budget.Reservation, bool) {
	amount := 0.0
	if estimate != nil {
		amount = *estimate
	}
	reservation, ok, err := s.opts.Budget.TryReserve(budgetKeyOf(key), amount, time.Now())
	if err != nil {
		s.log().Warn("Could not read a key's spend", "keyId", key.ID(), "error", err)
		return s.opts.Budget.Reserve(budgetKeyOf(key), amount), true
	}
	return reservation, ok
}

// observeCost reports what a provider says it charged, so a model routed as free
// that bills is treated as paid from then on.
func (s *Service) observeCost(entry *routing.Entry, reported *float64) {
	if s.opts.ObserveCost != nil && reported != nil {
		s.opts.ObserveCost(entry, *reported)
	}
}

// isPeerEntry reports whether an entry belongs to a pool partner.
func (s *Service) isPeerEntry(entry *routing.Entry) bool {
	return s.opts.IsPeerEntry != nil && s.opts.IsPeerEntry(entry)
}

// slugOf is String(entry.id).split('/')[0].
func slugOf(entry *routing.Entry) string {
	if at := strings.IndexByte(entry.ID, '/'); at >= 0 {
		return entry.ID[:at]
	}
	return entry.ID
}

// planTask is plan.task?.primary || 'chat'.
func planTask(plan *routing.RoutingPlan) string {
	if plan != nil && plan.Task != nil && plan.Task.Primary != "" {
		return plan.Task.Primary
	}
	return "chat"
}

// keyNameForLive is the name the live view shows for a request's key.
func keyNameForLive(run *executeRun) string {
	if run.peer {
		return "#pool"
	}
	return run.key.Name()
}

// maxInt and minInt are the larger and smaller of two ints.
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// newestAttempt is the most recently started attempt (the one hedging watches).
func newestAttempt(running map[*attempt]bool) *attempt {
	var newest *attempt
	for item := range running {
		if newest == nil || item.started.After(newest.started) {
			newest = item
		}
	}
	return newest
}

// allFree reports whether every running attempt is a free one.
func allFree(running map[*attempt]bool) bool {
	for item := range running {
		if !item.entry.IsFree {
			return false
		}
	}
	return true
}

// allIn reports whether every value is in the set.
func allIn(values []string, set map[string]bool) bool {
	for _, value := range values {
		if !set[value] {
			return false
		}
	}
	return true
}

// allContextOrPermanent reports whether every rejection is a context or a
// permanent one, and at least one is about the context window.
func allContextOrPermanent(reasons []string) bool {
	hasContext := false
	for _, reason := range reasons {
		if strings.HasPrefix(reason, "context_") {
			hasContext = true
			continue
		}
		if keyPolicyRejections[reason] || permanentRejections[reason] {
			continue
		}
		return false
	}
	return hasContext
}

// truncateText is String(message).slice(0, n).
func truncateText(text string, n int) string {
	runes := []rune(text)
	if len(runes) <= n {
		return text
	}
	return string(runes[:n])
}

// usageFields is what one request's usage row records about its outcome.
type usageFields struct {
	status            string
	httpStatus        int
	errorCode         string
	errorMessage      string
	modelRef          *int64
	modelLabel        string
	accountID         string
	routingGroup      string
	cachedInputTokens int64
	cacheCreation     int64
	inputTokens       *int64
	outputTokens      *int64
	costUSD           *float64
	isFree            *bool
	ttftMs            *int64
	units             *float64
}

// finisher is finish(): one row per request, metadata only. Recording must never
// break a request.
func (s *Service) finisher(run *executeRun, requestID, requestedModel string, startedAt time.Time, processing ProcessingResult, keepPrivate bool, keptPrivateReasons []string, plan *routing.RoutingPlan, trail *[]*Obj) func(usageFields) {
	key := run.key
	return func(fields usageFields) {
		success := fields.status == "success"
		if s.opts.Routing != nil {
			s.opts.Routing.LiveFinished(key.UserID(), requestID, success, fields.modelLabel)
		}
		if run.share != nil && success {
			run.answered = true
		}
		if s.opts.RecordPoolTraffic != nil {
			tokens := int64(0)
			if fields.inputTokens != nil {
				tokens += *fields.inputTokens
			}
			if fields.outputTokens != nil {
				tokens += *fields.outputTokens
			}
			s.opts.RecordPoolTraffic(key, fields.accountID, success, tokens)
		}
		if s.opts.Usage == nil {
			return
		}
		if run.peer {
			fields.errorMessage = ""
		}
		steps := *trail
		asAny := make([]any, 0, len(steps))
		for _, step := range steps {
			// A partner's request keeps codes only: provider error texts can
			// carry account details, and the trail names this instance's
			// connections.
			if run.peer {
				copied := clone(step)
				copied.Delete("message")
				asAny = append(asAny, copied)
				continue
			}
			asAny = append(asAny, step)
		}
		s.recordUsage(run, requestID, requestedModel, startedAt, processing, keepPrivate, keptPrivateReasons, plan, fields, asAny)
	}
}

// recordUsage writes the request's row.
func (s *Service) recordUsage(run *executeRun, requestID, requestedModel string, startedAt time.Time, processing ProcessingResult, keepPrivate bool, keptPrivateReasons []string, plan *routing.RoutingPlan, fields usageFields, trail []any) {
	keyID := run.key.ID()
	latency := int64(millis(time.Since(startedAt)))
	entry := usage.Entry{
		ID:                     requestID,
		UserID:                 run.key.UserID(),
		RouterKeyID:            &keyID,
		CreatedAt:              startedAt.UnixMilli(),
		APIFormat:              run.dialect,
		RequestedModel:         requestedModel,
		Status:                 fields.status,
		TrailJSON:              marshal(trail),
		Attempts:               int64(maxInt(1, len(trail))),
		Stream:                 run.streaming,
		LatencyMS:              &latency,
		RedactedCount:          processing.Redacted,
		CompressionSavedTokens: processing.SavedTokens,
		CachedInputTokens:      fields.cachedInputTokens,
		CacheCreationTokens:    fields.cacheCreation,
	}
	if fields.httpStatus != 0 {
		status := int64(fields.httpStatus)
		entry.HTTPStatus = &status
	}
	if fields.errorCode != "" {
		entry.ErrorCode = &fields.errorCode
	}
	if fields.errorMessage != "" {
		entry.ErrorMessage = &fields.errorMessage
	}
	if fields.modelRef != nil {
		entry.ModelRef = fields.modelRef
	}
	if fields.modelLabel != "" {
		entry.ModelLabel = &fields.modelLabel
	}
	if fields.accountID != "" {
		entry.AccountID = &fields.accountID
	}
	if fields.routingGroup != "" {
		entry.RoutingGroup = &fields.routingGroup
	}
	if keepPrivate {
		joined := strings.Join(keptPrivateReasons, ",")
		entry.KeptPrivate = &joined
	}
	entry.QueuedMS = int64(math.Round(run.queuedMs))
	if plan != nil && plan.Task != nil && plan.Task.Primary != "" {
		taskName := plan.Task.Primary
		entry.Task = &taskName
	}
	if fields.inputTokens != nil {
		entry.InputTokens = *fields.inputTokens
	}
	if fields.outputTokens != nil {
		entry.OutputTokens = *fields.outputTokens
	}
	if fields.costUSD != nil {
		entry.CostUSD = *fields.costUSD
	}
	if fields.isFree != nil {
		entry.IsFree = *fields.isFree
	}
	if fields.ttftMs != nil {
		entry.TTFTMS = fields.ttftMs
	}
	if fields.units != nil {
		entry.Units = *fields.units
	}
	if err := s.opts.Usage.Record(entry); err != nil {
		s.log().Error("Failed to record request", "requestId", requestID, "error", err)
	}
}

// int64Ptr is a pointer to a value.
func int64Ptr(value float64) *int64 {
	out := int64(value)
	return &out
}

// boolPtr is a pointer to a value.
func boolPtr(value bool) *bool { return &value }

// cachedOf is tokens.prompt_tokens_details?.cached_tokens.
func cachedOf(tokens *Obj) float64 {
	cached, _ := getNumber(getObj(tokens, "prompt_tokens_details"), "cached_tokens")
	return cached
}

// committer is commit(): it writes a successful attempt to the client (JSON or
// SSE) and records it.
func (s *Service) committer(run *executeRun, plan *routing.RoutingPlan, requestID, requestedModel, sessionID string, inputTokens int, stream bool, dialect string, translator Translator, processing ProcessingResult, keepPrivate bool, keptPrivateReasons []string, startedAt time.Time, attemptsStarted *int, trail *[]*Obj, answered func()) func(*attempt, *attemptResult) error {
	w := run.w
	return func(item *attempt, result *attemptResult) error {
		entry := item.entry
		if !run.peer {
			w.Header().Set("X-NeoRouter-Model", entry.ID)
			if entry.RoutingGroup != "" {
				w.Header().Set("X-NeoRouter-Group", entry.RoutingGroup)
			}
			models := make([]string, 0, len(*trail)+1)
			for _, step := range *trail {
				models = append(models, getString(step, "model"))
			}
			models = append(models, entry.ID)
			w.Header().Set("X-NeoRouter-Trail", strings.Join(models, ","))
		}
		if keepPrivate {
			w.Header().Set("X-NeoRouter-Kept-Private", strings.Join(keptPrivateReasons, ","))
		}
		if processing.CompressionLevel != "" && processing.CompressionLevel != "off" {
			w.Header().Set("X-NeoRouter-Compression", processing.CompressionLevel+"; saved~="+jsString(float64(processing.SavedTokens)))
		}
		if processing.Redacted > 0 {
			w.Header().Set("X-NeoRouter-Redacted", jsString(float64(processing.Redacted)))
		}
		finish := s.finisher(run, requestID, requestedModel, startedAt, processing, keepPrivate, keptPrivateReasons, plan, trail)
		stats := s.statsBody(run, plan, requestID, attemptsStarted, trail)

		if !stream {
			tokens := NormalizeUsage(getValue(result.response, "usage"))
			if tokens == nil {
				completion := 0
				if choice := asObj(firstItem(getList(result.response, "choices"))); choice != nil {
					completion = (utf16Len(TextOf(getValue(getObj(choice, "message"), "content"))) + 3) / 4
				}
				tokens = newObjOf("prompt_tokens", float64(inputTokens), "completion_tokens", float64(completion))
			}
			s.opts.Routing.RememberAffinity(plan.AffinityScope, sessionID, entry, time.Now())
			cost := 0.0
			if s.opts.Budget != nil {
				cost = budget.CostOf(priceOf(entry), budgetUsageOf(tokens))
			}
			if item.reservation != nil {
				_ = item.reservation.Settle(cost, requestID)
			}
			s.observeCost(entry, ReportedCost(getObj(result.response, "usage")))
			promptTokens, _ := getNumber(tokens, "prompt_tokens")
			completionTokens, _ := getNumber(tokens, "completion_tokens")
			if s.opts.Quota != nil {
				_ = s.opts.Quota.RecordTokens(quotaEntryOf(entry), int64(promptTokens+completionTokens)-int64(inputTokens), time.Now())
			}
			latency := result.latencyMs
			s.opts.Routing.OnSuccess(entry, measurementOf(latency, nil, int(completionTokens), latency), result.headers, time.Now())
			*trail = append(*trail, trailItem(entry, "success", newObjOf("ms", latency)))
			cachedTokens := routing.CachedTokens(int(promptTokens), int(cachedOf(tokens)))
			creationTokens, _ := getNumber(tokens, "cache_creation_input_tokens")
			answered()
			finish(usageFields{
				status: "success", httpStatus: http.StatusOK, modelRef: &entry.Ref, modelLabel: entry.ID, accountID: entry.AccountID,
				routingGroup: entry.RoutingGroup, cachedInputTokens: int64(cachedTokens), cacheCreation: int64(creationTokens),
				inputTokens: int64Ptr(promptTokens), outputTokens: int64Ptr(completionTokens), costUSD: &cost, isFree: boolPtr(entry.IsFree),
			})
			response := result.response
			if s.opts.RestoreResponse != nil && processing.Vault != nil {
				response = s.opts.RestoreResponse(response, processing.Vault)
			}
			withUsage := clone(response)
			withUsage.Set("usage", tokens)
			body := translator.ResponseFromCanonical(withUsage, entry.ID)
			// An extra top-level field: OpenAI and Anthropic SDKs ignore what they
			// do not know.
			if !run.peer {
				body.Set("neorouter", stats(latency, nil, tokens, cost, item))
			}
			httpx.WriteJSON(w, http.StatusOK, body)
			return nil
		}

		header := w.Header()
		header.Set("Content-Type", "text/event-stream; charset=utf-8")
		header.Set("Cache-Control", "no-cache, no-transform")
		header.Set("Connection", "keep-alive")
		header.Set("X-Accel-Buffering", "no")
		w.WriteHeader(http.StatusOK)
		controller := http.NewResponseController(w)
		flush := func() { _ = controller.Flush() }
		flush()

		accumulator := result.accumulator
		var restorer StreamRestorer
		if s.opts.NewStreamRestorer != nil && processing.Vault != nil {
			restorer = s.opts.NewStreamRestorer(processing.Vault)
		}
		var template *Obj
		outgoing := func(chunk *Obj) *Obj {
			if chunk == nil {
				return chunk
			}
			if id := getString(chunk, "id"); id != "" {
				template = newObjOf("id", id, "object", getString(chunk, "object"), "created", getValue(chunk, "created"), "model", getString(chunk, "model"))
			}
			if restorer != nil {
				return restorer.Process(chunk)
			}
			return chunk
		}
		chunks := func(yield func(*Obj, error) bool) {
			for _, chunk := range result.buffered {
				if !yield(outgoing(chunk), nil) {
					return
				}
			}
			for {
				chunk, err := result.reader.nextWithin(streamIdleTimeoutMS*time.Millisecond, "The stream stalled.")
				if err != nil {
					yield(nil, err)
					return
				}
				if chunk == nil {
					if restorer != nil {
						tailTemplate := template
						if tailTemplate == nil {
							tailTemplate = newObj()
						}
						if tail := restorer.Flush(tailTemplate); tail != nil {
							yield(tail, nil)
						}
					}
					return
				}
				accumulator.Push(chunk)
				if !yield(outgoing(chunk), nil) {
					return
				}
			}
		}
		var streamError *providers.ProviderError
		err := translator.StreamFromCanonical(chunks, entry.ID, func(event Event) bool {
			writeSSE(w, event, flush)
			return true
		})
		// The reader stops pumping once the stream is over, even when the
		// translator stopped early because the client went away.
		if result.reader != nil {
			result.reader.close()
		}
		if err != nil {
			streamError = restartable(err)
			if streamError != nil && streamError.Kind != providers.KindAborted {
				writeSSE(w, streamErrorEvent(dialect, streamError), flush)
			}
		}
		durationMs := millis(time.Since(item.started))
		tokens := accumulator.Usage
		completionTokens := 0.0
		promptTokens := float64(inputTokens)
		if tokens == nil {
			estimate := EstimateOutputTokens(accumulator)
			tokens = newObjOf("prompt_tokens", float64(inputTokens), "completion_tokens", float64(estimate))
			completionTokens = float64(estimate)
		} else {
			promptTokens, _ = getNumber(tokens, "prompt_tokens")
			completionTokens, _ = getNumber(tokens, "completion_tokens")
		}
		cost := 0.0
		if s.opts.Budget != nil {
			cost = budget.CostOf(priceOf(entry), budgetUsageOf(tokens))
		}
		// An SSE comment line: every SSE parser skips it, readers who want it can
		// parse it.
		if !run.peer && streamError == nil {
			_, _ = w.Write([]byte(": neorouter " + marshal(stats(durationMs, result.ttftMs, tokens, cost, item)) + "\n\n"))
			flush()
		}
		if item.reservation != nil {
			_ = item.reservation.Settle(cost, requestID)
		}
		s.observeCost(entry, accumulator.ReportedCost)
		if s.opts.Quota != nil {
			_ = s.opts.Quota.RecordTokens(quotaEntryOf(entry), int64(promptTokens+completionTokens)-int64(inputTokens), time.Now())
		}
		generationMs := durationMs
		ttftMs := result.ttftMs
		if ttftMs != nil {
			generationMs = durationMs - *ttftMs
		}
		switch {
		case streamError != nil:
			if streamError.Kind != providers.KindAborted {
				s.opts.Routing.OnFailure(entry, routingFailureOf(streamError), routing.FailureOptions{EstimatedTokens: inputTokens, Now: time.Now()})
			}
		case invalidAccumulatedCalls(accumulator):
			s.opts.Routing.OnFailure(entry, routingFailureOf(providers.NewError(providers.KindInvalidOutput, "Streamed tool-call arguments were not valid JSON.", providers.ErrorOptions{})), routing.FailureOptions{EstimatedTokens: inputTokens, Now: time.Now()})
		default:
			s.opts.Routing.RememberAffinity(plan.AffinityScope, sessionID, entry, time.Now())
			s.opts.Routing.OnSuccess(entry, measurementOf(durationMs, ttftMs, int(completionTokens), generationMs), result.headers, time.Now())
		}
		outcome := "success"
		if streamError != nil {
			outcome = "stream_" + string(streamError.Kind)
		}
		*trail = append(*trail, trailItem(entry, outcome, newObjOf("ms", durationMs)))
		status := "success"
		errorCode := ""
		errorMessage := ""
		if streamError != nil {
			if streamError.Kind != providers.KindAborted {
				status = "error"
				errorCode = string(streamError.Kind)
				errorMessage = streamError.Message
			} else {
				errorCode = "client_closed"
			}
		}
		cachedTokens := routing.CachedTokens(int(promptTokens), int(cachedOf(tokens)))
		creationTokens, _ := getNumber(tokens, "cache_creation_input_tokens")
		var ttftValue *int64
		if ttftMs != nil {
			value := int64(*ttftMs)
			ttftValue = &value
		}
		if status == "success" {
			answered()
		}
		finish(usageFields{
			status: status, httpStatus: http.StatusOK, errorCode: errorCode, errorMessage: errorMessage,
			modelRef: &entry.Ref, modelLabel: entry.ID, accountID: entry.AccountID,
			routingGroup: entry.RoutingGroup, cachedInputTokens: int64(cachedTokens), cacheCreation: int64(creationTokens),
			inputTokens: int64Ptr(promptTokens), outputTokens: int64Ptr(completionTokens), costUSD: &cost,
			isFree: boolPtr(entry.IsFree), ttftMs: ttftValue,
		})
		return nil
	}
}

// statsBody is stats(): routing facts about one answer, sent alongside it (never
// the prompt).
func (s *Service) statsBody(run *executeRun, plan *routing.RoutingPlan, requestID string, attemptsStarted *int, trail *[]*Obj) func(durationMs float64, ttftMs *float64, tokens *Obj, cost float64, item *attempt) *Obj {
	key := run.key
	return func(durationMs float64, ttftMs *float64, tokens *Obj, cost float64, item *attempt) *Obj {
		generationMs := durationMs
		if ttftMs != nil {
			generationMs = durationMs - *ttftMs
		}
		tried := make([]any, 0, len(*trail)+1)
		for _, step := range *trail {
			tried = append(tried, getString(step, "model"))
		}
		last := ""
		if len(tried) > 0 {
			last, _ = tried[len(tried)-1].(string)
		}
		if last != item.entry.ID {
			tried = append(tried, item.entry.ID)
		}
		completionTokens, _ := getNumber(tokens, "completion_tokens")
		var perSecond any
		if generationMs > 0 && completionTokens > 0 {
			perSecond = math.Round(completionTokens / (generationMs / 1000))
		}
		var maxRaised any
		if item.boosted {
			maxRaised = float64(item.maxTokens)
		}
		var group any
		if item.entry.RoutingGroup != "" {
			group = item.entry.RoutingGroup
		}
		var taskName any
		if plan.Task != nil {
			taskName = plan.Task.Primary
		}
		var ttftValue any
		if ttftMs != nil {
			ttftValue = math.Round(*ttftMs)
		}
		return newObjOf(
			"request_id", requestID,
			"model", item.entry.ID,
			"routing_group", group,
			"task", taskName,
			"attempts", float64(*attemptsStarted),
			"trail", tried,
			"latency_ms", math.Round(durationMs),
			"ttft_ms", ttftValue,
			"tokens_per_second", perSecond,
			"free", item.entry.IsFree,
			"cost_usd", cost,
			"max_tokens_raised_to", maxRaised,
			"limits", s.requestBudget(key),
		)
	}
}
