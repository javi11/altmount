package api

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/gofiber/fiber/v2"
)

// handleGetSystemStats handles GET /api/system/stats
func (s *Server) handleGetSystemStats(c *fiber.Ctx) error {
	// Get queue statistics
	queueStats, err := s.queueRepo.GetQueueStats(c.Context())
	if err != nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Failed to retrieve queue statistics",
			"details": err.Error(),
		})
	}

	// Get health statistics
	healthStatsMap, err := s.healthRepo.GetHealthStats(c.Context())
	if err != nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Failed to retrieve health statistics",
			"details": err.Error(),
		})
	}

	// Convert to response format
	response := SystemStatsResponse{
		Queue:  *ToQueueStatsResponse(queueStats),
		Health: *ToHealthStatsResponse(healthStatsMap),
		System: s.getSystemInfo(),
	}

	return c.Status(200).JSON(fiber.Map{
		"success": true,
		"data":    response,
	})
}

// handleGetSystemHealth handles GET /api/system/health
func (s *Server) handleGetSystemHealth(c *fiber.Ctx) error {
	// Perform health checks
	healthCheck := s.checkSystemHealth(c.Context())

	// Set appropriate HTTP status code based on health
	switch healthCheck.Status {
	case "healthy":
		return c.Status(200).JSON(fiber.Map{
			"success": true,
			"data":    healthCheck,
		})
	case "degraded":
		// Return 200 but indicate degraded status
		return c.Status(200).JSON(fiber.Map{
			"success": true,
			"data":    healthCheck,
		})
	case "unhealthy":
		// Return 503 Service Unavailable for unhealthy status
		return c.Status(503).JSON(fiber.Map{
			"success": false,
			"data":    healthCheck,
		})
	}

	// Default case (shouldn't reach here)
	return c.Status(500).JSON(fiber.Map{
		"success": false,
		"message": "Unknown health status",
	})
}

// handleSystemCleanup handles POST /api/system/cleanup
func (s *Server) handleSystemCleanup(c *fiber.Ctx) error {
	// Parse request body
	var req SystemCleanupRequest
	if len(c.Body()) > 0 {
		if err := c.BodyParser(&req); err != nil {
			return c.Status(400).JSON(fiber.Map{
				"success": false,
				"message": "Invalid request body",
				"details": err.Error(),
			})
		}
	}

	// Parse parameters from query string if not in body
	if req.QueueOlderThan == nil {
		if queueOlderThan, err := ParseTimeParamFiber(c, "queue_older_than"); err != nil {
			return c.Status(422).JSON(fiber.Map{
				"success": false,
				"message": "Invalid queue_older_than parameter",
				"details": err.Error(),
			})
		} else if queueOlderThan != nil {
			req.QueueOlderThan = queueOlderThan
		}
	}

	if req.HealthOlderThan == nil {
		if healthOlderThan, err := ParseTimeParamFiber(c, "health_older_than"); err != nil {
			return c.Status(422).JSON(fiber.Map{
				"success": false,
				"message": "Invalid health_older_than parameter",
				"details": err.Error(),
			})
		} else if healthOlderThan != nil {
			req.HealthOlderThan = healthOlderThan
		}
	}

	// Parse dry_run parameter
	if dryRunStr := c.Query("dry_run"); dryRunStr != "" {
		req.DryRun = dryRunStr == "true"
	}

	// Set default cleanup times if not specified
	if req.QueueOlderThan == nil {
		// Default: clean queue items older than 7 days
		defaultTime := time.Now().Add(-7 * 24 * time.Hour)
		req.QueueOlderThan = &defaultTime
	}

	if req.HealthOlderThan == nil {
		// Default: clean health records older than 30 days
		defaultTime := time.Now().Add(-30 * 24 * time.Hour)
		req.HealthOlderThan = &defaultTime
	}

	// Perform cleanup operations
	var queueItemsRemoved, healthRecordsRemoved int
	var err error

	// Clean up queue items
	if !req.DryRun {
		queueItemsRemoved, err = s.queueRepo.ClearCompletedQueueItems(c.Context())
		if err != nil {
			return c.Status(500).JSON(fiber.Map{
				"success": false,
				"message": "Failed to cleanup queue items",
				"details": err.Error(),
			})
		}
	} else {
		// For dry run, we could count what would be removed
		// For now, we'll just return 0
		queueItemsRemoved = 0
	}

	// Clean up health records
	if !req.DryRun {
		// The current health repository doesn't have a time-based cleanup method
		// We'll need to implement this or return 0 for now
		healthRecordsRemoved = 0
	} else {
		healthRecordsRemoved = 0
	}

	// Prepare response
	response := SystemCleanupResponse{
		QueueItemsRemoved:    queueItemsRemoved,
		HealthRecordsRemoved: healthRecordsRemoved,
		DryRun:               req.DryRun,
	}

	return c.Status(200).JSON(fiber.Map{
		"success": true,
		"data":    response,
	})
}

