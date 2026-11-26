// Package adapters holds the concrete provider dialects of internal/providers:
// one adapter per entry of definitions.json, built through Factory. The shared
// behaviour every dialect gets (URL templating, headers, the key check, the
// model listing default).
//
// The chat dialects live in the chat files (base_adapter.go,
// openai_compatible.go, anthropic.go, google_gemini.go, codex.go, nous.go,
// cloudflare.go, local_runtime.go) and their helpers are prefixed chat. The
// media dialects (media_openai.go, media_deepgram.go, media_elevenlabs.go,
// media_riva.go, media_wav.go, search_engines.go, weather.go) attach their
// methods to the same *openAICompatible and *baseAdapter types from this
// package.
package adapters

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/NeoLabs-Systems/neorouter/internal/providers"
)

// validationTimeoutMS is the timeout of the cheap authenticated GET a key check
// makes.
const validationTimeoutMS = 20_000

// listTimeoutMS is the timeout of a model listing.
const listTimeoutMS = 30_000

// baseAdapter is one adapter bound to one definition and account. A dialect
// embeds it and adds the calls it can perform.
type baseAdapter struct {
	def     providers.Definition
	account providers.Account
	secret  string
	options map[string]any
	// baseURL is the account's base URL with the account's fields templated in
	// and trailing slashes removed.
	baseURL string
	client  *providers.Client
	deps    providers.AdapterDeps
	// supported is the capability set Supports answers from.
	supported providers.Capabilities
	// self is the adapter value built around this base, so Supports can answer:
	// a method this account can perform is one the value implements.
	self providers.Adapter
	// auth is the dialect's own key header, for the dialects that do not use the
	// definition's default. It is a field rather than an override because Go
	// method promotion would otherwise call the base implementation.
	auth func() map[string]string
}

// newBaseAdapter builds the shared part of one adapter. A nil HTTP client in
// deps falls back to a direct one, so a test can build an adapter without
// wiring the whole HTTP layer.
func newBaseAdapter(def providers.Definition, deps providers.AdapterDeps) baseAdapter {
	account := deps.Account
	base := baseAdapter{
		def:       def,
		account:   account,
		secret:    account.Secret,
		options:   account.Options,
		client:    deps.HTTP,
		deps:      deps,
		supported: providers.Of(providers.CapChat),
	}
	if base.client == nil {
		base.client = providers.NewClient(providers.Options{})
	}
	raw := account.BaseURL
	if raw == "" {
		raw = def.BaseURL
	}
	base.baseURL = strings.TrimRight(templateFields(raw, account.Options), "/")
	return base
}

// Definition is the provider this adapter was built from.
func (a *baseAdapter) Definition() providers.Definition { return a.def }

// Supports reports whether this account can serve method. Chat is always
// served; anything else needs the definition to offer it and the adapter to
// implement it.
func (a *baseAdapter) Supports(method providers.Capability) bool {
	if method == providers.CapChat {
		return true
	}
	if !a.supported.Supports(method) || a.self == nil {
		return false
	}
	switch method {
	case providers.CapEmbed:
		_, ok := a.self.(providers.Embedder)
		return ok
	case providers.CapGenerateImage:
		_, ok := a.self.(providers.ImageGenerator)
		return ok
	case providers.CapSpeech:
		_, ok := a.self.(providers.Speaker)
		return ok
	case providers.CapTranscribe:
		_, ok := a.self.(providers.Transcriber)
		return ok
	case providers.CapModerate:
		_, ok := a.self.(providers.Moderator)
		return ok
	case providers.CapDecide:
		_, ok := a.self.(providers.Decider)
		return ok
	case providers.CapCreateVideo:
		_, ok := a.self.(providers.VideoCreator)
		return ok
	case providers.CapSearch:
		_, ok := a.self.(providers.Searcher)
		return ok
	case providers.CapWeather:
		_, ok := a.self.(providers.Weatherer)
		return ok
	case providers.CapVoices:
		_, ok := a.self.(providers.Voicer)
		return ok
	}
	return true
}

// TranscribeTranslate reports whether transcription can translate to English.
// Every dialect that does not say otherwise can.
func (a *baseAdapter) TranscribeTranslate() bool { return true }

// TranscribeTimestamps reports whether transcription returns word timings.
func (a *baseAdapter) TranscribeTimestamps() bool { return true }

// Accounts the adapter speaks for, for the dialects that need them.
func (a *baseAdapter) userID() string { return a.account.UserID }

// optionString reads one account option as a string.
func (a *baseAdapter) optionString(name string) string { return a.account.OptionString(name) }

