package scanner

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/altmount/internal/nzbdav"
)

// BatchQueueAdder defines the interface for batch queue operations
type BatchQueueAdder interface {
	AddBatchToQueue(ctx context.Context, items []*database.ImportQueueItem) error
}

// MigrationRecorder defines the interface for recording import migrations
type MigrationRecorder interface {
	UpsertMigration(ctx context.Context, source, externalID, relativePath string) (int64, error)
	IsMigrationCompleted(ctx context.Context, source, externalID string) (bool, error)
	// LinkQueueItemID sets queue_item_id for all migration rows identified by
	// (source, externalIDs) where queue_item_id is currently NULL. Called after
	// AddBatchToQueue assigns IDs to the queue items.
	LinkQueueItemID(ctx context.Context, source string, externalIDs []string, queueItemID int64) error
}

// NzbDavImporter handles importing from NZBDav databases
type NzbDavImporter struct {
	batchAdder        BatchQueueAdder
	migrationRecorder MigrationRecorder
	log               *slog.Logger

	// State management
	mu         sync.RWMutex
	info       ImportInfo
	cancelFunc context.CancelFunc
	// epoch is bumped on every Start and Reset. performImport captures the epoch
	// at launch; its deferred state update is skipped if the epoch changed in the
	// meantime (Reset was called, or a new Start superseded it). This keeps
	// Reset synchronous and avoids stuck "Canceling" states when workers are slow.
	epoch int64
}

// NewNzbDavImporter creates a new NZBDav importer
func NewNzbDavImporter(batchAdder BatchQueueAdder, migrationRecorder MigrationRecorder) *NzbDavImporter {
	return &NzbDavImporter{
		batchAdder:        batchAdder,
		migrationRecorder: migrationRecorder,
		log:               slog.Default().With("component", "nzbdav-importer"),
		info:              ImportInfo{Status: ImportStatusIdle},
	}
}

// Start starts an asynchronous import from an NZBDav database
func (n *NzbDavImporter) Start(dbPath string, blobsPath string, cleanupFile bool) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	if n.info.Status == ImportStatusRunning || n.info.Status == ImportStatusCanceling {
		return fmt.Errorf("import already in progress - call reset to force clear")
	}

	// Create import context
	importCtx, cancel := context.WithCancel(context.Background())
	n.cancelFunc = cancel
	n.epoch++
	epoch := n.epoch

	// Initialize status
	n.info = ImportInfo{
		Status:  ImportStatusRunning,
		Total:   0,
		Added:   0,
		Failed:  0,
		Skipped: 0,
	}

	go n.performImport(importCtx, epoch, dbPath, blobsPath, cleanupFile)

	return nil
}

// GetStatus returns the current import status
func (n *NzbDavImporter) GetStatus() ImportInfo {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.info
}

// Cancel requests cancellation of the current import operation. Idempotent:
// calling while already canceling or idle is a no-op.
func (n *NzbDavImporter) Cancel() error {
	n.mu.Lock()
	defer n.mu.Unlock()

	if n.cancelFunc != nil {
		n.cancelFunc()
	}
	if n.info.Status == ImportStatusRunning {
		n.info.Status = ImportStatusCanceling
	}
	return nil
}

// Reset force-clears the import state to Idle regardless of current status.
// If a goroutine is still running it receives a cancellation and its deferred
// state update is invalidated via the epoch bump — the caller can immediately
// Start a new import without being blocked by a stuck "Canceling" state.
func (n *NzbDavImporter) Reset() {
	n.mu.Lock()
	defer n.mu.Unlock()

	n.epoch++
	if n.cancelFunc != nil {
		n.cancelFunc()
		n.cancelFunc = nil
	}
	n.info = ImportInfo{Status: ImportStatusIdle}
}

