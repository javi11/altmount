package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/javi11/altmount/internal/database"
)

// handleListHealth handles GET /api/health
func (s *Server) handleListHealth(w http.ResponseWriter, r *http.Request) {
	// Parse pagination
	pagination := ParsePagination(r)

	// Parse search parameter
	search := r.URL.Query().Get("search")

	// Parse status filter
	var statusFilter *database.HealthStatus
	if statusStr := r.URL.Query().Get("status"); statusStr != "" {
		status := database.HealthStatus(statusStr)
		// Validate status
		switch status {
		case database.HealthStatusHealthy, database.HealthStatusPartial, database.HealthStatusCorrupted, database.HealthStatusRepairTriggered:
			statusFilter = &status
		default:
			WriteValidationError(w, "Invalid status filter", "Valid values: healthy, partial, corrupted, repair_triggered")
			return
		}
	}

	// Parse since filter
	var sinceFilter *time.Time
	if since, err := ParseTimeParam(r, "since"); err != nil {
		WriteValidationError(w, "Invalid since parameter", err.Error())
		return
	} else if since != nil {
		sinceFilter = since
	}

	// Get health items with search support
	items, err := s.listHealthItems(statusFilter, pagination, sinceFilter, search)
	if err != nil {
		WriteInternalError(w, "Failed to retrieve health records", err.Error())
		return
	}

	// Get total count for pagination
	totalCount, err := s.countHealthItems(statusFilter, sinceFilter, search)
	if err != nil {
		WriteInternalError(w, "Failed to count health records", err.Error())
		return
	}

	// Convert to API response format
	response := make([]*HealthItemResponse, len(items))
	for i, item := range items {
		response[i] = ToHealthItemResponse(item)
	}

	// Create metadata
	meta := &APIMeta{
		Count:  len(response),
		Limit:  pagination.Limit,
		Offset: pagination.Offset,
		Total:  totalCount,
	}

	WriteSuccess(w, response, meta)
}

// listHealthItems is a helper method to list health items with filters
func (s *Server) listHealthItems(statusFilter *database.HealthStatus, pagination Pagination, sinceFilter *time.Time, search string) ([]*database.FileHealth, error) {
	return s.healthRepo.ListHealthItems(statusFilter, pagination.Limit, pagination.Offset, sinceFilter, search)
}

// countHealthItems is a helper method to count health items with filters
func (s *Server) countHealthItems(statusFilter *database.HealthStatus, sinceFilter *time.Time, search string) (int, error) {
	return s.healthRepo.CountHealthItems(statusFilter, sinceFilter, search)
}

// handleGetHealth handles GET /api/health/{id}
func (s *Server) handleGetHealth(w http.ResponseWriter, r *http.Request) {
	// Extract ID from path parameter - this could be either numeric ID or file path
	idStr := r.PathValue("id")
	if idStr == "" {
		WriteBadRequest(w, "Health record identifier is required", "")
		return
	}

	// Try to parse as numeric ID first
	if _, err := strconv.ParseInt(idStr, 10, 64); err == nil {
		// Get by ID - we'd need to implement this method in the repository
		WriteInternalError(w, "Get health by ID not yet implemented", "Use file path instead")
		return
	} else {
		// Treat as file path
		item, err := s.healthRepo.GetFileHealth(idStr)
		if err != nil {
			WriteInternalError(w, "Failed to retrieve health record", err.Error())
			return
		}

		if item == nil {
			WriteNotFound(w, "Health record not found", "")
			return
		}

		response := ToHealthItemResponse(item)
		WriteSuccess(w, response, nil)
	}
}

// handleDeleteHealth handles DELETE /api/health/{id}
func (s *Server) handleDeleteHealth(w http.ResponseWriter, r *http.Request) {
	// Extract ID from path parameter
	filePath := r.PathValue("id")
	if filePath == "" {
		WriteBadRequest(w, "Health record identifier is required", "")
		return
	}

	// Check if the record exists
	item, err := s.healthRepo.GetFileHealth(filePath)
	if err != nil {
		WriteInternalError(w, "Failed to check health record", err.Error())
		return
	}

	if item == nil {
		WriteNotFound(w, "Health record not found", "")
		return
	}

	// If the item is currently being checked, cancel the check first
	if item.Status == database.HealthStatusChecking {
		// Check if health worker is available
		if s.healthWorker != nil {
			// Check if there's actually an active check to cancel
			if s.healthWorker.IsCheckActive(filePath) {
				// Cancel the health check before deletion
				err = s.healthWorker.CancelHealthCheck(filePath)
				if err != nil {
					WriteInternalError(w, "Failed to cancel health check before deletion", err.Error())
					return
				}
			}
		}
	}

	// Delete the health record from database
	err = s.healthRepo.DeleteHealthRecord(filePath)
	if err != nil {
		WriteInternalError(w, "Failed to delete health record", err.Error())
		return
	}

	response := map[string]interface{}{
		"message":    "Health record deleted successfully",
		"file_path":  filePath,
		"deleted_at": time.Now().Format(time.RFC3339),
	}

	WriteSuccess(w, response, nil)
}

