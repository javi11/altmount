package api

import (
	"log/slog"
	"os"
	"sync"

	"github.com/gofiber/fiber/v2"
	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/altmount/internal/fuse"
)

// FuseManager handles the lifecycle of the FUSE server
type FuseManager struct {
	server *fuse.Server
	mu     sync.Mutex
	status string // "stopped", "starting", "running", "error"
	path   string
}

// NewFuseManager creates a new FUSE manager
func NewFuseManager() *FuseManager {
	return &FuseManager{
		status: "stopped",
	}
}

// Stop stops the FUSE mount if running
func (m *FuseManager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.server != nil {
		slog.Info("Unmounting FUSE on manager stop")
		if err := m.server.Unmount(); err != nil {
			slog.Error("Failed to unmount FUSE on manager stop", "error", err)
		}
		m.server = nil
		m.status = "stopped"
	}
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
		s.fuseManager.status = "running"
		s.fuseManager.mu.Unlock()

		if err := server.Mount(); err != nil {
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
		s.fuseManager.status = "running"
		s.fuseManager.mu.Unlock()

		if err := server.Mount(); err != nil {
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
