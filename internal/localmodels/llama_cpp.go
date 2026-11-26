package localmodels

// llama_cpp.go is server/services/local_models/runtimes/llama_cpp.js: llama.cpp's
// server, downloaded from its official releases for this machine — Metal on
// Apple Silicon, CUDA where an NVIDIA driver is present, the CPU build
// everywhere else. A build that fails to run (an old driver, a missing library)
// is marked broken and the next one is used, so a GPU problem falls back to the
// CPU instead of leaving the admin stuck.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
)

// LlamaCppBuild is the pinned build: tested with every model in the catalog.
const LlamaCppBuild = "b12202"

// LlamaCppReleaseAPI is where the pinned release's assets are listed.
const LlamaCppReleaseAPI = "https://api.github.com/repos/ggml-org/llama.cpp/releases/tags/" + LlamaCppBuild

// LlamaCppBinaryName is the server binary inside a build.
const LlamaCppBinaryName = "llama-server"

// LlamaVariant is one build of llama.cpp for this machine.
type LlamaVariant struct {
	ID     string   `json:"id"`
	GPU    bool     `json:"gpu"`
	Assets []string `json:"assets"`
}

// llamaCppCUDAVersion is the major CUDA version an NVIDIA driver reports.
func llamaCppCUDAVersion(machine Machine) int {
	if machine.GPU == nil || machine.GPU.Type != "cuda" {
		return 0
	}
	major := strings.SplitN(machine.GPU.CUDA, ".", 2)[0]
	value, err := strconv.Atoi(major)
	if err != nil {
		return 0
	}
	return value
}

// LlamaVariants lists the builds to try on this machine, best first; empty when
// none exists.
func LlamaVariants(machine Machine) []LlamaVariant {
	cuda := llamaCppCUDAVersion(machine)
	cudaBuild := func(arch string) []string {
		switch {
		case cuda >= 13:
			return []string{"ubuntu-cuda-13.5-" + arch}
		case cuda >= 12 && arch == "x64":
			return []string{"ubuntu-cuda-12.8-x64"}
		default:
			return nil
		}
	}
	var ids []string
	switch machine.Platform + "-" + machine.Arch {
	case "darwin-arm64":
		ids = []string{"macos-arm64"}
	case "darwin-x64":
		ids = []string{"macos-x64"}
	case "linux-x64":
		ids = append(cudaBuild("x64"), "ubuntu-x64")
	case "linux-arm64":
		ids = append(cudaBuild("arm64"), "ubuntu-arm64")
	}
	variants := make([]LlamaVariant, 0, len(ids))
	for _, id := range ids {
		gpu := strings.HasPrefix(id, "macos-arm64") || strings.Contains(id, "cuda")
		assets := []string{"llama-" + LlamaCppBuild + "-bin-" + id + ".tar.gz"}
		if strings.Contains(id, "cuda") {
			assets = append(assets, "cudart-llama-"+LlamaCppBuild+"-bin-"+id+".tar.gz")
		}
		variants = append(variants, LlamaVariant{ID: id, GPU: gpu, Assets: assets})
	}
	return variants
}

// llamaCppSupport is the engine's support(): a system with no build for it
// cannot run llama.cpp models at all.
func llamaCppSupport(machine Machine) Support {
	if len(LlamaVariants(machine)) == 0 {
		return unsupported("unsupported_system")
	}
	return supported()
}

// LlamaRuntimeState is what the runtime remembers between starts.
type LlamaRuntimeState struct {
	Broken []string `json:"broken"`
}

// LlamaStatus is the engine's line of the dashboard.
type LlamaStatus struct {
	Build     string   `json:"build"`
	Installed *string  `json:"installed"`
	GPU       bool     `json:"gpu"`
	Broken    []string `json:"broken"`
}

// LlamaBuild is the build ensure() settled on.
type LlamaBuild struct {
	ID     string
	GPU    bool
	Binary string
	Dir    string
	Env    []string
}

