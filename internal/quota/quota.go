// Package quota owns NeoRouter's live usage windows and the provider limits
// routing watches: usage counted in one-minute buckets per scope, the limits
// learned from provider headers and refusals, the "used up until later" marks a
// refusal leaves behind, and the capacity those limits add up to.
//
// A scope is either one model ("m:<ref>") or a pool shared by several models of
// one account ("p:<accountId>"). Buckets live in memory and are flushed to
// SQLite; limits and observations are written through.
package quota

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/dbx"
)

// The four windows a scope is limited on.
const (
	RPM = "rpm"
	RPD = "rpd"
	TPM = "tpm"
	TPD = "tpd"
)

// Metrics are the windows a scope is limited on, in the order the API reports
// them.
var Metrics = []string{RPM, RPD, TPM, TPD}

// Where a limit came from, strongest first.
const (
	SourceManual  = "manual"
	SourceHeader  = "header"
	SourceLearned = "learned"
	SourceCurated = "curated"
	SourceError   = "error"
)

var sourceRank = map[string]int{
	SourceManual:  3,
	SourceHeader:  2,
	SourceLearned: 1,
	SourceCurated: 0,
}

// Scope kinds.
const (
	KindModel = "model"
	KindPool  = "pool"
)

const (
	// MinuteMS is the width of one usage bucket.
	MinuteMS = int64(60_000)
	// DayMS is the daily window two of the metrics work on.
	DayMS = int64(86_400_000)

	dayMinutes = 1440

	headroomThreshold = 0.2
	headroomFloor     = 0.1
	// learnedLimitTTLMS drops a limit learned from one refusal after a week
	// without new evidence: providers raise limits and change plans.
	learnedLimitTTLMS = int64(7 * 86_400_000)
	// DefaultPlanRateAlpha is routing's pacing_policy.planRateAlpha, the weight
	// a new plan observation gets when a request's cost is estimated.
	DefaultPlanRateAlpha = 0.2
)

// Entry is one catalog entry (a model on one account) as internal/catalog
// presents it. The quota maths reads the identity fields; the rest is what the
// quotas view shows and what capacity counts.
type Entry struct {
	// Ref is the model's id: quota scopes key on it ("m:<ref>").
	Ref          int64
	ID           string
	Name         string
	AccountID    string
	DefinitionID string
	ModelID      string
	IsFree       bool
	IsPlan       bool
	IsFlatRate   bool
	Enabled      bool
	// AccountEnabled and Available are the account's and the model's own flags.
	AccountEnabled bool
	Available      bool
	// AllowancePace and Slots are the JSON internal/routing renders for a
	// prepaid plan's allowance pacing and a flat-rate account's slot use, nil
	// when they do not apply.
	AllowancePace json.RawMessage
	Slots         json.RawMessage
}

// Limits is a provider definition's curated limits ("limits").
type Limits struct {
	Pool      string             // "account" when one allowance is shared
	PoolMatch string             // models the allowance is shared across (regex)
	Defaults  map[string]float64 // default limit per metric
	Models    []ModelLimit       // per-model overrides
	Account   map[string]float64 // limits of the shared account allowance
}

// ModelLimit is one curated rule: models matching Match get Values.
type ModelLimit struct {
	Match  string
	Values map[string]float64
}

// HeaderConfig describes how a provider's rate-limit headers are read (the
// definition's "quota").
type HeaderConfig struct {
	// HeaderPeriods maps "requests"/"tokens" to "minute", "day" or "hour".
	HeaderPeriods map[string]string
	// PlainHeaderMetric is the metric of a bare x-ratelimit-limit.
	PlainHeaderMetric string
}

// Definition is the quota shape of a provider definition.
type Definition struct {
	Limits Limits
	Header HeaderConfig
}

// DefinitionSource is the narrow surface internal/quota consumes from
// internal/providers: the curated limits of a definition, nil when unknown.
type DefinitionSource interface {
	QuotaDefinition(definitionID string) *Definition
}

// CatalogSource is the narrow surface internal/quota consumes from
// internal/catalog: the entries one person can route to, one kind of model at a
// time (catalog.Service.ListForUser).
type CatalogSource interface {
	ListForUser(ctx context.Context, userID, capability string, includeUnavailable bool) ([]Entry, error)
}

// Scope is one place usage counts.
type Scope struct {
	Key       string
	Kind      string // KindModel or KindPool
	AccountID string // models only
	Curated   map[string]float64
}

// ModelScopeKey is the scope of one model.
func ModelScopeKey(ref int64) string { return "m:" + strconv.FormatInt(ref, 10) }

// PoolScopeKey is the scope of an account's shared allowance.
func PoolScopeKey(accountID string) string { return "p:" + accountID }

// PlanScopeKey is the scope of an account's prepaid plan.
func PlanScopeKey(accountID string) string { return "a:" + accountID }

// LimitInfo is one effective limit and where it came from.
type LimitInfo struct {
	Value  float64
	Source string
}

// Usage is a scope's sliding-window estimate.
type Usage struct {
	RPM float64 `json:"rpm"`
	TPM float64 `json:"tpm"`
	RPD float64 `json:"rpd"`
	TPD float64 `json:"tpd"`
}

func (u Usage) metric(metric string) float64 {
	switch metric {
	case RPM:
		return u.RPM
	case RPD:
		return u.RPD
	case TPM:
		return u.TPM
	default:
		return u.TPD
	}
}

