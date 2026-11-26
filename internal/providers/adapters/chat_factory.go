package adapters

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/NeoLabs-Systems/neorouter/internal/providers"
)

// Factory builds the chat adapters of this package. Its fields are the
// collaborators the adapters need from other domains: a domain declares the
// narrow interface it consumes and internal/app satisfies it (CONVERSION_PLAN.md
// rule 2). A nil collaborator is allowed — the connection then reports the
// failure a person can act on (connect it again, ask an admin) instead of
// panicking.
type Factory struct {
	// Codex is the ChatGPT sign-in store. *providers.OAuth satisfies it.
	Codex CodexSignIn
	// PersistCodexSecret stores a rotated ChatGPT secret on the account. The
	// OAuth service rotates the refresh token on every refresh, so a refresh that
	// is not stored leaves the saved connection with a dead one; nil stores
	// nothing, which is only right for a connection that is not saved yet.
	PersistCodexSecret func(accountID, secret string) error
	// Nous is the Nous Portal sign-in store. *providers.OAuth satisfies it.
	Nous NousSignIn
	// PersistNousSecret stores a rotated Portal secret on the account, with the
	// same rule as PersistCodexSecret.
	PersistNousSecret func(accountID, secret string) error
	// NousIsSignedIn reports whether a secret is a Portal sign-in rather than a
	// pasted API key. Nil means providers.NousIsSignIn.
	NousIsSignedIn func(secret string) bool
	// NousPortalURL overrides the Portal API host the Nous balance is read from
	// (NEOROUTER_NOUS_PORTAL_URL).
	NousPortalURL string
	// Local is the local model service.
	Local LocalModels

	once   sync.Once
	prompt *codexInstructions
}

// ErrNotChatAdapter is returned by NewAdapter for a definition this package's
// chat files do not implement, so a wiring layer can hand it to the media
// adapters instead.
var ErrNotChatAdapter = errors.New("adapters: definition is not served by a chat adapter")

// ChatAdapterNames are the adapter dialects NewAdapter builds.
var ChatAdapterNames = []providers.AdapterName{
	providers.AdapterOpenAICompatible,
	providers.AdapterAnthropic,
	providers.AdapterGoogleGemini,
	providers.AdapterCloudflare,
	providers.AdapterCodex,
	providers.AdapterNous,
	providers.AdapterLocalRuntime,
}

// AdapterFactory is the factory in the shape providers.Registry wants.
func (f *Factory) AdapterFactory() providers.AdapterFactory { return f.NewAdapter }

// NewAdapter builds the adapter for one definition and account. A definition of
// another domain's dialect returns ErrNotChatAdapter.
func (f *Factory) NewAdapter(def providers.Definition, deps providers.AdapterDeps) (providers.Adapter, error) {
	switch def.Adapter {
	case providers.AdapterOpenAICompatible:
		return newOpenAICompatible(def, deps), nil
	case providers.AdapterAnthropic:
		return newAnthropic(def, deps), nil
	case providers.AdapterGoogleGemini:
		return newGoogleGemini(def, deps), nil
	case providers.AdapterCloudflare:
		return newCloudflare(def, deps), nil
	case providers.AdapterCodex:
		f.once.Do(func() { f.prompt = &codexInstructions{} })
		return newCodex(def, deps, f.Codex, f.PersistCodexSecret, f.prompt), nil
	case providers.AdapterNous:
		isSignedIn := f.NousIsSignedIn
		if isSignedIn == nil {
			isSignedIn = providers.NousIsSignIn
		}
		return newNous(def, deps, f.Nous, isSignedIn, f.PersistNousSecret, f.NousPortalURL), nil
	case providers.AdapterLocalRuntime:
		return newLocalRuntime(def, deps, f.Local), nil
	}
	return nil, ErrNotChatAdapter
}

// CodexSignIn is the ChatGPT sign-in store: it renews the tokens shortly before
// they expire and knows the client version the backend expects. providers.OAuth
// satisfies it as it stands, so the wiring passes that service directly.
type CodexSignIn interface {
	// FreshCodexCredentials is the connection's valid credentials and the secret
	// to store for them; persist saves a rotated secret.
	FreshCodexCredentials(ctx context.Context, secret, accountID string, persist func(secret string) error) (providers.CodexCredentials, string, error)
	// CodexClientVersion is the client version the model listing asks for.
	CodexClientVersion(ctx context.Context) string
}

// NousSignIn is the Nous Portal sign-in store, in the shape providers.OAuth
// already has.
type NousSignIn interface {
	// FreshNousCredentials is the connection's valid credentials and the secret
	// to store for them; persist saves a rotated secret.
	FreshNousCredentials(ctx context.Context, secret, accountID string, persist func(secret string) error) (providers.NousCredentials, string, error)
}

// LocalModels is the local model service: which models run on this server,
// which of them the person may use, and how to reach the one serving a request.
type LocalModels interface {
	// Allowed reports whether the person may use this server's local models.
	Allowed(ctx context.Context, userID string) bool
	// Served are the models running right now, with what they can do.
	Served() []LocalModel
	// Running reports whether modelID is running and how to reach it.
	Running(modelID string) (LocalInstance, bool)
}

// LocalModel is one local model the server runs.
type LocalModel struct {
	ID            string
	Name          string
	Kind          string
	ContextWindow int
	Tools         bool
	Vision        bool
	Reasoning     bool
	// Translate marks a speech recogniser that can translate into English
	// (Whisper can, Moonshine cannot).
	Translate bool
	// Voices are the speakers a speech model offers.
	Voices []providers.Voice
}

// LocalInstance is how one running model answers.
type LocalInstance struct {
	// Endpoint is the llama.cpp server of a model that speaks the OpenAI API.
	Endpoint *LocalEndpoint
	// Speech synthesizes audio in a worker process; nil when the model is not a
	// speech model.
	Speech func(ctx context.Context, in LocalSpeechInput) (LocalSpeechOutput, error)
	// Transcribe reads audio in a worker process; nil when the model is not a
	// recogniser.
	Transcribe func(ctx context.Context, in LocalTranscribeInput) (LocalTranscribeOutput, error)
}

// LocalEndpoint is a running llama.cpp server.
type LocalEndpoint struct {
	BaseURL string
	// Token is the bearer the server was started with.
	Token string
}

// LocalSpeechInput is one local speech request.
type LocalSpeechInput struct {
	Text  string
	Voice string
	Speed *float64
	// Timeout bounds the worker call.
	Timeout time.Duration
}

// LocalSpeechOutput is synthesized audio: samples and their rate.
type LocalSpeechOutput struct {
	Samples []float32
	Rate    int
}

// LocalTranscribeInput is one local transcription request. The service decodes
// the audio (WAV, or anything else when ffmpeg is installed) before it runs the
// recogniser.
type LocalTranscribeInput struct {
	File      []byte
	Filename  string
	Mime      string
	Language  string
	Translate bool
	Timeout   time.Duration
}

// LocalTranscribeOutput is one local transcript.
type LocalTranscribeOutput struct {
	Text     string
	Language string
	Duration *float64
	Segments []providers.Segment
}
