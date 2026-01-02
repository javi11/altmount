package common

import (
	"context"
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

// ImportOptions contains configuration for importing files
type ImportOptions struct {
	MetadataService         *metadata.MetadataService
	PoolManager             pool.Manager
	MaxValidationGoroutines int
	SegmentSamplePercentage int
	AllowedFileExtensions   []string
	Timeout                 time.Duration
	ProgressTracker         progress.ProgressTracker
}

// ImportFile handles the common logic for importing a single file:
// 1. Validates extensions
// 2. Checks if file exists and is healthy
// 3. Validates segments
// 4. Creates and writes metadata
func ImportFile(
	ctx context.Context,
	parentDir string,
	file parser.ParsedFile,
	par2Refs []*metapb.Par2FileReference,
	nzbPath string,
	opts ImportOptions,
) (string, error) {
	// Double check if this specific file is allowed
	if !utils.IsAllowedFile(file.Filename, file.Size, opts.AllowedFileExtensions) {
		slog.DebugContext(ctx, "File not allowed", "filename", file.Filename)
		return "", nil // Not an error, just skipped
	}

	// Create virtual file path
	virtualPath := filepath.Join(parentDir, file.Filename)
	virtualPath = strings.ReplaceAll(virtualPath, string(filepath.Separator), "/")

	// Check if file already exists and is healthy
	if existingMeta, err := opts.MetadataService.ReadFileMetadata(virtualPath); err == nil && existingMeta != nil {
		if existingMeta.Status == metapb.FileStatus_FILE_STATUS_HEALTHY {
			slog.InfoContext(ctx, "Skipping re-import of healthy file",
				"file", file.Filename,
				"virtual_path", virtualPath)
			return virtualPath, nil
		}
	}

	// Validate segments
	if err := validation.ValidateSegmentsForFile(
		ctx,
		file.Filename,
		file.Size,
		file.Segments,
		file.Encryption,
		opts.PoolManager,
		opts.MaxValidationGoroutines,
		opts.SegmentSamplePercentage,
		opts.ProgressTracker,
		opts.Timeout,
	); err != nil {
		return "", fmt.Errorf("validation failed for %s: %w", file.Filename, err)
	}

	// Create file metadata
	fileMeta := opts.MetadataService.CreateFileMetadata(
		file.Size,
		nzbPath,
		metapb.FileStatus_FILE_STATUS_HEALTHY,
		file.Segments,
		file.Encryption,
		file.Password,
		file.Salt,
		file.AesKey,
		file.AesIv,
		file.ReleaseDate.Unix(),
		par2Refs,
		file.NzbdavID,
	)

	// Delete old metadata if exists (simple collision handling)
	metadataPath := opts.MetadataService.GetMetadataFilePath(virtualPath)
	if _, err := os.Stat(metadataPath); err == nil {
		_ = opts.MetadataService.DeleteFileMetadata(virtualPath)
	}

	// Write file metadata to disk
	if err := opts.MetadataService.WriteFileMetadata(virtualPath, fileMeta); err != nil {
		return "", fmt.Errorf("failed to write metadata for file %s: %w", file.Filename, err)
	}

	slog.DebugContext(ctx, "Created metadata file",
		"file", file.Filename,
		"virtual_path", virtualPath,
		"size", file.Size)

	return virtualPath, nil
}

// ConvertPar2Files converts parsed PAR2 files to metadata references
func ConvertPar2Files(par2Files []parser.ParsedFile) []*metapb.Par2FileReference {
	var par2Refs []*metapb.Par2FileReference
	for _, par2File := range par2Files {
		par2Refs = append(par2Refs, &metapb.Par2FileReference{
			Filename:    par2File.Filename,
			FileSize:    par2File.Size,
			SegmentData: par2File.Segments,
		})
	}
	return par2Refs
}
