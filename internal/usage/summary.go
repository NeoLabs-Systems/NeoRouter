package usage

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// Totals is what a set of requests adds up to.
type Totals struct {
	Requests     int64    `json:"requests"`
	Successes    int64    `json:"successes"`
	SuccessRate  *float64 `json:"successRate"`
	CostUSD      float64  `json:"costUsd"`
	SavedUSD     float64  `json:"savedUsd"`
	InputTokens  int64    `json:"inputTokens"`
	OutputTokens int64    `json:"outputTokens"`
	FreeRequests int64    `json:"freeRequests"`
	AvgLatencyMS *float64 `json:"avgLatencyMs"`
	AvgTTFTMS    *float64 `json:"avgTtftMs"`
	Fallbacks    int64    `json:"fallbacks"`
}

// SummaryBucket is one bucket of the activity series.
type SummaryBucket struct {
	Bucket    int64   `json:"bucket"`
	Requests  int64   `json:"requests"`
	Successes int64   `json:"successes"`
	Cost      float64 `json:"cost"`
	Tokens    int64   `json:"tokens"`
}

// SummaryModel is one row of the busiest models.
type SummaryModel struct {
	ModelLabel *string  `json:"model_label"`
	Requests   int64    `json:"requests"`
	Successes  int64    `json:"successes"`
	Cost       float64  `json:"cost"`
	Tokens     int64    `json:"tokens"`
	AvgLatency *float64 `json:"avg_latency"`
}

// SummaryKey is one row of the per-key breakdown.
type SummaryKey struct {
	ID       *string `json:"id"`
	Name     *string `json:"name"`
	Requests int64   `json:"requests"`
	Cost     float64 `json:"cost"`
}

// SummaryError is one row of the error breakdown.
type SummaryError struct {
	ErrorCode *string `json:"error_code"`
	Count     int64   `json:"count"`
}

// SummaryTotalsOnly is the summary of a view that needs no series.
type SummaryTotalsOnly struct {
	Range    string `json:"range"`
	BucketMS int64  `json:"bucketMs"`
	Totals   Totals `json:"totals"`
}

// Summary is everything the activity page charts for a range.
type Summary struct {
	Range     string          `json:"range"`
	BucketMS  int64           `json:"bucketMs"`
	Totals    Totals          `json:"totals"`
	Series    []SummaryBucket `json:"series"`
	TopModels []SummaryModel  `json:"topModels"`
	ByKey     []SummaryKey    `json:"byKey"`
	Errors    []SummaryError  `json:"errors"`
}

// SummaryOf is a person's activity over a range. onlyTotals skips the series and
// the breakdowns, which the home page does not need.
func (s *Service) SummaryOf(userID, rangeName string, now time.Time, onlyTotals bool) (any, error) {
	ctx := context.Background()
	since := rangeStart(rangeName, now)
	bucketMS := int64(86_400_000)
	if rangeName == "24h" {
		bucketMS = 3_600_000
	}
	var requests, successes, inputTokens, outputTokens, freeRequests, fallbacks sql.NullInt64
	var cost, saved, avgLatency, avgTTFT sql.NullFloat64
	err := s.DB.R.QueryRowContext(ctx, `SELECT COUNT(*) requests, SUM(status = 'success') successes, SUM(cost_usd) cost, SUM(saved_usd) saved,
		SUM(input_tokens) input_tokens, SUM(output_tokens) output_tokens, SUM(is_free AND status = 'success') free_requests,
		AVG(CASE WHEN status = 'success' THEN latency_ms END) avg_latency, AVG(CASE WHEN status = 'success' THEN ttft_ms END) avg_ttft,
		SUM(attempts > 1) fallbacks
		FROM requests WHERE user_id = ? AND created_at >= ?`, userID, since).Scan(&requests, &successes, &cost, &saved,
		&inputTokens, &outputTokens, &freeRequests, &avgLatency, &avgTTFT, &fallbacks)
	if err != nil {
		return nil, fmt.Errorf("usage: read totals: %w", err)
	}
	totals := Totals{
		Requests:     requests.Int64,
		Successes:    successes.Int64,
		CostUSD:      nullFloat(cost),
		SavedUSD:     nullFloat(saved),
		InputTokens:  inputTokens.Int64,
		OutputTokens: outputTokens.Int64,
		FreeRequests: freeRequests.Int64,
		Fallbacks:    fallbacks.Int64,
	}
	if totals.Requests > 0 {
		rate := float64(totals.Successes) / float64(totals.Requests)
		totals.SuccessRate = &rate
	}
	if avgLatency.Valid {
		totals.AvgLatencyMS = floatPtr(avgLatency.Float64)
	}
	if avgTTFT.Valid {
		totals.AvgTTFTMS = floatPtr(avgTTFT.Float64)
	}
	if onlyTotals {
		return SummaryTotalsOnly{Range: rangeName, BucketMS: bucketMS, Totals: totals}, nil
	}
	summary := Summary{
		Range:     rangeName,
		BucketMS:  bucketMS,
		Totals:    totals,
		Series:    []SummaryBucket{},
		TopModels: []SummaryModel{},
		ByKey:     []SummaryKey{},
		Errors:    []SummaryError{},
	}
	if summary.Series, err = s.summarySeries(userID, since, now, bucketMS); err != nil {
		return nil, err
	}
	if summary.TopModels, err = s.summaryModels(userID, since); err != nil {
		return nil, err
	}
	if summary.ByKey, err = s.summaryKeys(userID, since); err != nil {
		return nil, err
	}
	if summary.Errors, err = s.summaryErrors(userID, since); err != nil {
		return nil, err
	}
	return summary, nil
}

