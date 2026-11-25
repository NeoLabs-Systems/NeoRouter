package catalog

import (
	"math"
	"net/url"
	"regexp"
	"strings"
)

// Reason is why a model is free or paid. The values are part of the API
// contract (models.freeReason) and are stored in the models table.
type Reason string

// Free/paid reasons, exactly as free_classifier.js defines them.
const (
	ReasonManualFree        Reason = "manual_free"
	ReasonManualPaid        Reason = "manual_paid"
	ReasonBilledObserved    Reason = "billed_observed"
	ReasonNoFreeTier        Reason = "no_free_tier"
	ReasonProviderFreeModel Reason = "provider_free_model"
	ReasonListedZeroPrice   Reason = "listed_zero_price"
	ReasonFreeTierAccount   Reason = "free_tier_account"
	ReasonBillingActive     Reason = "billing_active"
	ReasonBillingUnverified Reason = "billing_unverified"
	ReasonNotInFreeTier     Reason = "not_in_free_tier"
	ReasonPaid              Reason = "paid"
	ReasonPriceUnknown      Reason = "price_unknown"
	ReasonFlatRatePlan      Reason = "flat_rate_plan"
)

// Prepaid plan markers stored in provider_accounts.prepaid
// (server/services/providers/billing_modes.js).
const (
	PrepaidNone     = 0 // billing_mode as stored: 'free_tier' or 'paid'
	PrepaidPlan     = 1 // "plan": a subscription whose allowance is spent first
	PrepaidPlanPaid = 2 // "plan_paid": plan, then billed per use
	PrepaidFlatRate = 3 // "flat_rate": unlimited use, never billed per request
)

// Quirks is the slice of a provider definition's quirks the catalog reads.
type Quirks struct {
	// NoTools forces supports_tools to 0 for the provider's models.
	NoTools bool
}

// Billing is the slice of a provider definition's billing block the catalog
// reads (server/providers/registry.js billing schema, defaults applied).
type Billing struct {
	// FreeTierAll covers every model the account can call.
	FreeTierAll bool
	// FreeTierModels are case-insensitive regular expressions naming the
	// models the free tier includes.
	FreeTierModels []string
	// AlwaysFree are regular expressions naming models that are free on this
	// provider whatever the billing mode.
	AlwaysFree []string
	// Verify is true when the definition carries a billing.verify rule, i.e.
	// the provider can prove automatically that the account is not billed. A
	// free-tier account that cannot be verified is not treated as free.
	Verify bool
	// LocalOnly limits the free tier to a loopback or private-network host.
	LocalOnly bool
	// KeylessOnly limits the free tier to connections without a key.
	KeylessOnly bool
	// TrustListingFree is billing.trustListingFree, which the definition
	// schema defaults to true. Nil means "not set", i.e. trusted.
	TrustListingFree *bool
}

// trustsListingFree reports billing.trustListingFree with the registry's
// default (true) applied.
func (b Billing) trustsListingFree() bool {
	return b.TrustListingFree == nil || *b.TrustListingFree
}

// Definition is the slice of a provider definition the catalog reads.
// internal/app adapts providers.Definition to it.
type Definition struct {
	ID       string
	Label    string
	Category string // "pool", "local", "free", "aggregator", …
	BaseURL  string
	Quirks   Quirks
	Billing  Billing
}

// Definitions is the provider registry as the catalog consumes it
// (providers/registry.js get()). ok is false for an unknown definition id.
type Definitions interface {
	Definition(id string) (Definition, bool)
}

// DefinitionFunc adapts a function to Definitions.
type DefinitionFunc func(id string) (Definition, bool)

// Definition satisfies Definitions.
func (f DefinitionFunc) Definition(id string) (Definition, bool) { return f(id) }

// Listing is what a provider's own model listing says about one model.
type Listing struct {
	// PaidOnly marks a model the listing says needs paid credit, on any plan.
	PaidOnly bool
	// FreeHint marks a model whose every reported price is exactly zero.
	FreeHint bool
	// PriceKnown marks a listing that reported a price at all.
	PriceKnown bool
}

// Overrides are the user's per-model overrides plus the evidence learned from
// real traffic (models.overrides_json).
type Overrides struct {
	// Free is the user's manual call: true free, false paid, nil not set.
	Free *bool
	// BilledAt is set when traffic proved the model was charged for; any
	// truthy value counts.
	BilledAt any
	// NoFreeTier is set when the provider refused the model on the free tier.
	NoFreeTier bool
	// PriceInSet reports that the user set a price; the price is then known.
	PriceInSet bool
	// PriceIn is the user's price override, in USD per million input tokens.
	PriceIn any
}

// ClassificationInput is everything one free/paid classification reads.
type ClassificationInput struct {
	Definition      Definition
	BillingMode     string // provider_accounts.billing_mode: 'free_tier' or 'paid'
	BillingState    string // provider_accounts.billing_state; "" means "unknown"
	ModelID         string
	Listing         Listing
	Overrides       Overrides
	AccountOverride string // provider_accounts.classification: "", "free" or "paid"
	BaseURL         string // the account's base URL; "" falls back to the definition's
	HasSecret       bool
}

// Classification is the verdict for one model.
type Classification struct {
	IsFree bool
	Reason Reason
}

