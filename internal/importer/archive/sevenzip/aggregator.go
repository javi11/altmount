package sevenzip

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	concpool "github.com/sourcegraph/conc/pool"

	"github.com/javi11/altmount/internal/importer/archive"
	"github.com/javi11/altmount/internal/importer/filesystem"
	"github.com/javi11/altmount/internal/importer/parser"
	"github.com/javi11/altmount/internal/importer/utils"
	"github.com/javi11/altmount/internal/importer/utils/nzbtrim"
	"github.com/javi11/altmount/internal/importer/validation"
	"github.com/javi11/altmount/internal/metadata"
	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/javi11/altmount/internal/pool"
	"github.com/javi11/altmount/internal/progress"
)

var (
	// ErrNoAllowedFiles indicates that the archive contains no files matching allowed extensions
	ErrNoAllowedFiles = archive.ErrNoAllowedFiles
	// ErrNoFilesProcessed indicates that no files were successfully processed (all files failed validation)
	ErrNoFilesProcessed = archive.ErrNoFilesProcessed
)

// getContentSegments delegates to archive.GetContentSegments.
func getContentSegments(content Content) []*metapb.SegmentData {
	return archive.GetContentSegments(content)
}

// validateSegmentIntegrity delegates to archive.ValidateSegmentIntegrity.
func validateSegmentIntegrity(ctx context.Context, content Content) error {
	return archive.ValidateSegmentIntegrity(ctx, content)
}

// newErrNoAllowedFiles builds a descriptive error showing which extensions were found
// vs which are allowed, making it actionable when imports fail silently.
func newErrNoAllowedFiles(sevenZipContents []Content, allowedExtensions []string) error {
	extSet := make(map[string]struct{})
	for _, c := range sevenZipContents {
		if c.IsDirectory {
			continue
		}
		ext := strings.ToLower(filepath.Ext(c.Filename))
		if ext == "" {
			ext = "(no extension)"
		}
		extSet[ext] = struct{}{}
	}
	found := make([]string, 0, len(extSet))
	for ext := range extSet {
		found = append(found, ext)
	}
	return fmt.Errorf("archive contains no files with allowed extensions (found: %v, allowed: %v)", found, allowedExtensions)
}

// hasAllowedFiles checks if any files within 7zip archive contents match allowed extensions
// If allowedExtensions is empty, all file types are allowed
func hasAllowedFiles(sevenZipContents []Content, allowedExtensions []string, filterSamples bool) bool {
	for _, content := range sevenZipContents {
		// Skip directories
		if content.IsDirectory {
			continue
		}
		// Check both the internal path and filename
		if utils.IsAllowedFile(content.InternalPath, content.Size, allowedExtensions, filterSamples) ||
			utils.IsAllowedFile(content.Filename, content.Size, allowedExtensions, filterSamples) {
			return true
		}
	}
	return false
}

// ProcessArchiveOptions holds all parameters for ProcessArchive.
type ProcessArchiveOptions struct {
	VirtualDir             string
	ArchiveFiles           []parser.ParsedFile
	Password               string
	ReleaseDate            int64
	NzbPath                string
	Processor              Processor
	MetadataService        *metadata.MetadataService
	PoolManager            pool.Manager
	ArchiveProgressTracker *progress.Tracker
	AllowedFileExtensions  []string
	ExtractedFiles         []parser.ExtractedFileInfo
	MaxPrefetch            int
	ReadTimeout            time.Duration
	IsoAnalyzeTimeout      time.Duration
	ExpandBlurayIso        bool
	FilterSamples          bool
	RenameToNzbName        bool
}

