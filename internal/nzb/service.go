package nzb

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"sync"
	"time"

	"github.com/javi11/altmount/internal/database"
)

// ServiceConfig holds configuration for the NZB service
type ServiceConfig struct {
	DatabasePath string
	WatchDir     string
	AutoImport   bool          // Enable automatic import via background scanner
	PollInterval time.Duration // Polling interval for background scanner
}

// Service provides high-level NZB management functionality
type Service struct {
	config    ServiceConfig
	db        *database.DB
	processor *Processor
	scanner   *Scanner
	log       *slog.Logger
	mu        sync.RWMutex
	started   bool
}

// NewService creates a new NZB service
func NewService(config ServiceConfig) (*Service, error) {
	// Initialize database
	dbConfig := database.Config{
		DatabasePath: config.DatabasePath,
	}

	db, err := database.New(dbConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize database: %w", err)
	}

	// Create processor
	processor := NewProcessor(db.Repository)

	service := &Service{
		config:    config,
		db:        db,
		processor: processor,
		log:       slog.Default().With("component", "nzb-service"),
	}

	// Initialize scanner with configuration; it will start background scanning if PollInterval > 0
	if config.WatchDir != "" {
		scannerConfig := ScannerConfig{
			ScanDir:      config.WatchDir,
			Recursive:    true,
			Extensions:   []string{".nzb"},
			MaxDepth:     0, // No limit
			Workers:      3,
			Timeout:      10 * time.Minute,
			PollInterval: 0,
		}
		if config.AutoImport && config.PollInterval > 0 {
			scannerConfig.PollInterval = config.PollInterval
		}
		service.scanner = NewScanner(scannerConfig, processor)
	}

	return service, nil
}

// Start starts the NZB service
func (s *Service) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.started {
		return fmt.Errorf("service is already started")
	}

	s.log.InfoContext(ctx, "Starting NZB service")
	s.started = true
	s.log.InfoContext(ctx, "NZB service started successfully")

	return nil
}

// Stop stops the NZB service
func (s *Service) Stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.started {
		return nil
	}

	s.log.InfoContext(ctx, "Stopping NZB service")

	s.started = false
	s.log.InfoContext(ctx, "NZB service stopped")

	return nil
}

// Close closes the service and releases resources
func (s *Service) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Stop background scanner if any
	if s.scanner != nil {
		_ = s.scanner.Close()
	}

	// Close database
	return s.db.Close()
}

// ImportFile imports a single NZB file
func (s *Service) ImportFile(ctx context.Context, nzbPath string) error {
	s.log.InfoContext(ctx, "Importing NZB file", "file", nzbPath)

	if err := s.processor.ProcessNzbFile(nzbPath); err != nil {
		s.log.ErrorContext(ctx, "Failed to import NZB file", "file", nzbPath, "error", err)
		return fmt.Errorf("failed to import NZB file %s: %w", nzbPath, err)
	}

	s.log.InfoContext(ctx, "Successfully imported NZB file", "file", nzbPath)
	return nil
}

// ImportDirectory imports all NZB files from a directory
func (s *Service) ImportDirectory(ctx context.Context, dirPath string) (*ImportResult, error) {
	s.log.InfoContext(ctx, "Importing NZB directory", "dir", dirPath)

	pattern := filepath.Join(dirPath, "*.nzb")
	files, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("failed to find NZB files in directory %s: %w", dirPath, err)
	}

	result := &ImportResult{
		TotalFiles:    len(files),
		StartTime:     time.Now(),
		ImportedFiles: make([]string, 0),
		FailedFiles:   make(map[string]string),
	}

	s.log.InfoContext(ctx, "Found NZB files for import", "count", len(files), "dir", dirPath)

	for _, file := range files {
		if err := s.ImportFile(ctx, file); err != nil {
			result.FailedFiles[file] = err.Error()
			s.log.ErrorContext(ctx, "Failed to import file during directory import", "file", file, "error", err)
		} else {
			result.ImportedFiles = append(result.ImportedFiles, file)
			result.SuccessCount++
		}
	}

	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(result.StartTime)

	s.log.InfoContext(ctx, "Directory import completed",
		"total", result.TotalFiles,
		"success", result.SuccessCount,
		"failed", len(result.FailedFiles),
		"duration", result.Duration,
	)

	return result, nil
}

