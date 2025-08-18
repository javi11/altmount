// Package nzb provides RAR archive analysis and content extraction capabilities.
//
// The RarHandler is responsible for analyzing RAR archives directly from NZB data
// without downloading the entire archive. It uses the rarindex package with a virtual
// filesystem to analyze RAR structure and stream data directly from Usenet.
//
// Key functionality:
// - AnalyzeRarContentFromNzb: Analyzes RAR archives using rarindex and returns file metadata with segments
// - convertAggregatedFilesToRarContent: Converts rarindex results to RarContent format
// - CreateFileMetadataFromRarContent: Converts RarContent to protobuf FileMetadata
//
// The segment calculation takes into account:
// - RAR part header offsets from rarindex analysis
// - File start/end positions within each part
// - Proper segment intersection calculations
// - Header-aware offset calculations using rarindex data
package nzb

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"time"

	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/javi11/altmount/pkg/rarindex"
	"github.com/javi11/nntppool"
)

// RarContent represents a file within a RAR archive for processing
type rarContent struct {
	InternalPath string                `json:"internal_path"`
	Filename     string                `json:"filename"`
	Size         int64                 `json:"size"`
	Segments     []*metapb.SegmentData `json:"segments"`               // Segment data for this file
	IsDirectory  bool                  `json:"is_directory,omitempty"` // Indicates if this is a directory
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

// CreateFileMetadataFromRarContent creates FileMetadata from RarContent for the metadata system
func (rh *RarHandler) CreateFileMetadataFromRarContent(
	rarContent rarContent,
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
// This implementation uses rarindex with UsenetFileSystem to analyze RAR structure and stream data from Usenet
// Returns an array of files to be added to the metadata with all the info and segments for each file
func (rh *RarHandler) AnalyzeRarContentFromNzb(ctx context.Context, rarFiles []ParsedFile) ([]rarContent, error) {
	// Create Usenet filesystem for RAR access - this enables rarindex to access
	// RAR part files directly from Usenet without downloading
	ufs := NewUsenetFileSystem(ctx, rh.cp, rarFiles, rh.maxWorkers)

	// Get sorted RAR files for proper multi-part reading
	rarFileNames := ufs.GetRarFiles()
	if len(rarFileNames) == 0 {
		return nil, fmt.Errorf("no RAR files found in the archive")
	}

	// Start with the first RAR file (usually .rar or .part001.rar)
	mainRarFile := rarFileNames[0]

	rh.log.Info("Starting RAR analysis via rarindex with Usenet streaming",
		"main_file", mainRarFile,
		"total_parts", len(rarFileNames),
		"rar_files", len(rarFiles))

	aggregatedFiles, err := rarindex.AggregateFromFirstFS(ufs, mainRarFile)
	if err != nil {
		return nil, fmt.Errorf("failed to aggregate RAR files: %w", err)
	}

	if len(aggregatedFiles) == 0 {
		return nil, fmt.Errorf("no files found in RAR archive")
	}

	rh.log.Debug("Successfully analyzed RAR archive via rarindex",
		"main_file", mainRarFile,
		"files_found", len(aggregatedFiles))

	// Convert rarindex results to RarContent
	rarContents, err := rh.convertAggregatedFilesToRarContent(aggregatedFiles, rarFiles)
	if err != nil {
		return nil, fmt.Errorf("failed to convert rarindex results to RarContent: %w", err)
	}

	return rarContents, nil
}

// convertAggregatedFilesToRarContent converts rarindex.AggregatedFile results to RarContent
func (rh *RarHandler) convertAggregatedFilesToRarContent(aggregatedFiles []rarindex.AggregatedFile, rarFiles []ParsedFile) ([]rarContent, error) {
	var rarContents []rarContent

	for _, aggregatedFile := range aggregatedFiles {
		rh.log.Debug("Converting aggregated file to RAR content",
			"file", aggregatedFile.Name,
			"total_packed_size", aggregatedFile.TotalPackedSize,
			"parts_count", len(aggregatedFile.Parts))

		// Convert AggregatedFilePart to RarPartInfo
		rarContent := rarContent{
			InternalPath: aggregatedFile.Name,
			Filename:     filepath.Base(aggregatedFile.Name),
			Size:         aggregatedFile.TotalUnpackedSize,
		}

		var (
			totalSize int64
			segments  []*metapb.SegmentData
		)
		for _, part := range aggregatedFile.Parts {
			// Find corresponding ParsedFile by path matching
			parsedFile := rh.findParsedFileByPath(part.Path, rarFiles)
			if parsedFile == nil {
				rh.log.Warn("Could not find corresponding ParsedFile for RAR part",
					"part_path", part.Path)
				continue
			}

			var (
				totalRead     int64
				offsetApplyed bool
			)
			for _, originalSeg := range parsedFile.Segments {
				sSize := originalSeg.EndOffset - originalSeg.StartOffset

				if totalRead == part.PackedSize {
					break // No more data to read from this part
				}

				if totalRead < part.DataOffset && sSize+totalRead <= part.DataOffset {
					totalRead += sSize
					continue // Skip segments that are before the part's data offset
				}

				start := originalSeg.StartOffset
				if !offsetApplyed {
					start += part.DataOffset - totalRead
					offsetApplyed = true // Apply offset only once for the first segment
				}

				end := originalSeg.EndOffset
				if part.PackedSize < totalRead+sSize-start {
					end = start + (part.PackedSize - totalRead)
				}

				// Create segments for this RAR part
				segment := &metapb.SegmentData{
					StartOffset: start,
					EndOffset:   end,
					Id:          originalSeg.Id,
				}

				// Add segment - it represents the Usenet download range for this RAR part
				segments = append(segments, segment)
				totalRead += end - start
			}

			// Update total size read from this part
			totalSize += totalRead
		}

		rarContent.Segments = segments
		rarContent.Size = totalSize

		rarContents = append(rarContents, rarContent)
	}

	return rarContents, nil
}

// findParsedFileByPath finds a ParsedFile by matching the file path
func (rh *RarHandler) findParsedFileByPath(path string, rarFiles []ParsedFile) *ParsedFile {
	baseName := filepath.Base(path)

	for i := range rarFiles {
		if rarFiles[i].Filename == path || filepath.Base(rarFiles[i].Filename) == baseName {
			return &rarFiles[i]
		}
	}

	return nil
}

// createSegmentsFromAggregatedParts creates segments from rarindex AggregatedFile data
func (rh *RarHandler) createSegmentsFromAggregatedParts(aggregatedFile rarindex.AggregatedFile, rarFiles []ParsedFile) []*metapb.SegmentData {
	var segments []*metapb.SegmentData

	for _, part := range aggregatedFile.Parts {
		// Find corresponding ParsedFile by path matching
		parsedFile := rh.findParsedFileByPath(part.Path, rarFiles)
		if parsedFile == nil {
			rh.log.Warn("Could not find corresponding ParsedFile for segment creation",
				"part_path", part.Path)
			continue
		}

		// Create segments for this RAR part
		// Segments represent Usenet download ranges, not RAR file offsets
		// The part.DataOffset will be used during file reconstruction to seek within the downloaded RAR data
		for _, originalSeg := range parsedFile.Segments {
			// Use original segment offsets - these are the correct Usenet download positions
			segment := &metapb.SegmentData{
				StartOffset: originalSeg.StartOffset,
				EndOffset:   originalSeg.EndOffset,
				Id:          originalSeg.Id,
			}

			// Add segment - it represents the Usenet download range for this RAR part
			segments = append(segments, segment)
		}
	}

	rh.log.Debug("Created segments from aggregated parts",
		"file", aggregatedFile.Name,
		"total_segments", len(segments))

	return segments
}
