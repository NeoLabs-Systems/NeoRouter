package usage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Counts is what NeoRouter learned about a model on one kind of task.
type Counts struct {
	Successes float64
	Failures  float64
	Liked     float64
	Disliked  float64
}

// Standing is what those counts add up to.
type Standing struct {
	Rate  float64
	Share float64
}

// TaskStats is one stored task-experience row.
type TaskStats struct {
	ModelRef  int64
	Task      string
	Successes float64
	Failures  float64
	Liked     float64
	Disliked  float64
	UpdatedAt int64
}

// Snapshot is the measured record of one model copy.
type Snapshot struct {
	Samples   int64
	Successes float64
	Failures  float64
	LatencyMS *float64
	TPS       *float64
}

// Parts are the parts of a model's worth routing weighs up.
type Parts struct {
	Intelligence float64
	Speed        float64
	Reliability  float64
}

// CatalogEntry is one catalog entry (a model on one provider account) as
// internal/catalog presents it. Ref is the model's id, which is what quota
// scopes and task statistics key on.
type CatalogEntry struct {
	Ref                int64
	ID                 string // "slug/model-id"
	Name               string
	ModelID            string
	CanonicalID        string
	AccountID          string
	DefinitionID       string
	Enabled            bool
	Available          bool
	AccountEnabled     bool
	AccountStatus      string
	IsFree             bool
	IsPlan             bool
	Intelligence       *float64
	IntelligenceSource string
	Skills             map[string]float64
	BillingMode        string
	FreeReason         string
}

// Routing is the narrow surface internal/usage consumes from internal/routing.
type Routing interface {
	// TaskSkills is the skill weights one kind of task calls on.
	TaskSkills(task string) map[string]float64
	// Availability says why a model copy cannot be used right now ("" when it can).
	Availability(ref int64, now time.Time) string
	// Snapshot is the measured record of one model copy.
	Snapshot(modelRef int64, now time.Time) Snapshot
	// Faded is the time-decayed outcome counts of one model on one task.
	Faded(row TaskStats, now time.Time) Counts
	// Standing rates how well those counts say the model did.
	Standing(counts Counts) Standing
	// NeedsCredits reports whether the account behind a model has run out of credit.
	NeedsCredits(entry CatalogEntry) bool
	// WorthParts scores a model for one task.
	WorthParts(entry CatalogEntry, task string, now time.Time) Parts
	// Worth folds those parts with the balanced profile's weights.
	Worth(parts Parts) float64
}

const (
	curveDays = 30
	// factsShown is how many discoveries the answer lists.
	factsShown = 12
	// untriedModels is how many measured models are listed even without traffic.
	untriedModels = 60
	// noEvidenceReliability is where a model without much evidence starts.
	noEvidenceReliability = 0.85
)

// lastingCooldowns are the refusals that outlast the moment: refused for this
// account, not merely busy.
var lastingCooldowns = map[string]bool{
	"not_available":    true,
	"payment_required": true,
	"auth_failed":      true,
	"incompatible":     true,
	"no_free_quota":    true,
}

// CurveDay is one day of first-try answers.
type CurveDay struct {
	Bucket   int64   `json:"bucket"`
	Requests int64   `json:"requests"`
	Tried    int64   `json:"tried"`
	FirstTry int64   `json:"firstTry"`
	Rate     float64 `json:"rate"`
}

// FreeParts are the parts of a free copy of a model.
type FreeParts struct {
	Quality     float64 `json:"quality"`
	Speed       float64 `json:"speed"`
	Reliability float64 `json:"reliability"`
}

// Cell is what is known about one model on one kind of task.
type Cell struct {
	Task        string     `json:"task"`
	Model       string     `json:"model"`
	Name        string     `json:"name"`
	Answers     int64      `json:"answers"`
	Score       float64    `json:"score"`
	Quality     float64    `json:"quality"`
	Speed       float64    `json:"speed"`
	Reliability float64    `json:"reliability"`
	Free        *FreeParts `json:"free"`
	Benchmarked bool       `json:"benchmarked"`
	Rate        *float64   `json:"rate"`
	Liked       int64      `json:"liked"`
	Disliked    int64      `json:"disliked"`
}

// MeasuredModel is the speed and reliability measured for one model.
type MeasuredModel struct {
	Model       string   `json:"model"`
	Name        string   `json:"name"`
	Samples     int64    `json:"samples"`
	LatencyMS   *float64 `json:"latencyMs"`
	TPS         *float64 `json:"tps"`
	Reliability *float64 `json:"reliability"`
}

