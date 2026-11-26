package localmodels

// memory.go measures how much memory this server really uses, every 30 seconds
// and averaged over the last hour — by everything else (the system, NeoRouter,
// other programs) and by each local model while it runs. The fit check uses
// these averages instead of fixed guesses once they are known.

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"time"
)

// SampleEveryMs is the sampling interval.
const SampleEveryMs = 30_000

// MemoryWindow is how many samples are averaged: one hour.
const MemoryWindow = 120

// RunningModel identifies a model process the sampler measures.
type RunningModel struct {
	ID  string
	PID int
}

// usage is one system reading.
type usage struct {
	usedMB    float64
	container bool
}

// processUsage is one model process reading: what it holds, and the part the
// system figure above counts.
type processUsage struct {
	totalMB   float64
	countedMB float64
}

// Sampler measures this machine's memory use over time. Its Host and clock are
// swapped in tests.
type Sampler struct {
	Host *Host
	// Every is the sampling interval; 0 means SampleEveryMs.
	Every int

	mu       sync.Mutex
	others   []int64
	models   map[string][]int64
	running  func() []RunningModel
	stop     chan struct{}
	started  bool
	sampling bool
	fixed    bool
}

// NewSampler returns a sampler reading this machine.
func NewSampler() *Sampler {
	return &Sampler{Host: DefaultHost(), models: map[string][]int64{}}
}

func (s *Sampler) host() *Host {
	if s.Host == nil {
		s.Host = DefaultHost()
	}
	return s.Host
}

// SetRunningModels tells the sampler which models are running now; the service
// supplies this (memory.start(models)).
func (s *Sampler) SetRunningModels(models func() []RunningModel) {
	s.mu.Lock()
	s.running = models
	s.mu.Unlock()
}

// Start begins measuring in the background (memory.start).
func (s *Sampler) Start(ctx context.Context) {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return
	}
	s.started = true
	stop := make(chan struct{})
	s.stop = stop
	every := s.Every
	if every <= 0 {
		every = SampleEveryMs
	}
	s.mu.Unlock()

	_ = s.Refresh(ctx)
	go func() {
		timer := newTicker(every)
		defer timer.stop()
		for {
			select {
			case <-stop:
				return
			case <-timer.c:
				_ = s.Refresh(context.Background())
			}
		}
	}()
}

// Stop ends the background measuring (memory.stop).
func (s *Sampler) Stop() {
	s.mu.Lock()
	if s.started && s.stop != nil {
		close(s.stop)
		s.stop = nil
	}
	s.started = false
	s.mu.Unlock()
}

// Refresh takes a sample now (one at a time), e.g. before deciding whether a
// model fits.
func (s *Sampler) Refresh(ctx context.Context) error {
	s.mu.Lock()
	if s.fixed {
		s.mu.Unlock()
		return nil
	}
	if s.sampling {
		s.mu.Unlock()
		return nil
	}
	s.sampling = true
	s.mu.Unlock()

	err := s.sample(ctx)

	s.mu.Lock()
	s.sampling = false
	s.mu.Unlock()
	return err
}

func (s *Sampler) sample(ctx context.Context) error {
	host := s.host()
	system := host.systemUsedMB(ctx)
	s.mu.Lock()
	running := []RunningModel{}
	if s.running != nil {
		running = s.running()
	}
	s.mu.Unlock()

	var modelsMB float64
	for _, model := range running {
		if model.PID <= 0 {
			continue
		}
		measured, ok := host.processMB(ctx, model.PID, system.container)
		if !ok {
			continue
		}
		modelsMB += measured.countedMB
		s.mu.Lock()
		s.pushModel(model.ID, measured.totalMB)
		s.mu.Unlock()
	}
	others := system.usedMB - modelsMB
	if others < 0 {
		others = 0
	}
	s.mu.Lock()
	s.others = pushSample(s.others, others)
	s.mu.Unlock()
	return nil
}

// pushModel records one model sample (caller holds the lock).
func (s *Sampler) pushModel(id string, value float64) {
	s.models[id] = pushSample(s.models[id], value)
}

