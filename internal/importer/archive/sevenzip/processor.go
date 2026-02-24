package sevenzip

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"log/slog"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/javi11/altmount/internal/errors"
	"github.com/javi11/altmount/internal/importer/archive"
	"github.com/javi11/altmount/internal/importer/filesystem"
	"github.com/javi11/altmount/internal/importer/parser"
	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/javi11/altmount/internal/pool"
	"github.com/javi11/altmount/internal/progress"
	"github.com/javi11/rardecode/v2"
	"github.com/javi11/sevenzip"
	"golang.org/x/text/encoding/unicode"
	"golang.org/x/text/transform"
)

// sevenZipProcessor handles 7zip archive analysis and content extraction
type sevenZipProcessor struct {
	log         *slog.Logger
	poolManager pool.Manager
	maxPrefetch int
	readTimeout time.Duration
}

// NewProcessor creates a new 7zip processor
func NewProcessor(poolManager pool.Manager, maxPrefetch int, readTimeout time.Duration) Processor {
	return &sevenZipProcessor{
		log:         slog.Default().With("component", "7z-processor"),
		poolManager: poolManager,
		maxPrefetch: maxPrefetch,
		readTimeout: readTimeout,
	}
}

// Pre-compiled regex patterns for 7zip file detection and sorting
var (
	// Pattern for multi-part 7zip: filename.7z.001, filename.7z.002
	sevenZipPartPattern = regexp.MustCompile(`^(.+)\.7z\.(\d+)$`)
	// Pattern for extracting just the number from .7z.001
	sevenZipPartNumberPattern = regexp.MustCompile(`\.7z\.(\d+)$`)
)

// Pre-compiled regex patterns for nested RAR detection (duplicated from rar package since unexported)
var (
	rarPartPattern    = regexp.MustCompile(`^(.+)\.part(\d+)\.rar$`)
	rarRPattern       = regexp.MustCompile(`^(.+)\.r(\d+)$`)
	rarNumericPattern = regexp.MustCompile(`^(.+)\.(\d+)$`)
)

// CreateFileMetadataFromSevenZipContent creates FileMetadata from SevenZipContent for the metadata system
func (sz *sevenZipProcessor) CreateFileMetadataFromSevenZipContent(
	content Content,
	sourceNzbPath string,
	releaseDate int64,
	nzbdavId string,
) *metapb.FileMetadata {
	now := time.Now().Unix()

	meta := &metapb.FileMetadata{
		FileSize:      content.Size,
		SourceNzbPath: sourceNzbPath,
		Status:        metapb.FileStatus_FILE_STATUS_HEALTHY,
		CreatedAt:     now,
		ModifiedAt:    now,
		SegmentData:   content.Segments,
		ReleaseDate:   releaseDate,
		NzbdavId:      nzbdavId,
	}

	// Set AES encryption if keys are present
	if len(content.AesKey) > 0 {
		meta.Encryption = metapb.Encryption_AES
		meta.AesKey = content.AesKey
		meta.AesIv = content.AesIV
	}

	// Populate nested sources for encrypted nested RAR files
	for _, ns := range content.NestedSources {
		meta.NestedSources = append(meta.NestedSources, &metapb.NestedSegmentSource{
			Segments:        ns.Segments,
			AesKey:          ns.AesKey,
			AesIv:           ns.AesIV,
			InnerOffset:     ns.InnerOffset,
			InnerLength:     ns.InnerLength,
			InnerVolumeSize: ns.InnerVolumeSize,
		})
	}

	return meta
}

// deriveAESKey derives the AES encryption key from a password using the 7-zip algorithm
func (sz *sevenZipProcessor) deriveAESKey(password string, fileInfo sevenzip.FileInfo) ([]byte, error) {
	// Build the input for hashing: salt + password (UTF-16LE)
	b := bytes.NewBuffer(fileInfo.AESSalt)

	// Convert password to UTF-16LE
	utf16le := unicode.UTF16(unicode.LittleEndian, unicode.IgnoreBOM)
	t := transform.NewWriter(b, utf16le.NewEncoder())
	if _, err := t.Write([]byte(password)); err != nil {
		return nil, fmt.Errorf("failed to encode password: %w", err)
	}

	// Calculate the key using SHA-256
	key := make([]byte, sha256.Size)

	if fileInfo.KDFIterations == 0 {
		// Special case: no hashing, use data directly (padded/truncated to 32 bytes)
		copy(key, b.Bytes())
	} else {
		// Apply SHA-256 hash in rounds
		h := sha256.New()
		for i := uint64(0); i < uint64(fileInfo.KDFIterations); i++ {
			h.Write(b.Bytes())
			binary.Write(h, binary.LittleEndian, i)
		}
		copy(key, h.Sum(nil))
	}

	return key, nil
}

