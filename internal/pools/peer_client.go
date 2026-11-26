package pools

// Outgoing control calls to other pool members, and the address rules for
// partners: HTTPS on the public internet; plain HTTP and private or local
// networks only where the instance allows private provider addresses (the
// admin's own LAN). A member without a public address has a relay address
// instead; calls to it go through the pool's host, sealed for that member.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/NeoLabs-Systems/neolabs-go/ssrf"
	"github.com/NeoLabs-Systems/neorouter/internal/providers"
)

// originOf is the origin of a NeoRouter ("https://router.example.com") from any
// URL on it, a member's relay address as it is, or "" when the value is neither.
func originOf(value string) string {
	if relayed, ok := parseRelayAddress(value); ok {
		return formatRelayAddress(relayed.PoolID, relayed.MemberID)
	}
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Host == "" {
		return ""
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "https" && scheme != "http" {
		return ""
	}
	if parsed.User != nil {
		return ""
	}
	host := strings.ToLower(parsed.Host)
	if (scheme == "http" && strings.HasSuffix(host, ":80")) || (scheme == "https" && strings.HasSuffix(host, ":443")) {
		host = host[:strings.LastIndex(host, ":")]
	}
	return scheme + "://" + host
}

// schemeOf is the protocol of an origin.
func schemeOf(origin string) string {
	parsed, err := url.Parse(origin)
	if err != nil {
		return ""
	}
	return strings.ToLower(parsed.Scheme)
}

// assertReachable applies the pool address rules to one partner origin.
func (s *Service) assertReachable(ctx context.Context, userID, origin string) error {
	if isRelayAddress(origin) {
		if !s.canUse(ctx, userID, origin) {
			return fail(http.StatusBadRequest, "POOL_RELAY_UNAVAILABLE",
				"This partner has no public address, and the pool’s host does not pass traffic along.")
		}
		return nil
	}
	restricted := s.publicOnly(ctx, userID)
	private, lookupErr := providers.CheckDestination(ctx, origin, nil)
	if lookupErr != nil && restricted {
		return fail(http.StatusBadGateway, "POOL_PARTNER_UNREACHABLE", "The other NeoRouter's address could not be resolved.")
	}
	if private && restricted {
		return fail(http.StatusBadRequest, "POOL_PRIVATE_ADDRESS",
			"This NeoRouter is on a local or private network. Only the instance admin can join pools there.")
	}
	if !private && schemeOf(origin) != "https" {
		return fail(http.StatusBadRequest, "POOL_HTTPS_REQUIRED", "Pool partners must be reached over HTTPS.")
	}
	return nil
}

// publicOnly reports whether this person's calls may only reach public
// addresses.
func (s *Service) publicOnly(ctx context.Context, userID string) bool {
	if s.Accounts == nil {
		return false
	}
	return s.Accounts.PublicOnly(ctx, userID)
}

// post sends one federation message: POST {origin}/federation/v1/{path} with a
// JSON body. Errors keep the partner's own code and message where it sent one.
func (s *Service) post(ctx context.Context, userID, origin, path string, body any) (any, error) {
	if err := s.assertReachable(ctx, userID, origin); err != nil {
		return nil, err
	}
	if s.Peers == nil {
		return nil, fail(http.StatusBadGateway, "POOL_PARTNER_UNREACHABLE",
			"The other NeoRouter could not be reached (no outbound HTTP layer).")
	}
	headers := http.Header{}
	headers.Set("User-Agent", PeerUserAgent)
	response, err := s.Peers.Request(ctx, strings.TrimRight(origin, "/")+"/federation/v1/"+path, providers.Request{
		Method:     http.MethodPost,
		JSON:       body,
		Timeout:    PeerTimeout,
		PublicOnly: s.publicOnly(ctx, userID),
		Owner:      userID,
		Headers:    headers,
	})
	if err != nil {
		var httpErr *HttpError
		if errors.As(err, &httpErr) {
			return nil, httpErr
		}
		return nil, s.peerFailure(err)
	}
	return response.Data, nil
}

// peerFailure turns one failed call into the pool error a person sees.
func (s *Service) peerFailure(err error) error {
	var providerErr *providers.ProviderError
	status := http.StatusBadGateway
	reached := false
	message := err.Error()
	var body any
	if errors.As(err, &providerErr) {
		message = providerErr.Message
		body = providerErr.Body
		if providerErr.Status > 0 {
			reached = true
			status = providerErr.Status
			if status >= 500 {
				status = http.StatusBadGateway
			}
		}
	}
	if reached {
		if remote, ok := bodyField(body, "error"); ok {
			code, _ := stringField(remote, "code")
			text, _ := stringField(remote, "message")
			if strings.HasPrefix(code, "POOL_") && text != "" {
				if len(text) > 300 {
					text = truncateUTF16(text, 300)
				}
				return fail(status, code, text)
			}
		}
		return fail(status, "POOL_PARTNER_REFUSED", "The other NeoRouter answered: "+message)
	}
	return fail(status, "POOL_PARTNER_UNREACHABLE",
		"The other NeoRouter could not be reached ("+message+").")
}

// answersDirectly reports whether a joiner's own address answers from here: one
// GET of its health page, under the usual address rules and without following
// redirects.
func (s *Service) answersDirectly(ctx context.Context, userID, origin string) bool {
	if err := s.assertReachable(ctx, userID, origin); err != nil {
		return false
	}
	client := ssrf.NewClient(ssrf.Options{AllowPrivate: !s.publicOnly(ctx, userID), Timeout: ProbeTimeout})
	client.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(origin, "/")+"/health", nil)
	if err != nil {
		return false
	}
	request.Header.Set("User-Agent", PeerUserAgent)
	response, err := client.Do(request)
	if err != nil {
		return false
	}
	_, _ = response.Body.Read(make([]byte, 0))
	response.Body.Close()
	return response.StatusCode >= 200 && response.StatusCode < 300
}

// objectOf is a decoded JSON object, or nil.
func objectOf(value any) map[string]any {
	object, _ := value.(map[string]any)
	return object
}

// bodyField is one field of a decoded JSON object.
func bodyField(value any, key string) (any, bool) {
	object, ok := value.(map[string]any)
	if !ok {
		return nil, false
	}
	field, ok := object[key]
	return field, ok
}

// stringField is one string field of a decoded JSON object.
func stringField(value any, key string) (string, bool) {
	object, ok := value.(map[string]any)
	if !ok {
		return "", false
	}
	text, ok := object[key].(string)
	return text, ok
}

// objectField is one object field of a decoded JSON object.
func objectField(value any, key string) map[string]any {
	object, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	field, _ := object[key].(map[string]any)
	return field
}

// listField is one list field of a decoded JSON object.
func listField(value any, key string) []any {
	object, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	list, _ := object[key].([]any)
	return list
}

// numberField is one number field of a decoded JSON object.
func numberField(value any, key string) float64 {
	object, ok := value.(map[string]any)
	if !ok {
		return 0
	}
	switch typed := object[key].(type) {
	case float64:
		return typed
	case json.Number:
		number, err := typed.Float64()
		if err != nil {
			return 0
		}
		return number
	}
	return 0
}

// truncateUTF16 cuts a string to n UTF-16 code units, as the wire limits count
// does.
func truncateUTF16(value string, n int) string {
	units := 0
	for index, r := range value {
		width := 1
		if r > 0xFFFF {
			width = 2
		}
		if units+width > n {
			return value[:index]
		}
		units += width
	}
	return value
}
