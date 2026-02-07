package postprocessor

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/javi11/altmount/internal/database"
)

// ScheduleHealthCheck schedules a health check for an imported file or all files in a directory
func (c *Coordinator) ScheduleHealthCheck(ctx context.Context, resultingPath string) error {
	if c.healthRepo == nil {
		return nil // Health checks not configured
	}

	// Resolve the absolute path in metadata
	metadataPath := c.metadataService.GetMetadataDirectoryPath(resultingPath)
	fileInfo, err := os.Stat(metadataPath)
	if err != nil {
		// If stat fails, check if it's a .meta file (single file case)
		metaFile := metadataPath + ".meta"
		if _, metaErr := os.Stat(metaFile); metaErr == nil {
			return c.scheduleSingleFileHealthCheck(ctx, resultingPath)
		}
		return fmt.Errorf("failed to stat metadata path for scheduling: %w", err)
	}

	if !fileInfo.IsDir() {
		return c.scheduleSingleFileHealthCheck(ctx, resultingPath)
	}

	// It's a directory - walk and schedule all files
	return filepath.WalkDir(metadataPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(d.Name(), ".meta") {
			return nil
		}

		// Calculate relative path from metadata root
		relPath, err := filepath.Rel(c.configGetter().Metadata.RootPath, path)
		if err != nil {
			return nil
		}

		// Remove .meta extension
		virtualPath := strings.TrimSuffix(relPath, ".meta")
		return c.scheduleSingleFileHealthCheck(ctx, virtualPath)
	})
}

// scheduleSingleFileHealthCheck schedules a health check for one specific file
func (c *Coordinator) scheduleSingleFileHealthCheck(ctx context.Context, virtualPath string) error {
	// Read metadata to get SourceNzbPath needed for health check
	fileMeta, err := c.metadataService.ReadFileMetadata(virtualPath)
	if err != nil || fileMeta == nil {
		return nil
	}

	// Add/Update health record with high priority
	err = c.healthRepo.AddFileToHealthCheck(ctx, virtualPath, 2, &fileMeta.SourceNzbPath, database.HealthPriorityNext)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to schedule health check for file",
			"path", virtualPath,
			"error", err)
		return err
	}

	slog.InfoContext(ctx, "Scheduled immediate health check for file", "path", virtualPath)

	// Resolve pending repairs in the directory if configured
	c.resolvePendingRepairs(ctx, virtualPath)

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
