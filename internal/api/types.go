package api

import (
	"strings"
	"time"

	"github.com/javi11/altmount/internal/database"
)

// Common API response structures

// APIResponse represents a standard API response wrapper
type APIResponse struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Error   *APIError   `json:"error,omitempty"`
	Meta    *APIMeta    `json:"meta,omitempty"`
}

// APIError represents an error response
type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details string `json:"details,omitempty"`
}

// APIMeta represents metadata for paginated responses
type APIMeta struct {
	Total  int `json:"total"`
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
	Count  int `json:"count"`
}

// Pagination represents pagination parameters
type Pagination struct {
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
}

// DefaultPagination returns default pagination settings
func DefaultPagination() Pagination {
	return Pagination{
		Limit:  50,
		Offset: 0,
	}
}

// Queue API Types

// QueueItemResponse represents a queue item in API responses
type QueueItemResponse struct {
	ID           int64                  `json:"id"`
	NzbPath      string                 `json:"nzb_path"`
	TargetPath   string                 `json:"target_path"`
	Priority     database.QueuePriority `json:"priority"`
	Status       database.QueueStatus   `json:"status"`
	CreatedAt    time.Time              `json:"created_at"`
	UpdatedAt    time.Time              `json:"updated_at"`
	StartedAt    *time.Time             `json:"started_at"`
	CompletedAt  *time.Time             `json:"completed_at"`
	RetryCount   int                    `json:"retry_count"`
	MaxRetries   int                    `json:"max_retries"`
	ErrorMessage *string                `json:"error_message"`
	BatchID      *string                `json:"batch_id"`
	Metadata     *string                `json:"metadata"`
}

// QueueListRequest represents request parameters for listing queue items
type QueueListRequest struct {
	Status *database.QueueStatus `json:"status"`
	Since  *time.Time            `json:"since"`
	Pagination
}

// QueueStatsResponse represents queue statistics in API responses
type QueueStatsResponse struct {
	TotalQueued         int       `json:"total_queued"`
	TotalProcessing     int       `json:"total_processing"`
	TotalCompleted      int       `json:"total_completed"`
	TotalFailed         int       `json:"total_failed"`
	AvgProcessingTimeMs *int      `json:"avg_processing_time_ms"`
	LastUpdated         time.Time `json:"last_updated"`
}

// QueueRetryRequest represents request to retry a queue item
type QueueRetryRequest struct {
	ResetRetryCount bool `json:"reset_retry_count,omitempty"`
}

// Health API Types

// HealthItemResponse represents a health record in API responses
type HealthItemResponse struct {
	ID            int64                 `json:"id"`
	FilePath      string                `json:"file_path"`
	Status        database.HealthStatus `json:"status"`
	LastChecked   time.Time             `json:"last_checked"`
	LastError     *string               `json:"last_error"`
	RetryCount    int                   `json:"retry_count"`
	MaxRetries    int                   `json:"max_retries"`
	NextRetryAt   *time.Time            `json:"next_retry_at"`
	SourceNzbPath *string               `json:"source_nzb_path"`
	ErrorDetails  *string               `json:"error_details"`
	CreatedAt     time.Time             `json:"created_at"`
	UpdatedAt     time.Time             `json:"updated_at"`
}

// HealthListRequest represents request parameters for listing health records
type HealthListRequest struct {
	Status *database.HealthStatus `json:"status"`
	Since  *time.Time             `json:"since"`
	Pagination
}

// HealthStatsResponse represents health statistics in API responses
type HealthStatsResponse struct {
	Pending   int `json:"pending"`
	Healthy   int `json:"healthy"`
	Partial   int `json:"partial"`
	Corrupted int `json:"corrupted"`
	Total     int `json:"total"`
}

// HealthRetryRequest represents request to retry a corrupted file
type HealthRetryRequest struct {
	ResetRetryCount bool `json:"reset_retry_count,omitempty"`
}

// HealthCleanupRequest represents request to cleanup health records
type HealthCleanupRequest struct {
	OlderThan *time.Time             `json:"older_than"`
	Status    *database.HealthStatus `json:"status"`
}

// HealthCheckRequest represents request to add file for manual health checking
type HealthCheckRequest struct {
	FilePath   string  `json:"file_path"`
	MaxRetries *int    `json:"max_retries,omitempty"`
	SourceNzb  *string `json:"source_nzb_path,omitempty"`
	Priority   bool    `json:"priority,omitempty"`
}

