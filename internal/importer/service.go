package importer

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/altmount/internal/metadata"
	"github.com/javi11/altmount/internal/pool"
	"github.com/javi11/altmount/pkg/rclonecli"
)

// ServiceConfig holds configuration for the NZB import service
type ServiceConfig struct {
	Workers int // Number of parallel queue workers (default: 2)
}

// ScanStatus represents the current status of a manual scan
type ScanStatus string

const (
	ScanStatusIdle      ScanStatus = "idle"
	ScanStatusScanning  ScanStatus = "scanning"
	ScanStatusCanceling ScanStatus = "canceling"
)

// ScanInfo holds information about the current scan operation
type ScanInfo struct {
	Status        ScanStatus `json:"status"`
	Path          string     `json:"path,omitempty"`
	StartTime     *time.Time `json:"start_time,omitempty"`
	FilesFound    int        `json:"files_found"`
	FilesAdded    int        `json:"files_added"`
	CurrentFile   string     `json:"current_file,omitempty"`
	LastError     *string    `json:"last_error,omitempty"`
}

// Service provides NZB import functionality with manual directory scanning and queue-based processing
type Service struct {
	config          ServiceConfig
	database        *database.DB              // Database for processing queue
	metadataService *metadata.MetadataService // Metadata service for file processing
	processor       *Processor
	rcloneClient    rclonecli.RcloneRcClient // Optional rclone client for VFS notifications
	log             *slog.Logger

	// Runtime state
	mu      sync.RWMutex
	running bool
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup

	// Manual scan state
	scanMu     sync.RWMutex
	scanInfo   ScanInfo
	scanCancel context.CancelFunc
}

// NewService creates a new NZB import service with manual scanning and queue processing capabilities
func NewService(config ServiceConfig, metadataService *metadata.MetadataService, database *database.DB, poolManager pool.Manager, rcloneClient rclonecli.RcloneRcClient) (*Service, error) {
	// Set defaults
	if config.Workers == 0 {
		config.Workers = 2
	}

	// Create processor with poolManager for dynamic pool access
	processor := NewProcessor(metadataService, poolManager)

	ctx, cancel := context.WithCancel(context.Background())

	service := &Service{
		config:          config,
		metadataService: metadataService,
		database:        database,
		processor:       processor,
		rcloneClient:    rcloneClient,
		log:             slog.Default().With("component", "importer-service"),
		ctx:             ctx,
		cancel:          cancel,
		scanInfo:        ScanInfo{Status: ScanStatusIdle},
	}

	return service, nil
}

// Start starts the NZB import service (queue workers only, manual scanning available via API)
func (s *Service) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return fmt.Errorf("service is already started")
	}

	s.log.InfoContext(ctx, "Starting NZB import service",
		"workers", s.config.Workers)

	// Start worker pool for processing queue items
	for i := 0; i < s.config.Workers; i++ {
		s.wg.Add(1)
		go s.workerLoop(i)
	}

	s.running = true
	s.log.InfoContext(ctx, "NZB import service started successfully")

	return nil
}

// Stop stops the NZB import service and all queue workers
func (s *Service) Stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return nil
	}

	s.log.InfoContext(ctx, "Stopping NZB import service")

	// Cancel all goroutines
	s.cancel()

	// Wait for all goroutines to finish
	s.wg.Wait()

	// Recreate context for potential restart
	s.ctx, s.cancel = context.WithCancel(context.Background())

	s.running = false
	s.log.InfoContext(ctx, "NZB import service stopped")

	return nil
}

// Close closes the NZB import service and releases all resources
func (s *Service) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		s.cancel()
		s.wg.Wait()
	}

	return nil
}

// IsRunning returns whether the service is running
func (s *Service) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

// Database returns the database instance for processing
func (s *Service) Database() *database.DB {
	return s.database
}

// GetQueueStats returns current queue statistics from database
func (s *Service) GetQueueStats(ctx context.Context) (*database.QueueStats, error) {
	return s.database.Repository.GetQueueStats()
}

