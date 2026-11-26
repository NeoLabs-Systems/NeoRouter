package status

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/ssrf"
)

// HostReportInterval is how often a running server reports its host.
const HostReportInterval = time.Hour

const (
	gib        = 1024 * 1024 * 1024
	mib        = 1024 * 1024
	reportTime = 5 * time.Second
)

// Host identifies the running build for the host report.
type Host struct {
	Version string
	// StartedAt is when the process started, which sets the uptime band.
	StartedAt time.Time
}

// HostSnapshot is one coarse host report: bands instead of exact hardware
// sizes, and never a hostname, address or installation id.
type HostSnapshot struct {
	AppVersion    string `json:"app_version"`
	OS            string `json:"os"`
	Architecture  string `json:"architecture"`
	CPUCoresRange string `json:"cpu_cores_range"`
	RAMGiBRange   string `json:"ram_gib_range"`
	// MemoryUsedPercentBand is absent where free memory cannot be read without
	// a helper process, rather than guessed.
	MemoryUsedPercentBand *int   `json:"memory_used_percent_band,omitempty"`
	ProcessRSSMiBRange    string `json:"process_rss_mib_range"`
	UptimeHoursRange      string `json:"uptime_hours_range"`
	// LoadPerCorePercentBand is only reported where the OS exposes a load
	// average, which excludes Windows.
	LoadPerCorePercentBand *int `json:"load_per_core_percent_band,omitempty"`
}

// Snapshot is the machine as coarse bands.
func (h Host) Snapshot() HostSnapshot {
	cores := runtime.NumCPU()
	total := totalMemory()
	snap := HostSnapshot{
		AppVersion:    h.Version,
		OS:            platformName(runtime.GOOS),
		Architecture:  archName(runtime.GOARCH),
		CPUCoresRange: powerOfTwoBucket(float64(cores)),
		RAMGiBRange:   powerOfTwoBucket(float64(total) / gib),
	}
	if total > 0 {
		if free, ok := freeMemory(); ok {
			snap.MemoryUsedPercentBand = band((1-float64(free)/float64(total))*100, 0, 100)
		}
	}
	snap.ProcessRSSMiBRange = powerOfTwoBucket(float64(processRSS()) / mib)
	if !h.StartedAt.IsZero() {
		snap.UptimeHoursRange = powerOfTwoBucket(time.Since(h.StartedAt).Hours())
	} else {
		snap.UptimeHoursRange = powerOfTwoBucket(0)
	}
	if load, ok := loadAverage(); ok && runtime.GOOS != "windows" && cores > 0 {
		snap.LoadPerCorePercentBand = band(load/float64(cores)*100, 0, 200)
	}
	return snap
}

// powerOfTwoBucket is the power-of-two band a capacity falls in, so a report
// never carries an exact hardware size.
func powerOfTwoBucket(value float64) string {
	if math.IsNaN(value) || math.IsInf(value, 0) || value <= 0 {
		return "unknown"
	}
	upper := math.Pow(2, math.Ceil(math.Log2(value)))
	return FormatNumber(upper/2) + "-" + FormatNumber(upper)
}

// FormatNumber formats a float for the magnitudes a host report carries: no
// exponent, no trailing zeros.
func FormatNumber(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// band rounds value to the nearest ten inside [min, max], like
// clamp(round(value / 10) * 10, min, max).
func band(value, min, max float64) *int {
	v := int(math.Round(value/10) * 10)
	if v < int(min) {
		v = int(min)
	}
	if v > int(max) {
		v = int(max)
	}
	return &v
}

// Poster sends one form-encoded report to the analytics endpoint.
type Poster func(ctx context.Context, endpoint string, form url.Values) error

// DefaultPoster posts with client, refusing redirects like fetch's
// redirect: 'error'. A nil client gets a guarded one that refuses internal
// destinations.
func DefaultPoster(client *http.Client) Poster {
	if client == nil {
		// MaxRedirects -1 makes ssrf's policy refuse the first redirect.
		client = ssrf.NewClient(ssrf.Options{Timeout: reportTime, MaxRedirects: -1})
	}
	return func(ctx context.Context, endpoint string, form url.Values) error {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		res, err := client.Do(req)
		if err != nil {
			return err
		}
		defer res.Body.Close()
		_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 4096))
		return nil
	}
}

// HostEvent is the Countly event of one host report.
type HostEvent struct {
	Key          string       `json:"key"`
	Count        int          `json:"count"`
	Segmentation HostSnapshot `json:"segmentation"`
}

