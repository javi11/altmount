package integration

import (
	"context"
	"fmt"
	"time"

	"github.com/javi11/altmount/internal/adapters/nzbfilesystem"
	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/altmount/internal/importer"
	"github.com/javi11/altmount/internal/metadata"
	"github.com/javi11/nntppool"
	"github.com/spf13/afero"
)

// NzbConfig holds configuration for the NZB system
type NzbConfig struct {
	QueueDatabasePath  string
	MetadataRootPath   string // Path to metadata root directory
	MaxRangeSize       int64  // Maximum range size for a single request
	StreamingChunkSize int64  // Chunk size for streaming when end=-1
	MountPath          string
	NzbDir             string        // Directory containing NZB files
	Password           string        // Global password for .bin files
	Salt               string        // Global salt for .bin files
	ProcessorWorkers   int           // Number of queue workers (default: 2)
	DownloadWorkers    int           // Number of download workers (default: 15)
	ScanInterval       time.Duration // Directory scan interval (default: 30s)
}

// NzbSystem represents the complete NZB-backed filesystem
type NzbSystem struct {
	queueDB        *database.QueueDB        // Queue database for processing queue
	metadataReader *metadata.MetadataReader // Metadata reader for serving files
	service        *importer.Service
	fs             afero.Fs
	pool           nntppool.UsenetConnectionPool
}

// NewNzbSystem creates a new NZB-backed virtual filesystem with metadata + queue architecture
func NewNzbSystem(config NzbConfig, cp nntppool.UsenetConnectionPool) (*NzbSystem, error) {
	// Initialize metadata service for serving files
	metadataService := metadata.NewMetadataService(config.MetadataRootPath)
	metadataReader := metadata.NewMetadataReader(metadataService)

	// Initialize queue database (for processing queue)
	queueDBConfig := database.QueueConfig{
		DatabasePath: config.QueueDatabasePath,
	}

	queueDB, err := database.NewQueueDB(queueDBConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize queue database: %w", err)
	}

	// Set defaults for workers and scan interval if not configured
	processorWorkers := config.ProcessorWorkers
	if processorWorkers <= 0 {
		processorWorkers = 2 // Default: 2 parallel workers
	}

	downloadWorkers := config.DownloadWorkers
	if downloadWorkers <= 0 {
		downloadWorkers = 15 // Default: 15 download workers
	}

	scanInterval := config.ScanInterval
	if scanInterval <= 0 {
		scanInterval = 30 * time.Second // Default: scan every 30 seconds
	}

	// Create NZB service using metadata + queue
	serviceConfig := importer.ServiceConfig{
		WatchDir:     config.NzbDir,
		ScanInterval: scanInterval,
		Workers:      processorWorkers,
	}

	// For now, we'll need to create a service that uses MetadataProcessor
	// This will be updated when we modify the NZB service
	service, err := importer.NewService(serviceConfig, metadataService, queueDB, cp) // nil for mainDB since we're using metadata
	if err != nil {
		queueDB.Close()
		return nil, fmt.Errorf("failed to create NZB service: %w", err)
	}

	if cp == nil {
		_ = service.Close()
		_ = queueDB.Close()
		return nil, fmt.Errorf("NNTP pool is required")
	}

	// Create health repository for file health tracking
	healthRepo := database.NewHealthRepository(queueDB.Connection())

	// Create metadata-based remote file handler
	metadataRemoteFile := nzbfilesystem.NewMetadataRemoteFile(
		metadataService,
		healthRepo,
		cp,
		downloadWorkers,
		nzbfilesystem.MetadataRemoteFileConfig{
			GlobalPassword:     config.Password,
			GlobalSalt:         config.Salt,
			MaxRangeSize:       config.MaxRangeSize,
			StreamingChunkSize: config.StreamingChunkSize,
		},
	)

	// Create filesystem backed by metadata
	fs := nzbfilesystem.NewNzbFilesystem(metadataRemoteFile)

	ctx := context.Background()

	if err := service.Start(ctx); err != nil {
		return nil, fmt.Errorf("failed to start NZB service: %w", err)
	}

	return &NzbSystem{
		queueDB:        queueDB,
		metadataReader: metadataReader,
		service:        service,
		fs:             fs,
		pool:           cp,
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

// QueueDatabase returns the queue database instance (for processing queue)
func (ns *NzbSystem) QueueDatabase() *database.QueueDB {
	return ns.queueDB
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

	// Close queue database (metadata doesn't need closing)
	if err := ns.queueDB.Close(); err != nil {
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
