package catalog

import (
	"database/sql"
)

// Stats is the routing statistics block of a model entry. A model without a
// row in model_stats reports zeros here.
type Stats struct {
	Successes        float64  `json:"successes"`
	Failures         float64  `json:"failures"`
	DecayedAt        int64    `json:"decayedAt"`
	LatencyMs        *float64 `json:"latencyMs"`
	TtftMs           *float64 `json:"ttftMs"`
	Tps              *float64 `json:"tps"`
	Samples          int64    `json:"samples"`
	Penalty          float64  `json:"penalty"`
	PenaltyAt        int64    `json:"penaltyAt"`
	CooldownUntil    int64    `json:"cooldownUntil"`
	CooldownReason   *string  `json:"cooldownReason"`
	BreakerOpenUntil int64    `json:"breakerOpenUntil"`
	LastError        *string  `json:"lastError"`
	LastSuccessAt    *int64   `json:"lastSuccessAt"`
	LastFailureAt    *int64   `json:"lastFailureAt"`
}

// Entry is one catalog row with everything routing needs: the model, its
// account, the canonical metadata and the learned statistics. Overrides win
// over provider data; provider data wins over canonical metadata.
type Entry struct {
	Ref          int64  `json:"ref"`
	ID           string `json:"id"`
	ModelID      string `json:"modelId"`
	Name         string `json:"name"`
	CanonicalID  string `json:"canonicalId"`
	Capability   string `json:"capability"`
	AccountID    string `json:"accountId"`
	UserID       string `json:"userId"`
	ProviderSlug string `json:"providerSlug"`
	DefinitionID string `json:"definitionId"`
	ProviderIdentity
	BillingMode     string   `json:"billingMode"`
	IsFlatRate      bool     `json:"isFlatRate"`
	SlotPlan        *string  `json:"slotPlan"`
	OverflowPolicy  string   `json:"overflowPolicy"`
	SlotWaitMs      *int64   `json:"slotWaitMs"`
	Enabled         bool     `json:"enabled"`
	Available       bool     `json:"available"`
	AccountEnabled  bool     `json:"accountEnabled"`
	AccountStatus   string   `json:"accountStatus"`
	ContextWindow   *int64   `json:"contextWindow"`
	MaxOutput       *int64   `json:"maxOutput"`
	Tools           *bool    `json:"tools"`
	Vision          *bool    `json:"vision"`
	JSON            *bool    `json:"json"`
	Reasoning       *bool    `json:"reasoning"`
	PriceIn         *float64 `json:"priceIn"`
	PriceOut        *float64 `json:"priceOut"`
	PriceCachedIn   *float64 `json:"priceCachedIn"`
	PriceCacheWrite *float64 `json:"priceCacheWrite"`
	PriceSource     *string  `json:"priceSource"`
	PriceUnit       *string  `json:"priceUnit"`
	UnitPrice       *float64 `json:"unitPrice"`
	IsFree          bool     `json:"isFree"`
	// Decision marks a System One decision model: it answers typed questions
	// rather than classifying text.
	Decision bool `json:"decision"`
	// IsPlan marks a model included in a prepaid plan: no extra cost per request.
	IsPlan bool `json:"isPlan"`
	// PlanOverflow marks a model billed per use once the plan is used up.
	PlanOverflow           bool               `json:"planOverflow"`
	FreeReason             *string            `json:"freeReason"`
	Intelligence           *float64           `json:"intelligence"`
	Skills                 map[string]float64 `json:"skills"`
	IntelligenceSource     *string            `json:"intelligenceSource"`
	IntelligenceConfidence *float64           `json:"intelligenceConfidence"`
	SpeedPriorTps          *float64           `json:"speedPriorTps"`
	ReleaseDate            *string            `json:"releaseDate"`
	ParamsB                *float64           `json:"paramsB"`
	Overrides              map[string]any     `json:"overrides"`
	Stats                  Stats              `json:"stats"`
}

