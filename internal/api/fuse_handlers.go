package api

import (
	"log/slog"
	"os"
	"sync"

	"github.com/gofiber/fiber/v2"
	"github.com/javi11/altmount/internal/fuse"
	fusecache "github.com/javi11/altmount/internal/fuse/cache"
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

// GetCache returns the FUSE metadata cache if available.
// Returns nil if FUSE is not running or cache is disabled.
func (m *FuseManager) GetCache() fusecache.Cache {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.server == nil {
		return nil
	}
	return m.server.GetCache()
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

	path := req.Path
	cfg := s.configManager.GetConfig()
	if path == "" {
		path = cfg.GetFuseMountPath()
	}

	if path == "" {
		return c.Status(422).JSON(fiber.Map{
			"success": false,
			"message": "Mount path is required",
		})
	}

	s.fuseManager.mu.Lock()
	if s.fuseManager.status == "running" {
		s.fuseManager.mu.Unlock()
		return c.Status(409).JSON(fiber.Map{
			"success": false,
			"message": "FUSE is already mounted",
		})
	}
	s.fuseManager.status = "starting"
	s.fuseManager.path = path
	s.fuseManager.mu.Unlock()

	// Ensure directory exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := os.MkdirAll(path, 0755); err != nil {
			s.fuseManager.mu.Lock()
			s.fuseManager.status = "error"
			s.fuseManager.mu.Unlock()
			return c.Status(500).JSON(fiber.Map{
				"success": false,
				"message": "Failed to create mount directory",
				"details": err.Error(),
			})
		}
	}

	// Start FUSE server in background
	go func() {
		ctx := c.Context()
		logger := slog.With("component", "fuse")
		adapter := fuse.NewContextAdapter(s.nzbFilesystem, cfg.Fuse)
		server := fuse.NewServer(path, adapter, logger, cfg.Fuse)

		s.fuseManager.mu.Lock()
		s.fuseManager.server = server
		s.fuseManager.status = "running"
		s.fuseManager.mu.Unlock()

		if err := server.Mount(); err != nil {
			slog.ErrorContext(ctx, "FUSE mount failed", "error", err)
			s.fuseManager.mu.Lock()
			s.fuseManager.status = "error"
			s.fuseManager.server = nil
			s.fuseManager.mu.Unlock()
			// Disconnect cache from import service on failure
			if s.importerService != nil {
				s.importerService.SetFuseCache(nil)
			}
		} else {
			// Wire up cache to import service after successful mount
			if s.importerService != nil {
				if cache := server.GetCache(); cache != nil {
					s.importerService.SetFuseCache(cache)
				}
			}
			// Mount returned (unmounted)
			s.fuseManager.mu.Lock()
			s.fuseManager.status = "stopped"
			s.fuseManager.server = nil
			s.fuseManager.mu.Unlock()
			// Disconnect cache from import service on unmount
			if s.importerService != nil {
				s.importerService.SetFuseCache(nil)
			}
		}
	}()

	return c.JSON(fiber.Map{
		"success": true,
		"message": "FUSE mount starting",
	})
}

// handleStopFuseMount stops the FUSE mount
func (s *Server) handleStopFuseMount(c *fiber.Ctx) error {
	s.fuseManager.mu.Lock()
	defer s.fuseManager.mu.Unlock()

	if s.fuseManager.server == nil {
		return c.Status(404).JSON(fiber.Map{
			"success": false,
			"message": "FUSE is not running",
		})
	}

	if err := s.fuseManager.server.Unmount(); err != nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Failed to unmount",
			"details": err.Error(),
		})
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

// AutoStartFuse automatically starts the FUSE mount if enabled in config
func (s *Server) AutoStartFuse() {
	cfg := s.configManager.GetConfig()

	// Log diagnostic info for troubleshooting
	slog.Debug("Checking FUSE auto-start conditions",
		"mount_path", cfg.Fuse.MountPath)

	if cfg.Fuse.Enabled == nil {
		slog.Info("FUSE auto-start skipped: enabled flag not configured")
		return
	}

	if !*cfg.Fuse.Enabled {
		slog.Debug("FUSE auto-start skipped: disabled in configuration")
		return
	}

	path := cfg.GetFuseMountPath()

	if path == "" {
		slog.Warn("FUSE auto-start skipped: no mount path available despite being enabled")
		return
	}

	slog.Info("Auto-starting FUSE mount", "path", path)

	// Create a fake fiber context for the internal call or just extract logic
	// For simplicity, let's just trigger it manually since we have all info

	s.fuseManager.mu.Lock()
	if s.fuseManager.status == "running" {
		s.fuseManager.mu.Unlock()
		return
	}
	s.fuseManager.status = "starting"
	s.fuseManager.path = path
	s.fuseManager.mu.Unlock()

	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := os.MkdirAll(path, 0755); err != nil {
			slog.Error("Failed to create auto-mount directory", "path", path, "error", err)
			s.fuseManager.mu.Lock()
			s.fuseManager.status = "error"
			s.fuseManager.mu.Unlock()
			return
		}
	}

	go func() {
		logger := slog.With("component", "fuse")
		adapter := fuse.NewContextAdapter(s.nzbFilesystem, cfg.Fuse)
		server := fuse.NewServer(path, adapter, logger, cfg.Fuse)

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
			// Disconnect cache from import service on failure
			if s.importerService != nil {
				s.importerService.SetFuseCache(nil)
			}
		} else {
			// Wire up cache to import service after successful mount
			if s.importerService != nil {
				if cache := server.GetCache(); cache != nil {
					s.importerService.SetFuseCache(cache)
				}
			}
			s.fuseManager.mu.Lock()
			s.fuseManager.status = "stopped"
			s.fuseManager.server = nil
			s.fuseManager.mu.Unlock()
			// Disconnect cache from import service on unmount
			if s.importerService != nil {
				s.importerService.SetFuseCache(nil)
			}
		}
	}()
}
