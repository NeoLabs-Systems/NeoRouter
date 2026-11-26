package providers

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// The fixtures record the expected answers for fixed inputs, in
// testdata/behaviour_fixtures.json.

type fixture struct {
	Classify         []classifyCase  `json:"classify"`
	AnnouncedReturn  []announcedCase `json:"announcedReturn"`
	LearnFromMessage []learnCase     `json:"learnFromMessage"`
	ExtractMessage   []extractCase   `json:"extractMessage"`
	AllowanceUsedUp  []allowanceCase `json:"allowanceUsedUp"`
	NormalizeModel   []modelCase     `json:"normalizeModel"`
	DerivedSession   []sessionCase   `json:"derivedSession"`
	Quota            []quotaCase     `json:"quota"`
	Waits            waitsCase       `json:"waits"`
	PublicDefinition []publicDefCase `json:"publicDefinition"`
}

type classifyExpect struct {
	Kind         string        `json:"kind"`
	Message      string        `json:"message"`
	RetryAfterMS *float64      `json:"retryAfterMs"`
	NeedsAction  bool          `json:"needsAction"`
	Learned      *LearnedLimit `json:"learned"`
}

type classifyCase struct {
	Status  int               `json:"status"`
	Body    any               `json:"body"`
	Headers map[string]string `json:"headers"`
	Expect  classifyExpect    `json:"expect"`
}

type announcedCase struct {
	Message string   `json:"message"`
	Now     int64    `json:"now"`
	Expect  *float64 `json:"expect"`
}

type learnCase struct {
	Message string        `json:"message"`
	Expect  *LearnedLimit `json:"expect"`
}

type extractCase struct {
	Body   any    `json:"body"`
	Expect string `json:"expect"`
}

type allowanceCase struct {
	Message string `json:"message"`
	Expect  bool   `json:"expect"`
}

type overridesCase struct {
	Kind          *string  `json:"kind"`
	ContextWindow *float64 `json:"contextWindow"`
	MaxOutput     *float64 `json:"maxOutput"`
	Tools         *bool    `json:"tools"`
	Vision        *bool    `json:"vision"`
	JSON          *bool    `json:"json"`
	Reasoning     *bool    `json:"reasoning"`
	PriceIn       *float64 `json:"priceIn"`
	PriceOut      *float64 `json:"priceOut"`
	PriceKnown    *bool    `json:"priceKnown"`
	FreeHint      *bool    `json:"freeHint"`
}

type modelCase struct {
	Raw       map[string]any  `json:"raw"`
	Overrides overridesCase   `json:"overrides"`
	Expect    json.RawMessage `json:"expect"`
}

type sessionCase struct {
	Canonical json.RawMessage `json:"canonical"`
	Salt      string          `json:"salt"`
	Expect    string          `json:"expect"`
}

type quotaCase struct {
	Parser string          `json:"parser"`
	Body   any             `json:"body"`
	Expect json.RawMessage `json:"expect"`
}

type waitsCase struct {
	SlotWaitMS    int64 `json:"slotWaitMs"`
	SlotMaxWaitMS int64 `json:"slotMaxWaitMs"`
}

type publicDefCase struct {
	ID     string          `json:"id"`
	Expect json.RawMessage `json:"expect"`
}

// loadFixture reads testdata/behaviour_fixtures.json.
func loadFixture(t *testing.T) fixture {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "behaviour_fixtures.json"))
	if err != nil {
		t.Fatalf("read fixtures: %v", err)
	}
	var out fixture
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode fixtures: %v", err)
	}
	return out
}

// generic decodes JSON into plain maps and slices, for value comparisons that
// ignore key order and Go's typing.
func generic(t *testing.T, raw []byte) any {
	t.Helper()
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
	return out
}

// genericOf marshals a value and decodes it back, so it can be compared with a
// fixture the same way.
func genericOf(t *testing.T, value any) any {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	return generic(t, raw)
}

// intPointer converts an optional number to an optional int.
func intPointer(value *float64) *int {
	if value == nil {
		return nil
	}
	out := int(*value)
	return &out
}

// float returns a pointer to value.
func float(value float64) *float64 { return &value }

// boolean returns a pointer to value.
func boolean(value bool) *bool { return &value }
