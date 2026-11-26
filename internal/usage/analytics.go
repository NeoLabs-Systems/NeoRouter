package usage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Group is one row of the analytics breakdowns.
type Group struct {
	Requests          int64    `json:"requests"`
	Successes         int64    `json:"successes"`
	Errors            int64    `json:"errors"`
	InputTokens       int64    `json:"inputTokens"`
	OutputTokens      int64    `json:"outputTokens"`
	CachedInputTokens int64    `json:"cachedInputTokens"`
	CacheHits         int64    `json:"cacheHits"`
	CostUSD           float64  `json:"costUsd"`
	SavedUSD          float64  `json:"savedUsd"`
	FreeRequests      int64    `json:"freeRequests"`
	Fallbacks         int64    `json:"fallbacks"`
	CacheHitRate      *float64 `json:"cacheHitRate"`
	SuccessRate       *float64 `json:"successRate"`
	LatencyP50        *int64   `json:"latencyP50"`
	LatencyP95        *int64   `json:"latencyP95"`
	TTFTP50           *int64   `json:"ttftP50"`
}

// accum is a group still being summed.
type accum struct {
	Group
	latencies []int64
	ttfts     []int64
}

// AnalyticsSeries is one bucket of the activity series.
type AnalyticsSeries struct {
	Bucket int64 `json:"bucket"`
	Group
}

// ProviderRow is one provider of the by-provider breakdown.
type ProviderRow struct {
	Key string `json:"key"`
	Group
	AccountCount       int      `json:"accountCount"`
	RequestsPerAccount *float64 `json:"requestsPerAccount"`
}

// NamedRow is one model of the by-model breakdown, which carries its label.
type NamedRow struct {
	Key  string `json:"key"`
	Name string `json:"name"`
	Group
}

// KeyRow is one entry of a breakdown keyed by a plain label.
type KeyRow struct {
	Key string `json:"key"`
	Group
}

// CountRow is a counted label: an error code or an attempt outcome.
type CountRow struct {
	Key   string `json:"key"`
	Count int64  `json:"count"`
}

// HistogramBucket is one latency histogram bucket; UpToMS is nil for the open
// ended last bucket.
type HistogramBucket struct {
	UpToMS *int64 `json:"upToMs"`
	Count  int64  `json:"count"`
}

// Analytics is everything the activity page charts, computed from request
// metadata.
type Analytics struct {
	Range             string            `json:"range"`
	BucketMS          int64             `json:"bucketMs"`
	Totals            Group             `json:"totals"`
	Series            []AnalyticsSeries `json:"series"`
	ByProvider        []ProviderRow     `json:"byProvider"`
	ByProviderAverage []ProviderRow     `json:"byProviderAverage"`
	ByRoutingGroup    []KeyRow          `json:"byRoutingGroup"`
	ByModel           []NamedRow        `json:"byModel"`
	ByKey             []KeyRow          `json:"byKey"`
	ByFormat          []KeyRow          `json:"byFormat"`
	ByCapability      []KeyRow          `json:"byCapability"`
	Errors            []CountRow        `json:"errors"`
	AttemptOutcomes   []CountRow        `json:"attemptOutcomes"`
	LatencyHistogram  []HistogramBucket `json:"latencyHistogram"`
}

// histogramEdges are the latency buckets of the activity page; the last one is
// open ended.
var histogramEdges = []int64{250, 500, 1000, 2000, 5000, 10000, 30000}

// requestRow is one logged request as the analytics queries read it.
type requestRow struct {
	CachedInputTokens sql.NullInt64
	RoutingGroup      sql.NullString
	CreatedAt         int64
	Status            string
	ErrorCode         sql.NullString
	InputTokens       int64
	OutputTokens      int64
	CostUSD           sql.NullFloat64
	SavedUSD          sql.NullFloat64
	IsFree            int64
	LatencyMS         sql.NullInt64
	TTFTMS            sql.NullInt64
	Attempts          int64
	Stream            int64
	APIFormat         string
	ModelLabel        sql.NullString
	AccountID         sql.NullString
	RouterKeyID       sql.NullString
	TrailJSON         string
	Capability        sql.NullString
	KeyName           sql.NullString
	DefinitionID      sql.NullString
	AccountSlug       sql.NullString
	AccountLabel      sql.NullString
	AccountBaseURL    sql.NullString
	CanonicalID       sql.NullString
}