// handleSystemRestart handles POST /api/system/restart
func (s *Server) handleSystemRestart(c *fiber.Ctx) error {
	// Parse request body if present
	var req SystemRestartRequest
	if len(c.Body()) > 0 {
		if err := c.BodyParser(&req); err != nil {
			return c.Status(400).JSON(fiber.Map{
				"success": false,
				"message": "Invalid request body",
				"details": err.Error(),
			})
		}
	}

	slog.InfoContext(c.Context(), "System restart requested", "force", req.Force, "user_agent", c.Get("User-Agent"))

	// Prepare response
	response := SystemRestartResponse{
		Message:   "Server restart initiated. The server will restart shortly.",
		Timestamp: time.Now(),
	}

	// Send response immediately before restart
	result := c.Status(200).JSON(fiber.Map{
		"success": true,
		"data":    response,
	})

	// Start restart process in a goroutine to allow response to be sent
	go s.performRestart(c.Context())

	return result
}

// performRestart performs the actual server restart
func (s *Server) performRestart(ctx context.Context) {
	slog.InfoContext(ctx, "Initiating server restart process")

	// Give a moment for the HTTP response to be sent
	time.Sleep(100 * time.Millisecond)

	// Get the current executable path
	executable, err := os.Executable()
	if err != nil {
		slog.ErrorContext(ctx, "Failed to get executable path for restart", "error", err)
		return
	}

	slog.InfoContext(ctx, "Restarting server", "executable", executable, "args", os.Args)

	// Use syscall.Exec to replace the current process
	// This preserves the process ID and is the cleanest way to restart
	err = syscall.Exec(executable, os.Args, os.Environ())
	if err != nil {
		slog.ErrorContext(ctx, "Failed to restart using syscall.Exec, trying exec.Command", "error", err)

		// Fallback: use exec.Command (this creates a new process)
		cmd := exec.CommandContext(ctx, executable, os.Args[1:]...)
		cmd.Env = os.Environ()

		if err := cmd.Start(); err != nil {
			slog.ErrorContext(ctx, "Failed to restart server using exec.Command", "error", err)
			return
		}

		slog.InfoContext(ctx, "Server restart initiated with new process", "pid", cmd.Process.Pid)

		// Exit the current process
		os.Exit(0)
	}
}

