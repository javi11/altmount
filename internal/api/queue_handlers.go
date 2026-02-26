package api

import (
	"fmt"
	"html"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/altmount/internal/nzblnk"
)

// transformQueueError transforms specific errors to user-friendly messages
func transformQueueError(err *string) string {
	if err == nil {
		return ""
	}

	// Check if it's the article not found error
	if strings.Contains(*err, "article is not found") {
		return "The file is incomplete or missing parts. Some segments of this file could not be found on any of the configured Usenet providers. This often happens with older or less popular files."
	}

	// Return the original error message for other errors
	return *err
}

// handleListQueue handles GET /api/queue
func (s *Server) handleListQueue(c *fiber.Ctx) error {
	// Parse pagination
	pagination := ParsePaginationFiber(c)

	// Parse status filter
	var statusFilter *database.QueueStatus
	if statusStr := c.Query("status"); statusStr != "" {
		status := database.QueueStatus(statusStr)
		// Validate status
		switch status {
		case database.QueueStatusPending, database.QueueStatusProcessing,
			database.QueueStatusCompleted, database.QueueStatusFailed:
			statusFilter = &status
		default:
			return RespondValidationError(c, "Invalid status filter", "Valid values: pending, processing, completed, failed")
		}
	}

	// Parse search parameter
	searchFilter := c.Query("search")

	// Parse sort parameters
	sortBy := c.Query("sort_by", "updated_at")
	sortOrder := c.Query("sort_order", "desc")

	// Validate sort_by
	validSortFields := map[string]bool{
		"created_at": true,
		"updated_at": true,
		"status":     true,
		"nzb_path":   true,
	}
	if !validSortFields[sortBy] {
		sortBy = "updated_at"
	}

	// Validate sort_order
	if sortOrder != "asc" && sortOrder != "desc" {
		sortOrder = "desc"
	}

	// Parse since filter
	var sinceFilter *time.Time
	if since, err := ParseTimeParamFiber(c, "since"); err != nil {
		return RespondValidationError(c, "Invalid since parameter", err.Error())
	} else if since != nil {
		sinceFilter = since
	}

	// Get total count for pagination
	totalCount, err := s.queueRepo.CountQueueItems(c.Context(), statusFilter, searchFilter, "")
	if err != nil {
		return RespondInternalError(c, "Failed to count queue items", err.Error())
	}

	// Get queue items from repository
	items, err := s.queueRepo.ListQueueItems(c.Context(), statusFilter, searchFilter, "", pagination.Limit, pagination.Offset, sortBy, sortOrder)
	if err != nil {
		return RespondInternalError(c, "Failed to retrieve queue items", err.Error())
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

	return RespondSuccessWithMeta(c, response, meta)
}

// handleGetQueue handles GET /api/queue/{id}
func (s *Server) handleGetQueue(c *fiber.Ctx) error {
	// Extract ID from path parameter
	idStr := c.Params("id")
	if idStr == "" {
		return RespondBadRequest(c, "Queue item ID is required", "")
	}

	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return RespondBadRequest(c, "Invalid queue item ID", "ID must be a valid integer")
	}

	// Get queue item from repository
	item, err := s.queueRepo.GetQueueItem(c.Context(), id)
	if err != nil {
		return RespondInternalError(c, "Failed to retrieve queue item", err.Error())
	}

	if item == nil {
		return RespondNotFound(c, "Queue item", "")
	}

	// Convert to API response format
	response := ToQueueItemResponse(item)
	return RespondSuccess(c, response)
}

// handleDeleteQueue handles DELETE /api/queue/{id}
func (s *Server) handleDeleteQueue(c *fiber.Ctx) error {
	// Extract ID from path parameter
	idStr := c.Params("id")
	if idStr == "" {
		return RespondBadRequest(c, "Queue item ID is required", "")
	}

	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return RespondBadRequest(c, "Invalid queue item ID", "ID must be a valid integer")
	}

	// Check if item exists first
	item, err := s.queueRepo.GetQueueItem(c.Context(), id)
	if err != nil {
		return RespondInternalError(c, "Failed to check queue item", err.Error())
	}

	if item == nil {
		return RespondNotFound(c, "Queue item", "")
	}

	// Prevent deletion of items currently being processed
	if item.Status == database.QueueStatusProcessing {
		return RespondConflict(c, "Cannot delete item currently being processed", "Wait for processing to complete or fail")
	}

	// Remove from queue
	err = s.queueRepo.RemoveFromQueue(c.Context(), id)
	if err != nil {
		return RespondInternalError(c, "Failed to delete queue item", err.Error())
	}

	return RespondNoContent(c)
}

