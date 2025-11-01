package importer

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/avast/retry-go/v4"
	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/altmount/internal/metadata"
	"github.com/javi11/altmount/internal/pool"
	"github.com/javi11/altmount/internal/progress"
	"github.com/javi11/altmount/internal/sabnzbd"
	"github.com/javi11/altmount/pkg/rclonecli"
	"github.com/javi11/nzbparser"
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
	Status      ScanStatus `json:"status"`
	Path        string     `json:"path,omitempty"`
	StartTime   *time.Time `json:"start_time,omitempty"`
	FilesFound  int        `json:"files_found"`
	FilesAdded  int        `json:"files_added"`
	CurrentFile string     `json:"current_file,omitempty"`
	LastError   *string    `json:"last_error,omitempty"`
}

// Service provides NZB import functionality with manual directory scanning and queue-based processing
type Service struct {
	config          ServiceConfig
	database        *database.DB              // Database for processing queue
	metadataService *metadata.MetadataService // Metadata service for file processing
	processor       *Processor
	rcloneClient    rclonecli.RcloneRcClient      // Optional rclone client for VFS notifications
	configGetter    config.ConfigGetter           // Config getter for dynamic configuration access
	sabnzbdClient   *sabnzbd.SABnzbdClient        // SABnzbd client for fallback
	broadcaster     *progress.ProgressBroadcaster // WebSocket progress broadcaster
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
func NewService(config ServiceConfig, metadataService *metadata.MetadataService, database *database.DB, poolManager pool.Manager, rcloneClient rclonecli.RcloneRcClient, configGetter config.ConfigGetter, broadcaster *progress.ProgressBroadcaster) (*Service, error) {
	// Set defaults
	if config.Workers == 0 {
		config.Workers = 2
	}

	// Get the initial config to pass max validation goroutines and full validation setting
	currentConfig := configGetter()
	maxValidationGoroutines := currentConfig.Import.MaxValidationGoroutines
	fullSegmentValidation := currentConfig.Import.FullSegmentValidation
	allowedFileExtensions := currentConfig.Import.AllowedFileExtensions
	maxImportConnections := currentConfig.Import.MaxImportConnections
	importCacheSizeMB := currentConfig.Import.ImportCacheSizeMB

	// Create processor with poolManager for dynamic pool access
	processor := NewProcessor(metadataService, poolManager, maxValidationGoroutines, fullSegmentValidation, allowedFileExtensions, maxImportConnections, importCacheSizeMB, broadcaster)

	ctx, cancel := context.WithCancel(context.Background())

	service := &Service{
		config:          config,
		metadataService: metadataService,
		database:        database,
		processor:       processor,
		rcloneClient:    rcloneClient,
		configGetter:    configGetter,
		sabnzbdClient:   sabnzbd.NewSABnzbdClient(),
		broadcaster:     broadcaster,
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

	// Reset any stale queue items from processing back to pending
	if err := s.database.Repository.ResetStaleItems(); err != nil {
		s.log.ErrorContext(ctx, "Failed to reset stale queue items", "error", err)
		return fmt.Errorf("failed to reset stale queue items: %w", err)
	}

	// Start worker pool for processing queue items
	for i := 0; i < s.config.Workers; i++ {
		s.wg.Add(1)
		go s.workerLoop(i)
	}

	s.running = true
	s.log.InfoContext(ctx, fmt.Sprintf("NZB import service started successfully with %d workers", s.config.Workers))

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

// SetRcloneClient sets or updates the RClone client for VFS notifications
func (s *Service) SetRcloneClient(client rclonecli.RcloneRcClient) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rcloneClient = client
	if client != nil {
		s.log.Info("RClone client updated for VFS notifications")
	} else {
		s.log.Info("RClone client disabled")
	}
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
		if _, err := s.AddToQueue(path, &scanPath, nil, nil); err != nil {
			s.log.Error("Failed to add file to queue during scan", "file", path, "error", err)
		}

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

// AddToQueue adds a new NZB file to the import queue with optional category and priority
func (s *Service) AddToQueue(filePath string, relativePath *string, category *string, priority *database.QueuePriority) (*database.ImportQueueItem, error) {
	// Calculate file size before adding to queue
	var fileSize *int64
	if size, err := s.CalculateFileSizeOnly(filePath); err != nil {
		s.log.Warn("Failed to calculate file size", "file", filePath, "error", err)
		// Continue with NULL file size - don't fail the queue addition
		fileSize = nil
	} else {
		fileSize = &size
	}

	// Use default priority if not specified
	itemPriority := database.QueuePriorityNormal
	if priority != nil {
		itemPriority = *priority
	}

	item := &database.ImportQueueItem{
		NzbPath:      filePath,
		RelativePath: relativePath,
		Category:     category,
		Priority:     itemPriority,
		Status:       database.QueueStatusPending,
		RetryCount:   0,
		MaxRetries:   3,
		FileSize:     fileSize,
		CreatedAt:    time.Now(),
	}

	if err := s.database.Repository.AddToQueue(item); err != nil {
		s.log.Error("Failed to add file to queue", "file", filePath, "error", err)
		return nil, err
	}

	if fileSize != nil {
		s.log.Info("Added NZB file to queue", "file", filePath, "queue_id", item.ID, "file_size", *fileSize)
	} else {
		s.log.Info("Added NZB file to queue", "file", filePath, "queue_id", item.ID, "file_size", "unknown")
	}

	return item, nil
}

// workerLoop processes queue items
func (s *Service) workerLoop(workerID int) {
	defer s.wg.Done()

	log := s.log.With("worker_id", workerID)

	// Get processing interval from configuration
	processingIntervalSeconds := s.configGetter().Import.QueueProcessingIntervalSeconds
	processingInterval := time.Duration(processingIntervalSeconds) * time.Second

	ticker := time.NewTicker(processingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.processQueueItems(s.ctx, workerID)
		case <-s.ctx.Done():
			log.Info("Queue worker stopped")
			return
		}
	}
}

// isDatabaseContentionError checks if an error is a retryable database contention error
func isDatabaseContentionError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "database is locked") ||
		strings.Contains(err.Error(), "database is busy")
}

