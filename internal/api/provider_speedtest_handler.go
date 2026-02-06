package api

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/altmount/internal/pool"
	"github.com/javi11/nntppool/v3"
	"github.com/javi11/nzbparser"
)

type ProviderSpeedTestResponse struct {
	SpeedMBps float64 `json:"speed_mbps"`
	Duration  float64 `json:"duration_seconds"`
}

// handleTestProviderSpeed tests the download speed of a specific provider
func (s *Server) handleTestProviderSpeed(c *fiber.Ctx) error {
	providerID := c.Params("id")
	if providerID == "" {
		return c.Status(400).JSON(fiber.Map{
			"success": false,
			"message": "Provider ID is required",
		})
	}

	if s.configManager == nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Configuration management not available",
		})
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
		return c.Status(404).JSON(fiber.Map{
			"success": false,
			"message": "Provider not found",
		})
	}

	// 1. Download Test NZB (1GB)
	// We use the 1GB file to ensure high-speed connections are properly tested
	nzbURL := "https://sabnzbd.org/tests/test_download_1GB.nzb"

	req, err := http.NewRequestWithContext(c.Context(), http.MethodGet, nzbURL, nil)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Failed to create request for test NZB",
			"details": err.Error(),
		})
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return c.Status(502).JSON(fiber.Map{
			"success": false,
			"message": "Failed to download test NZB",
			"details": err.Error(),
		})
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return c.Status(502).JSON(fiber.Map{
			"success": false,
			"message": "Failed to download test NZB",
			"details": fmt.Sprintf("Status: %s", resp.Status),
		})
	}

	// 2. Parse NZB
	nzbFile, err := nzbparser.Parse(resp.Body)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Failed to parse test NZB",
			"details": err.Error(),
		})
	}

	type segmentInfo struct {
		ID     string
		Groups []string
		Size   int64
	}

	var allSegments []segmentInfo
	for _, file := range nzbFile.Files {
		for _, seg := range file.Segments {
			allSegments = append(allSegments, segmentInfo{
				ID:     seg.ID,
				Groups: file.Groups,
				Size:   int64(seg.Bytes),
			})
		}
	}

	if len(allSegments) == 0 {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "No segments found in test NZB",
		})
	}

	// 3. Run Speed Test - Create provider and client
	provider, err := pool.NewProvider(c.Context(), *targetProvider, pool.ProviderOptions{
		ConnIdleTime: 60 * time.Second,
		ConnLifetime: 60 * time.Second,
	})
	if err != nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Failed to create provider",
			"details": err.Error(),
		})
	}

	client := nntppool.NewClient()
	err = client.AddProvider(provider, nntppool.ProviderPrimary)
	if err != nil {
		slog.Error("Failed to add provider to client", "error", err, "provider_id", providerID)
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": err.Error(),
		})
	}

	defer client.Close()

	// Test for up to 5 minutes
	testCtx, cancel := context.WithTimeout(c.Context(), 5*time.Minute)
	defer cancel()

	type pendingSegment struct {
		respCh <-chan nntppool.Response
		size   int64
	}

	startTime := time.Now()

	// Phase 1: Fire all async requests
	// The pool's inflightSem provides natural backpressure
	pending := make([]pendingSegment, 0, len(allSegments))
	for _, seg := range allSegments {
		ch := client.BodyAsync(testCtx, seg.ID, io.Discard)
		pending = append(pending, pendingSegment{respCh: ch, size: seg.Size})
	}

	// Phase 2: Drain all responses
	var totalBytes int64
	var testErr error
	for _, p := range pending {
		resp := <-p.respCh
		if resp.Err != nil {
			if testErr == nil {
				testErr = resp.Err
			}
			continue
		}
		totalBytes += p.size
	}

	if testErr != nil {
		slog.Error("Failed to test provider speed", "error", testErr, "provider_id", providerID)
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": testErr.Error(),
		})
	}

	duration := time.Since(startTime)

	var speed float64
	if duration.Seconds() == 0 {
		speed = 0
	} else {
		mb := float64(totalBytes) / 1024 / 1024
		speed = mb / duration.Seconds()
	}

	// Update provider config with speed test result
	now := time.Now()
	// Create a copy of the config to modify
	currentConfig := s.configManager.GetConfig()
	newConfig := currentConfig.DeepCopy() // DeepCopy ensures we don't modify the live config directly

	// Find the provider in the new config and update its fields
	for i, p := range newConfig.Providers {
		if p.ID == providerID {
			newConfig.Providers[i].LastSpeedTestMbps = speed
			newConfig.Providers[i].LastSpeedTestTime = &now
			break
		}
	}

	if err := s.configManager.UpdateConfig(newConfig); err != nil {
		slog.Error("Failed to update provider speed test result in config", "provider_id", providerID, "err", err)
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Failed to save speed test result",
			"details": err.Error(),
		})
	}

	// Persist changes to disk
	if err := s.configManager.SaveConfig(); err != nil {
		slog.Error("Failed to persist config after speed test", "err", err)
		// We don't fail the request since the test was successful and in-memory config is updated
	}

	return c.Status(200).JSON(fiber.Map{
		"success": true,
		"data": ProviderSpeedTestResponse{
			SpeedMBps: speed,
			Duration:  duration.Seconds(),
		},
	})
}
