package adapters

import (
	"context"
	"encoding/json"
	"iter"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/NeoLabs-Systems/neorouter/internal/providers"
)

// nous is providers/adapters/nous.js: Nous Portal over the device sign-in. The
// account secret holds the OAuth tokens as JSON, so the bearer is the inference
// JWT they carry, refreshed shortly before it expires; everything else is
// OpenAI-compatible. An account connected with a pasted API key keeps that key
// as its bearer.
type nous struct {
	openAICompatible
	signIn     NousSignIn
	isSignedIn func(secret string) bool
	persist    func(accountID, secret string) error
	portalURL  string
	mu         sync.Mutex
	// token is the bearer of the call being made: the OpenAI-compatible paths
	// read the headers synchronously, exactly as the JS adapter keeps a token
	// field for the same reason.
	token string
}

// nousPortalDefault is where a signed-in connection reads its credits.
const nousPortalDefault = "https://portal.nousresearch.com"

// newNous builds the Nous Portal dialect.
func newNous(def providers.Definition, deps providers.AdapterDeps, signIn NousSignIn, isSignedIn func(string) bool, persist func(accountID, secret string) error, portalURL string) *nous {
	adapter := &nous{
		openAICompatible: openAICompatible{baseAdapter: newBaseAdapter(def, deps)},
		signIn:           signIn,
		isSignedIn:       isSignedIn,
		persist:          persist,
	}
	adapter.self = adapter
	adapter.auth = adapter.authHeaders
	adapter.supported = providers.Of(providers.CapChat)
	adapter.portalURL = strings.TrimRight(portalURL, "/")
	if adapter.portalURL == "" {
		adapter.portalURL = nousPortalDefault
	}
	return adapter
}

// signedIn reports whether the connection holds Portal tokens rather than a
// pasted API key.
func (a *nous) signedIn() bool {
	return a.isSignedIn != nil && a.isSignedIn(a.secret)
}

// prepare keeps the bearer of the call being made current.
func (a *nous) prepare(ctx context.Context) error {
	token := a.secret
	if a.signedIn() {
		if a.signIn == nil {
			return providers.NewError(providers.KindAuth, "This Nous Portal connection has no valid sign-in. Connect it again.", providers.ErrorOptions{})
		}
		accountID := a.account.ID
		var persist func(string) error
		if a.persist != nil && accountID != "" {
			persist = func(secret string) error { return a.persist(accountID, secret) }
		}
		credentials, _, err := a.signIn.FreshNousCredentials(ctx, a.secret, accountID, persist)
		if err != nil {
			if classified := providers.AsProviderError(err); classified != nil {
				return classified
			}
			return providers.FromNetwork(err)
		}
		token = credentials.Access
	}
	if token == "" {
		return providers.NewError(providers.KindAuth, "This Nous Portal connection has no valid sign-in. Connect it again.", providers.ErrorOptions{})
	}
	a.mu.Lock()
	a.token = token
	a.mu.Unlock()
	return nil
}

// currentToken is the bearer of the call being made.
func (a *nous) currentToken() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.token
}

// authHeaders is the prepared bearer, or none before the first call.
func (a *nous) authHeaders() map[string]string {
	token := a.currentToken()
	if token == "" {
		return nil
	}
	return map[string]string{"Authorization": "Bearer " + token}
}

// Validate checks the key, with the sign-in refreshed first when there is one.
func (a *nous) Validate(ctx context.Context) (providers.Validation, error) {
	// A sign-in that cannot be refreshed is reported by the check itself.
	_ = a.prepare(ctx)
	return a.baseAdapter.Validate(ctx)
}

// ListModels reads the OpenAI-compatible listing with the refreshed bearer.
func (a *nous) ListModels(ctx context.Context) ([]providers.Model, error) {
	if err := a.prepare(ctx); err != nil {
		return nil, err
	}
	return a.openAICompatible.ListModels(ctx)
}

// Chat sends one canonical request with the refreshed bearer.
func (a *nous) Chat(ctx context.Context, req json.RawMessage, call providers.Call) (*providers.ChatResult, error) {
	if err := a.prepare(ctx); err != nil {
		return nil, err
	}
	return a.openAICompatible.Chat(ctx, req, call)
}

// Stream sends one canonical request with the refreshed bearer.
func (a *nous) Stream(ctx context.Context, req json.RawMessage, call providers.Call) iter.Seq2[json.RawMessage, error] {
	return func(yield func(json.RawMessage, error) bool) {
		if err := a.prepare(ctx); err != nil {
			yield(nil, err)
			return
		}
		for chunk, streamErr := range a.openAICompatible.Stream(ctx, req, call) {
			if !yield(chunk, streamErr) {
				return
			}
		}
	}
}

// FetchQuota reads the credits left from Portal's own account endpoint: a
// signed-in connection has them, an API key does not.
func (a *nous) FetchQuota(ctx context.Context) ([]providers.QuotaWindow, error) {
	if !a.signedIn() {
		return nil, nil
	}
	if err := a.prepare(ctx); err != nil {
		return nil, err
	}
	response, err := a.request(ctx, a.portalURL+"/api/oauth/account", providers.Request{
		Method: http.MethodGet,
		Headers: httpHeaders(map[string]string{
			"Authorization": "Bearer " + a.currentToken(),
			"Accept":        "application/json",
		}),
	}, validationTimeoutMS*time.Millisecond)
	if err != nil {
		return nil, err
	}
	data, _ := response.Data.(map[string]any)
	access, _ := data["paid_service_access"].(map[string]any)
	if value, ok := chatToNumber(access["total_usable_credits"]); ok {
		return []providers.QuotaWindow{{Metric: "balance", Remaining: &value}}, nil
	}
	subscription, _ := data["subscription"].(map[string]any)
	if value, ok := chatToNumber(subscription["credits_remaining"]); ok {
		return []providers.QuotaWindow{{Metric: "balance", Remaining: &value}}, nil
	}
	return nil, nil
}
