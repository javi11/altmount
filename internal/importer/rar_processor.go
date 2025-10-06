package importer

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"sort"
	"strings"
	"time"

	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/javi11/altmount/internal/pool"
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

// AnalyzeRarContentFromNzb analyzes RAR archives directly from NZB data without downloading
// This implementation uses rarlist with UsenetFileSystem to analyze RAR structure and stream data from Usenet
// It supports multiple independent RAR archives in a single NZB (e.g., season packs)
// Returns an array of files to be added to the metadata with all the info and segments for each file
func (rh *rarProcessor) AnalyzeRarContentFromNzb(ctx context.Context, rarFiles []ParsedFile) ([]rarContent, error) {
	if rh.poolManager == nil {
		return nil, NewNonRetryableError("no pool manager available", nil)
	}

	// Rename RAR files to match the first file's base name that will allow parse rar that have different files name
	sortFiles := renameRarFilesAndSort(rarFiles, rh.log)

	cp, err := rh.poolManager.GetPool()
	if err != nil {
		return nil, NewNonRetryableError("no connection pool available", err)
	}

	// Extract all RAR contents (may contain multiple independent archives)
	allRarContents := make([]rarContent, 0)
	remainingFiles := sortFiles
	archiveIndex := 1

	for len(remainingFiles) > 0 {
		rh.log.Info("Analyzing RAR archive",
			"archive_number", archiveIndex,
			"remaining_parts", len(remainingFiles))

		// Extract one archive and get the remaining files
		contents, usedFiles, err := rh.extractSingleArchive(ctx, cp, remainingFiles)
		if err != nil {
			// If extraction failed and we have many remaining parts, check for RAR headers
			// to provide better diagnostic information before failing
			if len(remainingFiles) > 10 {
				rh.log.Warn("Archive extraction failed, checking for RAR headers in remaining parts",
					"error", err,
					"remaining_parts", len(remainingFiles))
				
				hasHeaders := rh.checkForRarHeaders(ctx, cp, remainingFiles)
				if hasHeaders {
					rh.log.Error("Found RAR headers in remaining parts - indicating multi-episode archive with embedded headers",
						"remaining_parts", len(remainingFiles),
						"files_extracted", len(allRarContents))
					rh.log.Error("This archive structure is not fully supported - only first episode can be extracted")
					rh.log.Error("Recommendation: Use SABnzbd to download and extract the complete archive")
					
					// Return error to prevent false-positive "success" in Arr applications
					// This ensures Sonarr/Radarr don't mark partial extractions as complete downloads
					return nil, NewNonRetryableError(
						fmt.Sprintf("multi-episode archive with embedded RAR headers: extracted %d of %d+ episodes - full extraction not supported",
							len(allRarContents), archiveIndex),
						err)
				}
			}
			return nil, err
		}

		// Add contents from this archive
		allRarContents = append(allRarContents, contents...)

		// Remove used files from the remaining list
		remainingFiles = rh.filterUnusedFiles(remainingFiles, usedFiles)

		rh.log.Info("Archive extraction complete",
			"archive_number", archiveIndex,
			"files_extracted", len(contents),
			"parts_used", len(usedFiles),
			"parts_remaining", len(remainingFiles))

		// If no files were used, we have a problem - break to avoid infinite loop
		if len(usedFiles) == 0 {
			rh.log.Warn("No files were used in this iteration, stopping multi-archive detection",
				"remaining_files", len(remainingFiles))
			break
		}

		archiveIndex++
	}

	rh.log.Info("All RAR archives analyzed",
		"total_archives", archiveIndex-1,
		"total_files_extracted", len(allRarContents))

	return allRarContents, nil
}

