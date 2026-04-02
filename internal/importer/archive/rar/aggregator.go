package rar

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	concpool "github.com/sourcegraph/conc/pool"

	"github.com/javi11/altmount/internal/encryption/aes"
	"github.com/javi11/altmount/internal/importer/archive"
	"github.com/javi11/altmount/internal/importer/archive/iso"
	"github.com/javi11/altmount/internal/importer/filesystem"
	"github.com/javi11/altmount/internal/importer/parser"
	"github.com/javi11/altmount/internal/importer/utils"
	"github.com/javi11/altmount/internal/importer/validation"
	"github.com/javi11/altmount/internal/metadata"
	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/javi11/altmount/internal/pool"
	"github.com/javi11/altmount/internal/progress"
)

var (
	// ErrNoAllowedFiles indicates that the archive contains no files matching allowed extensions
	ErrNoAllowedFiles = archive.ErrNoAllowedFiles
	// ErrNoFilesProcessed indicates that no files were successfully processed (all files failed validation)
	ErrNoFilesProcessed = archive.ErrNoFilesProcessed
)

// getContentSegmentCount delegates to archive.GetContentSegmentCount.
func getContentSegmentCount(content Content) int {
	return archive.GetContentSegmentCount(content)
}

// getContentSegments delegates to archive.GetContentSegments.
func getContentSegments(content Content) []*metapb.SegmentData {
	return archive.GetContentSegments(content)
}

// validateSegmentIntegrity delegates to archive.ValidateSegmentIntegrity.
func validateSegmentIntegrity(ctx context.Context, content Content) error {
	return archive.ValidateSegmentIntegrity(ctx, content)
}

// calculateSegmentsToValidate calculates the actual number of segments that will be validated
// based on the validation mode (full or sampling) and sample percentage.
// This mirrors the logic in usenet.ValidateSegmentAvailability which uses selectSegmentsForValidation.
func calculateSegmentsToValidate(rarContents []Content, samplePercentage int) int {
	total := 0
	for _, content := range rarContents {
		if content.IsDirectory {
			continue
		}

		segmentCount := getContentSegmentCount(content)
		if samplePercentage == 100 {
			// Full validation mode: all segments will be validated
			total += segmentCount
		} else {
			// Sampling mode: first 3 + last 2 + random middle samples
			// Minimum 5 segments always validated for statistical validity
			minSegments := 5
			if segmentCount <= minSegments {
				total += segmentCount
			} else {
				// Fixed segments: first 3 + last 2 = 5 segments
				fixedSegments := 5
				// Middle segments: based on sample percentage
				middleSegmentCount := segmentCount - fixedSegments
				sampledMiddle := (middleSegmentCount * samplePercentage) / 100
				total += fixedSegments + sampledMiddle
			}
		}
	}
	return total
}

// newErrNoAllowedFiles builds a descriptive error showing which extensions were found
// vs which are allowed, making it actionable when imports fail silently.
func newErrNoAllowedFiles(rarContents []Content, allowedExtensions []string) error {
	extSet := make(map[string]struct{})
	for _, c := range rarContents {
		if c.IsDirectory {
			continue
		}
		ext := strings.ToLower(filepath.Ext(c.Filename))
		if ext == "" {
			ext = "(no extension)"
		}
		extSet[ext] = struct{}{}
	}
	found := make([]string, 0, len(extSet))
	for ext := range extSet {
		found = append(found, ext)
	}
	return fmt.Errorf("archive contains no files with allowed extensions (found: %v, allowed: %v)", found, allowedExtensions)
}

// hasAllowedFiles checks if any files within RAR archive contents match allowed extensions
// If allowedExtensions is empty, all file types are allowed
func hasAllowedFiles(rarContents []Content, allowedExtensions []string, filterSamples bool) bool {
	for _, content := range rarContents {
		// Skip directories
		if content.IsDirectory {
			continue
		}
		// Check both the internal path and filename
		if utils.IsAllowedFile(content.InternalPath, content.Size, allowedExtensions, filterSamples) ||
			utils.IsAllowedFile(content.Filename, content.Size, allowedExtensions, filterSamples) {
			return true
		}
	}
	return false
}

