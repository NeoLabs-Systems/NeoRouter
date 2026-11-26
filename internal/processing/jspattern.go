package processing

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Go's regexp engine (RE2) has no lookaround, but the shield's rule packs are
// JavaScript patterns and many of them use it as a boundary test:
//
//	(?<![\w+])          the match must not follow a word character or "+"
//	(?![\p{L}\p{N}])    the match must not be followed by a letter or digit
//
// Porting those by hand would mean retyping ~90 patterns, and a typo would
// quietly change what the shield hides. Instead compileJSPattern rewrites each
// assertion into an empty marker group at the same position and checks the
// assertion afterwards, against the text around that position:
//
//	(?<![\w+])\d+  ->  (?P<z0>)\d+     plus "no [\w+] ends here"
//
// The rules the translator keeps faithful:
//
//   - a marker group matches empty, so its offsets are the position the
//     assertion applied to; when the enclosing branch of an alternation is not
//     taken the group does not take part and the assertion is skipped, exactly
//     like JavaScript;
//   - a lookbehind body is tested against the text in front of the position,
//     anchored at both ends, for every body length it can have;
//   - \s, \S and . are expanded to the characters JavaScript means, which are
//     wider than Go's (\s in JS includes the no-break spaces, . excludes \r
//     and the line separators);
//   - capture groups are renumbered, so a rule's `group` and `firstGroup`
//     still name the same part of the match as in JavaScript.
//
// What it refuses, with an error at load time rather than silently: a
// backreference, and an assertion inside an assertion. No rule in the packs
// uses either.

// jsSpace is the set of runes JavaScript's \s matches.
var jsSpace = []charRange{
	{'\t', '\r'}, // tab, line feed, vertical tab, form feed, carriage return
	{' ', ' '},
	{'\u00a0', '\u00a0'},
	{'\u1680', '\u1680'},
	{'\u2000', '\u200a'},
	{'\u2028', '\u2029'},
	{'\u202f', '\u202f'},
	{'\u205f', '\u205f'},
	{'\u3000', '\u3000'},
	{'\ufeff', '\ufeff'},
}

type charRange struct{ lo, hi rune }

// jsPattern is a compiled JavaScript pattern.
type jsPattern struct {
	re     *regexp.Regexp
	checks []lookaround
	// suffixGroups hold a consumed trailing boundary: the real match ends before
	// them, because the character belongs to the assertion.
	suffixGroups []int
	// groupOf maps a JavaScript capture group number to its number in re (the
	// added groups shift the numbering).
	groupOf map[int]int
	// groupCount is the number of capture groups the JavaScript pattern had.
	groupCount int
}

// span is the real match boundaries: a consumed boundary character belongs to
// the assertion, not to the value.
func (p *jsPattern) span(loc []int) (int, int) {
	start, end := loc[0], loc[1]
	for _, group := range p.suffixGroups {
		if 2*group+1 < len(loc) && loc[2*group] >= 0 {
			end -= loc[2*group+1] - loc[2*group]
		}
	}
	return start, end
}

// lookaround is one assertion of a compiled pattern.
type lookaround struct {
	group    int // the marker group holding the position
	behind   bool
	negative bool
	body     *regexp.Regexp // \A(?:body) for a lookahead, \A(?:body)\z for a lookbehind
	minRunes int
	maxRunes int
}

// The two ways an assertion is compiled: a consumed trailing boundary, or an
// empty marker plus a check.
const (
	checkMarker = iota
	checkSuffix
)

type rawCheck struct {
	group    int
	kind     int
	behind   bool
	negative bool
	body     string
}

