package api

import (
	"encoding/json"
	"net/http"

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