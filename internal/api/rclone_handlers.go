package api

import (
	"net/http"

	"github.com/gofiber/fiber/v2"
	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/altmount/internal/rclone"
)

// RCloneHandlers handles RClone-related API endpoints
type RCloneHandlers struct {
	mountService *rclone.MountService
	configGetter config.ConfigGetter
}

// NewRCloneHandlers creates new RClone handlers
func NewRCloneHandlers(mountService *rclone.MountService, configGetter config.ConfigGetter) *RCloneHandlers {
	return &RCloneHandlers{
		mountService: mountService,
		configGetter: configGetter,
	}
}

// GetMountStatus returns the current mount status
func (h *RCloneHandlers) GetMountStatus(c *fiber.Ctx) error {
	status := h.mountService.GetStatus()
	return c.JSON(fiber.Map{
		"success": true,
		"data":    status,
	})
}

// StartMount starts the rclone mount
func (h *RCloneHandlers) StartMount(c *fiber.Ctx) error {
	if err := h.mountService.Mount(c.Context()); err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"success": false,
			"message": err.Error(),
		})
	}

	return c.JSON(fiber.Map{
		"success": true,
		"message": "Mount started successfully",
		"data":    h.mountService.GetStatus(),
	})
}

// StopMount stops the rclone mount
func (h *RCloneHandlers) StopMount(c *fiber.Ctx) error {
	if err := h.mountService.Unmount(); err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"success": false,
			"message": err.Error(),
		})
	}

	return c.JSON(fiber.Map{
		"success": true,
		"message": "Mount stopped successfully",
	})
}

// TestMountConfig tests the mount configuration
func (h *RCloneHandlers) TestMountConfig(c *fiber.Ctx) error {
	// Parse test configuration from request body
	var testConfig struct {
		MountPoint   string            `json:"mount_point"`
		MountOptions map[string]string `json:"mount_options"`
	}

	if err := c.BodyParser(&testConfig); err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"message": "Invalid request body",
		})
	}

	// Create a test config based on current config
	cfg := h.configGetter()
	testCfg := cfg.DeepCopy()

	// Override with test values if provided
	if testConfig.MountPoint != "" {
		testCfg.MountPath = testConfig.MountPoint
	}
	if testConfig.MountOptions != nil {
		testCfg.RClone.MountOptions = testConfig.MountOptions
	}

	// Test the configuration
	if err := h.mountService.TestMountConfig(testCfg); err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"message": err.Error(),
		})
	}

	return c.JSON(fiber.Map{
		"success": true,
		"message": "Mount configuration is valid",
	})
}

// RegisterRCloneRoutes registers RClone-related routes
func RegisterRCloneRoutes(apiGroup fiber.Router, handlers *RCloneHandlers) {
	rcloneGroup := apiGroup.Group("/rclone")

	// Mount management
	mountGroup := rcloneGroup.Group("/mount")
	mountGroup.Get("/status", handlers.GetMountStatus)
	mountGroup.Post("/start", handlers.StartMount)
	mountGroup.Post("/stop", handlers.StopMount)
	mountGroup.Delete("/", handlers.StopMount) // Alias for stop
	mountGroup.Post("/test", handlers.TestMountConfig)
}
