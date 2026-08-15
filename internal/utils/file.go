package utils

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"
)

// MoveFile atomically renames a file from src to dst.
// If the rename fails due to cross-device boundaries, it falls back to a robust copy-and-delete.
func MoveFile(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	} else if !isCrossDeviceError(err) {
		return err
	}

	// Fallback: Copy-and-delete for cross-device moves
	srcInfo, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("failed to stat source file: %w", err)
	}

	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open source file: %w", err)
	}
	defer srcFile.Close()

	// Create/overwrite destination with the same permissions as source
	dstFile, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, srcInfo.Mode().Perm())
	if err != nil {
		return fmt.Errorf("failed to create destination file: %w", err)
	}
	defer dstFile.Close()

	// Copy content
	if _, err := io.Copy(dstFile, srcFile); err != nil {
		dstFile.Close()
		os.Remove(dst) // Clean up partial write
		return fmt.Errorf("failed to copy content: %w", err)
	}

	// Sync to ensure data integrity
	if err := dstFile.Sync(); err != nil {
		dstFile.Close()
		os.Remove(dst) // Clean up partial write
		return fmt.Errorf("failed to sync destination file: %w", err)
	}

	// Close both files before attempting removal
	srcFile.Close()
	if err := dstFile.Close(); err != nil {
		return fmt.Errorf("failed to close destination file: %w", err)
	}

	// Remove source file
	if err := os.Remove(src); err != nil {
		return fmt.Errorf("failed to remove source file after successful copy: %w", err)
	}

	return nil
}

// isCrossDeviceError checks if an error is a cross-device link error (EXDEV).
func isCrossDeviceError(err error) bool {
	if err == nil {
		return false
	}
	// os.LinkError and os.PathError both unwrap to the underlying syscall error.
	if errors.Is(err, syscall.EXDEV) || isPlatformCrossDeviceError(err) {
		return true
	}
	// Fallback to string check. Windows never reports EXDEV (see isPlatformCrossDeviceError),
	// so its ERROR_NOT_SAME_DEVICE message is matched here as well.
	errStr := err.Error()
	return strings.Contains(errStr, "cross-device") || strings.Contains(errStr, "invalid cross-device link") ||
		strings.Contains(errStr, "EXDEV") || strings.Contains(errStr, "different disk drive")
}
