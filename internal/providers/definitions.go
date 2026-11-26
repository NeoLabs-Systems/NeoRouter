// Package providers holds NeoRouter's provider layer: the provider definitions
// (definitions.json), their registry, the error kinds routing reacts to, the
// outbound HTTP helper, the private-network guard, model metadata normalization
// and the adapter contract.
//
// The concrete adapters live in the subpackage internal/providers/adapters and
// import this package; nothing here imports them. A Registry resolves an
// adapter through the AdapterFactory it was built with.
package providers

// Definition is one provider: the static facts of a connection (where it
// lives, how it authenticates, what it can do, what it costs). It is the parsed
// form of a definition module — every documented default already applied — and
// is read-only: a copy is handed out, never mutated.
type Definition struct {
	ID          string   `json:"id"`
	Label       string   `json:"label"`
	Description string   `json:"description"`
	Category    Category `json:"category"`

	// Connect is the sign-in flow a provider is connected with instead of a
	// pasted key (codex_device, nous_device).
	Connect string `json:"connect,omitempty"`
	// Search names the dialect a web-search engine speaks.
	Search *SearchSpec `json:"search,omitempty"`
	// Weather names the dialect a weather service speaks.
	Weather *WeatherSpec `json:"weather,omitempty"`

	Website string `json:"website,omitempty"`
	KeyURL  string `json:"keyUrl,omitempty"`

	Adapter      AdapterName   `json:"adapter"`
	Capabilities []string      `json:"capabilities"`
	Media        MediaSpec     `json:"media"`
	Live         []LiveDialect `json:"live"`

	BaseURL         string   `json:"baseUrl"`
	EditableBaseURL bool     `json:"editableBaseUrl"`
	BaseURLSchemes  []string `json:"baseUrlSchemes"`

	Auth          Auth              `json:"auth"`
	KeyOptional   bool              `json:"keyOptional"`
	Fields        []Field           `json:"fields"`
	Headers       map[string]string `json:"headers"`
	SessionHeader string            `json:"sessionHeader,omitempty"`
	ChatPath      string            `json:"chatPath,omitempty"`

	Models      ModelsSpec    `json:"models"`
	ExtraModels []ExtraModel  `json:"extraModels"`
	Validate    *ValidateSpec `json:"validate,omitempty"`

	Billing Billing `json:"billing"`
	Limits  Limits  `json:"limits"`
	Quota   Quota   `json:"quota"`
	Quirks  Quirks  `json:"quirks"`
}

// SearchSpec is the search dialect a definition names.
type SearchSpec struct {
	Engine string `json:"engine"`
}

// WeatherSpec is the weather dialect a definition names.
type WeatherSpec struct {
	Service string `json:"service"`
}

// MediaSpec lists the non-chat endpoints a provider offers: the adapter method
// as key, and either the OpenAI-compatible path or true for an adapter with its
// own dialect. Use Endpoint to read one.
type MediaSpec struct {
	Embed         any  `json:"embed,omitempty"`
	GenerateImage any  `json:"generateImage,omitempty"`
	Speech        any  `json:"speech,omitempty"`
	Transcribe    any  `json:"transcribe,omitempty"`
	Moderate      any  `json:"moderate,omitempty"`
	Classify      any  `json:"classify,omitempty"`
	CreateVideo   any  `json:"createVideo,omitempty"`
	Decide        any  `json:"decide,omitempty"`
	Search        bool `json:"search,omitempty"`
	Weather       bool `json:"weather,omitempty"`
}

// Endpoint reports whether the definition offers method and, for adapters that
// speak the OpenAI paths, the path to call. An empty path with ok true means
// the adapter has its own dialect.
func (m MediaSpec) Endpoint(method Capability) (path string, ok bool) {
	switch method {
	case CapEmbed:
		return mediaPath(m.Embed)
	case CapGenerateImage:
		return mediaPath(m.GenerateImage)
	case CapSpeech:
		return mediaPath(m.Speech)
	case CapTranscribe:
		return mediaPath(m.Transcribe)
	case CapModerate:
		return mediaPath(m.Moderate)
	case CapClassify:
		return mediaPath(m.Classify)
	case CapCreateVideo:
		return mediaPath(m.CreateVideo)
	case CapDecide:
		return mediaPath(m.Decide)
	case CapSearch:
		return "", m.Search
	case CapWeather:
		return "", m.Weather
	}
	return "", false
}

// mediaPath reads one media entry: a path string or the literal true.
func mediaPath(value any) (string, bool) {
	switch v := value.(type) {
	case string:
		return v, true
	case bool:
		return "", v
	}
	return "", false
}

// Auth is how a provider wants its key.
type Auth struct {
	// Type is bearer, header or none.
	Type string `json:"type"`
	// Header carries the key when Type is header, e.g. x-api-key.
	Header string `json:"header,omitempty"`
	// AnonymousKey is sent when the account has no key (keyless providers).
	AnonymousKey string `json:"anonymousKey,omitempty"`
}

