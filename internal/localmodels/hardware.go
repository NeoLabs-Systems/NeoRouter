package localmodels

// hardware.go reads what this machine offers local models — processor, memory
// (the container's limit where there is one), graphics and free disk space —
// fresh at most every 30 seconds. Nothing here changes the machine.

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// MB is the megabyte every figure in this package counts in.
const MB = 1024 * 1024

// HardwareCacheMs is how long a detection is reused.
const HardwareCacheMs = 30_000

// UnifiedGPUShare is the share of an Apple Silicon Mac's memory its graphics
// may use (macOS's default limit).
const UnifiedGPUShare = 0.7

// CPU is the processor a machine reports.
type CPU struct {
	Name  string `json:"name"`
	Cores int    `json:"cores"`
}

// Memory is a machine's memory: its total, whether a container limit applies,
// and how much everything else uses on average (OthersMB, filled in by the
// service, nil until the first sample).
type Memory struct {
	TotalMB  int64  `json:"totalMb"`
	Limited  bool   `json:"limited"`
	OthersMB *int64 `json:"othersMb,omitempty"`
}

// GPU is a graphics card models can run on.
type GPU struct {
	Type string `json:"type"`
	Name string `json:"name"`
	// MemoryMB is the memory the graphics may use for models.
	MemoryMB int64 `json:"memoryMb"`
	// Unified marks an Apple Silicon GPU, which shares the system memory.
	Unified bool `json:"unified"`
	// CUDA is the newest CUDA version the NVIDIA driver supports, "" otherwise.
	CUDA string `json:"cuda"`
}

// Disk is the free and total space of the models' volume, nil when unknown.
type Disk struct {
	FreeMB  *int64 `json:"freeMb"`
	TotalMB *int64 `json:"totalMb"`
}

// Machine is one detection, the argument every fit decision takes.
type Machine struct {
	Platform string `json:"platform"`
	Arch     string `json:"arch"`
	CPU      CPU    `json:"cpu"`
	Memory   Memory `json:"memory"`
	GPU      *GPU   `json:"gpu"`
	Disk     Disk   `json:"disk"`
}

// Host reads the operating system. Its fields are swapped in tests.
type Host struct {
	// Command runs a program and returns its stdout, reporting false when it
	// could not run or exited non-zero.
	Command func(ctx context.Context, name string, args ...string) (string, bool)
	// ReadFile returns a file's contents, reporting false when unreadable.
	ReadFile func(path string) (string, bool)
	// TotalMem is the machine's physical memory in bytes.
	TotalMem func(ctx context.Context) uint64
	// CPUInfo is the processor's name and core count.
	CPUInfo func(ctx context.Context) CPU
	// Platform and Arch are the platform and architecture names the page shows
	// (darwin, linux, win32; x64, arm64).
	Platform string
	Arch     string
	// Statfs reports free and total bytes of a directory.
	Statfs func(dir string) (free, total int64, ok bool)
	// Now is the clock, for the detection cache.
	Now func() time.Time
}

// DefaultHost reads this machine.
func DefaultHost() *Host {
	return &Host{
		Command:  runCommand,
		ReadFile: readFileOr,
		TotalMem: totalMemory,
		CPUInfo:  cpuInfo,
		Platform: nodePlatform(runtime.GOOS),
		Arch:     nodeArch(runtime.GOARCH),
		Statfs:   statfs,
		Now:      time.Now,
	}
}

func (h *Host) command(ctx context.Context, name string, args ...string) (string, bool) {
	if h.Command == nil {
		return "", false
	}
	return h.Command(ctx, name, args...)
}

func (h *Host) readFile(path string) (string, bool) {
	if h.ReadFile == nil {
		return "", false
	}
	return h.ReadFile(path)
}

func (h *Host) now() time.Time {
	if h.Now == nil {
		return time.Now()
	}
	return h.Now()
}

// runCommand runs a program with a 5 second bound, like execFile's timeout.
func runCommand(ctx context.Context, name string, args ...string) (string, bool) {
	runCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(runCtx, name, args...).Output()
	if err != nil {
		return "", false
	}
	return string(out), true
}

func readFileOr(path string) (string, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	return string(data), true
}

// totalMemory reads the machine's physical memory: /proc/meminfo on Linux,
// sysctl on macOS and elsewhere.
func totalMemory(ctx context.Context) uint64 {
	if text, ok := readFileOr("/proc/meminfo"); ok {
		if kb := intField(text, "MemTotal"); kb != nil {
			return uint64(*kb) * 1024
		}
	}
	if out, ok := runCommand(ctx, "sysctl", "-n", "hw.memsize"); ok {
		if value, err := strconv.ParseUint(strings.TrimSpace(out), 10, 64); err == nil {
			return value
		}
	}
	return 0
}

