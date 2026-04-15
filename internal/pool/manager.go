package pool

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/javi11/nntppool/v4"
)

// Manager provides centralized NNTP connection pool management
type Manager interface {
	// GetPool returns the current connection pool or error if not available
	GetPool() (*nntppool.Client, error)

	// SetProviders creates/recreates the pool with new providers
	SetProviders(providers []nntppool.Provider) error

	// ClearPool shuts down and removes the current pool
	ClearPool() error

	// HasPool returns true if a pool is currently available
	HasPool() bool

	// GetMetrics returns the current pool metrics with calculated speeds
	GetMetrics() (MetricsSnapshot, error)

	// ResetMetrics resets specific cumulative metrics
	ResetMetrics(ctx context.Context, resetPeak bool, resetTotals bool) error

	// IncArticlesDownloaded increments the count of articles successfully downloaded
	IncArticlesDownloaded()

	// UpdateDownloadProgress updates the bytes downloaded for a specific stream
	UpdateDownloadProgress(id string, bytesDownloaded int64)

	// IncArticlesPosted increments the count of articles successfully posted
	IncArticlesPosted()

	// AddProvider adds a single provider to the running pool.
	// If no pool exists, a new one is created with this provider.
	AddProvider(provider nntppool.Provider) error

	// RemoveProvider removes a provider by its nntppool name (host:port or host:port+username).
	// If the last provider is removed, the pool is closed.
	RemoveProvider(name string) error

	// ResetProviderQuota resets the download quota counter for a provider by
	// removing and re-adding it without restoring persisted quota state.
	ResetProviderQuota(ctx context.Context, provider nntppool.Provider, poolName string) error
}

// StatsRepository defines the interface for persisting pool statistics
type StatsRepository interface {
	UpdateSystemStat(ctx context.Context, key string, value int64) error
	BatchUpdateSystemStats(ctx context.Context, stats map[string]int64) error
	GetSystemStats(ctx context.Context) (map[string]int64, error)
	AddBytesDownloadedToDailyStat(ctx context.Context, bytes int64) error
	AddProviderBytesToHourlyStat(ctx context.Context, providerID string, bytes int64) error
	GetProviderHourlyStats(ctx context.Context, hours int) (map[string]int64, error)
	ClearProviderHourlyStats(ctx context.Context) error
	GetOldestStatDate(ctx context.Context) (time.Time, error)
	GetOldestProviderStatDates(ctx context.Context) (map[string]time.Time, error)
}

// manager implements the Manager interface
type manager struct {
	mu             sync.RWMutex
	pool           *nntppool.Client
	metricsTracker *MetricsTracker
	repo           StatsRepository
	ctx            context.Context
	logger         *slog.Logger
}

// NewManager creates a new pool manager
func NewManager(ctx context.Context, repo StatsRepository) Manager {
	return &manager{
		ctx:    ctx,
		repo:   repo,
		logger: slog.Default().With("component", "pool"),
	}
}

// injectQuotaState loads persisted quota counters from the database and sets
// QuotaUsed / QuotaResetAt on each provider so nntppool can resume quota
// tracking across restarts.
func (m *manager) injectQuotaState(providers []nntppool.Provider) {
	if m.repo == nil {
		return
	}

	stats, err := m.repo.GetSystemStats(m.ctx)
	if err != nil {
		m.logger.ErrorContext(m.ctx, "Failed to load quota state from database", "error", err)
		return
	}

	// Build lookup maps from prefixed keys
	quotaUsed := make(map[string]int64)
	quotaResetAt := make(map[string]int64)
	for k, v := range stats {
		if after, ok := strings.CutPrefix(k, "quota_used:"); ok {
			quotaUsed[after] = v
		} else if after, ok := strings.CutPrefix(k, "quota_reset_at:"); ok {
			quotaResetAt[after] = v
		}
	}

	for i := range providers {
		name := providers[i].Host
		if providers[i].Auth.Username != "" {
			name += "+" + providers[i].Auth.Username
		}

		if used, ok := quotaUsed[name]; ok && used > 0 {
			providers[i].QuotaUsed = used
		}
		if resetNano, ok := quotaResetAt[name]; ok && resetNano > 0 {
			t := time.Unix(0, resetNano)
			if t.After(time.Now()) {
				providers[i].QuotaResetAt = t
			}
		}
	}
}

