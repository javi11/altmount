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
)

// ScannerConfig holds configuration for the NZB folder scanner
type ScannerConfig struct {
	ScanDir        string        // Root directory to scan for NZB files
	Recursive      bool          // Whether to scan subdirectories recursively
	Extensions     []string      // File extensions to include (default: [".nzb"])
	MaxDepth       int           // Maximum recursion depth (0 = no limit)
	Workers        int           // Number of worker goroutines for parallel processing
	Timeout        time.Duration // Timeout for scanning operation
	IgnorePatterns []string      // Patterns to ignore (supports wildcards)
	PollInterval   time.Duration // Background scan frequency (0 disables background scanning)
}

// Scanner provides efficient folder scanning for NZB files
type Scanner struct {
	config    ScannerConfig
	processor *Processor
	log       *slog.Logger
	// background runner
	mu      sync.RWMutex
	running bool
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
}

// ScanResult contains the results of a folder scan operation
type ScanResult struct {
	TotalFiles     int               `json:"total_files"`
	ProcessedFiles int               `json:"processed_files"`
	SkippedFiles   int               `json:"skipped_files"`
	FailedFiles    map[string]string `json:"failed_files"`
	NewFiles       []string          `json:"new_files"`
	ExistingFiles  []string          `json:"existing_files"`
	StartTime      time.Time         `json:"start_time"`
	EndTime        time.Time         `json:"end_time"`
	Duration       time.Duration     `json:"duration"`
	Errors         []string          `json:"errors"`
}

// ScanProgress provides real-time progress updates during scanning
type ScanProgress struct {
	CurrentFile    string    `json:"current_file"`
	FilesScanned   int       `json:"files_scanned"`
	TotalFiles     int       `json:"total_files"`
	FilesProcessed int       `json:"files_processed"`
	Errors         int       `json:"errors"`
	StartTime      time.Time `json:"start_time"`
}

// NewScanner creates a new NZB folder scanner and starts background scanning if PollInterval > 0
func NewScanner(config ScannerConfig, processor *Processor) *Scanner {
	// Set defaults
	if len(config.Extensions) == 0 {
		config.Extensions = []string{".nzb"}
	}
	if config.Workers == 0 {
		config.Workers = 3 // Default to 3 worker goroutines
	}
	if config.Timeout == 0 {
		config.Timeout = 10 * time.Minute // Default 10 minute timeout
	}
	if config.PollInterval < 0 {
		config.PollInterval = 0
	}

	ctx, cancel := context.WithCancel(context.Background())

	s := &Scanner{
		config:    config,
		processor: processor,
		log:       slog.Default().With("component", "nzb-scanner"),
		ctx:       ctx,
		cancel:    cancel,
	}

	// Start background loop if requested
	if config.PollInterval > 0 && config.ScanDir != "" {
		s.startBackground()
	}

	return s
}

// Close stops any background scanning and waits for shutdown
func (s *Scanner) Close() error {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return nil
	}
	s.cancel()
	s.mu.Unlock()

	s.wg.Wait()

	s.mu.Lock()
	s.running = false
	// Renew context so scanner could be restarted later if needed
	s.ctx, s.cancel = context.WithCancel(context.Background())
	s.mu.Unlock()
	return nil
}

