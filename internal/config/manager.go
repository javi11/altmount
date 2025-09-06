package config

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/javi11/nntppool"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

// Config represents the complete application configuration
type Config struct {
	WebDAV    WebDAVConfig     `yaml:"webdav" mapstructure:"webdav" json:"webdav"`
	API       APIConfig        `yaml:"api" mapstructure:"api" json:"api"`
	Database  DatabaseConfig   `yaml:"database" mapstructure:"database" json:"database"`
	Metadata  MetadataConfig   `yaml:"metadata" mapstructure:"metadata" json:"metadata"`
	Streaming StreamingConfig  `yaml:"streaming" mapstructure:"streaming" json:"streaming"`
	Health    HealthConfig     `yaml:"health" mapstructure:"health" json:"health,omitempty"`
	RClone    RCloneConfig     `yaml:"rclone" mapstructure:"rclone" json:"rclone"`
	Import    ImportConfig     `yaml:"import" mapstructure:"import" json:"import"`
	Log       LogConfig        `yaml:"log" mapstructure:"log" json:"log,omitempty"`
	SABnzbd   SABnzbdConfig    `yaml:"sabnzbd" mapstructure:"sabnzbd" json:"sabnzbd"`
	Arrs      ArrsConfig       `yaml:"arrs" mapstructure:"arrs" json:"arrs"`
	Providers []ProviderConfig `yaml:"providers" mapstructure:"providers" json:"providers"`
	LogLevel  string           `yaml:"log_level" mapstructure:"log_level" json:"log_level"`
}

// WebDAVConfig represents WebDAV server configuration
type WebDAVConfig struct {
	Port     int    `yaml:"port" mapstructure:"port" json:"port"`
	User     string `yaml:"user" mapstructure:"user" json:"user"`
	Password string `yaml:"password" mapstructure:"password" json:"password"`
}

// APIConfig represents REST API configuration
type APIConfig struct {
	Prefix string `yaml:"prefix" mapstructure:"prefix" json:"prefix"`
}

// DatabaseConfig represents database configuration
type DatabaseConfig struct {
	Path string `yaml:"path" mapstructure:"path" json:"path"`
}

// MetadataConfig represents metadata filesystem configuration
type MetadataConfig struct {
	RootPath string `yaml:"root_path" mapstructure:"root_path" json:"root_path"`
}

// StreamingConfig represents streaming and chunking configuration
type StreamingConfig struct {
	MaxRangeSize       int64 `yaml:"max_range_size" mapstructure:"max_range_size" json:"max_range_size"`
	StreamingChunkSize int64 `yaml:"streaming_chunk_size" mapstructure:"streaming_chunk_size" json:"streaming_chunk_size"`
	MaxDownloadWorkers int   `yaml:"max_download_workers" mapstructure:"max_download_workers" json:"max_download_workers"`
}

// RCloneConfig represents rclone configuration
type RCloneConfig struct {
	Password   string `yaml:"password" mapstructure:"password" json:"-"`
	Salt       string `yaml:"salt" mapstructure:"salt" json:"-"`
	VFSEnabled *bool  `yaml:"vfs_enabled" mapstructure:"vfs_enabled" json:"vfs_enabled"`
	VFSUrl     string `yaml:"vfs_url" mapstructure:"vfs_url" json:"vfs_url"`
	VFSUser    string `yaml:"vfs_user" mapstructure:"vfs_user" json:"vfs_user"`
	VFSPass    string `yaml:"vfs_pass" mapstructure:"vfs_pass" json:"-"`
}

// ImportConfig represents import processing configuration
type ImportConfig struct {
	MaxProcessorWorkers     int           `yaml:"max_processor_workers" mapstructure:"max_processor_workers" json:"max_processor_workers"`
	QueueProcessingInterval time.Duration `yaml:"queue_processing_interval" mapstructure:"queue_processing_interval" json:"queue_processing_interval"`
}