// compileJSPattern compiles a JavaScript regular expression from its source and
// flags (i, m and s are understood; g and d are implied by the API).
func compileJSPattern(source, flags string) (*jsPattern, error) {
	body, checks, groupOf, groupCount, err := translateJS(source)
	if err != nil {
		return nil, err
	}
	prefix := ""
	for _, flag := range []byte{'i', 'm', 's'} {
		if strings.IndexByte(flags, flag) >= 0 {
			prefix += string(flag)
		}
	}
	if prefix != "" {
		body = "(?" + prefix + ")" + body
	}
	re, err := regexp.Compile(body)
	if err != nil {
		return nil, err
	}
	pattern := &jsPattern{re: re, groupOf: groupOf, groupCount: groupCount}
	for _, check := range checks {
		switch check.kind {
		case checkSuffix:
			pattern.suffixGroups = append(pattern.suffixGroups, check.group)
			continue
		}
		anchored := `\A(?:` + check.body + `)`
		if check.behind {
			anchored += `\z`
		}
		compiled, err := regexp.Compile(anchored)
		if err != nil {
			return nil, err
		}
		minRunes, maxRunes := widthOf(check.body)
		if maxRunes > maxLookbehindRunes {
			maxRunes = maxLookbehindRunes
		}
		pattern.checks = append(pattern.checks, lookaround{
			group:    check.group,
			behind:   check.behind,
			negative: check.negative,
			body:     compiled,
			minRunes: minRunes,
			maxRunes: maxRunes,
		})
	}
	return pattern, nil
}

// maxLookbehindRunes bounds how far a lookbehind body may reach back.
const maxLookbehindRunes = 64

func translateJS(src string) (string, []rawCheck, map[int]int, int, error) {
	t := &jsTranslator{groupOf: map[int]int{}}
	if err := t.walk(src, true); err != nil {
		return "", nil, nil, 0, err
	}
	return t.out.String(), t.checks, t.groupOf, t.jsGroups, nil
}

type jsTranslator struct {
	out      strings.Builder
	checks   []rawCheck
	groupOf  map[int]int
	jsGroups int
	goGroups int
	// sinceBranch counts the atoms written since the current branch began, which
	// tells a leading assertion from one further in.
	sinceBranch int
}

func (t *jsTranslator) walk(src string, assertions bool) error {
	for i := 0; i < len(src); {
		switch c := src[i]; {
		case c == '\\':
			written, size, err := translateEscape(src[i:], false)
			if err != nil {
				return err
			}
			t.out.WriteString(written)
			t.sinceBranch++
			i += size
		case c == '[':
			end, err := classEnd(src, i)
			if err != nil {
				return err
			}
			written, err := translateClass(src[i:end])
			if err != nil {
				return err
			}
			t.out.WriteString(written)
			t.sinceBranch++
			i = end
		case c == '.':
			t.out.WriteString(`[^\n\r\x{2028}\x{2029}]`)
			t.sinceBranch++
			i++
		case c == '(':
			consumed, err := t.group(src, i, assertions)
			if err != nil {
				return err
			}
			i += consumed
		case c == ')':
			// Closes a plain or non-capturing group; an assertion consumed its own.
			t.out.WriteByte(c)
			i++
		default:
			r, size := utf8.DecodeRuneInString(src[i:])
			_ = r
			t.out.WriteString(src[i : i+size])
			t.sinceBranch++
			i += size
		}
	}
	return nil
}