// handleRepairHealth handles POST /api/health/{id}/repair
func (s *Server) handleRepairHealth(w http.ResponseWriter, r *http.Request) {
	// Extract ID from path parameter
	filePath := r.PathValue("id")
	if filePath == "" {
		WriteBadRequest(w, "Health record identifier is required", "")
		return
	}

	// Parse request body
	var req HealthRepairRequest
	if r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			WriteBadRequest(w, "Invalid request body", err.Error())
			return
		}
	}

	// Check if item exists
	item, err := s.healthRepo.GetFileHealth(filePath)
	if err != nil {
		WriteInternalError(w, "Failed to check health record", err.Error())
		return
	}

	if item == nil {
		WriteNotFound(w, "Health record not found", "")
		return
	}

	// Only allow repair of corrupted or partial files
	if item.Status != database.HealthStatusCorrupted && item.Status != database.HealthStatusPartial {
		WriteConflict(w, "Can only repair corrupted or partial files", "Current status: "+string(item.Status))
		return
	}

	// Get media file information to determine which ARR instance to use for repair
	mediaFiles, err := s.mediaRepo.GetMediaFilesByPath(filePath)
	if err != nil {
		WriteInternalError(w, "Failed to get media file information", err.Error())
		return
	}

	if len(mediaFiles) == 0 {
		WriteBadRequest(w, "Cannot repair file", "File is not tracked in any ARR instance")
		return
	}

	// Try to repair using the first available media file
	// (in practice, a file path should only be associated with one ARR instance)
	mediaFile := mediaFiles[0]

	// Trigger repair through ARR service
	ctx := r.Context()
	err = s.arrsService.TriggerFileRescan(ctx, mediaFile.InstanceType, mediaFile.InstanceName, &mediaFile)
	if err != nil {
		WriteInternalError(w, "Failed to trigger repair in ARR instance", err.Error())
		return
	}

	// Set repair triggered status after successful ARR notification
	err = s.healthRepo.SetRepairTriggered(filePath, nil)
	if err != nil {
		WriteInternalError(w, "Failed to update repair status", "ARR repair triggered but failed to update database: "+err.Error())
		return
	}

	// Get updated item
	updatedItem, err := s.healthRepo.GetFileHealth(filePath)
	if err != nil {
		WriteInternalError(w, "Failed to retrieve updated health record", err.Error())
		return
	}

	response := ToHealthItemResponse(updatedItem)
	WriteSuccess(w, response, nil)
}

// handleListCorrupted handles GET /api/health/corrupted
func (s *Server) handleListCorrupted(w http.ResponseWriter, r *http.Request) {
	// Parse pagination
	pagination := ParsePagination(r)

	// Get corrupted files using GetUnhealthyFiles
	items, err := s.healthRepo.GetUnhealthyFiles(pagination.Limit)
	if err != nil {
		WriteInternalError(w, "Failed to retrieve corrupted files", err.Error())
		return
	}

	// Filter to only corrupted files (GetUnhealthyFiles returns all unhealthy)
	corruptedItems := make([]*database.FileHealth, 0)
	for _, item := range items {
		if item.Status == database.HealthStatusCorrupted {
			corruptedItems = append(corruptedItems, item)
		}
	}

	// Apply offset
	if pagination.Offset >= len(corruptedItems) {
		corruptedItems = []*database.FileHealth{}
	} else {
		end := pagination.Offset + pagination.Limit
		if end > len(corruptedItems) {
			end = len(corruptedItems)
		}
		corruptedItems = corruptedItems[pagination.Offset:end]
	}

	// Convert to API response format
	response := make([]*HealthItemResponse, len(corruptedItems))
	for i, item := range corruptedItems {
		response[i] = ToHealthItemResponse(item)
	}

	// Create metadata
	meta := &APIMeta{
		Count:  len(response),
		Limit:  pagination.Limit,
		Offset: pagination.Offset,
	}

	WriteSuccess(w, response, meta)
}

// handleGetHealthStats handles GET /api/health/stats
func (s *Server) handleGetHealthStats(w http.ResponseWriter, r *http.Request) {
	stats, err := s.healthRepo.GetHealthStats()
	if err != nil {
		WriteInternalError(w, "Failed to retrieve health statistics", err.Error())
		return
	}

	response := ToHealthStatsResponse(stats)
	WriteSuccess(w, response, nil)
}

