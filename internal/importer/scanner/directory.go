package scanner

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

// QueueAdder defines the interface for adding items to the queue
type QueueAdder interface {
	AddToQueue(ctx context.Context, filePath string, relativePath *string) error
	IsFileInQueue(ctx context.Context, filePath string) bool
	IsFileProcessed(filePath string, scanRoot string) bool
}

// DirectoryScanner handles manual directory scanning for NZB/STRM files
type DirectoryScanner struct {
	queueAdder QueueAdder
	log        *slog.Logger

	// State management
	mu         sync.RWMutex
	info       ScanInfo
	cancelFunc context.CancelFunc
}

// NewDirectoryScanner creates a new directory scanner
func NewDirectoryScanner(queueAdder QueueAdder) *DirectoryScanner {
	return &DirectoryScanner{
		queueAdder: queueAdder,
		log:        slog.Default().With("component", "directory-scanner"),
		info:       ScanInfo{Status: ScanStatusIdle},
	}
}

// Start starts a manual scan of the specified directory
func (d *DirectoryScanner) Start(scanPath string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Check if already scanning
	if d.info.Status != ScanStatusIdle {
		return fmt.Errorf("scan already in progress, current status: %s", d.info.Status)
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
	d.cancelFunc = scanCancel

	// Initialize scan info
	now := time.Now()
	d.info = ScanInfo{
		Status:      ScanStatusScanning,
		Path:        scanPath,
		StartTime:   &now,
		FilesFound:  0,
		FilesAdded:  0,
		CurrentFile: "",
		LastError:   nil,
	}

	// Start scanning in goroutine
	go d.performScan(scanCtx, scanPath)

	d.log.InfoContext(context.Background(), "Manual scan started", "path", scanPath)
	return nil
}

// GetStatus returns the current scan status
func (d *DirectoryScanner) GetStatus() ScanInfo {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.info
}

// Cancel cancels the current scan operation
func (d *DirectoryScanner) Cancel() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.info.Status == ScanStatusIdle {
		return fmt.Errorf("no scan is currently running")
	}

	if d.info.Status == ScanStatusCanceling {
		return fmt.Errorf("scan is already being canceled")
	}

	// Update status and cancel context
	d.info.Status = ScanStatusCanceling
	if d.cancelFunc != nil {
		d.cancelFunc()
	}

	d.log.InfoContext(context.Background(), "Manual scan cancellation requested", "path", d.info.Path)
	return nil
}

// performScan performs the actual scanning work
func (d *DirectoryScanner) performScan(ctx context.Context, scanPath string) {
	defer func() {
		d.mu.Lock()
		d.info.Status = ScanStatusIdle
		d.info.CurrentFile = ""
		if d.cancelFunc != nil {
			d.cancelFunc()
			d.cancelFunc = nil
		}
		d.mu.Unlock()
	}()

	d.log.DebugContext(ctx, "Scanning directory for NZB files", "dir", scanPath)

	err := filepath.WalkDir(scanPath, func(path string, entry fs.DirEntry, err error) error {
		// Check for cancellation
		select {
		case <-ctx.Done():
			d.log.InfoContext(ctx, "Scan cancelled", "path", scanPath)
			return fmt.Errorf("scan cancelled")
		default:
		}

		if err != nil {
			d.log.WarnContext(ctx, "Error accessing path", "path", path, "error", err)
			d.mu.Lock()
			errMsg := err.Error()
			d.info.LastError = &errMsg
			d.mu.Unlock()
			return nil // Continue walking
		}

		// Skip directories
		if entry.IsDir() {
			return nil
		}

		// Update current file being processed
		d.mu.Lock()
		d.info.CurrentFile = path
		d.info.FilesFound++
		d.mu.Unlock()

		// Check if it's an NZB or STRM file
		ext := strings.ToLower(path)
		if !strings.HasSuffix(ext, ".nzb") && !strings.HasSuffix(ext, ".strm") {
			return nil
		}

		// Check if already in queue
		if d.queueAdder.IsFileInQueue(ctx, path) {
			return nil
		}

		// Check if already processed (metadata exists)
		if d.queueAdder.IsFileProcessed(path, scanPath) {
			d.log.DebugContext(ctx, "Skipping file - already processed", "file", path)
			return nil
		}

		// Add to queue
		if err := d.queueAdder.AddToQueue(ctx, path, &scanPath); err != nil {
			d.log.ErrorContext(ctx, "Failed to add file to queue during scan", "file", path, "error", err)
		}

		// Update files added counter
		d.mu.Lock()
		d.info.FilesAdded++
		d.mu.Unlock()

		return nil
	})

	if err != nil && !strings.Contains(err.Error(), "scan cancelled") {
		d.log.ErrorContext(ctx, "Failed to scan directory", "dir", scanPath, "error", err)
		d.mu.Lock()
		errMsg := err.Error()
		d.info.LastError = &errMsg
		d.mu.Unlock()
	}

	d.log.InfoContext(ctx, "Manual scan completed", "path", scanPath, "files_found", d.info.FilesFound, "files_added", d.info.FilesAdded)
}
