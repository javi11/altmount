package api

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/altmount/internal/pathutil"
)

// handleListHealth handles GET /api/health
func (s *Server) handleListHealth(c *fiber.Ctx) error {
	// Parse pagination
	pagination := ParsePaginationFiber(c)

	// Parse search parameter
	search := c.Query("search")

	// Parse sort parameters
	sortBy := c.Query("sort_by", "created_at")
	sortOrder := c.Query("sort_order", "desc")

	// Validate sort parameters
	validSortFields := map[string]bool{
		"file_path":          true,
		"created_at":         true,
		"status":             true,
		"priority":           true,
		"last_checked":       true,
		"scheduled_check_at": true,
	}
	if !validSortFields[sortBy] {
		sortBy = "created_at"
	}

	if sortOrder != "asc" && sortOrder != "desc" {
		sortOrder = "desc"
	}

	// Parse status filter
	var statusFilter *database.HealthStatus
	if statusStr := c.Query("status"); statusStr != "" {
		statusStr = strings.TrimSpace(statusStr)
		status := database.HealthStatus(statusStr)
		// Validate status
		switch status {
		case database.HealthStatusPending, database.HealthStatusChecking, database.HealthStatusCorrupted, database.HealthStatusRepairTriggered, database.HealthStatusHealthy:
			statusFilter = &status
		default:
			return RespondValidationError(c, fmt.Sprintf("Invalid status filter: '%s'", statusStr), "Valid values: pending, checking, corrupted, repair_triggered, healthy")
		}
	}

	// Parse since filter
	var sinceFilter *time.Time
	if since, err := ParseTimeParamFiber(c, "since"); err != nil {
		return RespondValidationError(c, "Invalid since parameter", err.Error())
	} else if since != nil {
		sinceFilter = since
	}

	// Get health items with search and sort support
	items, err := s.listHealthItems(c.Context(), statusFilter, pagination, sinceFilter, search, sortBy, sortOrder)
	if err != nil {
		return RespondInternalError(c, "Failed to retrieve health records", err.Error())
	}

	// Get total count for pagination
	totalCount, err := s.countHealthItems(c.Context(), statusFilter, sinceFilter, search)
	if err != nil {
		return RespondInternalError(c, "Failed to count health records", err.Error())
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

	return RespondSuccessWithMeta(c, response, meta)
}

// listHealthItems is a helper method to list health items with filters
func (s *Server) listHealthItems(ctx context.Context, statusFilter *database.HealthStatus, pagination Pagination, sinceFilter *time.Time, search string, sortBy string, sortOrder string) ([]*database.FileHealth, error) {
	return s.healthRepo.ListHealthItems(ctx, statusFilter, pagination.Limit, pagination.Offset, sinceFilter, search, sortBy, sortOrder)
}

// countHealthItems is a helper method to count health items with filters
func (s *Server) countHealthItems(ctx context.Context, statusFilter *database.HealthStatus, sinceFilter *time.Time, search string) (int, error) {
	return s.healthRepo.CountHealthItems(ctx, statusFilter, sinceFilter, search)
}

// handleGetHealth handles GET /api/health/{id}
func (s *Server) handleGetHealth(c *fiber.Ctx) error {
	// Extract ID from path parameter
	idStr := c.Params("id")
	if idStr == "" {
		return RespondBadRequest(c, "Health record identifier is required", "")
	}

	// Parse as numeric ID
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return RespondBadRequest(c, "Invalid health record ID", "ID must be a valid integer")
	}

	// Get by ID
	item, err := s.healthRepo.GetFileHealthByID(c.Context(), id)
	if err != nil {
		return RespondInternalError(c, "Failed to retrieve health record", err.Error())
	}

	if item == nil {
		return RespondNotFound(c, "Health record", "")
	}

	response := ToHealthItemResponse(item)
	return RespondSuccess(c, response)
}

