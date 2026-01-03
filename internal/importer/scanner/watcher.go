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
	var relativePath *string

	if relPath != "." && relPath != "" {
		// Use the first directory component as the category
		parts := strings.Split(filepath.ToSlash(relPath), "/")
		if len(parts) > 0 {
			cat := parts[0]
			category = &cat
			
			// Set RelativePath to the category root inside the watch dir
			// This ensures ProcessNzbFile calculates subfolders (like "4k") correctly relative to this root
			relRoot := filepath.Join(watchRoot, cat)
			relativePath = &relRoot
		}
	}

	// Add to queue
	priority := database.QueuePriorityNormal
	item, err := w.queueAdder.AddToQueue(ctx, filePath, relativePath, category, &priority)
	if err != nil {
		return fmt.Errorf("failed to add to queue: %w", err)
	}

	w.log.InfoContext(ctx, "Added watched NZB to queue",
		"file", filePath,
		"category", category,
		"queue_id", item.ID)

	// Note: We don't delete the file here because AddToQueue (Service.processNzbItem) 
	// handles moving/renaming the NZB to persistent storage.
	// If AddToQueue fails, we leave it. If it succeeds, the file at filePath might effectively be "consumed".
	
	// Wait, Service.AddToQueue just adds to DB.
	// The Service *Workers* process the item.
	// When a worker processes it (processNzbItem), it calls `ensurePersistentNzb` which moves/renames it.
	// So we DO NOT need to delete it here if the worker picks it up?
	
	// BUT `AddToQueue` returns immediately. The file stays there until a worker picks it up.
	// If we leave it, the next scan loop will find it again and add duplicate queue item!
	
	// WE MUST MOVE OR DELETE IT.
	// `Service.processNzbItem` expects `item.NzbPath` to point to a valid file.
	// If we delete it here, the worker won't find it.
	
	// Solution:
	// 1. Move file to a temporary staging area or the persistent area immediately?
	// 2. Or check `IsFileInQueue`?
	// 3. Or rename to `.nzb.processed`?
	
	// Standard "Watch" behavior: The application *takes ownership* of the file.
	// It usually moves it to a "tmp" or "queue" folder.
	
	// Since `Service` has logic to "ensurePersistentNzb" (move to .nzbs), we should rely on that.
	// BUT we need to prevent double-adding.
	
	// We can rename it to `.queued` extension?
	// Or we can rely on `QueueRepository.IsFileInQueue` check?
	// `IsFileInQueue` checks if `nzb_path` exists in DB.
	
	// If we add `/watch/movie.nzb` to DB.
	// Next loop finds `/watch/movie.nzb`.
	// Checks DB. It's there. Skips.
	
	// Eventually, Worker picks it up.
	// Worker calls `ensurePersistentNzb`.
	// `ensurePersistentNzb` moves `/watch/movie.nzb` to `/config/.nzbs/123_movie.nzb`.
	// And updates DB `nzb_path`.
	
	// Once moved, the file is gone from `/watch`.
	// So next loop won't find it.
	
	// This seems correct and safe!
	// Provided `AddToQueue` fails if it's already in queue.
	// `Service.AddToQueue` calls `repository.AddToQueue`.
	// `repository.AddToQueue` uses `ON CONFLICT(nzb_path) DO UPDATE`.
	// It upserts.
	
	// If we rely on Upsert, we might reset priority/status if we re-add?
	// We should check existence first.
	
	// I need `IsFileInQueue` capability in `WatchQueueAdder`.
	
	return nil
}