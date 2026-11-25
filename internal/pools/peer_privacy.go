package pools

// Keeping requests that may go to a pool partner as private as possible
// (peer_privacy.js). A partner's server sees what it passes on to its provider;
// this cannot be prevented entirely, only reduced:
//
//   - the privacy shield runs with a minimum for these requests: secrets, cards
//     and wallet data are hidden for good; email addresses, phone numbers, IBANs
//     and ID numbers become placeholders that only this instance can put back
//     into the answer
//   - very sensitive requests stay on the owner's own connections when the owner
//     wants that (on by default)
//   - identifying request fields never leave, and the partner keeps no content
//     and no per-request log

import (
	"context"
	"encoding/json"

	"github.com/NeoLabs-Systems/neorouter/internal/routerkeys"
)

// PoolPrivacyKey is the settings key the pool privacy choice is stored under.
const PoolPrivacyKey = "pool_privacy"

// PoolPrivacyDefault is what the settings are before a person changes them.
type PoolPrivacyDefault struct {
	KeepSensitivePrivate bool `json:"keepSensitivePrivate"`
}

// DefaultPoolPrivacy is the privacy choice before anyone changes it.
var DefaultPoolPrivacy = PoolPrivacyDefault{KeepSensitivePrivate: true}

// AlwaysHidden are the shield categories a pool request always hides.
var AlwaysHidden = []string{"secret", "card", "crypto"}

// AtLeastPlaceholder are the categories that become placeholders at minimum.
var AtLeastPlaceholder = []string{"email", "phone", "iban", "id"}

// PoolPrivacy is one person's pool privacy choice.
type PoolPrivacy struct {
	KeepSensitivePrivate bool `json:"keepSensitivePrivate"`
}

// PoolPrivacySettings is one person's pool privacy choice, with the defaults
// filled in.
func (s *Service) PoolPrivacySettings(ctx context.Context, userID string) PoolPrivacy {
	settings := PoolPrivacy{KeepSensitivePrivate: DefaultPoolPrivacy.KeepSensitivePrivate}
	if s.GetSetting == nil {
		return settings
	}
	raw, found, err := s.GetSetting(ctx, userID, PoolPrivacyKey)
	if err != nil || !found || len(raw) == 0 {
		return settings
	}
	var stored map[string]any
	if err := json.Unmarshal(raw, &stored); err != nil {
		return settings
	}
	if value, ok := stored["keepSensitivePrivate"].(bool); ok {
		settings.KeepSensitivePrivate = value
	}
	return settings
}

// SavePoolPrivacy stores one person's pool privacy choice.
func (s *Service) SavePoolPrivacy(ctx context.Context, userID string, input PoolPrivacyInput) (PoolPrivacy, error) {
	settings := s.PoolPrivacySettings(ctx, userID)
	if input.KeepSensitivePrivate != nil {
		settings.KeepSensitivePrivate = *input.KeepSensitivePrivate
	}
	if s.SetSetting != nil {
		if err := s.SetSetting(ctx, userID, PoolPrivacyKey, settings); err != nil {
			return settings, err
		}
	}
	return settings, nil
}

// PoolPrivacyInput is a change of the pool privacy choice.
type PoolPrivacyInput struct {
	KeepSensitivePrivate *bool `json:"keepSensitivePrivate"`
}

// PrivacyConfig is the privacy block of the processing settings. It is kept
// structural here so the pools domain and the processing domain stay apart; the
// wiring converts.
type PrivacyConfig struct {
	Enabled     bool              `json:"enabled"`
	Modes       map[string]string `json:"modes"`
	AI          json.RawMessage   `json:"ai"`
	CustomTerms []any             `json:"customTerms"`
	AllowTerms  []any             `json:"allowTerms"`
}

// WithPoolMinimum is the privacy settings with the pool minimum applied on top.
// With the shield switched off, only the minimum runs.
func WithPoolMinimum(own PrivacyConfig) PrivacyConfig {
	modes := make(map[string]string, len(own.Modes))
	for category, mode := range own.Modes {
		if own.Enabled {
			modes[category] = mode
		} else {
			modes[category] = "off"
		}
	}
	for _, category := range AlwaysHidden {
		if _, ok := modes[category]; ok {
			modes[category] = "hide"
		}
	}
	for _, category := range AtLeastPlaceholder {
		if mode, ok := modes[category]; ok && mode == "off" {
			modes[category] = "restore"
		}
	}
	limited := PrivacyConfig{Enabled: true, Modes: modes}
	if own.Enabled {
		limited.AI = own.AI
		limited.CustomTerms = own.CustomTerms
		limited.AllowTerms = own.AllowTerms
	} else {
		limited.AI = json.RawMessage(`{"enabled":false}`)
		limited.CustomTerms = []any{}
		limited.AllowTerms = []any{}
	}
	return limited
}

// MayUsePool reports whether a request with this key may reach a pool partner at
// all.
func (s *Service) MayUsePool(ctx context.Context, key *routerkeys.Row) bool {
	if key == nil || key.PoolAccess != "pool" || key.PeerLinkID != nil {
		return false
	}
	pools, err := s.pools(ctx, key.UserID)
	if err != nil {
		return false
	}
	return len(pools) > 0
}
