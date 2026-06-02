package worker

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/javi11/altmount/internal/arrs/clients"
	"github.com/javi11/altmount/internal/arrs/instances"
	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/altmount/internal/database"
)

type Worker struct {
	configGetter config.ConfigGetter
	instances    *instances.Manager
	clients      *clients.Manager
	repo         *database.Repository

	// Queue cleanup worker state
	workerCtx     context.Context
	workerCancel  context.CancelFunc
	workerWg      sync.WaitGroup
	workerMu      sync.Mutex
	workerRunning bool

	// firstSeen tracks when a failed import item was first seen
	// key: instanceName|queueID
	firstSeen   map[string]time.Time
	firstSeenMu sync.RWMutex
}

func NewWorker(configGetter config.ConfigGetter, instances *instances.Manager, clients *clients.Manager, repo *database.Repository) *Worker {
	return &Worker{
		configGetter: configGetter,
		instances:    instances,
		clients:      clients,
		repo:         repo,
		firstSeen:    make(map[string]time.Time),
	}
}

// Start starts the queue cleanup worker
func (w *Worker) Start(ctx context.Context) error {
	w.workerMu.Lock()
	defer w.workerMu.Unlock()

	if w.workerRunning {
		return nil
	}

	cfg := w.configGetter()

	// ARRs must be enabled
	if cfg.Arrs.Enabled == nil || !*cfg.Arrs.Enabled {
		slog.InfoContext(ctx, "ARR queue cleanup disabled (ARRs disabled)")
		return nil
	}

	// Queue cleanup covers ghost/empty-folder detection, the automatic-import-failure
	// purge and the message-rule pass. Enabled by default (nil or true).
	if !IsQueueCleanupEnabled(cfg) {
		slog.InfoContext(ctx, "ARR queue cleanup disabled")
		return nil
	}

	w.workerCtx, w.workerCancel = context.WithCancel(ctx)
	w.workerRunning = true

	interval := time.Duration(cfg.Arrs.QueueCleanupIntervalSeconds) * time.Second
	if interval <= 0 {
		interval = 10 * time.Second
	}

	w.workerWg.Add(1)
	go w.runWorker(interval)

	slog.InfoContext(ctx, "ARR queue cleanup worker started",
		"interval_seconds", cfg.Arrs.QueueCleanupIntervalSeconds)
	return nil
}

// Stop stops the queue cleanup worker
func (w *Worker) Stop(ctx context.Context) {
	w.workerMu.Lock()
	defer w.workerMu.Unlock()

	if !w.workerRunning {
		return
	}

	w.workerCancel()
	w.workerWg.Wait()
	w.workerRunning = false
	slog.InfoContext(ctx, "ARR queue cleanup worker stopped")
}

func (w *Worker) runWorker(interval time.Duration) {
	defer w.workerWg.Done()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Initial delay before first run
	select {
	case <-time.After(30 * time.Second):
	case <-w.workerCtx.Done():
		return
	}

	// Run initial cleanup
	w.safeCleanup()

	for {
		select {
		case <-ticker.C:
			w.safeCleanup()
		case <-w.workerCtx.Done():
			return
		}
	}
}

func (w *Worker) safeCleanup() {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("Panic in queue cleanup", "panic", r)
		}
	}()
	if !IsQueueCleanupEnabled(w.configGetter()) {
		return
	}
	// One unified pass per tick covers all six *arr types: ghost/empty-folder removal,
	// then the message-rule actions. force=false so items are observed over time and
	// only acted on once stuck past the grace period (ghost removal stays grace-free).
	if _, err := w.CleanupStuckQueue(w.workerCtx, false); err != nil {
		slog.Error("Queue cleanup failed", "error", err)
	}
}

// IsQueueCleanupEnabled reports whether the queue cleanup feature should be
// active based on the global arrs.enabled and arrs.queue_cleanup_enabled flags.
func IsQueueCleanupEnabled(cfg *config.Config) bool {
	if cfg.Arrs.Enabled == nil || !*cfg.Arrs.Enabled {
		return false
	}
	if cfg.Arrs.QueueCleanupEnabled != nil && !*cfg.Arrs.QueueCleanupEnabled {
		return false
	}
	return true
}

// captureSportarrIndexer persists the indexer reported by a Sportarr queue record
// against its download ID, so completed/failed imports are attributed correctly in
// indexer health instead of falling back to "Unknown". The queue row is only written
// when its indexer is still unset (avoiding per-poll churn and clobbering); the
// import-history UPDATE is self-guarded to Unknown/NULL rows, covering downloads that
// already imported before this poll observed them.
func (w *Worker) captureSportarrIndexer(ctx context.Context, downloadID, indexer string) {
	item, err := w.repo.GetQueueItemByDownloadID(ctx, downloadID)
	if err != nil {
		slog.DebugContext(ctx, "Failed to look up queue item for Sportarr indexer capture",
			"download_id", downloadID, "error", err)
	}
	if item != nil && (item.Indexer == nil || *item.Indexer == "" || *item.Indexer == database.IndexerUnknown) {
		if err := w.repo.UpdateQueueItemIndexerByDownloadID(ctx, downloadID, indexer); err != nil {
			slog.DebugContext(ctx, "Failed to set Sportarr indexer on queue item",
				"download_id", downloadID, "indexer", indexer, "error", err)
		} else {
			slog.InfoContext(ctx, "Captured Sportarr indexer for download",
				"download_id", downloadID, "indexer", indexer)
		}
	}

	if err := w.repo.UpdateImportHistoryIndexerByDownloadID(ctx, downloadID, indexer); err != nil {
		slog.DebugContext(ctx, "Failed to set Sportarr indexer on import history",
			"download_id", downloadID, "indexer", indexer, "error", err)
	}
}

