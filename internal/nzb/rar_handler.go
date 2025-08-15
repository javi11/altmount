package nzb

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/altmount/internal/usenet"
	"github.com/javi11/nntppool"
	"github.com/javi11/nzbparser"
	"github.com/javi11/rardecode/v2"
)

// RarHandler handles RAR archive analysis and content extraction
type RarHandler struct {
	log        *slog.Logger
	cp         nntppool.UsenetConnectionPool
	maxWorkers int
}

// NewRarHandler creates a new RAR handler
func NewRarHandler(cp nntppool.UsenetConnectionPool, maxWorkers int) *RarHandler {
	return &RarHandler{
		log:        slog.Default().With("component", "rar-handler"),
		cp:         cp,
		maxWorkers: maxWorkers,
	}
}

// AnalyzeRarContentFromNzb analyzes a RAR archive directly from NZB data without downloading
// This implementation uses rardecode.OpenReader with a virtual filesystem to stream RAR data from Usenet
// The virtualFile parameter is the directory that will contain the extracted files (not the RAR file itself)
func (rh *RarHandler) AnalyzeRarContentFromNzb(ctx context.Context, nzbFile *database.NzbFile, rarFiles []ParsedFile, virtualDir *database.VirtualFile) ([]database.RarContent, error) {
	// Create Usenet filesystem for RAR access - this is the key component that enables
	// rardecode.OpenReader to read RAR parts directly from Usenet without downloading
	ufs := NewUsenetFileSystem(ctx, rh.cp, nzbFile, rarFiles, rh.maxWorkers)

	// Get sorted RAR files for proper multi-part reading
	rarFileNames := ufs.GetRarFiles()
	if len(rarFileNames) == 0 {
		return nil, fmt.Errorf("no RAR files found in the archive")
	}

	// Start with the first RAR file (usually .rar or .part001.rar)
	mainRarFile := rarFileNames[0]

	rh.log.Info("Starting RAR analysis via Usenet streaming",
		"main_file", mainRarFile,
		"total_parts", len(rarFileNames),
		"nzb_segments", len(nzbFile.SegmentsData))

	// This is where the magic happens: rardecode.OpenReader will call ufs.Open() to access
	// RAR part files, which in turn creates UsenetFile instances that stream data directly
	// from Usenet using createUsenetReader() - no local file storage required!
	rarReader, err := rardecode.OpenReader(mainRarFile, rardecode.FileSystem(ufs))
	if err != nil {
		return nil, fmt.Errorf("failed to open RAR archive %s via Usenet filesystem: %w", mainRarFile, err)
	}
	defer rarReader.Close()

	rh.log.Debug("Successfully opened RAR archive via Usenet streaming", "main_file", mainRarFile)

	// Extract file entries from the RAR archive
	// Each call to rarReader.Next() may trigger reading from different RAR parts,
	// all streamed transparently from Usenet via our virtual filesystem
	var rarContents []database.RarContent
	fileCount := 0
	for {
		header, err := rarReader.Next()
		if err == io.EOF || (err != nil &&
			(strings.Contains(err.Error(), "rardecode: bad header crc") ||
				strings.Contains(err.Error(), "RAR signature not found"))) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read RAR header from Usenet stream: %w", err)
		}

		// Skip directories - focus on files
		if header.IsDir {
			rh.log.Debug("Skipping directory entry", "path", header.Name)
			continue
		}

		fileCount++
		rh.log.Debug("Streaming RAR file header from Usenet",
			"file", header.Name,
			"size", header.UnPackedSize,
			"offset", header.Offset,
			"file_num", fileCount)

		// Extract CRC32 if available - note: rardecode v2 may not expose CRC32 directly
		var crc32Ptr *string
		// CRC32 field may not be available in this version of rardecode

		// Add extended metadata from the RAR header
		var modTimePtr *time.Time
		if !header.ModificationTime.IsZero() {
			modTimePtr = &header.ModificationTime
		}

		rarContent := database.RarContent{
			VirtualFileID: virtualDir.ID,
			InternalPath:  filepath.Clean(header.Name),
			Filename:      filepath.Base(header.Name),
			Size:          header.UnPackedSize,
			FileOffset:    &header.Offset,
			CRC32:         crc32Ptr,
			IsDirectory:   header.IsDir,
			ModTime:       modTimePtr,
			// Note: FileOffset and RarPartIndex could be populated if needed for advanced streaming
		}

		rarContents = append(rarContents, rarContent)

		rh.log.Debug("Successfully streamed RAR file metadata from Usenet",
			"path", rarContent.InternalPath,
			"size", rarContent.Size,
			"compressed", rarContent.CompressedSize,
		)
	}

	rh.log.Info("Successfully analyzed RAR content via Usenet streaming",
		"archive_dir", virtualDir.VirtualPath,
		"files_found", len(rarContents),
		"rar_parts", len(rarFileNames),
		"total_bytes_in_rar", func() int64 {
			var total int64
			for _, rc := range rarContents {
				total += rc.Size
			}
			return total
		}(),
	)

	return rarContents, nil
}

