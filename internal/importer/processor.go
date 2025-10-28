package importer

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"strings"

	"github.com/javi11/altmount/internal/importer/archive/rar"
	"github.com/javi11/altmount/internal/importer/archive/sevenzip"
	"github.com/javi11/altmount/internal/importer/parser"
	"github.com/javi11/altmount/internal/importer/steps"
	"github.com/javi11/altmount/internal/metadata"
	"github.com/javi11/altmount/internal/pool"
)

// Processor handles the processing and storage of parsed NZB files using metadata storage
type Processor struct {
	parser                  *parser.Parser
	strmParser              *parser.StrmParser
	metadataService         *metadata.MetadataService
	rarProcessor            rar.Processor
	sevenZipProcessor       sevenzip.Processor
	poolManager             pool.Manager // Pool manager for dynamic pool access
	maxValidationGoroutines int          // Maximum concurrent goroutines for segment validation
	fullSegmentValidation   bool         // Whether to validate all segments or just a random sample
	log                     *slog.Logger

	// Pre-compiled regex patterns for RAR file sorting
	rarPartPattern    *regexp.Regexp // pattern.part###.rar
	rarRPattern       *regexp.Regexp // pattern.r### or pattern.r##
	rarNumericPattern *regexp.Regexp // pattern.### (numeric extensions)
}

// NewProcessor creates a new NZB processor using metadata storage
func NewProcessor(metadataService *metadata.MetadataService, poolManager pool.Manager, maxValidationGoroutines int, fullSegmentValidation bool) *Processor {
	return &Processor{
		parser:                  parser.NewParser(poolManager),
		strmParser:              parser.NewStrmParser(),
		metadataService:         metadataService,
		rarProcessor:            rar.NewProcessor(poolManager, 10, 64),      // 10 max workers, 64MB cache for RAR analysis
		sevenZipProcessor:       sevenzip.NewProcessor(poolManager, 10, 64), // 10 max workers, 64MB cache for 7zip analysis
		poolManager:             poolManager,
		maxValidationGoroutines: maxValidationGoroutines,
		fullSegmentValidation:   fullSegmentValidation,
		log:                     slog.Default().With("component", "nzb-processor"),

		// Initialize pre-compiled regex patterns for RAR file sorting
		rarPartPattern:    regexp.MustCompile(`^(.+)\.part(\d+)\.rar$`), // filename.part001.rar
		rarRPattern:       regexp.MustCompile(`^(.+)\.r(\d+)$`),         // filename.r00, filename.r01
		rarNumericPattern: regexp.MustCompile(`^(.+)\.(\d+)$`),          // filename.001, filename.002
	}
}

// ProcessNzbFile processes an NZB or STRM file maintaining the folder structure relative to relative path
func (proc *Processor) ProcessNzbFile(ctx context.Context, filePath, relativePath string) (string, error) {
	// Open and parse the file
	file, err := os.Open(filePath)
	if err != nil {
		return "", NewNonRetryableError("failed to open file", err)
	}
	defer file.Close()

	var parsed *parser.ParsedNzb

	// Determine file type and parse accordingly
	if strings.HasSuffix(strings.ToLower(filePath), ".strm") {
		parsed, err = proc.strmParser.ParseStrmFile(file, filePath)
		if err != nil {
			return "", NewNonRetryableError("failed to parse STRM file", err)
		}

		// Validate the parsed STRM
		if err := proc.strmParser.ValidateStrmFile(parsed); err != nil {
			return "", NewNonRetryableError("STRM validation failed", err)
		}
	} else {
		parsed, err = proc.parser.ParseFile(file, filePath)
		if err != nil {
			return "", NewNonRetryableError("failed to parse NZB file", err)
		}

		// Validate the parsed NZB
		if err := proc.parser.ValidateNzb(parsed); err != nil {
			return "", NewNonRetryableError("NZB validation failed", err)
		}
	}

	// Calculate the relative virtual directory path for this file
	virtualDir := steps.CalculateVirtualDirectory(filePath, relativePath)

	// Initialize batch tracking map for this import
	// Tracks all files created in this import to handle collisions correctly
	currentBatchFiles := make(map[string]bool)

	proc.log.Info("Processing file",
		"file_path", filePath,
		"virtual_dir", virtualDir,
		"type", parsed.Type,
		"total_size", parsed.TotalSize,
		"files", len(parsed.Files))

	// Create processing context
	pctx := &steps.ProcessingContext{
		Parsed:       parsed,
		VirtualDir:   virtualDir,
		CurrentBatch: currentBatchFiles,
		Processor:    proc,
	}

	// Select and execute pipeline based on file type
	pipeline := proc.getPipelineForType(parsed.Type)
	if pipeline == nil {
		return "", NewNonRetryableError(fmt.Sprintf("unknown file type: %s", parsed.Type), nil)
	}

	return pipeline.Execute(ctx, pctx)
}

// getPipelineForType returns the appropriate pipeline for a given file type
func (proc *Processor) getPipelineForType(fileType parser.NzbType) *steps.Pipeline {
	cfg := &steps.PipelineConfig{
		MetadataService:         proc.metadataService,
		RarProcessor:            proc.rarProcessor,
		SevenZipProcessor:       proc.sevenZipProcessor,
		PoolManager:             proc.poolManager,
		MaxValidationGoroutines: proc.maxValidationGoroutines,
		FullSegmentValidation:   proc.fullSegmentValidation,
		Log:                     proc.log,
	}

	switch fileType {
	case parser.NzbTypeSingleFile:
		return steps.BuildSingleFilePipeline(cfg)
	case parser.NzbTypeMultiFile:
		return steps.BuildMultiFilePipeline(cfg)
	case parser.NzbTypeRarArchive:
		return steps.BuildRarArchivePipeline(cfg)
	case parser.NzbType7zArchive:
		return steps.BuildSevenZipArchivePipeline(cfg)
	case parser.NzbTypeStrm:
		return steps.BuildStrmFilePipeline(cfg)
	default:
		return nil
	}
}
