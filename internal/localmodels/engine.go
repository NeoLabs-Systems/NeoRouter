package localmodels

// engine.go defines the engines local models run on. Each one says whether it
// can run here, installs what it needs, and starts one model.

import "context"

// Support is whether an engine runs on this machine, and why not.
type Support struct {
	OK     bool    `json:"ok"`
	Reason *string `json:"reason"`
}

// supported and unsupported build the two answers.
func supported() Support { return Support{OK: true} }

func unsupported(reason string) Support {
	return Support{OK: false, Reason: &reason}
}

// ModelFiles are the absolute paths of the files an engine loads.
type ModelFiles struct {
	// Model is the weights: the GGUF for llama.cpp, the model directory for the
	// transformers engine.
	Model string
	// Projector is llama.cpp's vision projector, "" when the model has none.
	Projector string
}

// PrepareOptions tune an engine's install step.
type PrepareOptions struct {
	// OnBytes reports download progress as a byte delta.
	OnBytes func(delta int64)
}

// StartInput is everything one start needs.
type StartInput struct {
	Entry   *Entry
	Files   ModelFiles
	Machine Machine
}

// Endpoint is a running llama.cpp server: where it listens and its API key.
type Endpoint struct {
	BaseURL string `json:"baseUrl"`
	Token   string `json:"token"`
}

// InvokeFunc speaks to a model that runs in-process: one request, one answer.
type InvokeFunc func(ctx context.Context, method string, payload any) (any, error)

// Instance is one running model.
type Instance struct {
	// Proc is the process the model runs in, watched and stopped by the runner.
	Proc Process
	// Endpoint is where llama.cpp answers, nil for the transformers engine.
	Endpoint *Endpoint
	// Invoke is how the transformers engine answers speech requests.
	Invoke InvokeFunc
	// ContextWindow is how much conversation the model keeps in mind (nil for
	// speech models).
	ContextWindow *int
	// GPU reports whether the model runs on the graphics card.
	GPU bool
	// Health is one probe: true while the model answers.
	Health func(ctx context.Context) bool
}

// Process is one model process: what the runner watches and stops.
type Process interface {
	// PID is the operating system process id, 0 when the model runs inside
	// NeoRouter itself.
	PID() int
	// Done is closed once the process has exited.
	Done() <-chan struct{}
	// Exit reports how it ended; meaningful once Done is closed.
	Exit() Exit
	// Stop ends it: SIGTERM, then SIGKILL after the grace period.
	Stop()
	// Output is what it printed before it was ready, for a failed start.
	Output() string
	// Ready marks the model as answered: from then on nothing it prints is kept.
	Ready()
}

// Exit is how a model process ended. Code is nil when a signal ended it.
type Exit struct {
	Code   *int
	Signal string
}

// Engine runs one kind of local model.
type Engine interface {
	// Support reports whether this engine can run models here.
	Support(machine Machine) Support
	// Prepare installs what the engine needs before the first model starts.
	Prepare(ctx context.Context, machine Machine, opts PrepareOptions) error
	// Start runs one model and returns once it answers.
	Start(ctx context.Context, in StartInput) (*Instance, error)
}

// DefaultSupport answers for the built-in engines, which is what the fit and
// the starter set use when nothing else is wired.
func DefaultSupport(engine string, machine Machine) Support {
	switch engine {
	case EngineLlamaCpp:
		return llamaCppSupport(machine)
	case EngineTransformers:
		return supported()
	default:
		return unsupported("unknown_engine")
	}
}

// engines is the registry the service starts models with.
type engines struct {
	llamaCpp     Engine
	transformers Engine
}

func (e engines) engineFor(id string) Engine {
	switch id {
	case EngineLlamaCpp:
		return e.llamaCpp
	case EngineTransformers:
		return e.transformers
	default:
		return nil
	}
}
