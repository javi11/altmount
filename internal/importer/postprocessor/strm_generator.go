package postprocessor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/altmount/internal/pathutil"
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

	// Check the metadata directory to determine if this is a file or directory
	metadataPath := pathutil.JoinAbsPath(cfg.Metadata.RootPath, resultingPath)
	fileInfo, err := os.Stat(metadataPath)

	// If stat fails, check if it's a .meta file (single file case)
	if err != nil {
		metaFile := metadataPath + ".meta"
		if _, metaErr := os.Stat(metaFile); metaErr == nil {
			return c.createSingleStrmFile(ctx, resultingPath, cfg.WebDAV.Port)
		}
		return fmt.Errorf("failed to stat metadata path: %w", err)
	}

	if !fileInfo.IsDir() {
		return c.createSingleStrmFile(ctx, resultingPath, cfg.WebDAV.Port)
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

		if err := c.createSingleStrmFile(ctx, relPath, cfg.WebDAV.Port); err != nil {
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

// createSingleStrmFile creates a STRM file for a single file with authentication
func (c *Coordinator) createSingleStrmFile(ctx context.Context, virtualPath string, port int) error {
	cfg := c.configGetter()

	strmPath := pathutil.JoinAbsPath(*cfg.Import.ImportDir, virtualPath) + ".strm"
	baseDir := filepath.Dir(strmPath)

	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return fmt.Errorf("failed to create STRM directory: %w", err)
	}

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
	hashedKey := hashAPIKey(adminAPIKey)

	// Determine host to use
	host := cfg.WebDAV.Host
	if host == "" {
		host = "localhost"
	}

	// Generate streaming URL with download_key
	encodedPath := strings.ReplaceAll(virtualPath, " ", "%20")
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

// hashAPIKey generates a SHA256 hash of the API key for secure comparison
func hashAPIKey(apiKey string) string {
	hash := sha256.Sum256([]byte(apiKey))
	return hex.EncodeToString(hash[:])
}