// entryColumns is the explicit column list behind SELECT_ENTRY: the model
// columns, its account, the canonical metadata and the routing statistics.
const entryColumns = `m.id, m.account_id, m.model_id, m.display_name, m.canonical_id, m.capability,
	m.context_window, m.max_output, m.supports_tools, m.supports_vision, m.supports_json, m.supports_reasoning,
	m.price_in, m.price_out, m.price_source, m.is_free, m.free_reason, m.enabled, m.available,
	m.overrides_json, m.raw_json, m.price_unit, m.price_per_unit,
	a.slug, a.user_id, a.definition_id, a.label, a.base_url, a.billing_mode, a.enabled, a.status, a.prepaid,
	a.options_json, a.overflow_policy, a.slot_wait_ms,
	md.intelligence, md.intelligence_source, md.intelligence_confidence, md.speed_tps, md.context_window,
	md.supports_tools, md.supports_vision, md.supports_reasoning, md.release_date, md.params_b, md.skills_json,
	s.successes, s.failures, s.decayed_at, s.latency_ms, s.ttft_ms, s.tps, s.samples, s.penalty, s.penalty_at,
	s.cooldown_until, s.cooldown_reason, s.breaker_open_until, s.last_error, s.last_success_at, s.last_failure_at`

const selectEntry = `SELECT ` + entryColumns + `
	FROM models m
	JOIN provider_accounts a ON a.id = m.account_id
	LEFT JOIN model_metadata md ON md.canonical_id = m.canonical_id
	LEFT JOIN model_stats s ON s.model_ref = m.id`

// entryRow is one scanned SELECT_ENTRY row.
type entryRow struct {
	id             int64
	accountID      string
	modelID        string
	displayName    sql.NullString
	canonicalID    string
	capability     string
	contextWindow  sql.NullInt64
	maxOutput      sql.NullInt64
	supportsTools  sql.NullInt64
	supportsVision sql.NullInt64
	supportsJSON   sql.NullInt64
	supportsReason sql.NullInt64
	priceIn        sql.NullFloat64
	priceOut       sql.NullFloat64
	priceSource    sql.NullString
	isFree         int64
	freeReason     sql.NullString
	enabled        int64
	available      int64
	overridesJSON  string
	rawJSON        string
	priceUnit      sql.NullString
	pricePerUnit   sql.NullFloat64

	slug           string
	userID         string
	definitionID   string
	label          string
	baseURL        sql.NullString
	billingMode    string
	accountEnabled int64
	accountStatus  string
	prepaid        int64
	optionsJSON    string
	overflowPolicy string
	slotWaitMs     sql.NullInt64

	mdIntelligence sql.NullFloat64
	mdSource       sql.NullString
	mdConfidence   sql.NullFloat64
	mdSpeedTps     sql.NullFloat64
	mdContext      sql.NullInt64
	mdTools        sql.NullInt64
	mdVision       sql.NullInt64
	mdReasoning    sql.NullInt64
	mdReleaseDate  sql.NullString
	mdParamsB      sql.NullFloat64
	mdSkillsJSON   sql.NullString

	sSuccesses        sql.NullFloat64
	sFailures         sql.NullFloat64
	sDecayedAt        sql.NullInt64
	sLatencyMs        sql.NullFloat64
	sTtftMs           sql.NullFloat64
	sTps              sql.NullFloat64
	sSamples          sql.NullInt64
	sPenalty          sql.NullFloat64
	sPenaltyAt        sql.NullInt64
	sCooldownUntil    sql.NullInt64
	sCooldownReason   sql.NullString
	sBreakerOpenUntil sql.NullInt64
	sLastError        sql.NullString
	sLastSuccessAt    sql.NullInt64
	sLastFailureAt    sql.NullInt64
}

type scanner interface{ Scan(dest ...any) error }

