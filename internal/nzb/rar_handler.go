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

// RarPartInfo represents information about which RAR part contains file data
type RarPartInfo struct {
	PartIndex       int    `json:"part_index"`        // Index in rarFiles array
	PartFilename    string `json:"part_filename"`     // e.g., "movie.part001.rar"
	FileStartOffset int64  `json:"file_start_offset"` // Where this file starts in this part (skipping headers)
	FileEndOffset   int64  `json:"file_end_offset"`   // Where this file ends in this part
	DataSize        int64  `json:"data_size"`         // How much of the file is in this part
}

// RarContent represents a file within a RAR archive for processing
type RarContent struct {
	VirtualFileID  int64         `json:"virtual_file_id"`
	InternalPath   string        `json:"internal_path"`
	Filename       string        `json:"filename"`
	Size           int64         `json:"size"`
	CompressedSize *int64        `json:"compressed_size,omitempty"`
	FileOffset     *int64        `json:"file_offset,omitempty"`
	CRC32          *string       `json:"crc32,omitempty"`
	IsDirectory    bool          `json:"is_directory"`
	ModTime        *time.Time    `json:"mod_time,omitempty"`
	PartMapping    []RarPartInfo `json:"part_mapping"` // Which parts contain this file's data
}

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

// calculateFilePartMapping determines which RAR parts contain file data and calculates offsets
func (rh *RarHandler) calculateFilePartMapping(
	fileOffset int64,
	fileSize int64,
	rarFiles []ParsedFile,
	volumeHeaders []rardecode.VolumeHeader,
) []RarPartInfo {
	var partMapping []RarPartInfo
	var cumulativeDataOffset int64 // Data-only cumulative offset (excluding headers)

	rh.log.Debug("Calculating file part mapping",
		"file_offset", fileOffset,
		"file_size", fileSize,
		"total_parts", len(rarFiles))

	for i, rarPart := range rarFiles {
		// Get header size for this part
		var headerSize int64
		if i < len(volumeHeaders) {
			headerSize = volumeHeaders[i].HeaderSize
		} else {
			// Fallback if no volume header available
			headerSize = int64(512)
		}

		// Calculate data size (excluding headers)
		dataSize := rarPart.Size - headerSize
		if dataSize < 0 {
			dataSize = 0 // Safety check
		}

		dataStart := cumulativeDataOffset
		dataEnd := cumulativeDataOffset + dataSize

		// Check if file overlaps with this part's DATA section
		fileEnd := fileOffset + fileSize
		if fileOffset < dataEnd && fileEnd > dataStart {
			// Calculate intersection with data section
			fileStartInData := int64(0)
			if fileOffset > dataStart {
				fileStartInData = fileOffset - dataStart
			}

			fileEndInData := dataSize
			if fileEnd < dataEnd {
				fileEndInData = fileEnd - dataStart
			}

			// Ensure we don't have negative values
			if fileEndInData > fileStartInData && fileStartInData >= 0 {
				dataLength := fileEndInData - fileStartInData

				partInfo := RarPartInfo{
					PartIndex:       i,
					PartFilename:    rarPart.Filename,
					FileStartOffset: headerSize + fileStartInData, // Skip header!
					FileEndOffset:   headerSize + fileEndInData,   // Skip header!
					DataSize:        dataLength,
				}

				partMapping = append(partMapping, partInfo)

				rh.log.Debug("File spans RAR part",
					"part_index", i,
					"part_filename", rarPart.Filename,
					"file_start_offset", partInfo.FileStartOffset,
					"file_end_offset", partInfo.FileEndOffset,
					"data_size", partInfo.DataSize,
					"header_size", headerSize)
			}
		}

		cumulativeDataOffset += dataSize // Only count data, not headers
	}

	rh.log.Debug("Completed file part mapping",
		"total_parts_with_file", len(partMapping),
		"total_data_size", func() int64 {
			var total int64
			for _, pm := range partMapping {
				total += pm.DataSize
			}
			return total
		}())

	return partMapping
}

