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
	"github.com/javi11/altmount/internal/database"
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
		"file_path":  true,
		"created_at": true,
		"status":     true,
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
		status := database.HealthStatus(statusStr)
		// Validate status
		switch status {
		case database.HealthStatusPending, database.HealthStatusChecking, database.HealthStatusCorrupted, database.HealthStatusRepairTriggered:
			statusFilter = &status
		default:
			return c.Status(400).JSON(fiber.Map{
				"success": false,
				"error": fiber.Map{
					"code":    "VALIDATION_ERROR",
					"message": "Invalid status filter",
					"details": "Valid values: pending, checking, corrupted, repair_triggered",
				},
			})
		}
	}

	// Parse since filter
	var sinceFilter *time.Time
	if since, err := ParseTimeParamFiber(c, "since"); err != nil {
		return c.Status(400).JSON(fiber.Map{
			"success": false,
			"error": fiber.Map{
				"code":    "VALIDATION_ERROR",
				"message": "Invalid since parameter",
				"details": err.Error(),
			},
		})
	} else if since != nil {
		sinceFilter = since
	}

	// Get health items with search and sort support
	items, err := s.listHealthItems(c.Context(), statusFilter, pagination, sinceFilter, search, sortBy, sortOrder)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"error": fiber.Map{
				"code":    "INTERNAL_SERVER_ERROR",
				"message": "Failed to retrieve health records",
				"details": err.Error(),
			},
		})
	}

	// Get total count for pagination
	totalCount, err := s.countHealthItems(c.Context(), statusFilter, sinceFilter, search)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"error": fiber.Map{
				"code":    "INTERNAL_SERVER_ERROR",
				"message": "Failed to count health records",
				"details": err.Error(),
			},
		})
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

	return c.Status(200).JSON(fiber.Map{
		"success": true,
		"data":    response,
		"meta":    meta,
	})
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
		return c.Status(400).JSON(fiber.Map{
			"success": false,
			"error": fiber.Map{
				"code":    "BAD_REQUEST",
				"message": "Health record identifier is required",
				"details": "",
			},
		})
	}

	// Parse as numeric ID
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{
			"success": false,
			"error": fiber.Map{
				"code":    "BAD_REQUEST",
				"message": "Invalid health record ID",
				"details": "ID must be a valid integer",
			},
		})
	}

	// Get by ID
	item, err := s.healthRepo.GetFileHealthByID(c.Context(), id)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"error": fiber.Map{
				"code":    "INTERNAL_SERVER_ERROR",
				"message": "Failed to retrieve health record",
				"details": err.Error(),
			},
		})
	}

	if item == nil {
		return c.Status(404).JSON(fiber.Map{
			"success": false,
			"error": fiber.Map{
				"code":    "NOT_FOUND",
				"message": "Health record not found",
				"details": "",
			},
		})
	}

	response := ToHealthItemResponse(item)
	return c.Status(200).JSON(fiber.Map{
		"success": true,
		"data":    response,
	})
}

// handleDeleteHealth handles DELETE /api/health/{id}
func (s *Server) handleDeleteHealth(c *fiber.Ctx) error {
	// Extract ID from path parameter
	idStr := c.Params("id")
	if idStr == "" {
		return c.Status(400).JSON(fiber.Map{
			"success": false,
			"message": "Health record identifier is required",
		})
	}

	// Parse as numeric ID
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{
			"success": false,
			"message": "Invalid health record ID",
			"details": "ID must be a valid integer",
		})
	}

	// Check if the record exists
	item, err := s.healthRepo.GetFileHealthByID(c.Context(), id)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Failed to check health record",
			"details": err.Error(),
		})
	}

	if item == nil {
		return c.Status(404).JSON(fiber.Map{
			"success": false,
			"message": "Health record not found",
		})
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
					return c.Status(500).JSON(fiber.Map{
						"success": false,
						"message": "Failed to cancel health check before deletion",
						"details": err.Error(),
					})
				}
			}
		}
	}

	// Delete the health record from database using ID
	err = s.healthRepo.DeleteHealthRecordByID(c.Context(), id)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Failed to delete health record",
			"details": err.Error(),
		})
	}

	response := map[string]interface{}{
		"message":    "Health record deleted successfully",
		"id":         id,
		"file_path":  item.FilePath,
		"deleted_at": time.Now().Format(time.RFC3339),
	}

	return c.Status(200).JSON(fiber.Map{
		"success": true,
		"data":    response,
	})
}

