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

// PostieMatcher defines the interface for matching Postie-generated NZBs to original queue items
type PostieMatcher interface {
	FindOriginalItem(ctx context.Context, nzbPath string) (*database.ImportQueueItem, error)
	LinkPostieUpload(ctx context.Context, itemID int64, nzbPath string) error
	CompletePostieUpload(ctx context.Context, itemID int64) error
	FailPostieUpload(ctx context.Context, itemID int64, reason string) error
}

// PostieReimporter defines the interface for re-importing Postie uploads
type PostieReimporter interface {
	ReimportForPostie(ctx context.Context, originalItemID int64, newNzbPath string) error
}

// Watcher handles monitoring a directory for new NZB files
type Watcher struct {
	queueAdder   WatchQueueAdder
	configGetter config.ConfigGetter
	log          *slog.Logger
	cancel       context.CancelFunc
	postieMatcher   PostieMatcher // Optional Postie matcher for re-import handling
	postieReimporter PostieReimporter // Optional service for re-importing Postie uploads
}

// NewWatcher creates a new directory watcher
func NewWatcher(queueAdder WatchQueueAdder, configGetter config.ConfigGetter) *Watcher {
	return &Watcher{
		queueAdder:   queueAdder,
		configGetter: configGetter,
		log:          slog.Default().With("component", "directory-watcher"),
	}
}

// SetPostieMatcher sets the Postie matcher for handling Postie-generated NZBs
func (w *Watcher) SetPostieMatcher(matcher PostieMatcher) {
	w.postieMatcher = matcher
	w.log.Info("Postie matcher configured for directory watcher")
}

