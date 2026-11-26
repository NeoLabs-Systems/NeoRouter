package processing

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testRegistry is the embedded pack build, built once per test binary.
func testRegistry(t *testing.T) *Registry {
	t.Helper()
	registry, err := NewRegistry()
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	return registry
}

// testEngine is an engine over the embedded packs.
func testEngine(t *testing.T) *Engine {
	t.Helper()
	return NewEngine(testRegistry(t))
}

// readTestdata decodes internal/processing/testdata/<name> into dst.
func readTestdata(t *testing.T, name string, dst any) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read testdata/%s: %v", name, err)
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		t.Fatalf("decode testdata/%s: %v", name, err)
	}
}

// ruleExamples is one rule's or detector's own examples
// (testdata/shield_rules.json).
type ruleExamples struct {
	ID     string      `json:"id"`
	Kind   string      `json:"kind"`
	File   string      `json:"file"`
	Match  []TestMatch `json:"match"`
	Ignore []string    `json:"ignore"`
}

// TestRulesFindTheirExamples: every rule and detector must find its own
// examples and nothing in the texts it must ignore.
func TestRulesFindTheirExamples(t *testing.T) {
	engine := testEngine(t)
	registry := engine.Registry()
	var examples []ruleExamples
	readTestdata(t, "shield_rules.json", &examples)
	if len(examples) < 290 {
		t.Fatalf("shield_rules.json holds %d items, the packs carry 298", len(examples))
	}
	rules := map[string]*Rule{}
	for _, rule := range registry.Rules() {
		rules[rule.ID] = rule
	}
	detectors := map[string]*Detector{}
	for _, detector := range registry.Detectors() {
		detectors[detector.ID] = detector
	}
	for _, item := range examples {
		if item.ID == "ai_entities" || item.ID == "custom_terms" {
			continue
		}
		t.Run(item.ID, func(t *testing.T) {
			scan := func(text string) []string {
				var spans []Span
				if item.Kind == "detector" {
					detector := detectors[item.ID]
					if detector == nil {
						t.Fatalf("detector %s is not in the registry", item.ID)
					}
					spans = engine.ScanOneDetector(detector, text)
				} else {
					rule := rules[item.ID]
					if rule == nil {
						t.Fatalf("rule %s is not in the registry", item.ID)
					}
					spans = engine.ScanOne(rule, text)
				}
				values := make([]string, 0, len(spans))
				for _, span := range spans {
					values = append(values, span.Value)
				}
				return values
			}
			for _, example := range item.Match {
				values := scan(example.Text)
				if len(values) == 0 {
					t.Errorf("%s should match: %q", item.ID, example.Text)
					continue
				}
				if example.Value != "" && !contains(values, example.Value) {
					t.Errorf("%s found %q in %q, expected %q", item.ID, values, example.Text, example.Value)
				}
			}
			for _, text := range item.Ignore {
				if values := scan(text); len(values) != 0 {
					t.Errorf("%s should ignore %q, found %q", item.ID, text, values)
				}
			}
		})
	}
}

// TestMainPacksAreCovered: every handwritten pack carries at least one example
// (the gitleaks import does not).
func TestMainPacksAreCovered(t *testing.T) {
	registry := testRegistry(t)
	untested := []string{}
	for _, rule := range registry.Rules() {
		if rule.File == "pack_gitleaks.json" {
			continue
		}
		if len(rule.Tests.Match) == 0 {
			untested = append(untested, rule.ID)
		}
	}
	if len(untested) > 0 {
		t.Fatalf("rules without examples: %s", strings.Join(untested, ", "))
	}
}

// TestBrokenPacksFailAtLoadTime covers broken packs.
func TestBrokenPacksFailAtLoadTime(t *testing.T) {
	noDetectors := []*Detector{}
	build := func(name, pack string) error {
		_, err := BuildRegistry(BuildOptions{
			Packs:     []PackSource{{Name: name, Data: []byte(pack)}},
			Detectors: noDetectors,
		})
		return err
	}
	rule := func(id string) string {
		return `{"id":"` + id + `","label":"X","category":"email","pattern":"x"}`
	}
	cases := []struct {
		name string
		pack string
		want string
	}{
		{"duplicate ids", `{"rules":[` + rule("same") + `,` + rule("same") + `]}`, "Duplicate shield rule id same"},
		{"unknown category", `{"rules":[{"id":"x","label":"X","category":"nope","pattern":"x"}]}`, "Invalid shield rule"},
		{"unknown validator", `{"rules":[{"id":"x","label":"X","category":"email","pattern":"x","validator":"nope"}]}`, "Invalid shield rule"},
		{"invalid pattern", `{"rules":[{"id":"x","label":"X","category":"email","pattern":"("}]}`, "invalid pattern"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			err := build("a.json", test.pack)
			if err == nil {
				t.Fatalf("expected an error for %s", test.name)
			}
			if !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error %q does not contain %q", err, test.want)
			}
		})
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
