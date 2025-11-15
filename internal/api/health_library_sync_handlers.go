package api

import (
	"log/slog"

	"github.com/gofiber/fiber/v2"
	"github.com/javi11/altmount/internal/health"
)

// LibrarySyncHandlers holds the library sync-related request handlers
type LibrarySyncHandlers struct {
	librarySyncWorker *health.LibrarySyncWorker
	configManager     ConfigManager
}

// NewLibrarySyncHandlers creates a new instance of library sync handlers
func NewLibrarySyncHandlers(librarySyncWorker *health.LibrarySyncWorker, configManager ConfigManager) *LibrarySyncHandlers {
	return &LibrarySyncHandlers{
		librarySyncWorker: librarySyncWorker,
		configManager:     configManager,
	}
}

// handleGetLibrarySyncStatus handles GET /api/health/library-sync/status
func (h *LibrarySyncHandlers) handleGetLibrarySyncStatus(c *fiber.Ctx) error {
	status := h.librarySyncWorker.GetStatus()
	return c.JSON(fiber.Map{
		"success": true,
		"data":    status,
	})
}

// handleStartLibrarySync handles POST /api/health/library-sync/start
func (h *LibrarySyncHandlers) handleStartLibrarySync(c *fiber.Ctx) error {
	err := h.librarySyncWorker.TriggerManualSync(c.Context())
	if err != nil {
		slog.ErrorContext(c.Context(), "Failed to trigger library sync", "error", err)
		return c.Status(fiber.StatusConflict).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	return c.JSON(fiber.Map{
		"success": true,
		"data": fiber.Map{
			"message": "Library sync triggered successfully",
		},
	})
}

// handleCancelLibrarySync handles POST /api/health/library-sync/cancel
func (h *LibrarySyncHandlers) handleCancelLibrarySync(c *fiber.Ctx) error {
	// Stop the library sync worker
	h.librarySyncWorker.Stop(c.Context())

	return c.JSON(fiber.Map{
		"success": true,
		"data": fiber.Map{
			"message": "Library sync cancelled successfully",
		},
	})
}

// handleDryRunLibrarySync handles POST /api/health/library-sync/dry-run
func (h *LibrarySyncHandlers) handleDryRunLibrarySync(c *fiber.Ctx) error {
	// Perform dry run using the refactored SyncLibrary method with dryRun=true
	result := h.librarySyncWorker.SyncLibrary(c.Context(), true)
	if result == nil {
		// This should not happen unless there was an error during dry run
		slog.ErrorContext(c.Context(), "Dry run returned nil result")
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"success": false,
			"error":   "Failed to perform dry run",
		})
	}

	// Convert internal DryRunResult to API DryRunSyncResult
	apiResult := DryRunSyncResult{
		OrphanedMetadataCount:  result.OrphanedMetadataCount,
		OrphanedLibraryFiles:   result.OrphanedLibraryFiles,
		OrphanedDirectories:    result.OrphanedDirectories,
		DatabaseRecordsToClean: result.DatabaseRecordsToClean,
		WouldCleanup:           result.WouldCleanup,
	}

	return c.JSON(fiber.Map{
		"success": true,
		"data":    apiResult,
	})
}

// handleGetSyncNeeded handles GET /api/health/library-sync/needed
// Returns whether a library sync is needed due to configuration changes
func (h *LibrarySyncHandlers) handleGetSyncNeeded(c *fiber.Ctx) error {
	needsSync := false
	reason := ""

	if h.configManager != nil && h.configManager.NeedsLibrarySync() {
		needsSync = true
		reason = "mount_path_changed"
	}

	return c.JSON(fiber.Map{
		"success": true,
		"data": fiber.Map{
			"needs_sync": needsSync,
			"reason":     reason,
		},
	})
}
