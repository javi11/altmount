package database

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// HealthRepository handles file health database operations
type HealthRepository struct {
	db interface {
		Exec(query string, args ...interface{}) (sql.Result, error)
		Query(query string, args ...interface{}) (*sql.Rows, error)
		QueryRow(query string, args ...interface{}) *sql.Row
	}
}

// NewHealthRepository creates a new health repository
func NewHealthRepository(db interface {
	Exec(query string, args ...interface{}) (sql.Result, error)
	Query(query string, args ...interface{}) (*sql.Rows, error)
	QueryRow(query string, args ...interface{}) *sql.Row
}) *HealthRepository {
	return &HealthRepository{
		db: db,
	}
}

// UpdateFileHealth updates or inserts a file health record
func (r *HealthRepository) UpdateFileHealth(filePath string, status HealthStatus, errorMessage *string, sourceNzbPath *string, errorDetails *string) error {
	query := `
		INSERT INTO file_health (file_path, status, last_checked, last_error, source_nzb_path, error_details, retry_count, repair_retry_count, created_at, updated_at)
		VALUES (?, ?, datetime('now'), ?, ?, ?, 0, 0, datetime('now'), datetime('now'))
		ON CONFLICT(file_path) DO UPDATE SET
		status = excluded.status,
		last_checked = datetime('now'),
		last_error = excluded.last_error,
		source_nzb_path = COALESCE(excluded.source_nzb_path, source_nzb_path),
		error_details = excluded.error_details,
		updated_at = datetime('now')
		WHERE status != excluded.status OR last_error != excluded.last_error
	`

	_, err := r.db.Exec(query, filePath, status, errorMessage, sourceNzbPath, errorDetails)
	if err != nil {
		return fmt.Errorf("failed to update file health: %w", err)
	}

	return nil
}

