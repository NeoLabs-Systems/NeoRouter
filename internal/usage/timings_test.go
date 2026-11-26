package usage

import (
	"math/rand/v2"
	"slices"
	"testing"
)

// Counting has to give exactly what sorting every duration gave.
func TestTimingsPercentileMatchesSortedValues(t *testing.T) {
	var empty timings
	if got := empty.percentile(0.5); got != nil {
		t.Fatalf("percentile of nothing = %d", *got)
	}
	random := rand.New(rand.NewPCG(1, 2))
	for _, size := range []int{1, 2, 3, 20, 101, 5000} {
		var counted timings
		values := make([]int64, size)
		for i := range values {
			values[i] = random.Int64N(300) // repeats on purpose
			counted.add(values[i])
		}
		slices.Sort(values)
		for _, p := range []float64{0, 0.5, 0.95, 1} {
			want := values[min(size-1, int(p*float64(size)))]
			if got := counted.percentile(p); got == nil || *got != want {
				t.Errorf("n=%d p=%v: got %v, want %d", size, p, got, want)
			}
		}
	}
}
