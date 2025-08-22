package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/javi11/altmount/internal/config"
)

// ConfigManager interface defines methods for configuration management
type ConfigManager interface {
	GetConfig() *config.Config
	UpdateConfig(config *config.Config) error
	ValidateConfig(config *config.Config) error
	ValidateConfigUpdate(config *config.Config) error
	OnConfigChange(callback config.ChangeCallback)
	ReloadConfig() error
	SaveConfig() error
}

// handleGetConfig returns the current configuration
func (s *Server) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	if s.configManager == nil {
		WriteInternalError(w, "Configuration management not available", "CONFIG_UNAVAILABLE")
		return
	}

	config := s.configManager.GetConfig()
	if config == nil {
		WriteInternalError(w, "Configuration not available", "CONFIG_NOT_FOUND")
		return
	}

	response := s.toConfigResponse(config)
	WriteSuccess(w, response, nil)
}

// handleUpdateConfig updates the entire configuration
func (s *Server) handleUpdateConfig(w http.ResponseWriter, r *http.Request) {
	if s.configManager == nil {
		WriteInternalError(w, "Configuration management not available", "CONFIG_UNAVAILABLE")
		return
	}

	var updateReq ConfigUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&updateReq); err != nil {
		WriteValidationError(w, "Invalid JSON in request body", err.Error())
		return
	}

	// Get current config and apply updates
	currentConfig := s.configManager.GetConfig()
	if currentConfig == nil {
		WriteInternalError(w, "Configuration not available", "CONFIG_NOT_FOUND")
		return
	}

	// Create a copy of current config and apply updates
	newConfig := *currentConfig
	s.applyConfigUpdates(&newConfig, &updateReq)

	// Validate the new configuration with API restrictions
	if err := s.configManager.ValidateConfigUpdate(&newConfig); err != nil {
		WriteValidationError(w, "Configuration validation failed", err.Error())
		return
	}

	// Update the configuration
	if err := s.configManager.UpdateConfig(&newConfig); err != nil {
		WriteInternalError(w, "Failed to update configuration", err.Error())
		return
	}

	// Save to file
	if err := s.configManager.SaveConfig(); err != nil {
		WriteInternalError(w, "Failed to save configuration", err.Error())
		return
	}

	response := s.toConfigResponse(&newConfig)
	WriteSuccess(w, response, nil)
}

// handlePatchConfigSection updates a specific configuration section
func (s *Server) handlePatchConfigSection(w http.ResponseWriter, r *http.Request) {
	if s.configManager == nil {
		WriteInternalError(w, "Configuration management not available", "CONFIG_UNAVAILABLE")
		return
	}

	// Extract section from URL path parameter
	section := r.PathValue("section")
	if section == "" {
		WriteValidationError(w, "Invalid configuration section path", "INVALID_PATH")
		return
	}

	var updateReq ConfigUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&updateReq); err != nil {
		WriteValidationError(w, "Invalid JSON in request body", err.Error())
		return
	}

	// Get current config
	currentConfig := s.configManager.GetConfig()
	if currentConfig == nil {
		WriteInternalError(w, "Configuration not available", "CONFIG_NOT_FOUND")
		return
	}

	// Create a copy and apply section-specific updates
	newConfig := *currentConfig
	if err := s.applySectionUpdate(&newConfig, section, &updateReq); err != nil {
		WriteValidationError(w, "Invalid configuration section", err.Error())
		return
	}

	// Validate the new configuration with API restrictions
	if err := s.configManager.ValidateConfigUpdate(&newConfig); err != nil {
		WriteValidationError(w, "Configuration validation failed", err.Error())
		return
	}

	// Update the configuration
	if err := s.configManager.UpdateConfig(&newConfig); err != nil {
		WriteInternalError(w, "Failed to update configuration", err.Error())
		return
	}

	// Save to file
	if err := s.configManager.SaveConfig(); err != nil {
		WriteInternalError(w, "Failed to save configuration", err.Error())
		return
	}

	response := s.toConfigResponse(&newConfig)
	WriteSuccess(w, response, nil)
}

// handleReloadConfig reloads configuration from file
func (s *Server) handleReloadConfig(w http.ResponseWriter, r *http.Request) {
	if s.configManager == nil {
		WriteInternalError(w, "Configuration management not available", "CONFIG_UNAVAILABLE")
		return
	}

	if err := s.configManager.ReloadConfig(); err != nil {
		WriteInternalError(w, "Failed to reload configuration", err.Error())
		return
	}

	config := s.configManager.GetConfig()
	response := s.toConfigResponse(config)
	WriteSuccess(w, response, nil)
}