// LlamaRuntime installs and locates llama.cpp builds.
type LlamaRuntime struct {
	Paths Paths
	// HTTP fetches release metadata and assets; nil uses http.DefaultClient.
	HTTP *http.Client
	// ReleaseAPI is the release to read, LlamaCppReleaseAPI by default.
	ReleaseAPI string
	// Command runs a program and returns its output, reporting false when it
	// could not run: it proves a build works before models use it.
	Command func(ctx context.Context, dir string, env []string, path string, args ...string) (string, bool)

	ensureMu sync.Mutex
}

// NewLlamaRuntime returns the runtime for one install's paths.
func NewLlamaRuntime(paths Paths) *LlamaRuntime {
	return &LlamaRuntime{Paths: paths}
}

func (r *LlamaRuntime) dir() string { return filepath.Join(r.Paths.RuntimesDir, "llama.cpp") }

func (r *LlamaRuntime) stateFile() string { return filepath.Join(r.dir(), "state.json") }

func (r *LlamaRuntime) releaseAPI() string {
	if r.ReleaseAPI != "" {
		return r.ReleaseAPI
	}
	return LlamaCppReleaseAPI
}

func (r *LlamaRuntime) variantDir(id string) string {
	return filepath.Join(r.dir(), LlamaCppBuild+"-"+id)
}

func (r *LlamaRuntime) binaryOf(id string) string {
	return filepath.Join(r.variantDir(id), LlamaCppBinaryName)
}

// State is the stored broken list.
func (r *LlamaRuntime) State() LlamaRuntimeState {
	state := LlamaRuntimeState{Broken: []string{}}
	data, err := os.ReadFile(r.stateFile())
	if err != nil {
		return state
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return LlamaRuntimeState{Broken: []string{}}
	}
	if state.Broken == nil {
		state.Broken = []string{}
	}
	return state
}

func (r *LlamaRuntime) writeState(state LlamaRuntimeState) error {
	if err := os.MkdirAll(r.dir(), 0o700); err != nil {
		return err
	}
	if state.Broken == nil {
		state.Broken = []string{}
	}
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return os.WriteFile(r.stateFile(), data, 0o600)
}

// usable lists the builds that are not marked broken.
func (r *LlamaRuntime) usable(machine Machine) []LlamaVariant {
	broken := map[string]bool{}
	for _, id := range r.State().Broken {
		broken[id] = true
	}
	out := []LlamaVariant{}
	for _, variant := range LlamaVariants(machine) {
		if !broken[variant.ID] {
			out = append(out, variant)
		}
	}
	return out
}

// Status is llamaCpp.status(hardware) for the dashboard.
func (r *LlamaRuntime) Status(machine Machine) LlamaStatus {
	var installed *string
	gpu := false
	for _, variant := range r.usable(machine) {
		if _, err := os.Stat(r.binaryOf(variant.ID)); err == nil {
			id := variant.ID
			installed = &id
			gpu = variant.GPU
			break
		}
	}
	return LlamaStatus{Build: LlamaCppBuild, Installed: installed, GPU: gpu, Broken: r.State().Broken}
}

// releaseAsset is one asset of the pinned release.
type releaseAsset struct {
	URL    string
	Size   int64
	SHA256 string
}

