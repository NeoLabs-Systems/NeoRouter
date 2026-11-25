package routing

import (
	"strconv"
	"time"
)

// What a router key may send right now, so a client can pace itself instead of
// sending requests that would only be refused or queued: the key's own
// per-minute limit, and how many requests the connected accounts take at once
// (stated, documented or learned per account; see concurrency.go).

// RequestBudget is a key's allowance right now.
type RequestBudget struct {
	Requests   *RequestRateBudget `json:"requests"`
	Concurrent ConcurrentBudget   `json:"concurrent"`
}

// RequestRateBudget is a key's per-minute request allowance.
type RequestRateBudget struct {
	PerMinute    int `json:"perMinute"`
	Remaining    int `json:"remaining"`
	ResetSeconds int `json:"resetSeconds"`
	IntervalMs   int `json:"intervalMs"`
}

// ConcurrentBudget is how many requests the accounts take at once. Limit is
// nil when at least one usable account has no known cap (then nothing is
// promised).
type ConcurrentBudget struct {
	Limit     *int `json:"limit"`
	InFlight  int  `json:"inFlight"`
	Available int  `json:"available"`
}

// usableEntries are the models a key can reach at all.
func (s *Service) usableEntries(key RouterKey) []*Entry {
	var out []*Entry
	for _, entry := range s.catalogList(key.UserID, capabilityChat) {
		if !entry.Enabled || !entry.AccountEnabled || !entry.Available {
			continue
		}
		if KindAllowed(entry, key, false) != "" {
			continue
		}
		out = append(out, entry)
	}
	return out
}

// RequestBudgetForKey is what a key may send right now.
func (s *Service) RequestBudgetForKey(key RouterKey, now time.Time) RequestBudget {
	var rpm *RPMState
	if s.opts.Keys != nil {
		rpm = s.opts.Keys.RPMState(key, now)
	}
	accounts := map[string]*Entry{}
	var order []string
	for _, entry := range s.usableEntries(key) {
		if _, ok := accounts[entry.AccountID]; !ok {
			accounts[entry.AccountID] = entry
			order = append(order, entry.AccountID)
		}
	}
	limit := 0
	unknown := len(accounts) == 0
	inFlight := 0
	for _, accountID := range order {
		entry := accounts[accountID]
		inFlight += s.load.inflight(entry).Account
		if known := s.conc.limitFor(entry, now); known != nil {
			limit += known.Limit
		} else {
			unknown = true
		}
	}
	out := RequestBudget{}
	if rpm != nil && rpm.Limit > 0 {
		out.Requests = &RequestRateBudget{
			PerMinute:    rpm.Limit,
			Remaining:    rpm.Remaining,
			ResetSeconds: rpm.ResetSeconds,
			IntervalMs:   (60_000 + rpm.Limit - 1) / rpm.Limit,
		}
	}
	if key.ConcurrentLimit != nil {
		ownLimit := *key.ConcurrentLimit
		ownInFlight := 0
		if s.opts.Keys != nil {
			ownInFlight = s.opts.Keys.InFlight(key)
		}
		ownAvailable := maxInt(0, ownLimit-ownInFlight)
		providerAvailable := 0
		if !unknown {
			providerAvailable = maxInt(0, limit-inFlight)
			if providerAvailable > ownAvailable {
				providerAvailable = ownAvailable
			}
		} else {
			providerAvailable = ownAvailable
		}
		out.Concurrent = ConcurrentBudget{Limit: &ownLimit, InFlight: ownInFlight, Available: providerAvailable}
		return out
	}
	out.Concurrent = ConcurrentBudget{InFlight: inFlight}
	if !unknown {
		out.Concurrent.Limit = &limit
		out.Concurrent.Available = maxInt(0, limit-inFlight)
	}
	return out
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// BudgetHeaders are the pacing headers a gateway response carries: OpenAI's own
// x-ratelimit-* names where they fit, so SDKs and proxies that already read
// them pace themselves; concurrency has no OpenAI header.
func BudgetHeaders(budget RequestBudget) map[string]string {
	out := map[string]string{}
	if budget.Requests != nil {
		out["x-ratelimit-limit-requests"] = strconv.Itoa(budget.Requests.PerMinute)
		out["x-ratelimit-remaining-requests"] = strconv.Itoa(budget.Requests.Remaining)
		out["x-ratelimit-reset-requests"] = strconv.Itoa(budget.Requests.ResetSeconds) + "s"
	}
	if budget.Concurrent.Limit != nil {
		out["X-NeoRouter-Concurrency-Limit"] = strconv.Itoa(*budget.Concurrent.Limit)
		out["X-NeoRouter-Concurrency-Available"] = strconv.Itoa(budget.Concurrent.Available)
	}
	return out
}
