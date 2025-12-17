package fuse

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"
	"github.com/spf13/afero"
)

// Server manages the FUSE mount
type Server struct {
	mountPoint string
	fileSystem afero.Fs
	logger     *slog.Logger
	server     *fuse.Server
}

// NewServer creates a new FUSE server instance
func NewServer(mountPoint string, fileSystem afero.Fs, logger *slog.Logger) *Server {
	return &Server{
		mountPoint: mountPoint,
		fileSystem: fileSystem,
		logger:     logger,
	}
}

// Mount mounts the filesystem and starts serving
// This method blocks until the filesystem is unmounted
func (s *Server) Mount() error {
	root := NewAltMountRoot(s.fileSystem, "", s.logger)

	// Configure FUSE options
	// We want to enable some caching to avoid hitting metadata service too often
	opts := &fs.Options{
		MountOptions: fuse.MountOptions{
			AllowOther: true, // Allow other users to access the mount
			Name:       "altmount",
			// Enable debug logging if needed, could be tied to app log level
			// Debug:      true, 
		},
		// Cache timeout settings
		EntryTimeout:    &[]time.Duration{1 * time.Second}[0],
		AttrTimeout:     &[]time.Duration{1 * time.Second}[0],
		NegativeTimeout: &[]time.Duration{1 * time.Second}[0],
	}

	server, err := fs.Mount(s.mountPoint, root, opts)
	if err != nil {
		return fmt.Errorf("failed to mount FUSE filesystem: %w", err)
	}

	s.server = server
	s.logger.Info("FUSE filesystem mounted", "mountpoint", s.mountPoint)

	// Block until unmount
	s.server.Wait()
	return nil
}

// Unmount gracefully unmounts the filesystem
func (s *Server) Unmount() error {
	if s.server == nil {
		return nil
	}
	
	s.logger.Info("Unmounting FUSE filesystem", "mountpoint", s.mountPoint)
	return s.server.Unmount()
}
