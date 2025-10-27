package steps

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/javi11/altmount/internal/importer/archive/rar"
	"github.com/javi11/altmount/internal/importer/archive/sevenzip"
	"github.com/javi11/altmount/internal/metadata"
	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/javi11/altmount/internal/pool"
)

// AnalyzeRarContentStep analyzes RAR archive content
type AnalyzeRarContentStep struct {
	rarProcessor rar.Processor
	log          *slog.Logger
}

// NewAnalyzeRarContentStep creates a step to analyze RAR content
func NewAnalyzeRarContentStep(rarProcessor rar.Processor, log *slog.Logger) *AnalyzeRarContentStep {
	return &AnalyzeRarContentStep{
		rarProcessor: rarProcessor,
		log:          log,
	}
}

// Execute analyzes RAR archive content
func (s *AnalyzeRarContentStep) Execute(ctx context.Context, pctx *ProcessingContext) error {
	if len(pctx.ArchiveFiles) == 0 {
		return nil
	}

	s.log.Info("Processing RAR archive with content analysis",
		"parts", len(pctx.ArchiveFiles),
		"rar_dir", pctx.VirtualDir)

	// Analyze RAR content with timeout
	password := pctx.Parsed.GetPassword()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	rarContents, err := s.rarProcessor.AnalyzeRarContentFromNzb(ctx, pctx.ArchiveFiles, password)
	if err != nil {
		s.log.Error("Failed to analyze RAR archive content", "error", err)
		return err
	}

	s.log.Info("Successfully analyzed RAR archive content", "files_in_archive", len(rarContents))

	// Store results in context
	pctx.ArchiveContents = rarContents

	return nil
}

// Name returns the step name
func (s *AnalyzeRarContentStep) Name() string {
	return "AnalyzeRarContent"
}

// ProcessRarArchiveFilesStep processes files extracted from RAR archives
type ProcessRarArchiveFilesStep struct {
	rarProcessor            rar.Processor
	metadataService         *metadata.MetadataService
	poolManager             pool.Manager
	maxValidationGoroutines int
	fullSegmentValidation   bool
	log                     *slog.Logger
}

// NewProcessRarArchiveFilesStep creates a step to process RAR archive files
func NewProcessRarArchiveFilesStep(
	rarProcessor rar.Processor,
	metadataService *metadata.MetadataService,
	poolManager pool.Manager,
	maxGoroutines int,
	fullValidation bool,
	log *slog.Logger,
) *ProcessRarArchiveFilesStep {
	return &ProcessRarArchiveFilesStep{
		rarProcessor:            rarProcessor,
		metadataService:         metadataService,
		poolManager:             poolManager,
		maxValidationGoroutines: maxGoroutines,
		fullSegmentValidation:   fullValidation,
		log:                     log,
	}
}

// Execute processes all files from the RAR archive
func (s *ProcessRarArchiveFilesStep) Execute(ctx context.Context, pctx *ProcessingContext) error {
	if pctx.ArchiveContents == nil {
		return nil
	}

	rarContents, ok := pctx.ArchiveContents.([]rar.Content)
	if !ok {
		return fmt.Errorf("invalid archive content type for RAR processing")
	}

	for _, rarContent := range rarContents {
		// Skip directories
		if rarContent.IsDirectory {
			s.log.Debug("Skipping directory in RAR archive", "path", rarContent.InternalPath)
			continue
		}

		// Flatten the internal path by extracting only the base filename
		normalizedInternalPath := strings.ReplaceAll(rarContent.InternalPath, "\\", "/")
		baseFilename := filepath.Base(normalizedInternalPath)

		// Generate a unique filename to handle duplicates
		uniqueFilename := GetUniqueFilename(pctx.VirtualDir, baseFilename, pctx.CurrentBatch, s.metadataService)

		// Create the virtual file path directly in the RAR directory (flattened)
		virtualFilePath := filepath.Join(pctx.VirtualDir, uniqueFilename)
		virtualFilePath = strings.ReplaceAll(virtualFilePath, string(filepath.Separator), "/")

		// Track this file in current batch
		TrackBatchFile(virtualFilePath, pctx.CurrentBatch)

		// Validate segments
		if err := ValidateSegmentsForFile(
			ctx,
			baseFilename,
			rarContent.Size,
			rarContent.Segments,
			metapb.Encryption_NONE,
			s.poolManager,
			s.maxValidationGoroutines,
			s.fullSegmentValidation,
		); err != nil {
			return err
		}

		// Create file metadata using the RAR handler's helper function
		fileMeta := s.rarProcessor.CreateFileMetadataFromRarContent(rarContent, pctx.Parsed.Path)

		// Write file metadata to disk
		if err := s.metadataService.WriteFileMetadata(virtualFilePath, fileMeta); err != nil {
			return fmt.Errorf("failed to write metadata for RAR file %s: %w", rarContent.Filename, err)
		}
	}

	s.log.Info("Successfully processed RAR archive files", "files_processed", len(rarContents))

	// Set the final virtual path
	pctx.VirtualPath = pctx.VirtualDir

	return nil
}

// Name returns the step name
func (s *ProcessRarArchiveFilesStep) Name() string {
	return "ProcessRarArchiveFiles"
}

// AnalyzeSevenZipContentStep analyzes 7zip archive content
type AnalyzeSevenZipContentStep struct {
	sevenZipProcessor sevenzip.Processor
	log               *slog.Logger
}