// summarySeries is a continuous series with empty buckets, so charts have a
// stable time axis.
func (s *Service) summarySeries(userID string, since int64, now time.Time, bucketMS int64) ([]SummaryBucket, error) {
	rows, err := s.DB.R.QueryContext(context.Background(), `SELECT CAST(created_at / ? AS INTEGER) * ? bucket, COUNT(*) requests,
		SUM(status = 'success') successes, SUM(cost_usd) cost, SUM(input_tokens + output_tokens) tokens
		FROM requests WHERE user_id = ? AND created_at >= ? GROUP BY bucket ORDER BY bucket`, bucketMS, bucketMS, userID, since)
	if err != nil {
		return nil, fmt.Errorf("usage: read series: %w", err)
	}
	defer rows.Close()
	byBucket := map[int64]SummaryBucket{}
	for rows.Next() {
		var bucket SummaryBucket
		var successes, tokens sql.NullInt64
		var cost sql.NullFloat64
		if err := rows.Scan(&bucket.Bucket, &bucket.Requests, &successes, &cost, &tokens); err != nil {
			return nil, fmt.Errorf("usage: read series: %w", err)
		}
		bucket.Successes, bucket.Tokens, bucket.Cost = successes.Int64, tokens.Int64, nullFloat(cost)
		byBucket[bucket.Bucket] = bucket
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("usage: read series: %w", err)
	}
	out := []SummaryBucket{}
	for bucket := since/bucketMS*bucketMS + bucketMS; bucket <= now.UnixMilli(); bucket += bucketMS {
		if row, ok := byBucket[bucket]; ok {
			out = append(out, row)
			continue
		}
		out = append(out, SummaryBucket{Bucket: bucket})
	}
	return out, nil
}

func (s *Service) summaryModels(userID string, since int64) ([]SummaryModel, error) {
	rows, err := s.DB.R.QueryContext(context.Background(), `SELECT model_label, COUNT(*) requests, SUM(status = 'success') successes,
		SUM(cost_usd) cost, SUM(input_tokens + output_tokens) tokens, AVG(latency_ms) avg_latency
		FROM requests WHERE user_id = ? AND created_at >= ? AND model_label IS NOT NULL GROUP BY model_label ORDER BY requests DESC LIMIT 10`,
		userID, since)
	if err != nil {
		return nil, fmt.Errorf("usage: read top models: %w", err)
	}
	defer rows.Close()
	out := []SummaryModel{}
	for rows.Next() {
		var row SummaryModel
		var label sql.NullString
		var successes, tokens sql.NullInt64
		var cost, latency sql.NullFloat64
		if err := rows.Scan(&label, &row.Requests, &successes, &cost, &tokens, &latency); err != nil {
			return nil, fmt.Errorf("usage: read top models: %w", err)
		}
		row.ModelLabel = nullString(label)
		row.Successes, row.Tokens, row.Cost = successes.Int64, tokens.Int64, nullFloat(cost)
		if latency.Valid {
			row.AvgLatency = floatPtr(latency.Float64)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("usage: read top models: %w", err)
	}
	return out, nil
}

func (s *Service) summaryKeys(userID string, since int64) ([]SummaryKey, error) {
	rows, err := s.DB.R.QueryContext(context.Background(), `SELECT r.router_key_id id, k.name, COUNT(*) requests, SUM(r.cost_usd) cost
		FROM requests r LEFT JOIN router_keys k ON k.id = r.router_key_id
		WHERE r.user_id = ? AND r.created_at >= ? GROUP BY r.router_key_id ORDER BY requests DESC`, userID, since)
	if err != nil {
		return nil, fmt.Errorf("usage: read key breakdown: %w", err)
	}
	defer rows.Close()
	out := []SummaryKey{}
	for rows.Next() {
		var row SummaryKey
		var id, name sql.NullString
		var cost sql.NullFloat64
		if err := rows.Scan(&id, &name, &row.Requests, &cost); err != nil {
			return nil, fmt.Errorf("usage: read key breakdown: %w", err)
		}
		row.ID, row.Name, row.Cost = nullString(id), nullString(name), nullFloat(cost)
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("usage: read key breakdown: %w", err)
	}
	return out, nil
}

func (s *Service) summaryErrors(userID string, since int64) ([]SummaryError, error) {
	rows, err := s.DB.R.QueryContext(context.Background(), `SELECT error_code, COUNT(*) count FROM requests
		WHERE user_id = ? AND created_at >= ? AND status = 'error' GROUP BY error_code ORDER BY count DESC LIMIT 10`, userID, since)
	if err != nil {
		return nil, fmt.Errorf("usage: read errors: %w", err)
	}
	defer rows.Close()
	out := []SummaryError{}
	for rows.Next() {
		var row SummaryError
		var code sql.NullString
		if err := rows.Scan(&code, &row.Count); err != nil {
			return nil, fmt.Errorf("usage: read errors: %w", err)
		}
		row.ErrorCode = nullString(code)
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("usage: read errors: %w", err)
	}
	return out, nil
}

func nullString(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	out := v.String
	return &out
}
