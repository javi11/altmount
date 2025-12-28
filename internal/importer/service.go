package importer

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"math/rand"
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
	"github.com/javi11/altmount/internal/importer/filesystem"
	"github.com/javi11/altmount/internal/metadata"
	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/javi11/altmount/internal/nzbdav"
	"github.com/javi11/altmount/internal/pool"
	"github.com/javi11/altmount/internal/progress"
	"github.com/javi11/altmount/internal/sabnzbd"
	"github.com/javi11/altmount/pkg/rclonecli"
	"github.com/javi11/nzbparser"
	"google.golang.org/protobuf/proto"
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
	skipHealthCheck := currentConfig.Import.SkipHealthCheck != nil && *currentConfig.Import.SkipHealthCheck
	readTimeout := time.Duration(currentConfig.Import.ReadTimeoutSeconds) * time.Second
	if readTimeout == 0 {
		readTimeout = 5 * time.Minute
	}

	// Create processor with poolManager for dynamic pool access
	processor := NewProcessor(metadataService, poolManager, maxImportConnections, segmentSamplePercentage, allowedFileExtensions, importCacheSizeMB, readTimeout, broadcaster, configGetter, skipHealthCheck)

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
	s.log.InfoContext(s.ctx, "Import service paused")
}

// Resume resumes the queue processing
func (s *Service) Resume() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.paused = false
	s.log.InfoContext(s.ctx, "Import service resumed")
}

// IsPaused returns whether the service is paused
func (s *Service) IsPaused() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.paused
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

	// Cancel all goroutines
	s.cancel()
	s.running = false
	s.mu.Unlock()

	// Wait for all goroutines to finish with timeout
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// All goroutines finished
	case <-time.After(30 * time.Second):
		s.log.WarnContext(ctx, "Timeout waiting for workers to stop, some goroutines may still be running")
	case <-ctx.Done():
		s.log.WarnContext(ctx, "Context cancelled while waiting for workers to stop")
		return ctx.Err()
	}

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

	s.log.InfoContext(s.ctx, "Manual scan started", "path", scanPath)
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

	s.log.InfoContext(s.ctx, "Manual scan cancellation requested", "path", s.scanInfo.Path)
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

		// Create workers
		numWorkers := 20 // 20 parallel workers for file creation
		var workerWg sync.WaitGroup
		batchChan := make(chan *database.ImportQueueItem, 100)

		// Start batch processor
		var batchWg sync.WaitGroup
		batchWg.Add(1)
		go func() {
			defer batchWg.Done()
			s.processQueueBatch(importCtx, batchChan)
		}()

		// Start workers
		for i := 0; i < numWorkers; i++ {
			workerWg.Add(1)
			go func() {
				defer workerWg.Done()
				for {
					select {
					case <-importCtx.Done():
						return
					case res, ok := <-nzbChan:
						if !ok {
							return
						}

						s.importMu.Lock()
						s.importInfo.Total++
						s.importMu.Unlock()

						item, err := s.createNzbFileAndPrepareItem(importCtx, res, rootFolder, nzbTempDir)
						if err != nil {
							s.log.ErrorContext(importCtx, "Failed to prepare item", "file", res.Name, "error", err)
							s.importMu.Lock()
							s.importInfo.Failed++
							s.importMu.Unlock()
							continue
						}

						select {
						case batchChan <- item:
						case <-importCtx.Done():
							return
						}
					}
				}
			}()
		}

		// Wait for workers to finish processing nzbChan
		workerWg.Wait()
		close(batchChan)
		batchWg.Wait()

		// Check for parser errors
		select {
		case err := <-errChan:
			if err != nil {
				s.log.ErrorContext(importCtx, "Error during NZBDav parsing", "error", err)
				s.importMu.Lock()
				msg := err.Error()
				s.importInfo.LastError = &msg
				s.importMu.Unlock()
			}
		default:
		}
	}()

	return nil
}

func (s *Service) processQueueBatch(ctx context.Context, batchChan <-chan *database.ImportQueueItem) {
	var batch []*database.ImportQueueItem
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	insertBatch := func() {
		if len(batch) > 0 {
			if err := s.database.Repository.AddBatchToQueue(ctx, batch); err != nil {
				s.log.ErrorContext(ctx, "Failed to add batch to queue", "count", len(batch), "error", err)
				s.importMu.Lock()
				s.importInfo.Failed += len(batch)
				s.importMu.Unlock()
			} else {
				s.importMu.Lock()
				s.importInfo.Added += len(batch)
				s.importMu.Unlock()
			}
			batch = nil // Reset batch
		}
	}

	for {
		select {
		case item, ok := <-batchChan:
			if !ok {
				// Channel closed, drain remaining batch
				insertBatch()
				return
			}
			batch = append(batch, item)
			if len(batch) >= 100 { // Batch size
				insertBatch()
			}
		case <-ticker.C:
			insertBatch()
		case <-ctx.Done():
			insertBatch()
			return
		}
	}
}

