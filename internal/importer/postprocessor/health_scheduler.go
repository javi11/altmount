package postprocessor

import (
	"context"
	"log/slog"
	"path/filepath"

	"github.com/javi11/altmount/internal/database"
)

// ScheduleHealthCheck schedules a health check for an imported file
func (c *Coordinator) ScheduleHealthCheck(ctx context.Context, resultingPath string) error {
	if c.healthRepo == nil {
		return nil // Health checks not configured
	}

	// Read metadata to get SourceNzbPath needed for health check
	fileMeta, err := c.metadataService.ReadFileMetadata(resultingPath)
	if err != nil {
		slog.WarnContext(ctx, "Failed to read metadata for health check scheduling",
			"path", resultingPath,
			"error", err)
		return err
	}

	if fileMeta == nil {
		return nil
	}

	// Add/Update health record with high priority
	err = c.healthRepo.AddFileToHealthCheck(ctx, resultingPath, 2, &fileMeta.SourceNzbPath, database.HealthPriorityNext)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to schedule immediate health check for imported file",
			"path", resultingPath,
			"error", err)
		return err
	}

	slog.InfoContext(ctx, "Scheduled immediate health check for imported file", "path", resultingPath)

	// Resolve pending repairs in the directory if configured
	c.resolvePendingRepairs(ctx, resultingPath)

	return nil
}

// resolvePendingRepairs resolves pending repairs in the same directory
func (c *Coordinator) resolvePendingRepairs(ctx context.Context, resultingPath string) {
	cfg := c.configGetter()
	resolveRepairs := true
	if cfg.Health.ResolveRepairOnImport != nil {
		resolveRepairs = *cfg.Health.ResolveRepairOnImport
	}

	if !resolveRepairs {
		return
	}

	parentDir := filepath.Dir(resultingPath)
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