// ReportHost sends one host snapshot. It returns the failure, but a caller may
// ignore it: analytics never affect a running server.
func ReportHost(ctx context.Context, cfg *Config, snap HostSnapshot, post Poster) error {
	if cfg == nil {
		return nil
	}
	if post == nil {
		post = DefaultPoster(nil)
	}
	events, err := json.Marshal([]HostEvent{{Key: "host-snapshot", Count: 1, Segmentation: snap}})
	if err != nil {
		return fmt.Errorf("status: encode host report: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, reportTime)
	defer cancel()
	// A stream label satisfies Countly's protocol without identifying an
	// installation.
	form := url.Values{
		"app_key":   {cfg.AppKey},
		"device_id": {"aggregate-server"},
		"events":    {string(events)},
	}
	if err := post(ctx, cfg.Endpoint, form); err != nil {
		return fmt.Errorf("status: report host: %w", err)
	}
	return nil
}

// StartHostAnalytics reports the host once now and then once an hour until ctx
// is cancelled. It does nothing when analytics are opted out.
func StartHostAnalytics(ctx context.Context, cfg *Config, host Host, post Poster) {
	if cfg == nil {
		return
	}
	go func() {
		_ = ReportHost(ctx, cfg, host.Snapshot(), post)
		ticker := time.NewTicker(HostReportInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_ = ReportHost(ctx, cfg, host.Snapshot(), post)
			}
		}
	}()
}

// platformName maps Go's GOOS to the value Node's os.platform() reports, so the
// analytics stream keeps one vocabulary.
func platformName(goos string) string {
	if goos == "windows" {
		return "win32"
	}
	return goos
}

// archName maps Go's GOARCH to Node's os.arch() names.
func archName(goarch string) string {
	switch goarch {
	case "amd64":
		return "x64"
	case "386":
		return "ia32"
	}
	return goarch
}

// processRSS approximates the resident size Node reports as
// process.memoryUsage().rss. Go keeps no such counter, so the runtime's total
// heap and stack bytes stand in.
func processRSS() uint64 {
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	return stats.Sys
}

// totalMemory is os.totalmem().
func totalMemory() uint64 {
	if runtime.GOOS == "darwin" {
		if v, ok := sysctlInt("hw.memsize"); ok && v > 0 {
			return uint64(v)
		}
		return 0
	}
	if info, ok := procMemoryInfo(); ok {
		return info.total
	}
	return 0
}

// freeMemory is os.freemem(). Linux exposes it in /proc/meminfo; elsewhere it
// needs per-OS counters this port does not read, and the caller omits the band.
func freeMemory() (uint64, bool) {
	if runtime.GOOS != "linux" {
		return 0, false
	}
	if info, ok := procMemoryInfo(); ok && info.total > 0 {
		return info.free, true
	}
	return 0, false
}

// loadAverage is os.loadavg()[0].
func loadAverage() (float64, bool) {
	switch runtime.GOOS {
	case "linux":
		raw, err := os.ReadFile("/proc/loadavg")
		if err != nil {
			return 0, false
		}
		fields := strings.Fields(string(raw))
		if len(fields) == 0 {
			return 0, false
		}
		v, err := strconv.ParseFloat(fields[0], 64)
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
			return 0, false
		}
		return v, true
	case "darwin":
		// sysctl prints "{ 1.23 1.45 1.67 }".
		raw, err := exec.Command("sysctl", "-n", "vm.loadavg").Output()
		if err != nil {
			return 0, false
		}
		for _, field := range strings.Fields(strings.Trim(string(raw), "{} \n")) {
			if v, err := strconv.ParseFloat(field, 64); err == nil {
				return v, true
			}
		}
	}
	return 0, false
}

// sysctlInt reads one integer sysctl on macOS.
func sysctlInt(name string) (int64, bool) {
	raw, err := exec.Command("sysctl", "-n", name).Output()
	if err != nil {
		return 0, false
	}
	v, err := strconv.ParseInt(strings.TrimSpace(string(raw)), 10, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

type memoryInfo struct{ total, free uint64 }

// procMemoryInfo reads MemTotal and MemFree from /proc/meminfo, the values
// Node's os.totalmem() and os.freemem() report on Linux.
func procMemoryInfo() (memoryInfo, bool) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return memoryInfo{}, false
	}
	defer f.Close()
	var info memoryInfo
	var haveTotal, haveFree bool
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 2 {
			continue
		}
		kb, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			continue
		}
		switch fields[0] {
		case "MemTotal:":
			info.total = kb * 1024
			haveTotal = true
		case "MemFree:":
			info.free = kb * 1024
			haveFree = true
		}
	}
	return info, haveTotal && haveFree
}
