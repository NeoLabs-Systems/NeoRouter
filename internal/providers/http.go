package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/ssrf"
)

// Proxy is the SOCKS outbound pool (services/proxies in JS): a configured
// deployment sends public upstream calls through it. The proxies domain
// satisfies this interface; a nil Proxy means direct connections.
type Proxy interface {
	// Enabled reports whether the pool is configured.
	Enabled() bool
	// Do sends the request through the pool. Redirects are handled by the
	// HTTP layer, so the implementation must not follow them.
	Do(req *http.Request) (*http.Response, error)
}

// TransportInfo is what a scheme transport needs to know about the call.
type TransportInfo struct {
	// Owner is the person the call is made for (a relayed pool partner needs it).
	Owner string
}

// Transport handles one address scheme that is not reached over the internet,
// such as relay:// for a pool partner behind its host. The HTTP layer hands it
// a prepared request and reads the response like any other.
type Transport func(ctx context.Context, rawURL string, req *http.Request, info TransportInfo) (*http.Response, error)

// Options configure a Client. The zero value is a working direct client with a
// public-destination guard.
type Options struct {
	// Direct is the client for calls that may reach any destination (an account
	// the instance admin created). It follows redirects.
	Direct *http.Client
	// Public refuses local and private destinations on every hop. It defaults
	// to an ssrf client; its dial-time check is the real protection, and the
	// pre-flight check in send produces the error text the JS layer produced.
	Public *http.Client
	// Proxy is the SOCKS pool; nil or disabled means direct connections.
	Proxy Proxy
	// Transports are the handlers for extra address schemes, by scheme
	// including the colon ("relay:").
	Transports map[string]Transport
	// Resolve is the resolver the private-network check uses; nil means the
	// system resolver.
	Resolve Resolver
	// Timeout is the default for one call, used when the call names none.
	Timeout time.Duration
}

// Client is NeoRouter's provider HTTP layer (providers/http.js): one function
// per upstream call, non-2xx answers classified into ProviderErrors, network
// failures classified too, and SSE reading.
type Client struct {
	direct       *http.Client
	directManual *http.Client
	public       *http.Client
	proxy        Proxy
	transports   map[string]Transport
	resolve      Resolver
	timeout      time.Duration
}

// maxRedirects is how many hops a call may follow before it is refused.
const maxRedirects = 5

// NewClient builds a provider HTTP layer.
func NewClient(opts Options) *Client {
	client := &Client{
		direct:     opts.Direct,
		public:     opts.Public,
		proxy:      opts.Proxy,
		transports: opts.Transports,
		resolve:    opts.Resolve,
		timeout:    opts.Timeout,
	}
	if client.direct == nil {
		client.direct = &http.Client{}
	}
	manual := *client.direct
	manual.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	client.directManual = &manual
	if client.public == nil {
		// The client timeout is only a backstop: a streamed answer can run for a
		// while, and the caller's own timeout bounds every call.
		client.public = ssrf.NewClient(ssrf.Options{Timeout: time.Hour})
	}
	return client
}

// Request is one upstream call.
type Request struct {
	Method  string
	Headers http.Header
	// JSON is encoded as the request body with Content-Type application/json,
	// unless the headers already carry one.
	JSON any
	// Body is sent unchanged (multipart forms, audio uploads); set ContentType.
	Body        io.Reader
	ContentType string
	// Timeout bounds the call; zero uses the client's default.
	Timeout time.Duration
	// PublicOnly refuses local and private destinations, on every redirect hop:
	// set it for accounts the instance admin did not create.
	PublicOnly bool
	// Owner is the person the call is made for.
	Owner string
}

// Response is a decoded upstream answer.
type Response struct {
	Status  int
	Headers http.Header
	// Data is the decoded body: the JSON value when the body is JSON, the text
	// as a string when it is not, and nil when the body is empty.
	Data any
}

// Object is the decoded body as a JSON object, or nil when it is not one.
func (r *Response) Object() map[string]any { return object(r.Data) }

