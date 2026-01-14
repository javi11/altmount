package config

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/javi11/altmount/internal/pathutil"
	"github.com/javi11/nntppool/v2"
	"github.com/jinzhu/copier"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

const MountProvider = "altmount"
const DefaultCategoryName = "Default"
const DefaultCategoryDir = "complete"

// Config represents the complete application configuration
type Config struct {
	WebDAV          WebDAVConfig     `yaml:"webdav" mapstructure:"webdav" json:"webdav"`
	API             APIConfig        `yaml:"api" mapstructure:"api" json:"api"`
	Auth            AuthConfig       `yaml:"auth" mapstructure:"auth" json:"auth"`
	Database        DatabaseConfig   `yaml:"database" mapstructure:"database" json:"database"`
	Metadata        MetadataConfig   `yaml:"metadata" mapstructure:"metadata" json:"metadata"`
	Streaming       StreamingConfig  `yaml:"streaming" mapstructure:"streaming" json:"streaming"`
	Health          HealthConfig     `yaml:"health" mapstructure:"health" json:"health,omitempty"`
	RClone          RCloneConfig     `yaml:"rclone" mapstructure:"rclone" json:"rclone"`
	Import          ImportConfig     `yaml:"import" mapstructure:"import" json:"import"`
	Log             LogConfig        `yaml:"log" mapstructure:"log" json:"log,omitempty"`
	SABnzbd         SABnzbdConfig    `yaml:"sabnzbd" mapstructure:"sabnzbd" json:"sabnzbd"`
	Arrs            ArrsConfig       `yaml:"arrs" mapstructure:"arrs" json:"arrs"`
	Fuse            FuseConfig       `yaml:"fuse" mapstructure:"fuse" json:"fuse"`
	Providers       []ProviderConfig `yaml:"providers" mapstructure:"providers" json:"providers"`
	MountPath       string           `yaml:"mount_path" mapstructure:"mount_path" json:"mount_path"` // WebDAV mount path
	ProfilerEnabled bool             `yaml:"profiler_enabled" mapstructure:"profiler_enabled" json:"profiler_enabled" default:"false"`
}

// WebDAVConfig represents WebDAV server configuration
type WebDAVConfig struct {
	Port     int    `yaml:"port" mapstructure:"port" json:"port"`
	User     string `yaml:"user" mapstructure:"user" json:"user"`
	Password string `yaml:"password" mapstructure:"password" json:"password"`
}

// FuseConfig represents FUSE mount configuration
type FuseConfig struct {
	MountPath           string `yaml:"mount_path" mapstructure:"mount_path" json:"mount_path"`
	Enabled             *bool  `yaml:"enabled" mapstructure:"enabled" json:"enabled"`
	AllowOther          bool   `yaml:"allow_other" mapstructure:"allow_other" json:"allow_other"`
	Debug               bool   `yaml:"debug" mapstructure:"debug" json:"debug"`
	AttrTimeoutSeconds  int    `yaml:"attr_timeout_seconds" mapstructure:"attr_timeout_seconds" json:"attr_timeout_seconds"`
	EntryTimeoutSeconds int    `yaml:"entry_timeout_seconds" mapstructure:"entry_timeout_seconds" json:"entry_timeout_seconds"`
	MaxDownloadWorkers  int    `yaml:"max_download_workers" mapstructure:"max_download_workers" json:"max_download_workers"`
	MaxCacheSizeMB      int    `yaml:"max_cache_size_mb" mapstructure:"max_cache_size_mb" json:"max_cache_size_mb"`
	MaxReadAheadMB      int    `yaml:"max_read_ahead_mb" mapstructure:"max_read_ahead_mb" json:"max_read_ahead_mb"`
}

// APIConfig represents REST API configuration
type APIConfig struct {
	Prefix      string `yaml:"prefix" mapstructure:"prefix" json:"prefix"`
	KeyOverride string `yaml:"key_override" mapstructure:"key_override" json:"key_override,omitempty"`
}

// AuthConfig represents authentication configuration
type AuthConfig struct {
	LoginRequired *bool `yaml:"login_required" mapstructure:"login_required" json:"login_required"`
}

// DatabaseConfig represents database configuration
type DatabaseConfig struct {
	Path string `yaml:"path" mapstructure:"path" json:"path"`
}

// MetadataConfig represents metadata filesystem configuration
type MetadataConfig struct {
	RootPath                 string `yaml:"root_path" mapstructure:"root_path" json:"root_path"`
	DeleteSourceNzbOnRemoval *bool  `yaml:"delete_source_nzb_on_removal" mapstructure:"delete_source_nzb_on_removal" json:"delete_source_nzb_on_removal,omitempty"`
	DeleteFailedNzb          *bool  `yaml:"delete_failed_nzb" mapstructure:"delete_failed_nzb" json:"delete_failed_nzb,omitempty"`
	DeleteCompletedNzb       *bool  `yaml:"delete_completed_nzb" mapstructure:"delete_completed_nzb" json:"delete_completed_nzb,omitempty"`
}

// StreamingConfig represents streaming and chunking configuration
type StreamingConfig struct {
	MaxDownloadWorkers int `yaml:"max_download_workers" mapstructure:"max_download_workers" json:"max_download_workers"`
	MaxCacheSizeMB     int `yaml:"max_cache_size_mb" mapstructure:"max_cache_size_mb" json:"max_cache_size_mb"`
}

