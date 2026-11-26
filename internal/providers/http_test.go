package providers

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strconv"
	"strings"
	"testing"

	"github.com/NeoLabs-Systems/neorouter/internal/config"
)

// fakeProxy is the SOCKS pool: it records what the HTTP layer sent it and
// answers with prepared responses.
type fakeProxy struct {
	requests []*http.Request
	from     []string
	replies  []*http.Response
}

func (p *fakeProxy) Enabled() bool { return true }

func (p *fakeProxy) Do(req *http.Request) (*http.Response, error) {
	p.requests = append(p.requests, req)
	p.from = append(p.from, req.Method+" "+req.URL.String())
	if len(p.replies) == 0 {
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{}`)), Request: req}, nil
	}
	reply := p.replies[0]
	p.replies = p.replies[1:]
	reply.Request = req
	return reply, nil
}

// TestProxyCarriesPublicCallsAndHandlesRedirects: with a pool configured,
// public destinations go through it, a key is dropped when the host changes,
// and a redirected POST becomes a GET.
func TestProxyCarriesPublicCallsAndHandlesRedirects(t *testing.T) {
	proxy := &fakeProxy{replies: []*http.Response{
		{
			StatusCode: http.StatusFound,
			Header:     http.Header{"Location": []string{"https://elsewhere.test/v1/models"}},
			Body:       io.NopCloser(strings.NewReader("moved")),
		},
		{
			StatusCode: http.StatusOK,
			Header:     http.Header{},
			Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
		},
	}}
	client := NewClient(Options{
		Proxy: proxy,
		Resolve: func(context.Context, string) ([]netip.Addr, error) {
			return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
		},
	})
	headers := http.Header{}
	headers.Set("Authorization", "Bearer secret")
	headers.Set("X-Api-Key", "secret")
	headers.Set("Cookie", "session=secret")
	response, err := client.Request(context.Background(), "https://example.test/v1/chat", Request{
		Method:  http.MethodPost,
		Headers: headers,
		JSON:    map[string]any{"model": "m1"},
	})
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if object(response.Data)["ok"] != true {
		t.Fatalf("data = %#v", response.Data)
	}
	if len(proxy.requests) != 2 {
		t.Fatalf("proxy saw %d requests, want 2", len(proxy.requests))
	}
	first := proxy.requests[0]
	if first.Method != http.MethodPost || first.Header.Get("Authorization") != "Bearer secret" {
		t.Fatalf("first hop = %s %v", first.Method, first.Header)
	}
	if first.Header.Get("X-Api-Key") != "secret" || first.Header.Get("Cookie") != "session=secret" {
		t.Fatalf("first hop headers = %v", first.Header)
	}
	if first.ContentLength != int64(len(`{"model":"m1"}`)) {
		t.Fatalf("first hop body length = %d", first.ContentLength)
	}
	second := proxy.requests[1]
	if second.Method != http.MethodGet {
		t.Fatalf("second hop method = %s", second.Method)
	}
	for _, name := range []string{"Authorization", "X-Api-Key", "Cookie"} {
		if value := second.Header.Get(name); value != "" {
			t.Fatalf("second hop kept %s = %q", name, value)
		}
	}
	if second.Body != nil && second.ContentLength > 0 {
		t.Fatalf("second hop kept a body")
	}
}

// TestTransportBeforeProxy: a registered scheme transport wins over the pool,
// because a relay partner is reached through the module that registered it.
func TestTransportBeforeProxy(t *testing.T) {
	proxy := &fakeProxy{}
	client := NewClient(Options{
		Proxy: proxy,
		Transports: map[string]Transport{
			"relay:": func(ctx context.Context, rawURL string, req *http.Request, info TransportInfo) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"relay":true}`)), Request: req}, nil
			},
		},
	})
	if _, err := client.Request(context.Background(), "relay://peer/v1/models", Request{}); err != nil {
		t.Fatalf("relay: %v", err)
	}
	if len(proxy.requests) != 0 {
		t.Fatalf("the pool saw a relayed call: %v", proxy.from)
	}
}

// TestTooManyRedirects: a redirect loop stops after five hops.
func TestTooManyRedirects(t *testing.T) {
	proxy := &redirectingProxy{}
	client := NewClient(Options{
		Proxy: proxy,
		Resolve: func(context.Context, string) ([]netip.Addr, error) {
			return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
		},
	})
	_, err := client.Request(context.Background(), "https://loop.test/v1", Request{})
	classified := AsProviderError(err)
	if classified == nil || classified.Kind != KindNetwork || classified.Message != "Too many redirects." {
		t.Fatalf("redirect loop = %+v", err)
	}
	if proxy.hops != maxRedirects+1 {
		t.Fatalf("hops = %d, want %d", proxy.hops, maxRedirects+1)
	}
}

// redirectingProxy answers every request with a redirect to itself.
type redirectingProxy struct {
	hops int
}

func (p *redirectingProxy) Enabled() bool { return true }

func (p *redirectingProxy) Do(req *http.Request) (*http.Response, error) {
	p.hops++
	return &http.Response{
		StatusCode: http.StatusFound,
		Header:     http.Header{"Location": []string{req.URL.String()}},
		Body:       io.NopCloser(strings.NewReader("")),
		Request:    req,
	}, nil
}

