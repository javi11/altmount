package api

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/javi11/altmount/internal/auth"
	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/nntppool/v4"
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
	NeedsLibrarySync() bool
	GetPreviousMountPath() string
	ClearLibrarySyncFlag()
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

// ApplyLogLevel applies the log level to the global logger
func ApplyLogLevel(level string) {
	if level != "" {
		slog.SetLogLoggerLevel(parseLogLevel(level))
	}
}

// getEffectiveLogLevel returns the effective log level, preferring new config over legacy
func getEffectiveLogLevel(newLevel, legacyLevel string) string {
	if newLevel != "" {
		return newLevel
	}
	if legacyLevel != "" {
		return legacyLevel
	}
	return "info"
}

// RegisterLogLevelHandler registers handler for log level configuration changes
func RegisterLogLevelHandler(ctx context.Context, configManager *config.Manager, debugMode *bool) {
	configManager.OnConfigChange(func(oldConfig, newConfig *config.Config) {
		// Determine old and new log levels
		oldLevel := getEffectiveLogLevel(oldConfig.Log.Level, oldConfig.Log.Level)
		newLevel := getEffectiveLogLevel(newConfig.Log.Level, newConfig.Log.Level)

		// Apply log level change if it changed
		if oldLevel != newLevel {
			ApplyLogLevel(newLevel)
			// Update Fiber logger debug mode
			*debugMode = newLevel == "debug"
			slog.InfoContext(ctx, "Log level updated dynamically",
				"old_level", oldLevel,
				"new_level", newLevel,
				"fiber_logging", *debugMode)
		}
	})
}

// handleGetConfig returns the current configuration
func (s *Server) handleGetConfig(c *fiber.Ctx) error {
	if s.configManager == nil {
		return RespondServiceUnavailable(c, "Configuration management not available", "CONFIG_UNAVAILABLE")
	}

	config := s.configManager.GetConfig()
	if config == nil {
		return RespondInternalError(c, "Configuration not available", "CONFIG_NOT_FOUND")
	}

	// Get API key from authenticated user or first admin user
	apiKey := s.getAPIKeyForConfig(c)

	response := ToConfigAPIResponse(config, apiKey)
	return RespondSuccess(c, response)
}

// handleUpdateConfig updates the entire configuration
func (s *Server) handleUpdateConfig(c *fiber.Ctx) error {
	if s.configManager == nil {
		return RespondServiceUnavailable(c, "Configuration management not available", "CONFIG_UNAVAILABLE")
	}

	// Get current config to use as base for defaults/missing fields
	currentConfig := s.configManager.GetConfig()
	newConfig := currentConfig.DeepCopy()

	// Decode directly into config structure
	if err := c.BodyParser(newConfig); err != nil {
		return RespondValidationError(c, "Invalid JSON in request body", err.Error())
	}

	slog.DebugContext(c.Context(), "Updating configuration")

	// Validate the new configuration with API restrictions
	if err := s.configManager.ValidateConfigUpdate(newConfig); err != nil {
		return RespondValidationError(c, "Configuration validation failed", err.Error())
	}

	// Update the configuration
	if err := s.configManager.UpdateConfig(newConfig); err != nil {
		return RespondInternalError(c, "Failed to update configuration", err.Error())
	}

	// Ensure SABnzbd category directories exist
	if err := s.ensureSABnzbdCategoryDirectories(newConfig); err != nil {
		// Log the error but don't fail the update
		slog.WarnContext(c.Context(), "Failed to create SABnzbd category directories", "error", err)
	}

	// Save to file
	if err := s.configManager.SaveConfig(); err != nil {
		return RespondInternalError(c, "Failed to save configuration", err.Error())
	}

	// Try to start RC server if RClone is enabled but RC is not running
	s.startRCServerIfNeeded(c.Context())

	// Get API key for response
	apiKey := s.getAPIKeyForConfig(c)

	response := ToConfigAPIResponse(newConfig, apiKey)
	return RespondSuccess(c, response)
}

