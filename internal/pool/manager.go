package pool

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/nntppool/v3"
)

// Manager provides centralized NNTP connection pool management
type Manager interface {
	// GetPool returns the current connection pool or error if not available
	GetPool() (nntppool.NNTPClient, error)

	// SetProviders creates/recreates the pool with new providers
	SetProviders(providers []config.ProviderConfig) error

	// ClearPool shuts down and removes the current pool
	ClearPool() error

	// HasPool returns true if a pool is currently available
	HasPool() bool

	// GetMetrics returns the current pool metrics with calculated speeds
	GetMetrics() (MetricsSnapshot, error)
}

// manager implements the Manager interface
type manager struct {
	mu             sync.RWMutex
	pool           nntppool.NNTPClient
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
func (m *manager) GetPool() (nntppool.NNTPClient, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.pool == nil {
		return nil, fmt.Errorf("NNTP connection pool not available - no providers configured")
	}

	return m.pool, nil
}

// SetProviders creates/recreates the pool with new providers
func (m *manager) SetProviders(providers []config.ProviderConfig) error {
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

	maxConnections := 0
	for _, provider := range providers {
		if provider.MaxConnections > maxConnections {
			maxConnections = provider.MaxConnections
		}
	}

	// Create new client with maxInflight = 100
	client := nntppool.NewClient(maxConnections)

	// Create and add providers
	m.logger.InfoContext(m.ctx, "Creating NNTP connection pool", "provider_count", len(providers))

	for _, p := range providers {
		provider, err := createProvider(m.ctx, p)
		if err != nil {
			// Clean up already added providers
			client.Close()
			return fmt.Errorf("failed to create provider %s: %w", p.Host, err)
		}

		// Determine provider tier
		tier := nntppool.ProviderPrimary
		if p.IsBackupProvider != nil && *p.IsBackupProvider {
			tier = nntppool.ProviderBackup
		}

		err = client.AddProvider(provider, tier)
		if err != nil {

			client.Close()
			return fmt.Errorf("failed to add provider %s: %w", p.Host, err)
		}

		m.logger.InfoContext(m.ctx, "Added provider", "host", p.Host, "tier", tier)
	}

	m.pool = client

	// Start metrics tracker
	m.metricsTracker = NewMetricsTracker(client)
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

// createProvider creates a new nntppool.Provider from config
func createProvider(ctx context.Context, cfg config.ProviderConfig) (*nntppool.Provider, error) {
	var tlsConfig *tls.Config
	if cfg.TLS {
		tlsConfig = &tls.Config{
			InsecureSkipVerify: cfg.InsecureTLS,
			ServerName:         cfg.Host,
		}
	}

	address := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)

	return nntppool.NewProvider(ctx, nntppool.ProviderConfig{
		Address:               address,
		MaxConnections:        cfg.MaxConnections,
		InitialConnections:    0,
		InflightPerConnection: 10,
		MaxConnIdleTime:       30 * time.Second,
		MaxConnLifetime:       30 * time.Second,
		Auth:                  nntppool.Auth{Username: cfg.Username, Password: cfg.Password},
		TLSConfig:             tlsConfig,
		ProxyURL:              cfg.ProxyURL,
	})
}
