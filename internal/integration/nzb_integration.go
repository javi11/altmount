package integration

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/javi11/altmount/internal/adapters/nzbfilesystem"
	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/altmount/internal/importer"
	"github.com/javi11/altmount/internal/metadata"
	"github.com/javi11/altmount/internal/pool"
	"github.com/javi11/altmount/pkg/rclonecli"
	"github.com/spf13/afero"
)

// NzbConfig holds configuration for the NZB system
type NzbConfig struct {
	QueueDatabasePath   string
	MetadataRootPath    string // Path to metadata root directory
	MaxRangeSize        int64  // Maximum range size for a single request
	StreamingChunkSize  int64  // Chunk size for streaming when end=-1
	Password            string // Global password for .bin files
	Salt                string // Global salt for .bin files
	MaxProcessorWorkers int    // Number of queue workers (default: 2)
	MaxDownloadWorkers  int    // Number of download workers (default: 15)
}

// NzbSystem represents the complete NZB-backed filesystem
type NzbSystem struct {
	database       *database.DB             // Database for processing queue
	metadataReader *metadata.MetadataReader // Metadata reader for serving files
	service        *importer.Service
	fs             afero.Fs
	poolManager    pool.Manager

	// Configuration tracking for dynamic updates
	maxDownloadWorkers  int
	maxProcessorWorkers int
	configMutex         sync.RWMutex
}

// NewNzbSystem creates a new NZB-backed virtual filesystem with metadata + queue architecture
func NewNzbSystem(config NzbConfig, poolManager pool.Manager, configGetter config.ConfigGetter, rcloneClient rclonecli.RcloneRcClient) (*NzbSystem, error) {
	// Initialize metadata service for serving files
	metadataService := metadata.NewMetadataService(config.MetadataRootPath)
	metadataReader := metadata.NewMetadataReader(metadataService)

	// Initialize database (for processing queue)
	dbConfig := database.Config{
		DatabasePath: config.QueueDatabasePath,
	}

	db, err := database.NewDB(dbConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize database: %w", err)
	}

	// Set defaults for workers and scan interval if not configured
	maxProcessorWorkers := config.MaxProcessorWorkers
	if maxProcessorWorkers <= 0 {
		maxProcessorWorkers = 2 // Default: 2 parallel workers
	}

	maxDownloadWorkers := config.MaxDownloadWorkers
	if maxDownloadWorkers <= 0 {
		maxDownloadWorkers = 15 // Default: 15 download workers
	}

	// Create NZB service using metadata + queue
	serviceConfig := importer.ServiceConfig{
		Workers: maxProcessorWorkers,
	}

	// Create service with poolManager for dynamic pool access
	service, err := importer.NewService(serviceConfig, metadataService, db, poolManager, rcloneClient)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create NZB service: %w", err)
	}

	// Create health repository for file health tracking
	healthRepo := database.NewHealthRepository(db.Connection())

	// Reset all in-progress file health checks on start up
	if err := healthRepo.ResetFileAllChecking(); err != nil {
		slog.Error("failed to reset in progress file health", "err", err)
	}

	// Create metadata-based remote file handler
	metadataRemoteFile := nzbfilesystem.NewMetadataRemoteFile(
		metadataService,
		healthRepo,
		poolManager,
		configGetter,
	)

	// Create filesystem backed by metadata
	fs := nzbfilesystem.NewNzbFilesystem(metadataRemoteFile)

	ctx := context.Background()

	if err := service.Start(ctx); err != nil {
		return nil, fmt.Errorf("failed to start NZB service: %w", err)
	}

	return &NzbSystem{
		database:            db,
		metadataReader:      metadataReader,
		service:             service,
		fs:                  fs,
		poolManager:         poolManager,
		maxDownloadWorkers:  maxDownloadWorkers,
		maxProcessorWorkers: maxProcessorWorkers,
	}, nil
}

// GetQueueStats returns current queue statistics
func (ns *NzbSystem) GetQueueStats(ctx context.Context) (*database.QueueStats, error) {
	return ns.service.GetQueueStats(ctx)
}

// GetServiceStats returns service statistics including queue stats
func (ns *NzbSystem) GetServiceStats(ctx context.Context) (*importer.ServiceStats, error) {
	return ns.service.GetStats(ctx)
}

// FileSystem returns the virtual filesystem interface
func (ns *NzbSystem) FileSystem() afero.Fs {
	return ns.fs
}

// MetadataReader returns the metadata reader instance (for serving files)
func (ns *NzbSystem) MetadataReader() *metadata.MetadataReader {
	return ns.metadataReader
}

// Database returns the database instance (for processing queue)
func (ns *NzbSystem) Database() *database.DB {
	return ns.database
}

// ImporterService returns the importer service instance
func (ns *NzbSystem) ImporterService() *importer.Service {
	return ns.service
}

// StartService starts the NZB service (including background scanning and processing)
func (ns *NzbSystem) StartService(ctx context.Context) error {
	return ns.service.Start(ctx)
}

// StopService stops the NZB service
func (ns *NzbSystem) StopService(ctx context.Context) error {
	return ns.service.Stop(ctx)
}

// Close closes the NZB system and releases resources
func (ns *NzbSystem) Close() error {
	if err := ns.service.Close(); err != nil {
		return err
	}

	// Close database (metadata doesn't need closing)
	if err := ns.database.Close(); err != nil {
		return err
	}

	return nil
}

// GetStats returns statistics about the NZB system using metadata
func (ns *NzbSystem) GetStats() (*Stats, error) {
	// TODO: Implement metadata queries to get statistics
	// For now return empty stats - this would use metadata reader for actual counts
	return &Stats{
		TotalNzbFiles:     0,
		TotalVirtualFiles: 0,
		TotalSize:         0,
	}, nil
}

// Stats holds statistics about the NZB system
type Stats struct {
	TotalNzbFiles     int
	TotalVirtualFiles int
	TotalSize         int64
}

// UpdateImportWorkers - removed: processor worker changes require server restart
// The maxProcessorWorkers field is maintained for reference but changes only take effect on restart

// GetDownloadWorkers returns the current download worker count
func (ns *NzbSystem) GetDownloadWorkers() int {
	ns.configMutex.RLock()
	defer ns.configMutex.RUnlock()
	return ns.maxDownloadWorkers
}

// GetImportWorkers returns the current import processor worker count
func (ns *NzbSystem) GetImportWorkers() int {
	ns.configMutex.RLock()
	defer ns.configMutex.RUnlock()
	return ns.maxProcessorWorkers
}
