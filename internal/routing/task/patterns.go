package task

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// textPattern is a compiled regular expression with the helpers the detector
// rules use.
type textPattern struct {
	re *regexp.Regexp
}

// compile builds a case-sensitive pattern (add (?i) for the ones the Node code
// declares with the i flag).
func compile(expr string) textPattern {
	return textPattern{re: regexp.MustCompile(expr)}
}

func (p textPattern) test(s string) bool { return p.re.MatchString(s) }

// count is (text.match(pattern) || []).length.
func (p textPattern) count(s string) int { return len(p.re.FindAllStringIndex(s, -1)) }

// wordAlt is one alternative of a words() pattern.
type wordAlt struct {
	whole bool // the source ended with $: the word must end there
}

// wordPattern is the Node classifier's words(...) helper: a case-insensitive
// whole-word match of one of several alternatives, where an alternative ending
// in "$" must end at a non-letter and every alternative must start at a
// non-letter (or the start of the text).
//
// The Node code expresses the trailing condition as a lookahead
// ((?!\p{L})); Go's RE2 engine has no lookahead, so one combined expression
// finds the candidates and the trailing rune is checked here.
type wordPattern struct {
	re   *regexp.Regexp
	alts []wordAlt
}

// words builds a words() pattern. Alternatives are regular expressions; a
// trailing "$" means "ends this word" and is not part of the expression.
func words(patterns ...string) *wordPattern {
	alts := make([]wordAlt, 0, len(patterns))
	parts := make([]string, 0, len(patterns))
	for _, pattern := range patterns {
		alt := wordAlt{}
		if strings.HasSuffix(pattern, "$") {
			alt.whole = true
			pattern = strings.TrimSuffix(pattern, "$")
		}
		alts = append(alts, alt)
		// Each alternative is wrapped in a non-capturing group; the group
		// index identifies which alternative matched.
		parts = append(parts, "(?:"+pattern+")")
	}
	expr := `(?i)(^|[^\p{L}])` + "(" + strings.Join(parts, "|") + ")"
	return &wordPattern{re: regexp.MustCompile(expr), alts: alts}
}

// matches walks every match, calling fn with the alternative index, the
// matched text and the byte offset just past it. It stops early when fn
// returns false.
func (w *wordPattern) matches(text string, fn func(alt int, word string, end int) bool) {
	for _, loc := range w.re.FindAllStringSubmatchIndex(text, -1) {
		alt, start, end := -1, -1, -1
		for i := range w.alts {
			group := i + 2 // group 1 is the separator
			if 2*group+1 < len(loc) && loc[2*group] >= 0 {
				alt, start, end = i, loc[2*group], loc[2*group+1]
				break
			}
		}
		if alt < 0 {
			continue
		}
		if w.alts[alt].whole && end < len(text) {
			r, _ := utf8.DecodeRuneInString(text[end:])
			if unicode.IsLetter(r) {
				continue
			}
		}
		if !fn(alt, text[start:end], end) {
			return
		}
	}
}

// test reports whether the text contains the pattern.
func (w *wordPattern) test(text string) bool {
	found := false
	w.matches(text, func(int, string, int) bool { found = true; return false })
	return found
}

// findIndex is where the first match starts (the word itself, not the
// separator), or -1.
func (w *wordPattern) findIndex(text string) int {
	loc := w.re.FindStringSubmatchIndex(text)
	if loc == nil {
		return -1
	}
	for i := range w.alts {
		group := i + 2
		if 2*group+1 < len(loc) && loc[2*group] >= 0 {
			return loc[2*group]
		}
	}
	return -1
}

// hits is signals.hits: how many different words of the pattern a text uses
// (repeating one word says no more).
func (w *wordPattern) hits(text string) int {
	seen := map[string]struct{}{}
	w.matches(text, func(_ int, word string, _ int) bool {
		seen[strings.ToLower(strings.TrimLeftFunc(word, func(r rune) bool { return !unicode.IsLetter(r) }))] = struct{}{}
		return true
	})
	return len(seen)
}

// numberPattern finds number candidates; the lookbehind/lookahead of the Node
// expression (not preceded by a letter, digit or '.', not followed by a letter
// or digit) is checked per match.
var numberPattern = regexp.MustCompile(`[$€£]?[0-9]+(?:[.,][0-9]+)?%?`)

// countNumbers is signals.count(NUMBER, text).
func countNumbers(text string) int {
	count := 0
	for _, loc := range numberPattern.FindAllStringIndex(text, -1) {
		start, end := loc[0], loc[1]
		if start > 0 {
			r, _ := utf8.DecodeLastRuneInString(text[:start])
			if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '.' {
				continue
			}
		}
		if end < len(text) {
			r, _ := utf8.DecodeRuneInString(text[end:])
			if unicode.IsLetter(r) || unicode.IsDigit(r) {
				continue
			}
		}
		count++
	}
	return count
}
