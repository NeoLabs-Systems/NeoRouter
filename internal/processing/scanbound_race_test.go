//go:build race

package processing

import "time"

// Under the race detector every regex operation is instrumented and several
// times slower, so the cold-scan perf bound is relaxed; -race is a correctness
// gate, not a performance one. The tight bound lives in scanbound_test.go.
const scanColdBound = 5 * time.Second
