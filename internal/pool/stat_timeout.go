package pool

import "time"

// StatManyTimeout scales a per-item Stat timeout into an overall deadline for a
// StatMany batch of count IDs run at the given concurrency, preserving the same
// worst-case bound a per-item context.WithTimeout gave when every check ran as
// its own goroutine: ceil(count/concurrency) waves, each capped at perItem.
func StatManyTimeout(count, concurrency int, perItem time.Duration) time.Duration {
	if count <= 0 {
		return perItem
	}
	if concurrency <= 0 {
		concurrency = 1
	}
	waves := max((count+concurrency-1)/concurrency, 1)
	return perItem * time.Duration(waves)
}