// LimitFact is a limit read out of an error message.
type LimitFact struct {
	Kind   string  `json:"kind"`
	Name   string  `json:"name"`
	Metric string  `json:"metric"`
	Value  float64 `json:"value"`
	At     int64   `json:"at"`
}

// RoomFact is the room a model turned out to need.
type RoomFact struct {
	Kind  string  `json:"kind"`
	Name  string  `json:"name"`
	Value float64 `json:"value"`
	At    int64   `json:"at"`
}

// RestingFact is a model the router is giving a rest right now.
type RestingFact struct {
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	Reason string `json:"reason"`
	Until  int64  `json:"until"`
	At     int64  `json:"at"`
}

// Learning is what routing learned from a person's traffic.
type Learning struct {
	Answers      int64           `json:"answers"`
	TasksKnown   int             `json:"tasksKnown"`
	ModelsKnown  int             `json:"modelsKnown"`
	Measurements int64           `json:"measurements"`
	Rescued      int64           `json:"rescued"`
	NoCapacity   int64           `json:"noCapacity"`
	FirstTryThen *float64        `json:"firstTryThen"`
	FirstTryNow  *float64        `json:"firstTryNow"`
	Curve        []CurveDay      `json:"curve"`
	Tasks        []string        `json:"tasks"`
	Models       []string        `json:"models"`
	Cells        []Cell          `json:"cells"`
	Fastest      []MeasuredModel `json:"fastest"`
	Discoveries  []any           `json:"discoveries"`
}

// LearningOf is what routing learned from this person's traffic.
func (s *Service) LearningOf(userID string, now time.Time) (*Learning, error) {
	curve, err := s.firstTryCurve(userID, now)
	if err != nil {
		return nil, err
	}
	cells, err := s.taskKnowledge(userID, now)
	if err != nil {
		return nil, err
	}
	measured, err := s.measuredModels(userID, now)
	if err != nil {
		return nil, err
	}
	discoveries, err := s.discoveries(userID, now)
	if err != nil {
		return nil, err
	}

	taskWeight := map[string]int64{}
	modelWeight := map[string]int64{}
	for _, cell := range cells {
		if cell.Answers == 0 {
			continue
		}
		taskWeight[cell.Task] += cell.Answers
		modelWeight[cell.Model] += cell.Answers
	}
	learning := &Learning{
		TasksKnown:   len(taskWeight),
		ModelsKnown:  len(modelWeight),
		Rescued:      curve.rescued,
		NoCapacity:   curve.noCapacity,
		FirstTryThen: curve.firstTryThen,
		FirstTryNow:  curve.firstTryNow,
		Curve:        curve.curve,
		Tasks:        busiest(taskWeight),
		Models:       busiest(modelWeight),
		Cells:        cells,
		Fastest:      []MeasuredModel{},
		Discoveries:  discoveries,
	}
	for _, weight := range taskWeight {
		learning.Answers += weight
	}
	fast := []MeasuredModel{}
	for _, model := range measured {
		learning.Measurements += model.Samples
		if model.LatencyMS != nil {
			fast = append(fast, model)
		}
	}
	sort.SliceStable(fast, func(i, j int) bool { return *fast[i].LatencyMS < *fast[j].LatencyMS })
	if len(fast) > 5 {
		fast = fast[:5]
	}
	learning.Fastest = fast
	return learning, nil
}

// busiest ranks weights busiest first.
func busiest(weights map[string]int64) []string {
	out := make([]string, 0, len(weights))
	for key := range weights {
		out = append(out, key)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if weights[out[i]] != weights[out[j]] {
			return weights[out[i]] > weights[out[j]]
		}
		return out[i] < out[j]
	})
	return out
}

type firstTry struct {
	curve        []CurveDay
	rescued      int64
	noCapacity   int64
	firstTryThen *float64
	firstTryNow  *float64
}

