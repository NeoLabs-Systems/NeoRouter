package usage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/dbx"
)

// fakeRouting stands in for internal/routing: it applies the same formulas the
// routing package owns (experience.standing, scoring.combineWorth with the
// balanced weights) to the rows this package reads.
type fakeRouting struct{}

var fakeTaskSkills = map[string]map[string]float64{
	"coding":      {"coding": 1, "agentic": 0.2},
	"translation": {"knowledge": 0.6, "instruction": 0.6},
}

func (fakeRouting) TaskSkills(task string) map[string]float64 { return fakeTaskSkills[task] }
func (fakeRouting) Availability(int64, time.Time) string      { return "" }
func (fakeRouting) Snapshot(int64, time.Time) Snapshot        { return Snapshot{} }
func (fakeRouting) NeedsCredits(CatalogEntry) bool            { return false }

func (fakeRouting) Faded(row TaskStats, now time.Time) Counts {
	return Counts{Successes: row.Successes, Failures: row.Failures, Liked: row.Liked, Disliked: row.Disliked}
}

func (fakeRouting) Standing(counts Counts) Standing {
	outcomes := counts.Successes + counts.Failures
	return Standing{
		Rate:  (counts.Successes + 2) / (outcomes + 3),
		Share: outcomes / (outcomes + 8),
	}
}

func (fakeRouting) WorthParts(entry CatalogEntry, task string, now time.Time) Parts {
	quality := 0.5
	if entry.Intelligence != nil {
		quality = *entry.Intelligence
		best := 0.0
		for skill := range fakeTaskSkills[task] {
			if value, ok := entry.Skills[skill]; ok && value > best {
				best = value
			}
		}
		if best > 0 {
			quality *= best
		}
	}
	return Parts{Intelligence: quality, Speed: 0.5}
}

func (fakeRouting) Worth(parts Parts) float64 {
	weights := []struct {
		weight float64
		value  float64
	}{{0.35, parts.Intelligence}, {0.2, parts.Speed}, {0.3, parts.Reliability}}
	total := 0.0
	sum := 0.0
	for _, item := range weights {
		total += item.weight
		sum += item.weight * math.Log(math.Max(item.value, 1e-6))
	}
	return math.Exp(sum / total)
}

// fakeCatalog stands in for internal/catalog.
func fakeCatalog(t *testing.T, db *dbx.DB) func(context.Context, string, string, bool) ([]CatalogEntry, error) {
	t.Helper()
	return func(ctx context.Context, userID, capability string, includeUnavailable bool) ([]CatalogEntry, error) {
		query := `SELECT m.id, m.model_id, m.display_name, m.canonical_id, m.account_id,
			m.enabled, m.available, a.enabled, a.status, a.definition_id, a.billing_mode, m.is_free, m.free_reason,
			md.intelligence, md.intelligence_source, md.skills_json
			FROM models m JOIN provider_accounts a ON a.id = m.account_id
			LEFT JOIN model_metadata md ON md.canonical_id = m.canonical_id WHERE a.user_id = ? AND m.capability = ?`
		if !includeUnavailable {
			query += ` AND m.available = 1`
		}
		rows, err := db.R.QueryContext(ctx, query, userID, capability)
		if err != nil {
			t.Fatalf("read catalog: %v", err)
		}
		defer rows.Close()
		out := []CatalogEntry{}
		for rows.Next() {
			var entry CatalogEntry
			var displayName, canonical, definition, status, billingMode, freeReason, source, skills sql.NullString
			var enabled, available, accountEnabled, isFree int64
			var intelligence sql.NullFloat64
			if err := rows.Scan(&entry.Ref, &entry.ModelID, &displayName, &canonical, &entry.AccountID, &enabled, &available,
				&accountEnabled, &status, &definition, &billingMode, &isFree, &freeReason, &intelligence, &source, &skills); err != nil {
				t.Fatalf("scan catalog: %v", err)
			}
			entry.Name = entry.ModelID
			if displayName.Valid && displayName.String != "" {
				entry.Name = displayName.String
			}
			entry.CanonicalID = canonical.String
			entry.DefinitionID = definition.String
			entry.AccountStatus = status.String
			entry.BillingMode = billingMode.String
			entry.FreeReason = freeReason.String
			entry.IntelligenceSource = source.String
			entry.Enabled, entry.Available = enabled != 0, available != 0
			entry.AccountEnabled, entry.IsFree = accountEnabled != 0, isFree != 0
			if intelligence.Valid {
				entry.Intelligence = &intelligence.Float64
			}
			if skills.Valid && skills.String != "" {
				if err := json.Unmarshal([]byte(skills.String), &entry.Skills); err != nil {
					t.Fatalf("decode skills: %v", err)
				}
			}
			out = append(out, entry)
		}
		return out, nil
	}
}

