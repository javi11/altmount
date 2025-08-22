package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/nntppool"
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
			ID:             p.ID,
			Host:           p.Host,
			Port:           p.Port,
			Username:       p.Username,
			MaxConnections: p.MaxConnections,
			TLS:            p.TLS,
			InsecureTLS:    p.InsecureTLS,
			PasswordSet:    p.Password != "",
			Enabled:        p.Enabled,
		}
	}

	return &ConfigResponse{
		WebDAV: WebDAVConfigResponse{
			Port:     config.WebDAV.Port,
			User:     config.WebDAV.User,
			Password: config.WebDAV.Password, // Include password for admin editing
			Debug:    config.WebDAV.Debug,
		},
		API: APIConfigResponse{
			Prefix: "/api", // Always hardcoded to /api
		},
		Database: DatabaseConfigResponse{
			Path: config.Database.Path,
		},
		Metadata: MetadataConfigResponse{
			RootPath: config.Metadata.RootPath,
		},
		Streaming: StreamingConfigResponse{
			MaxRangeSize:       config.Streaming.MaxRangeSize,
			StreamingChunkSize: config.Streaming.StreamingChunkSize,
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
	}

	if updates.Streaming != nil {
		if updates.Streaming.MaxRangeSize != nil {
			cfg.Streaming.MaxRangeSize = *updates.Streaming.MaxRangeSize
		}
		if updates.Streaming.StreamingChunkSize != nil {
			cfg.Streaming.StreamingChunkSize = *updates.Streaming.StreamingChunkSize
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
			if p.ID != nil {
				provider.ID = *p.ID
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
			if p.Enabled != nil {
				provider.Enabled = *p.Enabled
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
		}
	case "streaming":
		if updates.Streaming != nil {
			if updates.Streaming.MaxRangeSize != nil {
				cfg.Streaming.MaxRangeSize = *updates.Streaming.MaxRangeSize
			}
			if updates.Streaming.StreamingChunkSize != nil {
				cfg.Streaming.StreamingChunkSize = *updates.Streaming.StreamingChunkSize
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

// handleTestProvider tests NNTP provider connectivity
func (s *Server) handleTestProvider(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var testReq ProviderTestRequest
	if err := json.NewDecoder(r.Body).Decode(&testReq); err != nil {
		WriteValidationError(w, "Invalid JSON in request body", err.Error())
		return
	}

	// Test provider connectivity using nntppool
	start := time.Now()
	err := nntppool.TestProviderConnectivity(ctx, nntppool.UsenetProviderConfig{
		Host:        testReq.Host,
		Port:        testReq.Port,
		Username:    testReq.Username,
		Password:    testReq.Password,
		TLS:         testReq.TLS,
		InsecureSSL: testReq.InsecureTLS,
	}, nil, nil)
	latency := time.Since(start).Milliseconds()

	response := ProviderTestResponse{
		Success: err == nil,
		Latency: latency,
	}

	if err != nil {
		response.ErrorMessage = err.Error()
	}

	WriteSuccess(w, response, nil)
}

// handleCreateProvider creates a new NNTP provider
func (s *Server) handleCreateProvider(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if s.configManager == nil {
		WriteInternalError(w, "Configuration management not available", "CONFIG_UNAVAILABLE")
		return
	}

	var createReq ProviderCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&createReq); err != nil {
		WriteValidationError(w, "Invalid JSON in request body", err.Error())
		return
	}

	// Test provider connectivity before creating
	err := nntppool.TestProviderConnectivity(ctx, nntppool.UsenetProviderConfig{
		Host:        createReq.Host,
		Port:        createReq.Port,
		Username:    createReq.Username,
		Password:    createReq.Password,
		TLS:         createReq.TLS,
		InsecureSSL: createReq.InsecureTLS,
	}, nil, nil)
	if err != nil {
		WriteValidationError(w, "Provider connectivity test failed", err.Error())
		return
	}

	// Get current config
	currentConfig := s.configManager.GetConfig()
	if currentConfig == nil {
		WriteInternalError(w, "Configuration not available", "CONFIG_NOT_FOUND")
		return
	}

	// Create new provider with hash-generated ID
	providerID := config.GenerateProviderID(createReq.Host, createReq.Port, createReq.Username)
	newProvider := config.ProviderConfig{
		ID:             providerID,
		Host:           createReq.Host,
		Port:           createReq.Port,
		Username:       createReq.Username,
		Password:       createReq.Password,
		MaxConnections: createReq.MaxConnections,
		TLS:            createReq.TLS,
		InsecureTLS:    createReq.InsecureTLS,
		Enabled:        createReq.Enabled,
	}

	// Create a copy of current config and add new provider
	newConfig := *currentConfig
	newConfig.Providers = append(newConfig.Providers, newProvider)

	// Validate the new configuration
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

	// Return the new provider
	providerResponse := ProviderConfigResponse{
		ID:             newProvider.ID,
		Host:           newProvider.Host,
		Port:           newProvider.Port,
		Username:       newProvider.Username,
		MaxConnections: newProvider.MaxConnections,
		TLS:            newProvider.TLS,
		InsecureTLS:    newProvider.InsecureTLS,
		PasswordSet:    newProvider.Password != "",
		Enabled:        newProvider.Enabled,
	}

	WriteSuccess(w, providerResponse, nil)
}

// handleUpdateProvider updates an existing NNTP provider
func (s *Server) handleUpdateProvider(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if s.configManager == nil {
		WriteInternalError(w, "Configuration management not available", "CONFIG_UNAVAILABLE")
		return
	}

	// Extract provider ID from URL path parameter
	providerID := r.PathValue("id")
	if providerID == "" {
		WriteValidationError(w, "Provider ID is required", "MISSING_PROVIDER_ID")
		return
	}

	var updateReq ProviderUpdateRequest
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

	// Find the provider to update
	newConfig := *currentConfig
	var updatedProvider *config.ProviderConfig
	for i := range newConfig.Providers {
		if newConfig.Providers[i].ID == providerID {
			updatedProvider = &newConfig.Providers[i]
			break
		}
	}

	if updatedProvider == nil {
		WriteNotFound(w, "Provider not found", "PROVIDER_NOT_FOUND")
		return
	}

	// Apply updates
	hostChanged := false
	portChanged := false
	usernameChanged := false
	
	if updateReq.Host != nil {
		updatedProvider.Host = *updateReq.Host
		hostChanged = true
	}
	if updateReq.Port != nil {
		updatedProvider.Port = *updateReq.Port
		portChanged = true
	}
	if updateReq.Username != nil {
		updatedProvider.Username = *updateReq.Username
		usernameChanged = true
	}
	if updateReq.Password != nil {
		updatedProvider.Password = *updateReq.Password
	}
	if updateReq.MaxConnections != nil {
		updatedProvider.MaxConnections = *updateReq.MaxConnections
	}
	if updateReq.TLS != nil {
		updatedProvider.TLS = *updateReq.TLS
	}
	if updateReq.InsecureTLS != nil {
		updatedProvider.InsecureTLS = *updateReq.InsecureTLS
	}
	if updateReq.Enabled != nil {
		updatedProvider.Enabled = *updateReq.Enabled
	}

	// Regenerate ID if any identifying fields changed
	if hostChanged || portChanged || usernameChanged {
		updatedProvider.ID = config.GenerateProviderID(updatedProvider.Host, updatedProvider.Port, updatedProvider.Username)
	}

	// Test provider connectivity if connection details changed
	if updateReq.Host != nil || updateReq.Port != nil || updateReq.Username != nil ||
		updateReq.Password != nil || updateReq.TLS != nil || updateReq.InsecureTLS != nil {
		err := nntppool.TestProviderConnectivity(ctx, nntppool.UsenetProviderConfig{
			Host:        updatedProvider.Host,
			Port:        updatedProvider.Port,
			Username:    updatedProvider.Username,
			Password:    updatedProvider.Password,
			TLS:         updatedProvider.TLS,
			InsecureSSL: updatedProvider.InsecureTLS,
		}, nil, nil)
		if err != nil {
			WriteValidationError(w, "Provider connectivity test failed", err.Error())
			return
		}
	}

	// Validate the new configuration
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

	// Return the updated provider
	providerResponse := ProviderConfigResponse{
		ID:             updatedProvider.ID,
		Host:           updatedProvider.Host,
		Port:           updatedProvider.Port,
		Username:       updatedProvider.Username,
		MaxConnections: updatedProvider.MaxConnections,
		TLS:            updatedProvider.TLS,
		InsecureTLS:    updatedProvider.InsecureTLS,
		PasswordSet:    updatedProvider.Password != "",
		Enabled:        updatedProvider.Enabled,
	}

	WriteSuccess(w, providerResponse, nil)
}

// handleDeleteProvider deletes an NNTP provider
func (s *Server) handleDeleteProvider(w http.ResponseWriter, r *http.Request) {
	if s.configManager == nil {
		WriteInternalError(w, "Configuration management not available", "CONFIG_UNAVAILABLE")
		return
	}

	// Extract provider ID from URL path parameter
	providerID := r.PathValue("id")
	if providerID == "" {
		WriteValidationError(w, "Provider ID is required", "MISSING_PROVIDER_ID")
		return
	}

	// Get current config
	currentConfig := s.configManager.GetConfig()
	if currentConfig == nil {
		WriteInternalError(w, "Configuration not available", "CONFIG_NOT_FOUND")
		return
	}

	// Find and remove the provider
	newConfig := *currentConfig
	var providerIndex = -1
	for i, provider := range newConfig.Providers {
		if provider.ID == providerID {
			providerIndex = i
			break
		}
	}

	if providerIndex == -1 {
		WriteNotFound(w, "Provider not found", "PROVIDER_NOT_FOUND")
		return
	}

	// Remove provider from slice
	newConfig.Providers = append(newConfig.Providers[:providerIndex], newConfig.Providers[providerIndex+1:]...)

	// Validate the new configuration
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

	WriteSuccess(w, map[string]string{"message": "Provider deleted successfully"}, nil)
}

// handleReorderProviders reorders NNTP providers
func (s *Server) handleReorderProviders(w http.ResponseWriter, r *http.Request) {
	if s.configManager == nil {
		WriteInternalError(w, "Configuration management not available", "CONFIG_UNAVAILABLE")
		return
	}

	var reorderReq ProviderReorderRequest
	if err := json.NewDecoder(r.Body).Decode(&reorderReq); err != nil {
		WriteValidationError(w, "Invalid JSON in request body", err.Error())
		return
	}

	// Validate request
	if len(reorderReq.ProviderIDs) == 0 {
		WriteValidationError(w, "Provider IDs array cannot be empty", "EMPTY_PROVIDER_IDS")
		return
	}

	// Get current config
	currentConfig := s.configManager.GetConfig()
	if currentConfig == nil {
		WriteInternalError(w, "Configuration not available", "CONFIG_NOT_FOUND")
		return
	}

	// Validate that all provided IDs exist and match current providers
	if len(reorderReq.ProviderIDs) != len(currentConfig.Providers) {
		WriteValidationError(w, "Provider IDs count must match existing providers count", "PROVIDER_COUNT_MISMATCH")
		return
	}

	// Create a map of current providers by ID
	providerMap := make(map[string]config.ProviderConfig)
	for _, provider := range currentConfig.Providers {
		providerMap[provider.ID] = provider
	}

	// Validate all IDs exist and create reordered slice
	reorderedProviders := make([]config.ProviderConfig, 0, len(reorderReq.ProviderIDs))
	for _, id := range reorderReq.ProviderIDs {
		if provider, exists := providerMap[id]; exists {
			reorderedProviders = append(reorderedProviders, provider)
		} else {
			WriteValidationError(w, fmt.Sprintf("Provider ID '%s' not found", id), "PROVIDER_NOT_FOUND")
			return
		}
	}

	// Create new configuration with reordered providers
	newConfig := *currentConfig
	newConfig.Providers = reorderedProviders

	// Validate the new configuration
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

	// Return updated provider list
	response := make([]ProviderConfigResponse, len(newConfig.Providers))
	for i, provider := range newConfig.Providers {
		response[i] = ProviderConfigResponse{
			ID:             provider.ID,
			Host:           provider.Host,
			Port:           provider.Port,
			Username:       provider.Username,
			MaxConnections: provider.MaxConnections,
			TLS:            provider.TLS,
			InsecureTLS:    provider.InsecureTLS,
			PasswordSet:    provider.Password != "",
			Enabled:        provider.Enabled,
		}
	}

	WriteSuccess(w, response, nil)
}