// handleRetryQueue handles POST /api/queue/{id}/retry
func (s *Server) handleRetryQueue(c *fiber.Ctx) error {
	// Extract ID from path parameter
	idStr := c.Params("id")
	if idStr == "" {
		return RespondBadRequest(c, "Queue item ID is required", "")
	}

	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return RespondBadRequest(c, "Invalid queue item ID", "ID must be a valid integer")
	}

	// Check if item exists
	item, err := s.queueRepo.GetQueueItem(c.Context(), id)
	if err != nil {
		return RespondInternalError(c, "Failed to check queue item", err.Error())
	}

	if item == nil {
		return RespondNotFound(c, "Queue item", "")
	}

	// Only allow retry of pending, failed or completed items
	if item.Status != database.QueueStatusPending && item.Status != database.QueueStatusFailed && item.Status != database.QueueStatusCompleted {
		return RespondConflict(c, "Can only retry pending, failed or completed items", "Current status: "+string(item.Status))
	}

	// Update status to pending for manual retry
	err = s.queueRepo.UpdateQueueItemStatus(c.Context(), id, database.QueueStatusPending, nil)
	if err != nil {
		return RespondInternalError(c, "Failed to retry queue item", err.Error())
	}

	// Trigger background processing immediately
	if s.importerService != nil {
		s.importerService.ProcessItemInBackground(c.Context(), id)
	}

	// Get updated item
	updatedItem, err := s.queueRepo.GetQueueItem(c.Context(), id)
	if err != nil {
		return RespondInternalError(c, "Failed to retrieve updated queue item", err.Error())
	}

	response := ToQueueItemResponse(updatedItem)
	return RespondSuccess(c, response)
}

// handleCancelQueue handles POST /api/queue/{id}/cancel
func (s *Server) handleCancelQueue(c *fiber.Ctx) error {
	// Extract ID from path parameter
	idStr := c.Params("id")
	if idStr == "" {
		return RespondBadRequest(c, "Queue item ID is required", "")
	}

	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return RespondBadRequest(c, "Invalid queue item ID", "ID must be a valid integer")
	}

	// Check if item exists
	item, err := s.queueRepo.GetQueueItem(c.Context(), id)
	if err != nil {
		return RespondInternalError(c, "Failed to check queue item", err.Error())
	}

	if item == nil {
		return RespondNotFound(c, "Queue item", "")
	}

	// Only allow cancellation of processing items
	if item.Status != database.QueueStatusProcessing {
		return RespondConflict(c, "Can only cancel items that are currently processing", "Current status: "+string(item.Status))
	}

	// Request cancellation
	if s.importerService == nil {
		return RespondInternalError(c, "Importer service not available", "")
	}

	err = s.importerService.CancelProcessing(id)
	if err != nil {
		return RespondNotFound(c, "Item is not currently processing", err.Error())
	}

	return c.Status(fiber.StatusAccepted).JSON(fiber.Map{
		"success": true,
		"data": fiber.Map{
			"message": "Cancellation requested",
			"id":      id,
		},
	})
}

// handleGetQueueStats handles GET /api/queue/stats
func (s *Server) handleGetQueueStats(c *fiber.Ctx) error {
	stats, err := s.queueRepo.GetQueueStats(c.Context())
	if err != nil {
		return RespondInternalError(c, "Failed to retrieve queue statistics", err.Error())
	}

	response := ToQueueStatsResponse(stats)
	return RespondSuccess(c, response)
}