// handleDeleteHealth handles DELETE /api/health/{id}
func (s *Server) handleDeleteHealth(c *fiber.Ctx) error {
	// Extract ID from path parameter
	idStr := c.Params("id")
	if idStr == "" {
		return RespondBadRequest(c, "Health record identifier is required", "")
	}

	// Parse as numeric ID
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return RespondBadRequest(c, "Invalid health record ID", "ID must be a valid integer")
	}

	// Check if the record exists
	item, err := s.healthRepo.GetFileHealthByID(c.Context(), id)
	if err != nil {
		return RespondInternalError(c, "Failed to check health record", err.Error())
	}

	if item == nil {
		return RespondNotFound(c, "Health record", "")
	}

	// If the item is currently being checked, cancel the check first
	if item.Status == database.HealthStatusChecking {
		// Check if health worker is available
		if s.healthWorker != nil {
			// Check if there's actually an active check to cancel
			if s.healthWorker.IsCheckActive(item.FilePath) {
				// Cancel the health check before deletion
				err = s.healthWorker.CancelHealthCheck(c.Context(), item.FilePath)
				if err != nil {
					return RespondInternalError(c, "Failed to cancel health check before deletion", err.Error())
				}
			}
		}
	}

	// Delete the health record from database using ID
	err = s.healthRepo.DeleteHealthRecordByID(c.Context(), id)
	if err != nil {
		return RespondInternalError(c, "Failed to delete health record", err.Error())
	}

	return RespondSuccess(c, fiber.Map{
		"message":    "Health record deleted successfully",
		"id":         id,
		"file_path":  item.FilePath,
		"deleted_at": time.Now().Format(time.RFC3339),
	})
}

// handleDeleteHealthBulk handles POST /api/health/bulk/delete
func (s *Server) handleDeleteHealthBulk(c *fiber.Ctx) error {
	// Parse request body
	var req struct {
		FilePaths []string `json:"file_paths"`
	}

	if err := c.BodyParser(&req); err != nil {
		return RespondBadRequest(c, "Invalid request body", err.Error())
	}

	// Validate file paths
	if len(req.FilePaths) == 0 {
		return RespondValidationError(c, "At least one file path is required", "")
	}

	if len(req.FilePaths) > 100 {
		return RespondValidationError(c, "Too many file paths", "Maximum 100 files allowed per bulk operation")
	}

	// Check for any items currently being checked and cancel if needed
	if s.healthWorker != nil {
		for _, filePath := range req.FilePaths {
			// Get the record to check status
			item, err := s.healthRepo.GetFileHealth(c.Context(), filePath)
			if err != nil {
				continue // Skip if we can't get the record, will fail in bulk delete anyway
			}

			if item != nil && item.Status == database.HealthStatusChecking {
				// Check if there's actually an active check to cancel
				if s.healthWorker.IsCheckActive(filePath) {
					// Cancel the health check before deletion
					_ = s.healthWorker.CancelHealthCheck(c.Context(), filePath) // Ignore error, proceed with deletion
				}
			}
		}
	}

	// Delete health records in bulk
	err := s.healthRepo.DeleteHealthRecordsBulk(c.Context(), req.FilePaths)
	if err != nil {
		return RespondInternalError(c, "Failed to delete health records", err.Error())
	}

	return RespondSuccess(c, fiber.Map{
		"message":       "Health records deleted successfully",
		"deleted_count": len(req.FilePaths),
		"file_paths":    req.FilePaths,
		"deleted_at":    time.Now().Format(time.RFC3339),
	})
}

