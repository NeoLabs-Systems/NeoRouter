package live

import (
	"net/http"
	"regexp"

	"github.com/NeoLabs-Systems/neorouter/internal/providers"
)

// Connection is one provider account as the relay reads it: the definition the
// account was created from plus the account's own values. It is what
// account_service.getRuntime(accountId) and adapterFor(runtime) give the Node
// gateway, so the relay can read an account's base URL, headers and live
// protocols without depending on the accounts domain.
type Connection struct {
	Definition providers.Definition
	Account    providers.Account
}

// baseURLPlaceholder fills a definition's {field} base URL placeholders from the
// account's options (encodable, as the JS adapter does).
var baseURLPlaceholder = regexp.MustCompile(`\{(\w+)\}`)

// BaseURL is the adapter's baseUrl: the account's own URL, else the
// definition's, with {field} placeholders filled in and trailing slashes
// dropped.
func (c Connection) BaseURL() string {
	base := c.Account.BaseURL
	if base == "" {
		base = c.Definition.BaseURL
	}
	filled := baseURLPlaceholder.ReplaceAllStringFunc(base, func(match string) string {
		field := match[1 : len(match)-1]
		return encodeURIComponent(c.Account.OptionString(field))
	})
	for len(filled) > 1 && filled[len(filled)-1] == '/' {
		filled = filled[:len(filled)-1]
	}
	return filled
}

// Headers is the adapter's headers(): the provider's user agent and headers, the
// account's own headers, then whatever authenticates it.
func (c Connection) Headers() http.Header {
	headers := http.Header{}
	if agent := c.Definition.Quirks.UserAgent; agent != "" {
		headers.Set("User-Agent", agent)
	}
	for name, value := range c.Definition.Headers {
		headers.Set(name, value)
	}
	for name, value := range c.Account.OptionHeaders() {
		headers.Set(name, value)
	}
	secret := c.Account.Secret
	if secret == "" {
		secret = c.Definition.Auth.AnonymousKey
	}
	// A keyless provider, or one that wants no header at all.
	if secret == "" || c.Definition.Auth.Type == providers.AuthNone {
		return headers
	}
	// Custom endpoints may expect the key in a named header (e.g. x-api-key).
	if name := c.Account.OptionString("authHeader"); name != "" {
		headers.Set(name, secret)
		return headers
	}
	if c.Definition.Auth.Type == providers.AuthHeader && c.Definition.Auth.Header != "" {
		headers.Set(c.Definition.Auth.Header, secret)
		return headers
	}
	headers.Set("Authorization", "Bearer "+secret)
	return headers
}

// Speaks reports whether the provider speaks a live protocol
// (definition.live.includes(id)).
func (c Connection) Speaks(id string) bool {
	for _, protocol := range c.Definition.Live {
		if string(protocol) == id {
			return true
		}
	}
	return false
}
