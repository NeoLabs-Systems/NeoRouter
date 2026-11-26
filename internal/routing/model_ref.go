package routing

import (
	"encoding/json"
	"regexp"
	"strings"
	"sync"
	"time"
)

// How a request names a model: an explicit "provider/model", a pool group
// ("pool:name"), or one of the autos ("auto", "auto:fast", "auto-free",
// "auto-plan:best").

// GlobToRegExp turns a model pattern ("groq/*") into a case-insensitive,
// anchored regular expression.
func GlobToRegExp(pattern string) *regexp.Regexp {
	value := strings.TrimSpace(pattern)
	var b strings.Builder
	for _, r := range value {
		switch r {
		case '.', '+', '^', '$', '{', '}', '(', ')', '|', '[', ']', '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case '*':
			b.WriteString(".*")
		case '?':
			b.WriteByte('.')
		default:
			b.WriteRune(r)
		}
	}
	re, err := regexp.Compile("(?i)^" + b.String() + "$")
	if err != nil {
		return regexp.MustCompile(`^\Q` + value + `\E$`)
	}
	return re
}

// isRoutePattern reports whether an allowed-list entry bounds which autos a key
// may ask for (as opposed to which models).
func isRoutePattern(pattern string) bool {
	return regexp.MustCompile(`(?i)^(?:auto(?:$|[-:*?])|pool:)`).MatchString(strings.TrimSpace(pattern))
}

var kindToken = regexp.MustCompile(`(?i)^kind:([a-z_]+)$`)

// keyPatterns splits a key's allowed list into kinds, model patterns and
// routing entries.
func keyPatterns(key RouterKey) (kinds, models, routes []string) {
	var patterns []string
	_ = json.Unmarshal([]byte(key.AllowedModelsJSON), &patterns)
	trimmed := make([]string, 0, len(patterns))
	for _, pattern := range patterns {
		trimmed = append(trimmed, strings.TrimSpace(pattern))
	}
	var rest []string
	for _, pattern := range trimmed {
		if match := kindToken.FindStringSubmatch(pattern); match != nil {
			kinds = append(kinds, strings.ToLower(match[1]))
			continue
		}
		rest = append(rest, pattern)
	}
	for _, pattern := range rest {
		if isRoutePattern(pattern) {
			routes = append(routes, pattern)
		} else {
			models = append(models, pattern)
		}
	}
	return kinds, models, routes
}

// coverCache remembers whether a key's model patterns name any model of a kind
// in the user's catalog (it is asked once per candidate).
type coverCache struct {
	mu    sync.Mutex
	items map[string]cacheEntry[bool]
}

func (s *Service) patternsCoverKind(key RouterKey, capability string, models []string, now time.Time) bool {
	id := key.UserID + "|" + capability + "|" + strings.Join(models, "\n")
	s.covers.mu.Lock()
	if hit, ok := s.covers.items[id]; ok {
		if now.UnixMilli()-hit.at < 10_000 {
			s.covers.mu.Unlock()
			return hit.value
		}
	}
	s.covers.mu.Unlock()
	regexes := make([]*regexp.Regexp, 0, len(models))
	for _, model := range models {
		regexes = append(regexes, GlobToRegExp(model))
	}
	value := false
	if s.opts.Catalog != nil {
		for _, entry := range s.opts.Catalog.ListForUser(key.UserID, CatalogQuery{Capability: capability, IncludeUnavailable: true}) {
			for _, re := range regexes {
				if re.MatchString(entry.ID) {
					value = true
					break
				}
			}
			if value {
				break
			}
		}
	}
	s.covers.mu.Lock()
	if len(s.covers.items) > 500 {
		s.covers.items = map[string]cacheEntry[bool]{}
	}
	s.covers.items[id] = cacheEntry[bool]{at: now.UnixMilli(), value: value}
	s.covers.mu.Unlock()
	return value
}

