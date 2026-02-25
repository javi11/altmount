package database

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// HealthRepository handles file health database operations
type HealthRepository struct {
	db *sql.DB
}

// NewHealthRepository creates a new health repository
func NewHealthRepository(db *sql.DB) *HealthRepository {
	return &HealthRepository{
		db: db,
	}
}

// UpdateFileHealth updates or inserts a file health record
func (r *HealthRepository) UpdateFileHealth(ctx context.Context, filePath string, status HealthStatus, errorMessage *string, sourceNzbPath *string, errorDetails *string, noRetry bool) error {
	filePath = strings.TrimPrefix(filePath, "/")
	query := `
		INSERT INTO file_health (file_path, status, last_checked, last_error, source_nzb_path, error_details, retry_count, max_retries, repair_retry_count, created_at, updated_at, scheduled_check_at, priority)
		VALUES (?, ?, datetime('now'), ?, ?, ?, CASE WHEN ? THEN 1 ELSE 0 END, 2, 0, datetime('now'), datetime('now'), datetime('now'), CASE WHEN ? THEN 2 ELSE 0 END)
		ON CONFLICT(file_path) DO UPDATE SET
		status = excluded.status,
		last_checked = datetime('now'),
		last_error = excluded.last_error,
		source_nzb_path = COALESCE(excluded.source_nzb_path, source_nzb_path),
		error_details = excluded.error_details,
		retry_count = CASE WHEN ? THEN max_retries - 1 ELSE retry_count END,
		max_retries = excluded.max_retries,
		updated_at = datetime('now'),
		scheduled_check_at = datetime('now'),
		priority = CASE WHEN ? THEN 2 ELSE priority END
	`

	_, err := r.db.ExecContext(ctx, query, filePath, status, errorMessage, sourceNzbPath, errorDetails, noRetry, noRetry, noRetry, noRetry)
	if err != nil {
		return fmt.Errorf("failed to update file health: %w", err)
	}

	return nil
}

// GetFileHealth retrieves health record for a specific file
func (r *HealthRepository) GetFileHealth(ctx context.Context, filePath string) (*FileHealth, error) {
	filePath = strings.TrimPrefix(filePath, "/")
	query := `
		SELECT id, file_path, library_path, status, last_checked, last_error, retry_count, max_retries,
		       repair_retry_count, max_repair_retries, source_nzb_path,
		       error_details, created_at, updated_at, release_date, priority
		FROM file_health
		WHERE file_path = ?
	`

	var health FileHealth
	err := r.db.QueryRowContext(ctx, query, filePath).Scan(
		&health.ID, &health.FilePath, &health.LibraryPath, &health.Status, &health.LastChecked,
		&health.LastError, &health.RetryCount, &health.MaxRetries,
		&health.RepairRetryCount, &health.MaxRepairRetries,
		&health.SourceNzbPath, &health.ErrorDetails,
		&health.CreatedAt, &health.UpdatedAt, &health.ReleaseDate, &health.Priority,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get file health: %w", err)
	}

	return &health, nil
}

// GetFileHealthByID retrieves health record for a specific file by ID
func (r *HealthRepository) GetFileHealthByID(ctx context.Context, id int64) (*FileHealth, error) {
	query := `
		SELECT id, file_path, library_path, status, last_checked, last_error, retry_count, max_retries,
		       repair_retry_count, max_repair_retries, source_nzb_path,
		       error_details, created_at, updated_at, release_date, priority
		FROM file_health
		WHERE id = ?
	`

	var health FileHealth
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&health.ID, &health.FilePath, &health.LibraryPath, &health.Status, &health.LastChecked,
		&health.LastError, &health.RetryCount, &health.MaxRetries,
		&health.RepairRetryCount, &health.MaxRepairRetries,
		&health.SourceNzbPath, &health.ErrorDetails,
		&health.CreatedAt, &health.UpdatedAt, &health.ReleaseDate, &health.Priority,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get file health by ID: %w", err)
	}

	return &health, nil
}

// GetUnhealthyFiles returns files that need health checks (excluding repair_triggered files)
func (r *HealthRepository) GetUnhealthyFiles(ctx context.Context, limit int) ([]*FileHealth, error) {
	query := `
		SELECT id, file_path, status, last_checked, last_error, retry_count, max_retries,
		       repair_retry_count, max_repair_retries, source_nzb_path,
		       error_details, created_at, updated_at, release_date, scheduled_check_at,
			   library_path, priority
		FROM file_health
		WHERE scheduled_check_at IS NOT NULL
		  AND scheduled_check_at <= datetime('now')
		  AND retry_count < max_retries
		  AND status NOT IN ('repair_triggered', 'corrupted', 'checking')
		ORDER BY priority DESC, scheduled_check_at ASC
		LIMIT ?
	`

	rows, err := r.db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query files due for check: %w", err)
	}
	defer rows.Close()

	var files []*FileHealth
	for rows.Next() {
		var health FileHealth
		err := rows.Scan(
			&health.ID, &health.FilePath, &health.Status, &health.LastChecked,
			&health.LastError, &health.RetryCount, &health.MaxRetries,
			&health.RepairRetryCount, &health.MaxRepairRetries,
			&health.SourceNzbPath, &health.ErrorDetails,
			&health.CreatedAt, &health.UpdatedAt, &health.ReleaseDate,
			&health.ScheduledCheckAt,
			&health.LibraryPath,
			&health.Priority,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan file health: %w", err)
		}
		files = append(files, &health)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate unhealthy files: %w", err)
	}

	return files, nil
}

// SetPriority sets the priority for a file health record
func (r *HealthRepository) SetPriority(ctx context.Context, id int64, priority HealthPriority) error {
	query := `
		UPDATE file_health
		SET priority = ?,
		    updated_at = datetime('now')
		WHERE id = ?
	`

	_, err := r.db.ExecContext(ctx, query, priority, id)
	if err != nil {
		return fmt.Errorf("failed to set priority: %w", err)
	}

	return nil
}