// AnalyzeSevenZipContentFromNzb analyzes a 7zip archive directly from NZB data without downloading
// This implementation uses sevenzip with UsenetFileSystem to analyze 7z structure and stream data from Usenet
// Returns an array of files to be added to the metadata with all the info and segments for each file
func (sz *sevenZipProcessor) AnalyzeSevenZipContentFromNzb(ctx context.Context, sevenZipFiles []parser.ParsedFile, password string, progressTracker *progress.Tracker) ([]Content, error) {
	if sz.poolManager == nil {
		return nil, errors.NewNonRetryableError("no pool manager available", nil)
	}

	// Rename 7zip files to match the first file's base name and sort
	sortedFiles := renameSevenZipFilesAndSort(sevenZipFiles)

	// Create Usenet filesystem for 7zip access - this enables sevenzip to access
	// 7zip part files directly from Usenet without downloading
	ufs := filesystem.NewUsenetFileSystem(ctx, sz.poolManager, sortedFiles, sz.maxPrefetch, progressTracker, sz.readTimeout)

	// Extract filenames for first part detection
	fileNames := make([]string, len(sortedFiles))
	for i, file := range sortedFiles {
		fileNames[i] = file.Filename
	}

	// Find the first 7zip part using intelligent detection
	mainSevenZipFile, err := sz.getFirstSevenZipPart(fileNames)
	if err != nil {
		return nil, err
	}

	sz.log.InfoContext(ctx, "Starting 7zip analysis",
		"main_file", mainSevenZipFile,
		"total_parts", len(sortedFiles),
		"7z_files", len(sevenZipFiles),
		"has_password", password != "")

	// Create Afero adapter for the Usenet filesystem
	aferoFS := filesystem.NewAferoAdapter(ufs)

	// Check context before expensive archive open operation
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	// Open 7zip archive using OpenReaderWithPassword if password provided
	var reader *sevenzip.ReadCloser
	if password != "" {
		reader, err = sevenzip.OpenReaderWithPassword(mainSevenZipFile, password, aferoFS)
		if err != nil {
			return nil, errors.NewNonRetryableError("failed to open password-protected 7zip archive", err)
		}
		sz.log.InfoContext(ctx, "Using password to unlock 7zip archive")
	} else {
		reader, err = sevenzip.OpenReader(mainSevenZipFile, aferoFS)
		if err != nil {
			return nil, errors.NewNonRetryableError("failed to open 7zip archive", err)
		}
	}
	defer reader.Close()

	// List files with their offsets
	fileInfos, err := reader.ListFilesWithOffsets()
	if err != nil {
		return nil, errors.NewNonRetryableError("failed to list files in 7zip archive", err)
	}

	if len(fileInfos) == 0 {
		return nil, errors.NewNonRetryableError("no valid files found in 7zip archive. Compressed or encrypted archives are not supported", nil)
	}

	sz.log.DebugContext(ctx, "Successfully analyzed 7zip archive",
		"main_file", mainSevenZipFile,
		"files_found", len(fileInfos))

	// Check context before conversion phase
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	// Convert sevenzip FileInfo results to Content
	// Note: AES credentials are extracted per-file, not per-archive
	contents, err := sz.convertFileInfosToSevenZipContent(fileInfos, sevenZipFiles, password)
	if err != nil {
		return nil, errors.NewNonRetryableError("failed to convert 7zip results to content", err)
	}

	// Verify we have valid files after filtering
	if len(contents) == 0 {
		return nil, errors.NewNonRetryableError("no valid files found in 7zip archive after filtering. Only uncompressed files are supported", nil)
	}

	// Check for nested RAR archives and process them
	contents, err = sz.detectAndProcessNestedRars(ctx, contents)
	if err != nil {
		return nil, errors.NewNonRetryableError("failed to process nested RAR archives", err)
	}

	return contents, nil
}

