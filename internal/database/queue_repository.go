package database

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// QueueRepository handles queue-specific database operations
type QueueRepository struct {
	db DBQuerier
}

// NewQueueRepository creates a new queue repository
func NewQueueRepository(db *sql.DB) *QueueRepository {
	return &QueueRepository{db: db}
}

// RemoveFromQueue removes an item from the queue
func (r *QueueRepository) RemoveFromQueue(ctx context.Context, id int64) error {
	query := `DELETE FROM import_queue WHERE id = ?`
	_, err := r.db.ExecContext(ctx, query, id)

	return err
}

// RemoveFromQueueBulk removes multiple items from the queue in bulk
func (r *QueueRepository) RemoveFromQueueBulk(ctx context.Context, ids []int64) (*BulkOperationResult, error) {
	if len(ids) == 0 {
		return &BulkOperationResult{}, nil
	}

	result := &BulkOperationResult{
		FailedIDs: []int64{},
	}

	err := r.withQueueTransaction(ctx, func(txRepo *QueueRepository) error {
		for _, id := range ids {
			// Check status first - we can't delete processing items
			var status QueueStatus
			checkQuery := `SELECT status FROM import_queue WHERE id = ?`
			err := txRepo.db.QueryRowContext(ctx, checkQuery, id).Scan(&status)
			if err != nil {
				if err == sql.ErrNoRows {
					continue // Already gone, ignore
				}
				return fmt.Errorf("failed to check status for item %d: %w", id, err)
			}

			if status == QueueStatusProcessing {
				result.ProcessingCount++
				result.FailedIDs = append(result.FailedIDs, id)
				continue
			}

			// Perform deletion
			deleteQuery := `DELETE FROM import_queue WHERE id = ?`
			_, err = txRepo.db.ExecContext(ctx, deleteQuery, id)
			if err != nil {
				return fmt.Errorf("failed to delete item %d: %w", id, err)
			}
			result.DeletedCount++
		}
		return nil
	})

	if err != nil {
		return result, err
	}

	// If we couldn't delete some items because they were processing, return an error
	// so the API handler knows to return a conflict status
	if result.ProcessingCount > 0 {
		return result, fmt.Errorf("%d items were in processing status and could not be deleted", result.ProcessingCount)
	}

	return result, nil
}

// RestartQueueItemsBulk resets multiple queue items back to pending status
func (r *QueueRepository) RestartQueueItemsBulk(ctx context.Context, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}

	return r.withQueueTransaction(ctx, func(txRepo *QueueRepository) error {
		for _, id := range ids {
			// Only allow restart of failed or completed items
			query := `
				UPDATE import_queue 
				SET status = 'pending', started_at = NULL, completed_at = NULL, error_message = NULL, updated_at = datetime('now')
				WHERE id = ? AND status != 'processing'
			`
			_, err := txRepo.db.ExecContext(ctx, query, id)
			if err != nil {
				return fmt.Errorf("failed to restart item %d: %w", id, err)
			}
		}
		return nil
	})
}

// AddToQueue adds a new NZB file to the import queue
func (r *QueueRepository) AddToQueue(ctx context.Context, item *ImportQueueItem) error {
	query := `
		INSERT INTO import_queue (nzb_path, relative_path, category, priority, status, retry_count, max_retries, batch_id, metadata, file_size, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'), datetime('now'))
		ON CONFLICT(nzb_path) DO UPDATE SET
		priority = CASE WHEN excluded.priority < priority THEN excluded.priority ELSE priority END,
		category = excluded.category,
		batch_id = excluded.batch_id,
		metadata = excluded.metadata,
		file_size = excluded.file_size,
		status = excluded.status,
		retry_count = 0,
		started_at = NULL,
		updated_at = datetime('now'),
		relative_path = excluded.relative_path
		WHERE status NOT IN ('processing', 'pending')
	`

	result, err := r.db.ExecContext(ctx, query,
		item.NzbPath, item.RelativePath, item.Category, item.Priority, item.Status,
		item.RetryCount, item.MaxRetries, item.BatchID, item.Metadata, item.FileSize)
	if err != nil {
		return fmt.Errorf("failed to add queue item: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("failed to get queue item id: %w", err)
	}

	item.ID = id
	item.CreatedAt = time.Now()
	item.UpdatedAt = time.Now()
	return nil
}

func (r *QueueRepository) AddStoragePath(ctx context.Context, itemID int64, storagePath string) error {
	query := `
		UPDATE import_queue
		SET storage_path = ?, updated_at = datetime('now')
		WHERE id = ?
	`

	_, err := r.db.ExecContext(ctx, query, storagePath, itemID)
	if err != nil {
		return fmt.Errorf("failed to add storage path: %w", err)
	}

	return nil
}

// IsFileInQueue checks if a file is already in the queue (pending or processing)
func (r *QueueRepository) IsFileInQueue(ctx context.Context, filePath string) (bool, error) {
	query := `SELECT 1 FROM import_queue WHERE nzb_path = ? AND status IN ('pending', 'processing', 'paused') LIMIT 1`

	var exists int
	err := r.db.QueryRowContext(ctx, query, filePath).Scan(&exists)
	if err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, fmt.Errorf("failed to check if file in queue: %w", err)
	}

	return true, nil
}

