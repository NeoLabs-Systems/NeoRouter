package routing

import (
	"math"
	"math/rand"
	"strconv"
	"unicode/utf16"
)

// utf16Len is the length in UTF-16 code units.
func utf16Len(s string) int { return len(utf16.Encode([]rune(s))) }

func formatRef(ref int64) string { return strconv.FormatInt(ref, 10) }

func randFloat64() float64 { return rand.Float64() }

func pow(base, exponent float64) float64 { return math.Pow(base, exponent) }

func minf(a, b float64) float64 { return math.Min(a, b) }
func maxf(a, b float64) float64 { return math.Max(a, b) }

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func minInt64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

// clamp01 is value clamped to 0..1.
func clamp01(value float64) float64 { return math.Max(0, math.Min(1, value)) }

// logScale maps a value onto 0..1 on a log scale; ok is false for a value that
// is not finite or not positive.
func logScale(value, low, high float64) (float64, bool) {
	if math.IsNaN(value) || math.IsInf(value, 0) || value <= 0 {
		return 0, false
	}
	return clamp01((math.Log(value) - math.Log(low)) / (math.Log(high) - math.Log(low))), true
}

func roundTo(value float64, digits int) float64 {
	scale := math.Pow(10, float64(digits))
	return math.Round(value*scale) / scale
}

// floatPtr is a convenience for the nullable columns and fields.
func floatPtr(value float64) *float64 { return &value }

// float64Ptr is the same, spelled the way the scoring code reads best.
func float64Ptr(value float64) *float64 { return &value }

// intPtr is a convenience for the nullable columns and fields.
func intPtr(value int) *int { return &value }
