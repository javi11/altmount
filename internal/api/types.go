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
	Import    ImportAPIResponse     `json:"import"`
	RClone    RCloneAPIResponse     `json:"rclone"`
	SABnzbd   SABnzbdAPIResponse    `json:"sabnzbd"`
	Providers []ProviderAPIResponse `json:"providers"`
	APIKey    string                `json:"api_key,omitempty"` // User's API key for authentication
}

// RCloneAPIResponse sanitizes RClone config for API responses
type RCloneAPIResponse struct {
	// Encryption
	PasswordSet bool `json:"password_set"`
	SaltSet     bool `json:"salt_set"`

		// RC (Remote Control) Configuration

		RCEnabled    bool              `json:"rc_enabled"`

		RCUrl        string            `json:"rc_url"`

		VFSName      string            `json:"vfs_name"`

		RCPort       int               `json:"rc_port"`

	
	RCUser    string            `json:"rc_user"`
	RCPassSet bool              `json:"rc_pass_set"`
	RCOptions map[string]string `json:"rc_options"`

	// Mount Configuration
	MountEnabled bool              `json:"mount_enabled"`
	MountOptions map[string]string `json:"mount_options"`

	// Mount-Specific Settings
	AllowOther    bool   `json:"allow_other"`
	AllowNonEmpty bool   `json:"allow_non_empty"`
	ReadOnly      bool   `json:"read_only"`
	Timeout       string `json:"timeout"`
	Syslog        bool   `json:"syslog"`

	// System and filesystem options
	LogLevel    string `json:"log_level"`
	UID         int    `json:"uid"`
	GID         int    `json:"gid"`
	Umask       string `json:"umask"`
	BufferSize  string `json:"buffer_size"`
	AttrTimeout string `json:"attr_timeout"`
	Transfers   int    `json:"transfers"`

	// VFS Cache Settings
	CacheDir             string `json:"cache_dir"`
	VFSCacheMode         string `json:"vfs_cache_mode"`
	VFSCacheMaxSize      string `json:"vfs_cache_max_size"`
	VFSCacheMaxAge       string `json:"vfs_cache_max_age"`
	ReadChunkSize        string `json:"read_chunk_size"`
	ReadChunkSizeLimit   string `json:"read_chunk_size_limit"`
	VFSReadAhead         string `json:"vfs_read_ahead"`
	DirCacheTime         string `json:"dir_cache_time"`
	VFSCacheMinFreeSpace string `json:"vfs_cache_min_free_space"`
	VFSDiskSpaceTotal    string `json:"vfs_disk_space_total"`
	VFSReadChunkStreams  int    `json:"vfs_read_chunk_streams"`

	// Advanced Settings
	NoModTime          bool `json:"no_mod_time"`
	NoChecksum         bool `json:"no_checksum"`
	AsyncRead          bool `json:"async_read"`
	VFSFastFingerprint bool `json:"vfs_fast_fingerprint"`
	UseMmap            bool `json:"use_mmap"`
}

// ProviderAPIResponse sanitizes Provider config for API responses
type ProviderAPIResponse struct {
	ID                string     `json:"id"`
	Host              string     `json:"host"`
	Port              int        `json:"port"`
	Username          string     `json:"username"`
	MaxConnections    int        `json:"max_connections"`
	TLS               bool       `json:"tls"`
	InsecureTLS       bool       `json:"insecure_tls"`
	ProxyURL          string     `json:"proxy_url,omitempty"`
	PasswordSet       bool       `json:"password_set"`
	Enabled           bool       `json:"enabled"`
	IsBackupProvider  bool       `json:"is_backup_provider"`
	LastSpeedTestMbps float64    `json:"last_speed_test_mbps"`
	LastSpeedTestTime *time.Time `json:"last_speed_test_time,omitempty"`
}

// ImportAPIResponse handles Import config for API responses
type ImportAPIResponse struct {
	MaxProcessorWorkers            int                   `json:"max_processor_workers"`
	QueueProcessingIntervalSeconds int                   `json:"queue_processing_interval_seconds"` // Interval in seconds
	AllowedFileExtensions          []string              `json:"allowed_file_extensions"`
	MaxImportConnections           int                   `json:"max_import_connections"`
	ImportCacheSizeMB              int                   `json:"import_cache_size_mb"`
	SegmentSamplePercentage        int                   `json:"segment_sample_percentage"` // Percentage of segments to check (1-100)
	ImportStrategy                 config.ImportStrategy `json:"import_strategy"`
	ImportDir                      *string               `json:"import_dir,omitempty"`
	SkipHealthCheck                bool                  `json:"skip_health_check"`
}