// handleRepairHealth handles POST /api/health/{id}/repair
func (s *Server) handleRepairHealth(c *fiber.Ctx) error {
	// Extract ID from path parameter
	idStr := c.Params("id")
	if idStr == "" {
		return RespondBadRequest(c, "Health record identifier is required", "")
	}

	// Parse as numeric ID
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return RespondBadRequest(c, "Invalid health record ID", "ID must be a valid integer")
	}

	// Parse request body
	var req HealthRepairRequest
	if len(c.Body()) > 0 {
		if err := c.BodyParser(&req); err != nil {
			return RespondBadRequest(c, "Invalid request body", err.Error())
		}
	}

	// Check if item exists
	item, err := s.healthRepo.GetFileHealthByID(c.Context(), id)
	if err != nil {
		return RespondInternalError(c, "Failed to check health record", err.Error())
	}

	if item == nil {
		return RespondNotFound(c, "Health record", "")
	}

	// Determine the path to use for ARR rescan
	// Step 1: Try to use library_path from database if available
	// Step 2: If not in DB, search for library item using FindLibraryItem
	// Step 3: Determine final path (library path or mount path fallback)
	// Step 4: Trigger rescan with resolved path
	ctx := c.Context()
	cfg := s.configManager.GetConfig()

	var libraryPath string
	if item.LibraryPath != nil && *item.LibraryPath != "" {
		libraryPath = *item.LibraryPath
	}

	// Determine final path for ARR rescan
	pathForRescan := libraryPath
	if pathForRescan == "" && cfg.Import.ImportStrategy == config.ImportStrategySYMLINK && cfg.Import.ImportDir != nil && *cfg.Import.ImportDir != "" {
		pathForRescan = pathutil.JoinAbsPath(*cfg.Import.ImportDir, item.FilePath)
		slog.InfoContext(ctx, "Using symlink import path for manual repair",
			"file_path", item.FilePath,
			"symlink_path", pathForRescan)
	}
	if pathForRescan == "" {
		// Fallback to mount path if no library path found
		pathForRescan = pathutil.JoinAbsPath(cfg.MountPath, item.FilePath)
		slog.InfoContext(ctx, "Using mount path fallback for manual repair",
			"file_path", item.FilePath,
			"mount_path", pathForRescan)
	}

	// Trigger rescan with the resolved path
	err = s.arrsService.TriggerFileRescan(ctx, pathForRescan, item.FilePath)
	if err != nil {
		// Check if this is a "no ARR instance found" error
		if strings.Contains(err.Error(), "no ARR instance found") {
			return RespondNotFound(c, "File not managed by any ARR instance", "This file is not found in any of the configured Radarr or Sonarr instances. Please ensure the file is in your media library and the ARR instances are properly configured.")
		}
		// Handle other errors as internal server errors
		return RespondInternalError(c, "Failed to trigger repair in ARR instance, you might need to trigger a manual library sync", err.Error())
	}

	// Update status to repair_triggered instead of deleting
	if err := s.healthRepo.SetRepairTriggered(ctx, item.FilePath, item.LastError, item.ErrorDetails); err != nil {
		slog.ErrorContext(ctx, "Failed to set repair_triggered status after repair trigger",
			"error", err,
			"file_path", item.FilePath)
		// Don't fail the repair trigger if update fails
	} else {
		slog.InfoContext(ctx, "Set status to repair_triggered after successful repair trigger",
			"file_path", item.FilePath)
	}

	// Get updated item
	updatedItem, err := s.healthRepo.GetFileHealthByID(c.Context(), id)
	if err != nil {
		return RespondInternalError(c, "Failed to retrieve updated health record", err.Error())
	}

	response := ToHealthItemResponse(updatedItem)
	return RespondSuccess(c, response)
}

// handleRepairHealthBulk handles POST /api/health/bulk/repair
func (s *Server) handleRepairHealthBulk(c *fiber.Ctx) error {
	// Parse request body
	var req struct {
		FilePaths []string `json:"file_paths"`
	}

	if err := c.BodyParser(&req); err != nil {
		return RespondBadRequest(c, "Invalid request body", err.Error())
	}

	// Validate file paths
	if len(req.FilePaths) == 0 {
		return RespondValidationError(c, "At least one file path is required", "")
	}

	if len(req.FilePaths) > 100 {
		return RespondValidationError(c, "Too many file paths", "Maximum 100 files allowed per bulk operation")
	}

	ctx := c.Context()
	cfg := s.configManager.GetConfig()
	successCount := 0
	failedCount := 0
	errors := make(map[string]string)

	for _, filePath := range req.FilePaths {
		// Check if item exists
		item, err := s.healthRepo.GetFileHealth(ctx, filePath)
		if err != nil {
			failedCount++
			errors[filePath] = fmt.Sprintf("Failed to check health record: %v", err)
			continue
		}

		if item == nil {
			failedCount++
			errors[filePath] = "Health record not found"
			continue
		}

		// Determine path for rescan
		var libraryPath string
		if item.LibraryPath != nil && *item.LibraryPath != "" {
			libraryPath = *item.LibraryPath
		}

		pathForRescan := libraryPath
		if pathForRescan == "" {
			pathForRescan = pathutil.JoinAbsPath(cfg.MountPath, item.FilePath)
		}

		// Trigger rescan
		err = s.arrsService.TriggerFileRescan(ctx, pathForRescan, item.FilePath)
		if err != nil {
			// If failed, track error but don't delete record yet?
			// Actually existing single repair endpoint deletes it even if it fails?
			// No, single endpoint returns 500/404 if TriggerFileRescan fails, and only deletes if successful (mostly).
			// Wait, lines 437 in single handler:
			// if err != nil { ... return ... }
			// if err := s.healthRepo.DeleteHealthRecord...
			// So it only deletes if TriggerFileRescan succeeds.

			failedCount++
			errors[filePath] = fmt.Sprintf("Failed to trigger repair: %v", err)
			continue
		}

		// Update status to repair_triggered instead of deleting
		if err := s.healthRepo.SetRepairTriggered(ctx, item.FilePath, item.LastError, item.ErrorDetails); err != nil {
			slog.ErrorContext(ctx, "Failed to set repair_triggered status after repair trigger",
				"error", err,
				"file_path", item.FilePath)
			// Don't count as failure since repair was triggered
		}

		successCount++
	}

	return RespondSuccess(c, fiber.Map{
		"message":       "Bulk repair operation completed",
		"success_count": successCount,
		"failed_count":  failedCount,
		"errors":        errors,
	})
}