func (r *entryRow) scan(row scanner) error {
	return row.Scan(
		&r.id, &r.accountID, &r.modelID, &r.displayName, &r.canonicalID, &r.capability,
		&r.contextWindow, &r.maxOutput, &r.supportsTools, &r.supportsVision, &r.supportsJSON, &r.supportsReason,
		&r.priceIn, &r.priceOut, &r.priceSource, &r.isFree, &r.freeReason, &r.enabled, &r.available,
		&r.overridesJSON, &r.rawJSON, &r.priceUnit, &r.pricePerUnit,
		&r.slug, &r.userID, &r.definitionID, &r.label, &r.baseURL, &r.billingMode, &r.accountEnabled, &r.accountStatus, &r.prepaid,
		&r.optionsJSON, &r.overflowPolicy, &r.slotWaitMs,
		&r.mdIntelligence, &r.mdSource, &r.mdConfidence, &r.mdSpeedTps, &r.mdContext,
		&r.mdTools, &r.mdVision, &r.mdReasoning, &r.mdReleaseDate, &r.mdParamsB, &r.mdSkillsJSON,
		&r.sSuccesses, &r.sFailures, &r.sDecayedAt, &r.sLatencyMs, &r.sTtftMs, &r.sTps, &r.sSamples, &r.sPenalty, &r.sPenaltyAt,
		&r.sCooldownUntil, &r.sCooldownReason, &r.sBreakerOpenUntil, &r.sLastError, &r.sLastSuccessAt, &r.sLastFailureAt,
	)
}

// toEntry projects a row into the entry shape the API and routing use.
// Port of catalog_service.js toEntry.
func (s *Service) toEntry(row *entryRow) Entry {
	overrides := parseJSONObject(row.overridesJSON)
	raw := parseJSONObject(row.rawJSON)
	baseURL := ""
	if row.baseURL.Valid {
		baseURL = row.baseURL.String
	}
	name := row.modelID
	if row.displayName.Valid && row.displayName.String != "" {
		name = row.displayName.String
	}
	entry := Entry{
		Ref:                    row.id,
		ID:                     row.slug + "/" + row.modelID,
		ModelID:                row.modelID,
		Name:                   name,
		CanonicalID:            row.canonicalID,
		Capability:             row.capability,
		AccountID:              row.accountID,
		UserID:                 row.userID,
		ProviderSlug:           row.slug,
		DefinitionID:           row.definitionID,
		ProviderIdentity:       s.providerIdentityOf(row.definitionID, row.slug, row.label, baseURL),
		BillingMode:            row.billingMode,
		IsFlatRate:             row.prepaid == PrepaidFlatRate,
		SlotPlan:               slotPlanOf(row.optionsJSON),
		OverflowPolicy:         row.overflowPolicy,
		SlotWaitMs:             int64PtrIfValid(row.slotWaitMs),
		Enabled:                row.enabled != 0,
		Available:              row.available != 0,
		AccountEnabled:         row.accountEnabled != 0,
		AccountStatus:          row.accountStatus,
		ContextWindow:          pickInt(overrides, "contextWindow", int64PtrIfValid(row.contextWindow), int64PtrIfValid(row.mdContext)),
		MaxOutput:              pickInt(overrides, "maxOutput", int64PtrIfValid(row.maxOutput), nil),
		Tools:                  pickBool(overrides, "tools", boolPtrIfValid(row.supportsTools), boolPtrIfValid(row.mdTools)),
		Vision:                 pickBool(overrides, "vision", boolPtrIfValid(row.supportsVision), boolPtrIfValid(row.mdVision)),
		JSON:                   boolPtrIfValid(row.supportsJSON),
		Reasoning:              pickBool(overrides, "", boolPtrIfValid(row.supportsReason), boolPtrIfValid(row.mdReasoning)),
		PriceIn:                pickFloat(overrides, "priceIn", floatPtrIfValid(row.priceIn), nil),
		PriceOut:               pickFloat(overrides, "priceOut", floatPtrIfValid(row.priceOut), nil),
		PriceCachedIn:          cachePrice(raw, overrides, "priceCachedIn"),
		PriceCacheWrite:        cachePrice(raw, overrides, "priceCacheWrite"),
		PriceUnit:              pickString(overrides, "priceUnit", stringPtrIfValid(row.priceUnit), nil),
		UnitPrice:              pickFloat(overrides, "unitPrice", floatPtrIfValid(row.pricePerUnit), nil),
		IsFree:                 row.prepaid != PrepaidFlatRate && row.isFree != 0,
		Decision:               row.capability == "moderation" && truthy(raw["decision"]),
		IsPlan:                 row.prepaid == PrepaidFlatRate || (row.isFree == 0 && row.prepaid != 0),
		PlanOverflow:           row.prepaid == PrepaidPlanPaid,
		FreeReason:             stringPtrIfValid(row.freeReason),
		Intelligence:           pickFloat(overrides, "intelligence", floatPtrIfValid(row.mdIntelligence), nil),
		Skills:                 parseSkills(row.mdSkillsJSON),
		IntelligenceSource:     stringPtrIfValid(row.mdSource),
		IntelligenceConfidence: floatPtrIfValid(row.mdConfidence),
		SpeedPriorTps:          floatPtrIfValid(row.mdSpeedTps),
		ReleaseDate:            stringPtrIfValid(row.mdReleaseDate),
		ParamsB:                floatPtrIfValid(row.mdParamsB),
		Overrides:              overrides,
		Stats: Stats{
			Successes:        floatOr0(row.sSuccesses),
			Failures:         floatOr0(row.sFailures),
			DecayedAt:        int64Or0(row.sDecayedAt),
			LatencyMs:        floatPtrIfValid(row.sLatencyMs),
			TtftMs:           floatPtrIfValid(row.sTtftMs),
			Tps:              floatPtrIfValid(row.sTps),
			Samples:          int64Or0(row.sSamples),
			Penalty:          floatOr0(row.sPenalty),
			PenaltyAt:        int64Or0(row.sPenaltyAt),
			CooldownUntil:    int64Or0(row.sCooldownUntil),
			CooldownReason:   stringPtrIfValid(row.sCooldownReason),
			BreakerOpenUntil: int64Or0(row.sBreakerOpenUntil),
			LastError:        stringPtrIfValid(row.sLastError),
			LastSuccessAt:    int64PtrIfValid(row.sLastSuccessAt),
			LastFailureAt:    int64PtrIfValid(row.sLastFailureAt),
		},
	}
	if entry.OverflowPolicy == "" {
		entry.OverflowPolicy = "auto"
	}
	// A price the user typed makes the price source, and the benchmark value
	// it replaces, manual.
	if _, ok := overrides["priceIn"]; ok {
		entry.PriceSource = stringPtrOf("manual")
	} else {
		entry.PriceSource = stringPtrIfValid(row.priceSource)
	}
	if _, ok := overrides["intelligence"]; ok {
		entry.IntelligenceSource = stringPtrOf("manual")
	}
	return entry
}

