package api

import (
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// handleGetSystemStats handles GET /api/system/stats
func (s *Server) handleGetSystemStats(w http.ResponseWriter, r *http.Request) {
	// Get queue statistics
	queueStats, err := s.queueRepo.GetQueueStats()
	if err != nil {
		WriteInternalError(w, "Failed to retrieve queue statistics", err.Error())
		return
	}

	// Get health statistics
	healthStatsMap, err := s.healthRepo.GetHealthStats()
	if err != nil {
		WriteInternalError(w, "Failed to retrieve health statistics", err.Error())
		return
	}

	// Convert to response format
	response := SystemStatsResponse{
		Queue:  *ToQueueStatsResponse(queueStats),
		Health: *ToHealthStatsResponse(healthStatsMap),
		System: s.getSystemInfo(),
	}

	WriteSuccess(w, response, nil)
}

// handleGetSystemHealth handles GET /api/system/health
func (s *Server) handleGetSystemHealth(w http.ResponseWriter, r *http.Request) {
	// Perform health checks
	healthCheck := s.checkSystemHealth(r.Context())

	// Set appropriate HTTP status code based on health
	switch healthCheck.Status {
	case "healthy":
		WriteSuccess(w, healthCheck, nil)
	case "degraded":
		// Return 200 but indicate degraded status
		WriteSuccess(w, healthCheck, nil)
	case "unhealthy":
		// Return 503 Service Unavailable for unhealthy status
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		
		response := &APIResponse{
			Success: false,
			Data:    healthCheck,
		}
		
		json.NewEncoder(w).Encode(response)
	}
}

// handleSystemCleanup handles POST /api/system/cleanup
func (s *Server) handleSystemCleanup(w http.ResponseWriter, r *http.Request) {
	// Parse request body
	var req SystemCleanupRequest
	if r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			WriteBadRequest(w, "Invalid request body", err.Error())
			return
		}
	}

	// Parse parameters from query string if not in body
	if req.QueueOlderThan == nil {
		if queueOlderThan, err := ParseTimeParam(r, "queue_older_than"); err != nil {
			WriteValidationError(w, "Invalid queue_older_than parameter", err.Error())
			return
		} else if queueOlderThan != nil {
			req.QueueOlderThan = queueOlderThan
		}
	}

	if req.HealthOlderThan == nil {
		if healthOlderThan, err := ParseTimeParam(r, "health_older_than"); err != nil {
			WriteValidationError(w, "Invalid health_older_than parameter", err.Error())
			return
		} else if healthOlderThan != nil {
			req.HealthOlderThan = healthOlderThan
		}
	}

	// Parse dry_run parameter
	if dryRunStr := r.URL.Query().Get("dry_run"); dryRunStr != "" {
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
			WriteInternalError(w, "Failed to cleanup queue items", err.Error())
			return
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
		DryRun:              req.DryRun,
	}

	WriteSuccess(w, response, nil)
}

// handleSystemRestart handles POST /api/system/restart
func (s *Server) handleSystemRestart(w http.ResponseWriter, r *http.Request) {
	// Parse request body if present
	var req SystemRestartRequest
	if r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			WriteBadRequest(w, "Invalid request body", err.Error())
			return
		}
	}

	s.logger.Info("System restart requested", "force", req.Force, "user_agent", r.UserAgent())

	// Prepare response
	response := SystemRestartResponse{
		Message:   "Server restart initiated. The server will restart shortly.",
		Timestamp: time.Now(),
	}

	// Send response immediately before restart
	WriteSuccess(w, response, nil)

	// Start restart process in a goroutine to allow response to be sent
	go s.performRestart()
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
func (s *Server) handleGetPoolMetrics(w http.ResponseWriter, r *http.Request) {
	// Check if pool manager is available
	if s.poolManager == nil {
		WriteInternalError(w, "Pool manager not available", "NNTP pool manager not configured")
		return
	}

	// Check if pool is available
	if !s.poolManager.HasPool() {
		response := PoolMetricsResponse{
			ActiveConnections:       0,
			TotalBytesDownloaded:    0,
			DownloadSpeed:           0.0,
			ErrorRate:               0.0,
			CurrentMemoryUsage:      0,
			TotalConnections:        0,
			CommandSuccessRate:      0.0,
			AcquireWaitTimeMs:       0,
			LastUpdated:             time.Now(),
		}
		WriteSuccess(w, response, nil)
		return
	}

	// Get the pool
	pool, err := s.poolManager.GetPool()
	if err != nil {
		WriteInternalError(w, "Failed to get NNTP pool", err.Error())
		return
	}

	// Get metrics snapshot from the pool
	snapshot := pool.GetMetricsSnapshot()

	// Map nntppool metrics to our response format
	response := PoolMetricsResponse{
		ActiveConnections:       int(snapshot.ActiveConnections),
		TotalBytesDownloaded:    snapshot.TotalBytesDownloaded,
		DownloadSpeed:           snapshot.DownloadSpeed,
		ErrorRate:               snapshot.ErrorRate,
		CurrentMemoryUsage:      int64(snapshot.CurrentMemoryUsage),
		TotalConnections:        int64(snapshot.TotalConnections),
		CommandSuccessRate:      snapshot.CommandSuccessRate,
		AcquireWaitTimeMs:       int64(snapshot.AverageAcquireWaitTime.Milliseconds()),
		LastUpdated:             time.Now(),
	}

	WriteSuccess(w, response, nil)
}