// Do performs one upstream call and returns the answer unread, for streams and
// binary bodies. A non-2xx answer is classified, as it is for Request.
func (c *Client) Do(ctx context.Context, rawURL string, req Request) (*http.Response, error) {
	prepared, cancel, err := c.prepare(ctx, rawURL, req)
	if err != nil {
		return nil, err
	}
	response, err := c.send(ctx, rawURL, prepared, req)
	if err != nil {
		cancel()
		return nil, FromNetwork(err)
	}
	response.Body = &cancelOnClose{ReadCloser: response.Body, cancel: cancel}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		payload, readErr := readBody(response)
		cancel()
		if readErr != nil {
			return nil, FromNetwork(readErr)
		}
		return nil, ClassifyHTTP(response.StatusCode, payload, response.Header)
	}
	return response, nil
}

// Request performs one upstream call and decodes the answer. Non-2xx answers
// become classified ProviderErrors, and so do network failures and timeouts.
func (c *Client) Request(ctx context.Context, rawURL string, req Request) (*Response, error) {
	response, err := c.Do(ctx, rawURL, req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	data, err := readBody(response)
	if err != nil {
		return nil, FromNetwork(err)
	}
	return &Response{Status: response.StatusCode, Headers: response.Header, Data: data}, nil
}

// prepare builds the HTTP request: method, headers, body, timeout.
func (c *Client) prepare(ctx context.Context, rawURL string, req Request) (*http.Request, context.CancelFunc, error) {
	method := req.Method
	if method == "" {
		method = http.MethodGet
	}
	var body []byte
	headers := make(http.Header, len(req.Headers)+1)
	for name, values := range req.Headers {
		headers[name] = append([]string(nil), values...)
	}
	switch {
	case req.JSON != nil:
		encoded, err := json.Marshal(req.JSON)
		if err != nil {
			return nil, nil, NewError(KindBadRequest, "The request could not be encoded: "+err.Error(), ErrorOptions{})
		}
		body = encoded
		if headers.Get("Content-Type") == "" {
			headers.Set("Content-Type", "application/json")
		}
	case req.Body != nil:
		// Buffered so a redirected call can be sent again, as fetch does with a
		// FormData or binary body.
		raw, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, nil, NewError(KindBadRequest, "The request body could not be read: "+err.Error(), ErrorOptions{})
		}
		body = raw
		if req.ContentType != "" && headers.Get("Content-Type") == "" {
			headers.Set("Content-Type", req.ContentType)
		}
	}
	timeout := req.Timeout
	if timeout == 0 {
		timeout = c.timeout
	}
	callCtx, cancel := ctx, context.CancelFunc(func() {})
	if timeout > 0 {
		callCtx, cancel = context.WithTimeout(ctx, timeout)
	}
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	prepared, err := http.NewRequestWithContext(callCtx, method, rawURL, reader)
	if err != nil {
		cancel()
		return nil, nil, NewError(KindBadRequest, "The provider URL is not valid: "+err.Error(), ErrorOptions{})
	}
	prepared.Header = headers
	if body != nil {
		prepared.GetBody = func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(body)), nil
		}
	}
	return prepared, cancel, nil
}

