package processing

import (
	"container/list"
	"crypto/sha256"
	"encoding/hex"
	"math"
	"regexp"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"
)

// The engine runs the rules and detectors over one text, cheapest work first:
//
//  1. keyword prefilter: a rule with keywords only runs when the lowercased
//     text contains one (rules that need context use their context words),
//  2. the regex itself,
//  3. validators: a failed check drops the match, a passed one adds
//     validatorBonus,
//  4. context words near the match add or subtract contextBonus/contextPenalty,
//  5. only matches scoring at least threshold are kept,
//  6. allow-lists, then overlaps (the more sensitive category wins, then the
//     longer match).
//
// Results are cached per text, because chat clients re-send the whole history
// every turn.
const (
	// Threshold is the score a span needs to be reported.
	threshold        = 0.5
	validatorBonus   = 0.3
	contextBonus     = 0.3
	contextPenalty   = 0.4
	cacheSize        = 2000
	contextSeparator = " \u0000 "
)

// placeholderPattern is an existing placeholder in the text; the shield never
// rewrites one.
var placeholderPattern = regexp.MustCompile(`\[\s*([A-Za-z]{2,12})[ _-]?(\d{1,4})\s*\]`)

// Engine runs the shield over texts and caches the result per text.
type Engine struct {
	registry *Registry

	mu     sync.Mutex
	cache  map[string]*list.Element
	recent *list.List
}

type cacheEntry struct {
	key   string
	spans []Span
}

// NewEngine builds an engine over a registry.
func NewEngine(registry *Registry) *Engine {
	return &Engine{
		registry: registry,
		cache:    map[string]*list.Element{},
		recent:   list.New(),
	}
}

// Registry is the pack build this engine scans with.
func (e *Engine) Registry() *Registry { return e.registry }

// ScanOptions selects what may be hidden in one text.
type ScanOptions struct {
	// Modes maps a category to "hide", "restore" or "off"; nil hides nothing.
	Modes       map[string]string
	CustomTerms []string
	AllowTerms  []string
	// Entities are the AI pass's findings ({text, category}), matched exactly.
	Entities []Entity
	// Registry scans with another pack build (tests).
	Registry *Registry
}

// Scan is every private span in a text, in order, with overlaps resolved.
func (e *Engine) Scan(text string, options ScanOptions) []Span {
	if text == "" {
		return nil
	}
	registry := e.registry
	if options.Registry != nil {
		registry = options.Registry
	}
	enabled := enabledSet(options.Modes)
	var terms []string
	for _, term := range options.CustomTerms {
		if trimmed := strings.TrimSpace(term); len([]rune(trimmed)) >= 2 {
			terms = append(terms, trimmed)
		}
	}
	var allow []string
	for _, term := range options.AllowTerms {
		if trimmed := strings.ToLower(strings.TrimSpace(term)); trimmed != "" {
			allow = append(allow, trimmed)
		}
	}
	key := e.cacheKey(registry, enabled, terms, allow, text)
	spans, cached := e.cacheGet(key)
	if !cached {
		spans = e.baseScan(registry, text, enabled, terms, allow)
		e.cachePut(key, spans)
	}
	var fromEntities []Span
	if len(options.Entities) > 0 {
		if detector := findDetector(registry, "ai_entities"); detector != nil {
			ctx := &ScanContext{Lower: strings.ToLower(text), Entities: options.Entities, Enabled: enabled, Lists: registry}
			for _, span := range e.runDetector(detector, text, ctx) {
				if enabled[span.Category] && !allowed(span.Value, allow) {
					fromEntities = append(fromEntities, span)
				}
			}
		}
	}
	if len(fromEntities) > 0 {
		spans = append(append([]Span{}, spans...), fromEntities...)
	}
	return resolve(text, spans)
}

// ScanOne runs one rule or detector with every category enabled; the rule tests
// use it.
func (e *Engine) ScanOne(rule *Rule, text string) []Span {
	return e.runRule(rule, text)
}

