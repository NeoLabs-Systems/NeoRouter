package processing

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

// spanCase is one text and the spans expected in it (testdata/span_cases.json).
type spanCase struct {
	Text       string   `json:"text"`
	AllowTerms []string `json:"allowTerms"`
	Entities   []Entity `json:"entities"`
	Expected   []struct {
		Category string `json:"category"`
		Value    string `json:"value"`
		RuleID   string `json:"ruleId"`
	} `json:"expected"`
}

func describeSpans(spans []Span) []string {
	out := make([]string, 0, len(spans))
	for _, span := range spans {
		out = append(out, span.Category+"|"+span.Value+"|"+span.RuleID)
	}
	return out
}

// TestSpansMatchTheRecordedSpans checks the whole detection stack: for every
// text in the file the spans must be the recorded ones, in the same order.
func TestSpansMatchTheRecordedSpans(t *testing.T) {
	engine := testEngine(t)
	var cases []spanCase
	readTestdata(t, "span_cases.json", &cases)
	if len(cases) < 100 {
		t.Fatalf("span_cases.json holds %d texts", len(cases))
	}
	modes := DefaultModes
	for _, test := range cases {
		spans := engine.Scan(test.Text, ScanOptions{Modes: modes, AllowTerms: test.AllowTerms, Entities: test.Entities})
		want := make([]string, 0, len(test.Expected))
		for _, expected := range test.Expected {
			want = append(want, expected.Category+"|"+expected.Value+"|"+expected.RuleID)
		}
		got := describeSpans(spans)
		if strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Errorf("text %q:\n got %q\nwant %q", test.Text, got, want)
		}
	}
}

// corpus is testdata/corpus.json: texts and the redaction expected for them.
type corpus struct {
	Benign []string `json:"benign"`
	Letter struct {
		Text     string   `json:"text"`
		Redacted string   `json:"redacted"`
		Counts   []string `json:"counts"`
		Findings []struct {
			Token    string `json:"token"`
			Category string `json:"category"`
			Label    Label  `json:"label"`
		} `json:"findings"`
	} `json:"letter"`
}

// TestEverydayTextGivesNoFalseAlarms covers ordinary text.
func TestEverydayTextGivesNoFalseAlarms(t *testing.T) {
	engine := testEngine(t)
	var fixture corpus
	readTestdata(t, "corpus.json", &fixture)
	if len(fixture.Benign) < 20 {
		t.Fatalf("corpus.json holds %d benign texts", len(fixture.Benign))
	}
	for _, text := range fixture.Benign {
		if spans := engine.Scan(text, ScanOptions{Modes: DefaultModes}); len(spans) != 0 {
			t.Errorf("%q should hold nothing private, found %q", text, describeSpans(spans))
		}
	}
}

// TestGermanLetter: first names stay, everything identifying is hidden, and the
// same name gets the same placeholder.
func TestGermanLetter(t *testing.T) {
	engine := testEngine(t)
	var fixture corpus
	readTestdata(t, "corpus.json", &fixture)
	letter := fixture.Letter.Text
	redaction := RedactRequest(engine, Canonical{"messages": []any{map[string]any{"role": "user", "content": letter}}}, RequestRedactOptions{Modes: DefaultModes})
	if redaction == nil {
		t.Fatalf("the letter holds private values")
	}
	messages := redaction.Canonical.Messages()
	sent, _ := messages[len(messages)-1]["content"].(string)
	for _, hidden := range []string{"Schneider", "Becker", "+49 170", "14.07.1988", "Lindenstraße 12", "86 095 742 719", "DE136695976", "DE89 3704", "K-JB 1234", "50.9375"} {
		if strings.Contains(sent, hidden) {
			t.Errorf("%q should be hidden: %s", hidden, sent)
		}
	}
	if !strings.HasPrefix(sent, "Sehr geehrte Frau [LASTNAME_1],") {
		t.Errorf("the salutation keeps the placeholder: %s", sent)
	}
	if !strings.Contains(sent, "mein Name ist Jonas [LASTNAME_2]") {
		t.Errorf("the first name stays: %s", sent)
	}
	if !strings.HasSuffix(sent, "\nJonas [LASTNAME_2]") {
		t.Errorf("the same name gets the same placeholder: %s", sent)
	}
	counts := make([]string, 0, len(redaction.Vault.Counts))
	for category := range redaction.Vault.Counts {
		counts = append(counts, category)
	}
	if strings.Join(sortedStrings(counts), ",") != strings.Join(fixture.Letter.Counts, ",") {
		t.Fatalf("categories %v, want %v", sortedStrings(counts), fixture.Letter.Counts)
	}
	if sent != fixture.Letter.Redacted {
		t.Errorf("redacted letter:\n got %q\nwant %q", sent, fixture.Letter.Redacted)
	}
	findings := redaction.Vault.Findings()
	if len(findings) != len(fixture.Letter.Findings) {
		t.Fatalf("%d findings, want %d", len(findings), len(fixture.Letter.Findings))
	}
	for index, want := range fixture.Letter.Findings {
		got := findings[index]
		if got.Token != want.Token || got.Category != want.Category || got.Label != want.Label {
			t.Errorf("finding %d: got %+v, want %+v", index, got, want)
		}
	}
}

