package api

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/javi11/altmount/internal/arrs"
	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/altmount/internal/pathutil"
)

// getDefaultCategory returns the Default category from config or a fallback
func (s *Server) getDefaultCategory() config.SABnzbdCategory {
	if s.configManager != nil {
		cfg := s.configManager.GetConfig()
		for _, cat := range cfg.SABnzbd.Categories {
			if cat.Name == config.DefaultCategoryName {
				return cat
			}
		}
	}
	// Fallback if not found in config
	return config.SABnzbdCategory{
		Name:     config.DefaultCategoryName,
		Order:    0,
		Priority: 0,
		Dir:      config.DefaultCategoryDir,
	}
}

// handleSABnzbd is the main handler for SABnzbd API endpoints
func (s *Server) handleSABnzbd(c *fiber.Ctx) error {
	// Check if SABnzbd API is enabled
	if s.configManager != nil {
		config := s.configManager.GetConfig()
		if config.SABnzbd.Enabled == nil || !*config.SABnzbd.Enabled {
			return c.Status(404).SendString("Not Found")
		}
	}

	// Extract authentication parameters
	apiKey := c.Query("apikey")
	maUsername := c.Query("ma_username") // ARR URL
	maPassword := c.Query("ma_password") // ARR API key

	// Determine authentication method
	authenticated := false

	// Method 1: Traditional API key authentication
	if apiKey != "" {
		if s.validateAPIKey(c, apiKey) {
			authenticated = true
			// Still try auto-registration if ARR credentials provided
			if maUsername != "" && maPassword != "" {
				if s.arrsService == nil {
					return s.writeSABnzbdErrorFiber(c, "Radarr/Sonarr Management is disabled")
				}
				s.tryAutoRegisterARR(c)
			}
		}
	}

	// Method 2: ARR credentials authentication
	if !authenticated && maUsername != "" && maPassword != "" {
		if s.validateARRCredentials(c, maUsername, maPassword) {
			authenticated = true
		}
	}

	// Check if authenticated by either method
	if !authenticated {
		if apiKey != "" {
			return s.writeSABnzbdErrorFiber(c, "Invalid API key")
		}

		if maUsername != "" && s.arrsService == nil {
			return s.writeSABnzbdErrorFiber(c, "Radarr/Sonarr Management is disabled")
		}

		return s.writeSABnzbdErrorFiber(c, "Authentication required: provide either apikey or ma_username+ma_password")
	}

	// Get mode parameter to determine which API method to call
	mode := c.Query("mode")
	switch mode {
	case "addfile":
		return s.handleSABnzbdAddFile(c)
	case "addurl":
		return s.handleSABnzbdAddUrl(c)
	case "queue":
		return s.handleSABnzbdQueue(c)
	case "pause":
		return s.handleSABnzbdPause(c)
	case "resume":
		return s.handleSABnzbdResume(c)
	case "switch":
		return s.handleSABnzbdSwitch(c)
	case "history":
		return s.handleSABnzbdHistory(c)
	case "status":
		return s.handleSABnzbdStatus(c)
	case "get_config":
		return s.handleSABnzbdGetConfig(c)
	case "version":
		return s.handleSABnzbdVersion(c)
	default:
		return s.writeSABnzbdErrorFiber(c, fmt.Sprintf("Unknown mode: %s", mode))
	}
}

// handleSABnzbdPause handles global pause
func (s *Server) handleSABnzbdPause(c *fiber.Ctx) error {
	if s.importerService == nil {
		return s.writeSABnzbdErrorFiber(c, "Importer service not available")
	}
	s.importerService.Pause()
	return s.writeSABnzbdResponseFiber(c, SABnzbdResponse{Status: true})
}

// handleSABnzbdResume handles global resume
func (s *Server) handleSABnzbdResume(c *fiber.Ctx) error {
	if s.importerService == nil {
		return s.writeSABnzbdErrorFiber(c, "Importer service not available")
	}
	s.importerService.Resume()
	return s.writeSABnzbdResponseFiber(c, SABnzbdResponse{Status: true})
}