// Totals is requests and tokens over a span of buckets.
type Totals struct {
	Requests int64 `json:"requests"`
	Tokens   int64 `json:"tokens"`
}

// Observation is what a provider said about one metric: a header reading or a
// probe result. Limit, Remaining and ResetAt are nil when unknown.
type Observation struct {
	Metric    string
	Limit     *float64
	Remaining *float64
	ResetAt   *int64
}

// Window is one daily allowance as dailyWindow reports it.
type Window struct {
	StartAt   int64   `json:"startAt"`
	ResetAt   *int64  `json:"resetAt"`
	Rolling   bool    `json:"rolling"`
	Metric    string  `json:"metric"`
	Scope     string  `json:"scope"`
	Limit     float64 `json:"limit"`
	Used      float64 `json:"used"`
	Remaining float64 `json:"remaining"`
	Source    string  `json:"source"`
	Recent    float64 `json:"recent"`
	Expiring  float64 `json:"expiring"`
}

// Detail is one window of one scope in a routing decision.
type Detail struct {
	Scope     string   `json:"scope"`
	Kind      string   `json:"kind"`
	Metric    string   `json:"metric"`
	Limit     *float64 `json:"limit"`
	Used      float64  `json:"used"`
	Remaining float64  `json:"remaining"`
	Source    string   `json:"source"`
	ResetAt   *int64   `json:"resetAt"`
}

// Blocked names the window a request does not fit in.
type Blocked struct {
	Metric  string `json:"metric"`
	Scope   string `json:"scope"`
	ResetAt *int64 `json:"resetAt"`
}

// Evaluation is a request's quota picture: whether any window blocks it, how
// much headroom the tightest one leaves, and every window it was read from.
type Evaluation struct {
	Blocked  *Blocked `json:"blocked"`
	Headroom float64  `json:"headroom"`
	Factor   float64  `json:"factor"`
	Details  []Detail `json:"details"`
}

// State is what was observed about one scope's metric.
type State struct {
	Scope       string   `json:"scope"`
	Metric      string   `json:"metric"`
	Limit       *float64 `json:"limit"`
	Remaining   *float64 `json:"remaining"`
	ResetAt     *int64   `json:"resetAt"`
	Source      string   `json:"source"`
	ObservedAt  int64    `json:"observedAt"`
	LocalUsed   *float64 `json:"localUsed"`
	RequestCost *float64 `json:"requestCost"`
	// Renewed marks a window whose reset passed, shown as a fresh allowance.
	Renewed bool `json:"renewed,omitempty"`
}

type bucket struct {
	requests int64
	tokens   int64
}

type limitRecord struct {
	value     float64
	source    string
	updatedAt int64
}

type stateRecord struct {
	limit       *float64
	remaining   *float64
	resetAt     *int64
	source      string
	observedAt  int64
	localUsed   *float64
	requestCost *float64
}

// Service is the quota store. Every method is safe for concurrent use.
type Service struct {
	db      *dbx.DB
	now     func() time.Time
	defs    DefinitionSource
	catalog CatalogSource
	limits  *AccountLimits
	// PlanRateAlpha is routing's pacing_policy.planRateAlpha; zero means
	// DefaultPlanRateAlpha.
	PlanRateAlpha float64

	mu      sync.Mutex
	loaded  bool
	buckets map[string]map[int64]*bucket
	dirty   map[string]struct{}
	records map[string]*limitRecord
	states  map[string]*stateRecord
}

// New builds the service. defs may be nil, in which case no provider publishes
// curated limits.
func New(db *dbx.DB, defs DefinitionSource) *Service {
	return &Service{
		db:      db,
		defs:    defs,
		now:     time.Now,
		limits:  newAccountLimits(db),
		buckets: map[string]map[int64]*bucket{},
		dirty:   map[string]struct{}{},
		records: map[string]*limitRecord{},
		states:  map[string]*stateRecord{},
	}
}

// SetClock replaces the clock (tests).
func (s *Service) SetClock(now func() time.Time) {
	if now != nil {
		s.now = now
	}
}

// Now is the service clock.
func (s *Service) Now() time.Time { return s.now() }

// AccountLimits is the stated limits of provider accounts.
func (s *Service) AccountLimits() *AccountLimits { return s.limits }

// SetCatalog supplies the catalog entries the capacity maths walks
// (internal/catalog). Without it the capacity view reports nothing.
func (s *Service) SetCatalog(catalog CatalogSource) { s.catalog = catalog }

// wrapRead names a failed read.
func wrapRead(action string, err error) error {
	return fmt.Errorf("quota: read %s: %w", action, err)
}

// jsRound is rounding where halves go up, not away from zero.
func jsRound(v float64) float64 { return math.Floor(v + 0.5) }

func minuteOf(ms int64) int64 { return ms / MinuteMS }

func nullFloat(v sql.NullFloat64) *float64 {
	if !v.Valid {
		return nil
	}
	out := v.Float64
	return &out
}

func nullInt(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	out := v.Int64
	return &out
}

func floatPtr(v float64) *float64 { return &v }
func intPtr(v int64) *int64       { return &v }

func deref(v *float64) float64 {
	if v == nil {
		return 0
	}
	return *v
}

