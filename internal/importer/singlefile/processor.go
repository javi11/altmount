package singlefile

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/javi11/altmount/internal/importer/parser"
	"github.com/javi11/altmount/internal/importer/utils"
	"github.com/javi11/altmount/internal/importer/validation"
	"github.com/javi11/altmount/internal/metadata"
	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/javi11/altmount/internal/pool"
)

// ProcessSingleFile processes a single file (creates and writes metadata)
func ProcessSingleFile(
	ctx context.Context,
	virtualDir string,
	file parser.ParsedFile,
	par2Files []parser.ParsedFile,
	nzbPath string,
	metadataService *metadata.MetadataService,
	poolManager pool.Manager,
	maxValidationGoroutines int,
	segmentSamplePercentage int,
	allowedFileExtensions []string,
	timeout time.Duration,
) (string, error) {
	// Validate file extension before processing
	if !utils.HasAllowedFilesInRegular([]parser.ParsedFile{file}, allowedFileExtensions) {
		slog.WarnContext(ctx, "File does not match allowed extensions",
			"filename", file.Filename,
			"allowed_extensions", allowedFileExtensions)
		return "", fmt.Errorf("file '%s' does not match allowed extensions (allowed: %v)", file.Filename, allowedFileExtensions)
	}

	// Create virtual file path
	virtualFilePath := filepath.Join(virtualDir, file.Filename)
	virtualFilePath = strings.ReplaceAll(virtualFilePath, string(filepath.Separator), "/")

	// Check if file already exists and is healthy
	if existingMeta, err := metadataService.ReadFileMetadata(virtualFilePath); err == nil && existingMeta != nil {
		if existingMeta.Status == metapb.FileStatus_FILE_STATUS_HEALTHY {
			slog.InfoContext(ctx, "Skipping re-import of healthy file",
				"file", file.Filename,
				"virtual_path", virtualFilePath)
			return virtualDir, nil
		}
	}

	// Double check if this specific file is allowed
	if !utils.IsAllowedFile(file.Filename, file.Size, allowedFileExtensions) {
		return "", fmt.Errorf("file '%s' is not allowed", file.Filename)
	}

	// Validate segments
	if err := validation.ValidateSegmentsForFile(
		ctx,
		file.Filename,
		file.Size,
		file.Segments,
		file.Encryption,
		poolManager,
		maxValidationGoroutines,
		segmentSamplePercentage,
		nil, // No progress callback for single file imports
		timeout,
	); err != nil {
		return "", err
	}

	// Convert PAR2 files to metadata format
	var par2Refs []*metapb.Par2FileReference
	for _, par2File := range par2Files {
		par2Refs = append(par2Refs, &metapb.Par2FileReference{
			Filename:    par2File.Filename,
			FileSize:    par2File.Size,
			SegmentData: par2File.Segments,
		})
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
		par2Refs,
		file.NzbdavID,
	)

	// Write file metadata to disk
	if err := metadataService.WriteFileMetadata(virtualFilePath, fileMeta); err != nil {
		return "", fmt.Errorf("failed to write metadata for single file %s: %w", file.Filename, err)
	}

	slog.InfoContext(ctx, "Successfully processed single file",
		"file", file.Filename,
		"virtual_path", virtualFilePath,
		"size", file.Size)

	return virtualDir, nil
}