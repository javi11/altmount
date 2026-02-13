// Package pathutil provides path validation utilities.
package pathutil

import (
	"fmt"
	"os"
	"path/filepath"
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

// JoinAbsPath joins the base path with the sub path and returns the absolute path.
// If subPath is already absolute, it's cleaned and returned.
func JoinAbsPath(base, sub string) string {
	if filepath.IsAbs(sub) {
		return filepath.Clean(sub)
	}
	return filepath.Clean(filepath.Join(base, sub))
}