// performImport performs the actual import work
func (n *NzbDavImporter) performImport(ctx context.Context, epoch int64, dbPath string, blobsPath string, cleanupFile bool) {
	// Parse Database
	parser := nzbdav.NewParser(dbPath, blobsPath)
	nzbChan, errChan := parser.Parse()

	defer func() {
		n.mu.Lock()
		// Only update status if a newer Start/Reset hasn't superseded us.
		if n.epoch == epoch {
			n.info.Status = ImportStatusCompleted
			n.cancelFunc = nil
		}
		n.mu.Unlock()

		if cleanupFile {
			os.Remove(dbPath)
		}

		// Drain any remaining items from channels to prevent parser goroutine leaks
		go func() {
			for range nzbChan {
			}
		}()
		go func() {
			for range errChan {
			}
		}()
	}()

	// Create temp dir for NZBs
	nzbTempDir, err := os.MkdirTemp(os.TempDir(), "altmount-nzbdav-imports-")
	if err != nil {
		n.log.ErrorContext(ctx, "Failed to create temp directory for NZBs", "error", err)
		n.mu.Lock()
		msg := err.Error()
		n.info.LastError = &msg
		n.mu.Unlock()
		return
	}

	// Create workers
	numWorkers := 4 // Use fewer parallel workers for file creation
	var workerWg sync.WaitGroup
	batchChan := make(chan *database.ImportQueueItem, 100)

	// Start batch processor
	var batchWg sync.WaitGroup
	batchWg.Go(func() {
		n.processBatch(ctx, batchChan)
	})

	// Monitor error channel in background to catch query/DB failures early
	go func() {
		for err := range errChan {
			if err != nil {
				n.log.ErrorContext(ctx, "Error during NZBDav parsing", "error", err)
				n.mu.Lock()
				msg := err.Error()
				n.info.LastError = &msg
				n.mu.Unlock()
			}
		}
	}()

	// Start workers
	for range numWorkers {
		workerWg.Go(func() {
			for {
				select {
				case <-ctx.Done():
					return
				case res, ok := <-nzbChan:
					if !ok {
						return
					}

					n.mu.Lock()
					n.info.Total++
					n.mu.Unlock()

					item, err := n.createNzbFileAndPrepareItem(ctx, res, nzbTempDir)
					if err != nil {
						n.log.ErrorContext(ctx, "Failed to prepare item", "file", res.Name, "error", err)
						n.mu.Lock()
						n.info.Failed++
						n.mu.Unlock()
						continue
					}

					select {
					case batchChan <- item:
					case <-ctx.Done():
						return
					}
				}
			}
		})
	}

	// Wait for workers to finish processing nzbChan
	workerWg.Wait()
	close(batchChan)
	batchWg.Wait()

	// Check for parser errors
	select {
	case err := <-errChan:
		if err != nil {
			n.log.ErrorContext(ctx, "Error during NZBDav parsing", "error", err)
			n.mu.Lock()
			msg := err.Error()
			n.info.LastError = &msg
			n.mu.Unlock()
		}
	default:
	}
}

// nzbdavAlias mirrors nzbdav.ParsedNzbAlias stored in queue item metadata.
type nzbdavAlias struct {
	ID   string `json:"ID"`
	Name string `json:"Name"`
}

