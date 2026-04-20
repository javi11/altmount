package migration

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// SymlinkLookup looks up the final AltMount path for a given source and external ID.
type SymlinkLookup interface {
	LookupFinalPath(ctx context.Context, source, externalID string) (finalPath string, found bool, err error)
	MarkSymlinksMigrated(ctx context.Context, ids []int64) error
}

// RewriteReport summarizes results of a symlink rewrite operation.
type RewriteReport struct {
	Scanned   int
	Matched   int
	Rewritten int
	Unmatched []string // symlink paths that had no matching migration row
	Errors    []string // errors encountered (non-fatal)
}

// RewriteLibrarySymlinks walks libraryPath, finds symlinks whose target starts
// with sourceMountPath+"/.ids/", looks up the GUID in the lookup, and rewrites
// the symlink target to filepath.Join(altmountPath, finalPath).
//
// If dryRun is true, no filesystem changes are made but the report is populated.
func RewriteLibrarySymlinks(
	ctx context.Context,
	libraryPath string,
	sourceMountPath string,
	altmountPath string,
	source string,
	lookup SymlinkLookup,
	dryRun bool,
) (*RewriteReport, error) {
	report := &RewriteReport{}

	// Normalise source mount prefix used for matching.
	prefix := filepath.Clean(sourceMountPath) + "/.ids/"

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

		// Only process symlinks.
		if d.Type()&fs.ModeSymlink == 0 {
			return nil
		}

		report.Scanned++

		target, err := os.Readlink(path)
		if err != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("readlink %s: %v", path, err))
			return nil
		}

		// Must target our source mount's .ids directory.
		if !strings.HasPrefix(target, prefix) {
			return nil
		}

		// Extract GUID: last path component of the target.
		guid := filepath.Base(target)

		finalPath, found, err := lookup.LookupFinalPath(ctx, source, guid)
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
		newTarget := filepath.Join(altmountPath, strings.TrimPrefix(finalPath, "/"))

		// Atomic rewrite: write to a temp name then rename.
		tmpPath := path + ".new"
		if err := os.Symlink(newTarget, tmpPath); err != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("create temp symlink %s -> %s: %v", tmpPath, newTarget, err))
			return nil
		}
		if err := os.Rename(tmpPath, path); err != nil {
			// Clean up the temp file on rename failure.
			_ = os.Remove(tmpPath)
			report.Errors = append(report.Errors, fmt.Sprintf("rename %s -> %s: %v", tmpPath, path, err))
			return nil
		}

		report.Rewritten++
		return nil
	})
	if err != nil {
		return report, fmt.Errorf("walk library path %s: %w", libraryPath, err)
	}

	return report, nil
}
