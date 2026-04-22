package api

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/altmount/internal/importer"
	"github.com/javi11/altmount/internal/importer/migration"
)

// handleImportNzbdav handles POST /import/nzbdav
//
//	@Summary		Import from NZBDav source
//	@Description	Starts an import from a WebDAV/NZBDav source, fetching NZBs from the remote.
//	@Tags			Import
//	@Accept			json
//	@Produce		json
//	@Param			body	body		object{}	false	"Import configuration (uses server config if omitted)"
//	@Success		200		{object}	APIResponse
//	@Failure		500		{object}	APIResponse
//	@Security		BearerAuth
//	@Security		ApiKeyAuth
//	@Router			/import/nzbdav [post]
func (s *Server) handleImportNzbdav(c *fiber.Ctx) error {
	// Check if importer service is available
	if s.importerService == nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Importer service not available",
		})
	}

	// 1. Get Form Data
	blobsPath := c.FormValue("blobsPath")

	// 2. Handle File Source (Path or Upload)
	dbPath := c.FormValue("dbPath")
	var isTempFile bool

	if dbPath != "" {
		// Use server-side file path
		if _, err := os.Stat(dbPath); err != nil {
			return c.Status(422).JSON(fiber.Map{
				"success": false,
				"message": "Database file not found on server",
				"details": fmt.Sprintf("Path: %s, Error: %v", dbPath, err),
			})
		}
	} else {
		// Fallback to file upload
		file, err := c.FormFile("file")
		if err != nil {
			return c.Status(400).JSON(fiber.Map{
				"success": false,
				"message": "Database file is required (provide 'dbPath' or upload 'file')",
				"details": err.Error(),
			})
		}

		// Save file to temp location
		tempDir := os.TempDir()
		dbPath = filepath.Join(tempDir, fmt.Sprintf("nzbdav_%d.sqlite", time.Now().UnixNano()))
		if err := c.SaveFile(file, dbPath); err != nil {
			return c.Status(500).JSON(fiber.Map{
				"success": false,
				"message": "Failed to save uploaded file",
				"details": err.Error(),
			})
		}
		isTempFile = true
	}

	// Default blobsPath if not provided
	if blobsPath == "" {
		blobsPath = filepath.Join(filepath.Dir(dbPath), "blobs")
	}

	// 3. Start Async Import
	if err := s.importerService.StartNzbdavImport(dbPath, blobsPath, isTempFile); err != nil {
		if isTempFile {
			os.Remove(dbPath) // Clean up if start failed
		}
		return c.Status(409).JSON(fiber.Map{
			"success": false,
			"message": "Failed to start import",
			"details": err.Error(),
		})
	}

	return c.Status(202).JSON(fiber.Map{
		"success": true,
		"message": "Import started in background",
	})
}

// handleGetNzbdavImportStatus handles GET /import/nzbdav/status
//
//	@Summary		Get NZBDav import status
//	@Description	Returns the current status of the NZBDav import operation.
//	@Tags			Import
//	@Produce		json
//	@Success		200	{object}	APIResponse
//	@Security		BearerAuth
//	@Security		ApiKeyAuth
//	@Router			/import/nzbdav/status [get]
func (s *Server) handleGetNzbdavImportStatus(c *fiber.Ctx) error {
	if s.importerService == nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Importer service not available",
		})
	}

	status := s.importerService.GetImportStatus()
	resp := toImportStatusResponse(status)

	// Attach migration stats when available; omit on error to preserve existing behaviour.
	if s.migrationRepo != nil {
		if stats, err := s.migrationRepo.Stats(c.Context(), "nzbdav"); err == nil {
			resp["migration_stats"] = map[string]any{
				"pending":           stats.Pending,
				"imported":          stats.Imported,
				"failed":            stats.Failed,
				"symlinks_migrated": stats.SymlinksMigrated,
				"total":             stats.Total,
			}
		}
	}

	return c.Status(200).JSON(fiber.Map{
		"success": true,
		"data":    resp,
	})
}

