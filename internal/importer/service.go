package importer

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	"github.com/javi11/altmount/internal/arrs"
	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/altmount/internal/metadata"
	"github.com/javi11/altmount/internal/nzbdav"
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

// ImportJobStatus represents the status of an NZBDav import job
type ImportJobStatus string

const (
	ImportStatusIdle      ImportJobStatus = "idle"
	ImportStatusRunning   ImportJobStatus = "running"
	ImportStatusCanceling ImportJobStatus = "canceling"
)

// ImportInfo holds information about the current NZBDav import operation
type ImportInfo struct {
	Status    ImportJobStatus `json:"status"`
	Total     int             `json:"total"`
	Added     int             `json:"added"`
	Failed    int             `json:"failed"`
	LastError *string         `json:"last_error,omitempty"`
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

	// Manual scan state
	scanMu     sync.RWMutex
	scanInfo   ScanInfo
	scanCancel context.CancelFunc

	// Import state
	importMu     sync.RWMutex
	importInfo   ImportInfo
	importCancel context.CancelFunc
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

	// Create processor with poolManager for dynamic pool access
	processor := NewProcessor(metadataService, poolManager, maxImportConnections, segmentSamplePercentage, allowedFileExtensions, importCacheSizeMB, broadcaster)

	ctx, cancel := context.WithCancel(context.Background())

	service := &Service{
		config:          config,
		metadataService: metadataService,
		database:        database,
		processor:       processor,
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
		scanInfo:        ScanInfo{Status: ScanStatusIdle},
		importInfo:      ImportInfo{Status: ImportStatusIdle},
		paused:          false,
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
	if err := s.database.Repository.ResetStaleItems(ctx); err != nil {
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

// Pause pauses the queue processing
func (s *Service) Pause() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.paused = true
	s.log.InfoContext(context.Background(), "Import service paused")
}

// Resume resumes the queue processing
func (s *Service) Resume() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.paused = false
	s.log.InfoContext(context.Background(), "Import service resumed")
}

// IsPaused returns whether the service is paused
func (s *Service) IsPaused() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.paused
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
		s.log.InfoContext(context.Background(), "RClone client updated for VFS notifications")
	} else {
		s.log.InfoContext(context.Background(), "RClone client disabled")
	}
}

// SetArrsService sets or updates the ARRs service
func (s *Service) SetArrsService(service *arrs.Service) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.arrsService = service
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

	s.log.InfoContext(context.Background(), "Manual scan started", "path", scanPath)
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

	s.log.InfoContext(context.Background(), "Manual scan cancellation requested", "path", s.scanInfo.Path)
	return nil
}

