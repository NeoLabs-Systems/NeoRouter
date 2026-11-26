package gateway

// One request of a non-chat kind (embeddings, images, speech, transcription,
// System One, video job creation)
// through the same machinery as chat: key policy and budget, routing over the
// models of that kind, fallback across providers, learning from every attempt,
// quotas and the usage log. Streaming and hedging are chat-only; these calls are
// single-shot.
//
// internal/media is the caller: it passes what to call, what it costs and how to
// answer, and gets back the result and the model that produced it.

import (
	"context"
	"errors"
	"math"
	"math/rand"
	"net/http"
	"time"

	"github.com/NeoLabs-Systems/neorouter/internal/budget"
	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
	"github.com/NeoLabs-Systems/neorouter/internal/providers"
	"github.com/NeoLabs-Systems/neorouter/internal/routing"
	"github.com/NeoLabs-Systems/neorouter/internal/usage"
)

// capabilityBadRequests is how many consecutive bad requests are tolerated.
const capabilityBadRequests = 2

// CapabilityResult is what one non-chat attempt produced.
type CapabilityResult struct {
	// Value is what the caller's Invoke returned (the response body a media
	// handler writes).
	Value any
	// Headers are the upstream response headers.
	Headers http.Header
	// Units is how many units (images, seconds, searches) the provider billed.
	Units *float64
	// InputTokens is the prompt size the provider reported.
	InputTokens *int
}

// CapabilityRun is one non-chat request: what runCapability's options say.
type CapabilityRun struct {
	// W is the response writer, when the caller answers directly; nil when the
	// result is returned instead.
	W http.ResponseWriter
	R *http.Request
	// Key is the router key the request came from.
	Key *Key
	// Capability is the catalog kind, e.g. "embedding".
	Capability string
	// Method is the adapter method that serves it, e.g. providers.CapEmbed.
	Method providers.Capability
	// RequestedModel is what the client asked for ("auto" when absent).
	RequestedModel string
	// InputTokens is the size estimate for quota checks and the log.
	InputTokens int
	// ContextTokens is the text that must fit the model's context (defaults to
	// InputTokens).
	ContextTokens int
	// Units are the billed units for the log.
	Units float64
	// Format is the api_format of the usage row (defaults to Capability).
	Format string
	// FreeOnly keeps this request on free models.
	FreeOnly bool
	// RedactedCount is what the privacy shield hid, for the log.
	RedactedCount int64

	// Invoke calls the adapter for one attempt.
	Invoke func(ctx context.Context, entry *routing.Entry, adapter providers.Adapter, timeout time.Duration) (*CapabilityResult, error)
	// CostOf is what one attempt actually cost, in USD.
	CostOf func(entry *routing.Entry, result *CapabilityResult) float64
	// EstimateCost is the worst case before the call (nil = unknown).
	EstimateCost func(entry *routing.Entry) *float64
	// Filter is an extra candidate filter (embedding pins, timestamps).
	Filter func(entry *routing.Entry, adapter providers.Adapter) bool
	// OnNoCandidate explains an empty candidate list.
	OnNoCandidate func() error
	// Respond writes the HTTP response; when nil the result is returned.
	Respond func(result *CapabilityResult, entry *routing.Entry) error
}

// trailForCapability is one line of the trail.
func trailForCapability(entry *routing.Entry, outcome string, extra *Obj) *Obj {
	item := newObjOf("model", entry.ID, "outcome", outcome)
	if entry.TryingLuck {
		item.Set("luck", true)
	}
	if entry.LastResort {
		item.Set("lastResort", true)
	}
	setAll(item, extra)
	return item
}

