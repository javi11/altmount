// Package scanner provides directory scanning and NZBDav import functionality.
package scanner

import "time"

// ScanStatus represents the current status of a manual scan
type ScanStatus string

const (
	ScanStatusIdle      ScanStatus = "idle"
	ScanStatusScanning  ScanStatus = "scanning"
	ScanStatusCanceling ScanStatus = "canceling"
)

// ScanInfo holds information about the current scan operation
type ScanInfo struct {
	Status      ScanStatus `json:"status"`
	Path        string     `json:"path,omitempty"`
	StartTime   *time.Time `json:"start_time,omitempty"`
	FilesFound  int        `json:"files_found"`
	FilesAdded  int        `json:"files_added"`
	CurrentFile string     `json:"current_file,omitempty"`
	LastError   *string    `json:"last_error,omitempty"`
}

// ImportJobStatus represents the status of an NZBDav import job
type ImportJobStatus string

const (
	ImportStatusIdle      ImportJobStatus = "idle"
	ImportStatusRunning   ImportJobStatus = "running"
	ImportStatusCanceling ImportJobStatus = "canceling"
	ImportStatusCompleted ImportJobStatus = "completed"
)

// ImportInfo holds information about the current NZBDav import operation
type ImportInfo struct {
	Status    ImportJobStatus `json:"status"`
	Total     int             `json:"total"`
	Added     int             `json:"added"`
	Failed    int             `json:"failed"`
	Skipped   int             `json:"skipped"`
	LastError *string         `json:"last_error,omitempty"`
}
