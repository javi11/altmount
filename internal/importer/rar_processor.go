package importer

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"sort"
	"strings"
	"time"

	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/javi11/altmount/internal/pool"
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
	log            *slog.Logger
	poolManager    pool.Manager
	maxWorkers     int
	maxCacheSizeMB int
}

// NewRarProcessor creates a new RAR processor
func NewRarProcessor(poolManager pool.Manager, maxWorkers int, maxCacheSizeMB int) RarProcessor {
	return &rarProcessor{
		log:            slog.Default().With("component", "rar-processor"),
		poolManager:    poolManager,
		maxWorkers:     maxWorkers,
		maxCacheSizeMB: maxCacheSizeMB,
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
	if rh.poolManager == nil {
		return nil, NewNonRetryableError("no pool manager available", nil)
	}

	// Rename RAR files to match the first file's base name that will allow parse rar that have different files name
	sortFiles := renameRarFilesAndSort(rarFiles)

	// Create Usenet filesystem for RAR access - this enables rarlist to access
	// RAR part files directly from Usenet without downloading
	ufs := NewUsenetFileSystem(ctx, rh.poolManager, sortFiles, rh.maxWorkers, rh.maxCacheSizeMB)

	// Extract filenames for first part detection
	fileNames := make([]string, len(sortFiles))
	for i, file := range sortFiles {
		fileNames[i] = file.Filename
	}

	// Find the first RAR part using intelligent detection
	mainRarFile, err := rh.getFirstRarPart(fileNames)
	if err != nil {
		return nil, err
	}

	rh.log.Info("Starting RAR analysis",
		"main_file", mainRarFile,
		"total_parts", len(sortFiles),
		"rar_files", len(rarFiles))

	aggregatedFiles, err := rarlist.ListFilesFS(ufs, mainRarFile)
	if err != nil {
		return nil, NewNonRetryableError("failed to aggregate RAR files", err)
	}

	if len(aggregatedFiles) == 0 {
		return nil, NewNonRetryableError("no valid files found in RAR archive. Compressed or encrypted RARs are not supported", nil)
	}

	rh.log.Debug("Successfully analyzed RAR archive via rarlist",
		"main_file", mainRarFile,
		"files_found", len(aggregatedFiles))

	// Convert rarlist results to RarContent
	rarContents, err := rh.convertAggregatedFilesToRarContent(aggregatedFiles, rarFiles)
	if err != nil {
		return nil, NewNonRetryableError("failed to convert rarlist results to RarContent", err)
	}

	return rarContents, nil
}

// getFirstRarPart finds and returns the filename of the first part of a RAR archive
// This method prioritizes .rar files over .part001.rar over .r00 files
func (rh *rarProcessor) getFirstRarPart(rarFileNames []string) (string, error) {
	if len(rarFileNames) == 0 {
		return "", NewNonRetryableError("no RAR files provided", nil)
	}

	// If only one file, return it
	if len(rarFileNames) == 1 {
		return rarFileNames[0], nil
	}

	// Group files by base name and find first parts
	type candidateFile struct {
		filename string
		baseName string
		partNum  int
		priority int // Lower number = higher priority
	}

	var candidates []candidateFile

	for _, filename := range rarFileNames {
		base, part := rh.parseRarFilename(filename)

		// Only consider files that are actually first parts (part 0)
		if part != 0 {
			continue
		}

		// Determine priority based on file extension pattern
		priority := rh.getRarFilePriority(filename)

		candidates = append(candidates, candidateFile{
			filename: filename,
			baseName: base,
			partNum:  part,
			priority: priority,
		})
	}

	if len(candidates) == 0 {
		return "", NewNonRetryableError("no valid first RAR part found in archive", nil)
	}

	// Sort by priority (lower number = higher priority), then by filename for consistency
	best := candidates[0]
	for _, candidate := range candidates[1:] {
		if candidate.priority < best.priority ||
			(candidate.priority == best.priority && candidate.filename < best.filename) {
			best = candidate
		}
	}

	rh.log.Debug("Selected first RAR part",
		"filename", best.filename,
		"base_name", best.baseName,
		"priority", best.priority,
		"total_candidates", len(candidates))

	return best.filename, nil
}

// getRarFilePriority returns the priority for different RAR file types
// Lower number = higher priority
func (rh *rarProcessor) getRarFilePriority(filename string) int {
	lowerName := strings.ToLower(filename)

	// Priority 1: .rar files (main archive)
	if strings.HasSuffix(lowerName, ".rar") && !strings.Contains(lowerName, ".part") {
		return 1
	}

	// Priority 2: .part001.rar, .part01.rar patterns
	if strings.Contains(lowerName, ".part") && strings.HasSuffix(lowerName, ".rar") {
		return 2
	}

	// Priority 3: .r00 patterns
	if strings.Contains(lowerName, ".r0") {
		return 3
	}

	// Priority 4: .001 numeric patterns
	if len(lowerName) > 4 && lowerName[len(lowerName)-4:len(lowerName)-3] == "." {
		return 4
	}

	// Priority 5: Everything else
	return 5
}

// parseRarFilename extracts base name and part number from RAR filename
// This is a simplified version of the logic from processor.go
func (rh *rarProcessor) parseRarFilename(filename string) (base string, part int) {
	lowerFilename := strings.ToLower(filename)

	// Pattern 1: filename.part###.rar (e.g., movie.part001.rar, movie.part01.rar)
	if matches := partPattern.FindStringSubmatch(filename); len(matches) > 2 {
		base = matches[1]
		if partNum := parseInt(matches[2]); partNum >= 0 {
			// Convert 1-based part numbers to 0-based (part001 becomes 0, part002 becomes 1)
			if partNum > 0 {
				part = partNum - 1
			}
			return base, part
		}
	}

	// Pattern 2: filename.rar (first part)
	if strings.HasSuffix(lowerFilename, ".rar") {
		base = strings.TrimSuffix(filename, filepath.Ext(filename))
		return base, 0 // First part
	}

	// Pattern 3: filename.r## or filename.r### (e.g., movie.r00, movie.r01)
	if matches := rPattern.FindStringSubmatch(filename); len(matches) > 2 {
		base = matches[1]
		if partNum := parseInt(matches[2]); partNum >= 0 {
			// .r00 is part 0, .r01 is part 1, etc.
			return base, partNum
		}
	}

	// Pattern 4: filename.### (numeric extensions like .001, .002)
	if matches := numericPattern.FindStringSubmatch(filename); len(matches) > 2 {
		base = matches[1]
		if partNum := parseInt(matches[2]); partNum >= 0 {
			// .001 becomes part 0, .002 becomes part 1, etc.
			if partNum > 0 {
				part = partNum - 1
			}
			return base, part
		}
	}

	// Unknown pattern - return filename as base with high part number (sorts last)
	return filename, 999999
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
				rh.log.Error("Failed slicing part segments", "error", err, "part_path", part.Path, "file", af.Name)
				continue
			}

			// Attempt to patch missing segments if needed
			originalCovered := covered
			sliced, covered, err = patchMissingSegment(sliced, part.PackedSize, covered)
			if err != nil {
				return nil, NewNonRetryableError(
					fmt.Sprintf("incomplete NZB data for %s (part %s): %v",
						af.Name, filepath.Base(part.Path), err), nil)
			}

			// Log if patching was applied
			if covered > originalCovered {
				shortfall := covered - originalCovered
				lastSeg := sliced[len(sliced)-1]
				rh.log.Warn("Patched missing segment at end of part",
					"file", af.Name,
					"part_index", partIdx,
					"part_path", filepath.Base(part.Path),
					"shortfall", shortfall,
					"duplicated_segment", lastSeg.Id)
			}

			// Append maintaining order: parts order then segment order within part.
			fileSegments = append(fileSegments, sliced...)
		}

		rc.Segments = fileSegments
		out = append(out, rc)
	}

	return out, nil
}

