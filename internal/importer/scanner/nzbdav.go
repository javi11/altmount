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
		return fmt.Errorf("import already in progress")
	}

	// Create import context
	importCtx, cancel := context.WithCancel(context.Background())
	n.cancelFunc = cancel

	// Initialize status
	n.info = ImportInfo{
		Status:  ImportStatusRunning,
		Total:   0,
		Added:   0,
		Failed:  0,
		Skipped: 0,
	}

	go n.performImport(importCtx, dbPath, blobsPath, cleanupFile)

	return nil
}

// GetStatus returns the current import status
func (n *NzbDavImporter) GetStatus() ImportInfo {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.info
}

// Cancel cancels the current import operation
func (n *NzbDavImporter) Cancel() error {
	n.mu.Lock()
	defer n.mu.Unlock()

	if n.info.Status == ImportStatusIdle {
		return fmt.Errorf("no import is currently running")
	}

	if n.info.Status == ImportStatusCanceling {
		return fmt.Errorf("import is already being canceled")
	}

	n.info.Status = ImportStatusCanceling
	if n.cancelFunc != nil {
		n.cancelFunc()
	}

	return nil
}

// Reset resets the import status to Idle
func (n *NzbDavImporter) Reset() {
	n.mu.Lock()
	defer n.mu.Unlock()

	if n.info.Status == ImportStatusCompleted || n.info.Status == ImportStatusIdle {
		n.info = ImportInfo{Status: ImportStatusIdle}
	}
}

// performImport performs the actual import work
func (n *NzbDavImporter) performImport(ctx context.Context, dbPath string, blobsPath string, cleanupFile bool) {
	// Parse Database
	parser := nzbdav.NewParser(dbPath, blobsPath)
	nzbChan, errChan := parser.Parse()

	defer func() {
		n.mu.Lock()
		n.info.Status = ImportStatusCompleted
		n.cancelFunc = nil
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

// processBatch batches queue items and adds them to the queue.
// It uses migrationRecorder to deduplicate already-completed items and to
// record new items before enqueueing them.
func (n *NzbDavImporter) processBatch(ctx context.Context, batchChan <-chan *database.ImportQueueItem) {
	var batch []*database.ImportQueueItem
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	insertBatch := func() {
		if len(batch) > 0 {
			if err := n.batchAdder.AddBatchToQueue(ctx, batch); err != nil {
				n.log.ErrorContext(ctx, "Failed to add batch to queue", "count", len(batch), "error", err)
				n.mu.Lock()
				n.info.Failed += len(batch)
				n.mu.Unlock()
			} else {
				n.mu.Lock()
				n.info.Added += len(batch)
				n.mu.Unlock()
			}
			batch = nil // Reset batch
		}
	}

	// extractNzbdavID reads nzbdav_id from the item metadata JSON.
	extractNzbdavID := func(item *database.ImportQueueItem) string {
		if item.Metadata == nil {
			return ""
		}
		var meta struct {
			NzbdavID string `json:"nzbdav_id"`
		}
		if err := json.Unmarshal([]byte(*item.Metadata), &meta); err != nil {
			return ""
		}
		return meta.NzbdavID
	}

	// stripNzbdavIDFromMetadata rewrites the metadata JSON removing the nzbdav_id key,
	// retaining only other keys (e.g. extracted_files).
	stripNzbdavIDFromMetadata := func(item *database.ImportQueueItem) {
		if item.Metadata == nil {
			return
		}
		var metaMap map[string]any
		if err := json.Unmarshal([]byte(*item.Metadata), &metaMap); err != nil {
			return
		}
		delete(metaMap, "nzbdav_id")
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

	for {
		select {
		case item, ok := <-batchChan:
			if !ok {
				// Channel closed, drain remaining batch
				insertBatch()
				return
			}

			nzbdavID := extractNzbdavID(item)

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

				// Record migration row before enqueueing.
				relativePath := ""
				if item.Category != nil {
					relativePath = *item.Category
				}
				if _, err := n.migrationRecorder.UpsertMigration(ctx, "nzbdav", nzbdavID, relativePath); err != nil {
					n.log.ErrorContext(ctx, "Failed to upsert migration", "nzbdav_id", nzbdavID, "error", err)
				}
			}

			// Strip nzbdav_id from the queue item metadata — it lives in
			// import_migrations now. Keep extracted_files if present.
			stripNzbdavIDFromMetadata(item)

			batch = append(batch, item)
			if len(batch) >= 100 { // Batch size
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

	// Store original ID and extracted files in metadata
	metaMap := map[string]any{
		"nzbdav_id": res.ID,
	}
	if len(res.ExtractedFiles) > 0 {
		metaMap["extracted_files"] = res.ExtractedFiles
	}

	metaBytes, _ := json.Marshal(metaMap)
	metaJSON := string(metaBytes)

	// Prepare item struct. RelativePath is left nil so the import mirrors the
	// nzbdav folder structure under Category without an extra user-supplied prefix.
	// SkipArrNotification is true because nzbdav imports are migration jobs — ARR
	// scans should not be triggered for each individual item.
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
	}

	return item, nil
}

// sanitizeFilename replaces invalid characters in filenames
func sanitizeFilename(name string) string {
	return strings.ReplaceAll(name, "/", "_")
}
