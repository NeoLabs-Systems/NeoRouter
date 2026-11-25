package localmodels

// service.go is server/services/local_models/local_model_service.js: local
// models end to end — what this machine can run, installing, running and
// stopping (one runner per model), repairing, and handing requests to the
// running models. Admins manage it in the dashboard; the router reaches it
// through the "NeoRouter Local" connection.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/dbx"
	"github.com/NeoLabs-Systems/neorouter/internal/config"
	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
)

// AutoRepairEveryMs is how often a model that keeps failing is repaired
// automatically.
const AutoRepairEveryMs = 60 * 60_000

// PrivacyStatus is the AI privacy filter as the local models page reports it:
// processing privacy_model.status(). The filter is a local model too, and its
// memory is kept free while it is on but not yet loaded.
type PrivacyStatus struct {
	Enabled  bool           `json:"enabled"`
	Model    string         `json:"model"`
	MemoryMB int64          `json:"memoryMb"`
	Status   string         `json:"status"`
	Progress map[string]any `json:"progress"`
	Error    any            `json:"error"`
}

// privacyModelID and privacyMemoryMB are processing's constants, repeated here
// so the page still says something before that domain is wired.
const (
	privacyModelID  = "openai/privacy-filter"
	privacyMemoryMB = 2048
)

// Options wire the service. Only DB and Cfg are required.
type Options struct {
	DB  *dbx.DB
	Cfg func() config.Config
	Log *slog.Logger

	// Settings is internal/settings, where the access level lives.
	Settings SettingsStore

	// PrivacyStatus is processing privacy_model.status(); nil reports the
	// filter as off.
	PrivacyStatus func() PrivacyStatus
	// SetPrivacyEnabled is processing privacy_model.setEnabled().
	SetPrivacyEnabled func(enabled bool)

	// ConnectAccount creates a person's "NeoRouter Local" provider account
	// (providers account_service.create with DefinitionID).
	ConnectAccount func(ctx context.Context, userID string) error
	// SyncAccounts relists one local connection's models (catalog
	// catalog_service.syncAccount(accountId)).
	SyncAccounts func(ctx context.Context, accountID string) error

	// Hardware replaces the machine probe; nil detects this machine.
	Hardware HardwareProbe
	// HFFiles replaces the Hugging Face file list; nil asks Hugging Face.
	HFFiles FileResolver
	// Engines replaces the engine registry, by engine id.
	Engines map[string]Engine
	// HTTP is the client for downloads and Hugging Face; nil uses
	// http.DefaultClient.
	HTTP *http.Client
	// DataDir replaces config's data directory (tests).
	DataDir string
}

// Service is the local models domain.
type Service struct {
	DB  *dbx.DB
	cfg func() config.Config
	Log *slog.Logger

	Paths     Paths
	Store     *Store
	Access    *Access
	Installer *Installer
	Memory    *Sampler
	Hardware  HardwareProbe
	Runtime   *LlamaRuntime
	Processes *ProcessManager

	opts Options
	eng  engines

	mu             sync.Mutex
	runners        map[string]*Runner
	autoRepairedAt map[string]time.Time
}

// New builds the service and its collaborators.
func New(opts Options) (*Service, error) {
	if opts.DB == nil {
		return nil, errors.New("localmodels: a database is required")
	}
	log := opts.Log
	if log == nil {
		log = slog.Default()
	}
	dataDir := opts.DataDir
	if dataDir == "" {
		if opts.Cfg == nil {
			return nil, errors.New("localmodels: a config is required")
		}
		dataDir = opts.Cfg().Paths.Data
	}
	paths := NewPaths(dataDir)
	if err := paths.EnsureDirs(); err != nil {
		return nil, err
	}
	svc := &Service{
		DB:             opts.DB,
		cfg:            opts.Cfg,
		Log:            log,
		Paths:          paths,
		Store:          &Store{DB: opts.DB},
		Memory:         NewSampler(),
		Runtime:        NewLlamaRuntime(paths),
		Processes:      NewProcessManager(paths),
		opts:           opts,
		runners:        map[string]*Runner{},
		autoRepairedAt: map[string]time.Time{},
	}
	if opts.Hardware != nil {
		svc.Hardware = opts.Hardware
	} else {
		svc.Hardware = NewDetector(paths.Root)
	}
	svc.Access = &Access{
		DB:             opts.DB,
		Settings:       opts.Settings,
		Log:            log,
		ConnectAccount: opts.ConnectAccount,
		SyncAccounts:   opts.SyncAccounts,
	}

	llamaEngine := Engine(&LlamaServerEngine{Runtime: svc.Runtime, Processes: svc.Processes})
	transformersEngine := Engine(&TransformersEngine{Paths: paths, Log: log})
	if engine, ok := opts.Engines[EngineLlamaCpp]; ok && engine != nil {
		llamaEngine = engine
	}
	if engine, ok := opts.Engines[EngineTransformers]; ok && engine != nil {
		transformersEngine = engine
	}
	svc.eng = engines{llamaCpp: llamaEngine, transformers: transformersEngine}

	svc.Installer = NewInstaller(&Installer{
		Paths:    paths,
		Hardware: svc.Hardware,
		HFFiles:  opts.HFFiles,
		Runtime:  svc.Runtime,
		Engines:  svc.engineFor,
		HTTP:     opts.HTTP,
		Log:      log,
	})
	if svc.Installer.HFFiles == nil {
		svc.Installer.HFFiles = &HFClient{HTTP: opts.HTTP}
	}
	return svc, nil
}

