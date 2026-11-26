package processing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/httpx"
	"github.com/NeoLabs-Systems/neorouter/internal/config"
	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
)

// Request processing before routing: compression first (so everything after it,
// including AI detection, sees the smaller request), then the privacy shield.
// Settings are per user; both are off until the user turns them on.

// SettingKey is where one user's processing settings live.
const SettingKey = "request_processing"

// SettingsStore is the settings domain surface this package needs: the per-user
// JSON store and the global switch the admin settings page shares.
// *settings.Service satisfies it; internal/app wires it.
type SettingsStore interface {
	GetGlobalBool(ctx context.Context, key string, def bool) bool
	SetGlobal(ctx context.Context, key string, value any) error
	GetUserSetting(ctx context.Context, userID, key string) (json.RawMessage, bool, error)
	SetUserSetting(ctx context.Context, userID, key string, value any) error
}

// Space is the caller's space, as the auth and teams middleware resolve it.
type Space struct {
	OwnerID string
	// Permissions is the permission set of the space (processing.view,
	// processing.edit).
	Permissions map[string]bool
}

// Service is the processing domain.
type Service struct {
	Engine   *Engine
	Settings SettingsStore
	Privacy  *PrivacyModel
	Entities *EntityDetector
	Log      *slog.Logger
	Cfg      func() config.Config
	Now      func() int64
	// Space resolves the caller's space; nil means the routes are unguarded.
	Space func(ctx context.Context) (Space, bool)
	// Require guards a route with a permission.
	Require func(permission string) func(http.Handler) http.Handler
	// PoolMinimum raises the shield settings to the pool's minimum (the pools
	// domain provides it). Optional.
	PoolMinimum func(settings PrivacyConfig) PrivacyConfig
	// StatsQuery counts the requests of the last 30 days (/stats). Optional:
	// nil answers with zeros.
	StatsQuery func(ctx context.Context, userID string, sinceMillis int64) (Stats, error)
}

// New builds the domain from its dependencies.
func New(cfg func() config.Config, store SettingsStore, log *slog.Logger) (*Service, error) {
	registry, err := NewRegistry()
	if err != nil {
		return nil, err
	}
	if log == nil {
		log = slog.Default()
	}
	engine := NewEngine(registry)
	privacy := NewPrivacyModel(cfg, store, log)
	return &Service{
		Engine:   engine,
		Settings: store,
		Privacy:  privacy,
		Entities: &EntityDetector{Model: privacy, Log: log, Cache: map[string][]Entity{}},
		Log:      log,
		Cfg:      cfg,
		Now:      nowMillis,
	}, nil
}

// AIEnabled marks privacy.ai: the on-server privacy filter, which only works once
// an admin turned it on for this server.
type AIEnabled struct {
	Enabled bool `json:"enabled"`
}

// PrivacyConfig is one user's shield settings.
type PrivacyConfig struct {
	Enabled     bool              `json:"enabled"`
	Modes       map[string]string `json:"modes"`
	CustomTerms []string          `json:"customTerms"`
	AllowTerms  []string          `json:"allowTerms"`
	AI          AIEnabled         `json:"ai"`
}

// Settings is one user's processing settings, in their JSON order.
type Settings struct {
	Privacy     PrivacyConfig       `json:"privacy"`
	Compression CompressionDefaults `json:"compression"`
}

// DefaultSettings is what a user starts with: both features off.
func DefaultSettings() Settings {
	modes := make(map[string]string, len(DefaultModes))
	for category, mode := range DefaultModes {
		modes[category] = mode
	}
	return Settings{
		Privacy: PrivacyConfig{
			Enabled:     false,
			Modes:       modes,
			CustomTerms: []string{},
			AllowTerms:  []string{},
		},
		Compression: DefaultCompression(),
	}
}

// storedSettings is what sits in the settings table: a partial object, because an
// older server may have written only some of the fields.
type storedSettings struct {
	Privacy     *storedPrivacy     `json:"privacy"`
	Compression *storedCompression `json:"compression"`
}

type storedPrivacy struct {
	Enabled     *bool             `json:"enabled"`
	Modes       map[string]string `json:"modes"`
	CustomTerms []string          `json:"customTerms"`
	AllowTerms  []string          `json:"allowTerms"`
	AI          *AIEnabled        `json:"ai"`
}

type storedCompression struct {
	Level        *string         `json:"level"`
	Engines      map[string]bool `json:"engines"`
	MinTokens    *int            `json:"minTokens"`
	TargetTokens *int            `json:"targetTokens"`
	KeepRecent   *int            `json:"keepRecent"`
}

