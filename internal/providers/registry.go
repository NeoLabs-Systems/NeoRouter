package providers

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// Account is one connection an adapter talks through: an account row with its
// secret decrypted, or an unsaved definition being probed. It carries no
// behavior; the adapters read it.
type Account struct {
	ID           string
	UserID       string
	DefinitionID string
	// BaseURL overrides the definition's base URL when the account has one; it
	// may still contain {field} placeholders for the account's options.
	BaseURL string
	// Secret is the API key, or the OAuth access token for signed-in providers.
	// Empty is allowed for keyless and local providers.
	Secret string
	// Options are the per-account settings (definition Fields, authHeader,
	// headers, manualModels, modelsPath).
	Options map[string]any
	// PublicOnly is true when the account's owner is not the instance admin:
	// its calls may only reach public hosts.
	PublicOnly bool
}

// Option reads one account option.
func (a Account) Option(name string) any { return a.Options[name] }

// OptionString reads one account option as a string ("" when absent).
func (a Account) OptionString(name string) string {
	switch value := a.Options[name].(type) {
	case string:
		return value
	case fmt.Stringer:
		return value.String()
	case nil:
		return ""
	default:
		return fmt.Sprint(value)
	}
}

// OptionStrings reads one account option as a list of strings, accepting both
// the JSON array and a single string.
func (a Account) OptionStrings(name string) []string {
	switch value := a.Options[name].(type) {
	case []string:
		return value
	case []any:
		out := make([]string, 0, len(value))
		for _, item := range value {
			if text, ok := item.(string); ok {
				out = append(out, text)
			}
		}
		return out
	case string:
		if value == "" {
			return nil
		}
		return []string{value}
	}
	return nil
}

// OptionHeaders reads the account's extra request headers.
func (a Account) OptionHeaders() map[string]string {
	switch value := a.Options["headers"].(type) {
	case map[string]string:
		return value
	case map[string]any:
		out := make(map[string]string, len(value))
		for name, item := range value {
			if text, ok := item.(string); ok {
				out[name] = text
			}
		}
		return out
	}
	return nil
}

// AdapterDeps is everything an adapter is built with. The registry hands it to
// the AdapterFactory; an adapter keeps what it needs.
type AdapterDeps struct {
	// HTTP is the provider HTTP layer every upstream call goes through.
	HTTP *Client
	// Account is the connection the adapter speaks for.
	Account Account
	// UpstreamTimeout is the configured default for one upstream call, used
	// when neither the definition nor the call names a timeout.
	UpstreamTimeout time.Duration
	// Log records what an operator needs; adapters never log prompts or secrets.
	Log *slog.Logger
}

// TimeoutFor is the timeout one upstream call gets: the call's own, else the
// definition's quirks.timeoutMs, else the configured default.
func (d AdapterDeps) TimeoutFor(def Definition, call Call) time.Duration {
	switch {
	case call.Timeout > 0:
		return call.Timeout
	case def.Quirks.TimeoutMS > 0:
		return time.Duration(def.Quirks.TimeoutMS) * time.Millisecond
	default:
		return d.UpstreamTimeout
	}
}

// AdapterFactory builds the adapter for one definition and account. The
// adapters subpackage supplies it (NewAdapter); the registry never imports the
// adapters, so the two never form a cycle.
type AdapterFactory func(def Definition, deps AdapterDeps) (Adapter, error)

// ErrNoFactory is returned when a registry was built without an adapter
// factory, which only a definition-data test does.
var ErrNoFactory = errors.New("providers: no adapter factory configured")

// Registry is the provider definitions the server offers: loaded once from the
// embedded data, read-only, in definition-file order.
type Registry struct {
	factory AdapterFactory
	defs    []Definition
	index   map[string]int
}

// Load parses the embedded definition data. getenv applies the base URLs the
// definition modules read from the environment (NEOROUTER_NOUS_BASE_URL,
// NEOROUTER_CODEX_BASE_URL); nil leaves every definition at its documented
// default.
func Load(factory AdapterFactory, getenv func(string) string) (*Registry, error) {
	return Parse(embeddedDefinitions, factory, getenv)
}

// Parse builds a registry from definition JSON, which is the shape of
// definitions.json. It is Load for tests and for data that was not embedded.
func Parse(data []byte, factory AdapterFactory, getenv func(string) string) (*Registry, error) {
	defs, err := parseDefinitions(data)
	if err != nil {
		return nil, err
	}
	if getenv != nil {
		applyBaseURLOverrides(defs, getenv)
	}
	registry := &Registry{factory: factory, defs: defs, index: make(map[string]int, len(defs))}
	for i, def := range defs {
		registry.index[def.ID] = i
	}
	return registry, nil
}

// Len is how many definitions the registry holds.
func (r *Registry) Len() int { return len(r.defs) }

// All returns every definition in load order. The slice is a copy: definitions
// are immutable, and so is the registry.
func (r *Registry) All() []Definition {
	out := make([]Definition, len(r.defs))
	copy(out, r.defs)
	return out
}

// Get returns one definition by id.
func (r *Registry) Get(id string) (Definition, bool) {
	if i, ok := r.index[id]; ok {
		return r.defs[i], true
	}
	return Definition{}, false
}

