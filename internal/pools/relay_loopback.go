package pools

// A relayed request is answered by this NeoRouter itself, exactly as if it had
// come in over the network: through the same handler, router-key check, share
// limits and privacy rules (relay/loopback.js). The JS server handed it over on
// a private listener on 127.0.0.1 (opened on first use) that served the same app;
// here the same handler is called in process, with the request built the way the
// socket would have carried it and the answer streamed back. Which requests may
// be handed over at all is decided by the endpoint (relay_endpoint.go), not here.

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
)

// relayResponse is one answered relayed request.
type relayResponse struct {
	Status int
	Header http.Header
	Body   io.ReadCloser
}

// errNoLoopback is what a relayed request fails with when no app is wired in.
var errNoLoopback = errors.New("The relay has no app to answer with.")

// loopbackStart is what the handler answered before any body arrived.
type loopbackStart struct {
	status int
	header http.Header
}

// loopbackDispatch sends one request to this NeoRouter's own handler and waits for
// the answer to start. cancelled, when it closes, is the partner giving up: the
// handler is cancelled then. An answer that simply ends never cuts the handler
// short - it keeps its post-response work, as the JS handler did.
func (s *Service) loopbackDispatch(ctx context.Context, req relayRequest, cancelled <-chan struct{}) (*relayResponse, error) {
	handler := s.loopbackHandler()
	if handler == nil {
		return nil, errNoLoopback
	}
	target := &url.URL{Scheme: "http", Host: "127.0.0.1", Path: "/"}
	if parsed, err := url.ParseRequestURI(req.Path); err == nil {
		target.Path = parsed.Path
		target.RawQuery = parsed.RawQuery
	} else {
		target.Path = req.Path
	}
	headers := http.Header{}
	for name, values := range req.Headers {
		headers[name] = append([]string(nil), values...)
	}
	outgoing := &http.Request{
		Method:     req.Method,
		URL:        target,
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
		Header:     headers,
		Host:       "127.0.0.1",
		RemoteAddr: "127.0.0.1:0",
		RequestURI: target.RequestURI(),
	}
	if req.Body != nil {
		outgoing.Body = io.NopCloser(req.Body)
	} else {
		outgoing.Body = http.NoBody
	}
	// The handler's own lifetime: it outlives the answer, so its work is never cut
	// short by the partner having what it needed.
	if ctx != nil {
		outgoing = outgoing.WithContext(context.WithoutCancel(ctx))
	}
	handlerCtx, cancelHandler := context.WithCancel(outgoing.Context())
	outgoing = outgoing.WithContext(handlerCtx)

	reader, writer := io.Pipe()
	recorder := &loopbackWriter{
		header:  http.Header{},
		writer:  writer,
		started: make(chan loopbackStart, 1),
		done:    make(chan struct{}),
	}
	go func() {
		defer cancelHandler()
		defer func() {
			if recovered := recover(); recovered != nil {
				_ = writer.CloseWithError(errors.New("the handler panicked"))
				recorder.finish()
				return
			}
			writer.Close()
			recorder.finish()
		}()
		handler.ServeHTTP(recorder, outgoing)
	}()
	if cancelled != nil {
		// The partner gave up: the answer's own pipe fails, which is what stops the
		// handler at its next write. The handler itself is never cut short, so its
		// bookkeeping after the answer still finishes (the JS handler's did too).
		go func() {
			select {
			case <-cancelled:
				_ = writer.CloseWithError(errors.New("the partner cancelled the relayed request"))
			case <-recorder.done:
			}
		}()
	}

	select {
	case start := <-recorder.started:
		return &relayResponse{Status: start.status, Header: start.header, Body: reader}, nil
	case <-recorder.done:
		// The handler answered without writing anything: the socket would have
		// sent 200 with the handler's headers.
		leftover := recorder.snapshot()
		if leftover == nil {
			return &relayResponse{Status: http.StatusOK, Header: http.Header{}, Body: reader}, nil
		}
		return &relayResponse{Status: leftover.status, Header: leftover.header, Body: reader}, nil
	case <-ctx.Done():
		_ = writer.CloseWithError(ctx.Err())
		return nil, ctx.Err()
	}
}

// loopbackWriter is the http.ResponseWriter a relayed request is answered with:
// the header is collected until the handler starts the answer, the body streams
// through a pipe so the partner gets it as it is produced.
type loopbackWriter struct {
	mu      sync.Mutex
	header  http.Header
	status  int
	writer  *io.PipeWriter
	started chan loopbackStart
	done    chan struct{}
	began   bool
}

// Header is the response header being built.
func (w *loopbackWriter) Header() http.Header { return w.header }

// WriteHeader starts the answer.
func (w *loopbackWriter) WriteHeader(status int) {
	w.mu.Lock()
	if w.began {
		w.mu.Unlock()
		return
	}
	w.began = true
	w.status = status
	w.mu.Unlock()
	select {
	case w.started <- loopbackStart{status: status, header: w.header.Clone()}:
	default:
	}
}

// Write streams one chunk of the body.
func (w *loopbackWriter) Write(chunk []byte) (int, error) {
	w.mu.Lock()
	began := w.began
	w.mu.Unlock()
	if !began {
		w.WriteHeader(http.StatusOK)
	}
	return w.writer.Write(chunk)
}

// Flush keeps the streaming interfaces (and the compression middleware) working.
func (w *loopbackWriter) Flush() {}

// finish marks the handler as returned.
func (w *loopbackWriter) finish() {
	w.mu.Lock()
	defer w.mu.Unlock()
	select {
	case <-w.done:
	default:
		close(w.done)
	}
}

// snapshot is what the handler wrote, nil when it wrote nothing at all.
func (w *loopbackWriter) snapshot() *loopbackStart {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.began {
		return nil
	}
	return &loopbackStart{status: w.status, header: w.header.Clone()}
}

// headerValueLimit keeps one header value to the size the JS code allowed.
const headerValueLimit = 8192

// pickHeaders keeps the headers a partner needs and drops the rest.
func pickHeaders(headers http.Header, keep func(name string) bool) map[string]string {
	picked := map[string]string{}
	for name, values := range headers {
		lower := strings.ToLower(name)
		if !keep(lower) {
			continue
		}
		value := strings.Join(values, ", ")
		if len(value) > headerValueLimit {
			value = value[:headerValueLimit]
		}
		picked[lower] = value
	}
	return picked
}

// headerOf turns picked headers into an http.Header.
func headerOf(picked map[string]string) http.Header {
	header := http.Header{}
	for name, value := range picked {
		header[name] = []string{value}
	}
	return header
}
