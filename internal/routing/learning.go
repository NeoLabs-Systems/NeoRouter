package routing

import (
	"net/http"
	"time"
)

// What routing learns from one attempt (learning.js).

// FailureOptions carries the context of a failed attempt.
type FailureOptions struct {
	EstimatedTokens int
	// MaxTokens is the output limit the attempt ran under, nil when the client
	// set none.
	MaxTokens *int
	// Boosted says the attempt was given extra output room already.
	Boosted bool
	// RequestSpecific says the request carried its own options (a voice, size or
	// format) that one model may not offer, so a refusal of it is not the
	// model's fault.
	RequestSpecific bool
	// Running is the account's requests in flight, this one included; nil reads
	// the load tracker.
	Running *int
	Now     time.Time
}

// OnFailure records what a failed attempt says about a model and returns the
// policy for the caller (whether another model makes sense).
func (s *Service) OnFailure(entry *Entry, failure Failure, opts FailureOptions) Policy {
	policy := FailurePolicy(failure.Kind)
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}
	now := opts.Now
	if failure.Kind == KindAborted {
		return policy
	}
	message := string(failure.Kind) + ": " + failure.Message
	softFailure := FailureRecord{Message: &message, CountsForBreaker: false}
	if opts.RequestSpecific && failure.Kind == KindBadRequest {
		s.stats.recordFailure(entry.Ref, softFailure, now)
		return policy
	}
	if isTaskFailure(failure.Kind) && !(failure.Kind == KindTruncated && opts.MaxTokens != nil && !opts.Boosted) {
		s.experience.RecordOutcome(entry.Ref, entry.Task, false, now)
	}
	if failure.Kind == KindTruncated && opts.MaxTokens != nil {
		s.stats.raiseMinOutputTokens(entry.Ref, int64(*opts.MaxTokens)*truncatedHeadroom, now)
		// Out of room at the client's own limit says the limit was small, not
		// that the model is unreliable: it only learns the room it needs. Once
		// it was given extra room and still ran out, it counts as usual.
		if !opts.Boosted {
			s.stats.recordFailure(entry.Ref, softFailure, now)
			return policy
		}
	}
	if failure.Kind == KindContextLength {
		s.learnContextWindow(entry, opts.EstimatedTokens)
		return policy
	}
	// "Too many concurrent requests" says nothing about the per-minute rate:
	// the account is busy, not used up, so no cooldown and no exhausted rpm.
	busy := false
	if failure.Kind == KindRateLimit {
		running := opts.Running
		if running == nil {
			inFlight := s.load.inflight(entry).Account
			running = &inFlight
		}
		busy = s.conc.onRateLimited(entry, failure, running, now)
	}
	// A connection a person marked as fixed gets a few failures forgiven: they
	// leave no cooldown, rest or penalty, so it is really tried again.
	forgiven := !busy && policy.PerAccount && s.health.takeChance(entry.AccountID)
	if !busy && !forgiven && policy.PerAccount {
		s.health.onFailure(entry.AccountID, now)
	}
	current := s.stats.snapshot(entry.Ref, now)
	var cooling *cooldown
	if !busy && !forgiven {
		cooling = s.cooldownFor(failure, current, now)
	}
	record := FailureRecord{
		// Busy is not unreliable: it neither costs reliability nor earns a
		// penalty.
		Weight:           policy.Weight,
		Penalty:          policy.Penalty,
		Message:          &message,
		CountsForBreaker: policy.Breaker && !forgiven,
	}
	if busy || forgiven {
		record.Weight = 0
		record.Penalty = 0
		record.CountsForBreaker = false
	}
	if cooling != nil {
		record.CooldownUntil = cooling.Until
		record.CooldownReason = &cooling.Reason
		record.Escalate = cooling.Escalate
	}
	s.stats.recordFailure(entry.Ref, record, now)
	if failure.Headers != nil {
		if s.opts.Quota != nil {
			s.opts.Quota.ObserveHeaders(entry, failure.Headers, now)
		}
		if s.opts.Billing != nil {
			s.opts.Billing.ObserveHeaders(entry, failure.Headers)
		}
		s.plansObserveHeaders(entry, failure.Headers, now)
	}
	if failure.Learned != nil && failure.Learned.Value != 0 && s.opts.Quota != nil {
		s.opts.Quota.LearnLimit(entry, *failure.Learned)
	}
	if failure.Kind == KindQuotaExhausted && s.opts.Billing != nil {
		s.opts.Billing.ObserveFailure(entry, failure)
	}
	if !forgiven && (failure.Kind == KindQuotaExhausted || (failure.Kind == KindRateLimit && !busy)) && s.opts.Quota != nil {
		metric := "rpm"
		if failure.Kind == KindQuotaExhausted {
			metric = "rpd"
		}
		var resetAt *time.Time
		if cooling != nil {
			at := fromMs(cooling.Until)
			resetAt = &at
		}
		s.opts.Quota.MarkExhausted(entry, metric, resetAt, now)
	}
	return policy
}

