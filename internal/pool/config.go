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

		handleProviderChanges(ctx, oldConfig, newConfig, poolManager)

		// Log changes that still require restart
		if oldConfig.Metadata.RootPath != newConfig.Metadata.RootPath {
			slog.InfoContext(ctx, "Metadata root path changed (restart required)",
				"old", oldConfig.Metadata.RootPath,
				"new", newConfig.Metadata.RootPath)
		}
	})
}

// handleProviderChanges applies incremental provider changes to the pool.
// It uses AddProvider/RemoveProvider for individual changes and falls back
// to full SetProviders only when provider order changes (nntppool v4 has no reorder API).
func handleProviderChanges(ctx context.Context, oldConfig, newConfig *config.Config, poolManager Manager) {
	changes := oldConfig.ProvidersDiff(newConfig)

	// No field-level changes — check if order changed
	if changes == nil {
		if oldConfig.ProvidersOrderChanged(newConfig) {
			slog.InfoContext(ctx, "NNTP provider order changed - recreating connection pool")
			providers := newConfig.ToNNTPProviders()
			if err := poolManager.SetProviders(providers); err != nil {
				slog.ErrorContext(ctx, "Failed to recreate NNTP connection pool", "err", err)
			}
		}
		return
	}

	slog.InfoContext(ctx, "NNTP providers changed - applying incremental updates",
		"change_count", len(changes))

	for _, change := range changes {
		switch change.Type {
		case config.ProviderAdded:
			applyProviderAdded(ctx, change, poolManager)

		case config.ProviderRemoved:
			applyProviderRemoved(ctx, change, poolManager)

		case config.ProviderModified:
			applyProviderModified(ctx, change, poolManager)
		}
	}
}

// applyProviderAdded handles a newly added provider.
// Only adds to pool if the provider is enabled.
func applyProviderAdded(ctx context.Context, change config.ProviderChange, poolManager Manager) {
	p := change.NewProvider
	if p.Enabled == nil || !*p.Enabled {
		slog.InfoContext(ctx, "New provider is disabled - skipping pool add",
			"provider_id", change.ProviderID, "host", p.Host)
		return
	}

	slog.InfoContext(ctx, "Adding new provider to pool",
		"provider_id", change.ProviderID, "host", p.Host, "port", p.Port)

	if err := poolManager.AddProvider(p.ToNNTPProvider()); err != nil {
		slog.ErrorContext(ctx, "Failed to add provider to pool",
			"err", err, "provider_id", change.ProviderID)
	}
}

// applyProviderRemoved handles a removed provider.
// Only removes from pool if the provider was enabled.
func applyProviderRemoved(ctx context.Context, change config.ProviderChange, poolManager Manager) {
	p := change.OldProvider
	if p.Enabled == nil || !*p.Enabled {
		slog.InfoContext(ctx, "Removed provider was disabled - no pool change needed",
			"provider_id", change.ProviderID, "host", p.Host)
		return
	}

	name := p.NNTPPoolName()
	slog.InfoContext(ctx, "Removing provider from pool",
		"provider_id", change.ProviderID, "pool_name", name)

	if err := poolManager.RemoveProvider(name); err != nil {
		slog.ErrorContext(ctx, "Failed to remove provider from pool",
			"err", err, "provider_id", change.ProviderID, "pool_name", name)
	}
}

// applyProviderModified handles a modified provider.
// Transitions: disabled→disabled (skip), enabled→disabled (remove),
// disabled→enabled (add), enabled→enabled with changes (remove + add).
func applyProviderModified(ctx context.Context, change config.ProviderChange, poolManager Manager) {
	oldP := change.OldProvider
	newP := change.NewProvider
	wasEnabled := oldP.Enabled != nil && *oldP.Enabled
	isEnabled := newP.Enabled != nil && *newP.Enabled

	switch {
	case !wasEnabled && !isEnabled:
		// disabled → disabled: no pool change
		slog.DebugContext(ctx, "Modified provider remains disabled - no pool change",
			"provider_id", change.ProviderID)

	case wasEnabled && !isEnabled:
		// enabled → disabled: remove from pool
		name := oldP.NNTPPoolName()
		slog.InfoContext(ctx, "Provider disabled - removing from pool",
			"provider_id", change.ProviderID, "pool_name", name)
		if err := poolManager.RemoveProvider(name); err != nil {
			slog.ErrorContext(ctx, "Failed to remove disabled provider from pool",
				"err", err, "provider_id", change.ProviderID, "pool_name", name)
		}

	case !wasEnabled && isEnabled:
		// disabled → enabled: add to pool
		slog.InfoContext(ctx, "Provider enabled - adding to pool",
			"provider_id", change.ProviderID, "host", newP.Host, "port", newP.Port)
		if err := poolManager.AddProvider(newP.ToNNTPProvider()); err != nil {
			slog.ErrorContext(ctx, "Failed to add enabled provider to pool",
				"err", err, "provider_id", change.ProviderID)
		}

	case wasEnabled && isEnabled:
		// enabled → enabled with config changes: remove old, add new
		oldName := oldP.NNTPPoolName()
		slog.InfoContext(ctx, "Provider config changed - replacing in pool",
			"provider_id", change.ProviderID, "old_name", oldName,
			"new_host", newP.Host, "new_port", newP.Port)

		if err := poolManager.RemoveProvider(oldName); err != nil {
			slog.ErrorContext(ctx, "Failed to remove old provider config from pool",
				"err", err, "provider_id", change.ProviderID, "pool_name", oldName)
			return // Don't add new if remove failed — pool state is unclear
		}
		if err := poolManager.AddProvider(newP.ToNNTPProvider()); err != nil {
			slog.ErrorContext(ctx, "Failed to add updated provider config to pool",
				"err", err, "provider_id", change.ProviderID)
		}
	}
}
