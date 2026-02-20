package segcache

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// ManagerConfig holds the full segment-cache configuration.
type ManagerConfig struct {
	Enabled        bool
	CachePath      string
	MaxSizeBytes   int64
	ExpiryDuration time.Duration
}

// DefaultManagerConfig returns a ManagerConfig with sensible defaults.
func DefaultManagerConfig() ManagerConfig {
	return ManagerConfig{
		Enabled:        false,
		CachePath:      "/tmp/altmount-segcache",
		MaxSizeBytes:   10 * 1024 * 1024 * 1024, // 10 GB
		ExpiryDuration: 24 * time.Hour,
	}
}

// WithDefaults returns a copy with zero values replaced by defaults.
func (cfg ManagerConfig) WithDefaults() ManagerConfig {
	defaults := DefaultManagerConfig()
	if cfg.CachePath == "" {
		cfg.CachePath = defaults.CachePath
	}
	if cfg.MaxSizeBytes <= 0 {
		cfg.MaxSizeBytes = defaults.MaxSizeBytes
	}
	if cfg.ExpiryDuration <= 0 {
		cfg.ExpiryDuration = defaults.ExpiryDuration
	}
	return cfg
}

// StatsSnapshot is a point-in-time view of cache statistics.
type StatsSnapshot struct {
	CacheHits   int64
	CacheMisses int64
	TotalSize   int64
	ItemCount   int
}

// Manager owns a SegmentCache and runs background maintenance goroutines
// for cleanup and catalog flushing.
type Manager struct {
	cache  *SegmentCache
	config ManagerConfig
	logger *slog.Logger
	hits   atomic.Int64
	misses atomic.Int64
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
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
func (m *Manager) Start(_ context.Context) {
	m.wg.Add(2)
	go m.cleanupLoop()
	go m.catalogFlushLoop()
}

// Stop shuts down background goroutines and saves the catalog.
func (m *Manager) Stop() {
	m.cancel()
	m.wg.Wait()

	if err := m.cache.SaveCatalog(); err != nil {
		m.logger.Warn("segcache: final catalog save failed", "error", err)
	}
}

// Cache returns the underlying SegmentCache for use as a usenet.SegmentStore.
func (m *Manager) Cache() *SegmentCache {
	return m.cache
}

// GetStats returns a point-in-time snapshot of cache statistics.
func (m *Manager) GetStats() StatsSnapshot {
	return StatsSnapshot{
		CacheHits:   m.hits.Load(),
		CacheMisses: m.misses.Load(),
		TotalSize:   m.cache.TotalSize(),
		ItemCount:   m.cache.ItemCount(),
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