// engineFor is the engine an entry names, nil when nothing is wired for it.
func (s *Service) engineFor(id string) Engine { return s.eng.engineFor(id) }

// support is the engine support the fit and the page use.
func (s *Service) support(engine string, machine Machine) Support {
	if impl := s.engineFor(engine); impl != nil {
		return impl.Support(machine)
	}
	return DefaultSupport(engine, machine)
}

// entryOrThrow is the 404 a model outside the catalog gets.
func entryOrThrow(id string) (*Entry, error) {
	entry, ok := CatalogEntry(id)
	if !ok {
		return nil, httpserver.New(404, "LOCAL_MODEL_UNKNOWN", "This model is not in the catalog.")
	}
	return entry, nil
}

// rowOrThrow is the 404 a model that is not installed gets.
func (s *Service) rowOrThrow(ctx context.Context, id string) (*StoreRow, error) {
	row, err := s.Store.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, httpserver.New(404, "LOCAL_MODEL_NOT_INSTALLED", "This model is not installed.")
	}
	return row, nil
}

// FilesOf returns the absolute paths of the files an engine loads.
func (s *Service) FilesOf(entry *Entry) (ModelFiles, error) {
	dir := s.Paths.ModelDir(entry.ID)
	files := ModelFiles{Model: dir}
	if entry.Engine == EngineLlamaCpp {
		model, err := s.Paths.Inside(dir, entry.Source.Include[0])
		if err != nil {
			return ModelFiles{}, err
		}
		files.Model = model
	}
	if entry.Source.Projector != "" {
		projector, err := s.Paths.Inside(dir, entry.Source.Projector)
		if err != nil {
			return ModelFiles{}, err
		}
		files.Projector = projector
	}
	return files, nil
}

// activeStates are the runner states that count as "using memory".
var activeStates = map[string]bool{StateStarting: true, StateRunning: true, StateRestarting: true}

// modelMemory is what a model takes: its average while it ran here, else the
// estimate.
func (s *Service) modelMemory(entry *Entry) int64 {
	return Needs(entry, s.Memory.ModelMB(entry.ID)).MemoryMB
}

// PrivacyStatus is the AI privacy filter's state.
func (s *Service) PrivacyStatus() PrivacyStatus {
	if s.opts.PrivacyStatus != nil {
		return s.opts.PrivacyStatus()
	}
	return PrivacyStatus{Model: privacyModelID, MemoryMB: privacyMemoryMB, Status: "off"}
}

// privacyReserveMB is the memory kept for the privacy filter while it is on but
// not loaded (once loaded, it is part of what everything else uses).
func (s *Service) privacyReserveMB() int64 {
	status := s.PrivacyStatus()
	if status.Enabled && status.Status != "ready" {
		return status.MemoryMB
	}
	return 0
}

// memoryInUse is the memory taken by models that are running or about to, and
// the privacy filter.
func (s *Service) memoryInUse(exceptID string) int64 {
	used := s.privacyReserveMB()
	s.mu.Lock()
	runners := make(map[string]*Runner, len(s.runners))
	for id, runner := range s.runners {
		runners[id] = runner
	}
	s.mu.Unlock()
	for id, runner := range runners {
		if id == exceptID || !activeStates[runner.State()] {
			continue
		}
		entry, ok := CatalogEntry(id)
		if !ok {
			continue
		}
		used += s.modelMemory(entry)
	}
	return used
}

