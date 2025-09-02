package pool

import (
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/javi11/nntppool"
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
}

// manager implements the Manager interface
type manager struct {
	mu     sync.RWMutex
	pool   nntppool.UsenetConnectionPool
	logger *slog.Logger
}

// NewManager creates a new pool manager
func NewManager(logger *slog.Logger) Manager {
	if logger == nil {
		logger = slog.Default()
	}

	return &manager{
		logger: logger,
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

	// Shut down existing pool if present
	if m.pool != nil {
		m.logger.Info("Shutting down existing NNTP connection pool")
		m.pool.Quit()
		m.pool = nil
	}

	// Return early if no providers (clear pool scenario)
	if len(providers) == 0 {
		m.logger.Info("No NNTP providers configured - pool cleared")
		return nil
	}

	// Create new pool with providers
	m.logger.Info("Creating NNTP connection pool", "provider_count", len(providers))
	pool, err := nntppool.NewConnectionPool(nntppool.Config{
		Providers:  providers,
		Logger:     m.logger,
		DelayType:  nntppool.DelayTypeFixed,
		RetryDelay: 10 * time.Millisecond,
	})
	if err != nil {
		return fmt.Errorf("failed to create NNTP connection pool: %w", err)
	}

	m.pool = pool
	m.logger.Info("NNTP connection pool created successfully")
	return nil
}

// ClearPool shuts down and removes the current pool
func (m *manager) ClearPool() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.pool != nil {
		m.logger.Info("Clearing NNTP connection pool")
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