// handleValidateConfig validates configuration without applying changes
func (s *Server) handleValidateConfig(w http.ResponseWriter, r *http.Request) {
	if s.configManager == nil {
		WriteInternalError(w, "Configuration management not available", "CONFIG_UNAVAILABLE")
		return
	}

	var validateReq ConfigValidateRequest
	if err := json.NewDecoder(r.Body).Decode(&validateReq); err != nil {
		WriteValidationError(w, "Invalid JSON in request body", err.Error())
		return
	}

	// Convert interface{} to Config struct
	configJSON, err := json.Marshal(validateReq.Config)
	if err != nil {
		WriteValidationError(w, "Failed to process configuration", err.Error())
		return
	}

	var cfg config.Config
	if err := json.Unmarshal(configJSON, &cfg); err != nil {
		WriteValidationError(w, "Invalid configuration structure", err.Error())
		return
	}

	// Validate the configuration
	validationErr := s.configManager.ValidateConfig(&cfg)

	response := ConfigValidateResponse{
		Valid: validationErr == nil,
	}

	if validationErr != nil {
		response.Errors = []ConfigValidationError{
			{
				Field:   "config",
				Message: validationErr.Error(),
			},
		}
	}

	WriteSuccess(w, response, nil)
}

// toConfigResponse converts config.Config to ConfigResponse (sanitized)
func (s *Server) toConfigResponse(config *config.Config) *ConfigResponse {
	if config == nil {
		return nil
	}

	// Convert providers and sanitize passwords
	providers := make([]ProviderConfigResponse, len(config.Providers))
	for i, p := range config.Providers {
		providers[i] = ProviderConfigResponse{
			Name:           p.Name,
			Host:           p.Host,
			Port:           p.Port,
			Username:       p.Username,
			MaxConnections: p.MaxConnections,
			TLS:            p.TLS,
			InsecureTLS:    p.InsecureTLS,
			PasswordSet:    p.Password != "",
		}
	}

	return &ConfigResponse{
		WebDAV: WebDAVConfigResponse{
			Port:  config.WebDAV.Port,
			User:  config.WebDAV.User,
			Debug: config.WebDAV.Debug,
		},
		API: APIConfigResponse{
			Prefix: "/api", // Always hardcoded to /api
		},
		Database: DatabaseConfigResponse{
			Path: config.Database.Path,
		},
		Metadata: MetadataConfigResponse{
			RootPath:           config.Metadata.RootPath,
			MaxRangeSize:       config.Metadata.MaxRangeSize,
			StreamingChunkSize: config.Metadata.StreamingChunkSize,
		},
		WatchPath: config.WatchPath,
		RClone: RCloneConfigResponse{
			PasswordSet: config.RClone.Password != "",
			SaltSet:     config.RClone.Salt != "",
		},
		Workers: WorkersConfigResponse{
			Download:  config.Workers.Download,
			Processor: config.Workers.Processor,
		},
		Providers: providers,
		Debug:     config.Debug,
	}
}

// applyConfigUpdates applies updates from ConfigUpdateRequest to Config
func (s *Server) applyConfigUpdates(cfg *config.Config, updates *ConfigUpdateRequest) {
	if updates.WebDAV != nil {
		if updates.WebDAV.Port != nil {
			cfg.WebDAV.Port = *updates.WebDAV.Port
		}
		if updates.WebDAV.User != nil {
			cfg.WebDAV.User = *updates.WebDAV.User
		}
		if updates.WebDAV.Password != nil {
			cfg.WebDAV.Password = *updates.WebDAV.Password
		}
		if updates.WebDAV.Debug != nil {
			cfg.WebDAV.Debug = *updates.WebDAV.Debug
		}
	}

	if updates.API != nil {
		// API prefix is now hardcoded to /api and cannot be changed
		// No updates allowed for API configuration
	}

	if updates.Database != nil {
		if updates.Database.Path != nil {
			cfg.Database.Path = *updates.Database.Path
		}
	}

	if updates.Metadata != nil {
		if updates.Metadata.RootPath != nil {
			cfg.Metadata.RootPath = *updates.Metadata.RootPath
		}
		if updates.Metadata.MaxRangeSize != nil {
			cfg.Metadata.MaxRangeSize = *updates.Metadata.MaxRangeSize
		}
		if updates.Metadata.StreamingChunkSize != nil {
			cfg.Metadata.StreamingChunkSize = *updates.Metadata.StreamingChunkSize
		}
	}

	if updates.WatchPath != nil {
		cfg.WatchPath = *updates.WatchPath
	}

	if updates.RClone != nil {
		if updates.RClone.Password != nil {
			cfg.RClone.Password = *updates.RClone.Password
		}
		if updates.RClone.Salt != nil {
			cfg.RClone.Salt = *updates.RClone.Salt
		}
	}

	if updates.Workers != nil {
		if updates.Workers.Download != nil {
			cfg.Workers.Download = *updates.Workers.Download
		}
		if updates.Workers.Processor != nil {
			cfg.Workers.Processor = *updates.Workers.Processor
		}
	}

	if updates.Providers != nil {
		providers := make([]config.ProviderConfig, len(*updates.Providers))
		for i, p := range *updates.Providers {
			provider := config.ProviderConfig{}
			if p.Name != nil {
				provider.Name = *p.Name
			}
			if p.Host != nil {
				provider.Host = *p.Host
			}
			if p.Port != nil {
				provider.Port = *p.Port
			}
			if p.Username != nil {
				provider.Username = *p.Username
			}
			if p.Password != nil {
				provider.Password = *p.Password
			}
			if p.MaxConnections != nil {
				provider.MaxConnections = *p.MaxConnections
			}
			if p.TLS != nil {
				provider.TLS = *p.TLS
			}
			if p.InsecureTLS != nil {
				provider.InsecureTLS = *p.InsecureTLS
			}
			providers[i] = provider
		}
		cfg.Providers = providers
	}

	if updates.Debug != nil {
		cfg.Debug = *updates.Debug
	}
}

