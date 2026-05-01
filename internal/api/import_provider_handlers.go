package api

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/javi11/altmount/internal/config"
	nntppool "github.com/javi11/nntppool/v4"
)

// handleCreateImportProvider creates a new import NNTP provider
//
//	@Summary		Create import NNTP provider
//	@Description	Adds a new NNTP provider to the import providers list.
//	@Tags			ImportProviders
//	@Accept			json
//	@Produce		json
//	@Param			body	body		ProviderCreateRequest	true	"Provider details"
//	@Success		201		{object}	APIResponse{data=ProviderAPIResponse}
//	@Failure		400		{object}	APIResponse
//	@Security		BearerAuth
//	@Security		ApiKeyAuth
//	@Router			/import-providers [post]
func (s *Server) handleCreateImportProvider(c *fiber.Ctx) error {
	if s.configManager == nil {
		return RespondServiceUnavailable(c, "Configuration management not available", "CONFIG_UNAVAILABLE")
	}

	currentConfig := s.configManager.GetConfig()
	if currentConfig == nil {
		return RespondInternalError(c, "Configuration not available", "CONFIG_NOT_FOUND")
	}

	var createReq struct {
		Host                     string `json:"host"`
		Port                     int    `json:"port"`
		Username                 string `json:"username"`
		Password                 string `json:"password"`
		MaxConnections           int    `json:"max_connections"`
		InflightRequests         int    `json:"inflight_requests"`
		TLS                      bool   `json:"tls"`
		InsecureTLS              bool   `json:"insecure_tls"`
		ProxyURL                 string `json:"proxy_url"`
		Enabled                  bool   `json:"enabled"`
		IsBackupProvider         bool   `json:"is_backup_provider"`
		SkipPing                 bool   `json:"skip_ping"`
		KeepaliveIntervalSeconds int    `json:"keepalive_interval_seconds"`
		KeepaliveCommand         string `json:"keepalive_command"`
		UserAgent                string `json:"user_agent"`
		QuotaBytes               int64  `json:"quota_bytes"`
		QuotaPeriodHours         int    `json:"quota_period_hours"`
	}

	if err := c.BodyParser(&createReq); err != nil {
		return RespondValidationError(c, "Invalid JSON in request body", err.Error())
	}

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
		return RespondValidationError(c, "MaxConnections must be positive", "INVALID_MAX_CONNECTIONS")
	}

	enabled := createReq.Enabled
	isBackup := createReq.IsBackupProvider
	newID := fmt.Sprintf("import_provider_%d", len(currentConfig.ImportProviders)+1)

	newProvider := config.ProviderConfig{
		ID:                       newID,
		Host:                     createReq.Host,
		Port:                     createReq.Port,
		Username:                 createReq.Username,
		Password:                 createReq.Password,
		MaxConnections:           createReq.MaxConnections,
		InflightRequests:         createReq.InflightRequests,
		TLS:                      createReq.TLS,
		InsecureTLS:              createReq.InsecureTLS,
		ProxyURL:                 createReq.ProxyURL,
		Enabled:                  &enabled,
		IsBackupProvider:         &isBackup,
		SkipPing:                 createReq.SkipPing,
		KeepaliveIntervalSeconds: createReq.KeepaliveIntervalSeconds,
		KeepaliveCommand:         createReq.KeepaliveCommand,
		UserAgent:                createReq.UserAgent,
		QuotaBytes:               createReq.QuotaBytes,
		QuotaPeriodHours:         createReq.QuotaPeriodHours,
	}

	newConfig := currentConfig.DeepCopy()
	newConfig.ImportProviders = append(newConfig.ImportProviders, newProvider)

	if err := s.configManager.ValidateConfigUpdate(newConfig); err != nil {
		return RespondValidationError(c, "Configuration validation failed", err.Error())
	}

	if err := s.configManager.UpdateConfig(newConfig); err != nil {
		return RespondInternalError(c, "Failed to update configuration", err.Error())
	}

	if err := s.configManager.SaveConfig(); err != nil {
		return RespondInternalError(c, "Failed to save configuration", err.Error())
	}

	newProvider = newConfig.ImportProviders[len(newConfig.ImportProviders)-1]

	response := ProviderAPIResponse{
		ID:                       newProvider.ID,
		Host:                     newProvider.Host,
		Port:                     newProvider.Port,
		Username:                 newProvider.Username,
		MaxConnections:           newProvider.MaxConnections,
		TLS:                      newProvider.TLS,
		InsecureTLS:              newProvider.InsecureTLS,
		ProxyURL:                 newProvider.ProxyURL,
		PasswordSet:              newProvider.Password != "",
		Enabled:                  newProvider.Enabled != nil && *newProvider.Enabled,
		IsBackupProvider:         newProvider.IsBackupProvider != nil && *newProvider.IsBackupProvider,
		InflightRequests:         newProvider.InflightRequests,
		LastRTTMs:                newProvider.LastRTTMs,
		SkipPing:                 newProvider.SkipPing,
		KeepaliveIntervalSeconds: newProvider.KeepaliveIntervalSeconds,
		KeepaliveCommand:         newProvider.KeepaliveCommand,
		UserAgent:                newProvider.UserAgent,
		QuotaBytes:               newProvider.QuotaBytes,
		QuotaPeriodHours:         newProvider.QuotaPeriodHours,
	}

	return RespondCreated(c, response)
}

