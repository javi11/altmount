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
	QueueStatusFallback   QueueStatus = "fallback" // Sent to external SABnzbd as fallback
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
	RelativePath *string       `db:"relative_path"`
	StoragePath  *string       `db:"storage_path"`
	Category     *string       `db:"category"` // SABnzbd-compatible category
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
	Metadata     *string       `db:"metadata"`  // JSON metadata
	FileSize     *int64        `db:"file_size"` // Total size in bytes calculated from segments
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
	HealthStatusPending         HealthStatus = "pending"          // File has not been checked yet
	HealthStatusChecking        HealthStatus = "checking"         // File is currently being checked
	HealthStatusRepairTriggered HealthStatus = "repair_triggered" // File repair has been triggered in Arrs
	HealthStatusCorrupted       HealthStatus = "corrupted"        // File has missing segments or is corrupted
)

// FileHealth represents the health tracking of files in the filesystem
type FileHealth struct {
	ID               int64        `db:"id"`
	FilePath         string       `db:"file_path"`
	Status           HealthStatus `db:"status"`
	LastChecked      time.Time    `db:"last_checked"`
	LastError        *string      `db:"last_error"`
	RetryCount       int          `db:"retry_count"`        // Health check retry count
	MaxRetries       int          `db:"max_retries"`        // Max health check retries
	RepairRetryCount int          `db:"repair_retry_count"` // Repair retry count
	MaxRepairRetries int          `db:"max_repair_retries"` // Max repair retries
	NextRetryAt      *time.Time   `db:"next_retry_at"`
	SourceNzbPath    *string      `db:"source_nzb_path"`
	ErrorDetails     *string      `db:"error_details"` // JSON error details
	CreatedAt        time.Time    `db:"created_at"`
	UpdatedAt        time.Time    `db:"updated_at"`
}

// User represents a user account in the system
type User struct {
	ID           int64      `db:"id"`
	UserID       string     `db:"user_id"`       // Unique identifier from auth provider
	Email        *string    `db:"email"`         // User email address (nullable)
	Name         *string    `db:"name"`          // User display name (nullable)
	AvatarURL    *string    `db:"avatar_url"`    // User avatar image URL (nullable)
	Provider     string     `db:"provider"`      // Auth provider (direct, github, google, dev, etc.)
	ProviderID   *string    `db:"provider_id"`   // Provider-specific user ID (nullable)
	PasswordHash *string    `db:"password_hash"` // Bcrypt password hash for direct auth (nullable)
	APIKey       *string    `db:"api_key"`       // API key for user authentication (nullable)
	IsAdmin      bool       `db:"is_admin"`      // Admin privileges flag
	CreatedAt    time.Time  `db:"created_at"`    // Account creation timestamp
	UpdatedAt    time.Time  `db:"updated_at"`    // Last profile update timestamp
	LastLogin    *time.Time `db:"last_login"`    // Last login timestamp (nullable)
}

// MediaFile represents a media file tracked by scrapers
type MediaFile struct {
	ID           int64     `db:"id"`
	InstanceName string    `db:"instance_name"` // Name from configuration
	InstanceType string    `db:"instance_type"` // "radarr" or "sonarr"
	ExternalID   int64     `db:"external_id"`   // Movie ID or Episode ID from API
	FileID       int64     `db:"file_id"`       // Movie File ID or Episode File ID from API (nullable)
	FilePath     string    `db:"file_path"`     // Full file path
	FileSize     *int64    `db:"file_size"`     // File size in bytes (nullable)
	CreatedAt    time.Time `db:"created_at"`    // When record was created
	UpdatedAt    time.Time `db:"updated_at"`    // When record was last updated
}