// checkForRarHeaders scans the remaining parts to detect if any contain RAR file headers
// This helps identify multiple independent archives with sequential numbering (e.g., season packs)
func (rh *rarProcessor) checkForRarHeaders(ctx context.Context, cp nntppool.UsenetConnectionPool, remainingFiles []ParsedFile) bool {
	// RAR file signatures
	// RAR 4.x: 52 61 72 21 1A 07 00 ("Rar!\x1A\x07\x00")
	// RAR 5.x: 52 61 72 21 1A 07 01 00 ("Rar!\x1A\x07\x01\x00")
	rarSignature := []byte{0x52, 0x61, 0x72, 0x21, 0x1A, 0x07}
	
	rh.log.Debug("Scanning remaining parts for RAR headers",
		"total_parts", len(remainingFiles))
	
	// Check first few files for RAR headers (don't need to check all)
	checkLimit := 10
	if len(remainingFiles) < checkLimit {
		checkLimit = len(remainingFiles)
	}
	
	for i := 0; i < checkLimit; i++ {
		file := remainingFiles[i]
		
		// Skip if no segments
		if len(file.Segments) == 0 {
			continue
		}
		
		// Read first segment (first ~700KB) to check for RAR header
		firstSegment := file.Segments[0]
		
		// Get article ID from first segment
		if firstSegment.Id == "" {
			continue
		}
		
		// Get a body reader from the pool
		reader, err := cp.BodyReader(ctx, firstSegment.Id, file.Groups)
		if err != nil {
			rh.log.Debug("Failed to get body reader for RAR header check",
				"file", file.Filename,
				"error", err)
			continue
		}
		
		// Read first 512 bytes to check for RAR signature
		headerBytes := make([]byte, 512)
		n, _ := reader.Read(headerBytes)
		reader.Close()
		
		if n < len(rarSignature) {
			continue
		}
		
		// Check if RAR signature exists in the first 512 bytes
		for j := 0; j <= n-len(rarSignature); j++ {
			if bytes.Equal(headerBytes[j:j+len(rarSignature)], rarSignature) {
				rh.log.Info("Found RAR header in remaining part",
					"file", file.Filename,
					"offset", j,
					"part_index", i)
				return true
			}
		}
	}
	
	rh.log.Debug("No RAR headers found in remaining parts",
		"parts_checked", checkLimit)
	return false
}

// extractSingleArchive extracts one RAR archive and returns its contents and the files it used
func (rh *rarProcessor) extractSingleArchive(ctx context.Context, cp nntppool.UsenetConnectionPool, rarFiles []ParsedFile) ([]rarContent, []string, error) {
	// Create Usenet filesystem for RAR access - this enables rarlist to access
	// RAR part files directly from Usenet without downloading
	ufs := NewUsenetFileSystem(ctx, cp, rarFiles, rh.maxWorkers, rh.maxCacheSizeMB, rh.log)

	// Extract filenames for first part detection
	fileNames := make([]string, len(rarFiles))
	for i, file := range rarFiles {
		fileNames[i] = file.Filename
	}

	// Find the first RAR part using intelligent detection
	mainRarFile, err := rh.getFirstRarPart(fileNames)
	if err != nil {
		return nil, nil, err
	}

	rh.log.Info("Starting RAR analysis",
		"main_file", mainRarFile,
		"total_parts", len(rarFiles))

	// Log the filenames available in the filesystem for rarlist matching
	rh.log.Debug("UsenetFileSystem initialized with RAR files",
		"main_file", mainRarFile,
		"total_files", len(rarFiles))
	for i, f := range rarFiles {
		rh.log.Debug("UFS file entry",
			"index", i,
			"filename", f.Filename)
	}

	aggregatedFiles, err := rarlist.ListFilesFS(ufs, mainRarFile)
	if err != nil {
		return nil, nil, NewNonRetryableError("failed to aggregate RAR files", err)
	}

	if len(aggregatedFiles) == 0 {
		return nil, nil, NewNonRetryableError("no valid files found in RAR archive. Compressed or encrypted RARs are not supported", nil)
	}

	rh.log.Debug("Successfully analyzed RAR archive via rarlist",
		"main_file", mainRarFile,
		"files_found", len(aggregatedFiles))

	// Log detailed information about the aggregated files
	for i, af := range aggregatedFiles {
		rh.log.Debug("RAR archive file details",
			"index", i,
			"name", af.Name,
			"total_packed_size", af.TotalPackedSize,
			"parts_count", len(af.Parts))
		for j, part := range af.Parts {
			rh.log.Debug("RAR archive file part",
				"file_index", i,
				"part_index", j,
				"path", part.Path,
				"data_offset", part.DataOffset,
				"packed_size", part.PackedSize)
		}
	}

	// Extract the list of RAR part files that were actually used
	usedFiles := rh.extractUsedFiles(aggregatedFiles)

	// Convert rarlist results to RarContent
	rarContents, err := rh.convertAggregatedFilesToRarContent(aggregatedFiles, rarFiles)
	if err != nil {
		return nil, nil, NewNonRetryableError("failed to convert rarlist results to RarContent", err)
	}

	return rarContents, usedFiles, nil
}

