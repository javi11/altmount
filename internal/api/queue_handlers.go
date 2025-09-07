package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/javi11/altmount/internal/database"
)

// handleListQueue handles GET /api/queue
func (s *Server) handleListQueue(w http.ResponseWriter, r *http.Request) {
	// Parse pagination
	pagination := ParsePagination(r)

	// Parse status filter
	var statusFilter *database.QueueStatus
	if statusStr := r.URL.Query().Get("status"); statusStr != "" {
		status := database.QueueStatus(statusStr)
		// Validate status
		switch status {
		case database.QueueStatusPending, database.QueueStatusProcessing,
			database.QueueStatusCompleted, database.QueueStatusFailed,
			database.QueueStatusRetrying:
			statusFilter = &status
		default:
			WriteValidationError(w, "Invalid status filter", "Valid values: pending, processing, completed, failed, retrying")
			return
		}
	}

	// Parse search parameter
	searchFilter := r.URL.Query().Get("search")

	// Parse since filter
	var sinceFilter *time.Time
	if since, err := ParseTimeParam(r, "since"); err != nil {
		WriteValidationError(w, "Invalid since parameter", err.Error())
		return
	} else if since != nil {
		sinceFilter = since
	}

	// Get total count for pagination
	totalCount, err := s.queueRepo.CountQueueItems(statusFilter, searchFilter)
	if err != nil {
		WriteInternalError(w, "Failed to count queue items", err.Error())
		return
	}

	// Get queue items from repository
	items, err := s.queueRepo.ListQueueItems(statusFilter, searchFilter, pagination.Limit, pagination.Offset)
	if err != nil {
		WriteInternalError(w, "Failed to retrieve queue items", err.Error())
		return
	}

	// Filter by since if provided
	if sinceFilter != nil {
		filtered := make([]*database.ImportQueueItem, 0)
		for _, item := range items {
			if item.CreatedAt.After(*sinceFilter) || item.UpdatedAt.After(*sinceFilter) {
				filtered = append(filtered, item)
			}
		}
		items = filtered
	}

	// Convert to API response format
	response := make([]*QueueItemResponse, len(items))
	for i, item := range items {
		response[i] = ToQueueItemResponse(item)
	}

	// Create metadata
	meta := &APIMeta{
		Total:  totalCount,
		Count:  len(response),
		Limit:  pagination.Limit,
		Offset: pagination.Offset,
	}

	WriteSuccess(w, response, meta)
}

// handleGetQueue handles GET /api/queue/{id}
func (s *Server) handleGetQueue(w http.ResponseWriter, r *http.Request) {
	// Extract ID from path parameter
	idStr := r.PathValue("id")
	if idStr == "" {
		WriteBadRequest(w, "Queue item ID is required", "")
		return
	}

	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		WriteBadRequest(w, "Invalid queue item ID", "ID must be a valid integer")
		return
	}

	// Get queue item from repository
	item, err := s.queueRepo.GetQueueItem(id)
	if err != nil {
		WriteInternalError(w, "Failed to retrieve queue item", err.Error())
		return
	}

	if item == nil {
		WriteNotFound(w, "Queue item not found", "")
		return
	}

	// Convert to API response format
	response := ToQueueItemResponse(item)
	WriteSuccess(w, response, nil)
}

// handleDeleteQueue handles DELETE /api/queue/{id}
func (s *Server) handleDeleteQueue(w http.ResponseWriter, r *http.Request) {
	// Extract ID from path parameter
	idStr := r.PathValue("id")
	if idStr == "" {
		WriteBadRequest(w, "Queue item ID is required", "")
		return
	}

	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		WriteBadRequest(w, "Invalid queue item ID", "ID must be a valid integer")
		return
	}

	// Check if item exists first
	item, err := s.queueRepo.GetQueueItem(id)
	if err != nil {
		WriteInternalError(w, "Failed to check queue item", err.Error())
		return
	}

	if item == nil {
		WriteNotFound(w, "Queue item not found", "")
		return
	}

	// Prevent deletion of items currently being processed
	if item.Status == database.QueueStatusProcessing {
		WriteConflict(w, "Cannot delete item currently being processed", "Wait for processing to complete or fail")
		return
	}

	// Remove from queue
	err = s.queueRepo.RemoveFromQueue(id)
	if err != nil {
		WriteInternalError(w, "Failed to delete queue item", err.Error())
		return
	}

	WriteNoContent(w)
}

