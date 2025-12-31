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
	StartNzbdavImport(dbPath string, rootFolder string, cleanupFile bool) error
	// GetImportStatus returns the current import status
	GetImportStatus() ImportInfo
	// CancelImport cancels an in-progress import
	CancelImport() error
}

// QueueOperations provides queue manipulation operations
type QueueOperations interface {
	// AddToQueue adds an item to the import queue
	AddToQueue(ctx context.Context, filePath string, relativePath *string, category *string, priority *database.QueuePriority) (*database.ImportQueueItem, error)
	// GetQueueStats returns queue statistics
	GetQueueStats(ctx context.Context) (*database.QueueStats, error)
}

// SymlinkCreator handles symlink creation for imported files
type SymlinkCreator interface {
	// CreateSymlinks creates symlinks for an imported item
	CreateSymlinks(item *database.ImportQueueItem, resultingPath string) error
}

// StrmGenerator handles STRM file generation
type StrmGenerator interface {
	// CreateStrmFiles creates STRM files for an imported item
	CreateStrmFiles(item *database.ImportQueueItem, resultingPath string) error
}

// VFSNotifier handles rclone VFS cache notifications
type VFSNotifier interface {
	// NotifyVFS notifies rclone VFS about file changes
	NotifyVFS(ctx context.Context, resultingPath string, async bool)
	// RefreshMountPathIfNeeded refreshes the mount path cache if required
	RefreshMountPathIfNeeded(ctx context.Context, resultingPath string, itemID int64)
}

// HealthScheduler handles health check scheduling for imported files
type HealthScheduler interface {
	// ScheduleHealthCheck schedules a health check for an imported file
	ScheduleHealthCheck(ctx context.Context, filePath string, sourceNzb string, priority database.HealthPriority) error
}

// ARRNotifier handles notifications to ARR applications (Sonarr/Radarr)
type ARRNotifier interface {
	// NotifyARR notifies ARR applications about imported content
	NotifyARR(ctx context.Context, item *database.ImportQueueItem, resultingPath string) error
}

// SABnzbdFallback handles fallback to external SABnzbd for failed imports
type SABnzbdFallback interface {
	// AttemptFallback tries to send a failed import to external SABnzbd
	AttemptFallback(ctx context.Context, item *database.ImportQueueItem) error
}

// IDMetadataLinker handles NzbDav ID metadata linking
type IDMetadataLinker interface {
	// HandleIDMetadataLinks creates ID-based metadata links
	HandleIDMetadataLinks(item *database.ImportQueueItem, resultingPath string)
}

// PostProcessor coordinates all post-import processing steps
type PostProcessor interface {
	SymlinkCreator
	StrmGenerator
	VFSNotifier
	HealthScheduler
	ARRNotifier
	SABnzbdFallback
	IDMetadataLinker

	// HandleSuccess performs all post-processing for successful imports
	HandleSuccess(ctx context.Context, item *database.ImportQueueItem, resultingPath string) error
	// HandleFailure performs all cleanup for failed imports
	HandleFailure(ctx context.Context, item *database.ImportQueueItem, processingErr error) error
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
	SetRcloneClient(client interface{})
	// SetArrsService sets the ARRs service for notifications
	SetArrsService(service interface{})
	// RegisterConfigChangeHandler registers a handler for configuration changes
	RegisterConfigChangeHandler(configManager interface{})
}

// FileSizeCalculator calculates file sizes for different file types
type FileSizeCalculator interface {
	// CalculateFileSizeOnly calculates the size of a file without full processing
	CalculateFileSizeOnly(filePath string) (int64, error)
}
