package postprocessor

import (
	"context"
	"log/slog"
	"path/filepath"

	"github.com/javi11/altmount/internal/database"
)

// ScheduleHealthCheck schedules an immediate health check for an imported file.
// Multi-file imports (e.g. season packs) schedule one check per written virtual
// file — resultingPath is a directory there, so the per-file paths are the only
// way each episode gets verified right after import and a partially-dead pack
// surfaces immediately. Single-file imports keep the old behavior (one check on
// resultingPath).
func (c *Coordinator) ScheduleHealthCheck(ctx context.Context, item *database.ImportQueueItem, resultingPath string, writtenPaths []string) error {
	if c.healthRepo == nil {
		return nil // Health checks not configured
	}

	// Prefer the explicit per-file list; fall back to the resulting path for
	// legacy callers (single-file imports resolve to the same thing).
	paths := writtenPaths
	if len(paths) == 0 {
		paths = []string{resultingPath}
	}

	cfg := c.configGetter()
	var indexer *string = nil
	if item != nil {
		indexer = item.Indexer
	}

	scheduled := 0
	var lastErr error
	repairDirs := make(map[string]struct{})
	for _, path := range paths {
		// Read metadata to get SourceNzbPath needed for health check
		fileMeta, err := c.metadataService.ReadFileMetadata(path)
		if err != nil {
			slog.WarnContext(ctx, "Failed to read metadata for health check scheduling",
				"path", path,
				"error", err)
			lastErr = err
			continue
		}
		if fileMeta == nil {
			continue
		}

		// Add/Update health record with high priority
		filePath := path
		if err := c.healthRepo.AddFileToHealthCheckWithMetadata(ctx, filePath, &filePath, cfg.GetMaxRetries(), cfg.GetMaxRepairRetries(), &fileMeta.SourceNzbPath, database.HealthPriorityNext, nil, nil, indexer); err != nil {
			slog.ErrorContext(ctx, "Failed to schedule immediate health check for imported file",
				"path", path,
				"error", err)
			lastErr = err
			continue
		}
		scheduled++
		repairDirs[filepath.Dir(path)] = struct{}{}
	}

	if scheduled > 0 {
		slog.InfoContext(ctx, "Scheduled immediate health check for imported files",
			"path", resultingPath, "files", scheduled)
	}

	// Resolve pending repairs in the affected directories if configured
	for dir := range repairDirs {
		c.resolvePendingRepairsInDir(ctx, dir)
	}

	if scheduled == 0 {
		return lastErr
	}
	return nil
}

// resolvePendingRepairsInDir resolves pending repairs in the given directory
func (c *Coordinator) resolvePendingRepairsInDir(ctx context.Context, parentDir string) {
	cfg := c.configGetter()
	resolveRepairs := true
	if cfg.Health.ResolveRepairOnImport != nil {
		resolveRepairs = *cfg.Health.ResolveRepairOnImport
	}

	if !resolveRepairs {
		return
	}

	if parentDir == "." || parentDir == "/" {
		return
	}

	count, err := c.healthRepo.ResolvePendingRepairsInDirectory(ctx, parentDir)
	if err == nil && count > 0 {
		slog.InfoContext(ctx, "Resolved pending repairs in directory due to new import",
			"directory", parentDir,
			"resolved_count", count)
	}
}
