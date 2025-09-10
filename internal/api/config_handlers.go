package api

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/altmount/pkg/rclonecli"
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

// handleGetConfig returns the current configuration
func (s *Server) handleGetConfig(c *fiber.Ctx) error {
	if s.configManager == nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Configuration management not available",
			"details": "CONFIG_UNAVAILABLE",
		})
	}

	config := s.configManager.GetConfig()
	if config == nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Configuration not available",
			"details": "CONFIG_NOT_FOUND",
		})
	}

	response := ToConfigAPIResponse(config)
	return c.Status(200).JSON(fiber.Map{
		"success": true,
		"data":    response,
	})
}

// handleUpdateConfig updates the entire configuration
func (s *Server) handleUpdateConfig(c *fiber.Ctx) error {
	if s.configManager == nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Configuration management not available",
			"details": "CONFIG_UNAVAILABLE",
		})
	}

	// Decode directly into config structure
	var newConfig config.Config
	if err := c.BodyParser(&newConfig); err != nil {
		return c.Status(422).JSON(fiber.Map{
			"success": false,
			"message": "Invalid JSON in request body",
			"details": err.Error(),
		})
	}

	// Validate the new configuration with API restrictions
	if err := s.configManager.ValidateConfigUpdate(&newConfig); err != nil {
		return c.Status(422).JSON(fiber.Map{
			"success": false,
			"message": "Configuration validation failed",
			"details": err.Error(),
		})
	}

	// Update the configuration
	if err := s.configManager.UpdateConfig(&newConfig); err != nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Failed to update configuration",
			"details": err.Error(),
		})
	}

	// Save to file
	if err := s.configManager.SaveConfig(); err != nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Failed to save configuration",
			"details": err.Error(),
		})
	}

	response := ToConfigAPIResponse(&newConfig)
	return c.Status(200).JSON(fiber.Map{
		"success": true,
		"data":    response,
	})
}

// handlePatchConfigSection updates a specific configuration section
func (s *Server) handlePatchConfigSection(c *fiber.Ctx) error {
	if s.configManager == nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Configuration management not available",
			"details": "CONFIG_UNAVAILABLE",
		})
	}

	// Extract section from URL path parameter
	section := c.Params("section")
	if section == "" {
		return c.Status(422).JSON(fiber.Map{
			"success": false,
			"message": "Invalid configuration section path",
			"details": "INVALID_PATH",
		})
	}

	// Get current config to merge with updates
	currentConfig := s.configManager.GetConfig()
	if currentConfig == nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Configuration not available",
			"details": "CONFIG_NOT_FOUND",
		})
	}

	// Create a copy and decode partial updates directly
	newConfig := *currentConfig // Start with current config
	if err := c.BodyParser(&newConfig); err != nil {
		return c.Status(422).JSON(fiber.Map{
			"success": false,
			"message": "Invalid JSON in request body",
			"details": err.Error(),
		})
	}

	// Validate the new configuration with API restrictions
	if err := s.configManager.ValidateConfigUpdate(&newConfig); err != nil {
		return c.Status(422).JSON(fiber.Map{
			"success": false,
			"message": "Configuration validation failed",
			"details": err.Error(),
		})
	}

	// Update the configuration
	if err := s.configManager.UpdateConfig(&newConfig); err != nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Failed to update configuration",
			"details": err.Error(),
		})
	}

	// Save to file
	if err := s.configManager.SaveConfig(); err != nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Failed to save configuration",
			"details": err.Error(),
		})
	}

	response := ToConfigAPIResponse(&newConfig)
	return c.Status(200).JSON(fiber.Map{
		"success": true,
		"data":    response,
	})
}

// handleReloadConfig reloads configuration from file
func (s *Server) handleReloadConfig(c *fiber.Ctx) error {
	if s.configManager == nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Configuration management not available",
			"details": "CONFIG_UNAVAILABLE",
		})
	}

	if err := s.configManager.ReloadConfig(); err != nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Failed to reload configuration",
			"details": err.Error(),
		})
	}

	config := s.configManager.GetConfig()
	response := ToConfigAPIResponse(config)
	return c.Status(200).JSON(fiber.Map{
		"success": true,
		"data":    response,
	})
}

// handleValidateConfig validates configuration without applying changes
func (s *Server) handleValidateConfig(c *fiber.Ctx) error {
	if s.configManager == nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Configuration management not available",
			"details": "CONFIG_UNAVAILABLE",
		})
	}

	// Decode directly into config structure
	var cfg config.Config
	if err := c.BodyParser(&cfg); err != nil {
		return c.Status(422).JSON(fiber.Map{
			"success": false,
			"message": "Invalid JSON in request body",
			"details": err.Error(),
		})
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

	return c.Status(200).JSON(fiber.Map{
		"success": true,
		"data":    response,
	})
}

