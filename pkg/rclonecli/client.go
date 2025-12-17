package rclonecli

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"runtime"
	"time"
)

// Mount creates a mount using the rclone RC API with retry logic
func (m *Manager) Mount(ctx context.Context, provider, mountPath, webdavURL string) error {
	return m.mountWithRetry(ctx, provider, mountPath, webdavURL, 3)
}

// mountWithRetry attempts to mount with retry logic
func (m *Manager) mountWithRetry(ctx context.Context, provider, mountPath, webdavURL string, maxRetries int) error {
	if !m.IsReady() {
		if err := m.WaitForReady(30 * time.Second); err != nil {
			return fmt.Errorf("rclone RC server not ready: %w", err)
		}
	}

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			// Try RC unmount first (cleans up rclone internal state)
			if m.IsReady() {
				req := RCRequest{
					Command: "mount/unmount",
					Args: map[string]interface{}{
						"mountPoint": mountPath,
					},
				}
				if _, err := m.makeRequest(req, true); err != nil {
					m.logger.DebugContext(ctx, "RC unmount before retry failed (may be expected)", "err", err, "provider", provider)
				}
			}

			// Force unmount using system commands
			if err := m.forceUnmountPath(mountPath); err != nil {
				m.logger.DebugContext(ctx, "Force unmount before retry returned error (may be expected)", "err", err, "provider", provider)
			}

			// Clean up mount directory if empty
			_ = os.Remove(mountPath)

			// Wait before retry
			wait := time.Duration(attempt*2) * time.Second
			m.logger.DebugContext(ctx, "Retrying mount operation", "attempt", attempt, "provider", provider)
			time.Sleep(wait)
		}

		if err := m.performMount(ctx, provider, mountPath, webdavURL); err != nil {
			m.logger.ErrorContext(ctx, "Mount attempt failed", "err", err, "provider", provider, "attempt", attempt+1)
			continue
		}

		return nil // Success
	}
	return fmt.Errorf("mount failed for %s", provider)
}

// performMount performs a single mount attempt
func (m *Manager) performMount(ctx context.Context, provider, mountPath, webdavURL string) error {
	cfg := m.cfg.GetConfig()

	m.logger.InfoContext(ctx, "Creating mount directory", "provider", provider, "path", mountPath)
	// Create mount directory
	if err := os.MkdirAll(mountPath, 0755); err != nil {
		if !os.IsExist(err) {
			return fmt.Errorf("failed to create mount directory %s: %w", mountPath, err)
		}
	}

	// Check if already mounted
	m.mountsMutex.RLock()
	existingMount, exists := m.mounts[provider]
	m.mountsMutex.RUnlock()

	if exists && existingMount.Mounted {
		m.logger.InfoContext(ctx, "Already mounted", "provider", provider, "path", mountPath)
		return nil
	}

	// Clean up any stale mount first
	if exists && !existingMount.Mounted {
		err := m.forceUnmountPath(mountPath)
		if err != nil {
			slog.InfoContext(ctx, "Nothing to unmount", "err", err, "provider", provider, "path", mountPath)
		}
	}

	// Create rclone config for this provider
	if err := m.createConfig(provider, webdavURL, cfg.WebDAV.User, cfg.WebDAV.Password); err != nil {
		return fmt.Errorf("failed to create rclone config: %w", err)
	}

	// Prepare mount arguments
	mountArgs := map[string]interface{}{
		"fs":         fmt.Sprintf("%s:", provider),
		"mountPoint": mountPath,
	}
	mountOpt := map[string]interface{}{
		"AllowNonEmpty": cfg.RClone.AllowNonEmpty,
		"AllowOther":    cfg.RClone.AllowOther,
		"DebugFUSE":     false,
		"DeviceName":    provider,
		"VolumeName":    provider,
	}

	configOpts := make(map[string]interface{})

	if cfg.RClone.BufferSize != "" {
		configOpts["BufferSize"] = cfg.RClone.BufferSize
	}

	if len(configOpts) > 0 {
		// Only add _config if there are options to set
		mountArgs["_config"] = configOpts
	}
	vfsOpt := map[string]interface{}{
		"CacheMode": cfg.RClone.VFSCacheMode,
	}
	vfsOpt["PollInterval"] = 0 // Poll interval not supported for webdav, set to 0

	// Add VFS options if caching is enabled
	if cfg.RClone.VFSCacheMode != "off" {
		if cfg.RClone.VFSCacheMaxAge != "" {
			if attrTimeout, e := time.ParseDuration(cfg.RClone.VFSCacheMaxAge); e == nil {
				vfsOpt["CacheMaxAge"] = attrTimeout.Nanoseconds()
			}
		}
		if cfg.RClone.VFSCacheMaxSize != "" {
			vfsOpt["CacheMaxSize"] = cfg.RClone.VFSCacheMaxSize
		}
		if cfg.RClone.VFSCachePollInterval != "" {
			vfsOpt["CachePollInterval"] = cfg.RClone.VFSCachePollInterval
		}
		if cfg.RClone.VFSReadChunkSize != "" {
			vfsOpt["ChunkSize"] = cfg.RClone.VFSReadChunkSize
		}
		if cfg.RClone.VFSReadAhead != "" {
			vfsOpt["ReadAhead"] = cfg.RClone.VFSReadAhead
		}
		if cfg.RClone.NoChecksum {
			vfsOpt["NoChecksum"] = cfg.RClone.NoChecksum
		}
		if cfg.RClone.NoModTime {
			vfsOpt["NoModTime"] = cfg.RClone.NoModTime
		}
	}

	// Add mount options based on configuration
	if cfg.RClone.UID != 0 {
		mountOpt["UID"] = cfg.RClone.UID
	}
	if cfg.RClone.GID != 0 {
		mountOpt["GID"] = cfg.RClone.GID
	}
	if cfg.RClone.AttrTimeout != "" {
		if attrTimeout, e := time.ParseDuration(cfg.RClone.AttrTimeout); e == nil {
			mountOpt["AttrTimeout"] = attrTimeout.Nanoseconds()
		}
	}

	mountArgs["vfsOpt"] = vfsOpt
	mountArgs["mountOpt"] = mountOpt
	// Make the mount request
	req := RCRequest{
		Command: "mount/mount",
		Args:    mountArgs,
	}

	_, err := m.makeRequest(req, true)
	if err != nil {
		// Clean up mount point on failure
		m.forceUnmountPath(mountPath)
		return fmt.Errorf("failed to create mount for %s: %w", provider, err)
	}

	// Store mount info
	mountInfo := &MountInfo{
		Provider:   provider,
		LocalPath:  mountPath,
		WebDAVURL:  webdavURL,
		Mounted:    true,
		MountedAt:  time.Now().Format(time.RFC3339),
		ConfigName: provider,
	}

	m.mountsMutex.Lock()
	m.mounts[provider] = mountInfo
	m.mountsMutex.Unlock()

	return nil
}