// handleUpdateImportProvider updates an existing import NNTP provider
//
//	@Summary		Update import NNTP provider
//	@Description	Updates an existing import NNTP provider by ID.
//	@Tags			ImportProviders
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string					true	"Provider ID"
//	@Param			body	body		ProviderUpdateRequest	true	"Fields to update"
//	@Success		200		{object}	APIResponse{data=ProviderAPIResponse}
//	@Failure		400		{object}	APIResponse
//	@Failure		404		{object}	APIResponse
//	@Security		BearerAuth
//	@Security		ApiKeyAuth
//	@Router			/import-providers/{id} [put]
func (s *Server) handleUpdateImportProvider(c *fiber.Ctx) error {
	if s.configManager == nil {
		return RespondServiceUnavailable(c, "Configuration management not available", "CONFIG_UNAVAILABLE")
	}

	providerID := c.Params("id")
	if providerID == "" {
		return RespondValidationError(c, "Provider ID is required", "MISSING_PROVIDER_ID")
	}

	currentConfig := s.configManager.GetConfig()
	if currentConfig == nil {
		return RespondInternalError(c, "Configuration not available", "CONFIG_NOT_FOUND")
	}

	providerIndex := -1
	for i, p := range currentConfig.ImportProviders {
		if p.ID == providerID {
			providerIndex = i
			break
		}
	}

	if providerIndex == -1 {
		return RespondNotFound(c, "Import provider", "PROVIDER_NOT_FOUND")
	}

	var updateReq struct {
		Host                     *string `json:"host,omitempty"`
		Port                     *int    `json:"port,omitempty"`
		Username                 *string `json:"username,omitempty"`
		Password                 *string `json:"password,omitempty"`
		MaxConnections           *int    `json:"max_connections,omitempty"`
		InflightRequests         *int    `json:"inflight_requests,omitempty"`
		TLS                      *bool   `json:"tls,omitempty"`
		InsecureTLS              *bool   `json:"insecure_tls,omitempty"`
		ProxyURL                 *string `json:"proxy_url,omitempty"`
		Enabled                  *bool   `json:"enabled,omitempty"`
		IsBackupProvider         *bool   `json:"is_backup_provider,omitempty"`
		SkipPing                 *bool   `json:"skip_ping,omitempty"`
		KeepaliveIntervalSeconds *int    `json:"keepalive_interval_seconds,omitempty"`
		KeepaliveCommand         *string `json:"keepalive_command,omitempty"`
		UserAgent                *string `json:"user_agent,omitempty"`
		QuotaBytes               *int64  `json:"quota_bytes,omitempty"`
		QuotaPeriodHours         *int    `json:"quota_period_hours,omitempty"`
	}

	if err := c.BodyParser(&updateReq); err != nil {
		return RespondValidationError(c, "Invalid JSON in request body", err.Error())
	}

	newConfig := currentConfig.DeepCopy()
	provider := newConfig.ImportProviders[providerIndex]

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
	if updateReq.SkipPing != nil {
		provider.SkipPing = *updateReq.SkipPing
	}
	if updateReq.KeepaliveIntervalSeconds != nil {
		provider.KeepaliveIntervalSeconds = *updateReq.KeepaliveIntervalSeconds
	}
	if updateReq.KeepaliveCommand != nil {
		provider.KeepaliveCommand = *updateReq.KeepaliveCommand
	}
	if updateReq.QuotaBytes != nil {
		provider.QuotaBytes = *updateReq.QuotaBytes
	}
	if updateReq.QuotaPeriodHours != nil {
		provider.QuotaPeriodHours = *updateReq.QuotaPeriodHours
	}
	if updateReq.UserAgent != nil {
		provider.UserAgent = *updateReq.UserAgent
	}

	newConfig.ImportProviders[providerIndex] = provider

	if err := s.configManager.ValidateConfigUpdate(newConfig); err != nil {
		return RespondValidationError(c, "Configuration validation failed", err.Error())
	}

	provider = newConfig.ImportProviders[providerIndex]

	if err := s.configManager.UpdateConfig(newConfig); err != nil {
		return RespondInternalError(c, "Failed to update configuration", err.Error())
	}

	if err := s.configManager.SaveConfig(); err != nil {
		return RespondInternalError(c, "Failed to save configuration", err.Error())
	}

	response := ProviderAPIResponse{
		ID:                       provider.ID,
		Host:                     provider.Host,
		Port:                     provider.Port,
		Username:                 provider.Username,
		MaxConnections:           provider.MaxConnections,
		TLS:                      provider.TLS,
		InsecureTLS:              provider.InsecureTLS,
		ProxyURL:                 provider.ProxyURL,
		PasswordSet:              provider.Password != "",
		Enabled:                  provider.Enabled != nil && *provider.Enabled,
		IsBackupProvider:         provider.IsBackupProvider != nil && *provider.IsBackupProvider,
		InflightRequests:         provider.InflightRequests,
		LastRTTMs:                provider.LastRTTMs,
		SkipPing:                 provider.SkipPing,
		KeepaliveIntervalSeconds: provider.KeepaliveIntervalSeconds,
		KeepaliveCommand:         provider.KeepaliveCommand,
		UserAgent:                provider.UserAgent,
		QuotaBytes:               provider.QuotaBytes,
		QuotaPeriodHours:         provider.QuotaPeriodHours,
	}

	return RespondSuccess(c, response)
}

