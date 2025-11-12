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

// LibraryItemFinder handles finding library items (symlinks and STRM files) with caching
type LibraryItemFinder struct {
	cache   map[string]string // virtual path -> library item path
	cacheMu sync.RWMutex
}

// NewLibraryItemFinder creates a new library item finder
func NewLibraryItemFinder() *LibraryItemFinder {
	return &LibraryItemFinder{
		cache: make(map[string]string),
	}
}

// FindLibraryItem searches for a library item (symlink or .strm file) based on import strategy
// It checks the cache first, and if not found, performs a recursive search through the library directory
// Returns the library item path if found, empty string otherwise
func (lif *LibraryItemFinder) FindLibraryItem(ctx context.Context, filePath string, cfg *config.Config) (string, error) {
	// If library_dir is not configured, return empty
	if cfg.Health.LibraryDir == nil || *cfg.Health.LibraryDir == "" {
		return "", nil
	}

	libraryDir := *cfg.Health.LibraryDir
	mountDir := cfg.MountPath

	// Check cache first using virtual path as key
	lif.cacheMu.RLock()
	if cachedPath, ok := lif.cache[filePath]; ok {
		lif.cacheMu.RUnlock()

		// Verify the cached item still exists
		if _, err := os.Lstat(cachedPath); err == nil {
			slog.DebugContext(ctx, "Found library item in cache",
				"virtual_path", filePath,
				"library_path", cachedPath)
			return cachedPath, nil
		}

		// Item no longer exists, remove from cache and continue searching
		lif.cacheMu.Lock()
		delete(lif.cache, filePath)
		lif.cacheMu.Unlock()
		slog.DebugContext(ctx, "Cached library item no longer exists, removed from cache",
			"virtual_path", filePath,
			"cached_path", cachedPath)
		// Fall through to directory search
	} else {
		lif.cacheMu.RUnlock()
	}

	// Get import strategy for selection logic
	strategy := cfg.Import.ImportStrategy

	slog.InfoContext(ctx, "Searching for library item",
		"virtual_path", filePath,
		"library_dir", libraryDir,
		"strategy", strategy)

	// Search for both symlinks and STRM files in a single pass
	foundSymlink, foundStrm := lif.findBothLibraryItems(ctx, filePath, libraryDir, mountDir)

	// Use strategy to decide which one to return if both exist
	var foundItem string
	if foundSymlink != "" && foundStrm != "" {
		switch strategy {
		case config.ImportStrategySYMLINK:
			foundItem = foundSymlink
			if foundItem != "" {
				slog.InfoContext(ctx, "Using symlink (strategy: SYMLINK)",
					"virtual_path", filePath,
					"library_path", foundItem)
			}

		case config.ImportStrategySTRM:
			foundItem = foundStrm
			if foundItem != "" {
				slog.InfoContext(ctx, "Using STRM file (strategy: STRM)",
					"virtual_path", filePath,
					"library_path", foundItem)
			}

		case config.ImportStrategyNone:
			// No library items should be used
			slog.DebugContext(ctx, "Import strategy is NONE, not using any library items found")
			return "", nil

		default:
			slog.WarnContext(ctx, "Unknown import strategy", "strategy", strategy)
			return "", nil
		}
	} else if foundSymlink != "" {
		foundItem = foundSymlink
	} else if foundStrm != "" {
		foundItem = foundStrm
	}

	if foundItem != "" {
		// Cache the successful finding
		lif.cacheMu.Lock()
		lif.cache[filePath] = foundItem
		lif.cacheMu.Unlock()
		return foundItem, nil
	}

	slog.InfoContext(ctx, "No matching library item found",
		"virtual_path", filePath,
		"strategy", strategy,
		"found_symlink", foundSymlink != "",
		"found_strm", foundStrm != "")
	return "", nil
}

// findBothLibraryItems searches for both symlinks and .strm files in a single directory walk
// Returns both paths (empty strings if not found), allowing caller to decide which to use based on strategy
func (lif *LibraryItemFinder) findBothLibraryItems(ctx context.Context, filePath, libraryDir, mountDir string) (symlinkPath, strmPath string) {
	mountFilePath := filepath.Join(mountDir, filePath)

	// Expected STRM file path (for quick comparison)
	expectedStrmPath := filepath.Join(libraryDir, filePath+".strm")
	expectedStrmPathNormalized := filepath.Join(libraryDir, strings.ReplaceAll(filePath, "\\", "/")+".strm")

	// Walk the library directory recursively once, checking for both types
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

		// Check if it's a symlink
		if d.Type()&os.ModeSymlink != 0 {
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
				symlinkPath = path
				slog.DebugContext(ctx, "Found symlink for virtual path",
					"virtual_path", filePath,
					"symlink_path", path)
			}
		}

		// Check if it's a .strm file matching our virtual path
		if !d.IsDir() && strings.HasSuffix(d.Name(), ".strm") {
			cleanPath := filepath.Clean(path)
			cleanExpected := filepath.Clean(expectedStrmPath)
			cleanExpectedNorm := filepath.Clean(expectedStrmPathNormalized)

			if cleanPath == cleanExpected || cleanPath == cleanExpectedNorm {
				strmPath = path
				slog.DebugContext(ctx, "Found .strm file for virtual path",
					"virtual_path", filePath,
					"strm_path", path)
			}
		}

		// Early exit if both found
		if symlinkPath != "" && strmPath != "" {
			return filepath.SkipAll
		}

		return nil
	})

	if err != nil && err != filepath.SkipAll {
		slog.ErrorContext(ctx, "Error during library item search", "error", err)
	}

	return symlinkPath, strmPath
}

// FindLibrarySymlink searches for a symlink in the library directory that points to the given file path
// Deprecated: Use FindLibraryItem instead, which handles both symlinks and STRM files based on import strategy
// Returns the library symlink path if found, empty string otherwise
func (lif *LibraryItemFinder) FindLibrarySymlink(ctx context.Context, filePath string, cfg *config.Config) (string, error) {
	// For backward compatibility, only search for symlinks
	// Check if library_dir is configured
	if cfg.Health.LibraryDir == nil || *cfg.Health.LibraryDir == "" {
		return "", nil
	}

	// Check if using symlink strategy
	if cfg.Import.ImportStrategy != config.ImportStrategySYMLINK {
		return "", nil
	}

	// Use FindLibraryItem which will search for symlinks
	return lif.FindLibraryItem(ctx, filePath, cfg)
}
