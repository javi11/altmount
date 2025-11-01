package pool

import (
	"log/slog"

	"github.com/javi11/altmount/internal/config"
)

// RegisterConfigHandlers registers handlers for pool-related configuration changes
func RegisterConfigHandlers(configManager *config.Manager, poolManager Manager, logger *slog.Logger) {
	configManager.OnConfigChange(func(oldConfig, newConfig *config.Config) {
		logger.Info("Configuration updated")

		// Handle provider changes dynamically using comprehensive comparison
		providersChanged := !oldConfig.ProvidersEqual(newConfig)

		if providersChanged {
			logger.Info("NNTP providers changed - updating connection pool",
				"old_count", len(oldConfig.Providers),
				"new_count", len(newConfig.Providers))

			// Update pool with new providers
			providers := newConfig.ToNNTPProviders()
			if err := poolManager.SetProviders(providers); err != nil {
				logger.Error("Failed to update NNTP connection pool", "err", err)
			} else {
				if len(providers) > 0 {
					logger.Info("NNTP connection pool updated successfully", "provider_count", len(providers))
				} else {
					logger.Info("NNTP connection pool cleared - no providers configured")
				}
			}
		}

		// Log changes that still require restart
		if oldConfig.Metadata.RootPath != newConfig.Metadata.RootPath {
			logger.Info("Metadata root path changed (restart required)",
				"old", oldConfig.Metadata.RootPath,
				"new", newConfig.Metadata.RootPath)
		}
	})
}
