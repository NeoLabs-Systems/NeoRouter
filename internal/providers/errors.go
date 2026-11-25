package providers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode/utf16"

	"github.com/NeoLabs-Systems/neolabs-go/llm"
)

// Kind is what a provider failure means, which is what routing reacts to:
// penalties, cooldowns and fallbacks are chosen from it. Keep this list closed
// and keep every kind handled in routing/learning.js's port.
type Kind string

const (
	// KindAuth: the provider refused the key.
	KindAuth Kind = "auth"
	// KindRateLimit: too many requests for now; retry after a short wait.
	KindRateLimit Kind = "rate_limit"
	// KindQuotaExhausted: the allowance is used up until it renews.
	KindQuotaExhausted Kind = "quota_exhausted"
	// KindPayment: the account cannot pay.
	KindPayment Kind = "payment"
	// KindContextLength: the request is longer than the model accepts.
	KindContextLength Kind = "context_length"
	// KindBadRequest: the provider rejected the request itself.
	KindBadRequest Kind = "bad_request"
	// KindNotFound: the model or endpoint does not exist for this account.
	KindNotFound Kind = "not_found"
	// KindServer: the provider failed.
	KindServer Kind = "server"
	// KindTimeout: no answer in time.
	KindTimeout Kind = "timeout"
	// KindNetwork: the connection failed.
	KindNetwork Kind = "network"
	// KindEmpty: the provider answered with nothing.
	KindEmpty Kind = "empty"
	// KindInvalidOutput: the provider answered with something unusable.
	KindInvalidOutput Kind = "invalid_output"
	// KindTruncated: the answer stopped at the token limit.
	KindTruncated Kind = "truncated"
	// KindAborted: the caller cancelled.
	KindAborted Kind = "aborted"
)

// Kinds lists every kind, in provider_error.js order.
var Kinds = []Kind{
	KindAuth, KindRateLimit, KindQuotaExhausted, KindPayment, KindContextLength,
	KindBadRequest, KindNotFound, KindServer, KindTimeout, KindNetwork, KindEmpty,
	KindInvalidOutput, KindTruncated, KindAborted,
}

// Valid reports whether the kind is one of Kinds.
func (k Kind) Valid() bool { return oneOf(k, Kinds...) }

// LearnedLimit is a limit pulled out of a rate-limit message, for the quota
// tracker to learn limits the provider does not publish in headers.
type LearnedLimit struct {
	Metric string
	Value  float64
}

// ProviderError is one classified provider failure.
type ProviderError struct {
	Kind    Kind
	Message string
	// Status is the HTTP status, or 0 when the failure had none.
	Status int
	// RetryAfter is how long the provider asked to wait, when RetryAfterKnown.
	RetryAfter      time.Duration
	RetryAfterKnown bool
	// Body is the provider's response body, decoded.
	Body any
	// Headers are the response headers, for the quota tracker.
	Headers http.Header
	// Learned is a limit read from the message.
	Learned *LearnedLimit
	// NeedsAction means only a person can fix this (a consent, a verification,
	// a plan); trying again by itself cannot help.
	NeedsAction bool
}

// Error implements error.
func (e *ProviderError) Error() string { return e.Message }

// ErrorOptions are the facts about a failure besides its kind and message.
type ErrorOptions struct {
	Status      int
	RetryAfter  *time.Duration
	Body        any
	Headers     http.Header
	Learned     *LearnedLimit
	NeedsAction bool
}

// NewError builds a classified failure.
func NewError(kind Kind, message string, opts ErrorOptions) *ProviderError {
	err := &ProviderError{
		Kind:        kind,
		Message:     message,
		Status:      opts.Status,
		Body:        opts.Body,
		Headers:     opts.Headers,
		Learned:     opts.Learned,
		NeedsAction: opts.NeedsAction,
	}
	if opts.RetryAfter != nil {
		err.RetryAfter = *opts.RetryAfter
		err.RetryAfterKnown = true
	}
	return err
}

// AsProviderError returns the classified failure inside err, or nil.
func AsProviderError(err error) *ProviderError {
	var target *ProviderError
	if errors.As(err, &target) {
		return target
	}
	return nil
}