// handleListCorrupted handles GET /api/health/corrupted
func (s *Server) handleListCorrupted(c *fiber.Ctx) error {
	// Parse pagination
	pagination := ParsePaginationFiber(c)

	// Get corrupted files using GetUnhealthyFiles
	items, err := s.healthRepo.GetUnhealthyFiles(c.Context(), pagination.Limit)
	if err != nil {
		return RespondInternalError(c, "Failed to retrieve corrupted files", err.Error())
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
		end := min(pagination.Offset+pagination.Limit, len(corruptedItems))
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

	return RespondSuccessWithMeta(c, response, meta)
}

// handleGetHealthStats handles GET /api/health/stats
func (s *Server) handleGetHealthStats(c *fiber.Ctx) error {
	stats, err := s.healthRepo.GetHealthStats(c.Context())
	if err != nil {
		return RespondInternalError(c, "Failed to retrieve health statistics", err.Error())
	}

	response := ToHealthStatsResponse(stats)
	return RespondSuccess(c, response)
}

// handleCleanupHealth handles DELETE /api/health/cleanup
func (s *Server) handleCleanupHealth(c *fiber.Ctx) error {
	// Parse request body
	var req HealthCleanupRequest
	if len(c.Body()) > 0 {
		if err := c.BodyParser(&req); err != nil {
			return RespondBadRequest(c, "Invalid request body", err.Error())
		}
	}

	// Parse older_than parameter from query if not in body
	if req.OlderThan == nil {
		if olderThan, err := ParseTimeParamFiber(c, "older_than"); err != nil {
			return RespondValidationError(c, "Invalid older_than parameter", err.Error())
		} else if olderThan != nil {
			req.OlderThan = olderThan
		}
	}

	// Parse status parameter from query if not in body
	if req.Status == nil {
		if statusStr := c.Query("status"); statusStr != "" {
			statusStr = strings.TrimSpace(statusStr)
			status := database.HealthStatus(statusStr)
			switch status {
			case database.HealthStatusPending, database.HealthStatusChecking, database.HealthStatusCorrupted, database.HealthStatusRepairTriggered, database.HealthStatusHealthy:
				req.Status = &status
			default:
				return RespondValidationError(c, fmt.Sprintf("Invalid status filter: '%s'", statusStr), "Valid values: pending, checking, corrupted, repair_triggered, healthy")
			}
		}
	}

	// Default to 7 days ago if not specified
	if req.OlderThan == nil {
		defaultTime := time.Now().Add(-7 * 24 * time.Hour)
		req.OlderThan = &defaultTime
	}

	// Perform cleanup with optional file deletion
	recordsDeleted, filesDeleted, deletionErrors, err := s.cleanupHealthRecords(c.Context(), *req.OlderThan, req.Status, req.DeleteFiles)
	if err != nil {
		return RespondInternalError(c, "Failed to cleanup health records", err.Error())
	}

	response := fiber.Map{
		"records_deleted": recordsDeleted,
		"older_than":      req.OlderThan.Format(time.RFC3339),
		"status_filter":   req.Status,
		"files_deleted":   filesDeleted,
	}

	// Include deletion errors if any occurred
	if len(deletionErrors) > 0 {
		response["file_deletion_errors"] = deletionErrors
		response["warning"] = fmt.Sprintf("%d file(s) could not be deleted", len(deletionErrors))
	}

	return RespondSuccess(c, response)
}

// cleanupHealthRecords is a helper method to cleanup health records
func (s *Server) cleanupHealthRecords(ctx context.Context, olderThan time.Time, statusFilter *database.HealthStatus, deleteFiles bool) (recordsDeleted int, filesDeleted int, deletionErrors []string, err error) {
	// If not deleting files, use direct SQL delete for efficiency (handles unlimited records)
	if !deleteFiles {
		count, deleteErr := s.healthRepo.DeleteHealthRecordsByDate(ctx, olderThan, statusFilter)
		if deleteErr != nil {
			return 0, 0, nil, fmt.Errorf("failed to delete health records: %w", deleteErr)
		}
		return count, 0, nil, nil
	}

	// If deleting files, need to fetch records in batches to get file paths
	const batchSize = 1000
	allFilePaths := make([]string, 0)
	deletedFileCount := 0
	fileErrors := make([]string, 0)
	offset := 0

	cfg := s.configManager.GetConfig()
	mountPath := cfg.MountPath

	// Process records in batches until no more records found
	for {
		// Fetch next batch of records
		items, queryErr := s.healthRepo.ListHealthItems(ctx, statusFilter, batchSize, offset, nil, "", "created_at", "asc")
		if queryErr != nil {
			return 0, 0, nil, fmt.Errorf("failed to query health records: %w", queryErr)
		}

		// No more records found
		if len(items) == 0 {
			break
		}

		// Filter items older than the specified date
		var oldItemsInBatch []*database.FileHealth
		for _, item := range items {
			if item.CreatedAt.Before(olderThan) {
				oldItemsInBatch = append(oldItemsInBatch, item)
			}
		}

		// If no items in this batch match the date criteria, we've processed all old records
		// (since results are sorted by created_at ascending)
		if len(oldItemsInBatch) == 0 {
			break
		}

		// Delete physical files and collect paths
		for _, item := range oldItemsInBatch {
			allFilePaths = append(allFilePaths, item.FilePath)

			// Determine path to delete
			var pathToDelete string
			if item.LibraryPath != nil && *item.LibraryPath != "" {
				pathToDelete = *item.LibraryPath
			} else {
				// Fallback to mount path
				pathToDelete = pathutil.JoinAbsPath(mountPath, item.FilePath)
			}

			// Attempt to delete the physical file using os.Remove
			if deleteErr := os.Remove(pathToDelete); deleteErr != nil {
				// Track error but continue with other files
				fileErrors = append(fileErrors, fmt.Sprintf("%s: %v", item.FilePath, deleteErr))
			} else {
				deletedFileCount++
			}
		}

		// If we got fewer items than the batch size, we've reached the end
		if len(items) < batchSize {
			break
		}

		// If all items in batch were old, continue to next batch
		// If not all items were old, we're done (sorted by date)
		if len(oldItemsInBatch) < len(items) {
			break
		}

		offset += batchSize
	}

	// No records to cleanup
	if len(allFilePaths) == 0 {
		return 0, 0, nil, nil
	}

	// Delete database records (proceed even if some file deletions failed)
	deleteErr := s.healthRepo.DeleteHealthRecordsBulk(ctx, allFilePaths)
	if deleteErr != nil {
		return 0, deletedFileCount, fileErrors, fmt.Errorf("failed to delete health records from database: %w", deleteErr)
	}

	return len(allFilePaths), deletedFileCount, fileErrors, nil
}

// handleAddHealthCheck handles POST /api/health/check
func (s *Server) handleAddHealthCheck(c *fiber.Ctx) error {
	// Parse request body
	var req HealthCheckRequest
	if err := c.BodyParser(&req); err != nil {
		return RespondBadRequest(c, "Invalid request body", err.Error())
	}

	// Validate required fields
	if req.FilePath == "" {
		return RespondValidationError(c, "file_path is required", "")
	}

	// Set default max retries if not specified
	maxRetries := 2 // Default from config
	if req.MaxRetries != nil {
		if *req.MaxRetries < 0 {
			return RespondValidationError(c, "max_retries must be non-negative", "")
		}
		maxRetries = *req.MaxRetries
	}

	// Add file to health database
	err := s.healthRepo.AddFileToHealthCheck(c.Context(), req.FilePath, maxRetries, req.SourceNzb, req.Priority)
	if err != nil {
		return RespondInternalError(c, "Failed to add file for health check", err.Error())
	}

	// Return the health record
	item, err := s.healthRepo.GetFileHealth(c.Context(), req.FilePath)
	if err != nil {
		return RespondInternalError(c, "Failed to retrieve added health record", err.Error())
	}

	response := ToHealthItemResponse(item)
	return RespondSuccess(c, response)
}

// handleGetHealthWorkerStatus handles GET /api/health/worker/status
func (s *Server) handleGetHealthWorkerStatus(c *fiber.Ctx) error {
	if s.healthWorker == nil {
		return RespondNotFound(c, "Health worker", "Health worker is not configured or not running")
	}

	stats := s.healthWorker.GetStats()
	response := HealthWorkerStatusResponse{
		Status:                 string(stats.Status),
		LastRunTime:            stats.LastRunTime,
		NextRunTime:            stats.NextRunTime,
		TotalRunsCompleted:     stats.TotalRunsCompleted,
		TotalFilesChecked:      stats.TotalFilesChecked,
		TotalFilesHealthy:      stats.TotalFilesHealthy,
		TotalFilesCorrupted:    stats.TotalFilesCorrupted,
		CurrentRunStartTime:    stats.CurrentRunStartTime,
		CurrentRunFilesChecked: stats.CurrentRunFilesChecked,
		LastError:              stats.LastError,
		ErrorCount:             stats.ErrorCount,
	}

	return RespondSuccess(c, response)
}

// handleDirectHealthCheck handles POST /api/health/{id}/check-now
func (s *Server) handleDirectHealthCheck(c *fiber.Ctx) error {
	// Extract ID from path parameter
	idStr := c.Params("id")
	if idStr == "" {
		return RespondBadRequest(c, "Health record identifier is required", "")
	}

	// Parse as numeric ID
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return RespondBadRequest(c, "Invalid health record ID", "ID must be a valid integer")
	}

	// Check if health worker is available
	if s.healthWorker == nil {
		return RespondNotFound(c, "Health worker", "Health worker is not configured or not running")
	}

	// Check if item exists in health database
	item, err := s.healthRepo.GetFileHealthByID(c.Context(), id)
	if err != nil {
		return RespondInternalError(c, "Failed to check health record", err.Error())
	}

	if item == nil {
		return RespondNotFound(c, "Health record", "")
	}

	// Prevent starting multiple checks for the same file
	if item.Status == database.HealthStatusChecking {
		return RespondConflict(c, "Health check already in progress", "This file is currently being checked")
	}

	// Immediately set status to 'checking' using ID
	err = s.healthRepo.SetFileCheckingByID(c.Context(), id)
	if err != nil {
		return RespondInternalError(c, "Failed to set checking status", err.Error())
	}

	// Start health check in background using worker (still needs file path)
	err = s.healthWorker.PerformBackgroundCheck(context.Background(), item.FilePath)
	if err != nil {
		return RespondInternalError(c, "Failed to start background health check", err.Error())
	}

	// Verify that the file still exists
	f, err := s.metadataReader.GetFileMetadata(item.FilePath)
	if f == nil || err != nil {
		return RespondInternalError(c, "Failed to retrieve file metadata", err.Error())
	}

	// Get the updated health record with 'checking' status
	updatedItem, err := s.healthRepo.GetFileHealthByID(c.Context(), id)
	if err != nil {
		return RespondInternalError(c, "Failed to retrieve updated health record", err.Error())
	}

	return RespondSuccess(c, fiber.Map{
		"message":     "Health check started",
		"id":          id,
		"file_path":   item.FilePath,
		"old_status":  string(item.Status),
		"new_status":  string(updatedItem.Status),
		"checked_at":  updatedItem.LastChecked,
		"health_data": ToHealthItemResponse(updatedItem),
	})
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

// handleRestartHealthChecksBulk handles POST /api/health/bulk/restart
func (s *Server) handleRestartHealthChecksBulk(c *fiber.Ctx) error {
	// Parse request body
	var req struct {
		FilePaths []string `json:"file_paths"`
	}

	if err := c.BodyParser(&req); err != nil {
		return RespondBadRequest(c, "Invalid request body", err.Error())
	}

	// Validate file paths
	if len(req.FilePaths) == 0 {
		return RespondValidationError(c, "At least one file path is required", "")
	}

	if len(req.FilePaths) > 100 {
		return RespondValidationError(c, "Too many file paths", "Maximum 100 files allowed per bulk operation")
	}

	// Cancel any active checks for these files
	if s.healthWorker != nil {
		for _, filePath := range req.FilePaths {
			// Check if there's an active check to cancel
			if s.healthWorker.IsCheckActive(filePath) {
				// Cancel the health check
				_ = s.healthWorker.CancelHealthCheck(c.Context(), filePath) // Ignore error, proceed with restart
			}
		}
	}

	// Reset all items to pending status using bulk method
	restartedCount, err := s.healthRepo.ResetHealthChecksBulk(c.Context(), req.FilePaths)
	if err != nil {
		return RespondInternalError(c, "Failed to restart health checks", err.Error())
	}

	if restartedCount == 0 {
		return RespondNotFound(c, "Health records", "No health records found to restart")
	}

	response := map[string]any{
		"message":         "Health checks restarted successfully",
		"restarted_count": restartedCount,
		"file_paths":      req.FilePaths,
		"restarted_at":    time.Now().Format(time.RFC3339),
	}

	return RespondSuccess(c, response)
}

// handleCancelHealthCheck handles POST /api/health/{id}/cancel
func (s *Server) handleCancelHealthCheck(c *fiber.Ctx) error {
	// Extract ID from path parameter
	idStr := c.Params("id")
	if idStr == "" {
		return RespondBadRequest(c, "Health record identifier is required", "")
	}

	// Parse as numeric ID
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return RespondBadRequest(c, "Invalid health record ID", "ID must be a valid integer")
	}

	// Check if health worker is available
	if s.healthWorker == nil {
		return RespondNotFound(c, "Health worker", "Health worker is not configured or not running")
	}

	// Check if item exists in health database
	item, err := s.healthRepo.GetFileHealthByID(c.Context(), id)
	if err != nil {
		return RespondInternalError(c, "Failed to check health record", err.Error())
	}

	if item == nil {
		return RespondNotFound(c, "Health record", "")
	}

	// Check if there's actually an active check to cancel (still needs file path)
	if !s.healthWorker.IsCheckActive(item.FilePath) {
		return RespondConflict(c, "No active health check found", "There is no active health check for this file")
	}

	// Cancel the health check (still needs file path)
	err = s.healthWorker.CancelHealthCheck(c.Context(), item.FilePath)
	if err != nil {
		return RespondInternalError(c, "Failed to cancel health check", err.Error())
	}

	// Get the updated health record
	updatedItem, err := s.healthRepo.GetFileHealthByID(c.Context(), id)
	if err != nil {
		return RespondInternalError(c, "Failed to retrieve updated health record", err.Error())
	}

	response := map[string]any{
		"message":      "Health check cancelled",
		"id":           id,
		"file_path":    item.FilePath,
		"old_status":   string(item.Status),
		"new_status":   string(updatedItem.Status),
		"cancelled_at": time.Now().Format(time.RFC3339),
		"health_data":  ToHealthItemResponse(updatedItem),
	}

	return RespondSuccess(c, response)
}

