package providers

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/netip"
	"testing"
	"time"
)

// A streamed answer has no total timeout, neither the call's, the layer's nor
// the HTTP client's.
func TestStreamsHaveNoTotalTimeout(t *testing.T) {
	url := newTestServer(t, func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "data: x\n\n") })
	client := NewClient(Options{Direct: &http.Client{Timeout: time.Nanosecond}, Timeout: time.Nanosecond})
	response, err := client.Do(context.Background(), url, Request{Streaming: true, Timeout: time.Nanosecond})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	defer response.Body.Close()
	if body, err := io.ReadAll(response.Body); err != nil || string(body) != "data: x\n\n" {
		t.Fatalf("body = %q, %v", body, err)
	}
	if _, err := client.Do(context.Background(), url, Request{}); KindOf(err) != KindTimeout {
		t.Fatalf("a plain call keeps its timeout: %v", err)
	}
}

// A public-only call is checked again at dial time, so a name that resolves
// inwards the second time reaches nothing; a failed lookup is refused too.
func TestPublicOnlyCallsCannotBeRebound(t *testing.T) {
	reached := false
	url := newTestServer(t, func(http.ResponseWriter, *http.Request) { reached = true })
	rebound := "http://localhost" + url[len("http://127.0.0.1"):]
	answers := []Resolver{
		func(context.Context, string) ([]netip.Addr, error) {
			return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
		},
		func(context.Context, string) ([]netip.Addr, error) { return nil, errors.New("no such host") },
	}
	for i, resolve := range answers {
		client := NewClient(Options{Resolve: resolve, Timeout: time.Minute})
		if _, err := client.Do(context.Background(), rebound, Request{PublicOnly: true}); err == nil {
			t.Fatalf("case %d: the call was made", i)
		}
	}
	if reached {
		t.Fatal("a local server was reached")
	}
}