// StartNzbdavImport starts an asynchronous import from an NZBDav database
func (s *Service) StartNzbdavImport(dbPath string, rootFolder string, cleanupFile bool) error {
	s.importMu.Lock()
	defer s.importMu.Unlock()

	if s.importInfo.Status != ImportStatusIdle {
		return fmt.Errorf("import already in progress")
	}

	// Create import context
	importCtx, cancel := context.WithCancel(context.Background())
	s.importCancel = cancel

	// Initialize status
	s.importInfo = ImportInfo{
		Status: ImportStatusRunning,
		Total:  0,
		Added:  0,
		Failed: 0,
	}

	go func() {
		// 1. Parse Database
		parser := nzbdav.NewParser(dbPath)
		nzbChan, errChan := parser.Parse()

		defer func() {
			s.importMu.Lock()
			s.importInfo.Status = ImportStatusIdle
			s.importCancel = nil
			s.importMu.Unlock()

			if cleanupFile {
				os.Remove(dbPath)
			}

			// Drain any remaining items from channels to prevent parser goroutine leaks.
			// This ensures the parser can complete even if we exit early due to cancellation.
			go func() {
				for range nzbChan {
				}
			}()
			go func() {
				for range errChan {
				}
			}()
		}()

		// Create temp dir for NZBs
		nzbTempDir, err := os.MkdirTemp(os.TempDir(), "altmount-nzbdav-imports-")
		if err != nil {
			s.log.ErrorContext(importCtx, "Failed to create temp directory for NZBs", "error", err)
			s.importMu.Lock()
			msg := err.Error()
			s.importInfo.LastError = &msg
			s.importMu.Unlock()
			return
		}

		for {
			select {
			case <-importCtx.Done():
				s.log.InfoContext(importCtx, "Import cancelled")
				return // defer will drain remaining channel items
			case res, ok := <-nzbChan:
				if !ok {
					nzbChan = nil
					break
				}

				s.importMu.Lock()
				s.importInfo.Total++
				s.importMu.Unlock()

				// Create Temp NZB File
				nzbFileName := fmt.Sprintf("%s.nzb", sanitizeFilename(res.Name))
				nzbPath := filepath.Join(nzbTempDir, nzbFileName)

				outFile, err := os.Create(nzbPath)
				if err != nil {
					s.log.ErrorContext(importCtx, "Failed to create temp NZB file", "file", nzbFileName, "error", err)
					s.importMu.Lock()
					s.importInfo.Failed++
					s.importMu.Unlock()
					continue
				}

				_, err = io.Copy(outFile, res.Content)
				outFile.Close()
				if err != nil {
					s.log.ErrorContext(importCtx, "Failed to write temp NZB file content", "file", nzbFileName, "error", err)
					s.importMu.Lock()
					s.importInfo.Failed++
					s.importMu.Unlock()
					os.Remove(nzbPath)
					continue
				}

				// Determine Category and Relative Path
				targetCategory := "other"
				lowerCat := strings.ToLower(res.Category)
				if strings.Contains(lowerCat, "movie") {
					targetCategory = "movies"
				} else if strings.Contains(lowerCat, "tv") || strings.Contains(lowerCat, "series") {
					targetCategory = "tv"
				}

				if res.RelPath != "" {
					targetCategory = filepath.Join(targetCategory, res.RelPath)
				}

				relPath := rootFolder
				priority := database.QueuePriorityNormal

				_, err = s.AddToQueue(nzbPath, &relPath, &targetCategory, &priority)
				if err != nil {
					s.log.ErrorContext(importCtx, "Failed to add to queue", "release", res.Name, "error", err)
					s.importMu.Lock()
					s.importInfo.Failed++
					s.importMu.Unlock()
					os.Remove(nzbPath)
				} else {
					s.importMu.Lock()
					s.importInfo.Added++
					s.importMu.Unlock()
				}

			case err := <-errChan:
				if err != nil {
					s.log.ErrorContext(importCtx, "Error during NZBDav parsing", "error", err)
					s.importMu.Lock()
					msg := err.Error()
					s.importInfo.LastError = &msg
					s.importMu.Unlock()
				}
				errChan = nil
			}

			if nzbChan == nil && errChan == nil {
				break
			}
		}
	}()

	return nil
}

// GetImportStatus returns the current import status
func (s *Service) GetImportStatus() ImportInfo {
	s.importMu.RLock()
	defer s.importMu.RUnlock()
	return s.importInfo
}

// CancelImport cancels the current import operation
func (s *Service) CancelImport() error {
	s.importMu.Lock()
	defer s.importMu.Unlock()

	if s.importInfo.Status == ImportStatusIdle {
		return fmt.Errorf("no import is currently running")
	}

	if s.importInfo.Status == ImportStatusCanceling {
		return fmt.Errorf("import is already being canceled")
	}

	s.importInfo.Status = ImportStatusCanceling
	if s.importCancel != nil {
		s.importCancel()
	}

	return nil
}

func sanitizeFilename(name string) string {
	return strings.ReplaceAll(name, "/", "_")
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

	s.log.DebugContext(ctx, "Scanning directory for NZB files", "dir", scanPath)

	err := filepath.WalkDir(scanPath, func(path string, d fs.DirEntry, err error) error {
		// Check for cancellation
		select {
		case <-ctx.Done():
			s.log.InfoContext(ctx, "Scan cancelled", "path", scanPath)
			return fmt.Errorf("scan cancelled")
		default:
		}

		if err != nil {
			s.log.WarnContext(ctx, "Error accessing path", "path", path, "error", err)
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
			s.log.ErrorContext(ctx, "Failed to add file to queue during scan", "file", path, "error", err)
		}

		// Update files added counter
		s.scanMu.Lock()
		s.scanInfo.FilesAdded++
		s.scanMu.Unlock()

		return nil
	})

	if err != nil && !strings.Contains(err.Error(), "scan cancelled") {
		s.log.ErrorContext(ctx, "Failed to scan directory", "dir", scanPath, "error", err)
		s.scanMu.Lock()
		errMsg := err.Error()
		s.scanInfo.LastError = &errMsg
		s.scanMu.Unlock()
	}

	s.log.InfoContext(ctx, "Manual scan completed", "path", scanPath, "files_found", s.scanInfo.FilesFound, "files_added", s.scanInfo.FilesAdded)
}