// RCloneConfig represents rclone configuration
type RCloneConfig struct {
	// RClone Path
	Path string `yaml:"path" mapstructure:"path" json:"path"`
	// Encryption
	Password string `yaml:"password" mapstructure:"password" json:"-"`
	Salt     string `yaml:"salt" mapstructure:"salt" json:"-"`

	// RC (Remote Control) Configuration
	RCEnabled *bool             `yaml:"rc_enabled" mapstructure:"rc_enabled" json:"rc_enabled"`
	RCUrl     string            `yaml:"rc_url" mapstructure:"rc_url" json:"rc_url"`
	RCPort    int               `yaml:"rc_port" mapstructure:"rc_port" json:"rc_port"`
	RCUser    string            `yaml:"rc_user" mapstructure:"rc_user" json:"rc_user"`
	RCPass    string            `yaml:"rc_pass" mapstructure:"rc_pass" json:"-"`
	RCOptions map[string]string `yaml:"rc_options" mapstructure:"rc_options" json:"rc_options"`

	// Mount Configuration
	MountEnabled *bool             `yaml:"mount_enabled" mapstructure:"mount_enabled" json:"mount_enabled"`
	VFSName      string            `yaml:"vfs_name" mapstructure:"vfs_name" json:"vfs_name"`
	MountOptions map[string]string `yaml:"mount_options" mapstructure:"mount_options" json:"mount_options"`
	LogLevel     string            `yaml:"log_level" mapstructure:"log_level" json:"log_level"`
	UID          int               `yaml:"uid" mapstructure:"uid" json:"uid"`
	GID          int               `yaml:"gid" mapstructure:"gid" json:"gid"`
	Umask        string            `yaml:"umask" mapstructure:"umask" json:"umask"`
	BufferSize   string            `yaml:"buffer_size" mapstructure:"buffer_size" json:"buffer_size"`
	AttrTimeout  string            `yaml:"attr_timeout" mapstructure:"attr_timeout" json:"attr_timeout"`
	Transfers    int               `yaml:"transfers" mapstructure:"transfers" json:"transfers"`

	// VFS Cache Settings
	CacheDir             string `yaml:"cache_dir" mapstructure:"cache_dir" json:"cache_dir"`
	VFSCacheMode         string `yaml:"vfs_cache_mode" mapstructure:"vfs_cache_mode" json:"vfs_cache_mode"`
	VFSCachePollInterval string `yaml:"vfs_cache_poll_interval" mapstructure:"vfs_cache_poll_interval" json:"vfs_cache_poll_interval"`
	VFSReadChunkSize     string `yaml:"vfs_read_chunk_size" mapstructure:"vfs_read_chunk_size" json:"vfs_read_chunk_size"`
	VFSCacheMaxSize      string `yaml:"vfs_cache_max_size" mapstructure:"vfs_cache_max_size" json:"vfs_cache_max_size"`
	VFSCacheMaxAge       string `yaml:"vfs_cache_max_age" mapstructure:"vfs_cache_max_age" json:"vfs_cache_max_age"`
	ReadChunkSize        string `yaml:"read_chunk_size" mapstructure:"read_chunk_size" json:"read_chunk_size"`
	ReadChunkSizeLimit   string `yaml:"read_chunk_size_limit" mapstructure:"read_chunk_size_limit" json:"read_chunk_size_limit"`
	VFSReadAhead         string `yaml:"vfs_read_ahead" mapstructure:"vfs_read_ahead" json:"vfs_read_ahead"`
	DirCacheTime         string `yaml:"dir_cache_time" mapstructure:"dir_cache_time" json:"dir_cache_time"`
	VFSCacheMinFreeSpace string `yaml:"vfs_cache_min_free_space" mapstructure:"vfs_cache_min_free_space" json:"vfs_cache_min_free_space"`
	VFSDiskSpaceTotal    string `yaml:"vfs_disk_space_total" mapstructure:"vfs_disk_space_total" json:"vfs_disk_space_total"`
	VFSReadChunkStreams  int    `yaml:"vfs_read_chunk_streams" mapstructure:"vfs_read_chunk_streams" json:"vfs_read_chunk_streams"`

	// Mount-Specific Settings
	AllowOther    bool   `yaml:"allow_other" mapstructure:"allow_other" json:"allow_other"`
	AllowNonEmpty bool   `yaml:"allow_non_empty" mapstructure:"allow_non_empty" json:"allow_non_empty"`
	ReadOnly      bool   `yaml:"read_only" mapstructure:"read_only" json:"read_only"`
	Timeout       string `yaml:"timeout" mapstructure:"timeout" json:"timeout"`
	Syslog        bool   `yaml:"syslog" mapstructure:"syslog" json:"syslog"`

	// Advanced Settings
	NoModTime          bool `yaml:"no_mod_time" mapstructure:"no_mod_time" json:"no_mod_time"`
	NoChecksum         bool `yaml:"no_checksum" mapstructure:"no_checksum" json:"no_checksum"`
	AsyncRead          bool `yaml:"async_read" mapstructure:"async_read" json:"async_read"`
	VFSFastFingerprint bool `yaml:"vfs_fast_fingerprint" mapstructure:"vfs_fast_fingerprint" json:"vfs_fast_fingerprint"`
	UseMmap            bool `yaml:"use_mmap" mapstructure:"use_mmap" json:"use_mmap"`
}

// ImportStrategy represents the import strategy type
type ImportStrategy string

const (
	ImportStrategyNone    ImportStrategy = "NONE"
	ImportStrategySYMLINK ImportStrategy = "SYMLINK"
	ImportStrategySTRM    ImportStrategy = "STRM"
)

// ImportConfig represents import processing configuration
type ImportConfig struct {
	MaxProcessorWorkers            int            `yaml:"max_processor_workers" mapstructure:"max_processor_workers" json:"max_processor_workers"`
	QueueProcessingIntervalSeconds int            `yaml:"queue_processing_interval_seconds" mapstructure:"queue_processing_interval_seconds" json:"queue_processing_interval_seconds"`
	AllowedFileExtensions          []string       `yaml:"allowed_file_extensions" mapstructure:"allowed_file_extensions" json:"allowed_file_extensions"`
	MaxImportConnections           int            `yaml:"max_import_connections" mapstructure:"max_import_connections" json:"max_import_connections"`
	ImportCacheSizeMB              int            `yaml:"import_cache_size_mb" mapstructure:"import_cache_size_mb" json:"import_cache_size_mb"`
	SegmentSamplePercentage        int            `yaml:"segment_sample_percentage" mapstructure:"segment_sample_percentage" json:"segment_sample_percentage"`
	ReadTimeoutSeconds             int            `yaml:"read_timeout_seconds" mapstructure:"read_timeout_seconds" json:"read_timeout_seconds"`
	ImportStrategy                 ImportStrategy `yaml:"import_strategy" mapstructure:"import_strategy" json:"import_strategy"`
	ImportDir                      *string        `yaml:"import_dir" mapstructure:"import_dir" json:"import_dir,omitempty"`
	SkipHealthCheck                *bool          `yaml:"skip_health_check" mapstructure:"skip_health_check" json:"skip_health_check,omitempty"`
	WatchDir                       *string        `yaml:"watch_dir" mapstructure:"watch_dir" json:"watch_dir,omitempty"`
	WatchIntervalSeconds           *int           `yaml:"watch_interval_seconds" mapstructure:"watch_interval_seconds" json:"watch_interval_seconds,omitempty"`
}

// LogConfig represents logging configuration with rotation support
type LogConfig struct {
	File       string `yaml:"file" mapstructure:"file" json:"file,omitempty"`                      // Log file path (empty = console only)
	Level      string `yaml:"level" mapstructure:"level" json:"level,omitempty"`                   // Log level (debug, info, warn, error)
	MaxSize    int    `yaml:"max_size" mapstructure:"max_size" json:"max_size,omitempty"`          // Max size in MB before rotation
	MaxAge     int    `yaml:"max_age" mapstructure:"max_age" json:"max_age,omitempty"`             // Max age in days to keep files
	MaxBackups int    `yaml:"max_backups" mapstructure:"max_backups" json:"max_backups,omitempty"` // Max number of old files to keep
	Compress   bool   `yaml:"compress" mapstructure:"compress" json:"compress,omitempty"`          // Compress old log files
}

