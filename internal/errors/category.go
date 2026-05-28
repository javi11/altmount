// Package errors provides shared error types used across multiple packages.
package errors

import (
	"errors"
	"strings"
)

// FailureCategory classifies why a queue item failed.
// It is stored in the import_queue.failure_category column and returned in the API response.
type FailureCategory string

const (
	// FailureArticlesNotFound: segments could not be found on any configured Usenet provider.
	FailureArticlesNotFound FailureCategory = "articles_not_found"

	// FailureProviderError: NNTP connection, authentication, or protocol error.
	FailureProviderError FailureCategory = "provider_error"

	// FailureExtractionFailed: RAR/7z decompression or archive parsing failed.
	FailureExtractionFailed FailureCategory = "extraction_failed"

	// FailureCorruptedFile: yEnc decode failure, segment assembly error, or data corruption.
	FailureCorruptedFile FailureCategory = "corrupted_file"

	// FailureTimeout: operation exceeded its deadline or read timeout.
	FailureTimeout FailureCategory = "timeout"

	// FailureCancelled: processing was cancelled by user request.
	FailureCancelled FailureCategory = "cancelled"

	// FailurePasswordNeeded: archive requires a password that was not provided.
	FailurePasswordNeeded FailureCategory = "password_needed"

	// FailureDiskFull: insufficient disk space to write output files.
	FailureDiskFull FailureCategory = "disk_full"

	// FailureInternal: unexpected internal error or unknown failure.
	FailureInternal FailureCategory = "internal"
)

// CategorizedError wraps an error with a FailureCategory.
// Use errors.As to extract the category from the error chain.
type CategorizedError struct {
	Category FailureCategory
	Err      error
}

// Error returns the underlying error message.
func (e *CategorizedError) Error() string {
	if e == nil || e.Err == nil {
		return ""
	}
	return e.Err.Error()
}

// Unwrap returns the underlying error for errors.As/errors.Is traversal.
func (e *CategorizedError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// Categorize tags err with a category. Returns nil when err is nil.
func Categorize(cat FailureCategory, err error) error {
	if err == nil {
		return nil
	}
	return &CategorizedError{Category: cat, Err: err}
}

// CategoryOf walks the error chain and extracts the first FailureCategory.
// Returns FailureInternal if no category is found.
func CategoryOf(err error) FailureCategory {
	if err == nil {
		return FailureInternal
	}
	var ce *CategorizedError
	if errors.As(err, &ce) {
		return ce.Category
	}
	return FailureInternal
}

// UserMessage returns a human-readable one-line summary for a failure category.
func (c FailureCategory) UserMessage() string {
	switch c {
	case FailureArticlesNotFound:
		return "Some segments could not be found on any provider. Try a different NZB or check provider availability."
	case FailureProviderError:
		return "The Usenet provider connection failed. Check provider credentials and network."
	case FailureExtractionFailed:
		return "Could not extract archive contents. The file may be incomplete or corrupt."
	case FailureCorruptedFile:
		return "File data is damaged or incomplete. The download may need to be retried."
	case FailureTimeout:
		return "The operation took too long to complete. Check network speed or increase timeout settings."
	case FailureCancelled:
		return "Processing was cancelled by user request."
	case FailurePasswordNeeded:
		return "This archive is password protected. Add a password to the import config."
	case FailureDiskFull:
		return "No disk space remaining for output files."
	default:
		return "An unexpected error occurred during processing."
	}
}

// CategoryFromError determines the FailureCategory by inspecting an error message
// and any wrapped categorized errors. This is the main classification function used
// when errors arrive from subsystems that don't explicitly categorize their errors.
func CategoryFromError(err error) FailureCategory {
	if err == nil {
		return FailureInternal
	}

	// Check for explicit category wrapping first.
	if cat := CategoryOf(err); cat != FailureInternal {
		return cat
	}

	errMsg := err.Error()

	// Cancellation detection (must come before more specific checks since
	// cancelled operations may produce misleading downstream errors).
	if strings.Contains(errMsg, "context canceled") ||
		strings.Contains(errMsg, "context deadline exceeded") ||
		strings.Contains(errMsg, "processing cancelled") {
		return FailureCancelled
	}

	// Article-not-found detection.
	if strings.Contains(errMsg, "article is not found") ||
		strings.Contains(errMsg, "article not found") ||
		strings.Contains(errMsg, ErrArticlesNotFound.Error()) {
		return FailureArticlesNotFound
	}

	// Provider/connection errors.
	if strings.Contains(errMsg, "connection refused") ||
		strings.Contains(errMsg, "connection reset") ||
		strings.Contains(errMsg, "no such host") ||
		strings.Contains(errMsg, "authentication failed") ||
		strings.Contains(errMsg, "401") ||
		strings.Contains(errMsg, "502") ||
		strings.Contains(errMsg, "503") {
		return FailureProviderError
	}

	// Timeout detection.
	if strings.Contains(errMsg, "timeout") ||
		strings.Contains(errMsg, "deadline") {
		return FailureTimeout
	}

	// Disk space.
	if strings.Contains(errMsg, "no space left") ||
		strings.Contains(errMsg, "disk full") {
		return FailureDiskFull
	}

	// Password detection.
	if strings.Contains(errMsg, "password") ||
		strings.Contains(errMsg, "encrypted") ||
		strings.Contains(errMsg, "encryption") {
		return FailurePasswordNeeded
	}

	// Extraction failures.
	if strings.Contains(errMsg, "extraction") ||
		strings.Contains(errMsg, "unpack") ||
		strings.Contains(errMsg, "decompression") {
		return FailureExtractionFailed
	}

	// Corruption detection.
	if strings.Contains(errMsg, "corrupted") ||
		strings.Contains(errMsg, "corrupt") ||
		strings.Contains(errMsg, "crc") ||
		strings.Contains(errMsg, "checksum") ||
		strings.Contains(errMsg, "yenc") {
		return FailureCorruptedFile
	}

	// No NNTP providers configured.
	if strings.Contains(errMsg, "no NNTP providers configured") {
		return FailureProviderError
	}

	return FailureInternal
}
