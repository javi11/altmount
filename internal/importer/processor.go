package importer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/javi11/nntppool/v4"
	"github.com/javi11/nzbparser"

	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/altmount/internal/importer/archive"
	"github.com/javi11/altmount/internal/importer/archive/rar"
	"github.com/javi11/altmount/internal/importer/archive/sevenzip"
	"github.com/javi11/altmount/internal/importer/filesystem"
	"github.com/javi11/altmount/internal/importer/multifile"
	"github.com/javi11/altmount/internal/importer/parser"
	"github.com/javi11/altmount/internal/importer/singlefile"
	"github.com/javi11/altmount/internal/importer/utils/nzbtrim"
	"github.com/javi11/altmount/internal/importer/validation"
	"github.com/javi11/altmount/internal/metadata"
	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/javi11/altmount/internal/nzbfile"
	"github.com/javi11/altmount/internal/pool"
	"github.com/javi11/altmount/internal/progress"
)

const (
	strmFileExtension = ".strm"
)

// Processor handles the processing and storage of parsed NZB files using metadata storage
type Processor struct {
	parser            *parser.Parser
	strmParser        *parser.StrmParser
	metadataService   *metadata.MetadataService
	rarProcessor      rar.Processor
	sevenZipProcessor sevenzip.Processor
	poolManager       pool.Manager // Pool manager for dynamic pool access
	configGetter      config.ConfigGetter
	validationTimeout time.Duration
	log               *slog.Logger
	broadcaster       *progress.ProgressBroadcaster // WebSocket progress broadcaster
	recorder          HistoryRecorder

	// Pre-compiled regex patterns for RAR file sorting
	rarPartPattern  *regexp.Regexp // pattern.part###.rar
	rarPartPattern2 *regexp.Regexp // pattern.r###
}

