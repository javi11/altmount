package pool

import (
	"context"
	"log/slog"

	"github.com/javi11/altmount/internal/config"
)

// RegisterConfigHandlers registers handlers for pool-related configuration changes
func RegisterConfigHandlers(ctx context.Context, configManager *config.Manager, poolManager Manager) {
	configManager.OnConfigChange(func(oldConfig, newConfig *config.Config) {
		slog.InfoContext(ctx, "Configuration updated")

		// Handle provider changes dynamically using comprehensive comparison
		providersChanged := !oldConfig.ProvidersEqual(newConfig)

		if providersChanged {
			slog.InfoContext(ctx, "NNTP providers changed - updating connection pool",
				"old_count", len(oldConfig.Providers),
				"new_count", len(newConfig.Providers))

			// Update pool with new providers
			providers := newConfig.ToNNTPProviders()
			if err := poolManager.SetProviders(providers); err != nil {
				slog.ErrorContext(ctx, "Failed to update NNTP connection pool", "err", err)
			} else {
				if len(providers) > 0 {
					slog.InfoContext(ctx, "NNTP connection pool updated successfully", "provider_count", len(providers))
				} else {
					slog.InfoContext(ctx, "NNTP connection pool cleared - no providers configured")
				}
			}
		}

		// Log changes that still require restart
		if oldConfig.Metadata.RootPath != newConfig.Metadata.RootPath {
			slog.InfoContext(ctx, "Metadata root path changed (restart required)",
				"old", oldConfig.Metadata.RootPath,
				"new", newConfig.Metadata.RootPath)
		}
	})
}
