package database

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// DBQuerier defines the interface for database query operations
// Both *sql.DB and *sql.Tx implement this interface
type DBQuerier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Repository provides database operations for NZB and file management
type Repository struct {
	db DBQuerier
}

// NewRepository creates a new repository instance
func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

// Transaction support

// WithTransaction executes a function within a database transaction
func (r *Repository) WithTransaction(ctx context.Context, fn func(*Repository) error) error {
	return r.withTransactionMode(ctx, "", fn)
}

// WithImmediateTransaction executes a function within an immediate database transaction
// This reduces lock contention for queue operations by acquiring write locks immediately
// Uses SQLite's IMMEDIATE transaction mode via BeginTx with Serializable isolation
func (r *Repository) WithImmediateTransaction(ctx context.Context, fn func(*Repository) error) error {
	// Cast to *sql.DB to access BeginTx method
	sqlDB, ok := r.db.(*sql.DB)
	if !ok {
		return fmt.Errorf("repository not connected to sql.DB")
	}

	tx, err := sqlDB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	// Create a repository that uses the transaction
	txRepo := &Repository{db: tx}

	err = fn(txRepo)
	if err != nil {
		if rollbackErr := tx.Rollback(); rollbackErr != nil {
			return fmt.Errorf("failed to rollback immediate transaction (original error: %w): %v", err, rollbackErr)
		}
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit immediate transaction: %w", err)
	}

	return nil
}

