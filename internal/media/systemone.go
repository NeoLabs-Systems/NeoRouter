package media

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"

	"github.com/NeoLabs-Systems/neorouter/internal/providers"
)

// System One models are small, fast classifiers that answer a yes/no question
// (is this unsafe? is this a prompt injection?) instead of writing text. Most
// providers serve them through chat completions with model-specific output
// formats; this file talks to each family and returns the OpenAI
// /v1/moderations result shape, so clients get one format whatever ran.

// hazardCategories maps Llama Guard / NemoGuard hazard codes (MLCommons
// taxonomy) to OpenAI categories.
var hazardCategories = map[string]string{
	"S1": "violence", "S2": "illicit", "S3": "sexual", "S4": "sexual/minors", "S5": "harassment",
	"S6": "illicit", "S7": "privacy", "S8": "intellectual_property", "S9": "illicit/violent",
	"S10": "hate", "S11": "self-harm", "S12": "sexual", "S13": "elections", "S14": "illicit",
}

// moderationCategories is OpenAI's category list, in its order.
var moderationCategories = []string{
	"harassment", "harassment/threatening", "hate", "hate/threatening", "illicit", "illicit/violent",
	"self-harm", "self-harm/intent", "self-harm/instructions", "sexual", "sexual/minors",
	"violence", "violence/graphic", "prompt_injection", "privacy", "intellectual_property", "elections",
}

// defaultSafetyPolicy is the policy a policy-following classifier gets when the
// client sends none.
const defaultSafetyPolicy = `Classify the user content against this safety policy.
Violations: instructions or encouragement for violence, weapons, terrorism, crime, drugs, hacking or fraud; sexual content involving minors; explicit sexual content; hate or harassment against protected groups; self-harm encouragement or instructions; sharing other people's private data.
Allowed: everything else, including discussion, news, fiction and education about these topics without actionable harm.
Answer only with JSON: {"violation": 0 or 1, "category": "<short category or none>", "rationale": "<one sentence>"}`

// isModerationCategory reports whether name is one OpenAI names.
func isModerationCategory(name string) bool {
	for _, category := range moderationCategories {
		if category == name {
			return true
		}
	}
	return false
}

// categoryFlags is the categories object of a moderation result: false for every
// category the model did not name, in OpenAI's order whatever order they were
// set in.
type categoryFlags struct {
	values map[string]bool
}

func newCategoryFlags() categoryFlags {
	return categoryFlags{values: map[string]bool{}}
}

func (c categoryFlags) get(name string) bool { return c.values[name] }

func (c categoryFlags) set(name string, value bool) { c.values[name] = value }

// MarshalJSON writes the categories in OpenAI's order.
func (c categoryFlags) MarshalJSON() ([]byte, error) {
	var buffer bytes.Buffer
	buffer.WriteByte('{')
	for index, name := range moderationCategories {
		if index > 0 {
			buffer.WriteByte(',')
		}
		key, err := json.Marshal(name)
		if err != nil {
			return nil, err
		}
		buffer.Write(key)
		buffer.WriteByte(':')
		if c.values[name] {
			buffer.WriteString("true")
		} else {
			buffer.WriteString("false")
		}
	}
	buffer.WriteByte('}')
	return buffer.Bytes(), nil
}

// categoryScores is the category_scores object of a moderation result, in the
// same order.
type categoryScores struct {
	values map[string]float64
}

func newCategoryScores() categoryScores {
	return categoryScores{values: map[string]float64{}}
}

func (c categoryScores) get(name string) float64 { return c.values[name] }

func (c categoryScores) set(name string, value float64) { c.values[name] = value }

// raise keeps the highest score a category was given.
func (c categoryScores) raise(name string, value float64) {
	if value > c.values[name] {
		c.values[name] = value
	}
}

// MarshalJSON writes the scores in OpenAI's order.
func (c categoryScores) MarshalJSON() ([]byte, error) {
	var buffer bytes.Buffer
	buffer.WriteByte('{')
	for index, name := range moderationCategories {
		if index > 0 {
			buffer.WriteByte(',')
		}
		key, err := json.Marshal(name)
		if err != nil {
			return nil, err
		}
		buffer.Write(key)
		buffer.WriteByte(':')
		buffer.WriteString(strconv.FormatFloat(c.values[name], 'f', -1, 64))
	}
	buffer.WriteByte('}')
	return buffer.Bytes(), nil
}

