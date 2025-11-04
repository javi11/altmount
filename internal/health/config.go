package health

import (
	"context"
	"log/slog"
	"sync"

	"github.com/javi11/altmount/internal/config"
)

// HealthSystemController manages dynamic enable/disable of the health system
type HealthSystemController struct {
	healthWorker      *HealthWorker
	librarySyncWorker *LibrarySyncWorker
	ctx               context.Context
	mu                sync.Mutex
}

// NewHealthSystemController creates a new health system controller
func NewHealthSystemController(
	healthWorker *HealthWorker,
	librarySyncWorker *LibrarySyncWorker,
	ctx context.Context,
) *HealthSystemController {
	return &HealthSystemController{
		healthWorker:      healthWorker,
		librarySyncWorker: librarySyncWorker,
		ctx:               ctx,
	}
}

// RegisterConfigChangeHandler registers a callback to handle health system enable/disable changes
func (hsc *HealthSystemController) RegisterConfigChangeHandler(configManager *config.Manager) {
	configManager.OnConfigChange(func(oldConfig, newConfig *config.Config) {
		hsc.mu.Lock()
		defer hsc.mu.Unlock()

		// Check if health enabled state has changed
		oldEnabled := oldConfig.Health.Enabled != nil && *oldConfig.Health.Enabled
		newEnabled := newConfig.Health.Enabled != nil && *newConfig.Health.Enabled

		if oldEnabled == newEnabled {
			// No change in enabled state
			return
		}

		if newEnabled {
			// Health system was disabled, now enabled - start workers
			slog.InfoContext(hsc.ctx, "Health system enabled via config change, starting workers")

			// Start health worker
			if !hsc.healthWorker.IsRunning() {
				if err := hsc.healthWorker.Start(hsc.ctx); err != nil {
					slog.ErrorContext(hsc.ctx, "Failed to start health worker", "error", err)
					return
				}
			}

			// Start library sync worker
			if !hsc.librarySyncWorker.IsRunning() {
				hsc.librarySyncWorker.StartLibrarySync(hsc.ctx)
			}

			slog.InfoContext(hsc.ctx, "Health system started successfully")
		} else {
			// Health system was enabled, now disabled - stop workers
			slog.InfoContext(hsc.ctx, "Health system disabled via config change, stopping workers")

			// Stop library sync worker first
			if hsc.librarySyncWorker.IsRunning() {
				hsc.librarySyncWorker.Stop(hsc.ctx)
			}

			// Stop health worker
			if hsc.healthWorker.IsRunning() {
				if err := hsc.healthWorker.Stop(hsc.ctx); err != nil {
					slog.ErrorContext(hsc.ctx, "Failed to stop health worker", "error", err)
					return
				}
			}

			slog.InfoContext(hsc.ctx, "Health system stopped successfully")
		}
	})
}
