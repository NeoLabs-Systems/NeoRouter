package live

import (
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/NeoLabs-Systems/neorouter/internal/providers"
)

// The live (speech-to-speech) WebSocket protocols NeoRouter speaks. Clients
// connect to the same path they would use at the provider, with a router key
// instead of the provider's, and NeoRouter relays the session to a provider
// that speaks the same protocol. Messages pass through unchanged except the
// model name in the opening message, so every feature of the protocol works.
// Each dialect says where the model is named, how to open the upstream socket
// and how its errors look.
//
// The dialect details below are the ones neolabs-go/livevoice speaks in
// gemini.go and openai.go: the Gemini BidiGenerateContent path, "models/<id>"
// in the setup message, the key in the query string, and the OpenAI
// session.start event carrying the model. livevoice has no usage parser (its
// adapters report through Handlers), so usageOf lives here.
const (
	geminiDialectPath = `^/ws/google\.ai\.generativelanguage\.(v1alpha|v1beta)\.GenerativeService\.BidiGenerateContent$`

	openAILivePath     = "/v1/live/sessions"
	openAIRealtimePath = "/v1/realtime"

	// insecureKeyProtocol carries the API key as a WebSocket subprotocol, the
	// way OpenAI's browser clients send it.
	insecureKeyProtocol = "openai-insecure-api-key."
)

var geminiPathRE = regexp.MustCompile(geminiDialectPath)

// sanitizedProtocols are the characters encodeURIComponent leaves alone.
const uriComponentSafe = "!*'()"

// upstreamTarget is where one attempt's provider socket lives.
type upstreamTarget struct {
	url     string
	headers http.Header
}

// dialect is one live protocol: where the model is named, how to open the
// upstream socket and how its errors look.
type dialect struct {
	// id is the protocol name stored as the request's api_format.
	id string
	// needsFirst marks a protocol whose model is named in the opening message.
	needsFirst bool
	// match reports whether a client path belongs to this protocol.
	match func(path string) bool
	// modelFromURL is the model a URL names; ok is false when the URL names none.
	modelFromURL func(u *url.URL) (string, bool)
	// modelFromFirst is the model the opening message names; ok is false when the
	// message does not open a session.
	modelFromFirst func(first *object) (string, bool)
	// rewriteFirst is the opening message with the chosen model in it.
	rewriteFirst func(first *object, modelID string) *object
	// upstream is the provider socket for one catalog entry.
	upstream func(conn Connection, modelID string, client *http.Request) upstreamTarget
	// errorEvent is the protocol's own error event; nil when the provider reports
	// errors by closing the socket (Gemini).
	errorEvent func(message, code string) *object
}

// dialects are every live protocol, in the order dialectFor tries them.
var dialects = []*dialect{openAILiveDialect, openAIRealtimeDialect, geminiLiveDialect}

// dialectFor is dialectFor(path): the protocol a client path speaks, nil for a
// path this gateway does not serve.
func dialectFor(path string) *dialect {
	for _, candidate := range dialects {
		if candidate.match(path) {
			return candidate
		}
	}
	return nil
}

// knownPath reports whether a path belongs to any live protocol; a request for
// another path is not this gateway's (the pool relay and the HTTP routes have
// their own).
func knownPath(path string) bool { return dialectFor(path) != nil }

// openAILiveDialect is OpenAI GPT-Live: the model arrives in the first event,
// session.start.
var openAILiveDialect = &dialect{
	id:         string(providers.LiveOpenAI),
	needsFirst: true,
	match:      func(path string) bool { return path == openAILivePath },
	modelFromURL: func(*url.URL) (string, bool) {
		return "", false
	},
	modelFromFirst: func(first *object) (string, bool) {
		if first.String("type") != "session.start" {
			return "", false
		}
		return modelField(first.Object("session")), true
	},
	rewriteFirst: func(first *object, modelID string) *object {
		out := copyObject(first)
		session := copyObject(first.Object("session"))
		session.Set("model", modelID)
		out.Set("session", session)
		return out
	},
	upstream: func(conn Connection, modelID string, _ *http.Request) upstreamTarget {
		// The model is not in the URL: the session.start event names it.
		return upstreamTarget{url: toWS(conn.BaseURL()) + "/live/sessions", headers: conn.Headers()}
	},
	errorEvent: openAIError,
}

// openAIRealtimeDialect is OpenAI Realtime: the model is a URL parameter and the
// server speaks first.
var openAIRealtimeDialect = &dialect{
	id: string(providers.LiveRealtime),
	match: func(path string) bool {
		return path == openAIRealtimePath
	},
	modelFromURL: func(u *url.URL) (string, bool) {
		if model := u.Query().Get("model"); model != "" {
			return model, true
		}
		return "auto", true
	},
	upstream: func(conn Connection, modelID string, client *http.Request) upstreamTarget {
		headers := conn.Headers()
		// A client that asked for a beta protocol keeps asking for it.
		if beta := client.Header.Get("openai-beta"); beta != "" {
			headers.Set("OpenAI-Beta", beta)
		}
		return upstreamTarget{
			url:     toWS(conn.BaseURL()) + "/realtime?model=" + encodeURIComponent(modelID),
			headers: headers,
		}
	},
	errorEvent: openAIError,
}