// checkGhostByImportHistory checks if a queue item has already been imported
// by looking up AltMount's import history. Returns true if confirmed ghost
// (i.e., the file has been moved to the library).
func (w *Worker) checkGhostByImportHistory(ctx context.Context, outputPath string, cfg *config.Config, instanceName, title string) bool {
	if outputPath == "" {
		return false
	}

	outPathSlash := filepath.ToSlash(outputPath)
	virtualPath := outPathSlash

	mountPathSlash := filepath.ToSlash(cfg.MountPath)
	if strings.HasPrefix(outPathSlash, mountPathSlash) {
		virtualPath = strings.TrimPrefix(outPathSlash, mountPathSlash)
	} else if cfg.Import.ImportDir != nil && *cfg.Import.ImportDir != "" {
		importDirSlash := filepath.ToSlash(*cfg.Import.ImportDir)
		if strings.HasPrefix(outPathSlash, importDirSlash) {
			virtualPath = strings.TrimPrefix(outPathSlash, importDirSlash)
		}
	}

	virtualPath = strings.TrimPrefix(virtualPath, "/")

	if virtualPath == outPathSlash || virtualPath == "" {
		return false
	}

	history, err := w.repo.GetImportHistoryByPath(ctx, virtualPath)
	if err != nil || history == nil {
		return false
	}

	if history.LibraryPath != nil && *history.LibraryPath != "" {
		slog.InfoContext(ctx, "Found ghost queue item (confirmed moved to library), cleaning up immediately",
			"path", outputPath, "library_path", *history.LibraryPath, "title", title, "instance", instanceName)
		return true
	}

	slog.DebugContext(ctx, "Item found in history but not yet moved to library, waiting for ARR final step",
		"path", outputPath, "title", title)
	return false
}

// isGhostByPathGone checks if a queue item is a ghost by verifying the source
// path no longer exists. Applies safety checks to avoid false positives from
// transient FUSE mount issues or broken symlinks.
func (w *Worker) isGhostByPathGone(ctx context.Context, outputPath string, queueID int64, cfg *config.Config, instanceName, title string) bool {
	if outputPath == "" {
		return false
	}

	// Check if path exists via Stat (follows symlinks)
	_, statErr := os.Stat(outputPath)
	if statErr == nil {
		// Path exists — not a ghost
		return false
	}
	if !os.IsNotExist(statErr) {
		// Some other error (permission, etc.) — don't assume ghost
		return false
	}

	// Broken symlink detection: if outputPath is inside ImportDir, check Lstat.
	// If Lstat succeeds but Stat fails, it's a broken symlink, not a ghost.
	if cfg.Import.ImportDir != nil && *cfg.Import.ImportDir != "" {
		importDir := filepath.Clean(*cfg.Import.ImportDir)
		if strings.HasPrefix(filepath.Clean(outputPath), importDir) {
			_, lstatErr := os.Lstat(outputPath)
			if lstatErr == nil {
				// Lstat succeeds (file entry exists) but Stat fails (target gone) → broken symlink
				slog.DebugContext(ctx, "Broken symlink detected in import dir, not treating as ghost",
					"path", outputPath, "title", title, "instance", instanceName)
				return false
			}
		}
	}

	// Minimum observation window: require the path to be missing for >=60s
	// to guard against transient FUSE hiccups.
	ghostKey := fmt.Sprintf("ghost|%s|%d", instanceName, queueID)
	w.firstSeenMu.Lock()
	seenTime, exists := w.firstSeen[ghostKey]
	if !exists {
		w.firstSeen[ghostKey] = time.Now()
		w.firstSeenMu.Unlock()
		slog.DebugContext(ctx, "First time seeing path gone, starting observation window",
			"path", outputPath, "title", title, "instance", instanceName)
		return false
	}
	w.firstSeenMu.Unlock()

	const ghostObservationWindow = 60 * time.Second
	if time.Since(seenTime) < ghostObservationWindow {
		return false
	}

	// Clean up tracking entry
	w.firstSeenMu.Lock()
	delete(w.firstSeen, ghostKey)
	w.firstSeenMu.Unlock()

	slog.WarnContext(ctx, "Found ghost queue item (source path gone after observation window), cleaning up",
		"path", outputPath, "title", title, "instance", instanceName,
		"missing_duration", time.Since(seenTime))
	return true
}