// SABnzbdAPIResponse sanitizes SABnzbd config for API responses
type SABnzbdAPIResponse struct {
	Enabled           bool                     `json:"enabled"`
	CompleteDir       string                   `json:"complete_dir"`
	Categories        []config.SABnzbdCategory `json:"categories"`
	FallbackHost      string                   `json:"fallback_host"`
	FallbackAPIKey    string                   `json:"fallback_api_key"`     // Obfuscated if set
	FallbackAPIKeySet bool                     `json:"fallback_api_key_set"` // Indicates if API key is set
}

// Helper functions to create API responses from core config types

// ToConfigAPIResponse converts config.Config to ConfigAPIResponse with sensitive data masked
func ToConfigAPIResponse(cfg *config.Config, apiKey string) *ConfigAPIResponse {
	if cfg == nil {
		return nil
	}

	// Convert providers with password masking
	providers := make([]ProviderAPIResponse, len(cfg.Providers))
	for i, p := range cfg.Providers {
		providers[i] = ProviderAPIResponse{
			ID:                p.ID,
			Host:              p.Host,
			Port:              p.Port,
			Username:          p.Username,
			MaxConnections:    p.MaxConnections,
			TLS:               p.TLS,
			InsecureTLS:       p.InsecureTLS,
			ProxyURL:          p.ProxyURL,
			PasswordSet:       p.Password != "",
			Enabled:           p.Enabled != nil && *p.Enabled,
			IsBackupProvider:  p.IsBackupProvider != nil && *p.IsBackupProvider,
			LastSpeedTestMbps: p.LastSpeedTestMbps,
			LastSpeedTestTime: p.LastSpeedTestTime,
		}
	}

	// Create RClone response with all configuration fields
	rcloneResp := RCloneAPIResponse{
		PasswordSet:  cfg.RClone.Password != "",
		SaltSet:      cfg.RClone.Salt != "",
		RCEnabled:    cfg.RClone.RCEnabled != nil && *cfg.RClone.RCEnabled,
		RCUrl:        cfg.RClone.RCUrl,
		VFSName:      cfg.RClone.VFSName,
		RCPort:       cfg.RClone.RCPort,
		RCUser:       cfg.RClone.RCUser,
		RCPassSet:    cfg.RClone.RCPass != "",
		RCOptions:    cfg.RClone.RCOptions,
		MountEnabled: cfg.RClone.MountEnabled != nil && *cfg.RClone.MountEnabled,
		MountOptions: cfg.RClone.MountOptions,

		// Mount-Specific Settings
		AllowOther:    cfg.RClone.AllowOther,
		AllowNonEmpty: cfg.RClone.AllowNonEmpty,
		ReadOnly:      cfg.RClone.ReadOnly,
		Timeout:       cfg.RClone.Timeout,
		Syslog:        cfg.RClone.Syslog,

		// System and filesystem options
		LogLevel:    cfg.RClone.LogLevel,
		UID:         cfg.RClone.UID,
		GID:         cfg.RClone.GID,
		Umask:       cfg.RClone.Umask,
		BufferSize:  cfg.RClone.BufferSize,
		AttrTimeout: cfg.RClone.AttrTimeout,
		Transfers:   cfg.RClone.Transfers,

		// VFS Cache Settings
		CacheDir:             cfg.RClone.CacheDir,
		VFSCacheMode:         cfg.RClone.VFSCacheMode,
		VFSCacheMaxSize:      cfg.RClone.VFSCacheMaxSize,
		VFSCacheMaxAge:       cfg.RClone.VFSCacheMaxAge,
		ReadChunkSize:        cfg.RClone.ReadChunkSize,
		ReadChunkSizeLimit:   cfg.RClone.ReadChunkSizeLimit,
		VFSReadAhead:         cfg.RClone.VFSReadAhead,
		DirCacheTime:         cfg.RClone.DirCacheTime,
		VFSCacheMinFreeSpace: cfg.RClone.VFSCacheMinFreeSpace,
		VFSDiskSpaceTotal:    cfg.RClone.VFSDiskSpaceTotal,
		VFSReadChunkStreams:  cfg.RClone.VFSReadChunkStreams,

		// Advanced Settings
		NoModTime:          cfg.RClone.NoModTime,
		NoChecksum:         cfg.RClone.NoChecksum,
		AsyncRead:          cfg.RClone.AsyncRead,
		VFSFastFingerprint: cfg.RClone.VFSFastFingerprint,
		UseMmap:            cfg.RClone.UseMmap,
	}

	// Create SABnzbd response with API key obfuscated
	fallbackAPIKey := ""
	if cfg.SABnzbd.FallbackAPIKey != "" {
		fallbackAPIKey = "********" // Obfuscate the actual key
	}

	sabnzbdResp := SABnzbdAPIResponse{
		Enabled:           cfg.SABnzbd.Enabled != nil && *cfg.SABnzbd.Enabled,
		CompleteDir:       cfg.SABnzbd.CompleteDir,
		Categories:        cfg.SABnzbd.Categories,
		FallbackHost:      cfg.SABnzbd.FallbackHost,
		FallbackAPIKey:    fallbackAPIKey,
		FallbackAPIKeySet: cfg.SABnzbd.FallbackAPIKey != "",
	}

	return &ConfigAPIResponse{
		Config:    cfg,
		Import:    ToImportAPIResponse(cfg.Import),
		RClone:    rcloneResp,
		SABnzbd:   sabnzbdResp,
		Providers: providers,
		APIKey:    apiKey,
	}
}

