package discovery

import (
	"context"
	"database/sql"
	"encoding/json"
	"reflect"
	"strings"
	"sync"

	"github.com/NeoLabs-Systems/neolabs-go/dbx"
)

// Benchmarks is the discovery leaderboard: the aggregated measurements plus the
// free and price annotations of the catalog, and the longest-lived page is its
// cached snapshot.
type Benchmarks struct {
	// DB is the install's database; the free listing reads the catalog's models.
	DB *dbx.DB
	// Catalog is the metadata projection the measurements come from.
	Catalog Catalog

	mu    sync.Mutex
	cache *cacheEntry
}

// cacheEntry is the last aggregation, kept while its inputs are unchanged.
type cacheEntry struct {
	version string
	refs    [4]any
	data    Aggregate
}

// Snapshot is benchmark_service.js snapshot(): the aggregate, rebuilt only when
// the fetchedAt stamp or one of the four source lists changed.
func (b *Benchmarks) Snapshot() Aggregate {
	current := b.Catalog.DiscoverySnapshot()
	raw, _ := json.Marshal(current.FetchedAt)
	version := string(raw)
	refs := [4]any{current.ArtificialAnalysis, current.OpenRouterBenchmarks, current.ArtificialAnalysisMedia, current.MTEB}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.cache != nil && b.cache.version == version && sameRefs(b.cache.refs, refs) {
		return b.cache.data
	}
	data := Build(current, b.Catalog.BenchmarkIdentity)
	b.cache = &cacheEntry{version: version, refs: refs, data: data}
	return data
}

func sameRefs(left, right [4]any) bool {
	for i := range left {
		if !reflect.DeepEqual(left[i], right[i]) {
			return false
		}
	}
	return true
}

// Listing is one free chat model, keyed by the benchmark id it is offered under.
type Listing struct {
	ID            string
	Name          string
	ContextWindow *int
	IsFree        bool
}

// Listings are free chat models in the order they were found: OpenRouter first,
// then the install's own catalog.
type Listings struct {
	byID  map[string]Listing
	order []string
}

// Has reports whether the id has a free listing.
func (l Listings) Has(id string) bool {
	_, ok := l.byID[id]
	return ok
}

// All returns the listings in order.
func (l Listings) All() []Listing {
	out := make([]Listing, 0, len(l.order))
	for _, id := range l.order {
		out = append(out, l.byID[id])
	}
	return out
}

// FreeListings is benchmark_service.js freeListings: free chat models keyed by
// benchmark id. OpenRouter counts only with every published charge confirmed
// zero, because missing prices are unknown rather than free; connected providers
// count when their own catalog marks the model free.
func (b *Benchmarks) FreeListings() Listings {
	found := Listings{byID: make(map[string]Listing)}
	add := func(model Listing) {
		id := "chat:" + b.Catalog.BenchmarkIdentity(model.ID, "chat")
		if _, ok := found.byID[id]; ok {
			return
		}
		found.byID[id] = model
		found.order = append(found.order, id)
	}
	for _, model := range b.Catalog.DiscoverySnapshot().OpenRouter {
		if model.IsFree {
			add(Listing{ID: model.ID, ContextWindow: model.ContextWindow, IsFree: true})
		}
	}
	for _, row := range b.catalogFreeChatModels() {
		add(row)
	}
	return found
}

