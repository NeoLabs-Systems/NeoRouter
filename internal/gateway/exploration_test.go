package gateway

import (
	"net/http"
	"testing"
)

// "auto" draws among the good candidates, so the router learns about more than
// its current favourite. Different draws have to reach different models.
func TestAutoRoutingDrawsAmongEqualModels(t *testing.T) {
	answeredBy := func(draw float64) string {
		f := newFixture(t, nil)
		f.draw = &draw
		for _, slug := range []string{"one", "two"} {
			id := f.account(slug, "free_tier")
			f.adapters[id].chat = mockChat(slug + "-model")
			f.model(slug, slug+"-model")
		}
		token, _ := f.key(nil)
		response := f.do(http.MethodPost, "/v1/chat/completions", token,
			map[string]any{"model": "auto", "messages": []map[string]any{{"role": "user", "content": "hello"}}}, nil)
		if response.Code != http.StatusOK {
			t.Fatalf("chat = %d %s", response.Code, response.Body.String())
		}
		for slug, adapter := range f.adapters {
			if adapter.callCount() > 0 {
				return slug
			}
		}
		t.Fatal("no model was called")
		return ""
	}
	if low, high := answeredBy(0.001), answeredBy(0.999); low == high {
		t.Fatalf("both draws reached %q: the router is not sampling", low)
	}
}
