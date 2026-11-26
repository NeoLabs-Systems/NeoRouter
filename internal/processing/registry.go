package processing

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"
)

// The shield's building blocks, loaded once per Registry:
//
//	categories.json   what can be hidden, its placeholder and default mode
//	pack_*.json       declarative pattern rules, one pack per source file
//	detectors         Go code for what one regex cannot express
//	data/*.txt        word lists the detectors read (names, wordlists)
//
// Every rule and detector is validated and ids must be unique, so a broken pack
// fails at start-up instead of silently letting data through.
//
//go:embed shield/data/pack_*.json
var packFiles embed.FS

//go:embed shield/data/*.txt
var listFiles embed.FS

// packOrder is the order the JS registry reads its packs in (files sorted by
// name, where secrets.js sorts before secrets_gitleaks.json).
var packOrder = []string{
	"pack_contact.json",
	"pack_crypto.json",
	"pack_finance.json",
	"pack_ids_de.json",
	"pack_ids_eu.json",
	"pack_ids_intl.json",
	"pack_personal.json",
	"pack_secrets.json",
	"pack_gitleaks.json",
}

// Label is a rule's name in English and German. A pack may give one string,
// which then counts for both languages.
type Label struct {
	EN string
	DE string
}

// UnmarshalJSON accepts "IBAN" and {"en": …, "de": …}.
func (l *Label) UnmarshalJSON(data []byte) error {
	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		l.EN, l.DE = text, text
		return nil
	}
	var pair struct {
		EN string `json:"en"`
		DE string `json:"de"`
	}
	if err := json.Unmarshal(data, &pair); err != nil {
		return err
	}
	l.EN, l.DE = pair.EN, pair.DE
	return nil
}

// MarshalJSON writes {"en","de"}, or null for an unknown rule (registry
// labelOf returns null for a rule that is not there).
func (l Label) MarshalJSON() ([]byte, error) {
	if l.EN == "" && l.DE == "" {
		return []byte("null"), nil
	}
	return json.Marshal(struct {
		EN string `json:"en"`
		DE string `json:"de"`
	}{l.EN, l.DE})
}

// Tests are a rule's own examples: each Match text must produce a span (with the
// named value, if one is given), each Ignore text must produce none.
type Tests struct {
	Match  []TestMatch `json:"match"`
	Ignore []string    `json:"ignore"`
}

// TestMatch is one example text, optionally naming the value expected.
type TestMatch struct {
	Text  string `json:"text"`
	Value string `json:"value"`
}

// UnmarshalJSON accepts a plain string or {"text","value"}.
func (t *TestMatch) UnmarshalJSON(data []byte) error {
	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		t.Text = text
		return nil
	}
	var pair struct {
		Text  string `json:"text"`
		Value string `json:"value"`
	}
	if err := json.Unmarshal(data, &pair); err != nil {
		return err
	}
	t.Text, t.Value = pair.Text, pair.Value
	return nil
}

// ScanContext is what a detector sees besides the text: the lowercased text, the
// user's own terms, the AI entities and the enabled categories.
type ScanContext struct {
	Lower       string
	CustomTerms []string
	Entities    []Entity
	Enabled     map[string]bool
	Lists       *Registry
}

// Entity is one value the privacy filter found.
type Entity struct {
	Text     string `json:"text"`
	Category string `json:"category"`
}

// Span is one private value in a text: where it is, what it is and which rule
// found it.
type Span struct {
	Start    int     `json:"start"`
	End      int     `json:"end"`
	Category string  `json:"category"`
	Value    string  `json:"value"`
	RuleID   string  `json:"ruleId"`
	Score    float64 `json:"score"`
}

// Detector is code that finds what one regex cannot express.
type Detector struct {
	ID       string
	Label    Label
	Category string
	File     string
	Keywords []string
	Scan     func(text string, ctx *ScanContext) []Span
	Tests    Tests
}

// contextSpec is a rule's context words with the sentences they must (or must
// not) appear in, compiled as one whole-word pattern each.
type contextSpec struct {
	positive *jsPattern
	negative *jsPattern
	window   int
	words    []string
}

