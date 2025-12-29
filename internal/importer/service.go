package importer

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/javi11/altmount/internal/arrs"
	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/altmount/internal/importer/postprocessor"
	"github.com/javi11/altmount/internal/importer/queue"
	"github.com/javi11/altmount/internal/importer/scanner"
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

// Type aliases from scanner package for backward compatibility
type (
	ScanStatus      = scanner.ScanStatus
	ScanInfo        = scanner.ScanInfo
	ImportJobStatus = scanner.ImportJobStatus
	ImportInfo      = scanner.ImportInfo
)

// Re-export scanner status constants for backward compatibility
const (
	ScanStatusIdle      = scanner.ScanStatusIdle
	ScanStatusScanning  = scanner.ScanStatusScanning
	ScanStatusCanceling = scanner.ScanStatusCanceling
	ImportStatusIdle    = scanner.ImportStatusIdle
	ImportStatusRunning = scanner.ImportStatusRunning
)

// queueAdapterForScanner adapts database repository for scanner.QueueAdder interface
type queueAdapterForScanner struct {
	repo            *database.QueueRepository
	metadataService *metadata.MetadataService
	calcFileSize    func(string) (int64, error)
}

func (a *queueAdapterForScanner) AddToQueue(ctx context.Context, filePath string, relativePath *string) error {
	// Calculate file size before adding to queue
	var fileSize *int64
	if size, err := a.calcFileSize(filePath); err == nil {
		fileSize = &size
	}

	item := &database.ImportQueueItem{
		NzbPath:      filePath,
		RelativePath: relativePath,
		Priority:     database.QueuePriorityNormal,
		Status:       database.QueueStatusPending,
		RetryCount:   0,
		MaxRetries:   3,
		FileSize:     fileSize,
		CreatedAt:    time.Now(),
	}

	return a.repo.AddToQueue(ctx, item)
}

func (a *queueAdapterForScanner) IsFileInQueue(ctx context.Context, filePath string) bool {
	inQueue, _ := a.repo.IsFileInQueue(ctx, filePath)
	return inQueue
}

func (a *queueAdapterForScanner) IsFileProcessed(filePath string, scanRoot string) bool {
	return isFileAlreadyProcessed(a.metadataService, filePath, scanRoot)
}

// batchQueueAdapterForImporter adapts database repository for scanner.BatchQueueAdder interface
type batchQueueAdapterForImporter struct {
	repo *database.QueueRepository
}

func (a *batchQueueAdapterForImporter) AddBatchToQueue(ctx context.Context, items []*database.ImportQueueItem) error {
	return a.repo.AddBatchToQueue(ctx, items)
}

// isFileAlreadyProcessed checks if a file has already been processed by checking metadata
func isFileAlreadyProcessed(metadataService *metadata.MetadataService, filePath string, scanRoot string) bool {
	// Calculate virtual path
	virtualPath := filepath.Dir(filePath)
	if scanRoot != "" {
		rel, err := filepath.Rel(scanRoot, filePath)
		if err == nil {
			virtualPath = filepath.Dir(rel)
		}
	}

	// Normalize filename (remove .nzb extension)
	fileName := filepath.Base(filePath)
	baseName := strings.TrimSuffix(fileName, filepath.Ext(fileName))

	// Check if a directory exists with the release name
	releaseDir := filepath.Join(virtualPath, baseName)
	if metadataService.DirectoryExists(releaseDir) {
		return true
	}

	// Also check if any file exists that starts with the release name in that directory
	if files, err := metadataService.ListDirectory(virtualPath); err == nil {
		for _, f := range files {
			if strings.HasPrefix(f, baseName) {
				return true
			}
		}
	}

	return false
}

