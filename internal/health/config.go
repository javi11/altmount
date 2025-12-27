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

// SyncMetadataDates performs a background backfill of missing release dates from metadata
func (hsc *HealthSystemController) SyncMetadataDates(ctx context.Context) {
	go func() {
		slog.InfoContext(ctx, "Starting background release date backfill")

		paths, err := hsc.healthWorker.healthRepo.GetFilesMissingReleaseDate(ctx)
		if err != nil {
			slog.ErrorContext(ctx, "Failed to get files missing release date", "error", err)
			return
		}

		if len(paths) == 0 {
			slog.InfoContext(ctx, "No files missing release date found")
			return
		}

		slog.InfoContext(ctx, "Found files missing release date", "count", len(paths))

		var records []database.BackfillRecord
		for _, path := range paths {
			meta, err := hsc.healthWorker.metadataService.ReadFileMetadata(path)
			if err != nil || meta == nil || meta.ReleaseDate == 0 {
				continue
			}

			records = append(records, database.BackfillRecord{
				FilePath:    path,
				ReleaseDate: time.Unix(meta.ReleaseDate, 0),
			})

			// Process in small batches to not lock the DB for too long
			if len(records) >= 100 {
				if err := hsc.healthWorker.healthRepo.BackfillReleaseDates(ctx, records); err != nil {
					slog.ErrorContext(ctx, "Failed to backfill release dates batch", "error", err)
				}
				records = nil
			}
		}

		// Final batch
		if len(records) > 0 {
			if err := hsc.healthWorker.healthRepo.BackfillReleaseDates(ctx, records); err != nil {
				slog.ErrorContext(ctx, "Failed to backfill final release dates batch", "error", err)
			}
		}

		slog.InfoContext(ctx, "Finished background release date backfill")
	}()
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

			// Run background backfill
			hsc.SyncMetadataDates(hsc.ctx)

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