// GetFilesForRepairNotification returns files that need repair notification (repair_triggered status)
func (r *HealthRepository) GetFilesForRepairNotification(ctx context.Context, limit int) ([]*FileHealth, error) {
	query := `
		SELECT id, file_path, status, last_checked, last_error, retry_count, max_retries,
		       repair_retry_count, max_repair_retries, source_nzb_path,
		       error_details, created_at, updated_at
		FROM file_health
		WHERE status = 'repair_triggered'
		  AND repair_retry_count < max_repair_retries
		  AND (scheduled_check_at IS NULL OR scheduled_check_at <= datetime('now'))
		ORDER BY last_checked ASC
		LIMIT ?
	`

	rows, err := r.db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query files for repair notification: %w", err)
	}
	defer rows.Close()

	var files []*FileHealth
	for rows.Next() {
		var health FileHealth
		err := rows.Scan(
			&health.ID, &health.FilePath, &health.Status, &health.LastChecked,
			&health.LastError, &health.RetryCount, &health.MaxRetries,
			&health.RepairRetryCount, &health.MaxRepairRetries,
			&health.SourceNzbPath, &health.ErrorDetails,
			&health.CreatedAt, &health.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan file health for repair notification: %w", err)
		}
		files = append(files, &health)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate files for repair notification: %w", err)
	}

	return files, nil
}

// IncrementRetryCount increments the retry count and schedules next check
func (r *HealthRepository) IncrementRetryCount(ctx context.Context, filePath string, errorMessage *string, errorDetails *string, nextCheck time.Time) error {
	query := `
		UPDATE file_health
		SET retry_count = retry_count + 1,
		    last_error = ?,
		    error_details = ?,
			status = 'pending',
			scheduled_check_at = ?,
		    updated_at = datetime('now')
		WHERE file_path = ?
	`

	_, err := r.db.ExecContext(ctx, query, errorMessage, errorDetails, nextCheck.UTC().Format("2006-01-02 15:04:05"), filePath)
	if err != nil {
		return fmt.Errorf("failed to increment retry count: %w", err)
	}

	return nil
}