// handleDeleteHealthBulk handles POST /api/health/bulk/delete
func (s *Server) handleDeleteHealthBulk(c *fiber.Ctx) error {
	// Parse request body
	var req struct {
		FilePaths []string `json:"file_paths"`
	}

	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{
			"success": false,
			"message": "Invalid request body",
			"details": err.Error(),
		})
	}

	// Validate file paths
	if len(req.FilePaths) == 0 {
		return c.Status(422).JSON(fiber.Map{
			"success": false,
			"message": "At least one file path is required",
		})
	}

	if len(req.FilePaths) > 100 {
		return c.Status(422).JSON(fiber.Map{
			"success": false,
			"message": "Too many file paths",
			"details": "Maximum 100 files allowed per bulk operation",
		})
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
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Failed to delete health records",
			"details": err.Error(),
		})
	}

	response := map[string]interface{}{
		"message":       "Health records deleted successfully",
		"deleted_count": len(req.FilePaths),
		"file_paths":    req.FilePaths,
		"deleted_at":    time.Now().Format(time.RFC3339),
	}

	return c.Status(200).JSON(fiber.Map{
		"success": true,
		"data":    response,
	})
}

// handleRepairHealth handles POST /api/health/{id}/repair
func (s *Server) handleRepairHealth(c *fiber.Ctx) error {
	// Extract ID from path parameter
	idStr := c.Params("id")
	if idStr == "" {
		return c.Status(400).JSON(fiber.Map{
			"success": false,
			"message": "Health record identifier is required",
		})
	}

	// Parse as numeric ID
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{
			"success": false,
			"message": "Invalid health record ID",
			"details": "ID must be a valid integer",
		})
	}

	// Parse request body
	var req HealthRepairRequest
	if len(c.Body()) > 0 {
		if err := c.BodyParser(&req); err != nil {
			return c.Status(400).JSON(fiber.Map{
				"success": false,
				"message": "Invalid request body",
				"details": err.Error(),
			})
		}
	}

	// Check if item exists
	item, err := s.healthRepo.GetFileHealthByID(c.Context(), id)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Failed to check health record",
			"details": err.Error(),
		})
	}

	if item == nil {
		return c.Status(404).JSON(fiber.Map{
			"success": false,
			"message": "Health record not found",
		})
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
	if pathForRescan == "" {
		// Fallback to mount path if no library path found
		pathForRescan = filepath.Join(cfg.MountPath, item.FilePath)
		slog.InfoContext(ctx, "Using mount path fallback for manual repair",
			"file_path", item.FilePath,
			"mount_path", pathForRescan)
	}

	// Trigger rescan with the resolved path
	err = s.arrsService.TriggerFileRescan(ctx, pathForRescan)
	if err != nil {
		// Check if this is a "no ARR instance found" error
		if strings.Contains(err.Error(), "no ARR instance found") {
			return c.Status(404).JSON(fiber.Map{
				"success": false,
				"message": "File not managed by any ARR instance",
				"details": "This file is not found in any of the configured Radarr or Sonarr instances. Please ensure the file is in your media library and the ARR instances are properly configured.",
			})
		}
		// Handle other errors as internal server errors
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Failed to trigger repair in ARR instance, you might need to trigger a manual library sync",
			"details": err.Error(),
		})
	}

	// Remove file from health table since repair was successfully triggered to ARR
	if err := s.healthRepo.DeleteHealthRecord(ctx, item.FilePath); err != nil {
		slog.ErrorContext(ctx, "Failed to delete health record after repair trigger",
			"error", err,
			"file_path", item.FilePath)
		// Don't fail the repair trigger if deletion fails - just log the error
	} else {
		slog.InfoContext(ctx, "Removed file from health table after successful repair trigger",
			"file_path", item.FilePath)
	}

	// Get updated item
	updatedItem, err := s.healthRepo.GetFileHealthByID(c.Context(), id)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Failed to retrieve updated health record",
			"details": err.Error(),
		})
	}

	response := ToHealthItemResponse(updatedItem)
	return c.Status(200).JSON(fiber.Map{
		"success": true,
		"data":    response,
	})
}

// handleListCorrupted handles GET /api/health/corrupted
func (s *Server) handleListCorrupted(c *fiber.Ctx) error {
	// Parse pagination
	pagination := ParsePaginationFiber(c)

	// Get corrupted files using GetUnhealthyFiles
	items, err := s.healthRepo.GetUnhealthyFiles(c.Context(), pagination.Limit)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Failed to retrieve corrupted files",
			"details": err.Error(),
		})
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

	return c.Status(200).JSON(fiber.Map{
		"success": true,
		"data":    response,
		"meta":    meta,
	})
}

// handleGetHealthStats handles GET /api/health/stats
func (s *Server) handleGetHealthStats(c *fiber.Ctx) error {
	stats, err := s.healthRepo.GetHealthStats(c.Context())
	if err != nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Failed to retrieve health statistics",
			"details": err.Error(),
		})
	}

	response := ToHealthStatsResponse(stats)
	return c.Status(200).JSON(fiber.Map{
		"success": true,
		"data":    response,
	})
}