// pickFloat is the JS three-way pick: an override wins, then the model's own
// value, then the canonical fallback.
func pickFloat(overrides map[string]any, key string, own, fallback *float64) *float64 {
	if value, ok := overrides[key]; ok {
		return toFloatPtr(value)
	}
	if own != nil {
		return own
	}
	return fallback
}

func pickInt(overrides map[string]any, key string, own, fallback *int64) *int64 {
	if value, ok := overrides[key]; ok {
		return toIntPtr(value)
	}
	if own != nil {
		return own
	}
	return fallback
}

// pickBool is pickFloat for flags; an empty key means the field has no
// override and the model's own value wins.
func pickBool(overrides map[string]any, key string, own, fallback *bool) *bool {
	if key != "" {
		if value, ok := overrides[key]; ok {
			if value == nil {
				return nil
			}
			flag := truthy(value)
			return &flag
		}
	}
	if own != nil {
		return own
	}
	return fallback
}

func pickString(overrides map[string]any, key string, own, manual *string) *string {
	if value, ok := overrides[key]; ok {
		if text, isText := value.(string); isText {
			return &text
		}
		if value == nil {
			return nil
		}
		return manual
	}
	if own != nil {
		return own
	}
	return nil
}

// cachePrice is a price retained from a listing or provider facts, in USD per
// million tokens: the override, then the stored raw value, never negative.
func cachePrice(raw, overrides map[string]any, field string) *float64 {
	value, ok := overrides[field]
	if !ok || value == nil {
		value = raw[field]
	}
	number := toFloatPtr(value)
	if number == nil || *number < 0 {
		return nil
	}
	return number
}

// slotPlanOf reads provider_accounts.options_json.slotPlan, null when unset.
func slotPlanOf(optionsJSON string) *string {
	plan, ok := parseJSONObject(optionsJSON)["slotPlan"]
	if !ok {
		return nil
	}
	if text, isText := plan.(string); isText && text != "" {
		return &text
	}
	return nil
}
