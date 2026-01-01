package scanner

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/altmount/internal/database"
)

// WatchQueueAdder defines the interface for adding items to the queue with category support
type WatchQueueAdder interface {
	AddToQueue(ctx context.Context, filePath string, relativePath *string, category *string, priority *database.QueuePriority) (*database.ImportQueueItem, error)
	IsFileInQueue(ctx context.Context, filePath string) (bool, error)
}

// Watcher handles monitoring a directory for new NZB files
type Watcher struct {
	queueAdder   WatchQueueAdder
	configGetter config.ConfigGetter
	log          *slog.Logger
	cancel       context.CancelFunc
}

// NewWatcher creates a new directory watcher
func NewWatcher(queueAdder WatchQueueAdder, configGetter config.ConfigGetter) *Watcher {
	return &Watcher{
		queueAdder:   queueAdder,
		configGetter: configGetter,
		log:          slog.Default().With("component", "directory-watcher"),
	}
}

// Start starts the watcher loop
func (w *Watcher) Start(ctx context.Context) error {
	cfg := w.configGetter()
	if cfg.Import.WatchDir == nil || *cfg.Import.WatchDir == "" {
		return nil // Watcher disabled
	}

	watchDir := *cfg.Import.WatchDir
	if _, err := os.Stat(watchDir); os.IsNotExist(err) {
		return fmt.Errorf("watch directory does not exist: %s", watchDir)
	}

	interval := 10 * time.Second
	if cfg.Import.WatchIntervalSeconds != nil && *cfg.Import.WatchIntervalSeconds > 0 {
		interval = time.Duration(*cfg.Import.WatchIntervalSeconds) * time.Second
	}

	w.log.InfoContext(ctx, "Starting directory watcher", "dir", watchDir, "interval", interval)

	// Create cancellable context
	watchCtx, cancel := context.WithCancel(ctx)
	w.cancel = cancel

	go w.watchLoop(watchCtx, watchDir, interval)

	return nil
}

// Stop stops the watcher
func (w *Watcher) Stop() {
	if w.cancel != nil {
		w.cancel()
		w.cancel = nil
		w.log.Info("Directory watcher stopped")
	}
}

func (w *Watcher) watchLoop(ctx context.Context, watchDir string, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Initial scan
	w.scanDirectory(ctx, watchDir)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.scanDirectory(ctx, watchDir)
		}
	}
}

func (w *Watcher) scanDirectory(ctx context.Context, watchDir string) {
	err := filepath.WalkDir(watchDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			// If watch dir disappears, we might want to stop or just log
			w.log.WarnContext(ctx, "Error accessing path", "path", path, "error", err)
			return nil
		}

		if d.IsDir() {
			// Skip hidden directories
			if strings.HasPrefix(d.Name(), ".") && d.Name() != "." && d.Name() != ".." {
				return filepath.SkipDir
			}
			return nil
		}

		// Skip hidden files
		if strings.HasPrefix(d.Name(), ".") {
			return nil
		}

		// Check extension
		if !strings.HasSuffix(strings.ToLower(d.Name()), ".nzb") {
			return nil
		}

		// Process NZB file
		if err := w.processNzb(ctx, watchDir, path); err != nil {
			w.log.ErrorContext(ctx, "Failed to process watched file", "file", path, "error", err)
		}

		return nil
	})

	if err != nil {
		w.log.ErrorContext(ctx, "Error walking watch directory", "dir", watchDir, "error", err)
	}
}

func (w *Watcher) processNzb(ctx context.Context, watchRoot, filePath string) error {
	w.log.DebugContext(ctx, "Found new NZB file", "file", filePath)

	// Check if file is stable (not being written to)
	// We check size, sleep 100ms, check size again.
	info1, err := os.Stat(filePath)
	if err != nil {
		return err
	}
	// Sleep briefly to check for modification
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(500 * time.Millisecond):
	}
	info2, err := os.Stat(filePath)
	if err != nil {
		return err
	}

	if info1.Size() != info2.Size() || info1.ModTime() != info2.ModTime() {
		w.log.DebugContext(ctx, "File is changing, skipping for now", "file", filePath)
		return nil
	}

	// Check if already in queue to avoid duplicates/resets
	if inQueue, err := w.queueAdder.IsFileInQueue(ctx, filePath); err != nil {
		return fmt.Errorf("failed to check queue: %w", err)
	} else if inQueue {
		w.log.DebugContext(ctx, "File already in queue, skipping", "file", filePath)
		return nil
	}

	// Determine category from subdirectory
	relPath, err := filepath.Rel(watchRoot, filepath.Dir(filePath))
	if err != nil {
		return fmt.Errorf("failed to calculate relative path: %w", err)
	}

	var category *string
	if relPath != "." && relPath != "" {
		// Clean relative path to be used as category
		cat := filepath.ToSlash(relPath)
		category = &cat
	}

	// Add to queue
	// We pass nil for relativePath so it uses the MountPath/ImportDir as root
	// We want the category to determine the subfolder
	priority := database.QueuePriorityNormal
	item, err := w.queueAdder.AddToQueue(ctx, filePath, nil, category, &priority)
	if err != nil {
		return fmt.Errorf("failed to add to queue: %w", err)
	}

	w.log.InfoContext(ctx, "Added watched NZB to queue",
		"file", filePath,
		"category", category,
		"queue_id", item.ID)

	return nil
}