// Rule is one declarative pattern rule.
type Rule struct {
	ID       string
	Label    Label
	Category string
	File     string
	Pattern  *jsPattern
	// Group is the JavaScript capture group that holds the private value; 0
	// means the whole match (or, with FirstGroup, the first group that matched).
	Group      int
	GroupName  string
	FirstGroup bool
	Keywords   []string
	Validator  string
	Accept     string
	MinEntropy float64
	HasEntropy bool
	Score      float64
	Context    contextSpec
	Allow      []*jsPattern
	Stopwords  []string
	Tests      Tests
}

// Registry is the compiled set of rules, detectors and word lists.
type Registry struct {
	version   string
	rules     []*Rule
	detectors []*Detector
	labels    map[string]Label
	listsMu   sync.Mutex
	lists     map[string]map[string]struct{}
}

// RegistryVersion is the content hash the scan cache keys on.
func (r *Registry) Version() string { return r.version }

// Rules are the pattern rules in pack order.
func (r *Registry) Rules() []*Rule { return r.rules }

// Detectors are the code detectors in file order.
func (r *Registry) Detectors() []*Detector { return r.detectors }

// LabelOf is the label of the rule or detector with this id, or the zero Label
// when there is none.
func (r *Registry) LabelOf(ruleID string) Label { return r.labels[ruleID] }

// WordList is one list from shield/data, one entry per line, "#" lines being
// comments (source and licence).
func (r *Registry) WordList(name string) map[string]struct{} {
	r.listsMu.Lock()
	defer r.listsMu.Unlock()
	if r.lists == nil {
		r.lists = map[string]map[string]struct{}{}
	}
	if list, ok := r.lists[name]; ok {
		return list
	}
	list := map[string]struct{}{}
	raw, err := listFiles.ReadFile("shield/data/" + name + ".txt")
	if err != nil {
		r.lists[name] = list
		return list
	}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		list[strings.ToLower(line)] = struct{}{}
	}
	r.lists[name] = list
	return list
}

// PackSource is one rule pack as it would be read from disk.
type PackSource struct {
	Name string
	Data []byte
}

// BuildOptions replaces what NewRegistry loads; the tests use it to prove a
// broken pack fails at load time.
type BuildOptions struct {
	Packs     []PackSource
	Detectors []*Detector
}

type patternJSON struct {
	Source string
	Flags  string
}

// UnmarshalJSON accepts a pattern string or {"source","flags"}.
func (p *patternJSON) UnmarshalJSON(data []byte) error {
	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		p.Source = text
		return nil
	}
	var object struct {
		Source string `json:"source"`
		Flags  string `json:"flags"`
	}
	if err := json.Unmarshal(data, &object); err != nil {
		return err
	}
	p.Source, p.Flags = object.Source, object.Flags
	return nil
}

type contextJSON struct {
	Positive []string `json:"positive"`
	Negative []string `json:"negative"`
	Window   *int     `json:"window"`
}

type allowJSON struct {
	Regexes   []string `json:"regexes"`
	Stopwords []string `json:"stopwords"`
}

type ruleJSON struct {
	ID         string          `json:"id"`
	Label      Label           `json:"label"`
	Category   string          `json:"category"`
	Pattern    patternJSON     `json:"pattern"`
	Flags      string          `json:"flags"`
	Group      json.RawMessage `json:"group"`
	FirstGroup bool            `json:"firstGroup"`
	Keywords   []string        `json:"keywords"`
	Validator  string          `json:"validator"`
	Accept     string          `json:"accept"`
	MinEntropy *float64        `json:"minEntropy"`
	Score      *float64        `json:"score"`
	Context    *contextJSON    `json:"context"`
	Allow      *allowJSON      `json:"allow"`
	Tests      Tests           `json:"tests"`
}

type packJSON struct {
	Source  string            `json:"source"`
	License string            `json:"license"`
	Allow   *allowJSON        `json:"allow"`
	Rules   []json.RawMessage `json:"rules"`
}