// handlePatchConfigSection updates a specific configuration section
func (s *Server) handlePatchConfigSection(c *fiber.Ctx) error {
	if s.configManager == nil {
		return RespondServiceUnavailable(c, "Configuration management not available", "CONFIG_UNAVAILABLE")
	}

	// Extract section from URL path parameter
	section := c.Params("section")
	if section == "" {
		return RespondValidationError(c, "Invalid configuration section path", "INVALID_PATH")
	}

	// Get current config to merge with updates
	currentConfig := s.configManager.GetConfig()
	if currentConfig == nil {
		return RespondInternalError(c, "Configuration not available", "CONFIG_NOT_FOUND")
	}

	// Create a copy to apply updates to
	newConfig := currentConfig.DeepCopy()

	// Decode into the specific section based on the URL parameter
	var err error
	switch section {
	case "webdav", "api", "auth", "database", "metadata", "streaming", "health", "rclone", "import", "log", "sabnzbd", "arrs", "fuse", "system", "mount_path", "mount":
		err = c.BodyParser(newConfig)
	default:
		return RespondValidationError(c, fmt.Sprintf("Unknown configuration section: %s", section), "INVALID_SECTION")
	}

	if err != nil {
		return RespondValidationError(c, "Invalid JSON in request body", err.Error())
	}

	slog.DebugContext(c.Context(), "Patching configuration section",
		"section", section)

	// Validate the new configuration with API restrictions
	if err := s.configManager.ValidateConfigUpdate(newConfig); err != nil {
		return RespondValidationError(c, "Configuration validation failed", err.Error())
	}

	// Update the configuration
	if err := s.configManager.UpdateConfig(newConfig); err != nil {
		return RespondInternalError(c, "Failed to update configuration", err.Error())
	}

	// Ensure SABnzbd category directories exist if SABnzbd section was updated
	if section == "sabnzbd" || section == "" {
		if err := s.ensureSABnzbdCategoryDirectories(newConfig); err != nil {
			// Log the error but don't fail the update
			slog.WarnContext(c.Context(), "Failed to create SABnzbd category directories", "error", err)
		}
	}

	// Save to file
	if err := s.configManager.SaveConfig(); err != nil {
		return RespondInternalError(c, "Failed to save configuration", err.Error())
	}

	// Try to start RC server if RClone/mount section was updated or full config update
	if section == "rclone" || section == "mount" || section == "" {
		s.startRCServerIfNeeded(c.Context())
	}

	// Get API key for response
	apiKey := s.getAPIKeyForConfig(c)

	response := ToConfigAPIResponse(newConfig, apiKey)
	return RespondSuccess(c, response)
}

// handleReloadConfig reloads configuration from file
func (s *Server) handleReloadConfig(c *fiber.Ctx) error {
	if s.configManager == nil {
		return RespondServiceUnavailable(c, "Configuration management not available", "CONFIG_UNAVAILABLE")
	}

	if err := s.configManager.ReloadConfig(); err != nil {
		return RespondInternalError(c, "Failed to reload configuration", err.Error())
	}

	config := s.configManager.GetConfig()

	// Get API key for response
	apiKey := s.getAPIKeyForConfig(c)

	response := ToConfigAPIResponse(config, apiKey)
	return RespondSuccess(c, response)
}