// runningProcesses are the models running now, for the memory measurements.
func (s *Service) runningProcesses() []RunningModel {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []RunningModel{}
	for id, runner := range s.runners {
		if runner.State() != StateRunning {
			continue
		}
		instance := runner.Instance()
		if instance == nil || instance.Proc == nil {
			continue
		}
		if pid := instance.Proc.PID(); pid > 0 {
			out = append(out, RunningModel{ID: id, PID: pid})
		}
	}
	return out
}

// machineNow is the machine with what everything else uses on average.
func (s *Service) machineNow(ctx context.Context, fresh bool) (Machine, error) {
	if fresh {
		_ = s.Memory.Refresh(ctx)
	}
	machine, err := s.Hardware.Detect(ctx, fresh)
	if err != nil {
		return Machine{}, err
	}
	machine.Memory.OthersMB = s.Memory.OthersMB()
	return machine, nil
}

// runnerFor returns the runner of a model, creating it on first use.
func (s *Service) runnerFor(id string) *Runner {
	s.mu.Lock()
	defer s.mu.Unlock()
	if runner, ok := s.runners[id]; ok {
		return runner
	}
	runner := NewRunner(id, func(ctx context.Context) (RunnerContext, error) {
		entry, err := entryOrThrow(id)
		if err != nil {
			return RunnerContext{}, err
		}
		files, err := s.FilesOf(entry)
		if err != nil {
			return RunnerContext{}, err
		}
		machine, err := s.machineNow(ctx, false)
		if err != nil {
			return RunnerContext{}, err
		}
		return RunnerContext{Entry: entry, Files: files, Machine: machine}, nil
	}, RunnerOptions{
		Engine:   s.engineFor,
		OnChange: func(*Runner) { s.Access.ScheduleSync() },
		OnGaveUp: func(runner *Runner) { s.autoRepair(runner.ID) },
		Log:      s.Log,
	})
	s.runners[id] = runner
	return runner
}

// existingRunner is the runner of a model, nil when it never started.
func (s *Service) existingRunner(id string) *Runner {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.runners[id]
}

// startModel starts a model if there is memory for it beside the ones already
// running.
func (s *Service) startModel(ctx context.Context, id string) error {
	entry, err := entryOrThrow(id)
	if err != nil {
		return err
	}
	runner := s.runnerFor(id)
	machine, err := s.machineNow(ctx, true)
	if err != nil {
		return err
	}
	assessed := Assess(entry, machine, AssessOptions{
		UsedMB:     s.memoryInUse(id),
		MeasuredMB: s.Memory.ModelMB(id),
		Support:    s.support,
	})
	if !assessed.FitsNow {
		runner.Refuse(&Problem{Code: "not_enough_memory", Message: "Not enough free memory. Stop another model first."})
		return nil
	}
	runner.Start()
	return nil
}

// download installs a model's files and then starts it when the admin wants it
// on.
func (s *Service) download(id string, verify bool) {
	entry, ok := CatalogEntry(id)
	if !ok {
		return
	}
	s.Installer.Run(entry, InstallOptions{
		Verify: verify,
		OnInstalled: func(ctx context.Context) error {
			installed := "installed"
			if err := s.Store.Update(ctx, id, StoreUpdate{Status: &installed}); err != nil {
				return err
			}
			row, err := s.Store.Get(ctx, id)
			if err != nil {
				return err
			}
			if row != nil && row.Enabled {
				return s.startModel(ctx, id)
			}
			return nil
		},
	})
}

