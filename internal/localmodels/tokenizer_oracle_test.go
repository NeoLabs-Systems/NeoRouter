package localmodels

// Parity with the oracle: the token ids transformers.js's AutoTokenizer produced
// for whisper-base (onnx-community/whisper-base) on the same texts. The ids are
// recorded here, so the comparison needs no network and no Node at test time:
//
//	const tok = await AutoTokenizer.from_pretrained(...)
//	Array.from((await tok(text)).input_ids.data, Number)
//
// The text tokens sit between the prefix transformers.js adds (start of
// transcript, no timestamps) and the end token, so that prefix and suffix are
// stripped before comparing: this package's tokenizer is the byte-level BPE
// underneath, and whisper.go adds its own forced prefix.

import (
	"os"
	"strings"
	"testing"
)

func TestTokenizerMatchesTheTransformersJSOracle(t *testing.T) {
	path := os.Getenv("NEOROUTER_TEST_WHISPER_TOKENIZER")
	if path == "" {
		t.Skip("set NEOROUTER_TEST_WHISPER_TOKENIZER to a whisper tokenizer.json to compare")
	}
	tokens, err := LoadTokenizer(path)
	if err != nil {
		t.Fatalf("load tokenizer: %v", err)
	}
	const (
		startOfTranscript = 50258
		notimestamps      = 50363
		endOfText         = 50257
	)
	cases := []struct {
		text string
		ids  []int32
	}{
		// " Hello world, it's 42!"
		{" Hello world, it's 42!", []int32{2425, 1002, 11, 309, 311, 14034, 0}},
		// "This is a sentence."
		{"This is a sentence.", []int32{5723, 307, 257, 8174, 13}},
		// "Grüße aus Berlin — 100% sicher?"
		{"Grüße aus Berlin — 100% sicher?", []int32{20038, 774, 11451, 3437, 13848, 3466, 2319, 4, 18623, 30}},
		// "  spaced  out  "
		{"  spaced  out  ", []int32{220, 43766, 220, 484, 220, 220}},
		// "don't stop"
		{"don't stop", []int32{13966, 380, 1590}},
		// "Hello, world!"
		{"Hello, world!", []int32{15947, 11, 1002, 0}},
		// "2026 and 3.5% growth"
		{"2026 and 3.5% growth", []int32{2009, 10880, 293, 805, 13, 20, 4, 4599}},
		// "日本語のテスト"
		{"日本語のテスト", []int32{27311, 31348, 2972, 22985, 40498}},
		// "  "
		{"  ", []int32{220, 220}},
		// "line\nbreak\ttab"
		{"line\nbreak\ttab", []int32{1889, 198, 13225, 197, 83, 455}},
	}
	_ = startOfTranscript
	_ = notimestamps
	_ = endOfText
	for _, tc := range cases {
		got := tokens.Encode(tc.text)
		// Drop the forced prefix and end token when the tokenizer emits them.
		filtered := []int32{}
		for _, id := range got {
			if id == startOfTranscript || id == notimestamps || id == endOfText {
				continue
			}
			filtered = append(filtered, id)
		}
		if len(filtered) != len(tc.ids) {
			t.Fatalf("%q: ids = %v, want %v", tc.text, filtered, tc.ids)
		}
		for index := range filtered {
			if filtered[index] != tc.ids[index] {
				t.Fatalf("%q: ids = %v, want %v", tc.text, filtered, tc.ids)
			}
		}
		decoded := tokens.Decode(tc.ids)
		if strings.TrimSpace(decoded) != strings.TrimSpace(tc.text) {
			t.Fatalf("%q decodes to %q", tc.text, decoded)
		}
	}
}
