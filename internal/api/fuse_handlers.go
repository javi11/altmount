package api

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"runtime"
	"sync"

	"github.com/gofiber/fiber/v2"
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

// startFuseServer internal method to start the FUSE server
func (s *Server) startFuseServer(path string) error {
	s.fuseManager.mu.Lock()
	if s.fuseManager.status == "running" {
		s.fuseManager.mu.Unlock()
		return fmt.Errorf("FUSE is already mounted")
	}
	s.fuseManager.status = "starting"
	s.fuseManager.path = path
	s.fuseManager.mu.Unlock()

	// Cleanup stale mount if any before MkdirAll
	if runtime.GOOS == "linux" {
		_ = exec.Command("fusermount", "-uz", path).Run()
		_ = exec.Command("umount", "-l", path).Run()
	}

	// Ensure directory exists
	if err := os.MkdirAll(path, 0755); err != nil {
		slog.Error("Failed to create FUSE mount directory", "path", path, "error", err)
		s.fuseManager.mu.Lock()
		s.fuseManager.status = "error"
		s.fuseManager.mu.Unlock()
		return fmt.Errorf("failed to create mount directory: %w", err)
	}

	slog.Info("Starting FUSE mount", "path", path)

	// Start FUSE server in background
	go func() {
		logger := slog.With("component", "fuse", "path", path)
		
		// Get configuration
		cfg := s.configManager.GetConfig().Fuse
		cfg.MountPath = path // Ensure path matches request

		adapter := fuse.NewContextAdapter(s.nzbFilesystem, cfg)
		server := fuse.NewServer(path, adapter, logger, cfg)

		s.fuseManager.mu.Lock()
		s.fuseManager.server = server
		s.fuseManager.status = "running"
		s.fuseManager.mu.Unlock()

		if err := server.Mount(); err != nil {
			slog.Error("FUSE mount failed", "path", path, "error", err)
			s.fuseManager.mu.Lock()
			s.fuseManager.status = "error"
			s.fuseManager.server = nil
			s.fuseManager.mu.Unlock()
		} else {
			slog.Info("FUSE unmounted successfully", "path", path)
			// Mount returned (unmounted)
			s.fuseManager.mu.Lock()
			s.fuseManager.status = "stopped"
			s.fuseManager.server = nil
			s.fuseManager.mu.Unlock()
		}
	}()

	return nil
}

// stopFuseServer internal method to stop the FUSE server
func (s *Server) stopFuseServer() error {
	s.fuseManager.mu.Lock()
	defer s.fuseManager.mu.Unlock()

	if s.fuseManager.server == nil {
		return fmt.Errorf("FUSE is not running")
	}

	return s.fuseManager.server.Unmount()
}

// AutoStartFuse checks configuration and starts FUSE if enabled
func (s *Server) AutoStartFuse() {
	cfg := s.configManager.GetConfig()
	if cfg.Fuse.Enabled != nil && *cfg.Fuse.Enabled && cfg.Fuse.MountPath != "" {
		slog.Info("Auto-starting FUSE mount from configuration", "path", cfg.Fuse.MountPath)
		if err := s.startFuseServer(cfg.Fuse.MountPath); err != nil {
			slog.Error("Failed to auto-start FUSE mount", "error", err)
		}
	}
}

// handleStartFuseMount starts the FUSE mount
func (s *Server) handleStartFuseMount(c *fiber.Ctx) error {
	var req struct {
		Path string `json:"path"`
	}

	if err := c.BodyParser(&req); err != nil {
		return c.Status(422).JSON(fiber.Map{
			"success": false,
			"message": "Invalid JSON",
		})
	}

	if req.Path == "" {
		return c.Status(422).JSON(fiber.Map{
			"success": false,
			"message": "Mount path is required",
		})
	}

	if err := s.startFuseServer(req.Path); err != nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": err.Error(),
		})
	}

	// Update configuration
	enabled := true
	cfg := s.configManager.GetConfig().DeepCopy()
	cfg.Fuse.Enabled = &enabled
	cfg.Fuse.MountPath = req.Path

	if err := s.configManager.UpdateConfig(cfg); err != nil {
		slog.Warn("Failed to update config for FUSE start", "error", err)
	} else {
		if err := s.configManager.SaveConfig(); err != nil {
			slog.Warn("Failed to save config for FUSE start", "error", err)
		}
	}

	return c.JSON(fiber.Map{
		"success": true,
		"message": "FUSE mount starting",
	})
}

// handleStopFuseMount stops the FUSE mount
func (s *Server) handleStopFuseMount(c *fiber.Ctx) error {
	if err := s.stopFuseServer(); err != nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Failed to unmount",
			"details": err.Error(),
		})
	}

	// Update configuration
	enabled := false
	cfg := s.configManager.GetConfig().DeepCopy()
	cfg.Fuse.Enabled = &enabled

	if err := s.configManager.UpdateConfig(cfg); err != nil {
		slog.Warn("Failed to update config for FUSE stop", "error", err)
	} else {
		if err := s.configManager.SaveConfig(); err != nil {
			slog.Warn("Failed to save config for FUSE stop", "error", err)
		}
	}

	return c.JSON(fiber.Map{
		"success": true,
		"message": "FUSE unmount requested",
	})
}

// handleGetFuseStatus returns the current status
func (s *Server) handleGetFuseStatus(c *fiber.Ctx) error {
	s.fuseManager.mu.Lock()
	defer s.fuseManager.mu.Unlock()

	return c.JSON(fiber.Map{
		"success": true,
		"data": fiber.Map{
			"status": s.fuseManager.status,
			"path":   s.fuseManager.path,
		},
	})
}
