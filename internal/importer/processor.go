package importer

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/altmount/internal/database"
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
	configGetter            config.ConfigGetter
	maxImportConnections    int // Maximum concurrent NNTP connections for validation and archive processing
	segmentSamplePercentage int // Percentage of segments to check when sampling (1-100)
	validationTimeout       time.Duration
	allowedFileExtensions   []string
	log                     *slog.Logger
	broadcaster             *progress.ProgressBroadcaster // WebSocket progress broadcaster
	recorder                HistoryRecorder

	// Pre-compiled regex patterns for RAR file sorting
	rarPartPattern  *regexp.Regexp // pattern.part###.rar
	rarPartPattern2 *regexp.Regexp // pattern.r###
}

// NewProcessor creates a new NZB processor using metadata storage
func NewProcessor(metadataService *metadata.MetadataService, poolManager pool.Manager, maxImportConnections int, segmentSamplePercentage int, allowedFileExtensions []string, maxDownloadPrefetch int, readTimeout time.Duration, broadcaster *progress.ProgressBroadcaster, configGetter config.ConfigGetter, recorder HistoryRecorder) *Processor {
	return &Processor{
		parser:                  parser.NewParser(poolManager),
		strmParser:              parser.NewStrmParser(),
		metadataService:         metadataService,
		rarProcessor:            rar.NewProcessor(poolManager, maxImportConnections, maxDownloadPrefetch, readTimeout),
		sevenZipProcessor:       sevenzip.NewProcessor(poolManager, maxDownloadPrefetch, readTimeout),
		poolManager:             poolManager,
		configGetter:            configGetter,
		maxImportConnections:    maxImportConnections,
		segmentSamplePercentage: segmentSamplePercentage,
		validationTimeout:       30 * time.Second, // Default validation timeout for imports
		allowedFileExtensions:   allowedFileExtensions,
		log:                     slog.Default().With("component", "nzb-processor"),
		broadcaster:             broadcaster,
		recorder:                recorder,

		// Initialize pre-compiled regex patterns for RAR file sorting
		rarPartPattern:  regexp.MustCompile(`(?i)^(.+)\.part(\d+)\.rar$`), // filename.part001.rar
		rarPartPattern2: regexp.MustCompile(`(?i)^(.+)\.r(\d+)$`),         // filename.r00
	}
}

// getCleanNzbName removes the queue ID prefix from the NZB filename if present
func (proc *Processor) getCleanNzbName(nzbPath string, queueID int) string {
	baseName := filepath.Base(nzbPath)
	prefix := fmt.Sprintf("%d_", queueID)
	if after, ok := strings.CutPrefix(baseName, prefix); ok {
		return after
	}
	return baseName
}

func (proc *Processor) SetSegmentSamplePercentage(percentage int) {
	proc.segmentSamplePercentage = percentage
}

func (proc *Processor) SetRecorder(recorder HistoryRecorder) {
	proc.recorder = recorder
}

func (proc *Processor) isCategoryFolder(path string) bool {
	cfg := proc.configGetter()
	normalizedPath := strings.Trim(filepath.ToSlash(path), "/")
	completeDir := strings.Trim(filepath.ToSlash(cfg.SABnzbd.CompleteDir), "/")

	// Helper to check if a name matches a category
	matchesCategory := func(name string) bool {
		name = strings.Trim(filepath.ToSlash(name), "/")
		if name == "" {
			return false
		}

		// Check exact match
		if normalizedPath == name {
			return true
		}

		// Check match with complete_dir prefix (e.g. complete/tv)
		if completeDir != "" && normalizedPath == strings.Trim(completeDir+"/"+name, "/") {
			return true
		}

		return false
	}

	// Check complete_dir itself
	if normalizedPath == completeDir {
		return true
	}

	// Check configured categories
	for _, cat := range cfg.SABnzbd.Categories {
		// Check both the category name and its specific directory if set
		if matchesCategory(cat.Name) {
			return true
		}
		if cat.Dir != "" && matchesCategory(cat.Dir) {
			return true
		}
	}

	return false
}

// updateProgress emits a progress update if broadcaster is available
func (proc *Processor) updateProgress(queueID int, percentage int) {
	if proc.broadcaster != nil {
		proc.broadcaster.UpdateProgress(queueID, percentage)
	}
}

// checkCancellation checks if processing should be cancelled
func (proc *Processor) checkCancellation(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return fmt.Errorf("processing cancelled: %w", ctx.Err())
	default:
		return nil
	}
}

