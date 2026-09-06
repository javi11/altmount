// Package importer provides import queue processing for NZB files.
package importer

import (
	"errors"

	sharedErrors "github.com/kipsilabs/altmount/internal/errors"
)

// ErrContentProbeInconclusive means the media content probe could not reach a
// verdict because of a transient provider, timeout, or connection error.
// Nothing is known about the written content, so the release must not be
// reported to the Arr app or counted towards its blocklist breaker.
var ErrContentProbeInconclusive = errors.New("content verification inconclusive")

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

	// ErrFallbackNotConfigured indicates that SABnzbd fallback is not enabled or configured.
	ErrFallbackNotConfigured = sharedErrors.ErrFallbackNotConfigured

	// ErrArticlesNotFound indicates that one or more NZB segments could not be found on any provider.
	ErrArticlesNotFound = sharedErrors.ErrArticlesNotFound
)