// handleCleanupHealth handles DELETE /api/health/cleanup
func (s *Server) handleCleanupHealth(w http.ResponseWriter, r *http.Request) {
	// Parse request body
	var req HealthCleanupRequest
	if r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			WriteBadRequest(w, "Invalid request body", err.Error())
			return
		}
	}

	// Parse older_than parameter from query if not in body
	if req.OlderThan == nil {
		if olderThan, err := ParseTimeParam(r, "older_than"); err != nil {
			WriteValidationError(w, "Invalid older_than parameter", err.Error())
			return
		} else if olderThan != nil {
			req.OlderThan = olderThan
		}
	}

	// Parse status parameter from query if not in body
	if req.Status == nil {
		if statusStr := r.URL.Query().Get("status"); statusStr != "" {
			status := database.HealthStatus(statusStr)
			switch status {
			case database.HealthStatusHealthy, database.HealthStatusPartial, database.HealthStatusCorrupted, database.HealthStatusRepairTriggered:
				req.Status = &status
			default:
				WriteValidationError(w, "Invalid status filter", "Valid values: healthy, partial, corrupted, repair_triggered")
				return
			}
		}
	}

	// Default to 7 days ago if not specified
	if req.OlderThan == nil {
		defaultTime := time.Now().Add(-7 * 24 * time.Hour)
		req.OlderThan = &defaultTime
	}

	// For now, we can only cleanup all records or none (the repository doesn't support selective cleanup)
	// We'll need to implement a more sophisticated cleanup method
	count, err := s.cleanupHealthRecords(*req.OlderThan, req.Status)
	if err != nil {
		WriteInternalError(w, "Failed to cleanup health records", err.Error())
		return
	}

	response := map[string]interface{}{
		"removed_count": count,
		"older_than":    req.OlderThan.Format(time.RFC3339),
		"status_filter": req.Status,
	}

	WriteSuccess(w, response, nil)
}

// cleanupHealthRecords is a helper method to cleanup health records
func (s *Server) cleanupHealthRecords(olderThan time.Time, statusFilter *database.HealthStatus) (int, error) {
	// The current repository only supports CleanupHealthRecords with a list of existing files
	// For now, we'll return 0 and suggest implementing selective cleanup in the repository

	// This should be implemented in the health repository with proper filtering
	return 0, fmt.Errorf("selective health record cleanup not yet implemented")
}

// handleAddHealthCheck handles POST /api/health/check
func (s *Server) handleAddHealthCheck(w http.ResponseWriter, r *http.Request) {
	// Parse request body
	var req HealthCheckRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteBadRequest(w, "Invalid request body", err.Error())
		return
	}

	// Validate required fields
	if req.FilePath == "" {
		WriteValidationError(w, "file_path is required", "")
		return
	}

	// Set default max retries if not specified
	maxRetries := 2 // Default from config
	if req.MaxRetries != nil {
		if *req.MaxRetries < 0 {
			WriteValidationError(w, "max_retries must be non-negative", "")
			return
		}
		maxRetries = *req.MaxRetries
	}

	// Add file to health database
	err := s.healthRepo.AddFileToHealthCheck(req.FilePath, maxRetries, req.SourceNzb)
	if err != nil {
		WriteInternalError(w, "Failed to add file for health check", err.Error())
		return
	}

	// Return the health record
	item, err := s.healthRepo.GetFileHealth(req.FilePath)
	if err != nil {
		WriteInternalError(w, "Failed to retrieve added health record", err.Error())
		return
	}

	response := ToHealthItemResponse(item)
	WriteSuccess(w, response, nil)
}

// handleGetHealthWorkerStatus handles GET /api/health/worker/status
func (s *Server) handleGetHealthWorkerStatus(w http.ResponseWriter, r *http.Request) {
	if s.healthWorker == nil {
		WriteNotFound(w, "Health worker not available", "Health worker is not configured or not running")
		return
	}

	stats := s.healthWorker.GetStats()
	response := HealthWorkerStatusResponse{
		Status:                 string(stats.Status),
		LastRunTime:            stats.LastRunTime,
		NextRunTime:            stats.NextRunTime,
		TotalRunsCompleted:     stats.TotalRunsCompleted,
		TotalFilesChecked:      stats.TotalFilesChecked,
		TotalFilesRecovered:    stats.TotalFilesRecovered,
		TotalFilesCorrupted:    stats.TotalFilesCorrupted,
		CurrentRunStartTime:    stats.CurrentRunStartTime,
		CurrentRunFilesChecked: stats.CurrentRunFilesChecked,
		PendingManualChecks:    stats.PendingManualChecks,
		LastError:              stats.LastError,
		ErrorCount:             stats.ErrorCount,
	}

	WriteSuccess(w, response, nil)
}

