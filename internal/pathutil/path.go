// Package pathutil provides path validation utilities.
package pathutil

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// CheckDirectoryWritable checks if a directory exists and is writable.
// If the directory doesn't exist, it attempts to create it.
func CheckDirectoryWritable(path string) error {
	if path == "" {
		return fmt.Errorf("path cannot be empty")
	}

	// Convert to absolute path for clearer error messages
	absPath, err := filepath.Abs(path)
	if err != nil {
		absPath = path // fallback to original if abs fails
	}

	// Check if path exists
	info, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Directory doesn't exist, try to create it
			if err := os.MkdirAll(absPath, 0755); err != nil {
				return fmt.Errorf("directory %s does not exist and cannot be created: %w", absPath, err)
			}
		} else {
			return fmt.Errorf("cannot access directory %s: %w", absPath, err)
		}
	} else {
		// Path exists, check if it's a directory
		if !info.IsDir() {
			return fmt.Errorf("path %s exists but is not a directory", absPath)
		}
	}

	// Test write permissions by creating a temporary file
	testFile := filepath.Join(absPath, ".altmount-write-test")
	file, err := os.Create(testFile)
	if err != nil {
		return fmt.Errorf("directory %s is not writable: %w", absPath, err)
	}

	// Write some test data
	_, writeErr := file.Write([]byte("test"))
	file.Close()

	// Clean up test file
	os.Remove(testFile)

	if writeErr != nil {
		return fmt.Errorf("directory %s is not writable: %w", absPath, writeErr)
	}

	return nil
}

// RemoveEmptyDirs recursively removes empty parent directories starting from 'path'
// up towards 'root' (exclusive). It stops if it encounters a non-empty directory
// or reaches the root.
func RemoveEmptyDirs(root, path string) {
	if root == "" || path == "" {
		return
	}

	// Clean paths for consistent comparison
	root = filepath.Clean(root)
	path = filepath.Clean(path)

	// If path is root or not under root, stop
	if path == root || !strings.HasPrefix(path, root) {
		return
	}

	// Try to remove the directory
	err := os.Remove(path)
	if err != nil {
		// Directory is likely not empty or we lack permissions
		return
	}

	// Successfully removed, try the parent
	parent := filepath.Dir(path)
	RemoveEmptyDirs(root, parent)
}

// JoinAbsPath safely joins a base path with another path (which could be absolute or relative).
// If the second path is absolute and starts with the base path, it returns the second path as is.
// Otherwise, it joins them normally.
func JoinAbsPath(basePath, otherPath string) string {
	if basePath == "" {
		return otherPath
	}

	// Ensure consistent slashes for comparison
	cleanBase := strings.TrimSuffix(filepath.ToSlash(basePath), "/")
	cleanOther := filepath.ToSlash(otherPath)

	// If otherPath is absolute and starts with basePath, don't join
	if filepath.IsAbs(cleanOther) && (cleanOther == cleanBase || strings.HasPrefix(cleanOther, cleanBase+"/")) {
		return filepath.FromSlash(cleanOther)
	}

	// Join them, ensuring otherPath is treated as relative to base
	relOther := strings.TrimPrefix(cleanOther, "/")
	return filepath.Join(basePath, filepath.FromSlash(relOther))
}

// CheckFileDirectoryWritable checks if the directory containing a file path is writable.
func CheckFileDirectoryWritable(filePath string, fileType string) error {
	if filePath == "" {
		return nil // Empty path is valid for some config options (like log file)
	}

	// Get the directory part of the file path
	dir := filepath.Dir(filePath)
	if dir == "" || dir == "." {
		dir = "./" // current directory
	}

	if err := CheckDirectoryWritable(dir); err != nil {
		return fmt.Errorf("%s file directory check failed: %w", fileType, err)
	}

	return nil
}
