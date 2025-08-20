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
	// Build quick lookup for rar part ParsedFile by both full path and base name
	fileIndex := make(map[string]*ParsedFile, len(rarFiles)*2)
	for i := range rarFiles {
		pf := &rarFiles[i]
		fileIndex[pf.Filename] = pf
		fileIndex[filepath.Base(pf.Filename)] = pf
	}

	out := make([]rarContent, 0, len(aggregatedFiles))

	for _, af := range aggregatedFiles {
		rc := rarContent{
			InternalPath: af.Name,
			Filename:     filepath.Base(af.Name),
			Size:         af.TotalPackedSize,
		}

		var fileSegments []*metapb.SegmentData
		var accumulated int64

		for partIdx, part := range af.Parts {
			if part.PackedSize <= 0 {
				continue
			}

			pf := fileIndex[part.Path]
			if pf == nil {
				pf = fileIndex[filepath.Base(part.Path)]
			}
			if pf == nil {
				rh.log.Warn("RAR part not found among parsed NZB files", "part_path", part.Path, "file", af.Name)
				continue
			}

			// Extract the slice of this part's bytes that belong to the aggregated file.
			sliced, covered, err := slicePartSegments(pf.Segments, part.DataOffset, part.PackedSize)
			if err != nil {
				rh.log.Warn("Failed slicing part segments", "error", err, "part_path", part.Path, "file", af.Name)
				continue
			}
			// Append maintaining order: parts order then segment order within part.
			fileSegments = append(fileSegments, sliced...)
			accumulated += covered

			if covered != part.PackedSize {
				rh.log.Warn("Part coverage mismatch", "file", af.Name, "part_index", partIdx, "expected", part.PackedSize, "covered", covered, "data_offset", part.DataOffset)
			}
		}

		// Validation: sum of trimmed segment lengths should match total packed size.
		var sum int64
		for _, s := range fileSegments {
			sum += (s.EndOffset - s.StartOffset + 1)
		}
		if sum != af.TotalPackedSize {
			rh.log.Warn("Aggregated file coverage mismatch", "file", af.Name, "expected", af.TotalPackedSize, "got", sum)
		}
		rc.Segments = fileSegments
		out = append(out, rc)
	}

	return out, nil
}

// slicePartSegments returns the slice of segment ranges (cloned and trimmed) covering
// [dataOffset, dataOffset+length-1] within a part file represented by ordered segments.
// Assumes each segment's Start/End offsets are relative to the segment itself starting at 0
// and that segments are contiguous in the original order. Returns covered bytes actually found.
func slicePartSegments(segments []*metapb.SegmentData, dataOffset int64, length int64) ([]*metapb.SegmentData, int64, error) {
	if length <= 0 {
		return nil, 0, nil
	}
	if dataOffset < 0 {
		return nil, 0, fmt.Errorf("negative dataOffset")
	}

	targetStart := dataOffset
	targetEnd := dataOffset + length - 1
	var covered int64
	out := []*metapb.SegmentData{}

	// cumulative absolute position inside the part file
	var absPos int64
	for _, seg := range segments {
		segSize := (seg.EndOffset - seg.StartOffset + 1)
		if segSize <= 0 {
			continue
		}
		segAbsStart := absPos + seg.StartOffset // usually absPos
		segAbsEnd := absPos + seg.EndOffset

		// If segment ends before target range starts, skip
		if segAbsEnd < targetStart {
			absPos += segSize
			continue
		}
		// If segment starts after target range ends, we can stop.
		if segAbsStart > targetEnd {
			break
		}

		overlapStart := segAbsStart
		if overlapStart < targetStart {
			overlapStart = targetStart
		}
		overlapEnd := segAbsEnd
		if overlapEnd > targetEnd {
			overlapEnd = targetEnd
		}
		if overlapEnd >= overlapStart {
			// Translate back to segment-relative offsets.
			relStart := seg.StartOffset + (overlapStart - segAbsStart)
			relEnd := seg.StartOffset + (overlapEnd - segAbsStart)
			if relStart < seg.StartOffset {
				relStart = seg.StartOffset
			}
			if relEnd > seg.EndOffset {
				relEnd = seg.EndOffset
			}
			out = append(out, &metapb.SegmentData{
				Id:          seg.Id,
				StartOffset: relStart,
				EndOffset:   relEnd,
				SegmentSize: seg.SegmentSize,
			})
			covered += (relEnd - relStart + 1)
			if overlapEnd == targetEnd { // done
				break
			}
		}
		absPos += segSize
	}

	return out, covered, nil
}