// learningFixture is one person with a connection and two models.
type learningFixture struct {
	service *Service
	good    int64
	poor    int64
}

func newLearningFixture(t *testing.T) *learningFixture {
	t.Helper()
	service := testService(t)
	service.Routing = fakeRouting{}
	service.Catalog = fakeCatalog(t, service.DB)
	insertAccount(t, service, "acct", "u", "custom_openai", "host", "Host")
	exec(t, service, `UPDATE provider_accounts SET billing_mode = 'free_tier'`)
	fixture := &learningFixture{service: service}
	fixture.good = insertModel(t, service, "acct", "good-model", "good-model", "good-model")
	fixture.poor = insertModel(t, service, "acct", "poor-model", "poor-model", "poor-model")
	exec(t, service, `UPDATE models SET is_free = 1`)
	return fixture
}

// outcomes writes what routing would have learned about a model on a task.
func (f *learningFixture) outcomes(t *testing.T, modelRef int64, task string, successes, failures float64) {
	t.Helper()
	exec(t, f.service, `INSERT INTO model_task_stats (model_ref, task, successes, failures, updated_at) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(model_ref, task) DO UPDATE SET successes = excluded.successes, failures = excluded.failures`,
		modelRef, task, successes, failures, time.Now().UnixMilli())
}

// request writes one logged request with its trail.
func (f *learningFixture) request(t *testing.T, id string, at time.Time, status string, errorCode string, trail string) {
	t.Helper()
	exec(t, f.service, `INSERT INTO requests (id, user_id, created_at, api_format, requested_model, status, error_code, trail_json)
		VALUES (?, 'u', ?, 'openai', 'auto', ?, ?, ?)`, id, at.UnixMilli(), status, errorCode, trail)
}