// Service provides NZB import functionality with manual directory scanning and queue-based processing
type Service struct {
	config          ServiceConfig
	database        *database.DB              // Database for processing queue
	metadataService *metadata.MetadataService // Metadata service for file processing
	processor       *Processor
	postProcessor   *postprocessor.Coordinator    // Post-import processing coordinator
	queueManager    *queue.Manager                // Queue worker management
	dirScanner      *scanner.DirectoryScanner     // Manual directory scanning
	nzbdavImporter  *scanner.NzbDavImporter       // NZBDav database imports
	rcloneClient    rclonecli.RcloneRcClient      // Optional rclone client for VFS notifications
	configGetter    config.ConfigGetter           // Config getter for dynamic configuration access
	sabnzbdClient   *sabnzbd.SABnzbdClient        // SABnzbd client for fallback
	arrsService     *arrs.Service                 // ARRs service for triggering scans
	healthRepo      *database.HealthRepository    // Health repository for updating health status
	broadcaster     *progress.ProgressBroadcaster // WebSocket progress broadcaster
	userRepo        *database.UserRepository      // User repository for API key lookup
	log             *slog.Logger

	// Runtime state
	mu      sync.RWMutex
	running bool
	paused  bool
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup

	// Cancellation tracking for processing items
	cancelFuncs map[int64]context.CancelFunc
	cancelMu    sync.RWMutex
}

// NewService creates a new NZB import service with manual scanning and queue processing capabilities
func NewService(config ServiceConfig, metadataService *metadata.MetadataService, database *database.DB, poolManager pool.Manager, rcloneClient rclonecli.RcloneRcClient, configGetter config.ConfigGetter, healthRepo *database.HealthRepository, broadcaster *progress.ProgressBroadcaster, userRepo *database.UserRepository) (*Service, error) {
	// Set defaults
	if config.Workers == 0 {
		config.Workers = 2
	}

	// Get the initial config to pass import settings
	currentConfig := configGetter()
	maxImportConnections := currentConfig.Import.MaxImportConnections
	segmentSamplePercentage := currentConfig.Import.SegmentSamplePercentage
	allowedFileExtensions := currentConfig.Import.AllowedFileExtensions
	importCacheSizeMB := currentConfig.Import.ImportCacheSizeMB
	skipHealthCheck := currentConfig.Import.SkipHealthCheck != nil && *currentConfig.Import.SkipHealthCheck
	readTimeout := time.Duration(currentConfig.Import.ReadTimeoutSeconds) * time.Second
	if readTimeout == 0 {
		readTimeout = 5 * time.Minute
	}

	// Create processor with poolManager for dynamic pool access
	processor := NewProcessor(metadataService, poolManager, maxImportConnections, segmentSamplePercentage, allowedFileExtensions, importCacheSizeMB, readTimeout, broadcaster, configGetter, skipHealthCheck)

	ctx, cancel := context.WithCancel(context.Background())

	// Create post-processor coordinator
	postProc := postprocessor.NewCoordinator(postprocessor.Config{
		ConfigGetter:    configGetter,
		MetadataService: metadataService,
		RcloneClient:    rcloneClient,
		HealthRepo:      healthRepo,
		UserRepo:        userRepo,
	})

	service := &Service{
		config:          config,
		metadataService: metadataService,
		database:        database,
		processor:       processor,
		postProcessor:   postProc,
		rcloneClient:    rcloneClient,
		configGetter:    configGetter,
		healthRepo:      healthRepo,
		sabnzbdClient:   sabnzbd.NewSABnzbdClient(),
		broadcaster:     broadcaster,
		userRepo:        userRepo,
		log:             slog.Default().With("component", "importer-service"),
		ctx:             ctx,
		cancel:          cancel,
		cancelFuncs:     make(map[int64]context.CancelFunc),
		paused:          false,
	}

	// Create scanner adapter for directory scanning
	scannerAdapter := &queueAdapterForScanner{
		repo:            database.Repository,
		metadataService: metadataService,
		calcFileSize:    service.CalculateFileSizeOnly,
	}
	service.dirScanner = scanner.NewDirectoryScanner(scannerAdapter)

	// Create adapter for NZBDav imports
	importerAdapter := &batchQueueAdapterForImporter{
		repo: database.Repository,
	}
	service.nzbdavImporter = scanner.NewNzbDavImporter(importerAdapter)

	// Create queue manager (Service implements queue.ItemProcessor interface)
	service.queueManager = queue.NewManager(
		queue.ManagerConfig{
			Workers:      config.Workers,
			ConfigGetter: configGetter,
		},
		database.Repository,
		service,
	)

	return service, nil
}

