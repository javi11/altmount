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

		// EndOffset is inclusive. Overlap exists if current start <= previous end.
		if curr.StartOffset <= prev.EndOffset {
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

	fileIndex := make(map[string]*ParsedFile, len(rarFiles)*2)
	for i := range rarFiles {
		pf := &rarFiles[i]
		fileIndex[pf.Filename] = pf
		fileIndex[filepath.Base(pf.Filename)] = pf
	}

	for _, aggregatedFile := range aggregatedFiles {
		rc := rarContent{
			InternalPath: aggregatedFile.Name,
			Filename:     filepath.Base(aggregatedFile.Name),
			Size:         aggregatedFile.TotalUnpackedSize,
		}

		var segments []*metapb.SegmentData
		remaining := aggregatedFile.TotalUnpackedSize

		for _, part := range aggregatedFile.Parts {
			if remaining <= 0 {
				break
			}

			parsedFile := fileIndex[part.Path]
			if parsedFile == nil {
				parsedFile = fileIndex[filepath.Base(part.Path)]
			}
			if parsedFile == nil {
				rh.log.Warn("RAR part file not found among NZB parsed files", "part_path", part.Path)
				continue
			}

			partContribution := part.PackedSize
			if partContribution > remaining {
				partContribution = remaining
			}
			if partContribution <= 0 {
				continue
			}

			// Desired inclusive byte range inside this part
			rangeStart := part.DataOffset
			rangeEnd := part.DataOffset + partContribution - 1

			filePos := int64(0)
			for _, origSeg := range parsedFile.Segments {
				segSize := (origSeg.EndOffset - origSeg.StartOffset) + 1 // inclusive size
				if segSize <= 0 {
					continue
				}

				segFileStart := filePos + origSeg.StartOffset
				segFileEnd := filePos + origSeg.EndOffset // inclusive

				// Skip if no overlap
				if segFileEnd < rangeStart {
					filePos += segSize
					continue
				}
				if segFileStart > rangeEnd {
					break
				}

				// Overlap slice
				sliceStart := segFileStart
				if sliceStart < rangeStart {
					sliceStart = rangeStart
				}
				sliceEnd := segFileEnd
				if sliceEnd > rangeEnd {
					sliceEnd = rangeEnd
				}

				trimStart := origSeg.StartOffset + (sliceStart - segFileStart)
				trimEnd := origSeg.StartOffset + (sliceEnd - segFileStart) // inclusive
				if trimEnd > origSeg.EndOffset {
					trimEnd = origSeg.EndOffset
				}
				if trimStart < origSeg.StartOffset {
					trimStart = origSeg.StartOffset
				}
				if trimEnd >= trimStart {
					segments = append(segments, &metapb.SegmentData{
						Id:          origSeg.Id,
						StartOffset: trimStart,
						EndOffset:   trimEnd,
					})
				}

				filePos += segSize
				if segFileEnd >= rangeEnd {
					break
				}
			}

			remaining -= partContribution
		}

		// Compute size from segments (inclusive semantics)
		var computed int64
		for _, s := range segments {
			computed += (s.EndOffset - s.StartOffset + 1)
		}
		if aggregatedFile.TotalUnpackedSize > 0 && computed != aggregatedFile.TotalUnpackedSize {
			rh.log.Warn("Segment size sum does not match unpacked size", "file", aggregatedFile.Name, "expected", aggregatedFile.TotalUnpackedSize, "got", computed)
		}

		rc.Size = computed
		rc.Segments = segments
		rarContents = append(rarContents, rc)
	}

	return rarContents, nil
}
