package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/javi11/altmount/internal/database"
)

// handleSABnzbd is the main handler for SABnzbd API endpoints
func (s *Server) handleSABnzbd(w http.ResponseWriter, r *http.Request) {
	// Parse query parameters
	query := r.URL.Query()

	// Check for API key authentication
	apiKey := query.Get("apikey")
	if apiKey == "" {
		s.writeSABnzbdError(w, "API key required")
		return
	}

	// Validate API key using existing authentication system
	if !s.validateSABnzbdAPIKey(r, apiKey) {
		s.writeSABnzbdError(w, "Invalid API key")
		return
	}

	// Get mode parameter to determine which API method to call
	mode := query.Get("mode")
	switch mode {
	case "addfile":
		s.handleSABnzbdAddFile(w, r)
	case "addurl":
		s.handleSABnzbdAddUrl(w, r)
	case "queue":
		s.handleSABnzbdQueue(w, r)
	case "history":
		s.handleSABnzbdHistory(w, r)
	case "status":
		s.handleSABnzbdStatus(w, r)
	case "get_config":
		s.handleSABnzbdGetConfig(w, r)
	case "version":
		s.handleSABnzbdVersion(w, r)
	default:
		s.writeSABnzbdError(w, fmt.Sprintf("Unknown mode: %s", mode))
	}
}

// validateSABnzbdAPIKey validates the API key using AltMount's existing authentication
func (s *Server) validateSABnzbdAPIKey(r *http.Request, apiKey string) bool {
	if s.userRepo == nil {
		return false
	}

	user, err := s.userRepo.GetUserByAPIKey(apiKey)
	if err != nil || user == nil {
		return false
	}

	return true
}

// handleSABnzbdAddFile handles file upload for NZB files
func (s *Server) handleSABnzbdAddFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeSABnzbdError(w, "Method not allowed")
		return
	}

	// Parse multipart form
	err := r.ParseMultipartForm(10 << 20) // 10MB max
	if err != nil {
		s.writeSABnzbdError(w, "Failed to parse form data")
		return
	}

	// Get uploaded file
	file, header, err := r.FormFile("nzbfile")
	if err != nil {
		// Try alternative field name
		file, header, err = r.FormFile("name")
		if err != nil {
			s.writeSABnzbdError(w, "No NZB file provided")
			return
		}
	}
	defer file.Close()

	// Validate file extension
	if !strings.HasSuffix(strings.ToLower(header.Filename), ".nzb") {
		s.writeSABnzbdError(w, "Invalid file type, must be .nzb")
		return
	}

	// Create temporary file
	tempDir := os.TempDir()
	tempFile := filepath.Join(tempDir, header.Filename)

	outFile, err := os.Create(tempFile)
	if err != nil {
		s.writeSABnzbdError(w, "Failed to create temporary file")
		return
	}
	defer outFile.Close()

	// Copy uploaded file to temporary location
	_, err = io.Copy(outFile, file)
	if err != nil {
		s.writeSABnzbdError(w, "Failed to save file")
		return
	}

	// Add to queue using importer service
	if s.importerService == nil {
		s.writeSABnzbdError(w, "Importer service not available")
		return
	}

	// Get category from form
	category := r.FormValue("cat")
	if category == "" {
		category = "default"
	}

	// Add the file to the processing queue
	item := &database.ImportQueueItem{
		NzbPath:    tempFile,
		Category:   &category,
		Priority:   s.parseSABnzbdPriority(r.FormValue("priority")),
		Status:     database.QueueStatusPending,
		RetryCount: 0,
		MaxRetries: 3,
		CreatedAt:  time.Now(),
	}

	err = s.queueRepo.AddToQueue(item)
	if err != nil {
		s.writeSABnzbdError(w, "Failed to add to queue")
		return
	}

	// Return success response
	response := SABnzbdAddResponse{
		Status: true,
		NzoIds: []string{fmt.Sprintf("%d", item.ID)},
	}

	s.writeSABnzbdResponse(w, response)
}