// timeoutFor is the timeout of one call: the call's own, else the definition's
// quirks.timeoutMs, else the configured upstream timeout.
func (a *baseAdapter) timeoutFor(call providers.Call) time.Duration {
	return a.deps.TimeoutFor(a.def, call)
}

// timeoutOf is the timeout for one call whose timeout the caller did not choose:
// the definition's quirks.timeoutMs, else the configured upstream timeout.
func (a *baseAdapter) timeoutOf() time.Duration {
	return a.deps.TimeoutFor(a.def, providers.Call{})
}

// url resolves a path against the account's base URL. A path that is already an
// absolute URL is returned unchanged, as definitions use whole URLs for their
// secondary endpoints.
func (a *baseAdapter) url(pathname string) string {
	if strings.HasPrefix(pathname, "http://") || strings.HasPrefix(pathname, "https://") {
		return pathname
	}
	if strings.HasPrefix(pathname, "/") {
		return a.baseURL + pathname
	}
	return a.baseURL + "/" + pathname
}

// authHeaders is how the definition wants its key: the dialect's own header when
// it has one, else a bearer token, a named header, or nothing for a keyless
// provider.
func (a *baseAdapter) authHeaders() map[string]string {
	if a.auth != nil {
		return a.auth()
	}
	return a.definitionAuthHeaders()
}

// definitionAuthHeaders is the key handling for every dialect that does not
// override it.
func (a *baseAdapter) definitionAuthHeaders() map[string]string {
	auth := a.def.Auth
	secret := a.secret
	if secret == "" {
		secret = auth.AnonymousKey
	}
	if secret == "" || auth.Type == providers.AuthNone {
		return nil
	}
	// Custom endpoints may expect the key in a named header (e.g. x-api-key).
	if name := a.optionString("authHeader"); name != "" {
		return map[string]string{name: secret}
	}
	if auth.Type == providers.AuthHeader {
		return map[string]string{auth.Header: secret}
	}
	return map[string]string{"Authorization": "Bearer " + secret}
}

// headers are the headers of one call: the provider's user agent, the
// definition's own headers, the account's, the key, and the call's.
func (a *baseAdapter) headers(extra map[string]string) map[string]string {
	headers := a.baseHeaders()
	for name, value := range a.authHeaders() {
		headers[name] = value
	}
	for name, value := range extra {
		headers[name] = value
	}
	return headers
}

// baseHeaders are the headers the provider and the account ask for, without the
// key.
func (a *baseAdapter) baseHeaders() map[string]string {
	headers := map[string]string{}
	if agent := a.def.Quirks.UserAgent; agent != "" {
		headers["User-Agent"] = agent
	}
	for name, value := range a.def.Headers {
		headers[name] = value
	}
	for name, value := range a.account.OptionHeaders() {
		headers[name] = value
	}
	return headers
}

// request performs one decoded upstream call. timeout 0 means the definition's
// own timeout. Every call of an account goes through here, so accounts of
// non-admin owners stay on public hosts.
func (a *baseAdapter) request(ctx context.Context, pathname string, req providers.Request, timeout time.Duration) (*providers.Response, error) {
	return a.client.Request(ctx, a.url(pathname), a.prepare(req, timeout))
}

// requestRaw performs one upstream call and returns the answer unread, for
// streams and binary bodies.
func (a *baseAdapter) requestRaw(ctx context.Context, pathname string, req providers.Request, timeout time.Duration) (*http.Response, error) {
	return a.client.Do(ctx, a.url(pathname), a.prepare(req, timeout))
}

// requestStream opens a streamed answer. It has no total timeout: the caller's
// context ends it (the gateway's first-token and idle timers).
func (a *baseAdapter) requestStream(ctx context.Context, pathname string, req providers.Request) (*http.Response, error) {
	req.Streaming = true
	return a.requestRaw(ctx, pathname, req, 0)
}

// prepare fills in what every call of the account carries: the call bounds and
// the owner the private-network rule is checked against.
func (a *baseAdapter) prepare(req providers.Request, timeout time.Duration) providers.Request {
	if timeout == 0 {
		timeout = a.timeoutOf()
	}
	req.Timeout = timeout
	req.PublicOnly = a.account.PublicOnly
	req.Owner = a.account.UserID
	return req
}

// get performs an authenticated GET and decodes the answer.
func (a *baseAdapter) get(ctx context.Context, pathname string, timeout time.Duration) (*providers.Response, error) {
	return a.request(ctx, pathname, providers.Request{Method: http.MethodGet, Headers: httpHeaders(a.headers(nil))}, timeout)
}

