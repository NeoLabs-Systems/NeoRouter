package adapters

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/NeoLabs-Systems/neorouter/internal/providers"
)

// Nous Portal, ported from providers/adapters/nous.js and
// test/api/nous_portal.test.js: a signed-in connection sends the inference JWT
// its tokens carry, a pasted key is sent as it is, and the Portal account
// endpoint is the balance.

// fakeNousSignIn stands in for services/providers/nous_oauth.go.
type fakeNousSignIn struct {
	signedIn  bool
	access    string
	refreshes int
	err       error
}

func (f *fakeNousSignIn) FreshNousCredentials(ctx context.Context, secret, accountID string, persist func(secret string) error) (providers.NousCredentials, string, error) {
	if f.err != nil {
		return providers.NousCredentials{}, "", f.err
	}
	f.refreshes++
	return providers.NousCredentials{Access: f.access}, secret, nil
}

// nousAdapter builds the dialect with the fake sign-in.
func nousAdapter(t *testing.T, baseURL, secret string, signIn *fakeNousSignIn) *nous {
	t.Helper()
	isSignedIn := func(string) bool { return signIn.signedIn }
	return newNous(chatDefinition(t, "nous"), chatDeps{baseURL: baseURL, secret: secret}.deps(), signIn, isSignedIn, nil, "")
}

func TestNousSignedInSendsTheRefreshedBearer(t *testing.T) {
	upstream := newChatProvider(t, func(w http.ResponseWriter, r *http.Request, call chatCall) {
		switch r.URL.Path {
		case "/v1/models":
			chatJSON(w, http.StatusOK, `{"data":[{"id":"hermes-5","context_length":131072}]}`)
		case "/v1/chat/completions":
			chatJSON(w, http.StatusOK, `{"id":"c1","object":"chat.completion","created":1,"model":"hermes-5","choices":[{"index":0,"message":{"role":"assistant","content":"Hello from Nous"},"finish_reason":"stop"}],"usage":{"prompt_tokens":7,"completion_tokens":3,"total_tokens":10}}`)
		default:
			chatJSON(w, http.StatusNotFound, `{"error":{"message":"no such path"}}`)
		}
	})
	signIn := &fakeNousSignIn{signedIn: true, access: "jwt-access"}
	adapter := nousAdapter(t, upstream.url()+"/v1", `{"access":"expired"}`, signIn)

	models, err := adapter.ListModels(context.Background())
	if err != nil {
		t.Fatalf("list models: %v", err)
	}
	if len(models) != 1 || models[0].ID != "hermes-5" {
		t.Fatalf("models = %v", models)
	}
	if got := upstream.last(t).headers.Get("Authorization"); got != "Bearer jwt-access" {
		t.Errorf("authorization = %q, want the refreshed JWT", got)
	}

	result, err := adapter.Chat(context.Background(), chatRequest(t, `{"messages":[{"role":"user","content":"Hi"}]}`), providers.Call{ModelID: "hermes-5"})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	if got := upstream.last(t).headers.Get("Authorization"); got != "Bearer jwt-access" {
		t.Errorf("authorization = %q", got)
	}
	message := chatPath(t, decodedChatResult(t, result.Body), "choices").([]any)[0].(map[string]any)["message"].(map[string]any)
	if message["content"] != "Hello from Nous" {
		t.Errorf("content = %v", message["content"])
	}
	if signIn.refreshes < 2 {
		t.Errorf("refreshes = %d, want the token prepared for each call", signIn.refreshes)
	}
}

func TestNousPastedKeyIsUsedAsItIs(t *testing.T) {
	upstream := newChatProvider(t, func(w http.ResponseWriter, r *http.Request, call chatCall) {
		if r.URL.Path == "/v1/models" {
			chatJSON(w, http.StatusOK, `{"data":[{"id":"hermes-5"}]}`)
			return
		}
		chatJSON(w, http.StatusNotFound, `{"error":{"message":"no such path"}}`)
	})
	signIn := &fakeNousSignIn{signedIn: false, access: "never-used"}
	adapter := nousAdapter(t, upstream.url()+"/v1", "SI_SECRET_KEY", signIn)

	// The bearer is prepared per call, as the JS adapter's prepare() does.
	if headers := adapter.authHeaders(); headers != nil {
		t.Errorf("before a call there is no bearer: %v", headers)
	}
	if _, err := adapter.Chat(context.Background(), chatRequest(t, `{"messages":[]}`), providers.Call{ModelID: "hermes-5"}); err != nil {
		// The fake has no chat path; the bearer is what this test reads.
		t.Logf("chat: %v", err)
	}
	if got := upstream.last(t).headers.Get("Authorization"); got != "Bearer SI_SECRET_KEY" {
		t.Errorf("authorization = %q", got)
	}
	if signIn.refreshes != 0 {
		t.Errorf("an API key must never be refreshed: %d", signIn.refreshes)
	}

	windows, err := adapter.FetchQuota(context.Background())
	if err != nil {
		t.Fatalf("quota: %v", err)
	}
	if windows != nil {
		t.Errorf("an API key has no Portal account to read credits from: %v", windows)
	}
}