// getFirstSevenZipPart finds and returns the filename of the first part of a 7zip archive
// This method prioritizes .7z files over .7z.001 files
func (sz *sevenZipProcessor) getFirstSevenZipPart(sevenZipFileNames []string) (string, error) {
	if len(sevenZipFileNames) == 0 {
		return "", errors.NewNonRetryableError("no 7zip files provided", nil)
	}

	// If only one file, return it
	if len(sevenZipFileNames) == 1 {
		return sevenZipFileNames[0], nil
	}

	// Group files by base name and find first parts
	type candidateFile struct {
		filename string
		baseName string
		partNum  int
		priority int // Lower number = higher priority
	}

	var candidates []candidateFile

	for _, filename := range sevenZipFileNames {
		base, part := sz.parseSevenZipFilename(filename)

		// Only consider files that are actually first parts (part 0)
		if part != 0 {
			continue
		}

		// Determine priority based on file extension pattern
		priority := sz.getSevenZipFilePriority(filename)

		candidates = append(candidates, candidateFile{
			filename: filename,
			baseName: base,
			partNum:  part,
			priority: priority,
		})
	}

	if len(candidates) == 0 {
		return "", errors.NewNonRetryableError("no valid first 7zip part found in archive", nil)
	}

	// Sort by priority (lower number = higher priority), then by filename for consistency
	best := candidates[0]
	for _, candidate := range candidates[1:] {
		if candidate.priority < best.priority ||
			(candidate.priority == best.priority && candidate.filename < best.filename) {
			best = candidate
		}
	}

	sz.log.DebugContext(context.Background(), "Selected first 7zip part",
		"filename", best.filename,
		"base_name", best.baseName,
		"priority", best.priority,
		"total_candidates", len(candidates))

	return best.filename, nil
}

// getSevenZipFilePriority returns the priority for different 7zip file types
// Lower number = higher priority
func (sz *sevenZipProcessor) getSevenZipFilePriority(filename string) int {
	lowerName := strings.ToLower(filename)

	// Priority 1: .7z files (main archive)
	if strings.HasSuffix(lowerName, ".7z") && !strings.Contains(lowerName, ".7z.") {
		return 1
	}

	// Priority 2: .7z.001 patterns (first part of multi-part)
	if strings.Contains(lowerName, ".7z.") {
		return 2
	}

	// Priority 3: Everything else
	return 3
}

// parseSevenZipFilename extracts base name and part number from 7zip filename
func (sz *sevenZipProcessor) parseSevenZipFilename(filename string) (base string, part int) {
	lowerFilename := strings.ToLower(filename)

	// Pattern 1: filename.7z.001, filename.7z.002 (multi-part)
	if matches := sevenZipPartPattern.FindStringSubmatch(filename); len(matches) > 2 {
		base = matches[1]
		if partNum := archive.ParseInt(matches[2]); partNum >= 0 {
			// Convert 1-based part numbers to 0-based (001 becomes 0, 002 becomes 1)
			if partNum > 0 {
				part = partNum - 1
			}
			return base, part
		}
	}

	// Pattern 2: filename.7z (single archive)
	if strings.HasSuffix(lowerFilename, ".7z") {
		base = strings.TrimSuffix(filename, filepath.Ext(filename))
		return base, 0 // First part
	}

	// Unknown pattern - return filename as base with high part number (sorts last)
	return filename, 999999
}

// convertFileInfosToSevenZipContent converts sevenzip FileInfo results to Content
// Note: AES credentials are extracted per-file from each file's encryption metadata
func (sz *sevenZipProcessor) convertFileInfosToSevenZipContent(fileInfos []sevenzip.FileInfo, sevenZipFiles []parser.ParsedFile, password string) ([]Content, error) {
	out := make([]Content, 0, len(fileInfos))

	for _, fi := range fileInfos {
		// Skip directories (7zip lists directories as files with trailing slash)
		isDirectory := strings.HasSuffix(fi.Name, "/") || fi.Size == 0
		if isDirectory {
			sz.log.DebugContext(context.Background(), "Skipping directory in 7zip archive", "path", fi.Name)
			continue
		}

		// Skip compressed files - they cannot be directly streamed
		if fi.Compressed {
			sz.log.WarnContext(context.Background(), "Skipping compressed file in 7zip archive (compression not supported)", "path", fi.Name)
			continue
		}

		// Normalize backslashes in path (Windows-style paths in 7zip archives)
		normalizedName := strings.ReplaceAll(fi.Name, "\\", "/")

		// Extract AES credentials from this file's encryption metadata (if encrypted)
		// Each file can have its own encryption credentials
		var aesKey, aesIV []byte
		if password != "" && fi.Encrypted && len(fi.AESIV) > 0 {
			aesIV = fi.AESIV
			// Derive the AES key from the password using the 7-zip algorithm
			derivedKey, err := sz.deriveAESKey(password, fi)
			if err != nil {
				sz.log.WarnContext(context.Background(), "Failed to derive AES key for file",
					"file", normalizedName,
					"error", err)
				continue
			}
			aesKey = derivedKey
		}

		// Extract ID from the first part of the archive
		var nzbdavID string
		if len(sevenZipFiles) > 0 {
			nzbdavID = sevenZipFiles[0].NzbdavID
		}

		content := Content{
			InternalPath: normalizedName,
			Filename:     filepath.Base(normalizedName),
			Size:         int64(fi.Size),
			IsDirectory:  isDirectory,
			AesKey:       aesKey,
			AesIV:        aesIV,
			NzbdavID:     nzbdavID,
		}

		// Map the file's offset and size to segments from the 7z parts
		segments, err := sz.mapOffsetToSegments(fi, sevenZipFiles)
		if err != nil {
			sz.log.WarnContext(context.Background(), "Failed to map segments for file", "error", err, "file", fi.Name)
			continue
		}

		content.Segments = segments
		out = append(out, content)
	}

	return out, nil
}

