package localmodels

// llama_server.go runs chat, safety and embedding models — one llama.cpp server
// per model, bound to 127.0.0.1 on a free port and locked with a random key, so
// only NeoRouter can reach it.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"time"
)

// LoadTimeoutMs is how long a model may take to answer after it started.
const LoadTimeoutMs = 15 * 60_000

// LoadPollMs is how often a loading model is asked whether it is ready.
const LoadPollMs = 1000

// gpuFailure matches the errors that mean the GPU build cannot work here, so
// the CPU build should take over.
var gpuFailure = regexp.MustCompile(`(?i)cuda|cublas|ggml_cuda|no kernel image|driver version is insufficient|metal.*(fail|error)`)

// ContextSize is how much conversation a model keeps in mind, from the
// machine's memory (more context needs more memory). Embedding models use their
// own limit.
func ContextSize(entry *Entry, machine Machine) int {
	if entry.Kind == KindEmbedding {
		return 8192
	}
	totalGB := float64(machine.Memory.TotalMB) / 1024
	switch {
	case totalGB >= 64:
		return 32768
	case totalGB >= 24:
		return 16384
	default:
		return 8192
	}
}

// StartError is a failed start: the message the dashboard shows and what the
// model printed before it gave up.
type StartError struct {
	Message string
	Output  string
	Code    string
}

func (e *StartError) Error() string { return e.Message }

// LlamaServerEngine runs GGUF models on llama.cpp's server.
type LlamaServerEngine struct {
	// Runtime installs and locates the llama.cpp build.
	Runtime *LlamaRuntime
	// Processes starts and ends the model processes.
	Processes *ProcessManager
	// HTTP talks to the model; nil uses http.DefaultClient with a 5 second
	// bound per call.
	HTTP *http.Client
}

// Support is llama.cpp's support for this machine.
func (e *LlamaServerEngine) Support(machine Machine) Support { return llamaCppSupport(machine) }

// Prepare installs the llama.cpp build the machine needs.
func (e *LlamaServerEngine) Prepare(ctx context.Context, machine Machine, opts PrepareOptions) error {
	_, err := e.Runtime.Ensure(ctx, machine, opts)
	return err
}

// llamaHealth is one answer from the model's HTTP API.
type llamaHealth struct {
	status int
	data   map[string]any
}

// llamaGet asks the model for one of its own endpoints.
func (e *LlamaServerEngine) llamaGet(ctx context.Context, url, token string, timeoutMs int) llamaHealth {
	requestCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMs)*time.Millisecond)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, url, nil)
	if err != nil {
		return llamaHealth{}
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	client := e.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(request)
	if err != nil {
		return llamaHealth{}
	}
	defer response.Body.Close()
	var data map[string]any
	_ = json.NewDecoder(response.Body).Decode(&data)
	return llamaHealth{status: response.StatusCode, data: data}
}

// waitUntilLoaded polls the model's health endpoint until it answers.
func (e *LlamaServerEngine) waitUntilLoaded(ctx context.Context, managed Process, origin string) error {
	deadline := time.Now().Add(LoadTimeoutMs * time.Millisecond)
	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return err
		}
		select {
		case <-managed.Done():
			return fmt.Errorf("stopped while loading")
		default:
		}
		if e.llamaGet(ctx, origin+"/health", "", 5000).status == http.StatusOK {
			return nil
		}
		if err := sleepCtx(ctx, LoadPollMs*time.Millisecond); err != nil {
			return err
		}
	}
	return fmt.Errorf("took too long to load")
}

// Start runs the model and returns once it answers.
func (e *LlamaServerEngine) Start(ctx context.Context, in StartInput) (*Instance, error) {
	runtime, err := e.Runtime.Ensure(ctx, in.Machine, PrepareOptions{})
	if err != nil {
		return nil, &StartError{Message: err.Error(), Code: codeOf(err)}
	}
	port, err := FreePort()
	if err != nil {
		return nil, &StartError{Message: err.Error()}
	}
	tokenBytes := make([]byte, 24)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, &StartError{Message: err.Error()}
	}
	token := hex.EncodeToString(tokenBytes)
	contextWindow := ContextSize(in.Entry, in.Machine)
	args := []string{
		"-m", in.Files.Model,
		"--host", "127.0.0.1", "--port", strconv.Itoa(port),
		"--api-key", token,
		"--alias", in.Entry.ID,
		"--ctx-size", strconv.Itoa(contextWindow),
		"--no-webui",
	}
	if in.Files.Projector != "" {
		args = append(args, "--mmproj", in.Files.Projector)
	}
	if in.Entry.Kind == KindEmbedding {
		args = append(args, "--embeddings")
	}
	managed, err := e.Processes.Start(StartSpec{
		Path:   runtime.Binary,
		Args:   args,
		Dir:    runtime.Dir,
		Env:    runtime.Env,
		Marker: runtime.Binary,
	})
	if err != nil {
		return nil, &StartError{Message: err.Error()}
	}
	origin := "http://127.0.0.1:" + strconv.Itoa(port)
	if err := e.waitUntilLoaded(ctx, managed, origin); err != nil {
		output := managed.Output()
		managed.Stop()
		// A GPU build that fails on this machine is put aside; the next start
		// uses the CPU build.
		message := err.Error()
		if runtime.GPU && runtime.ID != "macos-arm64" && gpuFailure.MatchString(output) {
			e.Runtime.MarkBroken(runtime.ID)
		}
		return nil, &StartError{Message: "The model " + message + ".", Output: output}
	}
	props := e.llamaGet(ctx, origin+"/props", token, 5000)
	managed.Ready()
	window := contextWindow
	if value := propsContextWindow(props.data); value != nil {
		window = *value
	}
	health := func(ctx context.Context) bool {
		return e.llamaGet(ctx, origin+"/health", "", 5000).status == http.StatusOK
	}
	return &Instance{
		Proc:          managed,
		Endpoint:      &Endpoint{BaseURL: origin + "/v1", Token: token},
		ContextWindow: &window,
		GPU:           runtime.GPU,
		Health:        health,
	}, nil
}

// propsContextWindow reads props.data.default_generation_settings.n_ctx.
func propsContextWindow(data map[string]any) *int {
	if data == nil {
		return nil
	}
	props, ok := data["data"].(map[string]any)
	if !ok {
		return nil
	}
	settings, ok := props["default_generation_settings"].(map[string]any)
	if !ok {
		return nil
	}
	ctxSize, ok := settings["n_ctx"].(float64)
	if !ok || ctxSize <= 0 {
		return nil
	}
	value := int(ctxSize)
	return &value
}