// claimItemWithRetry attempts to claim a queue item with exponential backoff retry logic using retry-go
func (s *Service) claimItemWithRetry(workerID int, log *slog.Logger) (*database.ImportQueueItem, error) {
	var item *database.ImportQueueItem

	err := retry.Do(
		func() error {
			claimedItem, err := s.database.Repository.ClaimNextQueueItem()
			if err != nil {
				return err
			}

			item = claimedItem
			return nil
		},
		retry.Attempts(5),
		retry.Delay(10*time.Millisecond),
		retry.MaxDelay(500*time.Millisecond),
		retry.DelayType(retry.BackOffDelay),
		retry.RetryIf(isDatabaseContentionError),
		retry.OnRetry(func(n uint, err error) {
			log.Debug("Database contention, retrying claim",
				"attempt", n+1,
				"worker_id", workerID,
				"error", err)
		}),
	)

	if err != nil {
		return nil, fmt.Errorf("failed to claim queue item: %w", err)
	}

	if item == nil {
		return nil, nil
	}

	log.Debug("Next item in processing queue", "queue_id", item.ID, "file", item.NzbPath)
	return item, nil
}

// processQueueItems gets and processes pending queue items using two-database workflow
func (s *Service) processQueueItems(ctx context.Context, workerID int) {
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
	resultingPath, processingErr := s.processNzbItem(ctx, item)

	// Step 4: Update queue database with results
	if processingErr != nil {
		// Handle failure in queue database
		s.handleProcessingFailure(item, processingErr, log)
	} else {
		// Handle success (storage path, VFS notification, symlinks, status update)
		s.handleProcessingSuccess(item, resultingPath, log)
	}
}