// handleValidateConfig validates configuration without applying changes
func (s *Server) handleValidateConfig(c *fiber.Ctx) error {
	if s.configManager == nil {
		return RespondServiceUnavailable(c, "Configuration management not available", "CONFIG_UNAVAILABLE")
	}

	// Decode directly into config structure
	var cfg config.Config
	if err := c.BodyParser(&cfg); err != nil {
		return RespondValidationError(c, "Invalid JSON in request body", err.Error())
	}

	// Validate the configuration
	validationErr := s.configManager.ValidateConfig(&cfg)

	response := struct {
		Valid  bool `json:"valid"`
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

	return RespondSuccess(c, response)
}

// Provider Management Handlers

// handleTestProvider tests NNTP provider connectivity
func (s *Server) handleTestProvider(c *fiber.Ctx) error {
	if s.configManager == nil {
		return RespondServiceUnavailable(c, "Configuration management not available", "CONFIG_UNAVAILABLE")
	}

	// Decode test request
	var testReq struct {
		Host        string `json:"host"`
		Port        int    `json:"port"`
		Username    string `json:"username"`
		Password    string `json:"password"`
		TLS         bool   `json:"tls"`
		InsecureTLS bool   `json:"insecure_tls"`
		ProxyURL    string `json:"proxy_url"`
	}

	if err := c.BodyParser(&testReq); err != nil {
		return RespondValidationError(c, "Invalid JSON in request body", err.Error())
	}

	// Basic validation
	if testReq.Host == "" {
		return RespondValidationError(c, "Host is required", "MISSING_HOST")
	}
	if testReq.Port <= 0 || testReq.Port > 65535 {
		return RespondValidationError(c, "Valid port is required (1-65535)", "INVALID_PORT")
	}

	ctx, cancel := context.WithTimeout(c.Context(), 30*time.Second)
	defer cancel()

	host := fmt.Sprintf("%s:%d", testReq.Host, testReq.Port)
	var tlsCfg *tls.Config
	if testReq.TLS {
		tlsCfg = &tls.Config{
			InsecureSkipVerify: testReq.InsecureTLS,
			ServerName:         testReq.Host,
		}
	}

	result := nntppool.TestProvider(ctx, nntppool.Provider{
		Host:      host,
		TLSConfig: tlsCfg,
		Auth:      nntppool.Auth{Username: testReq.Username, Password: testReq.Password},
	})

	if result.Err != nil {
		return RespondSuccess(c, TestProviderResponse{
			Success:      false,
			ErrorMessage: result.Err.Error(),
		})
	}

	return RespondSuccess(c, TestProviderResponse{
		Success: true,
		RTTMs:   result.RTT.Milliseconds(),
	})
}

// handleCreateProvider creates a new NNTP provider
func (s *Server) handleCreateProvider(c *fiber.Ctx) error {
	if s.configManager == nil {
		return RespondServiceUnavailable(c, "Configuration management not available", "CONFIG_UNAVAILABLE")
	}

	// Get current config
	currentConfig := s.configManager.GetConfig()
	if currentConfig == nil {
		return RespondInternalError(c, "Configuration not available", "CONFIG_NOT_FOUND")
	}

	// Decode create request
	var createReq struct {
		Host             string `json:"host"`
		Port             int    `json:"port"`
		Username         string `json:"username"`
		Password         string `json:"password"`
		MaxConnections   int    `json:"max_connections"`
		InflightRequests int    `json:"inflight_requests"`
		TLS              bool   `json:"tls"`
		InsecureTLS      bool   `json:"insecure_tls"`
		ProxyURL         string `json:"proxy_url"`
		Enabled          bool   `json:"enabled"`
		IsBackupProvider bool   `json:"is_backup_provider"`
	}

	if err := c.BodyParser(&createReq); err != nil {
		return RespondValidationError(c, "Invalid JSON in request body", err.Error())
	}

	// Validation
	if createReq.Host == "" {
		return RespondValidationError(c, "Host is required", "MISSING_HOST")
	}
	if createReq.Port <= 0 || createReq.Port > 65535 {
		return RespondValidationError(c, "Valid port is required (1-65535)", "INVALID_PORT")
	}
	if createReq.Username == "" {
		return RespondValidationError(c, "Username is required", "MISSING_USERNAME")
	}
	if createReq.MaxConnections <= 0 {
		createReq.MaxConnections = 1 // Default
	}

	// Generate new ID
	newID := fmt.Sprintf("provider_%d", len(currentConfig.Providers)+1)

	// Create new provider
	newProvider := config.ProviderConfig{
		ID:               newID,
		Host:             createReq.Host,
		Port:             createReq.Port,
		Username:         createReq.Username,
		Password:         createReq.Password,
		MaxConnections:   createReq.MaxConnections,
		InflightRequests: createReq.InflightRequests,
		TLS:              createReq.TLS,
		InsecureTLS:      createReq.InsecureTLS,
		ProxyURL:         createReq.ProxyURL,
		Enabled:          &createReq.Enabled,
		IsBackupProvider: &createReq.IsBackupProvider,
	}

	// Add to config
	newConfig := currentConfig.DeepCopy()
	newConfig.Providers = append(newConfig.Providers, newProvider)

	// Validate and save
	if err := s.configManager.ValidateConfigUpdate(newConfig); err != nil {
		return RespondValidationError(c, "Configuration validation failed", err.Error())
	}

	if err := s.configManager.UpdateConfig(newConfig); err != nil {
		return RespondInternalError(c, "Failed to update configuration", err.Error())
	}

	if err := s.configManager.SaveConfig(); err != nil {
		return RespondInternalError(c, "Failed to save configuration", err.Error())
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
		ProxyURL:         newProvider.ProxyURL,
		PasswordSet:      newProvider.Password != "",
		Enabled:          newProvider.Enabled != nil && *newProvider.Enabled,
		IsBackupProvider: newProvider.IsBackupProvider != nil && *newProvider.IsBackupProvider,
		InflightRequests: newProvider.InflightRequests,
	}

	return RespondSuccess(c, response)
}

// handleUpdateProvider updates an existing NNTP provider
func (s *Server) handleUpdateProvider(c *fiber.Ctx) error {
	if s.configManager == nil {
		return RespondServiceUnavailable(c, "Configuration management not available", "CONFIG_UNAVAILABLE")
	}

	// Get provider ID from URL
	providerID := c.Params("id")
	if providerID == "" {
		return RespondValidationError(c, "Provider ID is required", "MISSING_PROVIDER_ID")
	}

	// Get current config
	currentConfig := s.configManager.GetConfig()
	if currentConfig == nil {
		return RespondInternalError(c, "Configuration not available", "CONFIG_NOT_FOUND")
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
		return RespondNotFound(c, "Provider", "PROVIDER_NOT_FOUND")
	}

	// Decode update request (partial update)
	var updateReq struct {
		Host             *string `json:"host,omitempty"`
		Port             *int    `json:"port,omitempty"`
		Username         *string `json:"username,omitempty"`
		Password         *string `json:"password,omitempty"`
		MaxConnections   *int    `json:"max_connections,omitempty"`
		InflightRequests *int    `json:"inflight_requests,omitempty"`
		TLS              *bool   `json:"tls,omitempty"`
		InsecureTLS      *bool   `json:"insecure_tls,omitempty"`
		ProxyURL         *string `json:"proxy_url,omitempty"`
		Enabled          *bool   `json:"enabled,omitempty"`
		IsBackupProvider *bool   `json:"is_backup_provider,omitempty"`
	}

	if err := c.BodyParser(&updateReq); err != nil {
		return RespondValidationError(c, "Invalid JSON in request body", err.Error())
	}

	// Create updated config with proper deep copy
	newConfig := currentConfig.DeepCopy()

	// Get the provider to modify from the deep copy
	provider := newConfig.Providers[providerIndex]

	// Apply updates
	if updateReq.Host != nil {
		if *updateReq.Host == "" {
			return RespondValidationError(c, "Host cannot be empty", "INVALID_HOST")
		}
		provider.Host = *updateReq.Host
	}
	if updateReq.Port != nil {
		if *updateReq.Port <= 0 || *updateReq.Port > 65535 {
			return RespondValidationError(c, "Valid port is required (1-65535)", "INVALID_PORT")
		}
		provider.Port = *updateReq.Port
	}
	if updateReq.Username != nil {
		if *updateReq.Username == "" {
			return RespondValidationError(c, "Username cannot be empty", "INVALID_USERNAME")
		}
		provider.Username = *updateReq.Username
	}
	if updateReq.Password != nil {
		provider.Password = *updateReq.Password
	}
	if updateReq.MaxConnections != nil {
		if *updateReq.MaxConnections <= 0 {
			return RespondValidationError(c, "MaxConnections must be positive", "INVALID_MAX_CONNECTIONS")
		}
		provider.MaxConnections = *updateReq.MaxConnections
	}
	if updateReq.InflightRequests != nil {
		provider.InflightRequests = *updateReq.InflightRequests
	}
	if updateReq.TLS != nil {
		provider.TLS = *updateReq.TLS
	}
	if updateReq.InsecureTLS != nil {
		provider.InsecureTLS = *updateReq.InsecureTLS
	}
	if updateReq.ProxyURL != nil {
		provider.ProxyURL = *updateReq.ProxyURL
	}
	if updateReq.Enabled != nil {
		provider.Enabled = updateReq.Enabled
	}
	if updateReq.IsBackupProvider != nil {
		provider.IsBackupProvider = updateReq.IsBackupProvider
	}

	// Assign the updated provider back to the slice
	newConfig.Providers[providerIndex] = provider

	// Validate and save
	if err := s.configManager.ValidateConfigUpdate(newConfig); err != nil {
		return RespondValidationError(c, "Configuration validation failed", err.Error())
	}

	if err := s.configManager.UpdateConfig(newConfig); err != nil {
		return RespondInternalError(c, "Failed to update configuration", err.Error())
	}

	if err := s.configManager.SaveConfig(); err != nil {
		return RespondInternalError(c, "Failed to save configuration", err.Error())
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
		ProxyURL:         provider.ProxyURL,
		PasswordSet:      provider.Password != "",
		Enabled:          provider.Enabled != nil && *provider.Enabled,
		IsBackupProvider: provider.IsBackupProvider != nil && *provider.IsBackupProvider,
		InflightRequests: provider.InflightRequests,
	}

	return RespondSuccess(c, response)
}

// handleDeleteProvider removes an NNTP provider
func (s *Server) handleDeleteProvider(c *fiber.Ctx) error {
	if s.configManager == nil {
		return RespondServiceUnavailable(c, "Configuration management not available", "CONFIG_UNAVAILABLE")
	}

	// Get provider ID from URL
	providerID := c.Params("id")
	if providerID == "" {
		return RespondValidationError(c, "Provider ID is required", "MISSING_PROVIDER_ID")
	}

	// Get current config
	currentConfig := s.configManager.GetConfig()
	if currentConfig == nil {
		return RespondInternalError(c, "Configuration not available", "CONFIG_NOT_FOUND")
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
		return RespondNotFound(c, "Provider", "PROVIDER_NOT_FOUND")
	}

	// Create new config without the provider
	newConfig := currentConfig.DeepCopy()
	newConfig.Providers = append(newConfig.Providers[:providerIndex],
		newConfig.Providers[providerIndex+1:]...)

	// Validate and save
	if err := s.configManager.ValidateConfigUpdate(newConfig); err != nil {
		return RespondValidationError(c, "Configuration validation failed", err.Error())
	}

	if err := s.configManager.UpdateConfig(newConfig); err != nil {
		return RespondInternalError(c, "Failed to update configuration", err.Error())
	}

	if err := s.configManager.SaveConfig(); err != nil {
		return RespondInternalError(c, "Failed to save configuration", err.Error())
	}

	response := struct {
		Message string `json:"message"`
	}{
		Message: "Provider deleted successfully",
	}

	return RespondSuccess(c, response)
}

// handleReorderProviders reorders the provider list
func (s *Server) handleReorderProviders(c *fiber.Ctx) error {
	if s.configManager == nil {
		return RespondServiceUnavailable(c, "Configuration management not available", "CONFIG_UNAVAILABLE")
	}

	// Decode reorder request
	var reorderReq struct {
		ProviderIDs []string `json:"provider_ids"`
	}

	if err := c.BodyParser(&reorderReq); err != nil {
		return RespondValidationError(c, "Invalid JSON in request body", err.Error())
	}

	if len(reorderReq.ProviderIDs) == 0 {
		return RespondValidationError(c, "Provider IDs array is required", "MISSING_PROVIDER_IDS")
	}

	// Get current config
	currentConfig := s.configManager.GetConfig()
	if currentConfig == nil {
		return RespondInternalError(c, "Configuration not available", "CONFIG_NOT_FOUND")
	}

	// Validate that all IDs exist and no duplicates
	providerMap := make(map[string]config.ProviderConfig)
	for _, p := range currentConfig.Providers {
		providerMap[p.ID] = p
	}

	if len(reorderReq.ProviderIDs) != len(currentConfig.Providers) {
		return RespondValidationError(c, "Provider IDs count mismatch", "INVALID_PROVIDER_COUNT")
	}

	// Build new ordered providers list
	newProviders := make([]config.ProviderConfig, 0, len(reorderReq.ProviderIDs))
	for _, id := range reorderReq.ProviderIDs {
		provider, exists := providerMap[id]
		if !exists {
			return RespondNotFound(c, fmt.Sprintf("Provider ID '%s'", id), "PROVIDER_NOT_FOUND")
		}
		newProviders = append(newProviders, provider)
	}

	// Create new config with reordered providers
	newConfig := currentConfig.DeepCopy()
	newConfig.Providers = newProviders

	// Validate and save
	if err := s.configManager.ValidateConfigUpdate(newConfig); err != nil {
		return RespondValidationError(c, "Configuration validation failed", err.Error())
	}

	if err := s.configManager.UpdateConfig(newConfig); err != nil {
		return RespondInternalError(c, "Failed to update configuration", err.Error())
	}

	if err := s.configManager.SaveConfig(); err != nil {
		return RespondInternalError(c, "Failed to save configuration", err.Error())
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
			ProxyURL:         p.ProxyURL,
			PasswordSet:      p.Password != "",
			Enabled:          p.Enabled != nil && *p.Enabled,
			IsBackupProvider: p.IsBackupProvider != nil && *p.IsBackupProvider,
			InflightRequests: p.InflightRequests,
		}
	}

	return RespondSuccess(c, providers)
}

