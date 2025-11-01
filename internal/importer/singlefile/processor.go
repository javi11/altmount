package singlefile

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

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
	nzbPath string,
	metadataService *metadata.MetadataService,
	poolManager pool.Manager,
	maxValidationGoroutines int,
	fullSegmentValidation bool,
	allowedFileExtensions []string,
	log *slog.Logger,
) (string, error) {
	// Validate file extension before processing
	if !utils.HasAllowedFilesInRegular([]parser.ParsedFile{file}, allowedFileExtensions) {
		log.Warn("File does not match allowed extensions",
			"filename", file.Filename,
			"allowed_extensions", allowedFileExtensions)
		return "", fmt.Errorf("file '%s' does not match allowed extensions (allowed: %v)", file.Filename, allowedFileExtensions)
	}

	// Create virtual file path
	virtualFilePath := filepath.Join(virtualDir, file.Filename)
	virtualFilePath = strings.ReplaceAll(virtualFilePath, string(filepath.Separator), "/")

	// Validate segments
	if err := validation.ValidateSegmentsForFile(
		ctx,
		file.Filename,
		file.Size,
		file.Segments,
		file.Encryption,
		poolManager,
		maxValidationGoroutines,
		fullSegmentValidation,
	); err != nil {
		return "", err
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
	metadataPath := metadataService.GetMetadataFilePath(virtualFilePath)
	if _, err := os.Stat(metadataPath); err == nil {
		_ = metadataService.DeleteFileMetadata(virtualFilePath)
	}

	// Write file metadata to disk
	if err := metadataService.WriteFileMetadata(virtualFilePath, fileMeta); err != nil {
		return "", fmt.Errorf("failed to write metadata for single file %s: %w", file.Filename, err)
	}

	log.Info("Successfully processed single file",
		"file", file.Filename,
		"virtual_path", virtualFilePath,
		"size", file.Size)

	return virtualFilePath, nil
}