// mapOffsetToSegments maps a file's offset within the 7z archive to Usenet segments
func (sz *sevenZipProcessor) mapOffsetToSegments(
	fi sevenzip.FileInfo,
	sevenZipFiles []parser.ParsedFile,
) ([]*metapb.SegmentData, error) {
	// The FileInfo provides:
	// - Offset: where the file data starts in the archive
	// - Size: the size of the file data
	// - FolderIndex: which folder/stream contains this data

	// For multi-part archives, we need to figure out which part contains the data
	// For now, we'll assume single-part or that the data is contiguous
	// This is a simplified implementation - a full implementation would need to handle
	// data spanning multiple archive parts

	var allSegments []*metapb.SegmentData
	var totalSize int64

	// Collect all segments from all 7z parts in order
	for _, szFile := range sevenZipFiles {
		for _, seg := range szFile.Segments {
			allSegments = append(allSegments, seg)
			totalSize += (seg.EndOffset - seg.StartOffset + 1)
		}
	}

	// Now slice the segments to cover [offset, offset + size]
	offset := int64(fi.Offset)
	size := int64(fi.Size)

	// For AES-encrypted files, the data in the archive is padded to 16-byte blocks.
	// We need to include the padding bytes in our segment mapping so the AES decrypt
	// reader can read the complete encrypted data.
	if fi.Encrypted && len(fi.AESIV) > 0 {
		const aesBlockSize = 16
		if size%aesBlockSize != 0 {
			size = size + (aesBlockSize - (size % aesBlockSize))
		}
	}

	slicedSegments, covered, err := sliceSegmentsForRange(allSegments, offset, size)
	if err != nil {
		return nil, fmt.Errorf("failed to slice segments: %w", err)
	}

	if covered != size {
		sz.log.WarnContext(context.Background(), "Segment coverage mismatch",
			"file", fi.Name,
			"expected", size,
			"covered", covered,
			"offset", offset)
	}

	return slicedSegments, nil
}

// sliceSegmentsForRange returns the slice of segment ranges covering [offset, offset+size-1]
// This is similar to slicePartSegments in rar_processor.go
func sliceSegmentsForRange(segments []*metapb.SegmentData, offset int64, size int64) ([]*metapb.SegmentData, int64, error) {
	if size <= 0 {
		return nil, 0, nil
	}
	if offset < 0 {
		return nil, 0, errors.NewNonRetryableError("negative offset", nil)
	}

	targetStart := offset
	targetEnd := offset + size - 1
	var covered int64
	out := []*metapb.SegmentData{}

	// cumulative absolute position across all segments
	var absPos int64
	for _, seg := range segments {
		segSize := (seg.EndOffset - seg.StartOffset + 1)
		if segSize <= 0 {
			continue
		}
		segAbsStart := absPos
		segAbsEnd := absPos + segSize - 1

		// If segment ends before target range starts, skip
		if segAbsEnd < targetStart {
			absPos += segSize
			continue
		}
		// If segment starts after target range ends, we can stop
		if segAbsStart > targetEnd {
			break
		}

		// Calculate overlap
		overlapStart := max(segAbsStart, targetStart)
		overlapEnd := min(segAbsEnd, targetEnd)

		if overlapEnd >= overlapStart {
			// Translate back to segment-relative offsets
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

			if overlapEnd == targetEnd {
				break
			}
		}
		absPos += segSize
	}

	return out, covered, nil
}

