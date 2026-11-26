package adapters

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/NeoLabs-Systems/neorouter/internal/providers"
)

// Shared fixtures for the media adapter tests: a definition from the real
// embedded data (so a quirk read here is the real one), and an account pointing
// at a fake upstream.

// mediaTestRegistry parses the embedded definitions, with no adapter factory:
// the tests build the adapters themselves.
func mediaTestRegistry(t *testing.T) *providers.Registry {
	t.Helper()
	registry, err := providers.Load(nil, nil)
	if err != nil {
		t.Fatalf("load definitions: %v", err)
	}
	return registry
}

// mediaTestDefinition is one definition of the embedded data.
func mediaTestDefinition(t *testing.T, id string) providers.Definition {
	t.Helper()
	def, ok := mediaTestRegistry(t).Get(id)
	if !ok {
		t.Fatalf("definition %q is missing", id)
	}
	return def
}

// mediaTestClient is the provider HTTP layer the adapters call through.
func mediaTestClient() *providers.Client {
	return providers.NewClient(providers.Options{Timeout: 10 * time.Second})
}

// mediaTestDeps is an account with a key pointing at baseURL.
func mediaTestDeps(secret, baseURL string, options map[string]any) providers.AdapterDeps {
	return providers.AdapterDeps{
		HTTP:    mediaTestClient(),
		Account: providers.Account{ID: "account-1", UserID: "user-1", Secret: secret, BaseURL: baseURL, Options: options},
	}
}

// mediaTestServer serves handler and returns its URL.
func mediaTestServer(t *testing.T, handler http.HandlerFunc) string {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server.URL
}

// mediaTestJSON answers with value as JSON.
func mediaTestJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Fatalf("write answer: %v", err)
	}
}

// mediaTestBody decodes a request body into a plain map.
func mediaTestBody(t *testing.T, r *http.Request) map[string]any {
	t.Helper()
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	decoded := map[string]any{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &decoded); err != nil {
			t.Fatalf("decode body %s: %v", raw, err)
		}
	}
	return decoded
}

// mediaTestErrorKind is the classified kind of a failure.
func mediaTestErrorKind(t *testing.T, err error) providers.Kind {
	t.Helper()
	if err == nil {
		t.Fatal("expected a failure")
	}
	kind := providers.KindOf(err)
	if kind == "" {
		t.Fatalf("failure is not classified: %v", err)
	}
	return kind
}

// mediaTestInt is a pointer to an int.
func mediaTestInt(value int) *int { return &value }

// mediaTestFloat is a pointer to a float64.
func mediaTestFloat(value float64) *float64 { return &value }

// mediaTestSame decodes value as plain JSON and compares it with want.
func mediaTestSame(t *testing.T, label string, want, got any) {
	t.Helper()
	wanted, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("encode %s: %v", label, err)
	}
	received, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("encode %s: %v", label, err)
	}
	var a, b any
	if err := json.Unmarshal(wanted, &a); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(received, &b); err != nil {
		t.Fatal(err)
	}
	if diff := mediaTestDiff(a, b); diff != "" {
		t.Fatalf("%s = %s, want %s", label, received, wanted)
	}
}

// mediaTestDiff is a compact "same JSON" check with the values spelled out.
func mediaTestDiff(want, got any) string {
	wanted, _ := json.Marshal(want)
	received, _ := json.Marshal(got)
	if string(wanted) == string(received) {
		return ""
	}
	return string(received)
}