// handleSABnzbdSwitch handles priority switching
func (s *Server) handleSABnzbdSwitch(c *fiber.Ctx) error {
	value := c.Query("value")
	value2 := c.Query("value2") // Priority (0, 1, 2)

	if value == "" || value2 == "" {
		return s.writeSABnzbdErrorFiber(c, "Missing parameters")
	}

	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return s.writeSABnzbdErrorFiber(c, "Invalid ID")
	}

	priority := s.parseSABnzbdPriority(value2)

	if err := s.queueRepo.UpdateQueueItemPriority(c.Context(), id, priority); err != nil {
		return s.writeSABnzbdErrorFiber(c, "Failed to update priority")
	}

	return s.writeSABnzbdResponseFiber(c, SABnzbdResponse{Status: true})
}

// handleSABnzbdQueuePause handles pausing/resuming a queue item
func (s *Server) handleSABnzbdQueuePause(c *fiber.Ctx, pause bool) error {
	value := c.Query("value")
	if value == "" {
		return s.writeSABnzbdErrorFiber(c, "Missing value parameter")
	}
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return s.writeSABnzbdErrorFiber(c, "Invalid value parameter")
	}

	item, err := s.queueRepo.GetQueueItem(c.Context(), id)
	if err != nil {
		return s.writeSABnzbdErrorFiber(c, "Failed to get queue item")
	}
	if item == nil {
		return s.writeSABnzbdErrorFiber(c, "Queue item not found")
	}

	if pause {
		if item.Status == database.QueueStatusPending {
			if err := s.queueRepo.UpdateQueueItemStatus(c.Context(), id, database.QueueStatusPaused, nil); err != nil {
				return s.writeSABnzbdErrorFiber(c, "Failed to pause item")
			}
		}
	} else {
		if item.Status == database.QueueStatusPaused {
			if err := s.queueRepo.UpdateQueueItemStatus(c.Context(), id, database.QueueStatusPending, nil); err != nil {
				return s.writeSABnzbdErrorFiber(c, "Failed to resume item")
			}
		}
	}

	return s.writeSABnzbdResponseFiber(c, SABnzbdResponse{Status: true})
}

// tryAutoRegisterARR attempts to auto-register an ARR instance from SABnzbd request parameters
// It extracts ma_username (ARR URL) and ma_password (ARR API key) from the query parameters
// This method logs errors but does not fail the SABnzbd request if registration fails
func (s *Server) tryAutoRegisterARR(c *fiber.Ctx) {
	// Check if arrsService is available
	if s.arrsService == nil {
		return
	}

	// Extract ma_username (ARR URL) and ma_password (ARR API key)
	maUsername := c.Query("ma_username")
	maPassword := c.Query("ma_password")

	// Both parameters must be present
	if maUsername == "" || maPassword == "" {
		return
	}

	// URL decode the username parameter (contains ARR URL)
	arrURL, err := url.QueryUnescape(maUsername)
	if err != nil {
		slog.ErrorContext(c.Context(), "Failed to decode ma_username parameter", "error", err, "raw_value", maUsername)
	}

	arrAPIKey := maPassword

	slog.DebugContext(c.Context(), "Attempting ARR auto-registration from SABnzbd request",
		"arr_url", arrURL)

	// Attempt to register the instance (category is auto-assigned based on ARR type)
	if err := s.arrsService.RegisterInstance(c.Context(), arrURL, arrAPIKey); err != nil {
		slog.ErrorContext(c.Context(), "Failed to auto-register ARR instance",
			"arr_url", arrURL,
			"error", err)
		return
	}

	slog.InfoContext(c.Context(), "Successfully auto-registered ARR instance", "arr_url", arrURL)
}