// ProcessNzbFile processes an NZB or STRM file maintaining the folder structure relative to relative path
func (proc *Processor) ProcessNzbFile(ctx context.Context, filePath, relativePath string, queueID int, allowedExtensionsOverride *[]string, virtualDirOverride *string, extractedFiles []parser.ExtractedFileInfo, category *string) (string, error) {
	// Determine max connections to use
	maxConnections := proc.maxImportConnections

	// Determine allowed file extensions to use
	allowedExtensions := proc.allowedFileExtensions
	if allowedExtensionsOverride != nil {
		allowedExtensions = *allowedExtensionsOverride
	}

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

	// Attach extracted files metadata if available (optimization)
	if len(extractedFiles) > 0 {
		parsed.ExtractedFiles = extractedFiles
	}
	// Update progress: parsing complete
	proc.updateProgress(queueID, 10)

	// Check for cancellation after parsing
	if err := proc.checkCancellation(ctx); err != nil {
		return "", err
	}

	// Step 2: Calculate virtual directory
	virtualDir := ""
	if virtualDirOverride != nil {
		virtualDir = *virtualDirOverride
	} else {
		virtualDir = filesystem.CalculateVirtualDirectory(filePath, relativePath)
	}

	proc.log.InfoContext(ctx, "Processing file",
		"file_path", filePath,
		"virtual_dir", virtualDir,
		"type", parsed.Type,
		"total_size", parsed.TotalSize,
		"files", len(parsed.Files),
		"max_connections", maxConnections)

	// Step 3: Separate files by type (regular, archive, PAR2)
	regularFiles, archiveFiles, par2Files := filesystem.SeparateFiles(parsed.Files, parsed.Type)

	// Check for cancellation before main processing
	if err := proc.checkCancellation(ctx); err != nil {
		return "", err
	}

	// Step 4: Process based on file type
	var result string
	switch parsed.Type {
	case parser.NzbTypeSingleFile:
		proc.updateProgress(queueID, 30)
		result, err = proc.processSingleFile(ctx, virtualDir, regularFiles, par2Files, parsed.Path, queueID, maxConnections, allowedExtensions, proc.validationTimeout, category)

	case parser.NzbTypeMultiFile:
		proc.updateProgress(queueID, 30)
		result, err = proc.processMultiFile(ctx, virtualDir, regularFiles, par2Files, parsed.Path, queueID, maxConnections, allowedExtensions, proc.validationTimeout, category)

	case parser.NzbTypeRarArchive:
		proc.updateProgress(queueID, 30)
		result, err = proc.processRarArchive(ctx, virtualDir, regularFiles, archiveFiles, parsed, queueID, maxConnections, allowedExtensions, proc.validationTimeout, parsed.ExtractedFiles, category)

	case parser.NzbType7zArchive:
		proc.updateProgress(queueID, 30)
		result, err = proc.processSevenZipArchive(ctx, virtualDir, regularFiles, archiveFiles, parsed, queueID, maxConnections, allowedExtensions, proc.validationTimeout, parsed.ExtractedFiles, category)

	case parser.NzbTypeStrm:
		proc.updateProgress(queueID, 30)
		result, err = proc.processSingleFile(ctx, virtualDir, regularFiles, par2Files, parsed.Path, queueID, maxConnections, allowedExtensions, proc.validationTimeout, category)

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
	par2Files []parser.ParsedFile,
	nzbPath string,
	queueID int,
	maxConnections int,
	allowedExtensions []string,
	timeout time.Duration,
	category *string,
) (string, error) {
	if len(regularFiles) == 0 {
		return "", fmt.Errorf("no regular files to process")
	}

	// Normalize virtualDir only for synthetic duplicate folders; skip if the NZB actually lives inside a
	// real directory named like the release (e.g. .../Season 01/<file>/<file>.nzb).
	nzbName := proc.getCleanNzbName(nzbPath, queueID)
	releaseName := strings.TrimSuffix(nzbName, filepath.Ext(nzbName))
	nzbDirBase := filepath.Base(filepath.Dir(nzbPath))
	fileDir := filepath.Dir(regularFiles[0].Filename)
	if fileDir == "." || fileDir == "" {
		// Only flatten when the enclosing folder is not the same real folder as the release name.
		if !strings.EqualFold(nzbDirBase, releaseName) && !strings.EqualFold(nzbDirBase, strings.TrimSuffix(regularFiles[0].Filename, filepath.Ext(regularFiles[0].Filename))) {
			normalizedDir := normalizeSingleFileVirtualDir(virtualDir, releaseName, regularFiles[0].Filename)

			// Only apply normalization if it doesn't result in a category root folder
			// We want to avoid flattening 'movies/MovieName/Movie.mkv' into 'movies/Movie.mkv'
			// because that confuses Sonarr/Radarr when they look for the job folder.
			if !proc.isCategoryFolder(normalizedDir) {
				virtualDir = normalizedDir
			}
		}
	}

	// Ensure we don't put the file directly into a category root folder
	// We MUST create a release folder so Sonarr/Radarr can find the "Job Folder"
	if proc.isCategoryFolder(virtualDir) {
		virtualDir = filepath.Join(virtualDir, releaseName)
		virtualDir = strings.ReplaceAll(virtualDir, string(filepath.Separator), "/")
	}

	// Rename the file to match the NZB name to handle obfuscated filenames
	// Keep NZB-provided subfolders but rename the leaf to the release name (preventing duplicate extensions)
	originalDir := filepath.Dir(regularFiles[0].Filename)
	normalizedBase := normalizeReleaseFilename(nzbName, filepath.Base(regularFiles[0].Filename))
	if originalDir != "." && originalDir != "" {
		regularFiles[0].Filename = filepath.Join(originalDir, normalizedBase)
	} else {
		regularFiles[0].Filename = normalizedBase
	}

	// Compute final parent/name, flattening only redundant nesting like file.mkv/file.mkv
	parentPath, finalName := filesystem.DetermineFileLocation(regularFiles[0], virtualDir)

	// Ensure the parent directory exists in metadata
	if err := filesystem.EnsureDirectoryExists(parentPath, proc.metadataService); err != nil {
		return "", err
	}

	// Use the final name for processing
	regularFiles[0].Filename = finalName

	// Determine sample percentage based on skipHealthCheck
	samplePercentage := proc.segmentSamplePercentage

	// Process the single file at the resolved parentPath
	result, err := singlefile.ProcessSingleFile(
		ctx,
		parentPath,
		regularFiles[0],
		par2Files,
		nzbPath,
		proc.metadataService,
		proc.poolManager,
		maxConnections,
		samplePercentage,
		allowedExtensions,
		timeout,
	)
	if err != nil {
		return "", err
	}

	// Record history
	if proc.recorder != nil {
		nzbID := int64(queueID)
		_ = proc.recorder.AddImportHistory(ctx, &database.ImportHistory{
			NzbID:       &nzbID,
			NzbName:     nzbName,
			FileName:    finalName,
			FileSize:    regularFiles[0].Size,
			VirtualPath: result,
			Category:    category,
			CompletedAt: time.Now(),
		})
	}

	return result, nil
}

// processMultiFile handles multi-file imports
func (proc *Processor) processMultiFile(
	ctx context.Context,
	virtualDir string,
	regularFiles []parser.ParsedFile,
	par2Files []parser.ParsedFile,
	nzbPath string,
	queueID int,
	maxConnections int,
	allowedExtensions []string,
	timeout time.Duration,
	category *string,
) (string, error) {
	// If there's only one regular file (and the rest are likely PAR2s), avoid creating a redundant
	// NZB-named directory that matches the file itself. Instead, keep the file directly under the
	// provided virtual directory (preserving any subpaths inside the NZB).
	// EXCEPTION: If the virtual directory is a category root (e.g. "movies"), we MUST create
	// the NZB folder to ensure Radarr/Sonarr can find the job folder correctly.
	singleLike := len(regularFiles) == 1 && !proc.isCategoryFolder(virtualDir)
	targetBaseDir := virtualDir
	nzbName := proc.getCleanNzbName(nzbPath, queueID)

	if singleLike {
		// Rename the leaf to the release name (prevents ext duplication) but keep NZB-provided subfolders.
		originalDir := filepath.Dir(regularFiles[0].Filename)
		normalizedBase := normalizeReleaseFilename(nzbName, filepath.Base(regularFiles[0].Filename))
		if originalDir != "." && originalDir != "" {
			regularFiles[0].Filename = filepath.Join(originalDir, normalizedBase)
		} else {
			regularFiles[0].Filename = normalizedBase
		}

		// Avoid nesting like /Season 02/<release>/<release>.mkv; drop the NZB-named folder here.
		if err := filesystem.EnsureDirectoryExists(targetBaseDir, proc.metadataService); err != nil {
			return "", err
		}
	} else {
		// Create NZB folder for true multi-file imports
		nzbFolder, err := filesystem.CreateNzbFolder(virtualDir, nzbName, proc.metadataService)
		if err != nil {
			return "", err
		}

		// Create directories for files
		if err := filesystem.CreateDirectoriesForFiles(nzbFolder, regularFiles, proc.metadataService); err != nil {
			return "", err
		}

		targetBaseDir = nzbFolder
	}

	// Determine sample percentage based on skipHealthCheck
	samplePercentage := proc.segmentSamplePercentage

	// Process all regular files
	if err := multifile.ProcessRegularFiles(
		ctx,
		targetBaseDir,
		regularFiles,
		par2Files,
		nzbPath,
		proc.metadataService,
		proc.poolManager,
		maxConnections,
		samplePercentage,
		allowedExtensions,
		timeout,
	); err != nil {
		return "", err
	}

	// Record history
	if proc.recorder != nil {
		nzbID := int64(queueID)
		var totalSize int64
		for _, f := range regularFiles {
			totalSize += f.Size
		}

		_ = proc.recorder.AddImportHistory(ctx, &database.ImportHistory{
			NzbID:       &nzbID,
			NzbName:     nzbName,
			FileName:    filepath.Base(targetBaseDir),
			FileSize:    totalSize,
			VirtualPath: targetBaseDir,
			Category:    category,
			CompletedAt: time.Now(),
		})
	}

	return targetBaseDir, nil
}

// processRarArchive handles RAR archive imports
func (proc *Processor) processRarArchive(
	ctx context.Context,
	virtualDir string,
	regularFiles []parser.ParsedFile,
	archiveFiles []parser.ParsedFile,
	parsed *parser.ParsedNzb,
	queueID int,
	maxConnections int,
	allowedExtensions []string,
	timeout time.Duration,
	extractedFiles []parser.ExtractedFileInfo,
	category *string,
) (string, error) {
	// Create NZB folder
	nzbName := proc.getCleanNzbName(parsed.Path, queueID)
	nzbFolder, err := filesystem.CreateNzbFolder(virtualDir, nzbName, proc.metadataService)
	if err != nil {
		return nzbFolder, err
	}

	// Process regular files first if any
	if len(regularFiles) > 0 {
		if err := filesystem.CreateDirectoriesForFiles(nzbFolder, regularFiles, proc.metadataService); err != nil {
			return nzbFolder, err
		}

		if err := multifile.ProcessRegularFiles(
			ctx,
			nzbFolder,
			regularFiles,
			nil, // No PAR2 files for archive imports
			parsed.Path,
			proc.metadataService,
			proc.poolManager,
			maxConnections,
			proc.segmentSamplePercentage,
			allowedExtensions,
			proc.validationTimeout,
		); err != nil {
			slog.DebugContext(ctx, "Failed to process regular files", "error", err)
		}
	}

	// Analyze and process RAR archive
	if len(archiveFiles) > 0 {
		proc.updateProgress(queueID, 50)

		// Create progress tracker for 50-80% range (archive analysis)
		archiveProgressTracker := proc.broadcaster.CreateTracker(queueID, 50, 80)

		// Get release date from first archive file
		var releaseDate int64
		if len(archiveFiles) > 0 {
			releaseDate = archiveFiles[0].ReleaseDate.Unix()
		}

		// Create progress tracker for 80-95% range (validation only, metadata handled separately)
		validationProgressTracker := proc.broadcaster.CreateTracker(queueID, 80, 95)

		// Determine sample percentage based on skipHealthCheck
		samplePercentage := proc.segmentSamplePercentage

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
			archiveProgressTracker,
			validationProgressTracker,
			maxConnections,
			samplePercentage,
			allowedExtensions,
			timeout,
			extractedFiles,
		)
		if err != nil {
			return nzbFolder, err
		} // Archive analysis complete, validation and finalization will happen in aggregator (80-100%)
	}

	// Record history
	if proc.recorder != nil {
		nzbID := int64(queueID)
		var totalSize int64
		for _, f := range regularFiles {
			totalSize += f.Size
		}
		for _, f := range archiveFiles {
			totalSize += f.Size
		}

		_ = proc.recorder.AddImportHistory(ctx, &database.ImportHistory{
			NzbID:       &nzbID,
			NzbName:     nzbName,
			FileName:    filepath.Base(nzbFolder),
			FileSize:    totalSize,
			VirtualPath: nzbFolder,
			Category:    category,
			CompletedAt: time.Now(),
		})
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
	maxConnections int,
	allowedExtensions []string,
	timeout time.Duration,
	extractedFiles []parser.ExtractedFileInfo,
	category *string,
) (string, error) {
	// Create NZB folder
	nzbName := proc.getCleanNzbName(parsed.Path, queueID)
	nzbFolder, err := filesystem.CreateNzbFolder(virtualDir, nzbName, proc.metadataService)
	if err != nil {
		return nzbFolder, err
	}

	// Process regular files first if any
	if len(regularFiles) > 0 {
		if err := filesystem.CreateDirectoriesForFiles(nzbFolder, regularFiles, proc.metadataService); err != nil {
			return nzbFolder, err
		}

		if err := multifile.ProcessRegularFiles(
			ctx,
			nzbFolder,
			regularFiles,
			nil, // No PAR2 files for archive imports
			parsed.Path,
			proc.metadataService,
			proc.poolManager,
			maxConnections,
			proc.segmentSamplePercentage,
			allowedExtensions,
			proc.validationTimeout,
		); err != nil {
			slog.DebugContext(ctx, "Failed to process regular files", "error", err)
		}
	}

	// Analyze and process 7zip archive
	if len(archiveFiles) > 0 {
		proc.updateProgress(queueID, 50)

		// Create progress tracker for 50-80% range (archive analysis)
		archiveProgressTracker := proc.broadcaster.CreateTracker(queueID, 50, 80)

		// Get release date from first archive file
		var releaseDate int64
		if len(archiveFiles) > 0 {
			releaseDate = archiveFiles[0].ReleaseDate.Unix()
		}

		// Create progress tracker for 80-95% range (validation only, metadata handled separately)
		validationProgressTracker := proc.broadcaster.CreateTracker(queueID, 80, 95)

		// Determine sample percentage based on skipHealthCheck
		samplePercentage := proc.segmentSamplePercentage

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
			archiveProgressTracker,
			validationProgressTracker,
			maxConnections,
			samplePercentage,
			allowedExtensions,
			timeout,
			extractedFiles,
		)
		if err != nil {
			return nzbFolder, err
		} // Archive analysis complete, validation and finalization will happen in aggregator (80-100%)
	}

	// Record history
	if proc.recorder != nil {
		nzbID := int64(queueID)
		var totalSize int64
		for _, f := range regularFiles {
			totalSize += f.Size
		}
		for _, f := range archiveFiles {
			totalSize += f.Size
		}

		_ = proc.recorder.AddImportHistory(ctx, &database.ImportHistory{
			NzbID:       &nzbID,
			NzbName:     nzbName,
			FileName:    filepath.Base(nzbFolder),
			FileSize:    totalSize,
			VirtualPath: nzbFolder,
			Category:    category,
			CompletedAt: time.Now(),
		})
	}

	return nzbFolder, nil
}