func ToImportAPIResponse(importConfig config.ImportConfig) ImportAPIResponse {
	return ImportAPIResponse{
		MaxProcessorWorkers:            importConfig.MaxProcessorWorkers,
		QueueProcessingIntervalSeconds: importConfig.QueueProcessingIntervalSeconds,
		AllowedFileExtensions:          importConfig.AllowedFileExtensions,
		MaxImportConnections:           importConfig.MaxImportConnections,
		ImportCacheSizeMB:              importConfig.ImportCacheSizeMB,
		SegmentSamplePercentage:        importConfig.SegmentSamplePercentage,
		ImportStrategy:                 importConfig.ImportStrategy,
		ImportDir:                      importConfig.ImportDir,
		SkipHealthCheck:                importConfig.SkipHealthCheck != nil && *importConfig.SkipHealthCheck,
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
	Percentage   *int                   `json:"percentage,omitempty"` // Progress percentage (0-100), only for items being processed
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
	LibraryPath      *string               `json:"library_path,omitempty"`
	Status           database.HealthStatus `json:"status"`
	LastChecked      time.Time             `json:"last_checked"`
	LastError        *string               `json:"last_error"`
	RetryCount       int                   `json:"retry_count"`
	MaxRetries       int                   `json:"max_retries"`
	SourceNzbPath    *string               `json:"source_nzb_path"`
	ErrorDetails     *string               `json:"error_details"`
	RepairRetryCount int                   `json:"repair_retry_count"`
	MaxRepairRetries int                   `json:"max_repair_retries"`
	CreatedAt        time.Time             `json:"created_at"`
	UpdatedAt        time.Time             `json:"updated_at"`
	ScheduledCheckAt *time.Time            `json:"scheduled_check_at,omitempty"`
	Priority         database.HealthPriority `json:"priority"`
}

// HealthListRequest represents request parameters for listing health records
type HealthListRequest struct {
	Status *database.HealthStatus `json:"status"`
	Since  *time.Time             `json:"since"`
	Pagination
}

// HealthStatsResponse represents health statistics in API responses
type HealthStatsResponse struct {
	Total           int `json:"total"`
	Pending         int `json:"pending"`
	Corrupted       int `json:"corrupted"`
	Healthy         int `json:"healthy"`
	RepairTriggered int `json:"repair_triggered"`
	Checking        int `json:"checking"`
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
	OlderThan   *time.Time             `json:"older_than"`
	Status      *database.HealthStatus `json:"status"`
	DeleteFiles bool                   `json:"delete_files"` // Whether to also delete the physical files
}

// HealthCheckRequest represents request to add file for manual health checking
type HealthCheckRequest struct {
	FilePath   string  `json:"file_path"`
	MaxRetries *int    `json:"max_retries,omitempty"`
	SourceNzb  *string `json:"source_nzb_path,omitempty"`
	Priority   database.HealthPriority `json:"priority,omitempty"`
}

// HealthWorkerStatusResponse represents the current status of the health worker
type HealthWorkerStatusResponse struct {
	Status                 string     `json:"status"`
	LastRunTime            *time.Time `json:"last_run_time,omitempty"`
	NextRunTime            *time.Time `json:"next_run_time,omitempty"`
	TotalRunsCompleted     int64      `json:"total_runs_completed"`
	TotalFilesChecked      int64      `json:"total_files_checked"`
	TotalFilesHealthy      int64      `json:"total_files_healthy"`
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

	// Transform error message for better user understanding
	errorMessage := transformQueueError(item.ErrorMessage)

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
		ErrorMessage: &errorMessage,
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
		LibraryPath:      item.LibraryPath,
		Status:           item.Status,
		LastChecked:      item.LastChecked,
		LastError:        item.LastError,
		RetryCount:       item.RetryCount,
		MaxRetries:       item.MaxRetries,
		SourceNzbPath:    item.SourceNzbPath,
		ErrorDetails:     item.ErrorDetails,
		RepairRetryCount: item.RepairRetryCount,
		MaxRepairRetries: item.MaxRepairRetries,
		CreatedAt:        item.CreatedAt,
		UpdatedAt:        item.UpdatedAt,
		ScheduledCheckAt: item.ScheduledCheckAt,
		Priority:         item.Priority,
	}
}

