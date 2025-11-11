package multifile

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/javi11/altmount/internal/importer/filesystem"
	"github.com/javi11/altmount/internal/importer/parser"
	"github.com/javi11/altmount/internal/importer/utils"
	"github.com/javi11/altmount/internal/importer/validation"
	"github.com/javi11/altmount/internal/metadata"
	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/javi11/altmount/internal/pool"
)

// ProcessRegularFiles processes multiple regular files
func ProcessRegularFiles(
	ctx context.Context,
	virtualDir string,
	files []parser.ParsedFile,
	nzbPath string,
	metadataService *metadata.MetadataService,
	poolManager pool.Manager,
	maxValidationGoroutines int,
	segmentSamplePercentage int,
	allowedFileExtensions []string,
) error {
	if len(files) == 0 {
		return nil
	}

	// Validate file extensions before processing
	if !utils.HasAllowedFilesInRegular(files, allowedFileExtensions) {
		slog.WarnContext(ctx, "No files with allowed extensions found",
			"allowed_extensions", allowedFileExtensions,
			"file_count", len(files))
		return fmt.Errorf("no files with allowed extensions found (allowed: %v)", allowedFileExtensions)
	}

	for _, file := range files {
		parentPath, filename := filesystem.DetermineFileLocation(file, virtualDir)

		// Ensure parent directory exists
		if err := filesystem.EnsureDirectoryExists(parentPath, metadataService); err != nil {
			return fmt.Errorf("failed to create parent directory %s: %w", parentPath, err)
		}

		// Create virtual file path
		virtualPath := filepath.Join(parentPath, filename)
		virtualPath = strings.ReplaceAll(virtualPath, string(filepath.Separator), "/")

		// Validate segments
		if err := validation.ValidateSegmentsForFile(
			ctx,
			filename,
			file.Size,
			file.Segments,
			file.Encryption,
			poolManager,
			maxValidationGoroutines,
			segmentSamplePercentage,
			nil, // No progress callback for multi-file imports
		); err != nil {
			return err
		}

		// Create file metadata
		fileMeta := metadataService.CreateFileMetadata(
			file.Size,
			nzbPath,
			metapb.FileStatus_FILE_STATUS_HEALTHY,
			file.Segments,
			file.Encryption,
			file.Password,
			file.Salt,
			file.ReleaseDate.Unix(),
		)

		// Delete old metadata if exists (simple collision handling)
		metadataPath := metadataService.GetMetadataFilePath(virtualPath)
		if _, err := os.Stat(metadataPath); err == nil {
			_ = metadataService.DeleteFileMetadata(virtualPath)
		}

		// Write file metadata to disk
		if err := metadataService.WriteFileMetadata(virtualPath, fileMeta); err != nil {
			return fmt.Errorf("failed to write metadata for file %s: %w", filename, err)
		}

		slog.DebugContext(ctx, "Created metadata file",
			"file", filename,
			"virtual_path", virtualPath,
			"size", file.Size)
	}

	slog.InfoContext(ctx, "Successfully processed regular files",
		"virtual_dir", virtualDir,
		"files", len(files))

	return nil
}
