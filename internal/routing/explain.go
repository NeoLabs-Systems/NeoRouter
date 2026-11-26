package routing

import (
	"fmt"
	"sort"
	"strings"
)

// Why no model could take a request, in words: groups of rejection reasons with
// their counts, largest first.
type rejectionGroup struct {
	reason  string
	label   string
	reasons []string
}

var rejectionGroups = []rejectionGroup{
	{"tokens", "are past this key's token limit until it resets", []string{"token_limit"}},
	{"credits", "need credits, and their connection is set to free", []string{"needs_credits"}},
	{"action", "need something only you can do at the provider (accepting terms, a consent, a verification or a plan)", []string{"needs_action"}},
	{"payment", "were refused by the provider for payment or plan reasons", []string{"payment_required", "no_free_quota"}},
	{"auth", "have a key the provider rejects", []string{"auth_failed"}},
	{"missing", "are listed but not served by the provider right now", []string{"not_available"}},
	{"busy", "are used up, rate-limited or cooling down for now", []string{"slot_overflow", "scheduled_downtime", "account_failing", "account_resting", "quota_exhausted", "quota_rpd", "quota_rpm", "quota_tpm", "quota_tpd", "rate_limited", "circuit_open", "account_busy", "held_for_priority", "plan_used_up", "kept_for_owner"}},
	{"fit", "cannot handle this request (tools, images or its size)", []string{"no_tools", "no_vision", "context_too_small", "context_unknown", "needs_more_tokens", "incompatible"}},
	{"unsupported", "cannot be used for this through NeoRouter yet (the provider speaks a different API for it)", []string{"unsupported"}},
	{"profile", "are left out by the routing style", []string{"outside_routing_group", "below_min_intelligence", "too_expensive", "too_slow", "provider_not_included", "provider_excluded", "model_excluded", "kind_not_in_style"}},
	{"budget", "have no known price or would go past this key's budget or price limit", []string{"price_unknown", "budget_exhausted", "key_too_expensive"}},
	{"key", "are not allowed for this key", []string{"not_free", "not_allowed_for_key", "other_kind", "not_in_pool", "not_shared", "peer_no_forward"}},
	{"metered", "could incur per-use charges, which this routing group does not allow", []string{"metered_not_allowed"}},
	{"paced", "are being spread out so their limits last until they reset (Settings → Usage → even out usage); keys with a high priority still get through", []string{"paced"}},
	{"private", "are pool partners, and this request holds very sensitive data that stays on your own connections", []string{"kept_private"}},
}

const switchedOffLabel = "are switched off or unavailable"

func rejectionLabel(reason string) string {
	for _, group := range rejectionGroups {
		for _, candidate := range group.reasons {
			if candidate == reason {
				return group.label
			}
		}
	}
	// Any quota metric that is used up belongs with the busy group.
	if strings.HasPrefix(reason, "quota") {
		for _, group := range rejectionGroups {
			if group.reason == "busy" {
				return group.label
			}
		}
	}
	return switchedOffLabel
}

// ExplainNoModel is explainNoModel: why no model could take a request, as a
// sentence with the rejection groups and their counts, largest first.
func ExplainNoModel(rejected map[string]int, capability string) string {
	// Counts are accumulated per label; labels keep the order of
	// rejectionGroups so the sentence is deterministic.
	counts := map[string]int{}
	for reason, count := range rejected {
		counts[rejectionLabel(reason)] += count
	}
	var order []string
	for _, group := range rejectionGroups {
		if _, ok := counts[group.label]; ok {
			order = append(order, group.label)
		}
	}
	if _, ok := counts[switchedOffLabel]; ok {
		order = append(order, switchedOffLabel)
	}
	kind := ""
	if capability != "chat" && capability != "" {
		kind = capability + " "
	}
	if len(order) == 0 {
		return fmt.Sprintf("There is no %smodel in your catalog yet.", kind)
	}
	sort.SliceStable(order, func(i, j int) bool { return counts[order[i]] > counts[order[j]] })
	parts := make([]string, 0, len(order))
	for _, label := range order {
		parts = append(parts, fmt.Sprintf("%d %s", counts[label], label))
	}
	return fmt.Sprintf("No %smodel can take this request right now: %s.", kind, strings.Join(parts, "; "))
}

// closestModels finds model names close to one that does not exist: a shortened
// name ("spark/qwen3.8"), another provider prefix or a small typo. Up to three.
func closestModels(requested string, entries []*Entry) []string {
	wanted := strings.ToLower(requested)
	tail := wanted
	if index := strings.LastIndex(wanted, "/"); index >= 0 {
		tail = wanted[index+1:]
	}
	type scoredModel struct {
		id     string
		score  int
		length int
	}
	var scored []scoredModel
	for _, entry := range entries {
		id := strings.ToLower(entry.ID)
		model := strings.ToLower(entry.ModelID)
		score := -1
		switch {
		case tail != "" && (strings.HasPrefix(model, tail) || strings.HasPrefix(id, wanted)):
			score = 0
		case utf16Len(tail) >= 4 && strings.Contains(model, tail):
			score = 1
		default:
			distance := editDistance(tail, model)
			if distance <= maxInt64Int(2, int64(utf16Len(tail)/5)) {
				score = 1 + int(distance)
			}
		}
		if score >= 0 {
			scored = append(scored, scoredModel{id: entry.ID, score: score, length: len(entry.ID)})
		}
	}
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score < scored[j].score
		}
		return scored[i].length < scored[j].length
	})
	seen := map[string]bool{}
	out := make([]string, 0, 3)
	for _, item := range scored {
		if seen[item.id] {
			continue
		}
		seen[item.id] = true
		out = append(out, item.id)
		if len(out) == 3 {
			break
		}
	}
	return out
}

func maxInt64Int(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// editDistance is the Levenshtein distance, with a shortcut for lengths that
// are far apart.
func editDistance(a, b string) int64 {
	ar, br := []rune(a), []rune(b)
	if diff := len(ar) - len(br); diff > 6 || diff < -6 {
		return mathInf
	}
	previous := make([]int64, len(br)+1)
	for index := range previous {
		previous[index] = int64(index)
	}
	for i := 1; i <= len(ar); i++ {
		current := make([]int64, len(br)+1)
		current[0] = int64(i)
		for j := 1; j <= len(br); j++ {
			substitution := previous[j-1]
			if ar[i-1] != br[j-1] {
				substitution++
			}
			current[j] = minInt64(minInt64(previous[j]+1, current[j-1]+1), substitution)
		}
		previous = current
	}
	return previous[len(br)]
}

// mathInf stands in for infinity.
const mathInf = int64(1) << 62
