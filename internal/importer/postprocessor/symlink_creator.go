package postprocessor

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
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

	// 1. Get the internal relative path (relative to FUSE mount)
	relPath := strings.TrimPrefix(resultingPath, "/")

	// 2. Strip any existing /complete or /category prefix from the internal path to start clean
	category := ""
	if item.Category != nil && *item.Category != "" {
		category = strings.Trim(*item.Category, "/")
	}

	if cfg.SABnzbd.CompleteDir != "" {
		completeDir := strings.Trim(filepath.ToSlash(cfg.SABnzbd.CompleteDir), "/")
		if after, ok := strings.CutPrefix(relPath, completeDir+"/"); ok {
			relPath = after
		} else if relPath == completeDir {
			relPath = ""
		}
	}
	if category != "" {
		if after, ok := strings.CutPrefix(relPath, category+"/"); ok {
			relPath = after
		} else if relPath == category {
			relPath = ""
		}
	}

	// 3. Build the clean, isolated library path
	// Construct: [CompleteDir] + [Category] + RelPath
	pathParts := []string{}
	if cfg.SABnzbd.CompleteDir != "" {
		pathParts = append(pathParts, strings.Trim(cfg.SABnzbd.CompleteDir, "/"))
	}
	if category != "" {
		pathParts = append(pathParts, category)
	}
	pathParts = append(pathParts, relPath)

	resultingPath = filepath.Join(pathParts...)
	resultingPath = filepath.ToSlash(filepath.Clean(resultingPath))

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

		// 1. Get the internal relative path (relative to FUSE mount)
		relPath = strings.TrimPrefix(relPath, "/")

		// 2. Strip any existing /complete or /category prefix from the internal path to start clean
		category := ""
		if item.Category != nil && *item.Category != "" {
			category = strings.Trim(*item.Category, "/")
		}

		if cfg.SABnzbd.CompleteDir != "" {
			completeDir := strings.Trim(filepath.ToSlash(cfg.SABnzbd.CompleteDir), "/")
			if after, ok := strings.CutPrefix(relPath, completeDir+"/"); ok {
				relPath = after
			} else if relPath == completeDir {
				relPath = ""
			}
		}
		if category != "" {
			if after, ok := strings.CutPrefix(relPath, category+"/"); ok {
				relPath = after
			} else if relPath == category {
				relPath = ""
			}
		}

		// 3. Build the clean, isolated library path
		// Construct: [CompleteDir] + [Category] + RelPath
		pathParts := []string{}
		if cfg.SABnzbd.CompleteDir != "" {
			pathParts = append(pathParts, strings.Trim(cfg.SABnzbd.CompleteDir, "/"))
		}
		if category != "" {
			pathParts = append(pathParts, category)
		}
		pathParts = append(pathParts, relPath)

		symlinkResultingPath := filepath.Join(pathParts...)
		symlinkResultingPath = filepath.ToSlash(filepath.Clean(symlinkResultingPath))

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

	if err := os.MkdirAll(baseDir, 0775); err != nil {
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
	if runtime.GOOS == "windows" {
		return fmt.Errorf("symlinks are not supported on Windows; use STRM import strategy instead")
	}
	if err := os.Symlink(actualPath, symlinkPath); err != nil {
		return fmt.Errorf("failed to create symlink: %w", err)
	}

	return nil
}
