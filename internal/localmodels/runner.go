package localmodels

// runner.go keeps one installed model running while the admin wants it on —
// starts it, watches it, restarts it after a crash or when it stops answering
// (waiting a little longer each time), and gives up only after repeated
// failures, at which point the service may repair it.

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// Runner states.
const (
	StateStopped    = "stopped"
	StateStarting   = "starting"
	StateRunning    = "running"
	StateRestarting = "restarting"
	StateFailed     = "failed"
)

// DefaultBackoffMs is how long the runner waits before each restart.
var DefaultBackoffMs = []int{2000, 5000, 15000, 30000, 60000}

// StableMs is running this long without trouble, which forgets earlier
// failures.
const StableMs = 10 * 60_000

// HealthEveryMs is how often a running model is asked whether it still answers.
const HealthEveryMs = 30_000

// UnhealthyLimit is how many failed health checks restart a model.
const UnhealthyLimit = 3

// Problem is why a model is not running, as the dashboard shows it.
type Problem struct {
	Code    string  `json:"code"`
	Message string  `json:"message"`
	Output  *string `json:"output"`
}

// RunnerContext is what a start needs, resolved fresh each time.
type RunnerContext struct {
	Entry   *Entry
	Files   ModelFiles
	Machine Machine
}

// RunnerOptions tune one runner. The hooks are the service's.
type RunnerOptions struct {
	// Engine resolves the engine an entry names; nil drops every start.
	Engine func(engine string) Engine
	// OnChange runs after a state or problem change (access.scheduleSync).
	OnChange func(*Runner)
	// OnGaveUp runs when the runner stops trying (the service repairs).
	OnGaveUp func(*Runner)
	// Log records state changes that matter; nil discards them.
	Log *slog.Logger
	// BackoffMs overrides DefaultBackoffMs (tests use a short one).
	BackoffMs []int
	// HealthEveryMs overrides HealthEveryMs.
	HealthEveryMs int
	// StableMs overrides StableMs.
	StableMs int
	// UnhealthyLimit overrides UnhealthyLimit.
	UnhealthyLimit int
}

// Runner keeps one model running.
type Runner struct {
	// ID is the catalog id of the model this runner keeps running.
	ID string
	// Context is called for each start.
	Context func(ctx context.Context) (RunnerContext, error)

	opts RunnerOptions

	mu        sync.Mutex
	state     string
	problem   *Problem
	instance  *Instance
	failures  int
	wanted    bool
	startedAt time.Time
	timer     *time.Timer
	health    chan struct{}
	unhealthy int
	cancel    context.CancelFunc
	change    chan struct{}
}

// NewRunner returns a stopped runner for one model.
func NewRunner(id string, butler func(ctx context.Context) (RunnerContext, error), opts RunnerOptions) *Runner {
	return &Runner{ID: id, Context: butler, opts: opts, state: StateStopped, change: make(chan struct{})}
}

func (r *Runner) log() *slog.Logger {
	if r.opts.Log != nil {
		return r.opts.Log
	}
	return slog.Default()
}

func (r *Runner) backoff() []int {
	if len(r.opts.BackoffMs) > 0 {
		return r.opts.BackoffMs
	}
	return DefaultBackoffMs
}

func (r *Runner) healthEveryMs() int {
	if r.opts.HealthEveryMs > 0 {
		return r.opts.HealthEveryMs
	}
	return HealthEveryMs
}

func (r *Runner) stableMs() int {
	if r.opts.StableMs > 0 {
		return r.opts.StableMs
	}
	return StableMs
}

func (r *Runner) unhealthyLimit() int {
	if r.opts.UnhealthyLimit > 0 {
		return r.opts.UnhealthyLimit
	}
	return UnhealthyLimit
}

// State is stopped, starting, running, restarting or failed.
func (r *Runner) State() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.state
}

// Problem is why the model is not running, nil while it runs.
func (r *Runner) Problem() *Problem {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.problem
}

// Instance is the running model, nil when it is not running.
func (r *Runner) Instance() *Instance {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.instance
}

// Wanted reports whether the admin wants the model on.
func (r *Runner) Wanted() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.wanted
}

