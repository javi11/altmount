package api

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/javi11/altmount/internal/database"
)

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
			database.QueueStatusCompleted, database.QueueStatusFailed,
			database.QueueStatusRetrying:
			statusFilter = &status
		default:
			return c.Status(400).JSON(fiber.Map{
				"success": false,
				"error": fiber.Map{
					"code":    "VALIDATION_ERROR",
					"message": "Invalid status filter",
					"details": "Valid values: pending, processing, completed, failed, retrying",
				},
			})
		}
	}

	// Parse search parameter
	searchFilter := c.Query("search")

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

	// Get total count for pagination
	totalCount, err := s.queueRepo.CountQueueItems(statusFilter, searchFilter)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"error": fiber.Map{
				"code":    "INTERNAL_SERVER_ERROR",
				"message": "Failed to count queue items",
				"details": err.Error(),
			},
		})
	}

	// Get queue items from repository
	items, err := s.queueRepo.ListQueueItems(statusFilter, searchFilter, pagination.Limit, pagination.Offset)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"error": fiber.Map{
				"code":    "INTERNAL_SERVER_ERROR",
				"message": "Failed to retrieve queue items",
				"details": err.Error(),
			},
		})
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

	return c.Status(200).JSON(fiber.Map{
		"success": true,
		"data":    response,
		"meta":    meta,
	})
}

// handleGetQueue handles GET /api/queue/{id}
func (s *Server) handleGetQueue(c *fiber.Ctx) error {
	// Extract ID from path parameter
	idStr := c.Params("id")
	if idStr == "" {
		return c.Status(400).JSON(fiber.Map{
			"success": false,
			"error": fiber.Map{
				"code":    "BAD_REQUEST",
				"message": "Queue item ID is required",
				"details": "",
			},
		})
	}

	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{
			"success": false,
			"error": fiber.Map{
				"code":    "BAD_REQUEST",
				"message": "Invalid queue item ID",
				"details": "ID must be a valid integer",
			},
		})
	}

	// Get queue item from repository
	item, err := s.queueRepo.GetQueueItem(id)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"error": fiber.Map{
				"code":    "INTERNAL_SERVER_ERROR",
				"message": "Failed to retrieve queue item",
				"details": err.Error(),
			},
		})
	}

	if item == nil {
		return c.Status(404).JSON(fiber.Map{
			"success": false,
			"error": fiber.Map{
				"code":    "NOT_FOUND",
				"message": "Queue item not found",
				"details": "",
			},
		})
	}

	// Convert to API response format
	response := ToQueueItemResponse(item)
	return c.Status(200).JSON(fiber.Map{
		"success": true,
		"data":    response,
	})
}

// handleDeleteQueue handles DELETE /api/queue/{id}
func (s *Server) handleDeleteQueue(c *fiber.Ctx) error {
	// Extract ID from path parameter
	idStr := c.Params("id")
	if idStr == "" {
		return c.Status(400).JSON(fiber.Map{
			"success": false,
			"error": fiber.Map{
				"code":    "BAD_REQUEST",
				"message": "Queue item ID is required",
				"details": "",
			},
		})
	}

	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{
			"success": false,
			"error": fiber.Map{
				"code":    "BAD_REQUEST",
				"message": "Invalid queue item ID",
				"details": "ID must be a valid integer",
			},
		})
	}

	// Check if item exists first
	item, err := s.queueRepo.GetQueueItem(id)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"error": fiber.Map{
				"code":    "INTERNAL_SERVER_ERROR",
				"message": "Failed to check queue item",
				"details": err.Error(),
			},
		})
	}

	if item == nil {
		return c.Status(404).JSON(fiber.Map{
			"success": false,
			"error": fiber.Map{
				"code":    "NOT_FOUND",
				"message": "Queue item not found",
				"details": "",
			},
		})
	}

	// Prevent deletion of items currently being processed
	if item.Status == database.QueueStatusProcessing {
		return c.Status(409).JSON(fiber.Map{
			"success": false,
			"error": fiber.Map{
				"code":    "CONFLICT",
				"message": "Cannot delete item currently being processed",
				"details": "Wait for processing to complete or fail",
			},
		})
	}

	// Remove from queue
	err = s.queueRepo.RemoveFromQueue(id)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"error": fiber.Map{
				"code":    "INTERNAL_SERVER_ERROR",
				"message": "Failed to delete queue item",
				"details": err.Error(),
			},
		})
	}

	return c.SendStatus(204)
}

