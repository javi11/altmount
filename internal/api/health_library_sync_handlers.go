package api

import (
	"errors"
	"log/slog"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/kipsilabs/altmount/internal/health"
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
	return RespondSuccess(c, status)
}

// handleStartLibrarySync handles POST /api/health/library-sync/start
func (h *LibrarySyncHandlers) handleStartLibrarySync(c *fiber.Ctx) error {
	err := h.librarySyncWorker.TriggerManualSync(c.Context())
	if err != nil {
		slog.ErrorContext(c.Context(), "Failed to trigger library sync", "error", err)
		return RespondConflict(c, "Library sync already running", err.Error())
	}

	return RespondMessage(c, "Library sync triggered successfully")
}

// handleCancelLibrarySync handles POST /api/health/library-sync/cancel
func (h *LibrarySyncHandlers) handleCancelLibrarySync(c *fiber.Ctx) error {
	// Stop the library sync worker
	h.librarySyncWorker.Stop(c.Context())

	return RespondMessage(c, "Library sync cancelled successfully")
}

// handleDryRunLibrarySync handles POST /api/health/library-sync/dry-run
func (h *LibrarySyncHandlers) handleDryRunLibrarySync(c *fiber.Ctx) error {
	// Perform dry run using the refactored SyncLibrary method with dryRun=true
	result := h.librarySyncWorker.SyncLibrary(c.Context(), true)
	if result == nil {
		// This should not happen unless there was an error during dry run
		slog.ErrorContext(c.Context(), "Dry run returned nil result")
		return RespondInternalError(c, "Failed to perform dry run", "")
	}

	// Convert internal DryRunResult to API DryRunSyncResult
	apiResult := DryRunSyncResult{
		OrphanedMetadataCount:  result.OrphanedMetadataCount,
		OrphanedLibraryFiles:   result.OrphanedLibraryFiles,
		DatabaseRecordsToClean: result.DatabaseRecordsToClean,
		WouldCleanup:           result.WouldCleanup,
	}

	return RespondSuccess(c, apiResult)
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

	return RespondSuccess(c, fiber.Map{
		"needs_sync": needsSync,
		"reason":     reason,
	})
}

// LibraryAuditRequest is the body of the library audit scan and cleanup
// endpoints. GraceHours keeps files imported more recently than this out of
// the orphan list (default 24); 0 disables the grace period.
type LibraryAuditRequest struct {
	GraceHours   *float64 `json:"grace_hours"`
	Orphans      []string `json:"orphans"`
	DeadSymlinks []string `json:"dead_symlinks"`
}

func (r LibraryAuditRequest) grace() time.Duration {
	if r.GraceHours == nil {
		return health.DefaultAuditGracePeriod
	}
	if *r.GraceHours <= 0 {
		return 0
	}
	return time.Duration(*r.GraceHours * float64(time.Hour))
}

// handleLibraryAuditScan handles POST /api/health/library-audit/scan: a dry
// run listing AltMount files no library symlink points at, and library
// symlinks whose AltMount file is gone.
func (h *LibrarySyncHandlers) handleLibraryAuditScan(c *fiber.Ctx) error {
	var req LibraryAuditRequest
	if len(c.Body()) > 0 {
		if err := c.BodyParser(&req); err != nil {
			return RespondBadRequest(c, "Invalid request body", err.Error())
		}
	}
	audit, err := h.librarySyncWorker.AuditLibrary(c.Context(), req.grace())
	if err != nil {
		return RespondValidationError(c, "Library audit failed", err.Error())
	}
	return RespondSuccess(c, audit)
}

// handleLibraryAuditCleanup handles POST /api/health/library-audit/cleanup:
// deletes the listed orphans and dead symlinks that a fresh scan still finds.
func (h *LibrarySyncHandlers) handleLibraryAuditCleanup(c *fiber.Ctx) error {
	var req LibraryAuditRequest
	if err := c.BodyParser(&req); err != nil {
		return RespondBadRequest(c, "Invalid request body", err.Error())
	}
	if len(req.Orphans) == 0 && len(req.DeadSymlinks) == 0 {
		return RespondValidationError(c, "Nothing selected to clean up", "")
	}
	result, err := h.librarySyncWorker.CleanupLibraryAudit(c.Context(), req.Orphans, req.DeadSymlinks, req.grace())
	switch {
	case errors.Is(err, health.ErrLibrarySyncInProgress):
		return RespondConflict(c, "Library sync is running", err.Error())
	case err != nil:
		return RespondValidationError(c, "Library audit cleanup failed", err.Error())
	}
	slog.InfoContext(c.Context(), "Library audit cleanup finished",
		"orphans_deleted", result.OrphansDeleted,
		"dead_symlinks_removed", result.DeadSymlinksRemoved,
		"skipped", len(result.Skipped))
	return RespondSuccess(c, result)
}
