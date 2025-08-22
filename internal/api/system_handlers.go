package api

import (
	"encoding/json"
	"net/http"
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
	// For now, we'll return a message indicating the restart command was received
	// In a production system, this would trigger a graceful shutdown and restart
	response := map[string]interface{}{
		"message": "Restart command received. Please restart the server manually.",
		"timestamp": time.Now(),
		"note": "Automatic restart is not implemented for safety. Please use your process manager or restart manually.",
	}
	
	WriteSuccess(w, response, nil)
}

// Additional system management endpoints could be added here, such as:
// - handleGetSystemConfig - GET /api/system/config - Get system configuration
// - handleUpdateSystemConfig - PUT /api/system/config - Update system configuration
// - handleGetSystemMetrics - GET /api/system/metrics - Get detailed system metrics
// - handleSystemMaintenance - POST /api/system/maintenance - Perform maintenance tasks