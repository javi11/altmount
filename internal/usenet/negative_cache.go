package usenet

import (
	"sync"
	"time"
)

// NegativeCache stores message IDs that failed to download to prevent immediate retries
type NegativeCache struct {
	failures sync.Map // string -> time.Time (expiration)
	ttl      time.Duration
}

// NewNegativeCache creates a new negative cache with the given TTL
func NewNegativeCache(ttl time.Duration) *NegativeCache {
	return &NegativeCache{
		ttl: ttl,
	}
}

// Put adds a message ID to the negative cache
func (c *NegativeCache) Put(messageID string) {
	c.failures.Store(messageID, time.Now().Add(c.ttl))
}

// Get checks if a message ID is in the negative cache and not expired
func (c *NegativeCache) Get(messageID string) bool {
	if expiration, ok := c.failures.Load(messageID); ok {
		if time.Now().Before(expiration.(time.Time)) {
			return true
		}
		// Expired, remove it
		c.failures.Delete(messageID)
	}
	return false
}
