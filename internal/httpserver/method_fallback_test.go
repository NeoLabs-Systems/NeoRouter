package httpserver_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
)

func TestMethodFallbackRewrites405ToTheJSON404(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/thing", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := httpserver.MethodFallback(mux)

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/thing", nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", response.Code)
	}
	if allow := response.Header().Get("Allow"); allow != "" {
		t.Fatalf("Allow header survived: %q", allow)
	}
	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v (%s)", err, response.Body.String())
	}
	if body.Error.Code != "NOT_FOUND" || body.Error.Message != "The requested resource was not found." {
		t.Fatalf("envelope = %+v", body.Error)
	}

	// The matching method is untouched.
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/thing", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want 200", response.Code)
	}
}

func TestMethodFallbackKeepsReal405BodiesOut(t *testing.T) {
	// A handler that writes its own 405 body (none exist in NeoRouter, but the
	// wrapper must not double-write): the envelope replaces it cleanly.
	handler := httpserver.MethodFallback(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusMethodNotAllowed)
		_, _ = w.Write([]byte("method not allowed"))
	}))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/x", nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", response.Code)
	}
	if got := response.Body.String(); got == "method not allowed" || len(got) == 0 {
		t.Fatalf("body = %q", got)
	}
}