// patchMissingSegment attempts to patch a missing segment at the end of a part by duplicating
// the last available segment. This is used when NZB data is incomplete but the gap is small
// enough to be filled with a duplicate segment (typically ≤800KB for a single missing segment).
//
// Parameters:
//   - segments: the current slice of segments covering part of the expected data
//   - expectedSize: the total size in bytes that should be covered
//   - coveredSize: the actual size in bytes covered by the segments
//
// Returns:
//   - patched segments slice with the duplicate segment appended
//   - new covered size after patching
//   - error if patching is not possible (multiple segments missing or no segments to duplicate)
func patchMissingSegment(segments []*metapb.SegmentData, expectedSize, coveredSize int64) ([]*metapb.SegmentData, int64, error) {
	shortfall := expectedSize - coveredSize
	if shortfall <= 0 {
		// No patching needed
		return segments, coveredSize, nil
	}

	const maxSingleSegmentSize = 800000 // ~800KB, typical segment is ~768KB

	// Check if the shortfall is small enough to be a single missing segment
	if shortfall > maxSingleSegmentSize {
		return nil, 0, NewNonRetryableError(
			fmt.Sprintf("missing %d bytes exceeds single segment threshold (%d bytes), cannot auto-patch", shortfall, maxSingleSegmentSize), nil)
	}

	// Check if we have segments to duplicate
	if len(segments) == 0 {
		return nil, 0, NewNonRetryableError("no segments available to duplicate for patching", nil)
	}

	// Duplicate the last segment to fill the gap
	lastSeg := segments[len(segments)-1]
	patchSeg := &metapb.SegmentData{
		Id:          lastSeg.Id,
		StartOffset: lastSeg.StartOffset,
		EndOffset:   lastSeg.StartOffset + shortfall - 1,
		SegmentSize: lastSeg.SegmentSize,
	}

	patchedSegments := append(segments, patchSeg)
	newCovered := coveredSize + shortfall

	return patchedSegments, newCovered, nil
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
		return nil, 0, NewNonRetryableError("negative dataOffset", nil)
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

// extractBaseFilename extracts the base filename without the part suffix
// This works with the original patterns (including leading zeros) to properly extract the base
func extractBaseFilename(filename string) string {
	// Try each pattern and extract the base name (group 1)
	if matches := partPattern.FindStringSubmatch(filename); len(matches) > 1 {
		return matches[1]
	}
	if matches := rPattern.FindStringSubmatch(filename); len(matches) > 1 {
		return matches[1]
	}
	if matches := numericPattern.FindStringSubmatch(filename); len(matches) > 1 {
		return matches[1]
	}

	// If no pattern matches, return the filename without extension
	return strings.TrimSuffix(filename, filepath.Ext(filename))
}

// stripLeadingZeros removes leading zeros from a numeric string while preserving at least one digit
func stripLeadingZeros(s string) string {
	if s == "" {
		return "0"
	}

	// Find first non-zero digit
	i := 0
	for i < len(s) && s[i] == '0' {
		i++
	}

	// If all digits are zero, return "0"
	if i == len(s) {
		return "0"
	}

	// Return string starting from first non-zero digit
	return s[i:]
}

func renameRarFilesAndSort(rarFiles []ParsedFile) []ParsedFile {
	// Get the base name of the first RAR file (without extension)
	// We need to use the original suffix (with leading zeros) to properly extract the base name
	firstFileBase := extractBaseFilename(rarFiles[0].Filename)

	// Rename all RAR files to match the base name of the first file while preserving original part naming
	for i := range rarFiles {
		originalFileName := rarFiles[i].Filename

		// Try to extract the part suffix from the original filename
		partSuffix := getPartSuffix(originalFileName)

		// Construct new filename with first file's base name and original part suffix
		rarFiles[i].Filename = firstFileBase + partSuffix
	}

	// Sort files by part number
	sort.Slice(rarFiles, func(i, j int) bool {
		partI := extractRarPartNumber(rarFiles[i].Filename)
		partJ := extractRarPartNumber(rarFiles[j].Filename)
		return partI < partJ
	})

	return rarFiles
}

func getPartSuffix(originalFileName string) string {
	if matches := partPatternNumber.FindStringSubmatch(originalFileName); len(matches) > 1 {
		return fmt.Sprintf(".part%s.rar", stripLeadingZeros(matches[1]))
	} else if matches := rPatternNumber.FindStringSubmatch(originalFileName); len(matches) > 1 {
		return fmt.Sprintf(".r%s", matches[1])
	} else if matches := numericPatternNumber.FindStringSubmatch(originalFileName); len(matches) > 1 {
		return fmt.Sprintf(".%s", matches[1])
	}

	return filepath.Ext(originalFileName)
}

// extractRarPartNumber extracts numeric part from RAR extension for sorting
func extractRarPartNumber(fileName string) int {
	partNumber := getPartNumber(fileName)
	if partNumber > 0 {
		return partNumber
	}

	return 999999 // Unknown format goes last
}
