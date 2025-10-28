package importer

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/javi11/altmount/internal/importer/archive/rar"
	"github.com/javi11/altmount/internal/importer/archive/sevenzip"
	"github.com/javi11/altmount/internal/importer/parser"
	"github.com/javi11/altmount/internal/importer/steps"
	"github.com/javi11/altmount/internal/metadata"
	"github.com/javi11/altmount/internal/pool"
)

const (
	strmFileExtension = ".strm"
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
	// Step 1: Open and parse the file
	file, err := os.Open(filePath)
	if err != nil {
		return "", NewNonRetryableError("failed to open file", err)
	}
	defer file.Close()

	var parsed *parser.ParsedNzb

	// Determine file type and parse accordingly
	if strings.HasSuffix(strings.ToLower(filePath), strmFileExtension) {
		parsed, err = proc.strmParser.ParseStrmFile(file, filePath)
		if err != nil {
			return "", NewNonRetryableError("failed to parse STRM file", err)
		}

		// Validate the parsed STRM
		if err := proc.strmParser.ValidateStrmFile(parsed); err != nil {
			return "", NewNonRetryableError("STRM validation failed", err)
		}
	} else {
		parsed, err = proc.parser.ParseFile(ctx, file, filePath)
		if err != nil {
			return "", NewNonRetryableError("failed to parse NZB file", err)
		}

		// Validate the parsed NZB
		if err := proc.parser.ValidateNzb(parsed); err != nil {
			return "", NewNonRetryableError("NZB validation failed", err)
		}
	}

	// Step 2: Calculate virtual directory
	virtualDir := steps.CalculateVirtualDirectory(filePath, relativePath)

	proc.log.Info("Processing file",
		"file_path", filePath,
		"virtual_dir", virtualDir,
		"type", parsed.Type,
		"total_size", parsed.TotalSize,
		"files", len(parsed.Files))

	// Step 3: Separate files by type (regular, archive, PAR2)
	regularFiles, archiveFiles, _ := steps.SeparateFiles(parsed.Files, parsed.Type)

	// Step 4: Process based on file type
	switch parsed.Type {
	case parser.NzbTypeSingleFile:
		return proc.processSingleFile(ctx, virtualDir, regularFiles, parsed.Path)

	case parser.NzbTypeMultiFile:
		return proc.processMultiFile(ctx, virtualDir, regularFiles, parsed.Path)

	case parser.NzbTypeRarArchive:
		return proc.processRarArchive(ctx, virtualDir, regularFiles, archiveFiles, parsed)

	case parser.NzbType7zArchive:
		return proc.processSevenZipArchive(ctx, virtualDir, regularFiles, archiveFiles, parsed)

	case parser.NzbTypeStrm:
		return proc.processSingleFile(ctx, virtualDir, regularFiles, parsed.Path)

	default:
		return "", NewNonRetryableError(fmt.Sprintf("unknown file type: %s", parsed.Type), nil)
	}
}

// processSingleFile handles single file imports
func (proc *Processor) processSingleFile(
	ctx context.Context,
	virtualDir string,
	regularFiles []parser.ParsedFile,
	nzbPath string,
) (string, error) {
	if len(regularFiles) == 0 {
		return "", fmt.Errorf("no regular files to process")
	}

	// Ensure directory exists
	if err := steps.EnsureDirectoryExists(virtualDir, proc.metadataService); err != nil {
		return "", err
	}

	// Process the single file
	return steps.ProcessSingleFile(
		ctx,
		virtualDir,
		regularFiles[0],
		nzbPath,
		proc.metadataService,
		proc.poolManager,
		proc.maxValidationGoroutines,
		proc.fullSegmentValidation,
		proc.log,
	)
}