// addRow counts one request into the group.
func (a *accum) addRow(row *requestRow) {
	a.Requests++
	if row.Status == "success" {
		a.Successes++
	} else {
		a.Errors++
	}
	a.InputTokens += row.InputTokens
	a.OutputTokens += row.OutputTokens
	a.CachedInputTokens += row.CachedInputTokens.Int64
	if row.Status == "success" && row.CachedInputTokens.Int64 > 0 {
		a.CacheHits++
	}
	a.CostUSD += nullFloat(row.CostUSD)
	a.SavedUSD += nullFloat(row.SavedUSD)
	if row.IsFree != 0 && row.Status == "success" {
		a.FreeRequests++
	}
	if row.Attempts > 1 {
		a.Fallbacks++
	}
	if row.Status == "success" && row.LatencyMS.Valid {
		a.latencies = append(a.latencies, row.LatencyMS.Int64)
	}
	if row.TTFTMS.Valid {
		a.ttfts = append(a.ttfts, row.TTFTMS.Int64)
	}
}

// finish sorts the timings and works out the rates.
func (a *accum) finish() Group {
	sort.Slice(a.latencies, func(i, j int) bool { return a.latencies[i] < a.latencies[j] })
	sort.Slice(a.ttfts, func(i, j int) bool { return a.ttfts[i] < a.ttfts[j] })
	group := a.Group
	if a.Successes > 0 {
		rate := float64(a.CacheHits) / float64(a.Successes)
		group.CacheHitRate = &rate
	}
	if a.Requests > 0 {
		rate := float64(a.Successes) / float64(a.Requests)
		group.SuccessRate = &rate
	}
	group.LatencyP50 = percentile(a.latencies, 0.5)
	group.LatencyP95 = percentile(a.latencies, 0.95)
	group.TTFTP50 = percentile(a.ttfts, 0.5)
	return group
}

// grouper counts rows under a key each, keeping the largest groups.
type grouper struct {
	keyOf    func(*requestRow) (string, bool)
	nameOf   func(*requestRow) string
	memberOf func(*requestRow) (string, bool)
	order    []string
	groups   map[string]*accum
	names    map[string]string
	members  map[string]map[string]bool
	missing  map[string]bool
}

func newGrouper(keyOf func(*requestRow) (string, bool)) *grouper {
	return &grouper{keyOf: keyOf, groups: map[string]*accum{}, names: map[string]string{}, members: map[string]map[string]bool{}, missing: map[string]bool{}}
}

func (g *grouper) add(row *requestRow) {
	key, ok := g.keyOf(row)
	if !ok {
		return
	}
	group, seen := g.groups[key]
	if !seen {
		group = &accum{}
		g.groups[key] = group
		g.order = append(g.order, key)
	}
	if g.nameOf != nil {
		if _, named := g.names[key]; !named {
			g.names[key] = g.nameOf(row)
		}
	}
	if g.memberOf != nil {
		if g.members[key] == nil {
			g.members[key] = map[string]bool{}
		}
		if member, ok := g.memberOf(row); ok {
			g.members[key][member] = true
		} else {
			g.members[key][""] = true
			g.missing[key] = true
		}
	}
	group.addRow(row)
}