// HealthConfig represents health checker configuration
type HealthConfig struct {
	Enabled                       *bool   `yaml:"enabled" mapstructure:"enabled" json:"enabled,omitempty"`
	LibraryDir                    *string `yaml:"library_dir" mapstructure:"library_dir" json:"library_dir,omitempty"`
	CleanupOrphanedMetadata       *bool   `yaml:"cleanup_orphaned_metadata" mapstructure:"cleanup_orphaned_metadata" json:"cleanup_orphaned_metadata,omitempty"`
	CheckIntervalSeconds          int     `yaml:"check_interval_seconds" mapstructure:"check_interval_seconds" json:"check_interval_seconds,omitempty"`
	MaxConnectionsForHealthChecks int     `yaml:"max_connections_for_health_checks" mapstructure:"max_connections_for_health_checks" json:"max_connections_for_health_checks,omitempty"`
	MaxConcurrentJobs             int     `yaml:"max_concurrent_jobs" mapstructure:"max_concurrent_jobs" json:"max_concurrent_jobs,omitempty"`
	SegmentSamplePercentage       int     `yaml:"segment_sample_percentage" mapstructure:"segment_sample_percentage" json:"segment_sample_percentage,omitempty"`
	LibrarySyncIntervalMinutes    int     `yaml:"library_sync_interval_minutes" mapstructure:"library_sync_interval_minutes" json:"library_sync_interval_minutes,omitempty"`
	LibrarySyncConcurrency        int     `yaml:"library_sync_concurrency" mapstructure:"library_sync_concurrency" json:"library_sync_concurrency,omitempty"`
	ResolveRepairOnImport         *bool   `yaml:"resolve_repair_on_import" mapstructure:"resolve_repair_on_import" json:"resolve_repair_on_import,omitempty"`
}

// GenerateProviderID creates a unique ID based on host, port, and username
func GenerateProviderID(host string, port int, username string) string {
	input := fmt.Sprintf("%s:%d@%s", host, port, username)
	hash := sha256.Sum256([]byte(input))
	return fmt.Sprintf("%x", hash)[:8] // First 8 characters for readability
}

// Path validation functions have been moved to internal/utils/path.go

// ProviderConfig represents a single NNTP provider configuration
type ProviderConfig struct {
	ID                string     `yaml:"id" mapstructure:"id" json:"id"`
	Host              string     `yaml:"host" mapstructure:"host" json:"host"`
	Port              int        `yaml:"port" mapstructure:"port" json:"port"`
	Username          string     `yaml:"username" mapstructure:"username" json:"username"`
	Password          string     `yaml:"password" mapstructure:"password" json:"-"`
	MaxConnections    int        `yaml:"max_connections" mapstructure:"max_connections" json:"max_connections"`
	TLS               bool       `yaml:"tls" mapstructure:"tls" json:"tls"`
	InsecureTLS       bool       `yaml:"insecure_tls" mapstructure:"insecure_tls" json:"insecure_tls"`
	ProxyURL          string     `yaml:"proxy_url" mapstructure:"proxy_url" json:"proxy_url,omitempty"`
	Enabled           *bool      `yaml:"enabled" mapstructure:"enabled" json:"enabled,omitempty"`
	IsBackupProvider  *bool      `yaml:"is_backup_provider" mapstructure:"is_backup_provider" json:"is_backup_provider,omitempty"`
	LastSpeedTestMbps float64    `yaml:"last_speed_test_mbps" mapstructure:"last_speed_test_mbps" json:"last_speed_test_mbps,omitempty"`
	LastSpeedTestTime *time.Time `yaml:"last_speed_test_time" mapstructure:"last_speed_test_time" json:"last_speed_test_time,omitempty"`
}

// SABnzbdConfig represents SABnzbd-compatible API configuration
type SABnzbdConfig struct {
	Enabled               *bool             `yaml:"enabled" mapstructure:"enabled" json:"enabled"`
	CompleteDir           string            `yaml:"complete_dir" mapstructure:"complete_dir" json:"complete_dir"`
	DownloadClientBaseURL string            `yaml:"download_client_base_url" mapstructure:"download_client_base_url" json:"download_client_base_url,omitempty"`
	Categories            []SABnzbdCategory `yaml:"categories" mapstructure:"categories" json:"categories"`
	// Fallback configuration for sending failed imports to external SABnzbd
	FallbackHost   string `yaml:"fallback_host" mapstructure:"fallback_host" json:"fallback_host"`
	FallbackAPIKey string `yaml:"fallback_api_key" mapstructure:"fallback_api_key" json:"fallback_api_key"` // Masked in API responses
}

// SABnzbdCategory represents a SABnzbd category configuration
type SABnzbdCategory struct {
	Name     string `yaml:"name" mapstructure:"name" json:"name"`
	Order    int    `yaml:"order" mapstructure:"order" json:"order"`
	Priority int    `yaml:"priority" mapstructure:"priority" json:"priority"`
	Dir      string `yaml:"dir" mapstructure:"dir" json:"dir"`
	Type     string `yaml:"type" mapstructure:"type" json:"type"` // "sonarr" or "radarr"
}

// IgnoredMessage represents an error message to ignore during queue cleanup
type IgnoredMessage struct {
	Message string `yaml:"message" mapstructure:"message" json:"message"`
	Enabled bool   `yaml:"enabled" mapstructure:"enabled" json:"enabled"`
}

// ArrsConfig represents arrs configuration
type ArrsConfig struct {
	Enabled                        *bool                `yaml:"enabled" mapstructure:"enabled" json:"enabled"`
	MaxWorkers                     int                  `yaml:"max_workers" mapstructure:"max_workers" json:"max_workers,omitempty"`
	WebhookBaseURL                 string               `yaml:"webhook_base_url" mapstructure:"webhook_base_url" json:"webhook_base_url,omitempty"`
	RadarrInstances                []ArrsInstanceConfig `yaml:"radarr_instances" mapstructure:"radarr_instances" json:"radarr_instances"`
	SonarrInstances                []ArrsInstanceConfig `yaml:"sonarr_instances" mapstructure:"sonarr_instances" json:"sonarr_instances"`
	QueueCleanupEnabled            *bool                `yaml:"queue_cleanup_enabled" mapstructure:"queue_cleanup_enabled" json:"queue_cleanup_enabled,omitempty"`
	QueueCleanupIntervalSeconds    int                  `yaml:"queue_cleanup_interval_seconds" mapstructure:"queue_cleanup_interval_seconds" json:"queue_cleanup_interval_seconds,omitempty"`
	CleanupAutomaticImportFailure  *bool                `yaml:"cleanup_automatic_import_failure" mapstructure:"cleanup_automatic_import_failure" json:"cleanup_automatic_import_failure,omitempty"`
	QueueCleanupGracePeriodMinutes int                  `yaml:"queue_cleanup_grace_period_minutes" mapstructure:"queue_cleanup_grace_period_minutes" json:"queue_cleanup_grace_period_minutes,omitempty"`
	QueueCleanupAllowlist          []IgnoredMessage     `yaml:"queue_cleanup_allowlist" mapstructure:"queue_cleanup_allowlist" json:"queue_cleanup_allowlist,omitempty"`
}

