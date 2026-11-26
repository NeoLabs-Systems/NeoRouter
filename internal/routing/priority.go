package routing

import "sync"

// Who goes first when models are busy. A request that has to wait for capacity
// announces what it waits for, and requests of a lower priority leave those
// models and accounts alone until it has been served. Nothing runs differently
// while there is room, and a request never waits on one of its own level.
var PriorityLevels = []string{"low", "normal", "high"}

func priorityRank(level string) int {
	if level == "" {
		level = "normal"
	}
	for index, candidate := range PriorityLevels {
		if candidate == level {
			return index
		}
	}
	return 0
}

// priorityQueue holds, per target ("model:<ref>" or "account:<id>"), how many
// waiting requests there are at each level.
type priorityQueue struct {
	mu      sync.Mutex
	waiting map[string][]int
}

func newPriorityQueue() *priorityQueue {
	return &priorityQueue{waiting: map[string][]int{}}
}

// join announces that a request waits for these targets; the returned function
// withdraws it (idempotent).
func (q *priorityQueue) join(targets []string, level string) func() {
	index := priorityRank(level)
	q.mu.Lock()
	for _, target := range targets {
		counts := q.waiting[target]
		if counts == nil {
			counts = make([]int, len(PriorityLevels))
		}
		counts[index]++
		q.waiting[target] = counts
	}
	q.mu.Unlock()
	left := false
	return func() {
		if left {
			return
		}
		left = true
		q.mu.Lock()
		defer q.mu.Unlock()
		for _, target := range targets {
			counts, ok := q.waiting[target]
			if !ok {
				continue
			}
			counts[index]--
			empty := true
			for _, count := range counts {
				if count != 0 {
					empty = false
					break
				}
			}
			if empty {
				delete(q.waiting, target)
			}
		}
	}
}

func (q *priorityQueue) outranked(target string, index int) bool {
	counts := q.waiting[target]
	for level := index + 1; level < len(counts); level++ {
		if counts[level] > 0 {
			return true
		}
	}
	return false
}

// heldBack reports whether a request of a higher priority waits for this model
// or its account.
func (q *priorityQueue) heldBack(entry *Entry, level string) bool {
	index := priorityRank(level)
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.outranked("model:"+formatRef(entry.Ref), index) {
		return true
	}
	return q.outranked("account:"+entry.AccountID, index)
}
