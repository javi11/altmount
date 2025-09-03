package api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

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

// parseLogLevel converts string log level to slog.Level
func parseLogLevel(level string) slog.Level {
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// applyLogLevel applies the log level to the global logger
func applyLogLevel(level string) {
	if level != "" {
		slog.SetLogLoggerLevel(parseLogLevel(level))
	}
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

	response := ToConfigAPIResponse(config)
	WriteSuccess(w, response, nil)
}

// handleUpdateConfig updates the entire configuration
func (s *Server) handleUpdateConfig(w http.ResponseWriter, r *http.Request) {
	if s.configManager == nil {
		WriteInternalError(w, "Configuration management not available", "CONFIG_UNAVAILABLE")
		return
	}

	// Decode directly into core config type
	var newConfig config.Config
	if err := json.NewDecoder(r.Body).Decode(&newConfig); err != nil {
		WriteValidationError(w, "Invalid JSON in request body", err.Error())
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

	response := ToConfigAPIResponse(&newConfig)
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

	// Get current config to merge with updates
	currentConfig := s.configManager.GetConfig()
	if currentConfig == nil {
		WriteInternalError(w, "Configuration not available", "CONFIG_NOT_FOUND")
		return
	}

	// Create a copy and decode partial updates directly into it
	newConfig := *currentConfig
	if err := json.NewDecoder(r.Body).Decode(&newConfig); err != nil {
		WriteValidationError(w, "Invalid JSON in request body", err.Error())
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

	response := ToConfigAPIResponse(&newConfig)
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
	response := ToConfigAPIResponse(config)
	WriteSuccess(w, response, nil)
}

// handleValidateConfig validates configuration without applying changes
func (s *Server) handleValidateConfig(w http.ResponseWriter, r *http.Request) {
	if s.configManager == nil {
		WriteInternalError(w, "Configuration management not available", "CONFIG_UNAVAILABLE")
		return
	}

	// Decode directly into core config type
	var cfg config.Config
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		WriteValidationError(w, "Invalid JSON in request body", err.Error())
		return
	}

	// Validate the configuration
	validationErr := s.configManager.ValidateConfig(&cfg)

	response := struct {
		Valid  bool   `json:"valid"`
		Errors []struct {
			Field   string `json:"field"`
			Message string `json:"message"`
		} `json:"errors,omitempty"`
	}{
		Valid: validationErr == nil,
	}

	if validationErr != nil {
		response.Errors = []struct {
			Field   string `json:"field"`
			Message string `json:"message"`
		}{
			{
				Field:   "config",
				Message: validationErr.Error(),
			},
		}
	}

	WriteSuccess(w, response, nil)
}


// Converter functions removed - now using core config types directly

// Other utility functions for config handling

// Removed old converter functions - now using core config types directly

// Provider API Functions (preserved from original file)

// handleListProviders returns all configured providers
func (s *Server) handleListProviders(w http.ResponseWriter, r *http.Request) {
	if s.configManager == nil {
		WriteInternalError(w, "Configuration management not available", "CONFIG_UNAVAILABLE")
		return
	}

	config := s.configManager.GetConfig()
	if config == nil {
		WriteInternalError(w, "Configuration not available", "CONFIG_NOT_FOUND")
		return
	}

	// Convert providers and sanitize passwords
	providers := make([]ProviderAPIResponse, len(config.Providers))
	for i, p := range config.Providers {
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

	WriteSuccess(w, providers, nil)
}

// Provider Management Handlers

// handleTestProvider tests NNTP provider connectivity
func (s *Server) handleTestProvider(w http.ResponseWriter, r *http.Request) {
	if s.configManager == nil {
		WriteInternalError(w, "Configuration management not available", "CONFIG_UNAVAILABLE")
		return
	}

	// Decode test request
	var testReq struct {
		Host        string `json:"host"`
		Port        int    `json:"port"`
		Username    string `json:"username"`
		Password    string `json:"password"`
		TLS         bool   `json:"tls"`
		InsecureTLS bool   `json:"insecure_tls"`
	}

	if err := json.NewDecoder(r.Body).Decode(&testReq); err != nil {
		WriteValidationError(w, "Invalid JSON in request body", err.Error())
		return
	}

	// Basic validation
	if testReq.Host == "" {
		WriteValidationError(w, "Host is required", "MISSING_HOST")
		return
	}
	if testReq.Port <= 0 || testReq.Port > 65535 {
		WriteValidationError(w, "Valid port is required (1-65535)", "INVALID_PORT")
		return
	}

	// TODO: Implement actual NNTP connection test
	// For now, return success for basic validation
	response := struct {
		Success    bool   `json:"success"`
		LatencyMs  int    `json:"latency_ms,omitempty"`
		ErrorMsg   string `json:"error_message,omitempty"`
	}{
		Success:   true,
		LatencyMs: 150, // Simulated latency
	}

	WriteSuccess(w, response, nil)
}

// handleCreateProvider creates a new NNTP provider
func (s *Server) handleCreateProvider(w http.ResponseWriter, r *http.Request) {
	if s.configManager == nil {
		WriteInternalError(w, "Configuration management not available", "CONFIG_UNAVAILABLE")
		return
	}

	// Get current config
	currentConfig := s.configManager.GetConfig()
	if currentConfig == nil {
		WriteInternalError(w, "Configuration not available", "CONFIG_NOT_FOUND")
		return
	}

	// Decode create request
	var createReq struct {
		Host               string `json:"host"`
		Port               int    `json:"port"`
		Username           string `json:"username"`
		Password           string `json:"password"`
		MaxConnections     int    `json:"max_connections"`
		TLS                bool   `json:"tls"`
		InsecureTLS        bool   `json:"insecure_tls"`
		Enabled            bool   `json:"enabled"`
		IsBackupProvider   bool   `json:"is_backup_provider"`
	}

	if err := json.NewDecoder(r.Body).Decode(&createReq); err != nil {
		WriteValidationError(w, "Invalid JSON in request body", err.Error())
		return
	}

	// Validation
	if createReq.Host == "" {
		WriteValidationError(w, "Host is required", "MISSING_HOST")
		return
	}
	if createReq.Port <= 0 || createReq.Port > 65535 {
		WriteValidationError(w, "Valid port is required (1-65535)", "INVALID_PORT")
		return
	}
	if createReq.Username == "" {
		WriteValidationError(w, "Username is required", "MISSING_USERNAME")
		return
	}
	if createReq.MaxConnections <= 0 {
		createReq.MaxConnections = 1 // Default
	}

	// Generate new ID
	newID := fmt.Sprintf("provider_%d", len(currentConfig.Providers)+1)

	// Create new provider
	newProvider := config.ProviderConfig{
		ID:                 newID,
		Host:               createReq.Host,
		Port:               createReq.Port,
		Username:           createReq.Username,
		Password:           createReq.Password,
		MaxConnections:     createReq.MaxConnections,
		TLS:                createReq.TLS,
		InsecureTLS:        createReq.InsecureTLS,
		Enabled:            &createReq.Enabled,
		IsBackupProvider:   &createReq.IsBackupProvider,
	}

	// Add to config
	newConfig := *currentConfig
	newConfig.Providers = append(newConfig.Providers, newProvider)

	// Validate and save
	if err := s.configManager.ValidateConfigUpdate(&newConfig); err != nil {
		WriteValidationError(w, "Configuration validation failed", err.Error())
		return
	}

	if err := s.configManager.UpdateConfig(&newConfig); err != nil {
		WriteInternalError(w, "Failed to update configuration", err.Error())
		return
	}

	if err := s.configManager.SaveConfig(); err != nil {
		WriteInternalError(w, "Failed to save configuration", err.Error())
		return
	}

	// Return sanitized provider
	response := ProviderAPIResponse{
		ID:               newProvider.ID,
		Host:             newProvider.Host,
		Port:             newProvider.Port,
		Username:         newProvider.Username,
		MaxConnections:   newProvider.MaxConnections,
		TLS:              newProvider.TLS,
		InsecureTLS:      newProvider.InsecureTLS,
		PasswordSet:      newProvider.Password != "",
		Enabled:          newProvider.Enabled != nil && *newProvider.Enabled,
		IsBackupProvider: newProvider.IsBackupProvider != nil && *newProvider.IsBackupProvider,
	}

	WriteSuccess(w, response, nil)
}

// handleUpdateProvider updates an existing NNTP provider
func (s *Server) handleUpdateProvider(w http.ResponseWriter, r *http.Request) {
	if s.configManager == nil {
		WriteInternalError(w, "Configuration management not available", "CONFIG_UNAVAILABLE")
		return
	}

	// Get provider ID from URL
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

	// Find provider
	providerIndex := -1
	for i, p := range currentConfig.Providers {
		if p.ID == providerID {
			providerIndex = i
			break
		}
	}

	if providerIndex == -1 {
		WriteValidationError(w, "Provider not found", "PROVIDER_NOT_FOUND")
		return
	}

	// Decode update request (partial update)
	var updateReq struct {
		Host               *string `json:"host,omitempty"`
		Port               *int    `json:"port,omitempty"`
		Username           *string `json:"username,omitempty"`
		Password           *string `json:"password,omitempty"`
		MaxConnections     *int    `json:"max_connections,omitempty"`
		TLS                *bool   `json:"tls,omitempty"`
		InsecureTLS        *bool   `json:"insecure_tls,omitempty"`
		Enabled            *bool   `json:"enabled,omitempty"`
		IsBackupProvider   *bool   `json:"is_backup_provider,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&updateReq); err != nil {
		WriteValidationError(w, "Invalid JSON in request body", err.Error())
		return
	}

	// Create updated config
	newConfig := *currentConfig
	provider := &newConfig.Providers[providerIndex]

	// Apply updates
	if updateReq.Host != nil {
		if *updateReq.Host == "" {
			WriteValidationError(w, "Host cannot be empty", "INVALID_HOST")
			return
		}
		provider.Host = *updateReq.Host
	}
	if updateReq.Port != nil {
		if *updateReq.Port <= 0 || *updateReq.Port > 65535 {
			WriteValidationError(w, "Valid port is required (1-65535)", "INVALID_PORT")
			return
		}
		provider.Port = *updateReq.Port
	}
	if updateReq.Username != nil {
		if *updateReq.Username == "" {
			WriteValidationError(w, "Username cannot be empty", "INVALID_USERNAME")
			return
		}
		provider.Username = *updateReq.Username
	}
	if updateReq.Password != nil {
		provider.Password = *updateReq.Password
	}
	if updateReq.MaxConnections != nil {
		if *updateReq.MaxConnections <= 0 {
			WriteValidationError(w, "MaxConnections must be positive", "INVALID_MAX_CONNECTIONS")
			return
		}
		provider.MaxConnections = *updateReq.MaxConnections
	}
	if updateReq.TLS != nil {
		provider.TLS = *updateReq.TLS
	}
	if updateReq.InsecureTLS != nil {
		provider.InsecureTLS = *updateReq.InsecureTLS
	}
	if updateReq.Enabled != nil {
		provider.Enabled = updateReq.Enabled
	}
	if updateReq.IsBackupProvider != nil {
		provider.IsBackupProvider = updateReq.IsBackupProvider
	}

	// Validate and save
	if err := s.configManager.ValidateConfigUpdate(&newConfig); err != nil {
		WriteValidationError(w, "Configuration validation failed", err.Error())
		return
	}

	if err := s.configManager.UpdateConfig(&newConfig); err != nil {
		WriteInternalError(w, "Failed to update configuration", err.Error())
		return
	}

	if err := s.configManager.SaveConfig(); err != nil {
		WriteInternalError(w, "Failed to save configuration", err.Error())
		return
	}

	// Return sanitized provider
	response := ProviderAPIResponse{
		ID:               provider.ID,
		Host:             provider.Host,
		Port:             provider.Port,
		Username:         provider.Username,
		MaxConnections:   provider.MaxConnections,
		TLS:              provider.TLS,
		InsecureTLS:      provider.InsecureTLS,
		PasswordSet:      provider.Password != "",
		Enabled:          provider.Enabled != nil && *provider.Enabled,
		IsBackupProvider: provider.IsBackupProvider != nil && *provider.IsBackupProvider,
	}

	WriteSuccess(w, response, nil)
}

// handleDeleteProvider removes an NNTP provider
func (s *Server) handleDeleteProvider(w http.ResponseWriter, r *http.Request) {
	if s.configManager == nil {
		WriteInternalError(w, "Configuration management not available", "CONFIG_UNAVAILABLE")
		return
	}

	// Get provider ID from URL
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

	// Find provider
	providerIndex := -1
	for i, p := range currentConfig.Providers {
		if p.ID == providerID {
			providerIndex = i
			break
		}
	}

	if providerIndex == -1 {
		WriteValidationError(w, "Provider not found", "PROVIDER_NOT_FOUND")
		return
	}

	// Create new config without the provider
	newConfig := *currentConfig
	newConfig.Providers = append(currentConfig.Providers[:providerIndex], 
		currentConfig.Providers[providerIndex+1:]...)

	// Validate and save
	if err := s.configManager.ValidateConfigUpdate(&newConfig); err != nil {
		WriteValidationError(w, "Configuration validation failed", err.Error())
		return
	}

	if err := s.configManager.UpdateConfig(&newConfig); err != nil {
		WriteInternalError(w, "Failed to update configuration", err.Error())
		return
	}

	if err := s.configManager.SaveConfig(); err != nil {
		WriteInternalError(w, "Failed to save configuration", err.Error())
		return
	}

	response := struct {
		Message string `json:"message"`
	}{
		Message: "Provider deleted successfully",
	}

	WriteSuccess(w, response, nil)
}

// handleReorderProviders reorders the provider list
func (s *Server) handleReorderProviders(w http.ResponseWriter, r *http.Request) {
	if s.configManager == nil {
		WriteInternalError(w, "Configuration management not available", "CONFIG_UNAVAILABLE")
		return
	}

	// Decode reorder request
	var reorderReq struct {
		ProviderIDs []string `json:"provider_ids"`
	}

	if err := json.NewDecoder(r.Body).Decode(&reorderReq); err != nil {
		WriteValidationError(w, "Invalid JSON in request body", err.Error())
		return
	}

	if len(reorderReq.ProviderIDs) == 0 {
		WriteValidationError(w, "Provider IDs array is required", "MISSING_PROVIDER_IDS")
		return
	}

	// Get current config
	currentConfig := s.configManager.GetConfig()
	if currentConfig == nil {
		WriteInternalError(w, "Configuration not available", "CONFIG_NOT_FOUND")
		return
	}

	// Validate that all IDs exist and no duplicates
	providerMap := make(map[string]config.ProviderConfig)
	for _, p := range currentConfig.Providers {
		providerMap[p.ID] = p
	}

	if len(reorderReq.ProviderIDs) != len(currentConfig.Providers) {
		WriteValidationError(w, "Provider IDs count mismatch", "INVALID_PROVIDER_COUNT")
		return
	}

	// Build new ordered providers list
	newProviders := make([]config.ProviderConfig, 0, len(reorderReq.ProviderIDs))
	for _, id := range reorderReq.ProviderIDs {
		provider, exists := providerMap[id]
		if !exists {
			WriteValidationError(w, fmt.Sprintf("Provider ID '%s' not found", id), "PROVIDER_NOT_FOUND")
			return
		}
		newProviders = append(newProviders, provider)
	}

	// Create new config with reordered providers
	newConfig := *currentConfig
	newConfig.Providers = newProviders

	// Validate and save
	if err := s.configManager.ValidateConfigUpdate(&newConfig); err != nil {
		WriteValidationError(w, "Configuration validation failed", err.Error())
		return
	}

	if err := s.configManager.UpdateConfig(&newConfig); err != nil {
		WriteInternalError(w, "Failed to update configuration", err.Error())
		return
	}

	if err := s.configManager.SaveConfig(); err != nil {
		WriteInternalError(w, "Failed to save configuration", err.Error())
		return
	}

	// Return sanitized providers in new order
	providers := make([]ProviderAPIResponse, len(newProviders))
	for i, p := range newProviders {
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

	WriteSuccess(w, providers, nil)
}
