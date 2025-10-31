package sevenzip

import (
	"context"
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
	log *slog.Logger,
) error {
	if len(archiveFiles) == 0 {
		return nil
	}

	log.Info("Analyzing 7zip archive content", "parts", len(archiveFiles))

	// Analyze 7zip content with timeout
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	sevenZipContents, err := sevenZipProcessor.AnalyzeSevenZipContentFromNzb(ctx, archiveFiles, password, progressTracker)
	if err != nil {
		log.Error("Failed to analyze 7zip archive content", "error", err)
		return err
	}

	log.Info("Successfully analyzed 7zip archive content", "files_in_archive", len(sevenZipContents))

	// Process extracted files
	for _, sevenZipContent := range sevenZipContents {
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

		log.Debug("Created metadata for 7zip extracted file",
			"file", baseFilename,
			"original_internal_path", sevenZipContent.InternalPath,
			"virtual_path", virtualFilePath,
			"size", sevenZipContent.Size,
			"segments", len(sevenZipContent.Segments))
	}

	log.Info("Successfully processed 7zip archive files", "files_processed", len(sevenZipContents))

	return nil
}