// ScanOneDetector runs one detector with every category enabled.
func (e *Engine) ScanOneDetector(detector *Detector, text string) []Span {
	enabled := map[string]bool{}
	for _, category := range Categories {
		enabled[category] = true
	}
	ctx := &ScanContext{Lower: strings.ToLower(text), Enabled: enabled, Lists: e.registry}
	return e.runDetector(detector, text, ctx)
}

// ClearCache drops the per-text results (the corpus test measures a cold scan).
func (e *Engine) ClearCache() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.cache = map[string]*list.Element{}
	e.recent = list.New()
}

func (e *Engine) cacheKey(registry *Registry, enabled map[string]bool, terms, allow []string, text string) string {
	names := make([]string, 0, len(enabled))
	for category := range enabled {
		names = append(names, category)
	}
	sort.Strings(names)
	hash := sha256.New()
	hash.Write([]byte(registry.Version()))
	hash.Write([]byte{0})
	hash.Write([]byte(strings.Join(names, ",")))
	hash.Write([]byte{0})
	hash.Write([]byte(strings.Join(terms, "\x01")))
	hash.Write([]byte{0})
	hash.Write([]byte(strings.Join(allow, "\x01")))
	hash.Write([]byte{0})
	hash.Write([]byte(text))
	return hex.EncodeToString(hash.Sum(nil))
}

func (e *Engine) cacheGet(key string) ([]Span, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	element, ok := e.cache[key]
	if !ok {
		return nil, false
	}
	e.recent.MoveToFront(element)
	return element.Value.(*cacheEntry).spans, true
}

func (e *Engine) cachePut(key string, spans []Span) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, exists := e.cache[key]; exists {
		return
	}
	e.cache[key] = e.recent.PushFront(&cacheEntry{key: key, spans: spans})
	if e.recent.Len() > cacheSize {
		oldest := e.recent.Back()
		if oldest != nil {
			e.recent.Remove(oldest)
			delete(e.cache, oldest.Value.(*cacheEntry).key)
		}
	}
}

// baseScan is the spans from rules and detectors, before AI entities are added.
func (e *Engine) baseScan(registry *Registry, text string, enabled map[string]bool, terms, allow []string) []Span {
	lower := strings.ToLower(text)
	ctx := &ScanContext{Lower: lower, CustomTerms: terms, Enabled: enabled, Lists: registry}
	spans := []Span{}
	for _, rule := range registry.Rules() {
		if enabled[rule.Category] && triggeredRule(rule, lower) {
			spans = append(spans, e.runRule(rule, text)...)
		}
	}
	for _, detector := range registry.Detectors() {
		if detector.ID == "ai_entities" {
			continue
		}
		if enabled[detector.Category] && triggeredDetector(detector, lower) {
			spans = append(spans, e.runDetector(detector, text, ctx)...)
		}
	}
	kept := spans[:0]
	for _, span := range spans {
		if !allowed(span.Value, allow) {
			kept = append(kept, span)
		}
	}
	return kept
}

func findDetector(registry *Registry, id string) *Detector {
	for _, detector := range registry.Detectors() {
		if detector.ID == id {
			return detector
		}
	}
	return nil
}

// needsContext is true for a rule that cannot reach the threshold on its own.
func needsContext(rule *Rule) bool {
	bonus := 0.0
	if rule.Validator != "" {
		bonus = validatorBonus
	}
	return rule.Score+bonus < threshold
}

func triggeredRule(rule *Rule, lower string) bool {
	keywords := rule.Keywords
	if len(keywords) == 0 && needsContext(rule) {
		for _, word := range rule.Context.words {
			keywords = append(keywords, strings.ToLower(word))
		}
	}
	return anyKeyword(keywords, lower)
}

func triggeredDetector(detector *Detector, lower string) bool {
	return anyKeyword(detector.Keywords, lower)
}

func anyKeyword(keywords []string, lower string) bool {
	if len(keywords) == 0 {
		return true
	}
	for _, word := range keywords {
		if strings.Contains(lower, word) {
			return true
		}
	}
	return false
}