// withTransactionMode executes a function within a database transaction with specified mode
func (r *Repository) withTransactionMode(ctx context.Context, mode string, fn func(*Repository) error) error {
	// Cast to *sql.DB to access Begin method
	sqlDB, ok := r.db.(*sql.DB)
	if !ok {
		return fmt.Errorf("repository not connected to sql.DB")
	}

	tx, err := sqlDB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	txRepo := &Repository{db: tx}

	err = fn(txRepo)
	if err != nil {
		if rollbackErr := tx.Rollback(); rollbackErr != nil {
			return fmt.Errorf("failed to rollback transaction (original error: %w): %w", err, rollbackErr)
		}
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// Queue operations

// AddToQueue adds an NZB file to the import queue with optimized concurrency
func (r *Repository) AddToQueue(ctx context.Context, item *ImportQueueItem) error {
	// Use UPSERT with immediate lock to prevent conflicts during concurrent inserts
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

	result, err := r.db.ExecContext(ctx, query,
		item.NzbPath, item.RelativePath, item.Category, item.Priority, item.Status,
		item.RetryCount, item.MaxRetries, item.BatchID, item.Metadata, item.FileSize)
	if err != nil {
		return fmt.Errorf("failed to add to queue: %w", err)
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

// GetNextQueueItems retrieves the next batch of items to process from the queue
// Uses optimized query with row-level locking for better concurrency
func (r *Repository) GetNextQueueItems(ctx context.Context, limit int) ([]*ImportQueueItem, error) {
	// Use a CTE to select items and immediately mark them as claimed to avoid race conditions
	query := `
		WITH selected_items AS (
			SELECT id, nzb_path, relative_path, category, priority, status, created_at, updated_at,
			       started_at, completed_at, retry_count, max_retries, error_message, batch_id, metadata, file_size
			FROM import_queue
			WHERE status = 'pending'
			  AND (started_at IS NULL OR datetime(started_at, '+10 minutes') < datetime('now'))
			ORDER BY priority ASC, created_at ASC
			LIMIT ?
		)
		SELECT * FROM selected_items
	`

	rows, err := r.db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to get next queue items: %w", err)
	}
	defer rows.Close()

	var items []*ImportQueueItem
	for rows.Next() {
		var item ImportQueueItem
		err := rows.Scan(
			&item.ID, &item.NzbPath, &item.RelativePath, &item.Category, &item.Priority, &item.Status,
			&item.CreatedAt, &item.UpdatedAt, &item.StartedAt, &item.CompletedAt,
			&item.RetryCount, &item.MaxRetries, &item.ErrorMessage, &item.BatchID, &item.Metadata, &item.FileSize,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan queue item: %w", err)
		}
		items = append(items, &item)
	}

	return items, rows.Err()
}

// ClaimNextQueueItem atomically claims and returns the next available queue item
// This prevents multiple workers from processing the same item
// Uses a single atomic UPDATE...RETURNING query to eliminate race conditions
func (r *Repository) ClaimNextQueueItem(ctx context.Context) (*ImportQueueItem, error) {
	// Use immediate transaction to atomically claim an item
	var claimedItem *ImportQueueItem

	err := r.WithImmediateTransaction(ctx, func(txRepo *Repository) error {
		// Single atomic operation: update and return in one query
		// This eliminates the race condition window between SELECT and UPDATE
		updateQuery := `
			UPDATE import_queue
			SET status = 'processing',
			    started_at = datetime('now'),
			    updated_at = datetime('now')
			WHERE id = (
				SELECT id FROM import_queue
				WHERE status = 'pending'
				  AND (started_at IS NULL OR datetime(started_at, '+10 minutes') < datetime('now'))
				ORDER BY priority ASC, created_at ASC
				LIMIT 1
			) AND status = 'pending'
			RETURNING id, nzb_path, relative_path, category, priority, status,
			          created_at, updated_at, started_at, completed_at,
			          retry_count, max_retries, error_message, batch_id, metadata, file_size
		`

		var item ImportQueueItem
		err := txRepo.db.QueryRowContext(ctx, updateQuery).Scan(
			&item.ID, &item.NzbPath, &item.RelativePath, &item.Category,
			&item.Priority, &item.Status, &item.CreatedAt, &item.UpdatedAt,
			&item.StartedAt, &item.CompletedAt, &item.RetryCount,
			&item.MaxRetries, &item.ErrorMessage, &item.BatchID,
			&item.Metadata, &item.FileSize,
		)
		if err != nil {
			if err == sql.ErrNoRows {
				// No items available to claim
				return nil
			}
			return fmt.Errorf("failed to claim queue item: %w", err)
		}

		claimedItem = &item
		return nil
	})

	if err != nil {
		return nil, err
	}

	return claimedItem, nil
}

// AddBatchToQueue adds multiple items to the queue in a single transaction for better performance
func (r *Repository) AddBatchToQueue(ctx context.Context, items []*ImportQueueItem) error {
	if len(items) == 0 {
		return nil
	}

	// Use immediate transaction for batch operations to reduce lock contention
	return r.WithImmediateTransaction(ctx, func(txRepo *Repository) error {
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

// UpdateQueueItemStatus updates the status of a queue item
func (r *Repository) UpdateQueueItemStatus(ctx context.Context, id int64, status QueueStatus, errorMessage *string) error {
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
func (r *Repository) IncrementDailyStat(ctx context.Context, statType string) error {
	column := "completed_count"
	if statType == "failed" {
		column = "failed_count"
	}

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

// GetQueueItem retrieves a specific queue item by ID
func (r *Repository) GetQueueItem(ctx context.Context, id int64) (*ImportQueueItem, error) {
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
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get queue item: %w", err)
	}

	return &item, nil
}

// GetQueueItemByPath retrieves a queue item by NZB path
func (r *Repository) GetQueueItemByPath(ctx context.Context, nzbPath string) (*ImportQueueItem, error) {
	query := `
		SELECT id, nzb_path, relative_path, category, priority, status, created_at, updated_at,
		       started_at, completed_at, retry_count, max_retries, error_message, batch_id, metadata, file_size, storage_path
		FROM import_queue WHERE nzb_path = ?
	`

	var item ImportQueueItem
	err := r.db.QueryRowContext(ctx, query, nzbPath).Scan(
		&item.ID, &item.NzbPath, &item.RelativePath, &item.Category, &item.Priority, &item.Status,
		&item.CreatedAt, &item.UpdatedAt, &item.StartedAt, &item.CompletedAt,
		&item.RetryCount, &item.MaxRetries, &item.ErrorMessage, &item.BatchID, &item.Metadata, &item.FileSize, &item.StoragePath,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get queue item by path: %w", err)
	}

	return &item, nil
}

// RemoveFromQueue removes an item from the queue
func (r *Repository) RemoveFromQueue(ctx context.Context, id int64) error {
	query := `DELETE FROM import_queue WHERE id = ?`

	result, err := r.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to remove from queue: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return sql.ErrNoRows
	}

	return nil
}

// BulkDeleteResult contains the result of a bulk delete operation
type BulkDeleteResult struct {
	DeletedCount    int64
	ProcessingCount int64
	RequestedCount  int64
}

// RemoveFromQueueBulk removes multiple items from the queue, excluding those currently being processed
func (r *Repository) RemoveFromQueueBulk(ctx context.Context, ids []int64) (*BulkDeleteResult, error) {
	if len(ids) == 0 {
		return &BulkDeleteResult{RequestedCount: 0}, nil
	}

	// Build placeholders for the IN clause
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}

	// First, count how many items are currently processing
	countQuery := fmt.Sprintf(`SELECT COUNT(*) FROM import_queue WHERE id IN (%s) AND status = ?`, strings.Join(placeholders, ","))
	countArgs := append(args, QueueStatusProcessing)

	var processingCount int64
	err := r.db.QueryRowContext(ctx, countQuery, countArgs...).Scan(&processingCount)
	if err != nil {
		return nil, fmt.Errorf("failed to count processing items: %w", err)
	}

	// If there are processing items, return error
	if processingCount > 0 {
		return &BulkDeleteResult{
			DeletedCount:    0,
			ProcessingCount: processingCount,
			RequestedCount:  int64(len(ids)),
		}, fmt.Errorf("cannot delete %d items that are currently being processed", processingCount)
	}

	// Delete items that are not processing
	deleteQuery := fmt.Sprintf(`DELETE FROM import_queue WHERE id IN (%s) AND status != ?`, strings.Join(placeholders, ","))
	deleteArgs := append(args, QueueStatusProcessing)

	result, err := r.db.ExecContext(ctx, deleteQuery, deleteArgs...)
	if err != nil {
		return nil, fmt.Errorf("failed to remove items from queue: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("failed to get rows affected: %w", err)
	}

	return &BulkDeleteResult{
		DeletedCount:    rowsAffected,
		ProcessingCount: processingCount,
		RequestedCount:  int64(len(ids)),
	}, nil
}

// RestartQueueItemsBulk resets multiple queue items to pending status for reprocessing
func (r *Repository) RestartQueueItemsBulk(ctx context.Context, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}

	// Build placeholders for the IN clause
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}

	// Reset items to pending status with cleared retry count and timestamps
	query := fmt.Sprintf(`
		UPDATE import_queue 
		SET status = 'pending',
		    retry_count = 0,
		    error_message = NULL,
		    started_at = NULL,
		    completed_at = NULL,
		    updated_at = datetime('now')
		WHERE id IN (%s)
	`, strings.Join(placeholders, ","))

	result, err := r.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("failed to restart queue items: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("no queue items found to restart")
	}

	return nil
}

// GetQueueStats retrieves current queue statistics
func (r *Repository) GetQueueStats(ctx context.Context) (*QueueStats, error) {
	// Update stats from actual queue data
	err := r.UpdateQueueStats(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to update queue stats: %w", err)
	}

	query := `
		SELECT id, total_queued, total_processing, total_completed, total_failed, 
		       avg_processing_time_ms, last_updated
		FROM queue_stats ORDER BY id DESC LIMIT 1
	`

	var stats QueueStats
	err = r.db.QueryRowContext(ctx, query).Scan(
		&stats.ID, &stats.TotalQueued, &stats.TotalProcessing, &stats.TotalCompleted,
		&stats.TotalFailed, &stats.AvgProcessingTimeMs, &stats.LastUpdated,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			// Initialize default stats if none exist
			defaultStats := &QueueStats{
				TotalQueued:     0,
				TotalProcessing: 0,
				TotalCompleted:  0,
				TotalFailed:     0,
				LastUpdated:     time.Now(),
			}
			return defaultStats, nil
		}
		return nil, fmt.Errorf("failed to get queue stats: %w", err)
	}

	return &stats, nil
}

// UpdateQueueStats updates queue statistics based on current queue state
func (r *Repository) UpdateQueueStats(ctx context.Context) error {
	// Get current counts
	countQueries := []string{
		`SELECT COUNT(*) FROM import_queue WHERE status = 'pending'`,
		`SELECT COUNT(*) FROM import_queue WHERE status = 'processing'`,
		`SELECT COUNT(*) FROM import_queue WHERE status = 'completed'`,
		`SELECT COUNT(*) FROM import_queue WHERE status = 'failed'`,
	}

	var counts [4]int
	for i, query := range countQueries {
		err := r.db.QueryRowContext(ctx, query).Scan(&counts[i])
		if err != nil {
			return fmt.Errorf("failed to get count for query %d: %w", i, err)
		}
	}

	// Calculate average processing time for completed items
	var avgProcessingTimeFloat sql.NullFloat64
	avgQuery := `
		SELECT AVG((julianday(completed_at) - julianday(started_at)) * 24 * 60 * 60 * 1000)
		FROM import_queue 
		WHERE status = 'completed' AND started_at IS NOT NULL AND completed_at IS NOT NULL
	`
	err := r.db.QueryRowContext(ctx, avgQuery).Scan(&avgProcessingTimeFloat)
	if err != nil {
		return fmt.Errorf("failed to calculate average processing time: %w", err)
	}

	// Convert float to int64 for storage
	var avgProcessingTime sql.NullInt64
	if avgProcessingTimeFloat.Valid {
		avgProcessingTime = sql.NullInt64{
			Int64: int64(avgProcessingTimeFloat.Float64),
			Valid: true,
		}
	}

	// Update or insert stats
	updateQuery := `
		UPDATE queue_stats SET 
		total_queued = ?, total_processing = ?, total_completed = ?, total_failed = ?,
		avg_processing_time_ms = ?, last_updated = ?
		WHERE id = (SELECT MAX(id) FROM queue_stats)
	`

	var avgTime any
	if avgProcessingTime.Valid {
		avgTime = avgProcessingTime.Int64
	} else {
		avgTime = nil
	}

	_, err = r.db.ExecContext(ctx, updateQuery, counts[0], counts[1], counts[2], counts[3], avgTime, time.Now())
	if err != nil {
		return fmt.Errorf("failed to update queue stats: %w", err)
	}

	return nil
}

// ListQueueItems retrieves queue items with optional filtering
func (r *Repository) ListQueueItems(ctx context.Context, status *QueueStatus, search string, category string, limit, offset int, sortBy, sortOrder string) ([]*ImportQueueItem, error) {
	var query string
	var args []any

	baseSelect := `SELECT id, nzb_path, relative_path, category, priority, status, created_at, updated_at,
	               started_at, completed_at, retry_count, max_retries, error_message, batch_id, metadata, file_size, storage_path
	               FROM import_queue`

	var conditions []string
	var conditionArgs []any

	if status != nil {
		conditions = append(conditions, "status = ?")
		conditionArgs = append(conditionArgs, *status)
	}

	if search != "" {
		conditions = append(conditions, "(nzb_path LIKE ? OR relative_path LIKE ?)")
		searchPattern := "%" + search + "%"
		conditionArgs = append(conditionArgs, searchPattern, searchPattern)
	}

	if category != "" {
		conditions = append(conditions, "category = ?")
		conditionArgs = append(conditionArgs, category)
	}

	if len(conditions) > 0 {
		query = baseSelect + " WHERE " + strings.Join(conditions, " AND ")
	} else {
		query = baseSelect
	}

	// Build ORDER BY clause with validation
	var orderByColumn string
	switch sortBy {
	case "created_at":
		orderByColumn = "created_at"
	case "updated_at":
		orderByColumn = "updated_at"
	case "status":
		orderByColumn = "status"
	case "nzb_path":
		orderByColumn = "nzb_path"
	default:
		orderByColumn = "updated_at"
	}

	sortDirection := "DESC"
	if sortOrder == "asc" {
		sortDirection = "ASC"
	}

	query += fmt.Sprintf(" ORDER BY %s %s LIMIT ? OFFSET ?", orderByColumn, sortDirection)
	args = append(conditionArgs, limit, offset)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list queue items: %w", err)
	}
	defer rows.Close()

	var items []*ImportQueueItem
	for rows.Next() {
		var item ImportQueueItem
		err := rows.Scan(
			&item.ID, &item.NzbPath, &item.RelativePath, &item.Category, &item.Priority, &item.Status,
			&item.CreatedAt, &item.UpdatedAt, &item.StartedAt, &item.CompletedAt,
			&item.RetryCount, &item.MaxRetries, &item.ErrorMessage, &item.BatchID, &item.Metadata, &item.FileSize, &item.StoragePath,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan queue item: %w", err)
		}
		items = append(items, &item)
	}

	return items, rows.Err()
}

// ListActiveQueueItems retrieves pending and processing queue items
func (r *Repository) ListActiveQueueItems(ctx context.Context, search string, category string, limit, offset int, sortBy, sortOrder string) ([]*ImportQueueItem, error) {
	var query string
	var args []any

	baseSelect := `SELECT id, nzb_path, relative_path, category, priority, status, created_at, updated_at,
	               started_at, completed_at, retry_count, max_retries, error_message, batch_id, metadata, file_size, storage_path
	               FROM import_queue`

	conditions := []string{"(status = 'pending' OR status = 'processing' OR status = 'paused')"}
	var conditionArgs []any

	if search != "" {
		conditions = append(conditions, "(nzb_path LIKE ? OR relative_path LIKE ?)")
		searchPattern := "%" + search + "%"
		conditionArgs = append(conditionArgs, searchPattern, searchPattern)
	}

	if category != "" {
		conditions = append(conditions, "category = ?")
		conditionArgs = append(conditionArgs, category)
	}

	query = baseSelect + " WHERE " + strings.Join(conditions, " AND ")

	// Build ORDER BY clause with validation
	var orderByColumn string
	switch sortBy {
	case "created_at":
		orderByColumn = "created_at"
	case "updated_at":
		orderByColumn = "updated_at"
	case "status":
		orderByColumn = "status"
	case "nzb_path":
		orderByColumn = "nzb_path"
	default:
		orderByColumn = "updated_at"
	}

	sortDirection := "DESC"
	if sortOrder == "asc" {
		sortDirection = "ASC"
	}

	query += fmt.Sprintf(" ORDER BY %s %s LIMIT ? OFFSET ?", orderByColumn, sortDirection)
	args = append(conditionArgs, limit, offset)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list active queue items: %w", err)
	}
	defer rows.Close()

	var items []*ImportQueueItem
	for rows.Next() {
		var item ImportQueueItem
		err := rows.Scan(
			&item.ID, &item.NzbPath, &item.RelativePath, &item.Category, &item.Priority, &item.Status,
			&item.CreatedAt, &item.UpdatedAt, &item.StartedAt, &item.CompletedAt,
			&item.RetryCount, &item.MaxRetries, &item.ErrorMessage, &item.BatchID, &item.Metadata, &item.FileSize, &item.StoragePath,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan queue item: %w", err)
		}
		items = append(items, &item)
	}

	return items, rows.Err()
}

// CountQueueItems counts the total number of queue items matching the given filters
func (r *Repository) CountQueueItems(ctx context.Context, status *QueueStatus, search string, category string) (int, error) {
	var query string
	var args []any

	baseQuery := `SELECT COUNT(*) FROM import_queue`

	var conditions []string
	var conditionArgs []any

	if status != nil {
		conditions = append(conditions, "status = ?")
		conditionArgs = append(conditionArgs, *status)
	}

	if search != "" {
		conditions = append(conditions, "(nzb_path LIKE ? OR relative_path LIKE ?)")
		searchPattern := "%" + search + "%"
		conditionArgs = append(conditionArgs, searchPattern, searchPattern)
	}

	if category != "" {
		conditions = append(conditions, "category = ?")
		conditionArgs = append(conditionArgs, category)
	}

	if len(conditions) > 0 {
		query = baseQuery + " WHERE " + strings.Join(conditions, " AND ")
	} else {
		query = baseQuery
	}

	args = conditionArgs

	var count int
	err := r.db.QueryRowContext(ctx, query, args...).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count queue items: %w", err)
	}

	return count, nil
}

