package localmodels

// installer.go gets a model onto disk — the engine it needs and its files,
// resumable, checked against their hashes, never more than the free disk space
// allows. One job per model; its progress (with time left) is what the admin
// sees.

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"
)

// rateWindowMs is the window download speed is measured over, for the time
// left.
const rateWindowMs = 10_000

// HardwareProbe is hardware.detect: what this machine offers.
type HardwareProbe interface {
	Detect(ctx context.Context, fresh bool) (Machine, error)
}

// installError is the failure line of a job, as the dashboard shows it.
type installError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// rateSample is one measurement of the download rate.
type rateSample struct {
	at    time.Time
	bytes int64
}

// installJob is one model's download.
type installJob struct {
	cancel     context.CancelFunc
	phase      string
	doneBytes  int64
	totalBytes int64
	samples    []rateSample
	err        *installError
}

// InstallProgress is what the dashboard shows while a model downloads.
type InstallProgress struct {
	Phase       string        `json:"phase"`
	DoneBytes   int64         `json:"doneBytes"`
	TotalBytes  int64         `json:"totalBytes"`
	SecondsLeft *int64        `json:"secondsLeft"`
	Error       *installError `json:"error"`
}

// Installer downloads catalog models.
type Installer struct {
	Paths    Paths
	Hardware HardwareProbe
	HFFiles  FileResolver
	Runtime  *LlamaRuntime
	Engines  func(engine string) Engine
	// HTTP downloads files; nil uses http.DefaultClient.
	HTTP *http.Client
	Log  *slog.Logger

	mu   sync.Mutex
	jobs map[string]*installJob
}

// NewInstaller returns an installer with no running jobs.
func NewInstaller(opts *Installer) *Installer {
	opts.jobs = map[string]*installJob{}
	return opts
}

func (i *Installer) log() *slog.Logger {
	if i.Log != nil {
		return i.Log
	}
	return slog.Default()
}

// InstallOptions are one job's settings.
type InstallOptions struct {
	// Verify also checks the files already there and fetches damaged ones again
	// (repairs).
	Verify bool
	// OnInstalled runs after every file is in place.
	OnInstalled func(ctx context.Context) error
}

// Run downloads what the entry needs. The job is registered before this
// returns, so a caller can read its progress straight away; the work happens in
// the background.
func (i *Installer) Run(entry *Entry, opts InstallOptions) {
	ctx, cancel := context.WithCancel(context.Background())
	job := &installJob{cancel: cancel, phase: "preparing", totalBytes: int64(entry.DownloadMB * MB)}
	i.mu.Lock()
	i.jobs[entry.ID] = job
	i.mu.Unlock()
	go i.runNow(ctx, entry, job, opts)
}

// runNow is one job's work.
func (i *Installer) runNow(ctx context.Context, entry *Entry, job *installJob, opts InstallOptions) {
	onBytes := func(delta int64) { i.track(job, delta) }
	fail := func(err error) {
		if ctx.Err() != nil {
			i.mu.Lock()
			if i.jobs[entry.ID] == job {
				delete(i.jobs, entry.ID)
			}
			i.mu.Unlock()
			job.cancel()
			return
		}
		i.log().Warn("Local model download failed", "model", entry.ID, "error", err)
		code := codeOf(err)
		if code == "" {
			code = "download_failed"
		}
		i.mu.Lock()
		job.err = &installError{Code: code, Message: err.Error()}
		job.phase = "failed"
		i.mu.Unlock()
	}

	machine, err := i.Hardware.Detect(ctx, true)
	if err != nil {
		fail(err)
		return
	}
	files, err := i.HFFiles.Resolve(ctx, entry)
	if err != nil {
		fail(err)
		return
	}
	dir := i.Paths.ModelDir(entry.ID)
	if opts.Verify {
		i.setPhase(job, "checking")
		for _, file := range files {
			target, err := i.Paths.Inside(dir, file.Path)
			if err != nil {
				fail(err)
				return
			}
			if _, statErr := os.Stat(target); statErr != nil {
				continue
			}
			complete, err := IsComplete(ctx, target, file.Size, file.SHA256, true)
			if err != nil {
				fail(err)
				return
			}
			if !complete {
				_ = os.Remove(target)
			}
		}
	}
	runtimeBytes := int64(0)
	if entry.Engine == EngineLlamaCpp {
		runtimeBytes, err = i.Runtime.PendingBytes(ctx, machine)
		if err != nil {
			fail(err)
			return
		}
	}
	modelBytes := int64(0)
	for _, file := range files {
		if file.Size != nil {
			modelBytes += *file.Size
		}
	}
	i.mu.Lock()
	job.totalBytes = runtimeBytes + modelBytes
	i.mu.Unlock()
	missingMB := float64(runtimeBytes+modelBytes-i.bytesOnDisk(dir, files)) / MB
	if machine.Disk.FreeMB != nil && missingMB+DiskMarginMB > float64(*machine.Disk.FreeMB) {
		fail(withCode("not_enough_disk", fmt.Errorf("There is not enough free disk space for this model.")))
		return
	}

	i.setPhase(job, "runtime")
	engine := i.Engines(entry.Engine)
	if engine == nil {
		fail(fmt.Errorf("Unknown local engine %s.", entry.Engine))
		return
	}
	if err := engine.Prepare(ctx, machine, PrepareOptions{OnBytes: onBytes}); err != nil {
		fail(err)
		return
	}
	i.setPhase(job, "model")
	for _, file := range files {
		target, err := i.Paths.Inside(dir, file.Path)
		if err != nil {
			fail(err)
			return
		}
		if err := Download(ctx, i.HTTP, file, target, DownloadOptions{OnBytes: onBytes}); err != nil {
			fail(err)
			return
		}
	}
	i.mu.Lock()
	if i.jobs[entry.ID] == job {
		delete(i.jobs, entry.ID)
	}
	i.mu.Unlock()
	i.log().Info("Local model installed", "model", entry.ID, "bytes", modelBytes)
	if opts.OnInstalled != nil {
		if err := opts.OnInstalled(ctx); err != nil {
			i.log().Warn("Local model could not be started after installing", "model", entry.ID, "error", err)
		}
	}
	job.cancel()
}

