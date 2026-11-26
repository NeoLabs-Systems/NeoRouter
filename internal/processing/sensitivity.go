package processing

import (
	"regexp"
	"strings"
)

// How sensitive a request is, judged locally and without any model call:
//
//	high  very sensitive; it stays on the owner's own connections and never goes
//	      to a pool partner (services/pools), when the owner wants that
//	low   ordinary personal data (an email address, a name), which the privacy
//	      shield already hides from partners
//	none  nothing found
//
// Built on the privacy shield's detectors, so the same checksums and structure
// tests that keep its false alarms low apply here. "high" is deliberately strict:
// only what is unmistakably secret, or bulk personal data such as a customer
// list. Topics (health, legal) are not judged by keywords: that would flag far
// too many harmless requests.

// wordTriggered are shield rules that find a value next to a word such as
// "password". The shield hides those values generously; to count as very
// sensitive, the value must also look like a real credential.
var wordTriggered = map[string]bool{
	"password_assignment":  true,
	"password_in_sentence": true,
	"env_secret":           true,
}

// notHigh are the rules that never make a request very sensitive: one-time codes
// expire within minutes, so they are worth hiding but not worth keeping home.
var notHigh = map[string]bool{"one_time_code": true}

// codeReference matches a variable or placeholder in code ("process.env.PASSWORD",
// "${TOKEN}", "DB_PASS").
var codeReference = regexp.MustCompile(`^(?:[A-Za-z_$][\w$]*(?:\.[A-Za-z_$][\w$]*)+|\$\{?[\w.]+\}?|\{\{?[\w.\s]+\}?\}|[A-Z][A-Z0-9_]+|<[^>]+>)$`)

// CredentialLike reports whether a value looks like a real credential: long
// enough, and mixing letters with digits, symbols or upper and lower case.
func CredentialLike(value string) bool {
	if len([]rune(value)) < 6 || codeReference.MatchString(value) {
		return false
	}
	classes := 0
	for _, pattern := range []*regexp.Regexp{
		regexp.MustCompile(`[a-z]`), regexp.MustCompile(`[A-Z]`), regexp.MustCompile(`\d`), regexp.MustCompile(`[^A-Za-z0-9]`),
	} {
		if pattern.MatchString(value) {
			classes++
		}
	}
	return classes >= 2
}

func strongSecret(item Span) bool {
	if item.Category != "secret" || notHigh[item.RuleID] || item.RuleID == "seed_phrase" {
		return false
	}
	if wordTriggered[item.RuleID] {
		return CredentialLike(item.Value)
	}
	return true
}

// highRule is one reason a request is very sensitive. Rules are independent; a
// new one is one more entry.
type highRule struct {
	reason string
	test   func(found []Span) bool
}

var highRules = []highRule{
	// API keys, real passwords, tokens, private key blocks, connection strings.
	{"secret", func(found []Span) bool {
		for _, item := range found {
			if strongSecret(item) {
				return true
			}
		}
		return false
	}},
	// Card numbers that pass Luhn and match an issuer.
	{"card", func(found []Span) bool { return anyCategory(found, "card") }},
	// A wallet seed phrase (not a wallet address, which is public by design).
	{"seed_phrase", func(found []Span) bool {
		for _, item := range found {
			if item.RuleID == "seed_phrase" {
				return true
			}
		}
		return false
	}},
	// Government and tax identifiers with valid check digits.
	{"government_id", func(found []Span) bool { return anyCategory(found, "id") }},
	// Bulk personal data: many people's details in one request.
	{"personal_records", func(found []Span) bool {
		return distinct(found, "birthdate") >= 3 || distinct(found, "address") >= 5 ||
			distinct(found, "email")+distinct(found, "phone") >= 15
	}},
}

func anyCategory(found []Span, category string) bool {
	for _, item := range found {
		if item.Category == category {
			return true
		}
	}
	return false
}

// distinct counts the different values of one category.
func distinct(found []Span, category string) int {
	seen := map[string]struct{}{}
	for _, item := range found {
		if item.Category == category {
			seen[strings.ToLower(item.Value)] = struct{}{}
		}
	}
	return len(seen)
}

var lowCategories = map[string]bool{
	"email": true, "phone": true, "person": true, "address": true, "birthdate": true,
	"iban": true, "ip": true, "location": true, "vehicle": true, "crypto": true,
}

// maxScannedChars bounds the work one request may cause.
const maxScannedChars = 200_000

// allModes turns every category on, so the scan sees everything the shield can
// find.
func allModes() map[string]string {
	modes := make(map[string]string, len(Categories))
	for _, category := range Categories {
		modes[category] = "hide"
	}
	return modes
}

// assessmentTexts are the texts of a request, the way the shield sees them.
func assessmentTexts(canonical Canonical) []string {
	texts := []string{}
	add := func(value any) {
		if text, ok := value.(string); ok && text != "" {
			texts = append(texts, text)
		}
	}
	for _, message := range canonical.Messages() {
		switch content := message["content"].(type) {
		case string:
			add(content)
		case []any:
			for _, raw := range content {
				if text, ok := raw.(string); ok {
					add(text)
					continue
				}
				if part, ok := raw.(map[string]any); ok {
					add(part["text"])
				}
			}
		}
		if calls, ok := message["tool_calls"].([]any); ok {
			for _, raw := range calls {
				call, ok := raw.(map[string]any)
				if !ok {
					continue
				}
				if function, ok := call["function"].(map[string]any); ok {
					add(function["arguments"])
				}
			}
		}
	}
	return texts
}

// Assessment is how sensitive a request is: level is "none", "low" or "high", and
// reasons name what made it high (never the values themselves).
type Assessment struct {
	Level   string   `json:"level"`
	Reasons []string `json:"reasons"`
}

// Assess judges one canonical request.
func Assess(engine *Engine, canonical Canonical) Assessment {
	budget := maxScannedChars
	found := []Span{}
	for _, text := range assessmentTexts(canonical) {
		if budget <= 0 {
			break
		}
		runes := []rune(text)
		slice := text
		if len(runes) > budget {
			slice = string(runes[:budget])
			budget = 0
		} else {
			budget -= len(runes)
		}
		found = append(found, engine.Scan(slice, ScanOptions{Modes: allModes()})...)
	}
	reasons := []string{}
	for _, rule := range highRules {
		if rule.test(found) {
			reasons = append(reasons, rule.reason)
		}
	}
	if len(reasons) > 0 {
		return Assessment{Level: "high", Reasons: reasons}
	}
	for _, item := range found {
		if lowCategories[item.Category] {
			return Assessment{Level: "low", Reasons: []string{}}
		}
	}
	return Assessment{Level: "none", Reasons: []string{}}
}
