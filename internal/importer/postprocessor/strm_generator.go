package postprocessor

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/javi11/altmount/internal/auth"
	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/altmount/internal/database"
)

// CreateStrmFiles creates STRM files for an imported file or directory
func (c *Coordinator) CreateStrmFiles(ctx context.Context, item *database.ImportQueueItem, resultingPath string) error {
	cfg := c.configGetter()

	// Check if STRM is enabled
	if cfg.Import.ImportStrategy != config.ImportStrategySTRM {
		return nil // Skip if not enabled
	}

	if cfg.Import.ImportDir == nil || *cfg.Import.ImportDir == "" {
		return fmt.Errorf("STRM directory not configured")
	}

	// Keep the original resulting path for metadata and streaming URL
	originalResultingPath := resultingPath

	category := ""
	if item.Category != nil {
		category = *item.Category
	}

	// Build the clean, isolated library path: [CompleteDir]/[Category]/<remainder>,
	// stripping any of those prefixes that are already present in the source path.
	resultingPath = buildLibraryRelPath(resultingPath, cfg.SABnzbd.CompleteDir, category)

	// Check the metadata directory to determine if this is a file or directory
	metadataPath := filepath.Join(cfg.Metadata.RootPath, strings.TrimPrefix(originalResultingPath, "/"))
	fileInfo, err := os.Stat(metadataPath)

	// If stat fails, check if it's a .meta file (single file case)
	if err != nil {
		metaFile := metadataPath + ".meta"
		if _, metaErr := os.Stat(metaFile); metaErr == nil {
			return c.CreateSingleStrmFile(ctx, resultingPath, originalResultingPath, cfg.WebDAV.Port)
		}
		return fmt.Errorf("failed to stat metadata path: %w", err)
	}

	if !fileInfo.IsDir() {
		return c.CreateSingleStrmFile(ctx, resultingPath, originalResultingPath, cfg.WebDAV.Port)
	}

	// Directory - walk through and create STRM files for all files
	var strmErrors []error
	strmCount := 0

	err = filepath.WalkDir(metadataPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			c.log.WarnContext(ctx, "Error accessing metadata path during STRM creation",
				"path", path,
				"error", err)
			return nil
		}

		if d.IsDir() || !strings.HasSuffix(d.Name(), ".meta") {
			return nil
		}

		// Calculate relative path from the root metadata directory
		relPathWithMeta, err := filepath.Rel(cfg.Metadata.RootPath, path)
		if err != nil {
			c.log.ErrorContext(ctx, "Failed to calculate relative path",
				"path", path,
				"base", cfg.Metadata.RootPath,
				"error", err)
			return nil
		}

		// Remove .meta extension
		relPath := strings.TrimSuffix(relPathWithMeta, ".meta")

		category := ""
		if item.Category != nil {
			category = *item.Category
		}

		// filepath.Rel returns OS-native separators (backslashes on Windows);
		// buildLibraryRelPath normalises them before stripping so we don't
		// double-prefix the category/CompleteDir on Windows (issue #585).
		strmResultingPath := buildLibraryRelPath(relPath, cfg.SABnzbd.CompleteDir, category)

		if err := c.CreateSingleStrmFile(ctx, strmResultingPath, relPath, cfg.WebDAV.Port); err != nil {
			c.log.ErrorContext(ctx, "Failed to create STRM file",
				"path", relPath,
				"error", err)
			strmErrors = append(strmErrors, err)
			return nil
		}

		strmCount++
		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to walk directory: %w", err)
	}

	if len(strmErrors) > 0 {
		c.log.WarnContext(ctx, "Some STRM files failed to create",
			"queue_id", item.ID,
			"total_errors", len(strmErrors),
			"successful", strmCount)
	}

	return nil
}

// CreateSingleStrmFile creates a STRM file for a single file with authentication
func (c *Coordinator) CreateSingleStrmFile(ctx context.Context, strmResultingPath, originalVirtualPath string, port int) error {
	cfg := c.configGetter()

	baseDir := filepath.Join(*cfg.Import.ImportDir, filepath.Dir(strings.TrimPrefix(strmResultingPath, "/")))

	if err := os.MkdirAll(baseDir, 0775); err != nil {
		return fmt.Errorf("failed to create STRM directory: %w", err)
	}

	// Keep original filename and add .strm extension
	filename := filepath.Base(strmResultingPath) + ".strm"
	strmPath := filepath.Join(*cfg.Import.ImportDir, filepath.Dir(strings.TrimPrefix(strmResultingPath, "/")), filename)

	// Get first admin user's API key for authentication
	if c.userRepo == nil {
		return fmt.Errorf("user repository not available for STRM generation")
	}

	users, err := c.userRepo.GetAllUsers(ctx)
	if err != nil || len(users) == 0 {
		return fmt.Errorf("no users with API keys found for STRM generation: %w", err)
	}

	// Find first admin user with an API key
	var adminAPIKey string
	for _, user := range users {
		if user.IsAdmin && user.APIKey != nil && *user.APIKey != "" {
			adminAPIKey = *user.APIKey
			break
		}
	}

	if adminAPIKey == "" {
		return fmt.Errorf("no admin user with API key found for STRM generation")
	}

	// Hash the API key with SHA256
	hashedKey := auth.HashAPIKey(adminAPIKey)

	// Determine host to use
	host := cfg.WebDAV.Host
	if host == "" {
		host = "localhost"
	}

	// Generate streaming URL with download_key using the ORIGINAL virtual path
	encodedPath := strings.ReplaceAll(originalVirtualPath, " ", "%20")
	streamURL := fmt.Sprintf("http://%s:%d/api/files/stream?path=%s&download_key=%s",
		host, port, encodedPath, hashedKey)

	// Check if STRM file already exists with the same content
	if existingContent, err := os.ReadFile(strmPath); err == nil {
		if string(existingContent) == streamURL {
			return nil // File exists with correct content
		}
	}

	if err := os.WriteFile(strmPath, []byte(streamURL), 0644); err != nil {
		return fmt.Errorf("failed to write STRM file: %w", err)
	}

	return nil
}

