package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

// SyncResult reports how many models one account's sync reconciled.
type SyncResult struct {
	Models int `json:"models"`
}

// overrideKeys are the per-model overrides a request may set. Evidence
// learned from traffic (billedAt, noFreeTier) can only be cleared, never set
// by hand.
var overrideKeys = []string{
	"free", "billedAt", "noFreeTier", "priceUnit", "unitPrice", "priceIn", "priceOut",
	"priceCachedIn", "priceCacheWrite", "contextWindow", "maxOutput", "tools", "vision", "intelligence",
}

// rawListing is the models.raw_json the sync writes: what the provider's own
// listing said about the model, kept for reclassification and re-pricing.
type rawListing struct {
	PriceCachedIn   *float64 `json:"priceCachedIn"`
	PriceCacheWrite *float64 `json:"priceCacheWrite"`
	FreeHint        bool     `json:"freeHint"`
	PriceKnown      bool     `json:"priceKnown"`
	Decision        bool     `json:"decision,omitempty"`
	PaidOnly        bool     `json:"paidOnly,omitempty"`
}

const upsertModelSQL = `INSERT INTO models
	(account_id, model_id, display_name, canonical_id, capability, context_window, max_output, supports_tools, supports_vision,
	 supports_json, supports_reasoning, price_in, price_out, price_source, is_free, free_reason, available, raw_json, price_unit, price_per_unit, enabled, last_seen_at)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?, ?, ?, strftime('%Y-%m-%dT%H:%M:%fZ','now'))
	ON CONFLICT(account_id, model_id) DO UPDATE SET display_name = excluded.display_name, canonical_id = excluded.canonical_id,
	  capability = excluded.capability, context_window = excluded.context_window, max_output = excluded.max_output,
	  supports_tools = excluded.supports_tools, supports_vision = excluded.supports_vision, supports_json = excluded.supports_json,
	  supports_reasoning = excluded.supports_reasoning, price_in = excluded.price_in, price_out = excluded.price_out,
	  price_source = excluded.price_source, raw_json = excluded.raw_json, price_unit = excluded.price_unit,
	  price_per_unit = excluded.price_per_unit, available = 1, last_seen_at = excluded.last_seen_at`

