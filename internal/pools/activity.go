package pools

// What one pool did for and with a person over the last days, read from the
// request metadata that is stored anyway (never content):
//
//	given  what partners used of ours: which connections and models, which
//	       partner, and what it cost us (paid models shared on purpose);
//	taken  what we used of partners: from whom and which models.

import (
	"context"
	"fmt"
	"math"
)

const (
	// activityTop is how many rows each list keeps.
	activityTop = 8
)

// ActivityRanges are the day ranges the pool page may ask for.
var ActivityRanges = []int{7, 30}

// Successful requests in the range, as "r", joined to the partner link "l".
const (
	activityGiven = `FROM requests r JOIN router_keys k ON k.id = r.router_key_id
		JOIN peer_links l ON l.id = k.peer_link_id
		WHERE r.user_id = ? AND l.pool_id = ? AND r.created_at >= ? AND r.status = 'success'`
	activityTaken = `FROM requests r JOIN peer_links l ON l.account_id = r.account_id AND l.user_id = r.user_id
		WHERE r.user_id = ? AND l.pool_id = ? AND r.created_at >= ? AND r.status = 'success'`
	activitySums = `COUNT(*) requests, COALESCE(SUM(r.input_tokens + r.output_tokens), 0) tokens,
		COALESCE(SUM(r.cost_usd), 0) costUsd`
)

// ActivityRow is one line of a pool activity list.
type ActivityRow struct {
	Key      *string `json:"key"`
	Name     *string `json:"name"`
	Requests int64   `json:"requests"`
	Tokens   int64   `json:"tokens"`
	CostUSD  jsNum   `json:"costUsd"`
}

// ActivityTotals is one direction's total.
type ActivityTotals struct {
	Requests int64 `json:"requests"`
	Tokens   int64 `json:"tokens"`
	CostUSD  jsNum `json:"costUsd"`
}

// GivenActivity is what partners used of ours.
type GivenActivity struct {
	ActivityTotals
	ByConnection []ActivityRow `json:"byConnection"`
	ByModel      []ActivityRow `json:"byModel"`
	ByPartner    []ActivityRow `json:"byPartner"`
}

// TakenActivity is what we used of partners.
type TakenActivity struct {
	ActivityTotals
	ByPartner []ActivityRow `json:"byPartner"`
	ByModel   []ActivityRow `json:"byModel"`
}

// PoolActivity is one pool's activity over one range.
type PoolActivity struct {
	Days  int           `json:"days"`
	Given GivenActivity `json:"given"`
	Taken TakenActivity `json:"taken"`
}

// activityGrouped runs one grouped list.
func (s *Service) activityGrouped(ctx context.Context, from, selectList, groupBy string, params []any) ([]ActivityRow, error) {
	rows, err := s.DB.R.QueryContext(ctx, `SELECT `+selectList+`, `+activitySums+` `+from+
		` GROUP BY `+groupBy+` ORDER BY requests DESC LIMIT `+itoa(int64(activityTop)), params...)
	if err != nil {
		return nil, fmt.Errorf("pools: read pool activity: %w", err)
	}
	defer rows.Close()
	list := []ActivityRow{}
	for rows.Next() {
		var (
			row      ActivityRow
			key      *string
			name     *string
			requests int64
			tokens   int64
			cost     float64
		)
		if err := rows.Scan(&key, &name, &requests, &tokens, &cost); err != nil {
			return nil, fmt.Errorf("pools: read pool activity: %w", err)
		}
		row.Key, row.Name = key, name
		row.Requests, row.Tokens = requests, tokens
		row.CostUSD = jsNum(roundTo6(cost))
		list = append(list, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("pools: read pool activity: %w", err)
	}
	return list, nil
}

// activityTotals runs one direction's total.
func (s *Service) activityTotals(ctx context.Context, from string, params []any) (ActivityTotals, error) {
	var (
		requests int64
		tokens   int64
		cost     float64
	)
	if err := s.DB.R.QueryRowContext(ctx, `SELECT `+activitySums+` `+from, params...).
		Scan(&requests, &tokens, &cost); err != nil {
		return ActivityTotals{}, fmt.Errorf("pools: read pool activity: %w", err)
	}
	return ActivityTotals{Requests: requests, Tokens: tokens, CostUSD: jsNum(roundTo6(cost))}, nil
}

// PoolActivityOf is one pool's activity. days is 7 or 30; anything else falls
// back to a week.
func (s *Service) PoolActivityOf(ctx context.Context, userID, poolID string, days int, now int64) (PoolActivity, error) {
	chosen := ActivityRanges[0]
	for _, allowed := range ActivityRanges {
		if days == allowed {
			chosen = days
		}
	}
	params := []any{userID, poolID, now - int64(chosen)*dayMS}
	result := PoolActivity{Days: chosen}

	givenTotals, err := s.activityTotals(ctx, activityGiven, params)
	if err != nil {
		return result, err
	}
	byConnection, err := s.activityGrouped(ctx,
		replaceOnce(activityGiven, "WHERE", "LEFT JOIN provider_accounts a ON a.id = r.account_id WHERE"),
		"r.account_id key, a.label name", "r.account_id", params)
	if err != nil {
		return result, err
	}
	byModel, err := s.activityGrouped(ctx, activityGiven, "r.model_label key, r.model_label name", "r.model_label", params)
	if err != nil {
		return result, err
	}
	byPartner, err := s.activityGrouped(ctx, activityGiven, "l.member_id key, l.name name", "l.member_id", params)
	if err != nil {
		return result, err
	}
	result.Given = GivenActivity{ActivityTotals: givenTotals, ByConnection: byConnection, ByModel: byModel, ByPartner: byPartner}

	takenTotals, err := s.activityTotals(ctx, activityTaken, params)
	if err != nil {
		return result, err
	}
	takenByPartner, err := s.activityGrouped(ctx, activityTaken, "l.member_id key, l.name name", "l.member_id", params)
	if err != nil {
		return result, err
	}
	takenByModel, err := s.activityGrouped(ctx, activityTaken, "r.model_label key, r.model_label name", "r.model_label", params)
	if err != nil {
		return result, err
	}
	result.Taken = TakenActivity{ActivityTotals: takenTotals, ByPartner: takenByPartner, ByModel: takenByModel}
	return result, nil
}

// roundTo6 is n rounded to six decimals.
func roundTo6(value float64) float64 { return math.Round(value*1e6) / 1e6 }

// replaceOnce replaces the first occurrence of a fragment.
func replaceOnce(value, from, to string) string {
	for index := 0; index+len(from) <= len(value); index++ {
		if value[index:index+len(from)] == from {
			return value[:index] + to + value[index+len(from):]
		}
	}
	return value
}
