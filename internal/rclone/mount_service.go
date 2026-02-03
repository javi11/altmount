package rclone

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/altmount/pkg/rclonecli"
)

// MountService handles rclone mount operations using RC server
type MountService struct {
	cfm     *config.Manager
	mu      sync.RWMutex
	manager *rclonecli.Manager
	mount   *rclonecli.Mount
}

// NewMountService creates a new mount service
func NewMountService(cfm *config.Manager) *MountService {
	return &MountService{
		cfm:     cfm,
		manager: rclonecli.NewManager(cfm),
	}
}

// Start starts the mount if enabled in configuration
func (s *MountService) Start(ctx context.Context) error {
	cfg := s.cfm.GetConfig()

	// Only start if mount is enabled
	if cfg.RClone.MountEnabled == nil || !*cfg.RClone.MountEnabled {
		slog.InfoContext(ctx, "RClone mount is disabled in configuration")
		return nil
	}

	// Start RC server
	if err := s.manager.Start(ctx); err != nil {
		return fmt.Errorf("failed to start rclone RC server: %w", err)
	}

	// Create and start mount
	return s.Mount(ctx)
}

// Mount creates the rclone mount
func (s *MountService) Mount(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	cfg := s.cfm.GetConfig()

	if s.mount != nil && s.mount.IsMounted() {
		return fmt.Errorf("already mounted at %s", cfg.MountPath)
	}
	if cfg.MountPath == "" {
		return fmt.Errorf("mount point not configured")
	}

	// Create WebDAV URL
	webdavURL := fmt.Sprintf("http://localhost:%d/webdav", cfg.WebDAV.Port)

	// Unmount any existing mount first
	if s.mount != nil {
		s.mount.Unmount(ctx)
	}

	// Always create new mount instance with fresh config values
	vfsName := cfg.RClone.VFSName
	if vfsName == "" {
		vfsName = config.MountProvider
	}
	s.mount = rclonecli.NewMount(vfsName, cfg.MountPath, webdavURL, s.manager)

	if err := s.mount.Mount(ctx); err != nil {
		return fmt.Errorf("failed to mount: %w", err)
	}

	slog.InfoContext(ctx, "RClone mount started", "mount_point", cfg.MountPath)

	return nil
}

// Unmount stops the rclone mount
func (s *MountService) Unmount(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.mount == nil || !s.mount.IsMounted() {
		return nil
	}

	// Unmount
	if s.mount != nil {
		if err := s.mount.Unmount(ctx); err != nil {
			slog.ErrorContext(ctx, "Failed to unmount", "error", err)
		}
	}

	s.mount = nil

	slog.InfoContext(ctx, "RClone mount stopped")
	return nil
}

// GetStatus returns the current mount status
func (s *MountService) GetStatus() rclonecli.MountInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.mount == nil {
		return rclonecli.MountInfo{
			Mounted: false,
		}
	}

	status, _ := s.mount.GetMountInfo()
	if status == nil {
		return rclonecli.MountInfo{
			Mounted: false,
		}
	}

	return *status
}

// Stop gracefully stops the mount service
func (s *MountService) Stop(ctx context.Context) error {
	err := s.Unmount(ctx)
	if err != nil {
		return err
	}

	return s.manager.Stop()
}

// RefreshPath refreshes a path in the VFS cache
func (s *MountService) RefreshPath(ctx context.Context, path string) error {
	if s.mount == nil {
		return fmt.Errorf("mount not active")
	}

	return s.mount.RefreshDir(ctx, []string{path})
}

// StartRCServer starts the RC server if RClone is enabled but doesn't create a mount
func (s *MountService) StartRCServer(ctx context.Context) error {
	cfg := s.cfm.GetConfig()

	// Check if RClone RC is enabled
	if cfg.RClone.RCEnabled == nil || !*cfg.RClone.RCEnabled {
		slog.DebugContext(ctx, "RClone RC is disabled, skipping RC server startup")
		return nil
	}

	// Check if RC server is already running and ready
	if s.manager.IsReady() {
		slog.DebugContext(ctx, "RClone RC server is already running and ready")
		return nil
	}

	slog.InfoContext(ctx, "RClone is enabled but RC server is not running, starting RC server")

	// Start the RC server
	if err := s.manager.Start(ctx); err != nil {
		slog.ErrorContext(ctx, "Failed to start RClone RC server", "error", err)
		return fmt.Errorf("failed to start RClone RC server: %w", err)
	}

	// Wait for the server to be ready with timeout
	if err := s.manager.WaitForReady(30 * time.Second); err != nil {
		slog.WarnContext(ctx, "RClone RC server started but not ready within timeout", "error", err)
		return fmt.Errorf("RClone RC server not ready: %w", err)
	}

	slog.InfoContext(ctx, "RClone RC server started successfully")
	return nil
}

// GetManager returns the underlying rclone manager for RC operations
func (s *MountService) GetManager() *rclonecli.Manager {
	return s.manager
}