// ToHealthStatsResponse converts health stats map to HealthStatsResponse
func ToHealthStatsResponse(stats map[database.HealthStatus]int) *HealthStatsResponse {
	pending := stats[database.HealthStatusPending]
	corrupted := stats[database.HealthStatusCorrupted]
	healthy := stats[database.HealthStatusHealthy]
	repairTriggered := stats[database.HealthStatusRepairTriggered]
	checking := stats[database.HealthStatusChecking]

	// Calculate total from all tracked statuses
	total := 0
	for _, count := range stats {
		total += count
	}

	return &HealthStatsResponse{
		Total:           total,
		Pending:         pending,
		Corrupted:       corrupted,
		Healthy:         healthy,
		RepairTriggered: repairTriggered,
		Checking:        checking,
	}
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
	ProxyURL    string `json:"proxy_url,omitempty"`
}

// ProviderTestResponse represents the result of testing provider connectivity
type ProviderTestResponse struct {
	Success      bool   `json:"success"`
	ErrorMessage string `json:"error_message,omitempty"`
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
	ProxyURL         string `json:"proxy_url,omitempty"`
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
	ProxyURL         *string `json:"proxy_url,omitempty"`
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
	FilePath     string  `json:"file_path"`
	RelativePath *string `json:"relative_path,omitempty"`
}

// ManualImportResponse represents the response from manually importing a file
type ManualImportResponse struct {
	QueueID int64  `json:"queue_id"`
	Message string `json:"message"`
}

// ProviderStatusResponse represents NNTP provider connection status in API responses
type ProviderStatusResponse struct {
	ID                    string    `json:"id"`
	Host                  string    `json:"host"`
	Username              string    `json:"username"`
	UsedConnections       int       `json:"used_connections"`
	MaxConnections        int       `json:"max_connections"`
	State                 string    `json:"state"`
	ErrorCount            int64     `json:"error_count"`
	LastConnectionAttempt time.Time `json:"last_connection_attempt"`
	LastSuccessfulConnect time.Time `json:"last_successful_connect"`
	FailureReason         string    `json:"failure_reason"`
	LastSpeedTestMbps     float64   `json:"last_speed_test_mbps"`
	LastSpeedTestTime     *time.Time `json:"last_speed_test_time,omitempty"`
}

// PoolMetricsResponse represents NNTP pool metrics in API responses
type PoolMetricsResponse struct {
	BytesDownloaded             int64                    `json:"bytes_downloaded"`
	BytesUploaded               int64                    `json:"bytes_uploaded"`
	ArticlesDownloaded          int64                    `json:"articles_downloaded"`
	ArticlesPosted              int64                    `json:"articles_posted"`
	TotalErrors                 int64                    `json:"total_errors"`
	ProviderErrors              map[string]int64         `json:"provider_errors"`
	DownloadSpeedBytesPerSec    float64                  `json:"download_speed_bytes_per_sec"`
	MaxDownloadSpeedBytesPerSec float64                  `json:"max_download_speed_bytes_per_sec"`
	UploadSpeedBytesPerSec      float64                  `json:"upload_speed_bytes_per_sec"`
	Timestamp                   time.Time                `json:"timestamp"`
	Providers                   []ProviderStatusResponse `json:"providers"`
}

type TestProviderResponse struct {
	Success      bool   `json:"success"`
	ErrorMessage string `json:"error_message,omitempty"`
}

// Library Sync API Types

// DryRunSyncResult represents the results of a library sync dry run
type DryRunSyncResult struct {
	OrphanedMetadataCount  int  `json:"orphaned_metadata_count"`   // Number of orphaned metadata files
	OrphanedLibraryFiles   int  `json:"orphaned_library_files"`    // Number of orphaned library files (symlinks/STRM)
	DatabaseRecordsToClean int  `json:"database_records_to_clean"` // Number of database records to clean
	WouldCleanup           bool `json:"would_cleanup"`             // Whether cleanup would occur based on config
}