// cpuInfo is os.cpus(): the first processor's model name and the core count.
func cpuInfo(ctx context.Context) CPU {
	cores := runtime.NumCPU()
	name := ""
	if text, ok := readFileOr("/proc/cpuinfo"); ok {
		name = cpuModelFromProc(text)
	}
	if name == "" {
		if out, ok := runCommand(ctx, "sysctl", "-n", "machdep.cpu.brand_string"); ok {
			name = strings.TrimSpace(out)
		}
	}
	if name == "" {
		name = "Unknown processor"
	}
	name = strings.Join(strings.Fields(name), " ")
	return CPU{Name: name, Cores: cores}
}

// cpuModelFromProc pulls the first "model name" out of /proc/cpuinfo.
func cpuModelFromProc(text string) string {
	for _, line := range strings.Split(text, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok || strings.TrimSpace(key) != "model name" {
			continue
		}
		if name := strings.TrimSpace(value); name != "" {
			return name
		}
	}
	return ""
}

// nodePlatform is Node's process.platform for a Go GOOS.
func nodePlatform(goos string) string {
	switch goos {
	case "windows":
		return "win32"
	case "darwin":
		return "darwin"
	case "linux":
		return "linux"
	case "freebsd":
		return "freebsd"
	default:
		return goos
	}
}

// nodeArch is Node's process.arch for a Go GOARCH.
func nodeArch(goarch string) string {
	switch goarch {
	case "amd64":
		return "x64"
	case "arm64":
		return "arm64"
	case "386":
		return "ia32"
	default:
		return goarch
	}
}

// intField reads `name: 123` (or `name 123`) out of a /proc-style text and
// returns the first number.
func intField(text, name string) *int64 {
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, name) {
			continue
		}
		rest := trimmed[len(name):]
		rest = strings.TrimLeft(rest, ": \t")
		if rest == trimmed {
			continue
		}
		digits := ""
		for _, r := range rest {
			if r < '0' || r > '9' {
				break
			}
			digits += string(r)
		}
		if digits == "" {
			continue
		}
		value, err := strconv.ParseInt(digits, 10, 64)
		if err != nil {
			continue
		}
		return &value
	}
	return nil
}

// Detector is hardware.detect: one cached reading of this machine.
type Detector struct {
	Host *Host
	// Root is the directory whose volume the disk figures describe.
	Root string
	// CacheMs overrides HardwareCacheMs (tests use 0 to always re-read).
	CacheMs int

	mu       sync.Mutex
	cached   *Machine
	cachedAt time.Time
}

// NewDetector returns a detector for this machine, measuring disk space of root.
func NewDetector(root string) *Detector {
	return &Detector{Host: DefaultHost(), Root: root}
}

// ResetForTests drops the cached reading (hardware.resetForTests).
func (d *Detector) ResetForTests() {
	d.mu.Lock()
	d.cached = nil
	d.mu.Unlock()
}

