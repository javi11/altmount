package integration

import (
	"context"
	"fmt"

	"github.com/javi11/altmount/internal/adapters"
	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/altmount/internal/nzb"
	"github.com/javi11/altmount/internal/virtualfs"
	"github.com/javi11/altmount/pkg/rclonecli"
	"github.com/spf13/afero"
)

// NzbConfig holds configuration for the NZB system
type NzbConfig struct {
	DatabasePath string
	MountPath    string
	NzbDir       string // Directory containing NZB files
}

// NzbSystem represents the complete NZB-backed filesystem
type NzbSystem struct {
	db      *database.DB
	service *nzb.Service
	fs      afero.Fs
}

// NewNzbSystem creates a new NZB-backed virtual filesystem
func NewNzbSystem(config NzbConfig, rcloneCli rclonecli.RcloneRcClient) (*NzbSystem, error) {
	// Initialize database
	dbConfig := database.Config{
		DatabasePath: config.DatabasePath,
	}
	
	db, err := database.New(dbConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize database: %w", err)
	}

	// Create NZB service
	serviceConfig := nzb.ServiceConfig{
		DatabasePath: config.DatabasePath,
		WatchDir:     config.NzbDir,
		AutoImport:   false, // Disabled by default, use scanning approach
		PollInterval: 0,
	}
	
	service, err := nzb.NewService(serviceConfig)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create NZB service: %w", err)
	}

	// Create NZB remote file handler
	nzbRemoteFile := adapters.NewNzbRemoteFile(db)

	// Create virtual filesystem with NZB backing
	fs := virtualfs.New(config.MountPath, nzbRemoteFile, rcloneCli)

	return &NzbSystem{
		db:      db,
		service: service,
		fs:      fs,
	}, nil
}

// ImportNzbFile imports a single NZB file into the database
func (ns *NzbSystem) ImportNzbFile(nzbPath string) error {
	return ns.service.ImportFile(context.Background(), nzbPath)
}

// ImportNzbDirectory imports all NZB files from a directory
func (ns *NzbSystem) ImportNzbDirectory(nzbDir string) error {
	result, err := ns.service.ImportDirectory(context.Background(), nzbDir)
	if err != nil {
		return err
	}

	if len(result.FailedFiles) > 0 {
		// Return info about first failed file
		for file, errMsg := range result.FailedFiles {
			return fmt.Errorf("failed to import %s: %s", file, errMsg)
		}
	}

	return nil
}

// FileSystem returns the virtual filesystem interface
func (ns *NzbSystem) FileSystem() afero.Fs {
	return ns.fs
}

// Database returns the database instance
func (ns *NzbSystem) Database() *database.DB {
	return ns.db
}

// ScanFolder performs a comprehensive scan of the NZB directory
func (ns *NzbSystem) ScanFolder(ctx context.Context) (*nzb.ScanResult, error) {
	return ns.service.ScanFolder(ctx)
}

// ScanFolderWithProgress scans the NZB directory with progress updates
func (ns *NzbSystem) ScanFolderWithProgress(ctx context.Context, progressChan chan<- nzb.ScanProgress) (*nzb.ScanResult, error) {
	return ns.service.ScanFolderWithProgress(ctx, progressChan)
}

// ScanCustomFolder scans a custom directory with specific configuration
func (ns *NzbSystem) ScanCustomFolder(ctx context.Context, scanConfig nzb.ScannerConfig) (*nzb.ScanResult, error) {
	return ns.service.ScanCustomFolder(ctx, scanConfig)
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
		TotalNzbFiles:    0,
		TotalVirtualFiles: 0,
		TotalSize:        0,
	}, nil
}

// Stats holds statistics about the NZB system
type Stats struct {
	TotalNzbFiles     int
	TotalVirtualFiles int
	TotalSize         int64
}