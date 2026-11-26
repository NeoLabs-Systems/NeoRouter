package providers

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

// embeddedDefinitions is the provider definition data.
//
//go:embed definitions.json
var embeddedDefinitions []byte

// dataSource names the embedded file in load errors.
const dataSource = "definitions.json"

var definitionID = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

// parseDefinitions reads definition JSON and validates it. It returns the definitions in file order,
// with every quota probe parser resolved.
func parseDefinitions(data []byte) ([]Definition, error) {
	var raw []json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("providers: read %s: %w", dataSource, err)
	}
	defs := make([]Definition, 0, len(raw))
	seen := make(map[string]bool, len(raw))
	for _, item := range raw {
		var def Definition
		if err := json.Unmarshal(item, &def); err != nil {
			return nil, fmt.Errorf("providers: read %s: %w", dataSource, err)
		}
		id := def.ID
		if id == "" {
			id = "unknown"
		}
		if issues := validateDefinition(&def); len(issues) > 0 {
			return nil, fmt.Errorf("Invalid provider definition in %s (%s): %s", dataSource, id, strings.Join(issues, "; "))
		}
		if seen[def.ID] {
			return nil, fmt.Errorf("Duplicate provider definition id %s in %s.", def.ID, dataSource)
		}
		seen[def.ID] = true
		defs = append(defs, def)
	}
	return defs, nil
}

// validateDefinition applies the definition schema rules and
// resolves the quota probe parser. It returns one issue per violated rule, each
// prefixed with the path that broke, and leaves the definition untouched except
// for the resolved parser.
func validateDefinition(def *Definition) []string {
	var issues []string
	add := func(format string, args ...any) {
		issues = append(issues, fmt.Sprintf(format, args...))
	}
	if !definitionID.MatchString(def.ID) {
		add("id: Invalid")
	}
	if strings.TrimSpace(def.Label) == "" {
		add("label: String must contain at least 1 character(s)")
	}
	if !def.Category.valid() {
		add("category: Invalid enum value")
	}
	if def.Connect != "" && def.Connect != "codex_device" && def.Connect != "nous_device" {
		add("connect: Invalid enum value")
	}
	if def.Search != nil && !oneOf(def.Search.Engine, "tavily", "exa", "linkup", "parallel", "firecrawl", "serper", "brave", "jina", "searxng") {
		add("search.engine: Invalid enum value")
	}
	if def.Weather != nil && !oneOf(def.Weather.Service, "open_meteo", "openweathermap", "weatherapi", "tomorrow", "visual_crossing") {
		add("weather.service: Invalid enum value")
	}
	if def.Website != "" && !isURL(def.Website) {
		add("website: Invalid url")
	}
	if def.KeyURL != "" && !isURL(def.KeyURL) {
		add("keyUrl: Invalid url")
	}
	if !def.Adapter.valid() {
		add("adapter: Invalid enum value")
	}
	for _, capability := range def.Capabilities {
		if capability != string(CapChat) {
			add("capabilities: Invalid enum value")
			break
		}
	}
	issues = append(issues, validateMedia(def.Media)...)
	issues = append(issues, validateModels(def.Models)...)
	for _, dialect := range def.Live {
		if !oneOf(dialect, LiveOpenAI, LiveRealtime, LiveGemini) {
			add("live: Invalid enum value")
			break
		}
	}
	for _, scheme := range def.BaseURLSchemes {
		if !oneOfString(scheme, "http", "https", "relay") {
			add("baseUrlSchemes: Invalid enum value")
			break
		}
	}
	if !oneOfString(def.Auth.Type, AuthBearer, AuthHeader, AuthNone) {
		add("auth.type: Invalid enum value")
	}
	for _, field := range def.Fields {
		if field.Key == "" {
			add("fields.key: Required")
		}
		if field.Label == "" {
			add("fields.label: Required")
		}
	}
	if def.Validate != nil && !def.Validate.Disabled && def.Validate.Path != "" && !strings.HasPrefix(def.Validate.Path, "/") && !isURL(def.Validate.Path) {
		add("validate.path: Invalid path")
	}
	issues = append(issues, validateBilling(def.Billing)...)
	issues = append(issues, validateLimits(def.Limits)...)
	issues = append(issues, validateQuota(def)...)
	issues = append(issues, validateQuirks(def.Quirks)...)
	return issues
}

// validateMedia applies the media schema: an entry is a path or the literal
// true.
func validateMedia(media MediaSpec) []string {
	var issues []string
	entries := []struct {
		name  string
		value any
	}{
		{"embed", media.Embed},
		{"generateImage", media.GenerateImage},
		{"speech", media.Speech},
		{"transcribe", media.Transcribe},
		{"moderate", media.Moderate},
		{"classify", media.Classify},
		{"createVideo", media.CreateVideo},
		{"decide", media.Decide},
	}
	for _, entry := range entries {
		switch entry.value.(type) {
		case nil, string, bool:
		default:
			issues = append(issues, fmt.Sprintf("media.%s: Invalid input", entry.name))
		}
	}
	return issues
}