// AnalyzeRarContent analyzes a RAR archive to extract its internal file structure (legacy method)
// This implementation uses the rardecode library to parse RAR headers and extract file lists
func (rh *RarHandler) AnalyzeRarContent(r io.Reader, virtualFile *database.VirtualFile, nzbFile *database.NzbFile) ([]database.RarContent, error) {
	// Extract file list from RAR headers
	fileEntries, err := rh.ExtractRarFileList(r)
	if err != nil {
		return nil, fmt.Errorf("failed to extract RAR file list: %w", err)
	}

	// Convert to database format
	rarContents := make([]database.RarContent, 0, len(fileEntries))
	for _, entry := range fileEntries {
		// Skip directories for now - focus on files
		if entry.IsDirectory {
			continue
		}

		var crc32Ptr *string
		if entry.CRC32 != "" {
			crc32Ptr = &entry.CRC32
		}

		rarContent := database.RarContent{
			VirtualFileID:  virtualFile.ID,
			InternalPath:   entry.Path,
			Filename:       entry.Filename,
			Size:           entry.Size,
			CompressedSize: entry.CompressedSize,
			CRC32:          crc32Ptr,
		}
		rarContents = append(rarContents, rarContent)
	}

	rh.log.Info("Successfully analyzed RAR content",
		"archive", virtualFile.VirtualPath,
		"files_found", len(rarContents),
	)

	return rarContents, nil
}

// ExtractRarFileList extracts the file list from RAR headers without full decompression
func (rh *RarHandler) ExtractRarFileList(reader io.Reader) ([]RarFileEntry, error) {
	rarReader, err := rardecode.NewReader(reader, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create RAR reader: %w", err)
	}

	var entries []RarFileEntry
	for {
		header, err := rarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read RAR header: %w", err)
		}

		// Extract file information from header
		entry := RarFileEntry{
			Path:           filepath.Clean(header.Name),
			Filename:       filepath.Base(header.Name),
			Size:           header.UnPackedSize,
			CompressedSize: header.PackedSize,
			CRC32:          "", // CRC not available in rardecode v2
			IsDirectory:    header.IsDir,
			ModTime:        header.ModificationTime,
			Attributes:     header.HostOS,
		}

		entries = append(entries, entry)
		rh.log.Debug("Found RAR entry",
			"path", entry.Path,
			"size", entry.Size,
			"compressed_size", entry.CompressedSize,
			"is_dir", entry.IsDirectory,
		)
	}

	return entries, nil
}

// IsValidRarArchive checks if the given data represents a valid RAR archive
func (rh *RarHandler) IsValidRarArchive(data []byte) bool {
	// Check for RAR signature
	if len(data) < 7 {
		return false
	}

	// RAR signature: "Rar!" followed by 0x1A 0x07 0x00 or 0x1A 0x07 0x01
	rarSignature := []byte{0x52, 0x61, 0x72, 0x21, 0x1A, 0x07}

	for i, b := range rarSignature {
		if data[i] != b {
			return false
		}
	}

	return true
}

// GetRarVersion determines the RAR archive version
func (rh *RarHandler) GetRarVersion(data []byte) (int, error) {
	if !rh.IsValidRarArchive(data) {
		return 0, fmt.Errorf("not a valid RAR archive")
	}

	if len(data) < 7 {
		return 0, fmt.Errorf("insufficient data to determine RAR version")
	}

	// RAR version is in the 7th byte
	version := int(data[6])
	return version, nil
}

