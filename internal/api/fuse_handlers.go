package api

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/altmount/internal/fuse"
	"github.com/javi11/altmount/internal/nzbfilesystem"
)

const (
	fuseMonitorInterval  = 15 * time.Second
	fuseRecoveryDelay    = 2 * time.Second
	fuseMaxRecoveryAttempts = 3
)

// MountFactory creates a new FUSE server for (re)mounting.
type MountFactory func(path string) *fuse.Server

// FuseManager handles the lifecycle of the FUSE server
type FuseManager struct {
	server *fuse.Server
	mu     sync.Mutex
	status string // "stopped", "starting", "running", "error"
	path   string

	// Background health monitor
	monitorCancel    context.CancelFunc
	recoveryAttempts int

	// Dependencies for recovery (creating new fuse.Server instances)
	mountFactory MountFactory
}

// NewFuseManager creates a new FUSE manager.
// mountFactory is used to create fresh fuse.Server instances during auto-recovery.
func NewFuseManager(factory MountFactory) *FuseManager {
	return &FuseManager{
		status:       "stopped",
		mountFactory: factory,
	}
}

// Stop stops the FUSE mount and health monitor if running
func (m *FuseManager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Stop the health monitor first
	if m.monitorCancel != nil {
		m.monitorCancel()
		m.monitorCancel = nil
	}

	if m.server != nil {
		slog.Info("Unmounting FUSE on manager stop")
		if err := m.server.Unmount(); err != nil {
			slog.Error("Failed to unmount FUSE on manager stop", "error", err)
		}
		m.server = nil
		m.status = "stopped"
	}
}

// startMonitor starts a background goroutine that periodically validates mount health.
// Must be called with m.mu NOT held. Called from the onReady callback.
func (m *FuseManager) startMonitor() {
	m.mu.Lock()
	// Cancel any existing monitor
	if m.monitorCancel != nil {
		m.monitorCancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.monitorCancel = cancel
	m.mu.Unlock()

	go func() {
		ticker := time.NewTicker(fuseMonitorInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.mu.Lock()
				server := m.server
				status := m.status
				m.mu.Unlock()

				if status != "running" || server == nil {
					continue
				}

				healthy, err := server.ValidateMount()
				if !healthy {
					slog.Warn("FUSE health check failed", "error", err)

					m.mu.Lock()
					m.status = "error"
					m.mu.Unlock()

					m.recoverMount(ctx)
				}
			}
		}
	}()
}

// recoverMount attempts to recover a failed FUSE mount with retry logic.
func (m *FuseManager) recoverMount(ctx context.Context) {
	m.mu.Lock()
	m.recoveryAttempts++
	attempt := m.recoveryAttempts
	path := m.path
	server := m.server
	factory := m.mountFactory
	m.mu.Unlock()

	if attempt > fuseMaxRecoveryAttempts {
		slog.Error("FUSE recovery exhausted, staying in error state",
			"attempts", attempt-1,
			"path", path)
		return
	}

	if factory == nil {
		slog.Error("FUSE recovery not possible: no mount factory configured")
		return
	}

	slog.Info("Attempting FUSE recovery",
		"attempt", attempt,
		"max_attempts", fuseMaxRecoveryAttempts,
		"path", path)

	// Force unmount existing mount
	if server != nil {
		_ = server.ForceUnmount()
	}

	// Wait before retry
	select {
	case <-ctx.Done():
		return
	case <-time.After(fuseRecoveryDelay):
	}

	// Create a new server and mount
	newServer := factory(path)

	m.mu.Lock()
	m.server = newServer
	m.status = "starting"
	m.mu.Unlock()

	go func() {
		onReady := func() {
			m.mu.Lock()
			m.status = "running"
			m.recoveryAttempts = 0
			m.mu.Unlock()
			slog.Info("FUSE recovery successful", "path", path)
		}

		if err := newServer.Mount(onReady); err != nil {
			slog.Error("FUSE recovery mount failed", "error", err, "attempt", attempt)
			m.mu.Lock()
			m.status = "error"
			m.server = nil
			m.mu.Unlock()

			// Try again if we haven't exhausted retries
			m.recoverMount(ctx)
		} else {
			// Mount returned normally (unmounted)
			m.mu.Lock()
			m.status = "stopped"
			m.server = nil
			m.mu.Unlock()
		}
	}()
}

