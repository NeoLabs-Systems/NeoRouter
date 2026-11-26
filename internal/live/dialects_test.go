package live

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/NeoLabs-Systems/neorouter/internal/providers"
)

// TestTokenOfPrecedence keeps the token sources in precedence order:
// Authorization, then x-api-key / x-goog-api-key, then ?key=, then the
// openai-insecure-api-key subprotocol.
func TestTokenOfPrecedence(t *testing.T) {
	tests := []struct {
		name    string
		target  string
		headers http.Header
		want    string
	}{
		{
			name:    "bearer",
			target:  "/v1/live/sessions",
			headers: http.Header{"Authorization": []string{"Bearer nrt_bearer"}},
			want:    "nrt_bearer",
		},
		{
			name:    "bearer is trimmed",
			target:  "/v1/live/sessions",
			headers: http.Header{"Authorization": []string{"Bearer   nrt_spaced  "}},
			want:    "nrt_spaced",
		},
		{
			name:    "basic auth is not a bearer",
			target:  "/v1/live/sessions",
			headers: http.Header{"Authorization": []string{"Basic nrt_basic"}},
			want:    "",
		},
		{
			name:    "x-api-key",
			target:  "/v1/live/sessions",
			headers: http.Header{"X-Api-Key": []string{" nrt_apikey "}},
			want:    "nrt_apikey",
		},
		{
			name:   "x-api-key wins over x-goog-api-key",
			target: "/v1/live/sessions",
			headers: http.Header{
				"X-Api-Key":      []string{"nrt_first"},
				"X-Goog-Api-Key": []string{"nrt_second"},
			},
			want: "nrt_first",
		},
		{
			name:    "x-goog-api-key",
			target:  "/v1/live/sessions",
			headers: http.Header{"X-Goog-Api-Key": []string{"nrt_goog"}},
			want:    "nrt_goog",
		},
		{
			name:   "a header wins over the query",
			target: "/v1/live/sessions?key=nrt_query",
			headers: http.Header{
				"X-Api-Key": []string{"nrt_header"},
			},
			want: "nrt_header",
		},
		{
			name:   "query key",
			target: "/v1/live/sessions?key=nrt_query",
			want:   "nrt_query",
		},
		{
			name:   "query key is decoded and not trimmed",
			target: "/v1/live/sessions?key=nrt%5Fquery",
			want:   "nrt_query",
		},
		{
			name:    "subprotocol",
			target:  "/v1/realtime",
			headers: http.Header{"Sec-Websocket-Protocol": []string{"realtime, openai-insecure-api-key.nrt_proto"}},
			want:    "nrt_proto",
		},
		{
			name:   "the query wins over the subprotocol",
			target: "/v1/realtime?key=nrt_query",
			headers: http.Header{
				"Sec-Websocket-Protocol": []string{"openai-insecure-api-key.nrt_proto"},
			},
			want: "nrt_query",
		},
		{
			name:   "nothing",
			target: "/v1/realtime",
			want:   "",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, test.target, nil)
			for name, values := range test.headers {
				for _, value := range values {
					request.Header.Add(name, value)
				}
			}
			if got := tokenOf(request); got != test.want {
				t.Fatalf("tokenOf = %q, want %q", got, test.want)
			}
		})
	}
}

// TestRejectBody keeps the raw refusal bodies.
func TestRejectBody(t *testing.T) {
	tests := []struct {
		status  int
		message string
		body    string
		reason  string
	}{
		{http.StatusUnauthorized, missingKeyText, `{"error":{"message":"A valid NeoRouter key (nrt_…) is required."}}`, "Unauthorized"},
		{http.StatusForbidden, "Pool partners can only use chat models.", `{"error":{"message":"Pool partners can only use chat models."}}`, "Bad Request"},
		{http.StatusTooManyRequests, "Slow down.", `{"error":{"message":"Slow down."}}`, "Too Many Requests"},
	}
	for _, test := range tests {
		recorder := httptest.NewRecorder()
		rejectSocket(recorder, test.status, test.message)
		if recorder.Code != test.status {
			t.Fatalf("status = %d, want %d", recorder.Code, test.status)
		}
		if got := recorder.Body.String(); got != test.body {
			t.Fatalf("body = %s, want %s", got, test.body)
		}
		if got := recorder.Header().Get("Content-Type"); got != "application/json" {
			t.Fatalf("content type = %q", got)
		}
	}
}

