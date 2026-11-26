package processing

import (
	"strings"
	"testing"
)

// The rule packs are JavaScript patterns; Go's RE2 engine has neither lookaround
// nor backreferences. These tests pin what compileJSPattern does instead, case by
// case, so a future change to the translator cannot quietly change what the shield
// hides.

// findAll is every match's text, the way matchAll would report it.
func findAll(t *testing.T, source, flags, text string) []string {
	t.Helper()
	pattern, err := compileJSPattern(source, flags)
	if err != nil {
		t.Fatalf("%s /%s/: %v", text, source, err)
	}
	out := []string{}
	iteratePattern(pattern, text, func(loc []int) bool {
		start, end := pattern.span(loc)
		out = append(out, text[start:end])
		return true
	})
	return out
}

// TestLookbehindBecomesACheck proves that a negative lookbehind is applied where
// the JavaScript applies it, including at the start of the text.
func TestLookbehindBecomesACheck(t *testing.T) {
	// /(?<!\d)\d{5,10}(?!\d)/: a run of digits that is exactly this long.
	cases := []struct {
		text string
		want string
	}{
		{"Kontonummer 1234567890", "1234567890"},
		{"ref 12345678901", ""}, // 11 digits: neither end is free
		{"a1234567890", ""},     // preceded by a letter, followed by the end: fits
		{"1234567890 2345678901", "1234567890,2345678901"},
		{"12345678901234567890", ""}, // 20 digits: no window of 5..10 is bounded
	}
	for _, test := range cases {
		got := strings.Join(findAll(t, `(?<!\d)\d{5,10}(?!\d)`, "", test.text), ",")
		if test.text == "a1234567890" {
			// The JavaScript matches here (the lookbehind only forbids a digit);
			// the Go port must agree.
			if got != "1234567890" {
				t.Errorf("%q: %q, want 1234567890", test.text, got)
			}
			continue
		}
		if got != test.want {
			t.Errorf("%q: %q, want %q", test.text, got, test.want)
		}
	}
}

// TestAlternativesStillBacktrack is the case a marker check alone gets wrong: the
// trailing lookahead must let the engine try a longer alternative.
func TestAlternativesStillBacktrack(t *testing.T) {
	// The context words of the ID card rule contain "ausweis" and
	// "ausweisnummer"; only the second one is a whole word here.
	pattern, err := wordRegex([]string{"ausweis", "personalausweis", "ausweisnummer"})
	if err != nil {
		t.Fatal(err)
	}
	if !pattern.matchesAnywhere("Ausweisnummer T220001293") {
		t.Fatal("the longer alternative must be found")
	}
	if !pattern.matchesAnywhere("Ausweis: T220001293") {
		t.Fatal("the short alternative must be found")
	}
	if pattern.matchesAnywhere("Personalausweisnummer") {
		t.Fatal("a word that only starts like a context word is no context")
	}
	if !pattern.matchesAnywhere("der Ausweis liegt hier") {
		t.Fatal("a whole word in a sentence counts")
	}
}

// TestTrailingBoundaryIsNotPartOfTheSpan proves the consumed boundary character is
// taken off the span again.
func TestTrailingBoundaryIsNotPartOfTheSpan(t *testing.T) {
	// /\b\d{3}\b/ written as a JavaScript boundary: the match must not swallow the
	// following space.
	if got := findAll(t, `(?<![\p{L}\p{N}])\d{3}(?![\p{L}\p{N}])`, "", "room 123 next"); strings.Join(got, ",") != "123" {
		t.Fatalf("span %q", got)
	}
	if got := findAll(t, `(?<![\p{L}\p{N}])\d{3}(?![\p{L}\p{N}])`, "", "123"); strings.Join(got, ",") != "123" {
		t.Fatalf("span at the start of the text %q", got)
	}
	if got := findAll(t, `(?<![\p{L}\p{N}])\d{3}(?![\p{L}\p{N}])`, "", "1234"); len(got) != 0 {
		t.Fatalf("digits inside a longer run are not a match: %q", got)
	}
}

// TestWhitespaceMatchesJavaScript proves the \s expansion: JavaScript's \s holds
// the no-break spaces, Go's does not.
func TestWhitespaceMatchesJavaScript(t *testing.T) {
	// /\bP\.?\s?O\.?\s?Box \d+/ in a text with no-break spaces.
	text := "P.\u00a0O.\u00a0Box 4711"
	if got := findAll(t, `P\.?[\s]?O\.?[\s]?Box \d+`, "", text); len(got) != 1 {
		t.Fatalf("no-break spaces are whitespace: %q", got)
	}
	// The same text through Go's own \s finds nothing.
	plain, err := compileJSPattern(`[^\S\n]+`, "")
	if err != nil {
		t.Fatal(err)
	}
	if !plain.matchesAnywhere("a\u00a0b") {
		t.Fatal("[^\\S\\n] means JavaScript whitespace without a line break")
	}
	if plain.matchesAnywhere("a\nb") {
		t.Fatal("[^\\S\\n] excludes the line break")
	}
}