// ProcessArchiveOptions holds all parameters for ProcessArchive.
type ProcessArchiveOptions struct {
	VirtualDir                string
	ArchiveFiles              []parser.ParsedFile
	Password                  string
	ReleaseDate               int64
	NzbPath                   string
	Processor                 Processor
	MetadataService           *metadata.MetadataService
	PoolManager               pool.Manager
	ArchiveProgressTracker    *progress.Tracker
	ValidationProgressTracker *progress.Tracker
	MaxValidationGoroutines   int
	SegmentSamplePercentage   int
	AllowedFileExtensions     []string
	Timeout                   time.Duration
	ExtractedFiles            []parser.ExtractedFileInfo
	MaxPrefetch               int
	ReadTimeout               time.Duration
	ExpandBlurayIso           bool
	FilterSamples             bool
	RenameToNzbName           bool
}

// ProcessArchive analyzes and processes RAR archive files, creating metadata for all extracted files.
// This function handles the complete workflow: analysis → file processing → metadata creation.
func ProcessArchive(ctx context.Context, opts ProcessArchiveOptions) error {
	archiveFiles := opts.ArchiveFiles
	virtualDir := opts.VirtualDir
	password := opts.Password
	releaseDate := opts.ReleaseDate
	nzbPath := opts.NzbPath
	rarProcessor := opts.Processor
	metadataService := opts.MetadataService
	poolManager := opts.PoolManager
	archiveProgressTracker := opts.ArchiveProgressTracker
	validationProgressTracker := opts.ValidationProgressTracker
	maxValidationGoroutines := opts.MaxValidationGoroutines
	segmentSamplePercentage := opts.SegmentSamplePercentage
	allowedFileExtensions := opts.AllowedFileExtensions
	timeout := opts.Timeout
	extractedFiles := opts.ExtractedFiles
	maxPrefetch := opts.MaxPrefetch
	readTimeout := opts.ReadTimeout
	expandBlurayIso := opts.ExpandBlurayIso
	filterSamples := opts.FilterSamples
	renameToNzbName := opts.RenameToNzbName

	if len(archiveFiles) == 0 {
		return nil
	}

	slog.InfoContext(ctx, "Analyzing RAR archive content", "parts", len(archiveFiles))

	// Analyze RAR content with timeout
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	// Group archive files by their base name to handle NZBs with multiple separate RAR archives
	archiveGroups := GroupArchivesByBaseName(archiveFiles)

	type groupResult struct {
		contents  []Content
		err       error
		firstName string
	}
	groupResults := make([]groupResult, len(archiveGroups))
	var wg sync.WaitGroup
	for i, group := range archiveGroups {
		wg.Add(1)
		go func(idx int, g []parser.ParsedFile) {
			defer wg.Done()
			groupContents, err := rarProcessor.AnalyzeRarContentFromNzb(ctx, g, password, archiveProgressTracker)
			groupResults[idx] = groupResult{groupContents, err, g[0].Filename}
		}(i, group)
	}
	wg.Wait()

	var rarContents []Content
	for _, r := range groupResults {
		if r.err != nil {
			slog.ErrorContext(ctx, "Failed to analyze RAR archive group", "error", r.err, "first_file", r.firstName)
			return r.err
		}
		rarContents = append(rarContents, r.contents...)
	}

	// Expand ISO files found inside the RAR archive into their inner media files
	rarContents, err := expandISOContents(ctx, expandBlurayIso, rarContents, poolManager, maxPrefetch, readTimeout, allowedFileExtensions)
	if err != nil {
		slog.WarnContext(ctx, "ISO expansion failed, proceeding without ISO contents", "error", err)
	}

	// Validate file extensions before processing
	if !hasAllowedFiles(rarContents, allowedFileExtensions, filterSamples) {
		err := newErrNoAllowedFiles(rarContents, allowedFileExtensions)
		slog.WarnContext(ctx, "RAR archive contains no files with allowed extensions", "error", err)
		return err
	}

	// Calculate total segments to validate for accurate progress tracking
	// This accounts for sampling mode if enabled
	totalSegmentsToValidate := calculateSegmentsToValidate(rarContents, segmentSamplePercentage)

	slog.InfoContext(ctx, "Starting RAR archive validation",
		"total_files", len(rarContents),
		"total_segments_to_validate", totalSegmentsToValidate,
		"sample_percentage", segmentSamplePercentage)

	// Determine if we should rename the file to match the NZB basename
	// Only do this if there's exactly one media file in the archive
	mediaFilesCount := 0
	for _, content := range rarContents {
		if !content.IsDirectory && (utils.IsAllowedFile(content.InternalPath, content.Size, allowedFileExtensions, filterSamples) ||
			utils.IsAllowedFile(content.Filename, content.Size, allowedFileExtensions, filterSamples)) {
			mediaFilesCount++
		}
	}

	nzbName := filepath.Base(nzbPath)
	releaseName := strings.TrimSuffix(nzbName, filepath.Ext(nzbName))
	shouldNormalizeName := renameToNzbName && mediaFilesCount == 1

	// Count ISO-expanded files so single-file ISOs omit the index suffix.
	isoExpandedCount := 0
	for _, c := range rarContents {
		if c.ISOExpansionIndex > 0 {
			isoExpandedCount++
		}
	}

	// Pre-pass: resolve paths, apply renames, and pre-compute per-file segment offsets so
	// each goroutine can build its own OffsetTracker without any sequential shared state.
	type fileToProcess struct {
		content         Content
		baseFilename    string
		virtualFilePath string
		isPreExtracted  bool
		segmentOffset   int
	}

	var filesToProcess []fileToProcess
	preProcessedCount := 0 // healthy files already counted as processed
	cumulativeOffset := 0

	for _, rarContent := range rarContents {
		if rarContent.IsDirectory {
			slog.DebugContext(ctx, "Skipping directory in RAR archive", "path", rarContent.InternalPath)
			continue
		}

		normalizedInternalPath := strings.ReplaceAll(rarContent.InternalPath, "\\", "/")
		baseFilename := filepath.Base(normalizedInternalPath)
		internalSubDir := filepath.ToSlash(filepath.Dir(normalizedInternalPath))

		if !utils.IsAllowedFile(rarContent.InternalPath, rarContent.Size, allowedFileExtensions, filterSamples) &&
			!utils.IsAllowedFile(rarContent.Filename, rarContent.Size, allowedFileExtensions, filterSamples) {
			continue
		}

		if rarContent.ISOExpansionIndex > 0 {
			ext := filepath.Ext(rarContent.Filename)
			if isoExpandedCount == 1 {
				baseFilename = releaseName + ext
			} else {
				baseFilename = fmt.Sprintf("%s_%d%s", releaseName, rarContent.ISOExpansionIndex, ext)
			}
			slog.InfoContext(ctx, "Renaming ISO-expanded file using NZB release name",
				"original", rarContent.Filename,
				"renamed", baseFilename)
			internalSubDir = "."
		} else if shouldNormalizeName && (utils.IsAllowedFile(rarContent.InternalPath, rarContent.Size, allowedFileExtensions, filterSamples) ||
			utils.IsAllowedFile(rarContent.Filename, rarContent.Size, allowedFileExtensions, filterSamples)) {
			baseFilename = normalizeArchiveReleaseFilename(nzbName, baseFilename)
			slog.InfoContext(ctx, "Normalizing obfuscated filename in RAR archive",
				"original", rarContent.Filename,
				"normalized", baseFilename)
			internalSubDir = "."
		}

		var virtualFilePath string
		if internalSubDir == "." || internalSubDir == "" {
			virtualFilePath = filepath.Join(virtualDir, baseFilename)
		} else {
			subDir := filepath.Join(virtualDir, internalSubDir)
			if err := filesystem.EnsureDirectoryExists(subDir, metadataService); err != nil {
				return fmt.Errorf("failed to create archive subdirectory %s: %w", subDir, err)
			}
			virtualFilePath = filepath.Join(subDir, baseFilename)
		}
		virtualFilePath = strings.ReplaceAll(virtualFilePath, string(filepath.Separator), "/")

		if existingMeta, err := metadataService.ReadFileMetadata(virtualFilePath); err == nil && existingMeta != nil {
			if existingMeta.Status == metapb.FileStatus_FILE_STATUS_HEALTHY {
				slog.InfoContext(ctx, "Skipping re-import of healthy RAR-extracted file",
					"file", baseFilename,
					"virtual_path", virtualFilePath)
				preProcessedCount++
				continue
			}
		}

		isPreExtracted := false
		for _, extracted := range extractedFiles {
			if extracted.Name == baseFilename && extracted.Size == rarContent.Size {
				isPreExtracted = true
				break
			}
		}

		filesToProcess = append(filesToProcess, fileToProcess{
			content:         rarContent,
			baseFilename:    baseFilename,
			virtualFilePath: virtualFilePath,
			isPreExtracted:  isPreExtracted,
			segmentOffset:   cumulativeOffset,
		})

		// Accumulate the segment offset for the next file's OffsetTracker.
		segCount := getContentSegmentCount(rarContent)
		if segmentSamplePercentage == 100 {
			cumulativeOffset += segCount
		} else {
			minSegments := 5
			if segCount <= minSegments {
				cumulativeOffset += segCount
			} else {
				fixedSegments := 5
				middleSegmentCount := segCount - fixedSegments
				sampledMiddle := (middleSegmentCount * segmentSamplePercentage) / 100
				cumulativeOffset += fixedSegments + sampledMiddle
			}
		}
	}

	// Parallel pass: validate segments and write metadata for each file concurrently.
	var filesProcessed int32
	p := concpool.New().WithErrors().WithFirstError().WithContext(ctx)

	for _, item := range filesToProcess {
		p.Go(func(ctx context.Context) error {
			if item.isPreExtracted {
				slog.InfoContext(ctx, "Skipping validation for pre-extracted file (found in database)",
					"file", item.baseFilename,
					"size", item.content.Size)
			} else {
				if err := validateSegmentIntegrity(ctx, item.content); err != nil {
					slog.ErrorContext(ctx, "Skipping RAR file due to segment integrity failure (missing segments in NZB)",
						"file", item.baseFilename,
						"error", err)
					return nil
				}

				var offsetTracker *progress.OffsetTracker
				if validationProgressTracker != nil && totalSegmentsToValidate > 0 {
					offsetTracker = progress.NewOffsetTracker(
						validationProgressTracker,
						item.segmentOffset,
						totalSegmentsToValidate,
					)
				}

				validationSegments := getContentSegments(item.content)

				var validationSize int64
				if len(item.content.NestedSources) > 0 {
					for _, ns := range item.content.NestedSources {
						sourceSize := int64(0)
						for _, seg := range ns.Segments {
							sourceSize += seg.EndOffset - seg.StartOffset + 1
						}
						validationSize += sourceSize
					}
				} else {
					validationSize = item.content.PackedSize
					if len(item.content.AesKey) > 0 {
						validationSize = aes.EncryptedSize(item.content.PackedSize)
					}
				}

				if err := validation.ValidateSegmentsForFile(
					ctx,
					item.baseFilename,
					validationSize,
					validationSegments,
					metapb.Encryption_NONE,
					poolManager,
					maxValidationGoroutines,
					segmentSamplePercentage,
					offsetTracker,
					timeout,
				); err != nil {
					slog.WarnContext(ctx, "Skipping RAR file due to validation error", "error", err, "file", item.baseFilename)
					return nil
				}
			}

			fileMeta := rarProcessor.CreateFileMetadataFromRarContent(item.content, nzbPath, releaseDate, item.content.NzbdavID)

			metadataPath := metadataService.GetMetadataFilePath(item.virtualFilePath)
			if _, err := os.Stat(metadataPath); err == nil {
				_ = metadataService.DeleteFileMetadata(item.virtualFilePath)
			}

			if err := metadataService.WriteFileMetadata(item.virtualFilePath, fileMeta); err != nil {
				return fmt.Errorf("failed to write metadata for RAR file %s: %w", item.content.Filename, err)
			}

			slog.InfoContext(ctx, "Created metadata for RAR extracted file",
				"file", item.baseFilename,
				"virtual_path", item.virtualFilePath,
				"size", item.content.Size)

			atomic.AddInt32(&filesProcessed, 1)
			return nil
		})
	}

	if err := p.Wait(); err != nil {
		return err
	}

	if int(atomic.LoadInt32(&filesProcessed))+preProcessedCount == 0 && len(rarContents) > 0 {
		return ErrNoFilesProcessed
	}

	// Ensure validation progress is at 95% (end of validation range)
	if validationProgressTracker != nil && totalSegmentsToValidate > 0 {
		validationProgressTracker.Update(totalSegmentsToValidate, totalSegmentsToValidate)
	}

	// Update progress to 100% after all metadata written (95-100% for metadata finalization)
	// Use UpdateAbsolute since validationProgressTracker is limited to 80-95% range
	if validationProgressTracker != nil {
		validationProgressTracker.UpdateAbsolute(100)
	}

	slog.InfoContext(ctx, "Successfully processed RAR archive files",
		"files_processed", int(atomic.LoadInt32(&filesProcessed))+preProcessedCount)

	return nil
}