// extractUsedFiles returns the list of RAR part filenames that were actually used by rarlist
func (rh *rarProcessor) extractUsedFiles(aggregatedFiles []rarlist.AggregatedFile) []string {
	usedFilesMap := make(map[string]bool)
	for _, af := range aggregatedFiles {
		for _, part := range af.Parts {
			usedFilesMap[part.Path] = true
		}
	}

	usedFiles := make([]string, 0, len(usedFilesMap))
	for filename := range usedFilesMap {
		usedFiles = append(usedFiles, filename)
	}
	return usedFiles
}

// filterUnusedFiles removes used files from the remaining file list
func (rh *rarProcessor) filterUnusedFiles(allFiles []ParsedFile, usedFiles []string) []ParsedFile {
	usedFilesMap := make(map[string]bool)
	for _, filename := range usedFiles {
		usedFilesMap[filename] = true
	}

	remainingFiles := make([]ParsedFile, 0)
	for _, file := range allFiles {
		if !usedFilesMap[file.Filename] {
			remainingFiles = append(remainingFiles, file)
		}
	}
	return remainingFiles
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

	rh.log.Debug("Analyzing RAR files for first part detection",
		"total_files", len(rarFileNames),
		"filenames", rarFileNames)

	for _, filename := range rarFileNames {
		base, part := rh.parseRarFilename(filename)

		rh.log.Debug("Parsed RAR filename",
			"filename", filename,
			"base", base,
			"part", part)

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

	// If no part 0 found, check if this might be a multi-volume RAR with missing initial parts
	// In such cases, we should fail rather than attempt to process from a mid-volume file
	if len(candidates) == 0 {
		// Count how many parts we have - if it's many files with sequential numbering,
		// this is likely a broken multi-volume RAR
		if len(rarFileNames) > 5 {
			rh.log.Error("No part 0 found in multi-volume RAR archive - initial parts may be missing or corrupted",
				"total_parts", len(rarFileNames),
				"sample_files", rarFileNames[:min(5, len(rarFileNames))])
			return "", NewNonRetryableError("multi-volume RAR archive missing part 0 (.rar or .r00) - cannot process", nil)
		}

		// For small archives (≤5 files), try fallback to lowest-numbered part
		// This handles edge cases like single-file non-standard archives
		rh.log.Warn("No part 0 found in small archive, attempting fallback to lowest-numbered part",
			"total_files", len(rarFileNames))

		lowestPart := 999999
		var lowestFile string

		for _, filename := range rarFileNames {
			base, part := rh.parseRarFilename(filename)

			if part < lowestPart {
				lowestPart = part
				lowestFile = filename
				
				// Determine priority based on file extension pattern
				priority := rh.getRarFilePriority(filename)

				candidates = []candidateFile{{
					filename: filename,
					baseName: base,
					partNum:  part,
					priority: priority,
				}}
			} else if part == lowestPart {
				// Same part number, add as candidate
				priority := rh.getRarFilePriority(filename)

				candidates = append(candidates, candidateFile{
					filename: filename,
					baseName: base,
					partNum:  part,
					priority: priority,
				})
			}
		}

		if len(candidates) > 0 {
			rh.log.Warn("Using lowest-numbered part as fallback",
				"filename", lowestFile,
				"part", lowestPart)
		}
	}

	if len(candidates) == 0 {
		rh.log.Error("No valid first RAR part found",
			"total_files", len(rarFileNames),
			"files_checked", rarFileNames)
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
	// Note: Filenames are already normalized by renameRarFilesAndSort before this is called
	
	// Strip trailing ] bracket if present (malformed NZB filenames)
	// E.g., "file.r31]" becomes "file.r31"
	cleanFilename := strings.TrimSuffix(filename, "]")
	lowerFilename := strings.ToLower(cleanFilename)

	// Pattern 1: filename.part###.rar (e.g., movie.part001.rar, movie.part01.rar)
	if matches := partPattern.FindStringSubmatch(cleanFilename); len(matches) > 2 {
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
		base = strings.TrimSuffix(cleanFilename, filepath.Ext(cleanFilename))
		return base, 0 // First part
	}

	// Pattern 3: filename.r## or filename.r### (e.g., movie.r00, movie.r01)
	if matches := rPattern.FindStringSubmatch(cleanFilename); len(matches) > 2 {
		base = matches[1]
		if partNum := parseInt(matches[2]); partNum >= 0 {
			// .r00 is part 0, .r01 is part 1, etc.
			return base, partNum
		}
	}

	// Pattern 4: filename.### (numeric extensions like .001, .002)
	if matches := numericPattern.FindStringSubmatch(cleanFilename); len(matches) > 2 {
		base = matches[1]
		if partNum := parseInt(matches[2]); partNum >= 0 {
			// .001 becomes part 0, .002 becomes part 1, etc.
			if partNum > 0 {
				part = partNum - 1
			}
			return base, part
		}
	}

	// Unknown pattern - return cleanFilename as base with high part number (sorts last)
	return cleanFilename, 999999
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
		rh.log.Debug("Converting aggregated file",
			"name", af.Name,
			"total_packed_size", af.TotalPackedSize,
			"parts_count", len(af.Parts))

		rc := rarContent{
			InternalPath: af.Name,
			Filename:     filepath.Base(af.Name),
			Size:         af.TotalPackedSize,
		}

		var fileSegments []*metapb.SegmentData
		var accumulated int64

		for partIdx, part := range af.Parts {
			if part.PackedSize <= 0 {
				rh.log.Debug("Skipping part with zero or negative size",
					"file", af.Name,
					"part_index", partIdx,
					"packed_size", part.PackedSize)
				continue
			}

			// Try lookup with full path first, then base name
			// The fileIndex includes both original and normalized (bracket-stripped) versions
			pf := fileIndex[part.Path]
			if pf == nil {
				pf = fileIndex[filepath.Base(part.Path)]
			}
			if pf == nil {
				rh.log.Warn("RAR part not found among parsed NZB files", "part_path", part.Path, "file", af.Name)
				continue
			}

			rh.log.Debug("Processing RAR part",
				"file", af.Name,
				"part_index", partIdx,
				"part_path", part.Path,
				"data_offset", part.DataOffset,
				"packed_size", part.PackedSize,
				"rar_file_size", pf.Size,
				"segments_count", len(pf.Segments))

			// Extract the slice of this part's bytes that belong to the aggregated file.
			sliced, covered, err := slicePartSegments(pf.Segments, part.DataOffset, part.PackedSize)
			if err != nil {
				rh.log.Warn("Failed slicing part segments", "error", err, "part_path", part.Path, "file", af.Name)
				continue
			}
			// Append maintaining order: parts order then segment order within part.
			fileSegments = append(fileSegments, sliced...)
			accumulated += covered

			rh.log.Debug("Part processed",
				"file", af.Name,
				"part_index", partIdx,
				"covered", covered,
				"accumulated", accumulated,
				"sliced_segments", len(sliced))

			if covered != part.PackedSize {
				rh.log.Warn("Part coverage mismatch", "file", af.Name, "part_index", partIdx, "expected", part.PackedSize, "covered", covered, "data_offset", part.DataOffset)
			}
		}

		// Validation: sum of trimmed segment lengths should match total packed size.
		var sum int64
		for _, s := range fileSegments {
			sum += (s.EndOffset - s.StartOffset + 1)
		}

		rh.log.Debug("File conversion complete",
			"name", af.Name,
			"total_packed_size", af.TotalPackedSize,
			"segments_sum", sum,
			"accumulated", accumulated,
			"total_segments", len(fileSegments))

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
	// Handle filenames with brackets like [PRiVATE]-[WtFnZb]-[actual.file.r00]
	// Extract the last bracketed section if it contains a RAR extension
	if lastBracket := strings.LastIndex(filename, "["); lastBracket >= 0 {
		if closeBracket := strings.Index(filename[lastBracket:], "]"); closeBracket >= 0 {
			innerFilename := filename[lastBracket+1 : lastBracket+closeBracket]
			// Check if this looks like a RAR filename
			lowerInner := strings.ToLower(innerFilename)
			if strings.HasSuffix(lowerInner, ".rar") ||
				strings.Contains(lowerInner, ".r0") ||
				strings.Contains(lowerInner, ".r1") ||
				strings.Contains(lowerInner, ".part") {
				filename = innerFilename
			}
		}
	}

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

func renameRarFilesAndSort(rarFiles []ParsedFile, log *slog.Logger) []ParsedFile {
	// Get the base name of the first RAR file (without extension)
	// We need to use the original suffix (with leading zeros) to properly extract the base name
	firstFileBase := extractBaseFilename(rarFiles[0].Filename)
	
	log.Debug("Normalizing RAR filenames",
		"base_extracted", firstFileBase,
		"original_first_file", rarFiles[0].Filename)

	// Rename all RAR files to match the base name of the first file while preserving original part naming
	for i := range rarFiles {
		originalFileName := rarFiles[i].Filename

		// Try to extract the part suffix from the original filename
		partSuffix := getPartSuffix(originalFileName)

		// Construct new filename with first file's base name and original part suffix
		newFilename := firstFileBase + partSuffix
		
		// Strip any trailing ] bracket (handles orphan brackets like "file.r20]")
		// This is a final cleanup for partial bracket corruption
		newFilename = strings.TrimSuffix(newFilename, "]")
		
		// Log if we're actually normalizing the filename (removing brackets, etc.)
		if originalFileName != newFilename {
			log.Debug("Normalized RAR filename",
				"original", originalFileName,
				"normalized", newFilename)
		}
		
		rarFiles[i].Filename = newFilename
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
	filename := originalFileName
	
	// Handle filenames with brackets like [PRiVATE]-[WtFnZb]-[actual.file.r00]
	// Extract the last bracketed section if it contains a RAR extension
	if lastBracket := strings.LastIndex(filename, "["); lastBracket >= 0 {
		if closeBracket := strings.Index(filename[lastBracket:], "]"); closeBracket >= 0 {
			innerFilename := filename[lastBracket+1 : lastBracket+closeBracket]
			// Check if this looks like a RAR filename
			lowerInner := strings.ToLower(innerFilename)
			if strings.HasSuffix(lowerInner, ".rar") ||
				strings.Contains(lowerInner, ".r0") ||
				strings.Contains(lowerInner, ".r1") ||
				strings.Contains(lowerInner, ".part") {
				filename = innerFilename
			}
		}
	}
	
	if matches := partPatternNumber.FindStringSubmatch(filename); len(matches) > 1 {
		return fmt.Sprintf(".part%s.rar", stripLeadingZeros(matches[1]))
	} else if matches := rPatternNumber.FindStringSubmatch(filename); len(matches) > 1 {
		return fmt.Sprintf(".r%s", matches[1])
	} else if matches := numericPatternNumber.FindStringSubmatch(filename); len(matches) > 1 {
		return fmt.Sprintf(".%s", matches[1])
	}

	return filepath.Ext(filename)
}

// extractRarPartNumber extracts numeric part from RAR extension for sorting
func extractRarPartNumber(fileName string) int {
	partNumber := getPartNumber(fileName)
	if partNumber > 0 {
		return partNumber
	}

	return 999999 // Unknown format goes last
}
