package cmd

import (
	"fmt"

	"github.com/javi11/nntppool"
	"github.com/spf13/viper"
)

// Config represents the complete application configuration
type Config struct {
	WebDAV    WebDAVConfig     `yaml:"webdav" mapstructure:"webdav"`
	Database  DatabaseConfig   `yaml:"database" mapstructure:"database"`
	Metadata  MetadataConfig   `yaml:"metadata" mapstructure:"metadata"`
	MountPath string           `yaml:"mount_path" mapstructure:"mount_path"`
	NZBDir    string           `yaml:"nzb_dir" mapstructure:"nzb_dir"`
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

// DatabaseConfig represents database configuration (queue only)
type DatabaseConfig struct {
	QueuePath string `yaml:"queue_path" mapstructure:"queue_path"`
}

// MetadataConfig represents metadata filesystem configuration
type MetadataConfig struct {
	RootPath  string `yaml:"root_path" mapstructure:"root_path"`
	CacheSize int    `yaml:"cache_size" mapstructure:"cache_size"`
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

// DefaultConfig returns a config with default values
func DefaultConfig() *Config {
	return &Config{
		WebDAV: WebDAVConfig{
			Port:     8080,
			User:     "usenet",
			Password: "usenet",
			Debug:    false,
		},
		Database: DatabaseConfig{
			QueuePath: "altmount_queue.db",
		},
		Metadata: MetadataConfig{
			RootPath:  "./metadata",
			CacheSize: 1000, // Default cache size for metadata entries
		},
		MountPath: "/mnt/altmount",
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

	if c.Metadata.CacheSize < 0 {
		return fmt.Errorf("metadata cache_size must be non-negative")
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
