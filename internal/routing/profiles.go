package routing

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"golang.org/x/text/unicode/norm"

	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
)

// Weights are relative; scoring normalizes them. Reliability counts in every
// preset, and among models of similar quality the router takes the cheaper one
// anyway, so three presets cover it (profiles.js).

// PresetProfile is one built-in routing style.
type PresetProfile struct {
	ID      string
	Label   string
	Weights Weights
}

// Presets are the built-in styles, in the order GET /routing/profiles lists
// them.
var Presets = []PresetProfile{
	{"balanced", "Balanced", Weights{Intelligence: 0.35, Speed: 0.2, Reliability: 0.3, Cost: 0.15}},
	{"fast", "Fastest", Weights{Intelligence: 0.15, Speed: 0.5, Reliability: 0.3, Cost: 0.05}},
	{"smart", "Smartest", Weights{Intelligence: 0.6, Speed: 0.05, Reliability: 0.3, Cost: 0.05}},
}

// ProfileAliases are earlier preset names, still accepted in keys, model names
// (auto:best) and apps.
var ProfileAliases = map[string]string{"reliable": "balanced", "cheap": "balanced", "best": "smart"}

// DefaultConstraints are a style's constraints before a user's own settings.
func DefaultConstraints() Constraints {
	return Constraints{
		IncludeProviders: []string{},
		ExcludeProviders: []string{},
		ExcludeModels:    []string{},
		Adaptive:         true,
		Exploration:      0.08,
	}
}

// reservedSlugs are names reserved for presets, aliases and plain auto.
var reservedSlugs = map[string]bool{
	"balanced": true, "fast": true, "smart": true,
	"reliable": true, "cheap": true, "best": true,
	"default": true, "custom": true,
}

func presetByID(id string) *PresetProfile {
	for i := range Presets {
		if Presets[i].ID == id {
			return &Presets[i]
		}
	}
	return nil
}

func presetProfile(preset *PresetProfile) *Profile {
	return &Profile{
		ID:          preset.ID,
		Slug:        preset.ID,
		Name:        preset.Label,
		Preset:      true,
		Weights:     preset.Weights,
		Constraints: DefaultConstraints(),
	}
}

// Slugify turns a style name into the readable id used in model ids
// ("auto:paid-first").
func Slugify(name string) string {
	decomposed := norm.NFKD.String(strings.ToLower(name))
	var stripped strings.Builder
	for _, r := range decomposed {
		// Drop combining marks (the Node code's [\u0300-\u036f]).
		if r >= 0x0300 && r <= 0x036f {
			continue
		}
		stripped.WriteRune(r)
	}
	var out strings.Builder
	dash := false
	for _, r := range stripped.String() {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			out.WriteRune(r)
			dash = false
		default:
			if !dash && out.Len() > 0 {
				out.WriteByte('-')
				dash = true
			}
		}
	}
	slug := strings.Trim(out.String(), "-")
	if len(slug) > 40 {
		slug = strings.TrimRight(slug[:40], "-")
	}
	if slug == "" {
		return "style"
	}
	return slug
}

