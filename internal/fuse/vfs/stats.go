package vfs

import "sync/atomic"

// Stats tracks VFS cache statistics.
type Stats struct {
	CacheHits      atomic.Int64
	CacheMisses    atomic.Int64
	BytesFromCache atomic.Int64
	BytesFromNNTP  atomic.Int64
	ActiveFiles    atomic.Int32
}

// Snapshot returns a point-in-time copy of the stats.
type StatsSnapshot struct {
	CacheHits      int64 `json:"cache_hits"`
	CacheMisses    int64 `json:"cache_misses"`
	BytesFromCache int64 `json:"bytes_from_cache"`
	BytesFromNNTP  int64 `json:"bytes_from_nntp"`
	ActiveFiles    int32 `json:"active_files"`
	TotalCacheSize int64 `json:"total_cache_size"`
	CachedItems    int   `json:"cached_items"`
}

// Snapshot creates a point-in-time copy of the stats.
func (s *Stats) Snapshot(cache *Cache) StatsSnapshot {
	snap := StatsSnapshot{
		CacheHits:      s.CacheHits.Load(),
		CacheMisses:    s.CacheMisses.Load(),
		BytesFromCache: s.BytesFromCache.Load(),
		BytesFromNNTP:  s.BytesFromNNTP.Load(),
		ActiveFiles:    s.ActiveFiles.Load(),
	}
	if cache != nil {
		snap.TotalCacheSize = cache.TotalSize()
		snap.CachedItems = cache.ItemCount()
	}
	return snap
}