// handleDirectHealthCheck handles POST /api/health/{id}/check-now
func (s *Server) handleDirectHealthCheck(w http.ResponseWriter, r *http.Request) {
	// Extract ID from path parameter
	filePath := r.PathValue("id")
	if filePath == "" {
		WriteBadRequest(w, "Health record identifier is required", "")
		return
	}

	// Check if health worker is available
	if s.healthWorker == nil {
		WriteNotFound(w, "Health worker not available", "Health worker is not configured or not running")
		return
	}

	// Check if item exists in health database
	item, err := s.healthRepo.GetFileHealth(filePath)
	if err != nil {
		WriteInternalError(w, "Failed to check health record", err.Error())
		return
	}

	if item == nil {
		WriteNotFound(w, "Health record not found", "")
		return
	}

	// Prevent starting multiple checks for the same file
	if item.Status == database.HealthStatusChecking {
		WriteConflict(w, "Health check already in progress", "This file is currently being checked")
		return
	}

	// Immediately set status to 'checking'
	err = s.healthRepo.SetFileChecking(filePath)
	if err != nil {
		WriteInternalError(w, "Failed to set checking status", err.Error())
		return
	}

	// Start health check in background using worker
	err = s.healthWorker.PerformBackgroundCheck(context.Background(), filePath)
	if err != nil {
		WriteInternalError(w, "Failed to start background health check", err.Error())
		return
	}

	// Verify that the file still exists
	f, err := s.metadataReader.GetFileMetadata(filePath)
	if f == nil || err != nil {
		WriteInternalError(w, "Failed to retrieve file metadata", err.Error())
		return
	}

	// Get the updated health record with 'checking' status
	updatedItem, err := s.healthRepo.GetFileHealth(filePath)
	if err != nil {
		WriteInternalError(w, "Failed to retrieve updated health record", err.Error())
		return
	}

	response := map[string]interface{}{
		"message":     "Health check started",
		"file_path":   filePath,
		"old_status":  string(item.Status),
		"new_status":  string(updatedItem.Status),
		"checked_at":  updatedItem.LastChecked,
		"health_data": ToHealthItemResponse(updatedItem),
	}

	WriteSuccess(w, response, nil)
}

// UploadAndCheckRequest represents request to check health of a file by metadata path
type UploadAndCheckRequest struct {
	FilePath         string  `json:"file_path"`
	CheckAllSegments bool    `json:"check_all_segments,omitempty"`
	MaxRetries       *int    `json:"max_retries,omitempty"`
	SourceNzb        *string `json:"source_nzb_path,omitempty"`
}

// UploadAndCheckResponse represents response from immediate health check
type UploadAndCheckResponse struct {
	FilePath     string                `json:"file_path"`
	HealthStatus database.HealthStatus `json:"health_status"`
	CheckResult  string                `json:"check_result"`
	ErrorMessage *string               `json:"error_message,omitempty"`
	CheckedAt    time.Time             `json:"checked_at"`
	SegmentsInfo *SegmentsInfo         `json:"segments_info,omitempty"`
}

// SegmentsInfo provides details about segment checking results
type SegmentsInfo struct {
	TotalSegments   int  `json:"total_segments"`
	MissingSegments int  `json:"missing_segments"`
	CheckedAll      bool `json:"checked_all"`
}

// handleCancelHealthCheck handles POST /api/health/{id}/cancel
func (s *Server) handleCancelHealthCheck(w http.ResponseWriter, r *http.Request) {
	// Extract ID from path parameter
	filePath := r.PathValue("id")
	if filePath == "" {
		WriteBadRequest(w, "Health record identifier is required", "")
		return
	}

	// Check if health worker is available
	if s.healthWorker == nil {
		WriteNotFound(w, "Health worker not available", "Health worker is not configured or not running")
		return
	}

	// Check if item exists in health database
	item, err := s.healthRepo.GetFileHealth(filePath)
	if err != nil {
		WriteInternalError(w, "Failed to check health record", err.Error())
		return
	}

	if item == nil {
		WriteNotFound(w, "Health record not found", "")
		return
	}

	// Check if there's actually an active check to cancel
	if !s.healthWorker.IsCheckActive(filePath) {
		WriteConflict(w, "No active health check found", "There is no active health check for this file")
		return
	}

	// Cancel the health check
	err = s.healthWorker.CancelHealthCheck(filePath)
	if err != nil {
		WriteInternalError(w, "Failed to cancel health check", err.Error())
		return
	}

	// Get the updated health record
	updatedItem, err := s.healthRepo.GetFileHealth(filePath)
	if err != nil {
		WriteInternalError(w, "Failed to retrieve updated health record", err.Error())
		return
	}

	response := map[string]interface{}{
		"message":      "Health check cancelled",
		"file_path":    filePath,
		"old_status":   string(item.Status),
		"new_status":   string(updatedItem.Status),
		"cancelled_at": time.Now().Format(time.RFC3339),
		"health_data":  ToHealthItemResponse(updatedItem),
	}

	WriteSuccess(w, response, nil)
}
