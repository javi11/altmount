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
	"github.com/javi11/nntppool"
)

// ServiceConfig holds configuration for the simplified NZB service
type ServiceConfig struct {
	WatchDir     string        // Directory to scan for NZB files
	ScanInterval time.Duration // How often to scan directory (default: 30s)
	Workers      int           // Number of parallel workers (default: 2)
}

// Service provides simplified NZB queue-based importing
type Service struct {
	config          ServiceConfig
	queueDB         *database.QueueDB         // Queue database for processing queue
	metadataService *metadata.MetadataService // Metadata service for file processing
	processor       *Processor
	log             *slog.Logger

	// Runtime state
	mu      sync.RWMutex
	running bool
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
}

// NewService creates a new simplified NZB service with separate main and queue databases
func NewService(config ServiceConfig, metadataService *metadata.MetadataService, queueDB *database.QueueDB, cp nntppool.UsenetConnectionPool) (*Service, error) {
	// Set defaults
	if config.ScanInterval == 0 {
		config.ScanInterval = 30 * time.Second
	}
	if config.Workers == 0 {
		config.Workers = 2
	}

	// Create processor with main database repository (for processed files)
	processor := NewProcessor(metadataService, cp)

	ctx, cancel := context.WithCancel(context.Background())

	service := &Service{
		config:          config,
		metadataService: metadataService,
		queueDB:         queueDB,
		processor:       processor,
		log:             slog.Default().With("component", "nzb-service"),
		ctx:             ctx,
		cancel:          cancel,
	}

	return service, nil
}

// Start starts the simplified NZB service
func (s *Service) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return fmt.Errorf("service is already started")
	}

	s.log.InfoContext(ctx, "Starting simplified NZB service",
		"watch_dir", s.config.WatchDir,
		"scan_interval", s.config.ScanInterval,
		"workers", s.config.Workers)

	// Start directory scanner if watch directory is configured
	if s.config.WatchDir != "" {
		s.wg.Add(1)
		go s.scannerLoop()
	}

	// Start worker pool for processing queue items
	for i := 0; i < s.config.Workers; i++ {
		s.wg.Add(1)
		go s.workerLoop(i)
	}

	s.running = true
	s.log.InfoContext(ctx, "NZB service started successfully")

	return nil
}

// Stop stops the NZB service
func (s *Service) Stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return nil
	}

	s.log.InfoContext(ctx, "Stopping NZB service")

	// Cancel all goroutines
	s.cancel()

	// Wait for all goroutines to finish
	s.wg.Wait()

	// Recreate context for potential restart
	s.ctx, s.cancel = context.WithCancel(context.Background())

	s.running = false
	s.log.InfoContext(ctx, "NZB service stopped")

	return nil
}

// Close closes the service and releases resources
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

// QueueDatabase returns the queue database instance for processing
func (s *Service) QueueDatabase() *database.QueueDB {
	return s.queueDB
}

// GetQueueStats returns current queue statistics from queue database
func (s *Service) GetQueueStats(ctx context.Context) (*database.QueueStats, error) {
	return s.queueDB.Repository.GetQueueStats()
}

// scannerLoop runs in background and scans directory for new NZB files
func (s *Service) scannerLoop() {
	defer s.wg.Done()

	ticker := time.NewTicker(s.config.ScanInterval)
	defer ticker.Stop()

	s.log.Info("Directory scanner started", "watch_dir", s.config.WatchDir)

	// Do initial scan
	s.scanDirectory()

	// Regular scanning loop
	for {
		select {
		case <-ticker.C:
			s.scanDirectory()
		case <-s.ctx.Done():
			s.log.Info("Directory scanner stopped")
			return
		}
	}
}

// scanDirectory scans the watch directory and adds new files to queue
func (s *Service) scanDirectory() {
	if s.config.WatchDir == "" {
		return
	}

	s.log.Debug("Scanning directory for NZB files", "dir", s.config.WatchDir)

	err := filepath.WalkDir(s.config.WatchDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			s.log.Warn("Error accessing path", "path", path, "error", err)
			return nil // Continue walking
		}

		// Skip directories
		if d.IsDir() {
			return nil
		}

		// Check if it's an NZB file
		if !strings.HasSuffix(strings.ToLower(path), ".nzb") {
			return nil
		}

		// Check if already in queue (simplified check during scanning)
		if s.isFileAlreadyInQueue(path) {
			return nil
		}

		// Add to queue
		s.addToQueue(path)
		return nil
	})

	if err != nil {
		s.log.Error("Failed to scan directory", "dir", s.config.WatchDir, "error", err)
	}
}

// isFileAlreadyInQueue checks if file is already in queue (simplified scanning)
func (s *Service) isFileAlreadyInQueue(filePath string) bool {
	// Only check queue database during scanning for performance
	// The processor will check main database for duplicates when processing
	inQueue, err := s.queueDB.Repository.IsFileInQueue(filePath)
	if err != nil {
		s.log.Warn("Failed to check if file in queue", "file", filePath, "error", err)
		return false // Assume not in queue on error
	}
	return inQueue
}

// addToQueue adds a new NZB file to the import queue
func (s *Service) addToQueue(filePath string) {
	item := &database.ImportQueueItem{
		NzbPath:    filePath,
		WatchRoot:  &s.config.WatchDir,
		Priority:   database.QueuePriorityNormal,
		Status:     database.QueueStatusPending,
		RetryCount: 0,
		MaxRetries: 3,
		CreatedAt:  time.Now(),
	}

	if err := s.queueDB.Repository.AddToQueue(item); err != nil {
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
		item, err := s.queueDB.Repository.ClaimNextQueueItem()
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
		if err := s.queueDB.Repository.UpdateQueueItemStatus(item.ID, database.QueueStatusCompleted, nil); err != nil {
			log.Error("Failed to mark item as completed", "queue_id", item.ID, "error", err)
		} else {
			log.Info("Successfully processed queue item", "queue_id", item.ID, "file", item.NzbPath)
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
		if err := s.queueDB.Repository.UpdateQueueItemStatus(item.ID, database.QueueStatusRetrying, &errorMessage); err != nil {
			log.Error("Failed to mark item for retry", "queue_id", item.ID, "error", err)
		} else {
			log.Info("Item marked for retry", "queue_id", item.ID, "retry_count", item.RetryCount+1)
		}
	} else {
		// Max retries exceeded, mark as failed in queue database
		if err := s.queueDB.Repository.UpdateQueueItemStatus(item.ID, database.QueueStatusFailed, &errorMessage); err != nil {
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
	WatchDir     string               `json:"watch_dir"`
	ScanInterval time.Duration        `json:"scan_interval"`
	Workers      int                  `json:"workers"`
	QueueStats   *database.QueueStats `json:"queue_stats,omitempty"`
}

// GetStats returns service statistics
func (s *Service) GetStats(ctx context.Context) (*ServiceStats, error) {
	stats := &ServiceStats{
		IsRunning:    s.IsRunning(),
		WatchDir:     s.config.WatchDir,
		ScanInterval: s.config.ScanInterval,
		Workers:      s.config.Workers,
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