// StartManualScan starts a manual scan of the specified directory
func (s *Service) StartManualScan(scanPath string) error {
	s.scanMu.Lock()
	defer s.scanMu.Unlock()

	// Check if already scanning
	if s.scanInfo.Status != ScanStatusIdle {
		return fmt.Errorf("scan already in progress, current status: %s", s.scanInfo.Status)
	}

	// Validate path
	if scanPath == "" {
		return fmt.Errorf("scan path cannot be empty")
	}

	// Check if path exists
	if _, err := filepath.Abs(scanPath); err != nil {
		return fmt.Errorf("invalid path: %w", err)
	}

	// Create scan context
	scanCtx, scanCancel := context.WithCancel(context.Background())
	s.scanCancel = scanCancel

	// Initialize scan info
	now := time.Now()
	s.scanInfo = ScanInfo{
		Status:      ScanStatusScanning,
		Path:        scanPath,
		StartTime:   &now,
		FilesFound:  0,
		FilesAdded:  0,
		CurrentFile: "",
		LastError:   nil,
	}

	// Start scanning in goroutine
	go s.performManualScan(scanCtx, scanPath)

	s.log.Info("Manual scan started", "path", scanPath)
	return nil
}

// GetScanStatus returns the current scan status
func (s *Service) GetScanStatus() ScanInfo {
	s.scanMu.RLock()
	defer s.scanMu.RUnlock()
	return s.scanInfo
}

// CancelScan cancels the current scan operation
func (s *Service) CancelScan() error {
	s.scanMu.Lock()
	defer s.scanMu.Unlock()

	if s.scanInfo.Status == ScanStatusIdle {
		return fmt.Errorf("no scan is currently running")
	}

	if s.scanInfo.Status == ScanStatusCanceling {
		return fmt.Errorf("scan is already being canceled")
	}

	// Update status and cancel context
	s.scanInfo.Status = ScanStatusCanceling
	if s.scanCancel != nil {
		s.scanCancel()
	}

	s.log.Info("Manual scan cancellation requested", "path", s.scanInfo.Path)
	return nil
}

// performManualScan performs the actual scanning work in a separate goroutine
func (s *Service) performManualScan(ctx context.Context, scanPath string) {
	defer func() {
		s.scanMu.Lock()
		s.scanInfo.Status = ScanStatusIdle
		s.scanInfo.CurrentFile = ""
		if s.scanCancel != nil {
			s.scanCancel()
			s.scanCancel = nil
		}
		s.scanMu.Unlock()
	}()

	s.log.Debug("Scanning directory for NZB files", "dir", scanPath)

	err := filepath.WalkDir(scanPath, func(path string, d fs.DirEntry, err error) error {
		// Check for cancellation
		select {
		case <-ctx.Done():
			s.log.Info("Scan cancelled", "path", scanPath)
			return fmt.Errorf("scan cancelled")
		default:
		}

		if err != nil {
			s.log.Warn("Error accessing path", "path", path, "error", err)
			s.scanMu.Lock()
			errMsg := err.Error()
			s.scanInfo.LastError = &errMsg
			s.scanMu.Unlock()
			return nil // Continue walking
		}

		// Skip directories
		if d.IsDir() {
			return nil
		}

		// Update current file being processed
		s.scanMu.Lock()
		s.scanInfo.CurrentFile = path
		s.scanInfo.FilesFound++
		s.scanMu.Unlock()

		// Check if it's an NZB or STRM file
		ext := strings.ToLower(path)
		if !strings.HasSuffix(ext, ".nzb") && !strings.HasSuffix(ext, ".strm") {
			return nil
		}

		// Check if already in queue (simplified check during scanning)
		if s.isFileAlreadyInQueue(path) {
			return nil
		}

		// Add to queue
		s.addToQueue(path, &scanPath)

		// Update files added counter
		s.scanMu.Lock()
		s.scanInfo.FilesAdded++
		s.scanMu.Unlock()

		return nil
	})

	if err != nil && !strings.Contains(err.Error(), "scan cancelled") {
		s.log.Error("Failed to scan directory", "dir", scanPath, "error", err)
		s.scanMu.Lock()
		errMsg := err.Error()
		s.scanInfo.LastError = &errMsg
		s.scanMu.Unlock()
	}

	s.log.Info("Manual scan completed", "path", scanPath, "files_found", s.scanInfo.FilesFound, "files_added", s.scanInfo.FilesAdded)
}

// isFileAlreadyInQueue checks if file is already in queue (simplified scanning)
func (s *Service) isFileAlreadyInQueue(filePath string) bool {
	// Only check queue database during scanning for performance
	// The processor will check main database for duplicates when processing
	inQueue, err := s.database.Repository.IsFileInQueue(filePath)
	if err != nil {
		s.log.Warn("Failed to check if file in queue", "file", filePath, "error", err)
		return false // Assume not in queue on error
	}
	return inQueue
}

