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
	"github.com/javi11/altmount/internal/importer/validation"
	"github.com/javi11/altmount/internal/metadata"
	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/javi11/altmount/internal/pool"
	"github.com/javi11/altmount/internal/progress"
)

var (
	// ErrNoAllowedFiles indicates that the archive contains no files matching allowed extensions
	ErrNoAllowedFiles = errors.New("archive contains no files with allowed extensions")
)

// hasAllowedFiles checks if any files within 7zip archive contents match allowed extensions
// If allowedExtensions is empty, returns true (all files allowed)
func hasAllowedFiles(sevenZipContents []Content, allowedExtensions []string) bool {
	// Empty list = allow all files
	if len(allowedExtensions) == 0 {
		return true
	}

	for _, content := range sevenZipContents {
		// Skip directories
		if content.IsDirectory {
			continue
		}
		// Check both the internal path and filename
		if isAllowedFile(content.InternalPath, allowedExtensions) || isAllowedFile(content.Filename, allowedExtensions) {
			return true
		}
	}
	return false
}

// isAllowedFile checks if a filename has an allowed extension
func isAllowedFile(filename string, allowedExtensions []string) bool {
	if filename == "" {
		return false
	}

	// Empty list = allow all files
	if len(allowedExtensions) == 0 {
		return true
	}

	ext := strings.ToLower(filepath.Ext(filename))
	for _, allowedExt := range allowedExtensions {
		if ext == strings.ToLower(allowedExt) {
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
	progressTracker *progress.Tracker,
	maxValidationGoroutines int,
	fullSegmentValidation bool,
	allowedFileExtensions []string,
) error {
	if len(archiveFiles) == 0 {
		return nil
	}

	slog.InfoContext(ctx, "Analyzing 7zip archive content", "parts", len(archiveFiles))

	// Analyze 7zip content with timeout
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	sevenZipContents, err := sevenZipProcessor.AnalyzeSevenZipContentFromNzb(ctx, archiveFiles, password, progressTracker)
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

	// Process extracted files
	for _, sevenZipContent := range sevenZipContents {
		// Skip directories
		if sevenZipContent.IsDirectory {
			slog.DebugContext(ctx, "Skipping directory in 7zip archive", "path", sevenZipContent.InternalPath)
			continue
		}

		// Flatten the internal path by extracting only the base filename
		normalizedInternalPath := strings.ReplaceAll(sevenZipContent.InternalPath, "\\", "/")
		baseFilename := filepath.Base(normalizedInternalPath)

		// Create the virtual file path directly in the 7zip directory (flattened)
		virtualFilePath := filepath.Join(virtualDir, baseFilename)
		virtualFilePath = strings.ReplaceAll(virtualFilePath, string(filepath.Separator), "/")

		// Validate segments
		if err := validation.ValidateSegmentsForFile(
			ctx,
			baseFilename,
			sevenZipContent.Size,
			sevenZipContent.Segments,
			metapb.Encryption_NONE,
			poolManager,
			maxValidationGoroutines,
			fullSegmentValidation,
		); err != nil {
			return err
		}

		// Create file metadata using the 7zip handler's helper function
		fileMeta := sevenZipProcessor.CreateFileMetadataFromSevenZipContent(sevenZipContent, nzbPath, releaseDate)

		// Delete old metadata if exists (simple collision handling)
		metadataPath := metadataService.GetMetadataFilePath(virtualFilePath)
		if _, err := os.Stat(metadataPath); err == nil {
			_ = metadataService.DeleteFileMetadata(virtualFilePath)
		}

		// Write file metadata to disk
		if err := metadataService.WriteFileMetadata(virtualFilePath, fileMeta); err != nil {
			return fmt.Errorf("failed to write metadata for 7zip file %s: %w", sevenZipContent.Filename, err)
		}

		slog.DebugContext(ctx, "Created metadata for 7zip extracted file",
			"file", baseFilename,
			"original_internal_path", sevenZipContent.InternalPath,
			"virtual_path", virtualFilePath,
			"size", sevenZipContent.Size,
			"segments", len(sevenZipContent.Segments))
	}

	slog.InfoContext(ctx, "Successfully processed 7zip archive files", "files_processed", len(sevenZipContents))

	return nil
}
