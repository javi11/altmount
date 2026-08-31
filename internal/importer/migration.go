package importer

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/javi11/altmount/internal/nzbfile"
)

const migrationSentinelFile = ".migration_nzb_compressed_v1"

// migrateNzbsToGz compresses all plain .nzb files in nzbDir to .nzb.gz.
// updater, if non-nil, is called with (oldPath, newPath) after each successful compression
// so the caller can update DB records. The sentinel file is written on completion.
func migrateNzbsToGz(ctx context.Context, nzbDir, sentinelPath string, updater func(ctx context.Context, oldPath, newPath string)) error {
	if _, err := os.Stat(sentinelPath); err == nil {
		return nil // already done
	}

	var count int
	err := filepath.WalkDir(nzbDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		lower := strings.ToLower(d.Name())
		if !strings.HasSuffix(lower, ".nzb") || strings.HasSuffix(lower, ".nzb.gz") {
			return nil
		}

		gzPath := path + ".gz"
		if compErr := nzbfile.Compress(path, gzPath); compErr != nil {
			slog.WarnContext(ctx, "NZB migration: failed to compress file",
				"path", path, "error", compErr)
			return nil
		}

		if updater != nil {
			updater(ctx, path, gzPath)
		}

		if rmErr := os.Remove(path); rmErr != nil && !os.IsNotExist(rmErr) {
			slog.WarnContext(ctx, "NZB migration: failed to delete original after compression",
				"path", path, "error", rmErr)
		}

		count++
		return nil
	})
	if err != nil {
		return fmt.Errorf("nzb migration walk failed: %w", err)
	}

	if writeErr := os.WriteFile(sentinelPath, []byte("done\n"), 0644); writeErr != nil {
		slog.WarnContext(ctx, "NZB migration: failed to write sentinel file",
			"path", sentinelPath, "error", writeErr)
	}

	if count > 0 {
		slog.InfoContext(ctx, "NZB compression migration complete", "compressed", count)
	}
	return nil
}