// Provider Management Handlers

// handleTestProvider tests NNTP provider connectivity
func (s *Server) handleTestProvider(c *fiber.Ctx) error {
	if s.configManager == nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Configuration management not available",
			"details": "CONFIG_UNAVAILABLE",
		})
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

	if err := c.BodyParser(&testReq); err != nil {
		return c.Status(422).JSON(fiber.Map{
			"success": false,
			"message": "Invalid JSON in request body",
			"details": err.Error(),
		})
	}

	// Basic validation
	if testReq.Host == "" {
		return c.Status(422).JSON(fiber.Map{
			"success": false,
			"message": "Host is required",
			"details": "MISSING_HOST",
		})
	}
	if testReq.Port <= 0 || testReq.Port > 65535 {
		return c.Status(422).JSON(fiber.Map{
			"success": false,
			"message": "Valid port is required (1-65535)",
			"details": "INVALID_PORT",
		})
	}

	err := nntppool.TestProviderConnectivity(c.Context(), nntppool.UsenetProviderConfig{
		Host:     testReq.Host,
		Port:     testReq.Port,
		Username: testReq.Username,
		Password: testReq.Password,
		TLS:      testReq.TLS,
	}, slog.Default(), nil)
	if err != nil {
		return c.Status(200).JSON(fiber.Map{
			"success": true,
			"data": TestProviderResponse{
				Success:      false,
				ErrorMessage: err.Error(),
			},
		})
	}

	// TODO: Implement actual NNTP connection test
	// For now, return success for basic validation
	response := TestProviderResponse{
		Success:      true,
		ErrorMessage: "",
	}

	return c.Status(200).JSON(fiber.Map{
		"success": true,
		"data":    response,
	})
}

// handleCreateProvider creates a new NNTP provider
func (s *Server) handleCreateProvider(c *fiber.Ctx) error {
	if s.configManager == nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Configuration management not available",
			"details": "CONFIG_UNAVAILABLE",
		})
	}

	// Get current config
	currentConfig := s.configManager.GetConfig()
	if currentConfig == nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Configuration not available",
			"details": "CONFIG_NOT_FOUND",
		})
	}

	// Decode create request
	var createReq struct {
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

	if err := c.BodyParser(&createReq); err != nil {
		return c.Status(422).JSON(fiber.Map{
			"success": false,
			"message": "Invalid JSON in request body",
			"details": err.Error(),
		})
	}

	// Validation
	if createReq.Host == "" {
		return c.Status(422).JSON(fiber.Map{
			"success": false,
			"message": "Host is required",
			"details": "MISSING_HOST",
		})
	}
	if createReq.Port <= 0 || createReq.Port > 65535 {
		return c.Status(422).JSON(fiber.Map{
			"success": false,
			"message": "Valid port is required (1-65535)",
			"details": "INVALID_PORT",
		})
	}
	if createReq.Username == "" {
		return c.Status(422).JSON(fiber.Map{
			"success": false,
			"message": "Username is required",
			"details": "MISSING_USERNAME",
		})
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
		TLS:              createReq.TLS,
		InsecureTLS:      createReq.InsecureTLS,
		Enabled:          &createReq.Enabled,
		IsBackupProvider: &createReq.IsBackupProvider,
	}

	// Add to config
	newConfig := currentConfig.DeepCopy()
	newConfig.Providers = append(newConfig.Providers, newProvider)

	// Validate and save
	if err := s.configManager.ValidateConfigUpdate(newConfig); err != nil {
		return c.Status(422).JSON(fiber.Map{
			"success": false,
			"message": "Configuration validation failed",
			"details": err.Error(),
		})
	}

	if err := s.configManager.UpdateConfig(newConfig); err != nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Failed to update configuration",
			"details": err.Error(),
		})
	}

	if err := s.configManager.SaveConfig(); err != nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Failed to save configuration",
			"details": err.Error(),
		})
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

	return c.Status(200).JSON(fiber.Map{
		"success": true,
		"data":    response,
	})
}