// Detect returns this machine, re-reading it when the reading is older than
// HardwareCacheMs or fresh is set.
func (d *Detector) Detect(ctx context.Context, fresh bool) (Machine, error) {
	cacheMs := d.CacheMs
	if cacheMs == 0 {
		cacheMs = HardwareCacheMs
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if !fresh && d.cached != nil && d.host().now().Sub(d.cachedAt) < time.Duration(cacheMs)*time.Millisecond {
		return *d.cached, nil
	}
	value := d.read(ctx)
	d.cached = &value
	d.cachedAt = d.host().now()
	return value, nil
}

func (d *Detector) host() *Host {
	if d.Host == nil {
		d.Host = DefaultHost()
	}
	return d.Host
}

// ContainerMemoryLimit is the cgroup's memory limit, cgroup v2 then v1. A huge
// number means "no limit" and is ignored.
func (h *Host) ContainerMemoryLimit(ctx context.Context) *int64 {
	if h.TotalMem == nil {
		return nil
	}
	physical := h.TotalMem(ctx)
	for _, file := range []string{"/sys/fs/cgroup/memory.max", "/sys/fs/cgroup/memory/memory.limit_in_bytes"} {
		text, ok := h.readFile(file)
		if !ok {
			continue
		}
		value, err := strconv.ParseInt(strings.TrimSpace(text), 10, 64)
		if err != nil || value <= 0 {
			continue
		}
		if physical == 0 || uint64(value) < physical {
			return &value
		}
	}
	return nil
}

// nvidiaCards lists the NVIDIA cards with their memory and the newest CUDA the
// driver supports.
func (h *Host) nvidiaCards(ctx context.Context) ([]struct {
	Name     string
	MemoryMB int64
	FreeMB   int64
}, string, bool) {
	list, ok := h.command(ctx, "nvidia-smi", "--query-gpu=name,memory.total,memory.free", "--format=csv,noheader,nounits")
	if !ok {
		return nil, "", false
	}
	var cards []struct {
		Name     string
		MemoryMB int64
		FreeMB   int64
	}
	for _, line := range strings.Split(strings.TrimSpace(list), "\n") {
		parts := strings.Split(line, ",")
		if len(parts) < 3 {
			continue
		}
		for index := range parts {
			parts[index] = strings.TrimSpace(parts[index])
		}
		memoryMB, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil || memoryMB <= 0 {
			continue
		}
		freeMB, _ := strconv.ParseInt(parts[2], 10, 64)
		cards = append(cards, struct {
			Name     string
			MemoryMB int64
			FreeMB   int64
		}{Name: parts[0], MemoryMB: memoryMB, FreeMB: freeMB})
	}
	if len(cards) == 0 {
		return nil, "", false
	}
	cuda := ""
	if header, ok := h.command(ctx, "nvidia-smi"); ok {
		cuda = cudaVersion(header)
	}
	return cards, cuda, true
}

// cudaVersion reads "CUDA Version: 13.0" out of nvidia-smi's header.
func cudaVersion(header string) string {
	for _, line := range strings.Split(header, "\n") {
		index := strings.Index(line, "CUDA Version:")
		if index < 0 {
			continue
		}
		rest := strings.TrimSpace(line[index+len("CUDA Version:"):])
		version := ""
		for _, r := range rest {
			if (r >= '0' && r <= '9') || r == '.' {
				version += string(r)
				continue
			}
			break
		}
		return version
	}
	return ""
}

// GPUDetection is the graphics card models may run on, nil when there is none.
func (h *Host) GPUDetection(ctx context.Context, totalMB int64) *GPU {
	if h.Platform == "darwin" && h.Arch == "arm64" {
		return &GPU{Type: "metal", Name: "Apple GPU", MemoryMB: int64(floor(float64(totalMB) * UnifiedGPUShare)), Unified: true}
	}
	if h.Platform == "linux" {
		cards, cuda, ok := h.nvidiaCards(ctx)
		if !ok {
			return nil
		}
		names := []string{}
		seen := map[string]bool{}
		var memoryMB int64
		for _, card := range cards {
			memoryMB += card.MemoryMB
			if !seen[card.Name] {
				seen[card.Name] = true
				names = append(names, card.Name)
			}
		}
		joined := strings.Join(names, ", ")
		if len(cards) > 1 {
			joined = strconv.Itoa(len(cards)) + "× " + joined
		}
		return &GPU{Type: "cuda", Name: joined, MemoryMB: memoryMB, CUDA: cuda}
	}
	return nil
}

// DiskDetection measures the models' volume, nil figures when it cannot.
func (h *Host) DiskDetection(root string) Disk {
	if h.Statfs == nil {
		return Disk{}
	}
	free, total, ok := h.Statfs(root)
	if !ok {
		return Disk{}
	}
	freeMB := free / MB
	totalMB := total / MB
	return Disk{FreeMB: &freeMB, TotalMB: &totalMB}
}

// read is hardware.read: one reading of the machine.
func (d *Detector) read(ctx context.Context) Machine {
	host := d.host()
	limit := host.ContainerMemoryLimit(ctx)
	totalBytes := uint64(0)
	if host.TotalMem != nil {
		totalBytes = host.TotalMem(ctx)
	}
	if limit != nil {
		totalBytes = uint64(*limit)
	}
	totalMB := int64(totalBytes / MB)
	cpu := CPU{Name: "Unknown processor", Cores: 1}
	if host.CPUInfo != nil {
		cpu = host.CPUInfo(ctx)
	}
	return Machine{
		Platform: host.Platform,
		Arch:     host.Arch,
		CPU:      cpu,
		Memory:   Memory{TotalMB: totalMB, Limited: limit != nil},
		GPU:      host.GPUDetection(ctx, totalMB),
		Disk:     host.DiskDetection(d.Root),
	}
}
