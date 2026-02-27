package usenet

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNegativeCache(t *testing.T) {
	cache := NewNegativeCache(100 * time.Millisecond)

	// Not in cache
	assert.False(t, cache.Get("msg1"))

	// Put in cache
	cache.Put("msg1")
	assert.True(t, cache.Get("msg1"))

	// Wait for expiration
	time.Sleep(150 * time.Millisecond)
	assert.False(t, cache.Get("msg1"))
}

func TestNegativeCache_MultipleItems(t *testing.T) {
	cache := NewNegativeCache(100 * time.Millisecond)

	cache.Put("msg1")
	cache.Put("msg2")

	assert.True(t, cache.Get("msg1"))
	assert.True(t, cache.Get("msg2"))

	time.Sleep(150 * time.Millisecond)

	assert.False(t, cache.Get("msg1"))
	assert.False(t, cache.Get("msg2"))
}
