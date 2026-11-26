package adapters

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/NeoLabs-Systems/neorouter/internal/providers"
)

// The media_* files of this package carry the non-chat dialects: the
// OpenAI-style media surface (media_openai.go), the voice providers
// (media_deepgram.go, media_elevenlabs.go, media_riva.go), the WAV helpers
// (media_wav.go), web search (media_search.go) and weather (media_weather.go).
// The chat_* files hold the chat dialects; the two sets share only the parent
// package and never edit each other's files.

// mediaBase is the per-account context a media adapter speaks through: the
// definition, the account's key and options, and the provider HTTP layer
// (providers/adapters/base_adapter.js). It is the media files' own base, so the
// chat agent's base adapter and this one never collide.
type mediaBase struct {
	def     providers.Definition
	deps    providers.AdapterDeps
	account providers.Account
	secret  string
	options map[string]any
	// baseURL is the account's base URL or the definition's, with the account's
	// option placeholders filled in and trailing slashes dropped.
	baseURL string
	// authPrefix replaces Bearer for a provider that names its own scheme
	// (Deepgram's "Token").
	authPrefix string
}

// newMediaBase builds the context every media adapter speaks through.
func newMediaBase(def providers.Definition, deps providers.AdapterDeps) *mediaBase {
	options := deps.Account.Options
	if options == nil {
		options = map[string]any{}
	}
	base := deps.Account.BaseURL
	if base == "" {
		base = def.BaseURL
	}
	return &mediaBase{
		def:     def,
		deps:    deps,
		account: deps.Account,
		secret:  deps.Account.Secret,
		options: options,
		baseURL: strings.TrimRight(mediaTemplate(base, options), "/"),
	}
}

// Definition is the provider this adapter was built from.
func (b *mediaBase) Definition() providers.Definition { return b.def }

// option reads one account option as the JS `options[field] || ”` would.
func (b *mediaBase) option(name string) string { return mediaOptionString(b.options[name]) }

// mediaTemplateField matches the {field} placeholders a base URL may carry.
var mediaTemplateField = regexp.MustCompile(`\{(\w+)\}`)

// mediaTemplate fills the account's option values into a base URL, encoded the
// way encodeURIComponent encodes them.
func mediaTemplate(raw string, options map[string]any) string {
	if raw == "" || !strings.Contains(raw, "{") {
		return raw
	}
	return mediaTemplateField.ReplaceAllStringFunc(raw, func(match string) string {
		return mediaEncodeURIComponent(mediaOptionString(options[match[1:len(match)-1]]))
	})
}

// mediaOptionString is String(value || ”) for one account option.
func mediaOptionString(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case bool:
		if v {
			return "true"
		}
		return ""
	case float64:
		if v == 0 {
			return ""
		}
		return strconv.FormatFloat(v, 'f', -1, 64)
	}
	return fmt.Sprint(value)
}

// mediaHexDigits is the percent-encoding alphabet.
const mediaHexDigits = "0123456789ABCDEF"

// mediaEncodeURIComponent is JavaScript's encodeURIComponent: everything
// outside the unreserved set and !~*'() is percent-encoded.
func mediaEncodeURIComponent(text string) string {
	var out strings.Builder
	out.Grow(len(text))
	for i := 0; i < len(text); i++ {
		char := text[i]
		switch {
		case char >= 'A' && char <= 'Z', char >= 'a' && char <= 'z', char >= '0' && char <= '9',
			char == '-', char == '_', char == '.', char == '!', char == '~', char == '*',
			char == '\'', char == '(', char == ')':
			out.WriteByte(char)
		default:
			out.WriteByte('%')
			out.WriteByte(mediaHexDigits[char>>4])
			out.WriteByte(mediaHexDigits[char&0x0f])
		}
	}
	return out.String()
}

// mediaAbsoluteURL matches an address that is already complete.
var mediaAbsoluteURL = regexp.MustCompile(`^https?://`)