// group translates the "(...)" starting at i and reports how many bytes it used.
//
// A boundary assertion at the start of a branch becomes a consumed character
// ((?:\\A|(C)) for "not preceded by C"), and one at the end of a branch becomes a
// consumed character too ((?:\\z|(C)) for "not followed by C"). Consuming the
// boundary is what makes the engine backtrack the way JavaScript does: with the
// boundary inside the pattern, a rejected longer alternative lets the engine try
// a shorter one, which a check applied after the fact cannot do. span() subtracts
// the consumed characters again.
func (t *jsTranslator) group(src string, i int, assertions bool) (int, error) {
	rest := src[i:]
	switch {
	case strings.HasPrefix(rest, "(?:"):
		t.out.WriteString("(?:")
		t.sinceBranch = 0
		return 3, nil
	case strings.HasPrefix(rest, "(?="), strings.HasPrefix(rest, "(?!"):
		end, err := matchingParen(src, i)
		if err != nil {
			return 0, err
		}
		if !assertions {
			return 0, fmt.Errorf("an assertion inside an assertion is not supported")
		}
		inner, err := translateBody(src[i+3 : end])
		if err != nil {
			return 0, err
		}
		negative := src[i+2] == '!'
		if complement, ok := consumable(inner, negative); ok && trailingInBranch(src, end) {
			t.emitConsuming(complement, negative)
			t.checks = append(t.checks, rawCheck{group: t.goGroups, kind: checkSuffix, negative: negative})
			return end + 1 - i, nil
		}
		t.goGroups++
		t.out.WriteString(fmt.Sprintf("(?P<z%d>)", len(t.checks)))
		t.checks = append(t.checks, rawCheck{group: t.goGroups, body: inner, negative: negative})
		return end + 1 - i, nil
	case strings.HasPrefix(rest, "(?<="), strings.HasPrefix(rest, "(?<!"):
		end, err := matchingParen(src, i)
		if err != nil {
			return 0, err
		}
		if !assertions {
			return 0, fmt.Errorf("an assertion inside an assertion is not supported")
		}
		inner, err := translateBody(src[i+4 : end])
		if err != nil {
			return 0, err
		}
		negative := src[i+3] == '!'
		t.goGroups++
		t.out.WriteString(fmt.Sprintf("(?P<z%d>)", len(t.checks)))
		t.checks = append(t.checks, rawCheck{group: t.goGroups, body: inner, behind: true, negative: negative})
		return end + 1 - i, nil
	case strings.HasPrefix(rest, "(?<"), strings.HasPrefix(rest, "(?P<"):
		// A named capture group: (?<name> or (?P<name>.
		open := strings.IndexByte(rest, '<')
		closing := strings.IndexByte(rest, '>')
		if closing < 0 || closing < open {
			return 0, fmt.Errorf("unterminated group name at %d", i)
		}
		name := rest[open+1 : closing]
		if !validGroupName(name) {
			return 0, fmt.Errorf("invalid group name %q", name)
		}
		t.jsGroups++
		t.goGroups++
		t.groupOf[t.jsGroups] = t.goGroups
		t.out.WriteString("(?P<" + name + ">")
		t.sinceBranch = 0
		return closing + 1, nil
	case strings.HasPrefix(rest, "(?"):
		// Inline flags: (?i), (?im), (?i: ... ), (?-i: ... ). Go reads them as
		// they are, so copy the prefix unchanged.
		end := 2
		for end < len(rest) && strings.IndexByte("imsuU-", rest[end]) >= 0 {
			end++
		}
		if end >= len(rest) || (rest[end] != ':' && rest[end] != ')') {
			return 0, fmt.Errorf("unsupported group syntax at %d", i)
		}
		t.out.WriteString(rest[:end])
		return end, nil
	default:
		t.jsGroups++
		t.goGroups++
		t.groupOf[t.jsGroups] = t.goGroups
		t.out.WriteByte('(')
		t.sinceBranch = 0
		return 1, nil
	}
}

// consumable is the body of an assertion written as the single character a
// consuming boundary has to test, or false when the body is not one character
// wide. "Not C here" consumes a character that is not C, which is C's complement:
// [abc] becomes [^abc], [^abc] becomes [abc], \d becomes [^\d].
func consumable(inner string, negative bool) (string, bool) {
	if !negative {
		return inner, true
	}
	if strings.HasPrefix(inner, "[") && strings.HasSuffix(inner, "]") {
		if strings.HasPrefix(inner, "[^") {
			return "[" + inner[2:], true
		}
		return "[^" + inner[1:], true
	}
	switch inner {
	case `\d`, `\w`, `\s`:
		return "[^" + inner + "]", true
	case ".":
		return `[^\n\r\x{2028}\x{2029}]`, true
	}
	if strings.HasPrefix(inner, `\p{`) && strings.HasSuffix(inner, "}") {
		return "[^" + inner + "]", true
	}
	if strings.HasPrefix(inner, `\P{`) && strings.HasSuffix(inner, "}") {
		return "[" + `\p` + inner[3:], true
	}
	return "", false
}

// emitConsuming writes an assertion as a consumed character: "not C here" becomes
// "(?:\A|(C))" (nothing before the text, or a character that is not C) and "C
// here" becomes "(C)". anchor is \A or \z, the only positions where the
// assertion holds without consuming anything.
func (t *jsTranslator) emitConsuming(inner string, negative bool) {
	t.goGroups++
	t.goGroups += captureGroupCount(inner)
	if negative {
		t.out.WriteString(`(?:\z|(` + inner + `))`)
		return
	}
	t.out.WriteString("(" + inner + ")")
}

