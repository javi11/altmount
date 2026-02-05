package fuse

import (
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
	fusecache "github.com/javi11/altmount/internal/fuse/cache"
	"github.com/spf13/afero"
)

// Server manages the FUSE mount
type Server struct {
	mountPoint string
	fileSystem afero.Fs
	logger     *slog.Logger
	server     *fuse.Server
	config     config.FuseConfig
	cache      fusecache.Cache // Metadata cache (can be nil if disabled)
}

// NewServer creates a new FUSE server instance
func NewServer(mountPoint string, fileSystem afero.Fs, logger *slog.Logger, cfg config.FuseConfig) *Server {
	return &Server{
		mountPoint: mountPoint,
		fileSystem: fileSystem,
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

	// Initialize metadata cache if enabled
	var metadataCache fusecache.Cache
	if s.config.MetadataCacheEnabled != nil && *s.config.MetadataCacheEnabled {
		cfg := fusecache.Config{
			StatCacheSize:     s.config.StatCacheSize,
			DirCacheSize:      s.config.DirCacheSize,
			NegativeCacheSize: s.config.NegativeCacheSize,
			StatTTL:           time.Duration(s.config.StatCacheTTLSeconds) * time.Second,
			DirTTL:            time.Duration(s.config.DirCacheTTLSeconds) * time.Second,
			NegativeTTL:       time.Duration(s.config.NegativeCacheTTLSeconds) * time.Second,
		}
		var err error
		metadataCache, err = fusecache.NewLRUCache(cfg)
		if err != nil {
			s.logger.Warn("Failed to create metadata cache, running without cache", "error", err)
		} else {
			s.logger.Info("FUSE metadata cache enabled",
				"stat_cache_size", cfg.StatCacheSize,
				"dir_cache_size", cfg.DirCacheSize,
				"negative_cache_size", cfg.NegativeCacheSize,
				"stat_ttl", cfg.StatTTL,
				"dir_ttl", cfg.DirTTL,
				"negative_ttl", cfg.NegativeTTL)
		}
	}
	s.cache = metadataCache

	root := NewAltMountRoot(s.fileSystem, "", s.logger, uid, gid, metadataCache)

	// Configure FUSE options
	// We want to enable some caching to avoid hitting metadata service too often
	attrTimeout := time.Duration(s.config.AttrTimeoutSeconds) * time.Second
	entryTimeout := time.Duration(s.config.EntryTimeoutSeconds) * time.Second

	// Ensure timeouts are at least 1s if they were 0/defaulted weirdly, though config validator should handle it.
	if attrTimeout == 0 {
		attrTimeout = 1 * time.Second
	}
	if entryTimeout == 0 {
		entryTimeout = 1 * time.Second
	}

	opts := &fs.Options{
		MountOptions: fuse.MountOptions{
			AllowOther:   s.config.AllowOther,
			Name:         "altmount",
			Debug:        s.config.Debug,
			MaxReadAhead: s.config.MaxReadAheadMB * 1024 * 1024,
			// AsyncRead:    true,
		},
		// Cache timeout settings
		EntryTimeout:    &entryTimeout,
		AttrTimeout:     &attrTimeout,
		NegativeTimeout: &entryTimeout, // Use same as entry timeout
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

// GetCache returns the metadata cache for external access (e.g., import service invalidation).
// Returns nil if caching is disabled.
func (s *Server) GetCache() fusecache.Cache {
	return s.cache
}
