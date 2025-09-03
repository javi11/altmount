package api

import (
	"strings"
	"time"

	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/altmount/internal/database"
)

// API Response Wrappers for sensitive data masking

// ConfigAPIResponse wraps config.Config with sensitive data handling
type ConfigAPIResponse struct {
	*config.Config
	RClone    RCloneAPIResponse    `json:"rclone"`
	Providers []ProviderAPIResponse `json:"providers"`
}

// RCloneAPIResponse sanitizes RClone config for API responses
type RCloneAPIResponse struct {
	PasswordSet bool   `json:"password_set"`
	SaltSet     bool   `json:"salt_set"`
	VFSEnabled  bool   `json:"vfs_enabled"`
	VFSURL      string `json:"vfs_url"`
	VFSUser     string `json:"vfs_user"`
	VFSPassSet  bool   `json:"vfs_pass_set"`
}

// ProviderAPIResponse sanitizes Provider config for API responses
type ProviderAPIResponse struct {
	ID               string `json:"id"`
	Host             string `json:"host"`
	Port             int    `json:"port"`
	Username         string `json:"username"`
	MaxConnections   int    `json:"max_connections"`
	TLS              bool   `json:"tls"`
	InsecureTLS      bool   `json:"insecure_tls"`
	PasswordSet      bool   `json:"password_set"`
	Enabled          bool   `json:"enabled"`
	IsBackupProvider bool   `json:"is_backup_provider"`
}

// ImportAPIResponse handles duration serialization for Import config
type ImportAPIResponse struct {
	MaxProcessorWorkers     int `json:"max_processor_workers"`
	QueueProcessingInterval int `json:"queue_processing_interval"` // Interval in seconds
}

// Helper functions to create API responses from core config types

// ToConfigAPIResponse converts config.Config to ConfigAPIResponse with sensitive data masked
func ToConfigAPIResponse(cfg *config.Config) *ConfigAPIResponse {
	if cfg == nil {
		return nil
	}

	// Convert providers with password masking
	providers := make([]ProviderAPIResponse, len(cfg.Providers))
	for i, p := range cfg.Providers {
		providers[i] = ProviderAPIResponse{
			ID:               p.ID,
			Host:             p.Host,
			Port:             p.Port,
			Username:         p.Username,
			MaxConnections:   p.MaxConnections,
			TLS:              p.TLS,
			InsecureTLS:      p.InsecureTLS,
			PasswordSet:      p.Password != "",
			Enabled:          p.Enabled != nil && *p.Enabled,
			IsBackupProvider: p.IsBackupProvider != nil && *p.IsBackupProvider,
		}
	}

	// Create RClone response with password status
	rcloneResp := RCloneAPIResponse{
		PasswordSet: cfg.RClone.Password != "",
		SaltSet:     cfg.RClone.Salt != "",
		VFSEnabled:  cfg.RClone.VFSEnabled != nil && *cfg.RClone.VFSEnabled,
		VFSURL:      cfg.RClone.VFSUrl,
		VFSUser:     cfg.RClone.VFSUser,
		VFSPassSet:  cfg.RClone.VFSPass != "",
	}

	return &ConfigAPIResponse{
		Config:    cfg,
		RClone:    rcloneResp,
		Providers: providers,
	}
}

// ToImportAPIResponse converts config.ImportConfig to ImportAPIResponse with duration as seconds
func ToImportAPIResponse(cfg *config.ImportConfig) ImportAPIResponse {
	return ImportAPIResponse{
		MaxProcessorWorkers:     cfg.MaxProcessorWorkers,
		QueueProcessingInterval: int(cfg.QueueProcessingInterval.Seconds()),
	}
}

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
	Category     *string                `json:"category"`
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
	FileSize     *int64                 `json:"file_size"`
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

// Health API Types

// HealthItemResponse represents a health record in API responses
type HealthItemResponse struct {
	ID               int64                 `json:"id"`
	FilePath         string                `json:"file_path"`
	Status           database.HealthStatus `json:"status"`
	LastChecked      time.Time             `json:"last_checked"`
	LastError        *string               `json:"last_error"`
	RetryCount       int                   `json:"retry_count"`
	MaxRetries       int                   `json:"max_retries"`
	NextRetryAt      *time.Time            `json:"next_retry_at"`
	SourceNzbPath    *string               `json:"source_nzb_path"`
	ErrorDetails     *string               `json:"error_details"`
	RepairRetryCount int                   `json:"repair_retry_count"`
	MaxRepairRetries int                   `json:"max_repair_retries"`
	CreatedAt        time.Time             `json:"created_at"`
	UpdatedAt        time.Time             `json:"updated_at"`
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

// HealthRepairRequest represents request to trigger repair for a corrupted file
type HealthRepairRequest struct {
	ResetRepairRetryCount bool `json:"reset_repair_retry_count,omitempty"`
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

// Configuration API Types - Now using core config types directly with minimal wrappers above

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
		Category:     item.Category,
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
		FileSize:     item.FileSize,
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
		ID:               item.ID,
		FilePath:         item.FilePath,
		Status:           item.Status,
		LastChecked:      item.LastChecked,
		LastError:        item.LastError,
		RetryCount:       item.RetryCount,
		MaxRetries:       item.MaxRetries,
		NextRetryAt:      item.NextRetryAt,
		SourceNzbPath:    item.SourceNzbPath,
		ErrorDetails:     item.ErrorDetails,
		RepairRetryCount: item.RepairRetryCount,
		MaxRepairRetries: item.MaxRepairRetries,
		CreatedAt:        item.CreatedAt,
		UpdatedAt:        item.UpdatedAt,
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
	Host             string `json:"host"`
	Port             int    `json:"port"`
	Username         string `json:"username"`
	Password         string `json:"password"`
	MaxConnections   int    `json:"max_connections"`
	TLS              bool   `json:"tls"`
	InsecureTLS      bool   `json:"insecure_tls"`
	Enabled          bool   `json:"enabled"`
	IsBackupProvider bool   `json:"is_backup_provider"`
}

// ProviderUpdateRequest represents a request to update an existing provider
type ProviderUpdateRequest struct {
	Host             *string `json:"host,omitempty"`
	Port             *int    `json:"port,omitempty"`
	Username         *string `json:"username,omitempty"`
	Password         *string `json:"password,omitempty"`
	MaxConnections   *int    `json:"max_connections,omitempty"`
	TLS              *bool   `json:"tls,omitempty"`
	InsecureTLS      *bool   `json:"insecure_tls,omitempty"`
	Enabled          *bool   `json:"enabled,omitempty"`
	IsBackupProvider *bool   `json:"is_backup_provider,omitempty"`
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
