package nzbfilesystem

import "errors"

// Chunk size constants for memory optimization
const (
	// MaxChunkSize defines maximum chunk size to prevent memory explosion (100MB limit)
	MaxChunkSize = 100 * 1024 * 1024 // 100MB

	// Small file threshold - files under this size are read entirely
	SmallFileThreshold = 10 * 1024 * 1024 // 10MB

	// Medium file threshold - files under this size use medium chunks
	MediumFileThreshold = 100 * 1024 * 1024 // 100MB

	// Large file threshold - files under this size use large chunks
	LargeFileThreshold = 1024 * 1024 * 1024 // 1GB

	// SmallFileChunkSize - chunk size for small files (entire file)
	SmallFileChunkSize = SmallFileThreshold

	// MediumFileChunkSize - chunk size for medium files
	MediumFileChunkSize = 10 * 1024 * 1024 // 10MB

	// LargeFileChunkSize - chunk size for large files
	LargeFileChunkSize = 25 * 1024 * 1024 // 25MB

	// VeryLargeFileChunkSize - chunk size for very large files (>=1GB)
	VeryLargeFileChunkSize = 50 * 1024 * 1024 // 50MB

	// SeekThreshold - if seeking more than this distance, close reader
	SeekThreshold = 1024 * 1024 // 1MB
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
