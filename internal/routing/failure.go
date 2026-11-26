package routing

import (
	"net/http"
	"time"
)

// Kind is what a provider failure was, as far as routing reacts to it. The
// providers domain maps its errors onto these values; routing never guesses a
// kind from a message.
type Kind string

// The error kinds routing knows.
const (
	KindAuth           Kind = "auth"
	KindRateLimit      Kind = "rate_limit"
	KindQuotaExhausted Kind = "quota_exhausted"
	KindPayment        Kind = "payment"
	KindContextLength  Kind = "context_length"
	KindBadRequest     Kind = "bad_request"
	KindNotFound       Kind = "not_found"
	KindServer         Kind = "server"
	KindTimeout        Kind = "timeout"
	KindNetwork        Kind = "network"
	KindEmpty          Kind = "empty"
	KindInvalidOutput  Kind = "invalid_output"
	KindTruncated      Kind = "truncated"
	KindAborted        Kind = "aborted"
)

// LearnedLimit is a limit a provider's refusal named ("limit: 1,000 requests
// per day").
type LearnedLimit struct {
	Metric string
	Value  float64
}

// Failure is one failed attempt as learning.go reads it: the kind, the message,
// and whatever the provider told us about when to come back. It is the routing
// view of a providers.ProviderError.
type Failure struct {
	Kind    Kind
	Message string
	Status  int
	// RetryAfter is the provider's retry hint; nil when it gave none.
	RetryAfter  *time.Duration
	NeedsAction bool
	Learned     *LearnedLimit
	Headers     http.Header
}

// Policy is how a kind of error changes what the router believes about a model.
// Fallback says whether trying another model makes sense for this request.
type Policy struct {
	Weight     float64
	Penalty    float64
	Breaker    bool
	Fallback   bool
	PerAccount bool
}

// FailurePolicy is how a kind of error changes what the router believes about
// a model (learning.policyFor).
func FailurePolicy(kind Kind) Policy {
	switch kind {
	case KindRateLimit:
		return Policy{Weight: 0.5, Penalty: 2, Fallback: true, PerAccount: true}
	case KindQuotaExhausted:
		return Policy{Weight: 0.5, Penalty: 1, Fallback: true, PerAccount: true}
	case KindPayment:
		return Policy{Weight: 1, Penalty: 3, Fallback: true, PerAccount: true}
	case KindAuth:
		return Policy{Weight: 1, Penalty: 3, Fallback: true, PerAccount: true}
	case KindNotFound:
		return Policy{Weight: 1, Penalty: 2, Fallback: true, PerAccount: true}
	case KindContextLength:
		return Policy{Weight: 0, Penalty: 0, Fallback: true, PerAccount: true}
	case KindTruncated:
		return Policy{Weight: 0.25, Penalty: 0.5, Fallback: true}
	case KindBadRequest:
		return Policy{Weight: 0.5, Penalty: 0.5, Fallback: true}
	case KindTimeout:
		return Policy{Weight: 1, Penalty: 1.5, Breaker: true, Fallback: true, PerAccount: true}
	case KindServer, KindNetwork, KindEmpty, KindInvalidOutput:
		return Policy{Weight: 1, Penalty: 1, Breaker: true, Fallback: true, PerAccount: true}
	case KindAborted:
		return Policy{Weight: 0, Penalty: 0, Fallback: false, PerAccount: true}
	default:
		return Policy{Weight: 1, Penalty: 1, Breaker: true, Fallback: true, PerAccount: true}
	}
}

// taskFailures are the failures that say the model handled this kind of task
// badly (as opposed to limits, outages or bad requests, which say nothing
// about the task).
func isTaskFailure(kind Kind) bool {
	return kind == KindEmpty || kind == KindInvalidOutput || kind == KindTruncated
}

// connectionFailures are the failures that say the connection is struggling,
// not that this request is odd. A single request may cost one connection this
// many failures (MAX_FAILURES_PER_ACCOUNT in router.go).
var connectionFailures = map[Kind]bool{
	KindRateLimit:      true,
	KindQuotaExhausted: true,
	KindPayment:        true,
	KindAuth:           true,
	KindNotFound:       true,
	KindServer:         true,
	KindNetwork:        true,
	KindTimeout:        true,
}

const (
	secondMs = int64(1000)
	minuteMs = 60 * secondMs
	hourMs   = 60 * minuteMs
	dayMs    = 24 * hourMs
)

// Cooldown ladders. One side effect of a failure is how long the model is left
// alone.
var (
	rateLimitCooldownMs = 90 * secondMs
	quotaLadderMs       = []int64{2 * minuteMs, 10 * minuteMs, hourMs, dayMs}
	notFoundLadderMs    = []int64{6 * hourMs, dayMs, 7 * dayMs}
	// Something only a person can do (a consent, a verification): checked
	// again rarely, and never as a long shot.
	needsActionLadderMs = []int64{dayMs, 3 * dayMs, 7 * dayMs}
	// A rate limit on a model that recently answered ends soon. One that has
	// not answered in an hour keeps refusing, so it is asked less and less
	// often.
	rateLimitLadderMs = []int64{rateLimitCooldownMs, 5 * minuteMs, 30 * minuteMs, 2 * hourMs, 6 * hourMs}
	stillAnsweringMs  = hourMs
	maxRetryAfterMs   = dayMs
)

// cooldown is how long a model is left alone after a failure, and why.
type cooldown struct {
	Until    int64
	Reason   string
	Escalate bool
}

func ladderStep(ladder []int64, level int64) int64 {
	index := level
	if index < 0 {
		index = 0
	}
	if index >= int64(len(ladder)) {
		index = int64(len(ladder) - 1)
	}
	return ladder[index]
}