// CreateAdapter builds the adapter for a definition and account. It is
// registry.createAdapter: the factory decides how the adapter speaks, this
// layer only refuses a definition that names an adapter nobody implements.
func (r *Registry) CreateAdapter(def Definition, deps AdapterDeps) (Adapter, error) {
	if !def.Adapter.valid() {
		return nil, fmt.Errorf("providers: unknown adapter %q for %s", def.Adapter, def.ID)
	}
	if r.factory == nil {
		return nil, ErrNoFactory
	}
	if def.ID == "" {
		return nil, errors.New("providers: adapter requested for a definition without an id")
	}
	return r.factory(def, deps)
}

// baseURLEnv are the definitions whose base URL the environment replaces.
// OpenRouter's is the same seam for its fixed API host: the PKCE sign-in
// validates and lists the new connection's models there, so a test (or a
// self-hosted mirror) needs to point it somewhere else.
var baseURLEnv = map[string]string{
	"nous":       "NEOROUTER_NOUS_BASE_URL",
	"chatgpt":    "NEOROUTER_CODEX_BASE_URL",
	"openrouter": "NEOROUTER_OPENROUTER_BASE_URL",
}

// applyBaseURLOverrides replaces the base URL of the definitions whose
// environment override is set.
func applyBaseURLOverrides(defs []Definition, getenv func(string) string) {
	for i := range defs {
		name, ok := baseURLEnv[defs[i].ID]
		if !ok {
			continue
		}
		if value := strings.TrimSpace(getenv(name)); value != "" {
			defs[i].BaseURL = value
		}
	}
}

// Waits are the routing slot-queue waits the public catalog reports, so the
// connect screen can say how long a wait may be.
type Waits struct {
	SlotWait    time.Duration // routing.slotQueueWaitMs
	SlotMaxWait time.Duration // routing.slotMaxWaitMs
}

// PublicDefinition is what the API tells a client about a provider
// (registry.publicDefinition): the connect-screen facts, never the key
// handling internals.
type PublicDefinition struct {
	ID              string        `json:"id"`
	Label           string        `json:"label"`
	Description     string        `json:"description"`
	Category        Category      `json:"category"`
	Website         *string       `json:"website"`
	KeyURL          *string       `json:"keyUrl"`
	Capabilities    []string      `json:"capabilities"`
	BaseURL         string        `json:"baseUrl"`
	EditableBaseURL bool          `json:"editableBaseUrl"`
	KeyOptional     bool          `json:"keyOptional"`
	Fields          []Field       `json:"fields"`
	Connect         *string       `json:"connect"`
	Billing         PublicBilling `json:"billing"`
}

// PublicBilling is the billing part of a public definition.
type PublicBilling struct {
	DefaultMode BillingMode `json:"defaultMode"`
	// FreeTierAvailable is true when the provider is known to have free models;
	// accounts may also find some in their own listing.
	FreeTierAvailable bool `json:"freeTierAvailable"`
	// FreeTierPartial means only some models are free.
	FreeTierPartial     bool      `json:"freeTierPartial"`
	Verified            bool      `json:"verified"`
	HasAlwaysFreeModels bool      `json:"hasAlwaysFreeModels"`
	Note                string    `json:"note"`
	Source              *string   `json:"source"`
	CheckedAt           *string   `json:"checkedAt"`
	FlatRate            *FlatRate `json:"flatRate"`
	SlotWaitMS          int64     `json:"slotWaitMs"`
	SlotMaxWaitMS       int64     `json:"slotMaxWaitMs"`
	PlanAvailable       bool      `json:"planAvailable"`
	PlanNote            *string   `json:"planNote"`
}

// PublicDefinition renders a definition for the API.
func (r *Registry) PublicDefinition(def Definition, waits Waits) PublicDefinition {
	freeModels := len(def.Billing.FreeTierModels) > 0
	alwaysFree := len(def.Billing.AlwaysFree) > 0
	billing := PublicBilling{
		DefaultMode:         def.Billing.DefaultMode,
		FreeTierAvailable:   def.Billing.FreeTierAll || freeModels || alwaysFree,
		FreeTierPartial:     !def.Billing.FreeTierAll && (freeModels || alwaysFree),
		Verified:            def.Billing.Verify != nil,
		HasAlwaysFreeModels: alwaysFree,
		Note:                def.Billing.Note,
		Source:              optionalString(def.Billing.Source),
		CheckedAt:           optionalString(def.Billing.CheckedAt),
		FlatRate:            def.Billing.FlatRate,
		SlotWaitMS:          waits.SlotWait.Milliseconds(),
		SlotMaxWaitMS:       waits.SlotMaxWait.Milliseconds(),
		PlanAvailable:       def.Billing.Plan != nil || def.EditableBaseURL,
	}
	if def.Billing.Plan != nil {
		billing.PlanNote = optionalString(def.Billing.Plan.Note)
	}
	return PublicDefinition{
		ID:              def.ID,
		Label:           def.Label,
		Description:     def.Description,
		Category:        def.Category,
		Website:         optionalString(def.Website),
		KeyURL:          optionalString(def.KeyURL),
		Capabilities:    def.Capabilities,
		BaseURL:         def.BaseURL,
		EditableBaseURL: def.EditableBaseURL,
		KeyOptional:     def.KeyOptional,
		Fields:          def.Fields,
		Connect:         optionalString(def.Connect),
		Billing:         billing,
	}
}

// optionalString is nil for an empty fact a provider may not have.
func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
