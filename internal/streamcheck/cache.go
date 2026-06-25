package streamcheck

import (
	"sync"
	"time"
)

// verdictCache is an in-memory, TTL-bounded cache of availability verdicts keyed
// by release fingerprint. It avoids re-issuing NNTP STAT round-trips for the same
// release when a client browses repeatedly. Verdicts are not persisted across
// restarts.
type verdictCache struct {
	mu      sync.Mutex
	entries map[string]cacheEntry
}

type cacheEntry struct {
	result    Result
	expiresAt time.Time
}

func newVerdictCache() *verdictCache {
	return &verdictCache{entries: make(map[string]cacheEntry)}
}

// get returns the cached verdict for key when present and unexpired.
func (c *verdictCache) get(key string) (Result, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	e, ok := c.entries[key]
	if !ok {
		return Result{}, false
	}
	if time.Now().After(e.expiresAt) {
		delete(c.entries, key)
		return Result{}, false
	}
	return e.result, true
}

// set stores result under key for the given ttl.
func (c *verdictCache) set(key string, result Result, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Opportunistic sweep to bound memory on long-running instances.
	if len(c.entries) > 4096 {
		now := time.Now()
		for k, e := range c.entries {
			if now.After(e.expiresAt) {
				delete(c.entries, k)
			}
		}
	}

	c.entries[key] = cacheEntry{result: result, expiresAt: time.Now().Add(ttl)}
}