// Classify implements the strict "100% free" rule: a model is free only when
// one of these holds, and an unknown price is never free.
//
//  1. the user explicitly marked it free (manual override)
//  2. the provider definition lists it as always free (e.g. ":free" models)
//  3. the provider's own listing reports every price field as exactly 0
//  4. the account is on the provider's free tier, the model is covered by that
//     free tier, and, where the provider can bill a card, an automatic check
//     verified the account is not billed
//
// Evidence from real traffic overrides 2–4: a model that reported a cost or a
// free-tier limit of 0 is never free again until the user says otherwise.
//
// Port of server/services/catalog/free_classifier.js classify.
func Classify(in ClassificationInput) Classification {
	billing := in.Definition.Billing
	billingState := in.BillingState
	if billingState == "" {
		billingState = "unknown"
	}
	free := func(reason Reason) Classification { return Classification{IsFree: true, Reason: reason} }
	paid := func(reason Reason) Classification { return Classification{IsFree: false, Reason: reason} }

	// A pool partner's model never costs us anything (the partner pays), so no
	// override can make it paid.
	if in.Definition.Category == "pool" {
		return free(ReasonProviderFreeModel)
	}
	if in.Overrides.Free != nil && *in.Overrides.Free {
		return free(ReasonManualFree)
	}
	if in.Overrides.Free != nil && !*in.Overrides.Free {
		return paid(ReasonManualPaid)
	}
	if in.AccountOverride == "paid" {
		return paid(ReasonManualPaid)
	}
	if truthy(in.Overrides.BilledAt) {
		return paid(ReasonBilledObserved)
	}
	// A model the listing marks as needing paid credit is never free, on any plan.
	if in.Listing.PaidOnly {
		return paid(ReasonPaid)
	}
	if in.AccountOverride == "free" {
		return free(ReasonManualFree)
	}
	if matchesAny(billing.AlwaysFree, in.ModelID) {
		return free(ReasonProviderFreeModel)
	}
	if in.Listing.FreeHint && billing.trustsListingFree() {
		return free(ReasonListedZeroPrice)
	}
	if in.BillingMode == "free_tier" && HasFreeTier(billing) {
		if in.Overrides.NoFreeTier {
			return paid(ReasonNoFreeTier)
		}
		baseURL := in.BaseURL
		if baseURL == "" {
			baseURL = in.Definition.BaseURL
		}
		if billing.LocalOnly && !IsPrivateURL(baseURL) {
			return paid(ReasonNotInFreeTier)
		}
		if billing.KeylessOnly && in.HasSecret {
			return paid(ReasonNotInFreeTier)
		}
		if !coveredByFreeTier(billing, in.ModelID) {
			return paid(ReasonNotInFreeTier)
		}
		if billingState == "paid" {
			return paid(ReasonBillingActive)
		}
		if billing.Verify && billingState != "free" {
			return paid(ReasonBillingUnverified)
		}
		return free(ReasonFreeTierAccount)
	}
	if in.Listing.PriceKnown {
		return paid(ReasonPaid)
	}
	return paid(ReasonPriceUnknown)
}

// HasFreeTier reports whether the definition declares free models: a free
// tier covering everything, or a list of covered models.
func HasFreeTier(billing Billing) bool {
	return billing.FreeTierAll || len(billing.FreeTierModels) > 0
}

// DeclaresFreeModels reports whether the provider is known to have free
// models: a free tier, or models that are always free (":free"). An account
// can also turn out to have free models from its listing (zero prices).
func DeclaresFreeModels(billing Billing) bool {
	return HasFreeTier(billing) || len(billing.AlwaysFree) > 0
}

func coveredByFreeTier(billing Billing, modelID string) bool {
	return billing.FreeTierAll || matchesAny(billing.FreeTierModels, modelID)
}

// matchesAny reports whether any case-insensitive pattern matches modelID.
func matchesAny(patterns []string, modelID string) bool {
	for _, pattern := range patterns {
		re, err := regexp.Compile("(?i)" + pattern)
		if err != nil {
			continue
		}
		if re.MatchString(modelID) {
			return true
		}
	}
	return false
}

var (
	private172       = regexp.MustCompile(`^172\.(1[6-9]|2\d|3[01])\.`)
	privateIPv6Local = regexp.MustCompile(`^f[cd][0-9a-f]{2}:`)
)

// IsPrivateURL reports whether a URL points at a loopback or private-network
// host (RFC 1918, link-local, .local).
func IsPrivateURL(raw string) bool {
	if raw == "" {
		return false
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" {
		return false
	}
	host := strings.Trim(parsed.Hostname(), "[]")
	return host == "localhost" || strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".localhost") ||
		host == "::1" || strings.HasPrefix(host, "127.") || strings.HasPrefix(host, "10.") ||
		strings.HasPrefix(host, "192.168.") || private172.MatchString(host) ||
		strings.HasPrefix(host, "169.254.") || privateIPv6Local.MatchString(host)
}

// truthy mirrors JavaScript truthiness: 0, "" and NaN are false.
func truthy(value any) bool {
	switch v := value.(type) {
	case nil:
		return false
	case bool:
		return v
	case string:
		return v != ""
	case float64:
		return v != 0 && !math.IsNaN(v)
	case float32:
		return v != 0
	case int:
		return v != 0
	case int64:
		return v != 0
	case int32:
		return v != 0
	case uint64:
		return v != 0
	default:
		return true
	}
}