// expandISOContents replaces any .iso Content entries with the media files found
// inside them. Non-ISO entries are passed through unchanged. Per-file errors are
// non-fatal: on failure the original ISO Content is kept.
func expandISOContents(
	ctx context.Context,
	expand bool,
	contents []Content,
	poolManager pool.Manager,
	maxPrefetch int,
	readTimeout time.Duration,
	allowedExtensions []string,
) ([]Content, error) {
	if !expand {
		return contents, nil
	}
	var result []Content
	for _, c := range contents {
		if c.IsDirectory || strings.ToLower(filepath.Ext(c.Filename)) != ".iso" {
			result = append(result, c)
			continue
		}

		src := iso.ISOSource{
			Filename: c.Filename,
			Segments: c.Segments,
			AesKey:   c.AesKey,
			AesIV:    c.AesIV,
			Size:     c.Size,
		}

		isoFiles, err := iso.AnalyzeISOContent(ctx, src, poolManager, maxPrefetch, readTimeout, allowedExtensions)
		if err != nil {
			slog.WarnContext(ctx, "Failed to analyze ISO content, keeping ISO as-is",
				"file", c.Filename, "error", err)
			result = append(result, c)
			continue
		}

		if len(isoFiles) == 0 {
			result = append(result, c)
			continue
		}

		// Sort ISO files by size descending so the largest (main feature) gets index 1.
		sort.Slice(isoFiles, func(i, j int) bool {
			return isoFiles[i].Size > isoFiles[j].Size
		})

		// Keep only the largest file (index 0 after sort); discard smaller streams.
		f := isoFiles[0]
		nc := Content{
			InternalPath:      f.InternalPath,
			Filename:          f.Filename,
			Size:              f.Size,
			PackedSize:        f.Size, // raw ISO data — packed == unpacked
			NzbdavID:          c.NzbdavID,
			ISOExpansionIndex: 1,
		}
		if f.NestedSource != nil {
			nc.NestedSources = []NestedSource{{
				Segments:        f.NestedSource.Segments,
				AesKey:          f.NestedSource.AesKey,
				AesIV:           f.NestedSource.AesIV,
				InnerOffset:     f.NestedSource.InnerOffset,
				InnerLength:     f.NestedSource.InnerLength,
				InnerVolumeSize: f.NestedSource.InnerVolumeSize,
			}}
		} else {
			nc.Segments = f.Segments
		}
		result = append(result, nc)
	}
	return result, nil
}