// GetPool returns the current connection pool or error if not available
func (m *manager) GetPool() (*nntppool.Client, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.pool == nil {
		return nil, fmt.Errorf("NNTP connection pool not available - no providers configured")
	}

	return m.pool, nil
}

// SetProviders creates/recreates the pool with new providers
func (m *manager) SetProviders(providers []nntppool.Provider) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Shut down existing pool and metrics tracker if present
	if m.pool != nil {
		m.logger.InfoContext(m.ctx, "Shutting down existing NNTP connection pool")
		if m.metricsTracker != nil {
			m.metricsTracker.Stop()
			m.metricsTracker = nil
		}
		m.pool.Close()
		m.pool = nil
	}

	// Return early if no providers (clear pool scenario)
	if len(providers) == 0 {
		m.logger.InfoContext(m.ctx, "No NNTP providers configured - pool cleared")
		return nil
	}

	// Restore quota state from DB before creating the pool
	m.injectQuotaState(providers)

	// Create new pool with providers
	m.logger.InfoContext(m.ctx, "Creating NNTP connection pool", "provider_count", len(providers))
	pool, err := nntppool.NewClient(m.ctx, providers)
	if err != nil {
		return fmt.Errorf("failed to create NNTP connection pool: %w", err)
	}

	m.pool = pool

	// Start metrics tracker
	m.metricsTracker = NewMetricsTracker(pool, m.repo)
	m.metricsTracker.Start(m.ctx)

	m.logger.InfoContext(m.ctx, "NNTP connection pool created successfully")
	return nil
}

// ClearPool shuts down and removes the current pool
func (m *manager) ClearPool() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.pool != nil {
		m.logger.InfoContext(m.ctx, "Clearing NNTP connection pool")
		if m.metricsTracker != nil {
			m.metricsTracker.Stop()
			m.metricsTracker = nil
		}
		m.pool.Close()
		m.pool = nil
	}

	return nil
}

// HasPool returns true if a pool is currently available
func (m *manager) HasPool() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.pool != nil
}

// GetMetrics returns the current pool metrics with calculated speeds
func (m *manager) GetMetrics() (MetricsSnapshot, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.pool == nil {
		return MetricsSnapshot{}, fmt.Errorf("NNTP connection pool not available")
	}

	if m.metricsTracker == nil {
		return MetricsSnapshot{}, fmt.Errorf("metrics tracker not available")
	}

	return m.metricsTracker.GetSnapshot(), nil
}

// ResetMetrics resets specific cumulative metrics
func (m *manager) ResetMetrics(ctx context.Context, resetPeak bool, resetTotals bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.metricsTracker != nil {
		return m.metricsTracker.Reset(ctx, resetPeak, resetTotals)
	}

	// If tracker not available, still try to reset DB directly
	if m.repo != nil {
		currentStats, err := m.repo.GetSystemStats(ctx)
		if err == nil {
			resetMap := make(map[string]int64)
			for k := range currentStats {
				if resetTotals {
					resetMap[k] = 0
				}
			}

			if resetTotals {
				resetMap["bytes_downloaded"] = 0
				resetMap["articles_downloaded"] = 0
				resetMap["bytes_uploaded"] = 0
				resetMap["articles_posted"] = 0
			}

			if resetPeak {
				resetMap["max_download_speed"] = 0
			}

			if len(resetMap) > 0 {
				_ = m.repo.BatchUpdateSystemStats(ctx, resetMap)
			}
		}
	}

	return nil
}

