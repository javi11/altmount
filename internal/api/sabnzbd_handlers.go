package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/altmount/internal/database"
)

var defaultCategory = config.SABnzbdCategory{
	Name:     "default",
	Order:    0,
	Priority: -100,
	Dir:      "",
}

const completeDir = "/sabnzbd"

// handleSABnzbd is the main handler for SABnzbd API endpoints
func (s *Server) handleSABnzbd(w http.ResponseWriter, r *http.Request) {
	// Check if SABnzbd API is enabled
	if s.configManager != nil {
		config := s.configManager.GetConfig()
		if config.SABnzbd.Enabled == nil || !*config.SABnzbd.Enabled {
			http.NotFound(w, r)
			return
		}
	}

	// Parse query parameters
	query := r.URL.Query()

	// Check for API key authentication
	apiKey := query.Get("apikey")
	if apiKey == "" {
		s.writeSABnzbdError(w, "API key required")
		return
	}

	// Validate API key using existing authentication system
	if !s.validateAPIKey(r, apiKey) {
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

	// Get and validate category from form first
	category := r.FormValue("cat")
	validatedCategory, err := s.validateSABnzbdCategory(category)
	if err != nil {
		s.writeSABnzbdError(w, err.Error())
		return
	}

	// Build category path and create temporary file with category subdirectory
	tempDir := os.TempDir()

	categoryPath := s.buildCategoryPath(validatedCategory)
	var tempFile string
	if categoryPath != "" {
		tempFile = filepath.Join(tempDir, completeDir, categoryPath, header.Filename)
		// Ensure category directory exists
		categoryDir := filepath.Join(tempDir, completeDir, categoryPath)
		if err := os.MkdirAll(categoryDir, 0755); err != nil {
			s.writeSABnzbdError(w, "Failed to create category directory")
			return
		}
	} else {
		tempFile = filepath.Join(tempDir, completeDir, header.Filename)

		// Ensure base directory exists
		if err := os.MkdirAll(filepath.Join(tempDir, completeDir), 0755); err != nil {
			s.writeSABnzbdError(w, "Failed to create base directory")
			return
		}
	}

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

	// Category validation was moved above file creation

	// Add the file to the processing queue using centralized method
	priority := s.parseSABnzbdPriority(r.FormValue("priority"))
	item, err := s.importerService.AddToQueue(tempFile, &tempDir, &validatedCategory, &priority)
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

	// Get and validate category from query parameters first
	category := query.Get("cat")
	validatedCategory, err := s.validateSABnzbdCategory(category)
	if err != nil {
		s.writeSABnzbdError(w, err.Error())
		return
	}

	// Create temporary file with category path
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

	// Build category path and create temporary file with category subdirectory
	categoryPath := s.buildCategoryPath(validatedCategory)
	var tempFile string
	if categoryPath != "" {
		tempFile = filepath.Join(tempDir, completeDir, categoryPath, filename)
		// Ensure category directory exists
		categoryDir := filepath.Join(tempDir, completeDir, categoryPath)
		if err := os.MkdirAll(categoryDir, 0755); err != nil {
			s.writeSABnzbdError(w, "Failed to create category directory")
			return
		}
	} else {
		tempFile = filepath.Join(tempDir, completeDir, filename)

		// Ensure base directory exists
		if err := os.MkdirAll(filepath.Join(tempDir, completeDir), 0755); err != nil {
			s.writeSABnzbdError(w, "Failed to create base directory")
			return
		}
	}

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

	// Category validation was moved above file creation

	// Add the file to the processing queue using centralized method
	priority := s.parseSABnzbdPriority(query.Get("priority"))
	item, err := s.importerService.AddToQueue(tempFile, &tempDir, &validatedCategory, &priority)
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
	slots := make([]SABnzbdQueueSlot, 0, len(items))
	for i, item := range items {
		slots = append(slots, ToSABnzbdQueueSlot(item, i))
	}

	response := SABnzbdQueueResponse{
		Status: true,
		Queue: SABnzbdQueueObject{
			Paused: false,
			Slots:  slots,
		},
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

	// Create the proper history response structure using the new struct
	response := SABnzbdCompleteHistoryResponse{
		History: SABnzbdHistoryObject{
			ActiveLang:      "en",
			Paused:          false,
			Session:         "1234567890abcdef0987654321fedcba",
			RestartReq:      false,
			PowerOptions:    true,
			Slots:           slots,
			Speed:           "0 ",
			HelpURI:         "http://wiki.sabnzbd.org/",
			Size:            "0 B",
			Uptime:          time.Since(s.startTime).String(),
			TotalSize:       "0 B",
			MonthSize:       "0 B",
			WeekSize:        "0 B",
			Version:         "4.5.0",
			NewRelURL:       "",
			DiskspaceTotal2: "74.43",
			ColorScheme:     "white",
			DiskspaceTotal1: "74.43",
			Nt:              runtime.GOOS == "windows",
			Status:          "Idle",
			LastWarning:     "",
			HaveWarnings:    "0",
			CacheArt:        "0",
			SizeLeft:        "0 B",
			FinishAction:    nil,
			PausedAll:       false,
			CacheSize:       "0 B",
			NewzbinURL:      "www.newzbin2.es",
			NewRelease:      "",
			PauseInt:        "0",
			MbLeft:          "0.00",
			Diskspace1:      "10.42",
			Darwin:          runtime.GOOS == "darwin",
			TimeLeft:        "0:00:00",
			Mb:              "0.00",
			NoOfSlots:       len(slots),
			DaySize:         "0 B",
			ETA:             "unknown",
			NzbQuota:        "",
			LoadAvg:         "",
			CacheMax:        "134217728",
			KbPerSec:        "0.00",
			SpeedLimit:      "",
			WebDir:          "",
			Diskspace2:      "10.42",
		},
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
	var config SABnzbdConfig

	if s.configManager != nil {
		cfg := s.configManager.GetConfig()

		completeDirPath := path.Join(cfg.SABnzbd.MountDir, completeDir)

		// Build misc configuration
		config.Misc = SABnzbdMiscConfig{
			CompleteDir:            completeDirPath,
			PreCheck:               0,
			HistoryRetention:       "",
			HistoryRetentionOption: "all",
			HistoryRetentionNumber: 1,
		}

		// Build categories from configuration
		if len(cfg.SABnzbd.Categories) > 0 {
			// Use configured categories
			for _, category := range cfg.SABnzbd.Categories {
				config.Categories = append(config.Categories, SABnzbdCategory{
					Name:     category.Name,
					Order:    category.Order,
					PP:       "3", // Default post-processing
					Script:   "None",
					Dir:      category.Dir,
					Newzbin:  "",
					Priority: category.Priority,
				})
			}
		} else {
			// Use default category when none configured
			config.Categories = []SABnzbdCategory{
				{
					Name:     "default",
					Order:    0,
					PP:       "3",
					Script:   "None",
					Dir:      "",
					Newzbin:  "",
					Priority: 0,
				},
			}
		}

		// Empty servers array (not exposing actual server configuration)
		config.Servers = []SABnzbdServer{}
	} else {
		// Fallback configuration when no config manager
		config = SABnzbdConfig{
			Misc: SABnzbdMiscConfig{
				CompleteDir:            "",
				PreCheck:               0,
				HistoryRetention:       "",
				HistoryRetentionOption: "all",
				HistoryRetentionNumber: 1,
			},
			Categories: []SABnzbdCategory{
				{
					Name:     "default",
					Order:    0,
					PP:       "3",
					Script:   "None",
					Dir:      "",
					Newzbin:  "",
					Priority: 0,
				},
			},
			Servers: []SABnzbdServer{},
		}
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

// buildCategoryPath builds the directory path for a category
func (s *Server) buildCategoryPath(category string) string {
	// Return empty for default category (no subdirectory)
	if category == "default" || category == "" {
		return ""
	}

	if s.configManager == nil {
		// No config manager, use category name as directory
		return category
	}

	config := s.configManager.GetConfig()

	// If no categories are configured, use category name as directory
	if len(config.SABnzbd.Categories) == 0 {
		return category
	}

	// Look for the category in configuration
	for _, configCategory := range config.SABnzbd.Categories {
		if configCategory.Name == category {
			// Use configured Dir if available, otherwise use category name
			if configCategory.Dir != "" {
				return configCategory.Dir
			}
			return category
		}
	}

	// Category not found in configuration, use category name as directory
	return category
}

// validateSABnzbdCategory validates and returns the category, or error if invalid
func (s *Server) validateSABnzbdCategory(category string) (string, error) {
	if category == "" {
		return defaultCategory.Name, nil
	}

	config := s.configManager.GetConfig()

	// If no categories are configured, allow any category and default to "default"
	if len(config.SABnzbd.Categories) == 0 {
		if category == "" {
			return defaultCategory.Name, nil
		}
		return category, nil
	}

	// If categories are configured, validate against the list
	if category == "" {
		category = defaultCategory.Name
	}

	// Check if category exists in configuration
	for _, configCategory := range config.SABnzbd.Categories {
		if configCategory.Name == category {
			return category, nil
		}
	}

	// Category not found in configuration
	return "", fmt.Errorf("invalid category '%s' - not found in configuration", category)
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