// handleGetQueueHistoricalStats handles GET /api/queue/stats/history
func (s *Server) handleGetQueueHistoricalStats(c *fiber.Ctx) error {
	// Get optional days parameter, default to 1 (24h)
	days := 1
	if daysStr := c.Query("days"); daysStr != "" {
		if d, err := strconv.Atoi(daysStr); err == nil && d > 0 {
			days = d
		}
	}

	// Limit to max 365 days for performance
	if days > 365 {
		days = 365
	}

	dailyStats, err := s.queueRepo.GetImportDailyStats(c.Context(), days)
	if err != nil {
		return RespondInternalError(c, "Failed to retrieve queue historical statistics", err.Error())
	}

	// For 24h view, we want more granular hourly stats for strict rolling window
	var hourlyStats []*database.ImportHourlyStat
	if days == 1 {
		hourlyStats, _ = s.queueRepo.GetImportHourlyStats(c.Context(), 24)
	}

	response := ToQueueHistoricalStatsResponse(dailyStats, hourlyStats)
	return RespondSuccess(c, response)
}

// handleClearCompletedQueue handles DELETE /api/queue/completed
func (s *Server) handleClearCompletedQueue(c *fiber.Ctx) error {
	// Clear completed items
	count, err := s.queueRepo.ClearCompletedQueueItems(c.Context())
	if err != nil {
		return RespondInternalError(c, "Failed to clear completed queue items", err.Error())
	}

	return RespondSuccess(c, fiber.Map{"removed_count": count})
}

// handleClearFailedQueue handles DELETE /api/queue/failed
func (s *Server) handleClearFailedQueue(c *fiber.Ctx) error {
	// Clear failed items
	count, err := s.queueRepo.ClearFailedQueueItems(c.Context())
	if err != nil {
		return RespondInternalError(c, "Failed to clear failed queue items", err.Error())
	}

	return RespondSuccess(c, fiber.Map{"removed_count": count})
}

// handleClearPendingQueue handles DELETE /api/queue/pending
func (s *Server) handleClearPendingQueue(c *fiber.Ctx) error {
	// Clear pending items
	count, err := s.queueRepo.ClearPendingQueueItems(c.Context())
	if err != nil {
		return RespondInternalError(c, "Failed to clear pending queue items", err.Error())
	}

	return RespondSuccess(c, fiber.Map{"removed_count": count})
}

// handleDeleteQueueBulk handles DELETE /api/queue/bulk
func (s *Server) handleDeleteQueueBulk(c *fiber.Ctx) error {
	// Parse request body
	var request struct {
		IDs []int64 `json:"ids"`
	}

	if err := c.BodyParser(&request); err != nil {
		return RespondBadRequest(c, "Invalid request body", err.Error())
	}

	// Validate IDs
	if len(request.IDs) == 0 {
		return RespondBadRequest(c, "No IDs provided", "At least one ID is required")
	}

	// Remove from queue in bulk (this will check for processing items)
	result, err := s.queueRepo.RemoveFromQueueBulk(c.Context(), request.IDs)
	if err != nil {
		// Check if the error is about processing items
		if result != nil && result.ProcessingCount > 0 {
			return RespondConflict(c, "Cannot delete items currently being processed", fmt.Sprintf("%d items are currently being processed", result.ProcessingCount))
		}

		return RespondInternalError(c, "Failed to delete queue items", err.Error())
	}

	return RespondSuccess(c, fiber.Map{
		"deleted_count": result.DeletedCount,
		"message":       fmt.Sprintf("Successfully deleted %d queue items", result.DeletedCount),
	})
}