var (
	idShape = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]*$`)
	// escapeRegExpPattern is the set of characters escapeRegExp escapes.
	regexpSpecials = regexp.MustCompile(`[.*+?^${}()|\[\]\\]`)
)

// escapeRegExp escapes a literal for a pattern, like registry.js escapeRegExp.
func escapeRegExp(value string) string {
	return regexpSpecials.ReplaceAllStringFunc(value, func(match string) string { return `\` + match })
}

// wordRegex compiles a word list as one pattern that only matches whole words.
// It is used as a yes/no test on the text around a match.
func wordRegex(list []string) (*jsPattern, error) {
	if len(list) == 0 {
		return nil, nil
	}
	escaped := make([]string, 0, len(list))
	for _, word := range list {
		escaped = append(escaped, escapeRegExp(word))
	}
	source := `(?<![\p{L}\p{N}])(?:` + strings.Join(escaped, "|") + `)(?![\p{L}\p{N}])`
	return compileJSPattern(source, "iu")
}

// NewRegistry compiles the embedded packs, categories and detectors.
func NewRegistry() (*Registry, error) { return BuildRegistry(BuildOptions{}) }

// BuildRegistry compiles the given packs (or the embedded ones) and detectors.
func BuildRegistry(options BuildOptions) (*Registry, error) {
	hash := sha256.New()
	hash.Write(categoriesJSON)
	packs := options.Packs
	if packs == nil {
		for _, name := range packOrder {
			data, err := packFiles.ReadFile("shield/data/" + name)
			if err != nil {
				return nil, fmt.Errorf("processing: read shield pack %s: %w", name, err)
			}
			packs = append(packs, PackSource{Name: name, Data: data})
		}
	}
	registry := &Registry{labels: map[string]Label{}}
	for _, pack := range packs {
		hash.Write(pack.Data)
		rules, err := compilePack(pack)
		if err != nil {
			return nil, err
		}
		registry.rules = append(registry.rules, rules...)
	}
	detectors := options.Detectors
	if detectors == nil {
		detectors = builtinDetectors()
	}
	for _, detector := range detectors {
		hash.Write([]byte(detector.ID + "\x00" + detector.Category + "\x00" + strings.Join(detector.Keywords, ",")))
		registry.detectors = append(registry.detectors, detector)
	}
	for _, rule := range registry.rules {
		if _, exists := registry.labels[rule.ID]; exists {
			return nil, fmt.Errorf("Duplicate shield rule id %s in %s.", rule.ID, rule.File)
		}
		registry.labels[rule.ID] = rule.Label
	}
	for _, detector := range registry.detectors {
		if _, exists := registry.labels[detector.ID]; exists {
			return nil, fmt.Errorf("Duplicate shield rule id %s in %s.", detector.ID, detector.File)
		}
		registry.labels[detector.ID] = detector.Label
	}
	registry.version = hex.EncodeToString(hash.Sum(nil))[:16]
	return registry, nil
}

func compilePack(pack PackSource) ([]*Rule, error) {
	var parsed packJSON
	if err := json.Unmarshal(pack.Data, &parsed); err != nil {
		return nil, fmt.Errorf("Invalid shield rule pack %s: %s", pack.Name, err)
	}
	rules := make([]*Rule, 0, len(parsed.Rules))
	for _, raw := range parsed.Rules {
		var item ruleJSON
		if err := json.Unmarshal(raw, &item); err != nil {
			return nil, fmt.Errorf("Invalid shield rule in %s (%s): %s", pack.Name, idIn(raw), err)
		}
		rule, err := compileRule(item, pack.Name, parsed.Allow)
		if err != nil {
			return nil, err
		}
		rules = append(rules, rule)
	}
	return rules, nil
}

// idIn reads just the id of a broken rule, for the error message.
func idIn(raw json.RawMessage) string {
	var probe struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil || probe.ID == "" {
		return "unknown"
	}
	return probe.ID
}

func compileRule(item ruleJSON, file string, packAllow *allowJSON) (*Rule, error) {
	invalid := func(format string, args ...any) error {
		return fmt.Errorf("Invalid shield rule in %s (%s): %s", file, item.ID, fmt.Sprintf(format, args...))
	}
	if !idShape.MatchString(item.ID) {
		return nil, invalid("id: invalid id")
	}
	if item.Label.EN == "" || item.Label.DE == "" {
		return nil, invalid("label: a label needs an en and a de text")
	}
	if !KnownCategory(item.Category) {
		return nil, invalid("category: unknown category %q", item.Category)
	}
	if item.Pattern.Source == "" {
		return nil, invalid("pattern: a rule needs a pattern")
	}
	// The declared flags field is validated as it is in the JS schema; a pattern
	// carried over as a RegExp brings its own flags, where g and d only describe
	// how the JS engine iterates.
	for _, flag := range item.Flags {
		if !strings.ContainsRune("imsuv", flag) {
			return nil, invalid("flags: invalid flag %q", string(flag))
		}
	}
	flags := item.Pattern.Flags
	if item.Pattern.Source != "" && flags == "" {
		flags = item.Flags
	}
	if item.Pattern.Source == "" {
		flags = item.Flags
	}
	pattern, err := compileJSPattern(item.Pattern.Source, flags)
	if err != nil {
		return nil, fmt.Errorf("Shield rule %s in %s has an invalid pattern: %s", item.ID, file, err)
	}
	rule := &Rule{
		ID:         item.ID,
		Label:      item.Label,
		Category:   item.Category,
		File:       file,
		Pattern:    pattern,
		FirstGroup: item.FirstGroup,
		Keywords:   []string{},
		Score:      0.6,
		Tests:      item.Tests,
	}
	if item.Score != nil {
		rule.Score = *item.Score
	}
	if rule.Score < 0 || rule.Score > 1 {
		return nil, invalid("score: a score is between 0 and 1")
	}
	for _, word := range item.Keywords {
		rule.Keywords = append(rule.Keywords, strings.ToLower(word))
	}
	if len(item.Group) > 0 {
		var number int
		if err := json.Unmarshal(item.Group, &number); err == nil {
			if number < 0 {
				return nil, invalid("group: a group number is 0 or higher")
			}
			rule.Group = number
		} else {
			var name string
			if err := json.Unmarshal(item.Group, &name); err != nil {
				return nil, invalid("group: a group is a number or a name")
			}
			rule.GroupName = name
		}
	}
	if item.Validator != "" {
		if _, ok := Validators[item.Validator]; !ok {
			return nil, invalid("validator: unknown validator %q", item.Validator)
		}
		rule.Validator = item.Validator
	}
	if item.Accept != "" {
		if _, ok := Validators[item.Accept]; !ok {
			return nil, invalid("accept: unknown validator %q", item.Accept)
		}
		rule.Accept = item.Accept
	}
	if item.MinEntropy != nil {
		if *item.MinEntropy < 0 || *item.MinEntropy > 8 {
			return nil, invalid("minEntropy: entropy is between 0 and 8")
		}
		rule.MinEntropy, rule.HasEntropy = *item.MinEntropy, true
	}
	rule.Context.window = 40
	if item.Context != nil {
		rule.Context.words = append(append([]string{}, item.Context.Positive...), item.Context.Negative...)
		if item.Context.Window != nil {
			if *item.Context.Window < 1 || *item.Context.Window > 200 {
				return nil, invalid("context.window: a window is between 1 and 200")
			}
			rule.Context.window = *item.Context.Window
		}
		if rule.Context.positive, err = wordRegex(item.Context.Positive); err != nil {
			return nil, invalid("context.positive: %s", err)
		}
		if rule.Context.negative, err = wordRegex(item.Context.Negative); err != nil {
			return nil, invalid("context.negative: %s", err)
		}
	}
	var regexes []string
	var stopwords []string
	if packAllow != nil {
		regexes = append(regexes, packAllow.Regexes...)
		stopwords = append(stopwords, packAllow.Stopwords...)
	}
	if item.Allow != nil {
		regexes = append(regexes, item.Allow.Regexes...)
		stopwords = append(stopwords, item.Allow.Stopwords...)
	}
	for _, source := range regexes {
		compiled, err := compileJSPattern(source, "i")
		if err != nil {
			return nil, invalid("allow.regexes: %s", err)
		}
		rule.Allow = append(rule.Allow, compiled)
	}
	for _, word := range stopwords {
		rule.Stopwords = append(rule.Stopwords, strings.ToLower(word))
	}
	return rule, nil
}