// handleStartFuseMount starts the FUSE mount
func (s *Server) handleStartFuseMount(c *fiber.Ctx) error {
	var req struct {
		Path string `json:"path"`
	}

	if err := c.BodyParser(&req); err != nil {
		return RespondValidationError(c, "Invalid JSON", err.Error())
	}

	if req.Path == "" {
		return RespondValidationError(c, "Mount path is required", "")
	}

	s.fuseManager.mu.Lock()
	if s.fuseManager.status == "running" {
		s.fuseManager.mu.Unlock()
		return RespondConflict(c, "FUSE is already mounted", "")
	}
	s.fuseManager.status = "starting"
	s.fuseManager.path = req.Path
	s.fuseManager.mu.Unlock()

	// Ensure directory exists
	if _, err := os.Stat(req.Path); os.IsNotExist(err) {
		if err := os.MkdirAll(req.Path, 0755); err != nil {
			s.fuseManager.mu.Lock()
			s.fuseManager.status = "error"
			s.fuseManager.mu.Unlock()
			return RespondInternalError(c, "Failed to create mount directory", err.Error())
		}
	}

	// Start FUSE server in background
	go func() {
		cfg := s.configManager.GetConfig()
		logger := slog.With("component", "fuse")
		server := fuse.NewServer(req.Path, s.nzbFilesystem, logger, cfg.Fuse, s.streamTracker)

		s.fuseManager.mu.Lock()
		s.fuseManager.server = server
		// Status stays "starting" until onReady fires
		s.fuseManager.mu.Unlock()

		onReady := func() {
			s.fuseManager.mu.Lock()
			s.fuseManager.status = "running"
			s.fuseManager.recoveryAttempts = 0
			s.fuseManager.mu.Unlock()

			// Start background health monitor
			s.fuseManager.startMonitor()
		}

		if err := server.Mount(onReady); err != nil {
			slog.Error("FUSE mount failed", "error", err)
			s.fuseManager.mu.Lock()
			s.fuseManager.status = "error"
			s.fuseManager.server = nil
			s.fuseManager.mu.Unlock()
		} else {
			// Mount returned (unmounted)
			s.fuseManager.mu.Lock()
			s.fuseManager.status = "stopped"
			s.fuseManager.server = nil
			s.fuseManager.mu.Unlock()
		}
	}()

	return RespondMessage(c, "FUSE mount starting")
}

// handleStopFuseMount stops the FUSE mount
func (s *Server) handleStopFuseMount(c *fiber.Ctx) error {
	s.fuseManager.mu.Lock()
	defer s.fuseManager.mu.Unlock()

	if s.fuseManager.server == nil {
		return RespondNotFound(c, "FUSE mount", "FUSE is not running")
	}

	// Stop health monitor before unmounting
	if s.fuseManager.monitorCancel != nil {
		s.fuseManager.monitorCancel()
		s.fuseManager.monitorCancel = nil
	}

	if err := s.fuseManager.server.Unmount(); err != nil {
		// Reset state so user isn't stuck in permanent "running"
		s.fuseManager.status = "error"
		s.fuseManager.server = nil
		return RespondInternalError(c, "Failed to unmount", err.Error())
	}

	return RespondMessage(c, "FUSE unmount requested")
}

// handleForceStopFuseMount force-unmounts the FUSE mount using platform-specific commands
func (s *Server) handleForceStopFuseMount(c *fiber.Ctx) error {
	s.fuseManager.mu.Lock()
	defer s.fuseManager.mu.Unlock()

	if s.fuseManager.status == "stopped" {
		return RespondNotFound(c, "FUSE mount", "FUSE is not running")
	}

	// Stop health monitor
	if s.fuseManager.monitorCancel != nil {
		s.fuseManager.monitorCancel()
		s.fuseManager.monitorCancel = nil
	}

	if s.fuseManager.server != nil {
		if err := s.fuseManager.server.ForceUnmount(); err != nil {
			slog.Error("Force unmount failed", "error", err)
			// Still reset state so user can retry
			s.fuseManager.server = nil
			s.fuseManager.status = "stopped"
			return RespondInternalError(c, "Force unmount failed", err.Error())
		}
	}

	s.fuseManager.server = nil
	s.fuseManager.status = "stopped"
	s.fuseManager.recoveryAttempts = 0

	return RespondMessage(c, "FUSE force unmount completed")
}

