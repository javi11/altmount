package api

import (
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/gofiber/fiber/v2"
)

// handleGetSystemStats handles GET /api/system/stats
func (s *Server) handleGetSystemStats(c *fiber.Ctx) error {
	// Get queue statistics
	queueStats, err := s.queueRepo.GetQueueStats()
	if err != nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Failed to retrieve queue statistics",
			"details": err.Error(),
		})
	}

	// Get health statistics
	healthStatsMap, err := s.healthRepo.GetHealthStats()
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
		queueItemsRemoved, err = s.queueRepo.ClearCompletedQueueItems(*req.QueueOlderThan)
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

	s.logger.Info("System restart requested", "force", req.Force, "user_agent", c.Get("User-Agent"))

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
	go s.performRestart()

	return result
}

// performRestart performs the actual server restart
func (s *Server) performRestart() {
	s.logger.Info("Initiating server restart process")

	// Give a moment for the HTTP response to be sent
	time.Sleep(100 * time.Millisecond)

	// Get the current executable path
	executable, err := os.Executable()
	if err != nil {
		s.logger.Error("Failed to get executable path for restart", "error", err)
		return
	}

	s.logger.Info("Restarting server", "executable", executable, "args", os.Args)

	// Use syscall.Exec to replace the current process
	// This preserves the process ID and is the cleanest way to restart
	err = syscall.Exec(executable, os.Args, os.Environ())
	if err != nil {
		s.logger.Error("Failed to restart using syscall.Exec, trying exec.Command", "error", err)

		// Fallback: use exec.Command (this creates a new process)
		cmd := exec.Command(executable, os.Args[1:]...)
		cmd.Env = os.Environ()

		if err := cmd.Start(); err != nil {
			s.logger.Error("Failed to restart server using exec.Command", "error", err)
			return
		}

		s.logger.Info("Server restart initiated with new process", "pid", cmd.Process.Pid)

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
			ActiveConnections:    0,
			TotalBytesDownloaded: 0,
			DownloadSpeed:        0.0,
			ErrorRate:            0.0,
			CurrentMemoryUsage:   0,
			TotalConnections:     0,
			CommandSuccessRate:   0.0,
			AcquireWaitTimeMs:    0,
			LastUpdated:          time.Now(),
		}
		return c.Status(200).JSON(fiber.Map{
			"success": true,
			"data":    response,
		})
	}

	// Get the pool
	pool, err := s.poolManager.GetPool()
	if err != nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Failed to get NNTP pool",
			"details": err.Error(),
		})
	}

	// Get metrics snapshot from the pool
	snapshot := pool.GetMetricsSnapshot()

	// Map nntppool metrics to our response format
	response := PoolMetricsResponse{
		ActiveConnections:    int(snapshot.ActiveConnections),
		TotalBytesDownloaded: snapshot.TotalBytesDownloaded,
		DownloadSpeed:        snapshot.DownloadSpeed,
		ErrorRate:            snapshot.ErrorRate,
		CurrentMemoryUsage:   int64(snapshot.CurrentMemoryUsage),
		TotalConnections:     int64(snapshot.TotalConnections),
		CommandSuccessRate:   snapshot.CommandSuccessRate,
		AcquireWaitTimeMs:    int64(snapshot.AverageAcquireWaitTime.Milliseconds()),
		LastUpdated:          time.Now(),
	}

	return c.Status(200).JSON(fiber.Map{
		"success": true,
		"data":    response,
	})
}
