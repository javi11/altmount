package pool

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

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

	// AddProvider adds a single provider to the running pool.
	// If no pool exists, a new one is created with this provider.
	AddProvider(provider nntppool.Provider) error

	// RemoveProvider removes a provider by its nntppool name (host:port or host:port+username).
	// If the last provider is removed, the pool is closed.
	RemoveProvider(name string) error
}

// manager implements the Manager interface
type manager struct {
	mu             sync.RWMutex
	pool           *nntppool.Client
	metricsTracker *MetricsTracker
	ctx            context.Context
	logger         *slog.Logger
}

// NewManager creates a new pool manager
func NewManager(ctx context.Context) Manager {
	return &manager{
		ctx:    ctx,
		logger: slog.Default().With("component", "pool"),
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

	// Create new pool with providers
	m.logger.InfoContext(m.ctx, "Creating NNTP connection pool", "provider_count", len(providers))
	pool, err := nntppool.NewClient(m.ctx, providers)
	if err != nil {
		return fmt.Errorf("failed to create NNTP connection pool: %w", err)
	}

	m.pool = pool

	// Start metrics tracker
	m.metricsTracker = NewMetricsTracker(pool)
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

// AddProvider adds a single provider to the running pool.
// If no pool exists yet, a new one is created with this single provider.
func (m *manager) AddProvider(provider nntppool.Provider) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.pool == nil {
		// No pool yet — create one with this single provider
		m.logger.InfoContext(m.ctx, "Creating NNTP connection pool for first provider", "provider", provider.Host)
		pool, err := nntppool.NewClient(m.ctx, []nntppool.Provider{provider}, nntppool.WithDispatchStrategy(nntppool.DispatchRoundRobin))
		if err != nil {
			return fmt.Errorf("failed to create NNTP connection pool: %w", err)
		}
		m.pool = pool
		m.metricsTracker = NewMetricsTracker(pool)
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
