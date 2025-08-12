package nzb

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
	config    ServiceConfig
	db        *database.DB
	processor *Processor
	log       *slog.Logger

	// Runtime state
	mu      sync.RWMutex
	running bool
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
}

// NewService creates a new simplified NZB service
func NewService(config ServiceConfig, db *database.DB, cp nntppool.UsenetConnectionPool) (*Service, error) {
	// Set defaults
	if config.ScanInterval == 0 {
		config.ScanInterval = 30 * time.Second
	}
	if config.Workers == 0 {
		config.Workers = 2
	}

	// Create processor with connection pool
	processor := NewProcessor(db.Repository, cp)

	ctx, cancel := context.WithCancel(context.Background())

	service := &Service{
		config:    config,
		db:        db,
		processor: processor,
		log:       slog.Default().With("component", "nzb-service"),
		ctx:       ctx,
		cancel:    cancel,
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

// Database returns the database instance
func (s *Service) Database() *database.DB {
	return s.db
}

// GetQueueStats returns current queue statistics
func (s *Service) GetQueueStats(ctx context.Context) (*database.QueueStats, error) {
	return s.db.Repository.GetQueueStats()
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

		// Check if already processed or in queue
		if s.isFileAlreadyKnown(path) {
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

// isFileAlreadyKnown checks if file is already imported or in queue
func (s *Service) isFileAlreadyKnown(filePath string) bool {
	// Check if already imported
	nzb, err := s.db.Repository.GetNzbFileByPath(filePath)
	if err != nil {
		s.log.Warn("Failed to check if file already imported", "file", filePath, "error", err)
		return false // Assume not known on error
	}
	if nzb != nil {
		return true // Already imported
	}

	// Check if already in queue
	queueItem, err := s.db.Repository.GetQueueItemByPath(filePath)
	if err != nil {
		s.log.Warn("Failed to check if file in queue", "file", filePath, "error", err)
		return false // Assume not known on error
	}
	if queueItem != nil {
		return true // Already in queue
	}

	return false // File is new
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

	if err := s.db.Repository.AddToQueue(item); err != nil {
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

// processQueueItems gets and processes pending queue items
func (s *Service) processQueueItems(workerID int) {
	log := s.log.With("worker_id", workerID)

	// Get next pending item
	items, err := s.db.Repository.GetNextQueueItems(1) // Get one item at a time
	if err != nil {
		log.Error("Failed to get next queue items", "error", err)
		return
	}

	if len(items) == 0 {
		return // No work to do
	}

	item := items[0]
	log.Debug("Processing queue item", "queue_id", item.ID, "file", item.NzbPath)

	// Mark as processing
	if err := s.db.Repository.UpdateQueueItemStatus(item.ID, database.QueueStatusProcessing, nil); err != nil {
		log.Error("Failed to mark item as processing", "queue_id", item.ID, "error", err)
		return
	}

	// Process the NZB file
	var processingErr error
	if item.WatchRoot != nil {
		processingErr = s.processor.ProcessNzbFileWithRoot(item.NzbPath, *item.WatchRoot)
	} else {
		processingErr = s.processor.ProcessNzbFile(item.NzbPath)
	}

	if processingErr != nil {
		// Handle failure
		s.handleProcessingFailure(item, processingErr, log)
	} else {
		// Mark as completed
		if err := s.db.Repository.UpdateQueueItemStatus(item.ID, database.QueueStatusCompleted, nil); err != nil {
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
		// Mark for retry
		if err := s.db.Repository.UpdateQueueItemStatus(item.ID, database.QueueStatusRetrying, &errorMessage); err != nil {
			log.Error("Failed to mark item for retry", "queue_id", item.ID, "error", err)
		} else {
			log.Info("Item marked for retry", "queue_id", item.ID, "retry_count", item.RetryCount+1)
		}
	} else {
		// Max retries exceeded, mark as failed
		if err := s.db.Repository.UpdateQueueItemStatus(item.ID, database.QueueStatusFailed, &errorMessage); err != nil {
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
	IsRunning     bool                 `json:"is_running"`
	WatchDir      string               `json:"watch_dir"`
	ScanInterval  time.Duration        `json:"scan_interval"`
	Workers       int                  `json:"workers"`
	QueueStats    *database.QueueStats `json:"queue_stats,omitempty"`
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