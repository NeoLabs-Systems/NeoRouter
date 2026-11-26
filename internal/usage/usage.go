// Package usage owns NeoRouter's request log: what every routed request cost,
// how long it took, and the aggregates the activity, health and learning views
// are built from. Only metadata is stored, never prompts, completions or keys.
package usage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/dbx"
	"modernc.org/sqlite"
)

// Range windows the activity views offer.
var rangeMS = map[string]int64{
	"24h": 86_400_000,
	"7d":  7 * 86_400_000,
	"30d": 30 * 86_400_000,
}

// Entry is one finished request as the log stores it. Everything is metadata:
// tokens, cost, timings and the trail of attempts, never a prompt or a secret.
type Entry struct {
	ID                     string
	UserID                 string
	RouterKeyID            *string
	CreatedAt              int64
	APIFormat              string
	RequestedModel         string
	ModelRef               *int64
	ModelLabel             *string
	AccountID              *string
	Status                 string
	HTTPStatus             *int64
	ErrorCode              *string
	ErrorMessage           *string
	InputTokens            int64
	OutputTokens           int64
	CostUSD                float64
	IsFree                 bool
	LatencyMS              *int64
	TTFTMS                 *int64
	Attempts               int64
	Stream                 bool
	TrailJSON              string
	RedactedCount          int64
	CompressionSavedTokens int64
	Capability             string
	Units                  float64
	Task                   *string
	QueuedMS               int64
	KeptPrivate            *string
	RoutingGroup           *string
	CachedInputTokens      int64
	CacheCreationTokens    int64
}

// Service reads and writes the request log.
type Service struct {
	DB  *dbx.DB
	Now func() time.Time
	Log *slog.Logger

	// ProviderName names the provider behind an account definition
	// (internal/catalog provider identity); nil falls back to the model label.
	ProviderName func(definitionID, slug, label, baseURL string) string
	// Routing is the narrow surface consumed from internal/routing.
	Routing Routing
	// Catalog lists one kind of model a person can route to (internal/catalog
	// ListForUser); includeUnavailable keeps the models switched off right now.
	Catalog func(ctx context.Context, userID, capability string, includeUnavailable bool) ([]CatalogEntry, error)
	// AllowanceUsedUp reports whether a provider error only says the plan's
	// included usage is used up (internal/providers).
	AllowanceUsedUp func(message string) bool
	// CapacityView and QuotasView render the quota views these routes serve
	// (internal/quota); nil leaves those routes reporting empty.
	CapacityView func(userID string, now time.Time) (any, error)
	QuotasView   func(userID string, now time.Time) (any, error)
	// Require wraps a handler with the permission check (internal/teams).
	Require func(permission string) func(http.Handler) http.Handler
	// Space resolves the request's space (internal/teams).
	Space func(ctx context.Context) (Space, bool)

	healthMu    sync.Mutex
	healthCache map[string]healthCached
}

// healthCached is one person's connection health, kept for a few minutes.
type healthCached struct {
	at     int64
	health *Health
}

func (s *Service) cachedHealth(userID string, now int64) (*Health, bool) {
	s.healthMu.Lock()
	defer s.healthMu.Unlock()
	cached, ok := s.healthCache[userID]
	if !ok || now-cached.at >= healthCacheMS {
		return nil, false
	}
	return cached.health, true
}

func (s *Service) storeHealth(userID string, now int64, health *Health) {
	s.healthMu.Lock()
	defer s.healthMu.Unlock()
	s.healthCache[userID] = healthCached{at: now, health: health}
}

// Space is the request's resolved space (internal/teams).
type Space struct {
	OwnerID string
	ActorID string
}

// New builds the service.
func New(db *dbx.DB) *Service {
	return &Service{DB: db, Now: time.Now, healthCache: map[string]healthCached{}}
}

// SetClock replaces the clock (tests).
func (s *Service) SetClock(now func() time.Time) {
	if now != nil {
		s.Now = now
	}
}

// rangeStart is where a range begins; an unknown range means 24h.
func rangeStart(name string, now time.Time) int64 {
	window, ok := rangeMS[name]
	if !ok {
		window = rangeMS["24h"]
	}
	return now.UnixMilli() - window
}

