package httpserver

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func doRequest(t *testing.T, h http.Handler, method, target string, body io.Reader, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, body)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestWriteErrorEnvelope(t *testing.T) {
	var logged bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logged, nil))

	cases := []struct {
		name       string
		err        error
		status     int
		wantBody   string
		wantRetry  string
		wantLogged bool
	}{
		{
			name:     "http error with details",
			err:      New(http.StatusForbidden, "MODEL_NOT_ALLOWED", "This key may not use that model.", map[string]any{"model": "gpt"}),
			status:   http.StatusForbidden,
			wantBody: `{"error":{"code":"MODEL_NOT_ALLOWED","message":"This key may not use that model.","details":{"model":"gpt"},"requestId":"req-1"}}`,
		},
		{
			name:     "http error without details keeps key order",
			err:      New(http.StatusNotFound, "NOT_FOUND", "Nope."),
			status:   http.StatusNotFound,
			wantBody: `{"error":{"code":"NOT_FOUND","message":"Nope.","requestId":"req-1"}}`,
		},
		{
			name:       "unknown error is masked and logged",
			err:        io.ErrUnexpectedEOF,
			status:     http.StatusInternalServerError,
			wantBody:   `{"error":{"code":"INTERNAL_ERROR","message":"An internal error occurred.","requestId":"req-1"}}`,
			wantLogged: true,
		},
		{
			name:       "server http error keeps its message",
			err:        New(http.StatusBadGateway, "UPSTREAM_FAILED", "The provider did not answer."),
			status:     http.StatusBadGateway,
			wantBody:   `{"error":{"code":"UPSTREAM_FAILED","message":"The provider did not answer.","requestId":"req-1"}}`,
			wantLogged: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			logged.Reset()
			h := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				WriteError(w, r, log, tc.err)
			}))
			rec := doRequest(t, h, http.MethodGet, "/api/v1/x", nil, map[string]string{"X-Request-Id": "req-1"})
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d", rec.Code, tc.status)
			}
			if got := strings.TrimSpace(rec.Body.String()); got != tc.wantBody {
				t.Fatalf("body =\n%s\nwant\n%s", got, tc.wantBody)
			}
			if ct := rec.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
				t.Fatalf("content type = %q", ct)
			}
			if got := rec.Header().Get("X-Request-Id"); got != "req-1" {
				t.Fatalf("echoed request id = %q", got)
			}
			if hasLog := logged.Len() > 0; hasLog != tc.wantLogged {
				t.Fatalf("logged = %v, want %v (%s)", hasLog, tc.wantLogged, logged.String())
			}
		})
	}
}

func TestWriteErrorRetryAfter(t *testing.T) {
	herr := New(http.StatusTooManyRequests, "RATE_LIMITED", "Too many requests. Please try again later.", map[string]int{"retryAfterSeconds": 3})
	herr.RetryAfter = 3
	rec := doRequest(t, RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		WriteError(w, r, nil, herr)
	})), http.MethodGet, "/api/v1/x", nil, nil)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := rec.Header().Get("Retry-After"); got != "3" {
		t.Fatalf("Retry-After = %q, want 3", got)
	}
	if !strings.Contains(rec.Body.String(), `"details":{"retryAfterSeconds":3}`) {
		t.Fatalf("body missing details: %s", rec.Body.String())
	}
}

func TestNotFoundMatchesNode(t *testing.T) {
	rec := httptest.NewRecorder()
	NotFound(rec)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d", rec.Code)
	}
	want := `{"error":{"code":"NOT_FOUND","message":"The requested resource was not found."}}`
	if got := strings.TrimSpace(rec.Body.String()); got != want {
		t.Fatalf("body = %s, want %s", got, want)
	}
}

func TestRequestIDGeneratesAndPassesThrough(t *testing.T) {
	var fromCtx string
	h := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fromCtx = RequestIDFrom(r.Context())
	}))
	rec := doRequest(t, h, http.MethodGet, "/health", nil, map[string]string{"X-Request-Id": "abc-123"})
	if got := rec.Header().Get("X-Request-Id"); got != "abc-123" {
		t.Fatalf("header = %q", got)
	}
	if fromCtx != "abc-123" {
		t.Fatalf("context = %q", fromCtx)
	}

	rec = doRequest(t, h, http.MethodGet, "/health", nil, nil)
	generated := rec.Header().Get("X-Request-Id")
	if len(generated) != 36 {
		t.Fatalf("generated request id = %q", generated)
	}
	if fromCtx != generated {
		t.Fatalf("context = %q, header = %q", fromCtx, generated)
	}
}

