package api

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/altmount/internal/nzbdav"
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

	if isTempFile {
		defer os.Remove(dbPath) // Clean up temp DB file
	}
	
	// 3. Parse Database
	parser := nzbdav.NewParser(dbPath)
	nzbChan, errChan := parser.Parse() // Use the new channel-based Parse method

	// Create temp dir for NZBs
	// Note: these temp files should be cleaned up by the importer service once processed
	nzbTempDir, err := os.MkdirTemp(os.TempDir(), "altmount-nzbdav-imports-")
	if err != nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Failed to create temp directory for NZBs",
			"details": err.Error(),
		})
	}
	// 4. Queue Imports
	addedCount := 0
	failedCount := 0
	totalCount := 0 // Keep track of total items processed
	
	for {
		select {
		case res, ok := <-nzbChan:
			if !ok {
				nzbChan = nil // Channel closed
				break
			}
			totalCount++

			// Create Temp NZB File
			nzbFileName := fmt.Sprintf("%s.nzb", sanitizeFilename(res.Name))
			nzbPath := filepath.Join(nzbTempDir, nzbFileName)
			
			outFile, err := os.Create(nzbPath)
			if err != nil {
				slog.Error("Failed to create temp NZB file", "file", nzbFileName, "error", err)
				failedCount++
				continue
			}

			_, err = io.Copy(outFile, res.Content)
			outFile.Close()
			if err != nil {
				slog.Error("Failed to write temp NZB file content", "file", nzbFileName, "error", err)
				failedCount++
				os.Remove(nzbPath) // Clean up partial file
				continue
			}

			// Determine Category and Relative Path
			targetCategory := "other"
			lowerCat := strings.ToLower(res.Category)
			if strings.Contains(lowerCat, "movie") {
				targetCategory = "movies"
			} else if strings.Contains(lowerCat, "tv") || strings.Contains(lowerCat, "series") {
				targetCategory = "tv"
			}

			// Append relative path from DB if present (e.g. "Series Name" or "Featurettes")
			// This preserves the directory structure within the category
			if res.RelPath != "" {
				targetCategory = filepath.Join(targetCategory, res.RelPath)
			}
			
			relPath := rootFolder
			priority := database.QueuePriorityNormal
			
			_, err = s.importerService.AddToQueue(nzbPath, &relPath, &targetCategory, &priority)
			if err != nil {
				slog.Error("Failed to add to queue", "release", res.Name, "error", err)
				failedCount++
				os.Remove(nzbPath) // Clean up NZB if not queued
			} else {
				addedCount++
			}
		case err := <-errChan:
			if err != nil {
				slog.Error("Error during NZBDav parsing", "error", err)
				return c.Status(500).JSON(fiber.Map{
					"success": false,
					"message": "Error during database parsing",
					"details": err.Error(),
				})
			}
			errChan = nil // Channel closed
		}

		if nzbChan == nil && errChan == nil {
			break // Both channels closed
		}
	}

	return c.Status(200).JSON(fiber.Map{
		"success": true,
		"data": fiber.Map{
			"added":  addedCount,
			"failed": failedCount,
			"total":  totalCount,
		},
	})
}

func sanitizeFilename(name string) string {
	// Simple sanitization
	return strings.ReplaceAll(name, "/", "_")
}