// Unmount unmounts a specific provider
func (m *Manager) Unmount(ctx context.Context, provider string) error {
	return m.unmount(ctx, provider)
}

// unmount is the internal unmount function
func (m *Manager) unmount(ctx context.Context, provider string) error {
	m.mountsMutex.RLock()
	mountInfo, exists := m.mounts[provider]
	m.mountsMutex.RUnlock()

	if !exists || !mountInfo.Mounted {
		m.logger.InfoContext(ctx, "Mount not found or already unmounted", "provider", provider)
		return nil
	}

	m.logger.InfoContext(ctx, "Unmounting", "provider", provider, "path", mountInfo.LocalPath)

	// Try RC unmount first
	req := RCRequest{
		Command: "mount/unmount",
		Args: map[string]interface{}{
			"mountPoint": mountInfo.LocalPath,
		},
	}

	var rcErr error
	if m.IsReady() {
		_, rcErr = m.makeRequest(req, true)
	}

	// If RC unmount fails or server is not ready, try force unmount
	if rcErr != nil {
		m.logger.WarnContext(ctx, "RC unmount failed, trying force unmount", "err", rcErr, "provider", provider)
		if err := m.forceUnmountPath(mountInfo.LocalPath); err != nil {
			m.logger.ErrorContext(ctx, "Force unmount failed", "err", err, "provider", provider)
			// Don't return error here, update the state anyway
		}
	}

	// Update mount info
	m.mountsMutex.Lock()
	if info, exists := m.mounts[provider]; exists {
		info.Mounted = false
		info.Error = ""
		if rcErr != nil {
			info.Error = rcErr.Error()
		}
	}
	m.mountsMutex.Unlock()

	m.logger.InfoContext(ctx, "Unmount completed", "provider", provider)
	return nil
}

// UnmountAll unmounts all mounts
func (m *Manager) UnmountAll(ctx context.Context) error {
	m.mountsMutex.RLock()
	providers := make([]string, 0, len(m.mounts))
	for provider, mount := range m.mounts {
		if mount.Mounted {
			providers = append(providers, provider)
		}
	}
	m.mountsMutex.RUnlock()

	var lastError error
	for _, provider := range providers {
		if err := m.unmount(ctx, provider); err != nil {
			lastError = err
			m.logger.DebugContext(ctx, "Failed to unmount", "err", err, "provider", provider)
		}
	}

	return lastError
}

