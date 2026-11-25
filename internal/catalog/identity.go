// Package catalog owns the model catalog: the rows reconciled from each
// provider's live model list, the strict free/paid classification, the
// canonical model identity, the shared metadata (benchmark and price)
// indexes, and the /api/v1/models and /api/v1/model-presets routes.
//
// It never imports another domain. Everything it needs from providers,
// settings or routing is declared here as a narrow consumer interface and is
// satisfied in internal/app.
package catalog

import (
	"regexp"
	"strconv"
	"strings"
)

// canonicalID maps provider-specific model ids onto one canonical id so
// metadata and learned behaviour can be shared, e.g. Groq
// "llama-3.3-70b-versatile" and OpenRouter
// "meta-llama/llama-3.3-70b-instruct:free" both become "llama3370b".
//
// Port of server/services/catalog/model_identity.js canonicalId.
func canonicalID(modelID string) string {
	id := strings.ToLower(strings.TrimSpace(modelID))
	for _, pattern := range canonicalPrefixes {
		id = pattern.ReplaceAllString(id, "")
	}
	id = canonicalTierSuffix.ReplaceAllString(id, "")
	if at := strings.LastIndexByte(id, '/'); at >= 0 {
		id = id[at+1:]
	}
	// Ollama-style tags: "llama3.3:70b" -> "llama3.3-70b".
	id = strings.ReplaceAll(id, ":", "-")
	tokens := splitTokens(id)
	for len(tokens) > 1 && vendorPrefixes[tokens[0]] {
		tokens = tokens[1:]
	}
	for len(tokens) > 1 {
		last := tokens[len(tokens)-1]
		// Date suffixes like 2024-08-06 split into three numeric tokens.
		if len(tokens) > 3 && isDigits(tokens[len(tokens)-3], 4) && isDigits(tokens[len(tokens)-2], 2) && isDigits(last, 2) {
			tokens = tokens[:len(tokens)-3]
			continue
		}
		// Two-part dates such as "preview-05-20".
		if len(tokens) > 2 && isDigits(tokens[len(tokens)-2], 2) && isDigits(last, 2) {
			tokens = tokens[:len(tokens)-2]
			continue
		}
		// Compact dates (20241022, 2507, 0613) and context markers (32768).
		if suffixTokens[last] || isDigits(last, -1) && len(last) >= 4 && len(last) <= 8 {
			tokens = tokens[:len(tokens)-1]
			continue
		}
		break
	}
	joined := nonAlnum.ReplaceAllString(strings.Join(tokens, ""), "")
	if joined != "" {
		return joined
	}
	return strings.ToLower(modelID)
}

var (
	canonicalPrefixes = []*regexp.Regexp{
		regexp.MustCompile(`^@cf/`),
		regexp.MustCompile(`^@hf/`),
		regexp.MustCompile(`^models/`),
		regexp.MustCompile(`^accounts/[^/]+/models/`),
		regexp.MustCompile(`^openrouter/`),
	}
	canonicalTierSuffix = regexp.MustCompile(`:(free|beta|extended|thinking|nitro|floor|exacto)$`)
	tokenSplit          = regexp.MustCompile(`[-_\s]+`)
	nonAlnum            = regexp.MustCompile(`[^a-z0-9]+`)
	paramCountPattern   = regexp.MustCompile(`(\d+(?:\.\d+)?)b`)
	moeParamPattern     = regexp.MustCompile(`(\d+)x(\d+(?:\.\d+)?)b`)
)

var suffixTokens = map[string]bool{
	"instruct": true, "chat": true, "it": true, "versatile": true, "instant": true,
	"fp8": true, "fp16": true, "bf16": true, "awq": true, "gptq": true, "int4": true,
	"int8": true, "hf": true, "free": true, "latest": true, "online": true, "beta": true,
	"api": true, "tput": true, "serverless": true, "fast": true, "preview": true, "exp": true,
}

var vendorPrefixes = map[string]bool{
	"meta": true, "mistralai": true, "google": true, "qwen": true, "deepseek-ai": true,
	"microsoft": true, "nvidia": true, "openai": true, "anthropic": true,
}

func splitTokens(value string) []string {
	parts := tokenSplit.Split(value, -1)
	tokens := parts[:0]
	for _, part := range parts {
		if part != "" {
			tokens = append(tokens, part)
		}
	}
	return tokens
}

// isDigits reports whether value is made of digits only; with a length above
// zero it must be exactly that long.
func isDigits(value string, length int) bool {
	if value == "" || (length > 0 && len(value) != length) {
		return false
	}
	for i := 0; i < len(value); i++ {
		if value[i] < '0' || value[i] > '9' {
			return false
		}
	}
	return true
}

func isLowerLetter(c byte) bool { return c >= 'a' && c <= 'z' }

// parameterCount is the parameter count in billions parsed from the id
// ("70b", "8x7b", "a3b" active). Null when the id names no size.
//
// Port of model_identity.js parameterCount, including its lookarounds: a size
// only counts when no letter precedes the number and no letter follows the
// "b".
func parameterCount(modelID string) *float64 {
	id := strings.ToLower(modelID)
	for _, loc := range moeParamPattern.FindAllStringSubmatchIndex(id, -1) {
		end := loc[1]
		if end < len(id) && isWordByte(id[end]) {
			continue
		}
		total, err1 := strconv.ParseFloat(id[loc[2]:loc[3]], 64)
		per, err2 := strconv.ParseFloat(id[loc[4]:loc[5]], 64)
		if err1 != nil || err2 != nil {
			continue
		}
		value := total * per
		return &value
	}
	var best float64
	found := false
	for _, loc := range paramCountPattern.FindAllStringSubmatchIndex(id, -1) {
		start, end := loc[0], loc[1]
		if start > 0 && isLowerLetter(id[start-1]) {
			continue
		}
		if end < len(id) && isLowerLetter(id[end]) {
			continue
		}
		value, err := strconv.ParseFloat(id[loc[2]:loc[3]], 64)
		if err != nil {
			continue
		}
		if !found || value > best {
			best = value
		}
		found = true
	}
	if !found {
		return nil
	}
	return &best
}

func isWordByte(c byte) bool {
	return isLowerLetter(c) || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_'
}