const insertSQL = `INSERT INTO requests (id, user_id, router_key_id, created_at, api_format, requested_model, model_ref, model_label,
	account_id, status, http_status, error_code, error_message, input_tokens, output_tokens, cost_usd, is_free, latency_ms, ttft_ms,
	attempts, stream, trail_json, saved_usd, redacted_count, compression_saved_tokens, capability, units, task, queued_ms, kept_private,
	routing_group, cached_input_tokens, cache_creation_tokens)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

func boolInt(v bool) int64 {
	if v {
		return 1
	}
	return 0
}

func stringOrNil(v *string) any {
	if v == nil {
		return nil
	}
	return *v
}

// Record writes one request. A model or connection removed while the request
// ran (a pool partner left) is recorded without the link to it, rather than not
// at all.
func (s *Service) Record(entry Entry) error {
	if err := s.insert(entry); err != nil {
		if !isForeignKeyError(err) || (entry.ModelRef == nil && entry.AccountID == nil) {
			return err
		}
		entry.ModelRef = nil
		entry.AccountID = nil
		if err := s.insert(entry); err != nil {
			return err
		}
	}
	return nil
}

func isForeignKeyError(err error) bool {
	var sqliteErr *sqlite.Error
	if errors.As(err, &sqliteErr) {
		return sqliteErr.Code() == 787 // SQLITE_CONSTRAINT_FOREIGNKEY
	}
	return strings.Contains(err.Error(), "FOREIGN KEY constraint failed")
}

func (s *Service) insert(entry Entry) error {
	saved, err := s.savedUSD(entry)
	if err != nil {
		return err
	}
	capability := entry.Capability
	if capability == "" {
		capability = "chat"
	}
	trail := entry.TrailJSON
	if trail == "" {
		trail = "[]"
	}
	_, err = s.DB.W.ExecContext(context.Background(), insertSQL,
		entry.ID, entry.UserID, stringOrNil(entry.RouterKeyID), entry.CreatedAt, entry.APIFormat, entry.RequestedModel,
		entry.ModelRef, stringOrNil(entry.ModelLabel), stringOrNil(entry.AccountID), entry.Status, entry.HTTPStatus,
		stringOrNil(entry.ErrorCode), stringOrNil(entry.ErrorMessage), entry.InputTokens, entry.OutputTokens,
		entry.CostUSD, boolInt(entry.IsFree), entry.LatencyMS, entry.TTFTMS, entry.Attempts, boolInt(entry.Stream),
		trail, saved, entry.RedactedCount, entry.CompressionSavedTokens, capability, entry.Units, stringOrNil(entry.Task),
		entry.QueuedMS, stringOrNil(entry.KeptPrivate), stringOrNil(entry.RoutingGroup), entry.CachedInputTokens,
		entry.CacheCreationTokens)
	if err != nil {
		return fmt.Errorf("usage: record request: %w", err)
	}
	return nil
}

// savedUSD is what a free answer would have cost at the model's market price:
// the median paid price of the same model across providers.
func (s *Service) savedUSD(entry Entry) (float64, error) {
	if !entry.IsFree || entry.Status != "success" || entry.ModelRef == nil {
		return 0, nil
	}
	var priceIn, priceOut sql.NullFloat64
	err := s.DB.R.QueryRowContext(context.Background(), `SELECT md.price_in, md.price_out FROM models m
		JOIN model_metadata md ON md.canonical_id = m.canonical_id WHERE m.id = ?`, *entry.ModelRef).Scan(&priceIn, &priceOut)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, nil
		}
		return 0, fmt.Errorf("usage: read market price: %w", err)
	}
	return (float64(entry.InputTokens)*nullFloat(priceIn) + float64(entry.OutputTokens)*nullFloat(priceOut)) / 1e6, nil
}

func nullFloat(v sql.NullFloat64) float64 {
	if !v.Valid {
		return 0
	}
	return v.Float64
}

func floatPtr(v float64) *float64 { return &v }

// Prune drops requests older than the retention window.
func (s *Service) Prune(retentionDays int, now time.Time) error {
	if _, err := s.DB.W.ExecContext(context.Background(), `DELETE FROM requests WHERE created_at < ?`,
		now.UnixMilli()-int64(retentionDays)*86_400_000); err != nil {
		return fmt.Errorf("usage: prune requests: %w", err)
	}
	return nil
}