// handleRetryQueue handles POST /api/queue/{id}/retry
func (s *Server) handleRetryQueue(c *fiber.Ctx) error {
	// Extract ID from path parameter
	idStr := c.Params("id")
	if idStr == "" {
		return c.Status(400).JSON(fiber.Map{
			"success": false,
			"error": fiber.Map{
				"code":    "BAD_REQUEST",
				"message": "Queue item ID is required",
				"details": "",
			},
		})
	}

	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{
			"success": false,
			"error": fiber.Map{
				"code":    "BAD_REQUEST",
				"message": "Invalid queue item ID",
				"details": "ID must be a valid integer",
			},
		})
	}

	// Check if item exists
	item, err := s.queueRepo.GetQueueItem(id)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"error": fiber.Map{
				"code":    "INTERNAL_SERVER_ERROR",
				"message": "Failed to check queue item",
				"details": err.Error(),
			},
		})
	}

	if item == nil {
		return c.Status(404).JSON(fiber.Map{
			"success": false,
			"error": fiber.Map{
				"code":    "NOT_FOUND",
				"message": "Queue item not found",
				"details": "",
			},
		})
	}

	// Only allow retry of pending, failed or completed items
	if item.Status != database.QueueStatusPending && item.Status != database.QueueStatusFailed && item.Status != database.QueueStatusCompleted {
		return c.Status(409).JSON(fiber.Map{
			"success": false,
			"error": fiber.Map{
				"code":    "CONFLICT",
				"message": "Can only retry pending, failed or completed items",
				"details": "Current status: " + string(item.Status),
			},
		})
	}

	// Update status to retrying
	err = s.queueRepo.UpdateQueueItemStatus(id, database.QueueStatusRetrying, nil)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"error": fiber.Map{
				"code":    "INTERNAL_SERVER_ERROR",
				"message": "Failed to retry queue item",
				"details": err.Error(),
			},
		})
	}

	// Trigger background processing immediately
	if s.importerService != nil {
		s.importerService.ProcessItemInBackground(id)
	}

	// Get updated item
	updatedItem, err := s.queueRepo.GetQueueItem(id)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"error": fiber.Map{
				"code":    "INTERNAL_SERVER_ERROR",
				"message": "Failed to retrieve updated queue item",
				"details": err.Error(),
			},
		})
	}

	response := ToQueueItemResponse(updatedItem)
	return c.Status(200).JSON(fiber.Map{
		"success": true,
		"data":    response,
	})
}

// handleGetQueueStats handles GET /api/queue/stats
func (s *Server) handleGetQueueStats(c *fiber.Ctx) error {
	stats, err := s.queueRepo.GetQueueStats()
	if err != nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"error": fiber.Map{
				"code":    "INTERNAL_SERVER_ERROR",
				"message": "Failed to retrieve queue statistics",
				"details": err.Error(),
			},
		})
	}

	response := ToQueueStatsResponse(stats)
	return c.Status(200).JSON(fiber.Map{
		"success": true,
		"data":    response,
	})
}

// handleClearCompletedQueue handles DELETE /api/queue/completed
func (s *Server) handleClearCompletedQueue(c *fiber.Ctx) error {
	// Parse older_than parameter
	olderThan, err := ParseTimeParamFiber(c, "older_than")
	if err != nil {
		return c.Status(400).JSON(fiber.Map{
			"success": false,
			"error": fiber.Map{
				"code":    "VALIDATION_ERROR",
				"message": "Invalid older_than parameter",
				"details": err.Error(),
			},
		})
	}

	// Default to 24 hours ago if not specified
	if olderThan == nil {
		defaultTime := time.Now().Add(-24 * time.Hour)
		olderThan = &defaultTime
	}

	// Clear completed items
	count, err := s.queueRepo.ClearCompletedQueueItems(*olderThan)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"error": fiber.Map{
				"code":    "INTERNAL_SERVER_ERROR",
				"message": "Failed to clear completed queue items",
				"details": err.Error(),
			},
		})
	}

	response := map[string]interface{}{
		"removed_count": count,
		"older_than":    olderThan.Format(time.RFC3339),
	}

	return c.Status(200).JSON(fiber.Map{
		"success": true,
		"data":    response,
	})
}

