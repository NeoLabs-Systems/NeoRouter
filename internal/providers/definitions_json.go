package providers

import (
	"bytes"
	"encoding/json"
)

// The two definition fields registry.js validates as unions: `validate`, which
// is false or an object, and `billing.verify`, which is one of two shapes.
// JSON has no unions, so each form is kept apart by the discriminator.

// MarshalJSON writes the union form registry.js validates: `false` or the
// object. An absent spec is nil and is never marshaled.
func (v ValidateSpec) MarshalJSON() ([]byte, error) {
	if v.Disabled {
		return []byte("false"), nil
	}
	type object struct {
		Path        string   `json:"path,omitempty"`
		ChatProbe   bool     `json:"chatProbe,omitempty"`
		ProbeModels []string `json:"probeModels,omitempty"`
	}
	return json.Marshal(object{Path: v.Path, ChatProbe: v.ChatProbe, ProbeModels: v.ProbeModels})
}

// UnmarshalJSON reads the union form: `false` or the object.
func (v *ValidateSpec) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if string(trimmed) == "false" {
		*v = ValidateSpec{Disabled: true}
		return nil
	}
	type object struct {
		Path        string   `json:"path"`
		ChatProbe   bool     `json:"chatProbe"`
		ProbeModels []string `json:"probeModels"`
	}
	var parsed object
	if err := json.Unmarshal(data, &parsed); err != nil {
		return err
	}
	*v = ValidateSpec{Path: parsed.Path, ChatProbe: parsed.ChatProbe, ProbeModels: parsed.ProbeModels}
	return nil
}

// MarshalJSON writes the discriminated union registry.js validates.
func (v Verify) MarshalJSON() ([]byte, error) {
	if v.Type == VerifyPaidModelProbe {
		return json.Marshal(struct {
			Type           string   `json:"type"`
			PaidOnlyModels []string `json:"paidOnlyModels"`
		}{v.Type, v.PaidOnlyModels})
	}
	return json.Marshal(struct {
		Type                  string   `json:"type"`
		MaxFreeRequestsPerDay float64  `json:"maxFreeRequestsPerDay"`
		ProbeModels           []string `json:"probeModels"`
	}{v.Type, v.MaxFreeRequestsPerDay, v.ProbeModels})
}

// UnmarshalJSON reads the discriminated union registry.js validates.
func (v *Verify) UnmarshalJSON(data []byte) error {
	var probe struct {
		Type                  string   `json:"type"`
		MaxFreeRequestsPerDay float64  `json:"maxFreeRequestsPerDay"`
		ProbeModels           []string `json:"probeModels"`
		PaidOnlyModels        []string `json:"paidOnlyModels"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return err
	}
	*v = Verify{
		Type:                  probe.Type,
		MaxFreeRequestsPerDay: probe.MaxFreeRequestsPerDay,
		ProbeModels:           probe.ProbeModels,
		PaidOnlyModels:        probe.PaidOnlyModels,
	}
	return nil
}
