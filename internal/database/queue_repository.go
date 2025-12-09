package database

import (
	"context"
	"database/sql"
	"fmt"
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
	query := `SELECT 1 FROM import_queue WHERE nzb_path = ? AND status IN ('pending', 'processing') LIMIT 1`

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

// UpdateQueueItemPriority updates the priority of a queue item
func (r *QueueRepository) UpdateQueueItemPriority(ctx context.Context, id int64, priority QueuePriority) error {
	query := `UPDATE import_queue SET priority = ?, updated_at = datetime('now') WHERE id = ?`
	_, err := r.db.ExecContext(ctx, query, priority, id)
	if err != nil {
		return fmt.Errorf("failed to update queue item priority: %w", err)
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
	query := `
		UPDATE import_queue
		SET status = 'pending', started_at = NULL, updated_at = datetime('now')
		WHERE status = 'processing'
	`

	result, err := r.db.ExecContext(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to reset stale queue items: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected > 0 {
		// Log the reset operation for operational visibility
		fmt.Printf("Reset %d stale queue items from processing to pending\n", rowsAffected)
	}

	return nil
}