// OnSlow records an attempt that lost a hedge race: that only says something
// about speed.
func (s *Service) OnSlow(entry *Entry, elapsedMs float64, now time.Time) {
	if now.IsZero() {
		now = time.Now()
	}
	s.stats.recordSlow(entry.Ref, elapsedMs, now)
}

// OnSuccess records what a successful attempt measured.
func (s *Service) OnSuccess(entry *Entry, measurement Measurement, headers http.Header, now time.Time) {
	if now.IsZero() {
		now = time.Now()
	}
	s.stats.recordSuccess(entry.Ref, measurement, now)
	s.health.onSuccess(entry.AccountID)
	s.experience.RecordOutcome(entry.Ref, entry.Task, true, now)
	running := s.load.inflight(entry).Account
	s.conc.onSuccess(entry, &running, now)
	if headers != nil {
		if s.opts.Quota != nil {
			s.opts.Quota.ObserveHeaders(entry, headers, now)
		}
		if s.opts.Billing != nil {
			s.opts.Billing.ObserveHeaders(entry, headers)
		}
		s.plansObserveHeaders(entry, headers, now)
	}
}

// cooldownFor is how long a failure leaves a model alone (learning.cooldownFor).
func (s *Service) cooldownFor(failure Failure, row StatsRow, now time.Time) *cooldown {
	nowMs := now.UnixMilli()
	var retryAfter *int64
	if failure.RetryAfter != nil {
		value := minInt64(failure.RetryAfter.Milliseconds(), maxRetryAfterMs)
		retryAfter = &value
	}
	atLeast := func(step int64) int64 {
		if retryAfter != nil && *retryAfter > step {
			return *retryAfter
		}
		return step
	}
	switch failure.Kind {
	case KindRateLimit:
		answering := row.LastSuccessAt != nil && nowMs-*row.LastSuccessAt < stillAnsweringMs
		step := rateLimitCooldownMs
		if !answering {
			step = ladderStep(rateLimitLadderMs, row.CooldownLevel)
		}
		return &cooldown{Until: nowMs + atLeast(step), Reason: "rate_limited", Escalate: !answering}
	case KindQuotaExhausted:
		if failure.Learned != nil && failure.Learned.Value == 0 {
			return &cooldown{Until: nowMs + dayMs, Reason: "no_free_quota"}
		}
		step := ladderStep(quotaLadderMs, row.CooldownLevel)
		return &cooldown{Until: nowMs + atLeast(step), Reason: "quota_exhausted", Escalate: true}
	case KindPayment:
		return &cooldown{Until: nowMs + dayMs, Reason: "payment_required"}
	case KindAuth:
		return &cooldown{Until: nowMs + 30*minuteMs, Reason: "auth_failed"}
	case KindNotFound:
		if failure.NeedsAction {
			return &cooldown{Until: nowMs + ladderStep(needsActionLadderMs, row.CooldownLevel), Reason: "needs_action", Escalate: true}
		}
		// Listed but not deployed (e.g. NVIDIA "Function not found"): back off
		// further each time it is still missing.
		return &cooldown{Until: nowMs + ladderStep(notFoundLadderMs, row.CooldownLevel), Reason: "not_available", Escalate: true}
	// A planned outage that said when it ends ("back at 12:00 UTC").
	case KindServer:
		if retryAfter != nil {
			return &cooldown{Until: nowMs + *retryAfter, Reason: "scheduled_downtime"}
		}
		return nil
	case KindBadRequest:
		// One 400 may be the client's fault; the same model rejecting again
		// within a short window means it does not support this kind of request.
		repeated := row.LastFailureAt != nil && nowMs-*row.LastFailureAt < 15*minuteMs &&
			row.LastError != nil && len(*row.LastError) >= len("bad_request") && (*row.LastError)[:len("bad_request")] == "bad_request"
		if !repeated {
			return nil
		}
		ladder := []int64{hourMs, 6 * hourMs, dayMs}
		return &cooldown{Until: nowMs + ladderStep(ladder, row.CooldownLevel), Reason: "incompatible", Escalate: true}
	}
	return nil
}

// learnContextWindow learns a model's context window from a context-length
// refusal ("too long" at an estimated size).
func (s *Service) learnContextWindow(entry *Entry, estimatedTokens int) {
	if estimatedTokens == 0 || s.opts.DB == nil {
		return
	}
	limit := int64(estimatedTokens) * 9 / 10
	_, err := s.opts.DB.W.Exec(`UPDATE models SET context_window = ? WHERE id = ? AND (context_window IS NULL OR context_window > ?)`,
		limit, entry.Ref, limit)
	if err != nil {
		s.log().Warn("routing: could not learn a context window", "model", entry.Ref, "error", err)
	}
}

// truncatedHeadroom: a reasoning model needs room for thinking plus the answer.
// Remember a budget comfortably above the one that was not enough.
const truncatedHeadroom = 2