// captureGroupCount counts the capture groups of a translated body.
func captureGroupCount(body string) int {
	count := 0
	for index := 0; index < len(body); {
		switch body[index] {
		case '\\':
			index += 2
		case '[':
			end, err := classEnd(body, index)
			if err != nil {
				return count
			}
			index = end
		case '(':
			if strings.HasPrefix(body[index:], "(?P<") || (strings.HasPrefix(body[index:], "(?<") && !strings.HasPrefix(body[index:], "(?<=") && !strings.HasPrefix(body[index:], "(?<!")) {
				count++
			} else if !strings.HasPrefix(body[index:], "(?") {
				count++
			}
			index++
		default:
			index++
		}
	}
	return count
}

// trailingInBranch reports whether the group closing at end is the last thing in
// its branch, so a lookahead there is a boundary of the whole match.
func trailingInBranch(src string, end int) bool {
	if end+1 >= len(src) {
		return true
	}
	switch src[end+1] {
	case ')', '|':
		return true
	}
	return false
}

// translateBody translates the inside of an assertion: no further assertions.
func translateBody(src string) (string, error) {
	body := &jsTranslator{groupOf: map[int]int{}}
	if err := body.walk(src, false); err != nil {
		return "", err
	}
	return body.out.String(), nil
}

func validGroupName(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_':
		default:
			return false
		}
	}
	return true
}

// matchingParen returns the index of the ")" closing the group that opens at i.
func matchingParen(src string, i int) (int, error) {
	depth := 0
	for j := i; j < len(src); j++ {
		switch src[j] {
		case '\\':
			j++
		case '[':
			end, err := classEnd(src, j)
			if err != nil {
				return 0, err
			}
			j = end - 1
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return j, nil
			}
		}
	}
	return 0, fmt.Errorf("unbalanced ( at %d", i)
}

// classEnd returns the index just past the character class starting at i.
func classEnd(src string, i int) (int, error) {
	j := i + 1
	if j < len(src) && src[j] == '^' {
		j++
	}
	if j < len(src) && src[j] == ']' {
		j++
	}
	for ; j < len(src); j++ {
		switch src[j] {
		case '\\':
			j++
		case ']':
			return j + 1, nil
		}
	}
	return 0, fmt.Errorf("unterminated character class at %d", i)
}

// translateClass rewrites one "[...]" class.
func translateClass(class string) (string, error) {
	inner := class[1 : len(class)-1]
	negated := strings.HasPrefix(inner, "^")
	if negated {
		inner = inner[1:]
	}
	if !negated && strings.Contains(inner, `\S`) {
		// [\s\S] is JavaScript's "any character, newlines included". A class that
		// already holds every non-space character holds everything.
		return "(?s:.)", nil
	}
	// [^\S\n] and [^\S\r] mean "JavaScript whitespace, but not a line break".
	// That is the only place \S appears inside a class in the packs.
	if negated && strings.Contains(inner, `\S`) {
		var removed []rune
		for i := 0; i < len(inner); {
			switch {
			case strings.HasPrefix(inner[i:], `\S`):
				i += 2
			case strings.HasPrefix(inner[i:], `\n`):
				removed = append(removed, '\n')
				i += 2
			case strings.HasPrefix(inner[i:], `\r`):
				removed = append(removed, '\r')
				i += 2
			default:
				r, size := utf8.DecodeRuneInString(inner[i:])
				removed = append(removed, r)
				i += size
			}
		}
		return "[" + subtractRunes(jsSpace, removed) + "]", nil
	}
	var out strings.Builder
	out.WriteByte('[')
	if negated {
		out.WriteByte('^')
	}
	for i := 0; i < len(inner); {
		if inner[i] == '\\' {
			written, size, err := translateEscape(inner[i:], true)
			if err != nil {
				return "", err
			}
			out.WriteString(written)
			i += size
			continue
		}
		out.WriteByte(inner[i])
		i++
	}
	out.WriteByte(']')
	return out.String(), nil
}

// subtractRunes renders a set of ranges minus some single runes as class
// contents.
func subtractRunes(set []charRange, removed []rune) string {
	var out strings.Builder
	for _, r := range set {
		for lo := r.lo; lo <= r.hi; {
			if containsRune(removed, lo) {
				lo++
				continue
			}
			hi := lo
			for hi+1 <= r.hi && !containsRune(removed, hi+1) {
				hi++
			}
			out.WriteString(runeClassPart(lo, hi))
			lo = hi + 1
		}
	}
	return out.String()
}

func containsRune(set []rune, r rune) bool {
	for _, item := range set {
		if item == r {
			return true
		}
	}
	return false
}