// TestDotAndAnyCharacter proves the . and [\s\S] rewrites.
func TestDotAndAnyCharacter(t *testing.T) {
	// JavaScript's . stops at \n and also at \r and the line separators.
	if got := findAll(t, `a.b`, "", "a\rb"); len(got) != 0 {
		t.Fatalf(". must not match \\r: %q", got)
	}
	if got := findAll(t, `a.b`, "", "a\u2028b"); len(got) != 0 {
		t.Fatalf(". must not match U+2028: %q", got)
	}
	if got := findAll(t, `a.b`, "", "a b"); len(got) != 1 {
		t.Fatalf(". matches an ordinary character: %q", got)
	}
	// [\s\S] is "any character", newlines included.
	if got := findAll(t, `begin[\s\S]+?end`, "", "begin\n\nend"); len(got) != 1 {
		t.Fatalf("[\\s\\S] reaches across lines: %q", got)
	}
}

// TestCaptureGroupsKeepTheirNumber proves the renumbering: a rule's group number
// still names the same part of the match.
func TestCaptureGroupsKeepTheirNumber(t *testing.T) {
	// /(?<![\w.])(a+)(-)(b+)/: group 2 is the dash, group 3 the b's.
	pattern, err := compileJSPattern(`(?<![\w.])(a+)(-)(b+)`, "")
	if err != nil {
		t.Fatal(err)
	}
	loc := pattern.re.FindStringSubmatchIndex("xx aa-bbb")
	if loc == nil {
		t.Fatal("no match")
	}
	text := "xx aa-bbb"
	for jsGroup, want := range map[int]string{1: "aa", 2: "-", 3: "bbb"} {
		goGroup := pattern.groupOf[jsGroup]
		if got := text[loc[2*goGroup]:loc[2*goGroup+1]]; got != want {
			t.Errorf("group %d is %q, want %q", jsGroup, got, want)
		}
	}
	if pattern.groupCount != 3 {
		t.Errorf("groupCount %d", pattern.groupCount)
	}
}

// TestNamedGroupsWork proves a named group survives the rewrite (the gitleaks pack
// uses (?<secret>…)).
func TestNamedGroupsWork(t *testing.T) {
	pattern, err := compileJSPattern(`(?<!x)key=(?<secret>[A-Za-z0-9]{4,})`, "")
	if err != nil {
		t.Fatal(err)
	}
	loc := pattern.re.FindStringSubmatchIndex("akey=abcd")
	if loc == nil {
		t.Fatal("no match")
	}
	group := pattern.re.SubexpIndex("secret")
	if got := "akey=abcd"[loc[2*group]:loc[2*group+1]]; got != "abcd" {
		t.Fatalf("named group %q", got)
	}
}

// TestMultiLineAnchorsStayLineAnchors proves that ^ and $ keep their meaning: the
// pattern is matched against the whole text, not a tail of it.
func TestMultiLineAnchorsStayLineAnchors(t *testing.T) {
	// /^[ \t]*DB_PASSWORD[ \t]*:[ \t]*(.+)$/gm has to find the line in the middle of
	// a file, and must not treat the middle of a line as a line start.
	text := "first line\nDB_PASSWORD: hunter2\nlast line"
	got := findAll(t, `^[ \t]*DB_PASSWORD[ \t]*:[ \t]*(.+)$`, "m", text)
	if strings.Join(got, ",") != "DB_PASSWORD: hunter2" {
		t.Fatalf("line anchor: %q", got)
	}
	if got := findAll(t, `^[ \t]*DB_PASSWORD`, "m", "x DB_PASSWORD"); len(got) != 0 {
		t.Fatalf("the middle of a line is no line start: %q", got)
	}
}

// TestBackreferencesAreRefused proves the translator fails loudly instead of
// silently dropping a rule.
func TestBackreferencesAreRefused(t *testing.T) {
	if _, err := compileJSPattern(`(a)\1`, ""); err == nil {
		t.Fatal("a backreference must be refused")
	}
	if _, err := compileJSPattern(`(?=(?=a))`, ""); err == nil {
		t.Fatal("a nested assertion must be refused")
	}
}

// TestWidthOf proves the lookbehind window calculation.
func TestWidthOf(t *testing.T) {
	cases := []struct {
		body string
		min  int
		max  int
	}{
		{`[\w+]`, 1, 1},
		{`\d`, 1, 1},
		{`\p{L}\p{N}`, 2, 2},
		{`a?b`, 1, 2},
		{`a{2,4}`, 2, 4},
		{`(?:ab|abc)`, 2, 3},
		{`x*`, 0, unbounded},
	}
	for _, test := range cases {
		gotMin, gotMax := widthOf(test.body)
		if gotMin != test.min || gotMax != test.max {
			t.Errorf("%s: width (%d,%d), want (%d,%d)", test.body, gotMin, gotMax, test.min, test.max)
		}
	}
}