// GetFileHealth retrieves health record for a specific file
func (r *HealthRepository) GetFileHealth(filePath string) (*FileHealth, error) {
	query := `
		SELECT id, file_path, status, last_checked, last_error, retry_count, max_retries,
		       repair_retry_count, max_repair_retries, next_retry_at, source_nzb_path, 
		       error_details, created_at, updated_at
		FROM file_health
		WHERE file_path = ?
	`

	var health FileHealth
	err := r.db.QueryRow(query, filePath).Scan(
		&health.ID, &health.FilePath, &health.Status, &health.LastChecked,
		&health.LastError, &health.RetryCount, &health.MaxRetries,
		&health.RepairRetryCount, &health.MaxRepairRetries,
		&health.NextRetryAt, &health.SourceNzbPath, &health.ErrorDetails,
		&health.CreatedAt, &health.UpdatedAt,
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
func (r *HealthRepository) GetFileHealthByID(id int64) (*FileHealth, error) {
	query := `
		SELECT id, file_path, status, last_checked, last_error, retry_count, max_retries,
		       repair_retry_count, max_repair_retries, next_retry_at, source_nzb_path, 
		       error_details, created_at, updated_at
		FROM file_health
		WHERE id = ?
	`

	var health FileHealth
	err := r.db.QueryRow(query, id).Scan(
		&health.ID, &health.FilePath, &health.Status, &health.LastChecked,
		&health.LastError, &health.RetryCount, &health.MaxRetries,
		&health.RepairRetryCount, &health.MaxRepairRetries,
		&health.NextRetryAt, &health.SourceNzbPath, &health.ErrorDetails,
		&health.CreatedAt, &health.UpdatedAt,
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
func (r *HealthRepository) GetUnhealthyFiles(limit int) ([]*FileHealth, error) {
	query := `
		SELECT id, file_path, status, last_checked, last_error, retry_count, max_retries,
		       repair_retry_count, max_repair_retries, next_retry_at, source_nzb_path, 
		       error_details, created_at, updated_at
		FROM file_health
		WHERE status IN ('pending', 'partial', 'corrupted') 
		  AND retry_count < max_retries
		  AND (next_retry_at IS NULL OR next_retry_at <= datetime('now'))
		ORDER BY 
		  CASE 
		    WHEN status = 'pending' THEN 0 
		    ELSE 1 
		  END,  -- Prioritize pending files
		  last_checked ASC
		LIMIT ?
	`

	rows, err := r.db.Query(query, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query unhealthy files: %w", err)
	}
	defer rows.Close()

	var files []*FileHealth
	for rows.Next() {
		var health FileHealth
		err := rows.Scan(
			&health.ID, &health.FilePath, &health.Status, &health.LastChecked,
			&health.LastError, &health.RetryCount, &health.MaxRetries,
			&health.RepairRetryCount, &health.MaxRepairRetries,
			&health.NextRetryAt, &health.SourceNzbPath, &health.ErrorDetails,
			&health.CreatedAt, &health.UpdatedAt,
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

// GetFilesForRepairNotification returns files that need repair notification (repair_triggered status)
func (r *HealthRepository) GetFilesForRepairNotification(limit int) ([]*FileHealth, error) {
	query := `
		SELECT id, file_path, status, last_checked, last_error, retry_count, max_retries,
		       repair_retry_count, max_repair_retries, next_retry_at, source_nzb_path, 
		       error_details, created_at, updated_at
		FROM file_health
		WHERE status = 'repair_triggered'
		  AND repair_retry_count < max_repair_retries
		  AND (next_retry_at IS NULL OR next_retry_at <= datetime('now'))
		ORDER BY last_checked ASC
		LIMIT ?
	`

	rows, err := r.db.Query(query, limit)
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
			&health.NextRetryAt, &health.SourceNzbPath, &health.ErrorDetails,
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

// IncrementRetryCount increments the retry count and calculates next retry time
func (r *HealthRepository) IncrementRetryCount(filePath string, errorMessage *string) error {
	// Exponential backoff: 1, 2, 4, 8, 16 minutes
	query := `
		UPDATE file_health 
		SET retry_count = retry_count + 1,
		    last_error = ?,
		    next_retry_at = datetime('now', '+' || (CASE 
		        WHEN retry_count = 0 THEN 1
		        WHEN retry_count = 1 THEN 2
		        WHEN retry_count = 2 THEN 4
		        WHEN retry_count = 3 THEN 8
		        ELSE 16
		    END) || ' minutes'),
			status = 'pending',
		    updated_at = datetime('now')
		WHERE file_path = ?
	`

	_, err := r.db.Exec(query, errorMessage, filePath)
	if err != nil {
		return fmt.Errorf("failed to increment retry count: %w", err)
	}

	return nil
}

// SetRepairTriggered sets a file's status to repair_triggered
func (r *HealthRepository) SetRepairTriggered(filePath string, errorMessage *string) error {
	query := `
		UPDATE file_health 
		SET status = 'repair_triggered',
		    last_error = ?,
		    next_retry_at = NULL,
		    updated_at = datetime('now')
		WHERE file_path = ?
	`

	result, err := r.db.Exec(query, errorMessage, filePath)
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
func (r *HealthRepository) SetCorrupted(filePath string, errorMessage *string) error {
	query := `
		UPDATE file_health 
		SET status = 'corrupted',
		    last_error = ?,
		    next_retry_at = NULL,
		    updated_at = datetime('now')
		WHERE file_path = ?
	`

	result, err := r.db.Exec(query, errorMessage, filePath)
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

// IncrementRepairRetryCount increments the repair retry count and calculates next retry time
func (r *HealthRepository) IncrementRepairRetryCount(filePath string, errorMessage *string) error {
	// Exponential backoff for repair retries: 5, 10, 20 minutes
	query := `
		UPDATE file_health 
		SET repair_retry_count = repair_retry_count + 1,
		    last_error = ?,
		    next_retry_at = datetime('now', '+' || (CASE 
		        WHEN repair_retry_count = 0 THEN 5
		        WHEN repair_retry_count = 1 THEN 10
		        WHEN repair_retry_count = 2 THEN 20
		        ELSE 30
		    END) || ' minutes'),
		    status = 'repair_triggered',
		    updated_at = datetime('now')
		WHERE file_path = ?
	`

	_, err := r.db.Exec(query, errorMessage, filePath)
	if err != nil {
		return fmt.Errorf("failed to increment repair retry count: %w", err)
	}

	return nil
}

// MarkAsCorrupted permanently marks a file as corrupted after all retries are exhausted
func (r *HealthRepository) MarkAsCorrupted(filePath string, finalError *string) error {
	query := `
		UPDATE file_health 
		SET status = 'corrupted',
		    last_error = ?,
		    next_retry_at = NULL,
		    updated_at = datetime('now')
		WHERE file_path = ?
	`

	result, err := r.db.Exec(query, finalError, filePath)
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
func (r *HealthRepository) GetHealthStats() (map[HealthStatus]int, error) {
	query := `
		SELECT status, COUNT(*) 
		FROM file_health 
		GROUP BY status
	`

	rows, err := r.db.Query(query)
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
func (r *HealthRepository) SetRepairTriggeredByID(id int64, errorMessage *string) error {
	query := `
		UPDATE file_health 
		SET status = 'repair_triggered',
		    last_error = ?,
		    next_retry_at = NULL,
		    updated_at = datetime('now')
		WHERE id = ?
	`

	result, err := r.db.Exec(query, errorMessage, id)
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
func (r *HealthRepository) SetFileCheckingByID(id int64) error {
	query := `
		UPDATE file_health 
		SET status = ?,
		    updated_at = datetime('now')
		WHERE id = ?
	`

	_, err := r.db.Exec(query, HealthStatusChecking, id)
	if err != nil {
		return fmt.Errorf("failed to set file status to checking by ID: %w", err)
	}

	return nil
}

// DeleteHealthRecordByID removes a specific health record from the database by ID
func (r *HealthRepository) DeleteHealthRecordByID(id int64) error {
	query := `DELETE FROM file_health WHERE id = ?`

	result, err := r.db.Exec(query, id)
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
func (r *HealthRepository) DeleteHealthRecord(filePath string) error {
	query := `DELETE FROM file_health WHERE file_path = ?`

	result, err := r.db.Exec(query, filePath)
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

// CleanupHealthRecords removes health records for files that no longer exist
func (r *HealthRepository) CleanupHealthRecords(existingFiles []string) error {
	if len(existingFiles) == 0 {
		// Remove all records if no files exist
		_, err := r.db.Exec("DELETE FROM file_health")
		return err
	}

	// Create placeholders for IN clause
	placeholders := make([]string, len(existingFiles))
	args := make([]interface{}, len(existingFiles))
	for i, file := range existingFiles {
		placeholders[i] = "?"
		args[i] = file
	}

	placeholderStr := "?" + strings.Repeat(",?", len(existingFiles)-1)
	query := fmt.Sprintf(`
		DELETE FROM file_health 
		WHERE file_path NOT IN (%s)
	`, placeholderStr)

	_, err := r.db.Exec(query, args...)
	if err != nil {
		return fmt.Errorf("failed to cleanup health records: %w", err)
	}

	return nil
}

// AddFileToHealthCheck adds a file to the health database for checking
func (r *HealthRepository) AddFileToHealthCheck(filePath string, maxRetries int, sourceNzbPath *string) error {
	query := `
		INSERT INTO file_health (file_path, status, last_checked, retry_count, max_retries, repair_retry_count, max_repair_retries, source_nzb_path, created_at, updated_at)
		VALUES (?, ?, datetime('now'), 0, ?, 0, 3, ?, datetime('now'), datetime('now'))
		ON CONFLICT(file_path) DO UPDATE SET
		max_retries = excluded.max_retries,
		max_repair_retries = excluded.max_repair_retries,
		source_nzb_path = COALESCE(excluded.source_nzb_path, source_nzb_path),
		updated_at = datetime('now')
	`

	_, err := r.db.Exec(query, filePath, HealthStatusPending, maxRetries, sourceNzbPath)
	if err != nil {
		return fmt.Errorf("failed to add file to health check: %w", err)
	}

	return nil
}

// ListHealthItems returns all health records with optional filtering, sorting and pagination
func (r *HealthRepository) ListHealthItems(statusFilter *HealthStatus, limit, offset int, sinceFilter *time.Time, search string, sortBy string, sortOrder string) ([]*FileHealth, error) {
	// Validate and prepare ORDER BY clause
	orderClause := "created_at DESC"
	if sortBy != "" {
		// Whitelist of allowed sort fields to prevent SQL injection
		allowedFields := map[string]string{
			"file_path":  "file_path",
			"created_at": "created_at",
			"status":     "status",
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
		       repair_retry_count, max_repair_retries, next_retry_at, source_nzb_path,
		       error_details, created_at, updated_at
		FROM file_health
		WHERE (? IS NULL OR status = ?)
		  AND (? IS NULL OR created_at >= ?)
		  AND (? = '' OR file_path LIKE ? OR (source_nzb_path IS NOT NULL AND source_nzb_path LIKE ?))
		ORDER BY %s
		LIMIT ? OFFSET ?
	`, orderClause)

	// Prepare arguments for the query
	var statusParam interface{} = nil
	if statusFilter != nil {
		statusParam = string(*statusFilter)
	}

	var sinceParam interface{} = nil
	if sinceFilter != nil {
		sinceParam = sinceFilter.Format("2006-01-02 15:04:05")
	}

	// Prepare search parameter with wildcards
	searchPattern := "%" + search + "%"

	args := []interface{}{
		statusParam, statusParam, // status filter (checked twice in WHERE clause)
		sinceParam, sinceParam, // since filter (checked twice in WHERE clause)
		search, searchPattern, searchPattern, // search filter (file_path and source_nzb_path)
		limit, offset,
	}

	rows, err := r.db.Query(query, args...)
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
			&health.NextRetryAt, &health.SourceNzbPath, &health.ErrorDetails,
			&health.CreatedAt, &health.UpdatedAt,
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
func (r *HealthRepository) CountHealthItems(statusFilter *HealthStatus, sinceFilter *time.Time, search string) (int, error) {
	query := `
		SELECT COUNT(*) 
		FROM file_health
		WHERE (? IS NULL OR status = ?)
		  AND (? IS NULL OR created_at >= ?)
		  AND (? = '' OR file_path LIKE ? OR (source_nzb_path IS NOT NULL AND source_nzb_path LIKE ?))
	`

	// Prepare arguments for the query
	var statusParam interface{} = nil
	if statusFilter != nil {
		statusParam = string(*statusFilter)
	}

	var sinceParam interface{} = nil
	if sinceFilter != nil {
		sinceParam = sinceFilter.Format("2006-01-02 15:04:05")
	}

	// Prepare search parameter with wildcards
	searchPattern := "%" + search + "%"

	args := []interface{}{
		statusParam, statusParam, // status filter (checked twice in WHERE clause)
		sinceParam, sinceParam, // since filter (checked twice in WHERE clause)
		search, searchPattern, searchPattern, // search filter (file_path and source_nzb_path)
	}

	var count int
	err := r.db.QueryRow(query, args...).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count health items: %w", err)
	}

	return count, nil
}

// SetFileChecking sets a file's status to 'checking'
func (r *HealthRepository) SetFileChecking(filePath string) error {
	query := `
		UPDATE file_health 
		SET status = ?,
		    updated_at = datetime('now')
		WHERE file_path = ?
	`

	_, err := r.db.Exec(query, HealthStatusChecking, filePath)
	if err != nil {
		return fmt.Errorf("failed to set file status to checking: %w", err)
	}

	return nil
}

func (r *HealthRepository) ResetFileAllChecking() error {
	query := `
		UPDATE file_health
		SET status = ?,
		    updated_at = datetime('now')
		WHERE status = ?
	`

	_, err := r.db.Exec(query, HealthStatusPending, HealthStatusChecking)
	if err != nil {
		return fmt.Errorf("failed to reset all file statuses: %w", err)
	}

	return nil
}

// DeleteHealthRecordsBulk removes multiple health records from the database
func (r *HealthRepository) DeleteHealthRecordsBulk(filePaths []string) error {
	if len(filePaths) == 0 {
		return nil
	}

	// Build placeholders for the IN clause
	placeholders := make([]string, len(filePaths))
	args := make([]interface{}, len(filePaths))
	for i, path := range filePaths {
		placeholders[i] = "?"
		args[i] = path
	}

	query := fmt.Sprintf(`DELETE FROM file_health WHERE file_path IN (%s)`, strings.Join(placeholders, ","))

	result, err := r.db.Exec(query, args...)
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
func (r *HealthRepository) ResetHealthChecksBulk(filePaths []string) (int, error) {
	if len(filePaths) == 0 {
		return 0, nil
	}

	// Build placeholders for the IN clause
	placeholders := make([]string, len(filePaths))
	args := make([]interface{}, len(filePaths))
	for i, path := range filePaths {
		placeholders[i] = "?"
		args[i] = path
	}

	query := fmt.Sprintf(`
		UPDATE file_health
		SET status = '%s',
		    retry_count = 0,
		    repair_retry_count = 0,
		    next_retry_at = NULL,
		    last_error = NULL,
		    error_details = NULL,
		    updated_at = datetime('now')
		WHERE file_path IN (%s)
	`, HealthStatusPending, strings.Join(placeholders, ","))

	result, err := r.db.Exec(query, args...)
	if err != nil {
		return 0, fmt.Errorf("failed to reset health records: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to get rows affected: %w", err)
	}

	return int(rowsAffected), nil
}
