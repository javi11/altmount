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
	"github.com/javi11/altmount/internal/importer/filesystem"
	"github.com/javi11/altmount/internal/importer/multifile"
	"github.com/javi11/altmount/internal/importer/parser"
	"github.com/javi11/altmount/internal/importer/singlefile"
	"github.com/javi11/altmount/internal/metadata"
	"github.com/javi11/altmount/internal/pool"
	"github.com/javi11/altmount/internal/progress"
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
	allowedFileExtensions   []string     // Allowed file extensions for validation (empty = allow all)
	log                     *slog.Logger
	broadcaster             *progress.ProgressBroadcaster // WebSocket progress broadcaster

	// Pre-compiled regex patterns for RAR file sorting
	rarPartPattern    *regexp.Regexp // pattern.part###.rar
	rarRPattern       *regexp.Regexp // pattern.r### or pattern.r##
	rarNumericPattern *regexp.Regexp // pattern.### (numeric extensions)
}

// NewProcessor creates a new NZB processor using metadata storage
func NewProcessor(metadataService *metadata.MetadataService, poolManager pool.Manager, maxValidationGoroutines int, fullSegmentValidation bool, allowedFileExtensions []string, maxImportConnections int, importCacheSizeMB int, broadcaster *progress.ProgressBroadcaster) *Processor {
	return &Processor{
		parser:                  parser.NewParser(poolManager),
		strmParser:              parser.NewStrmParser(),
		metadataService:         metadataService,
		rarProcessor:            rar.NewProcessor(poolManager, maxImportConnections, importCacheSizeMB),
		sevenZipProcessor:       sevenzip.NewProcessor(poolManager, maxImportConnections, importCacheSizeMB),
		poolManager:             poolManager,
		maxValidationGoroutines: maxValidationGoroutines,
		fullSegmentValidation:   fullSegmentValidation,
		allowedFileExtensions:   allowedFileExtensions,
		log:                     slog.Default().With("component", "nzb-processor"),
		broadcaster:             broadcaster,

		// Initialize pre-compiled regex patterns for RAR file sorting
		rarPartPattern:    regexp.MustCompile(`^(.+)\.part(\d+)\.rar$`), // filename.part001.rar
		rarRPattern:       regexp.MustCompile(`^(.+)\.r(\d+)$`),         // filename.r00, filename.r01
		rarNumericPattern: regexp.MustCompile(`^(.+)\.(\d+)$`),          // filename.001, filename.002
	}
}

// updateProgress emits a progress update if broadcaster is available
func (proc *Processor) updateProgress(queueID int, percentage int) {
	if proc.broadcaster != nil {
		proc.broadcaster.UpdateProgress(queueID, percentage)
	}
}

// ProcessNzbFile processes an NZB or STRM file maintaining the folder structure relative to relative path
func (proc *Processor) ProcessNzbFile(ctx context.Context, filePath, relativePath string, queueID int) (string, error) {
	// Update progress: starting
	proc.updateProgress(queueID, 0)
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

	// Update progress: parsing complete
	proc.updateProgress(queueID, 10)

	// Step 2: Calculate virtual directory
	virtualDir := filesystem.CalculateVirtualDirectory(filePath, relativePath)

	proc.log.Info("Processing file",
		"file_path", filePath,
		"virtual_dir", virtualDir,
		"type", parsed.Type,
		"total_size", parsed.TotalSize,
		"files", len(parsed.Files))

	// Step 3: Separate files by type (regular, archive, PAR2)
	regularFiles, archiveFiles, _ := filesystem.SeparateFiles(parsed.Files, parsed.Type)

	// Step 4: Process based on file type
	var result string
	switch parsed.Type {
	case parser.NzbTypeSingleFile:
		proc.updateProgress(queueID, 30)
		result, err = proc.processSingleFile(ctx, virtualDir, regularFiles, parsed.Path)

	case parser.NzbTypeMultiFile:
		proc.updateProgress(queueID, 30)
		result, err = proc.processMultiFile(ctx, virtualDir, regularFiles, parsed.Path)

	case parser.NzbTypeRarArchive:
		proc.updateProgress(queueID, 30)
		result, err = proc.processRarArchive(ctx, virtualDir, regularFiles, archiveFiles, parsed, queueID)

	case parser.NzbType7zArchive:
		proc.updateProgress(queueID, 30)
		result, err = proc.processSevenZipArchive(ctx, virtualDir, regularFiles, archiveFiles, parsed, queueID)

	case parser.NzbTypeStrm:
		proc.updateProgress(queueID, 30)
		result, err = proc.processSingleFile(ctx, virtualDir, regularFiles, parsed.Path)

	default:
		return "", NewNonRetryableError(fmt.Sprintf("unknown file type: %s", parsed.Type), nil)
	}

	// Update progress: complete
	if err == nil {
		proc.updateProgress(queueID, 100)
	}

	return result, err
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
	if err := filesystem.EnsureDirectoryExists(virtualDir, proc.metadataService); err != nil {
		return "", err
	}

	// Process the single file
	result, err := singlefile.ProcessSingleFile(
		ctx,
		virtualDir,
		regularFiles[0],
		nzbPath,
		proc.metadataService,
		proc.poolManager,
		proc.maxValidationGoroutines,
		proc.fullSegmentValidation,
		proc.allowedFileExtensions,
		proc.log,
	)
	if err != nil {
		return "", err
	}

	return result, nil
}