// RunCapability is runCapability: one non-chat request end to end. It returns the
// result and the entry that produced it, and the failure the caller must answer
// with (an *httpserver.HttpError) when every candidate failed.
func (s *Service) RunCapability(run *CapabilityRun) (*CapabilityResult, *routing.Entry, error) {
	if s.opts.Concurrency != nil {
		release, err := s.opts.Concurrency.Acquire(run.Key.Row)
		if err != nil {
			return nil, nil, err
		}
		defer release()
	}
	requestID := httpserver.RequestIDFrom(run.R.Context())
	if requestID == "" {
		requestID = newRequestID()
	}
	startedAt := time.Now()
	trail := make([]*Obj, 0, 4)
	key := run.Key
	capability := run.Capability
	format := run.Format
	if format == "" {
		format = capability
	}
	inputTokens := run.InputTokens
	contextTokens := run.ContextTokens
	if contextTokens == 0 {
		contextTokens = inputTokens
	}
	estimatedCost := func(entry *routing.Entry) *float64 {
		if run.EstimateCost == nil {
			return nil
		}
		return run.EstimateCost(entry)
	}

	if s.opts.Keys != nil {
		result := s.opts.Keys.ConsumeRPM(key.Row, time.Now())
		if window := s.opts.Keys.RPMStateOf(key.Row, time.Now()); window != nil && run.W != nil {
			run.W.Header().Set("x-ratelimit-limit-requests", jsString(float64(window.Limit)))
			run.W.Header().Set("x-ratelimit-remaining-requests", jsString(float64(window.Remaining)))
			run.W.Header().Set("x-ratelimit-reset-requests", jsString(float64(window.ResetSeconds))+"s")
		}
		if !result.Allowed {
			if run.W != nil {
				run.W.Header().Set("Retry-After", jsString(float64(result.RetryAfterSeconds)))
			}
			err := httpserver.New(http.StatusTooManyRequests, "KEY_RATE_LIMITED", "This router key exceeded its requests-per-minute limit.")
			err.RetryAfter = result.RetryAfterSeconds
			return nil, nil, err
		}
	}
	budgetExhausted := s.budgetExhausted(key)
	// A used-up budget stops a key only when it has nothing free to fall back on.
	if budgetExhausted && key.Row.Mode == "paid" && !key.Row.AllowPlan {
		message := "This router key has used its " + key.Row.BudgetPeriod + " budget of " + jsString(*key.Row.BudgetUSD) + " USD."
		return nil, nil, httpserver.New(http.StatusPaymentRequired, "BUDGET_EXHAUSTED", message)
	}

	plan, err := s.opts.Routing.NewPlan(routing.PlanOptions{
		UserID:         key.UserID(),
		Key:            key.Routing,
		Request:        routing.RequestInfo{},
		RequestedModel: run.RequestedModel,
		InputTokens:    contextTokens,
		FreeOnly:       run.FreeOnly || budgetExhausted,
		Capability:     capability,
		EstimateCost:   estimatedCost,
	})
	if err != nil {
		return nil, nil, err
	}
	if pacing := s.pacingState(key); pacing != "" && run.W != nil {
		run.W.Header().Set("X-NeoRouter-Budget-Pace", pacing)
	}

	queuedMs := 0.0
	var freesUpAt *int64
	finish := func(fields usageFields) {
		if s.opts.Usage == nil {
			return
		}
		entry := usage.Entry{
			ID:             requestID,
			UserID:         key.UserID(),
			RouterKeyID:    stringPtr(key.ID()),
			CreatedAt:      startedAt.UnixMilli(),
			APIFormat:      format,
			RequestedModel: run.RequestedModel,
			Attempts:       int64(math.Max(1, float64(len(trail)))),
			TrailJSON:      marshalAny(trail),
			QueuedMS:       int64(math.Round(queuedMs)),
			LatencyMS:      int64Ptr(millis(time.Since(startedAt))),
			InputTokens:    int64(inputTokens),
			Capability:     capability,
			Units:          run.Units,
			RedactedCount:  run.RedactedCount,
			Status:         fields.status,
		}
		if fields.status == "" {
			entry.Status = "error"
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
		if fields.units != nil {
			entry.Units = *fields.units
		}
		if recordErr := s.opts.Usage.Record(entry); recordErr != nil {
			s.log().Error("Failed to record request", "requestId", requestID, "error", recordErr)
		}
	}

	// Only providers whose adapter speaks this kind are candidates.
	supported := func(entry *routing.Entry) bool {
		if s.opts.Adapters == nil {
			return false
		}
		if !s.opts.Adapters.CanServe(entry.AccountID, run.Method) {
			return false
		}
		if run.Filter == nil {
			return true
		}
		adapter := s.opts.Adapters.Adapter(entry.AccountID)
		if adapter == nil {
			return false
		}
		return run.Filter(entry, adapter)
	}

	config := s.cfg()
	var lastError *providers.ProviderError
	badRequests := 0
	overBudget := false
	// Like chat: when every candidate is only blocked for a moment, wait for the
	// first one to free up instead of failing.
	waitUntil, _ := s.queueWindows(plan, key, startedAt, startedAt.Add(config.Routing.TotalBudget))
	totalBudget := config.Routing.TotalBudget
	nextChoice := func() *routing.Candidate {
		defer plan.LeaveQueue()
		for {
			if choice := plan.Next(routing.NextOptions{Filter: supported}); choice != nil {
				return choice
			}
			now := time.Now()
			freesUpAt = plan.FreesUpAt(now, supported)
			if freesUpAt == nil || time.UnixMilli(*freesUpAt).After(waitUntil) {
				// Better to try what may work again than to answer "no model
				// available".
				if plan.EnableLastResort(now, supported) {
					continue
				}
				if plan.EnableLongShots(now, supported) {
					continue
				}
				return nil
			}
			plan.JoinQueue(now, supported)
			pause := time.Until(waitUntil)
			free := time.Until(time.UnixMilli(*freesUpAt))
			poll := math.Max(queueMinPollMs, math.Min(millis(free), queuePollMs)) * (0.5 + rand.Float64())
			if wait := time.Duration(poll) * time.Millisecond; wait < pause {
				pause = wait
			}
			select {
			case <-time.After(pause):
			case <-run.R.Context().Done():
				return nil
			}
			queuedMs += millis(time.Since(now))
		}
	}

	for len(trail) < config.Routing.MaxAttempts && time.Since(startedAt) < totalBudget {
		choice := nextChoice()
		if choice == nil {
			break
		}
		entry := choice.Entry
		adapter := s.opts.Adapters.Adapter(entry.AccountID)
		if adapter == nil {
			continue
		}
		var reservation *budget.Reservation
		if s.opts.Budget != nil {
			var affordable bool
			if reservation, affordable = s.tryReserve(key, estimatedCost(entry)); !affordable {
				overBudget = true
				continue
			}
		}
		s.opts.Routing.PlanSent(entry)
		release := s.opts.Routing.Lease(entry)
		if s.opts.Quota != nil {
			_ = s.opts.Quota.RecordRequest(quotaEntryOf(entry), int64(inputTokens), time.Now())
		}
		attemptStarted := time.Now()
		timeout := totalBudget - time.Since(startedAt)
		if s.opts.Adapters != nil {
			if own := s.opts.Adapters.Timeout(entry.AccountID); own > 0 && own < timeout {
				timeout = own
			}
		}
		if timeout < time.Second {
			timeout = time.Second
		}
		ctx, cancel := context.WithTimeout(run.R.Context(), timeout)
		result, invokeErr := run.Invoke(ctx, entry, adapter, timeout)
		cancel()
		if invokeErr == nil {
			latencyMs := millis(time.Since(attemptStarted))
			cost := 0.0
			if run.CostOf != nil {
				cost = run.CostOf(entry, result)
			}
			if reservation != nil {
				_ = reservation.Settle(cost, requestID)
			}
			s.opts.Routing.OnSuccess(entry, measurementOf(latencyMs, nil, 0, latencyMs), result.Headers, time.Now())
			trail = append(trail, trailForCapability(entry, "success", newObjOf("ms", latencyMs)))
			if run.W != nil {
				run.W.Header().Set("X-NeoRouter-Model", entry.ID)
				models := make([]string, 0, len(trail))
				for _, item := range trail {
					models = append(models, getString(item, "model"))
				}
				run.W.Header().Set("X-NeoRouter-Trail", joinComma(models))
				if run.RedactedCount > 0 {
					run.W.Header().Set("X-NeoRouter-Redacted", jsString(float64(run.RedactedCount)))
				}
			}
			units := run.Units
			if result.Units != nil {
				units = *result.Units
			}
			reportedInput := inputTokens
			if result.InputTokens != nil {
				reportedInput = *result.InputTokens
			}
			finish(usageFields{
				status: "success", httpStatus: http.StatusOK, modelRef: &entry.Ref, modelLabel: entry.ID,
				accountID: entry.AccountID, costUSD: &cost, isFree: boolPtr(entry.IsFree),
				units: &units, inputTokens: int64Ptr(float64(reportedInput)),
			})
			release()
			// Without Respond (internal callers such as chat web search) the
			// result is returned.
			if run.Respond == nil {
				return result, entry, nil
			}
			if err := run.Respond(result, entry); err != nil {
				return result, entry, err
			}
			return result, entry, nil
		}
		if reservation != nil {
			reservation.Release()
		}
		classified := restartable(invokeErr)
		var httpErr *httpserver.HttpError
		if errors.As(invokeErr, &httpErr) {
			release()
			return nil, nil, httpErr
		}
		lastError = classified
		trail = append(trail, trailForCapability(entry, string(classified.Kind), newObjOf(
			"status", classified.Status,
			"ms", millis(time.Since(attemptStarted)),
			"message", truncateText(classified.Message, 200),
		)))
		// Media requests bring their own voice, size or format: a refusal of those
		// is about this request, not the model.
		policy := s.opts.Routing.OnFailure(entry, routingFailureOf(classified), routing.FailureOptions{
			EstimatedTokens: inputTokens,
			RequestSpecific: true,
			Now:             time.Now(),
		})
		plan.NoteFailure(entry, routing.Kind(classified.Kind))
		if classified.Kind == providers.KindAuth && s.opts.Adapters != nil {
			go s.opts.Adapters.Validate(entry.AccountID)
		}
		if classified.Kind == providers.KindBadRequest {
			badRequests++
		} else {
			badRequests = 0
		}
		release()
		if !policy.Fallback || badRequests >= capabilityBadRequests {
			break
		}
	}

	if len(trail) == 0 && run.OnNoCandidate != nil {
		if explained := run.OnNoCandidate(); explained != nil {
			info := errorOf(explained)
			finish(usageFields{status: "error", httpStatus: info.status, errorCode: info.code, errorMessage: info.message})
			return nil, nil, explained
		}
	}
	rejected := plan.RejectionSummary(time.Now(), supported)
	if overBudget && rejected["budget_exhausted"] == 0 {
		rejected["budget_exhausted"] = 1
	}
	reasons := make([]string, 0, len(rejected))
	for reason := range rejected {
		reasons = append(reasons, reason)
	}
	if !routing.ParseModel(run.RequestedModel).Auto && len(trail) == 0 && len(reasons) > 0 && allIn(reasons, keyPolicyRejections) {
		why := "is not allowed for this key"
		if rejected["not_free"] > 0 {
			why = "is not free and this key only allows free models"
		}
		message := "The model \"" + run.RequestedModel + "\" " + why + "."
		finish(usageFields{status: "error", httpStatus: http.StatusForbidden, errorCode: "MODEL_NOT_ALLOWED", errorMessage: message})
		return nil, nil, httpserver.New(http.StatusForbidden, "MODEL_NOT_ALLOWED", message, rejected)
	}
	status := http.StatusBadGateway
	if len(trail) == 0 {
		switch {
		case lastError != nil && lastError.Kind == providers.KindBadRequest && badRequests >= capabilityBadRequests:
			status = http.StatusBadRequest
		case budgetExhausted:
			status = http.StatusPaymentRequired
		case rejected["token_limit"] > 0:
			status = http.StatusTooManyRequests
		default:
			status = http.StatusServiceUnavailable
		}
	} else if lastError != nil && lastError.Kind == providers.KindBadRequest && badRequests >= capabilityBadRequests {
		status = http.StatusBadRequest
	}
	code := "ALL_ATTEMPTS_FAILED"
	message := "All " + jsString(float64(len(trail))) + " attempt(s) failed. Last error: "
	if len(trail) == 0 {
		if rejected["token_limit"] > 0 {
			code = "TOKEN_LIMIT_REACHED"
		} else {
			code = "NO_MODEL_AVAILABLE"
		}
		message = routing.ExplainNoModel(rejected, capability)
	} else {
		last := "unknown"
		if lastError != nil {
			last = lastError.Message
		}
		message += last
	}
	finish(usageFields{status: "error", httpStatus: status, errorCode: code, errorMessage: message})
	details := map[string]any{"trail": trail, "rejected": rejected}
	httpErr := httpserver.New(status, code, message, details)
	if freesUpAt != nil {
		httpErr.RetryAfter = secondsUntil(*freesUpAt)
		details["retryAfterSeconds"] = httpErr.RetryAfter
		if run.W != nil {
			setRetryAfter(run.W, httpErr.RetryAfter)
		}
	}
	return nil, nil, httpErr
}