// url resolves path against the base URL (BaseAdapter.url).
func (b *mediaBase) url(path string) string {
	if mediaAbsoluteURL.MatchString(path) {
		return path
	}
	separator := "/"
	if strings.HasPrefix(path, "/") {
		separator = ""
	}
	return b.baseURL + separator + path
}

// authHeaders is the account's key in the place the definition wants it.
func (b *mediaBase) authHeaders() http.Header {
	headers := http.Header{}
	auth := b.def.Auth
	kind := auth.Type
	if kind == "" {
		kind = providers.AuthBearer
	}
	secret := b.secret
	if secret == "" {
		secret = auth.AnonymousKey
	}
	if secret == "" || kind == providers.AuthNone {
		return headers
	}
	// Custom endpoints may expect the key in a named header (e.g. x-api-key).
	if name := b.option("authHeader"); name != "" {
		headers.Set(name, secret)
		return headers
	}
	if kind == providers.AuthHeader {
		headers.Set(auth.Header, secret)
		return headers
	}
	prefix := b.authPrefix
	if prefix == "" {
		prefix = "Bearer"
	}
	headers.Set("Authorization", prefix+" "+secret)
	return headers
}

// headers are the request headers every call of this account carries: the
// definition's, the account's own, the key, then extra (which wins).
func (b *mediaBase) headers(extra http.Header) http.Header {
	merged := http.Header{}
	if agent := b.def.Quirks.UserAgent; agent != "" {
		merged.Set("User-Agent", agent)
	}
	for name, value := range b.def.Headers {
		merged.Set(name, value)
	}
	for name, value := range b.account.OptionHeaders() {
		merged.Set(name, value)
	}
	for name, values := range b.authHeaders() {
		merged[name] = values
	}
	for name, values := range extra {
		merged[name] = values
	}
	return merged
}

// timeout is how long one call may take: the call's own, else the definition's
// quirks.timeoutMs, else the configured default.
func (b *mediaBase) timeout(call providers.Call) time.Duration {
	return b.deps.TimeoutFor(b.def, call)
}

// prepared fills in what every call of this account carries: its headers, the
// public-destination rule of its owner, and its timeout.
func (b *mediaBase) prepared(req providers.Request, call providers.Call) providers.Request {
	req.Headers = b.headers(req.Headers)
	req.PublicOnly = b.account.PublicOnly
	if req.Owner == "" {
		req.Owner = b.account.UserID
	}
	if req.Timeout == 0 {
		req.Timeout = b.timeout(call)
	}
	return req
}

// do performs one call and returns its decoded answer.
func (b *mediaBase) do(ctx context.Context, path string, req providers.Request, call providers.Call) (*providers.Response, error) {
	if b.deps.HTTP == nil {
		return nil, fmt.Errorf("adapters: no provider HTTP layer configured")
	}
	return b.deps.HTTP.Request(ctx, b.url(path), b.prepared(req, call))
}

// doGet is do for a GET.
func (b *mediaBase) doGet(ctx context.Context, path string, call providers.Call) (*providers.Response, error) {
	return b.do(ctx, path, providers.Request{Method: http.MethodGet}, call)
}

// doPost is do for a JSON POST.
func (b *mediaBase) doPost(ctx context.Context, path string, body any, call providers.Call) (*providers.Response, error) {
	return b.do(ctx, path, providers.Request{Method: http.MethodPost, JSON: body}, call)
}

// doRaw performs one call and hands back the answer unread: audio, images and
// videos come back as bytes, and their content type is part of the result.
func (b *mediaBase) doRaw(ctx context.Context, path string, req providers.Request, call providers.Call) (*http.Response, error) {
	if b.deps.HTTP == nil {
		return nil, fmt.Errorf("adapters: no provider HTTP layer configured")
	}
	response, err := b.deps.HTTP.Do(ctx, b.url(path), b.prepared(req, call))
	if err != nil {
		return nil, err
	}
	if call.OnHeaders != nil {
		call.OnHeaders(response.Header)
	}
	return response, nil
}