// validateARRCredentials validates ARR credentials and auto-registers if needed
// Returns true if credentials are valid (either already registered or newly registered)
func (s *Server) validateARRCredentials(c *fiber.Ctx, maUsername, maPassword string) bool {
	if s.arrsService == nil {
		slog.ErrorContext(c.Context(), "ARR service not available for credential validation")
		return false
	}

	// URL decode the username parameter (contains ARR URL)
	arrURL, err := url.QueryUnescape(maUsername)
	if err != nil {
		slog.ErrorContext(c.Context(), "Failed to decode ma_username parameter", "error", err, "raw_value", maUsername)
		return false
	}

	arrAPIKey := maPassword

	// Step 1: Check if instance exists and credentials match
	if instance := s.findARRInstanceByURL(arrURL); instance != nil {
		// Instance exists, verify credentials match
		if instance.APIKey == arrAPIKey {
			return true
		}

		slog.ErrorContext(c.Context(), "ARR credentials do not match registered instance", "arr_url", arrURL)
		return false
	}

	// Step 2: Instance doesn't exist, try to register it
	slog.DebugContext(c.Context(), "ARR instance not found, attempting auto-registration", "arr_url", arrURL)

	if err := s.arrsService.RegisterInstance(c.Context(), arrURL, arrAPIKey); err != nil {
		slog.ErrorContext(c.Context(), "Failed to auto-register ARR instance",
			"arr_url", arrURL,
			"error", err)

		return false
	}

	slog.InfoContext(c.Context(), "Successfully auto-registered and validated ARR instance", "arr_url", arrURL)
	return true
}

// handleSABnzbdAddFile handles file upload for NZB files
func (s *Server) handleSABnzbdAddFile(c *fiber.Ctx) error {
	if c.Method() != "POST" {
		return s.writeSABnzbdErrorFiber(c, "Method not allowed")
	}

	// Get uploaded file
	file, err := c.FormFile("nzbfile")
	if err != nil {
		// Try alternative field name
		file, err = c.FormFile("name")
		if err != nil {
			return s.writeSABnzbdErrorFiber(c, "No NZB file provided")
		}
	}

	// Validate file extension
	if !strings.HasSuffix(strings.ToLower(file.Filename), ".nzb") {
		return s.writeSABnzbdErrorFiber(c, "Invalid file type, must be .nzb")
	}

	// Get and validate category from form first
	category := c.FormValue("cat")
	validatedCategory, err := s.validateSABnzbdCategory(category)
	if err != nil {
		return s.writeSABnzbdErrorFiber(c, err.Error())
	}

	// Ensure category directories exist in both temp and mount paths
	if err := s.ensureCategoryDirectories(validatedCategory); err != nil {
		return s.writeSABnzbdErrorFiber(c, fmt.Sprintf("Failed to create category directories: %v", err))
	}

	// Build category path and create temporary file with category subdirectory
	tempDir := os.TempDir()
	uploadDir := filepath.Join(tempDir, "altmount-uploads")
	if err := os.MkdirAll(uploadDir, 0755); err != nil {
		return s.writeSABnzbdErrorFiber(c, "Failed to create upload directory")
	}

	categoryPath := s.buildCategoryPath(validatedCategory)
	var tempFile string
	if categoryPath != "" {
		tempFile = filepath.Join(uploadDir, categoryPath, file.Filename)
		// Ensure category subfolder exists in temp
		if err := os.MkdirAll(filepath.Dir(tempFile), 0755); err != nil {
			return s.writeSABnzbdErrorFiber(c, "Failed to create category directory")
		}
	} else {
		tempFile = filepath.Join(uploadDir, file.Filename)
	}

	// Save the uploaded file to temporary location
	if err := c.SaveFile(file, tempFile); err != nil {
		return s.writeSABnzbdErrorFiber(c, "Failed to save file")
	}

	// Add to queue using importer service
	if s.importerService == nil {
		return s.writeSABnzbdErrorFiber(c, "Importer service not available")
	}

	// Add the file to the processing queue using centralized method
	completeDir := s.configManager.GetConfig().SABnzbd.CompleteDir
	priority := s.parseSABnzbdPriority(c.FormValue("priority"))
	item, err := s.importerService.AddToQueue(c.Context(), tempFile, &completeDir, &validatedCategory, &priority)
	if err != nil {
		return s.writeSABnzbdErrorFiber(c, "Failed to add to queue")
	}

	// Return success response
	response := SABnzbdAddResponse{
		Status: true,
		NzoIds: []string{fmt.Sprintf("%d", item.ID)},
	}

	return s.writeSABnzbdResponseFiber(c, response)
}