// handleUploadToQueue handles POST /api/queue/upload
func (s *Server) handleUploadToQueue(c *fiber.Ctx) error {
	// Get uploaded file
	file, err := c.FormFile("file")
	if err != nil {
		return RespondBadRequest(c, "No file provided", "A file must be uploaded")
	}

	// Validate file extension
	if !strings.HasSuffix(strings.ToLower(file.Filename), ".nzb") {
		return RespondValidationError(c, "Invalid file type", "Only .nzb files are allowed")
	}

	// Validate file size (100MB limit)
	if file.Size > 100*1024*1024 {
		return RespondValidationError(c, "File too large", "File size must be less than 100MB")
	}

	// Get optional category from form
	category := c.FormValue("category")

	// Get optional relative_path from form
	relativePath := c.FormValue("relative_path")

	// Get optional priority from form
	priorityStr := c.FormValue("priority")
	var priority database.QueuePriority
	if priorityStr != "" {
		if p, err := strconv.Atoi(priorityStr); err == nil {
			priority = database.QueuePriority(p)
		} else {
			priority = database.QueuePriorityNormal
		}
	} else {
		priority = database.QueuePriorityNormal
	}

	// Create temporary directory for upload
	tempDir := os.TempDir()
	uploadDir := filepath.Join(tempDir, "altmount-uploads")
	if err := os.MkdirAll(uploadDir, 0755); err != nil {
		return RespondInternalError(c, "Failed to create upload directory", err.Error())
	}

	// Save the uploaded file to temporary location
	// Use filepath.Base to strip any path components from the filename
	safeFilename := filepath.Base(file.Filename)
	tempFile := filepath.Join(uploadDir, safeFilename)
	if err := c.SaveFile(file, tempFile); err != nil {
		return RespondInternalError(c, "Failed to save file", err.Error())
	}

	// Add to queue using importer service
	if s.importerService == nil {
		// Clean up temp file
		os.Remove(tempFile)
		return RespondServiceUnavailable(c, "Importer service not available", "The import service is not configured or running")
	}

	// Add the file to the processing queue
	var categoryPtr *string
	if category != "" {
		categoryPtr = &category
	}

	// Build base path from CompleteDir for manually uploaded files
	// The category will be appended to this by the processor
	var basePath *string
	if s.configManager != nil {
		completeDir := s.configManager.GetConfig().SABnzbd.CompleteDir
		if completeDir != "" {
			p := completeDir
			if relativePath != "" {
				p = filepath.Join(p, relativePath)
			}
			basePath = &p
		}
	}

	// For manually uploaded files, pass CompleteDir as the base path (not the temp upload directory)
	// The category will be appended to this by processNzbItem in the service
	item, err := s.importerService.AddToQueue(c.Context(), tempFile, basePath, categoryPtr, &priority)
	if err != nil {
		// Clean up temp file on error
		os.Remove(tempFile)
		return RespondInternalError(c, "Failed to add file to queue", err.Error())
	}

	// Convert to API response format
	response := ToQueueItemResponse(item)
	return RespondCreated(c, response)
}

// handleUploadNZBLnk handles POST /api/queue/upload-nzblnk
func (s *Server) handleUploadNZBLnk(c *fiber.Ctx) error {
	// Parse request body
	var req struct {
		Links        []string `json:"links"`
		Category     string   `json:"category"`
		Priority     int      `json:"priority"`
		RelativePath string   `json:"relative_path"`
	}

	if err := c.BodyParser(&req); err != nil {
		return RespondBadRequest(c, "Invalid request body", err.Error())
	}

	// Validate links
	if len(req.Links) == 0 {
		return RespondValidationError(c, "No links provided", "At least one NZBLNK is required")
	}

	if len(req.Links) > 20 {
		return RespondValidationError(c, "Too many links", "Maximum 20 links per request")
	}

	// Check if importer service is available
	if s.importerService == nil {
		return RespondServiceUnavailable(c, "Importer service not available", "The import service is not configured or running")
	}

	// Create resolver
	resolver := nzblnk.NewResolver()

	// Process each link
	type linkResult struct {
		Link         string `json:"link"`
		Success      bool   `json:"success"`
		QueueID      *int64 `json:"queue_id,omitempty"`
		Title        string `json:"title,omitempty"`
		ErrorMessage string `json:"error_message,omitempty"`
	}

	results := make([]linkResult, 0, len(req.Links))
	successCount := 0

	for _, link := range req.Links {
		result := linkResult{Link: link}

		// Parse the link first to get title for error messages
		params, err := nzblnk.ParseNZBLink(link)
		if err != nil {
			result.ErrorMessage = err.Error()
			results = append(results, result)
			continue
		}
		result.Title = params.Title

		// Resolve the link
		resolved, err := resolver.Resolve(c.Context(), link)
		if err != nil {
			result.ErrorMessage = err.Error()
			results = append(results, result)
			continue
		}

		// Create temp file for the NZB
		tempDir := os.TempDir()
		uploadDir := filepath.Join(tempDir, "altmount-uploads")
		if err := os.MkdirAll(uploadDir, 0755); err != nil {
			result.ErrorMessage = "Failed to create upload directory"
			results = append(results, result)
			continue
		}

		// Sanitize filename from title
		safeTitle := sanitizeFilename(resolved.Title)
		tempFile := filepath.Join(uploadDir, safeTitle+".nzb")

		// Embed password in NZB if provided
		nzbContent := resolved.NZBContent
		if resolved.Password != "" {
			nzbContent = embedPasswordInNZB(nzbContent, resolved.Password)
			slog.DebugContext(c.Context(), "Embedded password in NZB",
				"title", resolved.Title)
		}

		// Write NZB content to file
		if err := os.WriteFile(tempFile, nzbContent, 0644); err != nil {
			result.ErrorMessage = "Failed to save NZB file"
			results = append(results, result)
			continue
		}

		// Add to queue
		var categoryPtr *string
		if req.Category != "" {
			categoryPtr = &req.Category
		}

		var basePath *string
		if s.configManager != nil {
			completeDir := s.configManager.GetConfig().SABnzbd.CompleteDir
			if completeDir != "" {
				p := completeDir
				if req.RelativePath != "" {
					p = filepath.Join(p, req.RelativePath)
				}
				basePath = &p
			}
		}

		priority := database.QueuePriority(req.Priority)
		item, err := s.importerService.AddToQueue(c.Context(), tempFile, basePath, categoryPtr, &priority)
		if err != nil {
			os.Remove(tempFile)
			result.ErrorMessage = "Failed to add to queue: " + err.Error()
			results = append(results, result)
			continue
		}

		result.Success = true
		result.QueueID = &item.ID
		successCount++
		results = append(results, result)
	}

	return RespondCreated(c, fiber.Map{
		"results":       results,
		"success_count": successCount,
		"failed_count":  len(req.Links) - successCount,
	})
}