// mergeStored fills in the defaults and normalises the shape: defaults first,
// then whatever was stored.
func mergeStored(stored storedSettings) Settings {
	merged := DefaultSettings()
	if privacy := stored.Privacy; privacy != nil {
		if privacy.Enabled != nil {
			merged.Privacy.Enabled = *privacy.Enabled
		}
		for category, mode := range privacy.Modes {
			merged.Privacy.Modes[category] = mode
		}
		if privacy.CustomTerms != nil {
			merged.Privacy.CustomTerms = privacy.CustomTerms
		}
		if privacy.AllowTerms != nil {
			merged.Privacy.AllowTerms = privacy.AllowTerms
		}
		if privacy.AI != nil {
			merged.Privacy.AI = *privacy.AI
		}
	}
	if compression := stored.Compression; compression != nil {
		if compression.Level != nil {
			merged.Compression.Level = *compression.Level
		}
		if compression.Engines != nil {
			merged.Compression.Engines = compression.Engines
		}
		if compression.MinTokens != nil {
			merged.Compression.MinTokens = *compression.MinTokens
		}
		merged.Compression.TargetTokens = compression.TargetTokens
		if compression.KeepRecent != nil {
			merged.Compression.KeepRecent = *compression.KeepRecent
		}
	}
	return merged
}

// merge is merge(saveSettings(userId, next)): the defaults under a complete
// settings object.
func merge(next Settings) Settings {
	merged := DefaultSettings()
	merged.Privacy.Enabled = next.Privacy.Enabled
	for category, mode := range next.Privacy.Modes {
		merged.Privacy.Modes[category] = mode
	}
	if next.Privacy.CustomTerms != nil {
		merged.Privacy.CustomTerms = next.Privacy.CustomTerms
	}
	if next.Privacy.AllowTerms != nil {
		merged.Privacy.AllowTerms = next.Privacy.AllowTerms
	}
	merged.Privacy.AI = next.Privacy.AI
	merged.Compression = next.Compression
	if merged.Compression.Engines == nil {
		merged.Compression.Engines = map[string]bool{}
	}
	if merged.Compression.Level == "" {
		merged.Compression.Level = "off"
	}
	return merged
}

// GetSettings is one user's saved settings with the defaults applied.
func (s *Service) GetSettings(ctx context.Context, userID string) Settings {
	stored := storedSettings{}
	if s.Settings != nil {
		if raw, found, err := s.Settings.GetUserSetting(ctx, userID, SettingKey); err == nil && found {
			_ = json.Unmarshal(raw, &stored)
		}
	}
	return mergeStored(stored)
}

// SaveSettings stores settings after normalising them.
func (s *Service) SaveSettings(ctx context.Context, userID string, next Settings) (Settings, error) {
	merged := merge(next)
	if s.Settings == nil {
		return merged, nil
	}
	if err := s.Settings.SetUserSetting(ctx, userID, SettingKey, merged); err != nil {
		return merged, fmt.Errorf("processing: save settings: %w", err)
	}
	return merged, nil
}

// ApplyOptions carries what the caller knows about a request.
type ApplyOptions struct {
	// Settings overrides the stored settings (the settings preview).
	Settings *Settings
	// CompressionLevel comes from a request header and may only lower the level.
	CompressionLevel string
	// PoolMinimum: the request may go to a pool partner, so the shield runs with
	// at least the pool's minimum.
	PoolMinimum bool
}

// ApplyResult is what the pipeline did to a request.
type ApplyResult struct {
	Canonical Canonical
	Vault     *Vault
	Findings  []Finding
	Stats     ApplyStats
}

// ApplyStats is the stats block of the result.
type ApplyStats struct {
	Compression        CompressionStats `json:"compression"`
	Redacted           int              `json:"redacted"`
	RedactedByCategory map[string]int   `json:"redactedByCategory"`
	AIDetection        bool             `json:"aiDetection"`
}