// load reads the stored buckets, limits and observations once. Buckets older
// than the daily window are dropped.
func (s *Service) load(now int64) error {
	if s.loaded {
		return nil
	}
	s.loaded = true
	ctx := context.Background()
	if _, err := s.db.W.ExecContext(ctx, `DELETE FROM quota_buckets WHERE minute < ?`, minuteOf(now)-dayMinutes-1); err != nil {
		return fmt.Errorf("quota: prune buckets: %w", err)
	}
	rows, err := s.db.R.QueryContext(ctx, `SELECT scope_key, minute, requests, tokens FROM quota_buckets`)
	if err != nil {
		return fmt.Errorf("quota: read buckets: %w", err)
	}
	for rows.Next() {
		var scope string
		var minute, requests, tokens int64
		if err := rows.Scan(&scope, &minute, &requests, &tokens); err != nil {
			rows.Close()
			return fmt.Errorf("quota: read buckets: %w", err)
		}
		if s.buckets[scope] == nil {
			s.buckets[scope] = map[int64]*bucket{}
		}
		s.buckets[scope][minute] = &bucket{requests: requests, tokens: tokens}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("quota: read buckets: %w", err)
	}

	limitRows, err := s.db.R.QueryContext(ctx, `SELECT scope_key, metric, value, source, updated_at FROM quota_limits`)
	if err != nil {
		return fmt.Errorf("quota: read limits: %w", err)
	}
	for limitRows.Next() {
		var scope, metric, source string
		var value float64
		var updatedAt int64
		if err := limitRows.Scan(&scope, &metric, &value, &source, &updatedAt); err != nil {
			limitRows.Close()
			return fmt.Errorf("quota: read limits: %w", err)
		}
		s.records[scope+"|"+metric] = &limitRecord{value: value, source: source, updatedAt: updatedAt}
	}
	limitRows.Close()
	if err := limitRows.Err(); err != nil {
		return fmt.Errorf("quota: read limits: %w", err)
	}

	stateRows, err := s.db.R.QueryContext(ctx, `SELECT scope_key, metric, limit_value, remaining, reset_at, source, observed_at, local_used, request_cost FROM quota_state`)
	if err != nil {
		return fmt.Errorf("quota: read state: %w", err)
	}
	for stateRows.Next() {
		var scope, metric, source string
		var limit, remaining, localUsed, requestCost sql.NullFloat64
		var resetAt, observedAt sql.NullInt64
		if err := stateRows.Scan(&scope, &metric, &limit, &remaining, &resetAt, &source, &observedAt, &localUsed, &requestCost); err != nil {
			stateRows.Close()
			return fmt.Errorf("quota: read state: %w", err)
		}
		s.states[scope+"|"+metric] = &stateRecord{
			limit:       nullFloat(limit),
			remaining:   nullFloat(remaining),
			resetAt:     nullInt(resetAt),
			source:      source,
			observedAt:  observedAt.Int64,
			localUsed:   nullFloat(localUsed),
			requestCost: nullFloat(requestCost),
		}
	}
	stateRows.Close()
	if err := stateRows.Err(); err != nil {
		return fmt.Errorf("quota: read state: %w", err)
	}
	return nil
}

// Flush writes the dirty buckets and, when prune is set, drops expired ones.
func (s *Service) Flush(prune bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.flush(prune, s.now().UnixMilli())
}