// setState records a state and problem, notifying listeners when either
// changed. It must be called with the lock held.
func (r *Runner) setStateLocked(state string, problem *Problem) {
	changed := state != r.state || !sameProblem(problem, r.problem)
	r.state = state
	r.problem = problem
	if !changed {
		return
	}
	previous := r.change
	r.change = make(chan struct{})
	close(previous)
	go func() {
		if r.opts.OnChange != nil {
			r.opts.OnChange(r)
		}
	}()
}

// setState records a state and problem outside the lock.
func (r *Runner) setState(state string, problem *Problem) {
	r.mu.Lock()
	r.setStateLocked(state, problem)
	r.mu.Unlock()
}

// sameProblem compares two problems by value.
func sameProblem(left, right *Problem) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	if left.Code != right.Code || left.Message != right.Message {
		return false
	}
	if left.Output == nil || right.Output == nil {
		return left.Output == nil && right.Output == nil
	}
	return *left.Output == *right.Output
}

// Start asks for the model to run: it is started now, or again after the
// current attempt.
func (r *Runner) Start() {
	r.mu.Lock()
	r.wanted = true
	r.failures = 0
	if r.state == StateStarting || r.state == StateRunning {
		r.mu.Unlock()
		return
	}
	if r.timer != nil {
		r.timer.Stop()
		r.timer = nil
	}
	r.mu.Unlock()
	go r.launch()
}

// launch starts the model once.
func (r *Runner) launch() {
	r.mu.Lock()
	if !r.wanted {
		r.mu.Unlock()
		return
	}
	state := StateStarting
	var problem *Problem
	if r.failures > 0 {
		state = StateRestarting
		problem = r.problem
	}
	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	r.setStateLocked(state, problem)
	r.mu.Unlock()

	instance, err := r.startInstance(ctx)
	if err != nil {
		cancel()
		r.mu.Lock()
		wanted := r.wanted
		r.mu.Unlock()
		if !wanted {
			return
		}
		r.log().Warn("Local model failed to start", "model", r.ID, "error", err)
		r.failed(startProblem(err))
		return
	}
	r.mu.Lock()
	wanted := r.wanted
	if !wanted {
		r.mu.Unlock()
		instance.Proc.Stop()
		return
	}
	r.instance = instance
	r.startedAt = time.Now()
	r.mu.Unlock()

	go func() {
		<-instance.Proc.Done()
		r.exited(instance)
	}()
	r.watch(instance)
	r.setState(StateRunning, nil)
	r.log().Info("Local model running", "model", r.ID, "gpu", instance.GPU)
}

// startInstance resolves the entry and starts the engine.
func (r *Runner) startInstance(ctx context.Context) (*Instance, error) {
	context, err := r.Context(ctx)
	if err != nil {
		return nil, err
	}
	if r.opts.Engine == nil {
		return nil, fmt.Errorf("no engine is wired")
	}
	engine := r.opts.Engine(context.Entry.Engine)
	if engine == nil {
		return nil, fmt.Errorf("Unknown local engine %s.", context.Entry.Engine)
	}
	return engine.Start(ctx, StartInput{Entry: context.Entry, Files: context.Files, Machine: context.Machine})
}

// startProblem turns a start failure into the problem the dashboard shows.
func startProblem(err error) *Problem {
	problem := &Problem{Code: "start_failed", Message: err.Error()}
	if code := codeOf(err); code != "" {
		problem.Code = code
	}
	var startErr *StartError
	if asStartError(err, &startErr) {
		if startErr.Code != "" {
			problem.Code = startErr.Code
		}
		if startErr.Output != "" {
			output := startErr.Output
			problem.Output = &output
		}
	}
	return problem
}

// asStartError finds a *StartError in err.
func asStartError(err error, target **StartError) bool {
	for err != nil {
		if startErr, ok := err.(*StartError); ok {
			*target = startErr
			return true
		}
		unwrapper, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = unwrapper.Unwrap()
	}
	return false
}