// NewProcessor creates a new NZB processor using metadata storage
func NewProcessor(metadataService *metadata.MetadataService, poolManager pool.Manager, broadcaster *progress.ProgressBroadcaster, configGetter config.ConfigGetter, recorder HistoryRecorder) *Processor {
	return &Processor{
		parser:            parser.NewParser(poolManager, configGetter),
		strmParser:        parser.NewStrmParser(),
		metadataService:   metadataService,
		rarProcessor:      rar.NewProcessor(poolManager, configGetter),
		sevenZipProcessor: sevenzip.NewProcessor(poolManager, configGetter),
		poolManager:       poolManager,
		configGetter:      configGetter,
		validationTimeout: 30 * time.Second, // Default validation timeout for imports
		log:               slog.Default().With("component", "nzb-processor"),
		broadcaster:       broadcaster,
		recorder:          recorder,

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

func (proc *Processor) SetRecorder(recorder HistoryRecorder) {
	proc.recorder = recorder
}

func (proc *Processor) isCategoryFolder(path string, category *string) bool {
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
		// We must ensure it's at a directory boundary
		if completeDir != "" {
			prefix := strings.Trim(completeDir+"/"+name, "/")
			if normalizedPath == prefix {
				return true
			}
		}

		return false
	}

	// Check if path matches the provided category (for auto-detected categories)
	if category != nil && *category != "" {
		if matchesCategory(*category) {
			return true
		}
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

// updateProgressWithStage emits a progress update with a stage label if broadcaster is available
func (proc *Processor) updateProgressWithStage(queueID int, percentage int, stage string) {
	if proc.broadcaster != nil {
		proc.broadcaster.UpdateProgressWithStage(queueID, percentage, stage)
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

// preParseFastFail runs a per-file Stat-based reachability check against the raw NZB
// before any Body fetches. PAR2 files and sidecars are never Stat-checked (they're
// included regardless), so their segments are omitted from the sweep to avoid wasted
// round-trips. Returns (brokenFileIndexes, knownMissingSegmentIDs, error).
// Both maps are nil when no pool is available.
// Returns ErrNoFilesProcessed (wrapped) when all eligible regular files are broken.
func (proc *Processor) preParseFastFail(ctx context.Context, n *nzbparser.Nzb, cfg *config.Config, queueID, maxConnections int) (map[int]struct{}, map[string]struct{}, error) {
	if !proc.poolManager.HasPool() {
		return nil, nil, nil
	}

	// Build the fast-fail input index-aligned with n.Files. PAR2 files keep their
	// slot but carry no segments, so FastFailCheckFiles skips them (their broken
	// state is ignored below anyway) — this avoids Stat-ing every PAR2 part.
	fastFailFiles := make([]validation.FastFailFile, len(n.Files))
	for i, f := range n.Files {
		if filesystem.IsPar2File(f.Filename) {
			fastFailFiles[i] = validation.FastFailFile{Filename: f.Filename}
			continue
		}
		segs := make([]*metapb.SegmentData, len(f.Segments))
		for j, s := range f.Segments {
			segs[j] = &metapb.SegmentData{Id: s.ID}
		}
		// Tag RAR/split-volume parts with their set key so one unreachable part
		// dooms the whole set: the sweep skips the rest and marks every member
		// broken, excluding the doomed set from parsing as a single unit.
		groupKey, _ := rar.SetKey(f.Filename)
		fastFailFiles[i] = validation.FastFailFile{
			Filename: f.Filename,
			Segments: segs,
			GroupKey: groupKey,
		}
	}

	// Stat is a cheap single round-trip on the pool's normal lane; excess
	// requests queue and yield to streaming (priority lane). Run sweeps at the
	// pool's full connection capacity rather than MaxImportConnections (which
	// caps sustained body downloads) so multi-part releases don't crawl
	// 5-at-a-time.
	concurrency := fastFailConcurrency(cfg, maxConnections)

	// Phase 1: cheap release-level probe. Sample the whole release once
	// (≤55 Stats) and fail fast. Healthy releases — the common case — pay only
	// this and skip the per-file sweep entirely, keeping the "Checking segment
	// availability" stage short.
	probeStart := time.Now()
	missing, err := validation.FastFailReleaseProbe(
		ctx,
		fastFailFiles,
		proc.poolManager,
		cfg.Import.SegmentSamplePercentage,
		concurrency,
		proc.validationTimeout,
	)
	if err != nil {
		return nil, nil, err
	}
	if !missing {
		if proc.log != nil {
			proc.log.DebugContext(ctx, "Fast-fail release probe passed",
				"files", len(fastFailFiles),
				"duration", time.Since(probeStart))
		}
		return nil, nil, nil
	}

	// Phase 2 (escalation): the probe found an unreachable segment, so map
	// exactly which files are broken. This sweeps a per-file sample of every
	// file but only runs for releases that are already known to have missing
	// segments — those imports skip Body work below, so the extra Stats are
	// recovered.
	if proc.log != nil {
		proc.log.DebugContext(ctx, "Fast-fail release probe found a missing segment, escalating to per-file sweep",
			"files", len(fastFailFiles),
			"probe_duration", time.Since(probeStart))
	}

	// Report progress within the 0–10% band so the queue item doesn't appear
	// frozen at "Checking segment availability" during the network sweep.
	var fastFailTracker *progress.Tracker
	if proc.broadcaster != nil && proc.broadcaster.HasSubscribers() {
		fastFailTracker = proc.broadcaster.CreateTracker(queueID, 0, 10).WithStage("Checking segment availability")
	}

	results, err := validation.FastFailCheckFiles(
		ctx,
		fastFailFiles,
		proc.poolManager,
		cfg.Import.SegmentSamplePercentage,
		concurrency,
		proc.validationTimeout,
		fastFailTracker,
	)
	if err != nil {
		return nil, nil, err
	}

	brokenIdx := make(map[int]struct{})
	missingIDs := make(map[string]struct{})
	eligibleRegularCount := 0

	for i, result := range results {
		f := n.Files[i]
		isPar2 := filesystem.IsPar2File(f.Filename)

		if !isPar2 {
			eligibleRegularCount++
		}

		if result.Broken && !isPar2 {
			brokenIdx[i] = struct{}{}
			for _, id := range result.MissingSegmentIDs {
				missingIDs[id] = struct{}{}
			}
			if proc.log != nil {
				proc.log.WarnContext(ctx, "Skipping file due to early fast-fail segment check error",
					"file", f.Filename)
			}
		}
	}

	// With set-level propagation, a broken set has all its parts in brokenIdx, so
	// this equality is logical-unit accurate: it holds only when every RAR set and
	// every standalone regular file is broken — nothing healthy remains to import.
	if eligibleRegularCount > 0 && len(brokenIdx) == eligibleRegularCount {
		return nil, nil, multifile.ErrNoFilesProcessed
	}

	if len(brokenIdx) == 0 {
		return nil, nil, nil
	}

	if proc.log != nil {
		brokenSets := make(map[string]struct{})
		for i := range brokenIdx {
			if key, ok := rar.SetKey(n.Files[i].Filename); ok {
				brokenSets[key] = struct{}{}
			}
		}
		proc.log.WarnContext(ctx, "Excluding unreachable files from import",
			"broken_files", len(brokenIdx),
			"broken_rar_sets", len(brokenSets),
			"eligible_files", eligibleRegularCount)
	}

	return brokenIdx, missingIDs, nil
}

// fastFailConcurrency returns the goroutine cap for the fast-fail Stat sweep.
// Stats are cheap and queue on the pool's normal lane, so we use the pool's
// full connection capacity (sum of enabled providers' max connections) rather
// than the import body-download cap. Falls back to fallback when no capacity is
// configured and is bounded to keep goroutine counts sane.
func fastFailConcurrency(cfg *config.Config, fallback int) int {
	const maxConcurrency = 100

	capacity := 0
	for _, p := range cfg.Providers {
		if p.Enabled == nil || *p.Enabled {
			capacity += p.MaxConnections
		}
	}

	if capacity <= 0 {
		capacity = fallback
	}
	if capacity <= 0 {
		capacity = 1
	}
	if capacity > maxConcurrency {
		capacity = maxConcurrency
	}
	return capacity
}

// ProcessNzbFile processes an NZB or STRM file maintaining the folder structure relative to relative path.
// Returns (resultPath, writtenMetadataPaths, error). writtenMetadataPaths contains all virtual paths of
// metadata files written to disk; it is populated even on partial failure so callers can clean up.
// Paths prefixed with "DIR:" indicate a metadata directory that should be removed entirely.
func (proc *Processor) ProcessNzbFile(ctx context.Context, filePath, relativePath string, queueID int, allowedExtensionsOverride *[]string, virtualDirOverride *string, extractedFiles []parser.ExtractedFileInfo, category *string, metadata *string, downloadID *string) (string, []string, error) {
	// Gate this import behind the pool admission controller so we can cap how
	// many NZB imports run concurrently end-to-end and yield to streams under
	// load. The Acquire is a no-op when no caps are configured.
	if proc.poolManager != nil {
		release, err := proc.poolManager.AcquireImportSlot(ctx)
		if err != nil {
			return "", nil, fmt.Errorf("import admission cancelled: %w", err)
		}
		defer release()
	}

	cfg := proc.configGetter()

	// Determine max connections to use
	maxConnections := cfg.Import.MaxImportConnections

	// Determine allowed file extensions to use
	allowedExtensions := cfg.Import.AllowedFileExtensions
	if allowedExtensionsOverride != nil {
		allowedExtensions = *allowedExtensionsOverride
	}

	proc.updateProgressWithStage(queueID, 0, "Parsing NZB")
	file, err := nzbfile.Open(filePath)
	if err != nil {
		return "", nil, NewNonRetryableError("failed to open file", err)
	}
	defer file.Close()

	var parsed *parser.ParsedNzb
	var brokenIdx map[int]struct{}

	// Determine file type and parse accordingly
	if strings.HasSuffix(strings.ToLower(filePath), strmFileExtension) {
		parsed, err = proc.strmParser.ParseStrmFile(file, filePath)
		if err != nil {
			return "", nil, NewNonRetryableError("failed to parse STRM file", err)
		}

		// Validate the parsed STRM
		if err := proc.strmParser.ValidateStrmFile(parsed); err != nil {
			return "", nil, NewNonRetryableError("STRM validation failed", err)
		}
	} else {
		// Parse XML first — cheap, no network needed.
		n, xmlErr := nzbparser.Parse(file)
		if xmlErr != nil {
			return "", nil, NewNonRetryableError("failed to parse NZB file", xmlErr)
		}
		if len(n.Files) == 0 {
			return "", nil, NewNonRetryableError("NZB file contains no files", nil)
		}

		// Pre-parse Stat check — runs before any Body fetches.
		proc.updateProgressWithStage(queueID, 0, "Checking segment availability")
		var missingIDs map[string]struct{}
		var fastFailErr error
		brokenIdx, missingIDs, fastFailErr = proc.preParseFastFail(ctx, n, cfg, queueID, maxConnections)
		if fastFailErr != nil {
			return "", nil, NewNonRetryableError("fast-fail segment check failed", fastFailErr)
		}

		parseTracker := progress.NewTracker(proc.broadcaster, queueID, 2, 10)
		parsed, err = proc.parser.ParseNzb(ctx, n, filePath, parseTracker, parser.ParseOptions{
			BrokenFileIndexes:      brokenIdx,
			KnownMissingSegmentIDs: missingIDs,
		})
		if err != nil {
			return "", nil, NewNonRetryableError("failed to parse NZB file", err)
		}

		// Validate the parsed NZB
		if err := proc.parser.ValidateNzb(parsed); err != nil {
			return "", nil, NewNonRetryableError("NZB validation failed", err)
		}
	}

	// Attach extracted files metadata if available (optimization)
	if len(extractedFiles) > 0 {
		parsed.ExtractedFiles = extractedFiles
	}
	// Update progress: parsing complete, about to identify file type
	proc.updateProgressWithStage(queueID, 10, "Identifying files")

	// Check for cancellation after parsing
	if err := proc.checkCancellation(ctx); err != nil {
		return "", nil, err
	}

	// For NZB-based imports, ensure at least one NNTP provider is configured
	// and run fast-fail before path calculation, directory creation, archive
	// analysis, or metadata writes. STRM files are served via HTTP and don't
	// require an NNTP pool.
	if parsed.Type != parser.NzbTypeStrm {
		if !proc.poolManager.HasPool() {
			proc.log.WarnContext(ctx, "No NNTP providers configured, deferring item processing",
				"file_path", filePath, "queue_id", queueID)
			return "", nil, fmt.Errorf("no NNTP providers configured - item will be retried when providers are added")
		}

		// Doomed RAR/7z sets are excluded whole during fast-fail (set-level
		// propagation), so the parser already dropped them and surviving files
		// never contain a partially-broken known set. Any remaining broken index
		// belongs to a fully-excluded set or an unrelated file; the aggregator
		// isolates per-group analysis failures, so don't fail the whole import.
		if len(brokenIdx) > 0 &&
			(parsed.Type == parser.NzbTypeRarArchive || parsed.Type == parser.NzbType7zArchive) {
			proc.log.WarnContext(ctx, "Proceeding with archive import despite unreachable excluded parts",
				"broken_files", len(brokenIdx))
		}
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
		return "", nil, err
	}

	// Step 5: Process based on file type
	var result string
	var writtenPaths []string

	// Bare-ISO Blu-ray expansion. ISOs posted directly to Usenet (without
	// RAR/7z wrapping) are classified as NzbTypeSingleFile/NzbTypeMultiFile
	// by the parser and would otherwise bypass archive.ExpandISOContents.
	// Peel them out here, run the same expansion the RAR/7z aggregators run,
	// persist each expanded virtual file, and feed the remainder back into
	// normal dispatch. STRM imports skip this path: they have no NNTP
	// segments and the pool guard above explicitly excludes them.
	if parsed.Type != parser.NzbTypeStrm {
		importCfg := cfg.Import
		expandEnabled := true
		if importCfg.ExpandBlurayIso != nil {
			expandEnabled = *importCfg.ExpandBlurayIso
		}
		isoMaxPrefetch := importCfg.MaxDownloadPrefetch
		isoReadTimeout := time.Duration(importCfg.ReadTimeoutSeconds) * time.Second
		if isoReadTimeout == 0 {
			isoReadTimeout = 5 * time.Minute
		}

		var isoReleaseDate int64
		if len(regularFiles) > 0 {
			isoReleaseDate = regularFiles[0].ReleaseDate.Unix()
		}

		// Progress tracker for the bare-ISO analysis phase. It fills the band
		// between "Identifying files" (10%) and "Validating segments" (30%),
		// which would otherwise sit frozen while the ISO filesystem walk and
		// Blu-ray playlist resolution run over NNTP. Gated on subscribers to
		// avoid overhead when nobody is watching (mirrors the RAR/7z path).
		var isoTracker *progress.Tracker
		if proc.broadcaster != nil && proc.broadcaster.HasSubscribers() {
			isoTracker = proc.broadcaster.CreateTracker(queueID, 10, 30).WithStage("Analyzing ISO")
		}

		isoWritten, expandedRegularFiles, isoErr := expandBareISOFiles(ctx, expandBareISODeps{
			enabled: expandEnabled,
			expand: func(ctx context.Context, enabled bool, contents []archive.Content) ([]archive.Content, error) {
				return archive.ExpandISOContents(ctx, enabled, contents,
					proc.poolManager, isoMaxPrefetch, isoReadTimeout, cfg.GetIsoAnalyzeTimeout(), allowedExtensions, isoTracker)
			},
			writeMetadata: func(virtualPath string, meta *metapb.FileMetadata) error {
				return proc.metadataService.WriteFileMetadata(virtualPath, meta)
			},
		}, regularFiles, virtualDir, proc.getCleanNzbName(parsed.Path, queueID), parsed.Path, isoReleaseDate)
		if isoErr != nil {
			return "", writtenPaths, NewNonRetryableError("bare-ISO expansion failed", isoErr)
		}
		writtenPaths = append(writtenPaths, isoWritten...)
		regularFiles = expandedRegularFiles

		// If bare-ISO expansion consumed every regular file and there are no
		// archive files, dispatch has nothing left to do. Return the first
		// expanded virtual path so callers get a meaningful result; the
		// "no files" error path lives in processSingleFile and would otherwise
		// trigger spuriously.
		if len(regularFiles) == 0 && len(archiveFiles) == 0 && len(isoWritten) > 0 {
			proc.updateProgress(queueID, 100)
			return isoWritten[0], writtenPaths, nil
		}
	}

	// dispatchPaths holds whatever the per-type handlers wrote so we can
	// merge it with any ISO-derived paths accumulated above. Handlers
	// already return their full set of written paths (including "DIR:"
	// prefixed cleanup markers) so we just concatenate.
	var dispatchPaths []string
	switch parsed.Type {
	case parser.NzbTypeSingleFile:
		proc.updateProgressWithStage(queueID, 30, "Validating segments")
		result, dispatchPaths, err = proc.processSingleFile(ctx, virtualDir, regularFiles, par2Files, parsed.Path, queueID, allowedExtensions, category, metadata, downloadID)

	case parser.NzbTypeMultiFile:
		proc.updateProgressWithStage(queueID, 30, "Writing metadata")
		result, dispatchPaths, err = proc.processMultiFile(ctx, virtualDir, regularFiles, par2Files, parsed.Path, queueID, allowedExtensions, category, metadata, downloadID)

	case parser.NzbTypeRarArchive:
		proc.updateProgressWithStage(queueID, 15, "Analyzing archive")
		result, dispatchPaths, err = proc.processRarArchive(ctx, virtualDir, regularFiles, archiveFiles, parsed, queueID, allowedExtensions, parsed.ExtractedFiles, category, metadata, downloadID)

	case parser.NzbType7zArchive:
		proc.updateProgressWithStage(queueID, 15, "Analyzing archive")
		result, dispatchPaths, err = proc.processSevenZipArchive(ctx, virtualDir, regularFiles, archiveFiles, parsed, queueID, allowedExtensions, parsed.ExtractedFiles, category, metadata, downloadID)

	case parser.NzbTypeStrm:
		proc.updateProgressWithStage(queueID, 30, "Validating segments")
		result, dispatchPaths, err = proc.processSingleFile(ctx, virtualDir, regularFiles, par2Files, parsed.Path, queueID, allowedExtensions, category, metadata, downloadID)

	default:
		return "", writtenPaths, NewNonRetryableError(fmt.Sprintf("unknown file type: %s", parsed.Type), nil)
	}
	writtenPaths = append(writtenPaths, dispatchPaths...)

	// Update progress: complete
	if err == nil {
		proc.updateProgress(queueID, 100)
	} else if errors.Is(err, nntppool.ErrArticleNotFound) {
		return result, writtenPaths, ErrArticlesNotFound
	}

	return result, writtenPaths, err
}

// processSingleFile handles single file imports
func (proc *Processor) processSingleFile(
	ctx context.Context,
	virtualDir string,
	regularFiles []parser.ParsedFile,
	par2Files []parser.ParsedFile,
	nzbPath string,
	queueID int,
	allowedExtensions []string,
	category *string,
	metadata *string,
	downloadID *string,
) (string, []string, error) {
	if len(regularFiles) == 0 {
		return "", nil, fmt.Errorf("no regular files to process")
	}

	importCfg := proc.configGetter().Import
	renameToNzbName := true
	if importCfg.RenameToNzbName != nil {
		renameToNzbName = *importCfg.RenameToNzbName
	}
	filterSampleFiles := true
	if importCfg.FilterSampleFiles != nil {
		filterSampleFiles = *importCfg.FilterSampleFiles
	}

	// Normalize virtualDir only for synthetic duplicate folders; skip if the NZB actually lives inside a
	// real directory named like the release (e.g. .../Season 01/<file>/<file>.nzb).
	nzbName := proc.getCleanNzbName(nzbPath, queueID)
	releaseName := nzbtrim.TrimNzbExtension(nzbName)
	nzbDirBase := filepath.Base(filepath.Dir(nzbPath))
	fileDir := filepath.Dir(regularFiles[0].Filename)
	if fileDir == "." || fileDir == "" {
		// Only flatten when the enclosing folder is not the same real folder as the release name.
		if !strings.EqualFold(nzbDirBase, releaseName) && !strings.EqualFold(nzbDirBase, strings.TrimSuffix(regularFiles[0].Filename, filepath.Ext(regularFiles[0].Filename))) {
			normalizedDir := normalizeSingleFileVirtualDir(virtualDir, releaseName, regularFiles[0].Filename)

			// Only apply normalization if it doesn't result in a category root folder
			// We want to avoid flattening 'movies/MovieName/Movie.mkv' into 'movies/Movie.mkv'
			// because that confuses Sonarr/Radarr when they look for the job folder.
			if !proc.isCategoryFolder(normalizedDir, category) {
				virtualDir = normalizedDir
			}
		}
	}

	// Ensure we don't put the file directly into a category root folder
	// We MUST create a release folder so Sonarr/Radarr can find the "Job Folder"
	if proc.isCategoryFolder(virtualDir, category) {
		virtualDir = filepath.Join(virtualDir, releaseName)
		virtualDir = strings.ReplaceAll(virtualDir, string(filepath.Separator), "/")
	}

	// Rename the file to match the NZB name to handle obfuscated filenames
	// Keep NZB-provided subfolders but rename the leaf to the release name (preventing duplicate extensions)
	regularFiles = applyNzbRename(renameToNzbName, nzbName, regularFiles)

	// Compute final parent/name, flattening only redundant nesting like file.mkv/file.mkv
	parentPath, finalName := filesystem.DetermineFileLocation(regularFiles[0], virtualDir)

	// Ensure the parent directory exists in metadata
	if err := filesystem.EnsureDirectoryExists(parentPath, proc.metadataService); err != nil {
		return "", nil, err
	}

	// Use the final name for processing
	regularFiles[0].Filename = finalName

	// Process the single file at the resolved parentPath
	result, writtenPath, err := singlefile.ProcessSingleFile(
		ctx,
		parentPath,
		regularFiles[0],
		par2Files,
		nzbPath,
		proc.metadataService,
		allowedExtensions,
		filterSampleFiles,
	)
	var writtenPaths []string
	if writtenPath != "" {
		writtenPaths = []string{writtenPath}
	}
	if err != nil {
		return "", writtenPaths, err
	}

	// Record history
	if proc.recorder != nil {
		nzbID := int64(queueID)
		if err := proc.recorder.AddImportHistory(ctx, &database.ImportHistory{
			DownloadID:  downloadID,
			NzbID:       &nzbID,
			NzbName:     nzbName,
			FileName:    finalName,
			FileSize:    regularFiles[0].Size,
			VirtualPath: result,
			Category:    category,
			Metadata:    metadata,
			CompletedAt: time.Now(),
		}); err != nil {
			proc.log.ErrorContext(ctx, "Failed to add import history", "error", err, "nzb_name", nzbName)
		}
	}

	return result, writtenPaths, nil
}

// processMultiFile handles multi-file imports
func (proc *Processor) processMultiFile(
	ctx context.Context,
	virtualDir string,
	regularFiles []parser.ParsedFile,
	par2Files []parser.ParsedFile,
	nzbPath string,
	queueID int,
	allowedExtensions []string,
	category *string,
	metadata *string,
	downloadID *string,
) (string, []string, error) {
	// If there's only one regular file (and the rest are likely PAR2s), avoid creating a redundant
	// NZB-named directory that matches the file itself. Instead, keep the file directly under the
	// provided virtual directory (preserving any subpaths inside the NZB).
	// EXCEPTION: If the virtual directory is a category root (e.g. "movies"), we MUST create
	// the NZB folder to ensure Radarr/Sonarr can find the job folder correctly.
	importCfg := proc.configGetter().Import
	filterSampleFiles := true
	if importCfg.FilterSampleFiles != nil {
		filterSampleFiles = *importCfg.FilterSampleFiles
	}

	nzbName := proc.getCleanNzbName(nzbPath, queueID)

	// Create NZB folder for multi-file imports, even if early fast-fail filtering
	// leaves only one regular file. The release still originated as a multi-file
	// NZB and should keep its job-folder shape.
	nzbFolder, err := filesystem.CreateNzbFolder(virtualDir, nzbName, proc.metadataService)
	if err != nil {
		return "", nil, err
	}

	// Create directories for files
	if err := filesystem.CreateDirectoriesForFiles(nzbFolder, regularFiles, proc.metadataService); err != nil {
		return "", nil, err
	}

	targetBaseDir := nzbFolder

	// Progress tracker for the metadata write phase. Files are written
	// concurrently; this ticks the bar across [30,95] as each completes so the
	// item doesn't appear frozen at "Writing metadata" on large releases.
	// NOT gated on HasSubscribers(): the tracker also persists the latest
	// percentage in the broadcaster's state map, which is replayed to clients
	// that connect mid-import (GetAllProgress). Gating on a live subscriber here
	// left late-connecting clients stuck at the dispatch's initial 30%.
	var writeTracker *progress.Tracker
	if proc.broadcaster != nil {
		writeTracker = proc.broadcaster.CreateTracker(queueID, 30, 95).WithStage("Writing metadata")
	}

	// Process all regular files
	writtenPaths, err := multifile.ProcessRegularFiles(
		ctx,
		targetBaseDir,
		regularFiles,
		par2Files,
		nzbPath,
		proc.metadataService,
		allowedExtensions,
		filterSampleFiles,
		writeTracker,
	)
	if err != nil {
		return "", writtenPaths, err
	}

	// Record history
	if proc.recorder != nil {
		nzbID := int64(queueID)

		var totalSize int64
		for _, f := range regularFiles {
			totalSize += f.Size
		}

		if err := proc.recorder.AddImportHistory(ctx, &database.ImportHistory{
			DownloadID:  downloadID,
			NzbID:       &nzbID,
			NzbName:     nzbName,
			FileName:    filepath.Base(targetBaseDir),
			FileSize:    totalSize,
			VirtualPath: targetBaseDir,
			Category:    category,
			Metadata:    metadata,
			CompletedAt: time.Now(),
		}); err != nil {
			proc.log.ErrorContext(ctx, "Failed to add import history", "error", err, "nzb_name", nzbName)
		}
	}

	return targetBaseDir, writtenPaths, nil
}

// processRarArchive handles RAR archive imports
func (proc *Processor) processRarArchive(
	ctx context.Context,
	virtualDir string,
	regularFiles []parser.ParsedFile,
	archiveFiles []parser.ParsedFile,
	parsed *parser.ParsedNzb,
	queueID int,
	allowedExtensions []string,
	extractedFiles []parser.ExtractedFileInfo,
	category *string,
	metadata *string,
	downloadID *string,
) (string, []string, error) {
	importCfg := proc.configGetter().Import
	maxPrefetch := importCfg.MaxDownloadPrefetch
	readTimeout := time.Duration(importCfg.ReadTimeoutSeconds) * time.Second
	if readTimeout == 0 {
		readTimeout = 5 * time.Minute
	}
	expandBlurayIso := true
	if importCfg.ExpandBlurayIso != nil {
		expandBlurayIso = *importCfg.ExpandBlurayIso
	}
	filterSampleFiles := true
	if importCfg.FilterSampleFiles != nil {
		filterSampleFiles = *importCfg.FilterSampleFiles
	}
	renameToNzbName := true
	if importCfg.RenameToNzbName != nil {
		renameToNzbName = *importCfg.RenameToNzbName
	}

	// Create NZB folder
	nzbName := proc.getCleanNzbName(parsed.Path, queueID)
	nzbFolder, err := filesystem.CreateNzbFolder(virtualDir, nzbName, proc.metadataService)
	if err != nil {
		return nzbFolder, nil, err
	}

	// Once the nzbFolder is created, track it for cleanup on failure.
	// "DIR:" prefix signals handleProcessingFailure to delete the whole directory.
	writtenPaths := []string{"DIR:" + nzbFolder}

	// Process regular files first if any
	if len(regularFiles) > 0 {
		if err := filesystem.CreateDirectoriesForFiles(nzbFolder, regularFiles, proc.metadataService); err != nil {
			return nzbFolder, writtenPaths, err
		}

		if _, err := multifile.ProcessRegularFiles(
			ctx,
			nzbFolder,
			regularFiles,
			nil, // No PAR2 files for archive imports
			parsed.Path,
			proc.metadataService,
			allowedExtensions,
			filterSampleFiles,
			nil, // archive progress is tracked by the archive tracker below
		); err != nil {
			slog.DebugContext(ctx, "Failed to process regular files", "error", err)
		}
	}

	if len(archiveFiles) > 0 {
		// Lazy tracker allocation: nil *progress.Tracker is safe (nil-receiver guard).
		var archiveProgressTracker *progress.Tracker
		if proc.broadcaster != nil && proc.broadcaster.HasSubscribers() {
			archiveProgressTracker = proc.broadcaster.CreateTracker(queueID, 15, 100)
			archiveProgressTracker.WithStage("Analyzing archive")
		}

		releaseDate := archiveFiles[0].ReleaseDate.Unix()

		err := rar.ProcessArchive(ctx, rar.ProcessArchiveOptions{
			VirtualDir:             nzbFolder,
			ArchiveFiles:           archiveFiles,
			Password:               parsed.GetPassword(),
			ReleaseDate:            releaseDate,
			NzbPath:                parsed.Path,
			Processor:              proc.rarProcessor,
			MetadataService:        proc.metadataService,
			PoolManager:            proc.poolManager,
			ArchiveProgressTracker: archiveProgressTracker,
			AllowedFileExtensions:  allowedExtensions,
			ExtractedFiles:         extractedFiles,
			MaxPrefetch:            maxPrefetch,
			ReadTimeout:            readTimeout,
			IsoAnalyzeTimeout:      proc.configGetter().GetIsoAnalyzeTimeout(),
			ExpandBlurayIso:        expandBlurayIso,
			FilterSamples:          filterSampleFiles,
			RenameToNzbName:        renameToNzbName,
		})
		if err != nil {
			return nzbFolder, writtenPaths, err
		}
	}

	if proc.recorder != nil {
		nzbID := int64(queueID)
		var totalSize int64
		for _, f := range regularFiles {
			totalSize += f.Size
		}
		for _, f := range archiveFiles {
			totalSize += f.Size
		}

		if err := proc.recorder.AddImportHistory(ctx, &database.ImportHistory{
			DownloadID:  downloadID,
			NzbID:       &nzbID,
			NzbName:     nzbName,
			FileName:    filepath.Base(nzbFolder),
			FileSize:    totalSize,
			VirtualPath: nzbFolder,
			Category:    category,
			Metadata:    metadata,
			CompletedAt: time.Now(),
		}); err != nil {
			proc.log.ErrorContext(ctx, "Failed to add import history", "error", err, "nzb_name", nzbName)
		}
	}

	return nzbFolder, writtenPaths, nil
}

// processSevenZipArchive handles 7zip archive imports
func (proc *Processor) processSevenZipArchive(
	ctx context.Context,
	virtualDir string,
	regularFiles []parser.ParsedFile,
	archiveFiles []parser.ParsedFile,
	parsed *parser.ParsedNzb,
	queueID int,
	allowedExtensions []string,
	extractedFiles []parser.ExtractedFileInfo,
	category *string,
	metadata *string,
	downloadID *string,
) (string, []string, error) {
	importCfg := proc.configGetter().Import
	maxPrefetch := importCfg.MaxDownloadPrefetch
	readTimeout := time.Duration(importCfg.ReadTimeoutSeconds) * time.Second
	if readTimeout == 0 {
		readTimeout = 5 * time.Minute
	}
	expandBlurayIso := true
	if importCfg.ExpandBlurayIso != nil {
		expandBlurayIso = *importCfg.ExpandBlurayIso
	}
	filterSampleFiles := true
	if importCfg.FilterSampleFiles != nil {
		filterSampleFiles = *importCfg.FilterSampleFiles
	}
	renameToNzbName := true
	if importCfg.RenameToNzbName != nil {
		renameToNzbName = *importCfg.RenameToNzbName
	}

	// Create NZB folder
	nzbName := proc.getCleanNzbName(parsed.Path, queueID)
	nzbFolder, err := filesystem.CreateNzbFolder(virtualDir, nzbName, proc.metadataService)
	if err != nil {
		return nzbFolder, nil, err
	}

	// Once the nzbFolder is created, track it for cleanup on failure.
	// "DIR:" prefix signals handleProcessingFailure to delete the whole directory.
	writtenPaths := []string{"DIR:" + nzbFolder}

	// Process regular files first if any
	if len(regularFiles) > 0 {
		if err := filesystem.CreateDirectoriesForFiles(nzbFolder, regularFiles, proc.metadataService); err != nil {
			return nzbFolder, writtenPaths, err
		}

		if _, err := multifile.ProcessRegularFiles(
			ctx,
			nzbFolder,
			regularFiles,
			nil, // No PAR2 files for archive imports
			parsed.Path,
			proc.metadataService,
			allowedExtensions,
			filterSampleFiles,
			nil, // archive progress is tracked by the archive tracker below
		); err != nil {
			slog.DebugContext(ctx, "Failed to process regular files", "error", err)
		}
	}

	if len(archiveFiles) > 0 {
		var archiveProgressTracker *progress.Tracker
		if proc.broadcaster != nil && proc.broadcaster.HasSubscribers() {
			archiveProgressTracker = proc.broadcaster.CreateTracker(queueID, 15, 100)
			archiveProgressTracker.WithStage("Analyzing archive")
		}

		releaseDate := archiveFiles[0].ReleaseDate.Unix()

		err := sevenzip.ProcessArchive(ctx, sevenzip.ProcessArchiveOptions{
			VirtualDir:             nzbFolder,
			ArchiveFiles:           archiveFiles,
			Password:               parsed.GetPassword(),
			ReleaseDate:            releaseDate,
			NzbPath:                parsed.Path,
			Processor:              proc.sevenZipProcessor,
			MetadataService:        proc.metadataService,
			PoolManager:            proc.poolManager,
			ArchiveProgressTracker: archiveProgressTracker,
			AllowedFileExtensions:  allowedExtensions,
			ExtractedFiles:         extractedFiles,
			MaxPrefetch:            maxPrefetch,
			ReadTimeout:            readTimeout,
			IsoAnalyzeTimeout:      proc.configGetter().GetIsoAnalyzeTimeout(),
			ExpandBlurayIso:        expandBlurayIso,
			FilterSamples:          filterSampleFiles,
			RenameToNzbName:        renameToNzbName,
		})
		if err != nil {
			return nzbFolder, writtenPaths, err
		}
	}

	if proc.recorder != nil {
		nzbID := int64(queueID)
		var totalSize int64
		for _, f := range regularFiles {
			totalSize += f.Size
		}
		for _, f := range archiveFiles {
			totalSize += f.Size
		}

		if err := proc.recorder.AddImportHistory(ctx, &database.ImportHistory{
			DownloadID:  downloadID,
			NzbID:       &nzbID,
			NzbName:     nzbName,
			FileName:    filepath.Base(nzbFolder),
			FileSize:    totalSize,
			VirtualPath: nzbFolder,
			Category:    category,
			Metadata:    metadata,
			CompletedAt: time.Now(),
		}); err != nil {
			proc.log.ErrorContext(ctx, "Failed to add import history", "error", err, "nzb_name", nzbName)
		}
	}

	return nzbFolder, writtenPaths, nil
}

// applyNzbRename renames the first file in files to match nzbName when renameToNzbName is true.
// Returns the slice unchanged when renameToNzbName is false or files is empty.
func applyNzbRename(renameToNzbName bool, nzbName string, files []parser.ParsedFile) []parser.ParsedFile {
	if !renameToNzbName || len(files) == 0 {
		return files
	}
	originalDir := filepath.Dir(files[0].Filename)
	normalizedBase := normalizeReleaseFilename(nzbName, filepath.Base(files[0].Filename))
	if originalDir != "." && originalDir != "" {
		files[0].Filename = filepath.Join(originalDir, normalizedBase)
	} else {
		files[0].Filename = normalizedBase
	}
	return files
}

// normalizeReleaseFilename aligns the filename to the NZB basename while keeping the original extension.
// It avoids generating duplicate extensions like ".mp4.mp4" when the NZB name already contains the suffix.
func normalizeReleaseFilename(nzbFilename, originalFilename string) string {
	releaseName := nzbtrim.TrimNzbExtension(nzbFilename)
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