// applySectionUpdate applies section-specific updates
func (s *Server) applySectionUpdate(cfg *config.Config, section string, updates *ConfigUpdateRequest) error {
	switch section {
	case "webdav":
		if updates.WebDAV != nil {
			if updates.WebDAV.Port != nil {
				cfg.WebDAV.Port = *updates.WebDAV.Port
			}
			if updates.WebDAV.User != nil {
				cfg.WebDAV.User = *updates.WebDAV.User
			}
			if updates.WebDAV.Password != nil {
				cfg.WebDAV.Password = *updates.WebDAV.Password
			}
			if updates.WebDAV.Debug != nil {
				cfg.WebDAV.Debug = *updates.WebDAV.Debug
			}
		}
	case "api":
		if updates.API != nil {
			// API is always enabled and prefix is hardcoded to /api
			// No configuration changes allowed
		}
	case "database":
		if updates.Database != nil {
			if updates.Database.Path != nil {
				cfg.Database.Path = *updates.Database.Path
			}
		}
	case "metadata":
		if updates.Metadata != nil {
			if updates.Metadata.RootPath != nil {
				cfg.Metadata.RootPath = *updates.Metadata.RootPath
			}
			if updates.Metadata.MaxRangeSize != nil {
				cfg.Metadata.MaxRangeSize = *updates.Metadata.MaxRangeSize
			}
			if updates.Metadata.StreamingChunkSize != nil {
				cfg.Metadata.StreamingChunkSize = *updates.Metadata.StreamingChunkSize
			}
		}
	case "rclone":
		if updates.RClone != nil {
			if updates.RClone.Password != nil {
				cfg.RClone.Password = *updates.RClone.Password
			}
			if updates.RClone.Salt != nil {
				cfg.RClone.Salt = *updates.RClone.Salt
			}
		}
	case "workers":
		if updates.Workers != nil {
			if updates.Workers.Download != nil {
				cfg.Workers.Download = *updates.Workers.Download
			}
			if updates.Workers.Processor != nil {
				cfg.Workers.Processor = *updates.Workers.Processor
			}
		}
	case "providers":
		if updates.Providers != nil {
			providers := make([]config.ProviderConfig, len(*updates.Providers))
			for i, p := range *updates.Providers {
				provider := config.ProviderConfig{}
				if p.Name != nil {
					provider.Name = *p.Name
				}
				if p.Host != nil {
					provider.Host = *p.Host
				}
				if p.Port != nil {
					provider.Port = *p.Port
				}
				if p.Username != nil {
					provider.Username = *p.Username
				}
				if p.Password != nil {
					provider.Password = *p.Password
				}
				if p.MaxConnections != nil {
					provider.MaxConnections = *p.MaxConnections
				}
				if p.TLS != nil {
					provider.TLS = *p.TLS
				}
				if p.InsecureTLS != nil {
					provider.InsecureTLS = *p.InsecureTLS
				}
				providers[i] = provider
			}
			cfg.Providers = providers
		}
	default:
		return fmt.Errorf("invalid section: %s", section)
	}
	return nil
}
