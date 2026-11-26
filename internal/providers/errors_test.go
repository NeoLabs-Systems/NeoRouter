package providers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/llm"
)

// TestClassify replays every recorded error body and status and requires the
// same kind, message, wait and learned limit.
func TestClassify(t *testing.T) {
	cases := loadFixture(t).Classify
	if len(cases) == 0 {
		t.Fatal("no classify fixtures")
	}
	for _, testCase := range cases {
		name := fmt.Sprintf("%d/%s", testCase.Status, describe(testCase.Body))
		t.Run(name, func(t *testing.T) {
			headers := http.Header{}
			for field, value := range testCase.Headers {
				headers.Set(field, value)
			}
			err := ClassifyHTTP(testCase.Status, testCase.Body, headers)
			if got := string(err.Kind); got != testCase.Expect.Kind {
				t.Fatalf("kind = %q, want %q (%q)", got, testCase.Expect.Kind, err.Message)
			}
			if err.Message != testCase.Expect.Message {
				t.Fatalf("message = %q, want %q", err.Message, testCase.Expect.Message)
			}
			if err.NeedsAction != testCase.Expect.NeedsAction {
				t.Fatalf("needsAction = %v, want %v", err.NeedsAction, testCase.Expect.NeedsAction)
			}
			if testCase.Expect.RetryAfterMS == nil {
				if err.RetryAfterKnown {
					t.Fatalf("retryAfter = %v, want none", err.RetryAfter)
				}
			} else {
				if !err.RetryAfterKnown {
					t.Fatalf("retryAfter unknown, want %v ms", *testCase.Expect.RetryAfterMS)
				}
				if got := float64(err.RetryAfter.Milliseconds()); got != *testCase.Expect.RetryAfterMS {
					t.Fatalf("retryAfter = %v ms, want %v ms", got, *testCase.Expect.RetryAfterMS)
				}
			}
			if !reflect.DeepEqual(err.Learned, testCase.Expect.Learned) {
				t.Fatalf("learned = %+v, want %+v", err.Learned, testCase.Expect.Learned)
			}
		})
	}
}

// describe names a body for a subtest.
func describe(body any) string {
	raw := fmt.Sprintf("%v", body)
	if len(raw) > 60 {
		raw = raw[:60]
	}
	return strings.ReplaceAll(raw, " ", "_")
}

// TestAnnouncedReturn reads the announced outages.
func TestAnnouncedReturn(t *testing.T) {
	for _, testCase := range loadFixture(t).AnnouncedReturn {
		now := time.UnixMilli(testCase.Now)
		got, ok := AnnouncedReturn(testCase.Message, now)
		if testCase.Expect == nil {
			if ok {
				t.Fatalf("%q: got %v, want no announcement", testCase.Message, got)
			}
			continue
		}
		if !ok {
			t.Fatalf("%q: no announcement, want %v ms", testCase.Message, *testCase.Expect)
		}
		if ms := float64(got.Milliseconds()); ms != *testCase.Expect {
			t.Fatalf("%q: got %v ms, want %v ms", testCase.Message, ms, *testCase.Expect)
		}
	}
}

// TestLearnFromMessage reads the limits learned from messages.
func TestLearnFromMessage(t *testing.T) {
	for _, testCase := range loadFixture(t).LearnFromMessage {
		if got := LearnFromMessage(testCase.Message); !reflect.DeepEqual(got, testCase.Expect) {
			t.Fatalf("%q: got %+v, want %+v", testCase.Message, got, testCase.Expect)
		}
	}
}

// TestExtractMessage reads the message out of every body shape.
func TestExtractMessage(t *testing.T) {
	for _, testCase := range loadFixture(t).ExtractMessage {
		if got := ExtractMessage(testCase.Body); got != testCase.Expect {
			t.Fatalf("%v: got %q, want %q", testCase.Body, got, testCase.Expect)
		}
	}
}

// TestAllowanceUsedUp keeps the plan-limit wording in step.
func TestAllowanceUsedUp(t *testing.T) {
	for _, testCase := range loadFixture(t).AllowanceUsedUp {
		if got := AllowanceUsedUp(testCase.Message); got != testCase.Expect {
			t.Fatalf("%q: got %v, want %v", testCase.Message, got, testCase.Expect)
		}
	}
}