// handleRetryQueue handles POST /api/queue/{id}/retry
func (s *Server) handleRetryQueue(w http.ResponseWriter, r *http.Request) {
	// Extract ID from path parameter
	idStr := r.PathValue("id")
	if idStr == "" {
		WriteBadRequest(w, "Queue item ID is required", "")
		return
	}

	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		WriteBadRequest(w, "Invalid queue item ID", "ID must be a valid integer")
		return
	}

	// Check if item exists
	item, err := s.queueRepo.GetQueueItem(id)
	if err != nil {
		WriteInternalError(w, "Failed to check queue item", err.Error())
		return
	}

	if item == nil {
		WriteNotFound(w, "Queue item not found", "")
		return
	}

	// Only allow retry of pending, failed or completed items
	if item.Status != database.QueueStatusPending && item.Status != database.QueueStatusFailed && item.Status != database.QueueStatusCompleted {
		WriteConflict(w, "Can only retry pending, failed or completed items", "Current status: "+string(item.Status))
		return
	}

	// Update status to retrying
	err = s.queueRepo.UpdateQueueItemStatus(id, database.QueueStatusRetrying, nil)
	if err != nil {
		WriteInternalError(w, "Failed to retry queue item", err.Error())
		return
	}

	// Trigger background processing immediately
	if s.importerService != nil {
		s.importerService.ProcessItemInBackground(id)
	}

	// Get updated item
	updatedItem, err := s.queueRepo.GetQueueItem(id)
	if err != nil {
		WriteInternalError(w, "Failed to retrieve updated queue item", err.Error())
		return
	}

	response := ToQueueItemResponse(updatedItem)
	WriteSuccess(w, response, nil)
}

// handleGetQueueStats handles GET /api/queue/stats
func (s *Server) handleGetQueueStats(w http.ResponseWriter, r *http.Request) {
	stats, err := s.queueRepo.GetQueueStats()
	if err != nil {
		WriteInternalError(w, "Failed to retrieve queue statistics", err.Error())
		return
	}

	response := ToQueueStatsResponse(stats)
	WriteSuccess(w, response, nil)
}

// handleClearCompletedQueue handles DELETE /api/queue/completed
func (s *Server) handleClearCompletedQueue(w http.ResponseWriter, r *http.Request) {
	// Parse older_than parameter
	olderThan, err := ParseTimeParam(r, "older_than")
	if err != nil {
		WriteValidationError(w, "Invalid older_than parameter", err.Error())
		return
	}

	// Default to 24 hours ago if not specified
	if olderThan == nil {
		defaultTime := time.Now().Add(-24 * time.Hour)
		olderThan = &defaultTime
	}

	// Clear completed items
	count, err := s.queueRepo.ClearCompletedQueueItems(*olderThan)
	if err != nil {
		WriteInternalError(w, "Failed to clear completed queue items", err.Error())
		return
	}

	response := map[string]interface{}{
		"removed_count": count,
		"older_than":    olderThan.Format(time.RFC3339),
	}

	WriteSuccess(w, response, nil)
}

// handleDeleteQueueBulk handles DELETE /api/queue/bulk
func (s *Server) handleDeleteQueueBulk(w http.ResponseWriter, r *http.Request) {
	// Parse request body
	var request struct {
		IDs []int64 `json:"ids"`
	}

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		WriteBadRequest(w, "Invalid request body", err.Error())
		return
	}

	// Validate IDs
	if len(request.IDs) == 0 {
		WriteBadRequest(w, "No IDs provided", "At least one ID is required")
		return
	}

	// Check if any items are currently being processed
	processedCount := 0
	for _, id := range request.IDs {
		item, err := s.queueRepo.GetQueueItem(id)
		if err != nil {
			WriteInternalError(w, "Failed to check queue item", err.Error())
			return
		}

		if item != nil && item.Status == database.QueueStatusProcessing {
			processedCount++
		}
	}

	if processedCount > 0 {
		WriteConflict(w, "Cannot delete items currently being processed", 
			fmt.Sprintf("%d items are currently being processed", processedCount))
		return
	}

	// Remove from queue in bulk
	err := s.queueRepo.RemoveFromQueueBulk(request.IDs)
	if err != nil {
		WriteInternalError(w, "Failed to delete queue items", err.Error())
		return
	}

	response := map[string]interface{}{
		"deleted_count": len(request.IDs),
		"message":       fmt.Sprintf("Successfully deleted %d queue items", len(request.IDs)),
	}

	WriteSuccess(w, response, nil)
}
