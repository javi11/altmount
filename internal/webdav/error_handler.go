package webdav

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"

	"github.com/javi11/altmount/internal/nzbfilesystem"
	"github.com/javi11/altmount/internal/slogutil"
	"golang.org/x/net/webdav"
)

// customErrorHandler wraps a webdav.FileSystem and maps our custom errors to HTTP status codes
type customErrorHandler struct {
	fileSystem webdav.FileSystem
}

// Implement webdav.FileSystem interface by delegating to wrapped filesystem
func (c *customErrorHandler) Mkdir(ctx context.Context, name string, perm os.FileMode) error {
	return c.fileSystem.Mkdir(ctx, name, perm)
}

func (c *customErrorHandler) RemoveAll(ctx context.Context, name string) error {
	return c.fileSystem.RemoveAll(ctx, name)
}

func (c *customErrorHandler) Rename(ctx context.Context, oldName, newName string) error {
	return c.fileSystem.Rename(ctx, oldName, newName)
}

func (c *customErrorHandler) Stat(ctx context.Context, name string) (os.FileInfo, error) {
	return c.fileSystem.Stat(ctx, name)
}

func (c *customErrorHandler) OpenFile(ctx context.Context, name string, flag int, perm os.FileMode) (webdav.File, error) {
	file, err := c.fileSystem.OpenFile(ctx, name, flag, perm)
	if err != nil {
		return nil, c.mapError(err)
	}

	ctx = slogutil.With(
		ctx,
		"path", name,
	)

	// Wrap the file to handle read errors
	return &errorHandlingFile{
		File: file,
		ctx:  ctx,
	}, nil
}

// mapError converts our custom errors to appropriate HTTP errors
func (c *customErrorHandler) mapError(err error) error {
	// Check for our custom error types
	var partialErr *nzbfilesystem.PartialContentError
	var corruptedErr *nzbfilesystem.CorruptedFileError

	if errors.As(err, &partialErr) {
		// Partial content - return 206 via custom error
		return &HTTPError{
			StatusCode: http.StatusPartialContent,
			Message:    "Partial content available due to missing articles",
			Err:        err,
		}
	}

	if errors.As(err, &corruptedErr) || errors.Is(err, nzbfilesystem.ErrFileIsCorrupted) {
		// Corrupted file - return 404 Not Found
		return &HTTPError{
			StatusCode: http.StatusNotFound,
			Message:    "File unavailable due to missing articles",
			Err:        err,
		}
	}

	// Return original error for other cases
	return err
}

// HTTPError represents an HTTP error with a specific status code
type HTTPError struct {
	StatusCode int
	Message    string
	Err        error
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("HTTP %d: %s", e.StatusCode, e.Message)
}

func (e *HTTPError) Unwrap() error {
	return e.Err
}

// errorHandlingFile wraps a webdav.File and handles read errors from our virtual files
type errorHandlingFile struct {
	webdav.File
	ctx context.Context
}

func (f *errorHandlingFile) Read(p []byte) (int, error) {
	n, err := f.File.Read(p)
	if err != nil && err != io.EOF {
		// Check for our custom error types
		var partialErr *nzbfilesystem.PartialContentError
		var corruptedErr *nzbfilesystem.CorruptedFileError

		if errors.As(err, &partialErr) {
			// Partial content - log and return the partial data with proper error
			slog.WarnContext(f.ctx, "Partial content due to missing articles",
				"bytes_read", partialErr.BytesRead,
				"total_expected", partialErr.TotalExpected)
			// Return the partial data but with a custom error indicating partial content
			return n, &HTTPError{
				StatusCode: http.StatusPartialContent,
				Message:    "Partial content available due to missing articles",
				Err:        err,
			}
		}

		if errors.As(err, &corruptedErr) {
			// Corrupted file - log and return 503
			slog.ErrorContext(f.ctx, "File corrupted due to missing articles",
				"total_expected", corruptedErr.TotalExpected)
			return n, &HTTPError{
				StatusCode: http.StatusServiceUnavailable,
				Message:    "File unavailable due to missing articles",
				Err:        err,
			}
		}
	}

	return n, err
}