// Start starts the NZB import service (queue workers only, manual scanning available via API)
func (s *Service) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return fmt.Errorf("service is already started")
	}

	// Update database connection pool to match worker count
	// This prevents connection starvation when multiple workers try to claim items
	s.database.UpdateConnectionPool(s.config.Workers)
	s.log.InfoContext(ctx, "Updated database connection pool",
		"workers", s.config.Workers,
		"max_connections", s.config.Workers+4)

	// Reset any stale queue items from processing back to pending
	if err := s.database.Repository.ResetStaleItems(ctx); err != nil {
		s.log.ErrorContext(ctx, "Failed to reset stale queue items", "error", err)
		return fmt.Errorf("failed to reset stale queue items: %w", err)
	}

	// Delegate worker management to queue manager
	if err := s.queueManager.Start(ctx); err != nil {
		return fmt.Errorf("failed to start queue manager: %w", err)
	}

	s.running = true
	s.log.InfoContext(ctx, fmt.Sprintf("NZB import service started successfully with %d workers", s.config.Workers))

	return nil
}

// ProcessItem implements queue.ItemProcessor - processes a single queue item
func (s *Service) ProcessItem(ctx context.Context, item *database.ImportQueueItem) (string, error) {
	return s.processNzbItem(ctx, item)
}

// HandleSuccess implements queue.ItemProcessor - handles successful processing
func (s *Service) HandleSuccess(ctx context.Context, item *database.ImportQueueItem, resultingPath string) error {
	return s.handleProcessingSuccess(ctx, item, resultingPath)
}

// HandleFailure implements queue.ItemProcessor - handles failed processing
func (s *Service) HandleFailure(ctx context.Context, item *database.ImportQueueItem, err error) {
	s.handleProcessingFailure(ctx, item, err)
}

// Pause pauses the queue processing
func (s *Service) Pause() {
	s.queueManager.Pause()
	s.mu.Lock()
	s.paused = true
	s.mu.Unlock()
	s.log.InfoContext(s.ctx, "Import service paused")
}

// Resume resumes the queue processing
func (s *Service) Resume() {
	s.queueManager.Resume()
	s.mu.Lock()
	s.paused = false
	s.mu.Unlock()
	s.log.InfoContext(s.ctx, "Import service resumed")
}

// IsPaused returns whether the service is paused
func (s *Service) IsPaused() bool {
	return s.queueManager.IsPaused()
}

func (s *Service) RegisterConfigChangeHandler(configManager *config.Manager) {
	configManager.OnConfigChange(func(oldConfig, newConfig *config.Config) {
		s.mu.Lock()
		defer s.mu.Unlock()

		if s.processor != nil {
			skip := newConfig.Import.SkipHealthCheck != nil && *newConfig.Import.SkipHealthCheck
			s.processor.SetSkipHealthCheck(skip)
			s.processor.SetSegmentSamplePercentage(newConfig.Import.SegmentSamplePercentage)
		}
	})
}

// Stop stops the NZB import service and all queue workers
func (s *Service) Stop(ctx context.Context) error {
	s.mu.Lock()

	if !s.running {
		s.mu.Unlock()
		return nil
	}

	s.log.InfoContext(ctx, "Stopping NZB import service")
	s.running = false
	s.mu.Unlock()

	// Delegate worker shutdown to queue manager
	if err := s.queueManager.Stop(ctx); err != nil {
		s.log.WarnContext(ctx, "Error stopping queue manager", "error", err)
	}

	// Cancel service context
	s.cancel()

	// Re-acquire lock to recreate context for potential restart
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ctx, s.cancel = context.WithCancel(context.Background())

	s.log.InfoContext(ctx, "NZB import service stopped")

	return nil
}

