package steps

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/javi11/altmount/internal/importer/archive/rar"
	"github.com/javi11/altmount/internal/importer/archive/sevenzip"
	"github.com/javi11/altmount/internal/importer/parser"
	"github.com/javi11/altmount/internal/metadata"
	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/javi11/altmount/internal/pool"
	"github.com/javi11/altmount/internal/progress"
)

// AnalyzeRarArchive analyzes RAR archive content from NZB files
func AnalyzeRarArchive(
	ctx context.Context,
	archiveFiles []parser.ParsedFile,
	password string,
	rarProcessor rar.Processor,
	log *slog.Logger,
	progressTracker *progress.Tracker,
) ([]rar.Content, error) {
	if len(archiveFiles) == 0 {
		return nil, nil
	}

	log.Info("Analyzing RAR archive content", "parts", len(archiveFiles))

	// Analyze RAR content with timeout
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	rarContents, err := rarProcessor.AnalyzeRarContentFromNzb(ctx, archiveFiles, password, progressTracker)
	if err != nil {
		log.Error("Failed to analyze RAR archive content", "error", err)
		return nil, err
	}

	return rarContents, nil
}

// ProcessRarArchiveFiles processes files extracted from RAR archives
func ProcessRarArchiveFiles(
	ctx context.Context,
	virtualDir string,
	contents []rar.Content,
	nzbPath string,
	releaseDate int64,
	rarProcessor rar.Processor,
	metadataService *metadata.MetadataService,
	poolManager pool.Manager,
	maxValidationGoroutines int,
	fullSegmentValidation bool,
	log *slog.Logger,
) error {
	if len(contents) == 0 {
		return nil
	}

	for _, rarContent := range contents {
		// Skip directories
		if rarContent.IsDirectory {
			log.Debug("Skipping directory in RAR archive", "path", rarContent.InternalPath)
			continue
		}

		// Flatten the internal path by extracting only the base filename
		normalizedInternalPath := strings.ReplaceAll(rarContent.InternalPath, "\\", "/")
		baseFilename := filepath.Base(normalizedInternalPath)

		// Create the virtual file path directly in the RAR directory (flattened)
		virtualFilePath := filepath.Join(virtualDir, baseFilename)
		virtualFilePath = strings.ReplaceAll(virtualFilePath, string(filepath.Separator), "/")

		// Validate segments
		if err := ValidateSegmentsForFile(
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

		log.Debug("Created metadata for RAR extracted file",
			"file", baseFilename,
			"original_internal_path", rarContent.InternalPath,
			"virtual_path", virtualFilePath,
			"size", rarContent.Size,
			"segments", len(rarContent.Segments))
	}

	log.Info("Successfully processed RAR archive files", "files_processed", len(contents))

	return nil
}

// AnalyzeSevenZipArchive analyzes 7zip archive content from NZB files
func AnalyzeSevenZipArchive(
	ctx context.Context,
	archiveFiles []parser.ParsedFile,
	password string,
	sevenZipProcessor sevenzip.Processor,
	log *slog.Logger,
	progressTracker *progress.Tracker,
) ([]sevenzip.Content, error) {
	if len(archiveFiles) == 0 {
		return nil, nil
	}

	log.Info("Analyzing 7zip archive content", "parts", len(archiveFiles))

	// Analyze 7zip content with timeout
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	sevenZipContents, err := sevenZipProcessor.AnalyzeSevenZipContentFromNzb(ctx, archiveFiles, password, progressTracker)
	if err != nil {
		log.Error("Failed to analyze 7zip archive content", "error", err)
		return nil, err
	}

	log.Info("Successfully analyzed 7zip archive content", "files_in_archive", len(sevenZipContents))

	return sevenZipContents, nil
}

// ProcessSevenZipArchiveFiles processes files extracted from 7zip archives
func ProcessSevenZipArchiveFiles(
	ctx context.Context,
	virtualDir string,
	contents []sevenzip.Content,
	nzbPath string,
	releaseDate int64,
	sevenZipProcessor sevenzip.Processor,
	metadataService *metadata.MetadataService,
	poolManager pool.Manager,
	maxValidationGoroutines int,
	fullSegmentValidation bool,
	log *slog.Logger,
) error {
	if len(contents) == 0 {
		return nil
	}

	for _, sevenZipContent := range contents {
		// Skip directories
		if sevenZipContent.IsDirectory {
			log.Debug("Skipping directory in 7zip archive", "path", sevenZipContent.InternalPath)
			continue
		}

		// Flatten the internal path by extracting only the base filename
		normalizedInternalPath := strings.ReplaceAll(sevenZipContent.InternalPath, "\\", "/")
		baseFilename := filepath.Base(normalizedInternalPath)

		// Create the virtual file path directly in the 7zip directory (flattened)
		virtualFilePath := filepath.Join(virtualDir, baseFilename)
		virtualFilePath = strings.ReplaceAll(virtualFilePath, string(filepath.Separator), "/")

		// Validate segments
		if err := ValidateSegmentsForFile(
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

		log.Debug("Created metadata for 7zip extracted file",
			"file", baseFilename,
			"original_internal_path", sevenZipContent.InternalPath,
			"virtual_path", virtualFilePath,
			"size", sevenZipContent.Size,
			"segments", len(sevenZipContent.Segments))
	}

	log.Info("Successfully processed 7zip archive files", "files_processed", len(contents))

	return nil
}
