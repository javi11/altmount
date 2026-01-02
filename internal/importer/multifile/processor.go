package multifile

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/javi11/altmount/internal/importer/common"
	"github.com/javi11/altmount/internal/importer/filesystem"
	"github.com/javi11/altmount/internal/importer/parser"
	"github.com/javi11/altmount/internal/importer/utils"
	"github.com/javi11/altmount/internal/metadata"
	"github.com/javi11/altmount/internal/pool"
)

// ProcessRegularFiles processes multiple regular files
func ProcessRegularFiles(
	ctx context.Context,
	virtualDir string,
	files []parser.ParsedFile,
	par2Files []parser.ParsedFile,
	nzbPath string,
	metadataService *metadata.MetadataService,
	poolManager pool.Manager,
	maxValidationGoroutines int,
	segmentSamplePercentage int,
	allowedFileExtensions []string,
	timeout time.Duration,
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

	// Convert PAR2 files to metadata format (shared across all files)
	par2Refs := common.ConvertPar2Files(par2Files)

	opts := common.ImportOptions{
		MetadataService:         metadataService,
		PoolManager:             poolManager,
		MaxValidationGoroutines: maxValidationGoroutines,
		SegmentSamplePercentage: segmentSamplePercentage,
		AllowedFileExtensions:   allowedFileExtensions,
		Timeout:                 timeout,
	}

	for _, file := range files {
		parentPath, filename := filesystem.DetermineFileLocation(file, virtualDir)

		// Ensure parent directory exists
		if err := filesystem.EnsureDirectoryExists(parentPath, metadataService); err != nil {
			return fmt.Errorf("failed to create parent directory %s: %w", parentPath, err)
		}

		// Update filename in file object temporarily as DetermineFileLocation might have changed it
		fileCopy := file
		fileCopy.Filename = filename

		// Import the file using common logic
		if _, err := common.ImportFile(ctx, parentPath, fileCopy, par2Refs, nzbPath, opts); err != nil {
			return err
		}
	}

	slog.InfoContext(ctx, "Successfully processed regular files",
		"virtual_dir", virtualDir,
		"files", len(files))

	return nil
}