// Apply runs the pipeline over one canonical request.
//
// Compression is an optimisation: if it fails, the request goes out as is. The
// privacy shield is a promise: if it fails, the request fails.
func (s *Service) Apply(ctx context.Context, userID string, canonical Canonical, options ApplyOptions) (ApplyResult, error) {
	own := s.GetSettings(ctx, userID)
	if options.Settings != nil {
		own = merge(*options.Settings)
	}
	config := own
	if options.PoolMinimum && s.PoolMinimum != nil {
		config.Privacy = s.PoolMinimum(own.Privacy)
	}
	compressed := Compress(canonical, config.Compression, options.CompressionLevel)
	result := compressed.Canonical
	var vault *Vault
	aiUsed := false
	if config.Privacy.Enabled {
		entitiesFor := func(int) []Entity { return nil }
		if config.Privacy.AI.Enabled && s.Privacy != nil && s.Privacy.Enabled(ctx) {
			if s.Entities != nil {
				entitiesFor = s.Entities.DetectEntities(ctx, result)
			}
			aiUsed = true
		}
		redaction := RedactRequest(s.Engine, result, RequestRedactOptions{
			Modes:       config.Privacy.Modes,
			CustomTerms: config.Privacy.CustomTerms,
			AllowTerms:  config.Privacy.AllowTerms,
			EntitiesFor: entitiesFor,
		})
		if redaction != nil {
			result = redaction.Canonical
			vault = redaction.Vault
		}
	}
	stats := ApplyStats{
		Compression:        compressed.Stats,
		RedactedByCategory: map[string]int{},
		AIDetection:        aiUsed,
	}
	if vault != nil {
		stats.Redacted = vault.Size()
		stats.RedactedByCategory = vault.Counts
	}
	findings := []Finding{}
	if vault != nil {
		findings = vault.Findings()
	}
	return ApplyResult{Canonical: result, Vault: vault, Findings: findings, Stats: stats}, nil
}

// ShieldTexts hides the values of texts that leave for a provider without an
// answer to put values back into (embeddings, image and video prompts, direct web
// searches). Only the "hide" categories apply: values that must never leave.
func (s *Service) ShieldTexts(ctx context.Context, userID string, texts []string) ([]string, int) {
	config := s.GetSettings(ctx, userID)
	if !config.Privacy.Enabled {
		return texts, 0
	}
	modes := make(map[string]string, len(config.Privacy.Modes))
	for category, mode := range config.Privacy.Modes {
		if mode == "hide" {
			modes[category] = "hide"
		} else {
			modes[category] = "off"
		}
	}
	vault := NewVault(s.Engine, modes)
	shielded := make([]string, len(texts))
	for index, text := range texts {
		shielded[index] = vault.RedactText(text, RedactOptions{
			CustomTerms: config.Privacy.CustomTerms,
			AllowTerms:  config.Privacy.AllowTerms,
		})
	}
	return shielded, vault.Size()
}

// PreviewOptions is one preview request: a sample text, or whole messages, and
// optionally a draft of the settings.
type PreviewOptions struct {
	Text     string
	Messages []map[string]any
	Role     string
	Settings *Settings
	// CanEdit: only someone who may edit the settings can preview a draft.
	CanEdit bool
}

// PreviewResult is what would be sent for a sample.
type PreviewResult struct {
	Messages []map[string]any `json:"messages"`
	Findings []Finding        `json:"findings"`
	Stats    ApplyStats       `json:"stats"`
}

// Preview runs the pipeline for the settings page: it shows what would be sent.
func (s *Service) Preview(ctx context.Context, userID string, options PreviewOptions) (PreviewResult, error) {
	config := s.GetSettings(ctx, userID)
	if options.CanEdit && options.Settings != nil {
		config = merge(*options.Settings)
	}
	messages := options.Messages
	if len(messages) == 0 {
		if options.Role == "tool" {
			messages = []map[string]any{
				{"role": "user", "content": "Summarize the tool output."},
				{"role": "tool", "content": options.Text},
			}
		} else {
			messages = []map[string]any{{"role": "user", "content": options.Text}}
		}
	}
	canonical := Canonical{"messages": messages}
	result, err := s.Apply(ctx, userID, canonical, ApplyOptions{Settings: &config})
	if err != nil {
		return PreviewResult{}, err
	}
	hadSystem := false
	for _, original := range messages {
		if messageRole(original) == "system" {
			hadSystem = true
			break
		}
	}
	kept := []map[string]any{}
	for _, message := range result.Canonical.Messages() {
		if messageRole(message) == "system" && !hadSystem {
			continue
		}
		kept = append(kept, message)
	}
	return PreviewResult{Messages: kept, Findings: result.Findings, Stats: result.Stats}, nil
}

// Routes registers the processing API at /api/v1/processing.
func (s *Service) Routes(mux *http.ServeMux) {
	mux.Handle("GET /api/v1/processing/settings", s.guard("processing.view", s.getSettings))
	mux.Handle("PUT /api/v1/processing/settings", s.guard("processing.edit", s.putSettings))
	mux.Handle("POST /api/v1/processing/preview", s.guard("processing.view", s.postPreview))
	mux.Handle("GET /api/v1/processing/stats", s.guard("processing.view", s.getStats))
}