// CountActiveQueueItems counts the total number of pending and processing queue items
func (r *Repository) CountActiveQueueItems(ctx context.Context, search string, category string) (int, error) {
	var query string
	var args []any

	baseQuery := `SELECT COUNT(*) FROM import_queue WHERE (status = 'pending' OR status = 'processing' OR status = 'paused')`

	var conditions []string
	var conditionArgs []any

	if search != "" {
		conditions = append(conditions, "(nzb_path LIKE ? OR relative_path LIKE ?)")
		searchPattern := "%" + search + "%"
		conditionArgs = append(conditionArgs, searchPattern, searchPattern)
	}

	if category != "" {
		conditions = append(conditions, "category = ?")
		conditionArgs = append(conditionArgs, category)
	}

	if len(conditions) > 0 {
		query = baseQuery + " AND " + strings.Join(conditions, " AND ")
	} else {
		query = baseQuery
	}

	args = conditionArgs

	var count int
	err := r.db.QueryRowContext(ctx, query, args...).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count active queue items: %w", err)
	}

	return count, nil
}

// ClearCompletedQueueItems removes completed and failed items from the queue
func (r *Repository) ClearCompletedQueueItems(ctx context.Context) (int, error) {
	query := `
		DELETE FROM import_queue 
		WHERE status IN ('completed')
	`

	result, err := r.db.ExecContext(ctx, query)
	if err != nil {
		return 0, fmt.Errorf("failed to clear completed queue items: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to get rows affected: %w", err)
	}

	return int(rowsAffected), nil
}