func (s *Service) createNzbFileAndPrepareItem(ctx context.Context, res *nzbdav.ParsedNzb, rootFolder, nzbTempDir string) (*database.ImportQueueItem, error) {
	// Check context before file operations
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	// Create Temp NZB File
	// Use ID to ensure uniqueness and avoid collisions with releases having the same name in the temp directory
	// but don't include it in the filename to avoid it appearing in the final folder/file names
	nzbSubDir := filepath.Join(nzbTempDir, sanitizeFilename(res.ID))
	if err := os.MkdirAll(nzbSubDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create temp NZB subdirectory: %w", err)
	}

	nzbFileName := fmt.Sprintf("%s.nzb", sanitizeFilename(res.Name))
	nzbPath := filepath.Join(nzbSubDir, nzbFileName)

	outFile, err := os.Create(nzbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create temp NZB file: %w", err)
	}

	// Copy content
	_, err = io.Copy(outFile, res.Content)
	outFile.Close()
	if err != nil {
		os.Remove(nzbPath)
		return nil, fmt.Errorf("failed to write temp NZB file content: %w", err)
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

	// Store original ID in metadata
	metaJSON := fmt.Sprintf(`{"nzbdav_id": "%s"}`, res.ID)

	// Prepare item struct
	item := &database.ImportQueueItem{
		NzbPath:      nzbPath,
		RelativePath: &relPath,
		Category:     &targetCategory,
		Priority:     priority,
		Status:       database.QueueStatusPending,
		RetryCount:   0,
		MaxRetries:   3,
		CreatedAt:    time.Now(),
		Metadata:     &metaJSON,
	}

	// Calculate file size
	if size, err := s.CalculateFileSizeOnly(nzbPath); err == nil {
		item.FileSize = &size
	}

	return item, nil
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
		if s.isFileAlreadyInQueue(ctx, path) {
			return nil
		}

		// Check if already processed (metadata exists)
		if s.isFileAlreadyProcessed(path, scanPath) {
			s.log.DebugContext(ctx, "Skipping file - already processed", "file", path)
			return nil
		}

		// Add to queue
		if _, err := s.AddToQueue(ctx, path, &scanPath, nil, nil); err != nil {
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
func (s *Service) isFileAlreadyInQueue(ctx context.Context, filePath string) bool {
	// Only check queue database during scanning for performance
	// The processor will check main database for duplicates when processing
	inQueue, err := s.database.Repository.IsFileInQueue(ctx, filePath)
	if err != nil {
		s.log.WarnContext(ctx, "Failed to check if file in queue", "file", filePath, "error", err)
		return false // Assume not in queue on error
	}
	return inQueue
}

// isFileAlreadyProcessed checks if a file has already been processed by checking metadata
func (s *Service) isFileAlreadyProcessed(filePath string, scanRoot string) bool {
	// Calculate virtual path
	// Assuming scanRoot maps to root of virtual FS for simplicity in manual scan
	// or use CalculateVirtualDirectory logic if needed
	virtualPath := filesystem.CalculateVirtualDirectory(filePath, scanRoot)
	
	// Check if we have metadata for this path
	// For single files: virtualPath/filename (minus .nzb)
	// For multi files: virtualPath/filename (minus .nzb) as directory
	
	// Normalize filename (remove .nzb extension)
	fileName := filepath.Base(filePath)
	baseName := strings.TrimSuffix(fileName, filepath.Ext(fileName))
	
	// Construct potential virtual paths
	// 1. As a file (single file import flattened or nested)
	// We need to check if a file exists with the release name
	// But we don't know the final extension... 
	// However, metadata service stores files with their final names.
	
	// Better approach: Check if a directory exists with the release name
	// Most imports create a folder with the release name
	releaseDir := filepath.Join(virtualPath, baseName)
	if s.metadataService.DirectoryExists(releaseDir) {
		return true
	}
	
	// Also check if any file exists that starts with the release name in that directory
	// This covers flattened single files
	if files, err := s.metadataService.ListDirectory(virtualPath); err == nil {
		for _, f := range files {
			if strings.HasPrefix(f, baseName) {
				return true
			}
		}
	}

	return false
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
	errStr := err.Error()
	return strings.Contains(errStr, "database is locked") ||
		strings.Contains(errStr, "database is busy") ||
		strings.Contains(errStr, "database table is locked")
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
		retry.Attempts(3), // Reduced from 5 - immediate transactions should succeed quickly
		retry.Delay(50*time.Millisecond), // Increased from 10ms
		retry.MaxDelay(5*time.Second),    // Increased from 500ms to allow better spreading
		retry.DelayType(retry.BackOffDelay),
		retry.RetryIf(isDatabaseContentionError),
		retry.OnRetry(func(n uint, err error) {
			// Add jitter to prevent synchronized retries across workers
			// Jitter range: 0-1000ms to desynchronize worker retries
			jitter := time.Duration(rand.Int63n(int64(time.Second)))
			time.Sleep(jitter)

			// Calculate exponential backoff for logging
			baseDelay := 50 * time.Millisecond
			backoffDelay := baseDelay * (1 << n) // Exponential: 50ms, 100ms, 200ms...
			if backoffDelay > 5*time.Second {
				backoffDelay = 5 * time.Second
			}

			// Only log warnings after first retry to reduce noise
			if n >= 1 {
				s.log.WarnContext(ctx, "Database contention, retrying claim",
					"attempt", n+1,
					"worker_id", workerID,
					"backoff_ms", backoffDelay.Milliseconds(),
					"jitter_ms", jitter.Milliseconds(),
					"error", err)
			} else {
				s.log.DebugContext(ctx, "Database contention, retrying claim",
					"attempt", n+1,
					"worker_id", workerID,
					"backoff_ms", backoffDelay.Milliseconds(),
					"jitter_ms", jitter.Milliseconds(),
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

	mountPath := filepath.Join(s.configGetter().MountPath, filepath.Dir(strings.TrimPrefix(resultingPath, "/")))
	if _, err := os.Stat(mountPath); err != nil {
		if os.IsNotExist(err) {
			cfg := s.configGetter()
			vfsName := cfg.RClone.VFSName
			if vfsName == "" {
				vfsName = config.MountProvider
			}

			// Refresh the root path if the mount path is not found
			err := s.rcloneClient.RefreshDir(s.ctx, vfsName, []string{"/"})
			if err != nil {
				s.log.ErrorContext(ctx, "Failed to refresh mount path", "queue_id", itemID, "path", mountPath, "error", err)
			}
		}
	}
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
	newFilename := fmt.Sprintf("%d_%s", item.ID, sanitizeFilename(filename))
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

	// Refresh mount path if needed
	s.refreshMountPathIfNeeded(ctx, resultingPath, item.ID)

	// Notify rclone VFS about the new import (blocking, ensures visibility for ARRs)
	s.notifyRcloneVFS(ctx, resultingPath, false)

	// Create category symlink (non-blocking)
	if err := s.createSymlinks(item, resultingPath); err != nil {
		s.log.WarnContext(ctx, "Failed to create symlink",
			"queue_id", item.ID,
			"path", resultingPath,
			"error", err)

		return err
	}

	// Create ID metadata links if applicable (for nzbdav compatibility)
	s.handleIdMetadataLinks(item, resultingPath)

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
		// Try to trigger scan on the specific instance that manages this file
		// resultingPath is the virtual path, e.g. "movies/MovieName/Movie.mkv"
		// This uses the Root Folder check which is fast and accurate
		fullMountPath := filepath.Join(s.configGetter().MountPath, strings.TrimPrefix(resultingPath, "/"))
		if err := s.arrsService.TriggerScanForFile(ctx, fullMountPath); err != nil {
			// Fallback: If we couldn't find a specific owner, broadcast to all instances of the type
			s.log.DebugContext(ctx, "Could not find specific ARR instance for file, broadcasting scan",
				"path", fullMountPath, "error", err)

			categoryName := *item.Category
			category := strings.ToLower(categoryName)
			arrType := ""

			// Try to find an explicit mapping in SABnzbd categories
			cfg := s.configGetter()
			for _, cat := range cfg.SABnzbd.Categories {
				if strings.EqualFold(cat.Name, categoryName) && cat.Type != "" {
					arrType = strings.ToLower(cat.Type)
					break
				}
			}

			// Fallback to heuristic if no explicit type is mapped
			if arrType == "" {
				if category == "tv" || strings.Contains(category, "tv") || strings.Contains(category, "show") || category == "sonarr" {
					arrType = "sonarr"
				} else if category == "movies" || strings.Contains(category, "movie") || category == "radarr" {
					arrType = "radarr"
				}
			}

			if arrType != "" {
				s.arrsService.TriggerDownloadScan(ctx, arrType)
			}
		}
	}

	// Schedule immediate health check for the new file
	if s.healthRepo != nil {
		// Calculate the mount relative path
		// resultingPath is the virtual path (e.g. "movies/Movie (Year)/Movie.mkv")
		
		// Read metadata to get SourceNzbPath needed for health check
		fileMeta, err := s.metadataService.ReadFileMetadata(resultingPath)
		if err != nil {
			slog.WarnContext(ctx, "Failed to read metadata for health check scheduling", "path", resultingPath, "error", err)
		} else if fileMeta != nil {
			// Add/Update health record with high priority to ensure it's processed right away
			err := s.healthRepo.AddFileToHealthCheck(ctx, resultingPath, 2, &fileMeta.SourceNzbPath, database.HealthPriorityNext)
			if err != nil {
				slog.ErrorContext(ctx, "Failed to schedule immediate health check for imported file", "path", resultingPath, "error", err)
			} else {
				slog.InfoContext(ctx, "Scheduled immediate health check for imported file", "path", resultingPath)
			}
		}

		// Also check for OTHER files in the same directory that were marked for repair.
		// This handles the case where "Movie.2020.mkv" (corrupted) is replaced by "Movie.REPACK.mkv".
		// The directory would be "movies/Movie (Year)".
		cfg := s.configGetter()
		resolveRepairs := true
		if cfg.Health.ResolveRepairOnImport != nil {
			resolveRepairs = *cfg.Health.ResolveRepairOnImport
		}

		if resolveRepairs {
			parentDir := filepath.Dir(resultingPath)
			if parentDir != "." && parentDir != "/" {
				if count, err := s.healthRepo.ResolvePendingRepairsInDirectory(ctx, parentDir); err == nil && count > 0 {
					slog.InfoContext(ctx, "Resolved pending repairs in directory due to new import",
						"directory", parentDir,
						"resolved_count", count)
				}
			}
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
	s.cancelMu.RLock()
	cancel, exists := s.cancelFuncs[itemID]
	s.cancelMu.RUnlock()

	if !exists {
		return fmt.Errorf("item %d is not currently processing", itemID)
	}

	s.log.InfoContext(s.ctx, "Cancelling processing for queue item", "item_id", itemID)
	cancel()
	return nil
}

// notifyRcloneVFS notifies rclone VFS about a new import
func (s *Service) notifyRcloneVFS(ctx context.Context, resultingPath string, async bool) {
	if s.rcloneClient == nil {
		return // No rclone client configured or RClone RC is disabled
	}

	refreshFunc := func(path string) {
		// Derive timeout context from parent context for proper cancellation propagation
		// Increased timeout to 60 seconds as vfs/refresh can be slow
		refreshCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
		defer cancel()

		cfg := s.configGetter()
		vfsName := cfg.RClone.VFSName
		if vfsName == "" {
			vfsName = config.MountProvider
		}

		// Refresh both the path and its parent to ensure visibility
		// Ensure paths are relative to the rclone remote root (no leading slash)
		normalizeForRclone := func(p string) string {
			p = strings.TrimPrefix(p, "/")
			if p == "" {
				return "."
			}
			return p
		}

		dirsToRefresh := []string{normalizeForRclone(path)}
		parentDir := filepath.Dir(path)
		if parentDir != "." && parentDir != "/" {
			dirsToRefresh = append(dirsToRefresh, normalizeForRclone(parentDir))

			// Also refresh grandparent if parent might be new (e.g. /complete/tv)
			grandParent := filepath.Dir(parentDir)
			if grandParent != "." && grandParent != "/" {
				dirsToRefresh = append(dirsToRefresh, normalizeForRclone(grandParent))
			}
		}

		slog.DebugContext(refreshCtx, "Notifying rclone VFS refresh", "dirs", dirsToRefresh, "vfs", vfsName)

		err := s.rcloneClient.RefreshDir(refreshCtx, vfsName, dirsToRefresh)
		if err != nil {
			slog.WarnContext(refreshCtx, "Failed to notify rclone VFS refresh",
				"dirs", dirsToRefresh,
				"error", err)
		} else {
			slog.InfoContext(refreshCtx, "Successfully notified rclone VFS refresh",
				"dirs", dirsToRefresh)
		}
	}

	if async {
		go refreshFunc(resultingPath)
	} else {
		refreshFunc(resultingPath)
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
	actualPath := filepath.Join(cfg.MountPath, strings.TrimPrefix(resultingPath, "/"))

	// Check the metadata directory to determine if this is a file or directory
	// (Don't use os.Stat on mount path as it might not be immediately available)
	metadataPath := filepath.Join(cfg.Metadata.RootPath, strings.TrimPrefix(resultingPath, "/"))
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
		actualFilePath := filepath.Join(cfg.MountPath, strings.TrimPrefix(relPath, "/"))

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

	baseDir := filepath.Join(*cfg.Import.ImportDir, filepath.Dir(strings.TrimPrefix(resultingPath, "/")))

	// Ensure category directory exists
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return fmt.Errorf("failed to create symlink category directory: %w", err)
	}

	symlinkPath := filepath.Join(*cfg.Import.ImportDir, strings.TrimPrefix(resultingPath, "/"))

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
	metadataPath := filepath.Join(cfg.Metadata.RootPath, strings.TrimPrefix(resultingPath, "/"))
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

func (s *Service) handleIdMetadataLinks(item *database.ImportQueueItem, resultingPath string) {
	// 1. Check if the queue item itself has a release-level ID in its metadata
	if item.Metadata != nil && *item.Metadata != "" {
		var meta struct {
			NzbdavID string `json:"nzbdav_id"`
		}
		if err := json.Unmarshal([]byte(*item.Metadata), &meta); err == nil && meta.NzbdavID != "" {
			if err := s.createIDMetadataLink(meta.NzbdavID, resultingPath); err != nil {
				s.log.Warn("Failed to create release ID metadata link", "id", meta.NzbdavID, "error", err)
			}
		}
	}

	// 2. Check individual files for IDs
	cfg := s.configGetter()
	metadataPath := filepath.Join(cfg.Metadata.RootPath, strings.TrimPrefix(resultingPath, "/"))

	_ = filepath.WalkDir(metadataPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(d.Name(), ".meta") {
			return nil
		}

		// Read the metadata file to find the ID
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		// Parse the protobuf metadata to get the ID
		meta := &metapb.FileMetadata{}
		if err := proto.Unmarshal(data, meta); err != nil {
			return nil
		}

		// Check sidecar ID file if not in proto (compatibility mode)
		if meta.NzbdavId == "" {
			if idData, err := os.ReadFile(path + ".id"); err == nil {
				meta.NzbdavId = string(idData)
			}
		}

		if meta.NzbdavId != "" {
			// Calculate the virtual path from the metadata file path
			relPath, err := filepath.Rel(cfg.Metadata.RootPath, path)
			if err != nil {
				return nil
			}
			// Remove .meta extension
			virtualPath := strings.TrimSuffix(relPath, ".meta")

			if err := s.createIDMetadataLink(meta.NzbdavId, virtualPath); err != nil {
				s.log.Warn("Failed to create file ID metadata link", "id", meta.NzbdavId, "error", err)
			}
		}

		return nil
	})
}

func (s *Service) createIDMetadataLink(nzbdavID, resultingPath string) error {
	cfg := s.configGetter()
	metadataRoot := cfg.Metadata.RootPath

	// Calculate sharded path
	// 04db0bde-7ad0-46a3-a2f4-9ef8efd0d7d7 -> .ids/0/4/d/b/0/04db0bde-7ad0-46a3-a2f4-9ef8efd0d7d7.meta
	id := strings.ToLower(nzbdavID)
	if len(id) < 5 {
		return nil // Invalid ID for sharding
	}

	shardPath := filepath.Join(".ids", string(id[0]), string(id[1]), string(id[2]), string(id[3]), string(id[4]))
	fullShardDir := filepath.Join(metadataRoot, shardPath)

	if err := os.MkdirAll(fullShardDir, 0755); err != nil {
		return err
	}

	targetMetaPath := s.metadataService.GetMetadataFilePath(resultingPath)
	linkPath := filepath.Join(fullShardDir, id+".meta")

	// Remove if exists
	os.Remove(linkPath)

	// Create relative symlink if possible, or absolute
	// Relative is better if metadataRoot moves
	relTarget, err := filepath.Rel(fullShardDir, targetMetaPath)
	if err != nil {
		return os.Symlink(targetMetaPath, linkPath)
	}

	return os.Symlink(relTarget, linkPath)
}