// Install adds models to this server: the memory and disk check of the catalog
// fit, the row, and one download per model.
func (s *Service) Install(ctx context.Context, userID string, ids []string) (*Overview, error) {
	machine, err := s.machineNow(ctx, true)
	if err != nil {
		return nil, err
	}
	entries := make([]*Entry, 0, len(ids))
	for _, id := range ids {
		entry, err := entryOrThrow(id)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	for _, entry := range entries {
		row, err := s.Store.Get(ctx, entry.ID)
		if err != nil {
			return nil, err
		}
		if row != nil {
			return nil, httpserver.New(409, "LOCAL_MODEL_INSTALLED", "This model is already installed.")
		}
		assessed := Assess(entry, machine, AssessOptions{Support: s.support})
		switch {
		case assessed.Rating == RatingUnsupported:
			return nil, httpserver.New(422, "LOCAL_MODEL_UNSUPPORTED", "This system cannot run this model.")
		case assessed.Rating == RatingTooBig:
			return nil, httpserver.New(422, "LOCAL_MODEL_TOO_BIG", "This server does not have enough memory for this model.")
		case !assessed.DiskOK:
			return nil, httpserver.New(422, "LOCAL_MODEL_NO_DISK", "There is not enough free disk space for this model.")
		}
	}
	if err := s.Paths.EnsureDirs(); err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if err := s.Store.Add(ctx, entry.ID, userID); err != nil {
			return nil, err
		}
	}
	// The admin who installs a model can use it straight away.
	s.Access.Connect(ctx, userID)
	for _, entry := range entries {
		s.download(entry.ID, false)
	}
	return s.Overview(ctx)
}

// InstallStarter installs the suggested first set for this machine, all at once.
func (s *Service) InstallStarter(ctx context.Context, userID string) (*Overview, error) {
	machine, err := s.machineNow(ctx, true)
	if err != nil {
		return nil, err
	}
	ids := []string{}
	for _, item := range SuggestStarter(machine, s.support) {
		row, err := s.Store.Get(ctx, item.ID)
		if err != nil {
			return nil, err
		}
		if row == nil {
			ids = append(ids, item.ID)
		}
	}
	if len(ids) == 0 {
		return nil, httpserver.New(422, "LOCAL_MODEL_NO_STARTER", "No starter set fits this server.")
	}
	return s.Install(ctx, userID, ids)
}

// Retry tries again: a stopped download where it stopped, a model that could
// not start once more.
func (s *Service) Retry(ctx context.Context, id string) (*Overview, error) {
	row, err := s.rowOrThrow(ctx, id)
	if err != nil {
		return nil, err
	}
	if s.Installer.Active(id) {
		return s.Overview(ctx)
	}
	if row.Status == "downloading" {
		s.download(id, false)
	} else {
		enabled := true
		if err := s.Store.Update(ctx, id, StoreUpdate{Enabled: &enabled}); err != nil {
			return nil, err
		}
		if err := s.startModel(ctx, id); err != nil {
			return nil, err
		}
	}
	return s.Overview(ctx)
}