// refreshMountPathIfNeeded checks if the mount path exists and refreshes the root directory if not found
func (s *Service) refreshMountPathIfNeeded(resultingPath string, itemID int64, log *slog.Logger) {
	if s.rcloneClient == nil {
		return
	}

	mountPath := filepath.Join(s.configGetter().MountPath, filepath.Dir(resultingPath))
	if _, err := os.Stat(mountPath); err != nil {
		if os.IsNotExist(err) {
			// Refresh the root path if the mount path is not found
			err := s.rcloneClient.RefreshDir(s.ctx, config.MountProvider, []string{"/"})
			if err != nil {
				log.Error("Failed to refresh mount path", "queue_id", itemID, "path", mountPath, "error", err)
			}
		}
	}
}

// processNzbItem processes the NZB file for a queue item
func (s *Service) processNzbItem(ctx context.Context, item *database.ImportQueueItem) (string, error) {
	// Determine the base path, incorporating category if present
	basePath := ""
	if item.RelativePath != nil {
		basePath = *item.RelativePath
	}

	// If category is specified, append it to the base path
	if item.Category != nil && *item.Category != "" {
		basePath = filepath.Join(basePath, *item.Category)
	}

	return s.processor.ProcessNzbFile(ctx, item.NzbPath, basePath, int(item.ID))
}

// handleProcessingSuccess handles all steps after successful NZB processing
func (s *Service) handleProcessingSuccess(item *database.ImportQueueItem, resultingPath string, log *slog.Logger) error {
	// Add storage path to database
	if err := s.database.Repository.AddStoragePath(item.ID, resultingPath); err != nil {
		log.Error("Failed to add storage path", "queue_id", item.ID, "error", err)
		return err
	}

	// Refresh mount path if needed
	s.refreshMountPathIfNeeded(resultingPath, item.ID, log)

	// Notify rclone VFS about the new import (async, don't fail on error)
	s.notifyRcloneVFS(resultingPath, log)

	// Create category symlink (non-blocking)
	if err := s.createSymlinks(item, resultingPath); err != nil {
		log.Warn("Failed to create symlink",
			"queue_id", item.ID,
			"path", resultingPath,
			"error", err)
		// Don't fail the import, just log the warning
	}

	// Mark as completed in queue database
	if err := s.database.Repository.UpdateQueueItemStatus(item.ID, database.QueueStatusCompleted, nil); err != nil {
		log.Error("Failed to mark item as completed", "queue_id", item.ID, "error", err)
		return err
	}

	// Clear progress tracking
	if s.broadcaster != nil {
		s.broadcaster.ClearProgress(int(item.ID))
	}

	log.Info("Successfully processed queue item", "queue_id", item.ID, "file", item.NzbPath)
	return nil
}

// handleProcessingFailure handles when processing fails
func (s *Service) handleProcessingFailure(item *database.ImportQueueItem, processingErr error, log *slog.Logger) {
	errorMessage := processingErr.Error()

	log.Warn("Processing failed",
		"queue_id", item.ID,
		"file", item.NzbPath,
		"error", processingErr)

	// Mark as failed in queue database (no automatic retry)
	if err := s.database.Repository.UpdateQueueItemStatus(item.ID, database.QueueStatusFailed, &errorMessage); err != nil {
		log.Error("Failed to mark item as failed", "queue_id", item.ID, "error", err)
	} else {
		log.Error("Item failed",
			"queue_id", item.ID,
			"file", item.NzbPath)
	}

	// Clear progress tracking
	if s.broadcaster != nil {
		s.broadcaster.ClearProgress(int(item.ID))
	}

	// Attempt SABnzbd fallback if configured
	if err := s.attemptSABnzbdFallback(item, log); err != nil {
		log.Error("Failed to send to external SABnzbd",
			"queue_id", item.ID,
			"file", item.NzbPath,
			"fallback_host", s.configGetter().SABnzbd.FallbackHost,
			"error", err)

	} else {
		// Mark item as fallback instead of removing from queue
		if err := s.database.Repository.UpdateQueueItemStatus(item.ID, database.QueueStatusFallback, nil); err != nil {
			log.Error("Failed to mark item as fallback", "queue_id", item.ID, "error", err)
		} else {
			log.Info("Item marked as fallback after successful SABnzbd transfer",
				"queue_id", item.ID,
				"file", item.NzbPath,
				"fallback_host", s.configGetter().SABnzbd.FallbackHost)
		}
	}
}