// ArrsInstanceConfig represents a single arrs instance configuration
type ArrsInstanceConfig struct {
	Name              string `yaml:"name" mapstructure:"name" json:"name"`
	URL               string `yaml:"url" mapstructure:"url" json:"url"`
	APIKey            string `yaml:"api_key" mapstructure:"api_key" json:"api_key"`
	Category          string `yaml:"category" mapstructure:"category" json:"category,omitempty"`
	Enabled           *bool  `yaml:"enabled" mapstructure:"enabled" json:"enabled,omitempty"`
	SyncIntervalHours *int   `yaml:"sync_interval_hours" mapstructure:"sync_interval_hours" json:"sync_interval_hours,omitempty"`
}

// DeepCopy returns a deep copy of the configuration using the copier library.
// This handles all pointer fields, slices, and maps automatically.
func (c *Config) DeepCopy() *Config {
	if c == nil {
		return nil
	}

	copyCfg := &Config{}
	if err := copier.CopyWithOption(copyCfg, c, copier.Option{DeepCopy: true}); err != nil {
		// Fallback to shallow copy if deep copy fails (should not happen)
		shallowCopy := *c
		return &shallowCopy
	}

	return copyCfg
}

// Validate validates the configuration
func (c *Config) Validate() error {
	if c.WebDAV.Port <= 0 || c.WebDAV.Port > 65535 {
		return fmt.Errorf("webdav port must be between 1 and 65535")
	}

	if c.Streaming.MaxDownloadWorkers <= 0 {
		return fmt.Errorf("streaming max_download_workers must be greater than 0")
	}

	if c.Streaming.MaxCacheSizeMB <= 0 {
		c.Streaming.MaxCacheSizeMB = 32 // Default to 32MB if not set
	}

	if c.Import.MaxProcessorWorkers <= 0 {
		return fmt.Errorf("import max_processor_workers must be greater than 0")
	}

	if c.Import.QueueProcessingIntervalSeconds < 1 {
		return fmt.Errorf("import queue_processing_interval_seconds must be at least 1 second")
	}

	if c.Import.QueueProcessingIntervalSeconds > 300 {
		return fmt.Errorf("import queue_processing_interval_seconds must not exceed 300 seconds")
	}

	if c.Import.MaxImportConnections <= 0 {
		return fmt.Errorf("import max_import_connections must be greater than 0")
	}

	if c.Import.ImportCacheSizeMB <= 0 {
		return fmt.Errorf("import import_cache_size_mb must be greater than 0")
	}

	if c.Import.SegmentSamplePercentage < 1 || c.Import.SegmentSamplePercentage > 100 {
		return fmt.Errorf("import segment_sample_percentage must be between 1 and 100")
	}

	if c.Import.ReadTimeoutSeconds <= 0 {
		c.Import.ReadTimeoutSeconds = 300
	}

	// Validate import strategy
	validStrategies := map[ImportStrategy]bool{
		ImportStrategyNone:    true,
		ImportStrategySYMLINK: true,
		ImportStrategySTRM:    true,
	}
	if !validStrategies[c.Import.ImportStrategy] {
		return fmt.Errorf("import_strategy must be one of: NONE, SYMLINK, STRM")
	}

	// Validate import directory when strategy requires it
	if c.Import.ImportStrategy == ImportStrategySYMLINK || c.Import.ImportStrategy == ImportStrategySTRM {
		if c.Import.ImportDir == nil || *c.Import.ImportDir == "" {
			return fmt.Errorf("import_dir cannot be empty when import strategy is %s", c.Import.ImportStrategy)
		}
		if !filepath.IsAbs(*c.Import.ImportDir) {
			return fmt.Errorf("import_dir must be an absolute path")
		}
	}

	// Validate watch directory if configured
	if c.Import.WatchDir != nil && *c.Import.WatchDir != "" {
		if !filepath.IsAbs(*c.Import.WatchDir) {
			return fmt.Errorf("import watch_dir must be an absolute path")
		}
		if c.Import.WatchIntervalSeconds != nil && *c.Import.WatchIntervalSeconds <= 0 {
			return fmt.Errorf("import watch_interval_seconds must be greater than 0")
		}
	}

	// Validate log level (both old and new config)
	if c.Log.Level != "" {
		validLevels := []string{"debug", "info", "warn", "error"}
		isValid := false
		for _, level := range validLevels {
			if c.Log.Level == level {
				isValid = true
				break
			}
		}
		if !isValid {
			return fmt.Errorf("log_level must be one of: debug, info, warn, error")
		}
	}

	// Validate log configuration
	if c.Log.Level != "" {
		validLevels := []string{"debug", "info", "warn", "error"}
		isValid := false
		for _, level := range validLevels {
			if c.Log.Level == level {
				isValid = true
				break
			}
		}
		if !isValid {
			return fmt.Errorf("log.level must be one of: debug, info, warn, error")
		}
	}

	if c.Log.MaxSize < 0 {
		return fmt.Errorf("log.max_size must be non-negative")
	}

	if c.Log.MaxAge < 0 {
		return fmt.Errorf("log.max_age must be non-negative")
	}

	if c.Log.MaxBackups < 0 {
		return fmt.Errorf("log.max_backups must be non-negative")
	}

	// Validate metadata configuration (now required)
	if c.Metadata.RootPath == "" {
		return fmt.Errorf("metadata root_path cannot be empty")
	}

	// Validate streaming configuration

	// Validate health configuration (always active)
	if c.Health.CheckIntervalSeconds <= 0 {
		return fmt.Errorf("health check_interval_seconds must be greater than 0")
	}
	if c.Health.MaxConnectionsForHealthChecks <= 0 {
		return fmt.Errorf("health max_connections_for_health_checks must be greater than 0")
	}
	if c.Health.MaxConcurrentJobs <= 0 {
		return fmt.Errorf("health max_concurrent_jobs must be greater than 0")
	}
	if c.Health.LibrarySyncIntervalMinutes < 0 {
		return fmt.Errorf("health library_sync_interval_minutes must be non-negative")
	}
	if c.Health.SegmentSamplePercentage < 1 || c.Health.SegmentSamplePercentage > 100 {
		return fmt.Errorf("health segment_sample_percentage must be between 1 and 100")
	}

	// Validate health configuration - requires library_dir when enabled and using a strategy other than NONE
	if c.Health.Enabled != nil && *c.Health.Enabled {
		if c.Import.ImportStrategy != ImportStrategyNone {
			if c.Health.LibraryDir == nil || *c.Health.LibraryDir == "" {
				return fmt.Errorf("health library_dir is required when health system is enabled with %s strategy", c.Import.ImportStrategy)
			}
			if !filepath.IsAbs(*c.Health.LibraryDir) {
				return fmt.Errorf("health library_dir must be an absolute path")
			}
		}
	}

	// Validate cleanup orphaned metadata - requires library_dir when enabled and using a strategy other than NONE
	if c.Health.CleanupOrphanedMetadata != nil && *c.Health.CleanupOrphanedMetadata {
		if c.Import.ImportStrategy != ImportStrategyNone {
			if c.Health.LibraryDir == nil || *c.Health.LibraryDir == "" {
				return fmt.Errorf("health library_dir is required when cleanup_orphaned_metadata is enabled with %s strategy", c.Import.ImportStrategy)
			}
			if !filepath.IsAbs(*c.Health.LibraryDir) {
				return fmt.Errorf("health library_dir must be an absolute path")
			}
		}
	}

	// Auto-enable RC when mount is enabled (mount requires RC to function)
	if c.RClone.MountEnabled != nil && *c.RClone.MountEnabled {
		if c.RClone.RCEnabled == nil || !*c.RClone.RCEnabled {
			// Auto-enable RC since mount requires it
			enabled := true
			c.RClone.RCEnabled = &enabled
		}
	}

	// Validate RClone Mount configuration
	if c.RClone.MountEnabled != nil && *c.RClone.MountEnabled {
		if c.MountPath == "" {
			return fmt.Errorf("rclone mount_path cannot be empty when mount is enabled")
		}
		if !filepath.IsAbs(c.MountPath) {
			return fmt.Errorf("rclone mount_path must be an absolute path")
		}
	}

	// Validate SABnzbd configuration
	if c.SABnzbd.Enabled != nil && *c.SABnzbd.Enabled {
		// CompleteDir is a virtual path relative to the mount point, not an absolute filesystem path
		// It defaults to "/" (root of mount) if not specified
		// Normalize: remove leading/trailing slashes for consistency, then ensure it starts with /
		if c.SABnzbd.CompleteDir == "" {
			c.SABnzbd.CompleteDir = "/"
		} else {
			// Normalize the path: ensure it starts with / and remove trailing /
			cleanDir := strings.Trim(c.SABnzbd.CompleteDir, "/")
			if cleanDir == "" {
				c.SABnzbd.CompleteDir = "/"
			} else {
				c.SABnzbd.CompleteDir = "/" + cleanDir
			}
		}

		// Validate categories if provided
		categoryNames := make(map[string]bool)
		for i, category := range c.SABnzbd.Categories {
			if category.Name == "" {
				return fmt.Errorf("sabnzbd category %d: name cannot be empty", i)
			}
			if categoryNames[category.Name] {
				return fmt.Errorf("sabnzbd category %d: duplicate category name '%s'", i, category.Name)
			}
			categoryNames[category.Name] = true
		}

		// Validate fallback configuration if host is provided
		if c.SABnzbd.FallbackHost != "" {
			// Basic URL validation
			if !strings.HasPrefix(c.SABnzbd.FallbackHost, "http://") && !strings.HasPrefix(c.SABnzbd.FallbackHost, "https://") {
				return fmt.Errorf("sabnzbd fallback_host must start with http:// or https://")
			}
			// Warn if API key is missing (but don't fail validation)
			if c.SABnzbd.FallbackAPIKey == "" {
				fmt.Printf("Warning: SABnzbd fallback_host is set but fallback_api_key is empty\n")
			}
		}
	}

	// Validate mount_path
	if c.MountPath != "" && !filepath.IsAbs(c.MountPath) {
		return fmt.Errorf("mount_path must be an absolute path")
	}

	// Validate scraper configuration
	if c.Arrs.Enabled != nil && *c.Arrs.Enabled {
		// Mount path is required when ARRs is enabled
		if c.MountPath == "" {
			return fmt.Errorf("mount_path is required when arrs is enabled")
		}
		if c.Arrs.MaxWorkers <= 0 {
			return fmt.Errorf("scraper max_workers must be greater than 0")
		}
	}

	// Validate each provider
	for i, provider := range c.Providers {
		if provider.Host == "" {
			return fmt.Errorf("provider %d: host cannot be empty", i)
		}
		if provider.Port <= 0 || provider.Port > 65535 {
			return fmt.Errorf("provider %d: port must be between 1 and 65535", i)
		}
		if provider.MaxConnections <= 0 {
			return fmt.Errorf("provider %d: max_connections must be greater than 0", i)
		}
	}

	// Validate Fuse configuration
	if c.Fuse.MaxDownloadWorkers <= 0 {
		c.Fuse.MaxDownloadWorkers = 15 // Default
	}
	if c.Fuse.MaxCacheSizeMB <= 0 {
		c.Fuse.MaxCacheSizeMB = 32 // Default
	}
	if c.Fuse.MaxReadAheadMB <= 0 {
		c.Fuse.MaxReadAheadMB = 128 // Default 128MB
	}

	return nil
}

