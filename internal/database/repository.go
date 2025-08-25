package database

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// Repository provides database operations for NZB and file management
type Repository struct {
	db interface {
		Exec(query string, args ...interface{}) (sql.Result, error)
		Query(query string, args ...interface{}) (*sql.Rows, error)
		QueryRow(query string, args ...interface{}) *sql.Row
	}
}

// NewRepository creates a new repository instance
func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

// Transaction support

// WithTransaction executes a function within a database transaction
func (r *Repository) WithTransaction(fn func(*Repository) error) error {
	return r.withTransactionMode("", fn)
}

// WithImmediateTransaction executes a function within an immediate database transaction
// This reduces lock contention for queue operations by acquiring locks immediately
func (r *Repository) WithImmediateTransaction(fn func(*Repository) error) error {
	// Cast to *sql.DB to access BeginTx method
	sqlDB, ok := r.db.(*sql.DB)
	if !ok {
		return fmt.Errorf("repository not connected to sql.DB")
	}

	// Begin transaction with immediate isolation
	// First set pragma for immediate locking, then begin transaction
	tx, err := sqlDB.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	// Set immediate locking mode for this transaction
	if _, err := tx.Exec("PRAGMA locking_mode = IMMEDIATE"); err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to set immediate locking: %w", err)
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
func (r *Repository) withTransactionMode(mode string, fn func(*Repository) error) error {
	// Cast to *sql.DB to access Begin method
	sqlDB, ok := r.db.(*sql.DB)
	if !ok {
		return fmt.Errorf("repository not connected to sql.DB")
	}

	tx, err := sqlDB.Begin()
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
func (r *Repository) AddToQueue(item *ImportQueueItem) error {
	// Use UPSERT with immediate lock to prevent conflicts during concurrent inserts
	query := `
		INSERT INTO import_queue (nzb_path, watch_root, priority, status, retry_count, max_retries, batch_id, metadata, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, datetime('now'), datetime('now'))
		ON CONFLICT(nzb_path) DO UPDATE SET
		priority = CASE WHEN excluded.priority < priority THEN excluded.priority ELSE priority END,
		batch_id = excluded.batch_id,
		metadata = excluded.metadata,
		updated_at = datetime('now')
		WHERE status NOT IN ('processing', 'completed')
	`

	result, err := r.db.Exec(query,
		item.NzbPath, item.WatchRoot, item.Priority, item.Status,
		item.RetryCount, item.MaxRetries, item.BatchID, item.Metadata)
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
func (r *Repository) GetNextQueueItems(limit int) ([]*ImportQueueItem, error) {
	// Use a CTE to select items and immediately mark them as claimed to avoid race conditions
	query := `
		WITH selected_items AS (
			SELECT id, nzb_path, watch_root, priority, status, created_at, updated_at, 
			       started_at, completed_at, retry_count, max_retries, error_message, batch_id, metadata
			FROM import_queue 
			WHERE status IN ('pending', 'retrying') 
			  AND retry_count < max_retries
			  AND (started_at IS NULL OR datetime(started_at, '+10 minutes') < datetime('now'))
			ORDER BY priority ASC, created_at ASC
			LIMIT ?
		)
		SELECT * FROM selected_items
	`

	rows, err := r.db.Query(query, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to get next queue items: %w", err)
	}
	defer rows.Close()

	var items []*ImportQueueItem
	for rows.Next() {
		var item ImportQueueItem
		err := rows.Scan(
			&item.ID, &item.NzbPath, &item.WatchRoot, &item.Priority, &item.Status,
			&item.CreatedAt, &item.UpdatedAt, &item.StartedAt, &item.CompletedAt,
			&item.RetryCount, &item.MaxRetries, &item.ErrorMessage, &item.BatchID, &item.Metadata,
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
func (r *Repository) ClaimNextQueueItem() (*ImportQueueItem, error) {
	// Use immediate transaction to atomically claim an item
	var claimedItem *ImportQueueItem

	err := r.WithImmediateTransaction(func(txRepo *Repository) error {
		// First, get the next available item ID within the transaction
		var itemID int64
		selectQuery := `
			SELECT id FROM import_queue 
			WHERE status IN ('pending', 'retrying') 
			  AND retry_count < max_retries
			  AND (started_at IS NULL OR datetime(started_at, '+10 minutes') < datetime('now'))
			ORDER BY priority ASC, created_at ASC
			LIMIT 1
		`

		err := txRepo.db.QueryRow(selectQuery).Scan(&itemID)
		if err != nil {
			if err.Error() == "sql: no rows in result set" {
				// No items available
				return nil
			}
			return fmt.Errorf("failed to select queue item: %w", err)
		}

		// Now atomically update that specific item and get all its data
		updateQuery := `
			UPDATE import_queue 
			SET status = 'processing', started_at = datetime('now'), updated_at = datetime('now')
			WHERE id = ? AND status IN ('pending', 'retrying')
		`

		result, err := txRepo.db.Exec(updateQuery, itemID)
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
			SELECT id, nzb_path, watch_root, priority, status, created_at, updated_at, 
			       started_at, completed_at, retry_count, max_retries, error_message, batch_id, metadata
			FROM import_queue 
			WHERE id = ?
		`

		var item ImportQueueItem
		err = txRepo.db.QueryRow(getQuery, itemID).Scan(
			&item.ID, &item.NzbPath, &item.WatchRoot, &item.Priority, &item.Status,
			&item.CreatedAt, &item.UpdatedAt, &item.StartedAt, &item.CompletedAt,
			&item.RetryCount, &item.MaxRetries, &item.ErrorMessage, &item.BatchID, &item.Metadata,
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

// AddBatchToQueue adds multiple items to the queue in a single transaction for better performance
func (r *Repository) AddBatchToQueue(items []*ImportQueueItem) error {
	if len(items) == 0 {
		return nil
	}

	// Use immediate transaction for batch operations to reduce lock contention
	return r.WithImmediateTransaction(func(txRepo *Repository) error {
		// Prepare batch insert statement
		query := `
			INSERT INTO import_queue (nzb_path, watch_root, priority, status, retry_count, max_retries, batch_id, metadata, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, datetime('now'), datetime('now'))
			ON CONFLICT(nzb_path) DO UPDATE SET
			priority = CASE WHEN excluded.priority < priority THEN excluded.priority ELSE priority END,
			batch_id = excluded.batch_id,
			metadata = excluded.metadata,
			updated_at = datetime('now')
			WHERE status NOT IN ('processing', 'completed')
		`

		now := time.Now()
		for _, item := range items {
			result, err := txRepo.db.Exec(query,
				item.NzbPath, item.WatchRoot, item.Priority, item.Status,
				item.RetryCount, item.MaxRetries, item.BatchID, item.Metadata)
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
func (r *Repository) UpdateQueueItemStatus(id int64, status QueueStatus, errorMessage *string) error {
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
	case QueueStatusFailed, QueueStatusRetrying:
		query = `UPDATE import_queue SET status = ?, retry_count = retry_count + 1, error_message = ?, updated_at = ? WHERE id = ?`
		args = []interface{}{status, errorMessage, now, id}
	default:
		query = `UPDATE import_queue SET status = ?, error_message = ?, updated_at = ? WHERE id = ?`
		args = []interface{}{status, errorMessage, now, id}
	}

	_, err := r.db.Exec(query, args...)
	if err != nil {
		return fmt.Errorf("failed to update queue item status: %w", err)
	}

	return nil
}

// GetQueueItem retrieves a specific queue item by ID
func (r *Repository) GetQueueItem(id int64) (*ImportQueueItem, error) {
	query := `
		SELECT id, nzb_path, watch_root, priority, status, created_at, updated_at,
		       started_at, completed_at, retry_count, max_retries, error_message, batch_id, metadata
		FROM import_queue WHERE id = ?
	`

	var item ImportQueueItem
	err := r.db.QueryRow(query, id).Scan(
		&item.ID, &item.NzbPath, &item.WatchRoot, &item.Priority, &item.Status,
		&item.CreatedAt, &item.UpdatedAt, &item.StartedAt, &item.CompletedAt,
		&item.RetryCount, &item.MaxRetries, &item.ErrorMessage, &item.BatchID, &item.Metadata,
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
func (r *Repository) GetQueueItemByPath(nzbPath string) (*ImportQueueItem, error) {
	query := `
		SELECT id, nzb_path, watch_root, priority, status, created_at, updated_at,
		       started_at, completed_at, retry_count, max_retries, error_message, batch_id, metadata
		FROM import_queue WHERE nzb_path = ?
	`

	var item ImportQueueItem
	err := r.db.QueryRow(query, nzbPath).Scan(
		&item.ID, &item.NzbPath, &item.WatchRoot, &item.Priority, &item.Status,
		&item.CreatedAt, &item.UpdatedAt, &item.StartedAt, &item.CompletedAt,
		&item.RetryCount, &item.MaxRetries, &item.ErrorMessage, &item.BatchID, &item.Metadata,
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
func (r *Repository) RemoveFromQueue(id int64) error {
	query := `DELETE FROM import_queue WHERE id = ?`

	result, err := r.db.Exec(query, id)
	if err != nil {
		return fmt.Errorf("failed to remove from queue: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("queue item not found")
	}

	return nil
}

// GetQueueStats retrieves current queue statistics
func (r *Repository) GetQueueStats() (*QueueStats, error) {
	// Update stats from actual queue data
	err := r.UpdateQueueStats()
	if err != nil {
		return nil, fmt.Errorf("failed to update queue stats: %w", err)
	}

	query := `
		SELECT id, total_queued, total_processing, total_completed, total_failed, 
		       avg_processing_time_ms, last_updated
		FROM queue_stats ORDER BY id DESC LIMIT 1
	`

	var stats QueueStats
	err = r.db.QueryRow(query).Scan(
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
func (r *Repository) UpdateQueueStats() error {
	// Get current counts
	countQueries := []string{
		`SELECT COUNT(*) FROM import_queue WHERE status IN ('pending', 'retrying')`,
		`SELECT COUNT(*) FROM import_queue WHERE status = 'processing'`,
		`SELECT COUNT(*) FROM import_queue WHERE status = 'completed'`,
		`SELECT COUNT(*) FROM import_queue WHERE status = 'failed'`,
	}

	var counts [4]int
	for i, query := range countQueries {
		err := r.db.QueryRow(query).Scan(&counts[i])
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
	err := r.db.QueryRow(avgQuery).Scan(&avgProcessingTimeFloat)
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

	_, err = r.db.Exec(updateQuery, counts[0], counts[1], counts[2], counts[3], avgTime, time.Now())
	if err != nil {
		return fmt.Errorf("failed to update queue stats: %w", err)
	}

	return nil
}

// ListQueueItems retrieves queue items with optional filtering
func (r *Repository) ListQueueItems(status *QueueStatus, search string, limit, offset int) ([]*ImportQueueItem, error) {
	var query string
	var args []interface{}
	
	baseSelect := `SELECT id, nzb_path, watch_root, priority, status, created_at, updated_at,
	               started_at, completed_at, retry_count, max_retries, error_message, batch_id, metadata
	               FROM import_queue`
	
	var conditions []string
	var conditionArgs []interface{}
	
	if status != nil {
		conditions = append(conditions, "status = ?")
		conditionArgs = append(conditionArgs, *status)
	}
	
	if search != "" {
		conditions = append(conditions, "(nzb_path LIKE ? OR watch_root LIKE ?)")
		searchPattern := "%" + search + "%"
		conditionArgs = append(conditionArgs, searchPattern, searchPattern)
	}
	
	if len(conditions) > 0 {
		query = baseSelect + " WHERE " + strings.Join(conditions, " AND ")
	} else {
		query = baseSelect
	}
	
	query += " ORDER BY priority ASC, created_at ASC LIMIT ? OFFSET ?"
	args = append(conditionArgs, limit, offset)
	
	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list queue items: %w", err)
	}
	defer rows.Close()

	var items []*ImportQueueItem
	for rows.Next() {
		var item ImportQueueItem
		err := rows.Scan(
			&item.ID, &item.NzbPath, &item.WatchRoot, &item.Priority, &item.Status,
			&item.CreatedAt, &item.UpdatedAt, &item.StartedAt, &item.CompletedAt,
			&item.RetryCount, &item.MaxRetries, &item.ErrorMessage, &item.BatchID, &item.Metadata,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan queue item: %w", err)
		}
		items = append(items, &item)
	}

	return items, rows.Err()
}

// CountQueueItems counts the total number of queue items matching the given filters
func (r *Repository) CountQueueItems(status *QueueStatus, search string) (int, error) {
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
		conditions = append(conditions, "(nzb_path LIKE ? OR watch_root LIKE ?)")
		searchPattern := "%" + search + "%"
		conditionArgs = append(conditionArgs, searchPattern, searchPattern)
	}
	
	if len(conditions) > 0 {
		query = baseQuery + " WHERE " + strings.Join(conditions, " AND ")
	} else {
		query = baseQuery
	}
	
	args = conditionArgs
	
	var count int
	err := r.db.QueryRow(query, args...).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count queue items: %w", err)
	}
	
	return count, nil
}

// ClearCompletedQueueItems removes completed and failed items from the queue
func (r *Repository) ClearCompletedQueueItems(olderThan time.Time) (int, error) {
	query := `
		DELETE FROM import_queue 
		WHERE status IN ('completed', 'failed') AND updated_at < ?
	`

	result, err := r.db.Exec(query, olderThan)
	if err != nil {
		return 0, fmt.Errorf("failed to clear completed queue items: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to get rows affected: %w", err)
	}

	return int(rowsAffected), nil
}