// sanitizeFilename removes or replaces characters that are invalid in filenames
func sanitizeFilename(name string) string {
	// Replace invalid characters with underscore
	invalid := []string{"/", "\\", ":", "*", "?", "\"", "<", ">", "|"}
	result := name
	for _, char := range invalid {
		result = strings.ReplaceAll(result, char, "_")
	}
	// Trim spaces and dots from ends
	result = strings.TrimSpace(result)
	result = strings.Trim(result, ".")
	// Limit length
	if len(result) > 200 {
		result = result[:200]
	}
	if result == "" {
		result = "download"
	}
	return result
}

// embedPasswordInNZB injects a password into the NZB XML metadata
func embedPasswordInNZB(nzbContent []byte, password string) []byte {
	if password == "" {
		return nzbContent
	}

	content := string(nzbContent)
	passwordMeta := fmt.Sprintf(`<meta type="password">%s</meta>`, html.EscapeString(password))

	// If <head> exists, insert password meta inside it
	if strings.Contains(content, "<head>") {
		content = strings.Replace(content, "<head>", "<head>\n    "+passwordMeta, 1)
	} else {
		// If no <head>, add one after <nzb...> tag
		re := regexp.MustCompile(`(<nzb[^>]*>)`)
		content = re.ReplaceAllString(content, "$1\n  <head>\n    "+passwordMeta+"\n  </head>")
	}

	return []byte(content)
}

// handleRestartQueueBulk handles POST /api/queue/bulk/restart
func (s *Server) handleRestartQueueBulk(c *fiber.Ctx) error {
	// Parse request body
	var request struct {
		IDs []int64 `json:"ids"`
	}

	if err := c.BodyParser(&request); err != nil {
		return RespondBadRequest(c, "Invalid request body", err.Error())
	}

	// Validate IDs
	if len(request.IDs) == 0 {
		return RespondBadRequest(c, "No IDs provided", "At least one ID is required")
	}

	// Check if any items are currently being processed
	processedCount := 0
	notFoundCount := 0
	for _, id := range request.IDs {
		item, err := s.queueRepo.GetQueueItem(c.Context(), id)
		if err != nil {
			return RespondInternalError(c, "Failed to check queue item", err.Error())
		}

		if item == nil {
			notFoundCount++
			continue
		}

		if item.Status == database.QueueStatusProcessing {
			processedCount++
		}
	}

	if notFoundCount == len(request.IDs) {
		return RespondNotFound(c, "Queue items", "None of the provided IDs exist")
	}

	if processedCount > 0 {
		return RespondConflict(c, "Cannot restart items currently being processed", fmt.Sprintf("%d items are currently being processed", processedCount))
	}

	// Restart the queue items
	err := s.queueRepo.RestartQueueItemsBulk(c.Context(), request.IDs)
	if err != nil {
		return RespondInternalError(c, "Failed to restart queue items", err.Error())
	}

	return RespondSuccess(c, fiber.Map{
		"restarted_count": len(request.IDs) - notFoundCount,
		"message":         fmt.Sprintf("Successfully restarted %d queue items", len(request.IDs)-notFoundCount),
	})
}