// ClaimNextQueueItem atomically claims and returns the next available queue item
func (r *QueueRepository) ClaimNextQueueItem(ctx context.Context) (*ImportQueueItem, error) {
	// Use immediate transaction to atomically claim an item
	var claimedItem *ImportQueueItem

	err := r.withQueueTransaction(ctx, func(txRepo *QueueRepository) error {
		// First, get the next available item ID within the transaction
		var itemID int64
		selectQuery := `
			SELECT id FROM import_queue
			WHERE status = 'pending'
			ORDER BY priority ASC, created_at ASC
			LIMIT 1
		`

		err := txRepo.db.QueryRowContext(ctx, selectQuery).Scan(&itemID)
		if err != nil {
			if err == sql.ErrNoRows {
				// No items available
				return nil
			}
			return fmt.Errorf("failed to select queue item: %w", err)
		}

		// Now atomically update that specific item and get all its data
		updateQuery := `
			UPDATE import_queue
			SET status = 'processing', started_at = datetime('now'), updated_at = datetime('now')
			WHERE id = ? AND status = 'pending'
		`

		result, err := txRepo.db.ExecContext(ctx, updateQuery, itemID)
		if err != nil {
			return fmt.Errorf("failed to claim queue item %d: %w", itemID, err)
		}

		rowsAffected, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("failed to get rows affected: %w", err)
		}

		if rowsAffected == 0 {
			// Item was claimed by another worker between SELECT and UPDATE
			return nil
		}

		// Get the complete claimed item data
		getQuery := `
			SELECT id, nzb_path, relative_path, category, priority, status, created_at, updated_at, 
			       started_at, completed_at, retry_count, max_retries, error_message, batch_id, metadata, file_size
			FROM import_queue 
			WHERE id = ?
		`

		var item ImportQueueItem
		err = txRepo.db.QueryRowContext(ctx, getQuery, itemID).Scan(
			&item.ID, &item.NzbPath, &item.RelativePath, &item.Category, &item.Priority, &item.Status,
			&item.CreatedAt, &item.UpdatedAt, &item.StartedAt, &item.CompletedAt,
			&item.RetryCount, &item.MaxRetries, &item.ErrorMessage, &item.BatchID, &item.Metadata, &item.FileSize,
		)
		if err != nil {
			return fmt.Errorf("failed to get claimed item: %w", err)
		}

		claimedItem = &item
		return nil
	})

	if err != nil {
		return nil, err
	}

	return claimedItem, nil
}

// UpdateQueueItemStatus updates the status of a queue item
func (r *QueueRepository) UpdateQueueItemStatus(ctx context.Context, id int64, status QueueStatus, errorMessage *string) error {
	now := time.Now()
	var query string
	var args []any

	switch status {
	case QueueStatusProcessing:
		query = `UPDATE import_queue SET status = ?, started_at = ?, updated_at = ? WHERE id = ?`
		args = []any{status, now, now, id}
	case QueueStatusCompleted:
		query = `UPDATE import_queue SET status = ?, completed_at = ?, updated_at = ?, error_message = NULL WHERE id = ?`
		args = []any{status, now, now, id}
		// Track successful import
		_ = r.IncrementDailyStat(ctx, "completed")
	case QueueStatusFailed:
		query = `UPDATE import_queue SET status = ?, error_message = ?, updated_at = ? WHERE id = ?`
		args = []any{status, errorMessage, now, id}
		// Track failed import
		_ = r.IncrementDailyStat(ctx, "failed")
	default:
		query = `UPDATE import_queue SET status = ?, error_message = ?, updated_at = ? WHERE id = ?`
		args = []any{status, errorMessage, now, id}
	}

	_, err := r.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("failed to update queue item status: %w", err)
	}

	return nil
}