// UnmarshalYAML implements custom YAML unmarshaling for ImportConfig to handle queue_processing_interval as seconds
func (ic *ImportConfig) UnmarshalYAML(unmarshal func(interface{}) error) error {
	// Define a temporary struct to handle raw parsing with queue_processing_interval as seconds
	type rawImportConfig struct {
		MaxProcessorWorkers     int `yaml:"max_processor_workers"`
		QueueProcessingInterval int `yaml:"queue_processing_interval"` // Parse as seconds, convert to Duration
	}

	var raw rawImportConfig
	if err := unmarshal(&raw); err != nil {
		return err
	}

	ic.MaxProcessorWorkers = raw.MaxProcessorWorkers
	ic.QueueProcessingInterval = time.Duration(raw.QueueProcessingInterval) * time.Second
	return nil
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
	Enabled               *bool         `yaml:"enabled" mapstructure:"enabled" json:"enabled,omitempty"`
	AutoRepairEnabled     *bool         `yaml:"auto_repair_enabled" mapstructure:"auto_repair_enabled" json:"auto_repair_enabled,omitempty"`
	CheckInterval         time.Duration `yaml:"check_interval" mapstructure:"check_interval" json:"check_interval,omitempty"`
	MaxConcurrentJobs     int           `yaml:"max_concurrent_jobs" mapstructure:"max_concurrent_jobs" json:"max_concurrent_jobs,omitempty"`
	MaxRetries            int           `yaml:"max_retries" mapstructure:"max_retries" json:"max_retries,omitempty"`
	MaxSegmentConnections int           `yaml:"max_segment_connections" mapstructure:"max_segment_connections" json:"max_segment_connections,omitempty"`
	CheckAllSegments      bool          `yaml:"check_all_segments" mapstructure:"check_all_segments" json:"check_all_segments,omitempty"`
}

// GenerateProviderID creates a unique ID based on host, port, and username
func GenerateProviderID(host string, port int, username string) string {
	input := fmt.Sprintf("%s:%d@%s", host, port, username)
	hash := sha256.Sum256([]byte(input))
	return fmt.Sprintf("%x", hash)[:8] // First 8 characters for readability
}

// ProviderConfig represents a single NNTP provider configuration
type ProviderConfig struct {
	ID               string `yaml:"id" mapstructure:"id" json:"id"`
	Host             string `yaml:"host" mapstructure:"host" json:"host"`
	Port             int    `yaml:"port" mapstructure:"port" json:"port"`
	Username         string `yaml:"username" mapstructure:"username" json:"username"`
	Password         string `yaml:"password" mapstructure:"password" json:"-"`
	MaxConnections   int    `yaml:"max_connections" mapstructure:"max_connections" json:"max_connections"`
	TLS              bool   `yaml:"tls" mapstructure:"tls" json:"tls"`
	InsecureTLS      bool   `yaml:"insecure_tls" mapstructure:"insecure_tls" json:"insecure_tls"`
	Enabled          *bool  `yaml:"enabled" mapstructure:"enabled" json:"enabled,omitempty"`
	IsBackupProvider *bool  `yaml:"is_backup_provider" mapstructure:"is_backup_provider" json:"is_backup_provider,omitempty"`
}

// SABnzbdConfig represents SABnzbd-compatible API configuration
type SABnzbdConfig struct {
	Enabled    *bool             `yaml:"enabled" mapstructure:"enabled" json:"enabled"`
	MountDir   string            `yaml:"mount_dir" mapstructure:"mount_dir" json:"mount_dir"`
	Categories []SABnzbdCategory `yaml:"categories" mapstructure:"categories" json:"categories"`
}

// SABnzbdCategory represents a SABnzbd category configuration
type SABnzbdCategory struct {
	Name     string `yaml:"name" mapstructure:"name" json:"name"`
	Order    int    `yaml:"order" mapstructure:"order" json:"order"`
	Priority int    `yaml:"priority" mapstructure:"priority" json:"priority"`
	Dir      string `yaml:"dir" mapstructure:"dir" json:"dir"`
}