// handleGetPoolMetrics handles GET /api/system/pool/metrics
func (s *Server) handleGetPoolMetrics(c *fiber.Ctx) error {
	// Check if pool manager is available
	if s.poolManager == nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Pool manager not available",
			"details": "NNTP pool manager not configured",
		})
	}

	// Check if pool is available
	if !s.poolManager.HasPool() {
		response := PoolMetricsResponse{
			BytesDownloaded:          0,
			BytesUploaded:            0,
			ArticlesDownloaded:       0,
			ArticlesPosted:           0,
			TotalErrors:              0,
			ProviderErrors:           make(map[string]int64),
			DownloadSpeedBytesPerSec: 0.0,
			UploadSpeedBytesPerSec:   0.0,
			Timestamp:                time.Now(),
			Providers:                []ProviderStatusResponse{},
		}
		return c.Status(200).JSON(fiber.Map{
			"success": true,
			"data":    response,
		})
	}

	// Get metrics from the pool manager (includes calculated speeds)
	metrics, err := s.poolManager.GetMetrics()
	if err != nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Failed to get NNTP pool metrics",
			"details": err.Error(),
		})
	}

	// Get the pool to fetch provider information
	pool, err := s.poolManager.GetPool()
	if err != nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Failed to get NNTP pool",
			"details": err.Error(),
		})
	}

	// Get provider information from pool
	providersInfo := pool.GetProvidersInfo()

	// Get current configuration to access speed test results
	config := s.configManager.GetConfig()
	providerConfigMap := make(map[string]struct {
		Speed float64
		Time  *time.Time
	})
	if config != nil {
		for _, p := range config.Providers {
			providerConfigMap[p.ID] = struct {
				Speed float64
				Time  *time.Time
			}{
				Speed: p.LastSpeedTestMbps,
				Time:  p.LastSpeedTestTime,
			}
		}
	}

	// Map provider info to response format
	providers := make([]ProviderStatusResponse, 0, len(providersInfo))
	for _, providerInfo := range providersInfo {
		// Get error count for this provider from metrics
		errorCount := int64(0)
		if metrics.ProviderErrors != nil {
			// Try matching by provider ID first
			if count, exists := metrics.ProviderErrors[providerInfo.ID()]; exists {
				errorCount = count
			} else {
				// Fallback: Try matching by provider Host if ID doesn't work (might be a mismatch in nntppool keys)
				if count, exists := metrics.ProviderErrors[providerInfo.Host]; exists {
					errorCount = count
				}
			}
		}

		// Get speed test info
		var lastSpeedTestMbps float64
		var lastSpeedTestTime *time.Time

		// Try to look up by ID first
		if info, ok := providerConfigMap[providerInfo.ID()]; ok {
			lastSpeedTestMbps = info.Speed
			lastSpeedTestTime = info.Time
		} else if config != nil {
			// Fallback: try to find matching provider in config by Host and Username
			// This handles cases where nntppool might use a different ID scheme or ID is empty
			for _, p := range config.Providers {
				if p.Host == providerInfo.Host && p.Username == providerInfo.Username {
					lastSpeedTestMbps = p.LastSpeedTestMbps
					lastSpeedTestTime = p.LastSpeedTestTime
					break
				}
			}
		}

		providers = append(providers, ProviderStatusResponse{
			ID:                    providerInfo.ID(),
			Host:                  providerInfo.Host,
			Username:              providerInfo.Username,
			UsedConnections:       providerInfo.UsedConnections,
			MaxConnections:        providerInfo.MaxConnections,
			State:                 providerInfo.State.String(),
			ErrorCount:            errorCount,
			LastConnectionAttempt: providerInfo.LastConnectionAttempt,
			LastSuccessfulConnect: providerInfo.LastSuccessfulConnect,
			FailureReason:         providerInfo.FailureReason,
			LastSpeedTestMbps:     lastSpeedTestMbps,
			LastSpeedTestTime:     lastSpeedTestTime,
		})
	}

	// Map pool metrics to API response format
	response := PoolMetricsResponse{
		BytesDownloaded:             metrics.BytesDownloaded,
		BytesUploaded:               metrics.BytesUploaded,
		ArticlesDownloaded:          metrics.ArticlesDownloaded,
		ArticlesPosted:              metrics.ArticlesPosted,
		TotalErrors:                 metrics.TotalErrors,
		ProviderErrors:              metrics.ProviderErrors,
		DownloadSpeedBytesPerSec:    metrics.DownloadSpeedBytesPerSec,
		MaxDownloadSpeedBytesPerSec: metrics.MaxDownloadSpeedBytesPerSec,
		UploadSpeedBytesPerSec:      metrics.UploadSpeedBytesPerSec,
		Timestamp:                   metrics.Timestamp,
		Providers:                   providers,
	}

	return c.Status(200).JSON(fiber.Map{
		"success": true,
		"data":    response,
	})
}

// FileEntry represents a file or directory in the system browser
type FileEntry struct {
	Name    string    `json:"name"`
	Path    string    `json:"path"`
	IsDir   bool      `json:"is_dir"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"mod_time"`
}

// handleSystemBrowse handles GET /api/system/browse
func (s *Server) handleSystemBrowse(c *fiber.Ctx) error {
	path := c.Query("path")
	if path == "" {
		// Default to root or current working directory
		var err error
		path, err = os.Getwd()
		if err != nil {
			path = "/"
		}
	}

	// Read directory
	entries, err := os.ReadDir(path)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Failed to read directory",
			"details": err.Error(),
		})
	}

	var files []FileEntry
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}

		// Skip hidden files if desired, but for system browsing we might want them
		// For now, let's keep them

		files = append(files, FileEntry{
			Name:    entry.Name(),
			Path:    filepath.Join(path, entry.Name()),
			IsDir:   entry.IsDir(),
			Size:    info.Size(),
			ModTime: info.ModTime(),
		})
	}

	return c.Status(200).JSON(fiber.Map{
		"success": true,
		"data": fiber.Map{
			"current_path": path,
			"parent_path":  filepath.Dir(path),
			"files":        files,
		},
	})
}
