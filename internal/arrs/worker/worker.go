package worker

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/javi11/altmount/internal/arrs/clients"
	"github.com/javi11/altmount/internal/arrs/instances"
	"github.com/javi11/altmount/internal/arrs/model"
	"github.com/javi11/altmount/internal/config"
	"golift.io/starr"
)

type Worker struct {
	configGetter config.ConfigGetter
	instances    *instances.Manager
	clients      *clients.Manager

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

func NewWorker(configGetter config.ConfigGetter, instances *instances.Manager, clients *clients.Manager) *Worker {
	return &Worker{
		configGetter: configGetter,
		instances:    instances,
		clients:      clients,
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

	// Queue cleanup is enabled by default (when nil or true)
	if cfg.Arrs.QueueCleanupEnabled != nil && !*cfg.Arrs.QueueCleanupEnabled {
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
	if err := w.CleanupQueue(w.workerCtx); err != nil {
		slog.Error("Queue cleanup failed", "error", err)
	}
}

// CleanupQueue checks all ARR instances for importPending items with empty folders
// and removes them from the queue after deleting the empty folder
func (w *Worker) CleanupQueue(ctx context.Context) error {
	cfg := w.configGetter()
	instances := w.instances.GetAllInstances()

	for _, instance := range instances {
		if !instance.Enabled {
			continue
		}

		switch instance.Type {
		case "radarr":
			if err := w.cleanupRadarrQueue(ctx, instance, cfg); err != nil {
				slog.WarnContext(ctx, "Failed to cleanup Radarr queue",
					"instance", instance.Name, "error", err)
			}
		case "sonarr":
			if err := w.cleanupSonarrQueue(ctx, instance, cfg); err != nil {
				slog.WarnContext(ctx, "Failed to cleanup Sonarr queue",
					"instance", instance.Name, "error", err)
			}
		}
	}

	return nil
}

func (w *Worker) cleanupRadarrQueue(ctx context.Context, instance *model.ConfigInstance, cfg *config.Config) error {
	client, err := w.clients.GetOrCreateRadarrClient(instance.Name, instance.URL, instance.APIKey)
	if err != nil {
		return fmt.Errorf("failed to get Radarr client: %w", err)
	}

	queue, err := client.GetQueueContext(ctx, 0, 500)
	if err != nil {
		return fmt.Errorf("failed to get Radarr queue: %w", err)
	}

	var idsToRemove []int64
	for _, q := range queue.Records {
		// Check for completed items with warning status that are pending import
		if q.Status != "completed" || q.TrackedDownloadStatus != "warning" || (q.TrackedDownloadState != "importPending" && q.TrackedDownloadState != "importBlocked") {
			continue
		}

		// Check if path is within managed directories (import_dir, mount_path, or complete_dir)
		if !w.isPathManaged(q.OutputPath, cfg) {
			continue
		}

		// Check status messages for known issues
		shouldCleanup := false
		for _, msg := range q.StatusMessages {
			allMessages := strings.Join(msg.Messages, " ")

			// Automatic import failure cleanup (configurable)
			if cfg.Arrs.CleanupAutomaticImportFailure != nil && *cfg.Arrs.CleanupAutomaticImportFailure &&
				strings.Contains(allMessages, "Automatic import is not possible") {
				shouldCleanup = true
				break
			}

			// Check configured allowlist
			for _, allowedMsg := range cfg.Arrs.QueueCleanupAllowlist {
				if allowedMsg.Enabled && (strings.Contains(allMessages, allowedMsg.Message) || strings.Contains(msg.Title, allowedMsg.Message)) {
					shouldCleanup = true
					break
				}
			}

			if shouldCleanup {
				break
			}
		}

		if shouldCleanup {
			key := fmt.Sprintf("%s|%d", instance.Name, q.ID)
			w.firstSeenMu.Lock()
			seenTime, exists := w.firstSeen[key]
			if !exists {
				w.firstSeen[key] = time.Now()
				w.firstSeenMu.Unlock()
				slog.DebugContext(ctx, "First saw failed import pending item, starting grace period",
					"path", q.OutputPath, "title", q.Title, "instance", instance.Name)
				continue
			}
			w.firstSeenMu.Unlock()

			gracePeriod := time.Duration(cfg.Arrs.QueueCleanupGracePeriodMinutes) * time.Minute
			if time.Since(seenTime) < gracePeriod {
				slog.DebugContext(ctx, "Item still in grace period",
					"path", q.OutputPath, "title", q.Title, "instance", instance.Name,
					"remaining", gracePeriod-time.Since(seenTime))
				continue
			}

			slog.InfoContext(ctx, "Found failed import pending item after grace period",
				"path", q.OutputPath, "title", q.Title, "instance", instance.Name)
			idsToRemove = append(idsToRemove, q.ID)

			w.firstSeenMu.Lock()
			delete(w.firstSeen, key)
			w.firstSeenMu.Unlock()
		} else {
			// If it's no longer matching failure criteria, remove from tracking
			key := fmt.Sprintf("%s|%d", instance.Name, q.ID)
			w.firstSeenMu.Lock()
			delete(w.firstSeen, key)
			w.firstSeenMu.Unlock()
		}
	}

	// Remove from ARR queue with removeFromClient and blocklist flags
	if len(idsToRemove) > 0 {
		removeFromClient := true
		opts := &starr.QueueDeleteOpts{
			RemoveFromClient: &removeFromClient,
			BlockList:        false,
			SkipRedownload:   false,
		}
		for _, id := range idsToRemove {
			if err := client.DeleteQueueContext(ctx, id, opts); err != nil {
				slog.ErrorContext(ctx, "Failed to delete queue item",
					"id", id, "error", err)
			}
		}
		slog.InfoContext(ctx, "Cleaned up Radarr queue items",
			"instance", instance.Name, "count", len(idsToRemove))
	}
	return nil
}

func (w *Worker) cleanupSonarrQueue(ctx context.Context, instance *model.ConfigInstance, cfg *config.Config) error {
	client, err := w.clients.GetOrCreateSonarrClient(instance.Name, instance.URL, instance.APIKey)
	if err != nil {
		return fmt.Errorf("failed to get Sonarr client: %w", err)
	}

	queue, err := client.GetQueueContext(ctx, 0, 500)
	if err != nil {
		return fmt.Errorf("failed to get Sonarr queue: %w", err)
	}

	var idsToRemove []int64
	for _, q := range queue.Records {
		// Check for completed items with warning status that are pending import
		if q.Protocol != "usenet" || q.Status != "completed" || q.TrackedDownloadStatus != "warning" || (q.TrackedDownloadState != "importPending" && q.TrackedDownloadState != "importBlocked") {
			continue
		}

		// Check if path is within managed directories (import_dir, mount_path, or complete_dir)
		if !w.isPathManaged(q.OutputPath, cfg) {
			continue
		}

		// Check status messages for known issues
		shouldCleanup := false
		for _, msg := range q.StatusMessages {
			allMessages := strings.Join(msg.Messages, " ")

			// Automatic import failure cleanup (configurable)
			if cfg.Arrs.CleanupAutomaticImportFailure != nil && *cfg.Arrs.CleanupAutomaticImportFailure &&
				strings.Contains(allMessages, "Automatic import is not possible") {
				shouldCleanup = true
				break
			}

			// Check configured allowlist
			for _, allowedMsg := range cfg.Arrs.QueueCleanupAllowlist {
				if allowedMsg.Enabled && (strings.Contains(allMessages, allowedMsg.Message) || strings.Contains(msg.Title, allowedMsg.Message)) {
					shouldCleanup = true
					break
				}
			}

			if shouldCleanup {
				break
			}
		}

		if shouldCleanup {
			key := fmt.Sprintf("%s|%d", instance.Name, q.ID)
			w.firstSeenMu.Lock()
			seenTime, exists := w.firstSeen[key]
			if !exists {
				w.firstSeen[key] = time.Now()
				w.firstSeenMu.Unlock()
				slog.DebugContext(ctx, "First saw failed import pending item, starting grace period",
					"path", q.OutputPath, "title", q.Title, "instance", instance.Name)
				continue
			}
			w.firstSeenMu.Unlock()

			gracePeriod := time.Duration(cfg.Arrs.QueueCleanupGracePeriodMinutes) * time.Minute
			if time.Since(seenTime) < gracePeriod {
				slog.DebugContext(ctx, "Item still in grace period",
					"path", q.OutputPath, "title", q.Title, "instance", instance.Name,
					"remaining", gracePeriod-time.Since(seenTime))
				continue
			}

			slog.InfoContext(ctx, "Found failed import pending item after grace period",
				"path", q.OutputPath, "title", q.Title, "instance", instance.Name)
			idsToRemove = append(idsToRemove, q.ID)

			w.firstSeenMu.Lock()
			delete(w.firstSeen, key)
			w.firstSeenMu.Unlock()
		} else {
			// If it's no longer matching failure criteria, remove from tracking
			key := fmt.Sprintf("%s|%d", instance.Name, q.ID)
			w.firstSeenMu.Lock()
			delete(w.firstSeen, key)
			w.firstSeenMu.Unlock()
		}
	}

	// Remove from ARR queue with removeFromClient and blocklist flags
	if len(idsToRemove) > 0 {
		removeFromClient := true
		opts := &starr.QueueDeleteOpts{
			RemoveFromClient: &removeFromClient,
			BlockList:        false,
			SkipRedownload:   false,
		}
		for _, id := range idsToRemove {
			if err := client.DeleteQueueContext(ctx, id, opts); err != nil {
				slog.ErrorContext(ctx, "Failed to delete queue item",
					"id", id, "error", err)
			}
		}
		slog.InfoContext(ctx, "Cleaned up Sonarr queue items",
			"instance", instance.Name, "count", len(idsToRemove))
	}
	return nil
}

func (w *Worker) isPathManaged(path string, cfg *config.Config) bool {
	if path == "" {
		return false
	}

	cleanPath := filepath.Clean(path)

	// Check import_dir
	if cfg.Import.ImportDir != nil && *cfg.Import.ImportDir != "" {
		importDir := filepath.Clean(*cfg.Import.ImportDir)
		if strings.HasPrefix(cleanPath, importDir) {
			return true
		}
	}

	// Check mount_path
	if cfg.MountPath != "" {
		mountPath := filepath.Clean(cfg.MountPath)
		if strings.HasPrefix(cleanPath, mountPath) {
			return true
		}
	}

	// Check sabnzbd complete_dir
	if cfg.SABnzbd.Enabled != nil && *cfg.SABnzbd.Enabled && cfg.SABnzbd.CompleteDir != "" {
		completeDir := filepath.Clean(cfg.SABnzbd.CompleteDir)
		if strings.HasPrefix(cleanPath, completeDir) {
			return true
		}
	}

	return false
}