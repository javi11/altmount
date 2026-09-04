package metadata

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/javi11/altmount/internal/utils"
)

// BackslashSweepResult reports what MigrateBackslashPaths did.
type BackslashSweepResult struct {
	// Moved counts .meta files relocated to their normalized path.
	Moved int
	// Skipped counts files left in place because the normalized path was already
	// taken by a different file.
	Skipped int
}

// MigrateBackslashPaths relocates .meta files that were persisted with a
// backslash in their on-disk name.
//
// Before path normalization existed, a release whose filename contained a
// backslash was written verbatim, e.g. "…/foo\bar.mkv.meta". Every accessor now
// resolves that same virtual path to "…/foo/bar.mkv.meta", so without this sweep
// the legacy file is stranded: invisible to Readdir, dropped by library sync
// without being flagged corrupted, not deletable, and skipped by the v2→v3
// migration scan while its store reference count is never decremented. That is a
// quieter failure than the deadlock the normalization fixes, and it lands on
// exactly the installs that hit the deadlock.
//
// The sweep is idempotent: a second run finds nothing, because a normalized name
// contains no backslash.
//
// It does not touch .ids/ symlinks. Those point at the old location and are left
// dangling by the move, which is what CleanupOrphanedIDSymlinks already exists to
// resolve.
func (ms *MetadataService) MigrateBackslashPaths(ctx context.Context) (BackslashSweepResult, error) {
	var result BackslashSweepResult

	if _, err := os.Stat(ms.rootPath); os.IsNotExist(err) {
		return result, nil
	}

	idsRoot := filepath.Join(ms.rootPath, ".ids")

	// Collect before moving. Renaming entries during the walk would have the
	// walker descend into directories that are being emptied underneath it.
	var stranded []string

	err := filepath.WalkDir(ms.rootPath, func(p string, d fs.DirEntry, walkErr error) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if walkErr != nil {
			return nil // an unreadable entry is not a reason to abandon the sweep
		}

		// .ids holds symlinks keyed by ID rather than by path; nothing in there
		// is a .meta to relocate.
		if d.IsDir() && p == idsRoot {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".meta") {
			return nil
		}

		rel, relErr := filepath.Rel(ms.rootPath, p)
		if relErr != nil || !strings.Contains(rel, `\`) {
			return nil
		}

		stranded = append(stranded, rel)
		return nil
	})
	if err != nil {
		return result, fmt.Errorf("scanning for backslash metadata paths: %w", err)
	}

	for _, rel := range stranded {
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		default:
		}

		oldPath := filepath.Join(ms.rootPath, rel)
		virtualPath := strings.TrimSuffix(rel, ".meta")
		newPath := ms.metaFilePath(virtualPath)

		if newPath == oldPath {
			continue
		}

		// Never clobber. A normalized file already sitting there is a different
		// import that legitimately owns the name.
		if _, statErr := os.Stat(newPath); statErr == nil {
			slog.WarnContext(ctx, "Leaving backslash metadata in place, normalized path already taken",
				"path", oldPath, "normalized", newPath)
			result.Skipped++
			continue
		}

		if err := os.MkdirAll(filepath.Dir(newPath), 0755); err != nil {
			return result, fmt.Errorf("creating destination for %s: %w", rel, err)
		}
		if err := os.Rename(oldPath, newPath); err != nil {
			return result, fmt.Errorf("relocating %s: %w", rel, err)
		}

		// The .id sidecar rides along; its absence is normal.
		if err := os.Rename(oldPath+".id", newPath+".id"); err != nil && !os.IsNotExist(err) {
			slog.WarnContext(ctx, "Moved metadata but not its .id sidecar",
				"path", oldPath+".id", "error", err)
		}

		utils.RemoveEmptyDirs(ms.rootPath, filepath.Dir(oldPath))

		slog.InfoContext(ctx, "Relocated metadata written with a backslash path",
			"from", oldPath, "to", newPath)
		result.Moved++
	}

	if result.Moved > 0 || result.Skipped > 0 {
		slog.InfoContext(ctx, "Backslash metadata sweep complete",
			"moved", result.Moved, "skipped", result.Skipped)
	}

	return result, nil
}
