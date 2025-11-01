package rar

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

// hasAllowedFiles checks if any files within RAR archive contents match allowed extensions
// If allowedExtensions is empty, returns true (all files allowed)
func hasAllowedFiles(rarContents []Content, allowedExtensions []string) bool {
	// Empty list = allow all files
	if len(allowedExtensions) == 0 {
		return true
	}

	for _, content := range rarContents {
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

// ProcessArchive analyzes and processes RAR archive files, creating metadata for all extracted files.
// This function handles the complete workflow: analysis → file processing → metadata creation.
func ProcessArchive(
	ctx context.Context,
	virtualDir string,
	archiveFiles []parser.ParsedFile,
	password string,
	releaseDate int64,
	nzbPath string,
	rarProcessor Processor,
	metadataService *metadata.MetadataService,
	poolManager pool.Manager,
	progressTracker *progress.Tracker,
	maxValidationGoroutines int,
	fullSegmentValidation bool,
	allowedFileExtensions []string,
	log *slog.Logger,
) error {
	if len(archiveFiles) == 0 {
		return nil
	}

	log.Info("Analyzing RAR archive content", "parts", len(archiveFiles))

	// Analyze RAR content with timeout
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	rarContents, err := rarProcessor.AnalyzeRarContentFromNzb(ctx, archiveFiles, password, progressTracker)
	if err != nil {
		log.Error("Failed to analyze RAR archive content", "error", err)
		return err
	}

	// Validate file extensions before processing
	if !hasAllowedFiles(rarContents, allowedFileExtensions) {
		log.Warn("RAR archive contains no files with allowed extensions", "allowed_extensions", allowedFileExtensions)
		return ErrNoAllowedFiles
	}

	// Process extracted files
	for _, rarContent := range rarContents {
		// Skip directories
		if rarContent.IsDirectory {
			slog.DebugContext(ctx, "Skipping directory in RAR archive", "path", rarContent.InternalPath)
			continue
		}

		// Flatten the internal path by extracting only the base filename
		normalizedInternalPath := strings.ReplaceAll(rarContent.InternalPath, "\\", "/")
		baseFilename := filepath.Base(normalizedInternalPath)

		// Create the virtual file path directly in the RAR directory (flattened)
		virtualFilePath := filepath.Join(virtualDir, baseFilename)
		virtualFilePath = strings.ReplaceAll(virtualFilePath, string(filepath.Separator), "/")

		// Validate segments
		if err := validation.ValidateSegmentsForFile(
			ctx,
			baseFilename,
			rarContent.Size,
			rarContent.Segments,
			metapb.Encryption_NONE,
			poolManager,
			maxValidationGoroutines,
			fullSegmentValidation,
		); err != nil {
			return err
		}

		// Create file metadata using the RAR handler's helper function
		fileMeta := rarProcessor.CreateFileMetadataFromRarContent(rarContent, nzbPath, releaseDate)

		// Delete old metadata if exists (simple collision handling)
		metadataPath := metadataService.GetMetadataFilePath(virtualFilePath)
		if _, err := os.Stat(metadataPath); err == nil {
			_ = metadataService.DeleteFileMetadata(virtualFilePath)
		}

		// Write file metadata to disk
		if err := metadataService.WriteFileMetadata(virtualFilePath, fileMeta); err != nil {
			return fmt.Errorf("failed to write metadata for RAR file %s: %w", rarContent.Filename, err)
		}

		slog.DebugContext(ctx, "Created metadata for RAR extracted file",
			"file", baseFilename,
			"original_internal_path", rarContent.InternalPath,
			"virtual_path", virtualFilePath,
			"size", rarContent.Size,
			"segments", len(rarContent.Segments))
	}

	slog.InfoContext(ctx, "Successfully processed RAR archive files", "files_processed", len(rarContents))

	return nil
}