// extractBaseFilename extracts the base filename without the part suffix
func extractBaseFilenameSevenZip(filename string) string {
	// Try the part pattern
	if matches := sevenZipPartPattern.FindStringSubmatch(filename); len(matches) > 1 {
		return matches[1]
	}

	// If no pattern matches, return the filename without extension
	return strings.TrimSuffix(filename, filepath.Ext(filename))
}

// renameSevenZipFilesAndSort renames all 7z files to have the same base name and sorts them
func renameSevenZipFilesAndSort(sevenZipFiles []parser.ParsedFile) []parser.ParsedFile {
	// Check if ALL files have no extension - if so, we'll add .XXX extensions
	allFilesNoExt := true
	for _, file := range sevenZipFiles {
		if hasExtension(file.Filename) {
			allFilesNoExt = false
			break
		}
	}

	// Get base filename from first file if all files have no extension
	baseFilename := ""
	if allFilesNoExt {
		// Sort files alphabetically to ensure consistent base filename selection
		sort.Slice(sevenZipFiles, func(i, j int) bool {
			return sevenZipFiles[i].Filename < sevenZipFiles[j].Filename
		})
		// Use the first file's name as the base for all parts
		if len(sevenZipFiles) > 0 {
			baseFilename = sevenZipFiles[0].Filename
		}
	} else {
		// Sort files by part number BEFORE extracting base filename
		// This ensures we use the correct first part's base name
		sort.Slice(sevenZipFiles, func(i, j int) bool {
			partI := extractSevenZipPartNumber(sevenZipFiles[i].Filename)
			partJ := extractSevenZipPartNumber(sevenZipFiles[j].Filename)
			return partI < partJ
		})

		// Get the base name of the first 7zip file (for existing extension handling)
		firstFileBase := extractBaseFilenameSevenZip(sevenZipFiles[0].Filename)

		// Rename all files to match the base name of the first file while preserving original part naming
		for i := range sevenZipFiles {
			originalFileName := sevenZipFiles[i].Filename

			// Try to extract the part suffix from the original filename
			partSuffix := getPartSuffixSevenZip(originalFileName)

			// Construct new filename with first file's base name and original part suffix
			sevenZipFiles[i].Filename = firstFileBase + partSuffix
		}
	}

	// Apply normalization with unified base filename support
	normalizedFiles := make([]parser.ParsedFile, len(sevenZipFiles))
	for i, file := range sevenZipFiles {
		normalizedFiles[i] = file
		// Use OriginalIndex to preserve part numbering from original NZB order
		// Pass total file count for zero-padding and base filename for unified naming
		normalizedFiles[i].Filename = normalize7zPartFilename(file.Filename, file.OriginalIndex, allFilesNoExt, len(sevenZipFiles), baseFilename)
	}

	// Sort files by part number
	sort.Slice(normalizedFiles, func(i, j int) bool {
		partI := extractSevenZipPartNumber(normalizedFiles[i].Filename)
		partJ := extractSevenZipPartNumber(normalizedFiles[j].Filename)
		return partI < partJ
	})

	return normalizedFiles
}

// getPartSuffixSevenZip extracts the part suffix from a 7z filename
func getPartSuffixSevenZip(originalFileName string) string {
	if matches := sevenZipPartNumberPattern.FindStringSubmatch(originalFileName); len(matches) > 1 {
		// Keep the original number format with leading zeros (e.g., .001 stays .001)
		return fmt.Sprintf(".7z.%s", matches[1])
	}

	return filepath.Ext(originalFileName)
}

// extractSevenZipPartNumber extracts numeric part from 7z extension for sorting
func extractSevenZipPartNumber(fileName string) int {
	if matches := sevenZipPartNumberPattern.FindStringSubmatch(fileName); len(matches) > 1 {
		if partNum := archive.ParseInt(matches[1]); partNum > 0 {
			return partNum
		}
	}

	// If it's a .7z file (no part number), it's the first part
	if strings.HasSuffix(strings.ToLower(fileName), ".7z") {
		return 0
	}

	return 999999 // Unknown format goes last
}

// --- Nested RAR detection and processing ---

// isRarArchiveFile checks if a filename looks like a RAR archive file
// (single volume or any part of a multi-volume set).
func isRarArchiveFile(filename string) bool {
	lower := strings.ToLower(filename)
	if strings.HasSuffix(lower, ".rar") {
		return true
	}
	if rarRPattern.MatchString(lower) {
		return true
	}
	return false
}