// setPhase records a job's phase.
func (i *Installer) setPhase(job *installJob, phase string) {
	i.mu.Lock()
	job.phase = phase
	i.mu.Unlock()
}

// track adds a progress delta and records the rate samples.
func (i *Installer) track(job *installJob, delta int64) {
	i.mu.Lock()
	defer i.mu.Unlock()
	job.doneBytes += delta
	if job.doneBytes < 0 {
		job.doneBytes = 0
	}
	now := time.Now()
	if len(job.samples) == 0 || now.Sub(job.samples[len(job.samples)-1].at) >= time.Second {
		job.samples = append(job.samples, rateSample{at: now, bytes: job.doneBytes})
		for len(job.samples) > 1 && now.Sub(job.samples[0].at) > rateWindowMs*time.Millisecond {
			job.samples = job.samples[1:]
		}
	}
}

// secondsLeft is the time left at the current speed, nil while unknown.
func secondsLeft(job *installJob) *int64 {
	if job.phase != "runtime" && job.phase != "model" {
		return nil
	}
	if len(job.samples) < 3 {
		return nil
	}
	first := job.samples[0]
	last := job.samples[len(job.samples)-1]
	seconds := last.at.Sub(first.at).Seconds()
	if seconds <= 0 {
		return nil
	}
	rate := float64(last.bytes-first.bytes) / seconds
	if rate <= 0 {
		return nil
	}
	left := int64(ceil(float64(job.totalBytes-job.doneBytes) / rate))
	return &left
}

// Progress is a job's progress for the dashboard, nil when nothing is
// happening.
func (i *Installer) Progress(id string) *InstallProgress {
	i.mu.Lock()
	defer i.mu.Unlock()
	job := i.jobs[id]
	if job == nil {
		return nil
	}
	return &InstallProgress{
		Phase:       job.phase,
		DoneBytes:   job.doneBytes,
		TotalBytes:  job.totalBytes,
		SecondsLeft: secondsLeft(job),
		Error:       job.err,
	}
}

// Active reports whether a job is running without a failure.
func (i *Installer) Active(id string) bool {
	i.mu.Lock()
	defer i.mu.Unlock()
	job := i.jobs[id]
	return job != nil && job.err == nil
}

// Cancel stops one job.
func (i *Installer) Cancel(id string) {
	i.mu.Lock()
	job := i.jobs[id]
	delete(i.jobs, id)
	i.mu.Unlock()
	if job != nil {
		job.cancel()
	}
}

// CancelAll stops every job, for shutdown.
func (i *Installer) CancelAll() {
	i.mu.Lock()
	jobs := make([]*installJob, 0, len(i.jobs))
	for _, job := range i.jobs {
		jobs = append(jobs, job)
	}
	i.jobs = map[string]*installJob{}
	i.mu.Unlock()
	for _, job := range jobs {
		job.cancel()
	}
}

// bytesOnDisk is how much of the file list is already there.
func (i *Installer) bytesOnDisk(dir string, files []File) int64 {
	var total int64
	for _, file := range files {
		target, err := i.Paths.Inside(dir, file.Path)
		if err != nil {
			continue
		}
		if size := fileSize(target); size != nil {
			total += *size
		}
	}
	return total
}

// ceil rounds up.
func ceil(value float64) float64 {
	whole := float64(int64(value))
	if value > whole {
		return whole + 1
	}
	return whole
}