// handleSABnzbdAddUrl handles adding NZB from URL
func (s *Server) handleSABnzbdAddUrl(c *fiber.Ctx) error {
	nzbUrl := c.Query("name")

	if nzbUrl == "" {
		return s.writeSABnzbdErrorFiber(c, "URL parameter 'name' required")
	}

	// Download NZB file from URL
	resp, err := http.Get(nzbUrl)
	if err != nil {
		return s.writeSABnzbdErrorFiber(c, "Failed to download NZB from URL")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return s.writeSABnzbdErrorFiber(c, fmt.Sprintf("Failed to download NZB: HTTP %d", resp.StatusCode))
	}

	// Get and validate category from query parameters first
	category := c.Query("cat")
	validatedCategory, err := s.validateSABnzbdCategory(category)
	if err != nil {
		return s.writeSABnzbdErrorFiber(c, err.Error())
	}

	// Ensure category directories exist in both temp and mount paths
	if err := s.ensureCategoryDirectories(validatedCategory); err != nil {
		return s.writeSABnzbdErrorFiber(c, fmt.Sprintf("Failed to create category directories: %v", err))
	}

	// Create temporary file with category path
	tempDir := os.TempDir()
	uploadDir := filepath.Join(tempDir, "altmount-uploads")
	if err := os.MkdirAll(uploadDir, 0755); err != nil {
		return s.writeSABnzbdErrorFiber(c, "Failed to create upload directory")
	}

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
		tempFile = filepath.Join(uploadDir, categoryPath, filename)
		// Ensure category subfolder exists in temp
		if err := os.MkdirAll(filepath.Dir(tempFile), 0755); err != nil {
			return s.writeSABnzbdErrorFiber(c, "Failed to create category directory")
		}
	} else {
		tempFile = filepath.Join(uploadDir, filename)
	}

	outFile, err := os.Create(tempFile)
	if err != nil {
		return s.writeSABnzbdErrorFiber(c, "Failed to create temporary file")
	}
	defer outFile.Close()

	// Copy downloaded content to file
	_, err = io.Copy(outFile, resp.Body)
	if err != nil {
		return s.writeSABnzbdErrorFiber(c, "Failed to save downloaded file")
	}

	// Add to queue
	if s.importerService == nil {
		return s.writeSABnzbdErrorFiber(c, "Importer service not available")
	}

	// Add the file to the processing queue using centralized method
	completeDir := s.configManager.GetConfig().SABnzbd.CompleteDir
	priority := s.parseSABnzbdPriority(c.Query("priority"))
	item, err := s.importerService.AddToQueue(c.Context(), tempFile, &completeDir, &validatedCategory, &priority)
	if err != nil {
		return s.writeSABnzbdErrorFiber(c, "Failed to add to queue")
	}

	// Return success response
	response := SABnzbdAddResponse{
		Status: true,
		NzoIds: []string{fmt.Sprintf("%d", item.ID)},
	}

	return s.writeSABnzbdResponseFiber(c, response)
}