// ValidateDirectories validates that all configured directories are writable
// This performs actual filesystem checks and may create directories if needed
func (c *Config) ValidateDirectories() error {
	// Check metadata directory
	if err := pathutil.CheckDirectoryWritable(c.Metadata.RootPath); err != nil {
		return fmt.Errorf("metadata directory validation failed: %w", err)
	}

	// Check database directory
	if err := pathutil.CheckFileDirectoryWritable(c.Database.Path, "database"); err != nil {
		return err
	}

	// Check log file directory (only if log file is configured)
	if err := pathutil.CheckFileDirectoryWritable(c.Log.File, "log"); err != nil {
		return err
	}

	return nil
}

// ProvidersEqual compares the providers in this config with another config for equality
func (c *Config) ProvidersEqual(other *Config) bool {
	if len(c.Providers) != len(other.Providers) {
		return false
	}

	// Create maps for comparison (using ID as key for proper matching)
	oldMap := make(map[string]ProviderConfig)
	newMap := make(map[string]ProviderConfig)

	for _, provider := range c.Providers {
		oldMap[provider.ID] = provider
	}

	for _, provider := range other.Providers {
		newMap[provider.ID] = provider
	}

	// Check if all old providers exist in new config and are identical
	for id, oldProvider := range oldMap {
		newProvider, exists := newMap[id]
		if !exists {
			return false // Provider removed
		}

		// Compare all fields
		if oldProvider.ID != newProvider.ID ||
			oldProvider.Host != newProvider.Host ||
			oldProvider.Port != newProvider.Port ||
			oldProvider.Username != newProvider.Username ||
			oldProvider.Password != newProvider.Password ||
			oldProvider.MaxConnections != newProvider.MaxConnections ||
			oldProvider.TLS != newProvider.TLS ||
			oldProvider.InsecureTLS != newProvider.InsecureTLS ||
			oldProvider.ProxyURL != newProvider.ProxyURL ||
			*oldProvider.Enabled != *newProvider.Enabled ||
			*oldProvider.IsBackupProvider != *newProvider.IsBackupProvider {
			return false // Provider modified
		}
	}

	// Check if any new providers were added
	for id := range newMap {
		if _, exists := oldMap[id]; !exists {
			return false // Provider added
		}
	}

	return true // All providers are identical
}