// attemptSABnzbdFallback attempts to send a failed import to an external SABnzbd instance
func (s *Service) attemptSABnzbdFallback(item *database.ImportQueueItem, log *slog.Logger) error {
	// Get current configuration
	cfg := s.configGetter()

	// Check if SABnzbd is enabled and fallback is configured
	if cfg.SABnzbd.Enabled == nil || !*cfg.SABnzbd.Enabled {
		log.Debug("SABnzbd fallback not attempted - SABnzbd API not enabled", "queue_id", item.ID)
		return fmt.Errorf("SABnzbd fallback not attempted - SABnzbd API not enabled")
	}

	if cfg.SABnzbd.FallbackHost == "" {
		log.Debug("SABnzbd fallback not attempted - no fallback host configured", "queue_id", item.ID)
		return fmt.Errorf("SABnzbd fallback not attempted - no fallback host configured")
	}

	if cfg.SABnzbd.FallbackAPIKey == "" {
		log.Warn("SABnzbd fallback not attempted - no API key configured", "queue_id", item.ID)
		return fmt.Errorf("SABnzbd fallback not attempted - no API key configured")
	}

	// Check if the NZB file still exists
	if _, err := os.Stat(item.NzbPath); err != nil {
		log.Warn("SABnzbd fallback not attempted - NZB file not found",
			"queue_id", item.ID,
			"file", item.NzbPath,
			"error", err)
		return err
	}

	log.Info("Attempting to send failed import to external SABnzbd",
		"queue_id", item.ID,
		"file", item.NzbPath,
		"fallback_host", cfg.SABnzbd.FallbackHost)

	// Convert priority to SABnzbd format
	priority := s.convertPriorityToSABnzbd(item.Priority)

	// Send to external SABnzbd
	nzoID, err := s.sabnzbdClient.SendNZBFile(
		cfg.SABnzbd.FallbackHost,
		cfg.SABnzbd.FallbackAPIKey,
		item.NzbPath,
		item.Category,
		&priority,
	)
	if err != nil {
		return err
	}

	log.Info("Successfully sent failed import to external SABnzbd",
		"queue_id", item.ID,
		"file", item.NzbPath,
		"fallback_host", cfg.SABnzbd.FallbackHost,
		"sabnzbd_nzo_id", nzoID)

	return nil
}