// handleUpdateProvider updates an existing NNTP provider
func (s *Server) handleUpdateProvider(c *fiber.Ctx) error {
	if s.configManager == nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Configuration management not available",
			"details": "CONFIG_UNAVAILABLE",
		})
	}

	// Get provider ID from URL
	providerID := c.Params("id")
	if providerID == "" {
		return c.Status(422).JSON(fiber.Map{
			"success": false,
			"message": "Provider ID is required",
			"details": "MISSING_PROVIDER_ID",
		})
	}

	// Get current config
	currentConfig := s.configManager.GetConfig()
	if currentConfig == nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Configuration not available",
			"details": "CONFIG_NOT_FOUND",
		})
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
		return c.Status(422).JSON(fiber.Map{
			"success": false,
			"message": "Provider not found",
			"details": "PROVIDER_NOT_FOUND",
		})
	}

	// Decode update request (partial update)
	var updateReq struct {
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

	if err := c.BodyParser(&updateReq); err != nil {
		return c.Status(422).JSON(fiber.Map{
			"success": false,
			"message": "Invalid JSON in request body",
			"details": err.Error(),
		})
	}

	// Create updated config with proper deep copy
	newConfig := currentConfig.DeepCopy()

	// Get the provider to modify from the deep copy
	provider := newConfig.Providers[providerIndex]

	// Apply updates
	if updateReq.Host != nil {
		if *updateReq.Host == "" {
			return c.Status(422).JSON(fiber.Map{
				"success": false,
				"message": "Host cannot be empty",
				"details": "INVALID_HOST",
			})
		}
		provider.Host = *updateReq.Host
	}
	if updateReq.Port != nil {
		if *updateReq.Port <= 0 || *updateReq.Port > 65535 {
			return c.Status(422).JSON(fiber.Map{
				"success": false,
				"message": "Valid port is required (1-65535)",
				"details": "INVALID_PORT",
			})
		}
		provider.Port = *updateReq.Port
	}
	if updateReq.Username != nil {
		if *updateReq.Username == "" {
			return c.Status(422).JSON(fiber.Map{
				"success": false,
				"message": "Username cannot be empty",
				"details": "INVALID_USERNAME",
			})
		}
		provider.Username = *updateReq.Username
	}
	if updateReq.Password != nil {
		provider.Password = *updateReq.Password
	}
	if updateReq.MaxConnections != nil {
		if *updateReq.MaxConnections <= 0 {
			return c.Status(422).JSON(fiber.Map{
				"success": false,
				"message": "MaxConnections must be positive",
				"details": "INVALID_MAX_CONNECTIONS",
			})
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

	// Assign the updated provider back to the slice
	newConfig.Providers[providerIndex] = provider

	// Validate and save
	if err := s.configManager.ValidateConfigUpdate(newConfig); err != nil {
		return c.Status(422).JSON(fiber.Map{
			"success": false,
			"message": "Configuration validation failed",
			"details": err.Error(),
		})
	}

	if err := s.configManager.UpdateConfig(newConfig); err != nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Failed to update configuration",
			"details": err.Error(),
		})
	}

	if err := s.configManager.SaveConfig(); err != nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Failed to save configuration",
			"details": err.Error(),
		})
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

	return c.Status(200).JSON(fiber.Map{
		"success": true,
		"data":    response,
	})
}

// handleDeleteProvider removes an NNTP provider
func (s *Server) handleDeleteProvider(c *fiber.Ctx) error {
	if s.configManager == nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Configuration management not available",
			"details": "CONFIG_UNAVAILABLE",
		})
	}

	// Get provider ID from URL
	providerID := c.Params("id")
	if providerID == "" {
		return c.Status(422).JSON(fiber.Map{
			"success": false,
			"message": "Provider ID is required",
			"details": "MISSING_PROVIDER_ID",
		})
	}

	// Get current config
	currentConfig := s.configManager.GetConfig()
	if currentConfig == nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Configuration not available",
			"details": "CONFIG_NOT_FOUND",
		})
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
		return c.Status(422).JSON(fiber.Map{
			"success": false,
			"message": "Provider not found",
			"details": "PROVIDER_NOT_FOUND",
		})
	}

	// Create new config without the provider
	newConfig := currentConfig.DeepCopy()
	newConfig.Providers = append(newConfig.Providers[:providerIndex],
		newConfig.Providers[providerIndex+1:]...)

	// Validate and save
	if err := s.configManager.ValidateConfigUpdate(newConfig); err != nil {
		return c.Status(422).JSON(fiber.Map{
			"success": false,
			"message": "Configuration validation failed",
			"details": err.Error(),
		})
	}

	if err := s.configManager.UpdateConfig(newConfig); err != nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Failed to update configuration",
			"details": err.Error(),
		})
	}

	if err := s.configManager.SaveConfig(); err != nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Failed to save configuration",
			"details": err.Error(),
		})
	}

	response := struct {
		Message string `json:"message"`
	}{
		Message: "Provider deleted successfully",
	}

	return c.Status(200).JSON(fiber.Map{
		"success": true,
		"data":    response,
	})
}