// runeClassPart renders one inclusive range as part of a class body.
func runeClassPart(lo, hi rune) string {
	printable := func(r rune) bool {
		return r >= 0x20 && r < 0x7f && r != '\\' && r != ']' && r != '^' && r != '-'
	}
	switch {
	case lo == hi && printable(lo):
		return string(lo)
	case lo == hi:
		return fmt.Sprintf(`\x{%x}`, lo)
	case hi == lo+1 && printable(lo) && printable(hi):
		return string(lo) + string(hi)
	case printable(lo):
		return fmt.Sprintf(`%c-\x{%x}`, lo, hi)
	default:
		return fmt.Sprintf(`\x{%x}-\x{%x}`, lo, hi)
	}
}

// translateEscape rewrites one backslash escape. inClass says it sits inside a
// character class, where \s expands without brackets.
func translateEscape(src string, inClass bool) (string, int, error) {
	if len(src) < 2 {
		return "", 0, fmt.Errorf("dangling backslash")
	}
	switch c := src[1]; c {
	case 's':
		if inClass {
			return classContents(jsSpace), 2, nil
		}
		return "[" + classContents(jsSpace) + "]", 2, nil
	case 'S':
		if inClass {
			return "", 0, fmt.Errorf(`\S inside a character class is not supported`)
		}
		return "[^" + classContents(jsSpace) + "]", 2, nil
	case 'u':
		// \uXXXX or \u{XXXX} -> \x{XXXX}
		if len(src) > 2 && src[2] == '{' {
			end := strings.IndexByte(src, '}')
			if end < 0 {
				return "", 0, fmt.Errorf("unterminated \\u{...}")
			}
			return `\x{` + src[3:end] + `}`, end + 1, nil
		}
		if len(src) < 6 {
			return "", 0, fmt.Errorf("short \\u escape")
		}
		value, err := strconv.ParseUint(src[2:6], 16, 32)
		if err != nil {
			return "", 0, fmt.Errorf("bad \\u escape %q", src[:6])
		}
		return fmt.Sprintf(`\x{%x}`, value), 6, nil
	case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
		return "", 0, fmt.Errorf("backreference \\%c is not supported", c)
	case 'p', 'P':
		if len(src) > 2 && src[2] == '{' {
			end := strings.IndexByte(src, '}')
			if end < 0 {
				return "", 0, fmt.Errorf("unterminated \\p{...}")
			}
			return src[:end+1], end + 1, nil
		}
		if len(src) > 2 {
			return src[:3], 3, nil
		}
		return "", 0, fmt.Errorf("short \\p escape")
	case 'x':
		if len(src) > 3 && src[2] == '{' {
			end := strings.IndexByte(src, '}')
			if end < 0 {
				return "", 0, fmt.Errorf("unterminated \\x{...}")
			}
			return src[:end+1], end + 1, nil
		}
		if len(src) < 4 {
			return "", 0, fmt.Errorf("short \\x escape")
		}
		return src[:4], 4, nil
	case 'A', 'z', 'b', 'B', 'd', 'D', 'w', 'W', 'n', 'r', 't', 'f', 'v', 'a':
		return src[:2], 2, nil
	case 'Q', 'E', 'c', 'k':
		return "", 0, fmt.Errorf(`\%c is not supported`, c)
	default:
		r, size := utf8.DecodeRuneInString(src[1:])
		return escapeLiteral(r), 1 + size, nil
	}
}