func sortedStrings(values []string) []string {
	out := append([]string{}, values...)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// TestAllowTermsKeepValues covers allow terms.
func TestAllowTermsKeepValues(t *testing.T) {
	engine := testEngine(t)
	text := "Write to support@acme.example or call +49 170 1234567"
	categories := func(allow []string) []string {
		out := []string{}
		for _, span := range engine.Scan(text, ScanOptions{Modes: DefaultModes, AllowTerms: allow}) {
			out = append(out, span.Category)
		}
		return out
	}
	if got := categories([]string{"@acme.example"}); strings.Join(got, ",") != "phone" {
		t.Fatalf("allowing the domain leaves the phone: %v", got)
	}
	if got := categories([]string{"+49 170 1234567"}); strings.Join(got, ",") != "email" {
		t.Fatalf("allowing the phone leaves the email: %v", got)
	}
}

// TestAIEntitiesKeepTheFirstName covers names found by the model.
func TestAIEntitiesKeepTheFirstName(t *testing.T) {
	engine := testEngine(t)
	spans := engine.Scan("Termin mit Lena Wojtowicz", ScanOptions{
		Modes:    DefaultModes,
		Entities: []Entity{{Text: "Lena Wojtowicz", Category: "person"}},
	})
	values := make([]string, 0, len(spans))
	for _, span := range spans {
		values = append(values, span.Value)
	}
	if strings.Join(values, ",") != "Wojtowicz" {
		t.Fatalf("spans %v, want the family name only", values)
	}
}

// TestScanCache: a new text scans quickly and a repeat comes from the cache.
func TestScanCache(t *testing.T) {
	engine := testEngine(t)
	var fixture corpus
	readTestdata(t, "corpus.json", &fixture)
	block := strings.Join(fixture.Benign, "\n") + "\nMail jana@example.org, IBAN DE89 3704 0044 0532 0130 00, Herr Becker.\n"
	text := ""
	for len(text) < 20_000 {
		text += block
	}
	text = string([]rune(text)[:20_000])
	engine.ClearCache()
	engine.Scan(text+"warm-up", ScanOptions{Modes: DefaultModes})
	started := time.Now()
	engine.Scan(text, ScanOptions{Modes: DefaultModes})
	cold := time.Since(started)
	started = time.Now()
	engine.Scan(text, ScanOptions{Modes: DefaultModes})
	cached := time.Since(started)
	t.Logf("20k characters: %v new, %v cached", cold, cached)
	// The bound is 250 ms for 20 000 characters (relaxed under -race; see
	// scanbound_test.go).
	if cold > scanColdBound {
		t.Fatalf("cold scan took %v", cold)
	}
	if cached > cold {
		t.Fatalf("cached scan %v is not faster than a new one %v", cached, cold)
	}
}

// TestSpansAreJSONShaped keeps the wire shape of a span stable.
func TestSpansAreJSONShaped(t *testing.T) {
	engine := testEngine(t)
	spans := engine.Scan("Mail anna.schmidt@example.de now", ScanOptions{Modes: DefaultModes})
	encoded, err := json.Marshal(spans)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"start":`, `"end":`, `"category":"email"`, `"value":"anna.schmidt@example.de"`, `"ruleId":"email"`} {
		if !strings.Contains(string(encoded), key) {
			t.Fatalf("%s missing from %s", key, encoded)
		}
	}
}

func TestDefaultModesCoverEveryCategory(t *testing.T) {
	for _, category := range Categories {
		if mode, ok := DefaultModes[category]; !ok || (mode != "hide" && mode != "restore") {
			t.Errorf("category %s has mode %q", category, mode)
		}
	}
	if got := fmt.Sprintf("%v", AIOnlyCategories()); got != "[organization]" {
		t.Errorf("AI-only categories %s", got)
	}
}
