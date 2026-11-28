package providers

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
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

// A public-only call that starts on a public host is refused when it is
// redirected to a private address, by IP or by a name that resolves inwards,
// and the private server is never reached.
func TestPublicOnlyRedirectsCannotReachPrivateAddresses(t *testing.T) {
	inner := false
	internal := newTestServer(t, func(http.ResponseWriter, *http.Request) { inner = true })
	_, port, _ := net.SplitHostPort(strings.TrimPrefix(internal, "http://"))
	for _, target := range []string{
		internal + "/latest/meta-data",
		"http://169.254.169.254/latest/meta-data",
		"http://metadata.test:" + port + "/latest/meta-data",
	} {
		reached := false
		start := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			reached = true
			http.Redirect(w, r, target, http.StatusFound)
		}))
		t.Cleanup(start.Close)
		// The public host is the start server, whatever the dial-time guard
		// would make of 127.0.0.1.
		public := &http.Client{Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, network, start.Listener.Addr().String())
			},
		}}
		client := NewClient(Options{
			Public: public,
			Resolve: func(_ context.Context, host string) ([]netip.Addr, error) {
				if host == "metadata.test" {
					return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
				}
				return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
			},
			Timeout: time.Minute,
		})
		_, err := client.Do(context.Background(), "http://api.public.test/v1/models", Request{PublicOnly: true})
		if !reached {
			t.Fatalf("%s: the public start was not reached: %v", target, err)
		}
		var refused *ProviderError
		if !errors.As(err, &refused) || refused.Message != privateRefusal().Message {
			t.Fatalf("%s: err = %v", target, err)
		}
	}
	if inner {
		t.Fatal("a private server was reached through a redirect")
	}
}