// HealthWorkerStatusResponse represents the current status of the health worker
type HealthWorkerStatusResponse struct {
	Status                 string     `json:"status"`
	LastRunTime            *time.Time `json:"last_run_time,omitempty"`
	NextRunTime            *time.Time `json:"next_run_time,omitempty"`
	TotalRunsCompleted     int64      `json:"total_runs_completed"`
	TotalFilesChecked      int64      `json:"total_files_checked"`
	TotalFilesRecovered    int64      `json:"total_files_recovered"`
	TotalFilesCorrupted    int64      `json:"total_files_corrupted"`
	CurrentRunStartTime    *time.Time `json:"current_run_start_time,omitempty"`
	CurrentRunFilesChecked int        `json:"current_run_files_checked"`
	PendingManualChecks    int        `json:"pending_manual_checks"`
	LastError              *string    `json:"last_error,omitempty"`
	ErrorCount             int64      `json:"error_count"`
}

// System API Types

// SystemStatsResponse represents combined system statistics
type SystemStatsResponse struct {
	Queue  QueueStatsResponse  `json:"queue"`
	Health HealthStatsResponse `json:"health"`
	System SystemInfoResponse  `json:"system"`
}

// SystemInfoResponse represents system information
type SystemInfoResponse struct {
	Version   string    `json:"version,omitempty"`
	StartTime time.Time `json:"start_time"`
	Uptime    string    `json:"uptime"`
	GoVersion string    `json:"go_version,omitempty"`
}

// SystemHealthResponse represents system health check result
type SystemHealthResponse struct {
	Status     string                     `json:"status"` // "healthy", "degraded", "unhealthy"
	Timestamp  time.Time                  `json:"timestamp"`
	Components map[string]ComponentHealth `json:"components"`
}

// ComponentHealth represents health of a system component
type ComponentHealth struct {
	Status  string `json:"status"` // "healthy", "degraded", "unhealthy"
	Message string `json:"message,omitempty"`
	Details string `json:"details,omitempty"`
}

// SystemCleanupRequest represents request for system cleanup
type SystemCleanupRequest struct {
	QueueOlderThan  *time.Time `json:"queue_older_than"`
	HealthOlderThan *time.Time `json:"health_older_than"`
	DryRun          bool       `json:"dry_run,omitempty"`
}

// SystemCleanupResponse represents cleanup operation results
type SystemCleanupResponse struct {
	QueueItemsRemoved    int  `json:"queue_items_removed"`
	HealthRecordsRemoved int  `json:"health_records_removed"`
	DryRun               bool `json:"dry_run"`
}

// SystemRestartRequest represents request for system restart
type SystemRestartRequest struct {
	Force bool `json:"force,omitempty"` // Force restart even if unsafe
}

// SystemRestartResponse represents restart operation result
type SystemRestartResponse struct {
	Message   string    `json:"message"`
	Timestamp time.Time `json:"timestamp"`
}

// Configuration API Types

// ConfigResponse represents the configuration in API responses
type ConfigResponse struct {
	WebDAV    WebDAVConfigResponse     `json:"webdav"`
	API       APIConfigResponse        `json:"api"`
	Database  DatabaseConfigResponse   `json:"database"`
	Metadata  MetadataConfigResponse   `json:"metadata"`
	Streaming StreamingConfigResponse  `json:"streaming"`
	RClone    RCloneConfigResponse     `json:"rclone"`
	Import    ImportConfigResponse     `json:"import"`
	SABnzbd   SABnzbdConfigData        `json:"sabnzbd"`
	Providers []ProviderConfigResponse `json:"providers"`
	LogLevel  string                   `json:"log_level"`
}

// WebDAVConfigResponse represents WebDAV server configuration in API responses
type WebDAVConfigResponse struct {
	Port     int    `json:"port"`
	User     string `json:"user"`
	Password string `json:"password"`
}

// APIConfigResponse represents REST API configuration in API responses
type APIConfigResponse struct {
	Prefix string `json:"prefix"`
}

// DatabaseConfigResponse represents database configuration in API responses
type DatabaseConfigResponse struct {
	Path string `json:"path"`
}

// MetadataConfigResponse represents metadata configuration in API responses
type MetadataConfigResponse struct {
	RootPath string `json:"root_path"`
}

