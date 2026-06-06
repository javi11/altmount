package postprocessor

import (
	"context"
	"log/slog"
	"path"
	"path/filepath"
	"strings"

	"github.com/javi11/altmount/internal/database"
)

// maxDirExpansionDepth bounds the recursive walk of "DIR:" written-path entries.
// Import folders are shallow (an NZB folder, occasionally a Blu-ray structure from
// ISO expansion), so this is a safety net, not an expected limit.
const maxDirExpansionDepth = 8

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

	// Expand directory entries into per-file paths, and fall back to the
	// resulting path for legacy callers (single-file imports resolve to the
	// same thing).
	paths := c.expandWrittenPaths(writtenPaths)
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
	} else {
		// A successful import that schedules nothing means the file will only be
		// covered by library sync much later — and any pending repair in its
		// directory stays unresolved. Surface it instead of failing silently.
		slog.WarnContext(ctx, "No health checks scheduled for import - no readable file metadata found",
			"path", resultingPath, "written_paths", len(writtenPaths))
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

// expandWrittenPaths resolves "DIR:"-prefixed entries (whole-directory imports such
// as RAR/7z archives, which only report their NZB folder) into the per-file virtual
// paths beneath them by walking the metadata tree. Plain file entries pass through
// unchanged. Without this, archive imports would schedule no health checks at all:
// ReadFileMetadata("DIR:...") finds nothing and the loop above skips silently.
func (c *Coordinator) expandWrittenPaths(writtenPaths []string) []string {
	var out []string
	for _, p := range writtenPaths {
		dir, isDir := strings.CutPrefix(p, "DIR:")
		if !isDir {
			out = append(out, p)
			continue
		}
		out = append(out, c.listMetadataFilesRecursive(dir, 0)...)
	}
	return out
}

// listMetadataFilesRecursive returns the virtual file paths of all metadata files
// under virtualDir, recursing into subdirectories up to maxDirExpansionDepth.
func (c *Coordinator) listMetadataFilesRecursive(virtualDir string, depth int) []string {
	if depth > maxDirExpansionDepth || c.metadataService == nil {
		return nil
	}

	dirs, files, err := c.metadataService.ListDirectoryAll(virtualDir)
	if err != nil {
		c.log.Warn("Failed to list metadata directory for health check scheduling",
			"dir", virtualDir, "error", err)
		return nil
	}

	var out []string
	for _, f := range files {
		out = append(out, path.Join(virtualDir, f))
	}
	for _, d := range dirs {
		out = append(out, c.listMetadataFilesRecursive(path.Join(virtualDir, d.Name()), depth+1)...)
	}
	return out
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