// GetMountInfo returns information about a specific mount
func (m *Manager) GetMountInfo(provider string) (*MountInfo, bool) {
	m.mountsMutex.RLock()
	defer m.mountsMutex.RUnlock()

	info, exists := m.mounts[provider]
	if !exists {
		return nil, false
	}

	// Create a copy to avoid race conditions
	mountInfo := *info
	return &mountInfo, true
}

// GetAllMounts returns information about all mounts
func (m *Manager) GetAllMounts() map[string]*MountInfo {
	m.mountsMutex.RLock()
	defer m.mountsMutex.RUnlock()

	result := make(map[string]*MountInfo, len(m.mounts))
	for provider, info := range m.mounts {
		// Create a copy to avoid race conditions
		mountInfo := *info
		result[provider] = &mountInfo
	}

	return result
}

// IsMounted checks if a provider is mounted
func (m *Manager) IsMounted(provider string) bool {
	info, exists := m.GetMountInfo(provider)
	return exists && info.Mounted
}

// RefreshDir refreshes directories in the VFS cache
func (m *Manager) RefreshDir(ctx context.Context, provider string, dirs []string) error {
	if !m.IsReady() {
		return fmt.Errorf("rclone RC server not ready")
	}

	mountInfo, exists := m.GetMountInfo(provider)
	if !exists || !mountInfo.Mounted {
		return fmt.Errorf("provider %s not mounted", provider)
	}

	// If no specific directories provided, refresh root
	if len(dirs) == 0 {
		dirs = []string{"/"}
	}
	args := map[string]interface{}{
		"fs": fmt.Sprintf("%s:", provider),
	}
	for i, dir := range dirs {
		if dir != "" {
			if i == 0 {
				args["dir"] = dir
			} else {
				args[fmt.Sprintf("dir%d", i+1)] = dir
			}
		}
	}
	req := RCRequest{
		Command: "vfs/forget",
		Args:    args,
	}

	_, err := m.makeRequest(req, true)
	if err != nil {
		m.logger.ErrorContext(ctx, "Failed to refresh directory", "err", err, "provider", provider)
		return fmt.Errorf("failed to refresh directory %s for provider %s: %w", dirs, provider, err)
	}

	req = RCRequest{
		Command: "vfs/refresh",
		Args:    args,
	}

	_, err = m.makeRequest(req, true)
	if err != nil {
		m.logger.ErrorContext(ctx, "Failed to refresh directory", "err", err, "provider", provider)
		return fmt.Errorf("failed to refresh directory %s for provider %s: %w", dirs, provider, err)
	}
	return nil
}

// createConfig creates an rclone config entry for the provider
func (m *Manager) createConfig(configName, webdavURL string, user, pass string) error {
	req := RCRequest{
		Command: "config/create",
		Args: map[string]interface{}{
			"name": configName,
			"type": "webdav",
			"parameters": map[string]interface{}{
				"url":             webdavURL,
				"vendor":          "other",
				"pacer_min_sleep": "0",
				"user":            user,
				"pass":            pass,
			},
		},
	}

	_, err := m.makeRequest(req, true)
	if err != nil {
		return fmt.Errorf("failed to create config %s: %w", configName, err)
	}
	return nil
}

// forceUnmountPath attempts to force unmount a path using system commands
func (m *Manager) forceUnmountPath(mountPath string) error {
	var methods [][]string

	if runtime.GOOS == "darwin" {
		// macOS-specific commands
		methods = [][]string{
			{"umount", "-f", mountPath},                       // macOS force unmount
			{"diskutil", "unmount", "force", mountPath},       // macOS diskutil
			{"umount", mountPath},                             // Standard unmount
		}
	} else {
		// Linux/Unix commands
		methods = [][]string{
			{"fusermount", "-uz", mountPath},
			{"umount", mountPath},
			{"umount", "-l", mountPath}, // lazy unmount
			{"fusermount3", "-uz", mountPath},
		}
	}

	for _, method := range methods {
		if err := m.tryUnmountCommand(method...); err == nil {
			m.logger.InfoContext(m.ctx, "Successfully unmounted using system command", "command", method, "path", mountPath)
			return nil
		}
	}

	return fmt.Errorf("all force unmount attempts failed for %s", mountPath)
}

// tryUnmountCommand tries to run an unmount command
func (m *Manager) tryUnmountCommand(args ...string) error {
	if len(args) == 0 {
		return fmt.Errorf("no command provided")
	}

	cmd := exec.CommandContext(m.ctx, args[0], args[1:]...)
	return cmd.Run()
}
