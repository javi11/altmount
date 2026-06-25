package importer

import (
	"context"

	"github.com/javi11/altmount/internal/database"
)

// QueueManager manages the import queue worker lifecycle
type QueueManager interface {
	// Start begins processing queue items with the configured number of workers
	Start(ctx context.Context) error
	// Stop gracefully stops all workers and waits for completion
	Stop(ctx context.Context) error
	// Pause temporarily stops processing new items (workers remain active)
	Pause()
	// Resume continues processing after a pause
	Resume()
	// IsPaused returns whether the queue is currently paused
	IsPaused() bool
	// IsRunning returns whether the queue manager is active
	IsRunning() bool
	// CancelProcessing cancels processing of a specific item
	CancelProcessing(itemID int64) error
	// ProcessItemInBackground starts processing a specific item in the background
	ProcessItemInBackground(ctx context.Context, itemID int64)
}

// DirectoryScanner provides manual directory scanning functionality
type DirectoryScanner interface {
	// StartManualScan begins scanning a directory for NZB files
	StartManualScan(scanPath string) error
	// GetScanStatus returns the current scan status
	GetScanStatus() ScanInfo
	// CancelScan cancels an in-progress scan
	CancelScan() error
}

// NzbDavImporter handles bulk import from NzbDav databases
type NzbDavImporter interface {
	// StartNzbdavImport begins importing from an NzbDav database
	StartNzbdavImport(dbPath string, blobsPath string, cleanupFile bool) error
	// GetImportStatus returns the current import status
	GetImportStatus() ImportInfo
	// CancelImport cancels an in-progress import
	CancelImport() error
}

// QueueOperations provides queue manipulation operations
type QueueOperations interface {
	// AddToQueue adds an item to the import queue
	AddToQueue(ctx context.Context, filePath string, relativePath *string, category *string, priority *database.QueuePriority, metadata *string, downloadID *string, indexer *string) (*database.ImportQueueItem, error)
	// GetQueueStats returns queue statistics
	GetQueueStats(ctx context.Context) (*database.QueueStats, error)
}

// ImportService is the main interface combining all importer capabilities
type ImportService interface {
	QueueManager
	DirectoryScanner
	NzbDavImporter
	QueueOperations

	// Close releases all resources
	Close() error
	// SetRcloneClient sets the rclone client for VFS notifications
	SetRcloneClient(client any)
	// SetArrsService sets the ARRs service for notifications
	SetArrsService(service any)
	// RegisterConfigChangeHandler registers a handler for configuration changes
	RegisterConfigChangeHandler(configManager any)
	// RegenerateMetadata attempts to rebuild metadata for a file by finding its original NZB
	RegenerateMetadata(ctx context.Context, mountRelativePath string) error
}

// HistoryRecorder records successful import events in persistent storage
type HistoryRecorder interface {
	// AddImportHistory records a successful file import
	AddImportHistory(ctx context.Context, history *database.ImportHistory) error
}
