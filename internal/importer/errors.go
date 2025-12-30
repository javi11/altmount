// Package importer provides import queue processing for NZB files.
package importer

import (
	sharedErrors "github.com/javi11/altmount/internal/errors"
)

// Re-export error types and functions from shared errors package
// for backward compatibility with existing code.
type NonRetryableError = sharedErrors.NonRetryableError

var (
	// NewNonRetryableError creates a new non-retryable error with a message and optional cause.
	NewNonRetryableError = sharedErrors.NewNonRetryableError

	// WrapNonRetryable wraps an existing error as non-retryable.
	WrapNonRetryable = sharedErrors.WrapNonRetryable

	// IsNonRetryable checks if an error is non-retryable.
	IsNonRetryable = sharedErrors.IsNonRetryable

	// ErrNoRetryable is kept for backward compatibility with existing code.
	ErrNoRetryable = sharedErrors.ErrNoRetryable

	// ErrNoVideoFiles indicates that an import contains no video files.
	ErrNoVideoFiles = sharedErrors.ErrNoVideoFiles

	// ErrFallbackNotConfigured indicates that SABnzbd fallback is not enabled or configured.
	ErrFallbackNotConfigured = sharedErrors.ErrFallbackNotConfigured
)
