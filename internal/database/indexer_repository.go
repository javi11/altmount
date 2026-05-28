package database

import (
	"context"
	"fmt"
	"time"
)

type IndexerImportStat struct {
	ID           int64     `db:"id"`
	Indexer      string    `db:"indexer"`
	Status       string    `db:"status"`
	ErrorMessage *string   `db:"error_message"`
	CreatedAt    time.Time `db:"created_at"`
}

type IndexerAggregatedHealth struct {
	Indexer      string    `json:"indexer"`
	TotalImports int       `json:"total_imports"`
	SuccessCount int       `json:"success_count"`
	FailedCount  int       `json:"failed_count"`
	SuccessRate  float64   `json:"success_rate"`
	LastSeenAt   time.Time `json:"last_seen_at"`
}

// --- Consolidated Helper Functions ---

func logIndexerImport(ctx context.Context, db DBQuerier, indexer string, status string, errMsg string, downloadID *string) error {
	var errDetails any = nil
	if errMsg != "" {
		errDetails = errMsg
	}
	query := `
		INSERT INTO indexer_import_stats (indexer, status, error_message, download_id, created_at)
		VALUES (?, ?, ?, ?, datetime('now'))
	`
	_, err := db.ExecContext(ctx, query, indexer, status, errDetails, downloadID)
	return err
}

func getIndexerHealthStats(ctx context.Context, db DBQuerier) ([]*IndexerAggregatedHealth, error) {
	query := `
		SELECT 
			indexer,
			COUNT(*) as total_imports,
			SUM(CASE WHEN status = 'success' THEN 1 ELSE 0 END) as success_count,
			SUM(CASE WHEN status = 'failed' THEN 1 ELSE 0 END) as failed_count,
			MAX(created_at) as last_seen_at
		FROM indexer_import_stats
		GROUP BY indexer
		ORDER BY total_imports DESC
	`
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var stats []*IndexerAggregatedHealth
	for rows.Next() {
		var h IndexerAggregatedHealth
		var lastSeenStr string
		if err := rows.Scan(&h.Indexer, &h.TotalImports, &h.SuccessCount, &h.FailedCount, &lastSeenStr); err != nil {
			return nil, err
		}
		for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02T15:04:05"} {
			if t, err := time.Parse(layout, lastSeenStr); err == nil {
				h.LastSeenAt = t
				break
			}
		}
		if h.TotalImports > 0 {
			h.SuccessRate = (float64(h.SuccessCount) / float64(h.TotalImports)) * 100.0
		}
		stats = append(stats, &h)
	}
	return stats, rows.Err()
}