// geminiLiveDialect is Gemini Live: the model arrives in the first message,
// setup.
var geminiLiveDialect = &dialect{
	id:         string(providers.LiveGemini),
	needsFirst: true,
	match:      func(path string) bool { return geminiPathRE.MatchString(path) },
	modelFromURL: func(*url.URL) (string, bool) {
		return "", false
	},
	modelFromFirst: func(first *object) (string, bool) {
		if first.Object("setup") == nil {
			return "", false
		}
		return strings.TrimPrefix(modelField(first.Object("setup")), "models/"), true
	},
	rewriteFirst: func(first *object, modelID string) *object {
		out := copyObject(first)
		setup := copyObject(first.Object("setup"))
		setup.Set("model", "models/"+modelID)
		out.Set("setup", setup)
		return out
	},
	upstream: func(conn Connection, _ string, client *http.Request) upstreamTarget {
		// Gemini names the model in the setup message and takes the key in the
		// query string.
		target := originOf(conn.BaseURL()) + client.URL.Path
		return upstreamTarget{url: target + "?key=" + encodeURIComponent(conn.Account.Secret)}
	},
	// Gemini reports errors by closing the socket; the reason carries the text.
	errorEvent: nil,
}

// modelField is String(value.model || 'auto') for a setup or session object.
func modelField(holder *object) string {
	value, ok := holder.Get("model")
	if !ok || !truthy(value) {
		return "auto"
	}
	return jsString(value)
}

// openAIError is the OpenAI protocols' error event.
func openAIError(message, code string) *object {
	inner := newObject()
	inner.Set("type", "invalid_request_error")
	inner.Set("code", code)
	inner.Set("message", message)
	out := newObject()
	out.Set("type", "error")
	out.Set("error", inner)
	return out
}

// usageOf reads the token counts any of the protocols report, ok false when the
// message reports none.
func usageOf(event *object) (input, output float64, ok bool) {
	if gemini := event.Object("usageMetadata"); gemini != nil {
		count, present := gemini.Get("promptTokenCount")
		input = orZero(count, present)
		return input, coalesce(gemini, "responseTokenCount", "candidatesTokenCount"), true
	}
	openai := event.Object("response")
	if openai != nil {
		openai = openai.Object("usage")
	}
	if openai == nil {
		openai = event.Object("usage")
	}
	if openai == nil {
		return 0, 0, false
	}
	inputValue, hasInput := openai.Get("input_tokens")
	outputValue, hasOutput := openai.Get("output_tokens")
	if !hasInput && !hasOutput {
		return 0, 0, false
	}
	return orZero(inputValue, hasInput), orZero(outputValue, hasOutput), true
}

// toWS is toWs(base): the same address over the WebSocket scheme, without a
// trailing slash.
func toWS(base string) string {
	trimmed := strings.TrimRight(base, "/")
	lowered := strings.ToLower(trimmed)
	switch {
	case strings.HasPrefix(lowered, "https:"):
		return "wss:" + trimmed[len("https:"):]
	case strings.HasPrefix(lowered, "http:"):
		return "ws:" + trimmed[len("http:"):]
	}
	return trimmed
}

// originOf is new URL(base).origin: scheme and host, without a path.
func originOf(base string) string {
	parsed, err := url.Parse(base)
	if err != nil || parsed.Host == "" {
		// An unparseable base makes the socket fail as a network error; the
		// empty origin does that.
		return ""
	}
	return parsed.Scheme + "://" + parsed.Host
}

// encodeURIComponent percent-encodes every byte but the unreserved characters
// and !*'().
func encodeURIComponent(value string) string {
	var b strings.Builder
	for i := 0; i < len(value); i++ {
		c := value[i]
		if isURIComponentByte(c) {
			b.WriteByte(c)
			continue
		}
		b.WriteByte('%')
		b.WriteByte(hexDigits[c>>4])
		b.WriteByte(hexDigits[c&0x0f])
	}
	return b.String()
}

const hexDigits = "0123456789ABCDEF"

func isURIComponentByte(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		return true
	}
	return strings.IndexByte("-_.~"+uriComponentSafe, c) >= 0
}

// jsString is the text form of the JSON values a model name can be.
func jsString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case bool:
		if typed {
			return "true"
		}
		return "false"
	case nil:
		return "null"
	case json.Number:
		number, err := typed.Float64()
		if err != nil {
			return typed.String()
		}
		return strconv.FormatFloat(number, 'g', -1, 64)
	default:
		return ""
	}
}

// truthy is truthiness for the JSON values a field can hold.
func truthy(value any) bool {
	switch typed := value.(type) {
	case nil:
		return false
	case bool:
		return typed
	case string:
		return typed != ""
	case *object:
		return true
	case []any:
		return true
	default:
		return jsNumber(typed) != 0
	}
}

// copyObject is a shallow {...value}; an absent object copies as {}.
func copyObject(value *object) *object {
	out := newObject()
	if value == nil {
		return out
	}
	for _, key := range value.keys {
		out.Set(key, value.values[key])
	}
	return out
}