// RarContentReadSeeker combines io.ReadCloser and io.Seeker for RAR content access
type RarContentReadSeeker interface {
	io.ReadCloser
	io.Seeker
}

// Compile-time interface check
var _ RarContentReadSeeker = (*rarContentReader)(nil)

// CreateRarContentReader creates a streaming reader with seek support for a specific file within a RAR archive
// This method demonstrates the full streaming capability: it opens the RAR archive via
// the Usenet filesystem and provides a reader/seeker for a specific file within the archive,
// all without downloading the complete RAR files to disk.
func (rh *RarHandler) CreateRarContentReader(ctx context.Context, nzbFile *database.NzbFile, rarFiles []ParsedFile, targetPath string) (RarContentReadSeeker, error) {
	// Create Usenet filesystem for RAR access - this enables rardecode.OpenReader
	// to read RAR parts directly from Usenet via createUsenetReader()
	ufs := NewUsenetFileSystem(ctx, rh.cp, nzbFile, rarFiles, rh.maxWorkers)

	// Get sorted RAR files for proper multi-part reading
	rarFileNames := ufs.GetRarFiles()
	if len(rarFileNames) == 0 {
		return nil, fmt.Errorf("no RAR files found in the archive")
	}

	// Start with the first RAR file (usually .rar or .part001.rar)
	mainRarFile := rarFileNames[0]

	// Open the RAR archive using the virtual filesystem - this is where rardecode.OpenReader
	// will call ufs.Open() to access RAR part files, which stream data from Usenet
	rarReader, err := rardecode.OpenFS(mainRarFile,
		rardecode.FileSystem(ufs),
		rardecode.SkipCheck,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to open RAR archive %s via Usenet filesystem: %w", mainRarFile, err)
	}

	f, err := rarReader.Open(targetPath)
	if err == io.EOF {
		return nil, fmt.Errorf("file not found in RAR archive: %s", targetPath)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read RAR header from Usenet stream: %w", err)
	}

	rh.log.Info("Found target file in RAR archive",
		"file", targetPath)

	return &rarContentReader{
		rarReader: f,
	}, nil
}

// CreateDirectRarContentReader creates a direct streaming reader that bypasses rardecode for content reading
// This method provides optimal performance by using the file offset from RAR analysis to stream directly from Usenet
func (rh *RarHandler) CreateDirectRarContentReader(ctx context.Context, nzbFile *database.NzbFile, rarFiles []ParsedFile, targetFilename string, fileOffset, fileSize int64) (RarContentReadSeeker, error) {
	rh.log.Debug("Creating direct RAR content reader",
		"target_file", targetFilename,
		"file_offset", fileOffset,
		"file_size", fileSize,
		"rar_parts", len(rarFiles))

	return &DirectRarContentReader{
		ctx:            ctx,
		cp:             rh.cp,
		nzbFile:        nzbFile,
		rarFiles:       rarFiles,
		targetFilename: targetFilename,
		fileOffset:     fileOffset,
		fileSize:       fileSize,
		currentPos:     0,
		maxWorkers:     rh.maxWorkers,
		log:            rh.log.With("target_file", targetFilename),
		partBoundaries: nil, // Will be calculated on first use
		currentReader:  nil,
	}, nil
}

// rarContentReader wraps a rardecode reader for a specific file with seek support
// Implements RarContentReadSeeker interface for efficient RAR content access
type rarContentReader struct {
	rarReader fs.File
}

// DirectRarContentReader provides direct Usenet streaming for RAR content without rardecode overhead
// This implementation uses the file offset from RAR analysis to stream directly from Usenet
type DirectRarContentReader struct {
	ctx            context.Context
	cp             nntppool.UsenetConnectionPool
	nzbFile        *database.NzbFile
	rarFiles       []ParsedFile
	targetFilename string
	fileOffset     int64  // Starting offset of the file within the RAR stream
	fileSize       int64  // Size of the target file
	currentPos     int64  // Current reading position within the file
	maxWorkers     int
	log            *slog.Logger
	
	// RAR part information for multi-volume handling
	partBoundaries []RarPartBoundary
	currentReader  io.ReadCloser // Current Usenet reader
}