// RescanDirectory rescans a directory and imports any new files
func (s *Service) RescanDirectory(ctx context.Context, dirPath string) (*ImportResult, error) {
	s.log.InfoContext(ctx, "Rescanning directory for new files", "dir", dirPath)

	pattern := filepath.Join(dirPath, "*.nzb")
	files, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("failed to find NZB files: %w", err)
	}

	result := &ImportResult{
		TotalFiles:    len(files),
		StartTime:     time.Now(),
		ImportedFiles: make([]string, 0),
		FailedFiles:   make(map[string]string),
	}

	for _, file := range files {
		// Check if already processed
		exists, err := s.isFileProcessed(file)
		if err != nil {
			result.FailedFiles[file] = fmt.Sprintf("failed to check if file exists: %v", err)
			continue
		}

		if exists {
			s.log.DebugContext(ctx, "File already processed, skipping", "file", file)
			continue
		}

		if err := s.ImportFile(ctx, file); err != nil {
			result.FailedFiles[file] = err.Error()
		} else {
			result.ImportedFiles = append(result.ImportedFiles, file)
			result.SuccessCount++
		}
	}

	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(result.StartTime)

	s.log.InfoContext(ctx, "Directory rescan completed",
		"total_found", result.TotalFiles,
		"new_imported", result.SuccessCount,
		"failed", len(result.FailedFiles),
		"duration", result.Duration,
	)

	return result, nil
}

// GetStats returns service statistics
func (s *Service) GetStats(ctx context.Context) (*ServiceStats, error) {
	// TODO: Implement proper statistics collection
	// This would involve querying the database for counts and sizes

	stats := &ServiceStats{
		IsRunning:     s.IsRunning(),
		DatabasePath:  s.config.DatabasePath,
		WatchDir:      s.config.WatchDir,
		AutoImport:    s.config.AutoImport,
		TotalNzbFiles: 0, // TODO: Query database
		TotalSize:     0, // TODO: Query database
	}

	return stats, nil
}

// IsRunning returns whether the service is running
func (s *Service) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.started
}

// Database returns the database instance
func (s *Service) Database() *database.DB {
	return s.db
}

// isFileProcessed checks if a file has already been processed
func (s *Service) isFileProcessed(filePath string) (bool, error) {
	nzb, err := s.db.Repository.GetNzbFileByPath(filePath)
	if err != nil {
		return false, err
	}
	return nzb != nil, nil
}

// ImportResult holds the result of an import operation
type ImportResult struct {
	TotalFiles    int               `json:"total_files"`
	SuccessCount  int               `json:"success_count"`
	ImportedFiles []string          `json:"imported_files"`
	FailedFiles   map[string]string `json:"failed_files"`
	StartTime     time.Time         `json:"start_time"`
	EndTime       time.Time         `json:"end_time"`
	Duration      time.Duration     `json:"duration"`
}

// ScanFolder performs a comprehensive folder scan for NZB files
func (s *Service) ScanFolder(ctx context.Context) (*ScanResult, error) {
	if s.scanner == nil {
		return nil, fmt.Errorf("scanner not initialized - watch directory not configured")
	}

	s.log.InfoContext(ctx, "Starting folder scan")
	return s.scanner.ScanFolder(ctx)
}

// ScanFolderWithProgress scans folder with real-time progress updates
func (s *Service) ScanFolderWithProgress(ctx context.Context, progressChan chan<- ScanProgress) (*ScanResult, error) {
	if s.scanner == nil {
		return nil, fmt.Errorf("scanner not initialized - watch directory not configured")
	}

	s.log.InfoContext(ctx, "Starting folder scan with progress updates")
	return s.scanner.ScanFolderWithProgress(ctx, progressChan)
}

// ScanCustomFolder scans a custom directory with specific configuration
func (s *Service) ScanCustomFolder(ctx context.Context, scanConfig ScannerConfig) (*ScanResult, error) {
	customScanner := NewScanner(scanConfig, s.processor)
	s.log.InfoContext(ctx, "Starting custom folder scan", "scan_dir", scanConfig.ScanDir)
	return customScanner.ScanFolder(ctx)
}

// GetScannerStats returns scanner configuration and statistics
func (s *Service) GetScannerStats() *ScannerStats {
	if s.scanner == nil {
		return nil
	}
	stats := s.scanner.GetScannerStats()
	return &stats
}

// ServiceStats holds statistics about the service
type ServiceStats struct {
	IsRunning      bool          `json:"is_running"`
	DatabasePath   string        `json:"database_path"`
	WatchDir       string        `json:"watch_dir"`
	AutoImport     bool          `json:"auto_import"`
	WatcherRunning bool          `json:"watcher_running"`
	TotalNzbFiles  int           `json:"total_nzb_files"`
	TotalSize      int64         `json:"total_size"`
	ScannerConfig  *ScannerStats `json:"scanner_config,omitempty"`
}