// ServiceStats holds statistics about the service
type ServiceStats struct {
	IsRunning  bool                 `json:"is_running"`
	Workers    int                  `json:"workers"`
	QueueStats *database.QueueStats `json:"queue_stats,omitempty"`
	ScanInfo   ScanInfo             `json:"scan_info"`
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
func (s *Service) notifyRcloneVFS(resultingPath string, log *slog.Logger) {
	if s.rcloneClient == nil {
		return // No rclone client configured or RClone RC is disabled
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second) // 10 second timeout
	defer cancel()

	err := s.rcloneClient.RefreshDir(ctx, config.MountProvider, []string{resultingPath}) // Use RefreshDir with empty provider
	if err != nil {
		log.Warn("Failed to notify rclone VFS about new import",
			"virtual_dir", resultingPath,
			"error", err)
	} else {
		log.Info("Successfully notified rclone VFS about new import",
			"virtual_dir", resultingPath)
	}
}

// ProcessItemInBackground processes a specific queue item in the background
func (s *Service) ProcessItemInBackground(ctx context.Context, itemID int64) {
	go func() {
		log := s.log.With("item_id", itemID, "background", true)
		log.Debug("Starting background processing of queue item")

		// Get the queue item
		item, err := s.database.Repository.GetQueueItem(itemID)
		if err != nil {
			log.Error("Failed to get queue item for background processing", "error", err)
			return
		}

		if item == nil {
			log.Warn("Queue item not found for background processing")
			return
		}

		// Update status to processing
		if err := s.database.Repository.UpdateQueueItemStatus(itemID, database.QueueStatusProcessing, nil); err != nil {
			log.Error("Failed to update item status to processing", "error", err)
			return
		}

		// Process the NZB file
		resultingPath, processingErr := s.processNzbItem(ctx, item)

		// Update queue database with results
		if processingErr != nil {
			// Handle failure
			s.handleProcessingFailure(item, processingErr, log)
		} else {
			// Handle success (storage path, VFS notification, symlinks, status update)
			s.handleProcessingSuccess(item, resultingPath, log)
		}
	}()
}

// CalculateFileSizeOnly calculates the total file size from NZB/STRM segments
// This is a lightweight parser that only extracts size information without full processing
func (s *Service) CalculateFileSizeOnly(filePath string) (int64, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return 0, NewNonRetryableError("failed to open file for size calculation", err)
	}
	defer file.Close()

	if strings.HasSuffix(strings.ToLower(filePath), ".strm") {
		return s.calculateStrmFileSize(file)
	} else {
		return s.calculateNzbFileSize(file)
	}
}

// calculateNzbFileSize calculates the total size from NZB file segments
func (s *Service) calculateNzbFileSize(r io.Reader) (int64, error) {
	n, err := nzbparser.Parse(r)
	if err != nil {
		return 0, NewNonRetryableError("failed to parse NZB XML for size calculation", err)
	}

	if len(n.Files) == 0 {
		return 0, NewNonRetryableError("NZB file contains no files", nil)
	}

	var totalSize int64
	par2Pattern := regexp.MustCompile(`(?i)\.par2$|\.p\d+$|\.vol\d+\+\d+\.par2$`)

	for _, file := range n.Files {
		// Skip PAR2 files (same logic as existing parser)
		if par2Pattern.MatchString(file.Filename) {
			continue
		}

		// Sum all segment bytes directly
		for _, segment := range file.Segments {
			totalSize += int64(segment.Bytes)
		}
	}

	return totalSize, nil
}

// calculateStrmFileSize extracts file size from STRM file NXG link
func (s *Service) calculateStrmFileSize(r io.Reader) (int64, error) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" && strings.HasPrefix(line, "nxglnk://") {
			u, err := url.Parse(line)
			if err != nil {
				return 0, NewNonRetryableError("invalid NXG URL in STRM file", err)
			}

			fileSizeStr := u.Query().Get("file_size")
			if fileSizeStr == "" {
				return 0, NewNonRetryableError("missing file_size parameter in NXG link", nil)
			}

			fileSize, err := strconv.ParseInt(fileSizeStr, 10, 64)
			if err != nil {
				return 0, NewNonRetryableError("invalid file_size parameter in NXG link", err)
			}

			return fileSize, nil
		}
	}

	if err := scanner.Err(); err != nil {
		return 0, NewNonRetryableError("failed to read STRM file for size calculation", err)
	}

	return 0, NewNonRetryableError("no valid NXG link found in STRM file", nil)
}

// convertPriorityToSABnzbd converts AltMount queue priority to SABnzbd priority format
func (s *Service) convertPriorityToSABnzbd(priority database.QueuePriority) string {
	switch priority {
	case database.QueuePriorityHigh:
		return "2" // High
	case database.QueuePriorityLow:
		return "0" // Low
	default:
		return "1" // Normal
	}
}