// handleSABnzbdAddUrl handles adding NZB from URL
func (s *Server) handleSABnzbdAddUrl(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	nzbUrl := query.Get("name")

	if nzbUrl == "" {
		s.writeSABnzbdError(w, "URL parameter 'name' required")
		return
	}

	// Download NZB file from URL
	resp, err := http.Get(nzbUrl)
	if err != nil {
		s.writeSABnzbdError(w, "Failed to download NZB from URL")
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		s.writeSABnzbdError(w, fmt.Sprintf("Failed to download NZB: HTTP %d", resp.StatusCode))
		return
	}

	// Create temporary file
	tempDir := os.TempDir()

	// Extract filename from URL or use default
	filename := "downloaded.nzb"
	if u, err := url.Parse(nzbUrl); err == nil && u.Path != "" {
		if base := filepath.Base(u.Path); base != "" && base != "." {
			filename = base
		}
	}

	// Ensure .nzb extension
	if !strings.HasSuffix(strings.ToLower(filename), ".nzb") {
		filename += ".nzb"
	}

	tempFile := filepath.Join(tempDir, filename)

	outFile, err := os.Create(tempFile)
	if err != nil {
		s.writeSABnzbdError(w, "Failed to create temporary file")
		return
	}
	defer outFile.Close()

	// Copy downloaded content to file
	_, err = io.Copy(outFile, resp.Body)
	if err != nil {
		s.writeSABnzbdError(w, "Failed to save downloaded file")
		return
	}

	// Add to queue
	if s.importerService == nil {
		s.writeSABnzbdError(w, "Importer service not available")
		return
	}

	// Get category from query parameters
	category := query.Get("cat")
	if category == "" {
		category = "default"
	}

	item := &database.ImportQueueItem{
		NzbPath:    tempFile,
		Category:   &category,
		Priority:   s.parseSABnzbdPriority(query.Get("priority")),
		Status:     database.QueueStatusPending,
		RetryCount: 0,
		MaxRetries: 3,
		CreatedAt:  time.Now(),
	}

	err = s.queueRepo.AddToQueue(item)
	if err != nil {
		s.writeSABnzbdError(w, "Failed to add to queue")
		return
	}

	// Return success response
	response := SABnzbdAddResponse{
		Status: true,
		NzoIds: []string{fmt.Sprintf("%d", item.ID)},
	}

	s.writeSABnzbdResponse(w, response)
}

// handleSABnzbdQueue handles queue operations
func (s *Server) handleSABnzbdQueue(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()

	// Check for delete operation
	if query.Get("name") == "delete" {
		s.handleSABnzbdQueueDelete(w, r)
		return
	}

	// Get queue items
	if s.importerService == nil {
		s.writeSABnzbdError(w, "Importer service not available")
		return
	}

	// Get pending and processing items
	items, err := s.queueRepo.ListQueueItems(nil, "", 100, 0)
	if err != nil {
		s.writeSABnzbdError(w, "Failed to get queue")
		return
	}

	// Convert to SABnzbd format
	var slots []SABnzbdQueueSlot
	for i, item := range items {
		if item.Status == database.QueueStatusPending || item.Status == database.QueueStatusProcessing || item.Status == database.QueueStatusRetrying {
			slots = append(slots, ToSABnzbdQueueSlot(item, i))
		}
	}

	response := SABnzbdQueueResponse{
		Status:          true,
		Version:         "4.5.0", // Emulate SABnzbd version
		Paused:          false,
		PauseInt:        0,
		SizeLeft:        "0 B",
		Size:            "0 B",
		Speed:           "0 B/s",
		SpeedLimit:      "",
		SpeedLimitAbs:   "0",
		NoOfSlots:       len(slots),
		NoOfSlotsTotal:  len(slots),
		KbPerSec:        "0",
		MbLeft:          0,
		Mb:              0,
		TimeLeft:        "0:00:00",
		ETA:             "unknown",
		Slots:           slots,
		Diskspace1:      "0 B",
		Diskspace2:      "0 B",
		DiskspaceTotal1: "0 B",
		DiskspaceTotal2: "0 B",
	}

	s.writeSABnzbdResponse(w, response)
}

