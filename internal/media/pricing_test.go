package media

import (
	"testing"

	"github.com/NeoLabs-Systems/neorouter/internal/catalog"
)

func floatPtr(value float64) *float64 { return &value }
func intPtr(value int) *int           { return &value }
func stringPtr(value string) *string  { return &value }

func TestTokenCost(t *testing.T) {
	free := catalog.Entry{IsFree: true, PriceIn: floatPtr(3)}
	if cost := tokenCost(free, 1000); cost == nil || *cost != 0 {
		t.Fatalf("a free model costs nothing, got %v", cost)
	}
	plan := catalog.Entry{IsPlan: true, PriceIn: floatPtr(3)}
	if cost := tokenCost(plan, 1000); cost == nil || *cost != 0 {
		t.Fatalf("a plan model costs nothing, got %v", cost)
	}
	unknown := catalog.Entry{}
	if cost := tokenCost(unknown, 1000); cost != nil {
		t.Fatalf("an unknown price is nil, got %v", *cost)
	}
	priced := catalog.Entry{PriceIn: floatPtr(3)}
	if cost := tokenCost(priced, 1000); cost == nil || *cost != 0.003 {
		t.Fatalf("1000 tokens at 3 USD per million = 0.003, got %v", cost)
	}
}

func TestUnitCost(t *testing.T) {
	image := catalog.Entry{PriceUnit: stringPtr("image"), UnitPrice: floatPtr(0.04)}
	if cost := unitCost(image, "image", 2); cost == nil || *cost != 0.08 {
		t.Fatalf("two images at 0.04 = 0.08, got %v", cost)
	}
	if cost := unitCost(image, "second", 2); cost != nil {
		t.Fatalf("a unit the entry does not price is nil, got %v", *cost)
	}
	characters := catalog.Entry{PriceUnit: stringPtr("mchar"), UnitPrice: floatPtr(15)}
	if cost := unitCost(characters, "mchar", 1_000_000); cost == nil || *cost != 15 {
		t.Fatalf("a million characters at 15 = 15, got %v", cost)
	}
	if cost := unitCost(catalog.Entry{PriceUnit: stringPtr("image")}, "image", 1); cost != nil {
		t.Fatalf("an entry without a unit price is nil, got %v", *cost)
	}
	if cost := unitCost(catalog.Entry{IsFree: true, PriceUnit: stringPtr("image"), UnitPrice: floatPtr(1)}, "image", 1); cost == nil || *cost != 0 {
		t.Fatalf("a free entry costs nothing, got %v", cost)
	}
}

func TestPricedFallsBackToTokens(t *testing.T) {
	units := floatPtr(1)
	tokens := floatPtr(2)
	if got := priced(units, tokens); got != units {
		t.Fatalf("a known unit price wins, got %v", *got)
	}
	if got := priced(nil, tokens); got != tokens {
		t.Fatalf("an unknown unit price falls back to the token price, got %v", *got)
	}
	if got := priced(nil, nil); got != nil {
		t.Fatalf("both unknown is unknown, got %v", *got)
	}
}

func TestEstimateTokensCountsFourCharactersEach(t *testing.T) {
	if got := estimateTokens([]string{"hello"}); got != 2 {
		t.Fatalf("hello = %d, want 2", got)
	}
	if got := estimateTokens([]string{"abcd"}); got != 1 {
		t.Fatalf("abcd = %d, want 1", got)
	}
	if got := estimateTokens([]string{"ab", "abcdefgh"}); got != 3 {
		t.Fatalf("two texts = %d, want 3", got)
	}
	// String.prototype.length counts UTF-16 code units, so an emoji is two.
	if got := estimateTokens([]string{"😀😀😀😀"}); got != 2 {
		t.Fatalf("four emoji = %d, want 2", got)
	}
}

func TestEstimateAudioMinutes(t *testing.T) {
	if got := estimateAudioMinutes(960_000); got != 1 {
		t.Fatalf("960000 bytes = %v minutes, want 1", got)
	}
	if got := estimateAudioMinutes(100); got != 0.1 {
		t.Fatalf("a tiny upload still reserves 0.1 minutes, got %v", got)
	}
}

func TestTokensForAudio(t *testing.T) {
	if got := tokensForAudio(1); got != 1920 {
		t.Fatalf("a minute of audio = %d tokens, want 1920", got)
	}
	if got := tokensForAudio(2.5 / 60); got != 80 {
		t.Fatalf("2.5 seconds = %d tokens, want 80", got)
	}
}
