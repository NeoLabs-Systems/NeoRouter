package app

// The catalog's settings seam, and the usage log's view of routing and of the
// catalog (the activity, health and learning pages are built from them).

import (
	"context"
	"encoding/json"
	"time"

	"github.com/NeoLabs-Systems/neorouter/internal/catalog"
	"github.com/NeoLabs-Systems/neorouter/internal/providers"
	"github.com/NeoLabs-Systems/neorouter/internal/routing"
	"github.com/NeoLabs-Systems/neorouter/internal/routing/task"
	"github.com/NeoLabs-Systems/neorouter/internal/settings"
	"github.com/NeoLabs-Systems/neorouter/internal/usage"
)

// catalogSettings is the settings store as the catalog reads it: preset rows,
// the Artificial Analysis key and the seal/open pair for it.
type catalogSettings struct {
	svc *settings.Service
}

func (s catalogSettings) Global(key string) (json.RawMessage, bool, error) {
	return s.svc.GetGlobal(context.Background(), key)
}

func (s catalogSettings) SetGlobal(key string, value any) error {
	return s.svc.SetGlobal(context.Background(), key, value)
}

func (s catalogSettings) UserSetting(userID, key string) (json.RawMessage, bool, error) {
	return s.svc.GetUserSetting(context.Background(), userID, key)
}

func (s catalogSettings) SetUserSetting(userID, key string, value any) error {
	return s.svc.SetUserSetting(context.Background(), userID, key, value)
}

func (s catalogSettings) Seal(plain string) (string, error) { return s.svc.Sealer.Seal(plain) }

func (s catalogSettings) Open(sealed string) (string, error) { return s.svc.Sealer.Open(sealed) }

// usageRouting is the router as the usage log's health and learning views read
// it.
type usageRouting struct {
	svc *routing.Service
}

func (r usageRouting) TaskSkills(kind string) map[string]float64 {
	return task.SkillsOfKind(kind)
}

func (r usageRouting) Availability(ref int64, now time.Time) string {
	return r.svc.ModelAvailability(ref, now).Reason
}

func (r usageRouting) Snapshot(ref int64, now time.Time) usage.Snapshot {
	row := r.svc.ModelStats(ref, now)
	return usage.Snapshot{
		Samples:   row.Samples,
		Successes: row.Successes,
		Failures:  row.Failures,
		LatencyMS: row.LatencyMs,
		TPS:       row.TPS,
	}
}

func (r usageRouting) Faded(row usage.TaskStats, now time.Time) usage.Counts {
	faded := task.Faded(task.TaskRow{
		ModelRef:  row.ModelRef,
		Task:      row.Task,
		Successes: row.Successes,
		Failures:  row.Failures,
		Liked:     row.Liked,
		Disliked:  row.Disliked,
		UpdatedAt: row.UpdatedAt,
	}, now)
	return usage.Counts{
		Successes: faded.Successes,
		Failures:  faded.Failures,
		Liked:     faded.Liked,
		Disliked:  faded.Disliked,
	}
}

func (r usageRouting) Standing(counts usage.Counts) usage.Standing {
	standing := task.StandingOf(task.TaskRow{
		Successes: counts.Successes,
		Failures:  counts.Failures,
		Liked:     counts.Liked,
		Disliked:  counts.Disliked,
	})
	return usage.Standing{Rate: standing.Rate, Share: standing.Share}
}

func (r usageRouting) NeedsCredits(entry usage.CatalogEntry) bool {
	return routing.NeedsCredits(routingEntryOf(catalogEntryOf(entry)))
}

func (r usageRouting) WorthParts(entry usage.CatalogEntry, kind string, now time.Time) usage.Parts {
	parts := r.svc.WorthParts(routingEntryOf(catalogEntryOf(entry)), kind, task.SkillsOfKind(kind), now)
	return usage.Parts{Intelligence: parts.Intelligence, Speed: parts.Speed, Reliability: parts.Reliability}
}

func (r usageRouting) Worth(parts usage.Parts) float64 {
	return routing.CombineWorth(routing.WorthFeatures{
		Intelligence: parts.Intelligence,
		Speed:        parts.Speed,
		Reliability:  parts.Reliability,
	}, balancedWeights())
}

// balancedWeights is the "balanced" preset's weights, the style the learning
// views score with (profiles.js).
func balancedWeights() routing.Weights {
	for _, preset := range routing.Presets {
		if preset.ID == "balanced" {
			return preset.Weights
		}
	}
	return routing.Weights{Intelligence: 0.35, Speed: 0.2, Reliability: 0.3, Cost: 0.15}
}

// catalogEntryOf is a usage catalog row back in the catalog's own shape.
func catalogEntryOf(entry usage.CatalogEntry) catalog.Entry {
	out := catalog.Entry{
		Ref:            entry.Ref,
		ID:             entry.ID,
		ModelID:        entry.ModelID,
		Name:           entry.Name,
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
	if entry.IntelligenceSource != "" {
		source := entry.IntelligenceSource
		out.IntelligenceSource = &source
	}
	if entry.FreeReason != "" {
		reason := entry.FreeReason
		out.FreeReason = &reason
	}
	return out
}

// usageProviderName names the provider behind a model label
// (catalog/provider_identity.js): the definition's public label, else the
// account's slug, else the model's own label.
func usageProviderName(registry *providers.Registry) func(definitionID, slug, label, baseURL string) string {
	return func(definitionID, slug, label, baseURL string) string {
		if definitionID != "" {
			if def, ok := registry.Get(definitionID); ok && def.Label != "" {
				return def.Label
			}
		}
		if slug != "" {
			return slug
		}
		return label
	}
}