func TestCORS(t *testing.T) {
	env := func(values map[string]string) func(string) string {
		return func(k string) string { return values[k] }
	}
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })

	cases := []struct {
		name        string
		env         map[string]string
		origin      string
		method      string
		wantHeaders bool
		wantStatus  int
	}{
		{name: "localhost is allowed", origin: "http://localhost:5173", method: http.MethodGet, wantHeaders: true, wantStatus: http.StatusOK},
		{name: "loopback ipv4 is allowed", origin: "http://127.0.0.1", method: http.MethodGet, wantHeaders: true, wantStatus: http.StatusOK},
		{name: "loopback ipv6 is allowed", origin: "http://[::1]:8080", method: http.MethodGet, wantHeaders: true, wantStatus: http.StatusOK},
		{name: "configured origin is allowed", env: map[string]string{"NEOROUTER_ALLOWED_ORIGINS": "https://app.example.com, https://b.example.com"}, origin: "https://b.example.com", method: http.MethodGet, wantHeaders: true, wantStatus: http.StatusOK},
		{name: "wildcard allows anything", env: map[string]string{"NEOROUTER_ALLOWED_ORIGINS": "*"}, origin: "https://anywhere.example", method: http.MethodGet, wantHeaders: true, wantStatus: http.StatusOK},
		{name: "unknown origin has no headers", env: map[string]string{"NEOROUTER_ALLOWED_ORIGINS": "https://app.example.com"}, origin: "https://evil.example", method: http.MethodGet, wantStatus: http.StatusOK},
		{name: "no origin has no headers", method: http.MethodGet, wantStatus: http.StatusOK},
		{name: "preflight answers 204", origin: "http://localhost:5173", method: http.MethodOptions, wantHeaders: true, wantStatus: http.StatusNoContent},
		{name: "preflight from unknown origin still answers 204", origin: "https://evil.example", method: http.MethodOptions, wantStatus: http.StatusNoContent},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := CORS(env(tc.env), ok)
			headers := map[string]string{}
			if tc.origin != "" {
				headers["Origin"] = tc.origin
			}
			rec := doRequest(t, h, tc.method, "/api/v1/x", nil, headers)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tc.wantStatus)
			}
			if tc.wantHeaders {
				if got := rec.Header().Get("Access-Control-Allow-Origin"); got != tc.origin {
					t.Fatalf("allow origin = %q", got)
				}
				if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
					t.Fatalf("allow credentials = %q", got)
				}
				if got := rec.Header().Get("Access-Control-Allow-Methods"); got != "GET,POST,PUT,PATCH,DELETE,OPTIONS" {
					t.Fatalf("allow methods = %q", got)
				}
				if got := rec.Header().Get("Access-Control-Allow-Headers"); got != corsAllowHeaders {
					t.Fatalf("allow headers = %q", got)
				}
				if got := rec.Header().Get("Access-Control-Expose-Headers"); got != corsExposeHeaders {
					t.Fatalf("expose headers = %q", got)
				}
				if got := rec.Header().Values("Vary"); len(got) != 1 || got[0] != "Origin" {
					t.Fatalf("vary = %v", got)
				}
			} else if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
				t.Fatalf("unexpected allow origin %q", got)
			}
		})
	}
}