// runRule is every match of one rule with its score; matches below the
// threshold are kept out.
func (e *Engine) runRule(rule *Rule, text string) []Span {
	spans := []Span{}
	iteratePattern(rule.Pattern, text, func(loc []int) bool {
		value, start, ok := rule.valueOf(text, loc)
		if !ok || strings.TrimSpace(value) == "" {
			return true
		}
		if rule.Accept != "" && !Validators[rule.Accept](value) {
			return true
		}
		if rule.HasEntropy && entropy(value) < rule.MinEntropy {
			return true
		}
		if len(rule.Stopwords) > 0 {
			lower := strings.ToLower(value)
			for _, word := range rule.Stopwords {
				if strings.Contains(lower, word) {
					return true
				}
			}
		}
		for _, allow := range rule.Allow {
			if allow.matchesAnywhere(value) {
				return true
			}
		}
		score := rule.Score
		if rule.Validator != "" {
			if !Validators[rule.Validator](value) {
				return true
			}
			score += validatorBonus
		}
		score += contextScore(rule, text, start, start+len(value))
		if score < threshold {
			return true
		}
		spans = append(spans, Span{
			Start:    start,
			End:      start + len(value),
			Category: rule.Category,
			Value:    value,
			RuleID:   rule.ID,
			Score:    math.Min(1, score),
		})
		return true
	})
	return spans
}

// valueOf is the private value of one match: the whole match, one group, or the
// first group that matched (gitleaks style).
func (r *Rule) valueOf(text string, loc []int) (string, int, bool) {
	groupValue := func(group int) (string, int, bool) {
		if group <= 0 || 2*group+1 >= len(loc) {
			return "", 0, false
		}
		start, end := loc[2*group], loc[2*group+1]
		if start < 0 || end <= start {
			return "", 0, false
		}
		return text[start:end], start, true
	}
	if r.GroupName != "" {
		return groupValue(r.Pattern.re.SubexpIndex(r.GroupName))
	}
	if r.Group == 0 {
		if r.FirstGroup {
			for group := 1; group <= r.Pattern.groupCount; group++ {
				if value, start, ok := groupValue(r.Pattern.groupOf[group]); ok {
					return value, start, true
				}
			}
		}
		start, end := r.Pattern.span(loc)
		return text[start:end], start, true
	}
	return groupValue(r.Pattern.groupOf[r.Group])
}

// runDetector turns a detector's spans into engine spans.
func (e *Engine) runDetector(detector *Detector, text string, ctx *ScanContext) []Span {
	spans := []Span{}
	for _, span := range detector.Scan(text, ctx) {
		if span.Score == 0 {
			span.Score = 1
		}
		if span.Score < threshold || span.End <= span.Start {
			continue
		}
		span.Category = detector.Category
		if span.RuleID == "" {
			span.RuleID = detector.ID
		}
		if span.Value == "" && span.Start >= 0 && span.End <= len(text) {
			span.Value = text[span.Start:span.End]
		}
		spans = append(spans, span)
	}
	return spans
}

// contextScore is the bonus or penalty from the words around a match.
func contextScore(rule *Rule, text string, start, end int) float64 {
	if rule.Context.positive == nil && rule.Context.negative == nil {
		return 0
	}
	window := rule.Context.window
	from := runeBack(text, start, window)
	if from < 0 {
		from = 0
	}
	around := text[from:start] + contextSeparator + text[end:runeForward(text, end, window)]
	score := 0.0
	if rule.Context.positive != nil && rule.Context.positive.matchesAnywhere(around) {
		score += contextBonus
	}
	if rule.Context.negative != nil && rule.Context.negative.matchesAnywhere(around) {
		score -= contextPenalty
	}
	return score
}

