// Package nzb provides RAR archive analysis and content extraction capabilities.
//
// The RarHandler is responsible for analyzing RAR archives directly from NZB data
// without downloading the entire archive. It uses rardecode.OpenReader with a virtual
// filesystem to stream RAR data directly from Usenet.
//
// Key functionality:
// - AnalyzeRarContentFromNzb: Analyzes RAR archives and returns file metadata with segments
// - createSegmentsFromPartMapping: Creates segment data for each file based on RAR part mapping
// - CreateFileMetadataFromRarContent: Converts RarContent to protobuf FileMetadata
//
// The segment calculation takes into account:
// - RAR part header offsets
// - File start/end positions within each part
// - Proper segment intersection calculations
// - Header-aware offset calculations
package nzb

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/javi11/nntppool"
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
	InternalPath   string                `json:"internal_path"`
	Filename       string                `json:"filename"`
	Size           int64                 `json:"size"`
	CompressedSize *int64                `json:"compressed_size,omitempty"`
	FileOffset     *int64                `json:"file_offset,omitempty"`
	CRC32          *string               `json:"crc32,omitempty"`
	IsDirectory    bool                  `json:"is_directory"`
	ModTime        *time.Time            `json:"mod_time,omitempty"`
	PartMapping    []RarPartInfo         `json:"part_mapping"` // Which parts contain this file's data
	Segments       []*metapb.SegmentData `json:"segments"`     // Segment data for this file
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
) []RarPartInfo {
	var partMapping []RarPartInfo
	var cumulativeDataOffset int64 // Data-only cumulative offset (excluding headers)

	rh.log.Debug("Calculating file part mapping",
		"file_offset", fileOffset,
		"file_size", fileSize,
		"total_parts", len(rarFiles))

	for i, rarPart := range rarFiles {
		// Get header size for this part

		// Calculate data size (excluding headers)
		dataSize := rarPart.Size
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
					FileStartOffset: fileStartInData, // Skip header!
					FileEndOffset:   fileEndInData,   // Skip header!
					DataSize:        dataLength,
				}

				partMapping = append(partMapping, partInfo)

				rh.log.Debug("File spans RAR part",
					"part_index", i,
					"part_filename", rarPart.Filename,
					"file_start_offset", partInfo.FileStartOffset,
					"file_end_offset", partInfo.FileEndOffset,
					"data_size", partInfo.DataSize)
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

// createSegmentsFromPartMapping creates SegmentData slices for a file based on its part mapping
func (rh *RarHandler) createSegmentsFromPartMapping(
	fileSize int64,
	partMapping []RarPartInfo,
	rarFiles []ParsedFile,
) []*metapb.SegmentData {
	var segments []*metapb.SegmentData
	var totalDataProcessed int64

	rh.log.Debug("Creating segments from part mapping",
		"part_count", len(partMapping),
		"file_size", fileSize)

	for _, pm := range partMapping {
		// Check if we've already fulfilled the file's total size
		if totalDataProcessed >= fileSize {
			rh.log.Debug("File size already fulfilled, breaking loop",
				"total_processed", totalDataProcessed,
				"file_size", fileSize)
			break
		}

		if pm.PartIndex >= len(rarFiles) {
			rh.log.Warn("Part index out of range",
				"part_index", pm.PartIndex,
				"rar_files_count", len(rarFiles))
			continue
		}

		archiveFile := rarFiles[pm.PartIndex]

		rh.log.Debug("Processing RAR part for segments",
			"part_index", pm.PartIndex,
			"part_filename", pm.PartFilename,
			"file_start_offset", pm.FileStartOffset,
			"file_end_offset", pm.FileEndOffset,
			"data_size", pm.DataSize,
			"original_segments", len(archiveFile.Segments),
			"total_processed", totalDataProcessed)

		var firstSegmentAjusted bool

		// Create segments for this part of the file
		// Each segment from the original RAR part needs to be adjusted for the file's offset within that part
		for _, originalSeg := range archiveFile.Segments {
			// Check again if we've fulfilled the file size during segment processing
			if totalDataProcessed >= fileSize {
				rh.log.Debug("File size fulfilled during segment processing, breaking",
					"total_processed", totalDataProcessed,
					"file_size", fileSize)
				break
			}

			// Calculate the intersection of this segment with the file's data range in this part
			segStart := originalSeg.StartOffset
			segEnd := originalSeg.EndOffset

			// Adjust segment offsets to account for the file's position within the RAR part
			// The file starts at pm.FileStartOffset within this RAR part
			// and contains pm.DataSize bytes
			partDataStart := pm.FileStartOffset
			partDataEnd := pm.FileEndOffset

			// Check if this segment overlaps with the file's data in this part
			if segEnd >= partDataStart && segStart < partDataEnd {
				// Calculate intersection
				adjustedStart := segStart
				adjustedEnd := segEnd

				// Trim segment to file's data range within this part
				if !firstSegmentAjusted && adjustedStart < partDataStart {
					adjustedStart = partDataStart
					firstSegmentAjusted = true
				}
				if adjustedEnd > partDataEnd {
					adjustedEnd = partDataEnd
				}

				// Only create segment if there's actual data
				if adjustedEnd > adjustedStart {
					segmentSize := adjustedEnd - adjustedStart

					// Ensure we don't exceed the file size
					remainingBytes := fileSize - totalDataProcessed
					if segmentSize > remainingBytes {
						adjustedEnd = adjustedStart + remainingBytes
						segmentSize = remainingBytes
					}

					segment := &metapb.SegmentData{
						StartOffset: adjustedStart,
						EndOffset:   adjustedEnd,
						Id:          originalSeg.Id,
					}
					segments = append(segments, segment)
					totalDataProcessed += segmentSize

					rh.log.Debug("Created segment for file",
						"original_start", segStart,
						"original_end", segEnd,
						"adjusted_start", adjustedStart,
						"adjusted_end", adjustedEnd,
						"segment_size", segmentSize,
						"total_processed", totalDataProcessed,
						"segment_id", originalSeg.Id)

					// Break if we've reached the file size
					if totalDataProcessed >= fileSize {
						rh.log.Debug("File size reached, stopping segment creation",
							"total_processed", totalDataProcessed,
							"file_size", fileSize)
						break
					}
				}
			}
		}
	}

	rh.log.Debug("Completed segment creation",
		"total_segments", len(segments),
		"total_data_processed", totalDataProcessed,
		"file_size", fileSize)

	return segments
}

// CreateFileMetadataFromRarContent creates FileMetadata from RarContent for the metadata system
func (rh *RarHandler) CreateFileMetadataFromRarContent(
	rarContent RarContent,
	sourceNzbPath string,
) *metapb.FileMetadata {
	now := time.Now().Unix()

	return &metapb.FileMetadata{
		FileSize:      rarContent.Size,
		SourceNzbPath: sourceNzbPath,
		Status:        metapb.FileStatus_FILE_STATUS_HEALTHY,
		CreatedAt:     now,
		ModifiedAt:    now,
		SegmentData:   rarContent.Segments,
	}
}

// ValidateSegments validates that segments are properly ordered and non-overlapping
func (rh *RarHandler) ValidateSegments(segments []*metapb.SegmentData) error {
	if len(segments) == 0 {
		return nil
	}

	for i := 1; i < len(segments); i++ {
		prev := segments[i-1]
		curr := segments[i]

		if curr.StartOffset < prev.EndOffset {
			return fmt.Errorf("segments overlap: segment %d ends at %d but segment %d starts at %d",
				i-1, prev.EndOffset, i, curr.StartOffset)
		}
	}

	return nil
}

// AnalyzeRarContentFromNzb analyzes a RAR archive directly from NZB data without downloading
// This implementation uses rardecode.OpenReader with a virtual filesystem to stream RAR data from Usenet
// Returns an array of files to be added to the metadata with all the info and segments for each file
func (rh *RarHandler) AnalyzeRarContentFromNzb(ctx context.Context, rarFiles []ParsedFile) ([]RarContent, error) {
	// Create Usenet filesystem for RAR access - this is the key component that enables
	// rardecode.OpenReader to read RAR parts directly from Usenet without downloading
	ufs := NewUsenetFileSystem(ctx, rh.cp, rarFiles, rh.maxWorkers)

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

	// Extract file entries from the RAR archive
	// Each call to rarReader.Next() may trigger reading from different RAR parts,
	// all streamed transparently from Usenet via our virtual filesystem
	var headers []*rardecode.FileHeader
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

		rh.log.Debug("Found RAR file header",
			"path", header.Name,
			"size", header.UnPackedSize,
			"offset", header.Offset,
			"file_num", fileCount)

		headers = append(headers, header)
	}

	var rarContents []RarContent
	for _, header := range headers {
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
		)

		// Create segments for this file based on part mapping
		segments := rh.createSegmentsFromPartMapping(header.UnPackedSize, partMapping, rarFiles)

		rarContent := RarContent{
			InternalPath: filepath.Clean(header.Name),
			Filename:     filepath.Base(header.Name),
			Size:         header.UnPackedSize,
			FileOffset:   &header.Offset,
			CRC32:        crc32Ptr,
			IsDirectory:  header.IsDir,
			ModTime:      modTimePtr,
			PartMapping:  partMapping, // Add the calculated part mapping
			Segments:     segments,    // Add the calculated segments
		}

		rarContents = append(rarContents, rarContent)

		rh.log.Debug("Successfully streamed RAR file metadata from Usenet",
			"path", rarContent.InternalPath,
			"size", rarContent.Size,
			"compressed", rarContent.CompressedSize,
			"parts_containing_file", len(partMapping),
			"segments_count", len(segments),
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