// IncrementDailyStat increments the completed or failed count for the current day
func (r *QueueRepository) IncrementDailyStat(ctx context.Context, statType string) error {
	column := "completed_count"
	if statType == "failed" {
		column = "failed_count"
	}

	// Also increment hourly stat for rolling 24h calculation
	_ = r.IncrementHourlyStat(ctx, statType)

	query := fmt.Sprintf(`
		INSERT INTO import_daily_stats (day, %s, updated_at)
		VALUES (date('now'), 1, datetime('now'))
		ON CONFLICT(day) DO UPDATE SET
		%s = %s + 1,
		updated_at = datetime('now')
	`, column, column, column)

	_, err := r.db.ExecContext(ctx, query)
	return err
}

// IncrementHourlyStat increments the completed or failed count for the current hour
func (r *QueueRepository) IncrementHourlyStat(ctx context.Context, statType string) error {
	column := "completed_count"
	if statType == "failed" {
		column = "failed_count"
	}

	// Calculate start of current hour
	currentHour := time.Now().UTC().Truncate(time.Hour)

	query := fmt.Sprintf(`
		INSERT INTO import_hourly_stats (hour, %s, updated_at)
		VALUES (?, 1, datetime('now'))
		ON CONFLICT(hour) DO UPDATE SET
		%s = %s + 1,
		updated_at = datetime('now')
	`, column, column, column)

	_, err := r.db.ExecContext(ctx, query, currentHour)
	return err
}

// GetImportHourlyStats retrieves import statistics for the specified number of hours
func (r *QueueRepository) GetImportHourlyStats(ctx context.Context, hours int) ([]*ImportHourlyStat, error) {
	query := `
		SELECT hour, completed_count, failed_count, bytes_downloaded, updated_at
		FROM import_hourly_stats
		WHERE hour >= datetime('now', ?)
		ORDER BY hour ASC
	`

	rows, err := r.db.QueryContext(ctx, query, fmt.Sprintf("-%d hours", hours))
	if err != nil {
		return nil, fmt.Errorf("failed to get import hourly stats: %w", err)
	}
	defer rows.Close()

	var stats []*ImportHourlyStat
	for rows.Next() {
		var s ImportHourlyStat
		err := rows.Scan(&s.Hour, &s.CompletedCount, &s.FailedCount, &s.BytesDownloaded, &s.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan import hourly stat: %w", err)
		}
		stats = append(stats, &s)
	}

	return stats, rows.Err()
}

// GetImportDailyStats retrieves historical import statistics for the last N days
func (r *QueueRepository) GetImportDailyStats(ctx context.Context, days int) ([]*ImportDailyStat, error) {
	query := `
		SELECT day, completed_count, failed_count, bytes_downloaded, updated_at
		FROM import_daily_stats
		WHERE day >= date('now', ?)
		ORDER BY day ASC
	`

	rows, err := r.db.QueryContext(ctx, query, fmt.Sprintf("-%d days", days))
	if err != nil {
		return nil, fmt.Errorf("failed to get import history: %w", err)
	}
	defer rows.Close()

	var stats []*ImportDailyStat
	for rows.Next() {
		var s ImportDailyStat
		err := rows.Scan(&s.Day, &s.CompletedCount, &s.FailedCount, &s.BytesDownloaded, &s.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan import daily stat: %w", err)
		}

		stats = append(stats, &s)
	}

	return stats, nil
}

// GetImportHistory retrieves historical import statistics for the last N days (Alias for GetImportDailyStats)
func (r *QueueRepository) GetImportHistory(ctx context.Context, days int) ([]*ImportDailyStat, error) {
	return r.GetImportDailyStats(ctx, days)
}