// handleClearFailedQueue handles DELETE /api/queue/failed
func (s *Server) handleClearFailedQueue(c *fiber.Ctx) error {
	// Parse older_than parameter
	olderThan, err := ParseTimeParamFiber(c, "older_than")
	if err != nil {
		return c.Status(400).JSON(fiber.Map{
			"success": false,
			"error": fiber.Map{
				"code":    "VALIDATION_ERROR",
				"message": "Invalid older_than parameter",
				"details": err.Error(),
			},
		})
	}

	// Default to 24 hours ago if not specified
	if olderThan == nil {
		defaultTime := time.Now().Add(-24 * time.Hour)
		olderThan = &defaultTime
	}

	// Clear failed items
	count, err := s.queueRepo.ClearFailedQueueItems(*olderThan)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"error": fiber.Map{
				"code":    "INTERNAL_SERVER_ERROR",
				"message": "Failed to clear failed queue items",
				"details": err.Error(),
			},
		})
	}

	response := map[string]interface{}{
		"removed_count": count,
		"older_than":    olderThan.Format(time.RFC3339),
	}

	return c.Status(200).JSON(fiber.Map{
		"success": true,
		"data":    response,
	})
}

// handleDeleteQueueBulk handles DELETE /api/queue/bulk
func (s *Server) handleDeleteQueueBulk(c *fiber.Ctx) error {
	// Parse request body
	var request struct {
		IDs []int64 `json:"ids"`
	}

	if err := c.BodyParser(&request); err != nil {
		return c.Status(400).JSON(fiber.Map{
			"success": false,
			"error": fiber.Map{
				"code":    "BAD_REQUEST",
				"message": "Invalid request body",
				"details": err.Error(),
			},
		})
	}

	// Validate IDs
	if len(request.IDs) == 0 {
		return c.Status(400).JSON(fiber.Map{
			"success": false,
			"error": fiber.Map{
				"code":    "BAD_REQUEST",
				"message": "No IDs provided",
				"details": "At least one ID is required",
			},
		})
	}

	// Remove from queue in bulk (this will check for processing items)
	result, err := s.queueRepo.RemoveFromQueueBulk(request.IDs)
	if err != nil {
		// Check if the error is about processing items
		if result != nil && result.ProcessingCount > 0 {
			return c.Status(409).JSON(fiber.Map{
				"success": false,
				"error": fiber.Map{
					"code":    "CONFLICT",
					"message": "Cannot delete items currently being processed",
					"details": fmt.Sprintf("%d items are currently being processed", result.ProcessingCount),
				},
			})
		}

		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"error": fiber.Map{
				"code":    "INTERNAL_SERVER_ERROR",
				"message": "Failed to delete queue items",
				"details": err.Error(),
			},
		})
	}

	response := map[string]interface{}{
		"deleted_count": result.DeletedCount,
		"message":       fmt.Sprintf("Successfully deleted %d queue items", result.DeletedCount),
	}

	return c.Status(200).JSON(fiber.Map{
		"success": true,
		"data":    response,
	})
}

