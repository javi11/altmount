package api

import (
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
	
	// Parse status filter
	var statusFilter *database.HealthStatus
	if statusStr := r.URL.Query().Get("status"); statusStr != "" {
		status := database.HealthStatus(statusStr)
		// Validate status
		switch status {
		case database.HealthStatusHealthy, database.HealthStatusPartial, database.HealthStatusCorrupted:
			statusFilter = &status
		default:
			WriteValidationError(w, "Invalid status filter", "Valid values: healthy, partial, corrupted")
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

	// Get health items - we'll need to implement a list method in health repository
	items, err := s.listHealthItems(statusFilter, pagination, sinceFilter)
	if err != nil {
		WriteInternalError(w, "Failed to retrieve health records", err.Error())
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
	}

	WriteSuccess(w, response, meta)
}

// listHealthItems is a helper method to list health items with filters
// This would ideally be implemented in the HealthRepository
func (s *Server) listHealthItems(statusFilter *database.HealthStatus, pagination Pagination, sinceFilter *time.Time) ([]*database.FileHealth, error) {
	// Since we don't have a ListHealthItems method in the repository yet,
	// we'll implement a basic version here and suggest adding it to the repository later
	
	// For corrupted files, we can use GetUnhealthyFiles
	if statusFilter != nil && *statusFilter == database.HealthStatusCorrupted {
		return s.healthRepo.GetUnhealthyFiles(pagination.Limit)
	}

	// For now, return empty slice for other cases - this should be implemented in the repository
	return []*database.FileHealth{}, nil
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
	idStr := r.PathValue("id")
	if idStr == "" {
		WriteBadRequest(w, "Health record identifier is required", "")
		return
	}

	// For now, we'll treat this as a file path since that's what our repository supports
	// First check if the record exists
	item, err := s.healthRepo.GetFileHealth(idStr)
	if err != nil {
		WriteInternalError(w, "Failed to check health record", err.Error())
		return
	}

	if item == nil {
		WriteNotFound(w, "Health record not found", "")
		return
	}

	// We need to implement a delete method in the health repository
	// For now, return not implemented
	WriteInternalError(w, "Delete health record not yet implemented", "This feature needs to be added to the health repository")
}

// handleRetryHealth handles POST /api/health/{id}/retry
func (s *Server) handleRetryHealth(w http.ResponseWriter, r *http.Request) {
	// Extract ID from path parameter
	filePath := r.PathValue("id")
	if filePath == "" {
		WriteBadRequest(w, "Health record identifier is required", "")
		return
	}

	// Parse request body
	var req HealthRetryRequest
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

	// Only allow retry of corrupted or partial files
	if item.Status != database.HealthStatusCorrupted && item.Status != database.HealthStatusPartial {
		WriteConflict(w, "Can only retry corrupted or partial files", "Current status: "+string(item.Status))
		return
	}

	// Reset the file to healthy status to allow retry
	err = s.healthRepo.UpdateFileHealth(filePath, database.HealthStatusHealthy, nil, item.SourceNzbPath, nil)
	if err != nil {
		WriteInternalError(w, "Failed to reset health record", err.Error())
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
			case database.HealthStatusHealthy, database.HealthStatusPartial, database.HealthStatusCorrupted:
				req.Status = &status
			default:
				WriteValidationError(w, "Invalid status filter", "Valid values: healthy, partial, corrupted")
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