// AddImportHistory records a successful file import in the persistent history table
func (r *QueueRepository) AddImportHistory(ctx context.Context, history *ImportHistory) error {
	query := `
		INSERT INTO import_history (nzb_id, nzb_name, file_name, file_size, virtual_path, category, completed_at)
		VALUES (?, ?, ?, ?, ?, ?, datetime('now'))
	`
	_, err := r.db.ExecContext(ctx, query,
		history.NzbID, history.NzbName, history.FileName, history.FileSize,
		history.VirtualPath, history.Category)
	if err != nil {
		return fmt.Errorf("failed to add import history: %w", err)
	}
	return nil
}

// ListImportHistory retrieves the last N successful imports from the persistent history
func (r *QueueRepository) ListImportHistory(ctx context.Context, limit int) ([]*ImportHistory, error) {
	query := `
		SELECT h.id, h.nzb_id, h.nzb_name, h.file_name, h.file_size, h.virtual_path, f.library_path, h.category, h.completed_at
		FROM import_history h
		LEFT JOIN file_health f ON h.virtual_path = f.file_path
		ORDER BY h.completed_at DESC
		LIMIT ?
	`
	rows, err := r.db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to list import history: %w", err)
	}
	defer rows.Close()

	var history []*ImportHistory
	for rows.Next() {
		var h ImportHistory
		err := rows.Scan(&h.ID, &h.NzbID, &h.NzbName, &h.FileName, &h.FileSize, &h.VirtualPath, &h.LibraryPath, &h.Category, &h.CompletedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan import history: %w", err)
		}
		history = append(history, &h)
	}
	return history, nil
}

// IncrementRetryCountAndResetStatus increments the retry count and resets the status to pending
func (r *QueueRepository) IncrementRetryCountAndResetStatus(ctx context.Context, id int64, errorMessage *string) (bool, error) {
	query := `
		UPDATE import_queue 
		SET status = 'pending', retry_count = retry_count + 1, started_at = NULL, error_message = ?, updated_at = datetime('now')
		WHERE id = ? AND retry_count < max_retries
	`
	result, err := r.db.ExecContext(ctx, query, errorMessage, id)
	if err != nil {
		return false, fmt.Errorf("failed to increment retry count: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("failed to get rows affected: %w", err)
	}

	return rowsAffected > 0, nil
}

// FilterExistingNzbdavIds checks a list of nzbdav IDs and returns those that already exist in the queue
func (r *QueueRepository) FilterExistingNzbdavIds(ctx context.Context, ids []string) ([]string, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	// We can't pass too many parameters at once, so we batch the query
	batchSize := 500
	existingIds := make([]string, 0)

	for i := 0; i < len(ids); i += batchSize {
		end := min(i+batchSize, len(ids))

		batchIds := ids[i:end]

		// Build placeholders for the IN clause
		placeholders := make([]string, len(batchIds))
		args := make([]any, len(batchIds))
		for j, id := range batchIds {
			placeholders[j] = "?"
			args[j] = id
		}

		// Query using json_extract to find matching IDs
		query := fmt.Sprintf(`
			SELECT DISTINCT json_extract(metadata, '$.nzbdav_id') 
			FROM import_queue 
			WHERE json_extract(metadata, '$.nzbdav_id') IN (%s)
		`, strings.Join(placeholders, ","))

		rows, err := r.db.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, fmt.Errorf("failed to check existing nzbdav IDs: %w", err)
		}

		for rows.Next() {
			var id sql.NullString
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return nil, fmt.Errorf("failed to scan matching id: %w", err)
			}
			if id.Valid {
				existingIds = append(existingIds, id.String)
			}
		}
		rows.Close()
	}

	return existingIds, nil
}

// UpdateQueueItemPriority updates the priority of a queue item
func (r *QueueRepository) UpdateQueueItemPriority(ctx context.Context, id int64, priority QueuePriority) error {
	query := `UPDATE import_queue SET priority = ?, updated_at = datetime('now') WHERE id = ?`
	_, err := r.db.ExecContext(ctx, query, priority, id)
	if err != nil {
		return fmt.Errorf("failed to update queue item priority: %w", err)
	}
	return nil
}

// UpdateQueueItemNzbPath updates the NZB path of a queue item
func (r *QueueRepository) UpdateQueueItemNzbPath(ctx context.Context, id int64, nzbPath string) error {
	query := `UPDATE import_queue SET nzb_path = ?, updated_at = datetime('now') WHERE id = ?`
	_, err := r.db.ExecContext(ctx, query, nzbPath, id)
	if err != nil {
		return fmt.Errorf("failed to update queue item nzb path: %w", err)
	}
	return nil
}