// ScanFolder performs a comprehensive scan of the configured folder
func (s *Scanner) ScanFolder(ctx context.Context) (*ScanResult, error) {
	result := &ScanResult{
		FailedFiles:   make(map[string]string),
		NewFiles:      make([]string, 0),
		ExistingFiles: make([]string, 0),
		StartTime:     time.Now(),
		Errors:        make([]string, 0),
	}

	s.log.InfoContext(ctx, "Starting folder scan",
		"scan_dir", s.config.ScanDir,
		"recursive", s.config.Recursive,
		"extensions", s.config.Extensions,
		"workers", s.config.Workers,
	)

	// Create context with timeout
	scanCtx, cancel := context.WithTimeout(ctx, s.config.Timeout)
	defer cancel()

	// Discover all NZB files
	files, err := s.discoverFiles(scanCtx)
	if err != nil {
		return nil, fmt.Errorf("failed to discover files: %w", err)
	}

	result.TotalFiles = len(files)
	s.log.InfoContext(ctx, "File discovery completed", "total_files", result.TotalFiles)

	// Process files with worker pool
	if err := s.processFilesParallel(scanCtx, files, result); err != nil {
		return nil, fmt.Errorf("failed to process files: %w", err)
	}

	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(result.StartTime)

	s.log.InfoContext(ctx, "Folder scan completed",
		"total", result.TotalFiles,
		"processed", result.ProcessedFiles,
		"skipped", result.SkippedFiles,
		"failed", len(result.FailedFiles),
		"duration", result.Duration,
	)

	return result, nil
}

// ScanFolderWithProgress scans folder with real-time progress updates
func (s *Scanner) ScanFolderWithProgress(ctx context.Context, progressChan chan<- ScanProgress) (*ScanResult, error) {
	result, err := s.ScanFolder(ctx)

	// Send progress updates during processing if channel provided
	if progressChan != nil {
		defer close(progressChan)

		// Send initial progress
		progressChan <- ScanProgress{
			CurrentFile:    "Starting scan...",
			FilesScanned:   0,
			TotalFiles:     result.TotalFiles,
			FilesProcessed: 0,
			Errors:         len(result.Errors),
			StartTime:      result.StartTime,
		}

		// Send final progress
		progressChan <- ScanProgress{
			CurrentFile:    "Scan completed",
			FilesScanned:   result.TotalFiles,
			TotalFiles:     result.TotalFiles,
			FilesProcessed: result.ProcessedFiles,
			Errors:         len(result.Errors),
			StartTime:      result.StartTime,
		}
	}

	return result, err
}

// discoverFiles recursively discovers all NZB files in the scan directory
func (s *Scanner) discoverFiles(ctx context.Context) ([]string, error) {
	var files []string
	var mu sync.Mutex

	err := filepath.WalkDir(s.config.ScanDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			s.log.WarnContext(ctx, "Error accessing path during discovery", "path", path, "error", err)
			return nil // Continue walking, don't fail completely
		}

		// Check context for cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Skip directories
		if d.IsDir() {
			// Check recursion depth if limited
			if s.config.MaxDepth > 0 {
				relPath, err := filepath.Rel(s.config.ScanDir, path)
				if err == nil {
					depth := len(strings.Split(relPath, string(filepath.Separator)))
					if depth > s.config.MaxDepth {
						return filepath.SkipDir
					}
				}
			}

			// Skip if not recursive and not root directory
			if !s.config.Recursive && path != s.config.ScanDir {
				return filepath.SkipDir
			}

			return nil
		}

		// Check if file should be ignored
		if s.shouldIgnoreFile(path) {
			return nil
		}

		// Check if file has valid extension
		if s.isValidNzbFile(path) {
			mu.Lock()
			files = append(files, path)
			mu.Unlock()
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("error walking directory: %w", err)
	}

	return files, nil
}

