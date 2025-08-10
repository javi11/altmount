package nzb

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// WatcherConfig holds configuration for the NZB file watcher
type WatcherConfig struct {
	WatchDir     string        // Directory to watch for new NZB files
	PollInterval time.Duration // Interval for polling (fallback if fsnotify fails)
	Extensions   []string      // File extensions to watch (default: [".nzb"])
}

// Watcher monitors directories for new NZB files and triggers import
type Watcher struct {
	config    WatcherConfig
	processor *Processor
	log       *slog.Logger
	watcher   *fsnotify.Watcher
	mu        sync.RWMutex
	running   bool
	ctx       context.Context
	cancel    context.CancelFunc
}

// NewWatcher creates a new NZB file watcher
func NewWatcher(config WatcherConfig, processor *Processor) (*Watcher, error) {
	if config.PollInterval == 0 {
		config.PollInterval = 30 * time.Second
	}
	
	if len(config.Extensions) == 0 {
		config.Extensions = []string{".nzb"}
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed to create file watcher: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &Watcher{
		config:    config,
		processor: processor,
		log:       slog.Default().With("component", "nzb-watcher"),
		watcher:   watcher,
		ctx:       ctx,
		cancel:    cancel,
	}, nil
}

// Start begins watching for NZB files
func (w *Watcher) Start() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.running {
		return fmt.Errorf("watcher is already running")
	}

	// Add watch directory
	if err := w.watcher.Add(w.config.WatchDir); err != nil {
		return fmt.Errorf("failed to add watch directory: %w", err)
	}

	w.running = true
	w.log.InfoContext(w.ctx, "Starting NZB file watcher", "watch_dir", w.config.WatchDir)

	// Start the main watch loop
	go w.watchLoop()

	// Start periodic scanning as backup
	go w.periodicScan()

	return nil
}

// Stop stops the file watcher
func (w *Watcher) Stop() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if !w.running {
		return nil
	}

	w.log.InfoContext(w.ctx, "Stopping NZB file watcher")
	w.cancel()
	w.running = false

	if err := w.watcher.Close(); err != nil {
		return fmt.Errorf("failed to close file watcher: %w", err)
	}

	return nil
}

// IsRunning returns whether the watcher is currently running
func (w *Watcher) IsRunning() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.running
}

// watchLoop is the main event loop for file system events
func (w *Watcher) watchLoop() {
	for {
		select {
		case event, ok := <-w.watcher.Events:
			if !ok {
				w.log.WarnContext(w.ctx, "File watcher events channel closed")
				return
			}
			w.handleEvent(event)

		case err, ok := <-w.watcher.Errors:
			if !ok {
				w.log.WarnContext(w.ctx, "File watcher errors channel closed")
				return
			}
			w.log.ErrorContext(w.ctx, "File watcher error", "error", err)

		case <-w.ctx.Done():
			w.log.InfoContext(w.ctx, "File watcher stopping due to context cancellation")
			return
		}
	}
}

// periodicScan performs periodic scanning as a backup to fsnotify
func (w *Watcher) periodicScan() {
	ticker := time.NewTicker(w.config.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			w.scanDirectory()
		case <-w.ctx.Done():
			return
		}
	}
}

// handleEvent processes a file system event
func (w *Watcher) handleEvent(event fsnotify.Event) {
	// Only process write and create events
	if event.Op&fsnotify.Write == 0 && event.Op&fsnotify.Create == 0 {
		return
	}

	// Check if this is an NZB file
	if !w.isNzbFile(event.Name) {
		return
	}

	w.log.InfoContext(w.ctx, "Detected new NZB file", "file", event.Name, "event", event.Op.String())

	// Add a small delay to ensure file is completely written
	time.Sleep(1 * time.Second)

	// Process the file
	w.processFile(event.Name)
}

// scanDirectory scans the watch directory for new NZB files
func (w *Watcher) scanDirectory() {
	w.log.DebugContext(w.ctx, "Performing periodic scan", "dir", w.config.WatchDir)

	pattern := filepath.Join(w.config.WatchDir, "*.nzb")
	files, err := filepath.Glob(pattern)
	if err != nil {
		w.log.ErrorContext(w.ctx, "Failed to scan directory", "error", err, "pattern", pattern)
		return
	}

	for _, file := range files {
		// Check if file is already processed
		exists, err := w.isAlreadyProcessed(file)
		if err != nil {
			w.log.ErrorContext(w.ctx, "Failed to check if file is already processed", "file", file, "error", err)
			continue
		}

		if !exists {
			w.log.InfoContext(w.ctx, "Found unprocessed NZB file during scan", "file", file)
			w.processFile(file)
		}
	}
}

// processFile processes a single NZB file
func (w *Watcher) processFile(filePath string) {
	ctx := w.ctx
	log := w.log.With("file", filePath)

	log.InfoContext(ctx, "Processing NZB file")

	if err := w.processor.ProcessNzbFile(filePath); err != nil {
		log.ErrorContext(ctx, "Failed to process NZB file", "error", err)
		return
	}

	log.InfoContext(ctx, "Successfully processed NZB file")
}

// isNzbFile checks if a file has an NZB extension
func (w *Watcher) isNzbFile(filePath string) bool {
	ext := strings.ToLower(filepath.Ext(filePath))
	for _, validExt := range w.config.Extensions {
		if ext == strings.ToLower(validExt) {
			return true
		}
	}
	return false
}

// isAlreadyProcessed checks if an NZB file has already been processed
func (w *Watcher) isAlreadyProcessed(filePath string) (bool, error) {
	nzb, err := w.processor.repo.GetNzbFileByPath(filePath)
	if err != nil {
		return false, err
	}
	return nzb != nil, nil
}

// GetStats returns statistics about the watcher
func (w *Watcher) GetStats() WatcherStats {
	w.mu.RLock()
	defer w.mu.RUnlock()

	return WatcherStats{
		Running:   w.running,
		WatchDir:  w.config.WatchDir,
		StartTime: time.Now(), // TODO: Track actual start time
	}
}

// WatcherStats holds statistics about the watcher
type WatcherStats struct {
	Running   bool      `json:"running"`
	WatchDir  string    `json:"watch_dir"`
	StartTime time.Time `json:"start_time"`
}