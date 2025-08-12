package database

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"time"
)

// NzbType represents the type of NZB file
type NzbType string

const (
	NzbTypeSingleFile NzbType = "single_file"
	NzbTypeMultiFile  NzbType = "multi_file"
	NzbTypeRarArchive NzbType = "rar_archive"
)

// NzbSegment represents a single segment within an NZB file
type NzbSegment struct {
	Number    int      `json:"number"`
	Bytes     int64    `json:"bytes"`
	MessageID string   `json:"message_id"`
	Groups    []string `json:"groups"`
}

// NzbSegments is a slice of NzbSegment that implements database scanning
type NzbSegments []NzbSegment

// Scan implements the sql.Scanner interface
func (ns *NzbSegments) Scan(value interface{}) error {
	if value == nil {
		*ns = nil
		return nil
	}

	var bytes []byte
	switch v := value.(type) {
	case []byte:
		bytes = v
	case string:
		bytes = []byte(v)
	default:
		return errors.New("cannot scan non-string value into NzbSegments")
	}

	return json.Unmarshal(bytes, ns)
}

// Value implements the driver.Valuer interface
func (ns NzbSegments) Value() (driver.Value, error) {
	if len(ns) == 0 {
		return nil, nil
	}
	return json.Marshal(ns)
}

// NzbFile represents a complete NZB file entry
type NzbFile struct {
	ID             int64       `db:"id"`
	Path           string      `db:"path"`
	Filename       string      `db:"filename"`
	Size           int64       `db:"size"`
	CreatedAt      time.Time   `db:"created_at"`
	UpdatedAt      time.Time   `db:"updated_at"`
	NzbType        NzbType     `db:"nzb_type"`
	SegmentsCount  int         `db:"segments_count"`
	SegmentsData   NzbSegments `db:"segments_data"`
	SegmentSize    int64       `db:"segment_size"`
	RclonePassword *string     `db:"rclone_password"` // Password from NZB meta, NULL if not encrypted
	RcloneSalt     *string     `db:"rclone_salt"`     // Salt from NZB meta, NULL if not encrypted
}

// VirtualFile represents a virtual file extracted from NZB contents
type VirtualFile struct {
	ID          int64     `db:"id"`
	NzbFileID   *int64    `db:"nzb_file_id"` // Pointer to allow NULL for system directories
	ParentID    *int64    `db:"parent_id"`   // References another VirtualFile ID for directories
	VirtualPath string    `db:"virtual_path"`
	Filename    string    `db:"filename"`
	Size        int64     `db:"size"`
	CreatedAt   time.Time `db:"created_at"`
	IsDirectory bool      `db:"is_directory"`
	Encryption  *string   `db:"encryption"`  // Encryption type (e.g., "rclone"), NULL if not encrypted
}

// RarContent represents a file contained within a RAR archive
type RarContent struct {
	ID             int64     `db:"id"`
	VirtualFileID  int64     `db:"virtual_file_id"`
	InternalPath   string    `db:"internal_path"`
	Filename       string    `db:"filename"`
	Size           int64     `db:"size"`
	CompressedSize int64     `db:"compressed_size"`
	CRC32          *string   `db:"crc32"`
	CreatedAt      time.Time `db:"created_at"`
}

// FileMetadata represents additional metadata for virtual files
type FileMetadata struct {
	ID            int64     `db:"id"`
	VirtualFileID int64     `db:"virtual_file_id"`
	Key           string    `db:"key"`
	Value         string    `db:"value"`
	CreatedAt     time.Time `db:"created_at"`
}

// Par2File represents a PAR2 repair file associated with an NZB file
type Par2File struct {
	ID           int64       `db:"id"`
	NzbFileID    int64       `db:"nzb_file_id"`
	Filename     string      `db:"filename"`
	Size         int64       `db:"size"`
	SegmentsData NzbSegments `db:"segments_data"`
	CreatedAt    time.Time   `db:"created_at"`
}

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
	ID                   int64      `db:"id"`
	TotalQueued          int        `db:"total_queued"`
	TotalProcessing      int        `db:"total_processing"`
	TotalCompleted       int        `db:"total_completed"`
	TotalFailed          int        `db:"total_failed"`
	AvgProcessingTimeMs  *int       `db:"avg_processing_time_ms"`
	LastUpdated          time.Time  `db:"last_updated"`
}