// handleDeleteImportProvider removes an import NNTP provider
//
//	@Summary		Delete import NNTP provider
//	@Description	Removes an import NNTP provider by ID from the configuration.
//	@Tags			ImportProviders
//	@Produce		json
//	@Param			id	path	string	true	"Provider ID"
//	@Success		200	{object}	APIResponse
//	@Failure		404	{object}	APIResponse
//	@Security		BearerAuth
//	@Security		ApiKeyAuth
//	@Router			/import-providers/{id} [delete]
func (s *Server) handleDeleteImportProvider(c *fiber.Ctx) error {
	if s.configManager == nil {
		return RespondServiceUnavailable(c, "Configuration management not available", "CONFIG_UNAVAILABLE")
	}

	providerID := c.Params("id")
	if providerID == "" {
		return RespondValidationError(c, "Provider ID is required", "MISSING_PROVIDER_ID")
	}

	currentConfig := s.configManager.GetConfig()
	if currentConfig == nil {
		return RespondInternalError(c, "Configuration not available", "CONFIG_NOT_FOUND")
	}

	providerIndex := -1
	for i, p := range currentConfig.ImportProviders {
		if p.ID == providerID {
			providerIndex = i
			break
		}
	}

	if providerIndex == -1 {
		return RespondNotFound(c, "Import provider", "PROVIDER_NOT_FOUND")
	}

	newConfig := currentConfig.DeepCopy()
	newConfig.ImportProviders = append(
		newConfig.ImportProviders[:providerIndex],
		newConfig.ImportProviders[providerIndex+1:]...,
	)

	if err := s.configManager.ValidateConfigUpdate(newConfig); err != nil {
		return RespondValidationError(c, "Configuration validation failed", err.Error())
	}

	if err := s.configManager.UpdateConfig(newConfig); err != nil {
		return RespondInternalError(c, "Failed to update configuration", err.Error())
	}

	if err := s.configManager.SaveConfig(); err != nil {
		return RespondInternalError(c, "Failed to save configuration", err.Error())
	}

	return RespondSuccess(c, struct {
		Message string `json:"message"`
	}{Message: "Import provider deleted successfully"})
}

// handleReorderImportProviders reorders the import provider list
//
//	@Summary		Reorder import NNTP providers
//	@Description	Sets the priority order of import NNTP providers.
//	@Tags			ImportProviders
//	@Accept			json
//	@Produce		json
//	@Param			body	body		ProviderReorderRequest	true	"Ordered list of provider IDs"
//	@Success		200		{object}	APIResponse
//	@Failure		400		{object}	APIResponse
//	@Security		BearerAuth
//	@Security		ApiKeyAuth
//	@Router			/import-providers/reorder [put]
func (s *Server) handleReorderImportProviders(c *fiber.Ctx) error {
	if s.configManager == nil {
		return RespondServiceUnavailable(c, "Configuration management not available", "CONFIG_UNAVAILABLE")
	}

	var reorderReq struct {
		ProviderIDs []string `json:"provider_ids"`
	}

	if err := c.BodyParser(&reorderReq); err != nil {
		return RespondValidationError(c, "Invalid JSON in request body", err.Error())
	}

	if len(reorderReq.ProviderIDs) == 0 {
		return RespondValidationError(c, "Provider IDs array is required", "MISSING_PROVIDER_IDS")
	}

	currentConfig := s.configManager.GetConfig()
	if currentConfig == nil {
		return RespondInternalError(c, "Configuration not available", "CONFIG_NOT_FOUND")
	}

	providerMap := make(map[string]config.ProviderConfig)
	for _, p := range currentConfig.ImportProviders {
		providerMap[p.ID] = p
	}

	if len(reorderReq.ProviderIDs) != len(currentConfig.ImportProviders) {
		return RespondValidationError(c, "Provider IDs count mismatch", "INVALID_PROVIDER_COUNT")
	}

	newProviders := make([]config.ProviderConfig, 0, len(reorderReq.ProviderIDs))
	for _, id := range reorderReq.ProviderIDs {
		provider, exists := providerMap[id]
		if !exists {
			return RespondNotFound(c, fmt.Sprintf("Import provider ID '%s'", id), "PROVIDER_NOT_FOUND")
		}
		newProviders = append(newProviders, provider)
	}

	newConfig := currentConfig.DeepCopy()
	newConfig.ImportProviders = newProviders

	if err := s.configManager.ValidateConfigUpdate(newConfig); err != nil {
		return RespondValidationError(c, "Configuration validation failed", err.Error())
	}

	if err := s.configManager.UpdateConfig(newConfig); err != nil {
		return RespondInternalError(c, "Failed to update configuration", err.Error())
	}

	if err := s.configManager.SaveConfig(); err != nil {
		return RespondInternalError(c, "Failed to save configuration", err.Error())
	}

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
			LastRTTMs:        p.LastRTTMs,
		}
	}

	return RespondSuccess(c, providers)
}