// ArrsConfig represents arrs configuration
type ArrsConfig struct {
	Enabled         *bool                `yaml:"enabled" mapstructure:"enabled" json:"enabled"`
	MaxWorkers      int                  `yaml:"max_workers" mapstructure:"max_workers" json:"max_workers,omitempty"`
	MountPath       string               `yaml:"mount_path" mapstructure:"mount_path" json:"mount_path"`
	RadarrInstances []ArrsInstanceConfig `yaml:"radarr_instances" mapstructure:"radarr_instances" json:"radarr_instances"`
	SonarrInstances []ArrsInstanceConfig `yaml:"sonarr_instances" mapstructure:"sonarr_instances" json:"sonarr_instances"`
}

// ArrsInstanceConfig represents a single arrs instance configuration
type ArrsInstanceConfig struct {
	Name              string `yaml:"name" mapstructure:"name" json:"name"`
	URL               string `yaml:"url" mapstructure:"url" json:"url"`
	APIKey            string `yaml:"api_key" mapstructure:"api_key" json:"api_key"`
	Enabled           *bool  `yaml:"enabled" mapstructure:"enabled" json:"enabled,omitempty"`
	SyncIntervalHours *int   `yaml:"sync_interval_hours" mapstructure:"sync_interval_hours" json:"sync_interval_hours,omitempty"`
}

// DeepCopy returns a deep copy of the configuration
func (c *Config) DeepCopy() *Config {
	if c == nil {
		return nil
	}

	// Start with a shallow copy of value fields
	copyCfg := *c

	// Deep copy Health.Enabled pointer
	if c.Health.Enabled != nil {
		v := *c.Health.Enabled
		copyCfg.Health.Enabled = &v
	} else {
		copyCfg.Health.Enabled = nil
	}

	// Deep copy RClone.VFSEnabled pointer
	if c.RClone.VFSEnabled != nil {
		v := *c.RClone.VFSEnabled
		copyCfg.RClone.VFSEnabled = &v
	} else {
		copyCfg.RClone.VFSEnabled = nil
	}

	// Deep copy Providers slice and their pointer fields
	if c.Providers != nil {
		copyCfg.Providers = make([]ProviderConfig, len(c.Providers))
		for i, p := range c.Providers {
			pc := p // copy struct value
			if p.Enabled != nil {
				ev := *p.Enabled
				pc.Enabled = &ev
			} else {
				pc.Enabled = nil
			}
			if p.IsBackupProvider != nil {
				bv := *p.IsBackupProvider
				pc.IsBackupProvider = &bv
			} else {
				pc.IsBackupProvider = nil
			}
			copyCfg.Providers[i] = pc
		}
	} else {
		copyCfg.Providers = nil
	}

	// Deep copy SABnzbd.Enabled pointer
	if c.SABnzbd.Enabled != nil {
		v := *c.SABnzbd.Enabled
		copyCfg.SABnzbd.Enabled = &v
	} else {
		copyCfg.SABnzbd.Enabled = nil
	}

	// Deep copy SABnzbd Categories slice
	if c.SABnzbd.Categories != nil {
		copyCfg.SABnzbd.Categories = make([]SABnzbdCategory, len(c.SABnzbd.Categories))
		copy(copyCfg.SABnzbd.Categories, c.SABnzbd.Categories)
	} else {
		copyCfg.SABnzbd.Categories = nil
	}

	// Deep copy Arrs.Enabled pointer
	if c.Arrs.Enabled != nil {
		v := *c.Arrs.Enabled
		copyCfg.Arrs.Enabled = &v
	} else {
		copyCfg.Arrs.Enabled = nil
	}

	// Deep copy Scraper Radarr instances
	if c.Arrs.RadarrInstances != nil {
		copyCfg.Arrs.RadarrInstances = make([]ArrsInstanceConfig, len(c.Arrs.RadarrInstances))
		for i, inst := range c.Arrs.RadarrInstances {
			ic := inst // copy struct value
			if inst.Enabled != nil {
				ev := *inst.Enabled
				ic.Enabled = &ev
			} else {
				ic.Enabled = nil
			}
			if inst.SyncIntervalHours != nil {
				iv := *inst.SyncIntervalHours
				ic.SyncIntervalHours = &iv
			} else {
				ic.SyncIntervalHours = nil
			}

			copyCfg.Arrs.RadarrInstances[i] = ic
		}
	} else {
		copyCfg.Arrs.RadarrInstances = nil
	}

	// Deep copy Scraper Sonarr instances
	if c.Arrs.SonarrInstances != nil {
		copyCfg.Arrs.SonarrInstances = make([]ArrsInstanceConfig, len(c.Arrs.SonarrInstances))
		for i, inst := range c.Arrs.SonarrInstances {
			ic := inst // copy struct value
			if inst.Enabled != nil {
				ev := *inst.Enabled
				ic.Enabled = &ev
			} else {
				ic.Enabled = nil
			}
			if inst.SyncIntervalHours != nil {
				iv := *inst.SyncIntervalHours
				ic.SyncIntervalHours = &iv
			} else {
				ic.SyncIntervalHours = nil
			}

			copyCfg.Arrs.SonarrInstances[i] = ic
		}
	} else {
		copyCfg.Arrs.SonarrInstances = nil
	}

	return &copyCfg
}