// parseRarFilename extracts base name and part number from RAR filename.
func parseRarFilename(filename string) (base string, part int) {
	lowerFilename := strings.ToLower(filename)

	// Pattern 1: filename.part###.rar (e.g., movie.part001.rar)
	if matches := rarPartPattern.FindStringSubmatch(filename); len(matches) > 2 {
		base = matches[1]
		if partNum := archive.ParseInt(matches[2]); partNum >= 0 {
			if partNum > 0 {
				part = partNum - 1
			}
			return base, part
		}
	}

	// Pattern 2: filename.rar (first part)
	if strings.HasSuffix(lowerFilename, ".rar") {
		base = strings.TrimSuffix(filename, filepath.Ext(filename))
		return base, 0
	}

	// Pattern 3: filename.r## or filename.r### (e.g., movie.r00)
	if matches := rarRPattern.FindStringSubmatch(filename); len(matches) > 2 {
		base = matches[1]
		if partNum := archive.ParseInt(matches[2]); partNum >= 0 {
			return base, partNum
		}
	}

	// Pattern 4: filename.### (numeric extensions like .001, .002)
	if matches := rarNumericPattern.FindStringSubmatch(filename); len(matches) > 2 {
		base = matches[1]
		if partNum := archive.ParseInt(matches[2]); partNum >= 0 {
			if partNum > 0 {
				part = partNum - 1
			}
			return base, part
		}
	}

	return filename, 999999
}

// getRarFilePriority returns the priority for different RAR file types.
// Lower number = higher priority.
func getRarFilePriority(filename string) int {
	lowerName := strings.ToLower(filename)

	if strings.HasSuffix(lowerName, ".rar") && !strings.Contains(lowerName, ".part") {
		return 1
	}
	if strings.Contains(lowerName, ".part") && strings.HasSuffix(lowerName, ".rar") {
		return 2
	}
	if strings.Contains(lowerName, ".r0") {
		return 3
	}
	if len(lowerName) > 4 && lowerName[len(lowerName)-4:len(lowerName)-3] == "." {
		return 4
	}
	return 5
}

// getFirstRarPart finds the filename of the first part of a nested RAR archive.
func (sz *sevenZipProcessor) getFirstRarPart(rarFileNames []string) (string, error) {
	if len(rarFileNames) == 0 {
		return "", errors.NewNonRetryableError("no RAR files provided", nil)
	}

	if len(rarFileNames) == 1 {
		return rarFileNames[0], nil
	}

	type candidateFile struct {
		filename string
		baseName string
		priority int
	}

	var candidates []candidateFile

	for _, filename := range rarFileNames {
		base, part := parseRarFilename(filename)
		if part != 0 {
			continue
		}
		priority := getRarFilePriority(filename)
		candidates = append(candidates, candidateFile{
			filename: filename,
			baseName: base,
			priority: priority,
		})
	}

	if len(candidates) == 0 {
		return "", errors.NewNonRetryableError("no valid first RAR part found in archive", nil)
	}

	best := candidates[0]
	for _, candidate := range candidates[1:] {
		if candidate.priority < best.priority ||
			(candidate.priority == best.priority && candidate.filename < best.filename) {
			best = candidate
		}
	}

	return best.filename, nil
}

