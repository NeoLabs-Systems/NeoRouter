package usage

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/dbx"
	"github.com/NeoLabs-Systems/neorouter/internal/schema"
)

func testDB(t *testing.T) *dbx.DB {
	t.Helper()
	db, err := dbx.Open(context.Background(), filepath.Join(t.TempDir(), "neorouter.sqlite3"), dbx.Options{})
	if err != nil {
		t.Fatalf("dbx.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := schema.Ensure(context.Background(), db.W); err != nil {
		t.Fatalf("schema.Ensure: %v", err)
	}
	return db
}

func testService(t *testing.T) *Service {
	t.Helper()
	service := New(testDB(t))
	service.Log = slog.New(slog.NewTextHandler(io.Discard, nil))
	service.Space = func(context.Context) (Space, bool) { return Space{OwnerID: "u", ActorID: "u"}, true }
	return service
}

func exec(t *testing.T, service *Service, query string, args ...any) {
	t.Helper()
	if _, err := service.DB.W.ExecContext(context.Background(), query, args...); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
}

func insertUser(t *testing.T, service *Service, id string) {
	t.Helper()
	exec(t, service, `INSERT INTO users (id, username, email, password_hash, role) VALUES (?, ?, ?, 'x', 'user')
		ON CONFLICT(id) DO NOTHING`, id, id, id+"@example.test")
}

func insertAccount(t *testing.T, service *Service, id, user, definition, slug, label string) {
	t.Helper()
	insertUser(t, service, user)
	exec(t, service, `INSERT INTO provider_accounts (id, user_id, definition_id, slug, label, billing_mode) VALUES (?, ?, ?, ?, ?, 'paid')`,
		id, user, definition, slug, label)
}

func insertKey(t *testing.T, service *Service, id, user string) {
	t.Helper()
	insertUser(t, service, user)
	exec(t, service, `INSERT INTO router_keys (id, user_id, name, prefix, key_hash, mode) VALUES (?, ?, ?, ?, ?, 'both')`,
		id, user, id, id, "hash-"+id)
}

// insertModel writes one model row and returns its id.
func insertModel(t *testing.T, service *Service, account, modelID, displayName, canonical string) int64 {
	t.Helper()
	result, err := service.DB.W.ExecContext(context.Background(), `INSERT INTO models (account_id, model_id, display_name, canonical_id)
		VALUES (?, ?, ?, ?)`, account, modelID, displayName, canonical)
	if err != nil {
		t.Fatalf("insert model: %v", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("model id: %v", err)
	}
	return id
}

func ptr[T any](v T) *T { return &v }

func TestRecordAndSummary(t *testing.T) {
	service := testService(t)
	insertKey(t, service, "key1", "u")
	insertAccount(t, service, "acct", "u", "host", "host", "Host")
	now := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	service.SetClock(func() time.Time { return now })
	model := insertModel(t, service, "acct", "one", "One", "one")

	for i, entry := range []Entry{
		{ID: "r1", UserID: "u", RouterKeyID: ptr("key1"), CreatedAt: now.Add(-2 * time.Hour).UnixMilli(), APIFormat: "openai",
			RequestedModel: "auto", ModelRef: &model, ModelLabel: ptr("host/one"), Status: "success", HTTPStatus: ptr(int64(200)),
			InputTokens: 10, OutputTokens: 20, CostUSD: 0.001, LatencyMS: ptr(int64(120)), TTFTMS: ptr(int64(40)), Attempts: 1},
		{ID: "r2", UserID: "u", RouterKeyID: ptr("key1"), CreatedAt: now.Add(-time.Hour).UnixMilli(), APIFormat: "openai",
			RequestedModel: "auto", ModelLabel: ptr("host/one"), Status: "error", ErrorCode: ptr("RATE_LIMITED"),
			InputTokens: 5, Attempts: 3},
	} {
		if i == 0 {
			entry.TrailJSON = `[{"model":"host/one","outcome":"success"}]`
		}
		if err := service.Record(entry); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}
	body, err := service.SummaryOf("u", "24h", now, false)
	if err != nil {
		t.Fatalf("SummaryOf: %v", err)
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var summary map[string]any
	if err := json.Unmarshal(encoded, &summary); err != nil {
		t.Fatalf("decode: %v", err)
	}
	totals := summary["totals"].(map[string]any)
	if totals["requests"] != float64(2) || totals["successes"] != float64(1) || totals["fallbacks"] != float64(1) {
		t.Fatalf("totals = %v", totals)
	}
	if totals["successRate"] != 0.5 || totals["inputTokens"] != float64(15) || totals["outputTokens"] != float64(20) {
		t.Fatalf("totals = %v", totals)
	}
	if totals["avgLatencyMs"] != float64(120) || totals["avgTtftMs"] != float64(40) {
		t.Fatalf("averages = %v", totals)
	}
	// The series is continuous, one bucket per hour of the range.
	series := summary["series"].([]any)
	if len(series) != 24 {
		t.Fatalf("series buckets = %d", len(series))
	}
	var seriesRequests float64
	for _, bucket := range series {
		seriesRequests += bucket.(map[string]any)["requests"].(float64)
	}
	if seriesRequests != 2 {
		t.Fatalf("series requests = %v", seriesRequests)
	}
	topModels := summary["topModels"].([]any)
	if len(topModels) != 1 || topModels[0].(map[string]any)["model_label"] != "host/one" || topModels[0].(map[string]any)["requests"] != float64(2) {
		t.Fatalf("topModels = %v", topModels)
	}
	byKey := summary["byKey"].([]any)
	if len(byKey) != 1 || byKey[0].(map[string]any)["name"] != "key1" || byKey[0].(map[string]any)["requests"] != float64(2) {
		t.Fatalf("byKey = %v", byKey)
	}
	errors := summary["errors"].([]any)
	if len(errors) != 1 || errors[0].(map[string]any)["error_code"] != "RATE_LIMITED" || errors[0].(map[string]any)["count"] != float64(1) {
		t.Fatalf("errors = %v", errors)
	}
	// The totals-only answer has no series or breakdowns at all.
	totalsOnly, err := service.SummaryOf("u", "24h", now, true)
	if err != nil {
		t.Fatalf("SummaryOf: %v", err)
	}
	onlyJSON, _ := json.Marshal(totalsOnly)
	const wantPrefix = `{"range":"24h","bucketMs":3600000,"totals":{"requests":2,"successes":1,"successRate":0.5,`
	if len(onlyJSON) < len(wantPrefix) || string(onlyJSON[:len(wantPrefix)]) != wantPrefix {
		t.Fatalf("totals only = %s", onlyJSON)
	}
	if string(onlyJSON[len(onlyJSON)-1:]) != "}" || len(onlyJSON) != len(wantPrefix)+len(`"costUsd":0.001,"savedUsd":0,"inputTokens":15,"outputTokens":20,"freeRequests":0,"avgLatencyMs":120,"avgTtftMs":40,"fallbacks":1}}`) {
		t.Fatalf("totals only = %s", onlyJSON)
	}
}

func TestSavedUsageIsPricedAtTheMarketRate(t *testing.T) {
	service := testService(t)
	insertAccount(t, service, "acct", "u", "host", "host", "Host")
	model := insertModel(t, service, "acct", "one", "One", "one")
	exec(t, service, `INSERT INTO model_metadata (canonical_id, price_in, price_out) VALUES ('one', 1, 2)`)
	now := time.Now()
	if err := service.Record(Entry{ID: "r1", UserID: "u", CreatedAt: now.UnixMilli(), APIFormat: "openai", RequestedModel: "auto",
		ModelRef: &model, Status: "success", IsFree: true, InputTokens: 1000, OutputTokens: 1000}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	var saved float64
	if err := service.DB.R.QueryRowContext(context.Background(), `SELECT saved_usd FROM requests WHERE id = 'r1'`).Scan(&saved); err != nil {
		t.Fatalf("read saved_usd: %v", err)
	}
	if saved != 0.003 {
		t.Fatalf("saved_usd = %v", saved)
	}
}

func TestRecordDropsLinksToGoneModels(t *testing.T) {
	service := testService(t)
	insertUser(t, service, "u")
	now := time.Now()
	gone := int64(999)
	if err := service.Record(Entry{ID: "r1", UserID: "u", CreatedAt: now.UnixMilli(), APIFormat: "openai", RequestedModel: "auto",
		ModelRef: &gone, AccountID: ptr("gone"), Status: "error"}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	var modelRef sql.NullInt64
	var account sql.NullString
	if err := service.DB.R.QueryRowContext(context.Background(), `SELECT model_ref, account_id FROM requests WHERE id = 'r1'`).Scan(&modelRef, &account); err != nil {
		t.Fatalf("read row: %v", err)
	}
	if modelRef.Valid || account.Valid {
		t.Fatalf("links kept: %v %v", modelRef, account)
	}
}

func TestPruneKeepsTheRetentionWindow(t *testing.T) {
	service := testService(t)
	insertUser(t, service, "u")
	now := time.Now()
	for id, at := range map[string]int64{"old": now.Add(-100 * 24 * time.Hour).UnixMilli(), "new": now.Add(-time.Hour).UnixMilli()} {
		if err := service.Record(Entry{ID: id, UserID: "u", CreatedAt: at, APIFormat: "openai", RequestedModel: "auto", Status: "success"}); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}
	if err := service.Prune(90, now); err != nil {
		t.Fatalf("Prune: %v", err)
	}
	logs, err := service.Logs("u", LogQuery{})
	if err != nil {
		t.Fatalf("Logs: %v", err)
	}
	if len(logs) != 1 || logs[0].ID != "new" {
		t.Fatalf("logs = %+v", logs)
	}
}

// providerName is the narrow provider identity the analytics view consumes.
func providerName(definitionID, slug, label, baseURL string) string {
	switch definitionID {
	case "openai":
		return "OpenAI"
	case "groq":
		return "Groq"
	}
	if label != "" {
		return label
	}
	return slug
}

func TestAnalyticsCountsProvidersNotKeys(t *testing.T) {
	service := testService(t)
	service.ProviderName = providerName
	insertUser(t, service, "u")
	insertUser(t, service, "v")
	for _, account := range [][2]string{{"a", "openai"}, {"b", "openai"}, {"unused", "openai"}, {"old", "openai"}, {"c", "groq"}} {
		insertAccount(t, service, account[0], "u", account[1], account[0], account[0])
	}
	insertKey(t, service, "first", "u")
	insertKey(t, service, "second", "u")
	now := time.Now()
	sequence := 0
	record := func(account *string, key string, at int64, user string) {
		t.Helper()
		sequence++
		entry := Entry{ID: "r" + string(rune('a'+sequence)), UserID: user, RouterKeyID: &key, CreatedAt: at,
			APIFormat: "openai", RequestedModel: "auto", ModelLabel: ptr("test/model"), AccountID: account,
			Status: "success", HTTPStatus: ptr(int64(200)), InputTokens: 1, OutputTokens: 1, CostUSD: 0,
			LatencyMS: ptr(int64(100)), Attempts: 1, TrailJSON: "[]"}
		if err := service.Record(entry); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}
	for i := 0; i < 5; i++ {
		record(ptr("a"), "first", now.UnixMilli(), "u")
	}
	for i := 0; i < 4; i++ {
		record(ptr("b"), "second", now.UnixMilli(), "u")
	}
	for i := 0; i < 6; i++ {
		record(ptr("c"), "first", now.UnixMilli(), "u")
	}
	record(ptr("old"), "first", now.Add(-2*24*time.Hour).UnixMilli(), "u")
	record(ptr("a"), "first", now.UnixMilli(), "v")

	analytics, err := service.AnalyticsOf("u", "24h", now, nil)
	if err != nil {
		t.Fatalf("AnalyticsOf: %v", err)
	}
	if len(analytics.ByProvider) != 2 {
		t.Fatalf("byProvider = %+v", analytics.ByProvider)
	}
	openai := analytics.ByProvider[0]
	if openai.Key != "OpenAI" || openai.Requests != 9 || openai.AccountCount != 2 || openai.RequestsPerAccount == nil || *openai.RequestsPerAccount != 4.5 {
		t.Fatalf("openai = %+v", openai)
	}
	if analytics.ByProviderAverage[0].Key != "Groq" || analytics.ByProviderAverage[0].RequestsPerAccount == nil ||
		*analytics.ByProviderAverage[0].RequestsPerAccount != 6 {
		t.Fatalf("byProviderAverage = %+v", analytics.ByProviderAverage)
	}
	filtered, err := service.AnalyticsOf("u", "24h", now, []string{"first"})
	if err != nil {
		t.Fatalf("AnalyticsOf: %v", err)
	}
	if filtered.Totals.Requests != 11 {
		t.Fatalf("filtered totals = %+v", filtered.Totals)
	}
	var filteredOpenai *ProviderRow
	for i := range filtered.ByProvider {
		if filtered.ByProvider[i].Key == "OpenAI" {
			filteredOpenai = &filtered.ByProvider[i]
		}
	}
	if filteredOpenai == nil || filteredOpenai.AccountCount != 1 || filteredOpenai.RequestsPerAccount == nil || *filteredOpenai.RequestsPerAccount != 5 {
		t.Fatalf("filtered openai = %+v", filtered.ByProvider)
	}
	// A request without its account link keeps its provider label and has no
	// per-account average: missing history must not invent one.
	record(nil, "first", now.UnixMilli(), "u")
	unknown, err := service.AnalyticsOf("u", "24h", now, nil)
	if err != nil {
		t.Fatalf("AnalyticsOf: %v", err)
	}
	var test *ProviderRow
	for i := range unknown.ByProvider {
		if unknown.ByProvider[i].Key == "test" {
			test = &unknown.ByProvider[i]
		}
	}
	if test == nil || test.AccountCount != 0 || test.RequestsPerAccount != nil {
		t.Fatalf("unlinked provider = %+v", test)
	}
}

func TestAnalyticsSeriesAndBreakdowns(t *testing.T) {
	service := testService(t)
	insertKey(t, service, "key1", "u")
	now := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	trail := `[{"model":"host/one","outcome":"success"},{"model":"host/two","outcome":"rate_limit"}]`
	if err := service.Record(Entry{ID: "r1", UserID: "u", RouterKeyID: ptr("key1"), CreatedAt: now.Add(-30 * time.Minute).UnixMilli(),
		APIFormat: "openai", RequestedModel: "auto", ModelLabel: ptr("host/one:free"), Status: "success", Attempts: 2,
		Stream: true, Capability: "chat", LatencyMS: ptr(int64(300)), TTFTMS: ptr(int64(100)), TrailJSON: trail,
		CachedInputTokens: 40, InputTokens: 100, OutputTokens: 50, CostUSD: 0.01, IsFree: true}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	analytics, err := service.AnalyticsOf("u", "24h", now, nil)
	if err != nil {
		t.Fatalf("AnalyticsOf: %v", err)
	}
	if analytics.Totals.CacheHits != 1 || analytics.Totals.CacheHitRate == nil || *analytics.Totals.CacheHitRate != 1 {
		t.Fatalf("totals = %+v", analytics.Totals)
	}
	if analytics.Totals.LatencyP50 == nil || *analytics.Totals.LatencyP50 != 300 || analytics.Totals.TTFTP50 == nil || *analytics.Totals.TTFTP50 != 100 {
		t.Fatalf("timings = %+v", analytics.Totals)
	}
	if len(analytics.ByModel) != 1 || analytics.ByModel[0].Name != "one" {
		t.Fatalf("byModel = %+v", analytics.ByModel)
	}
	if analytics.ByModel[0].Key != "host/one:free" {
		t.Fatalf("byModel key = %+v", analytics.ByModel[0].Key)
	}
	if len(analytics.ByFormat) != 1 || analytics.ByFormat[0].Key != "openai · stream" {
		t.Fatalf("byFormat = %+v", analytics.ByFormat)
	}
	if len(analytics.ByKey) != 1 || analytics.ByKey[0].Key != "key1" {
		t.Fatalf("byKey = %+v", analytics.ByKey)
	}
	if len(analytics.ByCapability) != 1 || analytics.ByCapability[0].Key != "chat" {
		t.Fatalf("byCapability = %+v", analytics.ByCapability)
	}
	if len(analytics.AttemptOutcomes) != 2 || analytics.AttemptOutcomes[0].Key != "success" && analytics.AttemptOutcomes[0].Key != "rate_limit" {
		t.Fatalf("attemptOutcomes = %+v", analytics.AttemptOutcomes)
	}
	histogram := analytics.LatencyHistogram
	if len(histogram) != 8 || histogram[1].UpToMS == nil || *histogram[1].UpToMS != 500 || histogram[1].Count != 1 {
		t.Fatalf("histogram = %+v", histogram)
	}
	if histogram[7].UpToMS != nil {
		t.Fatalf("open ended bucket = %+v", histogram[7])
	}
	var seriesRequests int64
	for _, bucket := range analytics.Series {
		seriesRequests += bucket.Requests
	}
	if len(analytics.Series) != 24 || seriesRequests != 1 {
		t.Fatalf("series = %d buckets, %d requests", len(analytics.Series), seriesRequests)
	}
}

func TestLogsFilterAndShape(t *testing.T) {
	service := testService(t)
	insertKey(t, service, "key1", "u")
	now := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	for i, status := range []string{"success", "error"} {
		entry := Entry{ID: "r" + string(rune('a'+i)), UserID: "u", RouterKeyID: ptr("key1"),
			CreatedAt: now.Add(-time.Duration(i) * time.Minute).UnixMilli(), APIFormat: "openai", RequestedModel: "auto",
			ModelLabel: ptr("host/one"), Status: status, HTTPStatus: ptr(int64(200)), Attempts: 1, TrailJSON: `[{"model":"host/one","outcome":"success"}]`,
			LatencyMS: ptr(int64(150)), Units: 2.5}
		if status == "error" {
			entry.ErrorCode = ptr("UPSTREAM_ERROR")
			entry.HTTPStatus = nil
		}
		if err := service.Record(entry); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}
	logs, err := service.Logs("u", LogQuery{})
	if err != nil {
		t.Fatalf("Logs: %v", err)
	}
	if len(logs) != 2 || logs[0].ID != "ra" || logs[0].KeyName == nil || *logs[0].KeyName != "key1" {
		t.Fatalf("logs = %+v", logs)
	}
	if string(logs[0].Trail) != `[{"model":"host/one","outcome":"success"}]` {
		t.Fatalf("trail = %s", logs[0].Trail)
	}
	if logs[0].Capability != "chat" || logs[0].Units != 2.5 || logs[0].HTTPStatus == nil || *logs[0].HTTPStatus != 200 {
		t.Fatalf("row = %+v", logs[0])
	}
	filtered, err := service.Logs("u", LogQuery{Status: "error"})
	if err != nil {
		t.Fatalf("Logs: %v", err)
	}
	if len(filtered) != 1 || filtered[0].ErrorCode == nil || *filtered[0].ErrorCode != "UPSTREAM_ERROR" || filtered[0].HTTPStatus != nil {
		t.Fatalf("filtered = %+v", filtered)
	}
	before := now.Add(-30 * time.Second).UnixMilli()
	older, err := service.Logs("u", LogQuery{Before: &before})
	if err != nil {
		t.Fatalf("Logs: %v", err)
	}
	if len(older) != 1 || older[0].ID != "rb" {
		t.Fatalf("older = %+v", older)
	}
	limited, err := service.Logs("u", LogQuery{Limit: 1})
	if err != nil {
		t.Fatalf("Logs: %v", err)
	}
	if len(limited) != 1 {
		t.Fatalf("limited = %+v", limited)
	}
}

func TestConnectionHealthNamesWhatTheTrafficShows(t *testing.T) {
	service := testService(t)
	service.AllowanceUsedUp = func(message string) bool {
		return message == "add credits to continue"
	}
	insertAccount(t, service, "a1", "u", "host", "alive", "Alive")
	insertAccount(t, service, "a2", "u", "host", "dead", "Dead")
	insertAccount(t, service, "a3", "u", "host", "quiet", "Quiet")
	now := time.Now()
	// The alive connection answered; the dead one never has.
	alive := `[{"model":"alive/ok","outcome":"success"}]`
	if err := service.Record(Entry{ID: "r1", UserID: "u", CreatedAt: now.UnixMilli(), APIFormat: "openai", RequestedModel: "auto",
		Status: "success", TrailJSON: alive}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	deadTrail := `[{"model":"dead/one","outcome":"upstream_error","message":"boom"}]`
	for i := 0; i < 20; i++ {
		if err := service.Record(Entry{ID: "d" + string(rune('a'+i)), UserID: "u", CreatedAt: now.UnixMilli(), APIFormat: "openai",
			RequestedModel: "auto", Status: "error", TrailJSON: deadTrail}); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}
	health, err := service.Health("u", now)
	if err != nil {
		t.Fatalf("Health: %v", err)
	}
	if health.Requests != 21 || health.Failed != 20 {
		t.Fatalf("health = %+v", health)
	}
	if health.ShortOfCapacity {
		t.Fatal("21 requests is too few to call the router short of capacity")
	}
	if len(health.Failing) != 1 || health.Failing[0].Slug != "dead" || health.Failing[0].Attempts != 20 ||
		health.Failing[0].LastProblem == nil || *health.Failing[0].LastProblem != "boom" {
		t.Fatalf("failing = %+v", health.Failing)
	}
	// A client hanging up is not the router failing it, and a plan allowance
	// running out is a limit, not a broken connection.
	if err := service.Record(Entry{ID: "closed", UserID: "u", CreatedAt: now.UnixMilli(), APIFormat: "openai", RequestedModel: "auto",
		Status: "error", ErrorCode: ptr("client_closed"), TrailJSON: `[{"model":"quiet/one","outcome":"error","message":"add credits to continue"}]`}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	service.SetClock(func() time.Time { return now.Add(10 * time.Minute) })
	recomputed, err := service.Health("u", now.Add(10*time.Minute))
	if err != nil {
		t.Fatalf("Health: %v", err)
	}
	if recomputed.Requests != 21 {
		t.Fatalf("a closed client counted: %+v", recomputed)
	}
	// Within the cache window the answer is reused.
	cached, err := service.Health("u", now.Add(11*time.Minute))
	if err != nil {
		t.Fatalf("Health: %v", err)
	}
	if cached.Requests != recomputed.Requests {
		t.Fatalf("cached = %+v", cached)
	}
}

func TestRoutesServeTheActivityViews(t *testing.T) {
	service := testService(t)
	now := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	service.SetClock(func() time.Time { return now })
	service.CapacityView = func(userID string, at time.Time) (any, error) {
		return map[string]any{"tokensPerRequest": 1500}, nil
	}
	service.QuotasView = func(userID string, at time.Time) (any, error) {
		return map[string]any{"accounts": []any{}}, nil
	}
	mux := http.NewServeMux()
	service.Routes(mux)
	get := func(target string) *httptest.ResponseRecorder {
		t.Helper()
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
		return rec
	}
	if rec := get("/api/v1/usage/summary?only=totals&range=7d"); rec.Code != http.StatusOK ||
		rec.Body.String() != `{"range":"7d","bucketMs":86400000,"totals":{"requests":0,"successes":0,"successRate":null,"costUsd":0,"savedUsd":0,"inputTokens":0,"outputTokens":0,"freeRequests":0,"avgLatencyMs":null,"avgTtftMs":null,"fallbacks":0}}` {
		t.Fatalf("summary = %d %s", rec.Code, rec.Body.String())
	}
	if rec := get("/api/v1/usage/capacity"); rec.Code != http.StatusOK || rec.Body.String() != `{"tokensPerRequest":1500}` {
		t.Fatalf("capacity = %d %s", rec.Code, rec.Body.String())
	}
	if rec := get("/api/v1/usage/quotas"); rec.Code != http.StatusOK || rec.Body.String() != `{"accounts":[]}` {
		t.Fatalf("quotas = %d %s", rec.Code, rec.Body.String())
	}
	if rec := get("/api/v1/usage/logs"); rec.Code != http.StatusOK || rec.Body.String() != `{"logs":[]}` {
		t.Fatalf("logs = %d %s", rec.Code, rec.Body.String())
	}
	if rec := get("/api/v1/usage/health"); rec.Code != http.StatusOK ||
		rec.Body.String() != `{"requests":0,"failed":0,"noCapacity":0,"shortOfCapacity":false,"failing":[]}` {
		t.Fatalf("health = %d %s", rec.Code, rec.Body.String())
	}
	// Without a permission wrapper the routes are open; with one they go
	// through it.
	called := ""
	service.Require = func(permission string) func(http.Handler) http.Handler {
		called = permission
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				next.ServeHTTP(w, r)
			})
		}
	}
	guarded := http.NewServeMux()
	service.Routes(guarded)
	guarded.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/usage/logs", nil))
	if called != "activity.view" {
		t.Fatalf("permission = %q", called)
	}
}
