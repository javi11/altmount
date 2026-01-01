package scanner

import (
	"context"
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

// NzbDavImporter handles importing from NZBDav databases
type NzbDavImporter struct {
	batchAdder BatchQueueAdder
	log        *slog.Logger

	// State management
	mu         sync.RWMutex
	info       ImportInfo
	cancelFunc context.CancelFunc
}

// NewNzbDavImporter creates a new NZBDav importer
func NewNzbDavImporter(batchAdder BatchQueueAdder) *NzbDavImporter {
	return &NzbDavImporter{
		batchAdder: batchAdder,
		log:        slog.Default().With("component", "nzbdav-importer"),
		info:       ImportInfo{Status: ImportStatusIdle},
	}
}

// Start starts an asynchronous import from an NZBDav database
func (n *NzbDavImporter) Start(dbPath string, rootFolder string, cleanupFile bool) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	if n.info.Status != ImportStatusIdle {
		return fmt.Errorf("import already in progress")
	}

	// Create import context
	importCtx, cancel := context.WithCancel(context.Background())
	n.cancelFunc = cancel

	// Initialize status
	n.info = ImportInfo{
		Status: ImportStatusRunning,
		Total:  0,
		Added:  0,
		Failed: 0,
	}

	go n.performImport(importCtx, dbPath, rootFolder, cleanupFile)

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

// performImport performs the actual import work
func (n *NzbDavImporter) performImport(ctx context.Context, dbPath string, rootFolder string, cleanupFile bool) {
	// Parse Database
	parser := nzbdav.NewParser(dbPath)
	nzbChan, errChan := parser.Parse()

	defer func() {
		n.mu.Lock()
		n.info.Status = ImportStatusIdle
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
	batchWg.Add(1)
	go func() {
		defer batchWg.Done()
		n.processBatch(ctx, batchChan)
	}()

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
	for i := 0; i < numWorkers; i++ {
		workerWg.Add(1)
		go func() {
			defer workerWg.Done()
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

					item, err := n.createNzbFileAndPrepareItem(ctx, res, rootFolder, nzbTempDir)
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
		}()
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

// processBatch batches queue items and adds them to the queue
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

	for {
		select {
		case item, ok := <-batchChan:
			if !ok {
				// Channel closed, drain remaining batch
				insertBatch()
				return
			}
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
func (n *NzbDavImporter) createNzbFileAndPrepareItem(ctx context.Context, res *nzbdav.ParsedNzb, rootFolder, nzbTempDir string) (*database.ImportQueueItem, error) {
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

	// Determine Category and Relative Path
	targetCategory := "other"
	lowerCat := strings.ToLower(res.Category)
	if strings.Contains(lowerCat, "movie") {
		targetCategory = "movies"
	} else if strings.Contains(lowerCat, "tv") || strings.Contains(lowerCat, "series") {
		targetCategory = "tv"
	}

	if res.RelPath != "" {
		targetCategory = filepath.Join(targetCategory, res.RelPath)
	}

	relPath := rootFolder
	priority := database.QueuePriorityNormal

	// Store original ID in metadata
	metaJSON := fmt.Sprintf(`{"nzbdav_id": "%s"}`, res.ID)

	// Prepare item struct
	item := &database.ImportQueueItem{
		NzbPath:      nzbPath,
		RelativePath: &relPath,
		Category:     &targetCategory,
		Priority:     priority,
		Status:       database.QueueStatusPending,
		RetryCount:   0,
		MaxRetries:   3,
		CreatedAt:    time.Now(),
		Metadata:     &metaJSON,
	}

	return item, nil
}

// sanitizeFilename replaces invalid characters in filenames
func sanitizeFilename(name string) string {
	return strings.ReplaceAll(name, "/", "_")
}
