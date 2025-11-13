package api

import (
	"log/slog"

	"github.com/gofiber/fiber/v2"
	"github.com/javi11/altmount/internal/health"
)

// LibrarySyncHandlers holds the library sync-related request handlers
type LibrarySyncHandlers struct {
	librarySyncWorker *health.LibrarySyncWorker
}

// NewLibrarySyncHandlers creates a new instance of library sync handlers
func NewLibrarySyncHandlers(librarySyncWorker *health.LibrarySyncWorker) *LibrarySyncHandlers {
	return &LibrarySyncHandlers{
		librarySyncWorker: librarySyncWorker,
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