// firstTryCurve is how often the model the router chose first answered, per
// day, among requests where a model could be tried at all. Requests that found
// every model busy say nothing about the choice: they are counted apart, as no
// capacity.
func (s *Service) firstTryCurve(userID string, now time.Time) (*firstTry, error) {
	day := int64(86_400_000)
	since := (now.UnixMilli()-curveDays*day)/day*day + day
	rows, err := s.DB.R.QueryContext(context.Background(),
		`SELECT created_at, status, error_code, trail_json FROM requests WHERE user_id = ? AND created_at >= ? ORDER BY created_at`,
		userID, since)
	if err != nil {
		return nil, fmt.Errorf("usage: read first-try curve: %w", err)
	}
	defer rows.Close()
	result := &firstTry{}
	order := []int64{}
	days := map[int64]*CurveDay{}
	for rows.Next() {
		var createdAt int64
		var status string
		var errorCode, trailJSON sql.NullString
		if err := rows.Scan(&createdAt, &status, &errorCode, &trailJSON); err != nil {
			return nil, fmt.Errorf("usage: read first-try curve: %w", err)
		}
		bucket := createdAt / day * day
		dayStats, ok := days[bucket]
		if !ok {
			dayStats = &CurveDay{Bucket: bucket}
			days[bucket] = dayStats
			order = append(order, bucket)
		}
		dayStats.Requests++
		steps := trailSteps(trailJSON.String)
		if len(steps) == 0 {
			if status == "error" && strings.Contains(strings.ToLower(errorCode.String), "no_model_available") {
				result.noCapacity++
			}
			continue
		}
		dayStats.Tried++
		// A hedge that lost a race was not a wrong choice, only a slower one.
		if worked(steps[0]) {
			dayStats.FirstTry++
		}
		if status == "success" && !allWorked(steps) {
			result.rescued++
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("usage: read first-try curve: %w", err)
	}
	sort.Slice(order, func(i, j int) bool { return order[i] < order[j] })
	curve := []CurveDay{}
	for _, bucket := range order {
		dayStats := days[bucket]
		if dayStats.Tried == 0 {
			continue
		}
		dayStats.Rate = float64(dayStats.FirstTry) / float64(dayStats.Tried)
		curve = append(curve, *dayStats)
	}
	result.curve = curve
	// Thirds of the range, so one odd day does not decide the headline.
	third := max(1, len(curve)/3)
	if len(curve) >= 3 {
		result.firstTryThen = share(curve[:third])
	}
	if len(curve) > 0 {
		result.firstTryNow = share(curve[len(curve)-third:])
	}
	return result, nil
}

func worked(step trailStep) bool {
	return step.Outcome == "success" || step.Outcome == "hedge_lost"
}

func allWorked(steps []trailStep) bool {
	for _, step := range steps {
		if !worked(step) {
			return false
		}
	}
	return true
}

func share(days []CurveDay) *float64 {
	var tried, firstTry int64
	for _, day := range days {
		tried += day.Tried
		firstTry += day.FirstTry
	}
	if tried == 0 {
		return nil
	}
	value := float64(firstTry) / float64(tried)
	return &value
}

// usable reports whether a copy can take a request at all. Short cooldowns
// pass: a model that is merely busy can still be the best one.
func (s *Service) usable(entry CatalogEntry, now time.Time) bool {
	if !entry.Enabled || !entry.Available || !entry.AccountEnabled || entry.AccountStatus == "disabled" || entry.AccountStatus == "invalid" {
		return false
	}
	if s.Routing == nil {
		return true
	}
	if s.Routing.NeedsCredits(entry) {
		return false
	}
	return !lastingCooldowns[s.Routing.Availability(entry.Ref, now)]
}

// workedRate is how reliably a model delivered this kind of work: its usable
// answers, not the rate limits it hit, since being popular says nothing about
// what it is good at.
func workedRate(standing Standing) float64 {
	return standing.Share*standing.Rate + (1-standing.Share)*noEvidenceReliability
}

// cellState is a cell still being summed.
type cellState struct {
	cell      Cell
	count     Counts
	score     float64
	freeScore float64
}

// taskKnowledge is what it knows per kind of task about every model this person
// can use, not only the ones that happened to get traffic: a model nobody tried
// can still be the best one.
func (s *Service) taskKnowledge(userID string, now time.Time) ([]Cell, error) {
	if s.Routing == nil || s.Catalog == nil {
		return []Cell{}, nil
	}
	rows, err := s.DB.R.QueryContext(context.Background(), `SELECT s.model_ref, s.task, s.successes, s.failures, s.liked, s.disliked, s.updated_at
		FROM model_task_stats s JOIN models m ON m.id = s.model_ref JOIN provider_accounts a ON a.id = m.account_id WHERE a.user_id = ?`, userID)
	if err != nil {
		return nil, fmt.Errorf("usage: read task stats: %w", err)
	}
	stats := []TaskStats{}
	for rows.Next() {
		var row TaskStats
		if err := rows.Scan(&row.ModelRef, &row.Task, &row.Successes, &row.Failures, &row.Liked, &row.Disliked, &row.UpdatedAt); err != nil {
			rows.Close()
			return nil, fmt.Errorf("usage: read task stats: %w", err)
		}
		stats = append(stats, row)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("usage: read task stats: %w", err)
	}

	listed, err := s.Catalog(context.Background(), userID, "chat", true)
	if err != nil {
		return nil, err
	}
	entries := []CatalogEntry{}
	for _, entry := range listed {
		if s.usable(entry, now) {
			entries = append(entries, entry)
		}
	}
	byRef := map[int64]CatalogEntry{}
	for _, entry := range entries {
		byRef[entry.Ref] = entry
	}
	counts := map[string]Counts{}
	taskSet := map[string]bool{}
	for _, row := range stats {
		if _, ok := byRef[row.ModelRef]; !ok {
			continue
		}
		taskSet[row.Task] = true
		counts[statKey(row.ModelRef, row.Task)] = s.Routing.Faded(row, now)
	}
	tasks := make([]string, 0, len(taskSet))
	for task := range taskSet {
		tasks = append(tasks, task)
	}
	sort.Strings(tasks)

	tried := map[string]bool{}
	for _, row := range stats {
		if entry, ok := byRef[row.ModelRef]; ok {
			tried[modelOf(entry)] = true
		}
	}
	// The strongest measured models are listed too, whatever their traffic.
	// Others stay out, or the list would hold every model of every connection.
	measured := []CatalogEntry{}
	for _, entry := range entries {
		if measuredByBenchmarks(entry) {
			measured = append(measured, entry)
		}
	}
	sort.SliceStable(measured, func(i, j int) bool { return valueOf(measured[i].Intelligence) > valueOf(measured[j].Intelligence) })
	strongest := map[string]bool{}
	for _, entry := range measured {
		strongest[modelOf(entry)] = true
		if len(strongest) >= untriedModels {
			break
		}
	}

	cells := map[string]*cellState{}
	order := []string{}
	for _, entry := range entries {
		model := modelOf(entry)
		if !tried[model] && !strongest[model] {
			continue
		}
		for _, task := range tasks {
			key := task + "|" + model
			state, ok := cells[key]
			if !ok {
				state = &cellState{
					cell: Cell{
						Task:        task,
						Model:       model,
						Name:        entry.Name,
						Benchmarked: measuredFor(entry, task, s.Routing.TaskSkills),
					},
					score:     -1,
					freeScore: -1,
				}
				cells[key] = state
				order = append(order, key)
			}
			faded := counts[statKey(entry.Ref, task)]
			state.count.Successes += faded.Successes
			state.count.Failures += faded.Failures
			state.count.Liked += faded.Liked
			state.count.Disliked += faded.Disliked
			parts := s.Routing.WorthParts(entry, task, now)
			parts.Reliability = workedRate(s.Routing.Standing(state.count))
			value := s.Routing.Worth(parts)
			if value > state.score {
				state.score = value
				state.cell.Quality = parts.Intelligence
				state.cell.Speed = parts.Speed
				state.cell.Reliability = parts.Reliability
			}
			if entry.IsFree && value > state.freeScore {
				state.freeScore = value
				state.cell.Free = &FreeParts{Quality: parts.Intelligence, Speed: parts.Speed, Reliability: parts.Reliability}
			}
		}
	}
	out := make([]Cell, 0, len(order))
	for _, key := range order {
		state := cells[key]
		cell := state.cell
		cell.Score = state.score
		cell.Answers = int64(math.Round(state.count.Successes + state.count.Failures))
		cell.Liked = int64(math.Round(state.count.Liked))
		cell.Disliked = int64(math.Round(state.count.Disliked))
		if cell.Answers > 0 {
			rate := s.Routing.Standing(state.count).Rate
			cell.Rate = &rate
		}
		out = append(out, cell)
	}
	return out, nil
}

func statKey(modelRef int64, task string) string {
	return strconv.FormatInt(modelRef, 10) + "|" + task
}

func valueOf(v *float64) float64 {
	if v == nil {
		return 0
	}
	return *v
}

// modelOf identifies one model: ids can differ ("beam", "beam-1.0-501b"), so
// one benchmark record is one model.
func modelOf(entry CatalogEntry) string {
	if entry.Skills == nil {
		return entry.CanonicalID
	}
	encoded, err := json.Marshal(entry.Skills)
	if err != nil {
		return entry.CanonicalID
	}
	intelligence := "null"
	if entry.Intelligence != nil {
		intelligence = strconv.FormatFloat(*entry.Intelligence, 'g', -1, 64)
	}
	return intelligence + "|" + string(encoded)
}

// measuredByBenchmarks reports whether a model's intelligence comes from a
// measurement rather than an estimate.
func measuredByBenchmarks(entry CatalogEntry) bool {
	if entry.IntelligenceSource == "manual" {
		return true
	}
	return strings.HasPrefix(entry.IntelligenceSource, "artificial_analysis") ||
		strings.HasPrefix(entry.IntelligenceSource, "openrouter_benchmarks")
}

// measuredFor reports whether a model is measured for this kind of work, or
// only estimated. A model only OpenRouter knows has a score for reasoning and
// little else: measured for the work that calls on it, an estimate for the rest.
func measuredFor(entry CatalogEntry, task string, taskSkills func(string) map[string]float64) bool {
	if !measuredByBenchmarks(entry) {
		return false
	}
	if entry.IntelligenceSource != "openrouter_benchmarks" {
		return true
	}
	needed := taskSkills(task)
	if len(needed) == 0 {
		return true
	}
	for skill := range needed {
		if _, ok := entry.Skills[skill]; ok {
			return true
		}
	}
	return false
}

// measuredModels is the speed and reliability NeoRouter measured itself,
// across every copy of a model.
func (s *Service) measuredModels(userID string, now time.Time) ([]MeasuredModel, error) {
	if s.Routing == nil {
		return []MeasuredModel{}, nil
	}
	rows, err := s.DB.R.QueryContext(context.Background(), `SELECT s.model_ref, m.canonical_id, m.model_id, m.display_name
		FROM model_stats s JOIN models m ON m.id = s.model_ref JOIN provider_accounts a ON a.id = m.account_id
		WHERE a.user_id = ? AND s.samples > 0`, userID)
	if err != nil {
		return nil, fmt.Errorf("usage: read measured models: %w", err)
	}
	defer rows.Close()
	order := []string{}
	byCanonical := map[string]*MeasuredModel{}
	sums := map[string]Counts{}
	for rows.Next() {
		var modelRef int64
		var canonical, modelID string
		var displayName sql.NullString
		if err := rows.Scan(&modelRef, &canonical, &modelID, &displayName); err != nil {
			return nil, fmt.Errorf("usage: read measured models: %w", err)
		}
		seen := s.Routing.Snapshot(modelRef, now)
		model := byCanonical[canonical]
		if model == nil {
			name := modelID
			if displayName.Valid && displayName.String != "" {
				name = displayName.String
			}
			model = &MeasuredModel{Model: canonical, Name: name}
			byCanonical[canonical] = model
			order = append(order, canonical)
		}
		model.Samples += seen.Samples
		sums[canonical] = Counts{
			Successes: sums[canonical].Successes + seen.Successes,
			Failures:  sums[canonical].Failures + seen.Failures,
		}
		// The best copy: that is the one routing ends up preferring.
		if seen.LatencyMS != nil && (model.LatencyMS == nil || *seen.LatencyMS < *model.LatencyMS) {
			rounded := math.Round(*seen.LatencyMS)
			model.LatencyMS = &rounded
		}
		if seen.TPS != nil && (model.TPS == nil || *seen.TPS > *model.TPS) {
			rounded := math.Round(*seen.TPS)
			model.TPS = &rounded
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("usage: read measured models: %w", err)
	}
	out := make([]MeasuredModel, 0, len(order))
	for _, canonical := range order {
		model := *byCanonical[canonical]
		total := sums[canonical].Successes + sums[canonical].Failures
		if total > 0 {
			reliability := sums[canonical].Successes / total
			model.Reliability = &reliability
		}
		out = append(out, model)
	}
	return out, nil
}

// discoveries are the things nobody told NeoRouter: limits read out of error
// messages, room a model turned out to need, and models it is resting.
func (s *Service) discoveries(userID string, now time.Time) ([]any, error) {
	ctx := context.Background()
	type fact struct {
		at      int64
		resting bool
		payload any
	}
	facts := []fact{}

	rows, err := s.DB.R.QueryContext(ctx, `SELECT l.metric, l.value, l.updated_at, m.model_id, m.display_name FROM quota_limits l
		JOIN models m ON l.scope_key = 'm:' || m.id JOIN provider_accounts a ON a.id = m.account_id
		WHERE a.user_id = ? AND l.source = 'learned'`, userID)
	if err != nil {
		return nil, fmt.Errorf("usage: read learned limits: %w", err)
	}
	for rows.Next() {
		var metric, modelID string
		var value, updatedAt float64
		var displayName sql.NullString
		if err := rows.Scan(&metric, &value, &updatedAt, &modelID, &displayName); err != nil {
			rows.Close()
			return nil, fmt.Errorf("usage: read learned limits: %w", err)
		}
		name := modelID
		if displayName.Valid && displayName.String != "" {
			name = displayName.String
		}
		facts = append(facts, fact{at: int64(updatedAt), payload: LimitFact{Kind: "limit", Name: name, Metric: metric, Value: value, At: int64(updatedAt)}})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("usage: read learned limits: %w", err)
	}

	accountRows, err := s.DB.R.QueryContext(ctx, `SELECT l.metric, l.value, l.updated_at, a.label FROM quota_limits l
		JOIN provider_accounts a ON l.scope_key = 'p:' || a.id WHERE a.user_id = ? AND l.source = 'learned'`, userID)
	if err != nil {
		return nil, fmt.Errorf("usage: read learned account limits: %w", err)
	}
	for accountRows.Next() {
		var metric, label string
		var value, updatedAt float64
		if err := accountRows.Scan(&metric, &value, &updatedAt, &label); err != nil {
			accountRows.Close()
			return nil, fmt.Errorf("usage: read learned account limits: %w", err)
		}
		facts = append(facts, fact{at: int64(updatedAt), payload: LimitFact{Kind: "limit", Name: label, Metric: metric, Value: value, At: int64(updatedAt)}})
	}
	accountRows.Close()
	if err := accountRows.Err(); err != nil {
		return nil, fmt.Errorf("usage: read learned account limits: %w", err)
	}

	nowMS := now.UnixMilli()
	statRows, err := s.DB.R.QueryContext(ctx, `SELECT s.min_output_tokens, s.last_failure_at, s.cooldown_until, s.cooldown_reason,
		s.breaker_open_until, m.model_id, m.display_name
		FROM model_stats s JOIN models m ON m.id = s.model_ref JOIN provider_accounts a ON a.id = m.account_id
		WHERE a.user_id = ? AND (s.min_output_tokens > 0 OR s.cooldown_until > ? OR s.breaker_open_until > ?)`, userID, nowMS, nowMS)
	if err != nil {
		return nil, fmt.Errorf("usage: read model state: %w", err)
	}
	for statRows.Next() {
		var minOutput, cooldownUntil, breakerOpen, lastFailure sql.NullInt64
		var cooldownReason, modelID, displayName sql.NullString
		if err := statRows.Scan(&minOutput, &lastFailure, &cooldownUntil, &cooldownReason, &breakerOpen, &modelID, &displayName); err != nil {
			statRows.Close()
			return nil, fmt.Errorf("usage: read model state: %w", err)
		}
		name := modelID.String
		if displayName.Valid && displayName.String != "" {
			name = displayName.String
		}
		at := lastFailure.Int64
		if minOutput.Int64 > 0 {
			facts = append(facts, fact{at: at, payload: RoomFact{Kind: "room", Name: name, Value: float64(minOutput.Int64), At: at}})
		}
		until := max(cooldownUntil.Int64, breakerOpen.Int64)
		if until > nowMS {
			reason := "circuit_open"
			if cooldownUntil.Int64 > nowMS && cooldownReason.Valid {
				reason = cooldownReason.String
			}
			facts = append(facts, fact{at: at, resting: true, payload: RestingFact{Kind: "resting", Name: name, Reason: reason, Until: until, At: at}})
		}
	}
	statRows.Close()
	if err := statRows.Err(); err != nil {
		return nil, fmt.Errorf("usage: read model state: %w", err)
	}

	// What is happening now first, then the newest finds.
	sort.SliceStable(facts, func(i, j int) bool {
		if facts[i].resting != facts[j].resting {
			return facts[i].resting
		}
		return facts[i].at > facts[j].at
	})
	out := []any{}
	for _, item := range facts {
		if len(out) == factsShown {
			break
		}
		out = append(out, item.payload)
	}
	return out, nil
}