// top is the largest limit groups, by requests or by requests per account.
func (g *grouper) top(limit int, average bool) []ProviderRow {
	out := make([]ProviderRow, 0, len(g.order))
	for _, key := range g.order {
		group := g.groups[key]
		row := ProviderRow{Key: key, Group: group.finish()}
		if g.memberOf != nil {
			row.AccountCount = len(g.members[key])
			if g.members[key][""] {
				row.AccountCount--
			}
			// Deleted or missing account links cannot produce a reliable divisor.
			if !g.missing[key] && len(g.members[key]) > 0 {
				per := float64(group.Requests) / float64(len(g.members[key]))
				row.RequestsPerAccount = &per
			}
		}
		out = append(out, row)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if average {
			left, right := -1.0, -1.0
			if out[i].RequestsPerAccount != nil {
				left = *out[i].RequestsPerAccount
			}
			if out[j].RequestsPerAccount != nil {
				right = *out[j].RequestsPerAccount
			}
			if left != right {
				return left > right
			}
		}
		return out[i].Requests > out[j].Requests
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// AnalyticsOf is the activity breakdown of a person over a range. keyIds limits
// the numbers to a few router keys; an empty list means all of them.
func (s *Service) AnalyticsOf(userID, rangeName string, now time.Time, keyIDs []string) (*Analytics, error) {
	ctx := context.Background()
	since := rangeStart(rangeName, now)
	bucketMS := int64(86_400_000)
	switch rangeName {
	case "24h":
		bucketMS = 3_600_000
	case "7d":
		bucketMS = 6 * 3_600_000
	}
	filter, ids := keyFilter(keyIDs)
	rows, err := s.DB.R.QueryContext(ctx, `SELECT r.cached_input_tokens, r.routing_group, r.created_at, r.status, r.error_code,
		r.input_tokens, r.output_tokens, r.cost_usd, r.saved_usd, r.is_free, r.latency_ms, r.ttft_ms, r.attempts, r.stream,
		r.api_format, r.model_label, r.account_id, r.router_key_id, r.trail_json, r.capability, k.name key_name,
		a.definition_id, a.slug account_slug, a.label account_label, a.base_url account_base_url, m.canonical_id
		FROM requests r LEFT JOIN router_keys k ON k.id = r.router_key_id LEFT JOIN provider_accounts a ON a.id = r.account_id
		LEFT JOIN models m ON m.id = r.model_ref
		WHERE r.user_id = ? AND r.created_at >= ?`+filter+` ORDER BY r.created_at`, append([]any{userID, since}, ids...)...)
	if err != nil {
		return nil, fmt.Errorf("usage: read analytics: %w", err)
	}
	defer rows.Close()

	buckets := map[int64]*accum{}
	var bucketOrder []int64
	for bucket := since/bucketMS*bucketMS + bucketMS; bucket <= now.UnixMilli(); bucket += bucketMS {
		buckets[bucket] = &accum{}
		bucketOrder = append(bucketOrder, bucket)
	}
	total := &accum{}
	outcomes := map[string]int64{}
	var outcomeOrder []string
	histogram := make([]int64, len(histogramEdges)+1)
	byProvider := newGrouper(func(row *requestRow) (string, bool) { return s.providerOf(row), s.providerOf(row) != "" })
	byProvider.memberOf = func(row *requestRow) (string, bool) {
		if !row.AccountID.Valid || row.AccountID.String == "" {
			return "", false
		}
		return row.AccountID.String, true
	}
	byRoutingGroup := newGrouper(func(row *requestRow) (string, bool) {
		return row.RoutingGroup.String, row.RoutingGroup.Valid
	})
	byModel := newGrouper(func(row *requestRow) (string, bool) { return modelKeyOf(row) })
	byModel.nameOf = modelNameOf
	byKey := newGrouper(func(row *requestRow) (string, bool) {
		if row.KeyName.Valid && row.KeyName.String != "" {
			return row.KeyName.String, true
		}
		return row.RouterKeyID.String, row.RouterKeyID.Valid
	})
	byFormat := newGrouper(func(row *requestRow) (string, bool) {
		format := row.APIFormat
		if row.Stream != 0 {
			format += " · stream"
		}
		return format, true
	})
	byCapability := newGrouper(func(row *requestRow) (string, bool) {
		if row.Capability.Valid && row.Capability.String != "" {
			return row.Capability.String, true
		}
		return "chat", true
	})
	errorsGroup := newGrouper(func(row *requestRow) (string, bool) {
		if row.Status != "error" {
			return "", false
		}
		if row.ErrorCode.Valid && row.ErrorCode.String != "" {
			return row.ErrorCode.String, true
		}
		return "unknown", true
	})
	groups := []*grouper{byProvider, byRoutingGroup, byModel, byKey, byFormat, byCapability, errorsGroup}

	for rows.Next() {
		var row requestRow
		if err := rows.Scan(&row.CachedInputTokens, &row.RoutingGroup, &row.CreatedAt, &row.Status, &row.ErrorCode,
			&row.InputTokens, &row.OutputTokens, &row.CostUSD, &row.SavedUSD, &row.IsFree, &row.LatencyMS, &row.TTFTMS,
			&row.Attempts, &row.Stream, &row.APIFormat, &row.ModelLabel, &row.AccountID, &row.RouterKeyID, &row.TrailJSON,
			&row.Capability, &row.KeyName, &row.DefinitionID, &row.AccountSlug, &row.AccountLabel, &row.AccountBaseURL,
			&row.CanonicalID); err != nil {
			return nil, fmt.Errorf("usage: read analytics: %w", err)
		}
		total.addRow(&row)
		for _, group := range groups {
			group.add(&row)
		}
		bucket := row.CreatedAt / bucketMS * bucketMS
		if buckets[bucket] == nil {
			buckets[bucket] = &accum{}
			bucketOrder = append(bucketOrder, bucket)
		}
		buckets[bucket].addRow(&row)
		for _, outcome := range trailOutcomes(row.TrailJSON) {
			if _, seen := outcomes[outcome]; !seen {
				outcomeOrder = append(outcomeOrder, outcome)
			}
			outcomes[outcome]++
		}
		if row.Status == "success" && row.LatencyMS.Valid {
			histogram[histogramIndex(row.LatencyMS.Int64)]++
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("usage: read analytics: %w", err)
	}

	analytics := &Analytics{
		Range:            rangeName,
		BucketMS:         bucketMS,
		Totals:           total.finish(),
		Series:           []AnalyticsSeries{},
		ByProvider:       []ProviderRow{},
		ByRoutingGroup:   []KeyRow{},
		ByModel:          []NamedRow{},
		ByKey:            []KeyRow{},
		ByFormat:         []KeyRow{},
		ByCapability:     []KeyRow{},
		Errors:           []CountRow{},
		AttemptOutcomes:  []CountRow{},
		LatencyHistogram: latencyHistogram(histogram),
	}
	sort.Slice(bucketOrder, func(i, j int) bool { return bucketOrder[i] < bucketOrder[j] })
	for _, bucket := range bucketOrder {
		analytics.Series = append(analytics.Series, AnalyticsSeries{Bucket: bucket, Group: buckets[bucket].finish()})
	}
	providers := byProvider.top(20, false)
	analytics.ByProvider = providers
	analytics.ByProviderAverage = byProvider.top(20, true)
	for _, row := range byRoutingGroup.top(100, false) {
		analytics.ByRoutingGroup = append(analytics.ByRoutingGroup, KeyRow{Key: row.Key, Group: row.Group})
	}
	analytics.ByModel = byModel.topNamed(15)
	for _, row := range byKey.top(20, false) {
		analytics.ByKey = append(analytics.ByKey, KeyRow{Key: row.Key, Group: row.Group})
	}
	for _, row := range byFormat.top(10, false) {
		analytics.ByFormat = append(analytics.ByFormat, KeyRow{Key: row.Key, Group: row.Group})
	}
	for _, row := range byCapability.top(10, false) {
		analytics.ByCapability = append(analytics.ByCapability, KeyRow{Key: row.Key, Group: row.Group})
	}
	for _, row := range errorsGroup.top(10, false) {
		analytics.Errors = append(analytics.Errors, CountRow{Key: row.Key, Count: row.Requests})
	}
	for _, outcome := range outcomeOrder {
		analytics.AttemptOutcomes = append(analytics.AttemptOutcomes, CountRow{Key: outcome, Count: outcomes[outcome]})
	}
	sort.SliceStable(analytics.AttemptOutcomes, func(i, j int) bool {
		return analytics.AttemptOutcomes[i].Count > analytics.AttemptOutcomes[j].Count
	})
	return analytics, nil
}

// topNamed is top() for a breakdown whose rows carry a readable name.
func (g *grouper) topNamed(limit int) []NamedRow {
	out := []NamedRow{}
	for _, row := range g.top(limit, false) {
		out = append(out, NamedRow{Key: row.Key, Name: g.names[row.Key], Group: row.Group})
	}
	return out
}

// providerOf names the provider a request went to: accounts of one provider
// count as that provider.
func (s *Service) providerOf(row *requestRow) string {
	if row.DefinitionID.Valid && row.DefinitionID.String != "" {
		if s.ProviderName != nil {
			return s.ProviderName(row.DefinitionID.String, row.AccountSlug.String, row.AccountLabel.String, row.AccountBaseURL.String)
		}
		return row.DefinitionID.String
	}
	if row.ModelLabel.Valid {
		if index := strings.IndexByte(row.ModelLabel.String, '/'); index >= 0 {
			return row.ModelLabel.String[:index]
		}
	}
	return ""
}

// modelKeyOf is one model, whichever provider or account served it.
func modelKeyOf(row *requestRow) (string, bool) {
	if row.CanonicalID.Valid && row.CanonicalID.String != "" {
		return row.CanonicalID.String, true
	}
	if row.ModelLabel.Valid {
		return row.ModelLabel.String, true
	}
	return "", false
}

// modelNameOf is the readable name of a model: its label without the provider
// prefix and the ":free" marker.
func modelNameOf(row *requestRow) string {
	if !row.ModelLabel.Valid || row.ModelLabel.String == "" {
		return row.CanonicalID.String
	}
	label := row.ModelLabel.String
	if index := strings.LastIndexByte(label, '/'); index >= 0 {
		label = label[index+1:]
	}
	return strings.TrimSuffix(label, ":free")
}

// keyFilter limits a query to some router keys.
func keyFilter(keyIDs []string) (string, []any) {
	ids := make([]any, 0, len(keyIDs))
	for _, id := range keyIDs {
		if id == "" {
			continue
		}
		if len(ids) == 100 {
			break
		}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return "", nil
	}
	filter := " AND r.router_key_id IN (?" + strings.Repeat(", ?", len(ids)-1) + ")"
	return filter, ids
}

// trailOutcomes is the outcomes of the attempts in a stored trail.
func trailOutcomes(trailJSON string) []string {
	if trailJSON == "" {
		return nil
	}
	var steps []struct {
		Outcome string `json:"outcome"`
	}
	if err := json.Unmarshal([]byte(trailJSON), &steps); err != nil {
		return nil
	}
	out := make([]string, 0, len(steps))
	for _, step := range steps {
		if step.Outcome != "" {
			out = append(out, step.Outcome)
		}
	}
	return out
}

// histogramIndex is the first bucket a latency fits in, the last one when it
// fits in none.
func histogramIndex(latency int64) int {
	for index, edge := range histogramEdges {
		if latency <= edge {
			return index
		}
	}
	return len(histogramEdges)
}

func latencyHistogram(counts []int64) []HistogramBucket {
	out := make([]HistogramBucket, 0, len(counts))
	for index, count := range counts {
		bucket := HistogramBucket{Count: count}
		if index < len(histogramEdges) {
			bucket.UpToMS = &histogramEdges[index]
		}
		out = append(out, bucket)
	}
	return out
}