// ToNNTPProviders converts ProviderConfig slice to nntppool.UsenetProviderConfig slice (enabled only)
func (c *Config) ToNNTPProviders() []nntppool.UsenetProviderConfig {
	var providers []nntppool.UsenetProviderConfig
	for _, p := range c.Providers {
		// Only include enabled providers
		if *p.Enabled {
			isBackup := false
			if p.IsBackupProvider != nil {
				isBackup = *p.IsBackupProvider
			}
			providers = append(providers, nntppool.UsenetProviderConfig{
				Host:                           p.Host,
				Port:                           p.Port,
				Username:                       p.Username,
				Password:                       p.Password,
				MaxConnections:                 p.MaxConnections,
				MaxConnectionIdleTimeInSeconds: 60, // Default idle timeout
				TLS:                            p.TLS,
				InsecureSSL:                    p.InsecureTLS,
				ProxyURL:                       p.ProxyURL,
				MaxConnectionTTLInSeconds:      60, // Default connection TTL
				IsBackupProvider:               isBackup,
			})
		}
	}
	return providers
}

// ChangeCallback represents a function called when configuration changes
type ChangeCallback func(oldConfig, newConfig *Config)

// ConfigGetter represents a function that returns the current configuration
type ConfigGetter func() *Config

// Manager manages configuration state and persistence
type Manager struct {
	current           *Config
	configFile        string
	mutex             sync.RWMutex
	callbacks         []ChangeCallback
	needsLibrarySync  bool
	previousMountPath string
	librarySyncMutex  sync.RWMutex
}

// NewManager creates a new configuration manager
func NewManager(config *Config, configFile string) *Manager {
	return &Manager{
		current:    config,
		configFile: configFile,
	}
}

// GetConfig returns the current configuration (thread-safe)
func (m *Manager) GetConfig() *Config {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return m.current
}

// GetConfigGetter returns a function that provides the current configuration
func (m *Manager) GetConfigGetter() ConfigGetter {
	return m.GetConfig
}

// UpdateConfig updates the current configuration (thread-safe)
func (m *Manager) UpdateConfig(config *Config) error {
	m.mutex.Lock()
	// Take a deep copy of the old config so callbacks get an immutable snapshot
	var oldConfig *Config
	if m.current != nil {
		oldConfig = m.current.DeepCopy()
	}

	// Detect mount_path changes
	if oldConfig != nil && oldConfig.MountPath != config.MountPath {
		m.librarySyncMutex.Lock()
		m.needsLibrarySync = true
		m.previousMountPath = oldConfig.MountPath
		m.librarySyncMutex.Unlock()
	}

	m.current = config
	callbacks := make([]ChangeCallback, len(m.callbacks))
	copy(callbacks, m.callbacks)
	m.mutex.Unlock()

	// Notify callbacks after releasing the lock
	for _, callback := range callbacks {
		callback(oldConfig, config)
	}
	return nil
}

// OnConfigChange registers a callback to be called when configuration changes
func (m *Manager) OnConfigChange(callback ChangeCallback) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.callbacks = append(m.callbacks, callback)
}

// ValidateConfigUpdate validates configuration updates with additional restrictions
func (m *Manager) ValidateConfigUpdate(newConfig *Config) error {
	// First run standard validation
	if err := newConfig.Validate(); err != nil {
		return err
	}

	// Get current config for comparison
	m.mutex.RLock()
	currentConfig := m.current
	m.mutex.RUnlock()

	if currentConfig != nil {
		// Protect WebDAV port from API changes
		if newConfig.WebDAV.Port != currentConfig.WebDAV.Port {
			return fmt.Errorf("webdav port cannot be changed via API - requires server restart")
		}

		// Protect database path from API changes
		if newConfig.Database.Path != currentConfig.Database.Path {
			return fmt.Errorf("database path cannot be changed via API - requires server restart")
		}

		// Protect metadata root path from API changes
		if newConfig.Metadata.RootPath != currentConfig.Metadata.RootPath {
			return fmt.Errorf("metadata root_path cannot be changed via API - requires server restart")
		}

	}

	return nil
}

// ValidateConfig validates the configuration using existing validation logic
func (m *Manager) ValidateConfig(config *Config) error {
	return config.Validate()
}

// ReloadConfig reloads configuration from file
func (m *Manager) ReloadConfig() error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	// Set the config file for viper
	viper.SetConfigFile(m.configFile)

	// Read the configuration file
	if err := viper.ReadInConfig(); err != nil {
		return fmt.Errorf("error reading config file %s: %w", m.configFile, err)
	}

	// Create default config and unmarshal into it
	config := DefaultConfig()
	if err := viper.Unmarshal(config); err != nil {
		return fmt.Errorf("error unmarshaling config: %w", err)
	}

	// Validate configuration
	if err := config.Validate(); err != nil {
		return fmt.Errorf("config validation failed: %w", err)
	}

	m.current = config
	return nil
}

// SaveConfig saves the current configuration to file
func (m *Manager) SaveConfig() error {
	m.mutex.RLock()
	config := m.current
	m.mutex.RUnlock()

	if config == nil {
		return fmt.Errorf("no configuration to save")
	}

	return SaveToFile(config, m.configFile)
}

// NeedsLibrarySync returns whether a library sync is needed due to configuration changes
func (m *Manager) NeedsLibrarySync() bool {
	m.librarySyncMutex.RLock()
	defer m.librarySyncMutex.RUnlock()
	return m.needsLibrarySync
}