// processBatch batches queue items and adds them to the queue.
// It uses migrationRecorder to deduplicate already-completed items and to
// record new items before enqueueing them.
//
// After AddBatchToQueue assigns IDs to items, LinkQueueItemID is called so that
// MarkImported can later set final_path on every related migration row.
func (n *NzbDavImporter) processBatch(ctx context.Context, batchChan <-chan *database.ImportQueueItem) {
	var batch []*database.ImportQueueItem
	// batchExternalIDs[i] holds all nzbdav external IDs (canonical + aliases)
	// for batch[i]. Used to link queue_item_id after insertion.
	var batchExternalIDs [][]string

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	// extractMeta reads nzbdav-specific fields from the item metadata JSON.
	type nzbdavMeta struct {
		NzbdavID     string        `json:"nzbdav_id"`
		DavItemName  string        `json:"nzbdav_dav_item_name"`
		NzbdavAliases []nzbdavAlias `json:"nzbdav_aliases"`
	}
	extractMeta := func(item *database.ImportQueueItem) nzbdavMeta {
		if item.Metadata == nil {
			return nzbdavMeta{}
		}
		var m nzbdavMeta
		_ = json.Unmarshal([]byte(*item.Metadata), &m)
		return m
	}

	// stripNzbdavKeysFromMetadata removes nzbdav_* keys from metadata JSON,
	// retaining only other keys (e.g. extracted_files).
	stripNzbdavKeysFromMetadata := func(item *database.ImportQueueItem) {
		if item.Metadata == nil {
			return
		}
		var metaMap map[string]any
		if err := json.Unmarshal([]byte(*item.Metadata), &metaMap); err != nil {
			return
		}
		delete(metaMap, "nzbdav_id")
		delete(metaMap, "nzbdav_dav_item_name")
		delete(metaMap, "nzbdav_aliases")
		if len(metaMap) == 0 {
			item.Metadata = nil
			return
		}
		b, err := json.Marshal(metaMap)
		if err != nil {
			return
		}
		s := string(b)
		item.Metadata = &s
	}

	// fileRelPath builds the relative_path value for a migration row that maps
	// to a specific episode file within a season-pack directory. The "file:"
	// prefix signals LookupFinalPath to join the stored final_path (season dir)
	// with the episode filename.
	fileRelPath := func(name string) string {
		return "file:" + name
	}

	insertBatch := func() {
		if len(batch) == 0 {
			return
		}
		if err := n.batchAdder.AddBatchToQueue(ctx, batch); err != nil {
			n.log.ErrorContext(ctx, "Failed to add batch to queue", "count", len(batch), "error", err)
			n.mu.Lock()
			n.info.Failed += len(batch)
			n.mu.Unlock()
		} else {
			// Link queue_item_id for every migration row associated with each item.
			// AddBatchToQueue populates item.ID for all items in the slice.
			for i, item := range batch {
				if item.ID == 0 || len(batchExternalIDs[i]) == 0 {
					continue
				}
				if err := n.migrationRecorder.LinkQueueItemID(ctx, "nzbdav", batchExternalIDs[i], item.ID); err != nil {
					n.log.WarnContext(ctx, "Failed to link queue_item_id to migration rows",
						"queue_item_id", item.ID, "error", err)
				}
			}
			n.mu.Lock()
			n.info.Added += len(batch)
			n.mu.Unlock()
		}
		batch = nil
		batchExternalIDs = nil
	}

	for {
		select {
		case item, ok := <-batchChan:
			if !ok {
				insertBatch()
				return
			}

			meta := extractMeta(item)
			nzbdavID := meta.NzbdavID

			// Dedup: skip items already successfully imported.
			if nzbdavID != "" {
				completed, err := n.migrationRecorder.IsMigrationCompleted(ctx, "nzbdav", nzbdavID)
				if err != nil {
					n.log.ErrorContext(ctx, "Failed to check migration status", "nzbdav_id", nzbdavID, "error", err)
				} else if completed {
					n.mu.Lock()
					n.info.Skipped++
					n.mu.Unlock()
					continue
				}

				// Determine whether this blob represents a season pack (aliases with
				// distinct names from the canonical) or a single/duplicate release.
				isSeasonPack := false
				for _, a := range meta.NzbdavAliases {
					if a.Name != meta.DavItemName {
						isSeasonPack = true
						break
					}
				}

				// Build relative_path for the canonical migration row.
				canonicalRelPath := ""
				if item.Category != nil {
					canonicalRelPath = *item.Category
				}
				if isSeasonPack && meta.DavItemName != "" {
					// Season pack: each DavItem maps to a specific episode file.
					// Use "file:" prefix so LookupFinalPath can compute the
					// episode-specific path from the season directory.
					canonicalRelPath = fileRelPath(meta.DavItemName)
				}
				if _, err := n.migrationRecorder.UpsertMigration(ctx, "nzbdav", nzbdavID, canonicalRelPath); err != nil {
					n.log.ErrorContext(ctx, "Failed to upsert migration", "nzbdav_id", nzbdavID, "error", err)
				}

				// Register migration rows for alias DavItem IDs so the Phase 2
				// symlink rewriter can resolve every episode rclonelink.
				for _, alias := range meta.NzbdavAliases {
					aliasRelPath := canonicalRelPath // default: same as canonical (duplicate case)
					if isSeasonPack && alias.Name != "" {
						aliasRelPath = fileRelPath(alias.Name)
					}
					if _, err := n.migrationRecorder.UpsertMigration(ctx, "nzbdav", alias.ID, aliasRelPath); err != nil {
						n.log.ErrorContext(ctx, "Failed to upsert alias migration", "alias_id", alias.ID, "error", err)
					}
				}
			}

			// Collect all external IDs for this item so we can link them after insertion.
			var externalIDs []string
			if nzbdavID != "" {
				externalIDs = append(externalIDs, nzbdavID)
				for _, a := range meta.NzbdavAliases {
					externalIDs = append(externalIDs, a.ID)
				}
			}

			// Strip nzbdav_* keys from the queue item metadata — they live in
			// import_migrations now. Keep extracted_files if present.
			stripNzbdavKeysFromMetadata(item)

			batch = append(batch, item)
			batchExternalIDs = append(batchExternalIDs, externalIDs)
			if len(batch) >= 100 {
				insertBatch()
			}
		case <-ticker.C:
			insertBatch()
		case <-ctx.Done():
			insertBatch()
			return
		}
	}
}