// handleCleanupHealth handles DELETE /api/health/cleanup
func (s *Server) handleCleanupHealth(c *fiber.Ctx) error {
	// Parse request body
	var req HealthCleanupRequest
	if len(c.Body()) > 0 {
		if err := c.BodyParser(&req); err != nil {
			return c.Status(400).JSON(fiber.Map{
				"success": false,
				"message": "Invalid request body",
				"details": err.Error(),
			})
		}
	}

	// Parse older_than parameter from query if not in body
	if req.OlderThan == nil {
		if olderThan, err := ParseTimeParamFiber(c, "older_than"); err != nil {
			return c.Status(422).JSON(fiber.Map{
				"success": false,
				"message": "Invalid older_than parameter",
				"details": err.Error(),
			})
		} else if olderThan != nil {
			req.OlderThan = olderThan
		}
	}

	// Parse status parameter from query if not in body
	if req.Status == nil {
		if statusStr := c.Query("status"); statusStr != "" {
			status := database.HealthStatus(statusStr)
			switch status {
			case database.HealthStatusPending, database.HealthStatusChecking, database.HealthStatusCorrupted, database.HealthStatusRepairTriggered:
				req.Status = &status
			default:
				return c.Status(422).JSON(fiber.Map{
					"success": false,
					"message": "Invalid status filter",
					"details": "Valid values: pending, checking, corrupted, repair_triggered",
				})
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
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Failed to cleanup health records",
			"details": err.Error(),
		})
	}

	response := map[string]interface{}{
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

	return c.Status(200).JSON(fiber.Map{
		"success": true,
		"data":    response,
	})
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

			// Attempt to delete the physical file using os.Remove
			if deleteErr := os.Remove(item.FilePath); deleteErr != nil {
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
		return c.Status(400).JSON(fiber.Map{
			"success": false,
			"message": "Invalid request body",
			"details": err.Error(),
		})
	}

	// Validate required fields
	if req.FilePath == "" {
		return c.Status(422).JSON(fiber.Map{
			"success": false,
			"message": "file_path is required",
		})
	}

	// Set default max retries if not specified
	maxRetries := 2 // Default from config
	if req.MaxRetries != nil {
		if *req.MaxRetries < 0 {
			return c.Status(422).JSON(fiber.Map{
				"success": false,
				"message": "max_retries must be non-negative",
			})
		}
		maxRetries = *req.MaxRetries
	}

	// Add file to health database
	err := s.healthRepo.AddFileToHealthCheck(c.Context(), req.FilePath, maxRetries, req.SourceNzb)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Failed to add file for health check",
			"details": err.Error(),
		})
	}

	// Return the health record
	item, err := s.healthRepo.GetFileHealth(c.Context(), req.FilePath)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Failed to retrieve added health record",
			"details": err.Error(),
		})
	}

	response := ToHealthItemResponse(item)
	return c.Status(200).JSON(fiber.Map{
		"success": true,
		"data":    response,
	})
}

// handleGetHealthWorkerStatus handles GET /api/health/worker/status
func (s *Server) handleGetHealthWorkerStatus(c *fiber.Ctx) error {
	if s.healthWorker == nil {
		return c.Status(404).JSON(fiber.Map{
			"success": false,
			"message": "Health worker not available",
			"details": "Health worker is not configured or not running",
		})
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
		PendingManualChecks:    stats.PendingManualChecks,
		LastError:              stats.LastError,
		ErrorCount:             stats.ErrorCount,
	}

	return c.Status(200).JSON(fiber.Map{
		"success": true,
		"data":    response,
	})
}

