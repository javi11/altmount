package api

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/nntppool/v4"
)

type ProviderSpeedTestResponse struct {
	SpeedMBps float64 `json:"speed_mbps"`
	Duration  float64 `json:"duration_seconds"`
}

// handleTestProviderSpeed tests the download speed of a specific provider
func (s *Server) handleTestProviderSpeed(c *fiber.Ctx) error {
	providerID := c.Params("id")
	if providerID == "" {
		return RespondBadRequest(c, "Provider ID is required", "")
	}

	if s.configManager == nil {
		return RespondInternalError(c, "Configuration management not available", "")
	}

	cfg := s.configManager.GetConfig()
	var targetProvider *config.ProviderConfig
	for _, p := range cfg.Providers {
		if p.ID == providerID {
			targetProvider = &p
			break
		}
	}

	if targetProvider == nil {
		return RespondNotFound(c, "Provider", "")
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

	// Resolve provider name matching nntppool's resolveProviderName logic
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

	speed := result.WireSpeedBps / 1024 / 1024 // bytes/sec → MB/s

	// Update provider config with speed test result
	now := time.Now()
	currentConfig := s.configManager.GetConfig()
	newConfig := currentConfig.DeepCopy()

	for i, p := range newConfig.Providers {
		if p.ID == providerID {
			newConfig.Providers[i].LastSpeedTestMbps = speed
			newConfig.Providers[i].LastSpeedTestTime = &now
			break
		}
	}

	if err := s.configManager.UpdateConfig(newConfig); err != nil {
		slog.Error("Failed to update provider speed test result in config", "provider_id", providerID, "err", err)
		return RespondInternalError(c, "Failed to save speed test result", err.Error())
	}

	if err := s.configManager.SaveConfig(); err != nil {
		slog.Error("Failed to persist config after speed test", "err", err)
	}

	return RespondSuccess(c, ProviderSpeedTestResponse{
		SpeedMBps: speed,
		Duration:  result.Elapsed.Seconds(),
	})
}