// TestProviderProblems: the refusals a real server produced, read by what they
// mean.
func TestProviderProblems(t *testing.T) {
	cloudflare := ClassifyHTTP(403, map[string]any{
		"errors": []any{map[string]any{"message": "AiError: Model @cf/zai-org/glm-5.3 is not available on the Workers Free plan"}},
	}, nil)
	if cloudflare.Kind != KindNotFound || !cloudflare.NeedsAction {
		t.Fatalf("cloudflare plan refusal = %+v", cloudflare)
	}
	if cloudflare.Message != "AiError: Model @cf/zai-org/glm-5.3 is not available on the Workers Free plan" {
		t.Fatalf("cloudflare message = %q", cloudflare.Message)
	}
	if auth := ClassifyHTTP(401, map[string]any{"error": map[string]any{"message": "Invalid API key"}}, nil); auth.Kind != KindAuth || auth.NeedsAction {
		t.Fatalf("invalid key = %+v", auth)
	}
	// Ollama Cloud's included usage used up is a limit, not a broken connection.
	for _, status := range []int{402, 429, 403} {
		err := ClassifyHTTP(status, map[string]any{"error": "Add usage credits or subscribe and save up to 66%: https://ollama.com/plans"}, nil)
		if err.Kind != KindQuotaExhausted {
			t.Fatalf("HTTP %d: kind = %q, want %q", status, err.Kind, KindQuotaExhausted)
		}
	}
	if err := ClassifyHTTP(402, map[string]any{"error": "Insufficient balance"}, nil); err.Kind != KindPayment {
		t.Fatalf("balance = %q, want %q", err.Kind, KindPayment)
	}
	// A 503 that says when it ends waits until then.
	outage := ClassifyHTTP(503, map[string]any{"error": map[string]any{"message": "inference is back in 5 min"}}, nil)
	if outage.Kind != KindServer || !outage.RetryAfterKnown || outage.RetryAfter != 5*time.Minute {
		t.Fatalf("outage = %+v", outage)
	}
}

// TestParseRetryAfterDocumentsBothHeaderForms: the seconds header is read
// first, then the millisecond one, and an HTTP date is read against now.
func TestParseRetryAfterDocumentsBothHeaderForms(t *testing.T) {
	headers := http.Header{}
	headers.Set("Retry-After", "3")
	if wait, ok := ParseRetryAfter(headers); !ok || wait != 3*time.Second {
		t.Fatalf("seconds = %v %v", wait, ok)
	}
	// retry-after wins over retry-after-ms.
	headers.Set("Retry-After-Ms", "1500")
	if wait, ok := ParseRetryAfter(headers); !ok || wait != 3*time.Second {
		t.Fatalf("both = %v %v", wait, ok)
	}
	milliseconds := http.Header{}
	milliseconds.Set("Retry-After-Ms", "1500")
	if wait, ok := ParseRetryAfter(milliseconds); !ok || wait != 1500*time.Millisecond {
		t.Fatalf("milliseconds = %v %v", wait, ok)
	}
	date := http.Header{}
	date.Set("Retry-After", time.Now().Add(90*time.Second).UTC().Format(http.TimeFormat))
	wait, ok := ParseRetryAfter(date)
	if !ok || wait < 80*time.Second || wait > 91*time.Second {
		t.Fatalf("date = %v %v", wait, ok)
	}
	if wait, ok := ParseRetryAfter(nil); ok {
		t.Fatalf("nil headers = %v %v", wait, ok)
	}
}

// TestFromNetworkClassifies maps transport failures onto the kinds routing
// reacts to.
func TestFromNetworkClassifies(t *testing.T) {
	refused := FromNetwork(fmt.Errorf("dial: %w", syscall.ECONNREFUSED))
	if refused.Kind != KindNetwork || !strings.Contains(refused.Message, "(ECONNREFUSED)") {
		t.Fatalf("refused = %+v", refused)
	}
	if timeout := FromNetwork(context.DeadlineExceeded); timeout.Kind != KindTimeout || timeout.Message != "The provider did not respond in time." {
		t.Fatalf("timeout = %+v", timeout)
	}
	if aborted := FromNetwork(context.Canceled); aborted.Kind != KindAborted || aborted.Message != "The request was aborted." {
		t.Fatalf("aborted = %+v", aborted)
	}
	classified := NewError(KindAuth, "nope", ErrorOptions{})
	if got := FromNetwork(classified); got != classified {
		t.Fatalf("a classified failure was reclassified: %+v", got)
	}
	if dns := FromNetwork(&net.DNSError{Err: "no such host", Name: "x.invalid"}); dns.Kind != KindNetwork {
		t.Fatalf("dns = %+v", dns)
	}
}