// StreamingConfigResponse represents streaming configuration in API responses
type StreamingConfigResponse struct {
	MaxRangeSize       int64 `json:"max_range_size"`
	StreamingChunkSize int64 `json:"streaming_chunk_size"`
	MaxDownloadWorkers int   `json:"max_download_workers"`
}

// RCloneConfigResponse represents rclone configuration in API responses (sanitized)
type RCloneConfigResponse struct {
	PasswordSet bool `json:"password_set"`
	SaltSet     bool `json:"salt_set"`
}

// ImportConfigResponse represents import configuration in API responses
type ImportConfigResponse struct {
	MaxProcessorWorkers int `json:"max_processor_workers"`
}

// SABnzbdConfigData represents SABnzbd configuration in API responses
type SABnzbdConfigData struct {
	Enabled    bool                  `json:"enabled"`
	MountDir   string                `json:"mount_dir"`
	Categories []SABnzbdCategoryData `json:"categories"`
}

// SABnzbdCategoryData represents a SABnzbd category in API responses
type SABnzbdCategoryData struct {
	Name     string `json:"name"`
	Order    int    `json:"order"`
	Priority int    `json:"priority"`
	Dir      string `json:"dir"`
}

// ProviderConfigResponse represents a single NNTP provider configuration in API responses (sanitized)
type ProviderConfigResponse struct {
	ID             string `json:"id"`
	Host           string `json:"host"`
	Port           int    `json:"port"`
	Username       string `json:"username"`
	MaxConnections int    `json:"max_connections"`
	TLS            bool   `json:"tls"`
	InsecureTLS    bool   `json:"insecure_tls"`
	PasswordSet    bool   `json:"password_set"`
	Enabled        bool   `json:"enabled"`
}

// ConfigUpdateRequest represents a request to update configuration
type ConfigUpdateRequest struct {
	WebDAV    *WebDAVConfigRequest     `json:"webdav,omitempty"`
	API       *APIConfigRequest        `json:"api,omitempty"`
	Database  *DatabaseConfigRequest   `json:"database,omitempty"`
	Metadata  *MetadataConfigRequest   `json:"metadata,omitempty"`
	Streaming *StreamingConfigRequest  `json:"streaming,omitempty"`
	RClone    *RCloneConfigRequest     `json:"rclone,omitempty"`
	Import    *ImportConfigRequest     `json:"import,omitempty"`
	SABnzbd   *SABnzbdConfigUpdate     `json:"sabnzbd,omitempty"`
	Providers *[]ProviderConfigRequest `json:"providers,omitempty"`
	LogLevel  *string                  `json:"log_level,omitempty"`
}

// WebDAVConfigRequest represents WebDAV server configuration in update requests
type WebDAVConfigRequest struct {
	Port     *int    `json:"port,omitempty"`
	User     *string `json:"user,omitempty"`
	Password *string `json:"password,omitempty"`
	Debug    *bool   `json:"debug,omitempty"`
}

// APIConfigRequest represents REST API configuration in update requests
type APIConfigRequest struct {
	Prefix *string `json:"prefix,omitempty"`
}

// DatabaseConfigRequest represents database configuration in update requests
type DatabaseConfigRequest struct {
	Path *string `json:"path,omitempty"`
}

// MetadataConfigRequest represents metadata configuration in update requests
type MetadataConfigRequest struct {
	RootPath *string `json:"root_path,omitempty"`
}

// StreamingConfigRequest represents streaming configuration in update requests
type StreamingConfigRequest struct {
	MaxRangeSize       *int64 `json:"max_range_size,omitempty"`
	StreamingChunkSize *int64 `json:"streaming_chunk_size,omitempty"`
	MaxDownloadWorkers *int   `json:"max_download_workers,omitempty"`
}

// RCloneConfigRequest represents rclone configuration in update requests
type RCloneConfigRequest struct {
	Password *string `json:"password,omitempty"`
	Salt     *string `json:"salt,omitempty"`
}

// ImportConfigRequest represents import configuration in update requests
type ImportConfigRequest struct {
	MaxProcessorWorkers *int `json:"max_processor_workers,omitempty"`
}

// SABnzbdConfigUpdate represents SABnzbd configuration in update requests
type SABnzbdConfigUpdate struct {
	Enabled    *bool                    `json:"enabled,omitempty"`
	MountDir   *string                  `json:"complete_dir,omitempty"`
	Categories *[]SABnzbdCategoryUpdate `json:"categories,omitempty"`
}