// GetPreviousMountPath returns the previous mount path before the last change
func (m *Manager) GetPreviousMountPath() string {
	m.librarySyncMutex.RLock()
	defer m.librarySyncMutex.RUnlock()
	return m.previousMountPath
}

// ClearLibrarySyncFlag clears the library sync needed flag
func (m *Manager) ClearLibrarySyncFlag() {
	m.librarySyncMutex.Lock()
	defer m.librarySyncMutex.Unlock()
	m.needsLibrarySync = false
	m.previousMountPath = ""
}

// isRunningInDocker detects if the application is running inside a Docker container
func isRunningInDocker() bool {
	// Check for the presence of /.dockerenv file (most reliable method)
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}

	// Fallback: check /proc/self/cgroup for container indicators
	if data, err := os.ReadFile("/proc/self/cgroup"); err == nil {
		content := string(data)
		// Look for Docker container indicators in cgroup
		if strings.Contains(content, "/docker/") ||
			strings.Contains(content, "/docker-") ||
			strings.Contains(content, ".scope") {
			return true
		}
	}

	return false
}

// DefaultConfig returns a config with default values
// If configDir is provided, it will be used for database and log file paths
func DefaultConfig(configDir ...string) *Config {
	healthEnabled := false            // Health system disabled by default
	cleanupOrphanedMetadata := false  // Cleanup orphaned metadata disabled by default
	resolveRepairOnImport := false    // Disable smart replacement detection by default
	deleteSourceNzbOnRemoval := false // Delete source NZB on removal disabled by default
	vfsEnabled := false
	mountEnabled := false // Disabled by default
	sabnzbdEnabled := false
	scrapperEnabled := false
	fuseEnabled := false
	loginRequired := true // Require login by default
	skipHealthCheck := true
	watchIntervalSeconds := 10 // Default watch interval
	cleanupAutomaticImportFailure := false

	// Set paths based on whether we're running in Docker or have a specific config directory
	var dbPath, metadataPath, logPath, rclonePath, cachePath string

	// If a config directory is provided, use it
	if len(configDir) > 0 && configDir[0] != "" {
		dbPath = filepath.Join(configDir[0], "altmount.db")
		metadataPath = filepath.Join(configDir[0], "metadata")
		logPath = filepath.Join(configDir[0], "altmount.log")
		rclonePath = configDir[0]
		cachePath = filepath.Join(configDir[0], "cache")
	} else if isRunningInDocker() {
		dbPath = "/config/altmount.db"
		metadataPath = "/metadata"
		logPath = "/config/altmount.log"
		rclonePath = "/config"
		cachePath = "/config/cache"
	} else {
		dbPath = "./altmount.db"
		metadataPath = "./metadata"
		logPath = "./altmount.log"
		rclonePath = "."
		cachePath = "./cache"
	}

	return &Config{
		WebDAV: WebDAVConfig{
			Port:     8080,
			User:     "usenet",
			Password: "usenet",
		},
		API: APIConfig{
			Prefix: "/api",
		},
		Auth: AuthConfig{
			LoginRequired: &loginRequired,
		},
		Database: DatabaseConfig{
			Path: dbPath,
		},
		Metadata: MetadataConfig{
			RootPath:                 metadataPath,
			DeleteSourceNzbOnRemoval: &deleteSourceNzbOnRemoval,
		},
		Streaming: StreamingConfig{
			MaxDownloadWorkers: 15, // Default: 15 download workers
			MaxCacheSizeMB:     32, // Default: 32MB cache for ahead downloads
		},
		RClone: RCloneConfig{
			Path:         rclonePath,
			Password:     "",
			Salt:         "",
			RCEnabled:    &vfsEnabled, // Using vfsEnabled var for backward compatibility
			RCUrl:        "",
			RCUser:       "admin",
			RCPass:       "admin",
			RCPort:       5573, // Changed from 5572 to match your command
			VFSName:      MountProvider,
			MountEnabled: &mountEnabled,
			MountOptions: map[string]string{},

			// Mount Configuration defaults - matching your command
			LogLevel:    "INFO",
			UID:         1000,
			GID:         1000,
			Umask:       "002", // Changed from 0022 to match --umask=002
			BufferSize:  "32M", // Changed from 10M to match --buffer-size=32M
			AttrTimeout: "1s",
			Transfers:   4,
			Timeout:     "10m", // New field matching --timeout=10m

			// Mount-Specific Settings - matching your command
			AllowOther:    true,  // --allow-other
			AllowNonEmpty: true,  // --allow-non-empty
			ReadOnly:      false, // Not specified in your command, so false
			Syslog:        true,  // --syslog

			// VFS Cache Settings - matching your command
			CacheDir:           cachePath, // VFS cache directory (defaults to <rclone_path>/cache)
			VFSCacheMode:       "full",    // --vfs-cache-mode=full
			VFSCacheMaxSize:    "50G",     // --vfs-cache-max-size=50G (changed from 100G)
			VFSCacheMaxAge:     "504h",    // --vfs-cache-max-age=504h (changed from 100h)
			ReadChunkSize:      "32M",     // --vfs-read-chunk-size=32M (changed from 128M)
			ReadChunkSizeLimit: "2G",      // --vfs-read-chunk-size-limit=2G
			VFSReadAhead:       "128M",    // --vfs-read-ahead=128M (changed from 128k)
			DirCacheTime:       "10m",     // --dir-cache-time=10m (changed from 5m)

			// Additional VFS Settings (not specified in your command, using sensible defaults)
			VFSCacheMinFreeSpace: "1G",
			VFSDiskSpaceTotal:    "1G",
			VFSReadChunkStreams:  4,
		},
		Import: ImportConfig{
			MaxProcessorWorkers:            2, // Default: 2 processor workers
			QueueProcessingIntervalSeconds: 5, // Default: check for work every 5 seconds
			AllowedFileExtensions: []string{ // Default: common video extensions
				".mkv", ".mp4", ".avi", ".ts", ".m4v", ".mov", ".wmv", ".mpg", ".mpeg",
				".xvid", ".rm", ".rmvb", ".asf", ".asx", ".wtv", ".mk3d", ".dvr-ms",
			},
			MaxImportConnections:    5,                  // Default: 5 concurrent NNTP connections for validation and archive processing
			ImportCacheSizeMB:       64,                 // Default: 64MB cache for archive analysis
			SegmentSamplePercentage: 1,                  // Default: 1% segment sampling
			ReadTimeoutSeconds:      300,                // Default: 5 minutes read timeout
			ImportStrategy:          ImportStrategyNone, // Default: no import strategy (direct import)
			ImportDir:               nil,                // No default import directory
			SkipHealthCheck:         &skipHealthCheck,
			WatchDir:                nil,
			WatchIntervalSeconds:    &watchIntervalSeconds,
		},
		Log: LogConfig{
			File:       logPath, // Default log file path
			Level:      "info",  // Default log level
			MaxSize:    100,     // 100MB max size
			MaxAge:     30,      // Keep for 30 days
			MaxBackups: 10,      // Keep 10 old files
			Compress:   true,    // Compress old files
		},
		Health: HealthConfig{
			Enabled:                       &healthEnabled,           // Disabled by default
			CleanupOrphanedMetadata:       &cleanupOrphanedMetadata, // Disabled by default
			CheckIntervalSeconds:          5,
			MaxConnectionsForHealthChecks: 5,
			MaxConcurrentJobs:             1,                      // Default: 1 concurrent job
			SegmentSamplePercentage:       5,                      // Default: 5% segment sampling
			LibrarySyncIntervalMinutes:    360,                    // Default: sync every 6 hours
			ResolveRepairOnImport:         &resolveRepairOnImport, // Enabled by default
		},
		SABnzbd: SABnzbdConfig{
			Enabled:               &sabnzbdEnabled,
			CompleteDir:           "/complete",
			DownloadClientBaseURL: "http://altmount:8080/sabnzbd",
			Categories: []SABnzbdCategory{
				{
					Name:     "movies",
					Order:    1,
					Priority: 0,
				},
				{
					Name:     "tv",
					Order:    1,
					Priority: 1,
				},
			},
			FallbackHost:   "",
			FallbackAPIKey: "",
		},
		Providers: []ProviderConfig{},
		Arrs: ArrsConfig{
			Enabled:                        &scrapperEnabled, // Disabled by default
			MaxWorkers:                     5,                // Default to 5 concurrent workers
			WebhookBaseURL:                 "http://altmount:8080",
			RadarrInstances:                []ArrsInstanceConfig{},
			SonarrInstances:                []ArrsInstanceConfig{},
			CleanupAutomaticImportFailure:  &cleanupAutomaticImportFailure,
			QueueCleanupGracePeriodMinutes: 10, // Default to 10 minutes
			QueueCleanupAllowlist: []IgnoredMessage{
				{Message: "No files found are eligible", Enabled: true},
				{Message: "One or more episodes expected in this release were not imported or missing", Enabled: true},
				{Message: "is not a valid video file", Enabled: true},
				{Message: "Sample file", Enabled: true},
				{Message: "No video files were found in the selected folder", Enabled: true},
				{Message: "Could not find file", Enabled: true},
				{Message: "Unexpected error processing file", Enabled: true},
				{Message: "Download doesn't contain intermediate path", Enabled: true},
			},
		},
		Fuse: FuseConfig{
			Enabled:             &fuseEnabled,
			MountPath:           "",
			AllowOther:          true,
			Debug:               false,
			AttrTimeoutSeconds:  1,
			EntryTimeoutSeconds: 1,
			MaxDownloadWorkers:  15,
			MaxCacheSizeMB:      128,
			MaxReadAheadMB:      128,
		},
		MountPath: "", // Empty by default - required when ARRs is enabled
	}
}

