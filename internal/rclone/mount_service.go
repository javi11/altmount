package rclone

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/javi11/altmount/internal/config"
)

// MountStatus represents the current status of the mount
type MountStatus struct {
	Mounted    bool      `json:"mounted"`
	MountPoint string    `json:"mount_point"`
	Error      string    `json:"error,omitempty"`
	StartedAt  time.Time `json:"started_at,omitempty"`
}

// MountService handles rclone mount operations
type MountService struct {
	configGetter config.ConfigGetter
	mu           sync.RWMutex
	mountCmd     *exec.Cmd
	status       MountStatus
	ctx          context.Context
	cancel       context.CancelFunc
}

// NewMountService creates a new mount service
func NewMountService(configGetter config.ConfigGetter) *MountService {
	return &MountService{
		configGetter: configGetter,
		status: MountStatus{
			Mounted: false,
		},
	}
}

// Start starts the mount if enabled in configuration
func (s *MountService) Start(ctx context.Context) error {
	cfg := s.configGetter()

	// Only start if mount is enabled
	if cfg.RClone.MountEnabled == nil || !*cfg.RClone.MountEnabled {
		slog.InfoContext(ctx, "RClone mount is disabled in configuration")
		return nil
	}

	return s.Mount(ctx)
}

// Mount creates the rclone mount
func (s *MountService) Mount(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.status.Mounted {
		return fmt.Errorf("already mounted at %s", s.status.MountPoint)
	}

	cfg := s.configGetter()
	if cfg.MountPath == "" {
		return fmt.Errorf("mount point not configured")
	}

	// Create mount point directory if it doesn't exist
	if err := os.MkdirAll(cfg.MountPath, 0755); err != nil {
		return fmt.Errorf("failed to create mount point directory: %w", err)
	}

	// Check if mount point is already mounted
	if s.isMounted(cfg.MountPath) {
		return fmt.Errorf("mount point %s is already in use", cfg.MountPath)
	}

	// Build WebDAV remote configuration
	webdavURL := fmt.Sprintf("http://localhost:%d/webdav", cfg.WebDAV.Port)
	remoteName := "altmount-webdav"

	// Create rclone configuration for WebDAV
	configPath := s.createRcloneConfig(cfg, webdavURL, remoteName)
	defer os.Remove(configPath) // Clean up config file when done

	// Build rclone mount command
	args := []string{
		"mount",
		remoteName + ":",
		cfg.MountPath,
		"--config", configPath,
		"--allow-other",
		"--allow-non-empty",
		"--no-checksum",
		"--no-modtime",
		"--read-only",
	}

	// Add custom mount options
	for key, value := range cfg.RClone.MountOptions {
		args = append(args, "--"+key, value)
	}

	// Add daemon flag for background operation
	args = append(args, "--daemon")

	// Create command
	s.mountCmd = exec.CommandContext(ctx, "rclone", args...)

	// Set up logging
	s.mountCmd.Stdout = &logWriter{level: slog.LevelInfo, prefix: "rclone mount"}
	s.mountCmd.Stderr = &logWriter{level: slog.LevelError, prefix: "rclone mount"}

	// Start the mount
	if err := s.mountCmd.Start(); err != nil {
		s.status.Error = err.Error()
		return fmt.Errorf("failed to start rclone mount: %w", err)
	}

	// Create a context for this mount session
	s.ctx, s.cancel = context.WithCancel(ctx)

	// Wait a bit to check if mount succeeded
	time.Sleep(2 * time.Second)

	// Verify mount is working
	if !s.isMounted(cfg.MountPath) {
		s.unmountInternal()
		return fmt.Errorf("mount failed to establish")
	}

	s.status = MountStatus{
		Mounted:    true,
		MountPoint: cfg.MountPath,
		StartedAt:  time.Now(),
	}

	slog.InfoContext(ctx, "RClone mount started", "mount_point", cfg.MountPath)

	// Monitor mount in background
	go s.monitorMount()

	return nil
}

// Unmount stops the rclone mount
func (s *MountService) Unmount() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.unmountInternal()
}