// handleCancelQueueBulk handles POST /api/queue/bulk/cancel
func (s *Server) handleCancelQueueBulk(c *fiber.Ctx) error {
	// Parse request body
	var request struct {
		IDs []int64 `json:"ids"`
	}

	if err := c.BodyParser(&request); err != nil {
		return RespondBadRequest(c, "Invalid request body", err.Error())
	}

	// Validate IDs
	if len(request.IDs) == 0 {
		return RespondBadRequest(c, "No IDs provided", "At least one ID is required")
	}

	// Check importer service availability
	if s.importerService == nil {
		return RespondInternalError(c, "Importer service not available", "")
	}

	// Cancel each item and track results
	results := make(map[string]any)
	cancelledCount := 0
	notProcessingCount := 0
	notFoundCount := 0

	for _, id := range request.IDs {
		item, err := s.queueRepo.GetQueueItem(c.Context(), id)
		if err != nil {
			results[fmt.Sprintf("%d", id)] = "Error checking status"
			continue
		}

		if item == nil {
			notFoundCount++
			results[fmt.Sprintf("%d", id)] = "Not found"
			continue
		}

		if item.Status != database.QueueStatusProcessing {
			notProcessingCount++
			results[fmt.Sprintf("%d", id)] = fmt.Sprintf("Cannot cancel (status: %s)", item.Status)
			continue
		}

		err = s.importerService.CancelProcessing(id)
		if err != nil {
			results[fmt.Sprintf("%d", id)] = err.Error()
		} else {
			cancelledCount++
			results[fmt.Sprintf("%d", id)] = "Cancellation requested"
		}
	}

	return c.Status(fiber.StatusAccepted).JSON(fiber.Map{
		"success": true,
		"data": fiber.Map{
			"cancelled_count":      cancelledCount,
			"not_processing_count": notProcessingCount,
			"not_found_count":      notFoundCount,
			"results":              results,
			"message":              fmt.Sprintf("Cancellation requested for %d items", cancelledCount),
		},
	})
}