// TestErrorBodyEscaping pins the escaping: no HTML escaping.
func TestErrorBodyEscaping(t *testing.T) {
	if got := errorBody(`a < b & c`); got != `{"error":{"message":"a < b & c"}}` {
		t.Fatalf("errorBody = %s", got)
	}
}

// TestDialectPaths keeps the three protocol paths.
func TestDialectPaths(t *testing.T) {
	tests := map[string]string{
		"/v1/live/sessions": "openai_live",
		"/v1/realtime":      "openai_realtime",
		"/ws/google.ai.generativelanguage.v1alpha.GenerativeService.BidiGenerateContent": "gemini_live",
		"/ws/google.ai.generativelanguage.v1beta.GenerativeService.BidiGenerateContent":  "gemini_live",
		"/v1/chat/completions": "",
		"/v1/live/sessions/x":  "",
		"/ws/google.ai.generativelanguage.v1.GenerativeService.BidiGenerateContent": "",
	}
	for path, want := range tests {
		dialect := dialectFor(path)
		got := ""
		if dialect != nil {
			got = dialect.id
		}
		if got != want {
			t.Fatalf("dialectFor(%q) = %q, want %q", path, got, want)
		}
	}
}

// TestRewriteFirstKeepsKeyOrder keeps the opening message's keys in order with
// only the model changed.
func TestRewriteFirstKeepsKeyOrder(t *testing.T) {
	first, ok := parseObject([]byte(`{"type":"session.start","session":{"instructions":"hi","model":"auto","voice":"alloy"},"z":1}`))
	if !ok {
		t.Fatal("parse opening message")
	}
	rewritten, err := marshalObject(openAILiveDialect.rewriteFirst(first, "gpt-live-1"))
	if err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	want := `{"type":"session.start","session":{"instructions":"hi","model":"gpt-live-1","voice":"alloy"},"z":1}`
	if string(rewritten) != want {
		t.Fatalf("rewritten = %s, want %s", rewritten, want)
	}

	// A session object that names no model gets one, at the end.
	first, _ = parseObject([]byte(`{"type":"session.start","session":{"instructions":"hi"}}`))
	rewritten, err = marshalObject(openAILiveDialect.rewriteFirst(first, "gpt-live-1"))
	if err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	if got := string(rewritten); got != `{"type":"session.start","session":{"instructions":"hi","model":"gpt-live-1"}}` {
		t.Fatalf("rewritten = %s", got)
	}
}

// TestModelFromFirst keeps the model the opening messages name.
func TestModelFromFirst(t *testing.T) {
	tests := []struct {
		name    string
		dialect *dialect
		body    string
		want    string
		opens   bool
	}{
		{"openai session.start", openAILiveDialect, `{"type":"session.start","session":{"model":"auto"}}`, "auto", true},
		{"openai without a model", openAILiveDialect, `{"type":"session.start","session":{}}`, "auto", true},
		{"openai no session", openAILiveDialect, `{"type":"session.start"}`, "auto", true},
		{"openai other event", openAILiveDialect, `{"type":"session.input_audio.append"}`, "", false},
		{"gemini setup", geminiLiveDialect, `{"setup":{"model":"models/gemini-live-1"}}`, "gemini-live-1", true},
		{"gemini setup without a model", geminiLiveDialect, `{"setup":{}}`, "auto", true},
		{"gemini other message", geminiLiveDialect, `{"clientContent":{}}`, "", false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			first, ok := parseObject([]byte(test.body))
			if !ok {
				t.Fatalf("parse %s", test.body)
			}
			got, opens := test.dialect.modelFromFirst(first)
			if got != test.want || opens != test.opens {
				t.Fatalf("modelFromFirst = (%q, %v), want (%q, %v)", got, opens, test.want, test.opens)
			}
		})
	}
}