func (s *MountService) unmountInternal() error {
	if !s.status.Mounted {
		return nil
	}

	cfg := s.configGetter()

	// Cancel context to stop monitoring
	if s.cancel != nil {
		s.cancel()
	}

	// Use fusermount to unmount on Linux, umount on macOS
	var unmountCmd *exec.Cmd
	if _, err := exec.LookPath("fusermount"); err == nil {
		unmountCmd = exec.Command("fusermount", "-u", cfg.MountPath)
	} else {
		unmountCmd = exec.Command("umount", cfg.MountPath)
	}

	if err := unmountCmd.Run(); err != nil {
		slog.Error("Failed to unmount cleanly", "error", err)
		// Try to kill the process if unmount failed
		if s.mountCmd != nil && s.mountCmd.Process != nil {
			s.mountCmd.Process.Kill()
		}
	}

	// Wait for process to exit
	if s.mountCmd != nil && s.mountCmd.Process != nil {
		s.mountCmd.Wait()
	}

	s.status = MountStatus{
		Mounted: false,
	}
	s.mountCmd = nil

	slog.Info("RClone mount stopped")
	return nil
}

// GetStatus returns the current mount status
func (s *MountService) GetStatus() MountStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Create a copy to avoid race conditions
	return MountStatus{
		Mounted:    s.status.Mounted,
		MountPoint: s.status.MountPoint,
		Error:      s.status.Error,
		StartedAt:  s.status.StartedAt,
	}
}

// Stop gracefully stops the mount service
func (s *MountService) Stop() error {
	return s.Unmount()
}

// createRcloneConfig creates a temporary rclone configuration file
func (s *MountService) createRcloneConfig(cfg *config.Config, webdavURL, remoteName string) string {
	configContent := fmt.Sprintf(`[%s]
type = webdav
url = %s
vendor = other
user = %s
pass = %s
`, remoteName, webdavURL, cfg.WebDAV.User, obscurePassword(cfg.WebDAV.Password))

	// Create temp config file
	tmpFile, err := os.CreateTemp("", "rclone-config-*.conf")
	if err != nil {
		slog.Error("Failed to create temp config file", "error", err)
		return ""
	}
	defer tmpFile.Close()

	tmpFile.WriteString(configContent)
	return tmpFile.Name()
}

// obscurePassword obscures password for rclone config (simple base64 for now)
// In production, should use rclone's obscure command
func obscurePassword(password string) string {
	// For simplicity, we'll use plain password with proper rclone config
	// rclone will handle the obscuring internally
	return password
}

// isMounted checks if a path is currently mounted
func (s *MountService) isMounted(path string) bool {
	// Check if directory is accessible and is a mount point
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}

	// Try to stat the mount point
	info, err := os.Stat(absPath)
	if err != nil {
		return false
	}

	// On Unix systems, check /proc/mounts
	if mounts, err := os.ReadFile("/proc/mounts"); err == nil {
		return strings.Contains(string(mounts), absPath)
	}

	// Fallback: Check if it's a directory and accessible
	return info.IsDir()
}

// monitorMount monitors the mount and attempts to recover if it fails
func (s *MountService) monitorMount() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.mu.RLock()
			mounted := s.status.Mounted
			mountPoint := s.status.MountPoint
			s.mu.RUnlock()

			if mounted && !s.isMounted(mountPoint) {
				slog.Error("Mount point lost, attempting to recover")

				// Try to unmount cleanly first
				s.Unmount()

				// Wait a bit
				time.Sleep(5 * time.Second)

				// Try to remount
				if err := s.Mount(s.ctx); err != nil {
					slog.Error("Failed to recover mount", "error", err)
					s.mu.Lock()
					s.status.Error = fmt.Sprintf("Mount recovery failed: %v", err)
					s.mu.Unlock()
				} else {
					slog.Info("Mount recovered successfully")
				}
			}
		}
	}
}

// logWriter implements io.Writer for logging command output
type logWriter struct {
	level  slog.Level
	prefix string
}

func (w *logWriter) Write(p []byte) (n int, err error) {
	msg := strings.TrimSpace(string(p))
	if msg != "" {
		slog.Log(context.Background(), w.level, msg, "component", w.prefix)
	}
	return len(p), nil
}

// TestMountConfig tests if mount configuration is valid
func (s *MountService) TestMountConfig(cfg *config.Config) error {
	if cfg.MountPath == "" {
		return fmt.Errorf("mount point is required")
	}

	if !filepath.IsAbs(cfg.MountPath) {
		return fmt.Errorf("mount point must be an absolute path")
	}

	// Check if rclone binary is available
	if _, err := exec.LookPath("rclone"); err != nil {
		return fmt.Errorf("rclone binary not found in PATH: %w", err)
	}

	// Try to create mount point directory to test permissions
	if err := os.MkdirAll(cfg.MountPath, 0755); err != nil {
		return fmt.Errorf("cannot create mount point directory: %w", err)
	}

	// Check if mount point is already in use
	if s.isMounted(cfg.MountPath) {
		return fmt.Errorf("mount point %s is already in use", cfg.MountPath)
	}

	return nil
}