// RarPartBoundary represents the byte boundaries of each RAR part
type RarPartBoundary struct {
	PartIndex   int    // Index in the rarFiles array
	Filename    string // RAR part filename (e.g., movie.part001.rar)
	StartOffset int64  // Starting byte offset of this part in the combined stream
	EndOffset   int64  // Ending byte offset of this part in the combined stream
	Size        int64  // Size of this RAR part
}

// Compile-time interface check for DirectRarContentReader
var _ RarContentReadSeeker = (*DirectRarContentReader)(nil)

// Read implements io.Reader for DirectRarContentReader
func (drcr *DirectRarContentReader) Read(p []byte) (int, error) {
	if drcr.currentPos >= drcr.fileSize {
		return 0, io.EOF
	}
	
	// Calculate how much we can read without exceeding the file size
	remaining := drcr.fileSize - drcr.currentPos
	if int64(len(p)) > remaining {
		p = p[:remaining]
	}
	
	totalRead := 0
	for totalRead < len(p) && drcr.currentPos < drcr.fileSize {
		// Ensure we have a reader for the current position
		if err := drcr.ensureReaderForPosition(); err != nil {
			if totalRead > 0 {
				// Return partial read if we've read some data
				return totalRead, nil
			}
			return 0, err
		}
		
		// Try to read from the current reader
		n, err := drcr.currentReader.Read(p[totalRead:])
		totalRead += n
		drcr.currentPos += int64(n)
		
		if err == io.EOF {
			// Current reader reached EOF, check if we need to continue from next part
			if drcr.currentPos < drcr.fileSize {
				// File continues in next RAR part, transition to next part
				drcr.log.Debug("Current RAR part exhausted, transitioning to next part",
					"current_pos", drcr.currentPos,
					"file_size", drcr.fileSize,
					"bytes_read", totalRead)
				
				// Close current reader and clear it to force creation of new reader for next part
				if err := drcr.closeCurrentReader(); err != nil {
					drcr.log.Debug("Error closing reader during part transition", "error", err)
					// Continue anyway - we want to try the next part
				}
				
				// Continue the loop to read from next part
				continue
			} else {
				// We've reached the end of the file
				if totalRead > 0 {
					// Return the data we've read
					return totalRead, nil
				}
				return 0, io.EOF
			}
		} else if err != nil {
			// Any other error
			if totalRead > 0 {
				// Return partial read if we've read some data
				return totalRead, nil
			}
			return 0, err
		}
		
		// If we filled the buffer or read all data, break
		if totalRead == len(p) {
			break
		}
	}
	
	return totalRead, nil
}

// Seek implements io.Seeker for DirectRarContentReader
func (drcr *DirectRarContentReader) Seek(offset int64, whence int) (int64, error) {
	var newPos int64
	
	switch whence {
	case io.SeekStart:
		newPos = offset
	case io.SeekCurrent:
		newPos = drcr.currentPos + offset
	case io.SeekEnd:
		newPos = drcr.fileSize + offset
	default:
		return 0, fmt.Errorf("invalid whence value: %d", whence)
	}
	
	if newPos < 0 {
		return 0, fmt.Errorf("negative seek position: %d", newPos)
	}
	
	if newPos > drcr.fileSize {
		return 0, fmt.Errorf("seek position %d exceeds file size %d", newPos, drcr.fileSize)
	}
	
	// Update position first
	oldPos := drcr.currentPos
	drcr.currentPos = newPos
	
	// Close current reader as position changed significantly
	if err := drcr.closeCurrentReader(); err != nil {
		drcr.log.Debug("Error closing reader during seek",
			"old_pos", oldPos,
			"new_pos", newPos,
			"error", err)
		// Continue anyway - seeking should still work
	}
	
	drcr.log.Debug("Seek completed across RAR parts",
		"old_pos", oldPos,
		"new_pos", newPos,
		"file_size", drcr.fileSize)
	
	return newPos, nil
}