// TestModelFromURL keeps the realtime protocol's model parameter.
func TestModelFromURL(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/v1/realtime?model=gpt-live-1", nil)
	model, ok := openAIRealtimeDialect.modelFromURL(request.URL)
	if !ok || model != "gpt-live-1" {
		t.Fatalf("modelFromURL = (%q, %v)", model, ok)
	}
	// No parameter means "auto".
	request = httptest.NewRequest(http.MethodGet, "/v1/realtime", nil)
	if model, ok := openAIRealtimeDialect.modelFromURL(request.URL); !ok || model != "auto" {
		t.Fatalf("modelFromURL = (%q, %v), want auto", model, ok)
	}
}

// TestUsageOf keeps the token counts the three protocols report.
func TestUsageOf(t *testing.T) {
	tests := []struct {
		body   string
		input  float64
		output float64
		ok     bool
	}{
		{`{"usageMetadata":{"promptTokenCount":7,"responseTokenCount":3}}`, 7, 3, true},
		{`{"usageMetadata":{"promptTokenCount":7,"candidatesTokenCount":4}}`, 7, 4, true},
		{`{"usageMetadata":{"promptTokenCount":7}}`, 7, 0, true},
		{`{"response":{"usage":{"input_tokens":10,"output_tokens":5}}}`, 10, 5, true},
		{`{"usage":{"input_tokens":1,"output_tokens":2}}`, 1, 2, true},
		{`{"usage":{"input_tokens":0,"output_tokens":0}}`, 0, 0, true},
		{`{"usage":{"total_tokens":9}}`, 0, 0, false},
		{`{"type":"echo"}`, 0, 0, false},
		{`{"usageMetadata":{"promptTokenCount":0,"responseTokenCount":0}}`, 0, 0, true},
	}
	for _, test := range tests {
		event, ok := parseObject([]byte(test.body))
		if !ok {
			t.Fatalf("parse %s", test.body)
		}
		input, output, reported := usageOf(event)
		if input != test.input || output != test.output || reported != test.ok {
			t.Fatalf("usageOf(%s) = (%v, %v, %v), want (%v, %v, %v)",
				test.body, input, output, reported, test.input, test.output, test.ok)
		}
	}
}

// TestToWSAndOrigin keeps the address rewriting of the dialects.
func TestToWSAndOrigin(t *testing.T) {
	tests := map[string]string{
		"http://127.0.0.1:4600/v1":   "ws://127.0.0.1:4600/v1",
		"https://api.openai.com/v1":  "wss://api.openai.com/v1",
		"https://api.openai.com/v1/": "wss://api.openai.com/v1",
		"HTTPS://API.OPENAI.COM/v1":  "wss://API.OPENAI.COM/v1",
		"ws://x/v1":                  "ws://x/v1",
	}
	for base, want := range tests {
		if got := toWS(base); got != want {
			t.Fatalf("toWS(%q) = %q, want %q", base, got, want)
		}
	}
	if got := originOf("https://generativelanguage.googleapis.com/v1beta"); got != "https://generativelanguage.googleapis.com" {
		t.Fatalf("originOf = %q", got)
	}
	if got := originOf("http://127.0.0.1:4600/v1"); got != "http://127.0.0.1:4600" {
		t.Fatalf("originOf = %q", got)
	}
}

