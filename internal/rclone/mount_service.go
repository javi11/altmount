package rclone

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/altmount/pkg/rclonecli"
)

const MountProvider = "usenet"

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

	if s.mount != nil && s.mount.IsMounted() {
		return fmt.Errorf("already mounted at %s", s.mount.LocalPath)
	}

	cfg := s.cfm.GetConfig()
	if cfg.MountPath == "" {
		return fmt.Errorf("mount point not configured")
	}

	// Create WebDAV URL
	webdavURL := fmt.Sprintf("http://localhost:%d", cfg.WebDAV.Port)

	// Create mount instance
	s.mount = rclonecli.NewMount(MountProvider, cfg.MountPath, webdavURL, s.manager)

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