// handleSABnzbdQueueDelete handles deleting items from queue
func (s *Server) handleSABnzbdQueueDelete(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	nzoID := query.Get("value")

	if nzoID == "" {
		s.writeSABnzbdError(w, "Missing nzo_id parameter")
		return
	}

	// Convert nzo_id to database ID
	id, err := strconv.ParseInt(nzoID, 10, 64)
	if err != nil {
		s.writeSABnzbdError(w, "Invalid nzo_id")
		return
	}

	if s.importerService == nil {
		s.writeSABnzbdError(w, "Importer service not available")
		return
	}

	// Delete from queue
	err = s.queueRepo.RemoveFromQueue(id)
	if err != nil {
		s.writeSABnzbdError(w, "Failed to delete queue item")
		return
	}

	response := SABnzbdDeleteResponse{
		Status: true,
	}

	s.writeSABnzbdResponse(w, response)
}

// handleSABnzbdHistory handles history operations
func (s *Server) handleSABnzbdHistory(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()

	// Check for delete operation
	if query.Get("name") == "delete" {
		s.handleSABnzbdHistoryDelete(w, r)
		return
	}

	// Get completed and failed items
	if s.importerService == nil {
		s.writeSABnzbdError(w, "Importer service not available")
		return
	}

	// Get completed items
	completedStatus := database.QueueStatusCompleted
	completed, err := s.queueRepo.ListQueueItems(&completedStatus, "", 50, 0)
	if err != nil {
		s.writeSABnzbdError(w, "Failed to get completed items")
		return
	}

	// Get failed items
	failedStatus := database.QueueStatusFailed
	failed, err := s.queueRepo.ListQueueItems(&failedStatus, "", 50, 0)
	if err != nil {
		s.writeSABnzbdError(w, "Failed to get failed items")
		return
	}

	// Combine and convert to SABnzbd format
	var slots []SABnzbdHistorySlot
	index := 0
	for _, item := range completed {
		slots = append(slots, ToSABnzbdHistorySlot(item, index))
		index++
	}
	for _, item := range failed {
		slots = append(slots, ToSABnzbdHistorySlot(item, index))
		index++
	}

	response := SABnzbdHistoryResponse{
		Status:    true,
		Version:   "4.5.0",
		Paused:    false,
		NoOfSlots: len(slots),
		Slots:     slots,
		TotalSize: "0 B",
		MonthSize: "0 B",
		WeekSize:  "0 B",
		DaySize:   "0 B",
	}

	s.writeSABnzbdResponse(w, response)
}

// handleSABnzbdHistoryDelete handles deleting items from history
func (s *Server) handleSABnzbdHistoryDelete(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	nzoID := query.Get("value")

	if nzoID == "" {
		s.writeSABnzbdError(w, "Missing nzo_id parameter")
		return
	}

	// Convert nzo_id to database ID
	id, err := strconv.ParseInt(nzoID, 10, 64)
	if err != nil {
		s.writeSABnzbdError(w, "Invalid nzo_id")
		return
	}

	if s.importerService == nil {
		s.writeSABnzbdError(w, "Importer service not available")
		return
	}

	// Delete from queue (history items are still queue items with completed/failed status)
	err = s.queueRepo.RemoveFromQueue(id)
	if err != nil {
		s.writeSABnzbdError(w, "Failed to delete history item")
		return
	}

	response := SABnzbdDeleteResponse{
		Status: true,
	}

	s.writeSABnzbdResponse(w, response)
}