// customProfiles are the user's own styles, each with the readable name used
// in model ids ("auto:paid-first"); clashes with presets or each other get a
// number.
func (s *Service) customProfiles(userID string) []*Profile {
	taken := map[string]bool{}
	for slug := range reservedSlugs {
		taken[slug] = true
	}
	if s.opts.DB == nil {
		return nil
	}
	rows, err := s.opts.DB.R.Query(`SELECT id, user_id, name, weights_json, constraints_json, created_at, updated_at, created_by_user_id
		FROM routing_profiles WHERE user_id = ? ORDER BY created_at`, userID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []*Profile
	for rows.Next() {
		var (
			id, name, weightsJSON, constraintsJSON, createdAt, updatedAt string
			rowUserID, createdBy                                         *string
		)
		if err := rows.Scan(&id, &rowUserID, &name, &weightsJSON, &constraintsJSON, &createdAt, &updatedAt, &createdBy); err != nil {
			continue
		}
		base := Slugify(name)
		slug := base
		if taken[base] {
			slug = base + "-2"
		}
		for index := 3; taken[slug]; index++ {
			slug = fmt.Sprintf("%s-%d", base, index)
		}
		taken[slug] = true
		profile := &Profile{ID: id, Slug: slug, Name: name, Preset: false, Constraints: DefaultConstraints()}
		_ = json.Unmarshal([]byte(weightsJSON), &profile.Weights)
		_ = json.Unmarshal([]byte(constraintsJSON), &profile.Constraints)
		profile.CreatedAt, profile.UpdatedAt = createdAt, updatedAt
		if createdBy != nil {
			if s.opts.PublicUser != nil {
				profile.CreatedBy = s.opts.PublicUser(*createdBy)
			} else {
				profile.CreatedBy = map[string]any{"id": *createdBy}
			}
		}
		out = append(out, profile)
	}
	return out
}

// Profiles lists the presets and the user's own styles.
func (s *Service) Profiles(userID string) []*Profile {
	out := make([]*Profile, 0, len(Presets)+2)
	for i := range Presets {
		out = append(out, presetProfile(&Presets[i]))
	}
	return append(out, s.customProfiles(userID)...)
}

// ResolveProfile resolves "balanced", "fast", … or one of the user's own styles
// by its readable name ("paid-first") or its id.
func (s *Service) ResolveProfile(userID, reference string) *Profile {
	key := reference
	if alias, ok := ProfileAliases[reference]; ok {
		key = alias
	}
	if key == "" {
		key = "balanced"
	}
	if preset := presetByID(key); preset != nil {
		return presetProfile(preset)
	}
	for _, profile := range s.customProfiles(userID) {
		if profile.ID == key || profile.Slug == key {
			return profile
		}
	}
	return nil
}

// ProfileInput is a style to create or update (routes/routing.js).
type ProfileInput struct {
	Name        *string
	Weights     *Weights
	Constraints *Constraints
}

// CreateProfile stores a new style.
func (s *Service) CreateProfile(userID string, input ProfileInput, createdBy string) (*Profile, error) {
	if input.Name == nil || strings.TrimSpace(*input.Name) == "" {
		return nil, httpserver.New(400, "VALIDATION_ERROR", "name: Required")
	}
	if len([]rune(*input.Name)) > 80 {
		return nil, httpserver.New(400, "VALIDATION_ERROR", "name: String must contain at most 80 character(s)")
	}
	if input.Weights == nil {
		return nil, httpserver.New(400, "VALIDATION_ERROR", "weights: Required")
	}
	weights, err := validateWeights(*input.Weights)
	if err != nil {
		return nil, err
	}
	constraints := DefaultConstraints()
	if input.Constraints != nil {
		constraints = *input.Constraints
		if err := validateConstraints(constraints); err != nil {
			return nil, err
		}
	}
	id := "custom_" + randomHex(6)
	if s.opts.DB != nil {
		if _, err := s.opts.DB.W.Exec(`INSERT INTO routing_profiles (id, user_id, name, weights_json, constraints_json, created_by_user_id)
			VALUES (?, ?, ?, ?, ?, ?)`, id, userID, *input.Name, mustJSON(weights), mustJSON(constraints), createdBy); err != nil {
			return nil, fmt.Errorf("routing: create profile: %w", err)
		}
	}
	return s.ResolveProfile(userID, id), nil
}

// UpdateProfile changes a style. A preset cannot be changed.
func (s *Service) UpdateProfile(userID, id string, input ProfileInput) (*Profile, error) {
	existing := s.ResolveProfile(userID, id)
	if existing == nil || existing.Preset {
		return nil, httpserver.New(404, "PROFILE_NOT_FOUND", "This routing profile does not exist or is a built-in preset.")
	}
	name := existing.Name
	if input.Name != nil {
		name = *input.Name
	}
	weights := existing.Weights
	if input.Weights != nil {
		validated, err := validateWeights(*input.Weights)
		if err != nil {
			return nil, err
		}
		weights = validated
	}
	constraints := existing.Constraints
	if input.Constraints != nil {
		if err := validateConstraints(*input.Constraints); err != nil {
			return nil, err
		}
		constraints = *input.Constraints
	}
	if s.opts.DB != nil {
		if _, err := s.opts.DB.W.Exec(`UPDATE routing_profiles SET name = ?, weights_json = ?, constraints_json = ?,
			updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id = ? AND user_id = ?`,
			name, mustJSON(weights), mustJSON(constraints), id, userID); err != nil {
			return nil, fmt.Errorf("routing: update profile: %w", err)
		}
	}
	return s.ResolveProfile(userID, id), nil
}

// RemoveProfile deletes a style and points everything that used it back at
// balanced.
func (s *Service) RemoveProfile(userID, id string) error {
	if s.opts.DB == nil {
		return nil
	}
	result, err := s.opts.DB.W.Exec(`DELETE FROM routing_profiles WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return fmt.Errorf("routing: remove profile: %w", err)
	}
	changed, _ := result.RowsAffected()
	if changed == 0 {
		return httpserver.New(404, "PROFILE_NOT_FOUND", "This routing profile does not exist.")
	}
	if _, err := s.opts.DB.W.Exec("UPDATE router_keys SET profile = 'balanced' WHERE user_id = ? AND profile = ?", userID, id); err != nil {
		return fmt.Errorf("routing: reset key profiles: %w", err)
	}
	if _, err := s.opts.DB.W.Exec("UPDATE routing_groups SET config_json = json_set(config_json, '$.profile', NULL) WHERE user_id = ? AND json_extract(config_json, '$.profile') = ?", userID, id); err != nil {
		return fmt.Errorf("routing: reset group profiles: %w", err)
	}
	return nil
}

// validateWeights is weightsSchema: four numbers in 0..1, at least one above
// zero.
func validateWeights(weights Weights) (Weights, error) {
	for _, field := range []struct {
		name  string
		value float64
	}{
		{"intelligence", weights.Intelligence},
		{"speed", weights.Speed},
		{"reliability", weights.Reliability},
		{"cost", weights.Cost},
	} {
		if field.value < 0 {
			return weights, httpserver.New(400, "VALIDATION_ERROR", "weights."+field.name+": Number must be greater than or equal to 0")
		}
		if field.value > 1 {
			return weights, httpserver.New(400, "VALIDATION_ERROR", "weights."+field.name+": Number must be less than or equal to 1")
		}
	}
	if weights.total() <= 0 {
		return weights, httpserver.New(400, "VALIDATION_ERROR", "weights: At least one weight must be above zero.")
	}
	return weights, nil
}

// validateConstraints is constraintsSchema.
func validateConstraints(c Constraints) error {
	if c.MinIntelligence != nil && (*c.MinIntelligence < 0 || *c.MinIntelligence > 1) {
		return httpserver.New(400, "VALIDATION_ERROR", "constraints.minIntelligence: Number must be between 0 and 1")
	}
	if c.MaxPriceOut != nil && *c.MaxPriceOut < 0 {
		return httpserver.New(400, "VALIDATION_ERROR", "constraints.maxPriceOut: Number must be greater than or equal to 0")
	}
	if c.MaxTTFTMs != nil && *c.MaxTTFTMs <= 0 {
		return httpserver.New(400, "VALIDATION_ERROR", "constraints.maxTtftMs: Number must be greater than 0")
	}
	if c.Exploration < 0 || c.Exploration > 1 {
		return httpserver.New(400, "VALIDATION_ERROR", "constraints.exploration: Number must be between 0 and 1")
	}
	if c.Kinds != nil {
		for _, weight := range []float64{c.Kinds.Free, c.Kinds.Plan, c.Kinds.Paid} {
			if weight < 0 || weight > 1 {
				return httpserver.New(400, "VALIDATION_ERROR", "constraints.kinds: Number must be between 0 and 1")
			}
		}
		if c.Kinds.Free+c.Kinds.Plan+c.Kinds.Paid <= 0 {
			return httpserver.New(400, "VALIDATION_ERROR", "constraints.kinds: At least one kind of model must count.")
		}
	}
	return nil
}

func randomHex(bytes int) string {
	buf := make([]byte, bytes)
	if _, err := rand.Read(buf); err != nil {
		for i := range buf {
			buf[i] = byte(i * 7)
		}
	}
	return hex.EncodeToString(buf)
}

func mustJSON(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(encoded)
}

// MarshalJSON writes a profile the way the Node code's objects serialize: a
// preset is {id, slug, name, preset, weights, constraints}, a user's style is
// {id, name, preset, weights, constraints, createdAt, updatedAt, createdBy,
// slug}.
func (p *Profile) MarshalJSON() ([]byte, error) {
	var b strings.Builder
	b.WriteString(`{"id":`)
	id, err := json.Marshal(p.ID)
	if err != nil {
		return nil, err
	}
	b.Write(id)
	if p.Preset {
		slug, _ := json.Marshal(p.Slug)
		b.WriteString(`,"slug":`)
		b.Write(slug)
	}
	name, _ := json.Marshal(p.Name)
	b.WriteString(`,"name":`)
	b.Write(name)
	b.WriteString(`,"preset":`)
	preset, _ := json.Marshal(p.Preset)
	b.Write(preset)
	b.WriteString(`,"weights":`)
	weights, err := json.Marshal(p.Weights)
	if err != nil {
		return nil, err
	}
	b.Write(weights)
	b.WriteString(`,"constraints":`)
	constraints, err := json.Marshal(p.Constraints)
	if err != nil {
		return nil, err
	}
	b.Write(constraints)
	if !p.Preset {
		if p.CreatedAt != "" {
			b.WriteString(`,"createdAt":`)
			createdAt, _ := json.Marshal(p.CreatedAt)
			b.Write(createdAt)
		}
		if p.UpdatedAt != "" {
			b.WriteString(`,"updatedAt":`)
			updatedAt, _ := json.Marshal(p.UpdatedAt)
			b.Write(updatedAt)
		}
		if p.CreatedBy != nil {
			b.WriteString(`,"createdBy":`)
			createdBy, err := json.Marshal(p.CreatedBy)
			if err != nil {
				return nil, err
			}
			b.Write(createdBy)
		}
		slug, _ := json.Marshal(p.Slug)
		b.WriteString(`,"slug":`)
		b.Write(slug)
	}
	b.WriteString("}")
	return []byte(b.String()), nil
}
