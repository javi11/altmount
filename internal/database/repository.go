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
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
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
	var args []interface{}

	switch status {
	case QueueStatusProcessing:
		query = `UPDATE import_queue SET status = ?, started_at = ?, updated_at = ? WHERE id = ?`
		args = []interface{}{status, now, now, id}
	case QueueStatusCompleted:
		query = `UPDATE import_queue SET status = ?, completed_at = ?, updated_at = ?, error_message = NULL WHERE id = ?`
		args = []interface{}{status, now, now, id}
	case QueueStatusFailed:
		query = `UPDATE import_queue SET status = ?, error_message = ?, updated_at = ? WHERE id = ?`
		args = []interface{}{status, errorMessage, now, id}
	default:
		query = `UPDATE import_queue SET status = ?, error_message = ?, updated_at = ? WHERE id = ?`
		args = []interface{}{status, errorMessage, now, id}
	}

	_, err := r.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("failed to update queue item status: %w", err)
	}

	return nil
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
	args := make([]interface{}, len(ids))
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
	args := make([]interface{}, len(ids))
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

	var avgTime interface{}
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
	var args []interface{}

	baseSelect := `SELECT id, nzb_path, relative_path, category, priority, status, created_at, updated_at,
	               started_at, completed_at, retry_count, max_retries, error_message, batch_id, metadata, file_size, storage_path
	               FROM import_queue`

	var conditions []string
	var conditionArgs []interface{}

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
	var args []interface{}

	baseSelect := `SELECT id, nzb_path, relative_path, category, priority, status, created_at, updated_at,
	               started_at, completed_at, retry_count, max_retries, error_message, batch_id, metadata, file_size, storage_path
	               FROM import_queue`

	conditions := []string{"(status = 'pending' OR status = 'processing' OR status = 'paused')"}
	var conditionArgs []interface{}

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
	var args []interface{}

	baseQuery := `SELECT COUNT(*) FROM import_queue`

	var conditions []string
	var conditionArgs []interface{}

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
	var args []interface{}

	baseQuery := `SELECT COUNT(*) FROM import_queue WHERE (status = 'pending' OR status = 'processing' OR status = 'paused')`

	var conditions []string
	var conditionArgs []interface{}

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
		end := i + batchSize
		if end > len(ids) {
			end = len(ids)
		}

		batchIds := ids[i:end]

		// Build placeholders for the IN clause
		placeholders := make([]string, len(batchIds))
		args := make([]interface{}, len(batchIds))
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

// System stats operations

// GetSystemStat retrieves a system statistic by key
func (r *Repository) GetSystemStat(ctx context.Context, key string) (int64, error) {
	query := `SELECT value FROM system_stats WHERE key = ?`
	var value int64
	err := r.db.QueryRowContext(ctx, query, key).Scan(&value)
	if err != nil {
		if err == sql.ErrNoRows {
			return 0, nil
		}
		return 0, fmt.Errorf("failed to get system stat %s: %w", key, err)
	}
	return value, nil
}

// UpdateSystemStat updates a system statistic by key
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

// ResetSystemStats clears system statistics from the database
func (r *Repository) ResetSystemStats(ctx context.Context) error {
	query := `DELETE FROM system_stats WHERE key IN ('bytes_downloaded', 'articles_downloaded', 'max_download_speed')`
	_, err := r.db.ExecContext(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to reset system stats: %w", err)
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