// Validate validates the configuration
func (c *Config) Validate() error {
	if c.WebDAV.Port <= 0 || c.WebDAV.Port > 65535 {
		return fmt.Errorf("webdav port must be between 1 and 65535")
	}

	if c.Streaming.MaxDownloadWorkers <= 0 {
		return fmt.Errorf("streaming max_download_workers must be greater than 0")
	}

	if c.Import.MaxProcessorWorkers <= 0 {
		return fmt.Errorf("import max_processor_workers must be greater than 0")
	}

	if c.Import.QueueProcessingInterval < 1*time.Second {
		return fmt.Errorf("import queue_processing_interval must be at least 1 second")
	}

	if c.Import.QueueProcessingInterval > 5*time.Minute {
		return fmt.Errorf("import queue_processing_interval must not exceed 5 minutes")
	}

	// Validate log level (both old and new config)
	if c.LogLevel != "" {
		validLevels := []string{"debug", "info", "warn", "error"}
		isValid := false
		for _, level := range validLevels {
			if c.LogLevel == level {
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
	if c.Streaming.MaxRangeSize < 0 {
		return fmt.Errorf("streaming max_range_size must be non-negative")
	}

	if c.Streaming.StreamingChunkSize < 0 {
		return fmt.Errorf("streaming streaming_chunk_size must be non-negative")
	}

	// Validate health configuration
	if *c.Health.Enabled {
		if c.Health.CheckInterval <= 0 {
			return fmt.Errorf("health check_interval must be greater than 0")
		}
		if c.Health.MaxConcurrentJobs <= 0 {
			return fmt.Errorf("health max_concurrent_jobs must be greater than 0")
		}
		if c.Health.MaxRetries < 0 {
			return fmt.Errorf("health max_retries must be non-negative")
		}
		if c.Health.MaxSegmentConnections <= 0 {
			return fmt.Errorf("health max_segment_connections must be greater than 0")
		}
	}

	// Validate RClone VFS configuration
	if c.RClone.VFSEnabled != nil && *c.RClone.VFSEnabled {
		if c.RClone.VFSUrl == "" {
			return fmt.Errorf("rclone vfs_url cannot be empty when VFS is enabled")
		}
	}

	// Validate SABnzbd configuration
	if c.SABnzbd.Enabled != nil && *c.SABnzbd.Enabled {
		if c.SABnzbd.MountDir == "" {
			return fmt.Errorf("sabnzbd mount_dir cannot be empty when SABnzbd is enabled")
		}
		if !filepath.IsAbs(c.SABnzbd.MountDir) {
			return fmt.Errorf("sabnzbd mount_dir must be an absolute path")
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
	}

	// Validate scraper configuration
	if c.Arrs.Enabled != nil && *c.Arrs.Enabled {
		if c.Arrs.MountPath == "" {
			return fmt.Errorf("scraper mount_path cannot be empty when scraper is enabled")
		}
		if !filepath.IsAbs(c.Arrs.MountPath) {
			return fmt.Errorf("scraper mount_path must be an absolute path")
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
			oldProvider.Enabled != newProvider.Enabled ||
			oldProvider.IsBackupProvider != newProvider.IsBackupProvider {
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
				MaxConnectionIdleTimeInSeconds: 300, // Default idle timeout
				TLS:                            p.TLS,
				InsecureSSL:                    p.InsecureTLS,
				MaxConnectionTTLInSeconds:      3600, // Default connection TTL
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
	current    *Config
	configFile string
	mutex      sync.RWMutex
	callbacks  []ChangeCallback
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
func DefaultConfig() *Config {
	healthCheckEnabled := true
	autoRepairEnabled := false // Disabled by default for safety
	vfsEnabled := false
	sabnzbdEnabled := false
	scrapperEnabled := false

	// Set paths based on whether we're running in Docker
	dbPath := "./altmount.db"
	metadataPath := "./metadata"

	if isRunningInDocker() {
		dbPath = "/config/altmount.db"
		metadataPath = "/metadata"
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
		Database: DatabaseConfig{
			Path: dbPath,
		},
		Metadata: MetadataConfig{
			RootPath: metadataPath,
		},
		Streaming: StreamingConfig{
			MaxRangeSize:       33554432, // 32MB - Maximum range size for a single request
			StreamingChunkSize: 8388608,  // 8MB - Chunk size for streaming when end=-1
			MaxDownloadWorkers: 15,       // Default: 15 download workers
		},
		RClone: RCloneConfig{
			Password:   "",
			Salt:       "",
			VFSEnabled: &vfsEnabled,
			VFSUrl:     "",
			VFSUser:    "",
			VFSPass:    "",
		},
		Import: ImportConfig{
			MaxProcessorWorkers:     2,               // Default: 2 processor workers
			QueueProcessingInterval: 5 * time.Second, // Default: check for work every 5 seconds
		},
		Log: LogConfig{
			File:       "",     // Empty = console only
			Level:      "info", // Default log level
			MaxSize:    100,    // 100MB max size
			MaxAge:     30,     // Keep for 30 days
			MaxBackups: 10,     // Keep 10 old files
			Compress:   true,   // Compress old files
		},
		Health: HealthConfig{
			Enabled:               &healthCheckEnabled,
			AutoRepairEnabled:     &autoRepairEnabled,
			CheckInterval:         30 * time.Minute,
			MaxConcurrentJobs:     1,
			MaxRetries:            2,
			MaxSegmentConnections: 5,
			CheckAllSegments:      true,
		},
		SABnzbd: SABnzbdConfig{
			Enabled:    &sabnzbdEnabled,
			MountDir:   "",
			Categories: []SABnzbdCategory{},
		},
		Providers: []ProviderConfig{},
		LogLevel:  "info",
		Arrs: ArrsConfig{
			Enabled:         &scrapperEnabled, // Disabled by default
			MaxWorkers:      5,                // Default to 5 concurrent workers
			MountPath:       "",               // Empty by default - required when enabled
			RadarrInstances: []ArrsInstanceConfig{},
			SonarrInstances: []ArrsInstanceConfig{},
		},
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
			// Create default config file
			if err := SaveToFile(config, targetConfigFile); err != nil {
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