// ClearFailedQueueItems removes failed items from the queue
func (r *Repository) ClearFailedQueueItems(ctx context.Context) (int, error) {
	query := `
		DELETE FROM import_queue
		WHERE status = 'failed'
	`

	result, err := r.db.ExecContext(ctx, query)
	if err != nil {
		return 0, fmt.Errorf("failed to clear failed queue items: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to get rows affected: %w", err)
	}

	return int(rowsAffected), nil
}

// ClearPendingQueueItems removes pending items from the queue
func (r *Repository) ClearPendingQueueItems(ctx context.Context) (int, error) {
	query := `
		DELETE FROM import_queue
		WHERE status = 'pending'
	`

	result, err := r.db.ExecContext(ctx, query)
	if err != nil {
		return 0, fmt.Errorf("failed to clear pending queue items: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to get rows affected: %w", err)
	}

	return int(rowsAffected), nil
}

// IsFileInQueue checks if a file is already in the queue (pending or processing)
func (r *Repository) IsFileInQueue(ctx context.Context, filePath string) (bool, error) {
	query := `SELECT 1 FROM import_queue WHERE nzb_path = ? AND (status = 'pending' OR status = 'processing' OR status = 'paused') LIMIT 1`

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

// FilterExistingNzbdavIds checks a list of nzbdav IDs and returns those that already exist in the queue
// This is used for deduplication during import
func (r *Repository) FilterExistingNzbdavIds(ctx context.Context, ids []string) ([]string, error) {
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
		// We use DISTINCT to avoid duplicates in the result
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
func (r *Repository) UpdateQueueItemPriority(ctx context.Context, id int64, priority QueuePriority) error {
	query := `UPDATE import_queue SET priority = ?, updated_at = datetime('now') WHERE id = ?`
	_, err := r.db.ExecContext(ctx, query, priority, id)
	if err != nil {
		return fmt.Errorf("failed to update queue item priority: %w", err)
	}
	return nil
}

// AddImportHistory records a successful file import in the persistent history table
func (r *Repository) AddImportHistory(ctx context.Context, history *ImportHistory) error {
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

// ListImportHistory retrieves import history items with optional filtering and pagination
func (r *Repository) ListImportHistory(ctx context.Context, limit, offset int, search string, category string) ([]*ImportHistory, error) {
	query := `
		SELECT h.id, h.nzb_id, h.nzb_name, h.file_name, h.file_size, h.virtual_path, f.library_path, h.category, h.completed_at
		FROM import_history h
		LEFT JOIN file_health f ON h.virtual_path = f.file_path
		WHERE (? = '' OR h.nzb_name LIKE ? OR h.file_name LIKE ? OR h.virtual_path LIKE ?)
		  AND (? = '' OR h.category = ?)
		ORDER BY h.completed_at DESC
		LIMIT ? OFFSET ?
	`

	searchPattern := "%" + search + "%"
	rows, err := r.db.QueryContext(ctx, query, search, searchPattern, searchPattern, searchPattern, category, category, limit, offset)
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

	return history, rows.Err()
}

// GetImportDailyStats retrieves import statistics for the specified number of days
func (r *Repository) GetImportDailyStats(ctx context.Context, days int) ([]*ImportDailyStat, error) {
	query := `
		SELECT day, completed_count, failed_count, bytes_downloaded, updated_at
		FROM import_daily_stats
		WHERE day >= date('now', ?)
		ORDER BY day ASC
	`

	rows, err := r.db.QueryContext(ctx, query, fmt.Sprintf("-%d days", days))
	if err != nil {
		return nil, fmt.Errorf("failed to get import daily stats: %w", err)
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

	return stats, rows.Err()
}

// GetImportHourlyStats retrieves import statistics for the specified number of hours
func (r *Repository) GetImportHourlyStats(ctx context.Context, hours int) ([]*ImportHourlyStat, error) {
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

// AddBytesDownloadedToDailyStat increments the bytes_downloaded counter for the current day
func (r *Repository) AddBytesDownloadedToDailyStat(ctx context.Context, bytes int64) error {
	if bytes <= 0 {
		return nil
	}

	// Also add to hourly stat for rolling 24h calculation
	_ = r.AddBytesDownloadedToHourlyStat(ctx, bytes)

	query := `
		INSERT INTO import_daily_stats (day, bytes_downloaded, updated_at)
		VALUES (date('now'), ?, datetime('now'))
		ON CONFLICT(day) DO UPDATE SET
		bytes_downloaded = bytes_downloaded + excluded.bytes_downloaded,
		updated_at = datetime('now')
	`

	_, err := r.db.ExecContext(ctx, query, bytes)
	return err
}

// AddBytesDownloadedToHourlyStat increments the bytes_downloaded counter for the current hour
func (r *Repository) AddBytesDownloadedToHourlyStat(ctx context.Context, bytes int64) error {
	if bytes <= 0 {
		return nil
	}

	// Calculate start of current hour: YYYY-MM-DD HH:00:00
	currentHour := time.Now().UTC().Truncate(time.Hour)

	query := `
		INSERT INTO import_hourly_stats (hour, bytes_downloaded, updated_at)
		VALUES (?, ?, datetime('now'))
		ON CONFLICT(hour) DO UPDATE SET
		bytes_downloaded = bytes_downloaded + excluded.bytes_downloaded,
		updated_at = datetime('now')
	`

	_, err := r.db.ExecContext(ctx, query, currentHour, bytes)
	return err
}

// IncrementHourlyStat increments the completed or failed count for the current hour
func (r *Repository) IncrementHourlyStat(ctx context.Context, statType string) error {
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


// GetImportHistory retrieves historical import statistics for the last N days (Alias for GetImportDailyStats)
func (r *Repository) GetImportHistory(ctx context.Context, days int) ([]*ImportDailyStat, error) {
	return r.GetImportDailyStats(ctx, days)
}

// GetImportHistoryItem retrieves a specific import history item by ID
func (r *Repository) GetImportHistoryItem(ctx context.Context, id int64) (*ImportHistory, error) {
	query := `
		SELECT h.id, h.nzb_id, h.nzb_name, h.file_name, h.file_size, h.virtual_path, f.library_path, h.category, h.completed_at
		FROM import_history h
		LEFT JOIN file_health f ON h.virtual_path = f.file_path
		WHERE h.id = ?
	`

	var h ImportHistory
	err := r.db.QueryRowContext(ctx, query, id).Scan(&h.ID, &h.NzbID, &h.NzbName, &h.FileName, &h.FileSize, &h.VirtualPath, &h.LibraryPath, &h.Category, &h.CompletedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get import history item: %w", err)
	}

	return &h, nil
}

// GetSystemStats retrieves all system statistics as a map
func (r *Repository) GetSystemStats(ctx context.Context) (map[string]int64, error) {
	query := `SELECT key, value FROM system_stats`
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to get system stats: %w", err)
	}
	defer rows.Close()

	stats := make(map[string]int64)
	for rows.Next() {
		var key string
		var value int64
		if err := rows.Scan(&key, &value); err != nil {
			return nil, fmt.Errorf("failed to scan system stat: %w", err)
		}
		stats[key] = value
	}

	return stats, nil
}

// UpdateSystemStat updates or inserts a single system statistic
func (r *Repository) UpdateSystemStat(ctx context.Context, key string, value int64) error {
	query := `
		INSERT INTO system_stats (key, value, updated_at)
		VALUES (?, ?, datetime('now'))
		ON CONFLICT(key) DO UPDATE SET
		value = excluded.value,
		updated_at = datetime('now')
	`
	_, err := r.db.ExecContext(ctx, query, key, value)
	if err != nil {
		return fmt.Errorf("failed to update system stat %s: %w", key, err)
	}
	return nil
}

// BatchUpdateSystemStats updates multiple system statistics in a single transaction
func (r *Repository) BatchUpdateSystemStats(ctx context.Context, stats map[string]int64) error {
	if len(stats) == 0 {
		return nil
	}

	// Cast to *sql.DB to access BeginTx method
	sqlDB, ok := r.db.(*sql.DB)
	if !ok {
		return fmt.Errorf("repository not connected to sql.DB, cannot begin transaction")
	}

	tx, err := sqlDB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	query := `
		INSERT INTO system_stats (key, value, updated_at)
		VALUES (?, ?, datetime('now'))
		ON CONFLICT(key) DO UPDATE SET
		value = excluded.value,
		updated_at = datetime('now')
	`

	stmt, err := tx.PrepareContext(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	for key, value := range stats {
		if _, err := stmt.ExecContext(ctx, key, value); err != nil {
			return fmt.Errorf("failed to execute statement for key %s: %w", key, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// UpdateSystemState updates a system state string (JSON) by key
func (r *Repository) UpdateSystemState(ctx context.Context, key string, value string) error {
	query := `
		INSERT INTO system_state (key, value, updated_at)
		VALUES (?, ?, datetime('now'))
		ON CONFLICT(key) DO UPDATE SET
		value = excluded.value,
		updated_at = datetime('now')
	`
	_, err := r.db.ExecContext(ctx, query, key, value)
	if err != nil {
		return fmt.Errorf("failed to update system state %s: %w", key, err)
	}
	return nil
}

// GetSystemState retrieves a system state string by key
func (r *Repository) GetSystemState(ctx context.Context, key string) (string, error) {
	query := `SELECT value FROM system_state WHERE key = ?`
	var value string
	err := r.db.QueryRowContext(ctx, query, key).Scan(&value)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", nil
		}
		return "", fmt.Errorf("failed to get system state %s: %w", key, err)
	}
	return value, nil
}

// ClearImportHistory deletes all records from the import_history and import_daily_stats tables
func (r *Repository) ClearImportHistory(ctx context.Context) error {
	// Clear history records
	queryHistory := `DELETE FROM import_history`
	if _, err := r.db.ExecContext(ctx, queryHistory); err != nil {
		return fmt.Errorf("failed to clear import history: %w", err)
	}

	// Also clear daily stats
	return r.ClearDailyStats(ctx)
}

// ClearImportHistorySince deletes records from the import_history and import_queue tables
// since the specified time, and adjusts the import_daily_stats accordingly.
func (r *Repository) ClearImportHistorySince(ctx context.Context, since time.Time) error {
	return r.WithTransaction(ctx, func(txRepo *Repository) error {
		// 1. Count completed items in history per day
		queryHistoryCounts := `
			SELECT date(completed_at) as day, COUNT(*) as count
			FROM import_history
			WHERE completed_at >= ?
			GROUP BY day
		`
		rows, err := txRepo.db.QueryContext(ctx, queryHistoryCounts, since)
		if err != nil {
			return fmt.Errorf("failed to query history counts: %w", err)
		}
		defer rows.Close()

		type dayCount struct {
			day   string
			count int
		}
		var historyCounts []dayCount
		for rows.Next() {
			var dc dayCount
			if err := rows.Scan(&dc.day, &dc.count); err != nil {
				return err
			}
			historyCounts = append(historyCounts, dc)
		}
		rows.Close()

		// 2. Count failed items in queue per day
		queryFailedCounts := `
			SELECT date(updated_at) as day, COUNT(*) as count
			FROM import_queue
			WHERE status = 'failed' AND updated_at >= ?
			GROUP BY day
		`
		rows, err = txRepo.db.QueryContext(ctx, queryFailedCounts, since)
		if err != nil {
			return fmt.Errorf("failed to query failed counts: %w", err)
		}
		defer rows.Close()

		var failedCounts []dayCount
		for rows.Next() {
			var dc dayCount
			if err := rows.Scan(&dc.day, &dc.count); err != nil {
				return err
			}
			failedCounts = append(failedCounts, dc)
		}
		rows.Close()

		// 3. Decrement daily stats for completed items
		for _, hc := range historyCounts {
			queryUpdate := `
				UPDATE import_daily_stats 
				SET completed_count = MAX(0, completed_count - ?), updated_at = datetime('now')
				WHERE day = ?
			`
			if _, err := txRepo.db.ExecContext(ctx, queryUpdate, hc.count, hc.day); err != nil {
				return fmt.Errorf("failed to decrement completed daily stats: %w", err)
			}
		}

		// 4. Decrement daily stats for failed items
		for _, fc := range failedCounts {
			queryUpdate := `
				UPDATE import_daily_stats 
				SET failed_count = MAX(0, failed_count - ?), updated_at = datetime('now')
				WHERE day = ?
			`
			if _, err := txRepo.db.ExecContext(ctx, queryUpdate, fc.count, fc.day); err != nil {
				return fmt.Errorf("failed to decrement failed daily stats: %w", err)
			}
		}

		// 5. Delete from import_history
		queryDeleteHistory := `DELETE FROM import_history WHERE completed_at >= ?`
		if _, err := txRepo.db.ExecContext(ctx, queryDeleteHistory, since); err != nil {
			return fmt.Errorf("failed to delete from import history: %w", err)
		}

		// 6. Delete from import_queue (completed and failed items)
		queryDeleteQueue := `
			DELETE FROM import_queue 
			WHERE status IN ('completed', 'failed') 
			  AND (
				(status = 'completed' AND completed_at >= ?) OR 
				(status = 'failed' AND updated_at >= ?)
			  )
		`
		if _, err := txRepo.db.ExecContext(ctx, queryDeleteQueue, since, since); err != nil {
			return fmt.Errorf("failed to delete from import queue: %w", err)
		}

		return nil
	})
}

// ClearDailyStats deletes all records from the import_daily_stats and import_hourly_stats tables
func (r *Repository) ClearDailyStats(ctx context.Context) error {
	queryDaily := `DELETE FROM import_daily_stats`
	if _, err := r.db.ExecContext(ctx, queryDaily); err != nil {
		return fmt.Errorf("failed to clear daily stats: %w", err)
	}

	return r.ClearHourlyStats(ctx)
}

// ClearHourlyStats deletes all records from the import_hourly_stats table
func (r *Repository) ClearHourlyStats(ctx context.Context) error {
	queryHourly := `DELETE FROM import_hourly_stats`
	if _, err := r.db.ExecContext(ctx, queryHourly); err != nil {
		return fmt.Errorf("failed to clear hourly stats: %w", err)
	}
	return nil
}

