package providers

// The provider names the definition data and the API use: the category a
// provider is listed under, the adapter dialect it speaks and the live-voice
// protocols it offers.

// Category is the group a provider is listed under.
type Category string

const (
	CategoryFree       Category = "free"
	CategoryPaid       Category = "paid"
	CategoryAggregator Category = "aggregator"
	CategoryPlan       Category = "plan"
	CategoryLocal      Category = "local"
	CategoryCustom     Category = "custom"
	CategorySearch     Category = "search"
	CategoryWeather    Category = "weather"
	CategoryPool       Category = "pool"
)

// Categories lists every category in the order registry.js accepts.
var Categories = []Category{
	CategoryFree, CategoryPaid, CategoryAggregator, CategoryPlan,
	CategoryLocal, CategoryCustom, CategorySearch, CategoryWeather, CategoryPool,
}

func (c Category) valid() bool {
	for _, known := range Categories {
		if c == known {
			return true
		}
	}
	return false
}

// AdapterName is the upstream dialect an adapter speaks. Each name has one
// implementation in internal/providers/adapters.
type AdapterName string

const (
	AdapterOpenAICompatible AdapterName = "openai_compatible"
	AdapterAnthropic        AdapterName = "anthropic"
	AdapterGoogleGemini     AdapterName = "google_gemini"
	AdapterCloudflare       AdapterName = "cloudflare"
	AdapterSearch           AdapterName = "search"
	AdapterWeather          AdapterName = "weather"
	AdapterDeepgram         AdapterName = "deepgram"
	AdapterElevenLabs       AdapterName = "elevenlabs"
	AdapterCodex            AdapterName = "codex"
	AdapterNous             AdapterName = "nous"
	AdapterLocalRuntime     AdapterName = "local_runtime"
)

// Adapters lists every adapter name in registry.js order.
var Adapters = []AdapterName{
	AdapterOpenAICompatible, AdapterAnthropic, AdapterGoogleGemini, AdapterCloudflare,
	AdapterSearch, AdapterWeather, AdapterDeepgram, AdapterElevenLabs, AdapterCodex,
	AdapterNous, AdapterLocalRuntime,
}

func (a AdapterName) valid() bool {
	for _, known := range Adapters {
		if a == known {
			return true
		}
	}
	return false
}

// LiveDialect is a live-voice WebSocket protocol a provider speaks.
type LiveDialect string

const (
	LiveOpenAI   LiveDialect = "openai_live"
	LiveRealtime LiveDialect = "openai_realtime"
	LiveGemini   LiveDialect = "gemini_live"
)

// LiveDialects lists every live dialect registry.js accepts.
var LiveDialects = []LiveDialect{LiveOpenAI, LiveRealtime, LiveGemini}
