package live

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// mockUpstream is the fake live provider of test/support/mock_provider.js: it
// echoes events, reports usage and refuses (or hangs up on) models starting with
// fail-.
type mockUpstream struct {
	server *httptest.Server

	mu        sync.Mutex
	paths     []string
	models    []string
	auth      []string
	betaSeen  []string
	started   []string
	startBody []string
}

func newMockUpstream(t *testing.T) *mockUpstream {
	t.Helper()
	mock := &mockUpstream{}
	mock.server = httptest.NewServer(http.HandlerFunc(mock.handle))
	t.Cleanup(mock.server.Close)
	return mock
}

func (m *mockUpstream) URL() string { return mockURLFrom(m.server.URL) }

func mockURLFrom(serverURL string) string { return serverURL + "/v1" }

// note records one socket the provider saw.
func (m *mockUpstream) note(r *http.Request) string {
	model := r.URL.Query().Get("model")
	m.mu.Lock()
	defer m.mu.Unlock()
	m.paths = append(m.paths, r.URL.Path)
	m.models = append(m.models, model)
	m.auth = append(m.auth, r.Header.Get("Authorization"))
	m.betaSeen = append(m.betaSeen, r.Header.Get("OpenAI-Beta"))
	return model
}

func (m *mockUpstream) noteStarted(model, body string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.started = append(m.started, model)
	m.startBody = append(m.startBody, body)
}

func (m *mockUpstream) betas() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string{}, m.betaSeen...)
}

// gatewayURL is the provider's origin, which the Gemini dialect uses as is.
func (m *mockUpstream) gatewayURL() string { return m.server.URL }

func (m *mockUpstream) startedModels() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string{}, m.started...)
}

func (m *mockUpstream) handle(w http.ResponseWriter, r *http.Request) {
	model := m.note(r)
	refuse := func(status int, message string) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"error":{"message":"` + message + `"}}`))
	}
	gemini := strings.Contains(r.URL.Path, "BidiGenerateContent")
	if gemini {
		if r.URL.Query().Get("key") != mockSecret {
			refuse(http.StatusUnauthorized, "Invalid API key")
			return
		}
	} else if r.Header.Get("Authorization") != "Bearer "+mockSecret {
		refuse(http.StatusUnauthorized, "Invalid API key")
		return
	}
	// A model the provider will not serve at all.
	if strings.HasPrefix(model, "fail-") {
		refuse(http.StatusInternalServerError, "live refused")
		return
	}
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	defer conn.CloseNow()
	ctx := context.Background()
	send := func(payload string) bool {
		writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		return conn.Write(writeCtx, websocket.MessageText, []byte(payload)) == nil
	}
	if r.URL.Path == "/v1/realtime" {
		if !send(`{"type":"session.created","session":{"model":"` + model + `"}}`) {
			return
		}
	}
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		event, ok := parseObject(data)
		if !ok {
			continue
		}
		if event.String("type") == "session.start" {
			started := event.Object("session").String("model")
			body, _ := marshalObject(event)
			m.noteStarted(started, string(body))
			// A live model that fails once the session starts.
			if strings.HasPrefix(started, "fail-") {
				_ = conn.Close(websocket.StatusInternalError, "model failed")
				return
			}
			if !send(`{"type":"session.started","session":{"model":"` + started + `"}}`) {
				return
			}
			continue
		}
		echo, err := marshalObject(echoEvent(event))
		if err != nil {
			return
		}
		if !send(string(echo)) {
			return
		}
		switch event.String("type") {
		case "session.input_audio.append":
			if !send(`{"type":"response.done","response":{"usage":{"input_tokens":10,"output_tokens":5}}}`) {
				return
			}
		case "usage.binary":
			// Gemini-style usage, sent as a binary frame like Gemini does.
			writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			binary := conn.Write(writeCtx, websocket.MessageBinary,
				[]byte(`{"usageMetadata":{"promptTokenCount":7,"responseTokenCount":3}}`))
			cancel()
			if binary != nil {
				return
			}
		}
	}
}

// echoEvent is {type: 'echo', event}.
func echoEvent(event *object) *object {
	out := newObject()
	out.Set("type", "echo")
	out.Set("event", event)
	return out
}