// AllowedByKey reports whether a key's model patterns admit a model. With kinds
// listed, the key takes only those kinds, and model patterns narrow just the
// kinds they name a model of ("these chat models, and any speech model").
// Without kinds, model patterns limit everything, as before.
func (s *Service) AllowedByKey(entry *Entry, key RouterKey, now time.Time) bool {
	kinds, models, _ := keyPatterns(key)
	capability := entry.Capability
	if capability == "" {
		capability = capabilityChat
	}
	if len(kinds) > 0 {
		found := false
		for _, kind := range kinds {
			if kind == capability {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	if len(models) == 0 {
		return true
	}
	if len(kinds) > 0 && !s.patternsCoverKind(key, capability, models, now) {
		return true
	}
	for _, pattern := range models {
		if GlobToRegExp(pattern).MatchString(entry.ID) {
			return true
		}
	}
	return false
}

// RouteAllowedByKey reports whether the key may ask for this model name. With
// routing entries listed, an auto must match one of them (plain "auto" also
// matches as "auto:<key profile>"), and a model named directly needs model
// patterns too.
func (s *Service) RouteAllowedByKey(requested string, key RouterKey) bool {
	_, models, routes := keyPatterns(key)
	if len(routes) == 0 {
		return true
	}
	parsed := ParseModel(requested)
	if parsed.Group != "" {
		return anyGlobMatches(routes, "pool:"+parsed.Group)
	}
	if !parsed.Auto {
		return len(models) > 0
	}
	base := "auto"
	if parsed.Kind != "" {
		base = "auto-" + parsed.Kind
	}
	named := parsed.Profile
	if named == "default" {
		named = ""
	}
	var names []string
	if named != "" {
		names = append(names, base+":"+s.profileSlug(key.UserID, named))
	} else {
		names = append(names, base)
		profile := key.Profile
		if profile == "" {
			profile = "balanced"
		}
		names = append(names, base+":"+s.profileSlug(key.UserID, profile))
	}
	for _, name := range names {
		if anyGlobMatches(routes, name) {
			return true
		}
	}
	return false
}

// profileSlug resolves a style reference to its slug, keeping the reference
// when it does not resolve.
func (s *Service) profileSlug(userID, reference string) string {
	if profile := s.ResolveProfile(userID, reference); profile != nil {
		return profile.Slug
	}
	return reference
}

func anyGlobMatches(patterns []string, value string) bool {
	for _, pattern := range patterns {
		if GlobToRegExp(pattern).MatchString(value) {
			return true
		}
	}
	return false
}

// ParsedModel is how a request names its model.
type ParsedModel struct {
	Auto    bool
	Profile string
	Kind    string
	Group   string
	Model   string
}

var autoPattern = regexp.MustCompile(`(?i)^auto(?:-(free|plan|paid))?(?::(.+))?$`)

// ParseModel reads "auto", "auto:<profile>", "auto-free", "auto-plan:fast",
// "auto-paid:best" and "pool:<group>".
func ParseModel(requested string) ParsedModel {
	value := strings.TrimSpace(requested)
	if value == "" {
		return ParsedModel{Auto: true}
	}
	if strings.HasPrefix(value, "pool:") && len(value) > 5 {
		return ParsedModel{Auto: true, Group: value[5:]}
	}
	if match := autoPattern.FindStringSubmatch(value); match != nil {
		return ParsedModel{Auto: true, Kind: strings.ToLower(match[1]), Profile: match[2]}
	}
	return ParsedModel{Model: value}
}

// IsAutoModel reports whether a model name is an auto variant.
func IsAutoModel(value string) bool { return ParseModel(value).Auto }

// needsCreditsReasons are the free-classifier reasons that mean the model is
// simply not free (as opposed to an unverified free tier, which may still
// work).
var needsCreditsReasons = map[string]bool{"not_in_free_tier": true, "paid": true, "price_unknown": true}

// NeedsCredits reports whether a model needs credits its connection does not
// have.
func NeedsCredits(entry *Entry) bool {
	return entry.BillingMode == "free_tier" && !entry.IsFree && !entry.IsPlan && needsCreditsReasons[entry.FreeReason]
}

// KindAllowed reports whether a key's free/plan/paid switches admit a model.
// freeOnly (a used-up budget, the playground toggle) shuts paid models out on
// top. It returns the rejection reason, "" when allowed.
func KindAllowed(entry *Entry, key RouterKey, freeOnly bool) string {
	if entry.IsFree {
		if !key.allowFree() {
			return "not_allowed_for_key"
		}
		return ""
	}
	if entry.IsPlan {
		if !key.allowPlan() {
			return "not_allowed_for_key"
		}
		return ""
	}
	if freeOnly || key.Mode == "free_only" {
		return "not_free"
	}
	return ""
}
