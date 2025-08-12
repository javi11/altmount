package integration

import (
	"context"
	"fmt"
	"time"

	"github.com/javi11/altmount/internal/adapters/nzbfilesystem"
	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/altmount/internal/nzb"
	"github.com/javi11/nntppool"
	"github.com/spf13/afero"
)

// NzbConfig holds configuration for the NZB system
type NzbConfig struct {
	MainDatabasePath  string
	QueueDatabasePath string
	MountPath         string
	NzbDir            string // Directory containing NZB files
	Password          string // Global password for .bin files
	Salt              string // Global salt for .bin files
}

// NzbSystem represents the complete NZB-backed filesystem
type NzbSystem struct {
	mainDB  *database.DB      // Main database for serving files
	queueDB *database.QueueDB // Queue database for processing queue
	service *nzb.Service
	fs      afero.Fs
	pool    nntppool.UsenetConnectionPool
}

// NewNzbSystem creates a new NZB-backed virtual filesystem with two-database architecture
func NewNzbSystem(config NzbConfig, cp nntppool.UsenetConnectionPool, workers int) (*NzbSystem, error) {
	// Initialize main database (optimized for serving files)
	mainDBConfig := database.Config{
		DatabasePath: config.MainDatabasePath,
	}

	mainDB, err := database.New(mainDBConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize main database: %w", err)
	}

	// Initialize queue database (optimized for processing queue)
	queueDBConfig := database.QueueConfig{
		DatabasePath: config.QueueDatabasePath,
	}

	queueDB, err := database.NewQueueDB(queueDBConfig)
	if err != nil {
		mainDB.Close()
		return nil, fmt.Errorf("failed to initialize queue database: %w", err)
	}

	// Create NZB service using both databases
	serviceConfig := nzb.ServiceConfig{
		WatchDir:     config.NzbDir,
		ScanInterval: 30 * time.Second, // Scan every 30 seconds
		Workers:      2,                // 2 parallel workers (default)
	}

	service, err := nzb.NewService(serviceConfig, mainDB, queueDB, cp)
	if err != nil {
		mainDB.Close()
		queueDB.Close()
		return nil, fmt.Errorf("failed to create NZB service: %w", err)
	}

	if cp == nil {
		_ = service.Close()
		_ = mainDB.Close()
		_ = queueDB.Close()
		return nil, fmt.Errorf("NNTP pool is required")
	}
	if workers <= 0 {
		workers = 15
	}

	// Create NZB remote file handler with global credentials using main database
	nzbRemoteFile := nzbfilesystem.NewNzbRemoteFileWithConfig(mainDB, cp, workers, nzbfilesystem.NzbRemoteFileConfig{
		GlobalPassword: config.Password,
		GlobalSalt:     config.Salt,
	})

	// Create filesystem directly backed by NZB data from main database
	fs := nzbfilesystem.NewNzbFilesystem(nzbRemoteFile)

	ctx := context.Background()

	if err := service.Start(ctx); err != nil {
		return nil, fmt.Errorf("failed to start NZB service: %w", err)
	}

	return &NzbSystem{
		mainDB:  mainDB,
		queueDB: queueDB,
		service: service,
		fs:      fs,
		pool:    cp,
	}, nil
}

// GetQueueStats returns current queue statistics
func (ns *NzbSystem) GetQueueStats(ctx context.Context) (*database.QueueStats, error) {
	return ns.service.GetQueueStats(ctx)
}

// GetServiceStats returns service statistics including queue stats
func (ns *NzbSystem) GetServiceStats(ctx context.Context) (*nzb.ServiceStats, error) {
	return ns.service.GetStats(ctx)
}

// FileSystem returns the virtual filesystem interface
func (ns *NzbSystem) FileSystem() afero.Fs {
	return ns.fs
}

// MainDatabase returns the main database instance (for serving files)
func (ns *NzbSystem) MainDatabase() *database.DB {
	return ns.mainDB
}

// QueueDatabase returns the queue database instance (for processing queue)
func (ns *NzbSystem) QueueDatabase() *database.QueueDB {
	return ns.queueDB
}

// Database returns the main database instance (for backward compatibility)
func (ns *NzbSystem) Database() *database.DB {
	return ns.mainDB
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
	
	// Close both databases
	var lastErr error
	if err := ns.mainDB.Close(); err != nil {
		lastErr = err
	}
	if err := ns.queueDB.Close(); err != nil {
		lastErr = err
	}
	
	return lastErr
}

// GetStats returns statistics about the NZB system using main database
func (ns *NzbSystem) GetStats() (*Stats, error) {
	// TODO: Implement database queries to get statistics from main database
	// For now return empty stats - this would query ns.mainDB for actual counts
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
