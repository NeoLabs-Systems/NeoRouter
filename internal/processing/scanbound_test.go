//go:build !race

package processing

import "time"

// scanColdBound is the JavaScript test's bound: 250 ms for 20 000 characters.
const scanColdBound = 250 * time.Millisecond
