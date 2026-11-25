package localmodels

// views.go is the JSON the admin page reads: local_model_service.js's overview()
// and everything under it. Field order is the JSON order Express wrote, so the
// response stays byte-compatible.

// Overview is GET /api/v1/admin/local-models.
type Overview struct {
	System        SystemView          `json:"system"`
	Engines       EngineViews         `json:"engines"`
	Memory        MemoryView          `json:"memory"`
	PrivacyFilter PrivacyStatus       `json:"privacyFilter"`
	Access        string              `json:"access"`
	Starter       []StarterSuggestion `json:"starter"`
	Installed     []InstalledModel    `json:"installed"`
	Catalog       []CatalogView       `json:"catalog"`
}

// SystemView is the machine as the page shows it.
type SystemView struct {
	CPU           CPU      `json:"cpu"`
	MemoryMB      int64    `json:"memoryMb"`
	MemoryLimited bool     `json:"memoryLimited"`
	GPU           *GPUView `json:"gpu"`
	Disk          Disk     `json:"disk"`
}

// GPUView is the graphics card, without the fields the page does not show.
type GPUView struct {
	Type     string `json:"type"`
	Name     string `json:"name"`
	MemoryMB int64  `json:"memoryMb"`
}

// EngineViews is one line per engine.
type EngineViews struct {
	LlamaCpp     LlamaCppView `json:"llama_cpp"`
	Transformers Support      `json:"transformers"`
}

// LlamaCppView is the llama.cpp engine's support and its build.
type LlamaCppView struct {
	OK        bool     `json:"ok"`
	Reason    *string  `json:"reason"`
	Build     string   `json:"build"`
	Installed *string  `json:"installed"`
	GPU       bool     `json:"gpu"`
	Broken    []string `json:"broken"`
}

// MemoryView is the memory budget: usedMb is local models (and the privacy
// filter's reserve), othersMb what everything else uses on average, roomMb what
// models may use beside it.
type MemoryView struct {
	UsedMB   int64  `json:"usedMb"`
	OthersMB *int64 `json:"othersMb"`
	RoomMB   int64  `json:"roomMb"`
}

// InstalledModel is one installed model's line.
type InstalledModel struct {
	ID string `json:"id"`
	// Name falls back to the id when the catalog no longer has the model.
	Name    string  `json:"name"`
	Kind    *string `json:"kind"`
	State   string  `json:"state"`
	Enabled bool    `json:"enabled"`
	// Progress is the running download, nil when nothing is downloading.
	Progress *InstallProgress `json:"progress"`
	// Problem is a failed download or the runner's last failure.
	Problem any `json:"problem"`
	// MemoryMB is what the model takes, nil when it left the catalog.
	MemoryMB       *int64 `json:"memoryMb"`
	MemoryMeasured bool   `json:"memoryMeasured"`
	// GPU reports whether the running model uses the graphics card, nil when it
	// is not running.
	GPU *bool `json:"gpu"`
}

// CatalogView is one catalog entry with its fit on this machine. Assessment is
// embedded, so its fields follow this one's in the JSON, as the spread did.
type CatalogView struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Kind        string    `json:"kind"`
	Summary     Text      `json:"summary"`
	Languages   Languages `json:"languages"`
	License     string    `json:"license"`
	Recommended bool      `json:"recommended"`
	Features    Features  `json:"features"`
	Installed   bool      `json:"installed"`
	Assessment
}
