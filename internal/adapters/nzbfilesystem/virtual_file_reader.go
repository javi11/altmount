package nzbfilesystem

import (
	"errors"
	"fmt"
	"io"

	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/altmount/internal/nzb"
	"github.com/javi11/altmount/internal/usenet"
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
			// Check if this is an ArticleNotFoundError from usenet reader
			if articleErr, ok := err.(*usenet.ArticleNotFoundError); ok {
				// Update database status based on bytes read
				vf.updateFileStatusFromError(articleErr)

				// Create appropriate error for upper layers
				if articleErr.BytesRead > 0 || totalRead > 0 {
					// Some content was read - return partial content error
					return totalRead, &PartialContentError{
						BytesRead:     articleErr.BytesRead,
						TotalExpected: vf.virtualFile.Size,
						UnderlyingErr: articleErr,
					}
				} else {
					// No content read - return corrupted file error
					return totalRead, &CorruptedFileError{
						TotalExpected: vf.virtualFile.Size,
						UnderlyingErr: articleErr,
					}
				}
			}
			// Any other error should be returned as-is
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

	// Check if this file is extracted from a RAR archive
	isRarFile, err := vf.isExtractedFromRar()
	if err != nil {
		return 0, fmt.Errorf("failed to check if file is from RAR: %w", err)
	}

	if isRarFile {
		// For RAR files, use RAR content reader with seek support
		return vf.readAtFromRar(p, off)
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

	if vf.virtualFile.Encryption != nil {
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
			// Check if this is an ArticleNotFoundError from usenet reader
			if articleErr, ok := rerr.(*usenet.ArticleNotFoundError); ok {
				// Update database status based on bytes read
				vf.updateFileStatusFromError(articleErr)

				// Create appropriate error for upper layers
				if articleErr.BytesRead > 0 || int64(n) > 0 {
					// Some content was read - return partial content error
					return n, &PartialContentError{
						BytesRead:     articleErr.BytesRead,
						TotalExpected: vf.virtualFile.Size,
						UnderlyingErr: articleErr,
					}
				} else {
					// No content read - return corrupted file error
					return n, &CorruptedFileError{
						TotalExpected: vf.virtualFile.Size,
						UnderlyingErr: articleErr,
					}
				}
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

	// Check if this file is extracted from a RAR archive
	isRarFile, err := vf.isExtractedFromRar()
	if err != nil {
		return fmt.Errorf("failed to check if file is from RAR: %w", err)
	}

	if isRarFile {
		// For RAR files, create a RAR content reader instead of direct Usenet reader
		return vf.ensureRarReaderForPosition(position)
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
	if vf.virtualFile.Encryption != nil {
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

// updateFileStatusFromError updates the virtual file status in the database based on ArticleNotFoundError
func (vf *VirtualFile) updateFileStatusFromError(articleErr *usenet.ArticleNotFoundError) {
	if vf.db == nil || vf.virtualFile == nil {
		return // No database access or virtual file info
	}

	var status database.FileStatus
	if articleErr.BytesRead > 0 {
		// Some content was successfully read before error - mark as partial
		status = database.FileStatusPartial
	} else {
		// No content read - mark as corrupted/missing
		status = database.FileStatusCorrupted
	}

	// Update status in database - ignore errors as this is best-effort
	repo := database.NewRepository(vf.db.Connection())
	_ = repo.UpdateVirtualFileStatus(vf.virtualFile.ID, status)
}

// isExtractedFromRar checks if this virtual file was extracted from a RAR archive
func (vf *VirtualFile) isExtractedFromRar() (bool, error) {
	if vf.db == nil || vf.virtualFile == nil {
		return false, nil
	}

	repo := database.NewRepository(vf.db.Connection())
	metadata, err := repo.GetFileMetadata(vf.virtualFile.ID)
	if err != nil {
		// If metadata doesn't exist, it's not an error - just means it's not from RAR
		return false, nil
	}

	_, exists := metadata["extracted_from_rar"]
	return exists, nil
}

// ensureRarReaderForPosition creates a reader for RAR content at the specified position
func (vf *VirtualFile) ensureRarReaderForPosition(position int64) error {
	// Check if current reader can handle this position
	if vf.reader != nil {
		// If we already have a seeker, just seek to the position
		if seeker, ok := vf.reader.(io.Seeker); ok && position != vf.position {
			_, err := seeker.Seek(position, io.SeekStart)
			if err != nil {
				// Seeking failed, close and recreate reader
				_ = vf.reader.Close()
				vf.reader = nil
			} else {
				vf.position = position
				return nil
			}
		} else if position == vf.position {
			return nil
		} else {
			// Position changed and no seeking support, close current reader
			_ = vf.reader.Close()
			vf.reader = nil
		}
	}

	// Create RAR content reader with seek support
	rarReader, err := vf.createRarContentReader()
	if err != nil {
		return fmt.Errorf("failed to create RAR content reader: %w", err)
	}

	// Seek to the desired position if needed
	if position > 0 {
		_, err := rarReader.Seek(position, io.SeekStart)
		if err != nil {
			rarReader.Close()
			return fmt.Errorf("failed to seek in RAR content: %w", err)
		}
	}

	vf.reader = rarReader
	vf.position = position
	return nil
}

// readAtFromRar reads data from a specific offset in a RAR-extracted file
func (vf *VirtualFile) readAtFromRar(p []byte, off int64) (int, error) {
	// Create a new RAR content reader for this specific read operation
	rarReader, err := vf.createRarContentReader()
	if err != nil {
		return 0, fmt.Errorf("failed to create RAR content reader: %w", err)
	}
	defer rarReader.Close()

	// Seek to the desired offset using the seeker interface
	_, err = rarReader.Seek(off, io.SeekStart)
	if err != nil {
		return 0, fmt.Errorf("failed to seek in RAR content: %w", err)
	}

	// Read the requested data
	return io.ReadFull(rarReader, p)
}

// createRarContentReader creates a streaming reader with seek support for this RAR-extracted file
func (vf *VirtualFile) createRarContentReader() (nzb.RarContentReadSeeker, error) {
	if vf.db == nil || vf.virtualFile == nil || vf.nzbFile == nil {
		return nil, fmt.Errorf("missing database or file information")
	}

	// Get the RAR directory path from metadata
	repo := database.NewRepository(vf.db.Connection())
	metadata, err := repo.GetFileMetadata(vf.virtualFile.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get file metadata: %w", err)
	}

	rarDirPath, exists := metadata["extracted_from_rar"]
	if !exists || rarDirPath == "" {
		return nil, fmt.Errorf("file is not extracted from RAR or missing RAR directory metadata")
	}

	// Get the parent RAR directory to find the RAR content
	// First, find the RAR directory virtual file
	rarDirFile, err := repo.GetVirtualFileByPath(rarDirPath)
	if err != nil || rarDirFile == nil {
		return nil, fmt.Errorf("failed to find RAR directory: %s", rarDirPath)
	}

	// Get the RAR content information for this directory
	rarContents, err := repo.GetRarContentsByVirtualFileID(rarDirFile.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get RAR contents for directory ID %d: %w", rarDirFile.ID, err)
	}

	if len(rarContents) == 0 {
		return nil, fmt.Errorf("no RAR contents found for directory %s", rarDirPath)
	}

	// Find RAR files associated with this NZB
	rarFiles, err := vf.getRarFilesFromNzb()
	if err != nil {
		return nil, fmt.Errorf("failed to get RAR files from NZB: %w", err)
	}

	if len(rarFiles) == 0 {
		return nil, fmt.Errorf("no RAR files found in NZB")
	}

	// Use the RAR handler to create a content reader for this specific file
	rarHandler := nzb.NewRarHandler(vf.cp, vf.maxWorkers)
	targetPath := vf.virtualFile.Filename // The filename within the RAR archive

	return rarHandler.CreateRarContentReader(vf.ctx, vf.nzbFile, rarFiles, targetPath)
}

// getRarFilesFromNzb extracts RAR file information from the database
func (vf *VirtualFile) getRarFilesFromNzb() ([]nzb.ParsedFile, error) {
	// NEW IMPLEMENTATION: Query the database for RAR part NZB files
	// With the enhanced database schema, each RAR part is stored as a separate NZB record
	// with its proper filename and corresponding segments
	
	if vf.db == nil || vf.nzbFile == nil {
		return nil, fmt.Errorf("missing database or NZB file information")
	}

	repo := database.NewRepository(vf.db.Connection())
	
	// Get all RAR part NZB files for this parent NZB
	rarPartNzbFiles, err := repo.GetRarPartNzbFiles(vf.nzbFile.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get RAR part NZB files: %w", err)
	}

	if len(rarPartNzbFiles) == 0 {
		return nil, fmt.Errorf("no RAR part files found for NZB ID %d", vf.nzbFile.ID)
	}

	// Convert NZB files to ParsedFiles for the RAR handler
	var rarFiles []nzb.ParsedFile
	for _, nzbFile := range rarPartNzbFiles {
		rarFile := nzb.ParsedFile{
			Filename:     nzbFile.Filename,    // Actual RAR part filename (movie.rar, movie.r00, etc.)
			Size:         nzbFile.Size,        // Size of this specific part
			Segments:     nzbFile.SegmentsData, // Only segments for this RAR part
			IsRarArchive: true,                // All are RAR archive parts
		}
		rarFiles = append(rarFiles, rarFile)
	}

	return rarFiles, nil
}