// ProcessArchive analyzes and processes 7zip archive files, creating metadata for all extracted files.
// This function handles the complete workflow: analysis → file processing → metadata creation.
func ProcessArchive(ctx context.Context, opts ProcessArchiveOptions) error {
	archiveFiles := opts.ArchiveFiles
	virtualDir := opts.VirtualDir
	password := opts.Password
	releaseDate := opts.ReleaseDate
	nzbPath := opts.NzbPath
	sevenZipProcessor := opts.Processor
	metadataService := opts.MetadataService
	poolManager := opts.PoolManager
	archiveProgressTracker := opts.ArchiveProgressTracker
	allowedFileExtensions := opts.AllowedFileExtensions
	extractedFiles := opts.ExtractedFiles
	maxPrefetch := opts.MaxPrefetch
	readTimeout := opts.ReadTimeout
	analyzeTimeout := opts.IsoAnalyzeTimeout
	expandBlurayIso := opts.ExpandBlurayIso
	filterSamples := opts.FilterSamples
	renameToNzbName := opts.RenameToNzbName

	if len(archiveFiles) == 0 {
		return nil
	}

	slog.InfoContext(ctx, "Analyzing 7zip archive content", "parts", len(archiveFiles))

	// Analyze 7zip content with timeout
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	sevenZipContents, err := sevenZipProcessor.AnalyzeSevenZipContentFromNzb(ctx, archiveFiles, password, archiveProgressTracker)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to analyze 7zip archive content", "error", err)
		return err
	}

	slog.InfoContext(ctx, "Successfully analyzed 7zip archive content", "files_in_archive", len(sevenZipContents))

	// Expand ISO files found inside the 7zip archive into their inner media
	// files. ISO analysis (filesystem walk + Blu-ray playlist resolution over
	// NNTP) can take tens of seconds, so it gets its own progress label.
	// Slice(0,1) copies the archive tracker at the same range without mutating
	// it (7z header analysis above is already done); WithStage relabels the
	// copy. For archives with no ISO, ExpandISOContents emits no updates, so
	// the common case is unaffected.
	var isoProgressTracker *progress.Tracker
	if archiveProgressTracker != nil {
		isoProgressTracker = archiveProgressTracker.Slice(0, 1).WithStage("Analyzing ISO")
	}
	sevenZipContents, err = archive.ExpandISOContents(ctx, expandBlurayIso, sevenZipContents, poolManager, maxPrefetch, readTimeout, analyzeTimeout, allowedFileExtensions, isoProgressTracker)
	if err != nil {
		slog.WarnContext(ctx, "ISO expansion failed, proceeding without ISO contents", "error", err)
	}

	// Validate file extensions before processing
	if !hasAllowedFiles(sevenZipContents, allowedFileExtensions, filterSamples) {
		err := newErrNoAllowedFiles(sevenZipContents, allowedFileExtensions)
		slog.WarnContext(ctx, "7zip archive contains no files with allowed extensions", "error", err)
		return err
	}

	slog.InfoContext(ctx, "Starting 7zip archive processing",
		"total_files", len(sevenZipContents))

	// Determine if we should rename the file to match the NZB basename
	// Only do this if there's exactly one media file in the archive
	mediaFilesCount := 0
	for _, content := range sevenZipContents {
		if !content.IsDirectory && (utils.IsAllowedFile(content.InternalPath, content.Size, allowedFileExtensions, filterSamples) ||
			utils.IsAllowedFile(content.Filename, content.Size, allowedFileExtensions, filterSamples)) {
			mediaFilesCount++
		}
	}

	nzbName := filepath.Base(nzbPath)
	releaseName := nzbtrim.TrimNzbExtension(nzbName)
	shouldNormalizeName := renameToNzbName && mediaFilesCount == 1

	// Count ISO-expanded files so single-file ISOs omit the index suffix.
	isoExpandedCount := 0
	for _, c := range sevenZipContents {
		if c.ISOExpansionIndex > 0 {
			isoExpandedCount++
		}
	}

	// Pre-pass: resolve paths, apply renames.
	type fileToProcess struct {
		content         Content
		baseFilename    string
		virtualFilePath string
		isPreExtracted  bool
	}

	var filesToProcess []fileToProcess
	preProcessedCount := 0 // healthy files already counted as processed

	for _, sevenZipContent := range sevenZipContents {
		if sevenZipContent.IsDirectory {
			slog.DebugContext(ctx, "Skipping directory in 7zip archive", "path", sevenZipContent.InternalPath)
			continue
		}

		normalizedInternalPath := strings.ReplaceAll(sevenZipContent.InternalPath, "\\", "/")
		baseFilename := filepath.Base(normalizedInternalPath)
		internalSubDir := filepath.ToSlash(filepath.Dir(normalizedInternalPath))

		if !utils.IsAllowedFile(sevenZipContent.InternalPath, sevenZipContent.Size, allowedFileExtensions, filterSamples) &&
			!utils.IsAllowedFile(sevenZipContent.Filename, sevenZipContent.Size, allowedFileExtensions, filterSamples) {
			continue
		}

		if sevenZipContent.ISOExpansionIndex > 0 {
			ext := filepath.Ext(sevenZipContent.Filename)
			if isoExpandedCount == 1 {
				baseFilename = releaseName + ext
			} else {
				baseFilename = fmt.Sprintf("%s_%d%s", releaseName, sevenZipContent.ISOExpansionIndex, ext)
			}
			slog.InfoContext(ctx, "Renaming ISO-expanded file using NZB release name",
				"original", sevenZipContent.Filename,
				"renamed", baseFilename)
			internalSubDir = "."
		} else if shouldNormalizeName && (utils.IsAllowedFile(sevenZipContent.InternalPath, sevenZipContent.Size, allowedFileExtensions, filterSamples) ||
			utils.IsAllowedFile(sevenZipContent.Filename, sevenZipContent.Size, allowedFileExtensions, filterSamples)) {
			baseFilename = normalizeArchiveReleaseFilename(nzbName, baseFilename)
			slog.InfoContext(ctx, "Normalizing obfuscated filename in 7zip archive",
				"original", sevenZipContent.Filename,
				"normalized", baseFilename)
			internalSubDir = "."
		}

		var virtualFilePath string
		if internalSubDir == "." || internalSubDir == "" {
			virtualFilePath = filepath.Join(virtualDir, baseFilename)
		} else {
			subDir := filepath.Join(virtualDir, internalSubDir)
			if err := filesystem.EnsureDirectoryExists(subDir, metadataService); err != nil {
				return fmt.Errorf("failed to create archive subdirectory %s: %w", subDir, err)
			}
			virtualFilePath = filepath.Join(subDir, baseFilename)
		}
		virtualFilePath = strings.ReplaceAll(virtualFilePath, string(filepath.Separator), "/")

		if existingMeta, err := metadataService.ReadFileMetadata(virtualFilePath); err == nil && existingMeta != nil {
			if existingMeta.Status == metapb.FileStatus_FILE_STATUS_HEALTHY {
				slog.InfoContext(ctx, "Skipping re-import of healthy 7zip-extracted file",
					"file", baseFilename,
					"virtual_path", virtualFilePath)
				preProcessedCount++
				continue
			}
		}

		isPreExtracted := false
		for _, extracted := range extractedFiles {
			if extracted.Name == baseFilename && extracted.Size == sevenZipContent.Size {
				isPreExtracted = true
				break
			}
		}

		filesToProcess = append(filesToProcess, fileToProcess{
			content:         sevenZipContent,
			baseFilename:    baseFilename,
			virtualFilePath: virtualFilePath,
			isPreExtracted:  isPreExtracted,
		})
	}

	// Parallel pass: validate segments and write metadata for each file concurrently.
	var filesProcessed int32
	p := concpool.New().WithErrors().WithFirstError().WithContext(ctx)

	for _, item := range filesToProcess {
		item := item
		p.Go(func(ctx context.Context) error {
			if item.isPreExtracted {
				slog.InfoContext(ctx, "Skipping validation for pre-extracted file (found in database)",
					"file", item.baseFilename,
					"size", item.content.Size)
			} else {
				if err := validateSegmentIntegrity(ctx, item.content); err != nil {
					slog.ErrorContext(ctx, "Skipping SevenZip file due to segment integrity failure (missing segments in NZB)",
						"file", item.baseFilename,
						"error", err)
					return nil
				}

				validationSegments := getContentSegments(item.content)

				var validationSize int64
				if len(item.content.NestedSources) > 0 {
					for _, ns := range item.content.NestedSources {
						sourceSize := int64(0)
						for _, seg := range ns.Segments {
							sourceSize += seg.EndOffset - seg.StartOffset + 1
						}
						validationSize += sourceSize
					}
				} else {
					validationSize = item.content.Size
					if len(item.content.AesKey) > 0 {
						const aesBlockSize = 16
						if validationSize%aesBlockSize != 0 {
							validationSize = validationSize + (aesBlockSize - (validationSize % aesBlockSize))
						}
					}
				}

				// Local structural checks only; network reachability was confirmed at import start
				if err := validation.ValidateSegmentsForFile(
					item.baseFilename,
					validationSize,
					validationSegments,
					metapb.Encryption_NONE,
				); err != nil {
					slog.WarnContext(ctx, "Skipping 7zip file due to validation error", "error", err, "file", item.baseFilename)
					return nil
				}
			}

			fileMeta := sevenZipProcessor.CreateFileMetadataFromSevenZipContent(item.content, nzbPath, releaseDate, item.content.NzbdavID)

			metadataPath := metadataService.GetMetadataFilePath(item.virtualFilePath)
			if _, err := os.Stat(metadataPath); err == nil {
				_ = metadataService.DeleteFileMetadata(item.virtualFilePath)
			}

			if err := metadataService.WriteFileMetadata(item.virtualFilePath, fileMeta); err != nil {
				return fmt.Errorf("failed to write metadata for 7zip file %s: %w", item.content.Filename, err)
			}

			slog.InfoContext(ctx, "Created metadata for 7zip extracted file",
				"file", item.baseFilename,
				"virtual_path", item.virtualFilePath,
				"size", item.content.Size)

			atomic.AddInt32(&filesProcessed, 1)
			return nil
		})
	}

	if err := p.Wait(); err != nil {
		return err
	}

	if int(atomic.LoadInt32(&filesProcessed))+preProcessedCount == 0 && len(sevenZipContents) > 0 {
		return ErrNoFilesProcessed
	}

	slog.InfoContext(ctx, "Successfully processed 7zip archive files",
		"files_processed", int(atomic.LoadInt32(&filesProcessed))+preProcessedCount)

	return nil
}

// normalizeArchiveReleaseFilename aligns the filename to the NZB basename while keeping the original extension.
func normalizeArchiveReleaseFilename(nzbFilename, originalFilename string) string {
	releaseName := nzbtrim.TrimNzbExtension(nzbFilename)
	fileExt := filepath.Ext(originalFilename)

	if fileExt == "" {
		return releaseName
	}

	// If release name already contains the extension (e.g. Movie.mkv.nzb -> Movie.mkv), don't duplicate
	if strings.HasSuffix(strings.ToLower(releaseName), strings.ToLower(fileExt)) {
		return releaseName
	}

	return releaseName + fileExt
}