// SetRepairTriggered sets a file's status to repair_triggered
func (r *HealthRepository) SetRepairTriggered(ctx context.Context, filePath string, errorMessage *string, errorDetails *string) error {
	query := `
		UPDATE file_health
		SET status = 'repair_triggered',
		    last_error = ?,
		    error_details = ?,
			scheduled_check_at = datetime('now', '+1 hour'),
		    updated_at = datetime('now')
		WHERE file_path = ?
	`

	result, err := r.db.ExecContext(ctx, query, errorMessage, errorDetails, filePath)
	if err != nil {
		return fmt.Errorf("failed to update file status to repair_triggered: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("no file found to update status: %s", filePath)
	}

	return nil
}

// SetCorrupted sets a file's status to corrupted
func (r *HealthRepository) SetCorrupted(ctx context.Context, filePath string, errorMessage *string, errorDetails *string) error {
	query := `
		UPDATE file_health
		SET status = 'corrupted',
		    last_error = ?,
		    error_details = ?,
		    scheduled_check_at = NULL,
		    updated_at = datetime('now')
		WHERE file_path = ?
	`

	result, err := r.db.ExecContext(ctx, query, errorMessage, errorDetails, filePath)
	if err != nil {
		return fmt.Errorf("failed to update file status to corrupted: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("no file found to update status: %s", filePath)
	}

	return nil
}

// IncrementRepairRetryCount increments the repair retry count
func (r *HealthRepository) IncrementRepairRetryCount(ctx context.Context, filePath string, errorMessage *string, errorDetails *string) error {
	query := `
		UPDATE file_health
		SET repair_retry_count = repair_retry_count + 1,
		    last_error = ?,
		    error_details = ?,
		    status = 'repair_triggered',
		    updated_at = datetime('now')
		WHERE file_path = ?
	`

	_, err := r.db.ExecContext(ctx, query, errorMessage, errorDetails, filePath)
	if err != nil {
		return fmt.Errorf("failed to increment repair retry count: %w", err)
	}

	return nil
}

// MarkAsCorrupted permanently marks a file as corrupted after all retries are exhausted
func (r *HealthRepository) MarkAsCorrupted(ctx context.Context, filePath string, finalError *string, errorDetails *string) error {
	query := `
		UPDATE file_health
		SET status = 'corrupted',
		    last_error = ?,
		    error_details = ?,
		    updated_at = datetime('now')
		WHERE file_path = ?
	`

	result, err := r.db.ExecContext(ctx, query, finalError, errorDetails, filePath)
	if err != nil {
		return fmt.Errorf("failed to mark file as corrupted: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("no file found to mark as corrupted: %s", filePath)
	}

	return nil
}

// GetHealthStats returns statistics about file health
func (r *HealthRepository) GetHealthStats(ctx context.Context) (map[HealthStatus]int, error) {
	query := `
		SELECT status, COUNT(*) 
		FROM file_health 
		GROUP BY status
	`

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to get health stats: %w", err)
	}
	defer rows.Close()

	stats := make(map[HealthStatus]int)
	for rows.Next() {
		var status HealthStatus
		var count int
		err := rows.Scan(&status, &count)
		if err != nil {
			return nil, fmt.Errorf("failed to scan health stats: %w", err)
		}
		stats[status] = count
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate health stats: %w", err)
	}

	return stats, nil
}

// SetRepairTriggeredByID sets a file's status to repair_triggered by ID
func (r *HealthRepository) SetRepairTriggeredByID(ctx context.Context, id int64, errorMessage *string, errorDetails *string) error {
	query := `
		UPDATE file_health
		SET status = 'repair_triggered',
		    last_error = ?,
		    error_details = ?,
			scheduled_check_at = datetime('now', '+1 hour'),
		    updated_at = datetime('now')
		WHERE id = ?
	`

	result, err := r.db.ExecContext(ctx, query, errorMessage, errorDetails, id)
	if err != nil {
		return fmt.Errorf("failed to update file status to repair_triggered by ID: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("no file found to update status with ID: %d", id)
	}

	return nil
}

// SetFileCheckingByID sets a file's status to 'checking' by ID
func (r *HealthRepository) SetFileCheckingByID(ctx context.Context, id int64) error {
	query := `
		UPDATE file_health 
		SET status = ?,
		    updated_at = datetime('now')
		WHERE id = ?
	`

	_, err := r.db.ExecContext(ctx, query, HealthStatusChecking, id)
	if err != nil {
		return fmt.Errorf("failed to set file status to checking by ID: %w", err)
	}

	return nil
}

// DeleteHealthRecordByID removes a specific health record from the database by ID
func (r *HealthRepository) DeleteHealthRecordByID(ctx context.Context, id int64) error {
	query := `DELETE FROM file_health WHERE id = ?`

	result, err := r.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete health record by ID: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("no health record found to delete with ID: %d", id)
	}

	return nil
}

// DeleteHealthRecord removes a specific health record from the database
func (r *HealthRepository) DeleteHealthRecord(ctx context.Context, filePath string) error {
	filePath = strings.TrimPrefix(filePath, "/")
	query := `DELETE FROM file_health WHERE file_path = ?`

	result, err := r.db.ExecContext(ctx, query, filePath)
	if err != nil {
		return fmt.Errorf("failed to delete health record: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("no health record found to delete: %s", filePath)
	}

	return nil
}

// DeleteHealthRecordsByPrefix removes health records that start with the given prefix
func (r *HealthRepository) DeleteHealthRecordsByPrefix(ctx context.Context, prefix string) (int64, error) {
	prefix = strings.TrimPrefix(prefix, "/")
	if prefix == "" {
		return 0, nil
	}

	query := `DELETE FROM file_health WHERE file_path = ? OR file_path LIKE ?`
	likePattern := prefix + "/%"

	result, err := r.db.ExecContext(ctx, query, prefix, likePattern)
	if err != nil {
		return 0, fmt.Errorf("failed to delete health records by prefix %s: %w", prefix, err)
	}

	return result.RowsAffected()
}

// CleanupHealthRecords removes health records for files that no longer exist
func (r *HealthRepository) CleanupHealthRecords(ctx context.Context, existingFiles []string) error {
	if len(existingFiles) == 0 {
		// Remove all records if no files exist
		_, err := r.db.ExecContext(ctx, "DELETE FROM file_health")
		return err
	}

	// Create placeholders for IN clause
	placeholders := make([]string, len(existingFiles))
	args := make([]any, len(existingFiles))
	for i, file := range existingFiles {
		placeholders[i] = "?"
		args[i] = file
	}

	placeholderStr := "?" + strings.Repeat(",?", len(existingFiles)-1)
	query := fmt.Sprintf(`
		DELETE FROM file_health 
		WHERE file_path NOT IN (%s)
	`, placeholderStr)

	_, err := r.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("failed to cleanup health records: %w", err)
	}

	return nil
}

// RegisterCorruptedFile adds or updates a file as corrupted and schedules it for immediate check/repair
func (r *HealthRepository) RegisterCorruptedFile(ctx context.Context, filePath string, libraryPath *string, errorMessage string) error {
	query := `
		INSERT INTO file_health (
			file_path, library_path, status, last_error, error_details,
			retry_count, max_retries, repair_retry_count, max_repair_retries,
			created_at, updated_at, scheduled_check_at, last_checked, priority
		)
		VALUES (?, ?, 'pending', ?, ?, 1, 2, 0, 3, datetime('now'), datetime('now'), datetime('now'), datetime('now'), 2)
		ON CONFLICT(file_path) DO UPDATE SET
			library_path = COALESCE(excluded.library_path, library_path),
			status = 'pending',
			last_error = excluded.last_error,
			error_details = excluded.error_details,
			retry_count = CASE WHEN max_retries > 0 THEN max_retries - 1 ELSE 0 END,
			scheduled_check_at = datetime('now'),
			last_checked = datetime('now'),
			updated_at = datetime('now'),
			priority = 2
	`

	_, err := r.db.ExecContext(ctx, query, filePath, libraryPath, errorMessage, errorMessage)
	if err != nil {
		return fmt.Errorf("failed to register corrupted file: %w", err)
	}

	return nil
}

// AddFileToHealthCheck adds a file to the health database for checking
func (r *HealthRepository) AddFileToHealthCheck(ctx context.Context, filePath string, maxRetries int, sourceNzbPath *string, priority HealthPriority) error {
	return r.AddFileToHealthCheckWithMetadata(ctx, filePath, maxRetries, sourceNzbPath, priority, nil)
}

// AddFileToHealthCheckWithMetadata adds a file to the health database for checking with metadata
func (r *HealthRepository) AddFileToHealthCheckWithMetadata(ctx context.Context, filePath string, maxRetries int, sourceNzbPath *string, priority HealthPriority, releaseDate *time.Time) error {
	var releaseDateStr any = nil
	if releaseDate != nil {
		releaseDateStr = releaseDate.UTC().Format("2006-01-02 15:04:05")
	}

	query := `
		INSERT INTO file_health (file_path, status, last_checked, retry_count, max_retries, repair_retry_count, max_repair_retries, source_nzb_path, priority, release_date, created_at, updated_at, scheduled_check_at)
		VALUES (?, ?, datetime('now'), 0, ?, 0, 3, ?, ?, ?, datetime('now'), datetime('now'), datetime('now'))
		ON CONFLICT(file_path) DO UPDATE SET
		status = excluded.status,
		retry_count = 0,
		repair_retry_count = 0,
		last_error = NULL,
		error_details = NULL,
		max_retries = excluded.max_retries,
		max_repair_retries = excluded.max_repair_retries,
		source_nzb_path = COALESCE(excluded.source_nzb_path, source_nzb_path),
		priority = excluded.priority,
		release_date = COALESCE(excluded.release_date, release_date),
		updated_at = datetime('now'),
		scheduled_check_at = datetime('now')
	`

	_, err := r.db.ExecContext(ctx, query, filePath, HealthStatusPending, maxRetries, sourceNzbPath, priority, releaseDateStr)
	if err != nil {
		return fmt.Errorf("failed to add file to health check: %w", err)
	}

	return nil
}

// ListHealthItems returns all health records with optional filtering, sorting and pagination
func (r *HealthRepository) ListHealthItems(ctx context.Context, statusFilter *HealthStatus, limit, offset int, sinceFilter *time.Time, search string, sortBy string, sortOrder string) ([]*FileHealth, error) {
	// Validate and prepare ORDER BY clause
	orderClause := "created_at DESC"
	if sortBy != "" {
		// Whitelist of allowed sort fields to prevent SQL injection
		allowedFields := map[string]string{
			"file_path":          "file_path",
			"created_at":         "created_at",
			"status":             "status",
			"priority":           "priority",
			"last_checked":       "last_checked",
			"scheduled_check_at": "scheduled_check_at",
		}

		if field, ok := allowedFields[sortBy]; ok {
			orderDirection := "ASC"
			if sortOrder == "desc" || sortOrder == "DESC" {
				orderDirection = "DESC"
			}
			orderClause = fmt.Sprintf("%s %s", field, orderDirection)
		}
	}

	query := fmt.Sprintf(`
		SELECT id, file_path, status, last_checked, last_error, retry_count, max_retries,
		       repair_retry_count, max_repair_retries, source_nzb_path,
		       error_details, created_at, updated_at, scheduled_check_at,
			   library_path
		FROM file_health
		WHERE (? IS NULL OR status = ?)
		  AND (? IS NULL OR created_at >= ?)
		  AND (? = '' OR file_path LIKE ? OR (source_nzb_path IS NOT NULL AND source_nzb_path LIKE ?))
		ORDER BY %s
		LIMIT ? OFFSET ?
	`, orderClause)

	// Prepare arguments for the query
	var statusParam any = nil
	if statusFilter != nil {
		statusParam = string(*statusFilter)
	}

	var sinceParam any = nil
	if sinceFilter != nil {
		sinceParam = sinceFilter.Format("2006-01-02 15:04:05")
	}

	// Prepare search parameter with wildcards
	searchPattern := "%" + search + "%"

	args := []any{
		statusParam, statusParam, // status filter (checked twice in WHERE clause)
		sinceParam, sinceParam, // since filter (checked twice in WHERE clause)
		search, searchPattern, searchPattern, // search filter (file_path and source_nzb_path)
		limit, offset,
	}

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query health items: %w", err)
	}
	defer rows.Close()

	var files []*FileHealth
	for rows.Next() {
		var health FileHealth
		err := rows.Scan(
			&health.ID, &health.FilePath, &health.Status, &health.LastChecked,
			&health.LastError, &health.RetryCount, &health.MaxRetries,
			&health.RepairRetryCount, &health.MaxRepairRetries,
			&health.SourceNzbPath, &health.ErrorDetails,
			&health.CreatedAt, &health.UpdatedAt, &health.ScheduledCheckAt,
			&health.LibraryPath,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan health item: %w", err)
		}
		files = append(files, &health)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate health items: %w", err)
	}

	return files, nil
}

// CountHealthItems returns the total count of health records with optional filtering
func (r *HealthRepository) CountHealthItems(ctx context.Context, statusFilter *HealthStatus, sinceFilter *time.Time, search string) (int, error) {
	query := `
		SELECT COUNT(*) 
		FROM file_health
		WHERE (? IS NULL OR status = ?)
		  AND (? IS NULL OR created_at >= ?)
		  AND (? = '' OR file_path LIKE ? OR (source_nzb_path IS NOT NULL AND source_nzb_path LIKE ?))
	`

	// Prepare arguments for the query
	var statusParam any = nil
	if statusFilter != nil {
		statusParam = string(*statusFilter)
	}

	var sinceParam any = nil
	if sinceFilter != nil {
		sinceParam = sinceFilter.Format("2006-01-02 15:04:05")
	}

	// Prepare search parameter with wildcards
	searchPattern := "%" + search + "%"

	args := []any{
		statusParam, statusParam, // status filter (checked twice in WHERE clause)
		sinceParam, sinceParam, // since filter (checked twice in WHERE clause)
		search, searchPattern, searchPattern, // search filter (file_path and source_nzb_path)
	}

	var count int
	err := r.db.QueryRowContext(ctx, query, args...).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count health items: %w", err)
	}

	return count, nil
}