// processFilesParallel processes discovered files using a worker pool
func (s *Scanner) processFilesParallel(ctx context.Context, files []string, result *ScanResult) error {
	// Create work channels
	workChan := make(chan string, len(files))

	// Start worker goroutines
	var wg sync.WaitGroup
	var mu sync.Mutex

	for i := 0; i < s.config.Workers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			for filePath := range workChan {
				select {
				case <-ctx.Done():
					return
				default:
				}

				s.log.DebugContext(ctx, "Processing file", "worker", workerID, "file", filePath)

				// Check if file already exists in database
				exists, err := s.isFileAlreadyProcessed(filePath)
				if err != nil {
					mu.Lock()
					result.FailedFiles[filePath] = fmt.Sprintf("failed to check if file exists: %v", err)
					result.Errors = append(result.Errors, err.Error())
					mu.Unlock()
					continue
				}

				mu.Lock()
				if exists {
					result.ExistingFiles = append(result.ExistingFiles, filePath)
					result.SkippedFiles++
				} else {
					// Process new file
					if err := s.processor.ProcessNzbFile(filePath); err != nil {
						result.FailedFiles[filePath] = err.Error()
						result.Errors = append(result.Errors, err.Error())
						s.log.ErrorContext(ctx, "Failed to process NZB file", "file", filePath, "error", err)
					} else {
						result.NewFiles = append(result.NewFiles, filePath)
						result.ProcessedFiles++
						s.log.InfoContext(ctx, "Successfully processed NZB file", "file", filePath)
					}
				}
				mu.Unlock()
			}
		}(i)
	}

	// Send work to workers
	go func() {
		defer close(workChan)
		for _, file := range files {
			select {
			case workChan <- file:
			case <-ctx.Done():
				return
			}
		}
	}()

	// Wait for all workers to complete
	wg.Wait()

	return ctx.Err()
}

// isValidNzbFile checks if a file has a valid NZB extension
func (s *Scanner) isValidNzbFile(filePath string) bool {
	ext := strings.ToLower(filepath.Ext(filePath))
	for _, validExt := range s.config.Extensions {
		if ext == strings.ToLower(validExt) {
			return true
		}
	}
	return false
}

// shouldIgnoreFile checks if a file matches any ignore patterns
func (s *Scanner) shouldIgnoreFile(filePath string) bool {
	fileName := filepath.Base(filePath)

	for _, pattern := range s.config.IgnorePatterns {
		matched, err := filepath.Match(pattern, fileName)
		if err == nil && matched {
			return true
		}

		// Also check if the full path matches
		matched, err = filepath.Match(pattern, filePath)
		if err == nil && matched {
			return true
		}
	}

	return false
}

// isFileAlreadyProcessed checks if an NZB file has already been processed
func (s *Scanner) isFileAlreadyProcessed(filePath string) (bool, error) {
	nzb, err := s.processor.repo.GetNzbFileByPath(filePath)
	if err != nil {
		return false, err
	}
	return nzb != nil, nil
}

// GetScannerStats returns statistics about the scanner configuration
func (s *Scanner) GetScannerStats() ScannerStats {
	return ScannerStats{
		ScanDir:        s.config.ScanDir,
		Recursive:      s.config.Recursive,
		Extensions:     s.config.Extensions,
		MaxDepth:       s.config.MaxDepth,
		Workers:        s.config.Workers,
		Timeout:        s.config.Timeout,
		IgnorePatterns: s.config.IgnorePatterns,
	}
}

// ScannerStats holds statistics about the scanner
type ScannerStats struct {
	ScanDir        string        `json:"scan_dir"`
	Recursive      bool          `json:"recursive"`
	Extensions     []string      `json:"extensions"`
	MaxDepth       int           `json:"max_depth"`
	Workers        int           `json:"workers"`
	Timeout        time.Duration `json:"timeout"`
	IgnorePatterns []string      `json:"ignore_patterns"`
}

// startBackground kicks off the periodic scanning loop in a goroutine
func (s *Scanner) startBackground() {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	interval := s.config.PollInterval
	ctx := s.ctx
	s.mu.Unlock()

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		// Optional immediate scan
		if s.config.ScanDir != "" {
			if _, err := s.ScanFolder(ctx); err != nil && ctx.Err() == nil {
				s.log.ErrorContext(ctx, "Background scan failed", "error", err)
			}
		}

		if interval == 0 {
			return
		}

		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if s.config.ScanDir == "" {
					continue
				}
				if _, err := s.ScanFolder(ctx); err != nil && ctx.Err() == nil {
					s.log.ErrorContext(ctx, "Background scan failed", "error", err)
				}
			case <-ctx.Done():
				return
			}
		}
	}()
}