// TestEncodeURIComponent pins the percent-encoding.
func TestEncodeURIComponent(t *testing.T) {
	tests := map[string]string{
		"gpt-live-1":    "gpt-live-1",
		"a/b":           "a%2Fb",
		"a b":           "a%20b",
		"!*'()~._-":     "!*'()~._-",
		"key+with&more": "key%2Bwith%26more",
		"u\u00e9":       "u%C3%A9",
		"#":             "%23",
	}
	for value, want := range tests {
		if got := encodeURIComponent(value); got != want {
			t.Fatalf("encodeURIComponent(%q) = %q, want %q", value, got, want)
		}
	}
}

// TestConnectionHeaders pins the connection and auth headers.
func TestConnectionHeaders(t *testing.T) {
	connection := Connection{
		Definition: providers.Definition{
			BaseURL: "https://api.example.com/v1/",
			Auth:    providers.Auth{Type: providers.AuthBearer},
			Headers: map[string]string{"X-Definition": "one"},
			Quirks:  providers.Quirks{UserAgent: "NeoRouter/1"},
		},
		Account: providers.Account{
			BaseURL: "https://custom.example.com/v1/{accountId}/",
			Secret:  "sk-test",
			Options: map[string]any{"accountId": "a b", "headers": map[string]any{"X-Account": "two"}},
		},
	}
	if got := connection.BaseURL(); got != "https://custom.example.com/v1/a%20b" {
		t.Fatalf("base URL = %q", got)
	}
	headers := connection.Headers()
	if got := headers.Get("User-Agent"); got != "NeoRouter/1" {
		t.Fatalf("user agent = %q", got)
	}
	if got := headers.Get("X-Definition"); got != "one" {
		t.Fatalf("definition header = %q", got)
	}
	if got := headers.Get("X-Account"); got != "two" {
		t.Fatalf("account header = %q", got)
	}
	if got := headers.Get("Authorization"); got != "Bearer sk-test" {
		t.Fatalf("authorization = %q", got)
	}

	// A named auth header wins over the bearer.
	header := connection
	header.Account.Options = map[string]any{"authHeader": "x-api-key"}
	if got := header.Headers().Get("x-api-key"); got != "sk-test" {
		t.Fatalf("x-api-key = %q", got)
	}
	if got := header.Headers().Get("Authorization"); got != "" {
		t.Fatalf("authorization = %q, want none", got)
	}

	// A keyless provider sends no auth header at all.
	keyless := connection
	keyless.Account.Secret = ""
	keyless.Account.Options = nil
	if got := keyless.Headers().Get("Authorization"); got != "" {
		t.Fatalf("authorization = %q, want none", got)
	}
}

// TestSpeaks keeps definition.live: only accounts whose provider speaks the
// protocol are candidates.
func TestSpeaks(t *testing.T) {
	connection := Connection{Definition: providers.Definition{
		Live: []providers.LiveDialect{providers.LiveOpenAI, providers.LiveRealtime},
	}}
	if !connection.Speaks("openai_live") || !connection.Speaks("openai_realtime") {
		t.Fatal("the connection does not speak its definition's protocols")
	}
	if connection.Speaks("gemini_live") {
		t.Fatal("the connection speaks a protocol its definition does not list")
	}
}

// TestCloseReasonIsBounded keeps the 123-byte close reason limit.
func TestCloseReasonIsBounded(t *testing.T) {
	// 119 bytes then a three-byte rune: cutting at 120 splits the rune.
	long := strings.Repeat("a", 119) + "€"
	reason := closeReason(long)
	// The cut may add one replacement character, so the protocol's own 123-byte
	// bound is what matters.
	if len(reason) > 123 {
		t.Fatalf("close reason is %d bytes, want at most 123", len(reason))
	}
	if !strings.Contains(reason, "\uFFFD") {
		t.Fatalf("close reason = %q, want the replacement character of a cut rune", reason)
	}
	if got := closeReason("short"); got != "short" {
		t.Fatalf("close reason = %q", got)
	}
}