func pruneIndexerStats(ctx context.Context, db DBQuerier, hours int) (int64, error) {
	cutoff := time.Now().Add(-time.Duration(hours) * time.Hour)
	query := `DELETE FROM indexer_import_stats WHERE created_at >= ?`
	res, err := db.ExecContext(ctx, query, cutoff)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func deleteIndexerStats(ctx context.Context, db DBQuerier, indexer string) (int64, error) {
	query := `DELETE FROM indexer_import_stats WHERE indexer = ?`
	res, err := db.ExecContext(ctx, query, indexer)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func updateIndexerStatsByDownloadID(ctx context.Context, db DBQuerier, downloadID string, indexer string) error {
	query := `UPDATE indexer_import_stats SET indexer = ? WHERE download_id = ? AND indexer = 'Unknown'`
	_, err := db.ExecContext(ctx, query, indexer, downloadID)
	return err
}

func updateImportHistoryIndexerByDownloadID(ctx context.Context, db DBQuerier, downloadID string, indexer string) error {
	query := `UPDATE import_history SET indexer = ? WHERE download_id = ? AND (indexer = 'Unknown' OR indexer IS NULL)`
	_, err := db.ExecContext(ctx, query, indexer, downloadID)
	return err
}

// --- Repository Methods ---

// LogIndexerImport records a success or failure for an indexer persistently.
func (r *Repository) LogIndexerImport(ctx context.Context, indexer string, status string, errMsg string, downloadID *string) error {
	return logIndexerImport(ctx, r.db, indexer, status, errMsg, downloadID)
}

// GetIndexerHealthStats aggregates all historical records to calculate success/failure rates.
func (r *Repository) GetIndexerHealthStats(ctx context.Context) ([]*IndexerAggregatedHealth, error) {
	return getIndexerHealthStats(ctx, r.db)
}

// PruneIndexerStats deletes records that were created within the last N hours.
func (r *Repository) PruneIndexerStats(ctx context.Context, hours int) (int64, error) {
	return pruneIndexerStats(ctx, r.db, hours)
}

// DeleteIndexerStats deletes all records for a specific indexer and returns the number of rows affected.
func (r *Repository) DeleteIndexerStats(ctx context.Context, indexer string) (int64, error) {
	return deleteIndexerStats(ctx, r.db, indexer)
}

// --- QueueRepository Methods ---

// LogIndexerImport records a success or failure for an indexer persistently.
func (r *QueueRepository) LogIndexerImport(ctx context.Context, indexer string, status string, errMsg string, downloadID *string) error {
	return logIndexerImport(ctx, r.db, indexer, status, errMsg, downloadID)
}

// GetIndexerHealthStats aggregates all historical records to calculate success/failure rates.
func (r *QueueRepository) GetIndexerHealthStats(ctx context.Context) ([]*IndexerAggregatedHealth, error) {
	return getIndexerHealthStats(ctx, r.db)
}

// PruneIndexerStats deletes records that were created within the last N hours.
func (r *QueueRepository) PruneIndexerStats(ctx context.Context, hours int) (int64, error) {
	return pruneIndexerStats(ctx, r.db, hours)
}

// DeleteIndexerStats deletes all records for a specific indexer and returns the number of rows affected.
func (r *QueueRepository) DeleteIndexerStats(ctx context.Context, indexer string) (int64, error) {
	return deleteIndexerStats(ctx, r.db, indexer)
}

// UpdateQueueItemIndexer updates the indexer for a queue item by its ID
func (r *QueueRepository) UpdateQueueItemIndexer(ctx context.Context, id int64, indexer string) error {
	query := `UPDATE import_queue SET indexer = ?, updated_at = datetime('now') WHERE id = ?`
	_, err := r.db.ExecContext(ctx, query, indexer, id)
	if err != nil {
		return fmt.Errorf("failed to update queue item indexer by id: %w", err)
	}
	return nil
}

// UpdateQueueItemIndexerByDownloadID updates the indexer for a queue item by its DownloadID
func (r *QueueRepository) UpdateQueueItemIndexerByDownloadID(ctx context.Context, downloadID string, indexer string) error {
	query := `UPDATE import_queue SET indexer = ?, updated_at = datetime('now') WHERE download_id = ?`
	_, err := r.db.ExecContext(ctx, query, indexer, downloadID)
	if err != nil {
		return fmt.Errorf("failed to update queue item indexer by download_id: %w", err)
	}
	return nil
}

// UpdateIndexerStatsByDownloadID updates the indexer name for a stat record if it was logged as Unknown
func (r *Repository) UpdateIndexerStatsByDownloadID(ctx context.Context, downloadID string, indexer string) error {
	return updateIndexerStatsByDownloadID(ctx, r.db, downloadID, indexer)
}

func (r *QueueRepository) UpdateIndexerStatsByDownloadID(ctx context.Context, downloadID string, indexer string) error {
	return updateIndexerStatsByDownloadID(ctx, r.db, downloadID, indexer)
}

// UpdateImportHistoryIndexerByDownloadID updates the indexer for an import history item by its DownloadID
func (r *Repository) UpdateImportHistoryIndexerByDownloadID(ctx context.Context, downloadID string, indexer string) error {
	return updateImportHistoryIndexerByDownloadID(ctx, r.db, downloadID, indexer)
}

func (r *QueueRepository) UpdateImportHistoryIndexerByDownloadID(ctx context.Context, downloadID string, indexer string) error {
	return updateImportHistoryIndexerByDownloadID(ctx, r.db, downloadID, indexer)
}
