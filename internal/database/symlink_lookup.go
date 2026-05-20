package database

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/altmount/internal/importer/migration"
)

// DBSymlinkLookup adapts ImportMigrationRepository + HealthRepository to
// migration.SymlinkLookup. After a successful symlink rewrite batch it:
//
//  1. advances import_migrations.status to 'symlinks_migrated' for every
//     matched row, and
//  2. registers each rewritten final_path in file_health so the health
//     checker and VFS know about it.
//
// healthRepo and queueRepo may be nil; the lookup still resolves paths but
// CommitRewrites becomes a partial no-op for the missing side.
type DBSymlinkLookup struct {
	migRepo    *ImportMigrationRepository
	healthRepo *HealthRepository
	queueRepo  *Repository
	cfg        config.ConfigGetter
}

// NewDBSymlinkLookup creates a new DBSymlinkLookup. healthRepo and cfg are
// required to register rewritten files in file_health; queueRepo is used to
// resolve source_nzb_path from queue_item_id. Pass nil to skip those side
// effects.
func NewDBSymlinkLookup(migRepo *ImportMigrationRepository, healthRepo *HealthRepository, queueRepo *Repository, cfg config.ConfigGetter) *DBSymlinkLookup {
	return &DBSymlinkLookup{
		migRepo:    migRepo,
		healthRepo: healthRepo,
		queueRepo:  queueRepo,
		cfg:        cfg,
	}
}

// Resolve returns the migration row info for the given source and externalID.
// Returns found=false when no matching row exists or the row has no final_path.
//
// Season-pack episode rows use a "file:<episodeFilename>" relative_path to signal
// that final_path stores the season directory and the episode path must be computed
// by joining them. This keeps MarkImported simple (always stores the directory)
// while allowing per-episode resolution here.
func (l *DBSymlinkLookup) Resolve(ctx context.Context, source, externalID string) (migration.ResolvedSymlink, bool, error) {
	row, err := l.migRepo.LookupByExternalID(ctx, source, externalID)
	if err != nil {
		return migration.ResolvedSymlink{}, false, fmt.Errorf("lookup final path (source=%s, id=%s): %w", source, externalID, err)
	}
	if row == nil || row.FinalPath == nil {
		return migration.ResolvedSymlink{}, false, nil
	}
	finalPath := *row.FinalPath
	if episodeFilename, ok := strings.CutPrefix(row.RelativePath, "file:"); ok && episodeFilename != "" {
		finalPath = filepath.Join(finalPath, episodeFilename)
	}
	return migration.ResolvedSymlink{
		FinalPath:   finalPath,
		RowID:       row.ID,
		QueueItemID: row.QueueItemID,
	}, true, nil
}

// CommitRewrites advances every matched row to status=symlinks_migrated and
// registers each rewritten path in file_health. Individual file_health failures
// are collected and joined, never aborted, because partial registration is
// better than none — the migration status update is already a single bulk
// statement.
func (l *DBSymlinkLookup) CommitRewrites(ctx context.Context, items []migration.RewrittenItem) error {
	if len(items) == 0 {
		return nil
	}

	// 1. Bulk-advance migration status. Idempotent (WHERE id IN (...)).
	ids := make([]int64, len(items))
	for i, it := range items {
		ids[i] = it.RowID
	}
	if err := l.migRepo.MarkSymlinksMigrated(ctx, ids); err != nil {
		return fmt.Errorf("advance import_migrations to symlinks_migrated: %w", err)
	}

	// 2. Register each rewritten file in file_health. Skip silently when the
	// caller didn't wire a health repo or config — the migration status update
	// is still valuable on its own.
	if l.healthRepo == nil || l.cfg == nil {
		return nil
	}

	cfg := l.cfg()
	if cfg == nil {
		return nil
	}
	maxRetries := cfg.GetMaxRetries()
	maxRepairRetries := cfg.GetMaxRepairRetries()

	var healthErrs []error
	for _, it := range items {
		sourceNzbPath := l.resolveSourceNzbPath(ctx, it.QueueItemID)
		filePath := it.FinalPath
		if err := l.healthRepo.AddFileToHealthCheck(
			ctx,
			filePath,
			&filePath,
			maxRetries,
			maxRepairRetries,
			sourceNzbPath,
			HealthPriorityNext,
		); err != nil {
			healthErrs = append(healthErrs, fmt.Errorf("register file_health for %s: %w", filePath, err))
			continue
		}
	}

	if len(healthErrs) > 0 {
		return errors.Join(healthErrs...)
	}
	return nil
}

// resolveSourceNzbPath looks up the import_queue row referenced by queueItemID
// and returns its nzb_path. Returns nil when queueItemID is nil, the queueRepo
// is unavailable, or the lookup fails — health rows can be created without it.
func (l *DBSymlinkLookup) resolveSourceNzbPath(ctx context.Context, queueItemID *int64) *string {
	if queueItemID == nil || l.queueRepo == nil {
		return nil
	}
	item, err := l.queueRepo.GetQueueItem(ctx, *queueItemID)
	if err != nil {
		slog.WarnContext(ctx, "Failed to look up queue item for source_nzb_path",
			"queue_item_id", *queueItemID, "error", err)
		return nil
	}
	if item == nil || item.NzbPath == "" {
		return nil
	}
	p := item.NzbPath
	return &p
}
