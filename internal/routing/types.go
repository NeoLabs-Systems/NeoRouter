package routing

import "time"

// capabilityChat is the only capability with a prompt to read; media and
// embedding models route without task classification.
const capabilityChat = "chat"

// Model kinds, the three switches on a key (router.modelKind).
const (
	kindFree = "free"
	kindPlan = "plan"
	kindPaid = "paid"
)

// ModelKinds are the kinds an auto variant can be limited to (MODEL_KINDS).
var ModelKinds = []string{kindFree, kindPlan, kindPaid}

// Entry is one catalog model as routing sees it. It mirrors the object
// catalog_service.toEntry builds; the pointer fields keep the Node code's
// null-vs-unknown distinction (entry.tools === false, entry.priceOut === null).
//
// Routing annotates the entry of a chosen candidate (RoutingReason,
// RoutingGroup, LastResort, TryingLuck, Task); the gateway reads those back
// when it appends the attempt trail.
type Entry struct {
	Ref         int64
	ID          string // "<provider slug>/<model id>"
	ModelID     string
	Name        string
	CanonicalID string
	Capability  string
	AccountID   string
	UserID      string

	ProviderSlug string
	ProviderKey  string
	ProviderName string
	DefinitionID string

	BillingMode    string
	IsFlatRate     bool
	SlotPlan       string
	OverflowPolicy string // auto | overflow | queue
	SlotWaitMs     *int

	Enabled        bool
	Available      bool
	AccountEnabled bool
	AccountStatus  string

	ContextWindow int // 0 = unknown
	MaxOutput     int // 0 = unknown
	Tools         *bool
	Vision        *bool
	JSON          *bool
	Reasoning     *bool

	PriceIn         *float64
	PriceOut        *float64
	PriceCachedIn   *float64
	PriceCacheWrite *float64
	PriceSource     string
	PriceUnit       string // image | second | minute | mchar | search | call
	UnitPrice       *float64

	IsFree       bool
	Decision     bool
	IsPlan       bool
	PlanOverflow bool
	FreeReason   string

	Intelligence           *float64
	Skills                 map[string]float64
	IntelligenceSource     string
	IntelligenceConfidence *float64
	SpeedPriorTps          *float64
	ReleaseDate            string
	ParamsB                *float64

	// Stats carries only what a rejection reads (the catalog's join of
	// model_stats); scoring reads the live store by ref.
	Stats EntryStats

	// Task is the kind of task the request was classified as (set while
	// planning, so outcomes are learned per kind).
	Task string

	RoutingReason string
	RoutingGroup  string
	LastResort    bool
	TryingLuck    bool
}

// EntryStats is the part of model_stats the catalog scrolls into an entry.
type EntryStats struct {
	TTFTMs *float64
}

// ModelKind is router.modelKind: a model is free, part of a plan, or paid.
func ModelKind(entry *Entry) string {
	if entry == nil {
		return kindPaid
	}
	if entry.IsFree {
		return kindFree
	}
	if entry.IsPlan {
		return kindPlan
	}
	return kindPaid
}

// Needs is what a request needs from a model (capabilities/chat/canonical
// requirements()). Routing uses it as hard filters.
type Needs struct {
	Tools  bool
	Vision bool
	JSON   bool
	Stream bool
}

// RouterKey is the router key a request came from, as routing reads it
// (router_keys.router_key_service). Field names follow the JS key object.
type RouterKey struct {
	ID     string
	UserID string
	Name   string
	Mode   string // free_only | paid | both

	BudgetUSD    *float64
	BudgetPeriod string // total | monthly | daily
	RPMLimit     *int

	// AllowedModelsJSON is the raw allowed_models_json list of patterns.
	AllowedModelsJSON string
	Profile           string

	TokenLimit  *float64
	TokenPeriod string

	Priority           string // low | normal | high
	MaxPricePerMillion *float64
	PoolAccess         string // own | pool
	PeerLinkID         string
	RoutingGroup       string
	ConcurrentLimit    *int

	// AllowFree and AllowPlan are the 1/0 database flags; nil means allowed,
	// which is what an undefined field means in the Node code.
	AllowFree *bool
	AllowPlan *bool
}

// allowFree is key.allow_free !== 0.
func (k RouterKey) allowFree() bool { return k.AllowFree == nil || *k.AllowFree }

// allowPlan is key.allow_plan !== 0.
func (k RouterKey) allowPlan() bool { return k.AllowPlan == nil || *k.AllowPlan }