// GetQueueStats returns current queue statistics
func (r *QueueRepository) GetQueueStats(ctx context.Context) (*QueueStats, error) {
	// Count items by status
	queries := []struct {
		status string
		query  string
	}{
		{"pending", "SELECT COUNT(*) FROM import_queue WHERE status = 'pending'"},
		{"processing", "SELECT COUNT(*) FROM import_queue WHERE status = 'processing'"},
		{"completed", "SELECT COUNT(*) FROM import_queue WHERE status = 'completed'"},
		{"failed", "SELECT COUNT(*) FROM import_queue WHERE status = 'failed'"},
	}

	stats := &QueueStats{}
	var counts []int

	for _, q := range queries {
		var count int
		err := r.db.QueryRowContext(ctx, q.query).Scan(&count)
		if err != nil {
			return nil, fmt.Errorf("failed to get count for %s items: %w", q.status, err)
		}
		counts = append(counts, count)
	}

	// Include paused items in TotalQueued
	var pausedCount int
	err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM import_queue WHERE status = 'paused'").Scan(&pausedCount)
	if err != nil {
		return nil, fmt.Errorf("failed to get count for paused items: %w", err)
	}

	stats.TotalQueued = counts[0] + pausedCount // pending + paused
	stats.TotalProcessing = counts[1]           // processing
	stats.TotalCompleted = counts[2]            // completed
	stats.TotalFailed = counts[3]               // failed

	// Calculate average processing time for completed items
	var avgProcessingTimeFloat sql.NullFloat64
	avgQuery := `
		SELECT AVG((julianday(completed_at) - julianday(started_at)) * 24 * 60 * 60 * 1000)
		FROM import_queue 
		WHERE status = 'completed' AND started_at IS NOT NULL AND completed_at IS NOT NULL
	`
	err = r.db.QueryRowContext(ctx, avgQuery).Scan(&avgProcessingTimeFloat)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate average processing time: %w", err)
	}

	// Convert float to int64 for storage
	if avgProcessingTimeFloat.Valid {
		avgTime := int(avgProcessingTimeFloat.Float64)
		stats.AvgProcessingTimeMs = &avgTime
	}

	stats.LastUpdated = time.Now()
	return stats, nil
}

// AddBatchToQueue adds multiple items to the queue in a single transaction
func (r *QueueRepository) AddBatchToQueue(ctx context.Context, items []*ImportQueueItem) error {
	if len(items) == 0 {
		return nil
	}

	return r.withQueueTransaction(ctx, func(txRepo *QueueRepository) error {
		// Prepare batch insert statement
		query := `
			INSERT INTO import_queue (nzb_path, relative_path, category, priority, status, retry_count, max_retries, batch_id, metadata, file_size, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'), datetime('now'))
			ON CONFLICT(nzb_path) DO UPDATE SET
			priority = CASE WHEN excluded.priority < priority THEN excluded.priority ELSE priority END,
			category = excluded.category,
			batch_id = excluded.batch_id,
			metadata = excluded.metadata,
			file_size = excluded.file_size,
			updated_at = datetime('now')
			WHERE status NOT IN ('processing', 'completed')
		`

		now := time.Now()
		for _, item := range items {
			result, err := txRepo.db.ExecContext(ctx, query,
				item.NzbPath, item.RelativePath, item.Category, item.Priority, item.Status,
				item.RetryCount, item.MaxRetries, item.BatchID, item.Metadata, item.FileSize)
			if err != nil {
				return fmt.Errorf("failed to insert queue item %s: %w", item.NzbPath, err)
			}

			// Update ID for the item
			if id, err := result.LastInsertId(); err == nil {
				item.ID = id
				item.CreatedAt = now
				item.UpdatedAt = now
			}
		}

		return nil
	})
}

