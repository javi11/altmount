package postprocessor

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/altmount/internal/database"
)

// CreateSymlinks creates symlinks for an imported item based on the import strategy
func (c *Coordinator) CreateSymlinks(ctx context.Context, item *database.ImportQueueItem, resultingPath string) error {
	cfg := c.configGetter()

	// Check if symlinks are enabled
	if cfg.Import.ImportStrategy != config.ImportStrategySYMLINK {
		return nil // Skip if not enabled
	}

	if cfg.Import.ImportDir == nil || *cfg.Import.ImportDir == "" {
		return fmt.Errorf("symlink directory not configured")
	}

	// Keep the original resulting path for metadata and actual mount path lookups
	originalResultingPath := resultingPath

	// Strip SABnzbd CompleteDir prefix from resultingPath if present
	// This prevents creating nested "complete" folders in the symlink directory
	if cfg.SABnzbd.CompleteDir != "" {
		completeDir := filepath.ToSlash(cfg.SABnzbd.CompleteDir)
		if !strings.HasPrefix(completeDir, "/") {
			completeDir = "/" + completeDir
		}

		// Ensure checkPath starts with / for reliable prefix checking
		checkPath := resultingPath
		if !strings.HasPrefix(checkPath, "/") {
			checkPath = "/" + checkPath
		}

		if strings.HasPrefix(checkPath, completeDir) {
			// Check for directory boundary
			if len(checkPath) == len(completeDir) {
				resultingPath = "/"
			} else if checkPath[len(completeDir)] == '/' {
				resultingPath = checkPath[len(completeDir):]
			}
		}
	}

	// Ensure the resulting path respects the category if provided
	if item.Category != nil && *item.Category != "" {
		category := strings.Trim(*item.Category, "/")
		cleanPath := strings.TrimPrefix(resultingPath, "/")

		// If path doesn't start with category, prepend it
		if !strings.HasPrefix(cleanPath, category+"/") && cleanPath != category {
			resultingPath = filepath.Join(category, cleanPath)
		}
	}

	// Get the actual metadata/mount path (where the content actually lives)
	actualPath := filepath.Join(cfg.MountPath, strings.TrimPrefix(originalResultingPath, "/"))

	// Check the metadata directory to determine if this is a file or directory
	metadataPath := filepath.Join(cfg.Metadata.RootPath, strings.TrimPrefix(originalResultingPath, "/"))
	fileInfo, err := os.Stat(metadataPath)

	// If stat fails, check if it's a .meta file (single file case)
	if err != nil {
		metaFile := metadataPath + ".meta"
		if _, metaErr := os.Stat(metaFile); metaErr == nil {
			return c.createSingleSymlink(actualPath, resultingPath)
		}
		return fmt.Errorf("failed to stat metadata path: %w", err)
	}

	if !fileInfo.IsDir() {
		return c.createSingleSymlink(actualPath, resultingPath)
	}

	// Directory - walk through and create symlinks for all files
	var symlinkErrors []error
	symlinkCount := 0

	err = filepath.WalkDir(metadataPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			c.log.WarnContext(ctx, "Error accessing metadata path during symlink creation",
				"path", path,
				"error", err)
			return nil // Continue walking
		}

		if d.IsDir() || !strings.HasSuffix(d.Name(), ".meta") {
			return nil
		}

		// Calculate relative path from the root metadata directory
		relPath, err := filepath.Rel(cfg.Metadata.RootPath, path)
		if err != nil {
			c.log.ErrorContext(ctx, "Failed to calculate relative path",
				"path", path,
				"base", cfg.Metadata.RootPath,
				"error", err)
			return nil
		}

		// Remove .meta extension
		relPath = strings.TrimSuffix(relPath, ".meta")

		// Build the actual file path in the mount
		actualFilePath := filepath.Join(cfg.MountPath, strings.TrimPrefix(relPath, "/"))

		// Build the symlink resulting path (stripped if needed)
		symlinkResultingPath := relPath
		if cfg.SABnzbd.CompleteDir != "" {
			completeDir := filepath.ToSlash(cfg.SABnzbd.CompleteDir)
			if !strings.HasPrefix(completeDir, "/") {
				completeDir = "/" + completeDir
			}

			// Ensure checkPath starts with / for reliable prefix checking
			checkPath := symlinkResultingPath
			if !strings.HasPrefix(checkPath, "/") {
				checkPath = "/" + checkPath
			}

			if strings.HasPrefix(checkPath, completeDir) {
				// Check for directory boundary
				if len(checkPath) == len(completeDir) {
					symlinkResultingPath = "/"
				} else if checkPath[len(completeDir)] == '/' {
					symlinkResultingPath = checkPath[len(completeDir):]
				}
			}
		}

		// Ensure the resulting path respects the category if provided
		if item.Category != nil && *item.Category != "" {
			category := strings.Trim(*item.Category, "/")
			cleanPath := strings.TrimPrefix(symlinkResultingPath, "/")

			// If path doesn't start with category, prepend it
			if !strings.HasPrefix(cleanPath, category+"/") && cleanPath != category {
				symlinkResultingPath = filepath.Join(category, cleanPath)
			}
		}

		if err := c.createSingleSymlink(actualFilePath, symlinkResultingPath); err != nil {
			c.log.ErrorContext(ctx, "Failed to create symlink",
				"path", actualFilePath,
				"error", err)
			symlinkErrors = append(symlinkErrors, err)
			return nil
		}

		symlinkCount++
		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to walk directory: %w", err)
	}

	if len(symlinkErrors) > 0 {
		c.log.WarnContext(ctx, "Some symlinks failed to create",
			"queue_id", item.ID,
			"total_errors", len(symlinkErrors),
			"successful", symlinkCount)
	}

	return nil
}

// createSingleSymlink creates a symlink for a single file
func (c *Coordinator) createSingleSymlink(actualPath, resultingPath string) error {
	cfg := c.configGetter()

	baseDir := filepath.Join(*cfg.Import.ImportDir, filepath.Dir(strings.TrimPrefix(resultingPath, "/")))

	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return fmt.Errorf("failed to create symlink category directory: %w", err)
	}

	symlinkPath := filepath.Join(*cfg.Import.ImportDir, strings.TrimPrefix(resultingPath, "/"))

	// Remove existing symlink if present
	if _, err := os.Lstat(symlinkPath); err == nil {
		if err := os.Remove(symlinkPath); err != nil {
			return fmt.Errorf("failed to remove existing symlink: %w", err)
		}
	}

	// Create the symlink using the absolute actual path
	if err := os.Symlink(actualPath, symlinkPath); err != nil {
		return fmt.Errorf("failed to create symlink: %w", err)
	}

	return nil
}