func (s *Service) guard(permission string, handler http.HandlerFunc) http.Handler {
	if s.Require == nil {
		return handler
	}
	return s.Require(permission)(handler)
}

func (s *Service) space(r *http.Request) (Space, error) {
	if s.Space == nil {
		return Space{}, httpserver.New(http.StatusUnauthorized, "AUTHENTICATION_REQUIRED", "A valid session is required.")
	}
	space, ok := s.Space(r.Context())
	if !ok {
		return Space{}, httpserver.New(http.StatusUnauthorized, "AUTHENTICATION_REQUIRED", "A valid session is required.")
	}
	return space, nil
}

// settingsBody is GET /api/v1/processing/settings.
type settingsBody struct {
	Settings         Settings          `json:"settings"`
	Categories       []string          `json:"categories"`
	AIOnlyCategories []string          `json:"aiOnlyCategories"`
	Levels           []string          `json:"levels"`
	Engines          map[string]string `json:"engines"`
	PrivacyModel     PrivacyStatus     `json:"privacyModel"`
}

func (s *Service) getSettings(w http.ResponseWriter, r *http.Request) {
	space, err := s.space(r)
	if err != nil {
		httpserver.WriteError(w, r, s.Log, err)
		return
	}
	status := PrivacyStatus{Model: PrivacyModelID, MemoryMB: PrivacyModelMemoryMB, Status: "off"}
	if s.Privacy != nil {
		status = s.Privacy.Status(r.Context())
	}
	httpx.WriteJSON(w, http.StatusOK, settingsBody{
		Settings:         s.GetSettings(r.Context(), space.OwnerID),
		Categories:       Categories,
		AIOnlyCategories: AIOnlyCategories(),
		Levels:           Levels,
		Engines:          Engines,
		PrivacyModel:     status,
	})
}