// normalizeReleaseFilename aligns the filename to the NZB basename while keeping the original extension.
// It avoids generating duplicate extensions like ".mp4.mp4" when the NZB name already contains the suffix.
func normalizeReleaseFilename(nzbFilename, originalFilename string) string {
	releaseName := strings.TrimSuffix(nzbFilename, filepath.Ext(nzbFilename))
	fileExt := filepath.Ext(originalFilename)

	if fileExt == "" {
		return releaseName
	}

	if strings.HasSuffix(strings.ToLower(releaseName), strings.ToLower(fileExt)) {
		return releaseName
	}

	return releaseName + fileExt
}

// normalizeSingleFileVirtualDir flattens paths where the last directory component matches
// the release name or filename, avoiding redundant nesting like file.mkv/file.mkv.
func normalizeSingleFileVirtualDir(virtualDir, releaseName, filename string) string {
	cleanDir := filepath.Clean(virtualDir)
	if cleanDir == "." || cleanDir == string(filepath.Separator) {
		return "/"
	}

	base := filepath.Base(cleanDir)
	fileNoExt := strings.TrimSuffix(filename, filepath.Ext(filename))

	if strings.EqualFold(base, releaseName) || strings.EqualFold(base, filename) || strings.EqualFold(base, fileNoExt) {
		cleanDir = filepath.Dir(cleanDir)
		if cleanDir == "." {
			cleanDir = "/"
		}
	}

	return strings.ReplaceAll(cleanDir, string(filepath.Separator), "/")
}
