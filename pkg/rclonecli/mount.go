package rclonecli

import (
	"context"
	"fmt"
	"log/slog"
)

// Mount represents a mount using the rclone RC client
type Mount struct {
	Provider  string
	LocalPath string
	WebDAVURL string
	logger    *slog.Logger
	rcManager *Manager
}

// NewMount creates a new RC-based mount
func NewMount(provider, mountPath, webdavURL string, rcManager *Manager) *Mount {
	return &Mount{
		Provider:  provider,
		LocalPath: mountPath,
		WebDAVURL: webdavURL,
		rcManager: rcManager,
		logger:    rcManager.GetLogger(),
	}
}

// Mount creates the mount using rclone RC
func (m *Mount) Mount(ctx context.Context) error {
	if m.rcManager == nil {
		return fmt.Errorf("rclone manager is not available")
	}

	// Check if already mounted
	if m.rcManager.IsMounted(m.Provider) {
		m.logger.InfoContext(ctx, "Mount is already mounted", "provider", m.Provider, "path", m.LocalPath)
		return nil
	}

	m.logger.InfoContext(ctx, "Creating mount via RC", "provider", m.Provider, "webdav_url", m.WebDAVURL, "mount_path", m.LocalPath)

	if err := m.rcManager.Mount(ctx, m.Provider, m.LocalPath, m.WebDAVURL); err != nil {
		m.logger.ErrorContext(ctx, "Mount operation failed", "provider", m.Provider)
		return fmt.Errorf("mount failed for %s", m.Provider)
	}

	m.logger.InfoContext(ctx, "Successfully mounted WebDAV via RC", "provider", m.Provider, "path", m.LocalPath)
	return nil
}

// Unmount removes the mount using rclone RC
func (m *Mount) Unmount(ctx context.Context) error {
	if m.rcManager == nil {
		m.logger.WarnContext(ctx, "Rclone manager is not available, skipping unmount")
		return nil
	}

	if !m.rcManager.IsMounted(m.Provider) {
		m.logger.InfoContext(ctx, "Mount is not mounted, skipping unmount", "provider", m.Provider)
		return nil
	}

	m.logger.InfoContext(ctx, "Unmounting via RC", "provider", m.Provider)

	if err := m.rcManager.Unmount(ctx, m.Provider); err != nil {
		return fmt.Errorf("failed to unmount %s via RC: %w", m.Provider, err)
	}

	m.logger.InfoContext(ctx, "Successfully unmounted", "provider", m.Provider)
	return nil
}

// IsMounted checks if the mount is active via RC
func (m *Mount) IsMounted() bool {
	if m.rcManager == nil {
		return false
	}
	return m.rcManager.IsMounted(m.Provider)
}

// RefreshDir refreshes directories in the mount
func (m *Mount) RefreshDir(ctx context.Context, dirs []string) error {
	if m.rcManager == nil {
		return fmt.Errorf("rclone manager is not available")
	}

	if !m.IsMounted() {
		return fmt.Errorf("provider %s not properly mounted. Skipping refreshes", m.Provider)
	}

	if err := m.rcManager.RefreshDir(ctx, m.Provider, dirs); err != nil {
		return fmt.Errorf("failed to refresh directories for %s: %w", m.Provider, err)
	}

	return nil
}

// GetMountInfo returns mount information
func (m *Mount) GetMountInfo() (*MountInfo, bool) {
	if m.rcManager == nil {
		return nil, false
	}
	return m.rcManager.GetMountInfo(m.Provider)
}