// handleSetHealthPriority handles POST /api/health/{id}/priority
func (s *Server) handleSetHealthPriority(c *fiber.Ctx) error {
	// Extract ID from path parameter
	idStr := c.Params("id")
	if idStr == "" {
		return RespondBadRequest(c, "Health record identifier is required", "")
	}

	// Parse as numeric ID
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return RespondBadRequest(c, "Invalid health record ID", "ID must be a valid integer")
	}

	// Parse request body
	var req struct {
		Priority database.HealthPriority `json:"priority"`
	}

	if err := c.BodyParser(&req); err != nil {
		return RespondBadRequest(c, "Invalid request body", err.Error())
	}

	// Check if item exists in health database
	item, err := s.healthRepo.GetFileHealthByID(c.Context(), id)
	if err != nil {
		return RespondInternalError(c, "Failed to check health record", err.Error())
	}

	if item == nil {
		return RespondNotFound(c, "Health record", "")
	}

	// Set priority
	err = s.healthRepo.SetPriority(c.Context(), id, req.Priority)
	if err != nil {
		return RespondInternalError(c, "Failed to update priority", err.Error())
	}

	// Get the updated health record
	updatedItem, err := s.healthRepo.GetFileHealthByID(c.Context(), id)
	if err != nil {
		return RespondInternalError(c, "Failed to retrieve updated health record", err.Error())
	}

	response := map[string]any{
		"message":     "Health priority updated",
		"id":          id,
		"file_path":   item.FilePath,
		"priority":    updatedItem.Priority,
		"updated_at":  time.Now().Format(time.RFC3339),
		"health_data": ToHealthItemResponse(updatedItem),
	}

	return RespondSuccess(c, response)
}