// handleSABnzbdStatus handles full status request
func (s *Server) handleSABnzbdStatus(w http.ResponseWriter, r *http.Request) {
	// Get queue information
	var slots []SABnzbdQueueSlot
	if s.queueRepo != nil {
		items, err := s.queueRepo.ListQueueItems(nil, "", 50, 0)
		if err == nil {
			for i, item := range items {
				if item.Status == database.QueueStatusPending || item.Status == database.QueueStatusProcessing || item.Status == database.QueueStatusRetrying {
					slots = append(slots, ToSABnzbdQueueSlot(item, i))
				}
			}
		}
	}

	response := SABnzbdStatusResponse{
		Status:          true,
		Version:         "4.5.0",
		Uptime:          time.Since(s.startTime).String(),
		Color:           "green",
		Darwin:          runtime.GOOS == "darwin",
		Nt:              runtime.GOOS == "windows",
		Pid:             os.Getpid(),
		NewRelURL:       "",
		ActiveDownload:  len(slots) > 0,
		Paused:          false,
		PauseInt:        0,
		Remaining:       "0 B",
		MbLeft:          0,
		Diskspace1:      "0 B",
		Diskspace2:      "0 B",
		DiskspaceTotal1: "0 B",
		DiskspaceTotal2: "0 B",
		Loadavg:         "0.0",
		Cache: struct {
			Max  int `json:"max"`
			Left int `json:"left"`
			Art  int `json:"art"`
		}{
			Max:  100,
			Left: 100,
			Art:  0,
		},
		Folders: []string{},
		Slots:   slots,
	}

	s.writeSABnzbdResponse(w, response)
}

// handleSABnzbdGetConfig handles configuration request
func (s *Server) handleSABnzbdGetConfig(w http.ResponseWriter, r *http.Request) {
	// Return minimal configuration compatible with SABnzbd
	config := map[string]interface{}{
		"complete_dir": "/downloads/complete",
		"download_dir": "/downloads/incomplete",
		"categories": map[string]interface{}{
			"*": map[string]interface{}{
				"name":     "*",
				"order":    0,
				"pp":       "",
				"script":   "Default",
				"dir":      "",
				"newzbin":  "",
				"priority": -100,
			},
			"default": map[string]interface{}{
				"name":     "default",
				"order":    1,
				"pp":       "",
				"script":   "",
				"dir":      "",
				"newzbin":  "",
				"priority": 0,
			},
		},
	}

	response := SABnzbdConfigResponse{
		Status:  true,
		Version: "4.5.0",
		Config:  config,
	}

	s.writeSABnzbdResponse(w, response)
}

// handleSABnzbdVersion handles version request
func (s *Server) handleSABnzbdVersion(w http.ResponseWriter, r *http.Request) {
	response := SABnzbdVersionResponse{
		Status:  true,
		Version: "4.5.0",
	}

	s.writeSABnzbdResponse(w, response)
}

// parseSABnzbdPriority converts SABnzbd priority string to AltMount priority
func (s *Server) parseSABnzbdPriority(priority string) database.QueuePriority {
	switch strings.ToLower(priority) {
	case "high", "2":
		return database.QueuePriorityHigh
	case "low", "0":
		return database.QueuePriorityLow
	default:
		return database.QueuePriorityNormal
	}
}

// writeSABnzbdResponse writes a successful SABnzbd-compatible response
func (s *Server) writeSABnzbdResponse(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	if err := json.NewEncoder(w).Encode(data); err != nil {
		s.logger.Error("Failed to encode SABnzbd response", "error", err)
	}
}

// writeSABnzbdError writes a SABnzbd-compatible error response
func (s *Server) writeSABnzbdError(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK) // SABnzbd returns 200 even for errors

	response := SABnzbdResponse{
		Status: false,
		Error:  &message,
	}

	if err := json.NewEncoder(w).Encode(response); err != nil {
		s.logger.Error("Failed to encode SABnzbd error response", "error", err)
	}
}
