// Package errors provides shared error types used across multiple packages.
// This package exists to avoid import cycles between importer and its subpackages.
package errors

import (
	"errors"
	"fmt"
)

// NonRetryableError represents an error that should not be retried.
// Operations that encounter this error type should fail immediately
// without retry attempts.
type NonRetryableError struct {
	message string
	cause   error
}

// Error implements the error interface.
func (e *NonRetryableError) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("%s: %v", e.message, e.cause)
	}
	return e.message
}

// Unwrap returns the underlying cause error for error unwrapping.
func (e *NonRetryableError) Unwrap() error {
	return e.cause
}

// Is checks if the target error is a NonRetryableError.
func (e *NonRetryableError) Is(target error) bool {
	_, ok := target.(*NonRetryableError)
	return ok
}

// NewNonRetryableError creates a new non-retryable error with a message and optional cause.
func NewNonRetryableError(message string, cause error) error {
	return &NonRetryableError{
		message: message,
		cause:   cause,
	}
}

// WrapNonRetryable wraps an existing error as non-retryable.
func WrapNonRetryable(cause error) error {
	if cause == nil {
		return nil
	}
	return &NonRetryableError{
		message: "operation failed with non-retryable error",
		cause:   cause,
	}
}

// IsNonRetryable checks if an error is non-retryable.
func IsNonRetryable(err error) bool {
	if err == nil {
		return false
	}
	var nonRetryableErr *NonRetryableError
	return errors.As(err, &nonRetryableErr)
}

// Sentinel errors for common non-retryable conditions.
var (
	// ErrNoRetryable is kept for backward compatibility with existing code.
	ErrNoRetryable = &NonRetryableError{
		message: "no retryable errors found",
		cause:   nil,
	}

	// ErrNoVideoFiles indicates that an import contains no video files.
	ErrNoVideoFiles = &NonRetryableError{
		message: "import contains no video files",
		cause:   nil,
	}

	// ErrFallbackNotConfigured indicates that SABnzbd fallback is not enabled or configured.
	ErrFallbackNotConfigured = &NonRetryableError{
		message: "SABnzbd fallback not configured",
		cause:   nil,
	}
)