// pushSample appends a rounded sample, keeping the last MemoryWindow.
func pushSample(list []int64, value float64) []int64 {
	list = append(list, int64(roundHalfUp(value)))
	if len(list) > MemoryWindow {
		list = list[len(list)-MemoryWindow:]
	}
	return list
}

// OthersMB is the average memory used by everything except local models, nil
// before the first sample.
func (s *Sampler) OthersMB() *int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return average(s.others)
}

// ModelMB is the average memory a model used while running, nil if it has not
// run yet.
func (s *Sampler) ModelMB(id string) *int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return average(s.models[id])
}

// Forget drops a model's samples, e.g. when it is removed.
func (s *Sampler) Forget(id string) {
	s.mu.Lock()
	delete(s.models, id)
	s.mu.Unlock()
}

// average is the rounded mean of the samples, nil for none.
func average(list []int64) *int64 {
	if len(list) == 0 {
		return nil
	}
	var sum int64
	for _, value := range list {
		sum += value
	}
	value := int64(roundHalfUp(float64(sum) / float64(len(list))))
	return &value
}

// SamplerFigures are fixed figures instead of measurements (tests).
type SamplerFigures struct {
	Others *int64
	Models map[string]int64
}

// SetForTests installs fixed figures; nil measures this machine again.
func (s *Sampler) SetForTests(figures *SamplerFigures) {
	s.Stop()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fixed = figures != nil
	if figures == nil {
		s.others = nil
		s.models = map[string][]int64{}
		return
	}
	s.others = nil
	if figures.Others != nil {
		s.others = []int64{*figures.Others}
	}
	s.models = map[string][]int64{}
	for id, mb := range figures.Models {
		s.models[id] = []int64{mb}
	}
}

// systemUsedMB is how much memory this server currently uses, counted the way
// the dashboard will: the container's own use where there is one.
func (h *Host) systemUsedMB(ctx context.Context) usage {
	if h.Platform == "linux" {
		if container, ok := h.containerUsedMB(); ok {
			return container
		}
		if text, ok := h.readFile("/proc/meminfo"); ok {
			total := intField(text, "MemTotal")
			available := intField(text, "MemAvailable")
			if total != nil && available != nil {
				return usage{usedMB: float64(*total-*available) / 1024}
			}
		}
	}
	if h.Platform == "darwin" {
		if mac, ok := h.macUsedMB(ctx); ok {
			return mac
		}
	}
	total := uint64(0)
	if h.TotalMem != nil {
		total = h.TotalMem(ctx)
	}
	free := h.freeBytes(ctx)
	if total <= free {
		return usage{}
	}
	return usage{usedMB: float64(total-free) / MB}
}

// containerUsedMB is the container's own use without file cache it can drop,
// cgroup v2 then v1.
func (h *Host) containerUsedMB() (usage, bool) {
	if h.TotalMem == nil {
		return usage{}, false
	}
	physical := h.TotalMem(context.Background())
	pairs := [][2]string{
		{"/sys/fs/cgroup/memory.current", "/sys/fs/cgroup/memory.stat"},
		{"/sys/fs/cgroup/memory/memory.usage_in_bytes", "/sys/fs/cgroup/memory/memory.stat"},
	}
	for _, pair := range pairs {
		text, ok := h.readFile(pair[0])
		if !ok {
			continue
		}
		current, err := strconv.ParseInt(strings.TrimSpace(text), 10, 64)
		if err != nil || current <= 0 {
			continue
		}
		if physical > 0 && uint64(current) >= physical {
			continue
		}
		stats, _ := h.readFile(pair[1])
		inactive := intField(stats, "inactive_file")
		if inactive == nil {
			inactive = intField(stats, "total_inactive_file")
		}
		kept := int64(0)
		if inactive != nil {
			kept = *inactive
		}
		return usage{usedMB: float64(current-kept) / MB, container: true}, true
	}
	return usage{}, false
}

