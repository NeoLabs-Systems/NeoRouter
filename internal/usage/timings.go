package usage

import (
	"maps"
	"slices"
)

// timings counts how often each duration in milliseconds occurred. A range can
// hold millions of requests and every breakdown keeps its own percentiles, so
// memory has to grow with the distinct values, not with the requests.
type timings struct {
	counts map[int64]int64
	total  int64
}

func (t *timings) add(ms int64) {
	if t.counts == nil {
		t.counts = map[int64]int64{}
	}
	t.counts[ms]++
	t.total++
}

// percentile is the value at position floor(p*n) of the sorted durations, the
// last one at most; nil when there are none.
func (t *timings) percentile(p float64) *int64 {
	if t.total == 0 {
		return nil
	}
	index := min(t.total-1, int64(p*float64(t.total)))
	var seen int64
	for _, value := range slices.Sorted(maps.Keys(t.counts)) {
		if seen += t.counts[value]; seen > index {
			return &value
		}
	}
	return nil
}
