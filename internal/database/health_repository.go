package database

import (
	"database/sql"
	"fmt"
	"strings"
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
	return &HealthRepository{db: db}
}

// UpdateFileHealth updates or inserts a file health record
func (r *HealthRepository) UpdateFileHealth(filePath string, status HealthStatus, errorMessage *string, sourceNzbPath *string, errorDetails *string) error {
	query := `
		INSERT INTO file_health (file_path, status, last_checked, last_error, source_nzb_path, error_details, retry_count, created_at, updated_at)
		VALUES (?, ?, datetime('now'), ?, ?, ?, 0, datetime('now'), datetime('now'))
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
		       next_retry_at, source_nzb_path, error_details, created_at, updated_at
		FROM file_health
		WHERE file_path = ?
	`

	var health FileHealth
	err := r.db.QueryRow(query, filePath).Scan(
		&health.ID, &health.FilePath, &health.Status, &health.LastChecked,
		&health.LastError, &health.RetryCount, &health.MaxRetries,
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

// GetUnhealthyFiles returns files that need retry checks
func (r *HealthRepository) GetUnhealthyFiles(limit int) ([]*FileHealth, error) {
	query := `
		SELECT id, file_path, status, last_checked, last_error, retry_count, max_retries,
		       next_retry_at, source_nzb_path, error_details, created_at, updated_at
		FROM file_health
		WHERE status != 'healthy'
		  AND retry_count < max_retries
		  AND (next_retry_at IS NULL OR next_retry_at <= datetime('now'))
		ORDER BY last_checked ASC
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
		    updated_at = datetime('now')
		WHERE file_path = ?
	`

	_, err := r.db.Exec(query, errorMessage, filePath)
	if err != nil {
		return fmt.Errorf("failed to increment retry count: %w", err)
	}

	return nil
}

// MarkAsCorrupted permanently marks a file as corrupted after max retries
func (r *HealthRepository) MarkAsCorrupted(filePath string, finalError *string) error {
	query := `
		UPDATE file_health 
		SET status = 'corrupted',
		    last_error = ?,
		    next_retry_at = NULL,
		    updated_at = datetime('now')
		WHERE file_path = ? AND retry_count >= max_retries
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