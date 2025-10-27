package steps

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/javi11/altmount/internal/importer/archive/rar"
	"github.com/javi11/altmount/internal/importer/archive/sevenzip"
	"github.com/javi11/altmount/internal/importer/parser"
	"github.com/javi11/altmount/internal/metadata"
	"github.com/javi11/altmount/internal/pool"
)

// Step represents a single processing step in the pipeline
type Step interface {
	Execute(ctx context.Context, pctx *ProcessingContext) error
	Name() string
}

// ProcessingContext holds the state and data passed between pipeline steps
type ProcessingContext struct {
	// Input data
	Parsed     *parser.ParsedNzb
	VirtualDir string

	// Batch tracking for collision detection
	CurrentBatch map[string]bool

	// Processor reference for accessing dependencies
	Processor interface{} // Will be *importer.Processor but avoid circular import

	// Results from previous steps
	DirectoryStructure *DirectoryStructure
	RegularFiles       []parser.ParsedFile
	ArchiveFiles       []parser.ParsedFile
	Par2Files          []parser.ParsedFile

	// Archive-specific content (rar.Content or sevenzip.Content)
	ArchiveContents interface{}

	// Final result
	VirtualPath string
}

// DirectoryStructure represents the analyzed directory structure
type DirectoryStructure struct {
	Directories []DirectoryInfo
	CommonRoot  string
}

// DirectoryInfo represents information about a directory
type DirectoryInfo struct {
	Path   string
	Name   string
	Parent *string
}

// Pipeline represents a sequence of processing steps
type Pipeline struct {
	name  string
	steps []Step
}

// NewPipeline creates a new processing pipeline
func NewPipeline(name string, steps ...Step) *Pipeline {
	return &Pipeline{
		name:  name,
		steps: steps,
	}
}

// Execute runs all steps in the pipeline sequentially
func (p *Pipeline) Execute(ctx context.Context, pctx *ProcessingContext) (string, error) {
	for _, step := range p.steps {
		if err := step.Execute(ctx, pctx); err != nil {
			return "", fmt.Errorf("%s failed in pipeline %s: %w", step.Name(), p.name, err)
		}
	}

	// Return the final virtual path
	if pctx.VirtualPath == "" {
		return "", fmt.Errorf("pipeline %s completed but no virtual path was set", p.name)
	}

	return pctx.VirtualPath, nil
}

// Name returns the pipeline name
func (p *Pipeline) Name() string {
	return p.name
}

// PipelineConfig holds the dependencies needed to create pipelines
type PipelineConfig struct {
	MetadataService         *metadata.MetadataService
	RarProcessor            rar.Processor
	SevenZipProcessor       sevenzip.Processor
	PoolManager             pool.Manager
	MaxValidationGoroutines int
	FullSegmentValidation   bool
	Log                     *slog.Logger
}

// BuildSingleFilePipeline creates a pipeline for single file processing
func BuildSingleFilePipeline(cfg *PipelineConfig) *Pipeline {
	return NewPipeline("SingleFile",
		NewSeparateFilesStep(parser.NzbTypeSingleFile),
		NewEnsureDirectoryStep(cfg.MetadataService),
		NewProcessSingleFileStep(
			cfg.MetadataService,
			cfg.PoolManager,
			cfg.MaxValidationGoroutines,
			cfg.FullSegmentValidation,
			cfg.Log,
		),
	)
}

// BuildMultiFilePipeline creates a pipeline for multi-file processing
func BuildMultiFilePipeline(cfg *PipelineConfig) *Pipeline {
	return NewPipeline("MultiFile",
		NewCreateNzbFolderStep(cfg.MetadataService),
		NewSeparateFilesStep(parser.NzbTypeMultiFile),
		NewAnalyzeDirectoryStructureStep(cfg.MetadataService),
		NewCreateDirectoriesStep(cfg.MetadataService),
		NewProcessMultipleFilesStep(
			cfg.MetadataService,
			cfg.PoolManager,
			cfg.MaxValidationGoroutines,
			cfg.FullSegmentValidation,
			cfg.Log,
		),
	)
}

// BuildRarArchivePipeline creates a pipeline for RAR archive processing
func BuildRarArchivePipeline(cfg *PipelineConfig) *Pipeline {
	return NewPipeline("RarArchive",
		NewCreateNzbFolderStep(cfg.MetadataService),
		NewSeparateFilesStep(parser.NzbTypeRarArchive),
		// Process regular files if any
		NewAnalyzeDirectoryStructureStep(cfg.MetadataService),
		NewCreateDirectoriesStep(cfg.MetadataService),
		NewProcessMultipleFilesStep(
			cfg.MetadataService,
			cfg.PoolManager,
			cfg.MaxValidationGoroutines,
			cfg.FullSegmentValidation,
			cfg.Log,
		),
		// Process RAR archives
		NewAnalyzeRarContentStep(cfg.RarProcessor, cfg.Log),
		NewProcessRarArchiveFilesStep(
			cfg.RarProcessor,
			cfg.MetadataService,
			cfg.PoolManager,
			cfg.MaxValidationGoroutines,
			cfg.FullSegmentValidation,
			cfg.Log,
		),
	)
}

// BuildSevenZipArchivePipeline creates a pipeline for 7zip archive processing
func BuildSevenZipArchivePipeline(cfg *PipelineConfig) *Pipeline {
	return NewPipeline("SevenZipArchive",
		NewCreateNzbFolderStep(cfg.MetadataService),
		NewSeparateFilesStep(parser.NzbType7zArchive),
		// Process regular files if any
		NewAnalyzeDirectoryStructureStep(cfg.MetadataService),
		NewCreateDirectoriesStep(cfg.MetadataService),
		NewProcessMultipleFilesStep(
			cfg.MetadataService,
			cfg.PoolManager,
			cfg.MaxValidationGoroutines,
			cfg.FullSegmentValidation,
			cfg.Log,
		),
		// Process 7zip archives
		NewAnalyzeSevenZipContentStep(cfg.SevenZipProcessor, cfg.Log),
		NewProcessSevenZipArchiveFilesStep(
			cfg.SevenZipProcessor,
			cfg.MetadataService,
			cfg.PoolManager,
			cfg.MaxValidationGoroutines,
			cfg.FullSegmentValidation,
			cfg.Log,
		),
	)
}

// BuildStrmFilePipeline creates a pipeline for STRM file processing
func BuildStrmFilePipeline(cfg *PipelineConfig) *Pipeline {
	return NewPipeline("StrmFile",
		NewEnsureDirectoryStep(cfg.MetadataService),
		NewProcessSingleFileStep(
			cfg.MetadataService,
			cfg.PoolManager,
			cfg.MaxValidationGoroutines,
			cfg.FullSegmentValidation,
			cfg.Log,
		),
	)
}
