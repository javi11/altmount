package database

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"
)

// MediaRepository handles operations for media files table
type MediaRepository struct {
	db *sql.DB
}

// NewMediaRepository creates a new media repository
func NewMediaRepository(db *sql.DB) *MediaRepository {
	return &MediaRepository{
		db: db,
	}
}

// MediaFileInput represents input data for media file operations
type MediaFileInput struct {
	InstanceName string
	InstanceType string
	ExternalID   int64  // Movie ID or Episode ID
	FileID       *int64 // Movie File ID or Episode File ID (nullable)
	FilePath     string
	FileSize     *int64
}

// SyncResult represents the result of a sync operation
type SyncResult struct {
	Added   int
	Updated int
	Removed int
}

// SyncMediaFiles performs a complete sync operation for an instance
// This replaces all files for the instance with the provided list
func (r *MediaRepository) SyncMediaFiles(ctx context.Context, instanceName, instanceType string, files []MediaFileInput) (*SyncResult, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	result := &SyncResult{}
	now := time.Now()

	slog.DebugContext(ctx, "Starting media files sync",
		"instance", instanceName,
		"type", instanceType,
		"files", len(files))

	// Step 1: Upsert all current files
	if len(files) > 0 {
		for _, file := range files {
			var exists bool
			err := tx.QueryRowContext(ctx, `
				SELECT EXISTS(
					SELECT 1 FROM media_files 
					WHERE instance_name = ? AND instance_type = ? AND external_id = ?
				)`,
				instanceName, instanceType, file.ExternalID).Scan(&exists)
			if err != nil {
				return nil, fmt.Errorf("failed to check file existence: %w", err)
			}

			if exists {
				// Update existing record
				_, err = tx.ExecContext(ctx, `
					UPDATE media_files 
					SET file_id = ?, file_path = ?, file_size = ?, updated_at = ?
					WHERE instance_name = ? AND instance_type = ? AND external_id = ?`,
					file.FileID, file.FilePath, file.FileSize, now,
					instanceName, instanceType, file.ExternalID)
				if err != nil {
					return nil, fmt.Errorf("failed to update media file: %w", err)
				}
				result.Updated++
			} else {
				// Insert new record
				_, err = tx.ExecContext(ctx, `
					INSERT INTO media_files (instance_name, instance_type, external_id, file_id, file_path, file_size, created_at, updated_at)
					VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
					instanceName, instanceType, file.ExternalID, file.FileID, file.FilePath, file.FileSize, now, now)
				if err != nil {
					return nil, fmt.Errorf("failed to insert media file: %w", err)
				}
				result.Added++
			}
		}

		// Step 2: Remove files not in current sync (files that weren't updated in this sync)
		res, err := tx.ExecContext(ctx, `
			DELETE FROM media_files 
			WHERE instance_name = ? AND instance_type = ? AND updated_at < ?`,
			instanceName, instanceType, now)
		if err != nil {
			return nil, fmt.Errorf("failed to cleanup old media files: %w", err)
		}

		removed, err := res.RowsAffected()
		if err != nil {
			return nil, fmt.Errorf("failed to get removed count: %w", err)
		}
		result.Removed = int(removed)
	} else {
		// No files provided, remove all files for this instance
		res, err := tx.ExecContext(ctx, `
			DELETE FROM media_files 
			WHERE instance_name = ? AND instance_type = ?`,
			instanceName, instanceType)
		if err != nil {
			return nil, fmt.Errorf("failed to cleanup all media files: %w", err)
		}

		removed, err := res.RowsAffected()
		if err != nil {
			return nil, fmt.Errorf("failed to get removed count: %w", err)
		}
		result.Removed = int(removed)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	slog.DebugContext(ctx, "Media files sync completed",
		"instance", instanceName,
		"type", instanceType,
		"added", result.Added,
		"updated", result.Updated,
		"removed", result.Removed)

	return result, nil
}

// GetMediaFilesByPath returns media files matching a file path
// This can be used for health correlation
func (r *MediaRepository) GetMediaFilesByPath(ctx context.Context, filePath string) ([]MediaFile, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, instance_name, instance_type, external_id, file_id, file_path, file_size, created_at, updated_at
		FROM media_files 
		WHERE file_path = ?
		ORDER BY instance_name, instance_type`,
		filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to query media files by path: %w", err)
	}
	defer rows.Close()

	var files []MediaFile
	for rows.Next() {
		var file MediaFile
		err := rows.Scan(
			&file.ID,
			&file.InstanceName,
			&file.InstanceType,
			&file.ExternalID,
			&file.FileID,
			&file.FilePath,
			&file.FileSize,
			&file.CreatedAt,
			&file.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan media file: %w", err)
		}
		files = append(files, file)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating media files: %w", err)
	}

	return files, nil
}

// GetMediaFilesByInstance returns all media files for a specific instance
func (r *MediaRepository) GetMediaFilesByInstance(ctx context.Context, instanceName, instanceType string) ([]MediaFile, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, instance_name, instance_type, external_id, file_id, file_path, file_size, created_at, updated_at
		FROM media_files 
		WHERE instance_name = ? AND instance_type = ?
		ORDER BY file_path`,
		instanceName, instanceType)
	if err != nil {
		return nil, fmt.Errorf("failed to query media files by instance: %w", err)
	}
	defer rows.Close()

	var files []MediaFile
	for rows.Next() {
		var file MediaFile
		err := rows.Scan(
			&file.ID,
			&file.InstanceName,
			&file.InstanceType,
			&file.ExternalID,
			&file.FileID,
			&file.FilePath,
			&file.FileSize,
			&file.CreatedAt,
			&file.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan media file: %w", err)
		}
		files = append(files, file)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating media files: %w", err)
	}

	return files, nil
}

// GetMediaFilesCount returns the total count of media files
func (r *MediaRepository) GetMediaFilesCount(ctx context.Context) (int64, error) {
	var count int64
	err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM media_files").Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to get media files count: %w", err)
	}
	return count, nil
}

// GetMediaFilesCountByInstance returns the count of media files for a specific instance
func (r *MediaRepository) GetMediaFilesCountByInstance(ctx context.Context, instanceName, instanceType string) (int64, error) {
	var count int64
	err := r.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM media_files 
		WHERE instance_name = ? AND instance_type = ?`,
		instanceName, instanceType).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to get media files count by instance: %w", err)
	}
	return count, nil
}

// CleanupInstanceData removes all media files for a specific instance
// This can be called when an instance is removed from configuration
func (r *MediaRepository) CleanupInstanceData(ctx context.Context, instanceName, instanceType string) error {
	res, err := r.db.ExecContext(ctx, `
		DELETE FROM media_files 
		WHERE instance_name = ? AND instance_type = ?`,
		instanceName, instanceType)
	if err != nil {
		return fmt.Errorf("failed to cleanup instance data: %w", err)
	}

	removed, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get removed count: %w", err)
	}

	slog.InfoContext(ctx, "Cleaned up instance data",
		"instance", instanceName,
		"type", instanceType,
		"removed", removed)

	return nil
}