// patchMissingSegment duplicates the last segment to fill a small gap in coverage.
func patchMissingSegment(segments []*metapb.SegmentData, expectedSize, coveredSize int64) ([]*metapb.SegmentData, int64, error) {
	shortfall := expectedSize - coveredSize
	if shortfall <= 0 {
		return segments, coveredSize, nil
	}

	const maxSingleSegmentSize = 800000 // ~800KB, typical segment is ~768KB

	if shortfall > maxSingleSegmentSize {
		return nil, 0, errors.NewNonRetryableError(
			fmt.Sprintf("missing %d bytes exceeds single segment threshold (%d bytes), cannot auto-patch", shortfall, maxSingleSegmentSize), nil)
	}

	if len(segments) == 0 {
		return nil, 0, errors.NewNonRetryableError("no segments available to duplicate for patching", nil)
	}

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

// detectAndProcessNestedRars checks if any outer 7zip Contents are themselves RAR archives.
// If so, it analyzes the inner RARs and replaces them with their extracted contents.
// Non-RAR contents are passed through unchanged.
func (sz *sevenZipProcessor) detectAndProcessNestedRars(ctx context.Context, outerContents []Content) ([]Content, error) {
	var nonRarContents []Content
	rarContentsByBase := make(map[string][]Content)

	for _, c := range outerContents {
		if c.IsDirectory || !isRarArchiveFile(c.Filename) {
			nonRarContents = append(nonRarContents, c)
			continue
		}
		base, _ := parseRarFilename(c.Filename)
		rarContentsByBase[base] = append(rarContentsByBase[base], c)
	}

	if len(rarContentsByBase) == 0 {
		return outerContents, nil
	}

	sz.log.InfoContext(ctx, "Detected nested RAR archives inside 7zip",
		"rar_groups", len(rarContentsByBase),
		"non_rar_files", len(nonRarContents))

	var result []Content
	result = append(result, nonRarContents...)

	for base, innerRarContents := range rarContentsByBase {
		// Sort by part number for correct volume order
		slices.SortFunc(innerRarContents, func(a, b Content) int {
			_, partA := parseRarFilename(a.Filename)
			_, partB := parseRarFilename(b.Filename)
			return partA - partB
		})

		innerFiles, err := sz.processNestedRarContent(ctx, innerRarContents)
		if err != nil {
			sz.log.WarnContext(ctx, "Failed to process nested RAR in 7zip, keeping original",
				"base", base, "error", err)
			result = append(result, innerRarContents...)
			continue
		}

		if len(innerFiles) == 0 {
			sz.log.WarnContext(ctx, "Nested RAR in 7zip contained no files, keeping original",
				"base", base)
			result = append(result, innerRarContents...)
			continue
		}

		result = append(result, innerFiles...)
	}

	return result, nil
}

// processNestedRarContent analyzes inner RAR volumes and maps their files back
// to outer 7zip segments. For unencrypted outer 7zips, it flattens segments directly.
// For encrypted outer 7zips, it creates NestedSource entries.
func (sz *sevenZipProcessor) processNestedRarContent(ctx context.Context, innerRarContents []Content) ([]Content, error) {
	// Determine if outer 7zip is encrypted (check first volume)
	outerEncrypted := len(innerRarContents[0].AesKey) > 0

	// Build DecryptingFileEntry for each inner RAR volume
	entries := make([]filesystem.DecryptingFileEntry, 0, len(innerRarContents))
	for _, c := range innerRarContents {
		decryptedSize := c.PackedSize
		if decryptedSize == 0 {
			decryptedSize = c.Size
		}
		if outerEncrypted {
			decryptedSize = c.Size
		}

		entries = append(entries, filesystem.DecryptingFileEntry{
			Filename:      c.Filename,
			Segments:      c.Segments,
			DecryptedSize: decryptedSize,
			AesKey:        c.AesKey,
			AesIV:         c.AesIV,
		})
	}

	// Create filesystem for reading inner RAR volumes
	dfs := filesystem.NewDecryptingFileSystem(ctx, sz.poolManager, entries, sz.maxPrefetch, sz.readTimeout)

	// Find the first inner RAR part
	fileNames := make([]string, len(innerRarContents))
	for i, c := range innerRarContents {
		fileNames[i] = c.Filename
	}

	mainRarFile, err := sz.getFirstRarPart(fileNames)
	if err != nil {
		return nil, fmt.Errorf("failed to find first inner RAR part: %w", err)
	}

	sz.log.InfoContext(ctx, "Analyzing inner RAR archive from 7zip",
		"main_file", mainRarFile,
		"volumes", len(innerRarContents),
		"outer_encrypted", outerEncrypted)

	// Analyze inner RAR (no password — inner RAR is unencrypted)
	opts := []rardecode.Option{rardecode.FileSystem(dfs), rardecode.SkipCheck}
	if len(innerRarContents) > 1 {
		opts = append(opts, rardecode.ParallelRead(true))
	}

	aggregatedFiles, err := rardecode.ListArchiveInfo(mainRarFile, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to analyze inner RAR: %w", err)
	}

	if len(aggregatedFiles) == 0 {
		return nil, nil
	}

	// Validate inner files are stored (uncompressed)
	for _, file := range aggregatedFiles {
		if file.Compressed {
			compressionInfo := ""
			if file.CompressionMethod != "" {
				compressionInfo = fmt.Sprintf(" (uses %s compression)", file.CompressionMethod)
			}
			return nil, errors.NewNonRetryableError(
				fmt.Sprintf("compressed files are not supported in nested RAR: %s%s", file.Name, compressionInfo),
				nil,
			)
		}
	}

	// Build index of inner RAR volumes by filename for quick lookup
	innerVolumeIndex := make(map[string]*Content, len(innerRarContents))
	for i := range innerRarContents {
		c := &innerRarContents[i]
		innerVolumeIndex[c.Filename] = c
		innerVolumeIndex[filepath.Base(c.Filename)] = c
	}

	// Map inner files to outer segments
	var result []Content
	for _, af := range aggregatedFiles {
		normalizedName := strings.ReplaceAll(af.Name, "\\", "/")

		if outerEncrypted {
			content, err := sz.mapNestedFileEncrypted(ctx, af, normalizedName, innerVolumeIndex)
			if err != nil {
				sz.log.WarnContext(ctx, "Failed to map nested file (encrypted)", "file", af.Name, "error", err)
				continue
			}
			result = append(result, content)
		} else {
			content, err := sz.mapNestedFileFlat(ctx, af, normalizedName, innerVolumeIndex)
			if err != nil {
				sz.log.WarnContext(ctx, "Failed to map nested file (flat)", "file", af.Name, "error", err)
				continue
			}
			result = append(result, content)
		}
	}

	return result, nil
}

// mapNestedFileFlat maps an inner file to flattened outer segments (unencrypted outer 7zip).
func (sz *sevenZipProcessor) mapNestedFileFlat(ctx context.Context, af rardecode.ArchiveFileInfo, normalizedName string, innerVolumeIndex map[string]*Content) (Content, error) {
	rc := Content{
		InternalPath: normalizedName,
		Filename:     filepath.Base(normalizedName),
		Size:         af.TotalUnpackedSize,
		PackedSize:   af.TotalPackedSize,
	}

	var fileSegments []*metapb.SegmentData

	for _, part := range af.Parts {
		if part.PackedSize <= 0 {
			continue
		}

		outerContent := innerVolumeIndex[part.Path]
		if outerContent == nil {
			outerContent = innerVolumeIndex[filepath.Base(part.Path)]
		}
		if outerContent == nil {
			sz.log.WarnContext(ctx, "Inner RAR volume not found", "part_path", part.Path, "file", af.Name)
			continue
		}

		// Slice outer segments at the inner file's data offset
		sliced, covered, err := sliceSegmentsForRange(outerContent.Segments, part.DataOffset, part.PackedSize)
		if err != nil {
			sz.log.ErrorContext(ctx, "Failed slicing nested part segments", "error", err, "part_path", part.Path, "file", af.Name)
			continue
		}

		sliced, covered, err = patchMissingSegment(sliced, part.PackedSize, covered)
		if err != nil {
			return Content{}, errors.NewNonRetryableError(
				fmt.Sprintf("incomplete nested NZB data for %s (part %s): %v", af.Name, filepath.Base(part.Path), err), nil)
		}

		_ = covered
		fileSegments = append(fileSegments, sliced...)
	}

	rc.Segments = fileSegments
	return rc, nil
}

// mapNestedFileEncrypted maps an inner file to NestedSource entries (encrypted outer 7zip).
// Each inner volume part becomes a separate NestedSource with its own AES credentials.
func (sz *sevenZipProcessor) mapNestedFileEncrypted(ctx context.Context, af rardecode.ArchiveFileInfo, normalizedName string, innerVolumeIndex map[string]*Content) (Content, error) {
	rc := Content{
		InternalPath: normalizedName,
		Filename:     filepath.Base(normalizedName),
		Size:         af.TotalUnpackedSize,
		PackedSize:   af.TotalPackedSize,
	}

	for _, part := range af.Parts {
		if part.PackedSize <= 0 {
			continue
		}

		outerContent := innerVolumeIndex[part.Path]
		if outerContent == nil {
			outerContent = innerVolumeIndex[filepath.Base(part.Path)]
		}
		if outerContent == nil {
			sz.log.WarnContext(ctx, "Inner RAR volume not found for encrypted nesting", "part_path", part.Path, "file", af.Name)
			continue
		}

		ns := NestedSource{
			Segments:        outerContent.Segments,
			AesKey:          outerContent.AesKey,
			AesIV:           outerContent.AesIV,
			InnerOffset:     part.DataOffset,
			InnerLength:     part.PackedSize,
			InnerVolumeSize: outerContent.Size,
		}

		rc.NestedSources = append(rc.NestedSources, ns)
	}

	return rc, nil
}
