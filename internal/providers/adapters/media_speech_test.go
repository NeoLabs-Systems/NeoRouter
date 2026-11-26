package adapters

import (
	"strings"
	"testing"
)

// SpeechChunks is what makes a long text speakable on a host that caps one
// request (Groq's Orpheus takes 200 characters). Every part fits, and no text
// is lost.

// TestSpeechChunksFitsTheCap packs eight sentences and one very long word into
// parts of at most 200 characters.
func TestSpeechChunksFitsTheCap(t *testing.T) {
	const max = 200
	text := strings.Repeat("This sentence has a few words in it. ", 8) + strings.Repeat("x", 250) + "."
	chunks := SpeechChunks(strings.TrimSpace(text), max)
	if len(chunks) <= 2 {
		t.Fatalf("chunks = %d, want more than 2", len(chunks))
	}
	for _, chunk := range chunks {
		if mediaLength(chunk) > max {
			t.Fatalf("chunk of %d characters: %q", mediaLength(chunk), chunk)
		}
	}
	joined := strings.Join(chunks, " ")
	if want := strings.Join(strings.Fields(text), ""); strings.Join(strings.Fields(joined), "") != want {
		t.Fatal("text was lost between the parts")
	}
}

// TestSpeechChunksShortTextStaysOne: text under the cap is one request.
func TestSpeechChunksShortTextStaysOne(t *testing.T) {
	if got := SpeechChunks("Hi.", 200); len(got) != 1 || got[0] != "Hi." {
		t.Fatalf("chunks = %v", got)
	}
}

// TestSpeechChunksPacksSentences: sentences are packed together while they fit,
// which is fewer requests.
func TestSpeechChunksPacksSentences(t *testing.T) {
	got := SpeechChunks("One. Two. Three.", 10)
	want := []string{"One. Two.", "Three."}
	if len(got) != len(want) {
		t.Fatalf("chunks = %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("chunks = %v, want %v", got, want)
		}
	}
}

// TestSpeechChunksSplitsOnlyAtWhitespaceAfterASentence: the boundary is a
// sentence mark followed by whitespace.
func TestSpeechChunksSplitsOnlyAtWhitespaceAfterASentence(t *testing.T) {
	cases := map[string][]string{
		"a.b c":     {"a.b c"},
		"Hi. Bye":   {"Hi.", "Bye"},
		"Hi.  Bye":  {"Hi.", "Bye"},
		"1; 2: 3.":  {"1;", "2:", "3."},
		"no end":    {"no end"},
		"Trailing!": {"Trailing!"},
	}
	for text, want := range cases {
		got := mediaSentences(text)
		if len(got) != len(want) {
			t.Fatalf("mediaSentences(%q) = %v, want %v", text, got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("mediaSentences(%q) = %v, want %v", text, got, want)
			}
		}
	}
}

// TestMediaSplitEvery cuts a longer word wherever it must.
func TestMediaSplitEvery(t *testing.T) {
	got := mediaSplitEvery("abcdefg", 3)
	want := []string{"abc", "def", "g"}
	if len(got) != len(want) {
		t.Fatalf("pieces = %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("pieces = %v, want %v", got, want)
		}
	}
}
