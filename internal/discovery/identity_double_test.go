package discovery

import (
	"regexp"
	"strings"
)

// This file is the test double for the catalog domain's canonical-id rules, so
// the tests aggregate the way the real metadata service does. The production
// identity comes from internal/catalog through the Catalog interface; nothing
// here is part of the package's API.

var (
	identityPrefixes = []*regexp.Regexp{
		regexp.MustCompile(`^@cf/`), regexp.MustCompile(`^@hf/`), regexp.MustCompile(`^models/`),
		regexp.MustCompile(`^accounts/[^/]+/models/`), regexp.MustCompile(`^openrouter/`),
	}
	identityChannelSuffix = regexp.MustCompile(`:(free|beta|extended|thinking|nitro|floor|exacto)$`)
	identityLooseColon    = regexp.MustCompile(`:`)
	identitySeparators    = regexp.MustCompile(`[-_\s]+`)
	identityNonAlnum      = regexp.MustCompile(`[^a-z0-9]`)
	identityFourDigit     = regexp.MustCompile(`^\d{4}$`)
	identityTwoDigit      = regexp.MustCompile(`^\d{2}$`)
	identityDateLike      = regexp.MustCompile(`^\d{4,8}$`)
)

var identitySuffixTokens = map[string]bool{
	"instruct": true, "chat": true, "it": true, "versatile": true, "instant": true,
	"fp8": true, "fp16": true, "bf16": true, "awq": true, "gptq": true, "int4": true,
	"int8": true, "hf": true, "free": true, "latest": true, "online": true,
	"beta": true, "api": true, "tput": true, "serverless": true, "fast": true,
	"preview": true, "exp": true,
}

var identityVendorPrefixes = map[string]bool{
	"meta": true, "mistralai": true, "google": true, "qwen": true, "deepseek-ai": true,
	"microsoft": true, "nvidia": true, "openai": true, "anthropic": true,
}

// canonicalID mirrors the catalog's canonical id.
func canonicalID(modelID string) string {
	id := strings.ToLower(strings.TrimSpace(modelID))
	for _, pattern := range identityPrefixes {
		id = pattern.ReplaceAllString(id, "")
	}
	id = identityChannelSuffix.ReplaceAllString(id, "")
	if at := strings.LastIndexByte(id, '/'); at >= 0 {
		id = id[at+1:]
	}
	id = identityLooseColon.ReplaceAllString(id, "-")
	var tokens []string
	for _, token := range identitySeparators.Split(id, -1) {
		if token != "" {
			tokens = append(tokens, token)
		}
	}
	for len(tokens) > 1 && identityVendorPrefixes[tokens[0]] {
		tokens = tokens[1:]
	}
	for len(tokens) > 1 {
		last := tokens[len(tokens)-1]
		switch {
		case len(tokens) > 3 && identityFourDigit.MatchString(tokens[len(tokens)-3]) &&
			identityTwoDigit.MatchString(tokens[len(tokens)-2]) && identityTwoDigit.MatchString(last):
			tokens = tokens[:len(tokens)-3]
		case len(tokens) > 2 && identityTwoDigit.MatchString(tokens[len(tokens)-2]) && identityTwoDigit.MatchString(last):
			tokens = tokens[:len(tokens)-2]
		case identitySuffixTokens[last] || identityDateLike.MatchString(last):
			tokens = tokens[:len(tokens)-1]
		default:
			goto done
		}
	}
done:
	joined := identityNonAlnum.ReplaceAllString(strings.Join(tokens, ""), "")
	if joined == "" {
		return strings.ToLower(modelID)
	}
	return joined
}
