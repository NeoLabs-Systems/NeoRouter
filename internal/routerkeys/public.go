package routerkeys

import (
	"encoding/json"
	"fmt"
	"math"
)

// PublicKey is a router key as the API shows it.
type PublicKey struct {
	ID              string      `json:"id"`
	Name            string      `json:"name"`
	Prefix          string      `json:"prefix"`
	Mode            string      `json:"mode"`
	AllowFree       bool        `json:"allowFree"`
	AllowPlan       bool        `json:"allowPlan"`
	AllowPaid       bool        `json:"allowPaid"`
	BudgetUSD       *float64    `json:"budgetUsd"`
	BudgetPeriod    string      `json:"budgetPeriod"`
	SpentUSD        float64     `json:"spentUsd"`
	RemainingUSD    *float64    `json:"remainingUsd"`
	PeriodResetsAt  *string     `json:"periodResetsAt"`
	Pacing          *PublicPace `json:"pacing"`
	RPMLimit        *int64      `json:"rpmLimit"`
	ConcurrentLimit *int64      `json:"concurrentLimit"`
	Priority        string      `json:"priority"`
	MaxPricePerMil  *float64    `json:"maxPricePerMillion"`
	TokenLimit      *int64      `json:"tokenLimit"`
	TokenPeriod     string      `json:"tokenPeriod"`
	TokensUsed      *int64      `json:"tokensUsed"`
	TokenResetsAt   *string     `json:"tokenResetsAt"`
	AllowedModels   []string    `json:"allowedModels"`
	PoolAccess      string      `json:"poolAccess"`
	RoutingGroup    *string     `json:"routingGroup"`
	Profile         string      `json:"profile"`
	ExpiresAt       *string     `json:"expiresAt"`
	Expired         bool        `json:"expired"`
	Revoked         bool        `json:"revoked"`
	LastUsedAt      *string     `json:"lastUsedAt"`
	CreatedAt       string      `json:"createdAt"`
	CreatedBy       any         `json:"createdBy"`
	App             *PublicApp  `json:"app"`
}

// PublicPace is how a key's budget is pacing, as the keys API shows it.
type PublicPace struct {
	State    string   `json:"state"`
	Forecast *float64 `json:"forecast"`
}

// PublicApp names the app a key was made for ("Connect with NeoRouter").
type PublicApp struct {
	Name   string   `json:"name"`
	Origin *string  `json:"origin"`
	Scopes []string `json:"scopes"`
}

func round6Ptr(v *float64) *float64 {
	if v == nil {
		return nil
	}
	out := round6(*v)
	return &out
}

// Public renders one stored key for the API.
func (k *Keys) Public(row *Row) (*PublicKey, error) {
	now := k.Now()
	key := &PublicKey{
		ID:              row.ID,
		Name:            row.Name,
		Prefix:          Prefix + "_" + row.Prefix,
		Mode:            row.Mode,
		AllowFree:       row.AllowFree,
		AllowPlan:       row.AllowPlan,
		AllowPaid:       row.Mode != ModeFreeOnly,
		BudgetUSD:       row.BudgetUSD,
		BudgetPeriod:    row.BudgetPeriod,
		RPMLimit:        row.RPMLimit,
		ConcurrentLimit: row.ConcurrentLimit,
		Priority:        priorityOr(row.Priority),
		MaxPricePerMil:  row.MaxPricePerMil,
		TokenLimit:      row.TokenLimit,
		TokenPeriod:     periodOrMonthly(row.TokenPeriod),
		PoolAccess:      PoolOwn,
		RoutingGroup:    row.RoutingGroup,
		Profile:         row.Profile,
		ExpiresAt:       row.ExpiresAt,
		Revoked:         row.RevokedAt != nil,
		LastUsedAt:      row.LastUsedAt,
		CreatedAt:       row.CreatedAt,
		AllowedModels:   []string{},
	}
	if row.PoolAccess == PoolAlso {
		key.PoolAccess = PoolAlso
	}
	if row.AllowedModels != "" {
		if err := json.Unmarshal([]byte(row.AllowedModels), &key.AllowedModels); err != nil {
			return nil, fmt.Errorf("routerkeys: read allowed models: %w", err)
		}
	}
	if row.ExpiresAt != nil {
		if expiry, ok := parseTime(*row.ExpiresAt); ok && expiry <= now.UnixMilli() {
			key.Expired = true
		}
	}
	if k.Budget != nil {
		spent, err := k.Budget.Spent(row.ID, row.BudgetPeriod, now)
		if err != nil {
			return nil, err
		}
		key.SpentUSD = round6(spent)
		if row.BudgetUSD != nil {
			remaining := round6(math.Max(0, *row.BudgetUSD-spent))
			key.RemainingUSD = &remaining
		}
		if end := k.Budget.PeriodEnd(row.BudgetPeriod, now); end != nil {
			resets := isoTime(*end)
			key.PeriodResetsAt = &resets
		}
		if row.Mode != ModeFreeOnly {
			if pace := k.Budget.Pace(row.ID, row.BudgetUSD, row.BudgetPeriod, now); pace != nil {
				key.Pacing = &PublicPace{State: pace.State, Forecast: round6Ptr(pace.Forecast)}
			}
		}
		period := periodOrMonthly(row.TokenPeriod)
		if row.TokenLimit != nil {
			used, err := k.Budget.TokensUsed(row.ID, period, now)
			if err != nil {
				return nil, err
			}
			key.TokensUsed = &used
		}
		if end := k.Budget.PeriodEnd(period, now); end != nil {
			resets := isoTime(*end)
			key.TokenResetsAt = &resets
		}
	}
	if row.CreatedByUserID != nil && k.PublicUser != nil {
		key.CreatedBy = k.PublicUser(*row.CreatedByUserID)
	}
	if row.AppName != nil {
		app := &PublicApp{Name: *row.AppName, Origin: row.AppOrigin, Scopes: []string{}}
		if row.AppScopes != nil && *row.AppScopes != "" {
			if err := json.Unmarshal([]byte(*row.AppScopes), &app.Scopes); err != nil {
				return nil, fmt.Errorf("routerkeys: read app scopes: %w", err)
			}
		}
		key.App = app
	}
	return key, nil
}

func priorityOr(priority string) string {
	if priority == "" {
		return "normal"
	}
	return priority
}

func periodOrMonthly(period string) string {
	if period == "" {
		return "monthly"
	}
	return period
}
