package health

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/altmount/internal/database"
)

// HealthSystemController manages dynamic enable/disable of the health system
type HealthSystemController struct {
	healthWorker      *HealthWorker
	librarySyncWorker *LibrarySyncWorker
	mu                sync.Mutex
}

// NewHealthSystemController creates a new health system controller
func NewHealthSystemController(
	healthWorker *HealthWorker,
	librarySyncWorker *LibrarySyncWorker,
) *HealthSystemController {
	return &HealthSystemController{
		healthWorker:      healthWorker,
		librarySyncWorker: librarySyncWorker,
	}
}

// SyncMetadataDates performs a background backfill of missing release dates from metadata
func (hsc *HealthSystemController) SyncMetadataDates(ctx context.Context) {
	go func() {
		slog.InfoContext(ctx, "Starting background release date backfill")

		// Get items missing release dates in a large batch
		items, err := hsc.healthWorker.healthRepo.GetFilesMissingReleaseDate(ctx, 10000)
		if err != nil {
			slog.ErrorContext(ctx, "Failed to get files missing release date", "error", err)
			return
		}

		if len(items) == 0 {
			slog.InfoContext(ctx, "No files missing release date found")
			return
		}

		slog.InfoContext(ctx, "Found files missing release date", "count", len(items))

		var updates []database.BackfillUpdate
		for _, item := range items {
			meta, err := hsc.healthWorker.metadataService.ReadFileMetadata(item.FilePath)
			if err != nil || meta == nil || meta.ReleaseDate == 0 {
				continue
			}

			releaseDate := time.Unix(meta.ReleaseDate, 0)
			nextCheck := CalculateNextCheck(releaseDate, time.Now()) // initial check based on age

			updates = append(updates, database.BackfillUpdate{
				ID:               item.ID,
				ReleaseDate:      releaseDate,
				ScheduledCheckAt: nextCheck,
			})

			// Process in small batches to not lock the DB for too long
			if len(updates) >= 100 {
				if err := hsc.healthWorker.healthRepo.BackfillReleaseDates(ctx, updates); err != nil {
					slog.ErrorContext(ctx, "Failed to backfill release dates batch", "error", err)
				}
				updates = nil
			}
		}

		// Final batch
		if len(updates) > 0 {
			if err := hsc.healthWorker.healthRepo.BackfillReleaseDates(ctx, updates); err != nil {
				slog.ErrorContext(ctx, "Failed to backfill final release dates batch", "error", err)
			}
		}

		slog.InfoContext(ctx, "Finished background release date backfill")
	}()
}

// RegisterConfigChangeHandler registers a callback to handle health system enable/disable changes
func (hsc *HealthSystemController) RegisterConfigChangeHandler(ctx context.Context, configManager *config.Manager) {
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
			slog.InfoContext(ctx, "Health system enabled via config change, starting workers")

			// Start health worker
			if !hsc.healthWorker.IsRunning() {
				if err := hsc.healthWorker.Start(ctx); err != nil {
					slog.ErrorContext(ctx, "Failed to start health worker", "error", err)
					return
				}
			}

			// Start library sync worker
			if !hsc.librarySyncWorker.IsRunning() {
				hsc.librarySyncWorker.StartLibrarySync(ctx)
			}

			// Run background backfill
			hsc.SyncMetadataDates(ctx)

			slog.InfoContext(ctx, "Health system started successfully")
		} else {
			// Health system was enabled, now disabled - stop workers
			slog.InfoContext(ctx, "Health system disabled via config change, stopping workers")

			// Stop library sync worker first
			if hsc.librarySyncWorker.IsRunning() {
				hsc.librarySyncWorker.Stop(ctx)
			}

			// Stop health worker
			if hsc.healthWorker.IsRunning() {
				if err := hsc.healthWorker.Stop(ctx); err != nil {
					slog.ErrorContext(ctx, "Failed to stop health worker", "error", err)
					return
				}
			}

			slog.InfoContext(ctx, "Health system stopped successfully")
		}
	})
}