// moderationResult is one OpenAI moderation record.
type moderationResult struct {
	Flagged        bool           `json:"flagged"`
	Categories     categoryFlags  `json:"categories"`
	CategoryScores categoryScores `json:"category_scores"`
	Rationale      *string        `json:"rationale,omitempty"`
}

// newModerationResult is a clean record: nothing flagged, every score zero.
func newModerationResult() moderationResult {
	return moderationResult{Categories: newCategoryFlags(), CategoryScores: newCategoryScores()}
}

// flag marks one category.
func flagCategory(result *moderationResult, category string, score float64) {
	if !isModerationCategory(category) {
		category = "illicit"
	}
	result.Flagged = true
	result.Categories.set(category, true)
	result.CategoryScores.raise(category, score)
}

// systemOneFamily is the family of classifier a model id names.
func systemOneFamily(modelID string) string {
	id := strings.ToLower(modelID)
	switch {
	case strings.Contains(id, "prompt-guard"):
		return "prompt_guard"
	case strings.Contains(id, "safeguard"):
		return "safeguard"
	case strings.Contains(id, "llama-guard"):
		return "llama_guard"
	case containsAny(id, "nemoguard", "content-safety", "safety-guard"):
		return "nemoguard"
	case strings.Contains(id, "topic-control"):
		return "topic_control"
	case strings.Contains(id, "shieldgemma"):
		return "shieldgemma"
	case strings.Contains(id, "moderation"):
		return "native"
	}
	return "generic"
}

func containsAny(value string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}

// categoryFromText maps a free-text category ("Illicit behavior", "Violence") to
// the nearest OpenAI name.
func categoryFromText(text string) string {
	value := strings.ToLower(text)
	switch {
	case containsAny(value, "minor", "child"):
		return "sexual/minors"
	case strings.Contains(value, "sex"):
		return "sexual"
	case containsAny(value, "selfharm", "self-harm", "suicide"):
		return "self-harm"
	case strings.Contains(value, "hate"):
		return "hate"
	case containsAny(value, "harass", "bully"):
		return "harassment"
	case containsAny(value, "weapon", "bomb", "terror", "violent crime"):
		return "illicit/violent"
	case strings.Contains(value, "violen"):
		return "violence"
	case containsAny(value, "privacy", "personal data", "pii"):
		return "privacy"
	case containsAny(value, "injection", "jailbreak"):
		return "prompt_injection"
	}
	return "illicit"
}

// invalidOutput is the provider failure a classifier's unusable answer is.
func invalidOutput(message string) error {
	return providers.NewError(providers.KindInvalidOutput, message, providers.ErrorOptions{})
}

// parseSystemOne reads one classifier answer as an OpenAI moderation record.
func parseSystemOne(family, text string) (moderationResult, error) {
	result := newModerationResult()
	raw := strings.TrimSpace(text)
	switch family {
	case "prompt_guard":
		// Prompt Guard 2 answers with the probability that the text is an attack.
		score, err := strconv.ParseFloat(raw, 64)
		if raw == "" {
			// An empty string converts to 0.
			score, err = 0, nil
		}
		if err != nil {
			return result, invalidOutput("Prompt Guard returned no score.")
		}
		result.CategoryScores.set("prompt_injection", score)
		if score >= 0.5 {
			flagCategory(&result, "prompt_injection", score)
		}
		return result, nil
	case "llama_guard", "shieldgemma":
		// "safe" or "unsafe\nS9,S2" (ShieldGemma: "Yes"/"No").
		if guardUnsafePattern.MatchString(raw) {
			codes := hazardCodePattern.FindAllString(raw, -1)
			if len(codes) == 0 {
				flagCategory(&result, "illicit", 1)
			}
			for _, code := range codes {
				category, known := hazardCategories[code]
				if !known {
					category = "illicit"
				}
				flagCategory(&result, category, 1)
			}
			return result, nil
		}
		if !guardSafePattern.MatchString(raw) {
			return result, invalidOutput("The guard model returned an unexpected verdict.")
		}
		return result, nil
	case "nemoguard", "topic_control":
		verdict, categories := nemoguardVerdict(raw)
		if verdict == "" {
			return result, invalidOutput("The guard model returned an unexpected verdict.")
		}
		if strings.Contains(strings.ToLower(verdict), "unsafe") || strings.Contains(strings.ToLower(verdict), "off-topic") {
			names := splitCategories(categories)
			if len(names) == 0 {
				flagCategory(&result, "illicit", 1)
			}
			for _, name := range names {
				flagCategory(&result, categoryFromText(name), 1)
			}
		}
		return result, nil
	}
	// gpt-oss-safeguard and any instruction model: the JSON contract above.
	answer := jsonObjectIn(raw)
	if answer == nil || (answer["violation"] == nil && answer["flagged"] == nil) {
		return result, invalidOutput("The classifier did not answer in the expected JSON format.")
	}
	violation := answer["violation"]
	if violation == nil {
		if truthy(answer["flagged"]) {
			violation = 1.0
		} else {
			violation = 0.0
		}
	}
	if number, ok := violation.(float64); ok && number == 1 {
		category, _ := answer["category"].(string)
		flagCategory(&result, categoryFromText(category), 1)
	}
	if rationale, ok := answer["rationale"].(string); ok && rationale != "" {
		result.Rationale = &rationale
	}
	return result, nil
}