// SetFileChecking sets a file's status to 'checking'
func (r *HealthRepository) SetFileChecking(ctx context.Context, filePath string) error {
	query := `
		UPDATE file_health 
		SET status = ?,
		    updated_at = datetime('now')
		WHERE file_path = ?
	`

	_, err := r.db.ExecContext(ctx, query, HealthStatusChecking, filePath)
	if err != nil {
		return fmt.Errorf("failed to set file status to checking: %w", err)
	}

	return nil
}

func (r *HealthRepository) ResetFileAllChecking(ctx context.Context) error {
	query := `
		UPDATE file_health
		SET status = ?,
		    updated_at = datetime('now'),
			scheduled_check_at = datetime('now')
		WHERE status = ?
	`

	_, err := r.db.ExecContext(ctx, query, HealthStatusPending, HealthStatusChecking)
	if err != nil {
		return fmt.Errorf("failed to reset all file statuses: %w", err)
	}

	return nil
}

// ResetStalePendingFiles resets pending files that have exhausted retries back to retry_count=0
// so they can be re-checked in the next health cycle. Called during worker startup.
func (r *HealthRepository) ResetStalePendingFiles(ctx context.Context) error {
	query := `UPDATE file_health
	          SET retry_count = 0,
	              updated_at = datetime('now'),
	              scheduled_check_at = datetime('now')
	          WHERE status = ? AND retry_count >= max_retries`
	result, err := r.db.ExecContext(ctx, query, HealthStatusPending)
	if err != nil {
		return fmt.Errorf("failed to reset stale pending files: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows > 0 {
		slog.InfoContext(ctx, "Reset stale pending files", "count", rows)
	}
	return nil
}

// DeleteHealthRecordsBulk removes multiple health records from the database
func (r *HealthRepository) DeleteHealthRecordsBulk(ctx context.Context, filePaths []string) error {
	if len(filePaths) == 0 {
		return nil
	}

	// Build placeholders for the IN clause
	placeholders := make([]string, len(filePaths))
	args := make([]any, len(filePaths))
	for i, path := range filePaths {
		placeholders[i] = "?"
		args[i] = strings.TrimPrefix(path, "/")
	}

	query := fmt.Sprintf(`DELETE FROM file_health WHERE file_path IN (%s)`, strings.Join(placeholders, ","))

	result, err := r.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("failed to delete health records: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("no health records found to delete")
	}

	return nil
}

// ResetHealthChecksBulk resets multiple health records to pending status
func (r *HealthRepository) ResetHealthChecksBulk(ctx context.Context, filePaths []string) (int, error) {
	if len(filePaths) == 0 {
		return 0, nil
	}

	// Build placeholders for the IN clause; prepend the status value as first arg
	placeholders := make([]string, len(filePaths))
	args := make([]any, 0, len(filePaths)+1)
	args = append(args, string(HealthStatusPending))
	for i, path := range filePaths {
		placeholders[i] = "?"
		args = append(args, path)
	}

	query := fmt.Sprintf(`
		UPDATE file_health
		SET status = ?,
		    retry_count = 0,
		    repair_retry_count = 0,
		    last_error = NULL,
		    error_details = NULL,
		    updated_at = datetime('now'),
			scheduled_check_at = datetime('now')
		WHERE file_path IN (%s)
	`, strings.Join(placeholders, ","))

	result, err := r.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("failed to reset health records: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to get rows affected: %w", err)
	}

	return int(rowsAffected), nil
}