// handleReorderProviders reorders the provider list
func (s *Server) handleReorderProviders(c *fiber.Ctx) error {
	if s.configManager == nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Configuration management not available",
			"details": "CONFIG_UNAVAILABLE",
		})
	}

	// Decode reorder request
	var reorderReq struct {
		ProviderIDs []string `json:"provider_ids"`
	}

	if err := c.BodyParser(&reorderReq); err != nil {
		return c.Status(422).JSON(fiber.Map{
			"success": false,
			"message": "Invalid JSON in request body",
			"details": err.Error(),
		})
	}

	if len(reorderReq.ProviderIDs) == 0 {
		return c.Status(422).JSON(fiber.Map{
			"success": false,
			"message": "Provider IDs array is required",
			"details": "MISSING_PROVIDER_IDS",
		})
	}

	// Get current config
	currentConfig := s.configManager.GetConfig()
	if currentConfig == nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Configuration not available",
			"details": "CONFIG_NOT_FOUND",
		})
	}

	// Validate that all IDs exist and no duplicates
	providerMap := make(map[string]config.ProviderConfig)
	for _, p := range currentConfig.Providers {
		providerMap[p.ID] = p
	}

	if len(reorderReq.ProviderIDs) != len(currentConfig.Providers) {
		return c.Status(422).JSON(fiber.Map{
			"success": false,
			"message": "Provider IDs count mismatch",
			"details": "INVALID_PROVIDER_COUNT",
		})
	}

	// Build new ordered providers list
	newProviders := make([]config.ProviderConfig, 0, len(reorderReq.ProviderIDs))
	for _, id := range reorderReq.ProviderIDs {
		provider, exists := providerMap[id]
		if !exists {
			return c.Status(422).JSON(fiber.Map{
				"success": false,
				"message": fmt.Sprintf("Provider ID '%s' not found", id),
				"details": "PROVIDER_NOT_FOUND",
			})
		}
		newProviders = append(newProviders, provider)
	}

	// Create new config with reordered providers
	newConfig := currentConfig.DeepCopy()
	newConfig.Providers = newProviders

	// Validate and save
	if err := s.configManager.ValidateConfigUpdate(newConfig); err != nil {
		return c.Status(422).JSON(fiber.Map{
			"success": false,
			"message": "Configuration validation failed",
			"details": err.Error(),
		})
	}

	if err := s.configManager.UpdateConfig(newConfig); err != nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Failed to update configuration",
			"details": err.Error(),
		})
	}

	if err := s.configManager.SaveConfig(); err != nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Failed to save configuration",
			"details": err.Error(),
		})
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

	return c.Status(200).JSON(fiber.Map{
		"success": true,
		"data":    providers,
	})
}

// handleTestRCloneConnection tests the RClone RC connection
func (s *Server) handleTestRCloneConnection(c *fiber.Ctx) error {
	// Decode test request
	var testReq struct {
		VFSEnabled bool   `json:"vfs_enabled"`
		VFSURL     string `json:"vfs_url"`
		VFSUser    string `json:"vfs_user"`
		VFSPass    string `json:"vfs_pass"`
	}

	if err := c.BodyParser(&testReq); err != nil {
		return c.Status(422).JSON(fiber.Map{
			"success": false,
			"message": "Invalid JSON in request body",
			"details": err.Error(),
		})
	}

	// Validate that VFS is enabled
	if !testReq.VFSEnabled {
		return c.Status(422).JSON(fiber.Map{
			"success": false,
			"message": "VFS must be enabled to test connection",
			"details": "VFS_NOT_ENABLED",
		})
	}

	// Validate URL is provided
	if testReq.VFSURL == "" {
		return c.Status(422).JSON(fiber.Map{
			"success": false,
			"message": "VFS URL is required",
			"details": "MISSING_VFS_URL",
		})
	}

	// Create a temporary RClone client with the test configuration
	testConfig := &rclonecli.Config{
		VFSEnabled: testReq.VFSEnabled,
		VFSUrl:     testReq.VFSURL,
		VFSUser:    testReq.VFSUser,
		VFSPass:    testReq.VFSPass,
	}

	httpClient := &http.Client{
		Timeout: 10 * time.Second,
	}
	testClient := rclonecli.NewRcloneRcClient(testConfig, httpClient)

	// Test the connection by attempting to refresh the root directory
	ctx := c.Context()
	err := testClient.RefreshCache(ctx, "/", true, false) // async=true, recursive=false

	if err != nil {
		// Return success:true but with test result as failed
		return c.Status(200).JSON(fiber.Map{
			"success": true,
			"data": fiber.Map{
				"success":       false,
				"error_message": err.Error(),
			},
		})
	}

	// Connection successful
	return c.Status(200).JSON(fiber.Map{
		"success": true,
		"data": fiber.Map{
			"success":       true,
			"error_message": "",
		},
	})
}
