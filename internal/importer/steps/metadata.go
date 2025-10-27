package steps

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/javi11/altmount/internal/metadata"
	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/javi11/altmount/internal/pool"
)

// ProcessSingleFileStep processes a single file (creates and writes metadata)
type ProcessSingleFileStep struct {
	metadataService         *metadata.MetadataService
	poolManager             pool.Manager
	maxValidationGoroutines int
	fullSegmentValidation   bool
	log                     *slog.Logger
}

// NewProcessSingleFileStep creates a step to process a single file
func NewProcessSingleFileStep(
	metadataService *metadata.MetadataService,
	poolManager pool.Manager,
	maxGoroutines int,
	fullValidation bool,
	log *slog.Logger,
) *ProcessSingleFileStep {
	return &ProcessSingleFileStep{
		metadataService:         metadataService,
		poolManager:             poolManager,
		maxValidationGoroutines: maxGoroutines,
		fullSegmentValidation:   fullValidation,
		log:                     log,
	}
}

// Execute processes the single file
func (s *ProcessSingleFileStep) Execute(ctx context.Context, pctx *ProcessingContext) error {
	if len(pctx.RegularFiles) == 0 {
		return fmt.Errorf("no regular files to process")
	}

	file := pctx.RegularFiles[0]

	// Handle potential filename collisions
	uniqueFilename := GetUniqueFilename(pctx.VirtualDir, file.Filename, pctx.CurrentBatch, s.metadataService)

	// Create virtual file path with potentially adjusted filename
	virtualFilePath := filepath.Join(pctx.VirtualDir, uniqueFilename)
	virtualFilePath = strings.ReplaceAll(virtualFilePath, string(filepath.Separator), "/")

	// Track this file in current batch
	TrackBatchFile(virtualFilePath, pctx.CurrentBatch)

	// Validate segments
	if err := ValidateSegmentsForFile(
		file.Filename,
		file.Size,
		file.Segments,
		file.Encryption,
		s.poolManager,
		s.maxValidationGoroutines,
		s.fullSegmentValidation,
	); err != nil {
		return err
	}

	// Create file metadata
	fileMeta := s.metadataService.CreateFileMetadata(
		file.Size,
		pctx.Parsed.Path,
		metapb.FileStatus_FILE_STATUS_HEALTHY,
		file.Segments,
		file.Encryption,
		file.Password,
		file.Salt,
	)

	// Write file metadata to disk
	if err := s.metadataService.WriteFileMetadata(virtualFilePath, fileMeta); err != nil {
		return fmt.Errorf("failed to write metadata for single file %s: %w", file.Filename, err)
	}

	s.log.Info("Successfully processed single file",
		"file", file.Filename,
		"virtual_path", virtualFilePath,
		"size", file.Size)

	// Set the final virtual path
	pctx.VirtualPath = virtualFilePath

	return nil
}

// Name returns the step name
func (s *ProcessSingleFileStep) Name() string {
	return "ProcessSingleFile"
}

// ProcessMultipleFilesStep processes multiple regular files
type ProcessMultipleFilesStep struct {
	metadataService         *metadata.MetadataService
	poolManager             pool.Manager
	maxValidationGoroutines int
	fullSegmentValidation   bool
	log                     *slog.Logger
}

// NewProcessMultipleFilesStep creates a step to process multiple files
func NewProcessMultipleFilesStep(
	metadataService *metadata.MetadataService,
	poolManager pool.Manager,
	maxGoroutines int,
	fullValidation bool,
	log *slog.Logger,
) *ProcessMultipleFilesStep {
	return &ProcessMultipleFilesStep{
		metadataService:         metadataService,
		poolManager:             poolManager,
		maxValidationGoroutines: maxGoroutines,
		fullSegmentValidation:   fullValidation,
		log:                     log,
	}
}

// Execute processes all regular files
func (s *ProcessMultipleFilesStep) Execute(ctx context.Context, pctx *ProcessingContext) error {
	if len(pctx.RegularFiles) == 0 {
		// Set virtual path to the directory itself
		pctx.VirtualPath = pctx.VirtualDir
		return nil
	}

	for _, file := range pctx.RegularFiles {
		parentPath, filename := DetermineFileLocation(file, pctx.VirtualDir)

		// Ensure parent directory exists
		if err := EnsureDirectoryExists(parentPath, s.metadataService); err != nil {
			return fmt.Errorf("failed to create parent directory %s: %w", parentPath, err)
		}

		// Handle potential filename collisions
		uniqueFilename := GetUniqueFilename(parentPath, filename, pctx.CurrentBatch, s.metadataService)

		// Create virtual file path with potentially adjusted filename
		virtualPath := filepath.Join(parentPath, uniqueFilename)
		virtualPath = strings.ReplaceAll(virtualPath, string(filepath.Separator), "/")

		// Track this file in current batch
		TrackBatchFile(virtualPath, pctx.CurrentBatch)

		// Validate segments
		if err := ValidateSegmentsForFile(
			filename,
			file.Size,
			file.Segments,
			file.Encryption,
			s.poolManager,
			s.maxValidationGoroutines,
			s.fullSegmentValidation,
		); err != nil {
			return err
		}

		// Create file metadata
		fileMeta := s.metadataService.CreateFileMetadata(
			file.Size,
			pctx.Parsed.Path,
			metapb.FileStatus_FILE_STATUS_HEALTHY,
			file.Segments,
			file.Encryption,
			file.Password,
			file.Salt,
		)

		// Write file metadata to disk
		if err := s.metadataService.WriteFileMetadata(virtualPath, fileMeta); err != nil {
			return fmt.Errorf("failed to write metadata for file %s: %w", filename, err)
		}

		s.log.Debug("Created metadata file",
			"file", filename,
			"virtual_path", virtualPath,
			"size", file.Size)
	}

	s.log.Info("Successfully processed multiple files",
		"virtual_dir", pctx.VirtualDir,
		"files", len(pctx.RegularFiles))

	// Set the final virtual path to the directory
	pctx.VirtualPath = pctx.VirtualDir

	return nil
}

// Name returns the step name
func (s *ProcessMultipleFilesStep) Name() string {
	return "ProcessMultipleFiles"
}