// send performs the request: through a registered scheme transport, directly,
// or through the proxy pool, re-checking every redirect hop.
func (c *Client) send(ctx context.Context, rawURL string, prepared *http.Request, req Request) (*http.Response, error) {
	if transport := c.transportFor(rawURL); transport != nil {
		return transport(ctx, rawURL, prepared, TransportInfo{Owner: req.Owner})
	}
	proxyEnabled := c.proxy != nil && c.proxy.Enabled()
	if !req.PublicOnly && !proxyEnabled {
		return c.direct.Do(prepared)
	}
	current := prepared
	for hop := 0; ; hop++ {
		private := IsPrivateDestination(ctx, current.URL.String(), c.resolve)
		if req.PublicOnly && private {
			return nil, NewError(KindBadRequest, "This address is on a local or private network. Only the instance admin can use those.", ErrorOptions{})
		}
		var response *http.Response
		var err error
		if private || !proxyEnabled {
			response, err = c.directManual.Do(current)
		} else {
			response, err = c.proxy.Do(current)
		}
		if err != nil {
			return nil, err
		}
		location := ""
		if response.StatusCode >= 300 && response.StatusCode < 400 {
			location = response.Header.Get("Location")
		}
		if location == "" {
			return response, nil
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<16))
		response.Body.Close()
		if hop >= maxRedirects {
			return nil, NewError(KindNetwork, "Too many redirects.", ErrorOptions{})
		}
		next, err := url.Parse(location)
		if err != nil {
			return nil, NewError(KindNetwork, "Too many redirects.", ErrorOptions{})
		}
		nextURL := current.URL.ResolveReference(next)
		redirected, err := buildRedirect(current, nextURL, response.StatusCode)
		if err != nil {
			return nil, err
		}
		current = redirected
	}
}

// buildRedirect prepares the request for the next hop: the key is dropped when
// the host changes, and a 303 (or a 301/302 answer to a POST) becomes a GET.
func buildRedirect(from *http.Request, to *url.URL, status int) (*http.Request, error) {
	method := from.Method
	var body io.ReadCloser
	if from.GetBody != nil {
		if reader, err := from.GetBody(); err == nil {
			body = reader
		}
	}
	if status == http.StatusSeeOther || ((status == http.StatusMovedPermanently || status == http.StatusFound) && from.Method == http.MethodPost) {
		method = http.MethodGet
		body = nil
	}
	next, err := http.NewRequestWithContext(from.Context(), method, to.String(), body)
	if err != nil {
		return nil, NewError(KindNetwork, "Too many redirects.", ErrorOptions{})
	}
	next.Header = from.Header.Clone()
	if from.URL.Host != to.Host || from.URL.Scheme != to.Scheme {
		for _, name := range []string{"Authorization", "X-Api-Key", "X-Goog-Api-Key", "Cookie"} {
			next.Header.Del(name)
		}
	}
	if body == nil {
		next.Header.Del("Content-Type")
		next.ContentLength = 0
	}
	return next, nil
}

// transportFor returns the handler registered for the URL's scheme.
func (c *Client) transportFor(rawURL string) Transport {
	if len(c.transports) == 0 {
		return nil
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil
	}
	return c.transports[parsed.Scheme+":"]
}

// readBody reads the whole answer and decodes it: JSON when it parses, else the
// text, and nil when empty.
func readBody(response *http.Response) (any, error) {
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return nil, nil
	}
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err == nil {
		return decoded, nil
	}
	return string(raw), nil
}

// cancelOnClose releases a call's timeout when its body is closed.
type cancelOnClose struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (c *cancelOnClose) Close() error {
	err := c.ReadCloser.Close()
	c.cancel()
	return err
}

// AssertNoEmbeddedError turns a 200 answer that carries an error object into a
// classified failure, so routing learns from it. Some providers report a
// refusal in the body of a 200, or in the first stream event.
func AssertNoEmbeddedError(payload any) error {
	obj := object(payload)
	if obj == nil {
		return nil
	}
	if !truthy(obj["error"]) {
		return nil
	}
	if truthy(obj["choices"]) || truthy(obj["candidates"]) {
		return nil
	}
	status := 502
	errObject := object(obj["error"])
	for _, candidate := range []any{errObject["code"], errObject["status"]} {
		if value, ok := numberOf(candidate); ok && value >= 400 {
			status = int(value)
			break
		}
	}
	return ClassifyHTTP(status, payload, nil)
}

// EmptyResponseError is the failure for a provider that answered with nothing.
func EmptyResponseError() *ProviderError {
	return NewError(KindEmpty, "The provider returned an empty response.", ErrorOptions{})
}