// SyncAccount pulls the live model list from the provider and reconciles the
// catalog. Models that disappear are kept (history, stats) but marked
// unavailable. A nil result means the account has no runtime to sync.
//
// Port of catalog_service.js syncAccount.
func (s *Service) SyncAccount(ctx context.Context, accountID string) (*SyncResult, error) {
	var definitionID string
	err := s.DB.R.QueryRowContext(ctx, `SELECT definition_id FROM provider_accounts WHERE id = ?`, accountID).Scan(&definitionID)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("catalog: sync account: %w", err)
	}
	if s.Models == nil {
		return nil, nil
	}
	listed, ok, err := s.Models.ListModels(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	definition, _ := s.definition(definitionID)
	seen := make(map[string]bool, len(listed))
	tx, err := s.DB.W.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("catalog: sync account: %w", err)
	}
	defer tx.Rollback()
	upsert, err := tx.PrepareContext(ctx, upsertModelSQL)
	if err != nil {
		return nil, fmt.Errorf("catalog: sync account: %w", err)
	}
	defer upsert.Close()
	for _, model := range listed {
		if model.ID == "" || seen[model.ID] {
			continue
		}
		seen[model.ID] = true
		facts := mergeFacts(s.Metadata.ProviderFacts(definitionID, model.ID), definition.FactsFor(model.ID))
		priceFromFacts := !model.PriceKnown && facts.PriceIn != nil && facts.PriceOut != nil &&
			*facts.PriceIn >= 0 && *facts.PriceOut >= 0
		displayName := firstNonEmpty(model.Name, facts.Name, model.ID)
		// A chat model whose facts say it emits no text is still a chat model
		// in the listing, so the kind is used as the provider reported it.
		capability := model.Kind
		var priceIn, priceOut *float64
		var priceSource *string
		switch {
		case model.PriceKnown:
			priceIn, priceOut = model.PriceIn, model.PriceOut
			priceSource = stringPtrOf("listing")
		case priceFromFacts:
			priceIn, priceOut = facts.PriceIn, facts.PriceOut
			priceSource = stringPtrOf("models.dev")
		}
		var supportsTools *bool
		if definition.Quirks.NoTools {
			no := false
			supportsTools = &no
		} else {
			supportsTools = firstBool(model.Tools, facts.Tools)
		}
		raw := rawListing{
			PriceCachedIn:   firstValue(model.PriceCachedIn, facts.PriceCachedIn),
			PriceCacheWrite: firstValue(model.PriceCacheWrite, facts.PriceCacheWrite),
			FreeHint:        model.FreeHint,
			PriceKnown:      model.PriceKnown,
			Decision:        model.Decision,
			PaidOnly:        model.PaidOnly,
		}
		rawJSON, err := json.Marshal(raw)
		if err != nil {
			return nil, fmt.Errorf("catalog: sync account: %w", err)
		}
		var priceUnit *string
		if model.PriceUnit != "" {
			priceUnit = stringPtrOf(model.PriceUnit)
		}
		enabled := int64(1)
		if model.OptIn {
			enabled = 0
		}
		if _, err := upsert.ExecContext(ctx,
			accountID, model.ID, displayName, canonicalID(model.ID), capability,
			firstInt64(model.ContextWindow, facts.ContextWindow), firstInt64(model.MaxOutput, facts.MaxOutput),
			flagValue(supportsTools), flagValue(firstBool(model.Vision, facts.Vision)), flagValue(model.JSON),
			flagValue(firstBool(model.Reasoning, facts.Reasoning)),
			floatValue(priceIn), floatValue(priceOut), stringValue(priceSource),
			int64(0), nil, string(rawJSON), stringValue(priceUnit), floatValue(model.UnitPrice), enabled,
		); err != nil {
			return nil, fmt.Errorf("catalog: sync account: %w", err)
		}
	}
	existing, err := tx.QueryContext(ctx, `SELECT id, model_id FROM models WHERE account_id = ?`, accountID)
	if err != nil {
		return nil, fmt.Errorf("catalog: sync account: %w", err)
	}
	gone := make([]int64, 0)
	for existing.Next() {
		var id int64
		var modelID string
		if err := existing.Scan(&id, &modelID); err != nil {
			existing.Close()
			return nil, fmt.Errorf("catalog: sync account: %w", err)
		}
		if !seen[modelID] {
			gone = append(gone, id)
		}
	}
	existing.Close()
	if err := existing.Err(); err != nil {
		return nil, fmt.Errorf("catalog: sync account: %w", err)
	}
	for _, id := range gone {
		if _, err := tx.ExecContext(ctx, `UPDATE models SET available = 0 WHERE id = ?`, id); err != nil {
			return nil, fmt.Errorf("catalog: sync account: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("catalog: sync account: %w", err)
	}
	if err := s.ReclassifyAccount(ctx, accountID); err != nil {
		return nil, err
	}
	canonicals := make([]string, 0, len(seen))
	marked := make(map[string]bool, len(seen))
	for modelID := range seen {
		canonical := canonicalID(modelID)
		if marked[canonical] {
			continue
		}
		marked[canonical] = true
		canonicals = append(canonicals, canonical)
	}
	for _, canonical := range canonicals {
		if err := s.Metadata.RecomputeCanonical(ctx, canonical); err != nil {
			return nil, err
		}
	}
	if _, err := s.DB.W.ExecContext(ctx,
		`UPDATE provider_accounts SET last_synced_at = strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id = ?`, accountID); err != nil {
		return nil, fmt.Errorf("catalog: sync account: %w", err)
	}
	s.Log.Info("Synced provider models", "accountId", accountID, "models", len(seen))
	return &SyncResult{Models: len(seen)}, nil
}

// ReclassifyAccount recomputes the free status of every model of one account.
// Free status depends on the account's billing mode and the overrides, so it
// is recomputed whenever either changes, not only on sync.
//
// Port of catalog_service.js reclassifyAccount.
func (s *Service) ReclassifyAccount(ctx context.Context, accountID string) error {
	var definitionID, billingMode string
	var billingState, classification sql.NullString
	var baseURL, secretEncrypted sql.NullString
	var prepaid int64
	err := s.DB.R.QueryRowContext(ctx, `SELECT definition_id, billing_mode, billing_state, base_url, secret_encrypted,
		classification, prepaid FROM provider_accounts WHERE id = ?`, accountID).Scan(
		&definitionID, &billingMode, &billingState, &baseURL, &secretEncrypted, &classification, &prepaid)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return fmt.Errorf("catalog: reclassify account: %w", err)
	}
	definition, _ := s.definition(definitionID)
	rows, err := s.DB.R.QueryContext(ctx, `SELECT id, model_id, raw_json, overrides_json, price_source
		FROM models WHERE account_id = ?`, accountID)
	if err != nil {
		return fmt.Errorf("catalog: reclassify account: %w", err)
	}
	type modelRow struct {
		id          int64
		modelID     string
		rawJSON     string
		overridesJS string
		priceSource sql.NullString
	}
	var models []modelRow
	for rows.Next() {
		var row modelRow
		if err := rows.Scan(&row.id, &row.modelID, &row.rawJSON, &row.overridesJS, &row.priceSource); err != nil {
			rows.Close()
			return fmt.Errorf("catalog: reclassify account: %w", err)
		}
		models = append(models, row)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("catalog: reclassify account: %w", err)
	}
	tx, err := s.DB.W.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("catalog: reclassify account: %w", err)
	}
	defer tx.Rollback()
	update, err := tx.PrepareContext(ctx, `UPDATE models SET is_free = ?, free_reason = ? WHERE id = ?`)
	if err != nil {
		return fmt.Errorf("catalog: reclassify account: %w", err)
	}
	defer update.Close()
	for _, row := range models {
		var result Classification
		if prepaid == PrepaidFlatRate {
			result = Classification{IsFree: false, Reason: ReasonFlatRatePlan}
		} else {
			listing := parseJSONObject(row.rawJSON)
			overrides := overridesFromJSON(parseJSONObject(row.overridesJS))
			// The price counts as known once the provider reported one, the
			// catalog stored one, or the user typed one.
			priceKnown := truthy(listing["priceKnown"]) || row.priceSource.Valid || overrides.PriceInSet
			result = Classify(ClassificationInput{
				Definition:   definition,
				BillingMode:  billingMode,
				BillingState: billingState.String,
				ModelID:      row.modelID,
				Listing: Listing{
					PaidOnly:   truthy(listing["paidOnly"]),
					FreeHint:   truthy(listing["freeHint"]),
					PriceKnown: priceKnown,
				},
				Overrides:       overrides,
				AccountOverride: classification.String,
				BaseURL:         baseURL.String,
				HasSecret:       secretEncrypted.Valid && secretEncrypted.String != "",
			})
		}
		free := int64(0)
		if result.IsFree {
			free = 1
		}
		if _, err := update.ExecContext(ctx, free, string(result.Reason), row.id); err != nil {
			return fmt.Errorf("catalog: reclassify account: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("catalog: reclassify account: %w", err)
	}
	return nil
}

// overridesFromJSON converts stored overrides into the classifier's view.
func overridesFromJSON(raw map[string]any) Overrides {
	overrides := Overrides{}
	if value, ok := raw["free"]; ok {
		if value != nil {
			flag := truthy(value)
			overrides.Free = &flag
		}
	}
	if value, ok := raw["billedAt"]; ok {
		overrides.BilledAt = value
	}
	if value, ok := raw["noFreeTier"]; ok {
		overrides.NoFreeTier = truthy(value)
	}
	if value, ok := raw["priceIn"]; ok {
		overrides.PriceInSet = true
		overrides.PriceIn = value
	}
	return overrides
}

// FlagModel records evidence from real traffic on a model (e.g. {billedAt} or
// {noFreeTier}) and reclassifies its account. It returns false when the flag
// was already set, so callers log each finding once.
//
// Port of catalog_service.js flagModel.
func (s *Service) FlagModel(ctx context.Context, ref int64, flags map[string]any) (bool, error) {
	var accountID, overridesJSON string
	err := s.DB.R.QueryRowContext(ctx, `SELECT account_id, overrides_json FROM models WHERE id = ?`, ref).Scan(&accountID, &overridesJSON)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("catalog: flag model: %w", err)
	}
	overrides := parseJSONObject(overridesJSON)
	already := true
	for key := range flags {
		if !truthy(overrides[key]) {
			already = false
			break
		}
	}
	if already {
		return false, nil
	}
	for key, value := range flags {
		overrides[key] = value
	}
	encoded, err := json.Marshal(overrides)
	if err != nil {
		return false, fmt.Errorf("catalog: flag model: %w", err)
	}
	if _, err := s.DB.W.ExecContext(ctx, `UPDATE models SET overrides_json = ? WHERE id = ?`, string(encoded), ref); err != nil {
		return false, fmt.Errorf("catalog: flag model: %w", err)
	}
	if err := s.ReclassifyAccount(ctx, accountID); err != nil {
		return false, err
	}
	return true, nil
}

// firstValue returns the first non-nil pointer.
func firstValue(values ...*float64) *float64 {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

// firstBool returns the first non-nil pointer, JavaScript's `a ?? b`.
func firstBool(values ...*bool) *bool {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

// firstNonEmpty returns the first non-empty string.
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// firstInt64 mirrors JavaScript's `a || b`: a zero counts as missing.
func firstInt64(values ...*int64) *int64 {
	for _, value := range values {
		if value != nil && *value != 0 {
			return value
		}
	}
	return nil
}

// flagValue stores a tri-state flag as the models table columns do: 1, 0 or
// NULL.
func flagValue(flag *bool) any {
	if flag == nil {
		return nil
	}
	if *flag {
		return int64(1)
	}
	return int64(0)
}

func floatValue(value *float64) any {
	if value == nil {
		return nil
	}
	return *value
}

func stringValue(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}