// processMultiFile handles multi-file imports
func (proc *Processor) processMultiFile(
	ctx context.Context,
	virtualDir string,
	regularFiles []parser.ParsedFile,
	nzbPath string,
) (string, error) {
	// Create NZB folder
	nzbFolder, err := steps.CreateNzbFolder(virtualDir, filepath.Base(nzbPath), proc.metadataService)
	if err != nil {
		return "", err
	}

	// Create directories for files
	if err := steps.CreateDirectoriesForFiles(nzbFolder, regularFiles, proc.metadataService); err != nil {
		return "", err
	}

	// Process all regular files
	if err := steps.ProcessRegularFiles(
		ctx,
		nzbFolder,
		regularFiles,
		nzbPath,
		proc.metadataService,
		proc.poolManager,
		proc.maxValidationGoroutines,
		proc.fullSegmentValidation,
		proc.log,
	); err != nil {
		return "", err
	}

	return nzbFolder, nil
}

// processRarArchive handles RAR archive imports
func (proc *Processor) processRarArchive(
	ctx context.Context,
	virtualDir string,
	regularFiles []parser.ParsedFile,
	archiveFiles []parser.ParsedFile,
	parsed *parser.ParsedNzb,
) (string, error) {
	// Create NZB folder
	nzbFolder, err := steps.CreateNzbFolder(virtualDir, filepath.Base(parsed.Path), proc.metadataService)
	if err != nil {
		return "", err
	}

	// Process regular files first if any
	if len(regularFiles) > 0 {
		if err := steps.CreateDirectoriesForFiles(nzbFolder, regularFiles, proc.metadataService); err != nil {
			return "", err
		}

		if err := steps.ProcessRegularFiles(
			ctx,
			nzbFolder,
			regularFiles,
			parsed.Path,
			proc.metadataService,
			proc.poolManager,
			proc.maxValidationGoroutines,
			proc.fullSegmentValidation,
			proc.log,
		); err != nil {
			return "", err
		}
	}

	// Analyze and process RAR archive
	if len(archiveFiles) > 0 {
		rarContents, err := steps.AnalyzeRarArchive(
			ctx,
			archiveFiles,
			parsed.GetPassword(),
			proc.rarProcessor,
			proc.log,
		)
		if err != nil {
			return "", err
		}

		if err := steps.ProcessRarArchiveFiles(
			ctx,
			nzbFolder,
			rarContents,
			parsed.Path,
			proc.rarProcessor,
			proc.metadataService,
			proc.poolManager,
			proc.maxValidationGoroutines,
			proc.fullSegmentValidation,
			proc.log,
		); err != nil {
			return "", err
		}
	}

	return nzbFolder, nil
}

// processSevenZipArchive handles 7zip archive imports
func (proc *Processor) processSevenZipArchive(
	ctx context.Context,
	virtualDir string,
	regularFiles []parser.ParsedFile,
	archiveFiles []parser.ParsedFile,
	parsed *parser.ParsedNzb,
) (string, error) {
	// Create NZB folder
	nzbFolder, err := steps.CreateNzbFolder(virtualDir, filepath.Base(parsed.Path), proc.metadataService)
	if err != nil {
		return "", err
	}

	// Process regular files first if any
	if len(regularFiles) > 0 {
		if err := steps.CreateDirectoriesForFiles(nzbFolder, regularFiles, proc.metadataService); err != nil {
			return "", err
		}

		if err := steps.ProcessRegularFiles(
			ctx,
			nzbFolder,
			regularFiles,
			parsed.Path,
			proc.metadataService,
			proc.poolManager,
			proc.maxValidationGoroutines,
			proc.fullSegmentValidation,
			proc.log,
		); err != nil {
			return "", err
		}
	}

	// Analyze and process 7zip archive
	if len(archiveFiles) > 0 {
		sevenZipContents, err := steps.AnalyzeSevenZipArchive(
			ctx,
			archiveFiles,
			parsed.GetPassword(),
			proc.sevenZipProcessor,
			proc.log,
		)
		if err != nil {
			return "", err
		}

		if err := steps.ProcessSevenZipArchiveFiles(
			ctx,
			nzbFolder,
			sevenZipContents,
			parsed.Path,
			proc.sevenZipProcessor,
			proc.metadataService,
			proc.poolManager,
			proc.maxValidationGoroutines,
			proc.fullSegmentValidation,
			proc.log,
		); err != nil {
			return "", err
		}
	}

	return nzbFolder, nil
}
