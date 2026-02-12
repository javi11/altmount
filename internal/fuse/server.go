package fuse

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"time"

	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"
	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/altmount/internal/fuse/vfs"
	"github.com/javi11/altmount/internal/nzbfilesystem"
)

// Server manages the FUSE mount
type Server struct {
	mountPoint string
	nzbfs      *nzbfilesystem.NzbFilesystem
	logger     *slog.Logger
	server     *fuse.Server
	config     config.FuseConfig
	vfsm       *vfs.Manager // VFS disk cache manager (nil if disabled)
}

// NewServer creates a new FUSE server instance.
// Takes NzbFilesystem directly (no ContextAdapter needed).
func NewServer(mountPoint string, nzbfs *nzbfilesystem.NzbFilesystem, logger *slog.Logger, cfg config.FuseConfig) *Server {
	return &Server{
		mountPoint: mountPoint,
		nzbfs:      nzbfs,
		logger:     logger,
		config:     cfg,
	}
}

// getIDFromEnv parses a numeric ID from an environment variable with a default fallback
func getIDFromEnv(key string, defaultID int) int {
	if val := os.Getenv(key); val != "" {
		if id, err := strconv.Atoi(val); err == nil {
			return id
		}
	}
	return defaultID
}

// Mount mounts the filesystem and starts serving
// This method blocks until the filesystem is unmounted
func (s *Server) Mount() error {
	// Try to cleanup stale mount first
	s.CleanupMount()

	uid := uint32(getIDFromEnv("PUID", 1000))
	gid := uint32(getIDFromEnv("PGID", 1000))

	// Initialize VFS disk cache if enabled
	var vfsm *vfs.Manager
	if s.config.DiskCacheEnabled != nil && *s.config.DiskCacheEnabled {
		cachePath := s.config.DiskCachePath
		if cachePath == "" {
			cachePath = "/tmp/altmount-vfs-cache"
		}

		maxSizeGB := s.config.DiskCacheMaxSizeGB
		if maxSizeGB <= 0 {
			maxSizeGB = 10
		}

		expiryH := s.config.DiskCacheExpiryH
		if expiryH <= 0 {
			expiryH = 24
		}

		chunkSizeMB := s.config.ChunkSizeMB
		if chunkSizeMB <= 0 {
			chunkSizeMB = 4
		}

		readAheadChunks := s.config.ReadAheadChunks
		if readAheadChunks <= 0 {
			readAheadChunks = 4
		}

		vfsCfg := vfs.ManagerConfig{
			Enabled:         true,
			CachePath:       cachePath,
			MaxSizeBytes:    int64(maxSizeGB) * 1024 * 1024 * 1024,
			ExpiryDuration:  time.Duration(expiryH) * time.Hour,
			ChunkSize:       int64(chunkSizeMB) * 1024 * 1024,
			ReadAheadChunks: readAheadChunks,
		}

		var err error
		vfsm, err = vfs.NewManager(vfsCfg, s.logger.With("component", "vfs"))
		if err != nil {
			s.logger.Warn("Failed to create VFS disk cache, running without disk cache", "error", err)
		} else {
			vfsm.Start(context.Background())
			s.logger.Info("VFS disk cache enabled",
				"cache_path", cachePath,
				"max_size_gb", maxSizeGB,
				"expiry_hours", expiryH,
				"chunk_size_mb", chunkSizeMB,
				"read_ahead_chunks", readAheadChunks)
		}
	}
	s.vfsm = vfsm

	root := NewDir(s.nzbfs, "", s.logger, uid, gid, vfsm)

	// Configure FUSE options
	attrTimeout := time.Duration(s.config.AttrTimeoutSeconds) * time.Second
	entryTimeout := time.Duration(s.config.EntryTimeoutSeconds) * time.Second

	if attrTimeout == 0 {
		attrTimeout = 30 * time.Second
	}
	if entryTimeout == 0 {
		entryTimeout = 1 * time.Second
	}

	opts := &fs.Options{
		MountOptions: fuse.MountOptions{
			AllowOther:           s.config.AllowOther,
			Name:                 "altmount",
			Debug:                s.config.Debug,
			MaxReadAhead:         s.config.MaxReadAheadMB * 1024 * 1024,
			DisableXAttrs:        true,
			IgnoreSecurityLabels: true,
			MaxWrite:             1024 * 1024, // 1MB
		},
		EntryTimeout:    &entryTimeout,
		AttrTimeout:     &attrTimeout,
		NegativeTimeout: &entryTimeout,
	}

	server, err := fs.Mount(s.mountPoint, root, opts)
	if err != nil {
		if vfsm != nil {
			vfsm.Stop()
		}
		return fmt.Errorf("failed to mount FUSE filesystem: %w", err)
	}

	s.server = server
	s.logger.Info("FUSE filesystem mounted", "mountpoint", s.mountPoint)

	// Block until unmount
	s.server.Wait()

	// Stop VFS manager on unmount
	if s.vfsm != nil {
		s.vfsm.Stop()
	}

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
	return fmt.Errorf("failed to force unmount %s", s.mountPoint)
}

// CleanupMount checks for and cleans up stale mounts at the mountpoint
func (s *Server) CleanupMount() {
	_ = s.ForceUnmount()
}