// SABnzbdCategoryUpdate represents a SABnzbd category in update requests
type SABnzbdCategoryUpdate struct {
	Name     *string `json:"name,omitempty"`
	Order    *int    `json:"order,omitempty"`
	Priority *int    `json:"priority,omitempty"`
	Dir      *string `json:"dir,omitempty"`
}

// ProviderConfigRequest represents a single NNTP provider configuration in update requests
type ProviderConfigRequest struct {
	ID             *string `json:"id,omitempty"`
	Host           *string `json:"host,omitempty"`
	Port           *int    `json:"port,omitempty"`
	Username       *string `json:"username,omitempty"`
	Password       *string `json:"password,omitempty"`
	MaxConnections *int    `json:"max_connections,omitempty"`
	TLS            *bool   `json:"tls,omitempty"`
	InsecureTLS    *bool   `json:"insecure_tls,omitempty"`
	Enabled        *bool   `json:"enabled,omitempty"`
}

// ConfigValidateRequest represents a request to validate configuration
type ConfigValidateRequest struct {
	Config interface{} `json:"config"`
}

// ConfigValidateResponse represents the result of configuration validation
type ConfigValidateResponse struct {
	Valid  bool                    `json:"valid"`
	Errors []ConfigValidationError `json:"errors,omitempty"`
}

// ConfigValidationError represents a configuration validation error
type ConfigValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// Converter functions

// ToQueueItemResponse converts database.ImportQueueItem to QueueItemResponse
func ToQueueItemResponse(item *database.ImportQueueItem) *QueueItemResponse {
	if item == nil {
		return nil
	}

	// Generate target_path by removing .nzb extension
	targetPath := item.NzbPath
	if strings.HasSuffix(strings.ToLower(targetPath), ".nzb") {
		targetPath = targetPath[:len(targetPath)-4]
	}

	return &QueueItemResponse{
		ID:           item.ID,
		NzbPath:      item.NzbPath,
		TargetPath:   targetPath,
		Priority:     item.Priority,
		Status:       item.Status,
		CreatedAt:    item.CreatedAt,
		UpdatedAt:    item.UpdatedAt,
		StartedAt:    item.StartedAt,
		CompletedAt:  item.CompletedAt,
		RetryCount:   item.RetryCount,
		MaxRetries:   item.MaxRetries,
		ErrorMessage: item.ErrorMessage,
		BatchID:      item.BatchID,
		Metadata:     item.Metadata,
	}
}

// ToQueueStatsResponse converts database.QueueStats to QueueStatsResponse
func ToQueueStatsResponse(stats *database.QueueStats) *QueueStatsResponse {
	if stats == nil {
		return nil
	}
	return &QueueStatsResponse{
		TotalQueued:         stats.TotalQueued,
		TotalProcessing:     stats.TotalProcessing,
		TotalCompleted:      stats.TotalCompleted,
		TotalFailed:         stats.TotalFailed,
		AvgProcessingTimeMs: stats.AvgProcessingTimeMs,
		LastUpdated:         stats.LastUpdated,
	}
}

// ToHealthItemResponse converts database.FileHealth to HealthItemResponse
func ToHealthItemResponse(item *database.FileHealth) *HealthItemResponse {
	if item == nil {
		return nil
	}
	return &HealthItemResponse{
		ID:            item.ID,
		FilePath:      item.FilePath,
		Status:        item.Status,
		LastChecked:   item.LastChecked,
		LastError:     item.LastError,
		RetryCount:    item.RetryCount,
		MaxRetries:    item.MaxRetries,
		NextRetryAt:   item.NextRetryAt,
		SourceNzbPath: item.SourceNzbPath,
		ErrorDetails:  item.ErrorDetails,
		CreatedAt:     item.CreatedAt,
		UpdatedAt:     item.UpdatedAt,
	}
}

// ToHealthStatsResponse converts health stats map to HealthStatsResponse
func ToHealthStatsResponse(stats map[database.HealthStatus]int) *HealthStatsResponse {
	response := &HealthStatsResponse{
		Healthy:   stats[database.HealthStatusHealthy],
		Partial:   stats[database.HealthStatusPartial],
		Corrupted: stats[database.HealthStatusCorrupted],
	}
	response.Total = response.Healthy + response.Partial + response.Corrupted
	return response
}

// File Metadata API Types

