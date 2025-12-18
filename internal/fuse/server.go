package fuse

import (
	"fmt"
	"log/slog"
	"os/exec"
	"runtime"
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
	// Try to cleanup stale mount first
	s.CleanupMount()

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

// Unmount gracefully unmounts the filesystem, falling back to force unmount
func (s *Server) Unmount() error {
	s.logger.Info("Unmounting FUSE filesystem", "mountpoint", s.mountPoint)

	if s.server != nil {
		err := s.server.Unmount()
		if err == nil {
			return nil
		}
		s.logger.Warn("Standard unmount failed, attempting force unmount", "error", err)
	}

	return s.ForceUnmount()
}

// ForceUnmount attempts to lazy/force unmount the mountpoint
func (s *Server) ForceUnmount() error {
	if runtime.GOOS == "linux" {
		// Try fusermount -uz (lazy unmount)
		if err := exec.Command("fusermount", "-uz", s.mountPoint).Run(); err == nil {
			s.logger.Info("Successfully lazy unmounted using fusermount")
			return nil
		}
		// Fallback to umount -l
		if err := exec.Command("umount", "-l", s.mountPoint).Run(); err == nil {
			s.logger.Info("Successfully lazy unmounted using umount")
			return nil
		}
	}
	// Add macOS/Windows logic if needed, but Linux is primary target
	return fmt.Errorf("failed to force unmount %s", s.mountPoint)
}

// CleanupMount checks for and cleans up stale mounts at the mountpoint
func (s *Server) CleanupMount() {
	// Simple check: try to unmount. If it fails, it probably wasn't mounted or we lack perms.
	// We ignore errors here as we just want to ensure it's clean for the new mount.
	_ = s.ForceUnmount()
}