// AnalyzeRarContentFromNzb analyzes a RAR archive directly from NZB data without downloading
// This implementation uses rardecode.OpenReader with a virtual filesystem to stream RAR data from Usenet
// The virtualFile parameter is the directory that will contain the extracted files (not the RAR file itself)
func (rh *RarHandler) AnalyzeRarContentFromNzb(ctx context.Context, nzbFile *database.NzbFile, rarFiles []ParsedFile, virtualDir *database.VirtualFile) ([]RarContent, error) {
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
		"rar_files", len(rarFiles))

	// This is where the magic happens: rardecode.OpenReader will call ufs.Open() to access
	// RAR part files, which in turn creates UsenetFile instances that stream data directly
	// from Usenet using createUsenetReader() - no local file storage required!
	rarReader, err := rardecode.OpenReader(mainRarFile, rardecode.FileSystem(ufs))
	if err != nil {
		return nil, fmt.Errorf("failed to open RAR archive %s via Usenet filesystem: %w", mainRarFile, err)
	}
	defer rarReader.Close()

	rh.log.Debug("Successfully opened RAR archive via Usenet streaming", "main_file", mainRarFile)

	// Get volume headers first for part mapping calculations
	vol, err := rarReader.VolumeHeaders()
	if err != nil {
		return nil, fmt.Errorf("failed to read RAR volume headers: %w", err)
	}

	// Extract file entries from the RAR archive
	// Each call to rarReader.Next() may trigger reading from different RAR parts,
	// all streamed transparently from Usenet via our virtual filesystem
	var rarContents []RarContent
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

		// Calculate part mapping for this file using VolumeHeaders
		partMapping := rh.calculateFilePartMapping(
			header.Offset,
			header.UnPackedSize,
			rarFiles,
			vol,
		)

		rarContent := RarContent{
			VirtualFileID: virtualDir.ID,
			InternalPath:  filepath.Clean(header.Name),
			Filename:      filepath.Base(header.Name),
			Size:          header.UnPackedSize,
			FileOffset:    &header.Offset,
			CRC32:         crc32Ptr,
			IsDirectory:   header.IsDir,
			ModTime:       modTimePtr,
			PartMapping:   partMapping, // Add the calculated part mapping
		}

		rarContents = append(rarContents, rarContent)

		rh.log.Debug("Successfully streamed RAR file metadata from Usenet",
			"path", rarContent.InternalPath,
			"size", rarContent.Size,
			"compressed", rarContent.CompressedSize,
			"parts_containing_file", len(partMapping),
			"part_mapping", func() string {
				if len(partMapping) == 0 {
					return "no parts"
				}
				var parts []string
				for _, pm := range partMapping {
					parts = append(parts, fmt.Sprintf("%s[%d:%d](%d bytes)",
						pm.PartFilename, pm.FileStartOffset, pm.FileEndOffset, pm.DataSize))
				}
				return strings.Join(parts, ", ")
			}(),
		)
	}

	rh.log.Info("Successfully analyzed RAR content via Usenet streaming",
		"archive_dir", virtualDir.Name,
		"files_found", len(rarContents),
		"rar_parts", len(rarFileNames),
		"total_bytes_in_rar", func() int64 {
			var total int64
			for _, rc := range rarContents {
				total += rc.Size
			}
			return total
		}(),
		"part_mapping_summary", func() string {
			var totalMappings int
			for _, rc := range rarContents {
				totalMappings += len(rc.PartMapping)
			}
			return fmt.Sprintf("%d files with %d total part mappings", len(rarContents), totalMappings)
		}(),
	)

	return rarContents, nil
}