// releaseAssets reads the release's asset list from the GitHub API.
func (r *LlamaRuntime) releaseAssets(ctx context.Context) (map[string]releaseAsset, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, r.releaseAPI(), nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", downloadUserAgent)
	request.Header.Set("Accept", "application/vnd.github+json")
	client := r.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return nil, fmt.Errorf("GitHub answered %d for llama.cpp %s.", response.StatusCode, LlamaCppBuild)
	}
	var release struct {
		Assets []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
			Size               int64  `json:"size"`
			Digest             string `json:"digest"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(response.Body).Decode(&release); err != nil {
		return nil, err
	}
	assets := make(map[string]releaseAsset, len(release.Assets))
	for _, asset := range release.Assets {
		assets[asset.Name] = releaseAsset{
			URL:    asset.BrowserDownloadURL,
			Size:   asset.Size,
			SHA256: strings.TrimPrefix(asset.Digest, "sha256:"),
		}
	}
	return assets, nil
}

// environment is the environment a build runs with: its own libraries first on
// Linux.
func (r *LlamaRuntime) environment(id string) []string {
	env := os.Environ()
	if runtime.GOOS != "linux" {
		return env
	}
	existing := ""
	for _, entry := range env {
		if value, ok := strings.CutPrefix(entry, "LD_LIBRARY_PATH="); ok {
			existing = value
			break
		}
	}
	value := r.variantDir(id)
	if existing != "" {
		value += ":" + existing
	}
	return append(env, "LD_LIBRARY_PATH="+value)
}

// works reports whether the build starts on this machine at all.
func (r *LlamaRuntime) works(ctx context.Context, id string) bool {
	if r.Command == nil {
		return false
	}
	_, ok := r.Command(ctx, r.variantDir(id), r.environment(id), r.binaryOf(id), "--version")
	return ok
}

// install downloads and unpacks one build.
func (r *LlamaRuntime) install(ctx context.Context, variant LlamaVariant, opts PrepareOptions) error {
	assets, err := r.releaseAssets(ctx)
	if err != nil {
		return err
	}
	target := r.variantDir(variant.ID)
	incoming := target + ".incoming"
	if err := os.RemoveAll(incoming); err != nil {
		return err
	}
	client := r.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	for _, name := range variant.Assets {
		asset, ok := assets[name]
		if !ok {
			return fmt.Errorf("llama.cpp %s has no %s.", LlamaCppBuild, name)
		}
		archive := filepath.Join(r.dir(), "downloads", name)
		size := asset.Size
		file := File{URL: asset.URL, Size: &size, SHA256: asset.SHA256}
		if err := Download(ctx, client, file, archive, DownloadOptions{OnBytes: opts.OnBytes}); err != nil {
			return err
		}
		if err := extractTarGz(archive, incoming); err != nil {
			return err
		}
		if err := os.Remove(archive); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	if err := os.RemoveAll(target); err != nil {
		return err
	}
	return os.Rename(incoming, target)
}

// PendingBytes is the bytes still to download for the build Ensure would use (0
// when it is ready).
func (r *LlamaRuntime) PendingBytes(ctx context.Context, machine Machine) (int64, error) {
	usable := r.usable(machine)
	if len(usable) == 0 {
		return 0, nil
	}
	variant := usable[0]
	if _, err := os.Stat(r.binaryOf(variant.ID)); err == nil {
		return 0, nil
	}
	assets, err := r.releaseAssets(ctx)
	if err != nil {
		return 0, err
	}
	var total int64
	for _, name := range variant.Assets {
		total += assets[name].Size
	}
	return total, nil
}

// Ensure is the build to run models with, installing it first when needed.
// Builds that cannot start here are marked broken and skipped; one install runs
// at a time, because two models starting at once share the build.
func (r *LlamaRuntime) Ensure(ctx context.Context, machine Machine, opts PrepareOptions) (LlamaBuild, error) {
	r.ensureMu.Lock()
	defer r.ensureMu.Unlock()
	for _, variant := range r.usable(machine) {
		if _, err := os.Stat(r.binaryOf(variant.ID)); err != nil || !r.works(ctx, variant.ID) {
			if err := r.install(ctx, variant, opts); err != nil {
				return LlamaBuild{}, err
			}
		}
		if r.works(ctx, variant.ID) {
			return LlamaBuild{
				ID:     variant.ID,
				GPU:    variant.GPU,
				Binary: r.binaryOf(variant.ID),
				Dir:    r.variantDir(variant.ID),
				Env:    r.environment(variant.ID),
			}, nil
		}
		// A build that cannot run models here (CUDA with an old driver, a
		// missing library) is put aside and the next one is tried.
		r.MarkBroken(variant.ID)
	}
	return LlamaBuild{}, fmt.Errorf("llama.cpp does not run on this machine.")
}

// MarkBroken puts a build aside, deleting it, so the next start uses another.
func (r *LlamaRuntime) MarkBroken(id string) {
	state := r.State()
	found := false
	for _, broken := range state.Broken {
		if broken == id {
			found = true
			break
		}
	}
	if !found {
		state.Broken = append(state.Broken, id)
		_ = r.writeState(state)
	}
	_ = os.RemoveAll(r.variantDir(id))
}

// ClearBroken gives every build another chance (a driver may have been updated).
func (r *LlamaRuntime) ClearBroken() {
	_ = r.writeState(LlamaRuntimeState{Broken: []string{}})
}
