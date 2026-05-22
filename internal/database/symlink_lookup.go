package database

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/javi11/altmount/internal/importer/migration"
)

// DBSymlinkLookup adapts ImportMigrationRepository to migration.SymlinkLookup.
//
// CommitRewrites only advances import_migrations.status to 'symlinks_migrated'.
// Registration of the rewritten files in file_health is delegated to the
// library sync worker (triggered by the handler after a successful rewrite);
// that worker walks the library, reads each .meta file, and inserts file_health
// rows with proper library_path / source_nzb_path / release_date.
type DBSymlinkLookup struct {
	migRepo *ImportMigrationRepository
}

// NewDBSymlinkLookup creates a new DBSymlinkLookup wrapping the given repository.
func NewDBSymlinkLookup(migRepo *ImportMigrationRepository) *DBSymlinkLookup {
	return &DBSymlinkLookup{migRepo: migRepo}
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

// CommitRewrites bulk-advances every matched row to status=symlinks_migrated.
// Idempotent (WHERE id IN (...)). Re-runs are safe.
func (l *DBSymlinkLookup) CommitRewrites(ctx context.Context, items []migration.RewrittenItem) error {
	if len(items) == 0 {
		return nil
	}
	ids := make([]int64, len(items))
	for i, it := range items {
		ids[i] = it.RowID
	}
	if err := l.migRepo.MarkSymlinksMigrated(ctx, ids); err != nil {
		return fmt.Errorf("advance import_migrations to symlinks_migrated: %w", err)
	}
	return nil
}