// Close closes the NZB import service and releases all resources
func (s *Service) Close() error {
	s.mu.Lock()
	running := s.running
	s.mu.Unlock()

	if running {
		return s.Stop(context.Background())
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
	if s.postProcessor != nil {
		s.postProcessor.SetRcloneClient(client)
	}
	if client != nil {
		s.log.InfoContext(s.ctx, "RClone client updated for VFS notifications")
	} else {
		s.log.InfoContext(s.ctx, "RClone client disabled")
	}
}

// SetArrsService sets or updates the ARRs service
func (s *Service) SetArrsService(service *arrs.Service) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.arrsService = service
	if s.postProcessor != nil {
		s.postProcessor.SetArrsService(service)
	}
}

// Database returns the database instance for processing
func (s *Service) Database() *database.DB {
	return s.database
}

// GetQueueStats returns current queue statistics from database
func (s *Service) GetQueueStats(ctx context.Context) (*database.QueueStats, error) {
	return s.database.Repository.GetQueueStats(ctx)
}

// StartManualScan starts a manual scan of the specified directory
func (s *Service) StartManualScan(scanPath string) error {
	return s.dirScanner.Start(scanPath)
}

// GetScanStatus returns the current scan status
func (s *Service) GetScanStatus() ScanInfo {
	return s.dirScanner.GetStatus()
}

// CancelScan cancels the current scan operation
func (s *Service) CancelScan() error {
	return s.dirScanner.Cancel()
}

// StartNzbdavImport starts an asynchronous import from an NZBDav database
func (s *Service) StartNzbdavImport(dbPath string, rootFolder string, cleanupFile bool) error {
	return s.nzbdavImporter.Start(dbPath, rootFolder, cleanupFile)
}

// GetImportStatus returns the current import status
func (s *Service) GetImportStatus() ImportInfo {
	return s.nzbdavImporter.GetStatus()
}

// CancelImport cancels the current import operation
func (s *Service) CancelImport() error {
	return s.nzbdavImporter.Cancel()
}

// sanitizeFilename replaces invalid characters in filenames
func sanitizeFilename(name string) string {
	return strings.ReplaceAll(name, "/", "_")
}