// allowed keeps values the user excluded: exact, or a domain ("@example.com")
// or a suffix (".example.com").
func allowed(value string, allowTerms []string) bool {
	if len(allowTerms) == 0 {
		return false
	}
	lower := strings.ToLower(value)
	for _, term := range allowTerms {
		if lower == term {
			return true
		}
		if (strings.HasPrefix(term, "@") || strings.HasPrefix(term, ".")) && strings.HasSuffix(lower, term) {
			return true
		}
	}
	return false
}

// resolve keeps the most sensitive, then longest span where several overlap, and
// never touches existing placeholders.
func resolve(text string, spans []Span) []Span {
	placeholders := placeholderPattern.FindAllStringIndex(text, -1)
	rank := func(span Span) int {
		if priority, ok := Priorities[span.Category]; ok {
			return priority
		}
		return 5
	}
	sorted := append([]Span{}, spans...)
	sort.SliceStable(sorted, func(a, b int) bool {
		left, right := sorted[a], sorted[b]
		if rank(left) != rank(right) {
			return rank(left) < rank(right)
		}
		if (right.End - right.Start) != (left.End - left.Start) {
			return (right.End - right.Start) < (left.End - left.Start)
		}
		return right.Score < left.Score
	})
	chosen := make([]Span, 0, len(sorted))
	for _, span := range sorted {
		skip := false
		for _, placeholder := range placeholders {
			if span.Start < placeholder[1] && span.End > placeholder[0] {
				skip = true
				break
			}
		}
		if skip {
			continue
		}
		for _, other := range chosen {
			if span.Start < other.End && span.End > other.Start {
				skip = true
				break
			}
		}
		if skip {
			continue
		}
		chosen = append(chosen, span)
	}
	sort.SliceStable(chosen, func(a, b int) bool { return chosen[a].Start < chosen[b].Start })
	return chosen
}

// enabledSet is the categories a modes map turns on.
func enabledSet(modes map[string]string) map[string]bool {
	enabled := map[string]bool{}
	for category, mode := range modes {
		if mode != "" && mode != "off" {
			enabled[category] = true
		}
	}
	return enabled
}

// runeForward is the byte offset n runes after at, clamped to the text.
func runeForward(text string, at, n int) int {
	index := at
	for step := 0; step < n && index < len(text); step++ {
		_, size := utf8.DecodeRuneInString(text[index:])
		index += size
	}
	return index
}

// runeBack returns the byte index n runes before at, or -1 when the text is
// shorter.
func runeBack(text string, at, n int) int {
	index := at
	for step := 0; step < n; step++ {
		if index <= 0 {
			return -1
		}
		_, size := utf8.DecodeLastRuneInString(text[:index])
		index -= size
	}
	return index
}

// iteratePattern walks the non-overlapping matches of a pattern, skipping the
// ones a mid-pattern assertion rejects.
func iteratePattern(pattern *jsPattern, text string, fn func(loc []int) bool) {
	for _, loc := range pattern.re.FindAllStringSubmatchIndex(text, -1) {
		if !pattern.checksPass(text, loc) {
			continue
		}
		if !fn(loc) {
			return
		}
	}
}

// checksPass evaluates the assertions of a pattern at the positions its marker
// groups captured.
func (p *jsPattern) checksPass(text string, loc []int) bool {
	for _, check := range p.checks {
		group := check.group
		if 2*group+1 >= len(loc) || loc[2*group] < 0 {
			continue
		}
		at := loc[2*group]
		matched := false
		if check.behind {
			for size := check.minRunes; size <= check.maxRunes; size++ {
				back := runeBack(text, at, size)
				if back < 0 {
					continue
				}
				if check.body.MatchString(text[back:at]) {
					matched = true
					break
				}
			}
		} else {
			matched = check.body.MatchString(text[at:])
		}
		if matched == check.negative {
			return false
		}
	}
	return true
}

// matchesAnywhere reports whether the pattern matches at all.
func (p *jsPattern) matchesAnywhere(text string) bool {
	found := false
	iteratePattern(p, text, func([]int) bool {
		found = true
		return false
	})
	return found
}
