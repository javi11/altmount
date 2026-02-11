package nzbfilesystem

import (
	"path/filepath"
	"strings"
	"time"
)

// ActiveStream represents a file currently being streamed
type ActiveStream struct {
	ID               string    `json:"id"`
	FilePath         string    `json:"file_path"`
	StartedAt        time.Time `json:"started_at"`
	LastActivity     time.Time `json:"last_activity"`
	Source           string    `json:"source"`
	UserName         string    `json:"user_name,omitempty"`
	ClientIP         string    `json:"client_ip,omitempty"`
	UserAgent        string    `json:"user_agent,omitempty"`
	TotalSize        int64     `json:"total_size"`
	BytesSent        int64     `json:"bytes_sent"`
	BytesDownloaded  int64     `json:"bytes_downloaded"`
	CurrentOffset    int64     `json:"current_offset"`
	BytesPerSecond   int64     `json:"bytes_per_second"`
	DownloadSpeed    int64     `json:"download_speed"`
	SpeedAvg         int64     `json:"speed_avg"`
	ETA              int64     `json:"eta"` // Seconds remaining
	TotalConnections int       `json:"total_connections"`
	BufferedOffset   int64     `json:"buffered_offset"`
	Status           string    `json:"status"` // e.g., "Buffering", "Streaming", "Stalled"
}

// StreamTracker interface for tracking active streams
type StreamTracker interface {
	Add(filePath, source, userName, clientIP, userAgent string, totalSize int64) string
	UpdateProgress(id string, bytesRead int64)
	UpdateDownloadProgress(id string, bytesDownloaded int64)
	UpdateBufferedOffset(id string, offset int64)
	Remove(id string)
	IncArticlesDownloaded()
	IncArticlesPosted()
}

// normalizePath normalizes file paths for consistent database lookups
// Removes trailing slashes except for root path "/"
func normalizePath(path string) string {
	// Handle empty path
	if path == "" {
		return RootPath
	}

	// Handle root path - keep as is
	if path == RootPath {
		return path
	}

	// Replace backslashes with forward slashes first
	path = strings.ReplaceAll(path, "\\", "/")

	// Clean the path using filepath.Clean
	cleaned := filepath.Clean(path)

	// Remove trailing slashes and backslashes
	cleaned = strings.TrimRight(cleaned, "/\\")

	// Ensure we don't return empty string after trimming (e.g. if path was just slashes)
	if cleaned == "" || cleaned == "." {
		return RootPath
	}

	return cleaned
}
