package quota

import (
	"context"
	"database/sql"
	"math"
	"time"
)

// DefaultTokensPerRequest is the size of one answer when there is no history to
// go by yet.
const DefaultTokensPerRequest = 1500

// capacityHistoryMS is how far back the capacity maths looks.
const capacityHistoryMS = int64(30 * 86_400_000)

// Allowance is a scope's known limits per day and per minute, in requests and
// in tokens. A nil field is an unknown limit.
type Allowance struct {
	PerDay          *float64
	PerMinute       *float64
	TokensPerDay    *float64
	TokensPerMinute *float64
}

// Observed is what the free models really delivered over the last month.
type Observed struct {
	BestDay    float64 `json:"bestDay"`
	AverageDay float64 `json:"averageDay"`
	Days       int64   `json:"days"`
}

// Capacity is what the free models could take around the clock.
type Capacity struct {
	PerDay                float64  `json:"perDay"`
	PerHour               float64  `json:"perHour"`
	PerMinuteOnly         float64  `json:"perMinuteOnly"`
	TokensPerDay          float64  `json:"tokensPerDay"`
	TokensPerHour         float64  `json:"tokensPerHour"`
	TokensPerMinuteOnly   float64  `json:"tokensPerMinuteOnly"`
	TokensPerRequest      int64    `json:"tokensPerRequest"`
	ProvidersCounted      int      `json:"providersCounted"`
	ModelsWithoutLimit    int      `json:"modelsWithoutLimit"`
	ProvidersWithoutLimit int      `json:"providersWithoutLimit"`
	Observed              Observed `json:"observed"`
}

// TokensPerRequest is the person's own average tokens per answered request over
// the last month, or the default when there is no history.
func (s *Service) TokensPerRequest(userID string, now time.Time) (int64, error) {
	var average sql.NullFloat64
	err := s.db.R.QueryRowContext(context.Background(), `SELECT AVG(input_tokens + output_tokens) tokens FROM requests
		WHERE user_id = ? AND created_at >= ? AND status = 'success' AND input_tokens + output_tokens > 0`,
		userID, now.UnixMilli()-capacityHistoryMS).Scan(&average)
	if err != nil {
		return 0, wrapRead("tokens per request", err)
	}
	if !average.Valid || average.Float64 == 0 {
		return DefaultTokensPerRequest, nil
	}
	return int64(jsRound(average.Float64)), nil
}

// ObservedFree is what the free models delivered: the most free tokens answered
// in one day over the last month and the average over the active days.
func (s *Service) ObservedFree(userID string, now time.Time) (Observed, error) {
	var best, average sql.NullFloat64
	var days int64
	err := s.db.R.QueryRowContext(context.Background(), `SELECT MAX(tokens) best, AVG(tokens) average, COUNT(*) days FROM (
		  SELECT date(created_at / 1000, 'unixepoch') day, SUM(input_tokens + output_tokens) tokens FROM requests
		  WHERE user_id = ? AND created_at >= ? AND status = 'success' AND is_free = 1 GROUP BY day)`,
		userID, now.UnixMilli()-capacityHistoryMS).Scan(&best, &average, &days)
	if err != nil {
		return Observed{}, wrapRead("observed free usage", err)
	}
	return Observed{BestDay: jsRound(deref(nullFloat(best))), AverageDay: jsRound(deref(nullFloat(average))), Days: days}, nil
}