// post performs an authenticated POST of a JSON body and decodes the answer.
func (a *baseAdapter) post(ctx context.Context, pathname string, body any, timeout time.Duration) (*providers.Response, error) {
	return a.request(ctx, pathname, providers.Request{Method: http.MethodPost, Headers: httpHeaders(a.headers(nil)), JSON: body}, timeout)
}

// Validate is the default key check: an authenticated GET of the definition's
// probe. A refusal of the key is invalid, a limit is still a good key, and
// anything else is unknown rather than invalid.
func (a *baseAdapter) Validate(ctx context.Context) (providers.Validation, error) {
	if spec := a.def.Validate; spec != nil && spec.Disabled {
		return providers.UnknownKey("This provider cannot be validated without spending quota."), nil
	}
	pathname := "/models"
	if spec := a.def.Validate; spec != nil && spec.Path != "" {
		pathname = spec.Path
	} else if a.def.Models.Path != "" {
		pathname = a.def.Models.Path
	}
	response, err := a.get(ctx, pathname, validationTimeoutMS*time.Millisecond)
	if err != nil {
		return validationVerdict(err, false)
	}
	if text, ok := response.Data.(string); ok && chatContainsHTML(text) {
		return providers.UnknownKey("The provider returned an HTML page instead of JSON."), nil
	}
	return providers.ValidKey(), nil
}

// validationVerdict is how a key check reads a failure. okNotFound means the
// dialect also accepts "this model is not open to the account" as a good key.
func validationVerdict(err error, okNotFound bool) (providers.Validation, error) {
	switch providers.KindOf(err) {
	case providers.KindAuth:
		return providers.InvalidKey(err.Error()), nil
	case providers.KindRateLimit, providers.KindQuotaExhausted, providers.KindPayment:
		// The key was accepted; it is only out of allowance or credits for now.
		// Free models and the next period still work, so this is no problem with
		// the connection.
		return providers.ValidKey(), nil
	case providers.KindNotFound:
		if okNotFound {
			// The key was accepted; this one model is just not open to it.
			return providers.ValidKey(), nil
		}
	}
	return providers.UnknownKey(err.Error()), nil
}

// ListModels is the default listing: a provider that lists nothing.
func (a *baseAdapter) ListModels(context.Context) ([]providers.Model, error) { return nil, nil }

// FetchQuota is the default quota read: a provider that reports no balance.
func (a *baseAdapter) FetchQuota(context.Context) ([]providers.QuotaWindow, error) {
	return nil, nil
}

// httpHeaders turns the string map the definitions declare into a header set.
func httpHeaders(values map[string]string) http.Header {
	if len(values) == 0 {
		return nil
	}
	headers := make(http.Header, len(values))
	for name, value := range values {
		headers.Set(name, value)
	}
	return headers
}

// templateFields replaces the {field} placeholders of a base URL with the
// account's option of that name, percent-encoded as encodeURIComponent does.
func templateFields(raw string, options map[string]any) string {
	if !strings.Contains(raw, "{") {
		return raw
	}
	var out strings.Builder
	for {
		start := strings.IndexByte(raw, '{')
		if start < 0 {
			break
		}
		end := strings.IndexByte(raw[start:], '}')
		if end < 0 {
			break
		}
		end += start
		out.WriteString(raw[:start])
		field := raw[start+1 : end]
		if field != "" && isWord(field) {
			out.WriteString(chatEncodeURIComponent(optionText(options[field])))
		} else {
			out.WriteString(raw[start : end+1])
		}
		raw = raw[end+1:]
	}
	out.WriteString(raw)
	return out.String()
}

// isWord reports whether name is \w+ (the placeholder shape that is
// substituted).
func isWord(name string) bool {
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_':
		default:
			return false
		}
	}
	return name != ""
}

// optionText is String(option || ”) for a URL placeholder.
func optionText(value any) string {
	if !chatTruthy(value) {
		return ""
	}
	return chatString(value)
}

// chatEncodeURIComponent percent-encodes the account's fields for the base URL
// templating.
func chatEncodeURIComponent(value string) string {
	var out strings.Builder
	for i := 0; i < len(value); i++ {
		c := value[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9',
			c == '-', c == '_', c == '.', c == '!', c == '~', c == '*', c == '\'', c == '(', c == ')':
			out.WriteByte(c)
		default:
			const hex = "0123456789ABCDEF"
			out.WriteByte('%')
			out.WriteByte(hex[c>>4])
			out.WriteByte(hex[c&0xF])
		}
	}
	return out.String()
}

// chatContainsHTML is the check for a provider that answered with a web page
// instead of JSON.
func chatContainsHTML(text string) bool {
	lower := strings.ToLower(text)
	return strings.Contains(lower, "<html")
}
