package app

// The catalog row as each consumer domain sees it. A catalog entry is one model
// on one provider account; routing, quota, usage, pools and media each declare
// their own narrow shape of it, so the conversions live here.

import (
	"github.com/NeoLabs-Systems/neorouter/internal/catalog"
	"github.com/NeoLabs-Systems/neorouter/internal/pools"
	"github.com/NeoLabs-Systems/neorouter/internal/quota"
	"github.com/NeoLabs-Systems/neorouter/internal/routing"
	"github.com/NeoLabs-Systems/neorouter/internal/usage"
)

// routingEntryOf is one catalog row as the router plans over it.
func routingEntryOf(entry catalog.Entry) *routing.Entry {
	out := &routing.Entry{
		Ref:                    entry.Ref,
		ID:                     entry.ID,
		ModelID:                entry.ModelID,
		Name:                   entry.Name,
		CanonicalID:            entry.CanonicalID,
		Capability:             entry.Capability,
		AccountID:              entry.AccountID,
		UserID:                 entry.UserID,
		ProviderSlug:           entry.ProviderSlug,
		ProviderKey:            entry.ProviderKey,
		ProviderName:           entry.ProviderName,
		DefinitionID:           entry.DefinitionID,
		BillingMode:            entry.BillingMode,
		IsFlatRate:             entry.IsFlatRate,
		OverflowPolicy:         entry.OverflowPolicy,
		Enabled:                entry.Enabled,
		Available:              entry.Available,
		AccountEnabled:         entry.AccountEnabled,
		AccountStatus:          entry.AccountStatus,
		Tools:                  entry.Tools,
		Vision:                 entry.Vision,
		JSON:                   entry.JSON,
		Reasoning:              entry.Reasoning,
		PriceIn:                entry.PriceIn,
		PriceOut:               entry.PriceOut,
		PriceCachedIn:          entry.PriceCachedIn,
		PriceCacheWrite:        entry.PriceCacheWrite,
		UnitPrice:              entry.UnitPrice,
		IsFree:                 entry.IsFree,
		Decision:               entry.Decision,
		IsPlan:                 entry.IsPlan,
		PlanOverflow:           entry.PlanOverflow,
		Intelligence:           entry.Intelligence,
		Skills:                 entry.Skills,
		IntelligenceConfidence: entry.IntelligenceConfidence,
		SpeedPriorTps:          entry.SpeedPriorTps,
		ParamsB:                entry.ParamsB,
		Stats:                  routing.EntryStats{TTFTMs: entry.Stats.TtftMs},
	}
	if entry.SlotPlan != nil {
		out.SlotPlan = *entry.SlotPlan
	}
	if entry.ContextWindow != nil {
		out.ContextWindow = int(*entry.ContextWindow)
	}
	if entry.MaxOutput != nil {
		out.MaxOutput = int(*entry.MaxOutput)
	}
	if entry.PriceSource != nil {
		out.PriceSource = *entry.PriceSource
	}
	if entry.PriceUnit != nil {
		out.PriceUnit = *entry.PriceUnit
	}
	if entry.FreeReason != nil {
		out.FreeReason = *entry.FreeReason
	}
	if entry.IntelligenceSource != nil {
		out.IntelligenceSource = *entry.IntelligenceSource
	}
	if entry.ReleaseDate != nil {
		out.ReleaseDate = *entry.ReleaseDate
	}
	return out
}