// isFileAlreadyInQueue checks if file is already in queue (simplified scanning)
func (s *Service) isFileAlreadyInQueue(filePath string) bool {
	// Only check queue database during scanning for performance
	// The processor will check main database for duplicates when processing
	inQueue, err := s.database.Repository.IsFileInQueue(context.Background(), filePath)
	if err != nil {
		s.log.WarnContext(context.Background(), "Failed to check if file in queue", "file", filePath, "error", err)
		return false // Assume not in queue on error
	}
	return inQueue
}

// AddToQueue adds a new NZB file to the import queue with optional category and priority
func (s *Service) AddToQueue(filePath string, relativePath *string, category *string, priority *database.QueuePriority) (*database.ImportQueueItem, error) {
	// Calculate file size before adding to queue
	var fileSize *int64
	if size, err := s.CalculateFileSizeOnly(filePath); err != nil {
		s.log.WarnContext(context.Background(), "Failed to calculate file size", "file", filePath, "error", err)
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

	if err := s.database.Repository.AddToQueue(context.Background(), item); err != nil {
		s.log.ErrorContext(context.Background(), "Failed to add file to queue", "file", filePath, "error", err)
		return nil, err
	}

	if fileSize != nil {
		s.log.InfoContext(context.Background(), "Added NZB file to queue", "file", filePath, "queue_id", item.ID, "file_size", *fileSize)
	} else {
		s.log.InfoContext(context.Background(), "Added NZB file to queue", "file", filePath, "queue_id", item.ID, "file_size", "unknown")
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
			// Check if service is paused
			if s.IsPaused() {
				continue
			}
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
func (s *Service) claimItemWithRetry(ctx context.Context, workerID int) (*database.ImportQueueItem, error) {
	var item *database.ImportQueueItem

	err := retry.Do(
		func() error {
			claimedItem, err := s.database.Repository.ClaimNextQueueItem(ctx)
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
			// Only log warnings after multiple retries to reduce noise
			if n >= 2 {
				s.log.WarnContext(ctx, "Database contention, retrying claim",
					"attempt", n+1,
					"worker_id", workerID,
					"error", err)
			} else {
				s.log.DebugContext(ctx, "Database contention, retrying claim",
					"attempt", n+1,
					"worker_id", workerID,
					"error", err)
			}
		}),
	)

	if err != nil {
		return nil, fmt.Errorf("failed to claim queue item: %w", err)
	}

	if item == nil {
		return nil, nil
	}

	s.log.DebugContext(ctx, "Next item in processing queue", "queue_id", item.ID, "file", item.NzbPath)
	return item, nil
}

// processQueueItems gets and processes pending queue items using two-database workflow
func (s *Service) processQueueItems(ctx context.Context, workerID int) {
	// Step 1: Atomically claim next available item from queue database with retry logic
	item, err := s.claimItemWithRetry(ctx, workerID)
	if err != nil {
		// Only log non-contention errors
		if !strings.Contains(err.Error(), "database is locked") && !strings.Contains(err.Error(), "database is busy") {
			s.log.ErrorContext(ctx, "Failed to claim next queue item", "worker_id", workerID, "error", err)
		}
		return
	}

	if item == nil {
		return // No work to do
	}

	s.log.DebugContext(ctx, "Processing claimed queue item", "worker_id", workerID, "queue_id", item.ID, "file", item.NzbPath)

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

	// Step 3: Process the NZB file and write to main database using cancellable context
	resultingPath, processingErr := s.processNzbItem(itemCtx, item)

	// Step 4: Update queue database with results
	if processingErr != nil {
		// Handle failure in queue database
		s.handleProcessingFailure(ctx, item, processingErr)
	} else {
		// Handle success (storage path, VFS notification, symlinks, status update)
		s.handleProcessingSuccess(ctx, item, resultingPath)
	}
}

// refreshMountPathIfNeeded checks if the mount path exists and refreshes the root directory if not found
func (s *Service) refreshMountPathIfNeeded(ctx context.Context, resultingPath string, itemID int64) {
	if s.rcloneClient == nil {
		return
	}

	mountPath := filepath.Join(s.configGetter().MountPath, filepath.Dir(resultingPath))
	if _, err := os.Stat(mountPath); err != nil {
		if os.IsNotExist(err) {
			// Refresh the root path if the mount path is not found
			err := s.rcloneClient.RefreshDir(s.ctx, config.MountProvider, []string{"/"})
			if err != nil {
				s.log.ErrorContext(ctx, "Failed to refresh mount path", "queue_id", itemID, "path", mountPath, "error", err)
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

	// Determine if allowed extensions override is needed
	var allowedExtensionsOverride *[]string
	if item.Category != nil && strings.ToLower(*item.Category) == "test" {
		emptySlice := []string{}
		allowedExtensionsOverride = &emptySlice // Allow all extensions for test files
	}

	return s.processor.ProcessNzbFile(ctx, item.NzbPath, basePath, int(item.ID), allowedExtensionsOverride)
}

// handleProcessingSuccess handles all steps after successful NZB processing
func (s *Service) handleProcessingSuccess(ctx context.Context, item *database.ImportQueueItem, resultingPath string) error {
	// Add storage path to database
	if err := s.database.Repository.AddStoragePath(ctx, item.ID, resultingPath); err != nil {
		s.log.ErrorContext(ctx, "Failed to add storage path", "queue_id", item.ID, "error", err)
		return err
	}

	// Refresh mount path if needed
	s.refreshMountPathIfNeeded(ctx, resultingPath, item.ID)

	// Notify rclone VFS about the new import (async, don't fail on error)
	s.notifyRcloneVFS(ctx, resultingPath)

	// Create category symlink (non-blocking)
	if err := s.createSymlinks(item, resultingPath); err != nil {
		s.log.WarnContext(ctx, "Failed to create symlink",
			"queue_id", item.ID,
			"path", resultingPath,
			"error", err)

		return err
	}

	// Create STRM files (non-blocking)
	if err := s.createStrmFiles(item, resultingPath); err != nil {
		s.log.WarnContext(ctx, "Failed to create STRM file",
			"queue_id", item.ID,
			"path", resultingPath,
			"error", err)

		return err
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

	// Trigger ARR download scan if applicable
	if s.arrsService != nil && item.Category != nil {
		category := strings.ToLower(*item.Category)
		// Determine instance type based on category
		// Note: This assumes standard "tv" and "movies" categories.
		// Ideally we should match against configured categories in config.SABnzbd.Categories
		if category == "tv" || strings.Contains(category, "tv") || strings.Contains(category, "show") {
			s.arrsService.TriggerDownloadScan(ctx, "sonarr")
		} else if category == "movies" || strings.Contains(category, "movie") {
			s.arrsService.TriggerDownloadScan(ctx, "radarr")
		}
	}

	// Remove any existing health record for this file (in case it was corrupted and now replaced)
	if s.healthRepo != nil {
		// Calculate the mount relative path
		// resultingPath is the virtual path (e.g. "movies/Movie (Year)/Movie.mkv")
		// We can try to delete any health record matching this path
		if err := s.healthRepo.DeleteHealthRecord(ctx, resultingPath); err == nil {
			slog.InfoContext(ctx, "Removed health record for replaced file", "path", resultingPath)
		}
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

	cfg := s.configGetter()
	// Attempt SABnzbd fallback if configured
	if cfg.SABnzbd.FallbackHost != "" && cfg.SABnzbd.FallbackAPIKey != "" {
		if err := s.attemptSABnzbdFallback(ctx, item); err != nil {
			s.log.ErrorContext(ctx, "Failed to send to external SABnzbd",
				"queue_id", item.ID,
				"file", item.NzbPath,
				"fallback_host", s.configGetter().SABnzbd.FallbackHost,
				"error", err)

		} else {
			// Mark item as fallback instead of removing from queue
			if err := s.database.Repository.UpdateQueueItemStatus(ctx, item.ID, database.QueueStatusFallback, nil); err != nil {
				s.log.ErrorContext(ctx, "Failed to mark item as fallback", "queue_id", item.ID, "error", err)
			} else {
				s.log.DebugContext(ctx, "Item marked as fallback after successful SABnzbd transfer",
					"queue_id", item.ID,
					"file", item.NzbPath,
					"fallback_host", s.configGetter().SABnzbd.FallbackHost)
			}
		}
	}
}

// attemptSABnzbdFallback attempts to send a failed import to an external SABnzbd instance
func (s *Service) attemptSABnzbdFallback(ctx context.Context, item *database.ImportQueueItem) error {
	cfg := s.configGetter()

	// Check if the NZB file still exists
	if _, err := os.Stat(item.NzbPath); err != nil {
		s.log.WarnContext(ctx, "SABnzbd fallback not attempted - NZB file not found",
			"queue_id", item.ID,
			"file", item.NzbPath,
			"error", err)
		return err
	}

	s.log.InfoContext(ctx, "Attempting to send failed import to external SABnzbd",
		"queue_id", item.ID,
		"file", item.NzbPath,
		"fallback_host", cfg.SABnzbd.FallbackHost)

	// Convert priority to SABnzbd format
	priority := s.convertPriorityToSABnzbd(item.Priority)

	// Send to external SABnzbd
	nzoID, err := s.sabnzbdClient.SendNZBFile(
		ctx,
		cfg.SABnzbd.FallbackHost,
		cfg.SABnzbd.FallbackAPIKey,
		item.NzbPath,
		item.Category,
		&priority,
	)
	if err != nil {
		return err
	}

	s.log.InfoContext(ctx, "Successfully sent failed import to external SABnzbd",
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

	s.log.InfoContext(context.Background(), "Queue worker count update requested - restart required to take effect",
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
	s.cancelMu.RLock()
	cancel, exists := s.cancelFuncs[itemID]
	s.cancelMu.RUnlock()

	if !exists {
		return fmt.Errorf("item %d is not currently processing", itemID)
	}

	s.log.InfoContext(context.Background(), "Cancelling processing for queue item", "item_id", itemID)
	cancel()
	return nil
}

// notifyRcloneVFS notifies rclone VFS about a new import (async, non-blocking)
func (s *Service) notifyRcloneVFS(ctx context.Context, resultingPath string) {
	if s.rcloneClient == nil {
		return // No rclone client configured or RClone RC is disabled
	}

	refreshCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second) // 10 second timeout
	defer cancel()

	err := s.rcloneClient.RefreshDir(refreshCtx, config.MountProvider, []string{resultingPath}) // Use RefreshDir with empty provider
	if err != nil {
		s.log.WarnContext(ctx, "Failed to notify rclone VFS about new import",
			"virtual_dir", resultingPath,
			"error", err)
	} else {
		s.log.InfoContext(ctx, "Successfully notified rclone VFS about new import",
			"virtual_dir", resultingPath)
	}
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
	if cfg.Import.ImportStrategy != config.ImportStrategySYMLINK {
		return nil // Skip if not enabled
	}

	if cfg.Import.ImportDir == nil || *cfg.Import.ImportDir == "" {
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
			s.log.WarnContext(context.Background(), "Error accessing metadata path during symlink creation",
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
			s.log.ErrorContext(context.Background(), "Failed to calculate relative path",
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
			s.log.ErrorContext(context.Background(), "Failed to create symlink",
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
		s.log.WarnContext(context.Background(), "Some symlinks failed to create",
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

	baseDir := filepath.Join(*cfg.Import.ImportDir, filepath.Dir(resultingPath))

	// Ensure category directory exists
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return fmt.Errorf("failed to create symlink category directory: %w", err)
	}

	symlinkPath := filepath.Join(*cfg.Import.ImportDir, resultingPath)

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

// createStrmFiles creates STRM files for an imported file or directory
func (s *Service) createStrmFiles(item *database.ImportQueueItem, resultingPath string) error {
	cfg := s.configGetter()

	// Check if STRM is enabled
	if cfg.Import.ImportStrategy != config.ImportStrategySTRM {
		return nil // Skip if not enabled
	}

	if cfg.Import.ImportDir == nil || *cfg.Import.ImportDir == "" {
		return fmt.Errorf("STRM directory not configured")
	}

	// Check the metadata directory to determine if this is a file or directory
	metadataPath := filepath.Join(cfg.Metadata.RootPath, resultingPath)
	fileInfo, err := os.Stat(metadataPath)

	// If stat fails, check if it's a .meta file (single file case)
	if err != nil {
		// Try checking for .meta file
		metaFile := metadataPath + ".meta"
		if _, metaErr := os.Stat(metaFile); metaErr == nil {
			// It's a single file
			return s.createSingleStrmFile(resultingPath, cfg.WebDAV.Port)
		}
		return fmt.Errorf("failed to stat metadata path: %w", err)
	}

	if !fileInfo.IsDir() {
		// Single file - create one STRM file
		return s.createSingleStrmFile(resultingPath, cfg.WebDAV.Port)
	}

	// Directory - walk through and create STRM files for all files
	var strmErrors []error
	strmCount := 0

	// Walk the metadata directory to find all files
	err = filepath.WalkDir(metadataPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			s.log.WarnContext(context.Background(), "Error accessing metadata path during STRM creation",
				"path", path,
				"error", err)
			return nil // Continue walking
		}

		// Skip directories, we only create STRM files for files (.meta files)
		if d.IsDir() {
			return nil
		}

		// Only process .meta files
		if !strings.HasSuffix(d.Name(), ".meta") {
			return nil
		}

		// Calculate relative path from the root metadata directory
		relPath, err := filepath.Rel(cfg.Metadata.RootPath, path)
		if err != nil {
			s.log.ErrorContext(context.Background(), "Failed to calculate relative path",
				"path", path,
				"base", cfg.Metadata.RootPath,
				"error", err)
			return nil // Continue walking
		}

		// Remove .meta extension to get the actual filename
		relPath = strings.TrimSuffix(relPath, ".meta")

		// Create STRM file for this file
		if err := s.createSingleStrmFile(relPath, cfg.WebDAV.Port); err != nil {
			s.log.ErrorContext(context.Background(), "Failed to create STRM file",
				"path", relPath,
				"error", err)
			strmErrors = append(strmErrors, err)
			return nil // Continue walking
		}

		strmCount++

		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to walk directory: %w", err)
	}

	if len(strmErrors) > 0 {
		s.log.WarnContext(context.Background(), "Some STRM files failed to create",
			"queue_id", item.ID,
			"total_errors", len(strmErrors),
			"successful", strmCount)
		// Don't fail the import, just log the warning
	}

	return nil
}

// createSingleStrmFile creates a STRM file for a single file with authentication
func (s *Service) createSingleStrmFile(virtualPath string, port int) error {
	ctx := context.Background()
	cfg := s.configGetter()

	baseDir := filepath.Join(*cfg.Import.ImportDir, filepath.Dir(virtualPath))

	// Ensure directory exists
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return fmt.Errorf("failed to create STRM directory: %w", err)
	}

	// Keep original filename and add .strm extension
	filename := filepath.Base(virtualPath)
	filename = filename + ".strm"

	strmPath := filepath.Join(*cfg.Import.ImportDir, filepath.Dir(virtualPath), filename)

	// Get first admin user's API key for authentication
	users, err := s.userRepo.GetAllUsers(ctx)
	if err != nil || len(users) == 0 {
		return fmt.Errorf("no users with API keys found for STRM generation: %w", err)
	}

	// Find first admin user with an API key
	var adminAPIKey string
	for _, user := range users {
		if user.IsAdmin && user.APIKey != nil && *user.APIKey != "" {
			adminAPIKey = *user.APIKey
			break
		}
	}

	if adminAPIKey == "" {
		return fmt.Errorf("no admin user with API key found for STRM generation")
	}

	// Hash the API key with SHA256
	hashedKey := hashAPIKey(adminAPIKey)

	// Generate streaming URL with download_key
	// URL encode the path to handle special characters
	encodedPath := strings.ReplaceAll(virtualPath, " ", "%20")
	streamURL := fmt.Sprintf("http://localhost:%d/api/files/stream?path=%s&download_key=%s",
		port, encodedPath, hashedKey)

	// Check if STRM file already exists with the same content
	if existingContent, err := os.ReadFile(strmPath); err == nil {
		if string(existingContent) == streamURL {
			// File exists with correct content, skip
			return nil
		}
	}

	// Write the STRM file
	if err := os.WriteFile(strmPath, []byte(streamURL), 0644); err != nil {
		return fmt.Errorf("failed to write STRM file: %w", err)
	}

	return nil
}

// hashAPIKey generates a SHA256 hash of the API key for secure comparison
func hashAPIKey(apiKey string) string {
	hash := sha256.Sum256([]byte(apiKey))
	return hex.EncodeToString(hash[:])
}