// FileMetadataResponse represents file metadata information in API responses
type FileMetadataResponse struct {
	FileSize          int64                 `json:"file_size"`
	SourceNzbPath     string                `json:"source_nzb_path"`
	Status            string                `json:"status"`
	SegmentCount      int                   `json:"segment_count"`
	AvailableSegments *int                  `json:"available_segments"`
	Encryption        string                `json:"encryption"`
	CreatedAt         string                `json:"created_at"`
	ModifiedAt        string                `json:"modified_at"`
	PasswordProtected bool                  `json:"password_protected"`
	Segments          []SegmentInfoResponse `json:"segments"`
}

// SegmentInfoResponse represents segment information in API responses
type SegmentInfoResponse struct {
	SegmentSize int64  `json:"segment_size"`
	StartOffset int64  `json:"start_offset"`
	EndOffset   int64  `json:"end_offset"`
	MessageID   string `json:"message_id"`
	Available   bool   `json:"available"`
}

// Provider Management API Types

// ProviderTestRequest represents a request to test provider connectivity
type ProviderTestRequest struct {
	Host        string `json:"host"`
	Port        int    `json:"port"`
	Username    string `json:"username"`
	Password    string `json:"password"`
	TLS         bool   `json:"tls"`
	InsecureTLS bool   `json:"insecure_tls"`
}

// ProviderTestResponse represents the result of testing provider connectivity
type ProviderTestResponse struct {
	Success      bool   `json:"success"`
	ErrorMessage string `json:"error_message,omitempty"`
	Latency      int64  `json:"latency_ms,omitempty"`
}

// ProviderCreateRequest represents a request to create a new provider
type ProviderCreateRequest struct {
	Host           string `json:"host"`
	Port           int    `json:"port"`
	Username       string `json:"username"`
	Password       string `json:"password"`
	MaxConnections int    `json:"max_connections"`
	TLS            bool   `json:"tls"`
	InsecureTLS    bool   `json:"insecure_tls"`
	Enabled        bool   `json:"enabled"`
}

// ProviderUpdateRequest represents a request to update an existing provider
type ProviderUpdateRequest struct {
	Host           *string `json:"host,omitempty"`
	Port           *int    `json:"port,omitempty"`
	Username       *string `json:"username,omitempty"`
	Password       *string `json:"password,omitempty"`
	MaxConnections *int    `json:"max_connections,omitempty"`
	TLS            *bool   `json:"tls,omitempty"`
	InsecureTLS    *bool   `json:"insecure_tls,omitempty"`
	Enabled        *bool   `json:"enabled,omitempty"`
}

// ProviderReorderRequest represents a request to reorder providers
type ProviderReorderRequest struct {
	ProviderIDs []string `json:"provider_ids"`
}

// Import API Types

// ManualScanRequest represents a request to start a manual directory scan
type ManualScanRequest struct {
	Path string `json:"path"`
}

// ScanStatusResponse represents the current status of a manual scan operation
type ScanStatusResponse struct {
	Status      string     `json:"status"`
	Path        string     `json:"path,omitempty"`
	StartTime   *time.Time `json:"start_time,omitempty"`
	FilesFound  int        `json:"files_found"`
	FilesAdded  int        `json:"files_added"`
	CurrentFile string     `json:"current_file,omitempty"`
	LastError   *string    `json:"last_error,omitempty"`
}

// ManualImportRequest represents a request to manually import a file by path
type ManualImportRequest struct {
	FilePath string `json:"file_path"`
}

// ManualImportResponse represents the response from manually importing a file
type ManualImportResponse struct {
	QueueID int64  `json:"queue_id"`
	Message string `json:"message"`
}

// PoolMetricsResponse represents NNTP pool metrics in API responses
type PoolMetricsResponse struct {
	ActiveConnections    int       `json:"active_connections"`
	TotalBytesDownloaded int64     `json:"total_bytes_downloaded"`
	DownloadSpeed        float64   `json:"download_speed_bytes_per_sec"`
	ErrorRate            float64   `json:"error_rate_percent"`
	CurrentMemoryUsage   int64     `json:"current_memory_usage"`
	TotalConnections     int64     `json:"total_connections"`
	CommandSuccessRate   float64   `json:"command_success_rate_percent"`
	AcquireWaitTimeMs    int64     `json:"acquire_wait_time_ms"`
	LastUpdated          time.Time `json:"last_updated"`
}
