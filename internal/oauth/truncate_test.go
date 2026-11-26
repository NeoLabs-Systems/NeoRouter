package oauth

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestNonASCIINamesAreCutByCharacter(t *testing.T) {
	for _, count := range []int{1, 27, 30, 50, 80, 81, 400} {
		name := appName("client", strings.Repeat("語", count))
		if got := utf8.RuneCountInString(name); got != min(count, 80) || strings.ContainsRune(name, 0) {
			t.Fatalf("%d characters: %d kept (%q)", count, got, name)
		}
	}
}