// newTestServer starts a test server and returns its URL.
func newTestServer(t *testing.T, handler http.HandlerFunc) string {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server.URL
}

// TestRawBodyIsSentUnchanged: multipart and audio uploads go out as they are.
func TestRawBodyIsSentUnchanged(t *testing.T) {
	var seen string
	url := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		seen = r.Header.Get("Content-Type") + "|" + string(body)
		_, _ = w.Write([]byte(`{}`))
	})
	client := NewClient(Options{})
	if _, err := client.Request(context.Background(), url, Request{
		Method:      http.MethodPost,
		Body:        strings.NewReader("--form-boundary--"),
		ContentType: "multipart/form-data; boundary=form",
	}); err != nil {
		t.Fatalf("raw body: %v", err)
	}
	if seen != "multipart/form-data; boundary=form|--form-boundary--" {
		t.Fatalf("server saw %q", seen)
	}
}

// TestRequestSetsJSONContentType: the layer adds the JSON content type unless
// the caller named one.
func TestRequestSetsJSONContentType(t *testing.T) {
	var seen string
	url := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get("Content-Type")
		_, _ = w.Write([]byte(`{}`))
	})
	client := NewClient(Options{})
	headers := http.Header{}
	headers.Set("Content-Type", "application/vnd.custom+json")
	if _, err := client.Request(context.Background(), url, Request{Method: http.MethodPost, Headers: headers, JSON: map[string]any{"a": 1}}); err != nil {
		t.Fatalf("json: %v", err)
	}
	if seen != "application/vnd.custom+json" {
		t.Fatalf("content type = %q", seen)
	}
}

// TestDefaultUserAgentIsNotTheGoBot: Cloudflare-fronted providers (Nous Portal)
// answer Go's default "Go-http-client/1.1" with a 403 challenge page, so every
// upstream call carries an honest product UA unless the caller set one.
func TestDefaultUserAgentIsNotTheGoBot(t *testing.T) {
	var seen string
	url := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get("User-Agent")
		_, _ = w.Write([]byte(`{}`))
	})
	client := NewClient(Options{})
	if _, err := client.Request(context.Background(), url, Request{}); err != nil {
		t.Fatalf("request: %v", err)
	}
	if seen != "NeoRouter/"+config.Version {
		t.Fatalf("default user agent = %q", seen)
	}

	// A caller's own UA (a definition quirk) wins.
	headers := http.Header{}
	headers.Set("User-Agent", "quirk/1.0")
	if _, err := client.Request(context.Background(), url, Request{Headers: headers}); err != nil {
		t.Fatalf("request: %v", err)
	}
	if seen != "quirk/1.0" {
		t.Fatalf("caller user agent = %q", seen)
	}
}

// TestSSEDataOnlyLines: a stream that names no event still dispatches its data,
// and comment lines are skipped.
func TestSSEDataOnlyLines(t *testing.T) {
	body := strings.NewReader(": ping\r\ndata: one\r\n\r\ndata: two\r\ndata: three\r\n\r\n")
	var events []SSEEvent
	for event, err := range SSE(body) {
		if err != nil {
			t.Fatalf("sse: %v", err)
		}
		events = append(events, event)
	}
	want := []SSEEvent{{Data: "one"}, {Data: "two\nthree"}}
	if len(events) != len(want) || events[0] != want[0] || events[1] != want[1] {
		t.Fatalf("events = %#v, want %#v", events, want)
	}
}

// TestRequestEncodesBadBody: a body that cannot be encoded is a caller error,
// not a provider failure.
func TestRequestEncodesBadBody(t *testing.T) {
	client := NewClient(Options{})
	_, err := client.Request(context.Background(), "https://example.test/v1", Request{
		Method: http.MethodPost,
		JSON:   map[string]any{"bad": func() {}},
	})
	classified := AsProviderError(err)
	if classified == nil || classified.Kind != KindBadRequest {
		t.Fatalf("encode error = %+v", err)
	}
}

// TestRequestBadURL: an unusable URL is a caller error too.
func TestRequestBadURL(t *testing.T) {
	client := NewClient(Options{})
	_, err := client.Request(context.Background(), "://bad", Request{})
	classified := AsProviderError(err)
	if classified == nil || classified.Kind != KindBadRequest {
		t.Fatalf("bad url = %+v", err)
	}
}

// TestStatusPassthrough: the decoded answer carries the status and headers.
func TestStatusPassthrough(t *testing.T) {
	url := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-request-id", strconv.Itoa(42))
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"created":true}`))
	})
	response, err := NewClient(Options{}).Request(context.Background(), url, Request{Method: http.MethodPost, JSON: map[string]any{}})
	if err != nil {
		t.Fatalf("created: %v", err)
	}
	if response.Status != http.StatusCreated || response.Headers.Get("x-request-id") != "42" {
		t.Fatalf("response = %+v", response)
	}
	if response.Object()["created"] != true {
		t.Fatalf("object = %#v", response.Object())
	}
}