// NewAnalyzeSevenZipContentStep creates a step to analyze 7zip content
func NewAnalyzeSevenZipContentStep(sevenZipProcessor sevenzip.Processor, log *slog.Logger) *AnalyzeSevenZipContentStep {
	return &AnalyzeSevenZipContentStep{
		sevenZipProcessor: sevenZipProcessor,
		log:               log,
	}
}

// Execute analyzes 7zip archive content
func (s *AnalyzeSevenZipContentStep) Execute(ctx context.Context, pctx *ProcessingContext) error {
	if len(pctx.ArchiveFiles) == 0 {
		return nil
	}

	s.log.Info("Processing 7zip archive with content analysis",
		"parts", len(pctx.ArchiveFiles),
		"7z_dir", pctx.VirtualDir)

	// Analyze 7zip content with timeout
	password := pctx.Parsed.GetPassword()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	sevenZipContents, err := s.sevenZipProcessor.AnalyzeSevenZipContentFromNzb(ctx, pctx.ArchiveFiles, password)
	if err != nil {
		s.log.Error("Failed to analyze 7zip archive content", "error", err)
		return err
	}

	s.log.Info("Successfully analyzed 7zip archive content", "files_in_archive", len(sevenZipContents))

	// Store results in context
	pctx.ArchiveContents = sevenZipContents

	return nil
}

// Name returns the step name
func (s *AnalyzeSevenZipContentStep) Name() string {
	return "AnalyzeSevenZipContent"
}

// ProcessSevenZipArchiveFilesStep processes files extracted from 7zip archives
type ProcessSevenZipArchiveFilesStep struct {
	sevenZipProcessor       sevenzip.Processor
	metadataService         *metadata.MetadataService
	poolManager             pool.Manager
	maxValidationGoroutines int
	fullSegmentValidation   bool
	log                     *slog.Logger
}

// NewProcessSevenZipArchiveFilesStep creates a step to process 7zip archive files
func NewProcessSevenZipArchiveFilesStep(
	sevenZipProcessor sevenzip.Processor,
	metadataService *metadata.MetadataService,
	poolManager pool.Manager,
	maxGoroutines int,
	fullValidation bool,
	log *slog.Logger,
) *ProcessSevenZipArchiveFilesStep {
	return &ProcessSevenZipArchiveFilesStep{
		sevenZipProcessor:       sevenZipProcessor,
		metadataService:         metadataService,
		poolManager:             poolManager,
		maxValidationGoroutines: maxGoroutines,
		fullSegmentValidation:   fullValidation,
		log:                     log,
	}
}

// Execute processes all files from the 7zip archive
func (s *ProcessSevenZipArchiveFilesStep) Execute(ctx context.Context, pctx *ProcessingContext) error {
	if pctx.ArchiveContents == nil {
		return nil
	}

	sevenZipContents, ok := pctx.ArchiveContents.([]sevenzip.Content)
	if !ok {
		return fmt.Errorf("invalid archive content type for 7zip processing")
	}

	for _, sevenZipContent := range sevenZipContents {
		// Skip directories
		if sevenZipContent.IsDirectory {
			s.log.Debug("Skipping directory in 7zip archive", "path", sevenZipContent.InternalPath)
			continue
		}

		// Flatten the internal path by extracting only the base filename
		normalizedInternalPath := strings.ReplaceAll(sevenZipContent.InternalPath, "\\", "/")
		baseFilename := filepath.Base(normalizedInternalPath)

		// Generate a unique filename to handle duplicates
		uniqueFilename := GetUniqueFilename(pctx.VirtualDir, baseFilename, pctx.CurrentBatch, s.metadataService)

		// Create the virtual file path directly in the 7zip directory (flattened)
		virtualFilePath := filepath.Join(pctx.VirtualDir, uniqueFilename)
		virtualFilePath = strings.ReplaceAll(virtualFilePath, string(filepath.Separator), "/")

		// Track this file in current batch
		TrackBatchFile(virtualFilePath, pctx.CurrentBatch)

		// Validate segments
		if err := ValidateSegmentsForFile(
			ctx,
			baseFilename,
			sevenZipContent.Size,
			sevenZipContent.Segments,
			metapb.Encryption_NONE,
			s.poolManager,
			s.maxValidationGoroutines,
			s.fullSegmentValidation,
		); err != nil {
			return err
		}

		// Create file metadata using the 7zip handler's helper function
		fileMeta := s.sevenZipProcessor.CreateFileMetadataFromSevenZipContent(sevenZipContent, pctx.Parsed.Path)

		// Write file metadata to disk
		if err := s.metadataService.WriteFileMetadata(virtualFilePath, fileMeta); err != nil {
			return fmt.Errorf("failed to write metadata for 7zip file %s: %w", sevenZipContent.Filename, err)
		}

		s.log.Debug("Created metadata for 7zip extracted file",
			"file", uniqueFilename,
			"original_internal_path", sevenZipContent.InternalPath,
			"virtual_path", virtualFilePath,
			"size", sevenZipContent.Size,
			"segments", len(sevenZipContent.Segments))
	}

	s.log.Info("Successfully processed 7zip archive files", "files_processed", len(sevenZipContents))

	// Set the final virtual path
	pctx.VirtualPath = pctx.VirtualDir

	return nil
}

// Name returns the step name
func (s *ProcessSevenZipArchiveFilesStep) Name() string {
	return "ProcessSevenZipArchiveFiles"
}
