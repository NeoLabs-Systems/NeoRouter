package discovery

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/dbx"
)

// News is the free-model news feed: a persistent detection event per model, so a
// feed that restarts, disappears for a while or reorders does not invent news.
type News struct {
	DB         *dbx.DB
	Benchmarks *Benchmarks
	Catalog    Catalog
	// Now overrides the clock in tests.
	Now func() time.Time
}

func (n *News) now() time.Time {
	if n.Now != nil {
		return n.Now()
	}
	return time.Now()
}

// Observe is news_service.js observe: called after a metadata refresh, not by
// page reads. It records the free models at or above the median score of their
// capability and marks the rest unavailable.
func (n *News) Observe(ctx context.Context) error {
	updatedAt := n.Catalog.DiscoverySnapshot().FetchedAt["openrouter"]
	sourceAt, err := parseISOMillis(updatedAt)
	if err != nil {
		return nil // a source that was never fetched has no detection time yet
	}
	free := n.Benchmarks.FreeListings()
	candidates := n.Benchmarks.FreeCandidates()

	byCapability := make(map[string][]float64)
	for _, model := range candidates {
		byCapability[model.Capability] = append(byCapability[model.Capability], model.Score)
	}
	threshold := make(map[string]float64, len(byCapability))
	for capability, scores := range byCapability {
		sort.Float64s(scores)
		threshold[capability] = scores[int(ceilHalf(float64(len(scores)-1)*NewsQuantile))]
	}

	if n.DB == nil {
		return fmt.Errorf("discovery: observe news: no database")
	}
	now := n.now().UnixMilli()
	tx, err := n.DB.W.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("discovery: observe news: begin: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE discovery_news SET available = 0`); err != nil {
		return fmt.Errorf("discovery: observe news: reset availability: %w", err)
	}
	for _, listing := range free.All() {
		id := "chat:" + n.Catalog.BenchmarkIdentity(listing.ID, "chat")
		if _, err := tx.ExecContext(ctx,
			`UPDATE discovery_news SET available = 1, last_seen_at = ? WHERE id = ?`, sourceAt, id); err != nil {
			return fmt.Errorf("discovery: observe news: mark available: %w", err)
		}
	}
	for _, model := range candidates {
		if model.Score < threshold[model.Capability] {
			continue
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO discovery_news
				(id, canonical_id, capability, model_id, name, score, source_count, context_window,
				 detected_at, last_seen_at, available)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1)
			ON CONFLICT(id) DO UPDATE SET name = excluded.name, model_id = excluded.model_id,
				score = excluded.score, source_count = excluded.source_count,
				context_window = excluded.context_window,
				last_seen_at = excluded.last_seen_at, available = 1`,
			model.ID, model.CanonicalID, model.Capability, model.Listing.ID, model.Name,
			model.Score, model.SourceCount, model.Listing.ContextWindow, now, sourceAt); err != nil {
			return fmt.Errorf("discovery: observe news: upsert %s: %w", model.ID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("discovery: observe news: commit: %w", err)
	}
	return nil
}

// ceilHalf is Math.ceil, which for the non-negative values here is a plain
// round-up.
func ceilHalf(value float64) float64 {
	whole := float64(int64(value))
	if value > whole {
		return whole + 1
	}
	return whole
}

// NewsItem is one detected free model.
type NewsItem struct {
	ID            string  `json:"id"`
	ModelID       string  `json:"modelId"`
	Name          string  `json:"name"`
	Capability    string  `json:"capability"`
	Score         float64 `json:"score"`
	SourceCount   int     `json:"sourceCount"`
	ContextWindow *int    `json:"contextWindow"`
	DetectedAt    int64   `json:"detectedAt"`
	LastSeenAt    int64   `json:"lastSeenAt"`
	Available     bool    `json:"available"`
	URL           string  `json:"url"`
}

// NewsPage is the body of GET /api/v1/discovery/news.
type NewsPage struct {
	Items     []NewsItem `json:"items"`
	Total     int        `json:"total"`
	Offset    int        `json:"offset"`
	Limit     int        `json:"limit"`
	AsOf      int64      `json:"asOf"`
	UpdatedAt *string    `json:"updatedAt"`
}

// NewsOptions are the filters of the news page.
type NewsOptions struct {
	Query  string
	Offset int
	Limit  int
	// AsOf is the newest detection time the page shows; nil means now.
	AsOf *int64
}

// List is news_service.js list. The search is literal: instr() keeps '%' and '_'
// from a search box out of the query.
func (n *News) List(ctx context.Context, opts NewsOptions) (NewsPage, error) {
	limit := opts.Limit
	if limit == 0 {
		limit = PageSize
	}
	asOf := n.now().UnixMilli()
	if opts.AsOf != nil {
		asOf = *opts.AsOf
	}
	search := strings.ToLower(strings.TrimSpace(opts.Query))
	if n.DB == nil {
		return NewsPage{}, fmt.Errorf("discovery: list news: no database")
	}
	const where = `WHERE detected_at <= ? AND (? = '' OR instr(lower(name || ' ' || model_id), ?) > 0)`
	var total int
	if err := n.DB.R.QueryRowContext(ctx,
		`SELECT COUNT(*) count FROM discovery_news `+where, asOf, search, search).Scan(&total); err != nil {
		return NewsPage{}, fmt.Errorf("discovery: count news: %w", err)
	}
	rows, err := n.DB.R.QueryContext(ctx,
		`SELECT id, canonical_id, capability, model_id, name, score, source_count, context_window,
			detected_at, last_seen_at, available FROM discovery_news `+where+
			` ORDER BY detected_at DESC, id LIMIT ? OFFSET ?`,
		asOf, search, search, limit, opts.Offset)
	if err != nil {
		return NewsPage{}, fmt.Errorf("discovery: list news: %w", err)
	}
	defer rows.Close()
	items := make([]NewsItem, 0, limit)
	for rows.Next() {
		var item NewsItem
		var canonicalID string
		var window sql.NullInt64
		var available int
		if err := rows.Scan(&item.ID, &canonicalID, &item.Capability, &item.ModelID, &item.Name,
			&item.Score, &item.SourceCount, &window, &item.DetectedAt, &item.LastSeenAt, &available); err != nil {
			return NewsPage{}, fmt.Errorf("discovery: read news: %w", err)
		}
		if window.Valid {
			value := int(window.Int64)
			item.ContextWindow = &value
		}
		item.Available = available != 0
		item.URL = "https://openrouter.ai/" + encodeURIComponentPath(item.ModelID)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return NewsPage{}, fmt.Errorf("discovery: read news: %w", err)
	}
	fetched := n.Catalog.DiscoverySnapshot().FetchedAt["openrouter"]
	var updatedAt *string
	if fetched != "" {
		updatedAt = &fetched
	}
	return NewsPage{
		Items: items, Total: total, Offset: opts.Offset, Limit: limit,
		AsOf: asOf, UpdatedAt: updatedAt,
	}, nil
}

// encodeURIComponentPath is JavaScript's
// encodeURIComponent(id).replace(/%2F/g, '/'), which is how the OpenRouter link
// keeps its vendor path.
func encodeURIComponentPath(value string) string {
	var builder strings.Builder
	for _, b := range []byte(value) {
		if isURIComponentByte(b) {
			builder.WriteByte(b)
			continue
		}
		if b == '/' {
			builder.WriteByte('/')
			continue
		}
		builder.WriteString("%")
		builder.WriteString(strings.ToUpper(fmt.Sprintf("%02x", b)))
	}
	return builder.String()
}

// isURIComponentByte is the unreserved set of encodeURIComponent.
func isURIComponentByte(b byte) bool {
	switch {
	case b >= 'a' && b <= 'z', b >= 'A' && b <= 'Z', b >= '0' && b <= '9':
		return true
	}
	return strings.IndexByte("-_.!~*'()", b) >= 0
}

// parseISOMillis reads a timestamp the metadata store saved. The sources record
// ISO strings; a number is accepted too, like Date.parse.
func parseISOMillis(value string) (int64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, fmt.Errorf("discovery: empty timestamp")
	}
	if millis, ok := parseMillisNumber(value); ok {
		return millis, nil
	}
	stamp, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return 0, err
	}
	return stamp.UnixMilli(), nil
}

// parseMillisNumber reads a bare epoch-milliseconds value.
func parseMillisNumber(value string) (int64, bool) {
	var millis int64
	for _, r := range value {
		if r < '0' || r > '9' {
			return 0, false
		}
		millis = millis*10 + int64(r-'0')
	}
	if millis == 0 {
		return 0, false
	}
	return millis, true
}
