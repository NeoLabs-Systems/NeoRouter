package media

import (
	"math"

	"github.com/NeoLabs-Systems/neorouter/internal/catalog"
)

// Cost per kind. Token-priced kinds (embeddings, System One) use the
// per-million-token price; media kinds use a unit price: USD per image, per
// second of video, per minute of audio input, per million characters of speech,
// or per web search or weather call. An unknown price is never free: budget
// keys skip such models.

// tokenCost is what tokens cost at the entry's per-million-token input price,
// nil when the price is unknown.
func tokenCost(entry catalog.Entry, tokens float64) *float64 {
	if entry.IsFree || entry.IsPlan {
		return zeroCost()
	}
	if entry.PriceIn == nil {
		return nil
	}
	cost := tokens * *entry.PriceIn / 1e6
	return &cost
}

// unitCost is what amount of unit costs, nil when the entry does not price that
// unit.
func unitCost(entry catalog.Entry, unit string, amount float64) *float64 {
	if entry.IsFree || entry.IsPlan {
		return zeroCost()
	}
	if entry.PriceUnit == nil || *entry.PriceUnit != unit || entry.UnitPrice == nil {
		return nil
	}
	quantity := amount
	if unit == "mchar" {
		quantity = amount / 1e6
	}
	cost := quantity * *entry.UnitPrice
	return &cost
}

// priced is the fallback for a model whose unit price is unknown but which is
// priced per token instead (Gemini bills speech per token).
func priced(unitValue, tokenValue *float64) *float64 {
	if unitValue != nil {
		return unitValue
	}
	return tokenValue
}

// estimateTokens is a request's size in tokens: about four characters each.
func estimateTokens(texts []string) int {
	total := 0
	for _, text := range texts {
		total += int(math.Ceil(float64(utf16Length(text)) / 4))
	}
	return total
}

// estimateAudioMinutes is a rough audio length from the upload size (128 kbit/s
// is about 1 MB per minute), used only for the worst-case budget reservation.
func estimateAudioMinutes(bytes int) float64 {
	return math.Max(0.1, float64(bytes)/960_000)
}

// tokensForAudio is Gemini's rough token count (about 32 tokens per second of
// audio), which sizes a transcription against a model's window.
func tokensForAudio(minutes float64) int {
	return int(math.Ceil(minutes * 60 * 32))
}

// zeroCost is the free/plan answer: a known price, of nothing.
func zeroCost() *float64 {
	zero := 0.0
	return &zero
}
