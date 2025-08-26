package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/altmount/internal/importer"
)

// handleStartManualScan handles POST /import/scan
func (s *Server) handleStartManualScan(w http.ResponseWriter, r *http.Request) {
	// Check if importer service is available
	if s.importerService == nil {
		WriteInternalError(w, "Importer service not available", "")
		return
	}

	// Parse request body
	var req ManualScanRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteBadRequest(w, "Invalid request body", err.Error())
		return
	}

	// Validate request
	if req.Path == "" {
		WriteValidationError(w, "Path is required", "")
		return
	}

	// Start manual scan
	if err := s.importerService.StartManualScan(req.Path); err != nil {
		WriteConflict(w, "Failed to start scan", err.Error())
		return
	}

	// Return current scan status
	scanInfo := s.importerService.GetScanStatus()
	response := toScanStatusResponse(scanInfo)
	WriteSuccess(w, response, nil)
}

// handleGetScanStatus handles GET /import/scan/status
func (s *Server) handleGetScanStatus(w http.ResponseWriter, r *http.Request) {
	// Check if importer service is available
	if s.importerService == nil {
		WriteInternalError(w, "Importer service not available", "")
		return
	}

	// Get current scan status
	scanInfo := s.importerService.GetScanStatus()
	response := toScanStatusResponse(scanInfo)
	WriteSuccess(w, response, nil)
}

// handleCancelScan handles DELETE /import/scan
func (s *Server) handleCancelScan(w http.ResponseWriter, r *http.Request) {
	// Check if importer service is available
	if s.importerService == nil {
		WriteInternalError(w, "Importer service not available", "")
		return
	}

	// Cancel the scan
	if err := s.importerService.CancelScan(); err != nil {
		WriteConflict(w, "Failed to cancel scan", err.Error())
		return
	}

	// Return updated scan status
	scanInfo := s.importerService.GetScanStatus()
	response := toScanStatusResponse(scanInfo)
	WriteSuccess(w, response, nil)
}

// handleManualImportFile handles POST /import/file
func (s *Server) handleManualImportFile(w http.ResponseWriter, r *http.Request) {
	// Check for API key authentication
	apiKey := r.URL.Query().Get("apikey")
	if apiKey == "" {
		WriteUnauthorized(w, "API key required", "Please provide an API key via 'apikey' query parameter")
		return
	}

	// Validate API key using the refactored validation function
	if !s.validateAPIKey(r, apiKey) {
		WriteUnauthorized(w, "Invalid API key", "The provided API key is not valid")
		return
	}

	// Check if importer service is available
	if s.importerService == nil {
		WriteInternalError(w, "Importer service not available", "")
		return
	}

	// Parse request body
	var req ManualImportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteBadRequest(w, "Invalid request body", err.Error())
		return
	}

	// Validate request
	if req.FilePath == "" {
		WriteValidationError(w, "File path is required", "")
		return
	}

	// Check if file exists and is accessible
	fileInfo, err := os.Stat(req.FilePath)
	if err != nil {
		if os.IsNotExist(err) {
			WriteValidationError(w, "File not found", fmt.Sprintf("File does not exist: %s", req.FilePath))
		} else {
			WriteValidationError(w, "Cannot access file", err.Error())
		}
		return
	}

	// Check if it's a regular file (not directory)
	if fileInfo.IsDir() {
		WriteValidationError(w, "Path is a directory", "Expected a file, not a directory")
		return
	}

	// Check if file is already in queue
	inQueue, err := s.queueRepo.IsFileInQueue(req.FilePath)
	if err != nil {
		WriteInternalError(w, "Failed to check queue status", err.Error())
		return
	}

	if inQueue {
		WriteConflict(w, "File already in queue", fmt.Sprintf("File %s is already queued for processing", req.FilePath))
		return
	}

	// Add the file to the processing queue
	item := &database.ImportQueueItem{
		NzbPath:    req.FilePath,
		Priority:   database.QueuePriorityNormal,
		Status:     database.QueueStatusPending,
		RetryCount: 0,
		MaxRetries: 3,
		CreatedAt:  time.Now(),
	}

	err = s.queueRepo.AddToQueue(item)
	if err != nil {
		WriteInternalError(w, "Failed to add file to queue", err.Error())
		return
	}

	// Return success response
	response := ManualImportResponse{
		QueueID: item.ID,
		Message: fmt.Sprintf("File successfully added to import queue with ID %d", item.ID),
	}

	WriteSuccess(w, response, nil)
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