func TestSlidingWindowLimits(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	limiter := NewSlidingWindow(SlidingWindowOptions{
		Window: time.Minute,
		Limit:  2,
		Key:    func(*http.Request) string { return "k" },
		Now:    func() time.Time { return now },
	})
	h := limiter.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))

	for i := 0; i < 2; i++ {
		if rec := doRequest(t, h, http.MethodGet, "/api/v1/x", nil, nil); rec.Code != http.StatusOK {
			t.Fatalf("request %d: status = %d", i, rec.Code)
		}
	}
	rec := doRequest(t, h, http.MethodGet, "/api/v1/x", nil, nil)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", rec.Code)
	}
	if got := rec.Header().Get("Retry-After"); got != "60" {
		t.Fatalf("Retry-After = %q, want 60", got)
	}
	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
			Details struct {
				RetryAfterSeconds int `json:"retryAfterSeconds"`
			} `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Code != "RATE_LIMITED" || body.Error.Message != "Too many requests. Please try again later." {
		t.Fatalf("body = %s", rec.Body.String())
	}
	if body.Error.Details.RetryAfterSeconds != 60 {
		t.Fatalf("retryAfterSeconds = %d", body.Error.Details.RetryAfterSeconds)
	}

	limiter.Clear()
	if rec := doRequest(t, h, http.MethodGet, "/api/v1/x", nil, nil); rec.Code != http.StatusOK {
		t.Fatalf("after Clear: status = %d", rec.Code)
	}

	now = now.Add(61 * time.Second)
	if rec := doRequest(t, h, http.MethodGet, "/api/v1/x", nil, nil); rec.Code != http.StatusOK {
		t.Fatalf("after window: status = %d", rec.Code)
	}
}

func TestSlidingWindowDisabled(t *testing.T) {
	limiter := NewSlidingWindow(SlidingWindowOptions{Window: time.Minute, Limit: 0})
	h := limiter.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	rec := doRequest(t, h, http.MethodGet, "/api/v1/x", nil, nil)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"message":"This operation is disabled."`) {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestGzipMiddleware(t *testing.T) {
	big := strings.Repeat("a", 4096)
	h := Gzip(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/events":
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: "+big+"\n\n")
		case "/encoded":
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Content-Encoding", "br")
			_, _ = io.WriteString(w, big)
		case "/small":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"ok":true}`)
		case "/upgrade":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"padding":"`+big+`"}`)
		default:
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"padding":"`+big+`"}`)
		}
	}))

	rec := doRequest(t, h, http.MethodGet, "/json", nil, map[string]string{"Accept-Encoding": "gzip, deflate"})
	if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", got)
	}
	zr, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), big) {
		t.Fatalf("decompressed body is short: %d bytes", len(raw))
	}

	for _, path := range []string{"/events", "/encoded", "/small"} {
		rec := doRequest(t, h, http.MethodGet, path, nil, map[string]string{"Accept-Encoding": "gzip"})
		if got := rec.Header().Get("Content-Encoding"); got == "gzip" {
			t.Fatalf("%s was gzipped", path)
		}
	}
	rec = doRequest(t, h, http.MethodGet, "/json", nil, nil)
	if got := rec.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("compressed without Accept-Encoding: %q", got)
	}

	// A WebSocket upgrade must keep its own ResponseWriter.
	rec = doRequest(t, h, http.MethodGet, "/upgrade", nil, map[string]string{
		"Accept-Encoding": "gzip",
		"Connection":      "Upgrade",
		"Upgrade":         "websocket",
	})
	if got := rec.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("upgrade response was compressed: %q", got)
	}
}

func TestReadJSONGatewayShape(t *testing.T) {
	type payload struct {
		Model string `json:"model"`
	}
	cases := []struct {
		name       string
		body       string
		contentTyp string
		wantStatus int
		wantBody   string
		wantModel  string
	}{
		{
			name:       "valid body is decoded",
			body:       `{"model":"auto"}`,
			contentTyp: "application/json",
			wantStatus: http.StatusOK,
			wantModel:  "auto",
		},
		{
			name:       "malformed body",
			body:       `{"model": "auto", "messages": [`,
			contentTyp: "application/json",
			wantStatus: http.StatusBadRequest,
			wantBody:   `{"error":{"message":"The request body is not valid JSON.","type":"invalid_request_error","code":"INVALID_JSON"}}`,
		},
		{
			name:       "scalar body is rejected in strict mode",
			body:       `"hello"`,
			contentTyp: "application/json",
			wantStatus: http.StatusBadRequest,
			wantBody:   `{"error":{"message":"The request body is not valid JSON.","type":"invalid_request_error","code":"INVALID_JSON"}}`,
		},
		{
			name:       "oversized body",
			body:       `{"model":"` + strings.Repeat("x", 2048) + `"}`,
			contentTyp: "application/json",
			wantStatus: http.StatusRequestEntityTooLarge,
			wantBody:   `{"error":{"message":"The request body is too large.","type":"invalid_request_error","code":"BODY_TOO_LARGE"}}`,
		},
		{
			name:       "empty body is accepted",
			contentTyp: "application/json",
			wantStatus: http.StatusOK,
		},
		{
			name:       "non json content type is not parsed",
			body:       `not json at all`,
			contentTyp: "text/plain",
			wantStatus: http.StatusOK,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got payload
			h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := ReadJSON(w, r, &got, 1024); err != nil {
					return
				}
				w.WriteHeader(http.StatusOK)
			})
			headers := map[string]string{}
			if tc.contentTyp != "" {
				headers["Content-Type"] = tc.contentTyp
			}
			rec := doRequest(t, h, http.MethodPost, "/v1/chat/completions", strings.NewReader(tc.body), headers)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d (%s)", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if tc.wantBody != "" {
				if got := strings.TrimSpace(rec.Body.String()); got != tc.wantBody {
					t.Fatalf("body =\n%s\nwant\n%s", got, tc.wantBody)
				}
			}
			if got.Model != tc.wantModel {
				t.Fatalf("model = %q, want %q", got.Model, tc.wantModel)
			}
		})
	}
}

func TestReadJSONManagementShape(t *testing.T) {
	var dst map[string]any
	h := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := ReadJSONManagement(w, r, &dst, 128); err != nil {
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	rec := doRequest(t, h, http.MethodPost, "/api/v1/providers", strings.NewReader(`{bad`), map[string]string{
		"Content-Type": "application/json",
		"X-Request-Id": "mgmt-1",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"code":"INVALID_JSON"`) || !strings.Contains(rec.Body.String(), `"requestId":"mgmt-1"`) {
		t.Fatalf("body = %s", rec.Body.String())
	}

	rec = doRequest(t, h, http.MethodPost, "/api/v1/providers", strings.NewReader(`{"a":"`+strings.Repeat("x", 256)+`"}`), map[string]string{"Content-Type": "application/json"})
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"code":"BODY_TOO_LARGE"`) {
		t.Fatalf("body = %s", rec.Body.String())
	}
}
