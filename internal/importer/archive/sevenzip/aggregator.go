package sevenzip

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

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
	ErrNoAllowedFiles = errors.New("archive contains no files with allowed extensions")
	// ErrNoFilesProcessed indicates that no files were successfully processed (all files failed validation)
	ErrNoFilesProcessed = errors.New("no files were successfully processed (all files failed validation)")
)

// calculateSegmentsToValidate calculates the actual number of segments that will be validated
// based on the validation mode (full or sampling) and sample percentage.
// This mirrors the logic in usenet.ValidateSegmentAvailability which uses selectSegmentsForValidation.
func calculateSegmentsToValidate(sevenZipContents []Content, samplePercentage int) int {
	total := 0
	for _, content := range sevenZipContents {
		if content.IsDirectory {
			continue
		}

		segmentCount := len(content.Segments)
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

// hasAllowedFiles checks if any files within 7zip archive contents match allowed extensions
// If allowedExtensions is empty, all file types are allowed
func hasAllowedFiles(sevenZipContents []Content, allowedExtensions []string) bool {
	for _, content := range sevenZipContents {
		// Skip directories
		if content.IsDirectory {
			continue
		}
		// Check both the internal path and filename
		if utils.IsAllowedFile(content.InternalPath, content.Size, allowedExtensions) ||
			utils.IsAllowedFile(content.Filename, content.Size, allowedExtensions) {
			return true
		}
	}
	return false
}

// ProcessArchive analyzes and processes 7zip archive files, creating metadata for all extracted files.
// This function handles the complete workflow: analysis → file processing → metadata creation.
func ProcessArchive(
	ctx context.Context,
	virtualDir string,
	archiveFiles []parser.ParsedFile,
	password string,
	releaseDate int64,
	nzbPath string,
	sevenZipProcessor Processor,
	metadataService *metadata.MetadataService,
	poolManager pool.Manager,
	archiveProgressTracker *progress.Tracker,
	validationProgressTracker *progress.Tracker,
	maxValidationGoroutines int,
	segmentSamplePercentage int,
	allowedFileExtensions []string,
	timeout time.Duration,
	extractedFiles []parser.ExtractedFileInfo,
) error {
	if len(archiveFiles) == 0 {
		return nil
	}

	slog.InfoContext(ctx, "Analyzing 7zip archive content", "parts", len(archiveFiles))

	// Analyze 7zip content with timeout
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	sevenZipContents, err := sevenZipProcessor.AnalyzeSevenZipContentFromNzb(ctx, archiveFiles, password, archiveProgressTracker)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to analyze 7zip archive content", "error", err)
		return err
	}

	slog.InfoContext(ctx, "Successfully analyzed 7zip archive content", "files_in_archive", len(sevenZipContents))

	// Validate file extensions before processing
	if !hasAllowedFiles(sevenZipContents, allowedFileExtensions) {
		slog.WarnContext(ctx, "7zip archive contains no files with allowed extensions", "allowed_extensions", allowedFileExtensions)
		return ErrNoAllowedFiles
	}

	// Calculate total segments to validate for accurate progress tracking
	// This accounts for sampling mode if enabled
	totalSegmentsToValidate := calculateSegmentsToValidate(sevenZipContents, segmentSamplePercentage)
	validatedSegmentsCount := 0

	slog.InfoContext(ctx, "Starting 7zip archive validation",
		"total_files", len(sevenZipContents),
		"total_segments_to_validate", totalSegmentsToValidate,
		"sample_percentage", segmentSamplePercentage)

	// Process extracted files with segment-based progress tracking
	// 80-95% for validation loop, 95-100% for metadata finalization
	filesProcessed := 0

	// Determine if we should rename the file to match the NZB basename
	// Only do this if there's exactly one media file in the archive
	mediaFilesCount := 0
	for _, content := range sevenZipContents {
		if !content.IsDirectory && (utils.IsAllowedFile(content.InternalPath, content.Size, allowedFileExtensions) ||
			utils.IsAllowedFile(content.Filename, content.Size, allowedFileExtensions)) {
			mediaFilesCount++
		}
	}

	nzbName := filepath.Base(nzbPath)
	shouldNormalizeName := mediaFilesCount == 1

	for _, sevenZipContent := range sevenZipContents {
		// Skip directories
		if sevenZipContent.IsDirectory {
			slog.DebugContext(ctx, "Skipping directory in 7zip archive", "path", sevenZipContent.InternalPath)
			continue
		}

		// Flatten the internal path by extracting only the base filename
		normalizedInternalPath := strings.ReplaceAll(sevenZipContent.InternalPath, "\\", "/")
		baseFilename := filepath.Base(normalizedInternalPath)

		// Double check if this specific file is allowed
		if !utils.IsAllowedFile(sevenZipContent.InternalPath, sevenZipContent.Size, allowedFileExtensions) &&
			!utils.IsAllowedFile(sevenZipContent.Filename, sevenZipContent.Size, allowedFileExtensions) {
			continue
		}

		// Normalize filename to match NZB if it's the only media file
		if shouldNormalizeName && (utils.IsAllowedFile(sevenZipContent.InternalPath, sevenZipContent.Size, allowedFileExtensions) ||
			utils.IsAllowedFile(sevenZipContent.Filename, sevenZipContent.Size, allowedFileExtensions)) {
			// Extract release name and combine with original extension
			baseFilename = normalizeArchiveReleaseFilename(nzbName, baseFilename)
			slog.InfoContext(ctx, "Normalizing obfuscated filename in 7zip archive",
				"original", sevenZipContent.Filename,
				"normalized", baseFilename)
		}

		// Create the virtual file path directly in the 7zip directory (flattened)
		virtualFilePath := filepath.Join(virtualDir, baseFilename)
		virtualFilePath = strings.ReplaceAll(virtualFilePath, string(filepath.Separator), "/")

		// Check if file already exists and is healthy
		if existingMeta, err := metadataService.ReadFileMetadata(virtualFilePath); err == nil && existingMeta != nil {
			if existingMeta.Status == metapb.FileStatus_FILE_STATUS_HEALTHY {
				slog.InfoContext(ctx, "Skipping re-import of healthy 7zip-extracted file",
					"file", baseFilename,
					"virtual_path", virtualFilePath)
				filesProcessed++
				continue
			}
		}

		// Check if this file matches an already extracted file in the database
		isPreExtracted := false
		for _, extracted := range extractedFiles {
			if extracted.Name == baseFilename && extracted.Size == sevenZipContent.Size {
				isPreExtracted = true
				break
			}
		}

		if isPreExtracted {
			slog.InfoContext(ctx, "Skipping validation for pre-extracted file (found in database)",
				"file", baseFilename,
				"size", sevenZipContent.Size)
		} else {
			// Create offset tracker for real-time segment-level progress
			// This maps individual file segment progress (0→N) to cumulative progress across all files
			var offsetTracker *progress.OffsetTracker
			if validationProgressTracker != nil && totalSegmentsToValidate > 0 {
				offsetTracker = progress.NewOffsetTracker(
					validationProgressTracker,
					validatedSegmentsCount,  // Segments already validated in previous files
					totalSegmentsToValidate, // Total segments across all files
				)
			}

			// Determine encryption type for validation
			encryption := metapb.Encryption_NONE
			if len(sevenZipContent.AesKey) > 0 {
				encryption = metapb.Encryption_AES
			}

			// Validate segments with real-time progress updates
			if err := validation.ValidateSegmentsForFile(
				ctx,
				baseFilename,
				sevenZipContent.Size,
				sevenZipContent.Segments,
				encryption,
				poolManager,
				maxValidationGoroutines,
				segmentSamplePercentage,
				offsetTracker, // Real-time segment progress with cumulative offset
				timeout,
			); err != nil {
				slog.WarnContext(ctx, "Skipping 7zip file due to validation error", "error", err, "file", baseFilename)

				continue
			}
		}

		// Calculate and track segments validated for this file (for next file's offset)
		segmentCount := len(sevenZipContent.Segments)
		var fileSegmentsValidated int
		if segmentSamplePercentage == 100 {
			fileSegmentsValidated = segmentCount
		} else {
			// Sampling mode: calculate same as helper function
			minSegments := 5
			if segmentCount <= minSegments {
				fileSegmentsValidated = segmentCount
			} else {
				fixedSegments := 5
				middleSegmentCount := segmentCount - fixedSegments
				sampledMiddle := (middleSegmentCount * segmentSamplePercentage) / 100
				fileSegmentsValidated = fixedSegments + sampledMiddle
			}
		}

		// Update cumulative segment count for next file's offset
		validatedSegmentsCount += fileSegmentsValidated

		// Create file metadata using the 7zip handler's helper function
		fileMeta := sevenZipProcessor.CreateFileMetadataFromSevenZipContent(sevenZipContent, nzbPath, releaseDate, sevenZipContent.NzbdavID)

		// Delete old metadata if exists (simple collision handling)
		metadataPath := metadataService.GetMetadataFilePath(virtualFilePath)
		if _, err := os.Stat(metadataPath); err == nil {
			_ = metadataService.DeleteFileMetadata(virtualFilePath)
		}

		// Write file metadata to disk
		if err := metadataService.WriteFileMetadata(virtualFilePath, fileMeta); err != nil {
			return fmt.Errorf("failed to write metadata for 7zip file %s: %w", sevenZipContent.Filename, err)
		}

		slog.InfoContext(ctx, "Created metadata for 7zip extracted file",
			"file", baseFilename,
			"virtual_path", virtualFilePath,
			"size", sevenZipContent.Size)

		filesProcessed++
	}

	// If no files were processed but we had content, fail the import
	if filesProcessed == 0 && len(sevenZipContents) > 0 {
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

	slog.InfoContext(ctx, "Successfully processed 7zip archive files", "files_processed", filesProcessed)

	return nil
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