// createNzbFileAndPrepareItem creates an NZB file and prepares a queue item
func (n *NzbDavImporter) createNzbFileAndPrepareItem(ctx context.Context, res *nzbdav.ParsedNzb, nzbTempDir string) (*database.ImportQueueItem, error) {
	// Check context before file operations
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	// Create Temp NZB File
	// Use ID to ensure uniqueness and avoid collisions with releases having the same name in the temp directory
	// but don't include it in the filename to avoid it appearing in the final folder/file names
	nzbSubDir := filepath.Join(nzbTempDir, sanitizeFilename(res.ID))
	if err := os.MkdirAll(nzbSubDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create temp NZB subdirectory: %w", err)
	}

	nzbFileName := fmt.Sprintf("%s.nzb", sanitizeFilename(res.Name))
	nzbPath := filepath.Join(nzbSubDir, nzbFileName)

	outFile, err := os.Create(nzbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create temp NZB file: %w", err)
	}

	// Copy content
	_, err = io.Copy(outFile, res.Content)
	outFile.Close()
	if err != nil {
		os.Remove(nzbPath)
		return nil, fmt.Errorf("failed to write temp NZB file content: %w", err)
	}

	// Preserve nzbdav's folder layout verbatim so the imported mount mirrors
	// the source tree. Parser supplies (Category, RelPath) as the two halves
	// of the release's parent path.
	targetCategory := res.Category
	if targetCategory == "" {
		targetCategory = "other"
	}
	if res.RelPath != "" {
		targetCategory = filepath.Join(targetCategory, res.RelPath)
	}

	priority := database.QueuePriorityNormal

	// Store original ID, optional alias IDs, and extracted files in metadata.
	// nzbdav_dav_item_name: the DavItem.Name for the canonical (episode filename for season packs).
	// nzbdav_aliases: other DavItems sharing the same blob (non-empty for season packs).
	metaMap := map[string]any{
		"nzbdav_id": res.ID,
	}
	if res.DavItemName != "" {
		metaMap["nzbdav_dav_item_name"] = res.DavItemName
	}
	if len(res.AliasDavItems) > 0 {
		metaMap["nzbdav_aliases"] = res.AliasDavItems
	}
	if len(res.ExtractedFiles) > 0 {
		metaMap["extracted_files"] = res.ExtractedFiles
	}

	metaBytes, _ := json.Marshal(metaMap)
	metaJSON := string(metaBytes)

	// Prepare item struct. RelativePath is left nil so the import mirrors the
	// nzbdav folder structure under Category without an extra user-supplied prefix.
	// Migration jobs: skip ARR scans (per-item notifications are noisy) and skip
	// post-import link creation (symlinks/STRM). Library symlinks are rewritten
	// separately by Phase 2 (RewriteLibrarySymlinks).
	item := &database.ImportQueueItem{
		NzbPath:             nzbPath,
		Category:            &targetCategory,
		Priority:            priority,
		Status:              database.QueueStatusPending,
		RetryCount:          0,
		MaxRetries:          3,
		CreatedAt:           time.Now(),
		Metadata:            &metaJSON,
		SkipArrNotification: true,
		SkipPostImportLinks: true,
	}

	return item, nil
}

// sanitizeFilename replaces invalid characters in filenames
func sanitizeFilename(name string) string {
	return strings.ReplaceAll(name, "/", "_")
}