// SetEnabled records whether the admin wants a model running, and starts or
// stops it.
func (s *Service) SetEnabled(ctx context.Context, id string, enabled bool) (*Overview, error) {
	row, err := s.rowOrThrow(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := s.Store.Update(ctx, id, StoreUpdate{Enabled: &enabled}); err != nil {
		return nil, err
	}
	if row.Status != "installed" {
		return s.Overview(ctx)
	}
	if enabled {
		if err := s.startModel(ctx, id); err != nil {
			return nil, err
		}
	} else {
		s.runnerFor(id).Stop()
	}
	return s.Overview(ctx)
}

// Remove takes a model off this server.
func (s *Service) Remove(ctx context.Context, id string) (*Overview, error) {
	if _, err := s.rowOrThrow(ctx, id); err != nil {
		return nil, err
	}
	s.Installer.Cancel(id)
	if runner := s.existingRunner(id); runner != nil {
		runner.Stop()
	}
	s.mu.Lock()
	delete(s.runners, id)
	s.mu.Unlock()
	s.Memory.Forget(id)
	if err := s.Store.Remove(ctx, id); err != nil {
		return nil, err
	}
	if err := os.RemoveAll(s.Paths.ModelDir(id)); err != nil {
		return nil, fmt.Errorf("localmodels: remove model files: %w", err)
	}
	s.Access.ScheduleSync()
	return s.Overview(ctx)
}

// Repair puts a model right: it stops it, gives every engine build another
// chance, checks each file and fetches what is missing or damaged.
func (s *Service) Repair(ctx context.Context, id string) (*Overview, error) {
	if _, err := s.rowOrThrow(ctx, id); err != nil {
		return nil, err
	}
	s.Installer.Cancel(id)
	if runner := s.existingRunner(id); runner != nil {
		runner.Stop()
	}
	s.Runtime.ClearBroken()
	enabled := true
	if err := s.Store.Update(ctx, id, StoreUpdate{Enabled: &enabled}); err != nil {
		return nil, err
	}
	s.download(id, true)
	return s.Overview(ctx)
}

// autoRepair repairs a model that keeps failing, at most once an hour.
func (s *Service) autoRepair(id string) {
	s.mu.Lock()
	last := s.autoRepairedAt[id]
	now := time.Now()
	if now.Sub(last) < AutoRepairEveryMs*time.Millisecond {
		s.mu.Unlock()
		return
	}
	s.autoRepairedAt[id] = now
	s.mu.Unlock()
	s.Log.Warn("Local model keeps failing; repairing it", "model", id)
	go func() {
		if _, err := s.Repair(context.Background(), id); err != nil {
			s.Log.Warn("Automatic repair failed", "model", id, "error", err)
		}
	}()
}

// SetAccess records who may use local models.
func (s *Service) SetAccess(ctx context.Context, level string) (*Overview, error) {
	if err := s.Access.SetLevel(ctx, level); err != nil {
		return nil, err
	}
	return s.Overview(ctx)
}

// SetPrivacyFilter turns the AI privacy filter on or off.
func (s *Service) SetPrivacyFilter(ctx context.Context, enabled bool) (*Overview, error) {
	if s.opts.SetPrivacyEnabled != nil {
		s.opts.SetPrivacyEnabled(enabled)
	}
	return s.Overview(ctx)
}

// Served is one model answering right now.
type Served struct {
	Entry         *Entry
	ContextWindow *int
}

// ServedModels are the models answering right now, with what the router needs
// to list them.
func (s *Service) ServedModels() []Served {
	s.mu.Lock()
	defer s.mu.Unlock()
	served := []Served{}
	for id, runner := range s.runners {
		entry, ok := CatalogEntry(id)
		if !ok {
			continue
		}
		instance := runner.Instance()
		if runner.State() == StateRunning && instance != nil {
			served = append(served, Served{Entry: entry, ContextWindow: instance.ContextWindow})
		}
	}
	return served
}

// RunningInstance is the running model, nil when it is not running.
func (s *Service) RunningInstance(id string) *Instance {
	runner := s.existingRunner(id)
	if runner == nil {
		return nil
	}
	instance := runner.Instance()
	if instance == nil || runner.State() != StateRunning {
		return nil
	}
	return instance
}

// CanTranslate reports whether a model translates into English: only some
// speech recognisers do (Whisper does, Moonshine does not).
func (s *Service) CanTranslate(id string) bool {
	entry, ok := CatalogEntry(id)
	return ok && entry.Features.Translate
}

// Overview renders the admin page's data: the machine, the engines, the memory
// budget, the installed models and the catalog with its fit.
func (s *Service) Overview(ctx context.Context) (*Overview, error) {
	machine, err := s.machineNow(ctx, false)
	if err != nil {
		return nil, err
	}
	installed, err := s.Store.List(ctx)
	if err != nil {
		return nil, err
	}
	installedIDs := make(map[string]bool, len(installed))
	for _, row := range installed {
		installedIDs[row.ID] = true
	}
	usedMB := s.memoryInUse("")
	view := &Overview{
		System: SystemView{
			CPU:           machine.CPU,
			MemoryMB:      machine.Memory.TotalMB,
			MemoryLimited: machine.Memory.Limited,
			Disk:          machine.Disk,
		},
		Engines: EngineViews{
			LlamaCpp:     s.llamaCppView(machine),
			Transformers: s.support(EngineTransformers, machine),
		},
		Memory:        MemoryView{UsedMB: usedMB, OthersMB: machine.Memory.OthersMB, RoomMB: RoomMB(machine)},
		PrivacyFilter: s.PrivacyStatus(),
		Access:        s.Access.Level(ctx),
		Installed:     make([]InstalledModel, 0, len(installed)),
		Catalog:       make([]CatalogView, 0, len(Catalog())),
	}
	if machine.GPU != nil {
		view.System.GPU = &GPUView{Type: machine.GPU.Type, Name: machine.GPU.Name, MemoryMB: machine.GPU.MemoryMB}
	}
	if len(installed) == 0 {
		view.Starter = SuggestStarter(machine, s.support)
	} else {
		view.Starter = []StarterSuggestion{}
	}
	for _, row := range installed {
		view.Installed = append(view.Installed, s.installedView(row))
	}
	for _, entry := range Catalog() {
		view.Catalog = append(view.Catalog, CatalogView{
			ID:          entry.ID,
			Name:        entry.Name,
			Kind:        entry.Kind,
			Summary:     entry.Summary,
			Languages:   entry.Languages,
			License:     entry.License,
			Recommended: entry.Recommended,
			Features:    entry.Features,
			Installed:   installedIDs[entry.ID],
			Assessment:  Assess(entry, machine, AssessOptions{UsedMB: usedMB, MeasuredMB: s.Memory.ModelMB(entry.ID), Support: s.support}),
		})
	}
	return view, nil
}

// llamaCppView is the llama.cpp engine's line: its support and the build it
// would use.
func (s *Service) llamaCppView(machine Machine) LlamaCppView {
	support := s.support(EngineLlamaCpp, machine)
	status := s.Runtime.Status(machine)
	return LlamaCppView{
		OK:        support.OK,
		Reason:    support.Reason,
		Build:     status.Build,
		Installed: status.Installed,
		GPU:       status.GPU,
		Broken:    status.Broken,
	}
}

// installedView is one installed model's line of the page.
func (s *Service) installedView(row StoreRow) InstalledModel {
	entry, inCatalog := CatalogEntry(row.ID)
	runner := s.existingRunner(row.ID)
	progress := s.Installer.Progress(row.ID)
	state := StateStopped
	switch {
	case progress != nil:
		if progress.Error != nil {
			state = "download_failed"
		} else {
			state = "downloading"
		}
	case row.Status == "downloading":
		state = "download_failed"
	case runner != nil:
		state = runner.State()
	}
	view := InstalledModel{
		ID:             row.ID,
		Name:           row.ID,
		State:          state,
		Enabled:        row.Enabled,
		Progress:       progress,
		MemoryMeasured: s.Memory.ModelMB(row.ID) != nil,
	}
	if inCatalog {
		view.Name = entry.Name
		kind := entry.Kind
		view.Kind = &kind
		memory := s.modelMemory(entry)
		view.MemoryMB = &memory
	}
	if progress != nil && progress.Error != nil {
		view.Problem = progress.Error
	} else if runner != nil {
		if problem := runner.Problem(); problem != nil {
			view.Problem = problem
		}
	}
	if runner != nil {
		if instance := runner.Instance(); instance != nil {
			gpu := instance.GPU
			view.GPU = &gpu
		}
	}
	return view
}

// Resume is what a server start does: it ends model processes a crash left
// behind, finishes interrupted downloads and starts every model the admin left
// on, one after another so they do not all load at once.
func (s *Service) Resume(ctx context.Context) {
	if err := s.resumeNow(ctx); err != nil {
		s.Log.Error("Local models could not be resumed", "error", err)
	}
}

func (s *Service) resumeNow(ctx context.Context) error {
	s.Memory.SetRunningModels(s.runningProcesses)
	s.Memory.Start(ctx)
	s.Processes.EndOrphans()
	rows, err := s.Store.List(ctx)
	if err != nil {
		return err
	}
	inCatalog := []StoreRow{}
	for _, row := range rows {
		if _, ok := CatalogEntry(row.ID); ok {
			inCatalog = append(inCatalog, row)
		}
	}
	for _, row := range inCatalog {
		if row.Status == "downloading" {
			s.download(row.ID, false)
		}
	}
	for _, row := range inCatalog {
		if row.Status != "installed" || !row.Enabled {
			continue
		}
		if err := s.startModel(ctx, row.ID); err != nil {
			return err
		}
		s.runnerFor(row.ID).WaitSettled(ctx)
	}
	s.Access.ScheduleSync()
	return nil
}

// Shutdown stops every model and download, for server shutdown.
func (s *Service) Shutdown(ctx context.Context) {
	s.Memory.Stop()
	s.Installer.CancelAll()
	s.Access.StopSync()
	s.mu.Lock()
	runners := make([]*Runner, 0, len(s.runners))
	for _, runner := range s.runners {
		runners = append(runners, runner)
	}
	s.mu.Unlock()
	var wg sync.WaitGroup
	for _, runner := range runners {
		wg.Add(1)
		go func(runner *Runner) {
			defer wg.Done()
			runner.Stop()
		}(runner)
	}
	wg.Wait()
	s.Processes.KillAll()
}

// ResetForTests stops everything and forgets the runners, the way the Node
// service's resetForTests does.
func (s *Service) ResetForTests(ctx context.Context) {
	s.Shutdown(ctx)
	s.mu.Lock()
	s.runners = map[string]*Runner{}
	s.autoRepairedAt = map[string]time.Time{}
	s.mu.Unlock()
}