// escapeLiteral renders a rune so it stays literal in a pattern.
func escapeLiteral(r rune) string {
	if r < utf8.RuneSelf {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			return string(r)
		}
		return `\` + string(r)
	}
	return fmt.Sprintf(`\x{%x}`, r)
}

func classContents(set []charRange) string {
	var out strings.Builder
	for _, r := range set {
		out.WriteString(runeClassPart(r.lo, r.hi))
	}
	return out.String()
}

// unbounded is the saturating stand-in for an unlimited repetition in widthOf.
const unbounded = 1000

// widthOf is the number of runes a body can match, used to pick the windows a
// lookbehind is tested against. The maximum saturates at unbounded.
func widthOf(src string) (int, int) {
	lo, hi, _ := widthAlternation(src, 0)
	return lo, hi
}

// widthAlternation parses alternatives until the end of src or a ")".
func widthAlternation(src string, i int) (int, int, int) {
	bestLo, bestHi := 0, 0
	lo, hi := 0, 0
	first := true
	flush := func() {
		if first {
			bestLo, bestHi, first = lo, hi, false
			return
		}
		bestLo = minInt(bestLo, lo)
		bestHi = maxInt(bestHi, hi)
	}
	for i < len(src) {
		switch src[i] {
		case ')':
			flush()
			return bestLo, bestHi, i
		case '|':
			flush()
			lo, hi = 0, 0
			i++
			continue
		}
		atomLo, atomHi, next := widthAtom(src, i)
		i = next
		atomLo, atomHi, i = widthQuantified(src, i, atomLo, atomHi)
		lo += atomLo
		hi = addWidth(hi, atomHi)
	}
	flush()
	return bestLo, bestHi, i
}

// widthAtom parses one atom: an escape, a class, a group or a literal rune.
func widthAtom(src string, i int) (int, int, int) {
	switch {
	case i >= len(src):
		return 0, 0, i
	case src[i] == '\\':
		_, size, err := translateEscape(src[i:], false)
		if err != nil || size == 0 {
			_, size = utf8.DecodeRuneInString(src[i:])
		}
		return 1, 1, i + size
	case src[i] == '[':
		end, err := classEnd(src, i)
		if err != nil {
			return 1, 1, len(src)
		}
		return 1, 1, end
	case src[i] == '(':
		// Skip the group prefix ((?:, (?=, (?<name>, …) so its characters are not
		// counted as atoms.
		start := i + 1
		if start < len(src) && src[start] == '?' {
			switch {
			case strings.HasPrefix(src[start:], "?:") || strings.HasPrefix(src[start:], "?=") || strings.HasPrefix(src[start:], "?!"):
				start += 2
			case strings.HasPrefix(src[start:], "?<=") || strings.HasPrefix(src[start:], "?<!"):
				start += 3
			case strings.HasPrefix(src[start:], "?<"), strings.HasPrefix(src[start:], "?P<"):
				if closing := strings.IndexByte(src[start:], '>'); closing >= 0 {
					start += closing + 1
				}
			}
		}
		lo, hi, end := widthAlternation(src, start)
		if end < len(src) && src[end] == ')' {
			end++
		}
		return lo, hi, end
	default:
		_, size := utf8.DecodeRuneInString(src[i:])
		return 1, 1, i + size
	}
}

// widthQuantified applies one quantifier at i, if there is one.
func widthQuantified(src string, i, lo, hi int) (int, int, int) {
	if i >= len(src) {
		return lo, hi, i
	}
	switch src[i] {
	case '*':
		return 0, unbounded, i + 1
	case '+':
		return lo, unbounded, i + 1
	case '?':
		return 0, hi, i + 1
	case '{':
		end := strings.IndexByte(src[i:], '}')
		if end < 0 {
			return lo, hi, i
		}
		spec := src[i+1 : i+end]
		low, high := spec, spec
		if comma := strings.IndexByte(spec, ','); comma >= 0 {
			low, high = spec[:comma], spec[comma+1:]
		}
		minimum, err := strconv.Atoi(low)
		if err != nil {
			return lo, hi, i
		}
		maximum := minimum
		if high == "" {
			maximum = unbounded
		} else if parsed, err := strconv.Atoi(high); err == nil {
			maximum = parsed
		} else {
			return lo, hi, i
		}
		return times(minimum, lo), times(maximum, hi), i + end + 1
	}
	return lo, hi, i
}

func times(count, width int) int {
	if width == 0 {
		return 0
	}
	if count >= unbounded || width >= unbounded {
		return unbounded
	}
	return minInt(count*width, unbounded)
}

func addWidth(a, b int) int { return minInt(a+b, unbounded) }

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// findSubmatch is the first match's capture groups as strings, the way
// String.match returns them; nil when the pattern does not match. Group 0 is the
// whole match.
func (p *jsPattern) findSubmatch(text string) []string {
	var result []string
	iteratePattern(p, text, func(loc []int) bool {
		result = make([]string, 0, len(loc)/2)
		for group := 0; 2*group+1 < len(loc); group++ {
			if loc[2*group] < 0 {
				result = append(result, "")
				continue
			}
			result = append(result, text[loc[2*group]:loc[2*group+1]])
		}
		return false
	})
	return result
}