// SaveToFile saves a configuration to a YAML file
func SaveToFile(config *Config, filename string) error {
	if filename == "" {
		return fmt.Errorf("no config file path provided")
	}

	// Ensure the directory exists
	dir := filepath.Dir(filename)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// Marshal config to YAML
	data, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// Write to file
	if err := os.WriteFile(filename, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// LoadConfig loads configuration from file and merges with defaults
func LoadConfig(configFile string) (*Config, error) {
	config := DefaultConfig()

	var targetConfigFile string
	if configFile != "" {
		viper.SetConfigFile(configFile)
		targetConfigFile = configFile
	} else {
		// Look for config file in common locations
		viper.SetConfigName("config")
		viper.SetConfigType("yaml")
		targetConfigFile = "config.yaml"
	}

	// Read the configuration file
	if err := viper.ReadInConfig(); err != nil {
		// Check if it's a file not found error
		if os.IsNotExist(err) || strings.Contains(err.Error(), "no such file") {
			// Create default config file with paths relative to config directory
			configDir := filepath.Dir(targetConfigFile)
			configForSave := DefaultConfig(configDir)
			if err := SaveToFile(configForSave, targetConfigFile); err != nil {
				return nil, fmt.Errorf("failed to create default config file %s: %w", targetConfigFile, err)
			}

			// Log that we created a default config
			fmt.Printf("Created default configuration file: %s\n", targetConfigFile)
			fmt.Printf("Please review and modify the configuration as needed.\n")

			// Now try to read the newly created file
			viper.SetConfigFile(targetConfigFile)
			if err := viper.ReadInConfig(); err != nil {
				return nil, fmt.Errorf("error reading newly created config file %s: %w", targetConfigFile, err)
			}
		} else {
			// Other error (permissions, syntax, etc.)
			if configFile != "" {
				return nil, fmt.Errorf("error reading config file %s: %w", configFile, err)
			}
			return nil, fmt.Errorf("error reading config file: %w", err)
		}
	}

	// Unmarshal the config
	if err := viper.Unmarshal(config); err != nil {
		return nil, fmt.Errorf("error unmarshaling config: %w", err)
	}

	// If log file was not explicitly set in the config file and we have a specific config file path,
	// derive log file path from config file location
	if configFile != "" && !viper.IsSet("log.file") {
		configDir := filepath.Dir(configFile)
		config.Log.File = filepath.Join(configDir, "altmount.log")
	}

	// If cache_dir was not explicitly set or is empty, derive it from config file location
	if configFile != "" && (!viper.IsSet("rclone.cache_dir") || config.RClone.CacheDir == "") {
		configDir := filepath.Dir(configFile)
		config.RClone.CacheDir = filepath.Join(configDir, "cache")
	}

	// Check for PORT environment variable override
	if portEnv := os.Getenv("PORT"); portEnv != "" {
		port := 0
		_, err := fmt.Sscanf(portEnv, "%d", &port)
		if err != nil {
			return nil, fmt.Errorf("invalid PORT environment variable '%s': must be a number", portEnv)
		}
		if port <= 0 || port > 65535 {
			return nil, fmt.Errorf("invalid PORT environment variable %d: must be between 1 and 65535", port)
		}
		config.WebDAV.Port = port
		fmt.Printf("Using PORT from environment variable: %d\n", port)
	}

	// Validate configuration
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return config, nil
}

// GetConfigFilePath returns the configuration file path used by viper
func GetConfigFilePath() string {
	return viper.ConfigFileUsed()
}