// validateModels applies the models rules: the documented facts are positive
// whole numbers, keyed by model id.
func validateModels(models ModelsSpec) []string {
	var issues []string
	ids := make([]string, 0, len(models.Facts))
	for id := range models.Facts {
		ids = append(ids, id)
	}
	// Sorted so a definition with several bad facts always reports them alike.
	sort.Strings(ids)
	for _, id := range ids {
		facts := models.Facts[id]
		if facts.ContextWindow != nil && *facts.ContextWindow <= 0 {
			issues = append(issues, fmt.Sprintf("models.facts.%s.contextWindow: Number must be greater than 0", id))
		}
		if facts.MaxOutput != nil && *facts.MaxOutput <= 0 {
			issues = append(issues, fmt.Sprintf("models.facts.%s.maxOutput: Number must be greater than 0", id))
		}
	}
	return issues
}

func validateBilling(billing Billing) []string {
	var issues []string
	add := func(format string, args ...any) {
		issues = append(issues, fmt.Sprintf(format, args...))
	}
	if !oneOf(billing.DefaultMode, BillingModes...) {
		add("billing.defaultMode: Invalid enum value")
	}
	if billing.FlatRate != nil {
		if billing.FlatRate.DefaultPlan == "" {
			add("billing.flatRate.defaultPlan: Required")
		}
		if len(billing.FlatRate.Plans) == 0 {
			add("billing.flatRate.plans: Array must contain at least 1 element(s)")
		}
		for _, plan := range billing.FlatRate.Plans {
			if plan.ID == "" || plan.Label == "" {
				add("billing.flatRate.plans: Required")
			}
			if plan.Concurrent <= 0 {
				add("billing.flatRate.plans.concurrent: Number must be greater than 0")
			}
		}
	}
	if billing.Verify != nil {
		switch billing.Verify.Type {
		case VerifyRateLimitHeaders:
			if billing.Verify.MaxFreeRequestsPerDay <= 0 {
				add("billing.verify.maxFreeRequestsPerDay: Number must be greater than 0")
			}
		case VerifyPaidModelProbe:
			if len(billing.Verify.PaidOnlyModels) == 0 {
				add("billing.verify.paidOnlyModels: Array must contain at least 1 element(s)")
			}
		default:
			add("billing.verify.type: Invalid discriminator value")
		}
	}
	return issues
}

func validateLimits(limits Limits) []string {
	var issues []string
	add := func(format string, args ...any) {
		issues = append(issues, fmt.Sprintf(format, args...))
	}
	if !oneOfString(limits.Pool, "account", "model") {
		add("limits.pool: Invalid enum value")
	}
	for name, spec := range map[string]LimitSpec{"account": limits.Account, "defaults": limits.Defaults} {
		for field, value := range map[string]int{"concurrent": spec.Concurrent, "rpm": spec.RPM, "rpd": spec.RPD, "tpm": spec.TPM, "tpd": spec.TPD} {
			if value < 0 {
				add("limits.%s.%s: Number must be greater than 0", name, field)
			}
		}
	}
	for _, limit := range limits.Models {
		if limit.Match == "" {
			add("limits.models.match: Required")
		}
	}
	return issues
}

func validateQuota(def *Definition) []string {
	var issues []string
	add := func(format string, args ...any) {
		issues = append(issues, fmt.Sprintf(format, args...))
	}
	periods := def.Quota.HeaderPeriods
	if !oneOfString(periods.Requests, "minute", "day") {
		add("quota.headerPeriods.requests: Invalid enum value")
	}
	if !oneOfString(periods.Tokens, "minute", "day") {
		add("quota.headerPeriods.tokens: Invalid enum value")
	}
	if !oneOfString(def.Quota.PlainHeaderMetric, "rpm", "rpd") {
		add("quota.plainHeaderMetric: Invalid enum value")
	}
	if probe := def.Quota.Probe; probe != nil {
		if probe.Path == "" {
			add("quota.probe.path: Required")
		}
		parser, ok := quotaParsers[probe.ParseName]
		if !ok {
			add("quota.probe.parse: Unknown parser %q", probe.ParseName)
		} else {
			probe.Parse = parser
		}
	}
	return issues
}

func validateQuirks(quirks Quirks) []string {
	var issues []string
	add := func(format string, args ...any) {
		issues = append(issues, fmt.Sprintf(format, args...))
	}
	if quirks.TimeoutMS < 0 {
		add("quirks.timeoutMs: Number must be greater than 0")
	}
	if quirks.MaxSpeechChars < 0 {
		add("quirks.maxSpeechChars: Number must be greater than 0")
	}
	if quirks.ImageAPI != "" && !oneOfString(quirks.ImageAPI, "openai", "nvidia_genai") {
		add("quirks.imageApi: Invalid enum value")
	}
	if quirks.SpeechAPI != "" && !oneOfString(quirks.SpeechAPI, "openai", "riva") {
		add("quirks.speechApi: Invalid enum value")
	}
	for _, entry := range quirks.ModelVoices {
		if entry.Match == "" || len(entry.Voices) == 0 || entry.DefaultVoice == "" {
			add("quirks.modelVoices: Required")
			break
		}
	}
	return issues
}

// isURL mirrors z.string().url(): a parsable absolute URL.
func isURL(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && parsed.Scheme != "" && parsed.Host != ""
}

// oneOfString reports whether value is one of the allowed strings.
func oneOfString[T ~string](value T, allowed ...T) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

// oneOf reports whether value is one of the allowed values.
func oneOf[T comparable](value T, allowed ...T) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}