// IncArticlesDownloaded increments the count of articles successfully downloaded
func (m *manager) IncArticlesDownloaded() {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.metricsTracker != nil {
		m.metricsTracker.IncArticlesDownloaded()
	}
}

// UpdateDownloadProgress updates the bytes downloaded for a specific stream
func (m *manager) UpdateDownloadProgress(id string, bytesDownloaded int64) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.metricsTracker != nil {
		m.metricsTracker.UpdateDownloadProgress(id, bytesDownloaded)
	}
}

// IncArticlesPosted increments the count of articles successfully posted
func (m *manager) IncArticlesPosted() {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.metricsTracker != nil {
		m.metricsTracker.IncArticlesPosted()
	}
}

// AddProvider adds a single provider to the running pool.
// If no pool exists yet, a new one is created with this single provider.
func (m *manager) AddProvider(provider nntppool.Provider) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Restore quota state from DB
	providers := []nntppool.Provider{provider}
	m.injectQuotaState(providers)
	provider = providers[0]

	if m.pool == nil {
		// No pool yet — create one with this single provider
		m.logger.InfoContext(m.ctx, "Creating NNTP connection pool for first provider", "provider", provider.Host)
		pool, err := nntppool.NewClient(m.ctx, []nntppool.Provider{provider}, nntppool.WithDispatchStrategy(nntppool.DispatchRoundRobin))
		if err != nil {
			return fmt.Errorf("failed to create NNTP connection pool: %w", err)
		}
		m.pool = pool
		m.metricsTracker = NewMetricsTracker(pool, m.repo)
		m.metricsTracker.Start(m.ctx)
		return nil
	}

	m.logger.InfoContext(m.ctx, "Adding provider to NNTP connection pool", "provider", provider.Host)
	return m.pool.AddProvider(provider)
}

// RemoveProvider removes a provider by name from the running pool.
// If the last provider is removed, the pool and metrics tracker are shut down.
func (m *manager) RemoveProvider(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.pool == nil {
		return fmt.Errorf("NNTP connection pool not available - cannot remove provider")
	}

	m.logger.InfoContext(m.ctx, "Removing provider from NNTP connection pool", "provider", name)
	if err := m.pool.RemoveProvider(name); err != nil {
		return err
	}

	// If no providers remain, tear down the pool entirely
	if m.pool.NumProviders() == 0 {
		m.logger.InfoContext(m.ctx, "Last provider removed - shutting down NNTP connection pool")
		if m.metricsTracker != nil {
			m.metricsTracker.Stop()
			m.metricsTracker = nil
		}
		m.pool.Close()
		m.pool = nil
	}

	return nil
}

// ResetProviderQuota resets the download quota counter for a provider by
// removing it from the pool, clearing persisted quota state in the DB, and
// re-adding it with QuotaUsed=0 so it starts fresh.
func (m *manager) ResetProviderQuota(ctx context.Context, provider nntppool.Provider, poolName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.pool == nil {
		return fmt.Errorf("NNTP connection pool not available")
	}

	m.logger.InfoContext(ctx, "Resetting provider quota", "provider", poolName)

	// Remove the provider from the pool
	if err := m.pool.RemoveProvider(poolName); err != nil {
		return fmt.Errorf("failed to remove provider for quota reset: %w", err)
	}

	// Clear persisted quota state in the DB
	if m.repo != nil {
		stats := map[string]int64{
			"quota_used:" + poolName:     0,
			"quota_reset_at:" + poolName: 0,
		}
		if err := m.repo.BatchUpdateSystemStats(ctx, stats); err != nil {
			m.logger.ErrorContext(ctx, "Failed to clear persisted quota state", "err", err, "provider", poolName)
		}
	}

	// Re-add without injecting old quota state — QuotaUsed stays 0
	m.logger.InfoContext(ctx, "Re-adding provider with reset quota", "provider", provider.Host)
	return m.pool.AddProvider(provider)
}