// handleDirectHealthCheck handles POST /api/health/{id}/check-now
func (s *Server) handleDirectHealthCheck(c *fiber.Ctx) error {
	// Extract ID from path parameter
	idStr := c.Params("id")
	if idStr == "" {
		return c.Status(400).JSON(fiber.Map{
			"success": false,
			"message": "Health record identifier is required",
		})
	}

	// Parse as numeric ID
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{
			"success": false,
			"message": "Invalid health record ID",
			"details": "ID must be a valid integer",
		})
	}

	// Check if health worker is available
	if s.healthWorker == nil {
		return c.Status(404).JSON(fiber.Map{
			"success": false,
			"message": "Health worker not available",
			"details": "Health worker is not configured or not running",
		})
	}

	// Check if item exists in health database
	item, err := s.healthRepo.GetFileHealthByID(c.Context(), id)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Failed to check health record",
			"details": err.Error(),
		})
	}

	if item == nil {
		return c.Status(404).JSON(fiber.Map{
			"success": false,
			"message": "Health record not found",
		})
	}

	// Prevent starting multiple checks for the same file
	if item.Status == database.HealthStatusChecking {
		return c.Status(409).JSON(fiber.Map{
			"success": false,
			"message": "Health check already in progress",
			"details": "This file is currently being checked",
		})
	}

	// Immediately set status to 'checking' using ID
	err = s.healthRepo.SetFileCheckingByID(c.Context(), id)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Failed to set checking status",
			"details": err.Error(),
		})
	}

	// Start health check in background using worker (still needs file path)
	err = s.healthWorker.PerformBackgroundCheck(context.Background(), item.FilePath)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Failed to start background health check",
			"details": err.Error(),
		})
	}

	// Verify that the file still exists
	f, err := s.metadataReader.GetFileMetadata(item.FilePath)
	if f == nil || err != nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Failed to retrieve file metadata",
			"details": err.Error(),
		})
	}

	// Get the updated health record with 'checking' status
	updatedItem, err := s.healthRepo.GetFileHealthByID(c.Context(), id)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Failed to retrieve updated health record",
			"details": err.Error(),
		})
	}

	response := map[string]interface{}{
		"message":     "Health check started",
		"id":          id,
		"file_path":   item.FilePath,
		"old_status":  string(item.Status),
		"new_status":  string(updatedItem.Status),
		"checked_at":  updatedItem.LastChecked,
		"health_data": ToHealthItemResponse(updatedItem),
	}

	return c.Status(200).JSON(fiber.Map{
		"success": true,
		"data":    response,
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
		return c.Status(400).JSON(fiber.Map{
			"success": false,
			"message": "Invalid request body",
			"details": err.Error(),
		})
	}

	// Validate file paths
	if len(req.FilePaths) == 0 {
		return c.Status(422).JSON(fiber.Map{
			"success": false,
			"message": "At least one file path is required",
		})
	}

	if len(req.FilePaths) > 100 {
		return c.Status(422).JSON(fiber.Map{
			"success": false,
			"message": "Too many file paths",
			"details": "Maximum 100 files allowed per bulk operation",
		})
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
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Failed to restart health checks",
			"details": err.Error(),
		})
	}

	if restartedCount == 0 {
		return c.Status(404).JSON(fiber.Map{
			"success": false,
			"message": "No health records found to restart",
		})
	}

	response := map[string]interface{}{
		"message":         "Health checks restarted successfully",
		"restarted_count": restartedCount,
		"file_paths":      req.FilePaths,
		"restarted_at":    time.Now().Format(time.RFC3339),
	}

	return c.Status(200).JSON(fiber.Map{
		"success": true,
		"data":    response,
	})
}

// handleCancelHealthCheck handles POST /api/health/{id}/cancel
func (s *Server) handleCancelHealthCheck(c *fiber.Ctx) error {
	// Extract ID from path parameter
	idStr := c.Params("id")
	if idStr == "" {
		return c.Status(400).JSON(fiber.Map{
			"success": false,
			"message": "Health record identifier is required",
		})
	}

	// Parse as numeric ID
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{
			"success": false,
			"message": "Invalid health record ID",
			"details": "ID must be a valid integer",
		})
	}

	// Check if health worker is available
	if s.healthWorker == nil {
		return c.Status(404).JSON(fiber.Map{
			"success": false,
			"message": "Health worker not available",
			"details": "Health worker is not configured or not running",
		})
	}

	// Check if item exists in health database
	item, err := s.healthRepo.GetFileHealthByID(c.Context(), id)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Failed to check health record",
			"details": err.Error(),
		})
	}

	if item == nil {
		return c.Status(404).JSON(fiber.Map{
			"success": false,
			"message": "Health record not found",
		})
	}

	// Check if there's actually an active check to cancel (still needs file path)
	if !s.healthWorker.IsCheckActive(item.FilePath) {
		return c.Status(409).JSON(fiber.Map{
			"success": false,
			"message": "No active health check found",
			"details": "There is no active health check for this file",
		})
	}

	// Cancel the health check (still needs file path)
	err = s.healthWorker.CancelHealthCheck(c.Context(), item.FilePath)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Failed to cancel health check",
			"details": err.Error(),
		})
	}

	// Get the updated health record
	updatedItem, err := s.healthRepo.GetFileHealthByID(c.Context(), id)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Failed to retrieve updated health record",
			"details": err.Error(),
		})
	}

	response := map[string]interface{}{
		"message":      "Health check cancelled",
		"id":           id,
		"file_path":    item.FilePath,
		"old_status":   string(item.Status),
		"new_status":   string(updatedItem.Status),
		"cancelled_at": time.Now().Format(time.RFC3339),
		"health_data":  ToHealthItemResponse(updatedItem),
	}

	return c.Status(200).JSON(fiber.Map{
		"success": true,
		"data":    response,
	})
}
