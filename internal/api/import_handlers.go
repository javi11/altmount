package api

import (
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/altmount/internal/importer"
)

// handleStartManualScan handles POST /import/scan
func (s *Server) handleStartManualScan(c *fiber.Ctx) error {
	// Check if importer service is available
	if s.importerService == nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Importer service not available",
		})
	}

	// Parse request body
	var req ManualScanRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{
			"success": false,
			"message": "Invalid request body",
			"details": err.Error(),
		})
	}

	// Validate request
	if req.Path == "" {
		return c.Status(422).JSON(fiber.Map{
			"success": false,
			"message": "Path is required",
		})
	}

	// Start manual scan
	if err := s.importerService.StartManualScan(req.Path); err != nil {
		return c.Status(409).JSON(fiber.Map{
			"success": false,
			"message": "Failed to start scan",
			"details": err.Error(),
		})
	}

	// Return current scan status
	scanInfo := s.importerService.GetScanStatus()
	response := toScanStatusResponse(scanInfo)
	return c.Status(200).JSON(fiber.Map{
		"success": true,
		"data":    response,
	})
}

// handleGetScanStatus handles GET /import/scan/status
func (s *Server) handleGetScanStatus(c *fiber.Ctx) error {
	// Check if importer service is available
	if s.importerService == nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Importer service not available",
		})
	}

	// Get current scan status
	scanInfo := s.importerService.GetScanStatus()
	response := toScanStatusResponse(scanInfo)
	return c.Status(200).JSON(fiber.Map{
		"success": true,
		"data":    response,
	})
}

// handleCancelScan handles DELETE /import/scan
func (s *Server) handleCancelScan(c *fiber.Ctx) error {
	// Check if importer service is available
	if s.importerService == nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Importer service not available",
		})
	}

	// Cancel the scan
	if err := s.importerService.CancelScan(); err != nil {
		return c.Status(409).JSON(fiber.Map{
			"success": false,
			"message": "Failed to cancel scan",
			"details": err.Error(),
		})
	}

	// Return updated scan status
	scanInfo := s.importerService.GetScanStatus()
	response := toScanStatusResponse(scanInfo)
	return c.Status(200).JSON(fiber.Map{
		"success": true,
		"data":    response,
	})
}

// handleManualImportFile handles POST /import/file
func (s *Server) handleManualImportFile(c *fiber.Ctx) error {
	// Check for API key authentication
	apiKey := c.Query("apikey")
	if apiKey == "" {
		return c.Status(401).JSON(fiber.Map{
			"success": false,
			"message": "API key required",
			"details": "Please provide an API key via 'apikey' query parameter",
		})
	}

	// Validate API key using the refactored validation function
	if !s.validateAPIKey(c, apiKey) {
		return c.Status(401).JSON(fiber.Map{
			"success": false,
			"message": "Invalid API key",
			"details": "The provided API key is not valid",
		})
	}

	// Check if importer service is available
	if s.importerService == nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Importer service not available",
		})
	}

	// Parse request body
	var req ManualImportRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{
			"success": false,
			"message": "Invalid request body",
			"details": err.Error(),
		})
	}

	// Validate request
	if req.FilePath == "" {
		return c.Status(422).JSON(fiber.Map{
			"success": false,
			"message": "File path is required",
		})
	}

	// Check if file exists and is accessible
	fileInfo, err := os.Stat(req.FilePath)
	if err != nil {
		if os.IsNotExist(err) {
			return c.Status(422).JSON(fiber.Map{
				"success": false,
				"message": "File not found",
				"details": fmt.Sprintf("File does not exist: %s", req.FilePath),
			})
		} else {
			return c.Status(422).JSON(fiber.Map{
				"success": false,
				"message": "Cannot access file",
				"details": err.Error(),
			})
		}
	}

	// Check if it's a regular file (not directory)
	if fileInfo.IsDir() {
		return c.Status(422).JSON(fiber.Map{
			"success": false,
			"message": "Path is a directory",
			"details": "Expected a file, not a directory",
		})
	}

	// Check if file is already in queue
	inQueue, err := s.queueRepo.IsFileInQueue(c.Context(), req.FilePath)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Failed to check queue status",
			"details": err.Error(),
		})
	}

	if inQueue {
		return c.Status(409).JSON(fiber.Map{
			"success": false,
			"message": "File already in queue",
			"details": fmt.Sprintf("File %s is already queued for processing", req.FilePath),
		})
	}

	// Add the file to the processing queue
	item := &database.ImportQueueItem{
		NzbPath:      req.FilePath,
		Priority:     database.QueuePriorityNormal,
		Status:       database.QueueStatusPending,
		RetryCount:   0,
		MaxRetries:   3,
		CreatedAt:    time.Now(),
		RelativePath: req.RelativePath,
	}

	slog.DebugContext(c.Context(), "Adding file to queue", "file", req.FilePath, "relative_path", req.RelativePath)

	err = s.queueRepo.AddToQueue(c.Context(), item)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Failed to add file to queue",
			"details": err.Error(),
		})
	}

	slog.DebugContext(c.Context(), "File added to queue", "file", req.FilePath, "queue_id", item.ID)

	// Return success response
	response := ManualImportResponse{
		QueueID: item.ID,
		Message: fmt.Sprintf("File successfully added to import queue with ID %d", item.ID),
	}

	return c.Status(200).JSON(fiber.Map{
		"success": true,
		"data":    response,
	})
}

// handleGetImportHistory handles GET /api/import/history
func (s *Server) handleGetImportHistory(c *fiber.Ctx) error {
	limit := 50
	if l := c.Query("limit"); l != "" {
		if _, err := fmt.Sscanf(l, "%d", &limit); err != nil {
			limit = 50
		}
	}

	history, err := s.queueRepo.ListImportHistory(c.Context(), limit, 0, "", "")
	if err != nil {
		return RespondInternalError(c, "Failed to list import history", err.Error())
	}

	response := make([]*ImportHistoryResponse, len(history))
	for i, h := range history {
		response[i] = ToImportHistoryResponse(h)
	}

	return RespondSuccess(c, response)
}

// toScanStatusResponse converts importer.ScanInfo to ScanStatusResponse
func toScanStatusResponse(scanInfo importer.ScanInfo) *ScanStatusResponse {
	return &ScanStatusResponse{
		Status:      string(scanInfo.Status),
		Path:        scanInfo.Path,
		StartTime:   scanInfo.StartTime,
		FilesFound:  scanInfo.FilesFound,
		FilesAdded:  scanInfo.FilesAdded,
		CurrentFile: scanInfo.CurrentFile,
		LastError:   scanInfo.LastError,
	}
}

// handleClearImportHistory handles DELETE /api/import/history
func (s *Server) handleClearImportHistory(c *fiber.Ctx) error {
	if err := s.queueRepo.ClearImportHistory(c.Context()); err != nil {
		return RespondInternalError(c, "Failed to clear import history", err.Error())
	}

	return RespondSuccess(c, fiber.Map{
		"message": "Import history cleared successfully",
	})
}
