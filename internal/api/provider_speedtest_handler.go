package api

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/nntppool/v2"
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

	// 1. Download Test NZB (100MB)
	// We use the 100MB file as it's large enough for a decent test but not too huge
	nzbURL := "https://sabnzbd.org/tests/test_download_100MB.nzb"
	
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

	// 3. Run Speed Test
	poolCfg := nntppool.Config{
		Providers: []nntppool.UsenetProviderConfig{
			{
				Host:                           targetProvider.Host,
				Port:                           targetProvider.Port,
				Username:                       targetProvider.Username,
				Password:                       targetProvider.Password,
				TLS:                            targetProvider.TLS,
				MaxConnections:                 targetProvider.MaxConnections,
				InsecureSSL:                    targetProvider.InsecureTLS,
				MaxConnectionIdleTimeInSeconds: 60,
				MaxConnectionTTLInSeconds:      60,
			},
		},
		Logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		DelayType:      nntppool.DelayTypeFixed,
		RetryDelay:     10 * time.Millisecond,
		MinConnections: 0,
	}

	pool, err := nntppool.NewConnectionPool(poolCfg)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Failed to create connection pool",
			"details": err.Error(),
		})
	}
	defer pool.Quit()

	// Test for up to 10 seconds
	testCtx, cancel := context.WithTimeout(c.Context(), 10*time.Second)
	defer cancel()

	var totalBytes int64
	var workerWg sync.WaitGroup

	numWorkers := targetProvider.MaxConnections
	if numWorkers <= 0 {
		numWorkers = 10
	}
	if numWorkers > 20 {
		numWorkers = 20 // Cap for test to avoid resource exhaustion on server
	}

	segmentChan := make(chan segmentInfo, len(allSegments))
	for _, s := range allSegments {
		segmentChan <- s
	}
	close(segmentChan)

	startTime := time.Now()

	for i := 0; i < numWorkers; i++ {
		workerWg.Add(1)
		go func() {
			defer workerWg.Done()
			for {
				select {
				case <-testCtx.Done():
					return
				case seg, ok := <-segmentChan:
					if !ok {
						return
					}
					// Download
					_, err := pool.Body(testCtx, seg.ID, io.Discard, seg.Groups)
					if err == nil {
						atomic.AddInt64(&totalBytes, seg.Size)
					}
				}
			}
		}()
	}

	workerWg.Wait()
	duration := time.Since(startTime)

	if duration.Seconds() == 0 {
		return c.Status(200).JSON(fiber.Map{
			"success": true,
			"data": ProviderSpeedTestResponse{
				SpeedMBps: 0,
				Duration:  0,
			},
		})
	}

	mb := float64(totalBytes) / 1024 / 1024
	speed := mb / duration.Seconds()

	return c.Status(200).JSON(fiber.Map{
		"success": true,
		"data": ProviderSpeedTestResponse{
			SpeedMBps: speed,
			Duration:  duration.Seconds(),
		},
	})
}
