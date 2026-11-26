package catalog

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/NeoLabs-Systems/neolabs-go/dbx"
	"github.com/NeoLabs-Systems/neorouter/internal/config"
	"github.com/NeoLabs-Systems/neorouter/internal/schema"
)

func fp(value float64) *float64 { return &value }
func ip(value int64) *int64     { return &value }
func bp(value bool) *bool       { return &value }

// devFact builds a models.dev fact the way the source normalizes one.
func devFact(name string, priceIn, priceOut float64, contextWindow int64, reasoning bool, releaseDate string) *Fact {
	return &Fact{ProviderFacts: ProviderFacts{
		Name: name, PriceIn: fp(priceIn), PriceOut: fp(priceOut), ContextWindow: ip(contextWindow),
		Reasoning: bp(reasoning), ReleaseDate: releaseDate, TextOutput: bp(true),
	}}
}

// aaFact builds an Artificial Analysis record.
func aaFact(slug, name string, index float64, benchmarks map[string]float64) *Fact {
	if benchmarks == nil {
		benchmarks = map[string]float64{}
	}
	return &Fact{Slug: slug, Name: name, IntelligenceIndex: fp(index), Benchmarks: benchmarks}
}

// testEnv is a catalog service on a real temporary SQLite database.
type testEnv struct {
	svc      *Service
	db       *dbx.DB
	settings *fakeSettings
	models   *fakeModelLister
}

