package providers

import (
	"encoding/json"
	"testing"
)

// TestNormalizeModel replays every recorded listing entry and requires the same
// record.
func TestNormalizeModel(t *testing.T) {
	cases := loadFixture(t).NormalizeModel
	if len(cases) == 0 {
		t.Fatal("no model fixtures")
	}
	for _, testCase := range cases {
		name := jsString(testCase.Raw["id"])
		t.Run(name, func(t *testing.T) {
			overrides := ModelOverrides{
				Kind:          testCase.Overrides.Kind,
				ContextWindow: intPointer(testCase.Overrides.ContextWindow),
				MaxOutput:     intPointer(testCase.Overrides.MaxOutput),
				Tools:         testCase.Overrides.Tools,
				Vision:        testCase.Overrides.Vision,
				JSON:          testCase.Overrides.JSON,
				Reasoning:     testCase.Overrides.Reasoning,
				PriceIn:       testCase.Overrides.PriceIn,
				PriceOut:      testCase.Overrides.PriceOut,
				PriceKnown:    testCase.Overrides.PriceKnown,
				FreeHint:      testCase.Overrides.FreeHint,
			}
			got := genericOf(t, NormalizeModel(testCase.Raw, overrides))
			want := generic(t, testCase.Expect)
			if diff := diffValues("", got, want); diff != "" {
				t.Fatalf("%s", diff)
			}
		})
	}
}

// TestDetectKindKeepsEdgeCases covers the names that are easy to get wrong.
func TestDetectKindKeepsEdgeCases(t *testing.T) {
	cases := []struct{ id, kind string }{
		{"gpt-4.1", KindChat},
		{"text-embedding-3-large", KindEmbedding},
		{"bge-m3", KindEmbedding},
		{"rerank-v2", KindRerank},
		{"whisper-large-v3", KindTranscription},
		{"gpt-4o-mini-tts", KindSpeech},
		{"gpt-live", KindLive},
		{"realtime-preview", KindLive},
		{"lyria-002", KindOther},
		{"video-edit-pro", KindOther},
		{"dall-e-3", KindImage},
		// "diffusiongemma" is a text model; any other diffusion name is an image model.
		{"diffusiongemma-9b", KindChat},
		{"stable-diffusion-xl", KindImage},
		{"flux-pro", KindImage},
		{"veo-3", KindVideo},
		{"openrouter/auto", KindRouter},
		{"jev-router", KindRouter},
		{"llama-guard-4", KindModeration},
		{"deep-research-pro", KindOther},
		{"gpt-4o-ocr", KindOther},
	}
	for _, testCase := range cases {
		if got := DetectKind(testCase.id, nil); got != testCase.kind {
			t.Fatalf("%s: kind = %q, want %q", testCase.id, got, testCase.kind)
		}
	}
	// A declared type wins over the name.
	if got := DetectKind("gpt-4o", map[string]any{"type": "embedding"}); got != KindEmbedding {
		t.Fatalf("declared kind = %q", got)
	}
	if got := DetectKind("mystery", map[string]any{"architecture": map[string]any{"output_modalities": []any{"image"}}}); got != KindImage {
		t.Fatalf("modality kind = %q", got)
	}
}

// TestParseUnitPricing reads the media unit prices.
func TestParseUnitPricing(t *testing.T) {
	skus := ParseUnitPricing(map[string]any{"pricing_skus": map[string]any{
		"duration_seconds_720p":              0.04,
		"duration_seconds_1080p":             0.09,
		"cents_per_video_output_second_480p": 2.0,
		"input_reference_seconds_720p":       5.0,
	}})
	if skus.Unit == nil || *skus.Unit != "second" || skus.Price == nil || *skus.Price != 0.02 {
		t.Fatalf("skus = %+v", skus)
	}
	for _, testCase := range []struct {
		raw  map[string]any
		unit string
	}{
		{map[string]any{"price_per_minute": 0.6}, "minute"},
		{map[string]any{"price_per_mchar": 2.0}, "mchar"},
		{map[string]any{"price_per_search": 0.005}, "search"},
		{map[string]any{"price_per_call": 0.001}, "call"},
		{map[string]any{"pricing": map[string]any{"per_image": 0.04}}, "image"},
	} {
		got := ParseUnitPricing(testCase.raw)
		if got.Unit == nil || *got.Unit != testCase.unit || got.Price == nil {
			t.Fatalf("%v = %+v", testCase.raw, got)
		}
	}
	if got := ParseUnitPricing(map[string]any{}); got.Unit != nil || got.Price != nil {
		t.Fatalf("empty = %+v", got)
	}
}

// TestModelRecordJSON keeps the field names the catalog API and the database
// rely on.
func TestModelRecordJSON(t *testing.T) {
	model := NormalizeModel(map[string]any{"id": "m1", "context_length": 1000}, ModelOverrides{})
	encoded, err := json.Marshal(model)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	var fields map[string]any
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, name := range []string{
		"id", "name", "kind", "contextWindow", "maxOutput", "priceIn", "priceOut", "priceKnown",
		"freeHint", "priceCachedIn", "priceCacheWrite", "priceUnit", "unitPrice", "tools", "vision",
		"json", "reasoning", "decision", "paidOnly", "raw",
	} {
		if _, ok := fields[name]; !ok {
			t.Fatalf("record is missing %q: %s", name, encoded)
		}
	}
}

// TestToNumber reads the prices and sizes the listings report.
func TestToNumber(t *testing.T) {
	for _, testCase := range []struct {
		value  any
		expect *float64
	}{
		{"$1,200", float(1200)},
		{" 3 ", float(3)},
		{"", nil},
		{nil, nil},
		{2.5, float(2.5)},
		{"abc", nil},
		{true, nil},
	} {
		got := ToNumber(testCase.value)
		if (got == nil) != (testCase.expect == nil) {
			t.Fatalf("%v: got %v, want %v", testCase.value, got, testCase.expect)
		}
		if got != nil && *got != *testCase.expect {
			t.Fatalf("%v: got %v, want %v", testCase.value, *got, *testCase.expect)
		}
	}
}