// createSymlinks creates symlinks for an imported file or directory in the category folder
func (s *Service) createSymlinks(item *database.ImportQueueItem, resultingPath string) error {
	cfg := s.configGetter()

	// Check if symlinks are enabled
	if cfg.SABnzbd.SymlinkEnabled == nil || !*cfg.SABnzbd.SymlinkEnabled {
		return nil // Skip if not enabled
	}

	if cfg.SABnzbd.SymlinkDir == nil || *cfg.SABnzbd.SymlinkDir == "" {
		return fmt.Errorf("symlink directory not configured")
	}

	// Get the actual metadata/mount path (where the content actually lives)
	actualPath := filepath.Join(cfg.MountPath, resultingPath)

	// Check the metadata directory to determine if this is a file or directory
	// (Don't use os.Stat on mount path as it might not be immediately available)
	metadataPath := filepath.Join(cfg.Metadata.RootPath, resultingPath)
	fileInfo, err := os.Stat(metadataPath)

	// If stat fails, check if it's a .meta file (single file case)
	if err != nil {
		// Try checking for .meta file
		metaFile := metadataPath + ".meta"
		if _, metaErr := os.Stat(metaFile); metaErr == nil {
			// It's a single file
			return s.createSingleSymlink(actualPath, resultingPath)
		}
		return fmt.Errorf("failed to stat metadata path: %w", err)
	}

	if !fileInfo.IsDir() {
		// Single file - create one symlink
		return s.createSingleSymlink(actualPath, resultingPath)
	}

	// Directory - walk through and create symlinks for all files
	var symlinkErrors []error
	symlinkCount := 0

	// Walk the metadata directory to find all files
	err = filepath.WalkDir(metadataPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			s.log.Warn("Error accessing metadata path during symlink creation",
				"path", path,
				"error", err)
			return nil // Continue walking
		}

		// Skip directories, we only create symlinks for files (.meta files)
		if d.IsDir() {
			return nil
		}

		// Only process .meta files
		if !strings.HasSuffix(d.Name(), ".meta") {
			return nil
		}

		// Calculate relative path from the root metadata directory (not metadataPath)
		relPath, err := filepath.Rel(cfg.Metadata.RootPath, path)
		if err != nil {
			s.log.Error("Failed to calculate relative path",
				"path", path,
				"base", cfg.Metadata.RootPath,
				"error", err)
			return nil // Continue walking
		}

		// Remove .meta extension to get the actual filename
		relPath = strings.TrimSuffix(relPath, ".meta")

		// Build the actual file path in the mount (mount root + virtual path)
		actualFilePath := filepath.Join(cfg.MountPath, relPath)

		// The relPath already IS the full virtual path from root, so use it directly
		fileResultingPath := relPath

		// Create symlink for this file using the helper function
		if err := s.createSingleSymlink(actualFilePath, fileResultingPath); err != nil {
			s.log.Error("Failed to create symlink",
				"path", actualFilePath,
				"error", err)
			symlinkErrors = append(symlinkErrors, err)
			return nil // Continue walking
		}

		symlinkCount++

		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to walk directory: %w", err)
	}

	if len(symlinkErrors) > 0 {
		s.log.Warn("Some symlinks failed to create",
			"queue_id", item.ID,
			"total_errors", len(symlinkErrors),
			"successful", symlinkCount)
		// Don't fail the import, just log the warning
	}

	return nil
}

// createSingleSymlink creates a symlink for a single file
func (s *Service) createSingleSymlink(actualPath, resultingPath string) error {
	cfg := s.configGetter()

	baseDir := filepath.Join(*cfg.SABnzbd.SymlinkDir, filepath.Dir(resultingPath))

	// Ensure category directory exists
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return fmt.Errorf("failed to create symlink category directory: %w", err)
	}

	symlinkPath := filepath.Join(*cfg.SABnzbd.SymlinkDir, resultingPath)

	// Check if symlink already exists
	if _, err := os.Lstat(symlinkPath); err == nil {
		// Symlink exists, remove it first
		if err := os.Remove(symlinkPath); err != nil {
			return fmt.Errorf("failed to remove existing symlink: %w", err)
		}
	}

	// Create the symlink
	if err := os.Symlink(actualPath, symlinkPath); err != nil {
		return fmt.Errorf("failed to create symlink: %w", err)
	}

	return nil
}