// LimitsOf is a scope's known limits expressed per day and per minute, in
// requests and in tokens. A published token limit wins; otherwise requests are
// turned into tokens at perRequest tokens each.
func (s *Service) LimitsOf(scope Scope, perRequest int64) (Allowance, error) {
	known, err := s.EffectiveLimits(scope)
	if err != nil {
		return Allowance{}, err
	}
	per := float64(perRequest)
	// Some providers (Cerebras on paid keys) report a "daily" limit that is
	// just the minute limit times 1440. That is no cap at all, and adding it as
	// one made the total reach hundreds of millions of tokens.
	stretched := func(day, minute string) bool {
		d, dOK := known[day]
		m, mOK := known[minute]
		return dOK && mOK && d.Value >= m.Value*1440*0.9
	}
	if stretched(RPD, RPM) {
		delete(known, RPD)
	}
	if stretched(TPD, TPM) {
		delete(known, TPD)
	}
	tokens := func(tokenLimit *float64, requestLimit *float64) *float64 {
		if tokenLimit != nil {
			if requestLimit == nil {
				return tokenLimit
			}
			return floatPtr(math.Min(*tokenLimit, *requestLimit*per))
		}
		if requestLimit == nil {
			return nil
		}
		return floatPtr(*requestLimit * per)
	}
	value := func(metric string) *float64 {
		if info, ok := known[metric]; ok {
			return floatPtr(info.Value)
		}
		return nil
	}
	rpm, tpm := value(RPM), value(TPM)
	rpd, tpd := value(RPD), value(TPD)
	var tokensPerMinute *float64
	if rpm != nil || tpm != nil {
		tokensPerMinute = tokens(tpm, rpm)
	}
	var tokensPerDay *float64
	if rpd != nil || tpd != nil {
		tokensPerDay = tokens(tpd, rpd)
	}
	// A day can never hold more than the minute limit allows all day long.
	if tokensPerDay != nil && tokensPerMinute != nil {
		tokensPerDay = floatPtr(math.Min(*tokensPerDay, *tokensPerMinute*1440))
	}
	perDay := rpd
	if perDay != nil && rpm != nil {
		perDay = floatPtr(math.Min(*perDay, *rpm*1440))
	}
	return Allowance{PerDay: perDay, PerMinute: rpm, TokensPerDay: tokensPerDay, TokensPerMinute: tokensPerMinute}, nil
}

// FreeCapacity is how many chat requests, and tokens, the free models could
// take if used around the clock, from their known usage limits. Models sharing
// an account allowance count once against it; models with no known limit are
// counted apart.
func (s *Service) FreeCapacity(userID string, now time.Time) (*Capacity, error) {
	if s.catalog == nil {
		return &Capacity{}, nil
	}
	entries, err := s.catalog.ListForUser(context.Background(), userID, "chat", false)
	if err != nil {
		return nil, err
	}
	perRequest, err := s.TokensPerRequest(userID, now)
	if err != nil {
		return nil, err
	}
	pools := map[string]bool{}
	counted := map[string]bool{}
	unknown := map[string]bool{}
	var perDay, perMinute, tokensPerDay, tokensPerMinute float64
	unlimitedModels := 0
	add := func(limits Allowance, accountID string) {
		if limits.TokensPerDay != nil {
			tokensPerDay += *limits.TokensPerDay
		} else {
			tokensPerMinute += deref(limits.TokensPerMinute)
		}
		if limits.PerDay != nil {
			perDay += *limits.PerDay
		} else if limits.PerMinute != nil {
			perMinute += *limits.PerMinute
		}
		counted[accountID] = true
	}
	for _, entry := range entries {
		if !entry.IsFree || !entry.Enabled || !entry.AccountEnabled || !entry.Available {
			continue
		}
		scopes := s.ScopesFor(entry)
		var poolScope *Scope
		var modelScope *Scope
		for i := range scopes {
			switch scopes[i].Kind {
			case KindPool:
				if poolScope == nil {
					poolScope = &scopes[i]
				}
			case KindModel:
				if modelScope == nil {
					modelScope = &scopes[i]
				}
			}
		}
		shared := Allowance{}
		if poolScope != nil {
			shared, err = s.LimitsOf(*poolScope, perRequest)
			if err != nil {
				return nil, err
			}
		}
		if shared.TokensPerDay != nil || shared.TokensPerMinute != nil {
			// One allowance for every model in the pool.
			if !pools[poolScope.Key] {
				pools[poolScope.Key] = true
				add(shared, entry.AccountID)
			}
			continue
		}
		own := Allowance{}
		if modelScope != nil {
			own, err = s.LimitsOf(*modelScope, perRequest)
			if err != nil {
				return nil, err
			}
		}
		if own.TokensPerDay == nil && own.TokensPerMinute == nil {
			unlimitedModels++
			unknown[entry.AccountID] = true
			continue
		}
		add(own, entry.AccountID)
	}
	for accountID := range counted {
		delete(unknown, accountID)
	}
	observed, err := s.ObservedFree(userID, now)
	if err != nil {
		return nil, err
	}
	return &Capacity{
		PerDay:                jsRound(perDay),
		PerHour:               jsRound(perDay / 24),
		PerMinuteOnly:         jsRound(perMinute),
		TokensPerDay:          jsRound(tokensPerDay),
		TokensPerHour:         jsRound(tokensPerDay / 24),
		TokensPerMinuteOnly:   jsRound(tokensPerMinute),
		TokensPerRequest:      perRequest,
		ProvidersCounted:      len(counted),
		ModelsWithoutLimit:    unlimitedModels,
		ProvidersWithoutLimit: len(unknown),
		Observed:              observed,
	}, nil
}
