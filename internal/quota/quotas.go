package quota

import (
	"context"
	"encoding/json"
	"time"
)

// Account is what the quotas view needs from internal/providers: the account's
// id, the definition it uses, and the public JSON the providers domain already
// serves for it (embedded verbatim).
type Account struct {
	ID           string
	DefinitionID string
	JSON         json.RawMessage
}

// Concurrency is what routing knows about an account's simultaneous requests.
type Concurrency struct {
	Limit  int    `json:"limit"`
	Source string `json:"source"`
}

// Quotas is the per-account quota overview served at /api/v1/usage/quotas: the
// shared pool windows, every model with a known limit, what was observed about
// the account, and the stated limits in a readable summary.
type Quotas struct {
	Service *Service
	Now     func() time.Time
	// Capabilities lists every kind of model the catalog knows: an account's
	// allowance is spent by all of them, not only by chat.
	Capabilities func() []string
	// Accounts lists a person's provider accounts.
	Accounts func(userID string) []Account
	// ConcurrencyLimit is what routing learned or was told about the account's
	// simultaneous requests, nil when nothing is known.
	ConcurrencyLimit func(accountID, definitionID string) *Concurrency
}

type quotasView struct {
	Accounts []quotasAccount `json:"accounts"`
}

type quotasAccount struct {
	Account       json.RawMessage `json:"account"`
	Pool          []Detail        `json:"pool"`
	Models        []quotasModel   `json:"models"`
	Probes        []State         `json:"probes"`
	Concurrency   *Concurrency    `json:"concurrency"`
	Slots         json.RawMessage `json:"slots,omitempty"`
	AllowancePace json.RawMessage `json:"allowancePace,omitempty"`
	Limits        []SummaryRow    `json:"limits"`
}

type quotasModel struct {
	Ref     int64    `json:"ref"`
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	IsFree  bool     `json:"isFree"`
	Windows []Detail `json:"windows"`
}

// View builds the quotas overview for one person.
func (q *Quotas) View(userID string) (any, error) {
	now := time.Now()
	if q.Now != nil {
		now = q.Now()
	}
	accounts := []Account{}
	if q.Accounts != nil {
		accounts = q.Accounts(userID)
	}
	groups := make([]quotasAccount, 0, len(accounts))
	index := make(map[string]int, len(accounts))
	for _, account := range accounts {
		probes, err := q.Service.AccountStateNow(account.ID, now)
		if err != nil {
			return nil, err
		}
		var concurrency *Concurrency
		if q.ConcurrencyLimit != nil {
			concurrency = q.ConcurrencyLimit(account.ID, account.DefinitionID)
		}
		index[account.ID] = len(groups)
		groups = append(groups, quotasAccount{
			Account:     account.JSON,
			Pool:        []Detail{},
			Models:      []quotasModel{},
			Probes:      probes,
			Concurrency: concurrency,
		})
	}
	var entries []Entry
	if q.Service.catalog != nil && q.Capabilities != nil {
		for _, kind := range q.Capabilities() {
			listed, err := q.Service.catalog.ListForUser(context.Background(), userID, kind, false)
			if err != nil {
				return nil, err
			}
			entries = append(entries, listed...)
		}
	}
	for _, entry := range entries {
		position, ok := index[entry.AccountID]
		if !ok {
			continue
		}
		group := &groups[position]
		if entry.IsFlatRate && group.Slots == nil {
			group.Slots = entry.Slots
		}
		if entry.IsPlan && !entry.IsFlatRate && group.AllowancePace == nil {
			group.AllowancePace = entry.AllowancePace
		}
		evaluation, err := q.Service.Evaluate(entry, 0, now)
		if err != nil {
			return nil, err
		}
		var windows []Detail = []Detail{}
		for _, detail := range evaluation.Details {
			if detail.Kind == KindPool {
				duplicate := false
				for _, existing := range group.Pool {
					if existing.Metric == detail.Metric {
						duplicate = true
						break
					}
				}
				if !duplicate {
					group.Pool = append(group.Pool, detail)
				}
				continue
			}
			windows = append(windows, detail)
		}
		if len(windows) > 0 || entry.IsFree {
			group.Models = append(group.Models, quotasModel{
				Ref:     entry.Ref,
				ID:      entry.ID,
				Name:    entry.Name,
				IsFree:  entry.IsFree,
				Windows: windows,
			})
		}
	}
	for i := range groups {
		models := make([]SummaryModel, 0, len(groups[i].Models))
		for _, model := range groups[i].Models {
			models = append(models, SummaryModel{Windows: model.Windows})
		}
		groups[i].Limits = Summarize(SummaryGroup{Pool: groups[i].Pool, Models: models})
	}
	return quotasView{Accounts: groups}, nil
}