func TestNothingLearnedYetIsAnEmptyAnswer(t *testing.T) {
	fixture := newLearningFixture(t)
	learning, err := fixture.service.LearningOf("u", time.Now())
	if err != nil {
		t.Fatalf("LearningOf: %v", err)
	}
	if learning.Answers != 0 || len(learning.Cells) != 0 || learning.FirstTryNow != nil {
		t.Fatalf("learning = %+v", learning)
	}
	encoded, err := json.Marshal(learning)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal(encoded, &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := body["cells"].([]any); !ok {
		t.Fatalf("cells = %v", body["cells"])
	}
}

func TestTheBestModelForATaskWeighsEvidence(t *testing.T) {
	fixture := newLearningFixture(t)
	// forty answers, one of them a failure
	fixture.outcomes(t, fixture.good, "coding", 39, 1)
	fixture.outcomes(t, fixture.poor, "coding", 1, 0)
	fixture.outcomes(t, fixture.poor, "translation", 6, 6)

	learning, err := fixture.service.LearningOf("u", time.Now())
	if err != nil {
		t.Fatalf("LearningOf: %v", err)
	}
	if learning.TasksKnown != 2 || learning.ModelsKnown != 2 {
		t.Fatalf("known = %d tasks, %d models", learning.TasksKnown, learning.ModelsKnown)
	}
	if learning.Answers != 40+1+12 {
		t.Fatalf("answers = %d", learning.Answers)
	}
	if learning.Tasks[0] != "coding" {
		t.Fatalf("tasks = %v", learning.Tasks)
	}
	coding := bestFor(t, learning, "coding")
	if coding.Name != "good-model" {
		t.Fatalf("best coding = %+v", coding)
	}
	cell := cellFor(t, learning, "translation", "poor-model")
	if cell.Rate == nil || *cell.Rate <= 0.4 || *cell.Rate >= 0.6 {
		t.Fatalf("translation rate = %v", cell.Rate)
	}
}

func TestBeingGoodAtTheWorkBeatsMerelyAnswering(t *testing.T) {
	fixture := newLearningFixture(t)
	fixture.outcomes(t, fixture.good, "coding", 39, 1)
	fixture.outcomes(t, fixture.poor, "coding", 1, 0)
	exec(t, fixture.service, `INSERT INTO model_metadata (canonical_id, intelligence, intelligence_source, skills_json) VALUES
		('good-model', 0.3, 'artificial_analysis', '{"coding":0.35}'),
		('poor-model', 0.8, 'calibrated', '{"coding":0.9}')`)
	fixture.outcomes(t, fixture.poor, "coding", 36, 4)

	learning, err := fixture.service.LearningOf("u", time.Now())
	if err != nil {
		t.Fatalf("LearningOf: %v", err)
	}
	coding := bestFor(t, learning, "coding")
	if coding.Name != "poor-model" {
		t.Fatalf("best coding = %+v", coding)
	}
	if coding.Benchmarked {
		t.Fatal("its score is an estimate and should say so")
	}
	weak := cellFor(t, learning, "coding", "good-model")
	if weak.Rate == nil || *weak.Rate <= 0.9 {
		t.Fatalf("it does answer: %v", weak.Rate)
	}
	if weak.Quality >= 0.4 {
		t.Fatalf("but it is weak at coding: %v", weak.Quality)
	}
	if !weak.Benchmarked {
		t.Fatal("a measured model is benchmarked")
	}
	if weak.Score >= coding.Score {
		t.Fatalf("one score takes both into account: %v vs %v", weak.Score, coding.Score)
	}
	strong := cellFor(t, learning, "coding", "poor-model")
	if strong.Reliability >= weak.Reliability {
		t.Fatalf("a track record of failures costs it: %v vs %v", strong.Reliability, weak.Reliability)
	}
	if strong.Free == nil || strong.Free.Quality != strong.Quality {
		t.Fatalf("its free copy is the one it has: %+v", strong.Free)
	}
}

func bestFor(t *testing.T, learning *Learning, task string) Cell {
	t.Helper()
	var best *Cell
	for i := range learning.Cells {
		cell := &learning.Cells[i]
		if cell.Task != task {
			continue
		}
		if best == nil || cell.Score > best.Score {
			best = cell
		}
	}
	if best == nil {
		t.Fatalf("no cell for %q in %+v", task, learning.Cells)
	}
	return *best
}

func cellFor(t *testing.T, learning *Learning, task, name string) Cell {
	t.Helper()
	for _, cell := range learning.Cells {
		if cell.Task == task && cell.Name == name {
			return cell
		}
	}
	t.Fatalf("no cell for %q/%q", task, name)
	return Cell{}
}

func TestFirstTryAnswersCountPerDay(t *testing.T) {
	fixture := newLearningFixture(t)
	now := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	fixture.service.SetClock(func() time.Time { return now })
	missed := `[{"model":"a/b","outcome":"rate_limit"},{"model":"c/d","outcome":"success"}]`
	raced := `[{"model":"a/b","outcome":"hedge_lost"},{"model":"c/d","outcome":"success"}]`
	fixture.request(t, "busy-1", now, "error", "NO_MODEL_AVAILABLE", "[]")
	fixture.request(t, "old-1", now.AddDate(0, 0, -20), "success", "", missed)
	fixture.request(t, "old-2", now.AddDate(0, 0, -20), "success", "", raced)
	fixture.request(t, "mid-1", now.AddDate(0, 0, -10), "success", "", raced)
	fixture.request(t, "new-1", now, "success", "", raced)

	learning, err := fixture.service.LearningOf("u", now)
	if err != nil {
		t.Fatalf("LearningOf: %v", err)
	}
	if learning.Rescued != 1 {
		t.Fatalf("rescued = %d", learning.Rescued)
	}
	if learning.NoCapacity != 1 {
		t.Fatalf("no capacity = %d", learning.NoCapacity)
	}
	if len(learning.Curve) != 3 {
		t.Fatalf("curve = %+v", learning.Curve)
	}
	if learning.FirstTryThen == nil || *learning.FirstTryThen != 0.5 {
		t.Fatalf("firstTryThen = %v", learning.FirstTryThen)
	}
	if learning.FirstTryNow == nil || *learning.FirstTryNow != 1 {
		t.Fatalf("firstTryNow = %v", learning.FirstTryNow)
	}
	if string(mustJSON(t, learning.Curve[0])) != fmt.Sprintf(`{"bucket":%d,"requests":2,"tried":2,"firstTry":1,"rate":0.5}`,
		now.AddDate(0, 0, -20).UnixMilli()/86_400_000*86_400_000) {
		t.Fatalf("curve day = %s", mustJSON(t, learning.Curve[0]))
	}
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(encoded)
}

func TestDiscoveredLimitsAreListed(t *testing.T) {
	fixture := newLearningFixture(t)
	now := time.Now()
	exec(t, fixture.service, `INSERT INTO quota_limits (scope_key, metric, value, source, updated_at) VALUES (?, 'rpm', 12, 'learned', ?)`,
		"m:"+fmt.Sprint(fixture.good), now.UnixMilli())
	exec(t, fixture.service, `INSERT INTO model_stats (model_ref, min_output_tokens, last_failure_at) VALUES (?, 800, ?)`, fixture.poor, now.UnixMilli())

	learning, err := fixture.service.LearningOf("u", now)
	if err != nil {
		t.Fatalf("LearningOf: %v", err)
	}
	var limit, room *struct {
		Kind  string
		Name  string
		Value float64
	}
	for _, item := range learning.Discoveries {
		encoded := mustJSON(t, item)
		if containsKey(encoded, `"kind":"limit"`) {
			var fact LimitFact
			if err := json.Unmarshal([]byte(encoded), &fact); err != nil {
				t.Fatalf("decode limit fact: %v", err)
			}
			if fact.Name != "good-model" || fact.Value != 12 || fact.Metric != "rpm" {
				t.Fatalf("limit fact = %+v", fact)
			}
			limit = &struct {
				Kind  string
				Name  string
				Value float64
			}{fact.Kind, fact.Name, fact.Value}
		}
		if containsKey(encoded, `"kind":"room"`) {
			var fact RoomFact
			if err := json.Unmarshal([]byte(encoded), &fact); err != nil {
				t.Fatalf("decode room fact: %v", err)
			}
			if fact.Name != "poor-model" || fact.Value != 800 {
				t.Fatalf("room fact = %+v", fact)
			}
			room = &struct {
				Kind  string
				Name  string
				Value float64
			}{fact.Kind, fact.Name, fact.Value}
		}
	}
	if limit == nil || room == nil {
		t.Fatalf("discoveries = %+v", learning.Discoveries)
	}
}

func containsKey(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (haystack == needle || indexOf(haystack, needle) >= 0)
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}

func TestAnotherAccountSeesNoneOfIt(t *testing.T) {
	fixture := newLearningFixture(t)
	fixture.outcomes(t, fixture.good, "coding", 39, 1)
	exec(t, fixture.service, `INSERT INTO quota_limits (scope_key, metric, value, source, updated_at) VALUES (?, 'rpm', 12, 'learned', ?)`,
		"m:"+fmt.Sprint(fixture.good), time.Now().UnixMilli())
	insertUser(t, fixture.service, "v")

	learning, err := fixture.service.LearningOf("v", time.Now())
	if err != nil {
		t.Fatalf("LearningOf: %v", err)
	}
	if learning.Answers != 0 || len(learning.Cells) != 0 || len(learning.Discoveries) != 0 || len(learning.Curve) != 0 {
		t.Fatalf("another account sees %+v", learning)
	}
}

func TestLearningRoutesThroughTheActivityPermission(t *testing.T) {
	fixture := newLearningFixture(t)
	encoded, err := json.Marshal(mustLearning(t, fixture))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	const wantPrefix = `{"answers":0,"tasksKnown":0,"modelsKnown":0,"measurements":0,"rescued":0,"noCapacity":0,`
	if len(encoded) < len(wantPrefix) || string(encoded[:len(wantPrefix)]) != wantPrefix {
		t.Fatalf("learning = %s", encoded)
	}
}

func mustLearning(t *testing.T, fixture *learningFixture) *Learning {
	t.Helper()
	learning, err := fixture.service.LearningOf("u", time.Now())
	if err != nil {
		t.Fatalf("LearningOf: %v", err)
	}
	return learning
}