// handleMigrateNzbdavSymlinks handles POST /import/nzbdav/migrate-symlinks
//
//	@Summary		Migrate NZBDav library symlinks
//	@Description	Walks a library directory and rewrites symlinks that target the nzbdav mount to point at the altmount path instead.
//	@Tags			Import
//	@Accept			json
//	@Produce		json
//	@Param			body	body		object{}	true	"library_path, source_mount_path, dry_run"
//	@Success		200		{object}	APIResponse
//	@Failure		400		{object}	APIResponse
//	@Failure		500		{object}	APIResponse
//	@Security		BearerAuth
//	@Security		ApiKeyAuth
//	@Router			/import/nzbdav/migrate-symlinks [post]
func (s *Server) handleMigrateNzbdavSymlinks(c *fiber.Ctx) error {
	var req struct {
		LibraryPath     string `json:"library_path"`
		SourceMountPath string `json:"source_mount_path"`
		DryRun          bool   `json:"dry_run"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{
			"success": false,
			"message": "Invalid request body",
			"details": err.Error(),
		})
	}

	if req.LibraryPath == "" || !filepath.IsAbs(req.LibraryPath) {
		return c.Status(400).JSON(fiber.Map{
			"success": false,
			"message": "library_path must be a non-empty absolute path",
		})
	}
	if req.SourceMountPath == "" || !filepath.IsAbs(req.SourceMountPath) {
		return c.Status(400).JSON(fiber.Map{
			"success": false,
			"message": "source_mount_path must be a non-empty absolute path",
		})
	}

	cfg := s.configManager.GetConfig()
	if cfg == nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Configuration not available",
		})
	}

	// Determine which migration repo to use — prefer the dedicated field, fall back
	// to nil-safe check so existing deployments without a migration repo still fail
	// gracefully rather than panic.
	migRepo := s.migrationRepo
	if migRepo == nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Migration repository not available",
		})
	}

	ctx := c.Context()

	// Backfill idempotently before walking.
	if _, err := migRepo.BackfillFromImportQueue(ctx); err != nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Failed to backfill migration data",
			"details": err.Error(),
		})
	}

	lookup := database.NewDBSymlinkLookup(migRepo)

	report, err := migration.RewriteLibrarySymlinks(
		ctx,
		req.LibraryPath,
		req.SourceMountPath,
		cfg.MountPath,
		"nzbdav",
		lookup,
		req.DryRun,
	)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Symlink migration failed",
			"details": err.Error(),
		})
	}

	return c.Status(200).JSON(fiber.Map{
		"success": true,
		"data": fiber.Map{
			"scanned":              report.Scanned,
			"matched":              report.Matched,
			"rewritten":            report.Rewritten,
			"skipped_wrong_prefix": report.SkippedWrongPrefix,
			"unmatched":            report.Unmatched,
			"errors":               report.Errors,
			"dry_run":              req.DryRun,
		},
	})
}

// handleCancelNzbdavImport handles DELETE /import/nzbdav
//
//	@Summary		Cancel NZBDav import
//	@Description	Cancels the currently running NZBDav import operation.
//	@Tags			Import
//	@Produce		json
//	@Success		200	{object}	APIResponse
//	@Security		BearerAuth
//	@Security		ApiKeyAuth
//	@Router			/import/nzbdav [delete]
func (s *Server) handleCancelNzbdavImport(c *fiber.Ctx) error {
	if s.importerService == nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Importer service not available",
		})
	}

	if err := s.importerService.CancelImport(); err != nil {
		return c.Status(400).JSON(fiber.Map{
			"success": false,
			"message": "Failed to cancel import",
			"details": err.Error(),
		})
	}

	return c.Status(200).JSON(fiber.Map{
		"success": true,
		"message": "Import cancellation requested",
	})
}

// handleResetNzbdavImportStatus handles POST /import/nzbdav/reset
//
//	@Summary		Reset NZBDav import status
//	@Description	Resets the NZBDav import state so a new import can be started.
//	@Tags			Import
//	@Produce		json
//	@Success		200	{object}	APIResponse
//	@Security		BearerAuth
//	@Security		ApiKeyAuth
//	@Router			/import/nzbdav/reset [post]
func (s *Server) handleResetNzbdavImportStatus(c *fiber.Ctx) error {
	if s.importerService == nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Importer service not available",
		})
	}

	s.importerService.ResetNzbdavImportStatus()

	return c.Status(200).JSON(fiber.Map{
		"success": true,
		"message": "Import status reset",
	})
}

// handleClearPendingNzbdavMigrations handles DELETE /import/nzbdav/pending-migrations
//
//	@Summary		Clear pending NZBDav migration rows
//	@Description	Deletes all import_migrations rows with status='pending' for source='nzbdav'. Keeps imported/symlinks_migrated rows untouched. Use to remove orphaned rows from a cancelled/failed import before re-importing.
//	@Tags			Import
//	@Produce		json
//	@Success		200	{object}	APIResponse
//	@Security		BearerAuth
//	@Security		ApiKeyAuth
//	@Router			/import/nzbdav/pending-migrations [delete]
func (s *Server) handleClearPendingNzbdavMigrations(c *fiber.Ctx) error {
	if s.migrationRepo == nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Migration repository not available",
		})
	}

	deleted, err := s.migrationRepo.DeletePendingBySource(c.Context(), "nzbdav")
	if err != nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Failed to clear pending migrations",
			"details": err.Error(),
		})
	}

	return c.Status(200).JSON(fiber.Map{
		"success": true,
		"message": fmt.Sprintf("Cleared %d pending migration rows", deleted),
		"data": fiber.Map{
			"deleted": deleted,
		},
	})
}

// handleClearAllNzbdavMigrations handles DELETE /import/nzbdav/migrations
//
//	@Summary		Clear ALL NZBDav migration rows
//	@Description	Deletes every import_migrations row for source='nzbdav' regardless of status. Use to force a full re-import after the imported files have been deleted from AltMount. This will cause the scanner to re-process every blob on the next import.
//	@Tags			Import
//	@Produce		json
//	@Success		200	{object}	APIResponse
//	@Security		BearerAuth
//	@Security		ApiKeyAuth
//	@Router			/import/nzbdav/migrations [delete]
func (s *Server) handleClearAllNzbdavMigrations(c *fiber.Ctx) error {
	if s.migrationRepo == nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Migration repository not available",
		})
	}

	deleted, err := s.migrationRepo.DeleteAllBySource(c.Context(), "nzbdav")
	if err != nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Failed to clear migrations",
			"details": err.Error(),
		})
	}

	return c.Status(200).JSON(fiber.Map{
		"success": true,
		"message": fmt.Sprintf("Cleared %d migration rows", deleted),
		"data": fiber.Map{
			"deleted": deleted,
		},
	})
}

func toImportStatusResponse(info importer.ImportInfo) map[string]any {
	return map[string]any{
		"status":     string(info.Status),
		"total":      info.Total,
		"added":      info.Added,
		"failed":     info.Failed,
		"skipped":    info.Skipped,
		"last_error": info.LastError,
	}
}