// CreateRarPartsForFile creates RarParts for a specific file using part mapping information
func (rh *RarHandler) CreateRarPartsForFile(archiveFiles []ParsedFile, partMapping []RarPartInfo) database.RarParts {
	if len(partMapping) == 0 {
		return database.RarParts{}
	}

	rarParts := make(database.RarParts, len(partMapping))
	for i, pm := range partMapping {
		// Find the corresponding archive file
		if pm.PartIndex < len(archiveFiles) {
			archiveFile := archiveFiles[pm.PartIndex]

			// Convert segments for this part
			segmentData := database.SegmentData{
				Bytes: archiveFile.Size,
				ID:    "", // Will be filled with segments data
			}

			// Use the actual segments from the ParsedFile
			if len(archiveFile.Segments) > 0 {
				// For simplicity, we'll use the segment data conversion
				segmentData = archiveFile.Segments.ConvertToSegmentsData()
			}

			rarParts[i] = database.RarPart{
				SegmentData: segmentData,
				PartSize:    archiveFile.Size,
				Offset:      pm.FileStartOffset, // File-specific offset (header-aware)
				ByteCount:   pm.DataSize,        // Bytes of this file in this part
			}
		}
	}

	return rarParts
}

// AnalyzeRarContent analyzes a RAR archive to extract its internal file structure (legacy method)
// This implementation uses the rardecode library to parse RAR headers and extract file lists
func (rh *RarHandler) AnalyzeRarContent(r io.Reader, virtualFile *database.VirtualFile, nzbFile *database.NzbFile) ([]RarContent, error) {
	// Extract file list from RAR headers
	fileEntries, err := rh.ExtractRarFileList(r)
	if err != nil {
		return nil, fmt.Errorf("failed to extract RAR file list: %w", err)
	}

	// Convert to database format
	rarContents := make([]RarContent, 0, len(fileEntries))
	for _, entry := range fileEntries {
		// Skip directories for now - focus on files
		if entry.IsDirectory {
			continue
		}

		var crc32Ptr *string
		if entry.CRC32 != "" {
			crc32Ptr = &entry.CRC32
		}

		rarContent := RarContent{
			VirtualFileID:  virtualFile.ID,
			InternalPath:   entry.Path,
			Filename:       entry.Filename,
			Size:           entry.Size,
			CompressedSize: &entry.CompressedSize,
			CRC32:          crc32Ptr,
			IsDirectory:    entry.IsDirectory,
		}
		rarContents = append(rarContents, rarContent)
	}

	rh.log.Info("Successfully analyzed RAR content",
		"archive", virtualFile.Name,
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

// RarContentReadSeeker provides read access for RAR content
type RarContentReadSeeker interface {
	io.ReadCloser
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
// This method provides optimal performance by using range parameters to stream directly from Usenet
func (rh *RarHandler) CreateDirectRarContentReader(ctx context.Context, nzbFile *database.NzbFile, rarFiles []ParsedFile, targetFilename string, rangeOffset, rangeSize int64) (RarContentReadSeeker, error) {
	rh.log.Debug("Creating direct RAR content reader",
		"target_file", targetFilename,
		"range_offset", rangeOffset,
		"range_size", rangeSize,
		"rar_parts", len(rarFiles))

	return &DirectRarContentReader{
		ctx:              ctx,
		cp:               rh.cp,
		nzbFile:          nzbFile,
		rarFiles:         rarFiles,
		targetFilename:   targetFilename,
		rangeOffset:      rangeOffset,
		rangeSize:        rangeSize,
		maxWorkers:       rh.maxWorkers,
		log:              rh.log.With("target_file", targetFilename),
		partBoundaries:   nil, // Will be calculated on first use
		currentReader:    nil,
		currentPosition:  0,  // Start at the beginning of the range
		bytesRead:        0,  // No bytes read initially
		currentPartIndex: -1, // No part selected initially
	}, nil
}

// rarContentReader wraps a rardecode reader for a specific file with seek support
// Implements RarContentReadSeeker interface for efficient RAR content access
type rarContentReader struct {
	rarReader fs.File
}

// DirectRarContentReader provides direct Usenet streaming for RAR content without rardecode overhead
// This implementation uses range parameters to stream directly from Usenet
type DirectRarContentReader struct {
	ctx            context.Context
	cp             nntppool.UsenetConnectionPool
	nzbFile        *database.NzbFile
	rarFiles       []ParsedFile
	targetFilename string
	rangeOffset    int64 // Starting offset of the range within the RAR stream
	rangeSize      int64 // Size of the range to read
	maxWorkers     int
	log            *slog.Logger

	// RAR part information for multi-volume handling
	partBoundaries   []RarPartBoundary
	currentReader    io.ReadCloser // Current Usenet reader
	currentPosition  int64         // Current position within the overall range
	bytesRead        int64         // Total bytes read so far
	currentPartIndex int           // Index of the current RAR part being read
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
	if drcr.bytesRead >= drcr.rangeSize {
		return 0, io.EOF // We've read all requested data
	}

	// Ensure we have a reader for the current position
	if err := drcr.ensureReaderForCurrentPosition(); err != nil {
		return 0, err
	}

	// Calculate how many bytes we can read (don't exceed the total range size)
	remainingBytes := drcr.rangeSize - drcr.bytesRead
	bytesToRead := int64(len(p))
	if bytesToRead > remainingBytes {
		bytesToRead = remainingBytes
	}

	// Read from current reader
	n, err := drcr.currentReader.Read(p[:bytesToRead])
	drcr.bytesRead += int64(n)
	drcr.currentPosition += int64(n)

	drcr.log.Debug("Read bytes from RAR part",
		"bytes_read", n,
		"total_bytes_read", drcr.bytesRead,
		"current_position", drcr.currentPosition,
		"part_index", drcr.currentPartIndex)

	// If we got EOF from current reader, try to move to next part
	if err == io.EOF && drcr.bytesRead < drcr.rangeSize {
		drcr.log.Debug("EOF reached on current RAR part, attempting to move to next part",
			"current_part_index", drcr.currentPartIndex,
			"bytes_read", drcr.bytesRead,
			"total_range_size", drcr.rangeSize)

		// Close current reader
		if closeErr := drcr.closeCurrentReader(); closeErr != nil {
			drcr.log.Debug("Error closing current reader", "error", closeErr)
		}

		// Try to move to next part
		if err := drcr.moveToNextPart(); err != nil {
			if err == io.EOF {
				return n, io.EOF // No more parts available
			}
			return n, err
		}

		// If we successfully moved to next part and haven't read enough bytes yet,
		// try reading more from the new part
		if n < len(p) && drcr.bytesRead < drcr.rangeSize {
			additionalBytes, additionalErr := drcr.Read(p[n:])
			n += additionalBytes
			if additionalErr != nil && additionalErr != io.EOF {
				return n, additionalErr
			}

			return n, nil
		}
	}

	// Return EOF if we've read all requested bytes
	if drcr.bytesRead >= drcr.rangeSize {
		return n, io.EOF
	}

	return n, err
}

// Close implements io.Closer for DirectRarContentReader
func (drcr *DirectRarContentReader) Close() error {
	return drcr.closeCurrentReader()
}

// closeCurrentReader safely closes the current reader with proper error handling
func (drcr *DirectRarContentReader) closeCurrentReader() error {
	if drcr.currentReader != nil {
		drcr.log.Debug("Closing current Usenet reader",
			"range_offset", drcr.rangeOffset,
			"range_size", drcr.rangeSize)

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

// ensureReaderForCurrentPosition ensures we have a Usenet reader for the current position
func (drcr *DirectRarContentReader) ensureReaderForCurrentPosition() error {
	if drcr.currentReader != nil {
		return nil // Already have a reader
	}

	// Calculate the actual offset in the RAR stream
	actualOffset := drcr.rangeOffset + drcr.currentPosition

	// Find which RAR part contains this offset
	if err := drcr.calculatePartBoundaries(); err != nil {
		return err
	}

	targetPart, err := drcr.findPartForOffset(actualOffset)
	if err != nil {
		return fmt.Errorf("failed to find RAR part for position %d (offset %d): %w",
			drcr.currentPosition, actualOffset, err)
	}

	drcr.currentPartIndex = targetPart.PartIndex

	// Calculate offset within this specific RAR part
	offsetInPart := actualOffset - targetPart.StartOffset

	// Calculate how much we can read from this part
	remainingInPart := targetPart.Size - offsetInPart
	remainingInRange := drcr.rangeSize - drcr.bytesRead
	bytesToRead := remainingInRange
	if bytesToRead > remainingInPart {
		bytesToRead = remainingInPart
	}

	partEndOffset := offsetInPart + bytesToRead - 1

	drcr.log.Debug("Creating Usenet reader for current position",
		"position", drcr.currentPosition,
		"actual_offset", actualOffset,
		"part_index", targetPart.PartIndex,
		"part_filename", targetPart.Filename,
		"offset_in_part", offsetInPart,
		"part_end_offset", partEndOffset,
		"bytes_to_read", bytesToRead)

	// Create segment loader for this specific RAR part
	rarFile := drcr.rarFiles[targetPart.PartIndex]
	loader := rarFileSegmentLoader{segs: rarFile.Segments}

	// Get segments in the specified range for this part
	rg := usenet.GetSegmentsInRange(offsetInPart, partEndOffset, loader)

	// Create the Usenet reader for the calculated range
	reader, err := usenet.NewUsenetReader(drcr.ctx, drcr.cp, rg, drcr.maxWorkers)
	if err != nil {
		return fmt.Errorf("failed to create Usenet reader for part %s: %w", targetPart.Filename, err)
	}

	drcr.currentReader = reader
	return nil
}

// moveToNextPart moves to the next RAR part and creates a new reader
func (drcr *DirectRarContentReader) moveToNextPart() error {
	if err := drcr.calculatePartBoundaries(); err != nil {
		return err
	}

	// Check if we have a next part
	nextPartIndex := drcr.currentPartIndex + 1
	if nextPartIndex >= len(drcr.partBoundaries) {
		drcr.log.Debug("No more RAR parts available")
		return io.EOF
	}

	// Move to next part
	drcr.currentPartIndex = nextPartIndex
	nextPart := &drcr.partBoundaries[nextPartIndex]

	// Update current position to the start of the next part
	actualOffset := nextPart.StartOffset
	drcr.currentPosition = actualOffset - drcr.rangeOffset

	// Calculate how much we can read from this part
	remainingInRange := drcr.rangeSize - drcr.bytesRead
	bytesToRead := remainingInRange
	if bytesToRead > nextPart.Size {
		bytesToRead = nextPart.Size
	}

	partEndOffset := bytesToRead - 1

	drcr.log.Debug("Moving to next RAR part",
		"part_index", nextPartIndex,
		"part_filename", nextPart.Filename,
		"part_start_offset", nextPart.StartOffset,
		"current_position", drcr.currentPosition,
		"bytes_to_read", bytesToRead)

	// Create segment loader for this RAR part
	rarFile := drcr.rarFiles[nextPartIndex]
	loader := rarFileSegmentLoader{segs: rarFile.Segments}

	// Get segments for this part (starting from beginning since it's a new part)
	rg := usenet.GetSegmentsInRange(0, partEndOffset, loader)

	// Create the Usenet reader for the new part
	reader, err := usenet.NewUsenetReader(drcr.ctx, drcr.cp, rg, drcr.maxWorkers)
	if err != nil {
		return fmt.Errorf("failed to create Usenet reader for next part %s: %w", nextPart.Filename, err)
	}

	drcr.currentReader = reader
	return nil
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
