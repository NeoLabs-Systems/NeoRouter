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

// Health is how the connections really did over the last day, and whether
// requests go unanswered.
type Health struct {
	Requests        int64         `json:"requests"`
	Failed          int64         `json:"failed"`
	NoCapacity      int64         `json:"noCapacity"`
	ShortOfCapacity bool          `json:"shortOfCapacity"`
	Failing         []FailingSlug `json:"failing"`
}

// FailingSlug is one connection that keeps failing.
type FailingSlug struct {
	AccountID   string  `json:"accountId"`
	Slug        string  `json:"slug"`
	Label       string  `json:"label"`
	Attempts    int64   `json:"attempts"`
	Successes   int64   `json:"successes"`
	LastProblem *string `json:"lastProblem"`
}

const (
	healthDayMS        = int64(86_400_000)
	healthCacheMS      = int64(5 * 60 * 1000)
	healthMinAttempts  = 20
	healthFailingBelow = 0.05
	healthMinRequests  = 50
	healthFailedShare  = 0.3
)

// notTheConnection are the attempt outcomes that say nothing about the
// connection itself.
var notTheConnection = map[string]bool{
	"hedge_lost":      true,
	"rate_limit":      true,
	"quota_exhausted": true,
	"context_length":  true,
	"bad_request":     true,
	"truncated":       true,
	"aborted":         true,
	"stream_aborted":  true,
}

type healthEntry struct {
	accountID     string
	slug          string
	label         string
	attempts      int64
	successes     int64
	lastProblem   *string
	lastProblemAt int64
}

// Health is the connection health of a person's traffic. Reading a day of
// requests is not free, so the answer is kept for a few minutes.
func (s *Service) Health(userID string, now time.Time) (*Health, error) {
	cached, ok := s.cachedHealth(userID, now.UnixMilli())
	if ok {
		return cached, nil
	}
	health, err := s.computeHealth(userID, now)
	if err != nil {
		return nil, err
	}
	s.storeHealth(userID, now.UnixMilli(), health)
	return health, nil
}

func (s *Service) computeHealth(userID string, now time.Time) (*Health, error) {
	ctx := context.Background()
	connectionRows, err := s.DB.R.QueryContext(ctx,
		`SELECT id, slug, label FROM provider_accounts WHERE user_id = ? AND enabled = 1`, userID)
	if err != nil {
		return nil, fmt.Errorf("usage: read connections: %w", err)
	}
	order := []string{}
	bySlug := map[string]*healthEntry{}
	for connectionRows.Next() {
		entry := &healthEntry{}
		if err := connectionRows.Scan(&entry.accountID, &entry.slug, &entry.label); err != nil {
			connectionRows.Close()
			return nil, fmt.Errorf("usage: read connections: %w", err)
		}
		bySlug[entry.slug] = entry
		order = append(order, entry.slug)
	}
	connectionRows.Close()
	if err := connectionRows.Err(); err != nil {
		return nil, fmt.Errorf("usage: read connections: %w", err)
	}

	health := &Health{Failing: []FailingSlug{}}
	rows, err := s.DB.R.QueryContext(ctx,
		`SELECT created_at, status, error_code, trail_json FROM requests WHERE user_id = ? AND created_at >= ?`,
		userID, now.UnixMilli()-healthDayMS)
	if err != nil {
		return nil, fmt.Errorf("usage: read requests: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var createdAt int64
		var status string
		var errorCode, trailJSON sql.NullString
		if err := rows.Scan(&createdAt, &status, &errorCode, &trailJSON); err != nil {
			return nil, fmt.Errorf("usage: read requests: %w", err)
		}
		// A client hanging up is not the router failing it.
		if errorCode.String != "client_closed" {
			health.Requests++
			if status == "error" {
				health.Failed++
			}
			if strings.Contains(strings.ToLower(errorCode.String), "no_model_available") {
				health.NoCapacity++
			}
		}
		for _, step := range trailSteps(trailJSON.String) {
			// Only what says the connection itself is broken counts. A long shot
			// was expected to fail; limits, a request the model cannot take or a
			// lost race say nothing about the connection. A used-up plan
			// allowance (recorded before it was read that way) is a limit too.
			if step.Luck || notTheConnection[step.Outcome] {
				continue
			}
			if s.AllowanceUsedUp != nil && s.AllowanceUsedUp(step.Message) {
				continue
			}
			slug, _, _ := strings.Cut(step.Model, "/")
			entry := bySlug[slug]
			if entry == nil {
				continue
			}
			entry.attempts++
			if step.Outcome == "success" {
				entry.successes++
				continue
			}
			if step.Message != "" && createdAt >= entry.lastProblemAt {
				message := step.Message
				entry.lastProblem = &message
				entry.lastProblemAt = createdAt
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("usage: read requests: %w", err)
	}

	health.ShortOfCapacity = health.Requests >= healthMinRequests && float64(health.Failed)/float64(health.Requests) >= healthFailedShare
	failing := []*healthEntry{}
	for _, slug := range order {
		entry := bySlug[slug]
		if entry.attempts == 0 {
			continue
		}
		if entry.attempts >= healthMinAttempts && float64(entry.successes)/float64(entry.attempts) < healthFailingBelow {
			failing = append(failing, entry)
		}
	}
	sort.SliceStable(failing, func(i, j int) bool { return failing[i].attempts > failing[j].attempts })
	for _, entry := range failing {
		health.Failing = append(health.Failing, FailingSlug{
			AccountID:   entry.accountID,
			Slug:        entry.slug,
			Label:       entry.label,
			Attempts:    entry.attempts,
			Successes:   entry.successes,
			LastProblem: entry.lastProblem,
		})
	}
	return health, nil
}

// trailStep is one attempt of a stored trail.
type trailStep struct {
	Model   string `json:"model"`
	Outcome string `json:"outcome"`
	Message string `json:"message"`
	Luck    bool   `json:"luck"`
}

func trailSteps(trailJSON string) []trailStep {
	if trailJSON == "" {
		return nil
	}
	var steps []trailStep
	if err := json.Unmarshal([]byte(trailJSON), &steps); err != nil {
		return nil
	}
	return steps
}