// GetQueueItem retrieves a specific queue item by ID
func (r *QueueRepository) GetQueueItem(ctx context.Context, id int64) (*ImportQueueItem, error) {
	query := `
		SELECT id, nzb_path, relative_path, category, priority, status, created_at, updated_at,
		       started_at, completed_at, retry_count, max_retries, error_message, batch_id, metadata, file_size, storage_path
		FROM import_queue WHERE id = ?
	`

	var item ImportQueueItem
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&item.ID, &item.NzbPath, &item.RelativePath, &item.Category, &item.Priority, &item.Status,
		&item.CreatedAt, &item.UpdatedAt, &item.StartedAt, &item.CompletedAt,
		&item.RetryCount, &item.MaxRetries, &item.ErrorMessage, &item.BatchID, &item.Metadata, &item.FileSize, &item.StoragePath,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil // Item not found
		}
		return nil, fmt.Errorf("failed to get queue item: %w", err)
	}

	return &item, nil
}

// withQueueTransaction executes a function within a queue database transaction
func (r *QueueRepository) withQueueTransaction(ctx context.Context, fn func(*QueueRepository) error) error {
	// Cast to *sql.DB to access BeginTx method
	sqlDB, ok := r.db.(*sql.DB)
	if !ok {
		return fmt.Errorf("repository not connected to sql.DB")
	}

	tx, err := sqlDB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin queue transaction: %w", err)
	}

	// Create a repository that uses the transaction
	txRepo := &QueueRepository{db: tx}

	err = fn(txRepo)
	if err != nil {
		if rollbackErr := tx.Rollback(); rollbackErr != nil {
			return fmt.Errorf("failed to rollback queue transaction (original error: %w): %v", err, rollbackErr)
		}
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit queue transaction: %w", err)
	}

	return nil
}

// ResetStaleItems resets processing items back to pending on service startup
func (r *QueueRepository) ResetStaleItems(ctx context.Context) error {
	// Reset all items that are in 'processing' status
	// Since the service is just starting up, any item marked as processing is from a previous interrupted run
	query := `
		UPDATE import_queue
		SET status = 'pending', started_at = NULL, updated_at = datetime('now')
		WHERE status = 'processing'`

	result, err := r.db.ExecContext(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to reset stale queue items: %w", err)
	}

	_, err = result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	return nil
}

// ClearImportHistory deletes all records from the import_history and import_daily_stats tables
func (r *QueueRepository) ClearImportHistory(ctx context.Context) error {
	// Clear history records
	queryHistory := `DELETE FROM import_history`
	if _, err := r.db.ExecContext(ctx, queryHistory); err != nil {
		return fmt.Errorf("failed to clear import history: %w", err)
	}

	// Also clear daily stats
	return r.ClearDailyStats(ctx)
}

// ClearDailyStats deletes all records from the import_daily_stats and import_hourly_stats tables
func (r *QueueRepository) ClearDailyStats(ctx context.Context) error {
	queryDaily := `DELETE FROM import_daily_stats`
	if _, err := r.db.ExecContext(ctx, queryDaily); err != nil {
		return fmt.Errorf("failed to clear daily stats: %w", err)
	}

	return r.ClearHourlyStats(ctx)
}

// ClearHourlyStats deletes all records from the import_hourly_stats table
func (r *QueueRepository) ClearHourlyStats(ctx context.Context) error {
	queryHourly := `DELETE FROM import_hourly_stats`
	if _, err := r.db.ExecContext(ctx, queryHourly); err != nil {
		return fmt.Errorf("failed to clear hourly stats: %w", err)
	}
	return nil
}

// ClearImportHistorySince deletes records from the import_history and import_queue tables
// since the specified time, and adjusts the import_daily_stats and import_hourly_stats accordingly.
func (r *QueueRepository) ClearImportHistorySince(ctx context.Context, since time.Time) error {
	// Clear from persistent history
	queryHistory := `DELETE FROM import_history WHERE completed_at >= ?`
	if _, err := r.db.ExecContext(ctx, queryHistory, since); err != nil {
		return fmt.Errorf("failed to clear import history: %w", err)
	}

	// Also clear from queue (completed/failed)
	queryQueue := `DELETE FROM import_queue WHERE status IN ('completed', 'failed') AND updated_at >= ?`
	if _, err := r.db.ExecContext(ctx, queryQueue, since); err != nil {
		return fmt.Errorf("failed to clear queue history: %w", err)
	}

	// Note: We don't decrement daily/hourly counts here for simplicity,
	// as strict rolling 24h will naturally age out the data.
	return nil
}

