package nzb

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/javi11/altmount/internal/database"
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
func (rh *RarHandler) AnalyzeRarContentFromNzb(ctx context.Context, rarFiles []ParsedFile, virtualDir *database.VirtualFile) ([]RarContent, error) {
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
