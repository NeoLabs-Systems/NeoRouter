package catalog

import "testing"

func TestCanonicalIDsUnifyTheSameModelAcrossHosts(t *testing.T) {
	cases := []struct{ left, right string }{
		{"llama-3.3-70b-versatile", "meta-llama/llama-3.3-70b-instruct:free"},
		{"@cf/meta/llama-3.3-70b-instruct-fp8-fast", "llama3.3:70b"},
		{"claude-3-5-sonnet-20241022", "claude-3.5-sonnet"},
	}
	for _, testCase := range cases {
		if got, want := canonicalID(testCase.left), canonicalID(testCase.right); got != want {
			t.Errorf("canonicalID(%q) = %q, canonicalID(%q) = %q; want equal", testCase.left, got, testCase.right, want)
		}
	}
	if got := canonicalID("gpt-4o-2024-08-06"); got != "gpt4o" {
		t.Errorf("canonicalID(gpt-4o-2024-08-06) = %q, want gpt4o", got)
	}
	if canonicalID("gpt-4o") == canonicalID("gpt-4o-mini") {
		t.Error("gpt-4o and gpt-4o-mini must not share a canonical id")
	}
}

func TestParameterCount(t *testing.T) {
	cases := []struct {
		id   string
		want *float64
	}{
		{"mixtral-8x7b", fp(56)},
		{"llama-3.3-70b-versatile", fp(70)},
		{"gpt-4o", nil},
		{"a3b", nil}, // "a" before the number is a letter, so no size
		{"qwen3-30b-a3b", fp(30)},
	}
	for _, testCase := range cases {
		got := parameterCount(testCase.id)
		switch {
		case got == nil && testCase.want == nil:
		case got == nil || testCase.want == nil:
			t.Errorf("parameterCount(%q) = %v, want %v", testCase.id, got, testCase.want)
		case *got != *testCase.want:
			t.Errorf("parameterCount(%q) = %v, want %v", testCase.id, *got, *testCase.want)
		}
	}
}

func TestCanonicalIDFallsBackToTheInput(t *testing.T) {
	if got := canonicalID(""); got != "" {
		t.Errorf("canonicalID(\"\") = %q, want empty", got)
	}
	if got := canonicalID("///"); got != "///" {
		t.Errorf("canonicalID(///) = %q, want ///", got)
	}
}