func newTestEnv(t *testing.T, opts ...func(*Options)) *testEnv {
	t.Helper()
	ctx := context.Background()
	db, err := dbx.Open(ctx, t.TempDir()+"/neorouter.sqlite3", dbx.Options{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := schema.Ensure(ctx, db.W); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	settings := &fakeSettings{global: map[string]json.RawMessage{}, user: map[string]json.RawMessage{}}
	lister := &fakeModelLister{models: map[string][]ListedModel{}}
	options := Options{
		DB:          db,
		Log:         slog.New(slog.DiscardHandler),
		Definitions: DefinitionFunc(testDefinition),
		Models:      lister,
		Settings:    settings,
		Config:      func() config.Config { return config.Config{MetadataSourcesEnabled: true} },
		DataDir:     t.TempDir(),
	}
	for _, opt := range opts {
		opt(&options)
	}
	svc := New(options)
	svc.Metadata.SkillsFromBenchmarks = testSkillsFromBenchmarks
	env := &testEnv{svc: svc, db: db, settings: settings, models: lister}
	env.seedUser(t, "user-1")
	return env
}

// seedUser inserts a user row the account foreign key needs.
func (e *testEnv) seedUser(t *testing.T, id string) {
	t.Helper()
	if _, err := e.db.W.Exec(`INSERT INTO users (id, username, email, password_hash, role) VALUES (?, ?, ?, ?, 'user')`,
		id, "user-"+id, id+"@example.test", "x"); err != nil {
		t.Fatalf("seed user: %v", err)
	}
}

// seedAccount inserts one provider account row.
func (e *testEnv) seedAccount(t *testing.T, id, definitionID, slug string, billingMode string, mutate ...func(*accountSeed)) {
	t.Helper()
	seed := accountSeed{
		definitionID: definitionID, slug: slug, label: slug, billingMode: billingMode,
		billingState: "unknown", enabled: 1, status: "healthy", optionsJSON: "{}", overflowPolicy: "auto",
	}
	for _, apply := range mutate {
		if apply != nil {
			apply(&seed)
		}
	}
	var classification, secret any
	if seed.classification != "" {
		classification = seed.classification
	}
	if seed.secret != "" {
		secret = seed.secret
	}
	if _, err := e.db.W.Exec(`INSERT INTO provider_accounts
		(id, user_id, definition_id, slug, label, base_url, secret_encrypted, options_json, billing_mode, enabled, status,
		 billing_state, classification, prepaid, overflow_policy, slot_wait_ms)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, "user-1", seed.definitionID, seed.slug, seed.label, nilIfEmpty(seed.baseURL), secret, seed.optionsJSON,
		seed.billingMode, seed.enabled, seed.status, seed.billingState, classification, seed.prepaid,
		seed.overflowPolicy, nilIfEmptyInt(seed.slotWaitMs)); err != nil {
		t.Fatalf("seed account: %v", err)
	}
}

type accountSeed struct {
	definitionID   string
	slug           string
	label          string
	baseURL        string
	secret         string
	billingMode    string
	billingState   string
	classification string
	prepaid        int64
	enabled        int64
	status         string
	optionsJSON    string
	overflowPolicy string
	slotWaitMs     *int64
}

func nilIfEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nilIfEmptyInt(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

// fakeSettings is an in-memory internal/settings.
type fakeSettings struct {
	global map[string]json.RawMessage
	user   map[string]json.RawMessage
}

func (f *fakeSettings) Global(key string) (json.RawMessage, bool, error) {
	value, ok := f.global[key]
	return value, ok, nil
}

func (f *fakeSettings) SetGlobal(key string, value any) error {
	if value == nil {
		delete(f.global, key)
		return nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	f.global[key] = encoded
	return nil
}

func (f *fakeSettings) UserSetting(userID, key string) (json.RawMessage, bool, error) {
	value, ok := f.user[userID+"|"+key]
	return value, ok, nil
}

func (f *fakeSettings) SetUserSetting(userID, key string, value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	f.user[userID+"|"+key] = encoded
	return nil
}

func (f *fakeSettings) Seal(plain string) (string, error) { return "sealed:" + plain, nil }

func (f *fakeSettings) Open(sealed string) (string, error) { return trimSealed(sealed), nil }

func trimSealed(value string) string {
	const prefix = "sealed:"
	if len(value) > len(prefix) && value[:len(prefix)] == prefix {
		return value[len(prefix):]
	}
	return value
}

// fakeModelLister is the providers account service as the catalog consumes it.
type fakeModelLister struct {
	models map[string][]ListedModel
	known  map[string]bool
	err    error
}

func (f *fakeModelLister) ListModels(_ context.Context, accountID string) ([]ListedModel, bool, error) {
	if f.err != nil {
		return nil, false, f.err
	}
	if f.known != nil {
		if !f.known[accountID] {
			return nil, false, nil
		}
		return f.models[accountID], true, nil
	}
	models, ok := f.models[accountID]
	return models, ok, nil
}

// testSkillsFromBenchmarks mirrors routing's task skill mapping for the
// benchmarks the metadata indexes publish.
func testSkillsFromBenchmarks(mapped map[string]float64) map[string]float64 {
	benchmarkSkills := map[string][]string{
		"livecodebench": {"coding"}, "scicode": {"coding"}, "terminalbench_hard": {"coding", "agentic", "terminal"},
		"aime_25": {"math"}, "aime": {"math"}, "math_500": {"math"}, "gpqa": {"reasoning"}, "hle": {"reasoning"},
		"mmlu_pro": {"knowledge"}, "ifbench": {"instruction"}, "tau2": {"agentic"}, "lcr": {"long_context"},
	}
	type sum struct {
		total float64
		count float64
	}
	sums := map[string]*sum{}
	for benchmark, value := range mapped {
		for _, skill := range benchmarkSkills[benchmark] {
			if sums[skill] == nil {
				sums[skill] = &sum{}
			}
			sums[skill].total += value
			sums[skill].count++
		}
	}
	if len(sums) == 0 {
		return map[string]float64{}
	}
	skills := make(map[string]float64, len(sums))
	for skill, item := range sums {
		skills[skill] = roundTo(mathMin(1, item.total/item.count), 4)
	}
	return skills
}

// testDefinition is the provider registry as the catalog consumes it: the
// billing blocks of the real definitions, which the classification rules are
// written against.
func testDefinition(id string) (Definition, bool) {
	definition, ok := testDefinitions[id]
	return definition, ok
}

var testDefinitions = map[string]Definition{
	"openai": {ID: "openai", Label: "OpenAI", Category: "paid"},
	"openrouter": {
		ID: "openrouter", Label: "OpenRouter", Category: "aggregator",
		Billing: Billing{AlwaysFree: []string{`:free$`, `^openrouter/free$`}},
	},
	"groq": {
		ID: "groq", Label: "Groq", Category: "free",
		Billing: Billing{FreeTierAll: true, Verify: true},
	},
	"cerebras": {ID: "cerebras", Label: "Cerebras", Category: "paid"},
	"nvidia": {
		ID: "nvidia", Label: "NVIDIA NIM", Category: "free",
		Billing: Billing{FreeTierAll: true},
	},
	"google": {
		ID: "google", Label: "Google Gemini", Category: "free",
		Billing: Billing{Verify: true, FreeTierModels: []string{
			`^gemini-(2\.5|3|3\.\d)-flash(-lite)?(-preview)?$`,
			`^gemini-flash(-lite)?-latest$`,
			`^gemini-2\.5-pro$`,
			`^gemma-`,
			`^gemini-embedding-(001|2)(-preview)?$`,
			`^gemini-(2\.5|3\.\d)-flash(-lite)?(-preview)?-tts(-preview)?$`,
			`^gemini-\d+(\.\d+)?-transcribe$`,
			`^gemini-.*(-live|-native-audio)`,
		}},
	},
	"ollama_cloud": {
		ID: "ollama_cloud", Label: "Ollama Cloud", Category: "paid",
		Billing: Billing{FreeTierModels: []string{
			`^beam(:|$)`, `^gemma4:31b`, `^gpt-oss:(120b|20b)`, `^nemotron-3-(nano:30b|super|ultra)`,
		}},
	},
	"siliconflow": {ID: "siliconflow", Label: "SiliconFlow", Category: "aggregator"},
	"together": {
		ID: "together", Label: "Together AI", Category: "aggregator",
		Billing: Billing{TrustListingFree: bp(false)},
	},
	"zai": {
		ID: "zai", Label: "Z.ai (GLM)", Category: "free",
		Billing: Billing{AlwaysFree: []string{`^glm-4\.7-flash$`, `^glm-4\.5-flash$`, `^glm-4\.6v-flash$`}},
	},
	"nous": {
		ID: "nous", Label: "Nous Research", Category: "paid",
		Billing: Billing{AlwaysFree: []string{`:free$`}},
	},
	"huggingface": {ID: "huggingface", Label: "Hugging Face", Category: "paid"},
	"ollama": {
		ID: "ollama", Label: "Ollama", Category: "local",
		Billing: Billing{FreeTierAll: true, LocalOnly: true},
	},
	"ovh": {
		ID: "ovh", Label: "OVHcloud", Category: "free",
		Billing: Billing{FreeTierAll: true, KeylessOnly: true, TrustListingFree: bp(false)},
	},
	"neorouter_peer": {
		ID: "neorouter_peer", Label: "Pool partner", Category: "pool",
		Billing: Billing{FreeTierAll: true},
	},
	"custom_openai": {ID: "custom_openai", Label: "Custom OpenAI-compatible", Category: "custom"},
	// Kourier documents the limits its bare listing omits (models.facts in
	// definitions.json; kourier_test.go exercises a sync with them).
	"kourier": {
		ID: "kourier", Label: "Kourier", Category: "plan",
		Billing:    Billing{TrustListingFree: bp(false)},
		ModelFacts: map[string]ModelFacts{"deepseek-v4.1-flash": {ContextWindow: ip(262144), MaxOutput: ip(16384)}},
	},
}