// TestFromLLMMapping keeps the library's errors in NeoRouter's kinds and texts.
func TestFromLLMMapping(t *testing.T) {
	library := &llm.Error{Status: 401, Message: `401 {"error":{"message":"Invalid API key"}}`}
	classified := FromLLM(library)
	if classified.Kind != KindAuth {
		t.Fatalf("kind = %q, want %q", classified.Kind, KindAuth)
	}
	// The library prefixes the status; the message is the provider's own words.
	if classified.Message != `{"error":{"message":"Invalid API key"}}` {
		t.Fatalf("message = %q", classified.Message)
	}
	retryable := &llm.Error{Status: 429, Message: "429 Too many requests", RetryAfter: 4 * time.Second}
	mapped := FromLLM(retryable)
	if mapped.Kind != KindRateLimit || !mapped.RetryAfterKnown || mapped.RetryAfter != 4*time.Second {
		t.Fatalf("rate limit = %+v", mapped)
	}
	if mapped := FromLLM(context.DeadlineExceeded); mapped.Kind != KindTimeout {
		t.Fatalf("deadline = %+v", mapped)
	}
	if mapped := FromLLM(&llm.Error{Message: "dial refused", Retryable: true}); mapped.Kind != KindNetwork {
		t.Fatalf("transport = %+v", mapped)
	}
	if mapped := FromLLM(&llm.Error{Message: "unusable answer"}); mapped.Kind != KindInvalidOutput {
		t.Fatalf("protocol = %+v", mapped)
	}
	// Round trip: the error keeps its status and wait on the way out.
	original := NewError(KindServer, "boom", ErrorOptions{Status: 503, RetryAfter: int64Ptr(2 * time.Second)})
	back := FromLLM(original.ToLLM("test"))
	if back.Kind != KindServer || back.Status != 503 || !back.RetryAfterKnown || back.RetryAfter != 2*time.Second {
		t.Fatalf("round trip = %+v", back)
	}
}

// int64Ptr is a pointer helper for the wait in an ErrorOptions.
func int64Ptr(wait time.Duration) *time.Duration { return &wait }

// TestKindList keeps the closed list the routing layer reads in step.
func TestKindList(t *testing.T) {
	want := []Kind{
		"auth", "rate_limit", "quota_exhausted", "payment", "context_length", "bad_request",
		"not_found", "server", "timeout", "network", "empty", "invalid_output", "truncated", "aborted",
	}
	if !reflect.DeepEqual(Kinds, want) {
		t.Fatalf("kinds = %v, want %v", Kinds, want)
	}
	for _, kind := range Kinds {
		if !kind.Valid() {
			t.Fatalf("%q is not valid", kind)
		}
	}
	if Kind("nonsense").Valid() {
		t.Fatal("unknown kind reported valid")
	}
}

// TestAsProviderError walks a wrapped failure.
func TestAsProviderError(t *testing.T) {
	original := NewError(KindNotFound, "no such model", ErrorOptions{Status: 404})
	wrapped := fmt.Errorf("providers: chat: %w", original)
	if got := AsProviderError(wrapped); got != original {
		t.Fatalf("AsProviderError = %+v", got)
	}
	if got := AsProviderError(errors.New("other")); got != nil {
		t.Fatalf("AsProviderError = %+v, want nil", got)
	}
	if got := KindOf(wrapped); got != KindNotFound {
		t.Fatalf("KindOf = %q", got)
	}
}

// TestClassifyKeepsHeadersForQuotaLearning: the quota tracker reads the
// rate-limit headers off the failure.
func TestClassifyKeepsHeadersForQuotaLearning(t *testing.T) {
	headers := http.Header{}
	headers.Set("x-ratelimit-remaining-requests", "0")
	classified := ClassifyHTTP(429, map[string]any{"error": map[string]any{"message": "rate limited"}}, headers)
	if classified.Headers.Get("x-ratelimit-remaining-requests") != "0" {
		t.Fatalf("headers = %v", classified.Headers)
	}
	if classified.Status != 429 {
		t.Fatalf("status = %d", classified.Status)
	}
}