// handleResetAllHealthChecks handles POST /api/health/reset-all
func (s *Server) handleResetAllHealthChecks(c *fiber.Ctx) error {
	// Reset all items to pending status using repository method
	restartedCount, err := s.healthRepo.ResetAllHealthChecks(c.Context())
	if err != nil {
		return RespondInternalError(c, "Failed to reset all health checks", err.Error())
	}

	response := map[string]any{
		"message":         "All health checks reset successfully",
		"restarted_count": restartedCount,
		"restarted_at":    time.Now().Format(time.RFC3339),
	}

	return RespondSuccess(c, response)
}

// handleRegenerateSymlinks handles POST /api/health/regenerate-symlinks
func (s *Server) handleRegenerateSymlinks(c *fiber.Ctx) error {
	ctx := c.Context()
	cfg := s.configManager.GetConfig()

	// Validate that symlink strategy is enabled
	if cfg.Import.ImportStrategy != config.ImportStrategySYMLINK {
		return RespondBadRequest(c, "Symlink regeneration is only available when import strategy is set to SYMLINK", "")
	}

	if cfg.Import.ImportDir == nil || *cfg.Import.ImportDir == "" {
		return RespondBadRequest(c, "Import directory is not configured", "")
	}

	// Get all files without library path
	files, err := s.healthRepo.GetFilesWithoutLibraryPath(ctx)
	if err != nil {
		return RespondInternalError(c, "Failed to retrieve files without library path", err.Error())
	}

	if len(files) == 0 {
		return RespondSuccess(c, fiber.Map{
			"message":          "No files without library path found",
			"files_processed":  0,
			"symlinks_created": 0,
			"errors":           []string{},
			"completed_at":     time.Now().Format(time.RFC3339),
		})
	}

	successCount := 0
	errorCount := 0
	errors := make([]string, 0)

	for _, file := range files {
		// Build the actual file path in the mount
		actualPath := pathutil.JoinAbsPath(cfg.MountPath, file.FilePath)

		// Build the symlink path in the import directory
		symlinkPath := pathutil.JoinAbsPath(*cfg.Import.ImportDir, file.FilePath)

		// Create directory if needed
		baseDir := filepath.Dir(symlinkPath)
		if err := os.MkdirAll(baseDir, 0755); err != nil {
			errorCount++
			errors = append(errors, fmt.Sprintf("%s: failed to create directory: %v", file.FilePath, err))
			continue
		}

		// Remove existing symlink if present
		if _, err := os.Lstat(symlinkPath); err == nil {
			if err := os.Remove(symlinkPath); err != nil {
				errorCount++
				errors = append(errors, fmt.Sprintf("%s: failed to remove existing symlink: %v", file.FilePath, err))
				continue
			}
		}

		// Create the symlink
		if err := os.Symlink(actualPath, symlinkPath); err != nil {
			errorCount++
			errors = append(errors, fmt.Sprintf("%s: failed to create symlink: %v", file.FilePath, err))
			continue
		}

		// Update the library path in the database
		if err := s.healthRepo.UpdateLibraryPath(ctx, file.FilePath, symlinkPath); err != nil {
			slog.ErrorContext(ctx, "Failed to update library path in database",
				"file_path", file.FilePath,
				"symlink_path", symlinkPath,
				"error", err)
			// Don't count as error since symlink was created successfully
		}

		successCount++
	}

	response := fiber.Map{
		"message":          fmt.Sprintf("Regenerated symlinks for %d files", successCount),
		"files_processed":  len(files),
		"symlinks_created": successCount,
		"errors":           errors,
		"error_count":      errorCount,
		"completed_at":     time.Now().Format(time.RFC3339),
	}

	if errorCount > 0 {
		response["warning"] = fmt.Sprintf("%d file(s) failed to regenerate symlinks", errorCount)
	}

	return RespondSuccess(c, response)
}