func (s *Service) flush(prune bool, now int64) error {
	if !s.loaded || len(s.dirty) == 0 {
		return nil
	}
	ctx := context.Background()
	tx, err := s.db.W.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("quota: flush buckets: %w", err)
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO quota_buckets (scope_key, minute, requests, tokens) VALUES (?, ?, ?, ?)
		ON CONFLICT(scope_key, minute) DO UPDATE SET requests = excluded.requests, tokens = excluded.tokens`)
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("quota: flush buckets: %w", err)
	}
	for key := range s.dirty {
		scope, minuteText, ok := strings.Cut(key, "|")
		if !ok {
			continue
		}
		minute, err := strconv.ParseInt(minuteText, 10, 64)
		if err != nil {
			continue
		}
		b := s.buckets[scope][minute]
		if b == nil {
			continue
		}
		if _, err := stmt.ExecContext(ctx, scope, minute, b.requests, b.tokens); err != nil {
			stmt.Close()
			tx.Rollback()
			return fmt.Errorf("quota: flush buckets: %w", err)
		}
	}
	stmt.Close()
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("quota: flush buckets: %w", err)
	}
	clear(s.dirty)
	if !prune {
		return nil
	}
	s.pruneBuckets(now)
	return nil
}

func (s *Service) pruneBuckets(now int64) {
	cutoff := minuteOf(now) - dayMinutes - 1
	for _, minutes := range s.buckets {
		for minute := range minutes {
			if minute < cutoff {
				delete(minutes, minute)
			}
		}
	}
}

// AddUsage records requests and tokens a scope spent at now.
func (s *Service) AddUsage(scope string, requests, tokens int64, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.addUsage(scope, requests, tokens, now.UnixMilli())
}

func (s *Service) addUsage(scope string, requests, tokens, now int64) error {
	if err := s.load(now); err != nil {
		return err
	}
	minute := minuteOf(now)
	if s.buckets[scope] == nil {
		s.buckets[scope] = map[int64]*bucket{}
	}
	b := s.buckets[scope][minute]
	if b == nil {
		b = &bucket{}
		s.buckets[scope][minute] = b
	}
	b.requests += requests
	b.tokens += tokens
	s.dirty[fmt.Sprintf("%s|%d", scope, minute)] = struct{}{}
	return nil
}

// UsageOf is the sliding-window estimate of a scope: the current minute plus
// the overlapping share of the previous one, and the last day for the daily
// windows.
func (s *Service) UsageOf(scope string, now time.Time) (Usage, error) {
	return s.UsageOfMS(scope, now.UnixMilli())
}

// UsageOfMS is UsageOf for a caller that already holds epoch milliseconds.
func (s *Service) UsageOfMS(scope string, now int64) (Usage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.usageOf(scope, now)
}

func (s *Service) usageOf(scope string, now int64) (Usage, error) {
	var result Usage
	if err := s.load(now); err != nil {
		return result, err
	}
	minutes := s.buckets[scope]
	if minutes == nil {
		return result, nil
	}
	current := minuteOf(now)
	overlap := 1 - float64(now%MinuteMS)/float64(MinuteMS)
	for minute, b := range minutes {
		if minute > current-dayMinutes {
			result.RPD += float64(b.requests)
			result.TPD += float64(b.tokens)
		}
		if minute == current {
			result.RPM += float64(b.requests)
			result.TPM += float64(b.tokens)
		} else if minute == current-1 {
			result.RPM += float64(b.requests) * overlap
			result.TPM += float64(b.tokens) * overlap
		}
	}
	return result, nil
}

// UsageInLast is requests and tokens a scope used in the last minutes.
func (s *Service) UsageInLast(scope string, minutes int, now time.Time) (Totals, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	nowMS := now.UnixMilli()
	if err := s.load(nowMS); err != nil {
		return Totals{}, err
	}
	return s.usageInLast(scope, int64(minutes), nowMS), nil
}

func (s *Service) usageInLast(scope string, minutes, now int64) Totals {
	var result Totals
	current := minuteOf(now)
	from := current - minutes + 1
	for minute, b := range s.buckets[scope] {
		if minute >= from && minute <= current {
			result.Requests += b.requests
			result.Tokens += b.tokens
		}
	}
	return result
}

// UsageInRange is requests and tokens a scope used between two instants.
func (s *Service) UsageInRange(scope string, startAt, endAt time.Time) (Totals, error) {
	return s.UsageInRangeMS(scope, startAt.UnixMilli(), endAt.UnixMilli())
}

// UsageInRangeMS is UsageInRange in epoch milliseconds.
func (s *Service) UsageInRangeMS(scope string, startAt, endAt int64) (Totals, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.load(endAt); err != nil {
		return Totals{}, err
	}
	return s.usageInRange(scope, startAt, endAt), nil
}

func (s *Service) usageInRange(scope string, startAt, endAt int64) Totals {
	var result Totals
	from, to := minuteOf(startAt), minuteOf(endAt)
	for minute, b := range s.buckets[scope] {
		if minute < from || minute > to {
			continue
		}
		result.Requests += b.requests
		result.Tokens += b.tokens
	}
	return result
}

// NextReleaseAt is when one of a scope's buckets leaves a minutes-wide window,
// nil when nothing is about to expire.
func (s *Service) NextReleaseAt(scope string, minutes int, now time.Time) (*int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	nowMS := now.UnixMilli()
	if err := s.load(nowMS); err != nil {
		return nil, err
	}
	var next *int64
	for minute := range s.buckets[scope] {
		at := (minute + int64(minutes)) * MinuteMS
		if at <= nowMS {
			continue
		}
		if next == nil || at < *next {
			next = intPtr(at)
		}
	}
	return next, nil
}

// ScopesFor is where a model's usage counts, with the curated limits of each.
func (s *Service) ScopesFor(entry Entry) []Scope {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.scopesFor(entry)
}

func (s *Service) definition(definitionID string) *Definition {
	if s.defs == nil {
		return nil
	}
	return s.defs.QuotaDefinition(definitionID)
}

func (s *Service) scopesFor(entry Entry) []Scope {
	var spec Limits
	if def := s.definition(entry.DefinitionID); def != nil {
		spec = def.Limits
	}
	modelCurated := map[string]float64{}
	for metric, value := range spec.Defaults {
		modelCurated[metric] = value
	}
	for _, rule := range spec.Models {
		if !MatchesPattern(rule.Match, entry.ModelID) {
			continue
		}
		for metric, value := range rule.Values {
			modelCurated[metric] = value
		}
	}
	scopes := []Scope{{Key: ModelScopeKey(entry.Ref), AccountID: entry.AccountID, Curated: modelCurated, Kind: KindModel}}
	accountCurated := map[string]float64{}
	for metric, value := range spec.Account {
		accountCurated[metric] = value
	}
	stated := s.limits.Get(entry.AccountID)
	hasStated := false
	for _, metric := range Metrics {
		if stated[metric] != 0 {
			hasStated = true
			break
		}
	}
	if (MatchesPattern(spec.PoolMatch, entry.ModelID) && (len(accountCurated) > 0 || spec.Pool == "account")) || hasStated {
		scopes = append(scopes, Scope{Key: PoolScopeKey(entry.AccountID), Curated: accountCurated, Kind: KindPool})
	}
	return scopes
}

// DailyLimits is a scope's daily allowance, nil when unknown.
func (s *Service) DailyLimits(scope Scope, now time.Time) (rpd, tpd *float64, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	nowMS := now.UnixMilli()
	if err := s.load(nowMS); err != nil {
		return nil, nil, err
	}
	known := s.effectiveLimits(scope)
	daily := func(metric string) *float64 {
		if info, ok := known[metric]; ok {
			return floatPtr(info.Value)
		}
		if state := s.liveState(scope.Key, metric, nowMS); state != nil {
			return state.limit
		}
		return nil
	}
	return daily(RPD), daily(TPD), nil
}

// DailyWindow is one daily allowance with its used and remaining share, nil
// when the limit is not positive.
func (s *Service) DailyWindow(scope Scope, metric string, now time.Time) (*Window, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dailyWindow(scope, metric, now.UnixMilli())
}

func (s *Service) dailyWindow(scope Scope, metric string, now int64) (*Window, error) {
	if err := s.load(now); err != nil {
		return nil, err
	}
	bounds := s.dailyBounds(scope.Key, metric, now, nil)
	state := s.liveState(scope.Key, metric, now)
	limit, ok := s.effectiveLimits(scope)[metric]
	if !ok {
		if state == nil || state.limit == nil {
			return nil, nil
		}
		limit = LimitInfo{Value: *state.limit}
	}
	if limit.Value <= 0 {
		return nil, nil
	}
	local := s.rangeField(scope.Key, bounds.startAt, now, metric)
	afterObservation := 0.0
	if state != nil && state.localUsed != nil {
		afterObservation = math.Max(0, local-*state.localUsed)
	}
	remoteRemaining := math.Inf(1)
	if state != nil && state.remaining != nil {
		remoteRemaining = math.Max(0, *state.remaining-afterObservation)
	}
	remaining := math.Max(0, math.Min(limit.Value-local, remoteRemaining))
	source := limit.Source
	if source == "" && state != nil {
		source = state.source
	}
	expiring := 0.0
	if bounds.rolling {
		expiring = s.rangeField(scope.Key, now-DayMS, now-(dayMinutes-60)*MinuteMS, metric)
	}
	return &Window{
		StartAt:   bounds.startAt,
		ResetAt:   bounds.resetAt,
		Rolling:   bounds.rolling,
		Metric:    metric,
		Scope:     scope.Key,
		Limit:     limit.Value,
		Used:      limit.Value - remaining,
		Remaining: remaining,
		Source:    source,
		Recent:    s.rangeField(scope.Key, now-60*MinuteMS, now, metric),
		Expiring:  expiring,
	}, nil
}

// rangeField is requests or tokens a scope used between two instants.
func (s *Service) rangeField(scope string, startAt, endAt int64, metric string) float64 {
	totals := s.usageInRange(scope, startAt, endAt)
	if strings.HasPrefix(metric, "r") {
		return float64(totals.Requests)
	}
	return float64(totals.Tokens)
}

type bounds struct {
	startAt int64
	resetAt *int64
	rolling bool
}

// dailyBounds anchors a daily window to the provider's reset time when one is
// known, so yesterday's usage stays out of a renewed allowance.
func (s *Service) dailyBounds(scope, metric string, now int64, observation *stateRecord) bounds {
	state := observation
	if state == nil {
		state = s.states[scope+"|"+metric]
	}
	if state != nil && state.resetAt != nil && state.source != SourceError && *state.resetAt-state.observedAt <= DayMS {
		resetAt := *state.resetAt
		if resetAt <= now {
			resetAt += (floorDiv(now-resetAt, DayMS) + 1) * DayMS
		}
		return bounds{startAt: resetAt - DayMS, resetAt: intPtr(resetAt)}
	}
	return bounds{startAt: now - DayMS, rolling: true}
}

func floorDiv(a, b int64) int64 {
	q := a / b
	if a%b != 0 && (a < 0) != (b < 0) {
		q--
	}
	return q
}

// EffectiveLimits is the limit in force for each metric of a scope.
func (s *Service) EffectiveLimits(scope Scope) (map[string]LimitInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.load(s.now().UnixMilli()); err != nil {
		return nil, err
	}
	return s.effectiveLimits(scope), nil
}

func (s *Service) effectiveLimits(scope Scope) map[string]LimitInfo {
	result := map[string]LimitInfo{}
	// A limit a person stated for the account outranks everything inferred.
	var stated, accountOverride map[string]float64
	if scope.Kind == KindPool {
		stated = s.limits.Get(strings.TrimPrefix(scope.Key, "p:"))
	} else if scope.AccountID != "" {
		accountOverride = s.limits.Get(scope.AccountID)
	}
	now := s.now().UnixMilli()
	for _, metric := range Metrics {
		if stated[metric] != 0 {
			result[metric] = LimitInfo{Value: stated[metric], Source: SourceManual}
			continue
		}
		if stored := fresh(s.records[scope.Key+"|"+metric], now); stored != nil && !(accountOverride[metric] != 0 && stored.source == SourceLearned) {
			result[metric] = LimitInfo{Value: stored.value, Source: stored.source}
		} else if scope.Curated[metric] != 0 {
			result[metric] = LimitInfo{Value: scope.Curated[metric], Source: SourceCurated}
		}
	}
	return result
}

// fresh drops a learned limit that has not been confirmed for a week.
func fresh(record *limitRecord, now int64) *limitRecord {
	if record == nil {
		return nil
	}
	if record.source == SourceLearned && now-record.updatedAt > learnedLimitTTLMS {
		return nil
	}
	return record
}

// SetLimit records a limit for a scope's metric and writes it through.
func (s *Service) SetLimit(scope, metric string, value float64, source string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.setLimit(scope, metric, value, source)
}

func (s *Service) setLimit(scope, metric string, value float64, source string) error {
	now := s.now().UnixMilli()
	if err := s.load(now); err != nil {
		return err
	}
	key := scope + "|" + metric
	existing := fresh(s.records[key], now)
	// Learned limits only ever tighten; a fresher header or manual value wins.
	if existing != nil && source == SourceLearned && existing.value <= value {
		return nil
	}
	if existing != nil && sourceRank[existing.source] > sourceRank[source] {
		return nil
	}
	record := &limitRecord{value: math.Max(1, jsRound(value)), source: source, updatedAt: now}
	s.records[key] = record
	_, err := s.db.W.ExecContext(context.Background(), `INSERT INTO quota_limits (scope_key, metric, value, source, updated_at) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(scope_key, metric) DO UPDATE SET value = excluded.value, source = excluded.source, updated_at = excluded.updated_at`,
		scope, metric, record.value, source, record.updatedAt)
	if err != nil {
		return fmt.Errorf("quota: store limit: %w", err)
	}
	return nil
}

// ClearLimit forgets a scope's metric limit.
func (s *Service) ClearLimit(scope, metric string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.records, scope+"|"+metric)
	if _, err := s.db.W.ExecContext(context.Background(), `DELETE FROM quota_limits WHERE scope_key = ? AND metric = ?`, scope, metric); err != nil {
		return fmt.Errorf("quota: clear limit: %w", err)
	}
	return nil
}

// SetState records what a provider said about a scope's metric.
func (s *Service) SetState(scope, metric string, obs Observation, source string, now time.Time) error {
	return s.SetStateMS(scope, metric, obs, source, now.UnixMilli())
}

// SetStateMS is SetState in epoch milliseconds.
func (s *Service) SetStateMS(scope, metric string, obs Observation, source string, now int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.setState(scope, metric, obs, source, now)
}

func (s *Service) setState(scope, metric string, obs Observation, source string, now int64) error {
	if err := s.load(now); err != nil {
		return err
	}
	bounds := s.dailyBounds(scope, metric, now, &stateRecord{resetAt: obs.ResetAt, source: source, observedAt: now})
	isPlan := strings.HasPrefix(metric, "plan_")
	var localUsed *float64
	switch {
	case isPlan:
		totals := s.usageInLast(scope, dayMinutes, now)
		localUsed = floatPtr(float64(totals.Requests))
	case strings.HasSuffix(metric, "d"):
		localUsed = floatPtr(s.rangeField(scope, bounds.startAt, now, metric))
	}
	previous := s.states[scope+"|"+metric]
	var requestCost *float64
	if isPlan && previous != nil {
		requestCost = previous.requestCost
	}
	count := deref(localUsed)
	if previous != nil && previous.localUsed != nil {
		count -= *previous.localUsed
	}
	previousRemaining := obs.Remaining
	if previous != nil && previous.remaining != nil {
		previousRemaining = previous.remaining
	}
	spent := 0.0
	if previousRemaining != nil && obs.Remaining != nil {
		spent = *previousRemaining - *obs.Remaining
	}
	if isPlan && previous != nil && sameReset(previous.resetAt, obs.ResetAt) && count > 0 && spent > 0 && obs.Remaining != nil {
		sample := spent / count
		alpha := s.PlanRateAlpha
		if alpha == 0 {
			alpha = DefaultPlanRateAlpha
		}
		if requestCost == nil {
			requestCost = floatPtr(sample)
		} else {
			next := *requestCost*(1-alpha) + sample*alpha
			requestCost = &next
		}
	}
	record := &stateRecord{limit: obs.Limit, remaining: obs.Remaining, resetAt: obs.ResetAt, source: source, observedAt: now, localUsed: localUsed, requestCost: requestCost}
	if err := s.flush(false, now); err != nil {
		return err
	}
	s.states[scope+"|"+metric] = record
	_, err := s.db.W.ExecContext(context.Background(), `INSERT INTO quota_state (scope_key, metric, limit_value, remaining, reset_at, source, observed_at, local_used, request_cost)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT(scope_key, metric) DO UPDATE SET limit_value = excluded.limit_value,
		remaining = excluded.remaining, reset_at = excluded.reset_at, source = excluded.source, observed_at = excluded.observed_at,
		local_used = excluded.local_used, request_cost = excluded.request_cost`,
		scope, metric, obs.Limit, obs.Remaining, obs.ResetAt, source, now, localUsed, requestCost)
	if err != nil {
		return fmt.Errorf("quota: store state: %w", err)
	}
	return nil
}

func sameReset(a, b *int64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// liveState is an observation still worth trusting.
func (s *Service) liveState(scope, metric string, now int64) *stateRecord {
	state := s.states[scope+"|"+metric]
	if state == nil {
		return nil
	}
	if state.resetAt != nil && *state.resetAt <= now {
		return nil
	}
	// Without a reset time an observation is trusted for one window length.
	ttl := MinuteMS
	if strings.HasSuffix(metric, "d") {
		ttl = DayMS
	}
	if state.resetAt == nil && now-state.observedAt > ttl {
		return nil
	}
	return state
}

// ObserveHeaders records what a provider's rate-limit headers said and teaches
// the limits for future windows.
func (s *Service) ObserveHeaders(entry Entry, header map[string][]string, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	nowMS := now.UnixMilli()
	var cfg HeaderConfig
	shared := false
	if def := s.definition(entry.DefinitionID); def != nil {
		cfg = def.Header
		shared = def.Limits.Pool == "account"
	}
	observations := ParseRateLimitHeaders(header, cfg, now)
	if len(observations) == 0 {
		return nil
	}
	scopes := s.scopesFor(entry)
	target := scopes[0]
	if shared {
		for _, scope := range scopes {
			if scope.Kind == KindPool {
				target = scope
				break
			}
		}
	}
	for _, obs := range observations {
		if err := s.setState(target.Key, obs.Metric, obs, SourceHeader, nowMS); err != nil {
			return err
		}
		if obs.Limit != nil && *obs.Limit != 0 {
			if err := s.setLimit(target.Key, obs.Metric, *obs.Limit, SourceHeader); err != nil {
				return err
			}
		}
	}
	return nil
}

// LearnLimit records a limit read out of a provider's refusal.
func (s *Service) LearnLimit(entry Entry, learned Observation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if learned.Metric == "" || learned.Limit == nil || *learned.Limit == 0 {
		return nil
	}
	scopes := s.scopesFor(entry)
	target := scopes[0]
	if def := s.definition(entry.DefinitionID); def != nil && def.Limits.Pool == "account" {
		for _, scope := range scopes {
			if scope.Kind == KindPool {
				target = scope
				break
			}
		}
	}
	return s.setLimit(target.Key, learned.Metric, *learned.Limit, SourceLearned)
}

// MarkExhausted marks a scope as used up until resetAt (a 429 or quota error).
func (s *Service) MarkExhausted(entry Entry, metric string, resetAt *int64, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	nowMS := now.UnixMilli()
	scopes := s.scopesFor(entry)
	scope := scopes[0]
	if def := s.definition(entry.DefinitionID); def != nil && def.Limits.Pool == "account" {
		for _, candidate := range scopes {
			if candidate.Kind == KindPool {
				scope = candidate
				break
			}
		}
	}
	if err := s.load(nowMS); err != nil {
		return err
	}
	known := s.liveState(scope.Key, metric, nowMS)
	// Preserve an upstream reset instead of replacing it with a retry timer.
	precise := known != nil && known.resetAt != nil && known.source != SourceError
	var limit *float64
	if known != nil {
		limit = known.limit
	}
	source := SourceError
	reset := resetAt
	if precise {
		source = known.source
		reset = known.resetAt
	}
	zero := 0.0
	return s.setState(scope.Key, metric, Observation{Metric: metric, Limit: limit, Remaining: &zero, ResetAt: reset}, source, nowMS)
}

// ClearObservedExhaustion drops the "used up until later" marks that refusals
// left on an account and its models.
func (s *Service) ClearObservedExhaustion(accountID string, refs []int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	scopes := observedScopes(accountID, refs)
	for key, state := range s.states {
		scope, metric, _ := strings.Cut(key, "|")
		if state.source != SourceError || !scopes[scope] {
			continue
		}
		delete(s.states, key)
		if _, err := s.db.W.ExecContext(context.Background(), `DELETE FROM quota_state WHERE scope_key = ? AND metric = ? AND source = ?`, scope, metric, SourceError); err != nil {
			return fmt.Errorf("quota: clear exhaustion: %w", err)
		}
	}
	return nil
}

// ForgetObserved drops everything observed about an account's limits (headers,
// errors), keeping what a person stated.
func (s *Service) ForgetObserved(accountID string, refs []int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	scopes := observedScopes(accountID, refs)
	for key := range s.states {
		scope, metric, _ := strings.Cut(key, "|")
		if !scopes[scope] {
			continue
		}
		delete(s.states, key)
		if _, err := s.db.W.ExecContext(context.Background(), `DELETE FROM quota_state WHERE scope_key = ? AND metric = ?`, scope, metric); err != nil {
			return fmt.Errorf("quota: forget observations: %w", err)
		}
	}
	for key, record := range s.records {
		scope, metric, _ := strings.Cut(key, "|")
		if !scopes[scope] || record.source == SourceManual {
			continue
		}
		delete(s.records, key)
		if _, err := s.db.W.ExecContext(context.Background(), `DELETE FROM quota_limits WHERE scope_key = ? AND metric = ?`, scope, metric); err != nil {
			return fmt.Errorf("quota: forget limits: %w", err)
		}
	}
	return nil
}

func observedScopes(accountID string, refs []int64) map[string]bool {
	scopes := map[string]bool{PoolScopeKey(accountID): true, PlanScopeKey(accountID): true}
	for _, ref := range refs {
		scopes[ModelScopeKey(ref)] = true
	}
	return scopes
}

// Evaluate is the quota picture of a routing decision about entry.
func (s *Service) Evaluate(entry Entry, estimatedTokens float64, now time.Time) (Evaluation, error) {
	return s.EvaluateMS(entry, estimatedTokens, now.UnixMilli())
}

// EvaluateMS is Evaluate in epoch milliseconds.
func (s *Service) EvaluateMS(entry Entry, estimatedTokens float64, now int64) (Evaluation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.load(now); err != nil {
		return Evaluation{}, err
	}
	evaluation := Evaluation{Headroom: 1, Details: []Detail{}}
	for _, scope := range s.scopesFor(entry) {
		used, err := s.usageOf(scope.Key, now)
		if err != nil {
			return Evaluation{}, err
		}
		scopeLimits := s.effectiveLimits(scope)
		for _, metric := range Metrics {
			state := s.liveState(scope.Key, metric, now)
			var limit *float64
			if info, ok := scopeLimits[metric]; ok {
				limit = floatPtr(info.Value)
			} else if state != nil {
				limit = state.limit
			}
			var daily *Window
			if strings.HasSuffix(metric, "d") {
				daily, err = s.dailyWindow(scope, metric, now)
				if err != nil {
					return Evaluation{}, err
				}
			}
			var remaining *float64
			switch {
			case daily != nil:
				remaining = floatPtr(daily.Remaining)
			case limit != nil:
				remaining = floatPtr(*limit - used.metric(metric))
			}
			if daily == nil && state != nil && state.remaining != nil {
				if remaining == nil {
					remaining = floatPtr(*state.remaining)
				} else {
					remaining = floatPtr(math.Min(*remaining, *state.remaining))
				}
			}
			if remaining == nil {
				continue
			}
			need := estimatedTokens
			if strings.HasPrefix(metric, "r") {
				need = 1
			}
			resetAt := resetOf(daily, state)
			if *remaining < need && evaluation.Blocked == nil {
				evaluation.Blocked = &Blocked{Metric: metric, Scope: scope.Key, ResetAt: resetAt}
			}
			if limit != nil && *limit != 0 {
				evaluation.Headroom = math.Min(evaluation.Headroom, math.Max(0, *remaining) / *limit)
			}
			shownUsed := used.metric(metric)
			if daily != nil {
				shownUsed = daily.Used
			}
			source := ""
			if info, ok := scopeLimits[metric]; ok {
				source = info.Source
			}
			if source == "" && state != nil {
				source = state.source
			}
			evaluation.Details = append(evaluation.Details, Detail{
				Scope:     scope.Key,
				Kind:      scope.Kind,
				Metric:    metric,
				Limit:     limit,
				Used:      jsRound(shownUsed),
				Remaining: math.Max(0, jsRound(*remaining)),
				Source:    source,
				ResetAt:   resetAt,
			})
		}
	}
	if evaluation.Headroom >= headroomThreshold {
		evaluation.Factor = 1
	} else {
		evaluation.Factor = headroomFloor + (1-headroomFloor)*(evaluation.Headroom/headroomThreshold)
	}
	return evaluation, nil
}

func resetOf(daily *Window, state *stateRecord) *int64 {
	if daily != nil && daily.ResetAt != nil {
		return daily.ResetAt
	}
	if state != nil {
		return state.resetAt
	}
	return nil
}

// RecordUsage counts requests and tokens against a model and its account pool.
func (s *Service) RecordUsage(entry Entry, requests, tokens int64, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	nowMS := now.UnixMilli()
	if err := s.addUsage(ModelScopeKey(entry.Ref), requests, tokens, nowMS); err != nil {
		return err
	}
	return s.addUsage(PoolScopeKey(entry.AccountID), requests, tokens, nowMS)
}

// RecordRequest counts one answered request and the tokens it used.
func (s *Service) RecordRequest(entry Entry, tokens int64, now time.Time) error {
	return s.RecordUsage(entry, 1, tokens, now)
}

// RefundRequest takes back a request recorded when it was sent (a pool partner
// refused it, so it did not count there).
func (s *Service) RefundRequest(entry Entry, tokens int64, now time.Time) error {
	return s.RecordUsage(entry, -1, -tokens, now)
}

// RecordTokens counts tokens without a request (streaming usage).
func (s *Service) RecordTokens(entry Entry, tokens int64, now time.Time) error {
	if tokens == 0 {
		return nil
	}
	return s.RecordUsage(entry, 0, tokens, now)
}

// RecordPlanRequest counts one request against an account's prepaid plan.
func (s *Service) RecordPlanRequest(accountID string, now time.Time) error {
	return s.AddUsage(PlanScopeKey(accountID), 1, 0, now)
}

// AccountState is everything observed about an account's limits.
func (s *Service) AccountState(accountID string) ([]State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.load(s.now().UnixMilli()); err != nil {
		return nil, err
	}
	return s.accountState(accountID), nil
}

// AccountStateNow is AccountState for people to read: a window whose reset
// time has passed is shown renewed (nothing used) until the provider reports
// again.
func (s *Service) AccountStateNow(accountID string, now time.Time) ([]State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	nowMS := now.UnixMilli()
	if err := s.load(nowMS); err != nil {
		return nil, err
	}
	states := s.accountState(accountID)
	for i := range states {
		state := &states[i]
		if state.ResetAt != nil && *state.ResetAt <= nowMS && state.Limit != nil {
			state.Remaining = state.Limit
			state.ResetAt = nil
			state.Renewed = true
		}
	}
	return states, nil
}

func (s *Service) accountState(accountID string) []State {
	out := []State{}
	for _, scope := range []string{PoolScopeKey(accountID), PlanScopeKey(accountID)} {
		var metrics []string
		for key := range s.states {
			if metric, ok := strings.CutPrefix(key, scope+"|"); ok {
				metrics = append(metrics, metric)
			}
		}
		sort.Strings(metrics)
		for _, metric := range metrics {
			state := s.states[scope+"|"+metric]
			out = append(out, State{
				Scope:       scope,
				Metric:      metric,
				Limit:       state.limit,
				Remaining:   state.remaining,
				ResetAt:     state.resetAt,
				Source:      state.source,
				ObservedAt:  state.observedAt,
				LocalUsed:   state.localUsed,
				RequestCost: state.requestCost,
			})
		}
	}
	return out
}

// Reset drops the in-memory store (tests).
func (s *Service) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	clear(s.buckets)
	clear(s.dirty)
	clear(s.records)
	clear(s.states)
	s.limits.Reset()
	s.loaded = false
}
