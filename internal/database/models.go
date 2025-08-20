package database

import (
	"time"
)

// QueueStatus represents the status of a queued import
type QueueStatus string

const (
	QueueStatusPending    QueueStatus = "pending"
	QueueStatusProcessing QueueStatus = "processing"
	QueueStatusCompleted  QueueStatus = "completed"
	QueueStatusFailed     QueueStatus = "failed"
	QueueStatusRetrying   QueueStatus = "retrying"
)

// QueuePriority represents the priority level of a queued import
type QueuePriority int

const (
	QueuePriorityHigh   QueuePriority = 1
	QueuePriorityNormal QueuePriority = 2
	QueuePriorityLow    QueuePriority = 3
)

// ImportQueueItem represents a queued NZB file waiting for import
type ImportQueueItem struct {
	ID           int64         `db:"id"`
	NzbPath      string        `db:"nzb_path"`
	WatchRoot    *string       `db:"watch_root"`
	Priority     QueuePriority `db:"priority"`
	Status       QueueStatus   `db:"status"`
	CreatedAt    time.Time     `db:"created_at"`
	UpdatedAt    time.Time     `db:"updated_at"`
	StartedAt    *time.Time    `db:"started_at"`
	CompletedAt  *time.Time    `db:"completed_at"`
	RetryCount   int           `db:"retry_count"`
	MaxRetries   int           `db:"max_retries"`
	ErrorMessage *string       `db:"error_message"`
	BatchID      *string       `db:"batch_id"`
	Metadata     *string       `db:"metadata"` // JSON metadata
}

// QueueStats represents statistics about the import queue
type QueueStats struct {
	ID                  int64     `db:"id"`
	TotalQueued         int       `db:"total_queued"`
	TotalProcessing     int       `db:"total_processing"`
	TotalCompleted      int       `db:"total_completed"`
	TotalFailed         int       `db:"total_failed"`
	AvgProcessingTimeMs *int      `db:"avg_processing_time_ms"`
	LastUpdated         time.Time `db:"last_updated"`
}

// HealthStatus represents the health status of a file
type HealthStatus string

const (
	HealthStatusHealthy   HealthStatus = "healthy"
	HealthStatusPartial   HealthStatus = "partial"
	HealthStatusCorrupted HealthStatus = "corrupted"
)

// FileHealth represents the health tracking of files in the filesystem
type FileHealth struct {
	ID            int64         `db:"id"`
	FilePath      string        `db:"file_path"`
	Status        HealthStatus  `db:"status"`
	LastChecked   time.Time     `db:"last_checked"`
	LastError     *string       `db:"last_error"`
	RetryCount    int           `db:"retry_count"`
	MaxRetries    int           `db:"max_retries"`
	NextRetryAt   *time.Time    `db:"next_retry_at"`
	SourceNzbPath *string       `db:"source_nzb_path"`
	ErrorDetails  *string       `db:"error_details"` // JSON error details
	CreatedAt     time.Time     `db:"created_at"`
	UpdatedAt     time.Time     `db:"updated_at"`
}
