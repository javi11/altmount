package importer

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"time"

	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/javi11/nntppool"
	"github.com/javi11/rarlist"
)

// RarProcessor interface for analyzing RAR content from NZB data
type RarProcessor interface {
	// AnalyzeRarContentFromNzb analyzes a RAR archive directly from NZB data
	// without downloading. Returns an array of RarContent with file metadata and segments.
	AnalyzeRarContentFromNzb(ctx context.Context, rarFiles []ParsedFile) ([]rarContent, error)
	// CreateFileMetadataFromRarContent creates FileMetadata from RarContent for the metadata
	// system. This is used to convert RarContent into the protobuf format used by the metadata system.
	CreateFileMetadataFromRarContent(rarContent rarContent, sourceNzbPath string) *metapb.FileMetadata
}

// RarContent represents a file within a RAR archive for processing
type rarContent struct {
	InternalPath string                `json:"internal_path"`
	Filename     string                `json:"filename"`
	Size         int64                 `json:"size"`
	Segments     []*metapb.SegmentData `json:"segments"`               // Segment data for this file
	IsDirectory  bool                  `json:"is_directory,omitempty"` // Indicates if this is a directory
}

// rarProcessor handles RAR archive analysis and content extraction
type rarProcessor struct {
	log        *slog.Logger
	cp         nntppool.UsenetConnectionPool
	maxWorkers int
}

// NewRarProcessor creates a new RAR processor
func NewRarProcessor(cp nntppool.UsenetConnectionPool, maxWorkers int) RarProcessor {
	return &rarProcessor{
		log:        slog.Default().With("component", "rar-processor"),
		cp:         cp,
		maxWorkers: maxWorkers,
	}
}

// CreateFileMetadataFromRarContent creates FileMetadata from RarContent for the metadata system
func (rh *rarProcessor) CreateFileMetadataFromRarContent(
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

// AnalyzeRarContentFromNzb analyzes a RAR archive directly from NZB data without downloading
// This implementation uses rarlist with UsenetFileSystem to analyze RAR structure and stream data from Usenet
// Returns an array of files to be added to the metadata with all the info and segments for each file
func (rh *rarProcessor) AnalyzeRarContentFromNzb(ctx context.Context, rarFiles []ParsedFile) ([]rarContent, error) {
	// Create Usenet filesystem for RAR access - this enables rarlist to access
	// RAR part files directly from Usenet without downloading
	ufs := NewUsenetFileSystem(ctx, rh.cp, rarFiles, rh.maxWorkers)

	// Get sorted RAR files for proper multi-part reading
	rarFileNames := ufs.GetRarFiles()
	if len(rarFileNames) == 0 {
		return nil, fmt.Errorf("no RAR files found in the archive")
	}

	// Start with the first RAR file (usually .rar or .part001.rar)
	mainRarFile := rarFileNames[0]

	rh.log.Info("Starting RAR analysis via rarlist with Usenet streaming",
		"main_file", mainRarFile,
		"total_parts", len(rarFileNames),
		"rar_files", len(rarFiles))

	aggregatedFiles, err := rarlist.ListFilesFS(ufs, mainRarFile)
	if err != nil {
		return nil, fmt.Errorf("failed to aggregate RAR files: %w", err)
	}

	if len(aggregatedFiles) == 0 {
		return nil, fmt.Errorf("no valid files found in RAR archive. Compressed or encrypted RARs are not supported")
	}

	rh.log.Debug("Successfully analyzed RAR archive via rarlist",
		"main_file", mainRarFile,
		"files_found", len(aggregatedFiles))

	// Convert rarlist results to RarContent
	rarContents, err := rh.convertAggregatedFilesToRarContent(aggregatedFiles, rarFiles)
	if err != nil {
		return nil, fmt.Errorf("failed to convert rarlist results to RarContent: %w", err)
	}

	return rarContents, nil
}

// convertAggregatedFilesToRarContent converts rarlist.AggregatedFile results to RarContent
func (rh *rarProcessor) convertAggregatedFilesToRarContent(aggregatedFiles []rarlist.AggregatedFile, rarFiles []ParsedFile) ([]rarContent, error) {
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
			Size:         aggregatedFile.TotalPackedSize, // initial logical size based on packed bytes
		}

		var segments []*metapb.SegmentData
		remaining := aggregatedFile.TotalPackedSize

		for _, part := range aggregatedFile.Parts {
			if remaining <= 0 {
				break
			}
			if part.PackedSize <= 0 {
				continue
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

			// Desired inclusive byte range inside this part (only the portion contributing to remaining)
			rangeStart := part.DataOffset
			rangeEnd := part.DataOffset + partContribution - 1
			if rangeEnd < rangeStart { // safety
				continue
			}

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
						SegmentSize: origSeg.SegmentSize,
					})
				}

				filePos += segSize
				if segFileEnd >= rangeEnd {
					break
				}
			}

			remaining -= partContribution
		}

		// Compute packed size represented by segments (inclusive semantics)
		var computedPacked int64
		for _, s := range segments {
			computedPacked += (s.EndOffset - s.StartOffset + 1)
		}

		if computedPacked != aggregatedFile.TotalPackedSize {
			rh.log.Warn("Segment size sum does not match total packed size", "file", aggregatedFile.Name, "expected_packed", aggregatedFile.TotalPackedSize, "got", computedPacked, "unpacked_reported", aggregatedFile.TotalUnpackedSize)
		}

		// Keep logical size as totalPacked; do not overwrite with computed to surface inconsistencies.
		rc.Segments = segments
		rarContents = append(rarContents, rc)
	}

	return rarContents, nil
}
