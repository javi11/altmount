package nzbfilesystem

import (
	"errors"
	"fmt"
	"io"

	"github.com/javi11/altmount/internal/encryption"
)

// Read reads data from the virtual file using lazy reader creation with proper chunk continuation
func (vf *VirtualFile) Read(p []byte) (int, error) {
	vf.mu.Lock()
	defer vf.mu.Unlock()

	if len(p) == 0 {
		return 0, nil
	}

	if vf.virtualFile == nil {
		return 0, ErrVirtualFileNotInit
	}

	if vf.virtualFile.IsDirectory {
		return 0, ErrCannotReadDirectory
	}

	if vf.nzbFile == nil {
		return 0, ErrNoNzbData
	}

	totalRead := 0
	buf := p

	for totalRead < len(p) && vf.position < vf.virtualFile.Size {
		// Ensure we have a reader for the current position
		if err := vf.ensureReaderForPosition(vf.position); err != nil {
			if errors.Is(err, io.EOF) {
				if totalRead > 0 {
					return totalRead, nil
				}
				return 0, io.EOF
			}
			return totalRead, err
		}

		// Read from current reader
		n, err := vf.reader.Read(buf[totalRead:])
		totalRead += n
		vf.position += int64(n)

		// Handle different error conditions
		if err == io.EOF {
			if vf.position < vf.virtualFile.Size {
				// We've reached the end of this chunk but there's more file to read
				// Close current reader so next iteration will create a new reader for next chunk
				_ = vf.reader.Close()
				vf.reader = nil
				// Continue reading to fill the buffer with the next chunk
				continue
			} else {
				// We've reached the actual end of the file
				if totalRead > 0 {
					return totalRead, nil
				}
				return 0, io.EOF
			}
		} else if err != nil {
			// Any other error should be returned
			return totalRead, err
		}

		// If no error, we successfully read some data
		// Continue the loop to try to fill the rest of the buffer if needed
	}

	// If we've read all available data or filled the buffer
	if totalRead > 0 {
		return totalRead, nil
	}

	// Should not reach here under normal circumstances
	return 0, io.EOF
}

// ReadAt reads data at a specific offset
func (vf *VirtualFile) ReadAt(p []byte, off int64) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if vf.virtualFile.IsDirectory {
		return 0, ErrCannotReadDirectory
	}
	if vf.nzbFile == nil {
		return 0, ErrNoNzbData
	}
	if off < 0 {
		return 0, ErrNegativeOffset
	}
	if off >= vf.virtualFile.Size {
		return 0, io.EOF
	}

	// Limit read length to available bytes
	maxLen := int64(len(p))
	remain := vf.virtualFile.Size - off
	if maxLen > remain {
		maxLen = remain
	}

	// Early return for zero-length reads to prevent unnecessary reader creation
	if maxLen <= 0 {
		return 0, nil
	}

	end := off + maxLen - 1 // inclusive

	// Get HTTP range constraints to optimize reader creation
	rangeStart, rangeEnd, hasRange := vf.getRequestRange()
	if hasRange {
		// Validate that the requested read is within the HTTP range
		if off < rangeStart || off > rangeEnd {
			return 0, fmt.Errorf(ErrMsgReadOutsideRange, off, rangeStart, rangeEnd)
		}
		// Constrain end to not exceed the HTTP range
		if end > rangeEnd {
			end = rangeEnd
			maxLen = end - off + 1
		}
	}

	// Create reader with optimized range
	var reader io.ReadCloser
	var err error

	if vf.virtualFile.Encryption != nil && *vf.virtualFile.Encryption == string(encryption.RCloneCipherType) {
		reader, err = vf.wrapWithEncryption(off, end)
		if err != nil {
			return 0, fmt.Errorf(ErrMsgFailedWrapEncryption, err)
		}
	} else {
		reader, err = vf.createUsenetReader(vf.ctx, off, end)
		if err != nil {
			return 0, fmt.Errorf(ErrMsgFailedCreateUsenetReader, err)
		}
	}

	// Ensure reader is closed even if we panic or return early
	defer func() {
		if closeErr := reader.Close(); closeErr != nil {
			// Log error but don't override return values
		}
	}()

	buf := p[:maxLen]
	n := 0
	for n < len(buf) {
		nn, rerr := reader.Read(buf[n:])
		n += nn
		if rerr != nil {
			if errors.Is(rerr, io.EOF) {
				break
			}
			return n, rerr
		}
	}

	if int64(n) < int64(len(p)) {
		return n, io.EOF
	}

	return n, nil
}

// Seek sets the file position and invalidates reader if position changes significantly
func (vf *VirtualFile) Seek(offset int64, whence int) (int64, error) {
	vf.mu.Lock()
	defer vf.mu.Unlock()
	var abs int64

	switch whence {
	case io.SeekStart: // Relative to the origin of the file
		abs = offset
	case io.SeekCurrent: // Relative to the current offset
		abs = vf.position + offset
	case io.SeekEnd: // Relative to the end
		abs = int64(vf.virtualFile.Size) + offset
	default:
		return 0, ErrInvalidWhence
	}

	if abs < 0 {
		return 0, ErrSeekNegative
	}

	if abs > int64(vf.virtualFile.Size) {
		return 0, ErrSeekTooFar
	}

	// If we're seeking to a position far from current reader range, close the reader
	// This prevents memory leaks from keeping large readers open for distant positions
	if vf.reader != nil {
		// Calculate if the new position is outside a reasonable range from current position
		// Use SeekThreshold - if seeking more than threshold away, close reader
		distance := abs - vf.position
		if distance < 0 {
			distance = -distance
		}

		if distance > SeekThreshold {
			_ = vf.reader.Close()
			vf.reader = nil
		}
	}

	vf.position = abs
	return abs, nil
}

// ensureReaderForPosition creates or reuses a reader for the given position with smart chunking
// This implements lazy loading to avoid memory leaks from pre-caching entire files
func (vf *VirtualFile) ensureReaderForPosition(position int64) error {
	if vf.nzbFile == nil {
		return ErrNoNzbData
	}

	if vf.cp == nil {
		return ErrNoUsenetPool
	}

	if position < 0 {
		position = 0
	}

	if position >= vf.virtualFile.Size {
		return io.EOF
	}

	// Check if current reader can handle this position
	if vf.reader != nil {
		// If we have a reader and the position matches our current position, we're good
		if position == vf.position {
			return nil
		}
		// Position changed, close current reader
		_ = vf.reader.Close()
		vf.reader = nil
	}

	// Calculate smart range based on HTTP Range header and memory constraints
	start, end := vf.calculateSmartRange(position)

	// Create reader for the calculated range
	if vf.virtualFile.Encryption != nil && *vf.virtualFile.Encryption == string(encryption.RCloneCipherType) {
		// Wrap the usenet reader with rclone decryption
		decryptedReader, err := vf.wrapWithEncryption(start, end)
		if err != nil {
			return fmt.Errorf(ErrMsgFailedWrapEncryption, err)
		}

		vf.reader = decryptedReader
	} else {
		ur, err := vf.createUsenetReader(vf.ctx, start, end)
		if err != nil {
			return err
		}

		vf.reader = ur
	}

	// Set position to the start of our new reader range
	vf.position = start
	return nil
}