// doForeign performs a call to an address of another host without the
// account's headers: a helper endpoint such as an open-meteo geocoding lookup is
// not the provider's API, so its key is not sent to it.
func (b *mediaBase) doForeign(ctx context.Context, rawURL string, req providers.Request, call providers.Call) (*providers.Response, error) {
	if b.deps.HTTP == nil {
		return nil, fmt.Errorf("adapters: no provider HTTP layer configured")
	}
	req.PublicOnly = b.account.PublicOnly
	if req.Owner == "" {
		req.Owner = b.account.UserID
	}
	if req.Timeout == 0 {
		req.Timeout = b.timeout(call)
	}
	return b.deps.HTTP.Request(ctx, rawURL, req)
}

// mediaBytes is one downloaded body: its bytes, the type it came with and the
// upstream headers (openai_media.js's binary()).
type mediaBytes struct {
	Data    []byte
	Mime    string
	Headers http.Header
}

// readMediaBytes reads and closes a raw answer.
func readMediaBytes(response *http.Response) (mediaBytes, error) {
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		return mediaBytes{}, providers.FromNetwork(err)
	}
	mime := response.Header.Get("Content-Type")
	if mime == "" {
		mime = "application/octet-stream"
	}
	return mediaBytes{Data: data, Mime: mime, Headers: response.Header}, nil
}

// fetchBytes performs one raw call and reads its body.
func (b *mediaBase) fetchBytes(ctx context.Context, path string, req providers.Request, call providers.Call) (mediaBytes, error) {
	response, err := b.doRaw(ctx, path, req, call)
	if err != nil {
		return mediaBytes{}, err
	}
	return readMediaBytes(response)
}

// mediaHTMLPage is what a provider's HTML error page looks like: the key was
// not refused, the endpoint was simply not the API.
var mediaHTMLPage = regexp.MustCompile(`(?i)<html`)

// mediaValidButLimited are the kinds that mean the key was accepted and only
// the allowance is used up for now (base_adapter.js VALID_BUT_LIMITED).
var mediaValidButLimited = map[providers.Kind]bool{
	providers.KindRateLimit:      true,
	providers.KindQuotaExhausted: true,
	providers.KindPayment:        true,
}

// classifyValidation reads the provider error behind a failed check.
func classifyValidation(err error) (providers.Validation, error) {
	classified := providers.AsProviderError(err)
	if classified == nil {
		return providers.UnknownKey(err.Error()), nil
	}
	if classified.Kind == providers.KindAuth {
		return providers.InvalidKey(classified.Message), nil
	}
	if mediaValidButLimited[classified.Kind] {
		return providers.ValidKey(), nil
	}
	return providers.UnknownKey(classified.Message), nil
}

// validateDefault is base_adapter.js's check: an authenticated GET, where 401
// and 403 are a definite rejection and anything else unexpected is "unknown",
// never "invalid".
func (b *mediaBase) validateDefault(ctx context.Context) (providers.Validation, error) {
	spec := b.def.Validate
	if spec != nil && spec.Disabled {
		return providers.UnknownKey("This provider cannot be validated without spending quota."), nil
	}
	path := "/models"
	if spec != nil && spec.Path != "" {
		path = spec.Path
	} else if b.def.Models.Path != "" {
		path = b.def.Models.Path
	}
	response, err := b.doGet(ctx, path, providers.Call{Timeout: 20 * time.Second})
	if err != nil {
		return classifyValidation(err)
	}
	if text, ok := response.Data.(string); ok && mediaHTMLPage.MatchString(text) {
		return providers.UnknownKey("The provider returned an HTML page instead of JSON."), nil
	}
	return providers.ValidKey(), nil
}

// staticModels normalizes the definition's fixed model listing.
func (b *mediaBase) staticModels(overrides func(entry map[string]any) providers.ModelOverrides) []providers.Model {
	models := make([]providers.Model, 0, len(b.def.Models.Static))
	for _, entry := range b.def.Models.Static {
		raw := mediaObject(entry)
		var applied providers.ModelOverrides
		if overrides != nil {
			applied = overrides(raw)
		}
		models = append(models, providers.NormalizeModel(raw, applied))
	}
	return models
}
