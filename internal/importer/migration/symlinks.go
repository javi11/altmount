package migration

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// ResolvedSymlink holds the migration row info needed to rewrite a symlink and
// later commit the rewrite to the database.
type ResolvedSymlink struct {
	FinalPath   string // AltMount-relative final path (may include "file:<episode>" expansion already applied)
	RowID       int64  // import_migrations.id
	QueueItemID *int64 // FK to import_queue.id, used to look up source_nzb_path
}

// RewrittenItem describes one symlink that was successfully rewritten on disk
// and now needs its DB side-effects committed.
type RewrittenItem struct {
	RowID       int64
	FinalPath   string
	QueueItemID *int64
}

// SymlinkLookup resolves source-specific GUIDs to AltMount final paths and
// commits the DB side-effects of a successful rewrite batch.
type SymlinkLookup interface {
	// Resolve returns the migration row info for (source, externalID), or
	// found=false if no matching row exists or has no final_path yet.
	Resolve(ctx context.Context, source, externalID string) (resolved ResolvedSymlink, found bool, err error)
	// CommitRewrites is called once after the walk with every successfully
	// rewritten item. Implementations should advance import_migrations status
	// and register each final_path in file_health.
	CommitRewrites(ctx context.Context, items []RewrittenItem) error
}

// RewriteReport summarizes results of a symlink rewrite operation.
type RewriteReport struct {
	Scanned            int
	Matched            int
	Rewritten          int
	SkippedWrongPrefix int      // symlinks whose target didn't point at sourceMountPath/.ids/ — usually a misconfigured mount path
	Unmatched          []string // symlink paths that had no matching migration row
	Errors             []string // errors encountered (non-fatal)
}

// RewriteLibrarySymlinks walks libraryPath, finds symlinks (real OS symlinks or
// rclone .rclonelink text files) whose target starts with sourceMountPath+"/.ids/",
// looks up the GUID in the lookup, and rewrites the target to
// filepath.Join(altmountPath, finalPath).
//
// On a non-dry-run, after the walk completes, CommitRewrites is called with
// every successfully rewritten item so the DB-backed lookup can advance the
// import_migrations status and register the files in file_health.
//
// If dryRun is true, no filesystem changes are made and CommitRewrites is not
// invoked, but the report is populated.
func RewriteLibrarySymlinks(
	ctx context.Context,
	libraryPath string,
	sourceMountPath string,
	altmountPath string,
	source string,
	lookup SymlinkLookup,
	dryRun bool,
) (*RewriteReport, error) {
	report := &RewriteReport{
		Unmatched: []string{},
		Errors:    []string{},
	}

	// Normalise source mount prefix used for matching.
	prefix := filepath.Clean(sourceMountPath) + "/.ids/"

	// rewritten accumulates items whose symlink was actually rewritten on disk.
	// CommitRewrites is invoked once after the walk so a single batched DB
	// update covers the whole run.
	var rewritten []RewrittenItem

	err := filepath.WalkDir(libraryPath, func(path string, d fs.DirEntry, walkErr error) error {
		// Propagate hard walk errors.
		if walkErr != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("walk error at %s: %v", path, walkErr))
			return nil
		}

		// Check context cancellation at every entry.
		if err := ctx.Err(); err != nil {
			return err
		}

		isSymlink := d.Type()&fs.ModeSymlink != 0
		isRcloneLink := !d.IsDir() && strings.HasSuffix(d.Name(), ".rclonelink")

		// Only process real OS symlinks and rclone .rclonelink text files.
		if !isSymlink && !isRcloneLink {
			return nil
		}

		report.Scanned++

		var target string
		if isSymlink {
			var err error
			target, err = os.Readlink(path)
			if err != nil {
				report.Errors = append(report.Errors, fmt.Sprintf("readlink %s: %v", path, err))
				return nil
			}
		} else {
			// .rclonelink: file content is the symlink target path.
			content, err := os.ReadFile(path)
			if err != nil {
				report.Errors = append(report.Errors, fmt.Sprintf("read rclonelink %s: %v", path, err))
				return nil
			}
			target = strings.TrimRight(string(content), "\r\n")
		}

		// Must target our source mount's .ids directory.
		if !strings.HasPrefix(target, prefix) {
			report.SkippedWrongPrefix++
			return nil
		}

		// Extract GUID: last path component of the target.
		// Normalise to upper-case: nzbdav .ids/ paths use lowercase UUIDs but
		// import_migrations stores the DavItem ID in the original uppercase form.
		guid := strings.ToUpper(filepath.Base(target))

		resolved, found, err := lookup.Resolve(ctx, source, guid)
		if err != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("lookup %s (guid=%s): %v", path, guid, err))
			return nil
		}
		if !found {
			report.Unmatched = append(report.Unmatched, path)
			return nil
		}

		report.Matched++

		if dryRun {
			return nil
		}

		// Build the new target path.
		newTarget := filepath.Join(altmountPath, strings.TrimPrefix(resolved.FinalPath, "/"))

		if isSymlink {
			// Atomic rewrite via temp file + rename.
			tmpPath := path + ".new"
			if err := os.Symlink(newTarget, tmpPath); err != nil {
				report.Errors = append(report.Errors, fmt.Sprintf("create temp symlink %s -> %s: %v", tmpPath, newTarget, err))
				return nil
			}
			if err := os.Rename(tmpPath, path); err != nil {
				_ = os.Remove(tmpPath)
				report.Errors = append(report.Errors, fmt.Sprintf("rename %s -> %s: %v", tmpPath, path, err))
				return nil
			}
		} else {
			// .rclonelink: overwrite file content with the new target path.
			if err := os.WriteFile(path, []byte(newTarget), 0o644); err != nil {
				report.Errors = append(report.Errors, fmt.Sprintf("write rclonelink %s: %v", path, err))
				return nil
			}
		}

		report.Rewritten++
		rewritten = append(rewritten, RewrittenItem{
			RowID:       resolved.RowID,
			FinalPath:   resolved.FinalPath,
			QueueItemID: resolved.QueueItemID,
		})
		return nil
	})
	if err != nil {
		return report, fmt.Errorf("walk library path %s: %w", libraryPath, err)
	}

	// Commit DB side-effects once for the whole batch. Filesystem rewrites
	// already happened, so a commit error is logged but not propagated as a
	// fatal walk error — the caller still gets the full report.
	if !dryRun && len(rewritten) > 0 {
		if err := lookup.CommitRewrites(ctx, rewritten); err != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("commit rewrites: %v", err))
			slog.WarnContext(ctx, "Failed to commit symlink rewrite DB side-effects",
				"source", source,
				"rewritten_count", len(rewritten),
				"error", err)
		}
	}

	return report, nil
}