func TestNousPortalCreditsBecomeABalance(t *testing.T) {
	var portalCalls int
	upstream := newChatProvider(t, func(w http.ResponseWriter, r *http.Request, call chatCall) {
		if r.URL.Path != "/api/oauth/account" {
			t.Errorf("path = %q", r.URL.Path)
		}
		portalCalls++
		if got := r.Header.Get("Authorization"); got != "Bearer jwt-access" {
			t.Errorf("authorization = %q", got)
		}
		chatJSON(w, http.StatusOK, `{"paid_service_access":{"allowed":true,"total_usable_credits":42.5}}`)
	})
	signIn := &fakeNousSignIn{signedIn: true, access: "jwt-access"}
	adapter := newNous(chatDefinition(t, "nous"), chatDeps{baseURL: "http://127.0.0.1:1/v1", secret: "tokens"}.deps(), signIn, func(string) bool { return true }, nil, upstream.url())

	windows, err := adapter.FetchQuota(context.Background())
	if err != nil {
		t.Fatalf("quota: %v", err)
	}
	if portalCalls != 1 {
		t.Errorf("portal calls = %d", portalCalls)
	}
	if len(windows) != 1 || windows[0].Metric != "balance" {
		t.Fatalf("windows = %+v", windows)
	}
	if windows[0].Limit != nil {
		t.Errorf("a balance has no limit: %+v", windows[0])
	}
	if windows[0].Remaining == nil || *windows[0].Remaining != 42.5 {
		t.Errorf("remaining = %v", windows[0].Remaining)
	}

	// A Portal answer without credits states nothing.
	subscriptionOnly := newChatProvider(t, func(w http.ResponseWriter, r *http.Request, call chatCall) {
		chatJSON(w, http.StatusOK, `{"subscription":{"credits_remaining":5}}`)
	})
	adapter = newNous(chatDefinition(t, "nous"), chatDeps{baseURL: "http://127.0.0.1:1/v1", secret: "tokens"}.deps(), signIn, func(string) bool { return true }, nil, subscriptionOnly.url())
	windows, err = adapter.FetchQuota(context.Background())
	if err != nil {
		t.Fatalf("quota: %v", err)
	}
	if len(windows) != 1 || windows[0].Remaining == nil || *windows[0].Remaining != 5 {
		t.Fatalf("windows = %+v", windows)
	}
}

func TestNousSignInFailureIsReported(t *testing.T) {
	signIn := &fakeNousSignIn{signedIn: true, err: errors.New("refresh failed")}
	adapter := nousAdapter(t, "http://127.0.0.1:1/v1", "tokens", signIn)
	_, err := adapter.Chat(context.Background(), chatRequest(t, `{"messages":[]}`), providers.Call{ModelID: "hermes-5"})
	if err == nil {
		t.Fatal("a sign-in that cannot be refreshed must fail the call")
	}
	if providers.KindOf(err) != providers.KindNetwork {
		t.Errorf("kind = %q: %v", providers.KindOf(err), err)
	}

	// A connection whose secret is empty has no bearer at all.
	empty := nousAdapter(t, "http://127.0.0.1:1/v1", "", &fakeNousSignIn{signedIn: true})
	_, err = empty.Chat(context.Background(), chatRequest(t, `{"messages":[]}`), providers.Call{ModelID: "hermes-5"})
	if providers.KindOf(err) != providers.KindAuth {
		t.Fatalf("kind = %q: %v", providers.KindOf(err), err)
	}
	if err.Error() != "This Nous Portal connection has no valid sign-in. Connect it again." {
		t.Errorf("message = %q", err.Error())
	}
}

func TestNousStreamCarriesTheBearer(t *testing.T) {
	upstream := newChatProvider(t, func(w http.ResponseWriter, r *http.Request, call chatCall) {
		chatSSE(w, []string{
			`{"id":"c1","object":"chat.completion.chunk","created":1,"model":"hermes-5","choices":[{"index":0,"delta":{"role":"assistant","content":"Hi "}}]}`,
			`{"id":"c1","object":"chat.completion.chunk","created":1,"model":"hermes-5","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`,
		}, true)
	})
	adapter := nousAdapter(t, upstream.url()+"/v1", "tokens", &fakeNousSignIn{signedIn: true, access: "jwt-access"})
	chunks := chatChunks(t, adapter.Stream(context.Background(), chatRequest(t, `{"messages":[]}`), providers.Call{ModelID: "hermes-5"}))
	if len(chunks) != 2 {
		t.Fatalf("chunks = %d", len(chunks))
	}
	if got := upstream.last(t).headers.Get("Authorization"); got != "Bearer jwt-access" {
		t.Errorf("authorization = %q", got)
	}
	if chatNumberAt(t, chunks[1]["usage"].(map[string]any)["total_tokens"]) != 5 {
		t.Errorf("usage = %v", chunks[1]["usage"])
	}
}