// ResetAllHealthChecks resets all health records to pending status
func (r *HealthRepository) ResetAllHealthChecks(ctx context.Context) (int, error) {
	query := `
		UPDATE file_health
		SET status = 'pending',
		    retry_count = 0,
		    repair_retry_count = 0,
		    last_error = NULL,
		    error_details = NULL,
		    updated_at = datetime('now'),
			scheduled_check_at = datetime('now')
	`

	result, err := r.db.ExecContext(ctx, query)
	if err != nil {
		return 0, fmt.Errorf("failed to reset all health records: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to get rows affected: %w", err)
	}

	return int(rowsAffected), nil
}

// DeleteHealthRecordsByDate deletes health records older than the specified date with optional status filter
func (r *HealthRepository) DeleteHealthRecordsByDate(ctx context.Context, olderThan time.Time, statusFilter *HealthStatus) (int, error) {
	query := `
		DELETE FROM file_health
		WHERE created_at < ?
		  AND (? IS NULL OR status = ?)
	`

	// Prepare arguments for the query
	var statusParam any = nil
	if statusFilter != nil {
		statusParam = string(*statusFilter)
	}

	args := []any{
		olderThan.Format("2006-01-02 15:04:05"),
		statusParam, statusParam, // status filter (checked twice in WHERE clause)
	}

	result, err := r.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("failed to delete health records by date: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to get rows affected: %w", err)
	}

	return int(rowsAffected), nil
}

// AddHealthCheck adds or updates a health check record
func (r *HealthRepository) AddHealthCheck(
	ctx context.Context,
	filePath string,
	releaseDate time.Time,
	scheduledCheckAt time.Time,
	sourceNzbPath *string,
) error {
	query := `
		INSERT INTO file_health (
			file_path, status, last_checked, retry_count, max_retries,
			repair_retry_count, max_repair_retries, source_nzb_path,
			release_date, scheduled_check_at,
			created_at, updated_at
		)
		VALUES (?, ?, datetime('now'), 0, 2, 0, 3, ?, ?, ?, datetime('now'), datetime('now'))
		ON CONFLICT(file_path) DO UPDATE SET
			release_date = excluded.release_date,
			scheduled_check_at = excluded.scheduled_check_at,
			status = excluded.status,
			updated_at = datetime('now')
	`

	_, err := r.db.ExecContext(ctx, query, filePath, HealthStatusHealthy, sourceNzbPath, releaseDate.UTC(), scheduledCheckAt.UTC())
	if err != nil {
		return fmt.Errorf("failed to add health check: %w", err)
	}

	return nil
}

