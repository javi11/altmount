package vfs

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// ManagerConfig holds VFS manager configuration.
type ManagerConfig struct {
	Enabled        bool
	CachePath      string
	MaxSizeBytes   int64
	ExpiryDuration time.Duration
	ChunkSize      int64
	ReadAheadChunks int
}

// Manager manages the VFS disk cache lifecycle.
type Manager struct {
	cache   *Cache
	config  ManagerConfig
	logger  *slog.Logger
	stats   Stats
	files   sync.Map // path → *Downloader
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
}

// NewManager creates a new VFS manager.
func NewManager(cfg ManagerConfig, logger *slog.Logger) (*Manager, error) {
	cacheCfg := CacheConfig{
		CachePath:      cfg.CachePath,
		MaxSizeBytes:   cfg.MaxSizeBytes,
		ExpiryDuration: cfg.ExpiryDuration,
		ChunkSize:      cfg.ChunkSize,
	}

	cache, err := NewCache(cacheCfg, logger)
	if err != nil {
		return nil, err
	}

	return &Manager{
		cache:  cache,
		config: cfg,
		logger: logger,
	}, nil
}

// Start begins background maintenance (cleanup, metadata flush).
func (m *Manager) Start(ctx context.Context) {
	m.ctx, m.cancel = context.WithCancel(ctx)

	// Background cleanup goroutine
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.cleanupLoop()
	}()

	// Background metadata flush goroutine
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.flushLoop()
	}()

	m.logger.Info("VFS disk cache started",
		"cache_path", m.config.CachePath,
		"max_size_bytes", m.config.MaxSizeBytes,
		"chunk_size", m.config.ChunkSize,
		"read_ahead_chunks", m.config.ReadAheadChunks)
}

// Stop shuts down the VFS manager.
func (m *Manager) Stop() {
	m.logger.Info("Stopping VFS disk cache")

	if m.cancel != nil {
		m.cancel()
	}

	// Stop all active downloaders
	m.files.Range(func(_, value any) bool {
		if dl, ok := value.(*Downloader); ok {
			dl.Stop()
		}
		return true
	})

	m.wg.Wait()

	// Final metadata flush
	m.cache.FlushMetadata()

	m.logger.Info("VFS disk cache stopped")
}

// Open returns a CachedFile for the given path.
// Multiple callers can open the same file concurrently.
func (m *Manager) Open(ctx context.Context, path string, size int64, opener FileOpener) (*CachedFile, error) {
	item := m.cache.GetOrCreate(path, size)

	// Get or create downloader for this path
	var dl *Downloader
	if v, loaded := m.files.LoadOrStore(path, (*Downloader)(nil)); loaded && v != nil {
		dl = v.(*Downloader)
	} else {
		dl = NewDownloader(
			item,
			opener,
			path,
			size,
			m.config.ChunkSize,
			m.config.ReadAheadChunks,
			m.logger,
		)
		m.files.Store(path, dl)
		dl.Start(m.ctx)
	}

	cf, err := NewCachedFile(item, opener, path, size, m.config.ChunkSize, m.logger, dl)
	if err != nil {
		return nil, err
	}

	m.stats.ActiveFiles.Add(1)

	return cf, nil
}

// Close should be called when a CachedFile is closed to update stats.
func (m *Manager) Close(path string) {
	m.stats.ActiveFiles.Add(-1)
}

// GetStats returns a snapshot of the cache statistics.
func (m *Manager) GetStats() StatsSnapshot {
	return m.stats.Snapshot(m.cache)
}

func (m *Manager) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.cache.Cleanup()
		}
	}
}

func (m *Manager) flushLoop() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.cache.FlushMetadata()
		}
	}
}
