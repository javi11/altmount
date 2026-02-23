package fuse

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/altmount/internal/fuse/backend"
	"github.com/javi11/altmount/internal/nzbfilesystem"

	// Register backends via init()
	_ "github.com/javi11/altmount/internal/fuse/backend/cgofuse"
	_ "github.com/javi11/altmount/internal/fuse/backend/hanwen"
)

// StreamTracker is the subset of stream tracking needed by the FUSE layer.
// *api.StreamTracker satisfies this interface.
type StreamTracker = backend.StreamTracker

// Server manages the FUSE mount, delegating to a pluggable backend.
type Server struct {
	mountPoint    string
	nzbfs         *nzbfilesystem.NzbFilesystem
	logger        *slog.Logger
	config        config.FuseConfig
	streamTracker StreamTracker
	backendType   backend.Type

	be backend.Backend

	// ValidateMount goroutine leak guard
	validating   atomic.Int32
	lastHealthy  atomic.Bool
	lastHealthTS atomic.Int64
}

// NewServer creates a new FUSE server instance.
func NewServer(
	mountPoint string,
	nzbfs *nzbfilesystem.NzbFilesystem,
	logger *slog.Logger,
	cfg config.FuseConfig,
	st StreamTracker,
) *Server {
	bt := resolveBackendType(cfg.Backend)

	return &Server{
		mountPoint:    mountPoint,
		nzbfs:         nzbfs,
		logger:        logger,
		config:        cfg,
		streamTracker: st,
		backendType:   bt,
	}
}

// resolveBackendType determines the backend type from config, env var, or platform default.
func resolveBackendType(cfgBackend string) backend.Type {
	if cfgBackend != "" {
		return backend.Type(cfgBackend)
	}
	if env := os.Getenv("ALTMOUNT_FUSE_BACKEND"); env != "" {
		return backend.Type(env)
	}
	return backend.DefaultType()
}

// getIDFromEnv parses a numeric ID from an environment variable with a default fallback.
func getIDFromEnv(key string, defaultID int) int {
	if val := os.Getenv(key); val != "" {
		if id, err := strconv.Atoi(val); err == nil {
			return id
		}
	}
	return defaultID
}

// Mount mounts the filesystem and starts serving.
// The onReady callback is called after the kernel mount is confirmed live.
// This method blocks until the filesystem is unmounted.
func (s *Server) Mount(onReady func()) error {
	uid := uint32(getIDFromEnv("PUID", 1000))
	gid := uint32(getIDFromEnv("PGID", 1000))

	cfg := backend.Config{
		MountPoint:    s.mountPoint,
		NzbFs:         s.nzbfs,
		FuseConfig:    s.config,
		StreamTracker: s.streamTracker,
		UID:           uid,
		GID:           gid,
	}

	be, err := backend.Create(s.backendType, cfg)
	if err != nil {
		return fmt.Errorf("failed to create FUSE backend %q: %w", s.backendType, err)
	}

	s.be = be
	s.logger.Info("Using FUSE backend", "type", be.Type(), "mountpoint", s.mountPoint)

	return be.Mount(context.Background(), onReady)
}

// Unmount gracefully unmounts the filesystem, falling back to force unmount.
func (s *Server) Unmount() error {
	s.logger.Info("Unmounting FUSE filesystem", "mountpoint", s.mountPoint)

	if s.be != nil {
		return s.be.Unmount()
	}
	return nil
}

// ForceUnmount attempts to lazy/force unmount the mountpoint using platform-specific commands.
func (s *Server) ForceUnmount() error {
	if s.be != nil {
		return s.be.ForceUnmount()
	}
	return nil
}

// ValidateMount checks if the mount point is responsive by stat-ing the directory with a timeout.
// Uses an atomic guard to prevent multiple concurrent os.Stat goroutines (which leak when the
// mount is stuck). If a validation is already in-flight, returns the last cached result.
func (s *Server) ValidateMount() (bool, error) {
	if !s.validating.CompareAndSwap(0, 1) {
		healthy := s.lastHealthy.Load()
		if !healthy {
			return false, fmt.Errorf("mount point validation in progress (last check: unhealthy)")
		}
		return true, nil
	}

	type statResult struct {
		err error
	}

	ch := make(chan statResult, 1)
	go func() {
		defer s.validating.Store(0)
		_, err := os.Stat(s.mountPoint)
		ch <- statResult{err: err}
	}()

	select {
	case result := <-ch:
		if result.err != nil {
			s.lastHealthy.Store(false)
			s.lastHealthTS.Store(time.Now().UnixNano())
			return false, fmt.Errorf("mount point stat failed: %w", result.err)
		}
		s.lastHealthy.Store(true)
		s.lastHealthTS.Store(time.Now().UnixNano())
		return true, nil
	case <-time.After(5 * time.Second):
		s.lastHealthy.Store(false)
		s.lastHealthTS.Store(time.Now().UnixNano())
		return false, fmt.Errorf("mount point not responding (stat timed out after 5s)")
	}
}

// CleanupMount checks for and cleans up stale mounts at the mountpoint.
func (s *Server) CleanupMount() {
	_ = s.ForceUnmount()
}

// RefreshDirectory invalidates the kernel cache for a named directory entry.
// Only works with backends that implement backend.Refresher (e.g. hanwen).
func (s *Server) RefreshDirectory(name string) {
	if r, ok := s.be.(backend.Refresher); ok {
		r.RefreshDirectory(name)
	}
}

// BackendType returns the active backend type.
func (s *Server) BackendType() backend.Type {
	if s.be != nil {
		return s.be.Type()
	}
	return s.backendType
}
