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
	"sort"
	"strings"
	"time"

	"github.com/javi11/altmount/internal/importer/archive"
	"github.com/javi11/altmount/internal/importer/filesystem"
	"github.com/javi11/altmount/internal/importer/parser"
	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/javi11/altmount/internal/pool"
	"github.com/javi11/altmount/internal/progress"
	"github.com/javi11/sevenzip"
	"golang.org/x/text/encoding/unicode"
	"golang.org/x/text/transform"
)

// NewNonRetryableError creates a non-retryable error (defined here to avoid import cycles)
func NewNonRetryableError(message string, cause error) error {
	if cause != nil {
		return fmt.Errorf("%s: %w", message, cause)
	}
	return fmt.Errorf("%s", message)
}

// sevenZipProcessor handles 7zip archive analysis and content extraction
type sevenZipProcessor struct {
	log            *slog.Logger
	poolManager    pool.Manager
	maxWorkers     int
	maxCacheSizeMB int
	readTimeout    time.Duration
}

// NewProcessor creates a new 7zip processor
func NewProcessor(poolManager pool.Manager, maxWorkers int, maxCacheSizeMB int, readTimeout time.Duration) Processor {
	return &sevenZipProcessor{
		log:            slog.Default().With("component", "7z-processor"),
		poolManager:    poolManager,
		maxWorkers:     maxWorkers,
		maxCacheSizeMB: maxCacheSizeMB,
		readTimeout:    readTimeout,
	}
}

// Pre-compiled regex patterns for 7zip file detection and sorting
var (
	// Pattern for multi-part 7zip: filename.7z.001, filename.7z.002
	sevenZipPartPattern = regexp.MustCompile(`^(.+)\.7z\.(\d+)$`)
	// Pattern for extracting just the number from .7z.001
	sevenZipPartNumberPattern = regexp.MustCompile(`\.7z\.(\d+)$`)
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
		return nil, NewNonRetryableError("no pool manager available", nil)
	}

	// Rename 7zip files to match the first file's base name and sort
	sortedFiles := renameSevenZipFilesAndSort(sevenZipFiles)

	// Create Usenet filesystem for 7zip access - this enables sevenzip to access
	// 7zip part files directly from Usenet without downloading
	ufs := filesystem.NewUsenetFileSystem(ctx, sz.poolManager, sortedFiles, sz.maxWorkers, sz.maxCacheSizeMB, progressTracker, sz.readTimeout)

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

	// Open 7zip archive using OpenReaderWithPassword if password provided
	var reader *sevenzip.ReadCloser
	if password != "" {
		reader, err = sevenzip.OpenReaderWithPassword(mainSevenZipFile, password, aferoFS)
		if err != nil {
			return nil, NewNonRetryableError("failed to open password-protected 7zip archive", err)
		}
		sz.log.InfoContext(ctx, "Using password to unlock 7zip archive")
	} else {
		reader, err = sevenzip.OpenReader(mainSevenZipFile, aferoFS)
		if err != nil {
			return nil, NewNonRetryableError("failed to open 7zip archive", err)
		}
	}
	defer reader.Close()

	// List files with their offsets
	fileInfos, err := reader.ListFilesWithOffsets()
	if err != nil {
		return nil, NewNonRetryableError("failed to list files in 7zip archive", err)
	}

	if len(fileInfos) == 0 {
		return nil, NewNonRetryableError("no valid files found in 7zip archive. Compressed or encrypted archives are not supported", nil)
	}

	sz.log.DebugContext(ctx, "Successfully analyzed 7zip archive",
		"main_file", mainSevenZipFile,
		"files_found", len(fileInfos))

	// Convert sevenzip FileInfo results to Content
	// Note: AES credentials are extracted per-file, not per-archive
	contents, err := sz.convertFileInfosToSevenZipContent(fileInfos, sevenZipFiles, password)
	if err != nil {
		return nil, NewNonRetryableError("failed to convert 7zip results to content", err)
	}

	// Verify we have valid files after filtering
	if len(contents) == 0 {
		return nil, NewNonRetryableError("no valid files found in 7zip archive after filtering. Only uncompressed files are supported", nil)
	}

	return contents, nil
}

// getFirstSevenZipPart finds and returns the filename of the first part of a 7zip archive
// This method prioritizes .7z files over .7z.001 files
func (sz *sevenZipProcessor) getFirstSevenZipPart(sevenZipFileNames []string) (string, error) {
	if len(sevenZipFileNames) == 0 {
		return "", NewNonRetryableError("no 7zip files provided", nil)
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
		return "", NewNonRetryableError("no valid first 7zip part found in archive", nil)
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
		return nil, 0, NewNonRetryableError("negative offset", nil)
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
		overlapStart := segAbsStart
		if overlapStart < targetStart {
			overlapStart = targetStart
		}
		overlapEnd := segAbsEnd
		if overlapEnd > targetEnd {
			overlapEnd = targetEnd
		}

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
