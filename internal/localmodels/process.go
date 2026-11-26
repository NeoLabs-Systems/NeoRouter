package localmodels

// process.go manages a model process NeoRouter started — its output is kept
// only until it is ready (enough to explain a failed start, never prompts), it
// is ended with NeoRouter, and its pid is written down so that the next start
// can end it if NeoRouter itself crashed. Process supervision itself comes from
// neolabs-go/proc, which starts children in their own group and reports how
// they ended.

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/proc"
)

// TailLines is how much of a starting model's output is kept.
const TailLines = 40

// StopGraceMs is how long a model may take to exit on SIGTERM before it is
// killed.
const StopGraceMs = 10_000

// StartSpec describes one model process.
type StartSpec struct {
	Path string
	Args []string
	Dir  string
	Env  []string
	// Marker is part of the command line, so the next start can tell the
	// process it recorded is really ours.
	Marker string
}

// ProcessManager starts model processes and records their pids.
type ProcessManager struct {
	Paths Paths
	// CommandLine reads the command line of a pid; nil reads /proc or ps.
	CommandLine func(pid int) string
	// KillPID ends a pid this process did not start; nil uses a signal.
	KillPID func(pid int)

	mu   sync.Mutex
	live map[*ManagedProcess]struct{}
}

// NewProcessManager returns the manager for one install's paths.
func NewProcessManager(paths Paths) *ProcessManager {
	return &ProcessManager{Paths: paths, live: map[*ManagedProcess]struct{}{}}
}

// ManagedProcess is one model process NeoRouter started.
type ManagedProcess struct {
	process *proc.Process
	manager *ProcessManager
	marker  string

	mu        sync.Mutex
	tail      []string
	capturing bool

	exited   chan struct{}
	exitOnce sync.Once
	exit     Exit
}

// Start launches a model process and records its pid.
func (m *ProcessManager) Start(spec StartSpec) (*ManagedProcess, error) {
	managed := &ManagedProcess{manager: m, marker: spec.Marker, capturing: true, exited: make(chan struct{})}
	process, err := proc.Start(proc.Spec{
		Path:   spec.Path,
		Args:   spec.Args,
		Dir:    spec.Dir,
		Env:    spec.Env,
		Stdout: (*tailWriter)(managed),
		Stderr: (*tailWriter)(managed),
	})
	if err != nil {
		return nil, err
	}
	managed.process = process
	if pid := process.PID(); pid > 0 {
		m.remember(pid, spec.Marker)
	}
	m.mu.Lock()
	m.live[managed] = struct{}{}
	m.mu.Unlock()
	go func() {
		exit := process.Wait()
		code := exit.Code
		managed.finish(&code, exit.Signal)
	}()
	return managed, nil
}

// tailWriter appends a process's output to the managed process's tail.
type tailWriter ManagedProcess

func (w *tailWriter) Write(chunk []byte) (int, error) {
	managed := (*ManagedProcess)(w)
	managed.mu.Lock()
	defer managed.mu.Unlock()
	if !managed.capturing {
		return len(chunk), nil
	}
	for _, line := range strings.Split(string(chunk), "\n") {
		if line == "" {
			continue
		}
		managed.tail = append(managed.tail, line)
	}
	if len(managed.tail) > TailLines {
		managed.tail = managed.tail[len(managed.tail)-TailLines:]
	}
	return len(chunk), nil
}

// finish records how the process ended, once.
func (p *ManagedProcess) finish(code *int, signal string) {
	p.exitOnce.Do(func() {
		p.exit = Exit{Code: code, Signal: signal}
		if p.process != nil {
			if pid := p.process.PID(); pid > 0 {
				p.manager.forget(pid)
			}
		}
		p.manager.mu.Lock()
		delete(p.manager.live, p)
		p.manager.mu.Unlock()
		close(p.exited)
	})
}

// PID is the operating system process id.
func (p *ManagedProcess) PID() int {
	if p.process == nil {
		return 0
	}
	return p.process.PID()
}

// Done is closed once the process has exited; Exit is set by then.
func (p *ManagedProcess) Done() <-chan struct{} { return p.exited }

// Exit reports how the process ended; meaningful once Done is closed.
func (p *ManagedProcess) Exit() Exit { return p.exit }

// Ready marks the model as answered: from then on nothing it prints is kept.
func (p *ManagedProcess) Ready() {
	p.mu.Lock()
	p.capturing = false
	p.tail = nil
	p.mu.Unlock()
}

// Output is what the process printed before it was ready.
func (p *ManagedProcess) Output() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return strings.Join(p.tail, "\n")
}

// Stop ends the process: SIGTERM, then SIGKILL after the grace period.
func (p *ManagedProcess) Stop() {
	if p.process == nil || !p.process.Alive() {
		return
	}
	_ = p.process.Signal(syscall.SIGTERM)
	timer := time.AfterFunc(StopGraceMs*time.Millisecond, func() { p.process.Kill() })
	defer timer.Stop()
	<-p.process.Done()
}

// remember writes a pid into processes.json.
func (m *ProcessManager) remember(pid int, marker string) {
	pids := m.readPids()
	pids[strconv.Itoa(pid)] = marker
	m.writePids(pids)
}

// forget drops a pid from processes.json.
func (m *ProcessManager) forget(pid int) {
	pids := m.readPids()
	delete(pids, strconv.Itoa(pid))
	m.writePids(pids)
}

func (m *ProcessManager) readPids() map[string]string {
	pids := map[string]string{}
	data, err := os.ReadFile(m.Paths.ProcessesFile)
	if err != nil {
		return pids
	}
	if err := json.Unmarshal(data, &pids); err != nil {
		return map[string]string{}
	}
	return pids
}

// writePids saves the pids; best effort, it is only used to clean up after a
// crash.
func (m *ProcessManager) writePids(pids map[string]string) {
	if err := m.Paths.EnsureDirs(); err != nil {
		return
	}
	data, err := json.Marshal(pids)
	if err != nil {
		return
	}
	_ = os.WriteFile(m.Paths.ProcessesFile, data, 0o600)
}

// EndOrphans ends model processes a crashed NeoRouter left behind.
func (m *ProcessManager) EndOrphans() {
	for pid, marker := range m.readPids() {
		if !strings.Contains(m.commandOf(pid), marker) {
			continue
		}
		number, err := strconv.Atoi(pid)
		if err != nil {
			continue
		}
		if m.KillPID != nil {
			m.KillPID(number)
		} else {
			killPID(number)
		}
	}
	m.writePids(map[string]string{})
}

// KillAll ends every process still running, for shutdown.
func (m *ProcessManager) KillAll() {
	m.mu.Lock()
	live := make([]*ManagedProcess, 0, len(m.live))
	for managed := range m.live {
		live = append(live, managed)
	}
	m.mu.Unlock()
	for _, managed := range live {
		if managed.process != nil {
			managed.process.Kill()
		}
	}
}

// commandOf is the command line of a pid, to be sure it is still ours before
// ending it.
func (m *ProcessManager) commandOf(pid string) string {
	if m.CommandLine != nil {
		if number, err := strconv.Atoi(pid); err == nil {
			return m.CommandLine(number)
		}
		return ""
	}
	if data, err := os.ReadFile(filepath.Join("/proc", pid, "cmdline")); err == nil {
		return strings.ReplaceAll(string(data), "\x00", " ")
	}
	out, ok := runCommand(context.Background(), "ps", "-p", pid, "-o", "command=")
	if !ok {
		return ""
	}
	return out
}

// FreePort asks the operating system for a free port on the loopback address.
func FreePort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}
