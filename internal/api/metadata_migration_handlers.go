package api

import (
	"errors"
	"log/slog"

	"github.com/gofiber/fiber/v2"
	"github.com/javi11/altmount/internal/metadata"
)

// MetadataMigrationHandlers holds the legacy-metadata migration handlers.
type MetadataMigrationHandlers struct {
	worker *metadata.MigrationWorker
}

// NewMetadataMigrationHandlers creates a new instance of the migration handlers.
func NewMetadataMigrationHandlers(worker *metadata.MigrationWorker) *MetadataMigrationHandlers {
	return &MetadataMigrationHandlers{worker: worker}
}

// handleGetStatus handles GET /api/metadata/migration/status
func (h *MetadataMigrationHandlers) handleGetStatus(c *fiber.Ctx) error {
	return RespondSuccess(c, h.worker.GetStatus())
}

// handleDryRun handles POST /api/metadata/migration/dry-run
func (h *MetadataMigrationHandlers) handleDryRun(c *fiber.Ctx) error {
	result, err := h.worker.DryRun(c.Context())
	if err != nil {
		if errors.Is(err, metadata.ErrMigrationRunning) {
			return RespondConflict(c, "Metadata migration already running", err.Error())
		}
		slog.ErrorContext(c.Context(), "Metadata migration dry run failed", "error", err)
		return RespondInternalError(c, "Failed to perform migration dry run", err.Error())
	}
	return RespondSuccess(c, result)
}

// handleStart handles POST /api/metadata/migration/start
func (h *MetadataMigrationHandlers) handleStart(c *fiber.Ctx) error {
	if err := h.worker.Start(c.Context()); err != nil {
		if errors.Is(err, metadata.ErrMigrationRunning) {
			return RespondConflict(c, "Metadata migration already running", err.Error())
		}
		slog.ErrorContext(c.Context(), "Failed to start metadata migration", "error", err)
		return RespondInternalError(c, "Failed to start metadata migration", err.Error())
	}
	return RespondMessage(c, "Metadata migration started")
}

// handleCancel handles POST /api/metadata/migration/cancel
func (h *MetadataMigrationHandlers) handleCancel(c *fiber.Ctx) error {
	h.worker.Cancel()
	return RespondMessage(c, "Metadata migration cancelled")
}
