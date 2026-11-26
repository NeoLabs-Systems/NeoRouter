package pools

// What each partner took from us ("given") and what we took from each partner
// ("taken"), per UTC day, so both sides agree on where a day ends. Counts only,
// never content (traffic.js).

import (
	"context"
	"fmt"
	"math"

	"github.com/NeoLabs-Systems/neorouter/internal/routerkeys"
)

const dayMS = 86_400_000

// trafficAdd adds one day's counts for one direction.
func (s *Service) trafficAdd(ctx context.Context, linkID, dir string, requests, tokens int64, now int64) error {
	if linkID == "" || (requests == 0 && tokens == 0) {
		return nil
	}
	if tokens < 0 {
		tokens = 0
	}
	if _, err := s.DB.W.ExecContext(ctx, `INSERT INTO peer_usage_daily (link_id, day, direction, requests, tokens)
		VALUES (?, ?, ?, ?, ?) ON CONFLICT (link_id, day, direction) DO UPDATE SET
		requests = requests + excluded.requests, tokens = tokens + excluded.tokens`,
		linkID, dayOf(now), dir, requests, int64(math.Round(float64(tokens)))); err != nil {
		return fmt.Errorf("pools: count partner traffic: %w", err)
	}
	return nil
}

// trafficToday is one partner's counts for one day and direction.
func (s *Service) trafficToday(ctx context.Context, linkID, dir string, now int64) (balanceSide, error) {
	var side balanceSide
	err := s.DB.R.QueryRowContext(ctx, `SELECT requests, tokens FROM peer_usage_daily
		WHERE link_id = ? AND day = ? AND direction = ?`, linkID, dayOf(now), dir).
		Scan(&side.Requests, &side.Tokens)
	if err != nil {
		if isNoRows(err) {
			return balanceSide{}, nil
		}
		return balanceSide{}, fmt.Errorf("pools: read partner traffic: %w", err)
	}
	return side, nil
}

// trafficWindow is one partner's totals over the last days, today included.
func (s *Service) trafficWindow(ctx context.Context, linkID string, days int, now int64) (struct {
	Given balanceSide
	Taken balanceSide
}, error) {
	result := struct {
		Given balanceSide
		Taken balanceSide
	}{}
	if days < 1 {
		days = 1
	}
	since := dayOf(now - int64(days-1)*dayMS)
	rows, err := s.DB.R.QueryContext(ctx, `SELECT direction, SUM(requests), SUM(tokens) FROM peer_usage_daily
		WHERE link_id = ? AND day >= ? GROUP BY direction`, linkID, since)
	if err != nil {
		return result, fmt.Errorf("pools: read partner traffic: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			dir      string
			requests *int64
			tokens   *int64
			side     balanceSide
		)
		if err := rows.Scan(&dir, &requests, &tokens); err != nil {
			return result, fmt.Errorf("pools: read partner traffic: %w", err)
		}
		if requests != nil {
			side.Requests = *requests
		}
		if tokens != nil {
			side.Tokens = *tokens
		}
		if dir == "given" {
			result.Given = side
		} else {
			result.Taken = side
		}
	}
	if err := rows.Err(); err != nil {
		return result, fmt.Errorf("pools: read partner traffic: %w", err)
	}
	return result, nil
}

// trafficNextReset is when the day's share resets (next UTC midnight).
func trafficNextReset(now int64) int64 {
	return ((now + 1 + dayMS - 1) / dayMS) * dayMS
}

// RecordFinished counts a finished request: tokens a partner used on our
// connections, and what we used on a partner's (a request through their
// account). The gateway calls it when a request ends.
func (s *Service) RecordFinished(ctx context.Context, key *routerkeys.Row, accountID string, success bool, tokens int64) {
	if key != nil && key.PeerLinkID != nil && *key.PeerLinkID != "" {
		if err := s.trafficAdd(ctx, *key.PeerLinkID, "given", 0, tokens, s.nowMS()); err != nil {
			s.log().Warn("Could not count what a partner used", "error", err)
		}
	}
	if accountID == "" || !success {
		return
	}
	link, err := s.linkForAccount(ctx, accountID)
	if err != nil {
		s.log().Warn("Could not look up a partner link", "error", err)
		return
	}
	if link != nil {
		if err := s.trafficAdd(ctx, link.ID, "taken", 1, tokens, s.nowMS()); err != nil {
			s.log().Warn("Could not count what we used of a partner", "error", err)
		}
	}
}