// GroupArchivesByBaseName groups ParsedFiles by their RAR base name (case-insensitive).
// Returns groups in deterministic order (sorted by base name) for testability.
func GroupArchivesByBaseName(files []parser.ParsedFile) [][]parser.ParsedFile {
	groupMap := make(map[string][]parser.ParsedFile)
	var keys []string
	for _, f := range files {
		key := extractRarBaseName(f.Filename)
		if _, exists := groupMap[key]; !exists {
			keys = append(keys, key)
		}
		groupMap[key] = append(groupMap[key], f)
	}
	sort.Strings(keys)
	groups := make([][]parser.ParsedFile, 0, len(keys))
	for _, k := range keys {
		groups = append(groups, groupMap[k])
	}
	return groups
}

// normalizeArchiveReleaseFilename aligns the filename to the NZB basename while keeping the original extension.
func normalizeArchiveReleaseFilename(nzbFilename, originalFilename string) string {
	releaseName := strings.TrimSuffix(nzbFilename, filepath.Ext(nzbFilename))
	fileExt := filepath.Ext(originalFilename)

	if fileExt == "" {
		return releaseName
	}

	// If release name already contains the extension (e.g. Movie.mkv.nzb -> Movie.mkv), don't duplicate
	if strings.HasSuffix(strings.ToLower(releaseName), strings.ToLower(fileExt)) {
		return releaseName
	}

	return releaseName + fileExt
}