// SetPostieReimporter sets the Postie re-importer service
func (w *Watcher) SetPostieReimporter(reimporter PostieReimporter) {
	w.postieReimporter = reimporter
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

// getCategoryFromPath detects the category from the file's relative path by matching against
// configured category directories. The path must START with the category's Dir to match.
// For example, if a category has Dir="filmes/download/mov", then:
// - "filmes/download/mov/torrent/movie.nzb" -> matches
// - "other/filmes/download/mov/movie.nzb" -> does NOT match (doesn't start with the dir)
// Returns the category name and the matched category dir path, or nil if no category matches.
func (w *Watcher) getCategoryFromPath(relPath string) (*string, string) {
	cfg := w.configGetter()
	if cfg == nil || len(cfg.SABnzbd.Categories) == 0 {
		return nil, ""
	}

	// Normalize the relative path (use forward slashes, trim leading/trailing slashes)
	normalizedRelPath := strings.Trim(filepath.ToSlash(relPath), "/")
	if normalizedRelPath == "" || normalizedRelPath == "." {
		return nil, ""
	}

	// Build complete directory prefix from SABnzbd CompleteDir
	completeDir := strings.Trim(filepath.ToSlash(cfg.SABnzbd.CompleteDir), "/")

	var bestMatch *config.SABnzbdCategory
	var bestMatchLen int
	var bestMatchDir string

	for i := range cfg.SABnzbd.Categories {
		cat := &cfg.SABnzbd.Categories[i]

		// Get the category directory path (use Dir if set, otherwise use Name)
		catDir := cat.Dir
		if catDir == "" {
			catDir = cat.Name
		}
		catDir = strings.Trim(filepath.ToSlash(catDir), "/")
		if catDir == "" {
			continue
		}

		// Check if the relative path starts with the category directory
		// We need to check for exact prefix match at directory boundaries
		if strings.HasPrefix(normalizedRelPath, catDir) {
			// Verify it's a proper prefix (either exact match or followed by "/")
			remainder := normalizedRelPath[len(catDir):]
			if remainder == "" || strings.HasPrefix(remainder, "/") {
				// Prefer longer matches (more specific categories)
				if len(catDir) > bestMatchLen {
					bestMatch = cat
					bestMatchLen = len(catDir)
					bestMatchDir = catDir
				}
			}
		}

		// Also check with CompleteDir prefix if configured
		if completeDir != "" {
			catDirWithComplete := completeDir + "/" + catDir
			if strings.HasPrefix(normalizedRelPath, catDirWithComplete) {
				remainder := normalizedRelPath[len(catDirWithComplete):]
				if remainder == "" || strings.HasPrefix(remainder, "/") {
					if len(catDirWithComplete) > bestMatchLen {
						bestMatch = cat
						bestMatchLen = len(catDirWithComplete)
						bestMatchDir = catDirWithComplete
					}
				}
			}
		}
	}

	if bestMatch != nil {
		catName := bestMatch.Name
		w.log.Debug("Detected category from path",
			"relPath", relPath,
			"category", catName,
			"matchedDir", bestMatchDir)
		return &catName, bestMatchDir
	}

	return nil, ""
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

	// First, check if this NZB matches a Postie fallback item
	// This handles the case where Postie has uploaded a new NZB for a failed import
	if w.postieMatcher != nil {
		cfg := w.configGetter()
		if cfg.Postie.Enabled != nil && *cfg.Postie.Enabled {
			originalItem, err := w.postieMatcher.FindOriginalItem(ctx, filePath)
			if err != nil {
				w.log.WarnContext(ctx, "Postie matcher error", "file", filePath, "error", err)
				// Continue with normal processing
			} else if originalItem != nil {
				// This is a Postie-generated NZB - handle it specially
				return w.processPostieUpload(ctx, watchRoot, filePath, originalItem)
			}
		}
	}

	// Calculate relative path from watch root to file's directory
	relPath, err := filepath.Rel(watchRoot, filepath.Dir(filePath))
	if err != nil {
		return fmt.Errorf("failed to calculate relative path: %w", err)
	}

	var category *string
	var relativePath *string

	// Try to detect category from configured category directories
	// The path must START with the category's Dir to match
	detectedCategory, _ := w.getCategoryFromPath(relPath)

	if detectedCategory != nil {
		category = detectedCategory
		// Use the relPath as the relative path
		// This ensures subfolders inside the category are preserved and
		// CalculateVirtualDirectory handles it correctly after the NZB move.
		relativePath = &relPath
	} else if relPath != "." && relPath != "" {
		// No configured category matched - don't set a category
		// Just use the watch root as the relative path
		// We use the absolute path to avoid issues with CWD changes or relative path calculations
		absWatchRoot, err := filepath.Abs(watchRoot)
		if err == nil {
			relativePath = &absWatchRoot
		} else {
			relativePath = &watchRoot
		}

		w.log.DebugContext(ctx, "No category matched for path",
			"file", filePath,
			"relPath", relPath)
	} else {
		// relPath is "." (root of watch dir)
		// Use absolute watchRoot as base path
		absWatchRoot, err := filepath.Abs(watchRoot)
		if err == nil {
			relativePath = &absWatchRoot
		} else {
			relativePath = &watchRoot
		}
	}

	// Add to queue
	priority := database.QueuePriorityNormal
	item, err := w.queueAdder.AddToQueue(ctx, filePath, relativePath, category, &priority)
	if err != nil {
		return fmt.Errorf("failed to add to queue: %w", err)
	}

	// Log with category value (not pointer)
	categoryValue := ""
	if category != nil {
		categoryValue = *category
	}
	w.log.InfoContext(ctx, "Added watched NZB to queue",
		"file", filePath,
		"category", categoryValue,
		"queue_id", item.ID)

	return nil
}

// processPostieUpload handles a Postie-generated NZB by linking it to the original queue item
// and triggering a re-import with the new message IDs
func (w *Watcher) processPostieUpload(ctx context.Context, watchRoot, nzbPath string, originalItem *database.ImportQueueItem) error {
	w.log.InfoContext(ctx, "Processing Postie-generated NZB",
		"nzb_path", nzbPath,
		"queue_id", originalItem.ID,
		"original_name", originalItem.OriginalReleaseName)

	// Link the Postie upload to the original queue item
	if err := w.postieMatcher.LinkPostieUpload(ctx, originalItem.ID, nzbPath); err != nil {
		w.log.ErrorContext(ctx, "Failed to link Postie upload", "error", err)
		return err
	}

	// If we have a Postie re-importer service, use it to handle the re-import
	if w.postieReimporter != nil {
		if err := w.postieReimporter.ReimportForPostie(ctx, originalItem.ID, nzbPath); err != nil {
			w.log.ErrorContext(ctx, "Failed to re-import Postie NZB", "error", err)
			// Fall through to basic processing
		} else {
			// Mark Postie upload as completed after successful re-import
			if err := w.postieMatcher.CompletePostieUpload(ctx, originalItem.ID); err != nil {
				w.log.WarnContext(ctx, "Failed to mark Postie upload as completed", "error", err)
			}
			return nil
		}
	}

	// Fallback: Calculate relative path and add to queue normally
	relPath, err := filepath.Rel(watchRoot, filepath.Dir(nzbPath))
	if err != nil {
		return fmt.Errorf("failed to calculate relative path: %w", err)
	}

	// Use the absolute watch root as the relative path
	absWatchRoot, err := filepath.Abs(watchRoot)
	if err == nil {
		relPath = absWatchRoot
	} else {
		relPath = watchRoot
	}

	relativePath := &relPath

	// Add the NZB as a new queue item with a reference to the original
	priority := database.QueuePriorityNormal
	newItem, err := w.queueAdder.AddToQueue(ctx, nzbPath, relativePath, originalItem.Category, &priority)
	if err != nil {
		return fmt.Errorf("failed to add Postie NZB to queue: %w", err)
	}

	w.log.InfoContext(ctx, "Added Postie NZB to queue for re-import",
		"nzb_path", nzbPath,
		"new_queue_id", newItem.ID,
		"original_queue_id", originalItem.ID,
		"category", originalItem.Category)

	return nil
}
