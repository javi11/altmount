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
	DatabasePath string
	MountPath    string
	NzbDir       string // Directory containing NZB files
	Password     string // Global password for .bin files
	Salt         string // Global salt for .bin files
}

// NzbSystem represents the complete NZB-backed filesystem
type NzbSystem struct {
	db      *database.DB
	service *nzb.Service
	fs      afero.Fs
	pool    nntppool.UsenetConnectionPool
}

// NewNzbSystem creates a new NZB-backed virtual filesystem
func NewNzbSystem(config NzbConfig, cp nntppool.UsenetConnectionPool, workers int) (*NzbSystem, error) {
	// Initialize database
	dbConfig := database.Config{
		DatabasePath: config.DatabasePath,
	}

	db, err := database.New(dbConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize database: %w", err)
	}

	// Create simplified NZB service using existing database instance
	serviceConfig := nzb.ServiceConfig{
		WatchDir:     config.NzbDir,
		ScanInterval: 30 * time.Second, // Scan every 30 seconds
		Workers:      2,                // 2 parallel workers (default)
	}

	service, err := nzb.NewService(serviceConfig, db, cp)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create NZB service: %w", err)
	}

	if cp == nil {
		_ = service.Close()
		_ = db.Close()
		return nil, fmt.Errorf("NNTP pool is required")
	}
	if workers <= 0 {
		workers = 15
	}

	// Create NZB remote file handler with global credentials
	nzbRemoteFile := nzbfilesystem.NewNzbRemoteFileWithConfig(db, cp, workers, nzbfilesystem.NzbRemoteFileConfig{
		GlobalPassword: config.Password,
		GlobalSalt:     config.Salt,
	})

	// Create filesystem directly backed by NZB data
	fs := nzbfilesystem.NewNzbFilesystem(nzbRemoteFile)

	ctx := context.Background()

	if err := service.Start(ctx); err != nil {
		return nil, fmt.Errorf("failed to start NZB service: %w", err)
	}

	return &NzbSystem{
		db:      db,
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

// Database returns the database instance
func (ns *NzbSystem) Database() *database.DB {
	return ns.db
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
	return ns.db.Close()
}

// GetStats returns statistics about the NZB system
func (ns *NzbSystem) GetStats() (*Stats, error) {
	// TODO: Implement database queries to get statistics
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