// handleSABnzbdQueue handles queue operations
func (s *Server) handleSABnzbdQueue(c *fiber.Ctx) error {
	// Check for operations
	name := c.Query("name")
	switch name {
	case "delete":
		return s.handleSABnzbdQueueDelete(c)
	case "pause":
		return s.handleSABnzbdQueuePause(c, true)
	case "resume":
		return s.handleSABnzbdQueuePause(c, false)
	}

	// Get category filter from query parameter
	categoryFilter := s.normalizeCategoryFilter(c)

	// Get pagination parameters
	start := 0
	if s := c.Query("start"); s != "" {
		if val, err := strconv.Atoi(s); err == nil {
			start = val
		}
	}
	limit := 100
	if l := c.Query("limit"); l != "" {
		if val, err := strconv.Atoi(l); err == nil {
			limit = val
		}
	}

	// Get pending and processing items
	items, err := s.queueRepo.ListActiveQueueItems(c.Context(), "", categoryFilter, limit, start, "updated_at", "desc")
	if err != nil {
		return s.writeSABnzbdErrorFiber(c, "Failed to get queue")
	}

	// Get total count for pagination info

	// Let's just use a simple approach for now:
	totalCount, err := s.queueRepo.CountActiveQueueItems(c.Context(), "", categoryFilter)
	if err != nil {
		totalCount = len(items) // Fallback
	}

	// Convert to SABnzbd format
	slots := make([]SABnzbdQueueSlot, 0, len(items))
	var totalMb float64
	var totalMbLeft float64

	for i, item := range items {
		if item.Status == database.QueueStatusFallback {
			continue
		}

		slot := ToSABnzbdQueueSlot(item, start+i, s.progressBroadcaster)
		slots = append(slots, slot)

		if mb, err := strconv.ParseFloat(slot.Mb, 64); err == nil {
			totalMb += mb
		}
		if mbLeft, err := strconv.ParseFloat(slot.Mbleft, 64); err == nil {
			totalMbLeft += mbLeft
		}
	}

	status := "Idle"
	if len(slots) > 0 {
		status = "Downloading"
	}
	if s.importerService.IsPaused() {
		status = "Paused"
	}

	// Get download speed from pool manager
	kbpersec := "0.00"
	speed := "0"
	if s.poolManager != nil {
		if metrics, err := s.poolManager.GetMetrics(); err == nil {
			kbpersec = fmt.Sprintf("%.2f", metrics.DownloadSpeedBytesPerSec/1024.0)
			speed = fmt.Sprintf("%.0f", metrics.DownloadSpeedBytesPerSec)
		}
	}

	response := SABnzbdQueueResponse{
		Status: true,
		Queue: SABnzbdQueueObject{
			Paused:    s.importerService.IsPaused(),
			Slots:     slots,
			Noofslots: totalCount,
			Status:    status,
			Mbleft:    fmt.Sprintf("%.2f", totalMbLeft),
			Mb:        fmt.Sprintf("%.2f", totalMb),
			Kbpersec:  kbpersec,
			Speed:     speed,
			Version:   "4.5.0",
		},
	}

	return s.writeSABnzbdResponseFiber(c, response)
}

// handleSABnzbdQueueDelete handles deleting items from queue
func (s *Server) handleSABnzbdQueueDelete(c *fiber.Ctx) error {
	nzoID := c.Query("value")

	if nzoID == "" {
		return s.writeSABnzbdErrorFiber(c, "Missing nzo_id parameter")
	}

	// Convert nzo_id to database ID
	id, err := strconv.ParseInt(nzoID, 10, 64)
	if err != nil {
		return s.writeSABnzbdErrorFiber(c, "Invalid nzo_id")
	}

	if s.importerService == nil {
		return s.writeSABnzbdErrorFiber(c, "Importer service not available")
	}

	// Delete from queue
	err = s.queueRepo.RemoveFromQueue(c.Context(), id)
	if err != nil {
		return s.writeSABnzbdErrorFiber(c, "Failed to delete queue item")
	}

	response := SABnzbdDeleteResponse{
		Status: true,
	}

	return s.writeSABnzbdResponseFiber(c, response)
}

