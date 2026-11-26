package catalog

import (
	"math"
	"sort"
	"strings"
)

// Heuristic weights when no benchmark index is available. Each signal is
// normalized to [0, 1]; missing signals drop out and lower the confidence.
// benchmarkKeys reduces a benchmark or provider model name to matchable keys:
// both sides lose dates, reasoning-effort levels and serving suffixes. One key
// keeps the parameter sizes, a looser one drops them.
func benchmarkKeys(text string) (withParams, withoutParams string) {
	cleaned := strings.ToLower(text)
	cleaned = nonReasoning.ReplaceAllString(cleaned, "nonreasoning")
	cleaned = parenthesised.ReplaceAllString(cleaned, " ")
	cleaned = tierSuffix.ReplaceAllString(cleaned, "")
	parts := tokenSplit.Split(cleaned, -1)
	tokens := make([]string, 0, len(parts))
	loose := make([]string, 0, len(parts))
	for _, token := range parts {
		if token == "" || effortTokens[token] || servingTokens[token] || isDigits(token, -1) && len(token) >= 4 && len(token) <= 8 {
			continue
		}
		tokens = append(tokens, token)
		if !paramToken.MatchString(token) {
			loose = append(loose, token)
		}
	}
	return strings.Join(tokens, "-"), strings.Join(loose, "-")
}

// buildBenchmarkKeys indexes the benchmark records by name.
func buildBenchmarkKeys(records []*Fact) *benchmarkKeyIndex {
	exact := map[string]*Fact{}
	loose := map[string]*Fact{}
	// Records without an effort suffix are the provider default: index them first.
	ordered := make([]*Fact, 0, len(records))
	for _, record := range records {
		if record != nil {
			ordered = append(ordered, record)
		}
	}
	sort.SliceStable(ordered, func(a, b int) bool {
		left, right := 0, 0
		if effortSuffix.MatchString(ordered[a].Slug) {
			left = 1
		}
		if effortSuffix.MatchString(ordered[b].Slug) {
			right = 1
		}
		return left < right
	})
	for _, record := range ordered {
		if !truthyFloat(record.IntelligenceIndex) {
			continue
		}
		seen := map[string]bool{}
		for _, text := range []string{record.Slug, record.Name} {
			withParams, withoutParams := benchmarkKeys(text)
			if withParams != "" {
				if _, taken := exact[withParams]; !taken {
					exact[withParams] = record
				}
			}
			if withoutParams == "" || seen[withoutParams] {
				continue
			}
			seen[withoutParams] = true
			// Two different models collapsing onto one size-less key make it ambiguous.
			existing, taken := loose[withoutParams]
			switch {
			case !taken:
				loose[withoutParams] = record
			case existing != nil && !sameFloat(existing.IntelligenceIndex, record.IntelligenceIndex) &&
				effortSuffix.ReplaceAllString(existing.Slug, "") != effortSuffix.ReplaceAllString(record.Slug, ""):
				loose[withoutParams] = nil
			}
		}
	}
	return &benchmarkKeyIndex{exact: exact, loose: loose}
}

// paramSizes returns the parameter-size tokens of a name.
func paramSizes(text string) []string {
	parts := tokenSplit.Split(strings.ToLower(text), -1)
	sizes := make([]string, 0, len(parts))
	for _, part := range parts {
		if paramToken.MatchString(part) {
			sizes = append(sizes, part)
		}
	}
	return sizes
}

// sameSize keeps a size-less match from crossing sizes: a 405b model is not
// its 70b sibling. When either side names no size, the name is all there is to
// go on.
func sameSize(modelText string, record *Fact) bool {
	asked := paramSizes(modelText)
	known := []string{}
	if record != nil {
		known = append(known, paramSizes(record.Slug)...)
		known = append(known, paramSizes(record.Name)...)
	}
	if len(asked) == 0 || len(known) == 0 {
		return true
	}
	for _, size := range asked {
		if containsString(known, size) {
			return true
		}
	}
	return false
}

// indexSkillGap is how far the benchmark index and the skills may disagree
// before neither is taken at its word.
const indexSkillGap = 0.15

// aaShare is how much of the overall score the fuller index keeps when
// OpenRouter's skills are blended in.
const aaShare = 0.6

// reconcileWithSkills keeps the index and the model's benchmark scores telling
// one story.
func reconcileWithSkills(index float64, skills map[string]float64) float64 {
	if len(skills) == 0 {
		return index
	}
	sum := 0.0
	for _, value := range skills {
		sum += value
	}
	mean := sum / float64(len(skills))
	if math.Abs(mean-index) > indexSkillGap {
		return (index + mean) / 2
	}
	return index
}