// KindOf is the kind of a wrapped failure, or "" when it carries none. Routing
// reads it to pick a penalty, cooldown or fallback.
func KindOf(err error) Kind {
	if classified := AsProviderError(err); classified != nil {
		return classified.Kind
	}
	return ""
}

// FromNetwork turns a transport failure into a classified failure.
func FromNetwork(err error) *ProviderError {
	if err == nil {
		return nil
	}
	if classified := AsProviderError(err); classified != nil {
		return classified
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return NewError(KindTimeout, "The provider did not respond in time.", ErrorOptions{})
	}
	if errors.Is(err, context.Canceled) {
		return NewError(KindAborted, "The request was aborted.", ErrorOptions{})
	}
	cause := networkCode(err)
	message := "Network error"
	if cause != "" {
		message += " (" + cause + ")"
	}
	message += ": " + err.Error()
	return NewError(KindNetwork, message, ErrorOptions{})
}

// ToLLM renders a classified failure as the neolabs-go llm error, for code that
// shares the library's retry and stream guards.
func (e *ProviderError) ToLLM(provider string) *llm.Error {
	out := &llm.Error{Provider: provider, Status: e.Status, Message: e.Message}
	if e.RetryAfterKnown {
		out.RetryAfter = e.RetryAfter
	}
	if e.Status != 0 {
		out.Retryable = llm.RetryableStatus(e.Status)
	} else {
		switch e.Kind {
		case KindRateLimit, KindServer, KindTimeout, KindNetwork:
			out.Retryable = true
		}
	}
	return out
}

// FromLLM classifies a failure that came back from the neolabs-go llm library
// (adapters speak to providers through it), so routing sees the same kinds and
// texts it saw from the JS adapters. The library prefixes its message with the
// HTTP status; that prefix is removed first, because the JS texts are the
// provider's own words.
func FromLLM(err error) *ProviderError {
	if err == nil {
		return nil
	}
	if classified := AsProviderError(err); classified != nil {
		return classified
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return NewError(KindTimeout, "The provider did not respond in time.", ErrorOptions{})
	}
	if errors.Is(err, context.Canceled) {
		return NewError(KindAborted, "The request was aborted.", ErrorOptions{})
	}
	inner := llm.AsError(err)
	if inner == nil {
		return FromNetwork(err)
	}
	if inner.Status == 0 {
		kind := KindInvalidOutput
		if inner.Retryable || llm.IsTransient(err) {
			kind = KindNetwork
		}
		return NewError(kind, stripStatusPrefix(inner.Message), ErrorOptions{})
	}
	body := map[string]any{"error": map[string]any{"message": stripStatusPrefix(inner.Message)}}
	classified := ClassifyHTTP(inner.Status, body, nil)
	if !classified.RetryAfterKnown && inner.RetryAfter > 0 {
		classified.RetryAfter, classified.RetryAfterKnown = inner.RetryAfter, true
	}
	return classified
}

// stripStatusPrefix removes the "<status> " prefix the llm library puts in
// front of a provider's message.
func stripStatusPrefix(message string) string {
	head, rest, found := strings.Cut(message, " ")
	if !found {
		return message
	}
	if _, err := strconv.Atoi(head); err != nil {
		return message
	}
	return rest
}

// networkCode is the syscall error name Node puts in a network message, such as
// ECONNREFUSED.
func networkCode(err error) string {
	for _, candidate := range []struct {
		name   string
		target error
	}{
		{"ECONNREFUSED", syscall.ECONNREFUSED},
		{"ECONNRESET", syscall.ECONNRESET},
		{"ETIMEDOUT", syscall.ETIMEDOUT},
		{"ENETUNREACH", syscall.ENETUNREACH},
		{"EHOSTUNREACH", syscall.EHOSTUNREACH},
		{"EPIPE", syscall.EPIPE},
	} {
		if errors.Is(err, candidate.target) {
			return candidate.name
		}
	}
	return ""
}

// truncate keeps a message to 2000 UTF-16 units, the bound provider_error.js
// applies with String.prototype.slice.
func truncate(text string) string {
	units := utf16.Encode([]rune(text))
	if len(units) <= 2000 {
		return text
	}
	return string(utf16.Decode(units[:2000]))
}

// compactJSON is JSON.stringify for the error bodies that are objects.
func compactJSON(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprint(value)
	}
	return string(encoded)
}