// handleSABnzbdHistory handles history operations
func (s *Server) handleSABnzbdHistory(c *fiber.Ctx) error {
	// Check for delete operation
	if c.Query("name") == "delete" {
		return s.handleSABnzbdHistoryDelete(c)
	}

	// Get completed and failed items
	if s.importerService == nil {
		return s.writeSABnzbdErrorFiber(c, "Importer service not available")
	}

	// Get category filter from query parameter
	categoryFilter := s.normalizeCategoryFilter(c)

	// Get pagination parameters
	start := 0
	if s := c.Query("start"); s != "" {
		if val, err := strconv.Atoi(s); err == nil {
			start = val
		}
	}
	limit := 50
	if l := c.Query("limit"); l != "" {
		if val, err := strconv.Atoi(l); err == nil {
			limit = val
		}
	}

	// Get completed items
	completedStatus := database.QueueStatusCompleted
	completed, err := s.queueRepo.ListQueueItems(c.Context(), &completedStatus, "", categoryFilter, limit, start, "updated_at", "desc")
	if err != nil {
		return s.writeSABnzbdErrorFiber(c, "Failed to get completed items")
	}

	// Get total completed count
	totalCompleted, err := s.queueRepo.CountQueueItems(c.Context(), &completedStatus, "", categoryFilter)
	if err != nil {
		totalCompleted = len(completed)
	}

	// Get failed items
	failedStatus := database.QueueStatusFailed
	failed, err := s.queueRepo.ListQueueItems(c.Context(), &failedStatus, "", categoryFilter, limit, start, "updated_at", "desc")
	if err != nil {
		return s.writeSABnzbdErrorFiber(c, "Failed to get failed items")
	}

	// Get total failed count
	totalFailed, err := s.queueRepo.CountQueueItems(c.Context(), &failedStatus, "", categoryFilter)
	if err != nil {
		totalFailed = len(failed)
	}

	// Combine and convert to SABnzbd format
	slots := make([]SABnzbdHistorySlot, 0, len(completed)+len(failed))
	index := 0
	var totalBytes int64

	for _, item := range completed {
		// Calculate category-specific base path for this item
		itemBasePath := s.calculateItemBasePath()
		slot := ToSABnzbdHistorySlot(item, start+index, itemBasePath)
		slog.DebugContext(c.Context(), "Reporting completed item to SABnzbd API",
			"name", slot.Name,
			"path", slot.Path,
			"status", slot.Status)
		slots = append(slots, slot)
		totalBytes += slot.Bytes
		index++
	}
	for _, item := range failed {
		// Calculate category-specific base path for this item
		itemBasePath := s.calculateItemBasePath()
		slot := ToSABnzbdHistorySlot(item, start+index, itemBasePath)
		slots = append(slots, slot)
		totalBytes += slot.Bytes
		index++
	}

	// Create the proper history response structure using the new struct
	response := SABnzbdCompleteHistoryResponse{
		History: SABnzbdHistoryObject{
			Slots:     slots,
			TotalSize: formatHumanSize(totalBytes),
			MonthSize: "0 B",
			WeekSize:  "0 B",
			Version:   "4.5.0",
			DaySize:   "0 B",
			Noofslots: totalCompleted + totalFailed,
		},
	}

	return s.writeSABnzbdResponseFiber(c, response)
}

// handleSABnzbdHistoryDelete handles deleting items from history
func (s *Server) handleSABnzbdHistoryDelete(c *fiber.Ctx) error {
	nzoID := c.Query("value")

	if nzoID == "" {
		return s.writeSABnzbdErrorFiber(c, "Missing nzo_id parameter")
	}

	// Convert nzo_id to database ID
	id, err := strconv.ParseInt(nzoID, 10, 64)
	if err != nil {
		return s.writeSABnzbdErrorFiber(c, "Invalid nzo_id")
	}

	if s.importerService == nil {
		return s.writeSABnzbdErrorFiber(c, "Importer service not available")
	}

	// Delete from queue (history items are still queue items with completed/failed status)
	err = s.queueRepo.RemoveFromQueue(c.Context(), id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return s.writeSABnzbdResponseFiber(c, SABnzbdDeleteResponse{
				Status: true,
			})
		}

		return s.writeSABnzbdErrorFiber(c, fmt.Sprintf("Failed to delete history item: %v", err))
	}

	response := SABnzbdDeleteResponse{
		Status: true,
	}

	return s.writeSABnzbdResponseFiber(c, response)
}