// routingToCatalogEntry is a routing entry back in the catalog's own shape, for
// the consumers that take a catalog row (quota, pools). Fields routing does not
// carry stay zero.
func routingToCatalogEntry(entry *routing.Entry) catalog.Entry {
	if entry == nil {
		return catalog.Entry{}
	}
	out := catalog.Entry{
		Ref:                    entry.Ref,
		ID:                     entry.ID,
		ModelID:                entry.ModelID,
		Name:                   entry.Name,
		CanonicalID:            entry.CanonicalID,
		Capability:             entry.Capability,
		AccountID:              entry.AccountID,
		UserID:                 entry.UserID,
		ProviderSlug:           entry.ProviderSlug,
		ProviderIdentity:       catalog.ProviderIdentity{ProviderKey: entry.ProviderKey, ProviderName: entry.ProviderName},
		DefinitionID:           entry.DefinitionID,
		BillingMode:            entry.BillingMode,
		IsFlatRate:             entry.IsFlatRate,
		OverflowPolicy:         entry.OverflowPolicy,
		Enabled:                entry.Enabled,
		Available:              entry.Available,
		AccountEnabled:         entry.AccountEnabled,
		AccountStatus:          entry.AccountStatus,
		Tools:                  entry.Tools,
		Vision:                 entry.Vision,
		JSON:                   entry.JSON,
		Reasoning:              entry.Reasoning,
		PriceIn:                entry.PriceIn,
		PriceOut:               entry.PriceOut,
		PriceCachedIn:          entry.PriceCachedIn,
		PriceCacheWrite:        entry.PriceCacheWrite,
		UnitPrice:              entry.UnitPrice,
		IsFree:                 entry.IsFree,
		Decision:               entry.Decision,
		IsPlan:                 entry.IsPlan,
		PlanOverflow:           entry.PlanOverflow,
		Intelligence:           entry.Intelligence,
		Skills:                 entry.Skills,
		IntelligenceConfidence: entry.IntelligenceConfidence,
		SpeedPriorTps:          entry.SpeedPriorTps,
		ParamsB:                entry.ParamsB,
	}
	if entry.SlotPlan != "" {
		plan := entry.SlotPlan
		out.SlotPlan = &plan
	}
	if entry.ContextWindow != 0 {
		window := int64(entry.ContextWindow)
		out.ContextWindow = &window
	}
	if entry.MaxOutput != 0 {
		limit := int64(entry.MaxOutput)
		out.MaxOutput = &limit
	}
	if entry.PriceSource != "" {
		source := entry.PriceSource
		out.PriceSource = &source
	}
	if entry.PriceUnit != "" {
		unit := entry.PriceUnit
		out.PriceUnit = &unit
	}
	if entry.FreeReason != "" {
		reason := entry.FreeReason
		out.FreeReason = &reason
	}
	if entry.IntelligenceSource != "" {
		source := entry.IntelligenceSource
		out.IntelligenceSource = &source
	}
	if entry.ReleaseDate != "" {
		date := entry.ReleaseDate
		out.ReleaseDate = &date
	}
	return out
}

// quotaEntryOf is one catalog row as the quota store counts it.
func quotaEntryOf(entry catalog.Entry) quota.Entry {
	out := quota.Entry{
		Ref:            entry.Ref,
		ID:             entry.ID,
		Name:           entry.Name,
		AccountID:      entry.AccountID,
		DefinitionID:   entry.DefinitionID,
		ModelID:        entry.ModelID,
		IsFree:         entry.IsFree,
		IsPlan:         entry.IsPlan,
		IsFlatRate:     entry.IsFlatRate,
		Enabled:        entry.Enabled,
		Available:      entry.Available,
		AccountEnabled: entry.AccountEnabled,
	}
	return out
}

// usageEntryOf is one catalog row as the usage log's learning views read it.
func usageEntryOf(entry catalog.Entry) usage.CatalogEntry {
	out := usage.CatalogEntry{
		Ref:            entry.Ref,
		ID:             entry.ID,
		Name:           entry.Name,
		ModelID:        entry.ModelID,
		CanonicalID:    entry.CanonicalID,
		AccountID:      entry.AccountID,
		DefinitionID:   entry.DefinitionID,
		Enabled:        entry.Enabled,
		Available:      entry.Available,
		AccountEnabled: entry.AccountEnabled,
		AccountStatus:  entry.AccountStatus,
		IsFree:         entry.IsFree,
		IsPlan:         entry.IsPlan,
		Intelligence:   entry.Intelligence,
		Skills:         entry.Skills,
		BillingMode:    entry.BillingMode,
	}
	if entry.IntelligenceSource != nil {
		out.IntelligenceSource = *entry.IntelligenceSource
	}
	if entry.FreeReason != nil {
		out.FreeReason = *entry.FreeReason
	}
	return out
}

// poolsModelOf is one catalog row as the pool share policy judges it.
func poolsModelOf(entry catalog.Entry) pools.Model {
	return pools.Model{
		Ref:          entry.Ref,
		AccountID:    entry.AccountID,
		UserID:       entry.UserID,
		DefinitionID: entry.DefinitionID,
		IsFree:       entry.IsFree,
		IsPlan:       entry.IsPlan,
	}
}
