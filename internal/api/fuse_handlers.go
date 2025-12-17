package api

import (
	"log/slog"
	"os"
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

	s.fuseManager.mu.Lock()
	if s.fuseManager.status == "running" {
		s.fuseManager.mu.Unlock()
		return c.Status(409).JSON(fiber.Map{
			"success": false,
			"message": "FUSE is already mounted",
		})
	}
	s.fuseManager.status = "starting"
	s.fuseManager.path = req.Path
	s.fuseManager.mu.Unlock()

	// Ensure directory exists
	if err := os.MkdirAll(req.Path, 0755); err != nil {
		s.fuseManager.mu.Lock()
		s.fuseManager.status = "error"
		s.fuseManager.mu.Unlock()
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Failed to create mount directory",
			"details": err.Error(),
		})
	}

	// Start FUSE server in background
	go func() {
		logger := slog.With("component", "fuse")
		adapter := fuse.NewContextAdapter(s.nzbFilesystem)
		server := fuse.NewServer(req.Path, adapter, logger)
		
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
