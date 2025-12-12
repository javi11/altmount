package api

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/javi11/altmount/internal/importer"
)

// handleImportNzbdav handles POST /import/nzbdav
func (s *Server) handleImportNzbdav(c *fiber.Ctx) error {
	// Check if importer service is available
	if s.importerService == nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Importer service not available",
		})
	}

	// 1. Get Form Data
	rootFolder := c.FormValue("rootFolder")
	if rootFolder == "" {
		return c.Status(400).JSON(fiber.Map{
			"success": false,
			"message": "rootFolder is required",
		})
	}

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

	// 3. Start Async Import
	if err := s.importerService.StartNzbdavImport(dbPath, rootFolder, isTempFile); err != nil {
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
func (s *Server) handleGetNzbdavImportStatus(c *fiber.Ctx) error {
	if s.importerService == nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Importer service not available",
		})
	}

	status := s.importerService.GetImportStatus()
	return c.Status(200).JSON(fiber.Map{
		"success": true,
		"data":    toImportStatusResponse(status),
	})
}

// handleCancelNzbdavImport handles DELETE /import/nzbdav
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

func toImportStatusResponse(info importer.ImportInfo) map[string]interface{} {
	return map[string]interface{}{
		"status":     string(info.Status),
		"total":      info.Total,
		"added":      info.Added,
		"failed":     info.Failed,
		"last_error": info.LastError,
	}
}