// handleTestImportProviderSpeed tests the download speed of a specific import provider
//
//	@Summary		Test import provider download speed
//	@Description	Runs a speed test against the specified import NNTP provider and saves the result to config.
//	@Tags			ImportProviders
//	@Produce		json
//	@Param			id	path	string	true	"Provider ID"
//	@Success		200	{object}	APIResponse{data=ProviderSpeedTestResponse}
//	@Failure		400	{object}	APIResponse
//	@Failure		404	{object}	APIResponse
//	@Security		BearerAuth
//	@Security		ApiKeyAuth
//	@Router			/import-providers/{id}/speedtest [post]
func (s *Server) handleTestImportProviderSpeed(c *fiber.Ctx) error {
	providerID := c.Params("id")
	if providerID == "" {
		return RespondBadRequest(c, "Provider ID is required", "")
	}

	if s.configManager == nil {
		return RespondInternalError(c, "Configuration management not available", "")
	}

	cfg := s.configManager.GetConfig()
	var targetProvider *config.ProviderConfig
	for _, p := range cfg.ImportProviders {
		if p.ID == providerID {
			targetProvider = &p
			break
		}
	}

	if targetProvider == nil {
		return RespondNotFound(c, "Import provider", "")
	}

	host := fmt.Sprintf("%s:%d", targetProvider.Host, targetProvider.Port)
	var tlsCfg *tls.Config
	if targetProvider.TLS {
		tlsCfg = &tls.Config{
			InsecureSkipVerify: targetProvider.InsecureTLS,
			ServerName:         targetProvider.Host,
		}
	}

	pool, err := nntppool.NewClient(c.Context(), []nntppool.Provider{
		{
			Host:        host,
			TLSConfig:   tlsCfg,
			Auth:        nntppool.Auth{Username: targetProvider.Username, Password: targetProvider.Password},
			Connections: targetProvider.MaxConnections,
			IdleTimeout: 60 * time.Second,
		},
	})
	if err != nil {
		return RespondInternalError(c, "Failed to create connection pool", err.Error())
	}
	defer pool.Close()

	providerName := host
	if targetProvider.Username != "" {
		providerName = host + "+" + targetProvider.Username
	}

	testCtx, cancel := context.WithTimeout(c.Context(), 5*time.Minute)
	defer cancel()

	result, err := pool.SpeedTest(testCtx, nntppool.SpeedTestOptions{
		ProviderName: providerName,
	})
	if err != nil {
		return RespondInternalError(c, "Speed test failed", err.Error())
	}

	speed := result.WireSpeedBps / 1024 / 1024

	now := time.Now()
	currentConfig := s.configManager.GetConfig()
	newConfig := currentConfig.DeepCopy()

	for i, p := range newConfig.ImportProviders {
		if p.ID == providerID {
			newConfig.ImportProviders[i].LastSpeedTestMbps = speed
			newConfig.ImportProviders[i].LastSpeedTestTime = &now
			break
		}
	}

	if err := s.configManager.UpdateConfig(newConfig); err != nil {
		slog.ErrorContext(c.Context(), "Failed to update import provider speed test result in config", "provider_id", providerID, "err", err)
		return RespondInternalError(c, "Failed to save speed test result", err.Error())
	}

	if err := s.configManager.SaveConfig(); err != nil {
		slog.ErrorContext(c.Context(), "Failed to persist config after import provider speed test", "err", err)
	}

	return RespondSuccess(c, ProviderSpeedTestResponse{
		SpeedMBps: speed,
		Duration:  result.Elapsed.Seconds(),
	})
}
