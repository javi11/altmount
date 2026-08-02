package api

import (
	"sync"
	"time"
)

// stremioFailureCacheMaxEntries bounds the cache so a long-running server cannot
// accumulate failure keys without limit. On overflow the oldest entry is dropped.
const stremioFailureCacheMaxEntries = 1000

// stremioFailureCache remembers Stremio releases that failed in ways which leave no
// 'failed' row in import_queue, and which therefore cannot be recovered by
// Repository.GetFailedStremioQueueItems:
//
//   - pre-queue failures, where Prowlarr's download URL is dead or the NZB is
//     malformed, so no queue item is ever created;
//   - imports that complete but yield no playable media, which leave a 'completed'
//     row and would otherwise be badged as instantly-playable.
//
// It is deliberately process-local and not persisted: the durable half of the
// exclusion set comes from import_queue, so losing this on restart costs at most one
// retry of an affected release.
//
// Expiry is applied by the reader rather than stored, so a live change to
// Stremio.FailedReleaseTTLHours takes effect without recreating the cache — matching
// how the import_queue half applies the same TTL at its call site.
type stremioFailureCache struct {
	mu    sync.RWMutex
	items map[string]time.Time // release key -> when the failure was recorded
}

func newStremioFailureCache() *stremioFailureCache {
	return &stremioFailureCache{items: make(map[string]time.Time)}
}

// stremioFailureExpired reports whether a failure recorded at recordedAt has aged out.
// A non-positive ttl means failures never expire on age alone.
func stremioFailureExpired(recordedAt, now time.Time, ttl time.Duration) bool {
	return ttl > 0 && now.Sub(recordedAt) >= ttl
}

// Record marks a release key as failed, refreshing the timestamp if already present.
func (c *stremioFailureCache) Record(releaseKey string) {
	if releaseKey == "" {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Evict the oldest entry rather than growing without bound. Only needed when
	// adding a genuinely new key.
	if _, exists := c.items[releaseKey]; !exists && len(c.items) >= stremioFailureCacheMaxEntries {
		var oldestKey string
		var oldestAt time.Time
		for k, at := range c.items {
			if oldestKey == "" || at.Before(oldestAt) {
				oldestKey, oldestAt = k, at
			}
		}
		delete(c.items, oldestKey)
	}

	c.items[releaseKey] = time.Now()
}

// Has reports whether a release key is recorded as failed and not yet aged out.
func (c *stremioFailureCache) Has(releaseKey string, ttl time.Duration) bool {
	if releaseKey == "" {
		return false
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	recordedAt, ok := c.items[releaseKey]
	return ok && !stremioFailureExpired(recordedAt, time.Now(), ttl)
}

// Forget drops a release key, so a release that later imports successfully stops
// being excluded.
func (c *stremioFailureCache) Forget(releaseKey string) {
	if releaseKey == "" {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.items, releaseKey)
}

// Keys returns the set of release keys still considered failed under ttl, purging
// aged-out entries as a side effect. This lazy sweep is why the cache needs no
// background goroutine.
func (c *stremioFailureCache) Keys(ttl time.Duration) map[string]struct{} {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	keys := make(map[string]struct{}, len(c.items))
	for k, recordedAt := range c.items {
		if stremioFailureExpired(recordedAt, now, ttl) {
			delete(c.items, k)
			continue
		}
		keys[k] = struct{}{}
	}

	return keys
}