// TestExtractMessageKeeps2000Units: the cut counts UTF-16 units, so an emoji
// still costs two.
func TestExtractMessageKeeps2000Units(t *testing.T) {
	long := strings.Repeat("a", 5000)
	if got := ExtractMessage(map[string]any{"error": map[string]any{"message": long}}); len(got) != 2000 {
		t.Fatalf("length = %d, want 2000", len(got))
	}
	// 1999 units of text plus one emoji is 2001 units: the emoji is cut.
	withEmoji := strings.Repeat("a", 1999) + "\U0001F600"
	got := ExtractMessage(map[string]any{"error": map[string]any{"message": withEmoji}})
	if len([]rune(got)) != 2000 || strings.HasSuffix(got, "\U0001F600") {
		t.Fatalf("emoji survived the cut: %q", got[len(got)-8:])
	}
	// A body with nothing readable inside is the message itself.
	if got := ExtractMessage(map[string]any{"errors": []any{map[string]any{"code": 400.0}}}); got != `{"errors":[{"code":400}]}` {
		t.Fatalf("opaque body = %q", got)
	}
}

// TestEmptyResponseError keeps the text a client sees.
func TestEmptyResponseError(t *testing.T) {
	err := EmptyResponseError()
	if err.Kind != KindEmpty || err.Message != "The provider returned an empty response." {
		t.Fatalf("empty = %+v", err)
	}
}