// startRCServerIfNeeded starts the RC server if RClone is enabled and RC is not running
func (s *Server) startRCServerIfNeeded(ctx context.Context) {
	// Check if we have a mount service to work with
	if s.mountService == nil {
		slog.WarnContext(ctx, "Mount service not available, cannot start RC server")
		return
	}

	// Use the mount service to start the RC server (non-blocking for config save)
	go func() {
		if err := s.mountService.StartRCServer(ctx); err != nil {
			slog.ErrorContext(ctx, "Failed to start RClone RC server via mount service", "error", err)
			return
		}

		// Now that RC server is ready, initialize RClone client in importer service if available
		if s.importerService != nil {
			s.importerService.SetRcloneClient(s.mountService.GetManager())
			slog.InfoContext(ctx, "RClone client initialized in importer service")
		}
	}()
}

// ensureSABnzbdCategoryDirectories creates directories for all SABnzbd categories in the mount path
func (s *Server) ensureSABnzbdCategoryDirectories(cfg *config.Config) error {
	// Only process if SABnzbd is enabled
	if cfg.SABnzbd.Enabled == nil || !*cfg.SABnzbd.Enabled {
		return nil
	}

	// Create base SABnzbd complete directory
	baseDir := filepath.Join(cfg.Metadata.RootPath, cfg.SABnzbd.CompleteDir)
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return fmt.Errorf("failed to create SABnzbd base directory: %w", err)
	}

	// Create directories for each category
	for _, category := range cfg.SABnzbd.Categories {
		if category.Dir != "" {
			categoryDir := filepath.Join(baseDir, category.Dir)
			if err := os.MkdirAll(categoryDir, 0755); err != nil {
				return fmt.Errorf("failed to create category directory %s: %w", category.Name, err)
			}
		}
	}

	return nil
}

// getAPIKeyForConfig retrieves the API key for config responses
func (s *Server) getAPIKeyForConfig(c *fiber.Ctx) string {
	// Try to get user from context (if authenticated)
	user := auth.GetUserFromContext(c)
	if user != nil && user.APIKey != nil {
		return *user.APIKey
	}

	// Try to get from Arrs service which handles bootstrapping default admin if needed
	if s.arrsService != nil {
		if key := s.arrsService.GetFirstAdminAPIKey(c.Context()); key != "" {
			return key
		}
	}

	// If no authenticated user and arrs service didn't return one, try manual DB check
	if s.userRepo != nil {
		users, err := s.userRepo.ListUsers(c.Context(), 1, 0)
		if err == nil && len(users) > 0 && users[0].APIKey != nil {
			return *users[0].APIKey
		}
	}

	return ""
}
