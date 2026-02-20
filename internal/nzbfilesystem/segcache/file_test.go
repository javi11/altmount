package segcache_test

import (
	"log/slog"
	"testing"
	"time"

	"github.com/javi11/altmount/internal/nzbfilesystem/segcache"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newDefaultTestCache(t *testing.T) *segcache.SegmentCache {
	t.Helper()
	cfg := segcache.Config{
		CachePath:      t.TempDir(),
		MaxSizeBytes:   100 * 1024 * 1024,
		ExpiryDuration: 24 * time.Hour,
	}
	cache, err := segcache.NewSegmentCache(cfg, slog.Default())
	require.NoError(t, err)
	return cache
}

func TestSegmentCachePutAndGet(t *testing.T) {
	cache := newDefaultTestCache(t)

	data := []byte("Hello, Usenet segment data!")
	err := cache.Put("msg-001", data)
	require.NoError(t, err)

	got, ok := cache.Get("msg-001")
	assert.True(t, ok)
	assert.Equal(t, data, got)
}

func TestSegmentCacheGetMiss(t *testing.T) {
	cache := newDefaultTestCache(t)

	got, ok := cache.Get("nonexistent")
	assert.False(t, ok)
	assert.Nil(t, got)
}

func TestSegmentCacheOverwrite(t *testing.T) {
	cache := newDefaultTestCache(t)

	err := cache.Put("msg-001", []byte("first"))
	require.NoError(t, err)

	err = cache.Put("msg-001", []byte("second"))
	require.NoError(t, err)

	got, ok := cache.Get("msg-001")
	assert.True(t, ok)
	assert.Equal(t, []byte("second"), got)
}

func TestSegmentCacheMultipleKeys(t *testing.T) {
	cache := newDefaultTestCache(t)

	for i := range 10 {
		msgID := string(rune('A'+i)) + "-segment"
		err := cache.Put(msgID, []byte{byte(i)})
		require.NoError(t, err)
	}

	for i := range 10 {
		msgID := string(rune('A'+i)) + "-segment"
		got, ok := cache.Get(msgID)
		assert.True(t, ok)
		assert.Equal(t, []byte{byte(i)}, got)
	}
}

func TestSegmentCacheSatisfiesSegmentStore(t *testing.T) {
	// Compile-time check that *SegmentCache implements the Get/Put interface
	// matching usenet.SegmentStore.
	cache := newDefaultTestCache(t)

	var store interface {
		Get(string) ([]byte, bool)
		Put(string, []byte) error
	} = cache

	err := store.Put("test-msg", []byte("data"))
	require.NoError(t, err)

	got, ok := store.Get("test-msg")
	assert.True(t, ok)
	assert.Equal(t, []byte("data"), got)
}