// addToQueue adds a new NZB file to the import queue
func (s *Service) addToQueue(filePath string, watchRoot *string) {
	item := &database.ImportQueueItem{
		NzbPath:    filePath,
		WatchRoot:  watchRoot,
		Priority:   database.QueuePriorityNormal,
		Status:     database.QueueStatusPending,
		RetryCount: 0,
		MaxRetries: 3,
		CreatedAt:  time.Now(),
	}

	if err := s.database.Repository.AddToQueue(item); err != nil {
		s.log.Error("Failed to add file to queue", "file", filePath, "error", err)
		return
	}

	s.log.Info("Added NZB file to queue", "file", filePath, "queue_id", item.ID)
}

// workerLoop processes queue items
func (s *Service) workerLoop(workerID int) {
	defer s.wg.Done()

	log := s.log.With("worker_id", workerID)
	log.Info("Queue worker started")

	ticker := time.NewTicker(5 * time.Second) // Check for work every 5 seconds
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.processQueueItems(workerID)
		case <-s.ctx.Done():
			log.Info("Queue worker stopped")
			return
		}
	}
}

// claimItemWithRetry attempts to claim a queue item with exponential backoff retry logic
func (s *Service) claimItemWithRetry(workerID int, log *slog.Logger) (*database.ImportQueueItem, error) {
	const maxRetries = 5
	const baseDelay = 10 * time.Millisecond
	const maxDelay = 500 * time.Millisecond

	for attempt := 0; attempt < maxRetries; attempt++ {
		item, err := s.database.Repository.ClaimNextQueueItem()
		if err == nil {
			return item, nil
		}

		// Check if this is a database contention error
		if strings.Contains(err.Error(), "database is locked") ||
			strings.Contains(err.Error(), "database is busy") {

			if attempt == maxRetries-1 {
				// Final attempt failed, return the error
				return nil, fmt.Errorf("failed to claim queue item after %d attempts: %w", maxRetries, err)
			}

			// Calculate backoff delay with jitter
			delay := time.Duration(attempt+1) * baseDelay
			if delay > maxDelay {
				delay = maxDelay
			}

			// Add random jitter (0-50% of delay) to prevent thundering herd
			jitter := time.Duration(float64(delay) * (0.5 * float64(workerID%10) / 10.0))
			delay += jitter

			log.Debug("Database contention, retrying claim",
				"attempt", attempt+1,
				"delay", delay,
				"worker_id", workerID)

			time.Sleep(delay)
			continue
		}

		// Non-contention error, return immediately
		return nil, fmt.Errorf("failed to claim queue item: %w", err)
	}

	return nil, fmt.Errorf("failed to claim queue item after %d attempts", maxRetries)
}

// processQueueItems gets and processes pending queue items using two-database workflow
func (s *Service) processQueueItems(workerID int) {
	log := s.log.With("worker_id", workerID)

	// Step 1: Atomically claim next available item from queue database with retry logic
	item, err := s.claimItemWithRetry(workerID, log)
	if err != nil {
		// Only log non-contention errors
		if !strings.Contains(err.Error(), "database is locked") && !strings.Contains(err.Error(), "database is busy") {
			log.Error("Failed to claim next queue item", "error", err)
		}
		return
	}

	if item == nil {
		return // No work to do
	}

	log.Debug("Processing claimed queue item", "queue_id", item.ID, "file", item.NzbPath)

	// Step 3: Process the NZB file and write to main database
	var processingErr error
	if item.WatchRoot != nil {
		processingErr = s.processor.ProcessNzbFileWithRoot(item.NzbPath, *item.WatchRoot)
	} else {
		processingErr = s.processor.ProcessNzbFile(item.NzbPath)
	}

	// Step 4: Update queue database with results
	if processingErr != nil {
		// Handle failure in queue database
		s.handleProcessingFailure(item, processingErr, log)
	} else {
		// Mark as completed in queue database
		if err := s.database.Repository.UpdateQueueItemStatus(item.ID, database.QueueStatusCompleted, nil); err != nil {
			log.Error("Failed to mark item as completed", "queue_id", item.ID, "error", err)
		} else {
			log.Info("Successfully processed queue item", "queue_id", item.ID, "file", item.NzbPath)
			
			// Notify rclone VFS about the new import (async, don't fail on error)
			s.notifyRcloneVFS(item, log)
		}
	}
}