// handleAddTestQueueItem handles POST /api/queue/test
func (s *Server) handleAddTestQueueItem(c *fiber.Ctx) error {
	// Parse request body
	var req struct {
		Size string `json:"size"` // "100MB", "1GB", "10GB"
	}

	if err := c.BodyParser(&req); err != nil {
		return RespondBadRequest(c, "Invalid request body", err.Error())
	}

	// Validate size
	validSizes := map[string]bool{
		"100MB": true,
		"1GB":   true,
		"10GB":  true,
	}
	if !validSizes[req.Size] {
		return RespondValidationError(c, "Invalid size", "Allowed values: 100MB, 1GB, 10GB")
	}

	// Determine NZB URL
	nzbURL := fmt.Sprintf("https://sabnzbd.org/tests/test_download_%s.nzb", req.Size)

	// Create temporary file
	tempFile, err := os.CreateTemp("", fmt.Sprintf("test_download_%s_*.nzb", req.Size))
	if err != nil {
		return RespondInternalError(c, "Failed to create temp file", err.Error())
	}
	defer tempFile.Close()
	tempPath := tempFile.Name()

	// Download NZB
	resp, err := http.Get(nzbURL)
	if err != nil {
		os.Remove(tempPath)
		return RespondError(c, fiber.StatusBadGateway, "BAD_GATEWAY", "Failed to download test NZB from sabnzbd.org", err.Error())
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		os.Remove(tempPath)
		return RespondError(c, fiber.StatusBadGateway, "BAD_GATEWAY", "Failed to download test NZB", fmt.Sprintf("Remote server returned status: %s", resp.Status))
	}

	// Write content to file
	if _, err := io.Copy(tempFile, resp.Body); err != nil {
		os.Remove(tempPath)
		return RespondInternalError(c, "Failed to save test NZB", err.Error())
	}

	// Check if importer service is available
	if s.importerService == nil {
		os.Remove(tempPath)
		return RespondServiceUnavailable(c, "Importer service not available", "")
	}

	// Add to queue
	category := "test"
	priority := database.QueuePriorityHigh // Prioritize test files

	// Build base path from CompleteDir for manually uploaded files
	var basePath *string
	if s.configManager != nil {
		completeDir := s.configManager.GetConfig().SABnzbd.CompleteDir
		if completeDir != "" {
			basePath = &completeDir
		}
	}

	item, err := s.importerService.AddToQueue(c.Context(), tempPath, basePath, &category, &priority)
	if err != nil {
		os.Remove(tempPath)
		return RespondInternalError(c, "Failed to add test file to queue", err.Error())
	}

	response := ToQueueItemResponse(item)
	return RespondCreated(c, response)
}

// handleUpdateQueueItemPriority handles PATCH /api/queue/{id}/priority
func (s *Server) handleUpdateQueueItemPriority(c *fiber.Ctx) error {
	idStr := c.Params("id")
	if idStr == "" {
		return RespondBadRequest(c, "Queue item ID is required", "")
	}

	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return RespondBadRequest(c, "Invalid queue item ID", "ID must be a valid integer")
	}

	var req struct {
		Priority int `json:"priority"`
	}
	if err := c.BodyParser(&req); err != nil {
		return RespondBadRequest(c, "Invalid request body", err.Error())
	}

	// Validate priority: 1=high, 2=normal, 3=low
	if req.Priority < 1 || req.Priority > 3 {
		return RespondValidationError(c, "Invalid priority value", "Valid values: 1 (high), 2 (normal), 3 (low)")
	}

	item, err := s.queueRepo.GetQueueItem(c.Context(), id)
	if err != nil {
		return RespondInternalError(c, "Failed to check queue item", err.Error())
	}
	if item == nil {
		return RespondNotFound(c, "Queue item", "")
	}
	if item.Status == database.QueueStatusProcessing {
		return RespondConflict(c, "Cannot change priority of item currently being processed", "")
	}

	if err := s.queueRepo.UpdateQueueItemPriority(c.Context(), id, database.QueuePriority(req.Priority)); err != nil {
		return RespondInternalError(c, "Failed to update priority", err.Error())
	}

	updated, err := s.queueRepo.GetQueueItem(c.Context(), id)
	if err != nil {
		return RespondInternalError(c, "Failed to retrieve updated queue item", err.Error())
	}

	return RespondSuccess(c, ToQueueItemResponse(updated))
}

// handleDownloadNZB handles GET /api/queue/{id}/download
func (s *Server) handleDownloadNZB(c *fiber.Ctx) error {
	// Extract ID from path parameter
	idStr := c.Params("id")
	if idStr == "" {
		return RespondBadRequest(c, "Queue item ID is required", "")
	}

	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return RespondBadRequest(c, "Invalid queue item ID", "ID must be a valid integer")
	}

	// Get queue item from repository
	item, err := s.queueRepo.GetQueueItem(c.Context(), id)
	if err != nil {
		return RespondInternalError(c, "Failed to retrieve queue item", err.Error())
	}

	if item == nil {
		return RespondNotFound(c, "Queue item", "")
	}

	// Check if NZB file exists
	if _, err := os.Stat(item.NzbPath); os.IsNotExist(err) {
		return RespondNotFound(c, "NZB file", "The NZB file no longer exists on disk")
	}

	// Set headers for file download
	filename := filepath.Base(item.NzbPath)
	c.Set("Content-Type", "application/x-nzb")
	c.Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))

	// Send the file
	return c.SendFile(item.NzbPath)
}
