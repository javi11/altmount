package database

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// SegmentData represents segment information
type SegmentData struct {
	Bytes int64  `json:"bytes"`
	ID    string `json:"id"`
}

// Scan implements the sql.Scanner interface
func (sd *SegmentData) Scan(value interface{}) error {
	if value == nil {
		*sd = SegmentData{}
		return nil
	}

	var bytes []byte
	switch v := value.(type) {
	case []byte:
		bytes = v
	case string:
		bytes = []byte(v)
	default:
		return errors.New("cannot scan non-string value into SegmentData")
	}

	return json.Unmarshal(bytes, sd)
}

// Value implements the driver.Valuer interface
func (sd SegmentData) Value() (driver.Value, error) {
	if sd.Bytes == 0 && sd.ID == "" {
		return nil, nil
	}
	return json.Marshal(sd)
}

// RarPart represents a single RAR part with its segments
type RarPart struct {
	SegmentData SegmentData `json:"segment_data"`
	PartSize    int64       `json:"part_size"`
	Offset      int64       `json:"offset"`
	ByteCount   int64       `json:"bytecount"`
}

// RarParts is a slice of RarPart for JSON marshaling
type RarParts []RarPart

// Scan implements the sql.Scanner interface
func (rp *RarParts) Scan(value interface{}) error {
	if value == nil {
		*rp = nil
		return nil
	}

	var bytes []byte
	switch v := value.(type) {
	case []byte:
		bytes = v
	case string:
		bytes = []byte(v)
	default:
		return errors.New("cannot scan non-string value into RarParts")
	}

	return json.Unmarshal(bytes, rp)
}

// Value implements the driver.Valuer interface
func (rp RarParts) Value() (driver.Value, error) {
	if len(rp) == 0 {
		return nil, nil
	}
	return json.Marshal(rp)
}


// FileStatus represents the status of a file's availability
type FileStatus string

const (
	FileStatusHealthy   FileStatus = "healthy"   // All articles found and downloadable
	FileStatusPartial   FileStatus = "partial"   // Some articles missing but some content available
	FileStatusCorrupted FileStatus = "corrupted" // No articles found or completely unreadable
)

// VirtualFile represents a virtual file in the filesystem
type VirtualFile struct {
	ID          int64      `db:"id"`
	ParentID    *int64     `db:"parent_id"`
	Name        string     `db:"name"`
	Size        int64      `db:"size"`
	CreatedAt   time.Time  `db:"created_at"`
	IsDirectory bool       `db:"is_directory"`
	Status      FileStatus `db:"status"`
}

// NzbFile represents an NZB file that references a virtual file
type NzbFile struct {
	ID           int64        `db:"id"`
	Name         string       `db:"name"`
	CreatedAt    time.Time    `db:"created_at"`
	UpdatedAt    time.Time    `db:"updated_at"`
	SegmentsData *SegmentData `db:"segments_data"`
	Password     *string      `db:"password"`
	Encryption   *string      `db:"encryption"`
	Salt         *string      `db:"salt"`
}

// NzbRarFile represents an NZB RAR file that references a virtual file
type NzbRarFile struct {
	ID        int64     `db:"id"`
	Name      string    `db:"name"`
	CreatedAt time.Time `db:"created_at"`
	UpdatedAt time.Time `db:"updated_at"`
	RarParts  RarParts  `db:"rar_parts"`
}

// Par2File represents a PAR2 repair file that references a virtual file
type Par2File struct {
	ID           int64       `db:"id"`
	Name         string      `db:"name"`
	SegmentsData SegmentData `db:"segments_data"`
	CreatedAt    time.Time   `db:"created_at"`
}

// NzbSegment represents a segment from the old schema for conversion
type NzbSegment struct {
	Number    int      `json:"number"`
	Bytes     int64    `json:"bytes"`
	MessageID string   `json:"message_id"`
	Groups    []string `json:"groups"`
}

// NzbSegments is a slice of NzbSegment for easier handling
type NzbSegments []NzbSegment

// ConvertToSegmentsData converts old NzbSegments to new SegmentsData JSON format
func (segments NzbSegments) ConvertToSegmentsData() SegmentData {
	if len(segments) == 0 {
		return SegmentData{}
	}
	
	// Calculate total bytes and collect all message IDs
	var totalBytes int64
	var messageIDs []string
	
	for _, seg := range segments {
		totalBytes += seg.Bytes
		messageIDs = append(messageIDs, seg.MessageID)
	}
	
	// Create segments data with total bytes and comma-separated message IDs
	return SegmentData{
		Bytes: totalBytes,
		ID:    strings.Join(messageIDs, ","),
	}
}

// ConvertToRarParts converts RAR file segments to RarParts JSON format
func ConvertToRarParts(rarFiles []ParsedRarFile) RarParts {
	var rarParts RarParts
	
	var cumulativeOffset int64
	for _, rarFile := range rarFiles {
		// Convert segments to SegmentData
		segmentData := rarFile.Segments.ConvertToSegmentsData()
		
		rarPart := RarPart{
			SegmentData: segmentData,
			PartSize:    rarFile.Size,
			Offset:      cumulativeOffset,
			ByteCount:   segmentData.Bytes,
		}
		
		rarParts = append(rarParts, rarPart)
		cumulativeOffset += rarFile.Size
	}
	
	return rarParts
}

// ParsedRarFile represents a single RAR file for conversion
type ParsedRarFile struct {
	Filename string
	Size     int64
	Segments NzbSegments
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
	ID                  int64     `db:"id"`
	TotalQueued         int       `db:"total_queued"`
	TotalProcessing     int       `db:"total_processing"`
	TotalCompleted      int       `db:"total_completed"`
	TotalFailed         int       `db:"total_failed"`
	AvgProcessingTimeMs *int      `db:"avg_processing_time_ms"`
	LastUpdated         time.Time `db:"last_updated"`
}
