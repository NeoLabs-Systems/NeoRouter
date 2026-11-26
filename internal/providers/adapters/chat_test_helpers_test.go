package adapters

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/NeoLabs-Systems/neorouter/internal/providers"
)

// Adapter-level tests: the fake upstreams answer the shapes the providers use.

// registry loads the embedded definitions, the way the server does.
func chatRegistry(t *testing.T) *providers.Registry {
	t.Helper()
	loaded, err := providers.Load(nil, nil)
	if err != nil {
		t.Fatalf("load definitions: %v", err)
	}
	return loaded
}

// definition returns one provider definition.
func chatDefinition(t *testing.T, id string) providers.Definition {
	t.Helper()
	loaded, ok := chatRegistry(t).Get(id)
	if !ok {
		t.Fatalf("definition %q not found", id)
	}
	return loaded
}

// chatDeps is the wiring one adapter is built with in a test.
type chatDeps struct {
	baseURL string
	secret  string
	options map[string]any
	userID  string
	// publicOnly makes the account one of a non-admin person.
	publicOnly bool
}

// deps turns the test wiring into providers.AdapterDeps.
func (d chatDeps) deps() providers.AdapterDeps {
	return providers.AdapterDeps{
		HTTP: providers.NewClient(providers.Options{Timeout: 10 * time.Second}),
		Account: providers.Account{
			ID:         "acct-1",
			UserID:     d.userID,
			BaseURL:    d.baseURL,
			Secret:     d.secret,
			Options:    d.options,
			PublicOnly: d.publicOnly,
		},
		UpstreamTimeout: 10 * time.Second,
	}
}

// request is one call the fake upstream received.
type chatCall struct {
	method  string
	path    string
	query   string
	headers http.Header
	body    map[string]any
	raw     []byte
}

// chatProvider is an httptest server that records what it was asked and answers
// with the handler's response.
type chatProvider struct {
	server *httptest.Server
	mu     sync.Mutex
	calls  []chatCall
}

// newChatProvider starts a fake provider.
func newChatProvider(t *testing.T, handler func(w http.ResponseWriter, r *http.Request, call chatCall)) *chatProvider {
	t.Helper()
	upstream := &chatProvider{}
	upstream.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		call := chatCall{method: r.Method, path: r.URL.Path, query: r.URL.RawQuery, headers: r.Header.Clone(), raw: raw}
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &call.body)
		}
		upstream.mu.Lock()
		upstream.calls = append(upstream.calls, call)
		upstream.mu.Unlock()
		handler(w, r, call)
	}))
	t.Cleanup(upstream.server.Close)
	return upstream
}

// url is the fake provider's base URL.
func (u *chatProvider) url() string { return u.server.URL }

// last is the call the fake provider answered last.
func (u *chatProvider) last(t *testing.T) chatCall {
	t.Helper()
	u.mu.Lock()
	defer u.mu.Unlock()
	if len(u.calls) == 0 {
		t.Fatal("the provider was never called")
	}
	return u.calls[len(u.calls)-1]
}

// all is every call the fake provider answered.
func (u *chatProvider) all() []chatCall {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]chatCall(nil), u.calls...)
}

// chatJSON writes one JSON answer.
func chatJSON(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
}

// chatSSE writes a Server-Sent Events stream: one "data: <payload>" line per
// payload, then [DONE] when the caller asks for it.
func chatSSE(w http.ResponseWriter, payloads []string, done bool) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	for _, payload := range payloads {
		_, _ = io.WriteString(w, "data: "+payload+"\n\n")
	}
	if done {
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}
}

// chatChunks reads a canonical stream into its chunks.
func chatChunks(t *testing.T, stream func(yield func(json.RawMessage, error) bool)) []map[string]any {
	t.Helper()
	var chunks []map[string]any
	stream(func(chunk json.RawMessage, err error) bool {
		if err != nil {
			t.Fatalf("stream failed: %v", err)
		}
		var decoded map[string]any
		if decodeErr := json.Unmarshal(chunk, &decoded); decodeErr != nil {
			t.Fatalf("chunk is not JSON: %v (%s)", decodeErr, chunk)
		}
		chunks = append(chunks, decoded)
		return true
	})
	return chunks
}

// chatStreamFailure reads a canonical stream and returns the failure it ended with.
func chatStreamFailure(t *testing.T, stream func(yield func(json.RawMessage, error) bool)) error {
	t.Helper()
	var failure error
	stream(func(_ json.RawMessage, err error) bool {
		if err != nil {
			failure = err
			return false
		}
		return true
	})
	return failure
}

// chatRequest is a canonical chat request body.
func chatRequest(t *testing.T, body string) json.RawMessage {
	t.Helper()
	if !json.Valid([]byte(body)) {
		t.Fatalf("the test's request body is not JSON: %s", body)
	}
	return json.RawMessage(body)
}

// decodedChatResult decodes one canonical completion.
func decodedChatResult(t *testing.T, body json.RawMessage) map[string]any {
	t.Helper()
	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("completion is not JSON: %v (%s)", err, body)
	}
	return decoded
}

// number reads a decoded number, whatever Go type it arrived as.
func chatNumberAt(t *testing.T, value any) float64 {
	t.Helper()
	parsed, ok := value.(float64)
	if ok {
		return parsed
	}
	if whole, ok := value.(int64); ok {
		return float64(whole)
	}
	if whole, ok := value.(int); ok {
		return float64(whole)
	}
	t.Fatalf("%v is not a number", value)
	return 0
}

// chatPath reads a nested member of a decoded answer, for the assertions.
func chatPath(t *testing.T, value any, path ...string) any {
	t.Helper()
	current := value
	for _, name := range path {
		object, ok := current.(map[string]any)
		if !ok {
			t.Fatalf("%v is not an object (looking for %s)", current, strings.Join(path, "."))
		}
		current = object[name]
	}
	return current
}