// UpdateScheduledCheckTime updates the scheduled check time for a file
func (r *HealthRepository) UpdateScheduledCheckTime(ctx context.Context, filePath string, nextCheckTime time.Time) error {
	filePath = strings.TrimPrefix(filePath, "/")
	query := `
		UPDATE file_health
		SET status = ?,
		    scheduled_check_at = ?,
		    updated_at = datetime('now')
		WHERE file_path = ?
	`

	result, err := r.db.ExecContext(ctx, query, HealthStatusHealthy, nextCheckTime.UTC().Format("2006-01-02 15:04:05"), filePath)
	if err != nil {
		return fmt.Errorf("failed to update scheduled check time: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("no automatic health check found for file: %s", filePath)
	}

	return nil
}

// MarkAsHealthy marks a file as healthy and clears all retry/error state
func (r *HealthRepository) MarkAsHealthy(ctx context.Context, filePath string, nextCheckTime time.Time) error {
	query := `
		UPDATE file_health
		SET status = ?,
		    scheduled_check_at = ?,
		    retry_count = 0,
		    repair_retry_count = 0,
		    last_error = NULL,
		    error_details = NULL,
		    updated_at = datetime('now')
		WHERE file_path = ?
	`

	result, err := r.db.ExecContext(ctx, query, HealthStatusHealthy, nextCheckTime.UTC().Format("2006-01-02 15:04:05"), filePath)
	if err != nil {
		return fmt.Errorf("failed to mark file as healthy: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("no health check found for file: %s", filePath)
	}

	return nil
}

// UpdateHealthStatusBulk updates multiple health records in a single transaction
func (r *HealthRepository) UpdateHealthStatusBulk(ctx context.Context, updates []HealthStatusUpdate) error {
	if len(updates) == 0 {
		return nil
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Prepare common statements
	stmtHealthy, err := tx.PrepareContext(ctx, `
		UPDATE file_health 
		SET status = 'healthy', scheduled_check_at = ?, retry_count = 0, 
		    repair_retry_count = 0, last_error = NULL, error_details = NULL, 
		    updated_at = datetime('now'), last_checked = datetime('now')
		WHERE file_path = ?
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare healthy statement: %w", err)
	}
	defer stmtHealthy.Close()

	stmtRetry, err := tx.PrepareContext(ctx, `
		UPDATE file_health 
		SET retry_count = retry_count + 1, last_error = ?, error_details = ?, 
		    status = 'pending', scheduled_check_at = ?, 
		    updated_at = datetime('now'), last_checked = datetime('now')
		WHERE file_path = ?
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare retry statement: %w", err)
	}
	defer stmtRetry.Close()

	stmtRepair, err := tx.PrepareContext(ctx, `
		UPDATE file_health 
		SET repair_retry_count = repair_retry_count + 1, last_error = ?, 
		    error_details = ?, status = 'repair_triggered', 
		    updated_at = datetime('now'), last_checked = datetime('now'),
			scheduled_check_at = datetime('now', '+1 hour')
		WHERE file_path = ?
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare repair statement: %w", err)
	}
	defer stmtRepair.Close()

	stmtCorrupted, err := tx.PrepareContext(ctx, `
		UPDATE file_health 
		SET status = 'corrupted', last_error = ?, error_details = ?, 
		    scheduled_check_at = NULL,
		    updated_at = datetime('now'), last_checked = datetime('now')
		WHERE file_path = ?
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare corrupted statement: %w", err)
	}
	defer stmtCorrupted.Close()

	for _, update := range updates {
		filePath := update.FilePath
		switch update.Status {
		case HealthStatusHealthy:
			_, err = stmtHealthy.ExecContext(ctx, update.ScheduledCheckAt, filePath)
		case HealthStatusPending:
			_, err = stmtRetry.ExecContext(ctx, update.ErrorMessage, update.ErrorDetails, update.ScheduledCheckAt, filePath)
		case HealthStatusRepairTriggered:
			_, err = stmtRepair.ExecContext(ctx, update.ErrorMessage, update.ErrorDetails, filePath)
		case HealthStatusCorrupted:
			_, err = stmtCorrupted.ExecContext(ctx, update.ErrorMessage, update.ErrorDetails, filePath)
		}

		if err != nil {
			return fmt.Errorf("failed to execute update for %s: %w", update.FilePath, err)
		}
	}

	return tx.Commit()
}

// UpdateType represents the type of health update
type UpdateType int

const (
	UpdateTypeHealthy     UpdateType = 1
	UpdateTypeRetry       UpdateType = 2
	UpdateTypeRepairRetry UpdateType = 3
	UpdateTypeCorrupted   UpdateType = 4
)

// HealthStatusUpdate represents a single update request for batch processing
type HealthStatusUpdate struct {
	Type             UpdateType
	FilePath         string
	Status           HealthStatus
	ErrorMessage     *string
	ErrorDetails     *string
	ScheduledCheckAt time.Time
}

// BackfillRecord represents a record used for metadata backfilling
type BackfillRecord struct {
	ID       int64
	FilePath string
}

// BackfillUpdate represents an update for release date backfilling
type BackfillUpdate struct {
	ID               int64
	ReleaseDate      time.Time
	ScheduledCheckAt time.Time
}

// GetAllHealthCheckPaths returns all health check file paths (memory optimized)
func (r *HealthRepository) GetAllHealthCheckPaths(ctx context.Context) ([]string, error) {
	query := `
		SELECT file_path
		FROM file_health
		ORDER BY file_path ASC
	`

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query health check paths: %w", err)
	}
	defer rows.Close()

	var paths []string
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return nil, fmt.Errorf("failed to scan file path: %w", err)
		}
		paths = append(paths, path)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate health check paths: %w", err)
	}

	return paths, nil
}