// handleUploadToQueue handles POST /api/queue/upload
func (s *Server) handleUploadToQueue(c *fiber.Ctx) error {
	// Get uploaded file
	file, err := c.FormFile("file")
	if err != nil {
		return c.Status(400).JSON(fiber.Map{
			"success": false,
			"error": fiber.Map{
				"code":    "BAD_REQUEST",
				"message": "No file provided",
				"details": "A file must be uploaded",
			},
		})
	}

	// Validate file extension
	if !strings.HasSuffix(strings.ToLower(file.Filename), ".nzb") {
		return c.Status(400).JSON(fiber.Map{
			"success": false,
			"error": fiber.Map{
				"code":    "VALIDATION_ERROR",
				"message": "Invalid file type",
				"details": "Only .nzb files are allowed",
			},
		})
	}

	// Validate file size (100MB limit)
	if file.Size > 100*1024*1024 {
		return c.Status(400).JSON(fiber.Map{
			"success": false,
			"error": fiber.Map{
				"code":    "VALIDATION_ERROR",
				"message": "File too large",
				"details": "File size must be less than 100MB",
			},
		})
	}

	// Get optional category from form
	category := c.FormValue("category")

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
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"error": fiber.Map{
				"code":    "INTERNAL_SERVER_ERROR",
				"message": "Failed to create upload directory",
				"details": err.Error(),
			},
		})
	}

	// Save the uploaded file to temporary location
	tempFile := filepath.Join(uploadDir, file.Filename)
	if err := c.SaveFile(file, tempFile); err != nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"error": fiber.Map{
				"code":    "INTERNAL_SERVER_ERROR",
				"message": "Failed to save file",
				"details": err.Error(),
			},
		})
	}

	// Add to queue using importer service
	if s.importerService == nil {
		// Clean up temp file
		os.Remove(tempFile)
		return c.Status(503).JSON(fiber.Map{
			"success": false,
			"error": fiber.Map{
				"code":    "SERVICE_UNAVAILABLE",
				"message": "Importer service not available",
				"details": "The import service is not configured or running",
			},
		})
	}

	// Add the file to the processing queue
	var categoryPtr *string
	if category != "" {
		categoryPtr = &category
	}

	item, err := s.importerService.AddToQueue(tempFile, &uploadDir, categoryPtr, &priority)
	if err != nil {
		// Clean up temp file on error
		os.Remove(tempFile)
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"error": fiber.Map{
				"code":    "INTERNAL_SERVER_ERROR",
				"message": "Failed to add file to queue",
				"details": err.Error(),
			},
		})
	}

	// Convert to API response format
	response := ToQueueItemResponse(item)

	return c.Status(201).JSON(fiber.Map{
		"success": true,
		"data":    response,
	})
}

// handleRestartQueueBulk handles POST /api/queue/bulk/restart
func (s *Server) handleRestartQueueBulk(c *fiber.Ctx) error {
	// Parse request body
	var request struct {
		IDs []int64 `json:"ids"`
	}

	if err := c.BodyParser(&request); err != nil {
		return c.Status(400).JSON(fiber.Map{
			"success": false,
			"error": fiber.Map{
				"code":    "BAD_REQUEST",
				"message": "Invalid request body",
				"details": err.Error(),
			},
		})
	}

	// Validate IDs
	if len(request.IDs) == 0 {
		return c.Status(400).JSON(fiber.Map{
			"success": false,
			"error": fiber.Map{
				"code":    "BAD_REQUEST",
				"message": "No IDs provided",
				"details": "At least one ID is required",
			},
		})
	}

	// Check if any items are currently being processed
	processedCount := 0
	notFoundCount := 0
	for _, id := range request.IDs {
		item, err := s.queueRepo.GetQueueItem(id)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{
				"success": false,
				"error": fiber.Map{
					"code":    "INTERNAL_SERVER_ERROR",
					"message": "Failed to check queue item",
					"details": err.Error(),
				},
			})
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
		return c.Status(404).JSON(fiber.Map{
			"success": false,
			"error": fiber.Map{
				"code":    "NOT_FOUND",
				"message": "No queue items found",
				"details": "None of the provided IDs exist",
			},
		})
	}

	if processedCount > 0 {
		return c.Status(409).JSON(fiber.Map{
			"success": false,
			"error": fiber.Map{
				"code":    "CONFLICT",
				"message": "Cannot restart items currently being processed",
				"details": fmt.Sprintf("%d items are currently being processed", processedCount),
			},
		})
	}

	// Restart the queue items
	err := s.queueRepo.RestartQueueItemsBulk(request.IDs)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"error": fiber.Map{
				"code":    "INTERNAL_SERVER_ERROR",
				"message": "Failed to restart queue items",
				"details": err.Error(),
			},
		})
	}

	response := map[string]interface{}{
		"restarted_count": len(request.IDs) - notFoundCount,
		"message":         fmt.Sprintf("Successfully restarted %d queue items", len(request.IDs)-notFoundCount),
	}

	return c.Status(200).JSON(fiber.Map{
		"success": true,
		"data":    response,
	})
}
