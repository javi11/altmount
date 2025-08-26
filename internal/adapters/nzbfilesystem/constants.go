package nzbfilesystem

import (
	"errors"
	"fmt"
)

// File system constants
const (
	// RootPath represents the root directory path
	RootPath = "/"
)

// Error constants
var (
	ErrInvalidWhence = errors.New("seek: invalid whence")
	ErrSeekNegative  = errors.New("seek: negative position")
	ErrSeekTooFar    = errors.New("seek: too far")
)

// Article availability error types

// PartialContentError represents an error where some articles are missing but some content was read
type PartialContentError struct {
	BytesRead     int64
	TotalExpected int64
	UnderlyingErr error
}

func (e *PartialContentError) Error() string {
	return fmt.Sprintf("partial content: read %d/%d bytes, underlying error: %v",
		e.BytesRead, e.TotalExpected, e.UnderlyingErr)
}

func (e *PartialContentError) Unwrap() error {
	return e.UnderlyingErr
}

// CorruptedFileError represents an error where no articles could be read (complete failure)
type CorruptedFileError struct {
	TotalExpected int64
	UnderlyingErr error
}

func (e *CorruptedFileError) Error() string {
	return fmt.Sprintf("corrupted file: no content available from %d expected bytes, underlying error: %v",
		e.TotalExpected, e.UnderlyingErr)
}

func (e *CorruptedFileError) Unwrap() error {
	return e.UnderlyingErr
}

// Error message constants
var (
	ErrCannotRemoveRoot     = errors.New("cannot remove root directory")
	ErrCannotRenameRoot     = errors.New("cannot rename root directory")
	ErrCannotRenameToRoot   = errors.New("cannot rename to root directory")
	ErrDestinationExists    = errors.New("destination already exists")
	ErrNotDirectory         = errors.New("not a directory")
	ErrCannotReadDirectory  = errors.New("cannot read from directory")
	ErrNegativeOffset       = errors.New("negative offset")
	ErrVirtualFileNotInit   = errors.New("virtual file not initialized")
	ErrNoNzbData            = errors.New("no NZB data available for file")
	ErrNoUsenetPool         = errors.New("usenet connection pool not configured")
	ErrNoCipherConfig       = errors.New("no cipher configured for encryption")
	ErrNoEncryptionParams   = errors.New("no NZB data available for encryption parameters")
	ErrTruncateNotSupported = errors.New("truncate not supported for virtual files")
	ErrWriteNotSupported    = errors.New("write not supported for virtual files")
	ErrFailedListDirectory  = errors.New("failed to list directory contents")
	ErrFileIsCorrupted      = errors.New("file is corrupted, there are some missing segments")
)

// Database operation error message templates
const (
	ErrMsgFailedQueryVirtualFile    = "failed to query virtual file: %w"
	ErrMsgFailedDeleteVirtualFile   = "failed to delete virtual file: %w"
	ErrMsgFailedCheckDestination    = "failed to check destination: %w"
	ErrMsgFailedFindParent          = "failed to find parent directory: %w"
	ErrMsgFailedMoveFile            = "failed to move file: %w"
	ErrMsgFailedUpdateFilename      = "failed to update filename: %w"
	ErrMsgFailedGetDescendants      = "failed to get descendants: %w"
	ErrMsgFailedUpdateDescPath      = "failed to update descendant path: %w"
	ErrMsgFailedListDirectory       = "failed to list directory contents: %w"
	ErrMsgFailedCreateUsenetReader  = "failed to create usenet reader: %w"
	ErrMsgFailedCreateDecryptReader = "failed to create decrypt reader: %w"
	ErrMsgFailedWrapEncryption      = "failed to wrap reader with encryption: %w"
)

// Range validation error message templates
const (
	ErrMsgReadOutsideRange = "read offset %d is outside requested range %d-%d"
)