// handleSABnzbdStatus handles full status request
func (s *Server) handleSABnzbdStatus(c *fiber.Ctx) error {
	// Get queue information
	var slots []SABnzbdQueueSlot
	var totalMbLeft float64
	if s.queueRepo != nil {
		items, err := s.queueRepo.ListActiveQueueItems(c.Context(), "", "", 50, 0, "updated_at", "desc")
		if err == nil {
			for i, item := range items {
				slot := ToSABnzbdQueueSlot(item, i, s.progressBroadcaster)
				slots = append(slots, slot)

				// Parse mbleft from slot
				if mbLeft, err := strconv.ParseFloat(slot.Mbleft, 64); err == nil {
					totalMbLeft += mbLeft
				}
			}
		}
	}

	// Get actual disk space for storage directory
	cfg := s.configManager.GetConfig()
	targetPath := cfg.MountPath
	if targetPath == "" {
		targetPath = filepath.Join(os.TempDir(), "altmount-uploads")
	}
	diskFree, diskTotal := getDiskSpace(targetPath)

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
		Paused:          s.importerService != nil && s.importerService.IsPaused(),
		PauseInt:        0,
		Remaining:       fmt.Sprintf("%.1f MB", totalMbLeft),
		MbLeft:          totalMbLeft,
		Diskspace1:      formatHumanSize(int64(diskFree)),
		Diskspace2:      "0 B",
		DiskspaceTotal1: formatHumanSize(int64(diskTotal)),
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

	return s.writeSABnzbdResponseFiber(c, response)
}