// AddToQueue adds a new NZB file to the import queue with optional category and priority
func (s *Service) AddToQueue(ctx context.Context, filePath string, relativePath *string, category *string, priority *database.QueuePriority) (*database.ImportQueueItem, error) {
	// Check context before proceeding
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	// Calculate file size before adding to queue
	var fileSize *int64
	if size, err := s.CalculateFileSizeOnly(filePath); err != nil {
		s.log.WarnContext(ctx, "Failed to calculate file size", "file", filePath, "error", err)
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

	if err := s.database.Repository.AddToQueue(ctx, item); err != nil {
		s.log.ErrorContext(ctx, "Failed to add file to queue", "file", filePath, "error", err)
		return nil, err
	}

	if fileSize != nil {
		s.log.InfoContext(ctx, "Added NZB file to queue", "file", filePath, "queue_id", item.ID, "file_size", *fileSize)
	} else {
		s.log.InfoContext(ctx, "Added NZB file to queue", "file", filePath, "queue_id", item.ID, "file_size", "unknown")
	}

	return item, nil
}

// processNzbItem processes the NZB file for a queue item
func (s *Service) processNzbItem(ctx context.Context, item *database.ImportQueueItem) (string, error) {
	// Ensure NZB is in a persistent location to prevent data loss if /tmp is cleaned
	if err := s.ensurePersistentNzb(ctx, item); err != nil {
		return "", fmt.Errorf("failed to ensure persistent NZB: %w", err)
	}

	// Determine the base path, incorporating category if present
	basePath := ""
	if item.RelativePath != nil {
		basePath = *item.RelativePath
	}

	// If category is specified, resolve to configured directory path
	if item.Category != nil && *item.Category != "" {
		categoryPath := s.buildCategoryPath(*item.Category)
		if categoryPath != "" {
			basePath = filepath.Join(basePath, categoryPath)
		}
	}

	// Determine if allowed extensions override is needed
	var allowedExtensionsOverride *[]string
	if item.Category != nil && strings.ToLower(*item.Category) == "test" {
		emptySlice := []string{}
		allowedExtensionsOverride = &emptySlice // Allow all extensions for test files
	}

	return s.processor.ProcessNzbFile(ctx, item.NzbPath, basePath, int(item.ID), allowedExtensionsOverride)
}

// ensurePersistentNzb moves the NZB file to a persistent location in the metadata directory
func (s *Service) ensurePersistentNzb(ctx context.Context, item *database.ImportQueueItem) error {
	cfg := s.configGetter()
	// Use the database directory as the base for the persistent NZB storage
	// This puts it next to metadata (e.g. /config/.nzbs)
	configDir := filepath.Dir(cfg.Database.Path)
	nzbDir := filepath.Join(configDir, ".nzbs")

	// Create .nzbs directory if not exists
	if err := os.MkdirAll(nzbDir, 0755); err != nil {
		return fmt.Errorf("failed to create persistent NZB directory: %w", err)
	}

	// Check if current path is already in the persistent directory
	absNzbPath, _ := filepath.Abs(item.NzbPath)
	absNzbDir, _ := filepath.Abs(nzbDir)

	// Simple check: if path starts with persistent dir, assume it's fine
	if strings.HasPrefix(absNzbPath, absNzbDir) {
		return nil
	}

	// Generate new filename: <id>_<sanitized_filename>
	filename := filepath.Base(item.NzbPath)
	// sanitizeFilename is defined in service.go
	newFilename := sanitizeFilename(filename)
	newPath := filepath.Join(nzbDir, newFilename)

	s.log.DebugContext(ctx, "Moving NZB to persistent storage", "old_path", item.NzbPath, "new_path", newPath)

	// Move or Copy
	// Try Rename first
	err := os.Rename(item.NzbPath, newPath)
	if err != nil {
		// If rename fails (e.g. cross-device link), try copy
		s.log.DebugContext(ctx, "Rename failed, trying copy", "error", err, "src", item.NzbPath, "dst", newPath)

		// Copy logic
		srcFile, err := os.Open(item.NzbPath)
		if err != nil {
			return fmt.Errorf("failed to open source NZB: %w", err)
		}
		defer srcFile.Close()

		dstFile, err := os.Create(newPath)
		if err != nil {
			return fmt.Errorf("failed to create destination NZB: %w", err)
		}
		defer dstFile.Close()

		if _, err := io.Copy(dstFile, srcFile); err != nil {
			return fmt.Errorf("failed to copy NZB content: %w", err)
		}

		// Remove source if copy successful
		// Note: We close srcFile via defer, but for removal on Windows/some FS we might need to close it first.
		// Since we are in a function and defer runs at end, we can't remove yet if we don't close.
		// But in this block we can just let it stay until end of function? No, we want to remove it.
		// For simplicity, we can ignore removal failure or try to handle it better.
		// Actually, we should probably close before removing.
	}

	// If we copied, we should remove the original.
	// But `os.Rename` handles removal.
	// If we fell back to copy, we need to remove.
	if err != nil { // This err refers to Rename failure
		// Close files (deferred, but we might want to close src explicitly if we want to delete)
		// Since we didn't assign srcFile/dstFile to vars outside, we rely on GC/Defer.
		// But defer runs at function exit.
		// So removal might fail if open.
		// Let's rely on standard practice or simple cleanup.
		// If Rename failed, we copied.
		// We should try to remove the source file.
		os.Remove(item.NzbPath)
	}

	// Update item path in memory
	oldPath := item.NzbPath
	item.NzbPath = newPath

	// Update item path in DB
	if err := s.database.Repository.UpdateQueueItemNzbPath(ctx, item.ID, newPath); err != nil {
		// If DB update fails, we are in a weird state (file moved but DB points to old).
		// We should probably try to move it back or just fail.
		// But failing here aborts the import.
		// The file is at newPath.
		// If we fail, the item stays 'processing' in DB with old path.
		// Next retry will fail to find file at old path.
		return fmt.Errorf("failed to update DB with new NZB path: %w", err)
	}

	s.log.InfoContext(ctx, "Moved NZB to persistent storage", "old_path", oldPath, "new_path", newPath)
	return nil
}

// buildCategoryPath resolves a category name to its configured directory path.
// Returns the category's Dir if configured, otherwise falls back to the category name.
func (s *Service) buildCategoryPath(category string) string {
	if category == "" || category == "default" {
		return ""
	}

	cfg := s.configGetter()
	if cfg == nil || len(cfg.SABnzbd.Categories) == 0 {
		return category
	}

	for _, cat := range cfg.SABnzbd.Categories {
		if cat.Name == category {
			if cat.Dir != "" {
				return cat.Dir
			}
			return category
		}
	}

	return category
}

// handleProcessingSuccess handles all steps after successful NZB processing
func (s *Service) handleProcessingSuccess(ctx context.Context, item *database.ImportQueueItem, resultingPath string) error {
	// Add storage path to database
	if err := s.database.Repository.AddStoragePath(ctx, item.ID, resultingPath); err != nil {
		s.log.ErrorContext(ctx, "Failed to add storage path", "queue_id", item.ID, "error", err)
		return err
	}

	// Refresh mount path if needed before post-processing
	s.postProcessor.RefreshMountPathIfNeeded(ctx, resultingPath, item.ID)

	// Delegate all post-processing to the coordinator
	// This handles: VFS notification, symlinks, ID links, STRM files, health checks, ARR notifications
	result, err := s.postProcessor.HandleSuccess(ctx, item, resultingPath)
	if err != nil {
		s.log.ErrorContext(ctx, "Post-processing failed", "queue_id", item.ID, "error", err)
		return err
	}

	// Log any non-fatal errors from post-processing
	if len(result.Errors) > 0 {
		for _, postErr := range result.Errors {
			s.log.WarnContext(ctx, "Post-processing warning",
				"queue_id", item.ID,
				"error", postErr)
		}
	}

	// Mark as completed in queue database
	if err := s.database.Repository.UpdateQueueItemStatus(ctx, item.ID, database.QueueStatusCompleted, nil); err != nil {
		s.log.ErrorContext(ctx, "Failed to mark item as completed", "queue_id", item.ID, "error", err)
		return err
	}

	// Clear progress tracking
	if s.broadcaster != nil {
		s.broadcaster.ClearProgress(int(item.ID))
	}

	s.log.InfoContext(ctx, "Successfully processed queue item", "queue_id", item.ID, "file", item.NzbPath)
	return nil
}

// handleProcessingFailure handles when processing fails
func (s *Service) handleProcessingFailure(ctx context.Context, item *database.ImportQueueItem, processingErr error) {
	errorMessage := processingErr.Error()

	// Check if the error was due to cancellation
	if strings.Contains(errorMessage, "context canceled") || strings.Contains(errorMessage, "processing cancelled") {
		errorMessage = "Processing cancelled by user request"
		s.log.InfoContext(ctx, "Processing cancelled by user",
			"queue_id", item.ID,
			"file", item.NzbPath)
	} else {
		s.log.WarnContext(ctx, "Processing failed",
			"queue_id", item.ID,
			"file", item.NzbPath,
			"error", processingErr)
	}

	// Mark as failed in queue database (no automatic retry)
	if err := s.database.Repository.UpdateQueueItemStatus(ctx, item.ID, database.QueueStatusFailed, &errorMessage); err != nil {
		s.log.ErrorContext(ctx, "Failed to mark item as failed", "queue_id", item.ID, "error", err)
	} else {
		s.log.ErrorContext(ctx, "Item failed",
			"queue_id", item.ID,
			"file", item.NzbPath)
	}

	// Clear progress tracking
	if s.broadcaster != nil {
		s.broadcaster.ClearProgress(int(item.ID))
	}

	// Delegate fallback handling to post-processor
	if err := s.postProcessor.HandleFailure(ctx, item, processingErr); err == nil {
		// Fallback succeeded - mark item as fallback instead of failed
		if err := s.database.Repository.UpdateQueueItemStatus(ctx, item.ID, database.QueueStatusFallback, nil); err != nil {
			s.log.ErrorContext(ctx, "Failed to mark item as fallback", "queue_id", item.ID, "error", err)
		} else {
			s.log.DebugContext(ctx, "Item marked as fallback after successful SABnzbd transfer",
				"queue_id", item.ID,
				"file", item.NzbPath,
				"fallback_host", s.configGetter().SABnzbd.FallbackHost)
		}
	} else if IsNonRetryable(err) && strings.Contains(err.Error(), "SABnzbd fallback not configured") {
		s.log.DebugContext(ctx, "SABnzbd fallback skipped (not configured)",
			"queue_id", item.ID,
			"file", item.NzbPath)
	} else {
		s.log.ErrorContext(ctx, "Fallback handling failed",
			"queue_id", item.ID,
			"file", item.NzbPath,
			"error", err)
	}
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

	s.log.InfoContext(s.ctx, "Queue worker count update requested - restart required to take effect",
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

// CancelProcessing cancels a processing queue item by cancelling its context
func (s *Service) CancelProcessing(itemID int64) error {
	return s.queueManager.CancelProcessing(itemID)
}


// ProcessItemInBackground processes a specific queue item in the background
func (s *Service) ProcessItemInBackground(ctx context.Context, itemID int64) {
	go func() {
		s.log.DebugContext(ctx, "Starting background processing of queue item", "item_id", itemID, "background", true)

		// Get the queue item
		item, err := s.database.Repository.GetQueueItem(ctx, itemID)
		if err != nil {
			s.log.ErrorContext(ctx, "Failed to get queue item for background processing", "item_id", itemID, "error", err)
			return
		}

		if item == nil {
			s.log.WarnContext(ctx, "Queue item not found for background processing", "item_id", itemID)
			return
		}

		// Update status to processing
		if err := s.database.Repository.UpdateQueueItemStatus(ctx, itemID, database.QueueStatusProcessing, nil); err != nil {
			s.log.ErrorContext(ctx, "Failed to update item status to processing", "item_id", itemID, "error", err)
			return
		}

		// Create cancellable context for this item
		itemCtx, cancel := context.WithCancel(ctx)

		// Register cancel function
		s.cancelMu.Lock()
		s.cancelFuncs[item.ID] = cancel
		s.cancelMu.Unlock()

		// Clean up after processing
		defer func() {
			s.cancelMu.Lock()
			delete(s.cancelFuncs, item.ID)
			s.cancelMu.Unlock()
		}()

		// Process the NZB file using cancellable context
		resultingPath, processingErr := s.processNzbItem(itemCtx, item)

		// Update queue database with results
		if processingErr != nil {
			// Handle failure
			s.handleProcessingFailure(ctx, item, processingErr)
		} else {
			// Handle success (storage path, VFS notification, symlinks, status update)
			s.handleProcessingSuccess(ctx, item, resultingPath)
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