// Close implements io.Closer for DirectRarContentReader
func (drcr *DirectRarContentReader) Close() error {
	return drcr.closeCurrentReader()
}

// closeCurrentReader safely closes the current reader with proper error handling
func (drcr *DirectRarContentReader) closeCurrentReader() error {
	if drcr.currentReader != nil {
		drcr.log.Debug("Closing current Usenet reader",
			"current_pos", drcr.currentPos,
			"file_size", drcr.fileSize)
		
		err := drcr.currentReader.Close()
		drcr.currentReader = nil
		
		if err != nil {
			drcr.log.Debug("Error closing Usenet reader", "error", err)
			return err
		}
	}
	return nil
}

// calculatePartBoundaries calculates the byte boundaries for each RAR part
// This enables efficient multi-volume handling by knowing exactly which part contains any given byte offset
func (drcr *DirectRarContentReader) calculatePartBoundaries() error {
	if len(drcr.partBoundaries) > 0 {
		return nil // Already calculated
	}
	
	drcr.partBoundaries = make([]RarPartBoundary, len(drcr.rarFiles))
	var cumulativeOffset int64 = 0
	
	for i, rarFile := range drcr.rarFiles {
		boundary := RarPartBoundary{
			PartIndex:   i,
			Filename:    rarFile.Filename,
			StartOffset: cumulativeOffset,
			Size:        rarFile.Size,
			EndOffset:   cumulativeOffset + rarFile.Size - 1,
		}
		
		drcr.partBoundaries[i] = boundary
		cumulativeOffset += rarFile.Size
		
		drcr.log.Debug("Calculated RAR part boundary",
			"part", i,
			"filename", rarFile.Filename,
			"start_offset", boundary.StartOffset,
			"end_offset", boundary.EndOffset,
			"size", boundary.Size)
	}
	
	return nil
}

// findPartForOffset determines which RAR part contains the specified byte offset
func (drcr *DirectRarContentReader) findPartForOffset(offset int64) (*RarPartBoundary, error) {
	if err := drcr.calculatePartBoundaries(); err != nil {
		return nil, err
	}
	
	for i := range drcr.partBoundaries {
		part := &drcr.partBoundaries[i]
		if offset >= part.StartOffset && offset <= part.EndOffset {
			return part, nil
		}
	}
	
	return nil, fmt.Errorf("offset %d not found in any RAR part", offset)
}

// ensureReaderForPosition ensures we have a Usenet reader positioned correctly for the current file position
func (drcr *DirectRarContentReader) ensureReaderForPosition() error {
	if drcr.currentReader != nil {
		return nil // Already have a reader
	}
	
	// Calculate absolute offset in the RAR stream
	absoluteOffset := drcr.fileOffset + drcr.currentPos
	
	// Find which RAR part contains this offset
	part, err := drcr.findPartForOffset(absoluteOffset)
	if err != nil {
		return fmt.Errorf("failed to find RAR part for offset %d: %w", absoluteOffset, err)
	}
	
	// Calculate offset within this specific RAR part
	offsetInPart := absoluteOffset - part.StartOffset
	
	drcr.log.Debug("Creating Usenet reader for RAR content",
		"target_file", drcr.targetFilename,
		"file_offset", drcr.fileOffset,
		"current_pos", drcr.currentPos,
		"absolute_offset", absoluteOffset,
		"rar_part", part.Filename,
		"offset_in_part", offsetInPart)
	
	// Create Usenet reader for this specific RAR part starting at the calculated offset
	reader, err := drcr.createUsenetReaderForPart(part.PartIndex, offsetInPart)
	if err != nil {
		return fmt.Errorf("failed to create Usenet reader for part %s at offset %d: %w", 
			part.Filename, offsetInPart, err)
	}
	
	drcr.currentReader = reader
	return nil
}

// shouldTransitionToNextPart checks if we should transition to the next RAR part
func (drcr *DirectRarContentReader) shouldTransitionToNextPart() (bool, error) {
	if drcr.currentPos >= drcr.fileSize {
		return false, nil // Already at end of file
	}
	
	// Calculate absolute offset in the RAR stream
	absoluteOffset := drcr.fileOffset + drcr.currentPos
	
	// Find which RAR part should contain this offset
	_, err := drcr.findPartForOffset(absoluteOffset)
	if err != nil {
		return false, fmt.Errorf("failed to find RAR part for offset %d: %w", absoluteOffset, err)
	}
	
	// If we can find a part for this offset, we should transition
	return true, nil
}