// GetAllHealthCheckRecords returns all health check records tracked in health system
func (r *HealthRepository) GetAllHealthCheckRecords(ctx context.Context) ([]AutomaticHealthCheckRecord, error) {
	query := `
		SELECT file_path, library_path, 
			   release_date, scheduled_check_at,
			   source_nzb_path, status
		FROM file_health
		ORDER BY file_path ASC
	`

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query health check paths: %w", err)
	}
	defer rows.Close()

	var records []AutomaticHealthCheckRecord
	for rows.Next() {
		var (
			path               string
			libraryPath        *string
			releaseDate        *time.Time
			scheduledCheckAtNT sql.NullTime
			sourceNzbPath      *string
			status             HealthStatus
		)

		if err := rows.Scan(&path, &libraryPath, &releaseDate, &scheduledCheckAtNT, &sourceNzbPath, &status); err != nil {
			return nil, fmt.Errorf("failed to scan file path: %w", err)
		}
		var scheduledCheckAt time.Time
		if scheduledCheckAtNT.Valid {
			scheduledCheckAt = scheduledCheckAtNT.Time
		}
		records = append(records, AutomaticHealthCheckRecord{
			FilePath:         path,
			LibraryPath:      libraryPath,
			ReleaseDate:      releaseDate,
			ScheduledCheckAt: &scheduledCheckAt,
			SourceNzbPath:    sourceNzbPath,
			Status:           status,
		})
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate health check paths: %w", err)
	}

	return records, nil
}

// GetFilesMissingReleaseDate returns a list of files that don't have a release date cached
func (r *HealthRepository) GetFilesMissingReleaseDate(ctx context.Context, limit int) ([]BackfillRecord, error) {
	query := `
		SELECT id, file_path
		FROM file_health
		WHERE release_date IS NULL
		LIMIT ?
	`

	rows, err := r.db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []BackfillRecord
	for rows.Next() {
		var rec BackfillRecord
		if err := rows.Scan(&rec.ID, &rec.FilePath); err != nil {
			return nil, err
		}
		records = append(records, rec)
	}

	return records, nil
}