// processMultiFile handles multi-file imports
func (proc *Processor) processMultiFile(
	ctx context.Context,
	virtualDir string,
	regularFiles []parser.ParsedFile,
	nzbPath string,
) (string, error) {
	// Create NZB folder
	nzbFolder, err := filesystem.CreateNzbFolder(virtualDir, filepath.Base(nzbPath), proc.metadataService)
	if err != nil {
		return "", err
	}

	// Create directories for files
	if err := filesystem.CreateDirectoriesForFiles(nzbFolder, regularFiles, proc.metadataService); err != nil {
		return "", err
	}

	// Process all regular files
	if err := multifile.ProcessRegularFiles(
		ctx,
		nzbFolder,
		regularFiles,
		nzbPath,
		proc.metadataService,
		proc.poolManager,
		proc.maxValidationGoroutines,
		proc.fullSegmentValidation,
		proc.allowedFileExtensions,
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
	queueID int,
) (string, error) {
	// Create NZB folder
	nzbFolder, err := filesystem.CreateNzbFolder(virtualDir, filepath.Base(parsed.Path), proc.metadataService)
	if err != nil {
		return "", err
	}

	// Process regular files first if any
	if len(regularFiles) > 0 {
		if err := filesystem.CreateDirectoriesForFiles(nzbFolder, regularFiles, proc.metadataService); err != nil {
			return "", err
		}

		if err := multifile.ProcessRegularFiles(
			ctx,
			nzbFolder,
			regularFiles,
			parsed.Path,
			proc.metadataService,
			proc.poolManager,
			proc.maxValidationGoroutines,
			proc.fullSegmentValidation,
			proc.allowedFileExtensions,
			proc.log,
		); err != nil {
			slog.DebugContext(ctx, "Failed to process regular files", "error", err)
		}
	}

	// Analyze and process RAR archive
	if len(archiveFiles) > 0 {
		proc.updateProgress(queueID, 50)

		// Create progress tracker for 50-90% range
		progressTracker := proc.broadcaster.CreateTracker(queueID, 50, 90)

		// Get release date from first archive file
		var releaseDate int64
		if len(archiveFiles) > 0 {
			releaseDate = archiveFiles[0].ReleaseDate.Unix()
		}

		// Process archive with unified aggregator
		err := rar.ProcessArchive(
			ctx,
			nzbFolder,
			archiveFiles,
			parsed.GetPassword(),
			releaseDate,
			parsed.Path,
			proc.rarProcessor,
			proc.metadataService,
			proc.poolManager,
			progressTracker,
			proc.maxValidationGoroutines,
			proc.fullSegmentValidation,
			proc.allowedFileExtensions,
			proc.log,
		)
		if err != nil {
			return "", err
		}
		proc.updateProgress(queueID, 90)
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
	queueID int,
) (string, error) {
	// Create NZB folder
	nzbFolder, err := filesystem.CreateNzbFolder(virtualDir, filepath.Base(parsed.Path), proc.metadataService)
	if err != nil {
		return "", err
	}

	// Process regular files first if any
	if len(regularFiles) > 0 {
		if err := filesystem.CreateDirectoriesForFiles(nzbFolder, regularFiles, proc.metadataService); err != nil {
			return "", err
		}

		if err := multifile.ProcessRegularFiles(
			ctx,
			nzbFolder,
			regularFiles,
			parsed.Path,
			proc.metadataService,
			proc.poolManager,
			proc.maxValidationGoroutines,
			proc.fullSegmentValidation,
			proc.allowedFileExtensions,
			proc.log,
		); err != nil {
			slog.DebugContext(ctx, "Failed to process regular files", "error", err)
		}
	}

	// Analyze and process 7zip archive
	if len(archiveFiles) > 0 {
		proc.updateProgress(queueID, 50)

		// Create progress tracker for 50-90% range
		progressTracker := proc.broadcaster.CreateTracker(queueID, 50, 90)

		// Get release date from first archive file
		var releaseDate int64
		if len(archiveFiles) > 0 {
			releaseDate = archiveFiles[0].ReleaseDate.Unix()
		}

		// Process archive with unified aggregator
		err := sevenzip.ProcessArchive(
			ctx,
			nzbFolder,
			archiveFiles,
			parsed.GetPassword(),
			releaseDate,
			parsed.Path,
			proc.sevenZipProcessor,
			proc.metadataService,
			proc.poolManager,
			progressTracker,
			proc.maxValidationGoroutines,
			proc.fullSegmentValidation,
			proc.allowedFileExtensions,
			proc.log,
		)
		if err != nil {
			return "", err
		}
		proc.updateProgress(queueID, 90)
	}

	return nzbFolder, nil
}