// Auth types.
const (
	AuthBearer = "bearer"
	AuthHeader = "header"
	AuthNone   = "none"
)

// Field is a per-account setting a provider needs besides its key, such as a
// Cloudflare account id.
type Field struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Required    bool   `json:"required"`
	Placeholder string `json:"placeholder,omitempty"`
}

// ModelsSpec says where a provider lists its models.
type ModelsSpec struct {
	// Path overrides the default /models listing.
	Path string `json:"path,omitempty"`
	// Static is the whole listing for providers that publish no endpoint; the
	// entries are listing JSON, normalized by NormalizeModel.
	Static []any `json:"static,omitempty"`
	// ExtraPaths are further listings (OpenRouter's embedding and image models).
	ExtraPaths []string `json:"extraPaths"`
	// ExcludeFree drops models the listing prices at zero, for hosts whose free
	// models only work in their own app.
	ExcludeFree bool `json:"excludeFree,omitempty"`
	// OptIn are patterns of models that are listed but start switched off.
	OptIn []string `json:"optIn"`
	// Facts are documented per-model limits for hosts whose listing omits them,
	// keyed by model id. The listing itself always wins; these beat models.dev
	// for this provider.
	Facts map[string]ModelFacts `json:"facts,omitempty"`
}

// ModelFacts are the limits a provider documents for one model whose listing
// does not report them. An absent field is a limit the provider does not
// document, so the catalog falls back to models.dev for it.
type ModelFacts struct {
	ContextWindow *int64 `json:"contextWindow,omitempty"`
	MaxOutput     *int64 `json:"maxOutput,omitempty"`
}

// ExtraModel is a model a provider serves but does not list (NVIDIA's FLUX
// endpoints).
type ExtraModel struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
	Type string `json:"type,omitempty"`
}

// ValidateSpec is how a key is checked. The absent form (nil) is the default
// authenticated GET; the false form cannot be checked without spending quota.
type ValidateSpec struct {
	Disabled bool
	// Path overrides the endpoint checked.
	Path string
	// ChatProbe checks the key with a 1-token chat request, for hosts whose
	// /models is public.
	ChatProbe bool
	// ProbeModels are the cheap models to try first.
	ProbeModels []string
}

// BillingMode is the billing the provider is believed to use by default.
type BillingMode string

const (
	BillingFreeTier BillingMode = "free_tier"
	BillingPaid     BillingMode = "paid"
	BillingPlan     BillingMode = "plan"
	BillingPlanPaid BillingMode = "plan_paid"
	BillingFlatRate BillingMode = "flat_rate"
)

// BillingModes lists every accepted billing mode.
var BillingModes = []BillingMode{BillingFreeTier, BillingPaid, BillingPlan, BillingPlanPaid, BillingFlatRate}

// Billing records what a provider charges and how that was established.
type Billing struct {
	DefaultMode BillingMode `json:"defaultMode"`
	// Plan is a subscription with an included allowance sold for API use.
	Plan *Plan `json:"plan,omitempty"`
	// FlatRate is a prepaid plan the provider sells.
	FlatRate *FlatRate `json:"flatRate,omitempty"`
	// FreeTierAll means every model the account can call is free with
	// billing_mode=free_tier.
	FreeTierAll bool `json:"freeTierAll"`
	// FreeTierModels are patterns of the models the free tier includes.
	FreeTierModels []string `json:"freeTierModels"`
	// Verify is automatic proof that the account is not billed.
	Verify *Verify `json:"verify,omitempty"`
	// LocalOnly means the free tier applies only to a loopback or private base URL.
	LocalOnly bool `json:"localOnly"`
	// KeylessOnly means it applies only without a key.
	KeylessOnly bool `json:"keylessOnly"`
	// Source is where the free-tier facts were checked, CheckedAt when (YYYY-MM-DD).
	Source    string `json:"source,omitempty"`
	CheckedAt string `json:"checkedAt,omitempty"`
	// AlwaysFree are models that are free whatever the billing mode.
	AlwaysFree []string `json:"alwaysFree"`
	// TrustListingFree means a price of 0 in the listing means free.
	TrustListingFree bool   `json:"trustListingFree"`
	Note             string `json:"note"`
}

// Plan is a subscription's note.
type Plan struct {
	Note string `json:"note"`
}

// FlatRate is a prepaid plan a provider sells.
type FlatRate struct {
	DefaultPlan string         `json:"defaultPlan"`
	Plans       []FlatRatePlan `json:"plans"`
}

// FlatRatePlan is one prepaid plan.
type FlatRatePlan struct {
	ID         string `json:"id"`
	Label      string `json:"label"`
	Concurrent int    `json:"concurrent"`
}

// Verify kinds.
const (
	VerifyRateLimitHeaders = "ratelimit_headers"
	VerifyPaidModelProbe   = "paid_model_probe"
)

