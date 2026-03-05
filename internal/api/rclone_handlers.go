package api

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/altmount/internal/rclone"
	"github.com/javi11/altmount/pkg/rclonecli"
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
//
// @Summary      Get RClone mount status
// @Description  Returns the current status of the RClone mount
// @Tags         RClone
// @Produce      json
// @Success      200  {object}  APIResponse
// @Security     BearerAuth
// @Router       /api/rclone/mount/status [get]
func (h *RCloneHandlers) GetMountStatus(c *fiber.Ctx) error {
	status := h.mountService.GetStatus()
	return c.JSON(fiber.Map{
		"success": true,
		"data":    status,
	})
}

// StartMount starts the rclone mount
//
// @Summary      Start RClone mount
// @Description  Starts the RClone VFS mount
// @Tags         RClone
// @Produce      json
// @Success      200  {object}  APIResponse
// @Failure      500  {object}  APIResponse
// @Security     BearerAuth
// @Router       /api/rclone/mount/start [post]
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
//
// @Summary      Stop RClone mount
// @Description  Stops the RClone VFS mount
// @Tags         RClone
// @Produce      json
// @Success      200  {object}  APIResponse
// @Failure      500  {object}  APIResponse
// @Security     BearerAuth
// @Router       /api/rclone/mount/stop [post]
func (h *RCloneHandlers) StopMount(c *fiber.Ctx) error {
	if err := h.mountService.Unmount(c.Context()); err != nil {
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
//
// @Summary      Test RClone mount configuration
// @Description  Validates the RClone mount configuration without applying it
// @Tags         RClone
// @Accept       json
// @Produce      json
// @Param        body  body  object{mount_point=string,mount_options=object}  false  "Mount configuration to test"
// @Success      200  {object}  APIResponse
// @Failure      400  {object}  APIResponse
// @Security     BearerAuth
// @Router       /api/rclone/mount/test [post]
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

	return c.JSON(fiber.Map{
		"success": true,
		"message": "Mount configuration is valid",
	})
}

// TestRCloneConnection tests the RClone RC connection
//
// @Summary      Test RClone RC connection
// @Description  Tests connectivity to an external RClone RC server with the provided credentials
// @Tags         RClone
// @Accept       json
// @Produce      json
// @Param        body  body  object{rc_url=string,rc_user=string,rc_pass=string,vfs_name=string}  true  "RClone RC connection parameters"
// @Success      200  {object}  APIResponse
// @Failure      422  {object}  APIResponse
// @Security     BearerAuth
// @Router       /api/rclone/test [post]
func (h *RCloneHandlers) TestRCloneConnection(c *fiber.Ctx) error {
	// Decode test request
	var testReq struct {
		RCUrl   string `json:"rc_url"`
		RCUser  string `json:"rc_user"`
		RCPass  string `json:"rc_pass"`
		VFSName string `json:"vfs_name"`
	}

	if err := c.BodyParser(&testReq); err != nil {
		return c.Status(422).JSON(fiber.Map{
			"success": false,
			"message": "Invalid JSON in request body",
			"details": err.Error(),
		})
	}

	if testReq.RCUrl == "" {
		return c.Status(422).JSON(fiber.Map{
			"success": false,
			"message": "RC URL is required",
			"details": "MISSING_RC_URL",
		})
	}

	// Try to connect with timeout
	ctx, cancel := context.WithTimeout(c.Context(), 10*time.Second)
	defer cancel()

	// Test external RC server connection including VFS name verification
	err := rclonecli.TestConnection(ctx, testReq.RCUrl, testReq.RCUser, testReq.RCPass, testReq.VFSName, http.DefaultClient)
	if err != nil {
		return c.Status(200).JSON(fiber.Map{
			"success": true,
			"data": fiber.Map{
				"success":       false,
				"error_message": fmt.Sprintf("Failed to connect to external RC server: %v", err),
			},
		})
	}

	// Connection successful
	return c.Status(200).JSON(fiber.Map{
		"success": true,
		"data": fiber.Map{
			"success":       true,
			"error_message": "",
			"message":       fmt.Sprintf("Connected to external RC server at %s", testReq.RCUrl),
		},
	})
}

// ClearRCloneCache removes the rclone VFS cache directory and recreates it empty.
//
// @Summary      Clear RClone cache
// @Description  Removes the RClone VFS cache directory and recreates it empty
// @Tags         RClone
// @Produce      json
// @Success      200  {object}  APIResponse
// @Failure      400  {object}  APIResponse
// @Failure      500  {object}  APIResponse
// @Security     BearerAuth
// @Router       /api/rclone/cache [delete]
func (h *RCloneHandlers) ClearRCloneCache(c *fiber.Ctx) error {
	cfg := h.configGetter()
	cacheDir := cfg.RClone.CacheDir
	if cacheDir == "" {
		return RespondBadRequest(c, "Cache directory is not configured", "")
	}

	slog.InfoContext(c.Context(), "Clearing rclone cache directory", "cache_dir", cacheDir)

	if err := os.RemoveAll(cacheDir); err != nil {
		slog.ErrorContext(c.Context(), "Failed to remove rclone cache directory", "cache_dir", cacheDir, "error", err)
		return RespondInternalError(c, "Failed to clear rclone cache", err.Error())
	}

	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		slog.ErrorContext(c.Context(), "Failed to recreate rclone cache directory", "cache_dir", cacheDir, "error", err)
		return RespondInternalError(c, "Failed to recreate cache directory", err.Error())
	}

	slog.InfoContext(c.Context(), "Rclone cache directory cleared", "cache_dir", cacheDir)
	return RespondSuccess(c, fiber.Map{"cache_dir": cacheDir})
}

// RegisterRCloneRoutes registers RClone-related routes
func RegisterRCloneRoutes(apiGroup fiber.Router, handlers *RCloneHandlers) {
	rcloneGroup := apiGroup.Group("/rclone")

	// RC server testing
	rcloneGroup.Post("/test", handlers.TestRCloneConnection)

	// Cache management
	rcloneGroup.Delete("/cache", handlers.ClearRCloneCache)

	// Mount management
	mountGroup := rcloneGroup.Group("/mount")
	mountGroup.Get("/status", handlers.GetMountStatus)
	mountGroup.Post("/start", handlers.StartMount)
	mountGroup.Post("/stop", handlers.StopMount)
	mountGroup.Delete("/", handlers.StopMount) // Alias for stop
	mountGroup.Post("/test", handlers.TestMountConfig)
}