// transitionToNextPart handles the transition to the next RAR part
func (drcr *DirectRarContentReader) transitionToNextPart() error {
	// Close current reader using the improved cleanup method
	if err := drcr.closeCurrentReader(); err != nil {
		drcr.log.Debug("Error closing current reader during transition", "error", err)
		// Continue anyway - we want to try the next part
	}
	
	drcr.log.Debug("Transitioning to next RAR part",
		"current_pos", drcr.currentPos,
		"file_offset", drcr.fileOffset,
		"absolute_offset", drcr.fileOffset+drcr.currentPos)
	
	// ensureReaderForPosition will create a new reader for the current position
	return drcr.ensureReaderForPosition()
}

// createUsenetReaderForPart creates a Usenet reader for a specific RAR part starting at the given offset
func (drcr *DirectRarContentReader) createUsenetReaderForPart(partIndex int, offsetInPart int64) (io.ReadCloser, error) {
	if partIndex >= len(drcr.rarFiles) {
		return nil, fmt.Errorf("part index %d exceeds available parts %d", partIndex, len(drcr.rarFiles))
	}
	
	rarFile := drcr.rarFiles[partIndex]
	
	// Calculate the byte range to read from this part
	// We want to read from offsetInPart to the end of the part, but we may need to limit
	// based on how much of the target file remains
	remainingInFile := drcr.fileSize - drcr.currentPos
	remainingInPart := rarFile.Size - offsetInPart
	
	// Read until either we reach the end of the file or the end of this part
	bytesToRead := remainingInFile
	if bytesToRead > remainingInPart {
		bytesToRead = remainingInPart
	}
	
	endOffset := offsetInPart + bytesToRead - 1
	
	drcr.log.Debug("Creating Usenet reader for RAR part",
		"part_filename", rarFile.Filename,
		"part_size", rarFile.Size,
		"offset_in_part", offsetInPart,
		"end_offset", endOffset,
		"bytes_to_read", bytesToRead)
	
	// Create segment loader for this specific RAR part
	loader := rarFileSegmentLoader{segs: rarFile.Segments}
	
	// Get segments in the specified range for this part
	rg := usenet.GetSegmentsInRange(offsetInPart, endOffset, loader)
	
	// Create the Usenet reader for the calculated range
	return usenet.NewUsenetReader(drcr.ctx, drcr.cp, rg, drcr.maxWorkers)
}

// rarFileSegmentLoader adapts RAR file segments to the usenet.SegmentLoader interface
type rarFileSegmentLoader struct {
	segs database.NzbSegments
}

func (rsl rarFileSegmentLoader) GetSegmentCount() int {
	return len(rsl.segs)
}

func (rsl rarFileSegmentLoader) GetSegment(index int) (segment nzbparser.NzbSegment, groups []string, ok bool) {
	if index < 0 || index >= len(rsl.segs) {
		return nzbparser.NzbSegment{}, nil, false
	}
	s := rsl.segs[index]
	return nzbparser.NzbSegment{Number: s.Number, Bytes: int(s.Bytes), ID: s.MessageID}, s.Groups, true
}

func (rcr *rarContentReader) Read(p []byte) (n int, err error) {
	return rcr.rarReader.Read(p)
}

func (rcr *rarContentReader) Close() error {
	if rcr.rarReader != nil {
		return rcr.rarReader.Close()
	}
	return nil
}

// Seek implements io.Seeker for efficient access within RAR content
func (rcr *rarContentReader) Seek(offset int64, whence int) (int64, error) {
	if rcr.rarReader == nil {
		return 0, fmt.Errorf("RAR reader is closed")
	}

	// rardecode.ReadCloser has a Seek method, use it directly
	if r, ok := rcr.rarReader.(io.Seeker); ok {
		return r.Seek(offset, whence)
	}

	return 0, fmt.Errorf("RAR reader does not support seeking")
}