// handleProcessingFailure handles when processing fails
func (s *Service) handleProcessingFailure(item *database.ImportQueueItem, processingErr error, log *slog.Logger) {
	errorMessage := processingErr.Error()

	log.Warn("Processing failed",
		"queue_id", item.ID,
		"file", item.NzbPath,
		"error", processingErr,
		"retry_count", item.RetryCount,
		"max_retries", item.MaxRetries)

	// Check if we should retry
	if item.RetryCount < item.MaxRetries {
		// Mark for retry in queue database
		if err := s.database.Repository.UpdateQueueItemStatus(item.ID, database.QueueStatusRetrying, &errorMessage); err != nil {
			log.Error("Failed to mark item for retry", "queue_id", item.ID, "error", err)
		} else {
			log.Info("Item marked for retry", "queue_id", item.ID, "retry_count", item.RetryCount+1)
		}
	} else {
		// Max retries exceeded, mark as failed in queue database
		if err := s.database.Repository.UpdateQueueItemStatus(item.ID, database.QueueStatusFailed, &errorMessage); err != nil {
			log.Error("Failed to mark item as failed", "queue_id", item.ID, "error", err)
		} else {
			log.Error("Item failed permanently after max retries",
				"queue_id", item.ID,
				"file", item.NzbPath,
				"retry_count", item.RetryCount)
		}
	}
}

// ServiceStats holds statistics about the service
type ServiceStats struct {
	IsRunning    bool                 `json:"is_running"`
	Workers      int                  `json:"workers"`
	QueueStats   *database.QueueStats `json:"queue_stats,omitempty"`
	ScanInfo     ScanInfo             `json:"scan_info"`
}

// GetStats returns service statistics
func (s *Service) GetStats(ctx context.Context) (*ServiceStats, error) {
	stats := &ServiceStats{
		IsRunning: s.IsRunning(),
		Workers:   s.config.Workers,
		ScanInfo:  s.GetScanStatus(),
	}

	// Add queue statistics
	queueStats, err := s.GetQueueStats(ctx)
	if err != nil {
		s.log.WarnContext(ctx, "Failed to get queue stats", "error", err)
	} else {
		stats.QueueStats = queueStats
	}

	return stats, nil
}

// UpdateWorkerCount updates the worker count configuration (requires service restart to take effect)
// Dynamic worker scaling is not supported - changes only apply on next service restart
func (s *Service) UpdateWorkerCount(count int) error {
	if count <= 0 {
		return fmt.Errorf("worker count must be greater than 0")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.log.Info("Queue worker count update requested - restart required to take effect",
		"current_count", s.config.Workers,
		"requested_count", count,
		"running", s.running)

	// Configuration update is handled at the config manager level
	// Changes only take effect on service restart
	return nil
}

// GetWorkerCount returns the current configured worker count
func (s *Service) GetWorkerCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.config.Workers
}

// notifyRcloneVFS notifies rclone VFS about a new import (async, non-blocking)
func (s *Service) notifyRcloneVFS(item *database.ImportQueueItem, log *slog.Logger) {
	if s.rcloneClient == nil {
		return // No rclone client configured
	}

	// Calculate the virtual directory path for VFS notification
	var virtualDir string
	if item.WatchRoot != nil {
		// Calculate virtual directory based on NZB file location relative to watch root
		virtualDir = s.calculateVirtualDirectory(item.NzbPath, *item.WatchRoot)
	} else {
		// Default to root if no watch root specified
		virtualDir = "/"
	}

	// Run VFS notification in background (async)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second) // 10 second timeout
		defer cancel()

		err := s.rcloneClient.RefreshCache(ctx, virtualDir, true, false) // async=true, recursive=false
		if err != nil {
			log.Warn("Failed to notify rclone VFS about new import", 
				"queue_id", item.ID, 
				"file", item.NzbPath, 
				"virtual_dir", virtualDir, 
				"error", err)
		} else {
			log.Debug("Successfully notified rclone VFS about new import", 
				"queue_id", item.ID, 
				"virtual_dir", virtualDir)
		}
	}()
}

// calculateVirtualDirectory calculates the virtual directory for VFS notification
func (s *Service) calculateVirtualDirectory(nzbPath, watchRoot string) string {
	if watchRoot == "" {
		return "/"
	}

	// Clean paths for consistent comparison
	nzbPath = filepath.Clean(nzbPath)
	watchRoot = filepath.Clean(watchRoot)

	// Get relative path from watch root to NZB file
	relPath, err := filepath.Rel(watchRoot, nzbPath)
	if err != nil {
		// If we can't get relative path, default to root
		return "/"
	}

	// Get directory of NZB file (without filename)
	relDir := filepath.Dir(relPath)

	// Convert to virtual path
	if relDir == "." || relDir == "" {
		// NZB is directly in watch root
		return "/"
	}

	// Ensure virtual path starts with / and uses forward slashes
	virtualPath := "/" + strings.ReplaceAll(relDir, string(filepath.Separator), "/")
	return filepath.Clean(virtualPath)
}