// macUsedMB is macOS's memory in use the way Activity Monitor counts it (app
// memory, wired, compressed). Models on the graphics chip show up as wired.
func (h *Host) macUsedMB(ctx context.Context) (usage, bool) {
	text, ok := h.command(ctx, "vm_stat")
	if !ok {
		return usage{}, false
	}
	pageSize := int64(16384)
	if index := strings.Index(text, "page size of "); index >= 0 {
		rest := text[index+len("page size of "):]
		digits := ""
		for _, r := range rest {
			if r < '0' || r > '9' {
				break
			}
			digits += string(r)
		}
		if value, err := strconv.ParseInt(digits, 10, 64); err == nil && value > 0 {
			pageSize = value
		}
	}
	pages := func(label string) int64 {
		index := strings.Index(text, label+":")
		if index < 0 {
			return 0
		}
		rest := strings.TrimLeft(text[index+len(label)+1:], " \t")
		digits := ""
		for _, r := range rest {
			if r < '0' || r > '9' {
				break
			}
			digits += string(r)
		}
		value, err := strconv.ParseInt(digits, 10, 64)
		if err != nil {
			return 0
		}
		return value
	}
	used := pages("Anonymous pages") - pages("Pages purgeable") + pages("Pages wired down") + pages("Pages occupied by compressor")
	if used <= 0 {
		return usage{}, false
	}
	return usage{usedMB: float64(used*pageSize) / MB}, true
}

// freeBytes is os.freemem for platforms without /proc: free pages on macOS,
// zero elsewhere (which reports "everything in use" rather than a guess).
func (h *Host) freeBytes(ctx context.Context) uint64 {
	if h.Platform != "darwin" {
		return 0
	}
	out, ok := h.command(ctx, "vm_stat")
	if !ok {
		return 0
	}
	pageSize := uint64(16384)
	pages := uint64(0)
	for _, line := range strings.Split(out, "\n") {
		if !strings.HasPrefix(line, "Pages free:") {
			continue
		}
		rest := strings.TrimSpace(strings.TrimPrefix(line, "Pages free:"))
		rest = strings.TrimSuffix(rest, ".")
		if value, err := strconv.ParseUint(strings.TrimSpace(rest), 10, 64); err == nil {
			pages = value
		}
	}
	return pages * pageSize
}

// processMB is a model process's memory: {totalMb, countedMb}. countedMb is the
// part the system figure above includes; on a Linux host a model's weights sit
// in the file cache, which "available" memory already treats as free.
func (h *Host) processMB(ctx context.Context, pid int, container bool) (processUsage, bool) {
	if h.Platform == "linux" {
		text, ok := h.readFile("/proc/" + strconv.Itoa(pid) + "/status")
		if !ok {
			return processUsage{}, false
		}
		rss := intField(text, "VmRSS")
		if rss == nil {
			return processUsage{}, false
		}
		anon := intField(text, "RssAnon")
		counted := *rss
		if anon != nil && !container {
			counted = *anon
		}
		return processUsage{totalMB: float64(*rss) / 1024, countedMB: float64(counted) / 1024}, true
	}
	out, ok := h.command(ctx, "ps", "-o", "rss=", "-p", strconv.Itoa(pid))
	if !ok {
		return processUsage{}, false
	}
	rss, err := strconv.ParseInt(strings.TrimSpace(out), 10, 64)
	if err != nil || rss <= 0 {
		return processUsage{}, false
	}
	return processUsage{totalMB: float64(rss) / 1024, countedMB: float64(rss) / 1024}, true
}

// ticker is a small interval timer that can be stopped and waited for.
type ticker struct {
	c      chan struct{}
	stopCh chan struct{}
	once   sync.Once
}

func newTicker(everyMs int) *ticker {
	t := &ticker{c: make(chan struct{}, 1), stopCh: make(chan struct{})}
	go func() {
		timer := time.NewTimer(time.Duration(everyMs) * time.Millisecond)
		for {
			select {
			case <-t.stopCh:
				timer.Stop()
				return
			case <-timer.C:
				select {
				case t.c <- struct{}{}:
				default:
				}
				timer.Reset(time.Duration(everyMs) * time.Millisecond)
			}
		}
	}()
	return t
}

func (t *ticker) stop() {
	t.once.Do(func() { close(t.stopCh) })
}