// Verify is the check that proves an account is not billed.
type Verify struct {
	// Type is ratelimit_headers or paid_model_probe.
	Type string
	// MaxFreeRequestsPerDay and ProbeModels belong to ratelimit_headers.
	MaxFreeRequestsPerDay float64
	ProbeModels           []string
	// PaidOnlyModels belong to paid_model_probe.
	PaidOnlyModels []string
}

// Limits is how a provider's limits are pooled and learned.
type Limits struct {
	// Pool is account or model.
	Pool     string       `json:"pool"`
	Account  LimitSpec    `json:"account"`
	Defaults LimitSpec    `json:"defaults"`
	Models   []ModelLimit `json:"models"`
	// PoolMatch is the model pattern whose limits are pooled with the account.
	PoolMatch string `json:"poolMatch,omitempty"`
}

// LimitSpec is one set of published limits; a zero field is not published.
type LimitSpec struct {
	Concurrent int `json:"concurrent,omitempty"`
	RPM        int `json:"rpm,omitempty"`
	RPD        int `json:"rpd,omitempty"`
	TPM        int `json:"tpm,omitempty"`
	TPD        int `json:"tpd,omitempty"`
}

// ModelLimit is a limit set for models matching Match.
type ModelLimit struct {
	LimitSpec
	Match string `json:"match"`
}

// Quota is how a provider reports what is left.
type Quota struct {
	HeaderPeriods     HeaderPeriods `json:"headerPeriods"`
	PlainHeaderMetric string        `json:"plainHeaderMetric"` // rpm or rpd
	// Probe reads a quota endpoint; absent when the provider has none.
	Probe *QuotaProbe `json:"probe,omitempty"`
}

// HeaderPeriods is the window each rate-limit header counts over.
type HeaderPeriods struct {
	Requests string `json:"requests"` // minute or day
	Tokens   string `json:"tokens"`   // minute or day
}

// QuotaProbe is a quota endpoint and the parser that reads it.
type QuotaProbe struct {
	Path string `json:"path"`
	// ParseName is the parser's name as written in definitions.json.
	ParseName string `json:"parse"`
	// Parse is the parser function, resolved at load time.
	Parse QuotaParser `json:"-"`
}

// Quirks are the per-provider deviations an adapter has to honor.
type Quirks struct {
	// TimeoutMS overrides the configured upstream timeout.
	TimeoutMS int `json:"timeoutMs,omitempty"`
	// UserAgent is sent instead of the client's default.
	UserAgent string `json:"userAgent,omitempty"`

	SingleToolCall *bool `json:"singleToolCall,omitempty"`
	NoTools        *bool `json:"noTools,omitempty"`
	// MaxTokensField is the field that carries the output limit.
	MaxTokensField string `json:"maxTokensField,omitempty"`
	// MinMaxTokens is the smallest output limit the provider accepts.
	MinMaxTokens int      `json:"minMaxTokens,omitempty"`
	DropFields   []string `json:"dropFields,omitempty"`
	// StripSchemaKeys are keys removed from tool JSON schemas.
	StripSchemaKeys []string `json:"stripSchemaKeys,omitempty"`
	StreamUsage     *bool    `json:"streamUsage,omitempty"`

	// Media dialect details (adapters/media_openai.go).
	EmbeddingInputType    *bool    `json:"embeddingInputType,omitempty"`
	ImageAPI              string   `json:"imageApi,omitempty"` // openai or nvidia_genai
	NoImageResponseFormat *bool    `json:"noImageResponseFormat,omitempty"`
	DefaultVoice          string   `json:"defaultVoice,omitempty"`
	Voices                []string `json:"voices,omitempty"`
	// ModelVoices are voices per model; the first match wins over Voices.
	ModelVoices []ModelVoices `json:"modelVoices,omitempty"`
	// SpeechFormats are the formats the speech endpoint produces, preferred first.
	SpeechFormats []string `json:"speechFormats,omitempty"`
	// SpeechVoicesPath is a listing whose models name their voices.
	SpeechVoicesPath string `json:"speechVoicesPath,omitempty"`
	// MaxSpeechChars is the most text one speech request takes.
	MaxSpeechChars int `json:"maxSpeechChars,omitempty"`
	// SpeechAPI is openai (default) or riva.
	SpeechAPI         string `json:"speechApi,omitempty"`
	TranscriptionJSON *bool  `json:"transcriptionJson,omitempty"`
	// EmbeddingDimensionsField is the field that asks for fewer dimensions.
	EmbeddingDimensionsField string `json:"embeddingDimensionsField,omitempty"`
	// VideoDuration sends a whole-number duration instead of OpenAI's seconds.
	VideoDuration *bool `json:"videoDuration,omitempty"`
}

// ModelVoices is one model's voice set.
type ModelVoices struct {
	Match        string   `json:"match"`
	Voices       []string `json:"voices"`
	DefaultVoice string   `json:"defaultVoice"`
}

// IsTrue reports whether an optional definition flag is set. An absent flag
// means the same as false.
func IsTrue(flag *bool) bool { return flag != nil && *flag }
