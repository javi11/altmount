package pool

import (
	"context"
	"crypto/tls"
	"fmt"
	"time"

	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/nntppool/v3"
)

// ProviderOptions configures provider creation behavior
type ProviderOptions struct {
	// ForceImmediateConnection forces 1 initial connection for connectivity testing
	ForceImmediateConnection bool
	// MaxConnections overrides config.MaxConnections (0 = use config value)
	MaxConnections int
	// ConnIdleTime overrides the default idle timeout (0 = default 30s)
	ConnIdleTime time.Duration
	// ConnLifetime overrides the default lifetime (0 = default 30s)
	ConnLifetime time.Duration
}

// NewProvider creates an nntppool.Provider from config with optional options.
func NewProvider(ctx context.Context, cfg config.ProviderConfig, opts ...ProviderOptions) (*nntppool.Provider, error) {
	var opt ProviderOptions
	if len(opts) > 0 {
		opt = opts[0]
	}

	var tlsConfig *tls.Config
	if cfg.TLS {
		tlsConfig = &tls.Config{
			InsecureSkipVerify: cfg.InsecureTLS,
			ServerName:         cfg.Host,
		}
	}

	maxConns := cfg.MaxConnections
	if opt.MaxConnections > 0 {
		maxConns = opt.MaxConnections
	}

	initialConns := 0
	if opt.ForceImmediateConnection {
		initialConns = 1
	}

	idleTime := 30 * time.Second
	if opt.ConnIdleTime > 0 {
		idleTime = opt.ConnIdleTime
	}

	lifetime := 30 * time.Second
	if opt.ConnLifetime > 0 {
		lifetime = opt.ConnLifetime
	}

	address := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	return nntppool.NewProvider(ctx, nntppool.ProviderConfig{
		Address:               address,
		MaxConnections:        maxConns,
		InitialConnections:    initialConns,
		InflightPerConnection: 50,
		MaxConnIdleTime:       idleTime,
		MaxConnLifetime:       lifetime,
		Auth:                  nntppool.Auth{Username: cfg.Username, Password: cfg.Password},
		TLSConfig:             tlsConfig,
		ProxyURL:              cfg.ProxyURL,
	})
}

// NewProviderFromTestRequest creates a provider from API test request fields.
// Used for testing connectivity with credentials not yet saved to config.
func NewProviderFromTestRequest(ctx context.Context, host string, port int, username, password string, useTLS, insecureTLS bool, proxyURL string) (*nntppool.Provider, error) {
	cfg := config.ProviderConfig{
		Host:           host,
		Port:           port,
		Username:       username,
		Password:       password,
		TLS:            useTLS,
		InsecureTLS:    insecureTLS,
		ProxyURL:       proxyURL,
		MaxConnections: 1,
	}
	return NewProvider(ctx, cfg, ProviderOptions{
		ForceImmediateConnection: true,
	})
}
