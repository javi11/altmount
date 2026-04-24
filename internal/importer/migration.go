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

// runNzbCompressionMigration is a one-time background task that compresses legacy
// plain .nzb files in the persistent .nzbs/ directory to .nzb.gz.
func (s *Service) runNzbCompressionMigration(ctx context.Context) {
	nzbDir := s.GetNzbFolder()
	sentinelPath := filepath.Join(nzbDir, migrationSentinelFile)

	if _, err := os.Stat(nzbDir); os.IsNotExist(err) {
		return
	}

	updater := func(ctx context.Context, oldPath, newPath string) {
		item, err := s.database.Repository.GetQueueItemByNzbPath(ctx, oldPath)
		if err != nil {
			s.log.WarnContext(ctx, "NZB migration: DB lookup failed",
				"old_path", oldPath, "error", err)
			return
		}
		if item == nil {
			return
		}
		if err := s.database.Repository.UpdateQueueItemNzbPath(ctx, item.ID, newPath); err != nil {
			s.log.WarnContext(ctx, "NZB migration: failed to update DB path",
				"old_path", oldPath, "new_path", newPath, "error", err)
		}
	}

	if err := migrateNzbsToGz(ctx, nzbDir, sentinelPath, updater); err != nil {
		s.log.WarnContext(ctx, "NZB compression migration failed", "error", err)
	}
}
