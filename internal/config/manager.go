package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/javi11/nntppool"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

// Config represents the complete application configuration
type Config struct {
	WebDAV    WebDAVConfig     `yaml:"webdav" mapstructure:"webdav"`
	API       APIConfig        `yaml:"api" mapstructure:"api"`
	Database  DatabaseConfig   `yaml:"database" mapstructure:"database"`
	Metadata  MetadataConfig   `yaml:"metadata" mapstructure:"metadata"`
	WatchPath string           `yaml:"watch_path" mapstructure:"watch_path"`
	RClone    RCloneConfig     `yaml:"rclone" mapstructure:"rclone"`
	Workers   WorkersConfig    `yaml:"workers" mapstructure:"workers"`
	Providers []ProviderConfig `yaml:"providers" mapstructure:"providers"`
	Debug     bool             `yaml:"debug" mapstructure:"debug"`
}

// WebDAVConfig represents WebDAV server configuration
type WebDAVConfig struct {
	Port     int    `yaml:"port" mapstructure:"port"`
	User     string `yaml:"user" mapstructure:"user"`
	Password string `yaml:"password" mapstructure:"password"`
	Debug    bool   `yaml:"debug" mapstructure:"debug"`
}

// APIConfig represents REST API configuration
type APIConfig struct {
	Prefix string `yaml:"prefix" mapstructure:"prefix"`
}

// DatabaseConfig represents database configuration
type DatabaseConfig struct {
	Path string `yaml:"path" mapstructure:"path"`
}

// MetadataConfig represents metadata filesystem configuration
type MetadataConfig struct {
	RootPath           string `yaml:"root_path" mapstructure:"root_path"`
	MaxRangeSize       int64  `yaml:"max_range_size" mapstructure:"max_range_size"`
	StreamingChunkSize int64  `yaml:"streaming_chunk_size" mapstructure:"streaming_chunk_size"`
}

// RCloneConfig represents rclone configuration
type RCloneConfig struct {
	Password string `yaml:"password" mapstructure:"password"`
	Salt     string `yaml:"salt" mapstructure:"salt"`
}

// WorkersConfig represents worker configuration
type WorkersConfig struct {
	Download  int `yaml:"download" mapstructure:"download"`
	Processor int `yaml:"processor" mapstructure:"processor"`
}

// ProviderConfig represents a single NNTP provider configuration
type ProviderConfig struct {
	Name           string `yaml:"name" mapstructure:"name"`
	Host           string `yaml:"host" mapstructure:"host"`
	Port           int    `yaml:"port" mapstructure:"port"`
	Username       string `yaml:"username" mapstructure:"username"`
	Password       string `yaml:"password" mapstructure:"password"`
	MaxConnections int    `yaml:"max_connections" mapstructure:"max_connections"`
	TLS            bool   `yaml:"tls" mapstructure:"tls"`
	InsecureTLS    bool   `yaml:"insecure_tls" mapstructure:"insecure_tls"`
}

// Validate validates the configuration
func (c *Config) Validate() error {
	if c.WebDAV.Port <= 0 || c.WebDAV.Port > 65535 {
		return fmt.Errorf("webdav port must be between 1 and 65535")
	}

	if c.Workers.Download <= 0 {
		return fmt.Errorf("download workers must be greater than 0")
	}

	if c.Workers.Processor <= 0 {
		return fmt.Errorf("processor workers must be greater than 0")
	}

	// Validate metadata configuration (now required)
	if c.Metadata.RootPath == "" {
		return fmt.Errorf("metadata root_path cannot be empty")
	}

	if c.Metadata.MaxRangeSize < 0 {
		return fmt.Errorf("metadata max_range_size must be non-negative")
	}

	if c.Metadata.StreamingChunkSize < 0 {
		return fmt.Errorf("metadata streaming_chunk_size must be non-negative")
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

// ToNNTPProviders converts ProviderConfig slice to nntppool.UsenetProviderConfig slice
func (c *Config) ToNNTPProviders() []nntppool.UsenetProviderConfig {
	providers := make([]nntppool.UsenetProviderConfig, len(c.Providers))
	for i, p := range c.Providers {
		providers[i] = nntppool.UsenetProviderConfig{
			Host:                           p.Host,
			Port:                           p.Port,
			Username:                       p.Username,
			Password:                       p.Password,
			MaxConnections:                 p.MaxConnections,
			MaxConnectionIdleTimeInSeconds: 300, // Default idle timeout
			TLS:                            p.TLS,
			InsecureSSL:                    p.InsecureTLS,
			MaxConnectionTTLInSeconds:      3600, // Default connection TTL
		}
	}
	return providers
}

// ChangeCallback represents a function called when configuration changes
type ChangeCallback func(oldConfig, newConfig *Config)

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

// UpdateConfig updates the current configuration (thread-safe)
func (m *Manager) UpdateConfig(config *Config) error {
	m.mutex.Lock()
	oldConfig := m.current
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

// DefaultConfig returns a config with default values
func DefaultConfig() *Config {
	return &Config{
		WebDAV: WebDAVConfig{
			Port:     8080,
			User:     "usenet",
			Password: "usenet",
			Debug:    false,
		},
		API: APIConfig{
			Prefix: "/api",
		},
		Database: DatabaseConfig{
			Path: "altmount.db",
		},
		Metadata: MetadataConfig{
			RootPath:           "./metadata",
			MaxRangeSize:       33554432, // 32MB - Maximum range size for a single request
			StreamingChunkSize: 8388608,  // 8MB - Chunk size for streaming when end=-1
		},
		RClone: RCloneConfig{
			Password: "",
			Salt:     "",
		},
		Workers: WorkersConfig{
			Download:  15,
			Processor: 2,
		},
		Providers: []ProviderConfig{},
		Debug:     false,
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

	if configFile != "" {
		viper.SetConfigFile(configFile)
	} else {
		// Look for config file in common locations
		viper.SetConfigName("config")
		viper.SetConfigType("yaml")
	}

	// Read the configuration file
	if err := viper.ReadInConfig(); err != nil {
		if configFile != "" {
			// If a specific config file was provided but couldn't be read, return error
			return nil, fmt.Errorf("error reading config file %s: %w", configFile, err)
		}
		// No config file found - return helpful error
		return nil, fmt.Errorf("no configuration file found. Please create config.yaml or use --config flag")
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