// watch polls the running model's health, restarting it when it stops
// answering.
func (r *Runner) watch(instance *Instance) {
	r.mu.Lock()
	r.stopHealthLocked()
	stop := make(chan struct{})
	r.health = stop
	r.unhealthy = 0
	r.mu.Unlock()

	every := time.Duration(r.healthEveryMs()) * time.Millisecond
	go func() {
		ticker := time.NewTicker(every)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				r.mu.Lock()
				if r.instance != instance {
					r.mu.Unlock()
					return
				}
				if time.Since(r.startedAt) > time.Duration(r.stableMs())*time.Millisecond {
					r.failures = 0
				}
				r.mu.Unlock()

				healthy := true
				if instance.Health != nil {
					healthy = instance.Health(context.Background())
				}
				if healthy {
					r.mu.Lock()
					r.unhealthy = 0
					r.mu.Unlock()
					continue
				}
				r.mu.Lock()
				r.unhealthy++
				reached := r.unhealthy >= r.unhealthyLimit()
				r.mu.Unlock()
				if !reached {
					continue
				}
				r.log().Warn("Local model stopped answering; restarting", "model", r.ID)
				r.mu.Lock()
				// Recorded without a state change: the exit below reports it.
				r.problem = &Problem{Code: "not_answering", Message: "The model stopped answering."}
				r.mu.Unlock()
				instance.Proc.Stop()
			}
		}
	}()
}

// stopHealthLocked ends the health poll (caller holds the lock).
func (r *Runner) stopHealthLocked() {
	if r.health != nil {
		close(r.health)
		r.health = nil
	}
}

// exited handles a model process that ended.
func (r *Runner) exited(instance *Instance) {
	r.mu.Lock()
	if r.instance != instance {
		r.mu.Unlock()
		return
	}
	r.stopHealthLocked()
	r.instance = nil
	wanted := r.wanted
	problem := r.problem
	r.mu.Unlock()
	if !wanted {
		r.setState(StateStopped, nil)
		return
	}
	exit := instance.Proc.Exit()
	r.log().Warn("Local model exited unexpectedly", "model", r.ID, "code", exit.Code, "signal", exit.Signal)
	if problem != nil && problem.Code == "not_answering" {
		r.failed(problem)
		return
	}
	r.failed(&Problem{Code: "crashed", Message: crashMessage(exit)})
}

// crashMessage is `The model stopped unexpectedly (SIGKILL).` and its variants.
func crashMessage(exit Exit) string {
	switch {
	case exit.Signal != "":
		return fmt.Sprintf("The model stopped unexpectedly (%s).", exit.Signal)
	case exit.Code != nil:
		return fmt.Sprintf("The model stopped unexpectedly (exit %d).", *exit.Code)
	default:
		return "The model stopped unexpectedly."
	}
}

// failed records a failure: restart after a wait, or give up.
func (r *Runner) failed(problem *Problem) {
	backoff := r.backoff()
	r.mu.Lock()
	r.failures++
	if r.failures >= len(backoff) {
		r.wanted = false
		r.setStateLocked(StateFailed, problem)
		r.mu.Unlock()
		if r.opts.OnGaveUp != nil {
			r.opts.OnGaveUp(r)
		}
		return
	}
	r.setStateLocked(StateRestarting, problem)
	delay := time.Duration(backoff[r.failures-1]) * time.Millisecond
	r.timer = time.AfterFunc(delay, r.launch)
	r.mu.Unlock()
}

// Stop ends the model and stops trying.
func (r *Runner) Stop() {
	r.mu.Lock()
	r.wanted = false
	if r.timer != nil {
		r.timer.Stop()
		r.timer = nil
	}
	r.stopHealthLocked()
	if r.cancel != nil {
		r.cancel()
		r.cancel = nil
	}
	instance := r.instance
	r.instance = nil
	r.mu.Unlock()
	if instance != nil {
		instance.Proc.Stop()
	}
	r.setState(StateStopped, nil)
}

// Refuse is a start refused before it began, e.g. for lack of memory.
func (r *Runner) Refuse(problem *Problem) {
	r.mu.Lock()
	r.wanted = false
	r.setStateLocked(StateFailed, problem)
	r.mu.Unlock()
}

// WaitSettled waits until the model finished starting (it is running, or the
// attempt failed), so a boot starts models one after another.
func (r *Runner) WaitSettled(ctx context.Context) {
	for {
		r.mu.Lock()
		state := r.state
		change := r.change
		r.mu.Unlock()
		if state != StateStarting {
			return
		}
		select {
		case <-change:
		case <-ctx.Done():
			return
		}
	}
}