// TestHTTPRequestClassifiesAndDecodes exercises the layer against a real server.
func TestHTTPRequestClassifiesAndDecodes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/json":
			if r.Method != http.MethodPost {
				t.Errorf("method = %s", r.Method)
			}
			if got := r.Header.Get("Content-Type"); got != "application/json" {
				t.Errorf("content type = %q", got)
			}
			if got := r.Header.Get("Authorization"); got != "Bearer key" {
				t.Errorf("authorization = %q", got)
			}
			w.Header().Set("x-listed", "1")
			_, _ = w.Write([]byte(`{"data":[{"id":"m1"}]}`))
		case "/text":
			_, _ = w.Write([]byte("not json"))
		case "/empty":
			w.WriteHeader(http.StatusNoContent)
		case "/denied":
			w.Header().Set("retry-after", "2")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"message":"Rate limit reached for requests: Limit 500 requests per minute"}}`))
		case "/redirect":
			http.Redirect(w, r, "/echo", http.StatusTemporaryRedirect)
		case "/echo":
			_, _ = w.Write([]byte(`{"followed":true}`))
		case "/stream":
			w.Header().Set("Content-Type", "text/event-stream")
			flusher, _ := w.(http.Flusher)
			_, _ = w.Write([]byte(": keep-alive\n\nevent: delta\ndata: {\"a\":1}\ndata: {\"b\":2}\n\n"))
			flusher.Flush()
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewClient(Options{})
	ctx := context.Background()

	headers := http.Header{}
	headers.Set("Authorization", "Bearer key")
	response, err := client.Request(ctx, server.URL+"/json", Request{Method: http.MethodPost, Headers: headers, JSON: map[string]any{"model": "m1"}})
	if err != nil {
		t.Fatalf("json: %v", err)
	}
	if object(response.Data) == nil || len(list(member(response.Data, "data"))) != 1 {
		t.Fatalf("data = %#v", response.Data)
	}
	if response.Headers.Get("x-listed") != "1" || response.Status != http.StatusOK {
		t.Fatalf("response = %+v", response)
	}

	textual, err := client.Request(ctx, server.URL+"/text", Request{})
	if err != nil {
		t.Fatalf("text: %v", err)
	}
	if textual.Data != "not json" {
		t.Fatalf("text data = %#v", textual.Data)
	}

	empty, err := client.Request(ctx, server.URL+"/empty", Request{})
	if err != nil {
		t.Fatalf("empty: %v", err)
	}
	if empty.Data != nil {
		t.Fatalf("empty data = %#v", empty.Data)
	}

	denied, err := client.Request(ctx, server.URL+"/denied", Request{})
	if err == nil {
		t.Fatalf("denied: got %#v", denied)
	}
	classified := AsProviderError(err)
	if classified == nil || classified.Kind != KindRateLimit {
		t.Fatalf("denied = %+v", err)
	}
	if classified.Message != "Rate limit reached for requests: Limit 500 requests per minute" || !classified.RetryAfterKnown || classified.RetryAfter != 2*time.Second {
		t.Fatalf("denied message = %+v", classified)
	}
	if classified.Learned == nil || classified.Learned.Value != 500 {
		t.Fatalf("denied learned = %+v", classified.Learned)
	}

	redirected, err := client.Request(ctx, server.URL+"/redirect", Request{})
	if err != nil {
		t.Fatalf("redirect: %v", err)
	}
	if object(redirected.Data)["followed"] != true {
		t.Fatalf("redirect data = %#v", redirected.Data)
	}

	stream, err := client.Do(ctx, server.URL+"/stream", Request{})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	defer stream.Body.Close()
	var events []SSEEvent
	for event, err := range SSE(stream.Body) {
		if err != nil {
			t.Fatalf("sse: %v", err)
		}
		events = append(events, event)
	}
	want := []SSEEvent{
		{Event: "delta", Data: "{\"a\":1}\n{\"b\":2}"},
		{Data: "[DONE]"},
	}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %#v, want %#v", events, want)
	}
}

// TestHTTPTimeoutIsClassified: a call that runs past its timeout is a timeout,
// not a network error.
func TestHTTPTimeoutIsClassified(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()
	client := NewClient(Options{})
	_, err := client.Request(context.Background(), server.URL, Request{Timeout: 50 * time.Millisecond})
	classified := AsProviderError(err)
	if classified == nil || classified.Kind != KindTimeout {
		t.Fatalf("timeout = %+v", err)
	}
}

// TestHTTPPublicOnlyRefusesPrivateHosts keeps the admin-only rule and its text.
func TestHTTPPublicOnlyRefusesPrivateHosts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()
	// A stub resolver says every name is public, so only the address check can
	// decide; a server on 127.0.0.1 is private wherever the name points.
	client := NewClient(Options{Resolve: func(context.Context, string) ([]netip.Addr, error) { return nil, nil }})
	_, err := client.Request(context.Background(), server.URL, Request{PublicOnly: true})
	classified := AsProviderError(err)
	if classified == nil || classified.Kind != KindBadRequest {
		t.Fatalf("private destination = %+v", err)
	}
	if classified.Message != "This address is on a local or private network. Only the instance admin can use those." {
		t.Fatalf("message = %q", classified.Message)
	}
	// The admin's own account may reach it.
	if _, err := client.Request(context.Background(), server.URL, Request{}); err != nil {
		t.Fatalf("admin call: %v", err)
	}
}

// TestHTTPTransportHandlesItsScheme: a relay pool partner is reached through the
// transport registered for its scheme.
func TestHTTPTransportHandlesItsScheme(t *testing.T) {
	var seen string
	client := NewClient(Options{Transports: map[string]Transport{
		"relay:": func(ctx context.Context, rawURL string, req *http.Request, info TransportInfo) (*http.Response, error) {
			seen = rawURL + "|" + info.Owner
			return &http.Response{
				StatusCode: 200,
				Header:     http.Header{},
				Body:       io.NopCloser(strings.NewReader(`{"relayed":true}`)),
				Request:    req,
			}, nil
		},
	}})
	response, err := client.Request(context.Background(), "relay://partner/v1/models", Request{Owner: "user-1"})
	if err != nil {
		t.Fatalf("relay: %v", err)
	}
	if seen != "relay://partner/v1/models|user-1" {
		t.Fatalf("transport saw %q", seen)
	}
	if object(response.Data)["relayed"] != true {
		t.Fatalf("data = %#v", response.Data)
	}
}

// TestAssertNoEmbeddedError: a 200 that carries an error object becomes a
// failure, and a real completion does not.
func TestAssertNoEmbeddedError(t *testing.T) {
	err := AssertNoEmbeddedError(map[string]any{"error": map[string]any{"message": "model overloaded", "status": 503}})
	classified := AsProviderError(err)
	if classified == nil || classified.Kind != KindServer {
		t.Fatalf("embedded = %+v", err)
	}
	if err := AssertNoEmbeddedError(map[string]any{"choices": []any{}, "error": "ignored"}); err != nil {
		t.Fatalf("choices = %v", err)
	}
	if err := AssertNoEmbeddedError(map[string]any{"candidates": []any{map[string]any{}}}); err != nil {
		t.Fatalf("candidates = %v", err)
	}
	if err := AssertNoEmbeddedError(nil); err != nil {
		t.Fatalf("nil = %v", err)
	}
}