// Features are the four axes scoring weighs (0..1 each).
type Features struct {
	Intelligence float64 `json:"intelligence"`
	Speed        float64 `json:"speed"`
	Reliability  float64 `json:"reliability"`
	Cost         float64 `json:"cost"`
}

// factor returns one feature by name ("intelligence", "speed", …).
func (f Features) factor(name string) float64 {
	switch name {
	case "intelligence":
		return f.Intelligence
	case "speed":
		return f.Speed
	case "reliability":
		return f.Reliability
	case "cost":
		return f.Cost
	}
	return 0
}

func (f *Features) setFactor(name string, value float64) {
	switch name {
	case "intelligence":
		f.Intelligence = value
	case "speed":
		f.Speed = value
	case "reliability":
		f.Reliability = value
	case "cost":
		f.Cost = value
	}
}

// Weights are a profile's relative weights (scoring normalizes them).
type Weights struct {
	Intelligence float64 `json:"intelligence"`
	Speed        float64 `json:"speed"`
	Reliability  float64 `json:"reliability"`
	Cost         float64 `json:"cost"`
}

func (w Weights) total() float64 {
	return w.Intelligence + w.Speed + w.Reliability + w.Cost
}

func (w Weights) get(name string) float64 {
	switch name {
	case "intelligence":
		return w.Intelligence
	case "speed":
		return w.Speed
	case "reliability":
		return w.Reliability
	case "cost":
		return w.Cost
	}
	return 0
}

// Factors are the multipliers on a score. The last three are only set where
// the Node code adds them, so the JSON of a preview keeps the same keys.
type Factors struct {
	Quota         float64  `json:"quota"`
	Penalty       float64  `json:"penalty"`
	Load          float64  `json:"load"`
	Sibling       *float64 `json:"sibling,omitempty"`
	Pacing        *float64 `json:"pacing,omitempty"`
	CacheAffinity *float64 `json:"cacheAffinity,omitempty"`
}

// ScoreDetails is scoring.score's result.
type ScoreDetails struct {
	Score      float64
	Base       float64
	Capability float64
	Features   Features
	Factors    Factors
}

// Candidate is one entry of a ranked plan: the entry, its score details, its
// quota state, and (when rejected) why.
type Candidate struct {
	Entry        *Entry
	Details      *ScoreDetails
	Quota        QuotaEvaluation
	Rejected     string
	Score        float64
	Quality      float64
	OwnCost      float64
	Preference   int
	StyleTier    int
	Tier         int
	PricierEqual bool
}

// Constraints are a routing profile's constraints (profiles.js
// DEFAULT_CONSTRAINTS). Pointers mean "not set", as null does in the Node code.
type Constraints struct {
	MinIntelligence  *float64     `json:"minIntelligence"`
	MaxPriceOut      *float64     `json:"maxPriceOut"`
	MaxTTFTMs        *int         `json:"maxTtftMs"`
	IncludeProviders []string     `json:"includeProviders"`
	ExcludeProviders []string     `json:"excludeProviders"`
	ExcludeModels    []string     `json:"excludeModels"`
	Adaptive         bool         `json:"adaptive"`
	Exploration      float64      `json:"exploration"`
	Kinds            *KindWeights `json:"kinds"`
}

// KindWeights is how much each kind of model counts in a custom style.
type KindWeights struct {
	Free float64 `json:"free"`
	Plan float64 `json:"plan"`
	Paid float64 `json:"paid"`
}

func (k *KindWeights) get(kind string) float64 {
	if k == nil {
		return 0
	}
	switch kind {
	case kindFree:
		return k.Free
	case kindPlan:
		return k.Plan
	}
	return k.Paid
}

// Profile is a routing style: the presets from profiles.js or a user's own
// row in routing_profiles.
type Profile struct {
	ID          string      `json:"id"`
	Slug        string      `json:"slug"`
	Name        string      `json:"name"`
	Preset      bool        `json:"preset"`
	Weights     Weights     `json:"weights"`
	Constraints Constraints `json:"constraints"`
	CreatedAt   string      `json:"createdAt,omitempty"`
	UpdatedAt   string      `json:"updatedAt,omitempty"`
	CreatedBy   any         `json:"createdBy,omitempty"`
}

// Time helpers: the Node code counts everywhere in milliseconds.

func fromMs(ms int64) time.Time { return time.UnixMilli(ms) }
