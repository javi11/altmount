package database

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
)

// DBSymlinkLookup adapts ImportMigrationRepository to migration.SymlinkLookup.
type DBSymlinkLookup struct {
	repo *ImportMigrationRepository
}

// NewDBSymlinkLookup creates a new DBSymlinkLookup wrapping the given repository.
func NewDBSymlinkLookup(repo *ImportMigrationRepository) *DBSymlinkLookup {
	return &DBSymlinkLookup{repo: repo}
}

// LookupFinalPath returns the final AltMount path for the given source and externalID.
// Returns ("", false, nil) when no matching row exists or the row has no final_path.
//
// Season-pack episode rows use a "file:<episodeFilename>" relative_path to signal that
// final_path stores the season directory and the episode path must be computed by joining
// them. This keeps MarkImported simple (always stores the directory) while allowing
// per-episode resolution here.
func (l *DBSymlinkLookup) LookupFinalPath(ctx context.Context, source, externalID string) (string, bool, error) {
	row, err := l.repo.LookupByExternalID(ctx, source, externalID)
	if err != nil {
		return "", false, fmt.Errorf("lookup final path (source=%s, id=%s): %w", source, externalID, err)
	}
	if row == nil || row.FinalPath == nil {
		return "", false, nil
	}
	finalPath := *row.FinalPath
	if episodeFilename, ok := strings.CutPrefix(row.RelativePath, "file:"); ok && episodeFilename != "" {
		finalPath = filepath.Join(finalPath, episodeFilename)
	}
	return finalPath, true, nil
}

// MarkSymlinksMigrated sets status=symlinks_migrated for the given row IDs.
func (l *DBSymlinkLookup) MarkSymlinksMigrated(ctx context.Context, ids []int64) error {
	return l.repo.MarkSymlinksMigrated(ctx, ids)
}
