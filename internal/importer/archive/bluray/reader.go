package bluray

import (
	"fmt"
	"io"
	"io/fs"
)

// Reader provides read access to a specific .m2ts stream from a Blu-ray disc structure
// It sits on top of a filesystem that provides access to the BDMV files
type Reader struct {
	fileSystem    fs.FS     // Filesystem providing access to BDMV structure
	targetM2TS    string    // Target .m2ts file path (e.g., "BDMV/STREAM/00000.m2ts")
	file          fs.File   // Open file handle
	currentOffset int64     // Current read position
	fileSize      int64     // Total file size
}

// NewReader creates a new Blu-ray reader for extracting a specific .m2ts stream
func NewReader(fileSystem fs.FS, targetM2TSPath string) (*Reader, error) {
	if fileSystem == nil {
		return nil, fmt.Errorf("filesystem is nil")
	}

	if targetM2TSPath == "" {
		return nil, fmt.Errorf("target m2ts path is empty")
	}

	// Get file info to validate and get size
	fileInfo, err := fs.Stat(fileSystem, targetM2TSPath)
	if err != nil {
		return nil, fmt.Errorf("failed to stat target m2ts file: %w", err)
	}

	reader := &Reader{
		fileSystem:    fileSystem,
		targetM2TS:    targetM2TSPath,
		currentOffset: 0,
		fileSize:      fileInfo.Size(),
	}

	return reader, nil
}

// Open opens the target .m2ts file for reading
func (r *Reader) Open() error {
	if r.file != nil {
		return fmt.Errorf("file already open")
	}

	file, err := r.fileSystem.Open(r.targetM2TS)
	if err != nil {
		return fmt.Errorf("failed to open target m2ts file: %w", err)
	}

	r.file = file
	r.currentOffset = 0

	return nil
}

// Read implements io.Reader
func (r *Reader) Read(p []byte) (int, error) {
	if r.file == nil {
		if err := r.Open(); err != nil {
			return 0, err
		}
	}

	n, err := r.file.Read(p)
	r.currentOffset += int64(n)
	return n, err
}

// Seek implements io.Seeker
func (r *Reader) Seek(offset int64, whence int) (int64, error) {
	if r.file == nil {
		if err := r.Open(); err != nil {
			return 0, err
		}
	}

	// Check if the underlying file supports seeking
	seeker, ok := r.file.(io.Seeker)
	if !ok {
		return 0, fmt.Errorf("underlying file does not support seeking")
	}

	newOffset, err := seeker.Seek(offset, whence)
	if err != nil {
		return 0, err
	}

	r.currentOffset = newOffset
	return newOffset, nil
}

// Close implements io.Closer
func (r *Reader) Close() error {
	if r.file != nil {
		err := r.file.Close()
		r.file = nil
		r.currentOffset = 0
		return err
	}
	return nil
}

// Size returns the size of the .m2ts file
func (r *Reader) Size() int64 {
	return r.fileSize
}

// CurrentOffset returns the current read position
func (r *Reader) CurrentOffset() int64 {
	return r.currentOffset
}

// TargetPath returns the path of the target .m2ts file
func (r *Reader) TargetPath() string {
	return r.targetM2TS
}

// Ensure Reader implements io.ReadSeekCloser
var _ io.ReadSeekCloser = (*Reader)(nil)
