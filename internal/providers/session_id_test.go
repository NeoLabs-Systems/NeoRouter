package providers

import (
	"encoding/json"
	"net/http"
	"reflect"
	"testing"
)

// TestDerivedSessionParity keeps the conversation id the hosts that route by it
// receive identical to the JS derivation.
func TestDerivedSessionParity(t *testing.T) {
	cases := loadFixture(t).DerivedSession
	if len(cases) == 0 {
		t.Fatal("no session fixtures")
	}
	for _, testCase := range cases {
		if got := DerivedSession(testCase.Canonical, testCase.Salt); got != testCase.Expect {
			t.Fatalf("%s (salt %q): got %q, want %q", testCase.Canonical, testCase.Salt, got, testCase.Expect)
		}
	}
}

// TestDerivedSessionIsStable: later turns of one conversation keep the id, and
// different conversations do not share it.
func TestDerivedSessionIsStable(t *testing.T) {
	first := json.RawMessage(`{"messages":[{"role":"system","content":"be brief"},{"role":"user","content":"hi"}]}`)
	later := json.RawMessage(`{"messages":[{"role":"system","content":"be brief"},{"role":"user","content":"hi"},{"role":"assistant","content":"hello"},{"role":"user","content":"more"}]}`)
	other := json.RawMessage(`{"messages":[{"role":"system","content":"be brief"},{"role":"user","content":"something else"}]}`)
	if DerivedSession(first, "") != DerivedSession(later, "") {
		t.Fatal("the same conversation got two ids")
	}
	if DerivedSession(first, "") == DerivedSession(other, "") {
		t.Fatal("two conversations share an id")
	}
	if DerivedSession(first, "account-1") == DerivedSession(first, "account-2") {
		t.Fatal("the salt did not separate two accounts")
	}
}

// TestClientSession passes the client's own id through, and nothing else.
func TestClientSession(t *testing.T) {
	for _, name := range []string{"x-opencode-session", "X-Session-Id", "x-session-affinity", "x-claude-code-session-id", "session_id", "session-id"} {
		request, _ := http.NewRequest(http.MethodPost, "http://x/", nil)
		request.Header.Set(name, "abc-123")
		if got := ClientSession(request); got != "abc-123" {
			t.Fatalf("%s: got %q", name, got)
		}
	}
	// The first header in the JS order wins.
	request, _ := http.NewRequest(http.MethodPost, "http://x/", nil)
	request.Header.Set("x-opencode-session", "first")
	request.Header.Set("x-session-id", "second")
	if got := ClientSession(request); got != "first" {
		t.Fatalf("order: got %q", got)
	}
	// Anything that is not a plain id is refused.
	for _, value := range []string{"", "with space", "semi;colon", "quote\"d", "a/b", string(make([]byte, 200))} {
		request, _ := http.NewRequest(http.MethodPost, "http://x/", nil)
		request.Header.Set("x-session-id", value)
		if got := ClientSession(request); got != "" {
			t.Fatalf("%q accepted: %q", value, got)
		}
	}
	if got := ClientSession(nil); got != "" {
		t.Fatalf("nil request = %q", got)
	}
}

// TestDerivedSessionIgnoresUnusableInput: a canonical body that is not the
// expected shape still derives a stable id instead of failing.
func TestDerivedSessionIgnoresUnusableInput(t *testing.T) {
	got := DerivedSession(json.RawMessage(`not json`), "")
	if got != DerivedSession(json.RawMessage(`{"messages":[]}`), "") {
		t.Fatal("an unusable body derived a different id")
	}
	if len(got) != len("nr-")+32 {
		t.Fatalf("id = %q", got)
	}
}

// TestSessionHelpersKeepTypes: the fixture round trip is only meaningful when
// the ids are plain hex strings, so guard the shape.
func TestSessionIDShape(t *testing.T) {
	id := DerivedSession(json.RawMessage(`{"messages":[{"role":"user","content":"x"}]}`), "")
	for _, r := range id {
		if !(r == '-' || (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || r == 'n' || r == 'r') {
			t.Fatalf("id %q has an unexpected character %q", id, r)
		}
	}
	if !reflect.DeepEqual(ClientSession(&http.Request{}), "") {
		t.Fatal("an empty request carries a session")
	}
}