// catalogFreeChatModels reads the install's own free chat models; a database
// without a catalog yet simply contributes nothing.
func (b *Benchmarks) catalogFreeChatModels() []Listing {
	if b.DB == nil {
		return nil
	}
	rows, err := b.DB.R.QueryContext(context.Background(), `
		SELECT model_id id, COALESCE(display_name, model_id) name, MAX(context_window) contextWindow
		FROM models WHERE is_free = 1 AND available = 1 AND enabled = 1 AND capability = 'chat'
		GROUP BY model_id`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []Listing
	for rows.Next() {
		var row Listing
		var window sql.NullInt64
		if err := rows.Scan(&row.ID, &row.Name, &window); err != nil {
			return out
		}
		if window.Valid {
			value := int(window.Int64)
			row.ContextWindow = &value
		}
		row.IsFree = true
		out = append(out, row)
	}
	return out
}

// Candidate is a free model with benchmark evidence: its own results, or for a
// variant with none yet the base model's score scaled down by what variants
// usually give up.
type Candidate struct {
	ID          string
	CanonicalID string
	Name        string
	Capability  string
	Score       float64
	SourceCount int
	Listing     Listing
	// Estimated marks a score copied from the measured base model.
	Estimated bool
}

// FreeCandidates is benchmark_service.js freeCandidates.
func (b *Benchmarks) FreeCandidates() []Candidate {
	byID := make(map[string]Model, len(b.Snapshot().Models))
	for _, model := range b.Snapshot().Models {
		byID[model.ID] = model
	}
	var out []Candidate
	for _, listing := range b.FreeListings().All() {
		id := "chat:" + b.Catalog.BenchmarkIdentity(listing.ID, "chat")
		if own, ok := byID[id]; ok {
			out = append(out, Candidate{
				ID: own.ID, CanonicalID: own.CanonicalID, Name: own.Name,
				Capability: own.Capability, Score: own.Score, SourceCount: own.SourceCount,
				Listing: listing,
			})
			continue
		}
		base, share, ok := b.Catalog.VariantBase(listing.ID)
		if !ok {
			continue
		}
		measured, ok := byID["chat:"+b.Catalog.BenchmarkIdentity(base, "chat")]
		if !ok {
			continue
		}
		name := listing.Name
		if name == "" {
			name = listing.ID
		}
		out = append(out, Candidate{
			ID: id, CanonicalID: stripChatPrefix(id), Name: name, Capability: measured.Capability,
			Score: measured.Score * share, SourceCount: measured.SourceCount,
			Listing: listing, Estimated: true,
		})
	}
	return out
}

// stripChatPrefix is id.slice('chat:'.length).
func stripChatPrefix(id string) string {
	const prefix = "chat:"
	if len(id) >= len(prefix) {
		return id[len(prefix):]
	}
	return id
}

// hasSource reports whether the model has a score from this source.
func hasSource(model Model, sourceID string) bool {
	for _, score := range model.SourceScores {
		if score.SourceID == sourceID {
			return true
		}
	}
	return false
}

// priceOf is the map lookup the JS code writes as priced.get(id) ?? null.
func priceOf(prices map[string]float64, id string) *float64 {
	if price, ok := prices[id]; ok {
		return &price
	}
	return nil
}

// Prices is benchmark_service.js prices: the cheapest known blended price per
// million tokens (3 input : 1 output) per benchmark id, for the cost chart.
func (b *Benchmarks) Prices() map[string]float64 {
	found := make(map[string]float64)
	for _, model := range b.Catalog.DiscoverySnapshot().OpenRouter {
		if model.PriceIn == nil || model.PriceOut == nil {
			continue
		}
		blended := (3*(*model.PriceIn) + *model.PriceOut) / 4
		if blended <= 0 {
			continue
		}
		id := "chat:" + b.Catalog.BenchmarkIdentity(model.ID, "chat")
		if previous, ok := found[id]; !ok || previous > blended {
			found[id] = blended
		}
	}
	return found
}

// ListedModel is a model as the page reports it: the aggregate row plus what the
// catalog knows about its availability and price.
type ListedModel struct {
	Model
	IsFree bool     `json:"isFree"`
	Price  *float64 `json:"price"`
	Vendor *string  `json:"vendor"`
}

// Page is the body of GET /api/v1/discovery/benchmarks.
type Page struct {
	Models       []ListedModel `json:"models"`
	Total        int           `json:"total"`
	Offset       int           `json:"offset"`
	Limit        int           `json:"limit"`
	Comparison   []ListedModel `json:"comparison"`
	Sources      []Source      `json:"sources"`
	Capabilities []string      `json:"capabilities"`
}

// ListOptions are the filters of the benchmarks page.
type ListOptions struct {
	Query      string
	Capability string
	Source     string
	Free       bool
	Offset     int
	Limit      int
	Compare    []string
}

// List is benchmark_service.js list.
func (b *Benchmarks) List(opts ListOptions) Page {
	limit := opts.Limit
	if limit == 0 {
		limit = PageSize
	}
	data := b.Snapshot()
	freeModels := b.FreeListings()
	priced := b.Prices()
	present := func(model Model) ListedModel {
		listed := ListedModel{Model: model, IsFree: freeModels.Has(model.ID), Price: priceOf(priced, model.ID)}
		if at := strings.IndexByte(model.CanonicalID, '/'); at >= 0 {
			vendor := model.CanonicalID[:at]
			listed.Vendor = &vendor
		}
		return listed
	}
	search := strings.ToLower(strings.TrimSpace(opts.Query))
	filtered := make([]Model, 0, len(data.Models))
	for _, model := range data.Models {
		if opts.Capability != "" && model.Capability != opts.Capability {
			continue
		}
		if opts.Source != "" && !hasSource(model, opts.Source) {
			continue
		}
		if opts.Free && !freeModels.Has(model.ID) {
			continue
		}
		if search != "" && !strings.Contains(strings.ToLower(model.Name+" "+model.CanonicalID), search) {
			continue
		}
		filtered = append(filtered, model)
	}
	total := len(filtered)
	start := opts.Offset
	if start > total {
		start = total
	}
	end := start + limit
	if end > total {
		end = total
	}
	page := filtered[start:end]
	models := make([]ListedModel, 0, len(page))
	for _, model := range page {
		models = append(models, present(model))
	}
	comparison := make([]ListedModel, 0, 2)
	for _, id := range opts.Compare {
		if len(comparison) == 2 {
			break
		}
		for _, model := range data.Models {
			if model.ID == id {
				comparison = append(comparison, present(model))
				break
			}
		}
	}
	capabilities := make([]string, 0, 4)
	seen := make(map[string]bool, 4)
	for _, model := range data.Models {
		if !seen[model.Capability] {
			seen[model.Capability] = true
			capabilities = append(capabilities, model.Capability)
		}
	}
	return Page{
		Models: models, Total: total, Offset: opts.Offset, Limit: limit,
		Comparison: comparison, Sources: data.Sources, Capabilities: capabilities,
	}
}