// The patterns a classifier's answer is read with.
var (
	hazardCodePattern       = regexp.MustCompile(`S\d{1,2}`)
	jsonObjectPattern       = regexp.MustCompile(`(?s)\{.*\}`)
	userSafetyPattern       = regexp.MustCompile(`(?i)user safety:\s*(\w+)`)
	safetyCategoriesPattern = regexp.MustCompile(`(?i)safety categories:\s*(.+)`)
	offTopicPattern         = regexp.MustCompile(`(?i)off-topic|unsafe`)
	onTopicPattern          = regexp.MustCompile(`(?i)on-topic|safe`)
	guardUnsafePattern      = regexp.MustCompile(`^(?i:unsafe|yes)\b`)
	guardSafePattern        = regexp.MustCompile(`^(?i:safe|no)\b`)
)

// firstString is the first non-empty string of the values, "" when none is.
func firstString(values ...any) string {
	for _, value := range values {
		if text, ok := value.(string); ok && text != "" {
			return text
		}
	}
	return ""
}

// submatch is the first capture group of pattern, "" when it does not match.
func submatch(pattern *regexp.Regexp, value string) string {
	match := pattern.FindStringSubmatch(value)
	if len(match) < 2 {
		return ""
	}
	return match[1]
}

// splitCategories splits "Violence, Hate/Identity Hate" into its names.
func splitCategories(text string) []string {
	names := []string{}
	for _, name := range strings.Split(text, ",") {
		if trimmed := strings.TrimSpace(name); trimmed != "" {
			names = append(names, trimmed)
		}
	}
	return names
}

// nemoguardVerdict reads NemoGuard's answer, JSON or "User Safety: unsafe".
func nemoguardVerdict(raw string) (string, string) {
	verdict, categories := "", ""
	if answer := jsonObjectIn(raw); answer != nil {
		verdict = firstString(answer["User Safety"], answer["user_safety"], answer["Response Safety"])
		categories = firstString(answer["Safety Categories"], answer["categories"])
		if verdict != "" {
			return verdict, categories
		}
	}
	verdict = submatch(userSafetyPattern, raw)
	if verdict == "" {
		switch {
		case offTopicPattern.MatchString(raw):
			verdict = "unsafe"
		case onTopicPattern.MatchString(raw):
			verdict = "safe"
		}
	}
	categories = submatch(safetyCategoriesPattern, raw)
	return verdict, categories
}

// jsonObjectIn reads the first JSON object in a text, nil when there is none.
func jsonObjectIn(raw string) map[string]any {
	match := jsonObjectPattern.FindString(raw)
	if match == "" {
		return nil
	}
	var value map[string]any
	if err := json.Unmarshal([]byte(match), &value); err != nil {
		return nil
	}
	return value
}

// fromNative reads the records of a provider's own moderation endpoint.
func fromNative(records []any) []moderationResult {
	results := make([]moderationResult, 0, len(records))
	for _, record := range records {
		result := newModerationResult()
		item, _ := record.(map[string]any)
		categories, _ := item["categories"].(map[string]any)
		scores, _ := item["category_scores"].(map[string]any)
		for name, value := range categories {
			score := 0.0
			if raw, ok := scores[name].(float64); ok {
				score = raw
			} else if truthy(value) {
				score = 1
			}
			category := name
			if !isModerationCategory(category) {
				category = categoryFromText(category)
			}
			result.CategoryScores.raise(category, score)
			if truthy(value) {
				flagCategory(&result, category, score)
			}
		}
		if truthy(item["flagged"]) {
			result.Flagged = true
		}
		results = append(results, result)
	}
	return results
}