// handleSABnzbdGetConfig handles configuration request
func (s *Server) handleSABnzbdGetConfig(c *fiber.Ctx) error {
	var sabnzbdConfig SABnzbdConfig

	if s.configManager != nil {
		cfg := s.configManager.GetConfig()

		// Build misc configuration
		itemBasePath := s.calculateItemBasePath()
		sabnzbdConfig.Misc = SABnzbdMiscConfig{
			CompleteDir:            pathutil.JoinAbsPath(itemBasePath, cfg.SABnzbd.CompleteDir),
			PreCheck:               0,
			HistoryRetention:       "",
			HistoryRetentionOption: "all",
			HistoryRetentionNumber: 1,
		}

		// Build categories from configuration
		if len(cfg.SABnzbd.Categories) > 0 {
			// Use configured categories
			for _, category := range cfg.SABnzbd.Categories {
				sabnzbdConfig.Categories = append(sabnzbdConfig.Categories, SABnzbdCategory{
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
			sabnzbdConfig.Categories = []SABnzbdCategory{
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
		sabnzbdConfig.Servers = []SABnzbdServer{}
	} else {
		// Fallback configuration when no config manager
		sabnzbdConfig = SABnzbdConfig{
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
		Config:  sabnzbdConfig,
	}

	return s.writeSABnzbdResponseFiber(c, response)
}

// handleSABnzbdVersion handles version request
func (s *Server) handleSABnzbdVersion(c *fiber.Ctx) error {
	response := SABnzbdVersionResponse{
		Status:  true,
		Version: "4.5.0",
	}

	return s.writeSABnzbdResponseFiber(c, response)
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
	// Empty category uses Default category's Dir
	if category == "" {
		category = config.DefaultCategoryName
	}

	if s.configManager == nil {
		// No config manager, use category name as directory (Default uses its default dir)
		if category == config.DefaultCategoryName {
			return config.DefaultCategoryDir
		}
		return category
	}

	cfg := s.configManager.GetConfig()

	// If no categories are configured, use category name as directory
	if len(cfg.SABnzbd.Categories) == 0 {
		if category == config.DefaultCategoryName {
			return config.DefaultCategoryDir
		}
		return category
	}

	// Look for the category in configuration
	for _, configCategory := range cfg.SABnzbd.Categories {
		if configCategory.Name == category {
			// Use configured Dir if available, otherwise use category name
			if configCategory.Dir != "" {
				return configCategory.Dir
			}
			// For Default category with empty Dir, return default dir
			if category == config.DefaultCategoryName {
				return config.DefaultCategoryDir
			}
			return category
		}
	}

	// Category not found in configuration, use category name as directory
	return category
}

// validateSABnzbdCategory validates and returns the category, or error if invalid
func (s *Server) validateSABnzbdCategory(category string) (string, error) {
	defaultCategory := s.getDefaultCategory()
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

// writeSABnzbdResponseFiber writes a successful SABnzbd-compatible response (Fiber version)
func (s *Server) writeSABnzbdResponseFiber(c *fiber.Ctx, data interface{}) error {
	return c.Status(200).JSON(data)
}

// writeSABnzbdErrorFiber writes a SABnzbd-compatible error response (Fiber version)
func (s *Server) writeSABnzbdErrorFiber(c *fiber.Ctx, message string) error {
	response := SABnzbdResponse{
		Status: false,
		Error:  &message,
	}
	return c.Status(200).JSON(response) // SABnzbd returns 200 even for errors
}

// ensureCategoryDirectories creates directories for a category in both temp and mount paths
func (s *Server) ensureCategoryDirectories(category string) error {
	if s.configManager == nil {
		return fmt.Errorf("config manager not available")
	}

	categoryPath := s.buildCategoryPath(category)

	// Don't create directory for default category (empty path)
	if categoryPath == "" {
		return nil
	}

	// Create in temp path
	tempDir := filepath.Join(os.TempDir(), "altmount-uploads", categoryPath)
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}

	return nil
}

// normalizeCategoryFilter extracts and normalizes the category filter from query parameters
func (s *Server) normalizeCategoryFilter(c *fiber.Ctx) string {
	category := c.Query("category", "")
	if category == "" {
		category = c.Query("cat", "")
	}

	lower := strings.ToLower(category)
	if lower == "all" || lower == "*" || lower == "default" {
		return ""
	}

	return category
}

// calculateItemBasePath calculates the base path for an item based on the import strategy configuration
func (s *Server) calculateItemBasePath() string {
	if s.configManager == nil {
		return ""
	}

	cfg := s.configManager.GetConfig()

	// Determine if we should use import directory or mount path
	var basePath string
	if cfg.Import.ImportStrategy != config.ImportStrategyNone &&
		cfg.Import.ImportDir != nil && *cfg.Import.ImportDir != "" {
		// Use import directory as base when import strategy is enabled
		basePath = *cfg.Import.ImportDir
	} else {
		// Fall back to mount path
		basePath = cfg.MountPath
	}

	// Return base path with category folder
	return basePath
}

// normalizeURL normalizes a URL for comparison by removing trailing slashes
func normalizeURL(rawURL string) string {
	return strings.TrimSuffix(rawURL, "/")
}

// findARRInstanceByURL finds an ARR instance by URL
func (s *Server) findARRInstanceByURL(checkURL string) *arrs.ConfigInstance {
	if s.arrsService == nil {
		return nil
	}

	normalizedCheck := normalizeURL(checkURL)
	instances := s.arrsService.GetAllInstances()

	for _, instance := range instances {
		normalizedInstance := normalizeURL(instance.URL)
		if normalizedInstance == normalizedCheck {
			return instance
		}
	}

	return nil
}

// getDiskSpace returns free and total disk space in bytes for the given path
func getDiskSpace(path string) (free, total uint64) {
	// For Linux/Unix
	if runtime.GOOS != "windows" {
		var stat syscall.Statfs_t
		if err := syscall.Statfs(path, &stat); err == nil {
			// Available blocks * size per block = available space in bytes
			free = stat.Bavail * uint64(stat.Bsize)
			total = stat.Blocks * uint64(stat.Bsize)
			return
		}
	}

	// Fallback/Default
	return 0, 0
}