// handleGetFuseStatus returns the current status
func (s *Server) handleGetFuseStatus(c *fiber.Ctx) error {
	s.fuseManager.mu.Lock()
	defer s.fuseManager.mu.Unlock()

	data := fiber.Map{
		"status": s.fuseManager.status,
		"path":   s.fuseManager.path,
	}

	// Check mount health when status is "running"
	if s.fuseManager.status == "running" && s.fuseManager.server != nil {
		healthy, err := s.fuseManager.server.ValidateMount()
		data["healthy"] = healthy
		if err != nil {
			data["health_error"] = err.Error()
			// Auto-correct status if mount is unresponsive
			if !healthy {
				s.fuseManager.status = "error"
				data["status"] = "error"
			}
		}
	}

	return RespondSuccess(c, data)
}

// AutoStartFuse automatically starts the FUSE mount if enabled in config
func (s *Server) AutoStartFuse() {
	cfg := s.configManager.GetConfig()

	slog.Debug("Checking FUSE auto-start conditions",
		"mount_type", cfg.MountType,
		"mount_path", cfg.Fuse.MountPath)

	if cfg.MountType != config.MountTypeFuse {
		slog.Debug("FUSE auto-start skipped: mount_type is not fuse",
			"mount_type", cfg.MountType)
		return
	}

	if cfg.Fuse.MountPath == "" {
		slog.Warn("FUSE auto-start skipped: mount_path is empty despite mount_type=fuse")
		return
	}

	slog.Info("Auto-starting FUSE mount", "path", cfg.Fuse.MountPath)

	s.fuseManager.mu.Lock()
	if s.fuseManager.status == "running" {
		s.fuseManager.mu.Unlock()
		return
	}
	s.fuseManager.status = "starting"
	s.fuseManager.path = cfg.Fuse.MountPath
	s.fuseManager.mu.Unlock()

	if _, err := os.Stat(cfg.Fuse.MountPath); os.IsNotExist(err) {
		if err := os.MkdirAll(cfg.Fuse.MountPath, 0755); err != nil {
			slog.Error("Failed to create auto-mount directory", "path", cfg.Fuse.MountPath, "error", err)
			s.fuseManager.mu.Lock()
			s.fuseManager.status = "error"
			s.fuseManager.mu.Unlock()
			return
		}
	}

	go func() {
		logger := slog.With("component", "fuse")
		server := fuse.NewServer(cfg.Fuse.MountPath, s.nzbFilesystem, logger, cfg.Fuse, s.streamTracker)

		s.fuseManager.mu.Lock()
		s.fuseManager.server = server
		// Status stays "starting" until onReady fires
		s.fuseManager.mu.Unlock()

		onReady := func() {
			s.fuseManager.mu.Lock()
			s.fuseManager.status = "running"
			s.fuseManager.recoveryAttempts = 0
			s.fuseManager.mu.Unlock()

			// Start background health monitor
			s.fuseManager.startMonitor()
		}

		if err := server.Mount(onReady); err != nil {
			slog.Error("FUSE auto-mount failed", "error", err)
			s.fuseManager.mu.Lock()
			s.fuseManager.status = "error"
			s.fuseManager.server = nil
			s.fuseManager.mu.Unlock()
		} else {
			s.fuseManager.mu.Lock()
			s.fuseManager.status = "stopped"
			s.fuseManager.server = nil
			s.fuseManager.mu.Unlock()
		}
	}()
}

// newMountFactory creates a MountFactory that uses the given dependencies to build fuse.Server instances.
func newMountFactory(nzbfs *nzbfilesystem.NzbFilesystem, configManager ConfigManager, st *StreamTracker) MountFactory {
	return func(path string) *fuse.Server {
		cfg := configManager.GetConfig()
		logger := slog.With("component", "fuse")
		return fuse.NewServer(path, nzbfs, logger, cfg.Fuse, st)
	}
}
