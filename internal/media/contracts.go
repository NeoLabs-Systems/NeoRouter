package media

import (
	"context"
	"net/http"
	"time"

	"github.com/NeoLabs-Systems/neorouter/internal/catalog"
	"github.com/NeoLabs-Systems/neorouter/internal/providers"
	"github.com/NeoLabs-Systems/neorouter/internal/routerkeys"
)

// Runner runs one non-chat capability through the shared pipeline
// (server/capabilities/shared/run_capability.js): the key's per-minute limit and
// budget gate, the routing plan over the models of that kind, one attempt per
// candidate with fallback across providers, learning from every attempt, quota
// counting and the usage log. internal/gateway owns that pipeline; media
// declares this narrow view of it and internal/app satisfies it.
type Runner interface {
	// Run answers one capability request. It returns the attempt that succeeded,
	// and the error that ended the request (an *httpserver.HttpError with the
	// status, code and text the OpenAI-shaped gateway error carries).
	Run(ctx context.Context, options *RunOptions) (*Result, error)
}

// RunOptions is one capability request as media hands it to the pipeline —
// everything run_capability.js's options object carries.
type RunOptions struct {
	// Request is the HTTP request being answered; Response receives the
	// pipeline's headers (X-NeoRouter-Model/Trail/Redacted, the rate-limit
	// figures, Retry-After). Response is nil for an internal caller such as
	// chat's web-search tool, which reads the result instead of a response.
	Request  *http.Request
	Response http.ResponseWriter
	// Key is the router key the request is billed to and limited by.
	Key *routerkeys.Row
	// Capability is the catalog kind ('embedding', 'image', 'speech',
	// 'transcription', 'moderation', 'video', 'search', 'weather'); Method is
	// the adapter method that serves it, so only providers whose adapter speaks
	// it become candidates.
	Capability string
	Method     providers.Capability
	// RequestedModel is what the client asked for: 'auto' (or auto:<profile>)
	// lets routing choose, anything else is that model only.
	RequestedModel string
	// Format is the api_format the usage log records; empty means Capability.
	Format string
	// FreeOnly keeps the request on free models (the playground's switch, and a
	// key whose budget is used up).
	FreeOnly bool
	// InputTokens sizes the request for the quota checks and the log.
	// ContextTokens is the text that must fit the model's context window; nil
	// means InputTokens (audio is not text in a context window, so a
	// transcription passes its own figure).
	InputTokens   int
	ContextTokens *int
	// Units are the billed units of this request (images, seconds, minutes,
	// characters), for the log; RedactedCount is what the privacy shield hid.
	Units         float64
	RedactedCount int
	// Filter keeps a candidate only when it can serve this request: an embedding
	// pin, a transcriber that returns timestamps, a model that can translate.
	Filter func(entry catalog.Entry, adapter providers.Adapter) bool
	// NoCandidate explains an empty candidate list (a pinned embedding model no
	// provider serves); nil falls back to the pipeline's own "no model
	// available" answer.
	NoCandidate func() error
	// EstimateCost is the worst-case cost of one attempt in USD, reserved before
	// the call; nil is an unknown price, which keeps budget keys off such models.
	EstimateCost func(entry catalog.Entry) *float64
	// Invoke makes one attempt. The context carries the attempt's deadline.
	Invoke func(ctx context.Context, attempt Attempt) (*Outcome, error)
	// CostOf is what the attempt actually cost in USD.
	CostOf func(entry catalog.Entry, result *Outcome) *float64
	// Respond writes the answer. Without it the pipeline returns the result of
	// the attempt instead of writing a response (internal callers).
	Respond func(w http.ResponseWriter, result *Outcome, entry catalog.Entry) error
}

// Attempt is one candidate the pipeline chose for a request.
type Attempt struct {
	Entry   catalog.Entry
	Adapter providers.Adapter
	// Timeout bounds the call, the way adapter.timeoutMs bounded it in the JS
	// pipeline; the context passed to Invoke carries the same deadline.
	Timeout time.Duration
}

// Outcome is one attempt's answer: the adapter's result, plus the usage facts
// the pipeline logs when they differ from the request's own estimate (a
// transcription's real duration, an embedder's reported token count).
type Outcome struct {
	// Value is the adapter's result (providers.EmbedResult, *providers.ImageResult,
	// *providers.SpeechResult, *providers.Transcript, ...), read by Respond and
	// CostOf.
	Value       any
	InputTokens *int
	Units       *float64
	// Headers are the upstream response headers, for quota learning.
	Headers http.Header
}

// Result is what the pipeline returns for a finished request.
type Result struct {
	Outcome *Outcome
	Entry   catalog.Entry
}

// Accounts is the provider account service as media needs it
// (services/providers/account_service.js): the adapter bound to one connected
// account. internal/providers satisfies it.
type Accounts interface {
	// Adapter returns the adapter that speaks for accountID, or nil when the
	// account is gone or its adapter cannot be built.
	Adapter(accountID string) providers.Adapter
}

// KeyAuth is the gateway's router-key authentication middleware
// (server/middleware/router_key_auth.js), which every /v1 media route sits
// behind. internal/gateway satisfies it.
type KeyAuth interface {
	// Require wraps a handler so only a request with a valid router key reaches
	// it.
	Require(next http.Handler) http.Handler
	// Key reads the router key Require resolved for this request.
	Key(r *http.Request) (*routerkeys.Row, bool)
}

// Shielded is what the privacy shield (internal/processing) returned: the texts
// as they may leave, and how many values it hid.
type Shielded struct {
	Texts    []string
	Redacted int
}

// Shield hides what must not leave for a provider (services/processing
// shieldTexts): secrets and card numbers in text that leaves without an answer.
// A nil Shield lets every text through untouched.
type Shield func(userID string, texts []string) (Shielded, error)