// BackfillReleaseDates updates multiple health records with their release dates and next check times
func (r *HealthRepository) BackfillReleaseDates(ctx context.Context, updates []BackfillUpdate) error {
	if len(updates) == 0 {
		return nil
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		UPDATE file_health
		SET release_date = ?,
		    scheduled_check_at = ?,
		    updated_at = datetime('now')
		WHERE id = ?
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, up := range updates {
		_, err = stmt.ExecContext(ctx, up.ReleaseDate.UTC().Format("2006-01-02 15:04:05"), up.ScheduledCheckAt.UTC().Format("2006-01-02 15:04:05"), up.ID)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

// AutomaticHealthCheckRecord represents a batch insert record
type AutomaticHealthCheckRecord struct {
	FilePath         string
	LibraryPath      *string
	ReleaseDate      *time.Time
	ScheduledCheckAt *time.Time
	SourceNzbPath    *string
	Status           HealthStatus
}

// BatchAddAutomaticHealthChecks inserts multiple automatic health checks efficiently
func (r *HealthRepository) BatchAddAutomaticHealthChecks(ctx context.Context, records []AutomaticHealthCheckRecord) error {
	if len(records) == 0 {
		return nil
	}

	// SQLite has a limit on the number of parameters (typically 999)
	// Process in batches of 150 records (6 params each = 900 params per batch)
	const batchSize = 150

	for i := 0; i < len(records); i += batchSize {
		end := min(i+batchSize, len(records))

		batch := records[i:end]
		if err := r.batchInsertAutomaticHealthChecks(ctx, batch); err != nil {
			return fmt.Errorf("failed to insert batch starting at index %d: %w", i, err)
		}
	}

	return nil
}

// batchInsertAutomaticHealthChecks performs a single batch insert
func (r *HealthRepository) batchInsertAutomaticHealthChecks(ctx context.Context, records []AutomaticHealthCheckRecord) error {
	if len(records) == 0 {
		return nil
	}

	// Build the INSERT query with multiple value sets
	valueStrings := make([]string, len(records))
	args := make([]any, 0, len(records)*5)

	for i, record := range records {
		valueStrings[i] = "(?, ?, ?, datetime('now'), 0, 2, 0, 3, ?, ?, ?, datetime('now'), datetime('now'))"
		var releaseDateStr, scheduledCheckAtStr any = nil, nil
		if record.ReleaseDate != nil {
			releaseDateStr = record.ReleaseDate.UTC().Format("2006-01-02 15:04:05")
		}
		if record.ScheduledCheckAt != nil {
			scheduledCheckAtStr = record.ScheduledCheckAt.UTC().Format("2006-01-02 15:04:05")
		}

		args = append(args, record.FilePath, record.LibraryPath, HealthStatusHealthy, record.SourceNzbPath, releaseDateStr, scheduledCheckAtStr)
	}

	query := fmt.Sprintf(`
		INSERT INTO file_health (
			file_path, library_path, status, last_checked, retry_count, max_retries,
			repair_retry_count, max_repair_retries, source_nzb_path,
			release_date, scheduled_check_at,
			created_at, updated_at
		)
		VALUES %s
		ON CONFLICT(file_path) DO UPDATE SET
			library_path = COALESCE(excluded.library_path, library_path),
			status = CASE 
				WHEN source_nzb_path != excluded.source_nzb_path OR release_date != excluded.release_date THEN excluded.status 
				ELSE status 
			END,
			scheduled_check_at = CASE 
				WHEN source_nzb_path != excluded.source_nzb_path OR release_date != excluded.release_date THEN excluded.scheduled_check_at 
				ELSE scheduled_check_at 
			END,
			retry_count = CASE 
				WHEN source_nzb_path != excluded.source_nzb_path OR release_date != excluded.release_date THEN 0 
				ELSE retry_count 
			END,
			source_nzb_path = excluded.source_nzb_path,
			release_date = excluded.release_date,
			updated_at = datetime('now')
	`, strings.Join(valueStrings, ","))

	_, err := r.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("failed to batch insert health checks: %w", err)
	}

	return nil
}

// ResolvePendingRepairsInDirectory removes health records with repair_triggered or corrupted status
// that exist in the specified directory. This is used when a new file is imported
// into a directory, implying it is a replacement for the broken file.
func (r *HealthRepository) ResolvePendingRepairsInDirectory(ctx context.Context, dirPath string) (int64, error) {
	dirPath = strings.TrimPrefix(dirPath, "/")
	if dirPath == "" {
		return 0, nil
	}
	// Ensure directory path ends with separator to match files inside it
	if !strings.HasSuffix(dirPath, "/") {
		dirPath = dirPath + "/"
	}

	query := `
		DELETE FROM file_health
		WHERE file_path LIKE ?
		AND status IN ('repair_triggered', 'corrupted')
	`

	// Match paths starting with the directory
	likePattern := dirPath + "%"

	result, err := r.db.ExecContext(ctx, query, likePattern)
	if err != nil {
		return 0, fmt.Errorf("failed to resolve pending repairs in %s: %w", dirPath, err)
	}

	return result.RowsAffected()
}

// GetFilesWithoutLibraryPath returns all health records where library_path is NULL
func (r *HealthRepository) GetFilesWithoutLibraryPath(ctx context.Context) ([]*FileHealth, error) {
	query := `
		SELECT id, file_path, library_path, status, last_checked, last_error, retry_count, max_retries,
		       repair_retry_count, max_repair_retries, source_nzb_path,
		       error_details, created_at, updated_at, release_date, priority
		FROM file_health
		WHERE library_path IS NULL
		ORDER BY file_path ASC
	`

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query files without library path: %w", err)
	}
	defer rows.Close()

	var files []*FileHealth
	for rows.Next() {
		var health FileHealth
		err := rows.Scan(
			&health.ID, &health.FilePath, &health.LibraryPath, &health.Status, &health.LastChecked,
			&health.LastError, &health.RetryCount, &health.MaxRetries,
			&health.RepairRetryCount, &health.MaxRepairRetries,
			&health.SourceNzbPath, &health.ErrorDetails,
			&health.CreatedAt, &health.UpdatedAt, &health.ReleaseDate, &health.Priority,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan file health: %w", err)
		}
		files = append(files, &health)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate files without library path: %w", err)
	}

	return files, nil
}

// UpdateLibraryPath updates the library_path for a specific file
func (r *HealthRepository) UpdateLibraryPath(ctx context.Context, filePath string, libraryPath string) error {
	filePath = strings.TrimPrefix(filePath, "/")
	query := `
		UPDATE file_health
		SET library_path = ?,
		    updated_at = datetime('now')
		WHERE file_path = ?
	`

	result, err := r.db.ExecContext(ctx, query, libraryPath, filePath)
	if err != nil {
		return fmt.Errorf("failed to update library path: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("no health record found to update: %s", filePath)
	}

	return nil
}

// RenameHealthRecord updates the file_path of a health record or records under a directory after a MOVE operation
func (r *HealthRepository) RenameHealthRecord(ctx context.Context, oldPath, newPath string) error {
	oldPath = strings.TrimPrefix(oldPath, "/")
	newPath = strings.TrimPrefix(newPath, "/")

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// 1. Rename exact match
	_, err = tx.ExecContext(ctx, "UPDATE file_health SET file_path = ?, updated_at = datetime('now') WHERE file_path = ?", newPath, oldPath)
	if err != nil {
		return err
	}

	// 2. Rename children if it's a directory
	oldPrefix := oldPath + "/"
	newPrefix := newPath + "/"
	_, err = tx.ExecContext(ctx, `
		UPDATE file_health 
		SET file_path = ? || substr(file_path, ?),
		    updated_at = datetime('now')
		WHERE file_path LIKE ?`,
		newPrefix, len(oldPrefix)+1, oldPrefix+"%")
	if err != nil {
		return err
	}

	return tx.Commit()
}

// RelinkFileByFilename updates the file_path and library_path for a corrupted/triggered record that matches by filename
func (r *HealthRepository) RelinkFileByFilename(ctx context.Context, filename, filePath, libraryPath string) error {
	filePath = strings.TrimPrefix(filePath, "/")
	libraryPath = strings.TrimPrefix(libraryPath, "/")
	query := `
		UPDATE file_health
		SET file_path = ?,
		    library_path = ?,
		    status = 'pending',
		    updated_at = datetime('now'),
		    scheduled_check_at = datetime('now')
		WHERE (file_path LIKE ? OR file_path = ?)
		  AND status IN ('repair_triggered', 'corrupted')
	`

	likePattern := "%/" + filename
	_, err := r.db.ExecContext(ctx, query, filePath, libraryPath, likePattern, filename)
	if err != nil {
		return fmt.Errorf("failed to relink file by filename: %w", err)
	}

	return nil
}

// GetSystemState retrieves a persistent state value
func (r *HealthRepository) GetSystemState(ctx context.Context, key string) (string, error) {
	query := `SELECT value FROM system_state WHERE key = ?`
	var value string
	err := r.db.QueryRowContext(ctx, query, key).Scan(&value)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", nil
		}
		return "", fmt.Errorf("failed to get system state: %w", err)
	}
	return value, nil
}

// UpdateSystemState updates or inserts a persistent state value
func (r *HealthRepository) UpdateSystemState(ctx context.Context, key string, value string) error {
	query := `
		INSERT INTO system_state (key, value, updated_at)
		VALUES (?, ?, datetime('now'))
		ON CONFLICT(key) DO UPDATE SET
		value = excluded.value,
		updated_at = datetime('now')
	`
	_, err := r.db.ExecContext(ctx, query, key, value)
	if err != nil {
		return fmt.Errorf("failed to update system state: %w", err)
	}
	return nil
}
