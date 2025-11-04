package utils

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/javi11/altmount/internal/config"
)

// SymlinkFinder handles finding library symlinks with caching
type SymlinkFinder struct {
	cache   map[string]string // mount path -> library symlink path
	cacheMu sync.RWMutex
}

// NewSymlinkFinder creates a new symlink finder
func NewSymlinkFinder() *SymlinkFinder {
	return &SymlinkFinder{
		cache: make(map[string]string),
	}
}

// FindLibrarySymlink searches for a symlink in the library directory that points to the given file path
// It checks the cache first, and if not found, performs a recursive search through the library directory
// Returns the library symlink path if found, empty string otherwise
func (sf *SymlinkFinder) FindLibrarySymlink(ctx context.Context, mountFilePath string, cfg *config.Config) (string, error) {
	// If library_dir is not configured, return empty
	if cfg.Health.LibraryDir == nil || *cfg.Health.LibraryDir == "" {
		return "", nil
	}

	libraryDir := *cfg.Health.LibraryDir

	// Check cache first
	sf.cacheMu.RLock()
	if cachedPath, ok := sf.cache[mountFilePath]; ok {
		sf.cacheMu.RUnlock()

		// Verify the cached symlink still exists
		if _, err := os.Lstat(cachedPath); err == nil {
			slog.DebugContext(ctx, "Found symlink in cache", "mount_path", mountFilePath, "library_path", cachedPath)
			return cachedPath, nil
		}

		// Symlink no longer exists, remove from cache and continue searching
		sf.cacheMu.Lock()
		delete(sf.cache, mountFilePath)
		sf.cacheMu.Unlock()
		slog.DebugContext(ctx, "Cached symlink no longer exists, removed from cache", "mount_path", mountFilePath, "cached_path", cachedPath)
		// Fall through to directory search
	} else {
		sf.cacheMu.RUnlock()
	}

	// Get the mount directory from config to filter symlinks
	mountDir := cfg.Metadata.RootPath

	slog.InfoContext(ctx, "Searching for library symlink",
		"mount_path", mountFilePath,
		"library_dir", libraryDir,
		"mount_dir", mountDir)

	var foundSymlink string

	// Walk the library directory recursively
	err := filepath.WalkDir(libraryDir, func(path string, d os.DirEntry, err error) error {
		// Check context cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if err != nil {
			return nil // Continue walking despite errors
		}

		// Skip if not a symlink
		if d.Type()&os.ModeSymlink == 0 {
			return nil
		}

		// Read the symlink target
		target, err := os.Readlink(path)
		if err != nil {
			slog.WarnContext(ctx, "Failed to read symlink", "path", path, "error", err)
			return nil
		}

		// Make target absolute if it's relative
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(path), target)
		}

		// Clean the paths for comparison
		cleanTarget := filepath.Clean(target)
		cleanMountPath := filepath.Clean(mountFilePath)

		// Check if this symlink points to our mount path
		if cleanTarget == cleanMountPath {
			foundSymlink = path

			return filepath.SkipAll // Stop walking once found
		}

		// Cache symlinks that point to the mount directory for potential future use
		if strings.HasPrefix(cleanTarget, mountDir) {
			sf.cacheMu.Lock()
			sf.cache[cleanTarget] = path
			sf.cacheMu.Unlock()
		}

		return nil
	})

	if err != nil && err != filepath.SkipAll {
		slog.ErrorContext(ctx, "Error during library symlink search", "error", err)
		return "", err
	}

	if foundSymlink != "" {
		// Cache the successful finding
		sf.cacheMu.Lock()
		sf.cache[mountFilePath] = foundSymlink
		sf.cacheMu.Unlock()
		return foundSymlink, nil
	}

	slog.InfoContext(ctx, "No matching symlink found in library directory", "mount_path", mountFilePath)
	return "", nil
}