func (s *Service) putSettings(w http.ResponseWriter, r *http.Request) {
	space, err := s.space(r)
	if err != nil {
		httpserver.WriteError(w, r, s.Log, err)
		return
	}
	var body Settings
	if err := decodeBody(w, r, &body); err != nil {
		httpserver.WriteError(w, r, s.Log, err)
		return
	}
	if err := validateSettings(body); err != nil {
		httpserver.WriteError(w, r, s.Log, err)
		return
	}
	saved, err := s.SaveSettings(r.Context(), space.OwnerID, body)
	if err != nil {
		httpserver.WriteError(w, r, s.Log, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, struct {
		Settings Settings `json:"settings"`
	}{Settings: saved})
}

// previewRequest is what the settings page may send.
type previewRequest struct {
	Text     *string          `json:"text"`
	Role     *string          `json:"role"`
	Messages []map[string]any `json:"messages"`
	Settings *settingsPayload `json:"settings"`
}

type settingsPayload struct {
	Privacy     *privacyPayload     `json:"privacy"`
	Compression *compressionPayload `json:"compression"`
}

type privacyPayload struct {
	Enabled     bool              `json:"enabled"`
	Modes       map[string]string `json:"modes"`
	CustomTerms []string          `json:"customTerms"`
	AllowTerms  []string          `json:"allowTerms"`
	AI          *AIEnabled        `json:"ai"`
}

type compressionPayload struct {
	Level        string          `json:"level"`
	Engines      map[string]bool `json:"engines"`
	MinTokens    int             `json:"minTokens"`
	TargetTokens *int            `json:"targetTokens"`
	KeepRecent   int             `json:"keepRecent"`
}

func (s *Service) postPreview(w http.ResponseWriter, r *http.Request) {
	space, err := s.space(r)
	if err != nil {
		httpserver.WriteError(w, r, s.Log, err)
		return
	}
	var body previewRequest
	if err := decodeBody(w, r, &body); err != nil {
		httpserver.WriteError(w, r, s.Log, err)
		return
	}
	options := PreviewOptions{
		Messages: body.Messages,
		CanEdit:  space.Permissions["processing.edit"],
	}
	if body.Text != nil {
		options.Text = *body.Text
	}
	if body.Role != nil {
		options.Role = *body.Role
	}
	if body.Settings != nil {
		draft, err := body.Settings.settings()
		if err != nil {
			httpserver.WriteError(w, r, s.Log, err)
			return
		}
		options.Settings = &draft
	}
	result, err := s.Preview(r.Context(), space.OwnerID, options)
	if err != nil {
		httpserver.WriteError(w, r, s.Log, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, result)
}

// Stats is the 30-day summary GET /api/v1/processing/stats answers.
type Stats struct {
	Days               int `json:"days"`
	Requests           int `json:"requests"`
	ProtectedRequests  int `json:"protectedRequests"`
	Redacted           int `json:"redacted"`
	CompressedRequests int `json:"compressedRequests"`
	SavedTokens        int `json:"savedTokens"`
}

func (s *Service) getStats(w http.ResponseWriter, r *http.Request) {
	space, err := s.space(r)
	if err != nil {
		httpserver.WriteError(w, r, s.Log, err)
		return
	}
	body := Stats{Days: 30}
	if s.StatsQuery != nil {
		now := nowMillis()
		if s.Now != nil {
			now = s.Now()
		}
		stats, err := s.StatsQuery(r.Context(), space.OwnerID, now-30*86_400_000)
		if err != nil {
			httpserver.WriteError(w, r, s.Log, err)
			return
		}
		stats.Days = 30
		body = stats
	}
	httpx.WriteJSON(w, http.StatusOK, body)
}

// (body.Settings is the draft the settings page previews)
func (p *settingsPayload) settings() (Settings, error) {
	draft := DefaultSettings()
	if p.Privacy != nil {
		draft.Privacy.Enabled = p.Privacy.Enabled
		if p.Privacy.Modes != nil {
			draft.Privacy.Modes = p.Privacy.Modes
		}
		if p.Privacy.CustomTerms != nil {
			draft.Privacy.CustomTerms = p.Privacy.CustomTerms
		}
		if p.Privacy.AllowTerms != nil {
			draft.Privacy.AllowTerms = p.Privacy.AllowTerms
		}
		if p.Privacy.AI != nil {
			draft.Privacy.AI = *p.Privacy.AI
		}
	}
	if p.Compression != nil {
		draft.Compression = CompressionDefaults{
			Level:        p.Compression.Level,
			Engines:      p.Compression.Engines,
			MinTokens:    p.Compression.MinTokens,
			TargetTokens: p.Compression.TargetTokens,
			KeepRecent:   p.Compression.KeepRecent,
		}
	}
	if err := validateSettings(draft); err != nil {
		return Settings{}, err
	}
	return draft, nil
}

// validateSettings checks a settings body.
func validateSettings(settings Settings) error {
	invalid := func(message string) error {
		return httpserver.New(http.StatusBadRequest, "VALIDATION_ERROR", message)
	}
	for category, mode := range settings.Privacy.Modes {
		if !KnownCategory(category) {
			return invalid("Unknown category")
		}
		if mode != "off" && mode != "restore" && mode != "hide" {
			return invalid("Invalid enum value")
		}
	}
	if len(settings.Privacy.CustomTerms) > 500 {
		return invalid("Array must contain at most 500 element(s)")
	}
	for _, term := range append(append([]string{}, settings.Privacy.CustomTerms...), settings.Privacy.AllowTerms...) {
		trimmed := strings.TrimSpace(term)
		if len([]rune(trimmed)) < 2 || len([]rune(trimmed)) > 200 {
			return invalid("String must contain at least 2 character(s)")
		}
	}
	if len(settings.Privacy.AllowTerms) > 500 {
		return invalid("Array must contain at most 500 element(s)")
	}
	if levelIndex(settings.Compression.Level) < 0 {
		return invalid("Invalid enum value")
	}
	for name := range settings.Compression.Engines {
		if _, ok := Engines[name]; !ok {
			return invalid("Unknown engine")
		}
	}
	if settings.Compression.MinTokens < 0 || settings.Compression.MinTokens > 1_000_000 {
		return invalid("Number must be less than or equal to 1000000")
	}
	if settings.Compression.TargetTokens != nil {
		if *settings.Compression.TargetTokens < 500 || *settings.Compression.TargetTokens > 2_000_000 {
			return invalid("Number must be between 500 and 2000000")
		}
	}
	if settings.Compression.KeepRecent < 1 || settings.Compression.KeepRecent > 50 {
		return invalid("Number must be between 1 and 50")
	}
	return nil
}

// decodeBody reads a JSON body of at most the management limit, answering 413
// for a larger one and 400 for anything malformed.
func decodeBody(w http.ResponseWriter, r *http.Request, dst any) error {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, httpserver.DefaultJSONBodyLimit))
	if err := decoder.Decode(dst); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return httpserver.New(http.StatusRequestEntityTooLarge, "BODY_TOO_LARGE", "The request body is too large.")
		}
		return httpserver.New(http.StatusBadRequest, "VALIDATION_ERROR", "Invalid request body")
	}
	return nil
}

// nowMillis is Date.now().
func nowMillis() int64 { return time.Now().UnixMilli() }
