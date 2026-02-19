package segcache

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/singleflight"
)

// ManagerConfig holds the full segment-cache configuration.
type ManagerConfig struct {
	Enabled             bool
	CachePath           string
	MaxSizeBytes        int64
	ExpiryDuration      time.Duration
	ReadAheadSegments   int // number of segments to prefetch ahead (~750KB each)
	PrefetchConcurrency int // max parallel prefetch goroutines per file
}

// StatsSnapshot is a point-in-time view of cache statistics.
type StatsSnapshot struct {
	CacheHits   int64
	CacheMisses int64
	TotalSize   int64
	ItemCount   int
	ActiveFiles int64
}

// Manager owns a SegmentCache and manages per-file Prefetchers.
// It also runs background goroutines for cleanup, catalog flushing, and idle-Prefetcher reaping.
type Manager struct {
	cache       *SegmentCache
	config      ManagerConfig
	logger      *slog.Logger
	prefetchers sync.Map // path → *Prefetcher
	fetchGroups sync.Map // path → *singleflight.Group
	activeFiles atomic.Int64
	hits        atomic.Int64
	misses      atomic.Int64
	ctx         context.Context
	cancel      context.CancelFunc
	wg          sync.WaitGroup
}

// NewManager creates a Manager and loads any existing on-disk catalog.
func NewManager(cfg ManagerConfig, logger *slog.Logger) (*Manager, error) {
	cacheCfg := Config{
		CachePath:      cfg.CachePath,
		MaxSizeBytes:   cfg.MaxSizeBytes,
		ExpiryDuration: cfg.ExpiryDuration,
	}

	cache, err := NewSegmentCache(cacheCfg, logger)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &Manager{
		cache:  cache,
		config: cfg,
		logger: logger,
		ctx:    ctx,
		cancel: cancel,
	}, nil
}

// Start launches background maintenance goroutines.
// ctx is not stored; internal goroutines use the Manager's own derived context.
func (m *Manager) Start(_ context.Context) {
	m.wg.Add(3)
	go m.cleanupLoop()
	go m.catalogFlushLoop()
	go m.idleMonitor()
}

// Stop shuts down background goroutines and saves the catalog.
func (m *Manager) Stop() {
	m.cancel()
	m.wg.Wait()

	// Stop all prefetchers that are still running.
	m.prefetchers.Range(func(_, v any) bool {
		v.(*Prefetcher).Stop()
		return true
	})

	if err := m.cache.SaveCatalog(); err != nil {
		m.logger.Warn("segcache: final catalog save failed", "error", err)
	}
}

// Open returns a SegmentCachedFile for the given path.
// All files at the same path share a Prefetcher and singleflight.Group so that
// concurrent handles do not duplicate NNTP downloads.
func (m *Manager) Open(
	path string,
	segments []SegmentEntry,
	fileSize int64,
	opener FileOpener,
) (*SegmentCachedFile, error) {
	m.activeFiles.Add(1)

	// Shared singleflight.Group ensures at most one in-flight fetch per message ID.
	fg, _ := m.fetchGroups.LoadOrStore(path, &singleflight.Group{})
	fetchGroup := fg.(*singleflight.Group)

	readAhead := m.config.ReadAheadSegments
	if readAhead <= 0 {
		readAhead = 8
	}

	conc := m.config.PrefetchConcurrency
	if conc <= 0 {
		conc = 3
	}

	pf, _ := m.prefetchers.LoadOrStore(path, NewPrefetcher(
		segments, m.cache, opener, path, readAhead, conc, fetchGroup, m.logger,
	))
	prefetcher := pf.(*Prefetcher)

	return &SegmentCachedFile{
		path:       path,
		fileSize:   fileSize,
		segments:   segments,
		cache:      m.cache,
		opener:     opener,
		prefetcher: prefetcher,
		fetchGroup: fetchGroup,
		logger:     m.logger,
	}, nil
}

// Close decrements the active-file counter. Must be called once per Open.
func (m *Manager) Close(_ string) {
	m.activeFiles.Add(-1)
}

// GetStats returns a point-in-time snapshot of cache statistics.
func (m *Manager) GetStats() StatsSnapshot {
	return StatsSnapshot{
		CacheHits:   m.hits.Load(),
		CacheMisses: m.misses.Load(),
		TotalSize:   m.cache.TotalSize(),
		ItemCount:   m.cache.ItemCount(),
		ActiveFiles: m.activeFiles.Load(),
	}
}

func (m *Manager) cleanupLoop() {
	defer m.wg.Done()

	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.cache.Cleanup()
			m.cache.Evict()
		}
	}
}

func (m *Manager) catalogFlushLoop() {
	defer m.wg.Done()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			if err := m.cache.SaveCatalog(); err != nil {
				m.logger.WarnContext(m.ctx, "segcache: periodic catalog save failed", "error", err)
			}
		}
	}
}

func (m *Manager) idleMonitor() {
	defer m.wg.Done()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			deadline := time.Now().Add(-30 * time.Second)
			m.prefetchers.Range(func(k, v any) bool {
				p := v.(*Prefetcher)
				if p.LastSeen().Before(deadline) {
					p.Stop()
					m.prefetchers.Delete(k)
					m.fetchGroups.Delete(k)
				}
				return true
			})
		}
	}
}
