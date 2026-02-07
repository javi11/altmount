package pool

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/nntppool/v2"
)

// Manager provides centralized NNTP connection pool management
type Manager interface {
	// GetPool returns the current connection pool or error if not available
	GetPool() (nntppool.UsenetConnectionPool, error)

	// SetProviders creates/recreates the pool with new providers
	SetProviders(providers []nntppool.UsenetProviderConfig) error

	// ClearPool shuts down and removes the current pool
	ClearPool() error

	// HasPool returns true if a pool is currently available
	HasPool() bool

	// GetMetrics returns the current pool metrics with calculated speeds
	GetMetrics() (MetricsSnapshot, error)

	// ResetMetrics resets cumulative statistics
	ResetMetrics(ctx context.Context) error
}

// manager implements the Manager interface
type manager struct {
	mu             sync.RWMutex
	pool           nntppool.UsenetConnectionPool
	metricsTracker *MetricsTracker
	repo           *database.Repository
	ctx            context.Context
	logger         *slog.Logger
}

// NewManager creates a new pool manager
func NewManager(ctx context.Context, repo *database.Repository) Manager {
	return &manager{
		ctx:    ctx,
		repo:   repo,
		logger: slog.Default().With("component", "pool"),
	}
}

// GetPool returns the current connection pool or error if not available
func (m *manager) GetPool() (nntppool.UsenetConnectionPool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.pool == nil {
		return nil, fmt.Errorf("NNTP connection pool not available - no providers configured")
	}

	return m.pool, nil
}

// SetProviders creates/recreates the pool with new providers
func (m *manager) SetProviders(providers []nntppool.UsenetProviderConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Shut down existing pool and metrics tracker if present
	if m.pool != nil {
		m.logger.InfoContext(m.ctx, "Shutting down existing NNTP connection pool")
		if m.metricsTracker != nil {
			m.metricsTracker.UpdatePersistence(m.ctx)
			m.metricsTracker.Stop()
			m.metricsTracker = nil
		}
		m.pool.Quit()
		m.pool = nil
	}

	// Return early if no providers (clear pool scenario)
	if len(providers) == 0 {
		m.logger.InfoContext(m.ctx, "No NNTP providers configured - pool cleared")
		return nil
	}

	// Create new pool with providers
	m.logger.InfoContext(m.ctx, "Creating NNTP connection pool", "provider_count", len(providers))
	pool, err := nntppool.NewConnectionPool(nntppool.Config{
		Providers:      providers,
		Logger:         m.logger,
		DelayType:      nntppool.DelayTypeFixed,
		RetryDelay:     10 * time.Millisecond,
		MinConnections: 0,
		DrainTimeout:   5 * time.Second,
	})
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
			m.metricsTracker.UpdatePersistence(m.ctx)
			m.metricsTracker.Stop()
			m.metricsTracker = nil
		}
		m.pool.Quit()
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

// ResetMetrics resets cumulative statistics
func (m *manager) ResetMetrics(ctx context.Context) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.metricsTracker == nil {
		return fmt.Errorf("metrics tracker not available")
	}

	return m.metricsTracker.ResetStats(ctx)
}

