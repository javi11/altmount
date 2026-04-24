package parser

import (
	"context"
	"encoding/base64"
	stderrors "errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/javi11/altmount/internal/encryption"
	"github.com/javi11/altmount/internal/encryption/rclone"
	"github.com/javi11/altmount/internal/errors"
	"github.com/javi11/altmount/internal/importer/parser/fileinfo"
	"github.com/javi11/altmount/internal/importer/parser/par2"
	"github.com/javi11/altmount/internal/importer/utils/nzbtrim"
	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/javi11/altmount/internal/pool"
	"github.com/javi11/altmount/internal/progress"
	"github.com/javi11/altmount/internal/slogutil"
	"github.com/javi11/nntppool/v4"
	"github.com/javi11/nzbparser"
	concpool "github.com/sourcegraph/conc/pool"
)

// FirstSegmentData holds cached data from the first segment of an NZB file
// This avoids redundant fetching when both PAR2 extraction and file parsing need the same data
type FirstSegmentData struct {
	File                *nzbparser.NzbFile // Reference to the NZB file (for groups, subject, metadata)
	Headers             nntppool.YEncMeta  // yEnc headers (FileName, FileSize, PartSize)
	RawBytes            []byte             // Up to 16KB of raw data for PAR2 detection (may be less if segment is smaller)
	MissingFirstSegment bool               // True if first segment download failed (article not found, etc.)
	IsArticleNotFound   bool               // True only when 430 Not Found (permanent); false for timeouts/transient
	OriginalIndex       int                // Original position in the parsed NZB file list
}

// Parser handles NZB file parsing
type Parser struct {
	poolManager pool.Manager // Pool manager for dynamic pool access
	log         *slog.Logger // Logger for debug/error messages
}

// Use conc pool for parallel processing with proper error handling
type fileResult struct {
	parsedFile *ParsedFile
	err        error
}

// NewParser creates a new NZB parser
func NewParser(poolManager pool.Manager) *Parser {
	return &Parser{
		poolManager: poolManager,
		log:         slog.Default().With("component", "nzb-parser"),
	}
}

// ParseFile parses an NZB file from a reader.
// progressTracker, if non-nil, receives incremental updates as first segments are fetched (the
// longest phase). It is safe to pass nil — updates are skipped.
func (p *Parser) ParseFile(ctx context.Context, r io.Reader, nzbPath string, progressTracker progress.ProgressTracker) (*ParsedNzb, error) {
	ctx = slogutil.With(ctx, "nzb_path", nzbPath)

	// Add a safety timeout for the entire parsing process
	// Parsing large NZBs with many missing articles can sometimes hang in NNTP body fetching
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	n, err := nzbparser.Parse(r)
	if err != nil {
		return nil, errors.NewNonRetryableError("failed to parse NZB XML", err)
	}

	if len(n.Files) == 0 {
		return nil, errors.NewNonRetryableError("NZB file contains no files", nil)
	}

	parsed := &ParsedNzb{
		Path:     nzbPath,
		Filename: filepath.Base(nzbPath),
		Files:    make([]ParsedFile, 0, len(n.Files)),
	}
	// Determine segment size from meta chunk_size or fallback to first segment size
	if n.Meta != nil {
		if pwd, ok := n.Meta["password"]; ok && pwd != "" {
			parsed.SetPassword(pwd)
		}
	}

	// Fetch first segment data for all files in parallel
	// This cache is used by both PAR2 extraction and file parsing to avoid redundant fetches
	firstSegmentCache, notFoundIDs, err := p.fetchAllFirstSegments(ctx, n.Files, progressTracker)
	if err != nil {
		return nil, err
	}

	// If any cached first segment looks like a PAR2 index file, we need at
	// least 16KB of data for every other non-sidecar file so fileinfo can run
	// the MD5(first16KB) match against PAR2 descriptors. Otherwise skip the
	// additional-segment fan-out entirely.
	if p.hasPar2IndexCandidate(firstSegmentCache) {
		p.complete16KBReads(ctx, firstSegmentCache, notFoundIDs)
	}

	// Create a map of first segment ID to PartSize for optimization in normalizeSegmentSizesWithYenc
	// This avoids redundant fetching of yEnc headers for the first segment
	firstSegmentSizeCache := make(map[string]int64)
	for _, data := range firstSegmentCache {
		if data != nil && data.File != nil && !data.MissingFirstSegment && len(data.File.Segments) > 0 {
			if data.Headers.PartSize > 0 {
				firstSegmentSizeCache[data.File.Segments[0].ID] = int64(data.Headers.PartSize)
			}
		}
	}

	// Extract PAR2 file descriptors before processing files
	// This provides accurate filename and size information via MD5 hash matching
	// Convert firstSegmentCache to par2.FirstSegmentData format
	// Skip files with missing first segments as they cannot be matched
	par2Cache := make([]*par2.FirstSegmentData, 0, len(firstSegmentCache))
	for _, data := range firstSegmentCache {
		if data == nil || data.File == nil || data.MissingFirstSegment {
			continue
		}
		par2Cache = append(par2Cache, &par2.FirstSegmentData{
			File:     data.File,
			RawBytes: data.RawBytes,
		})
	}

	// Run PAR2 descriptor extraction in parallel with a one-shot representative
	// yEnc-header fetch for a middle segment. The representative PartSize is
	// reused as the "standard part size" during per-file normalization, cutting
	// one network call per multi-segment file.
	var (
		par2Descriptors     map[[16]byte]*par2.FileDescriptor
		par2Err             error
		nzbStandardPartSize int64
	)

	repSeg, repGroups, haveRep := pickRepresentativeMiddleSegment(firstSegmentCache, notFoundIDs)

	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		par2Descriptors, par2Err = par2.GetFileDescriptors(gctx, par2Cache, p.poolManager)
		return nil
	})
	if haveRep && p.poolManager != nil && p.poolManager.HasPool() {
		g.Go(func() error {
			h, err := p.fetchYencHeaders(gctx, repSeg, repGroups)
			if err != nil {
				p.log.DebugContext(gctx, "Representative yEnc header fetch failed, falling back to per-file normalization", "error", err)
				return nil
			}
			if h.PartSize > 0 {
				nzbStandardPartSize = int64(h.PartSize)
			}
			return nil
		})
	}
	_ = g.Wait()

	if par2Err != nil {
		if stderrors.Is(par2Err, context.Canceled) {
			return nil, errors.NewNonRetryableError("extracting PAR2 file descriptors canceled", par2Err)
		}
		p.log.WarnContext(ctx, "Failed to extract PAR2 file descriptors", "error", par2Err)
	}

	// Extract file information using priority-based filename selection
	// Convert firstSegmentCache to fileinfo format
	// Skip files with missing first segments as they cannot be processed
	filesWithFirstSegment := make([]*fileinfo.NzbFileWithFirstSegment, 0, len(firstSegmentCache))
	for _, data := range firstSegmentCache {
		// Skip files with missing first segment data
		// These files can't be properly processed (no PAR2 matching, no yEnc size data, no magic bytes)
		if data == nil || data.File == nil || data.MissingFirstSegment {
			continue
		}

		subjectHeader := ""
		if s, err := nzbparser.ParseSubject(data.File.Subject); err == nil {
			subjectHeader = s.Header
		}

		filesWithFirstSegment = append(filesWithFirstSegment, &fileinfo.NzbFileWithFirstSegment{
			NzbFile:       data.File,
			Headers:       &data.Headers,
			First16KB:     data.RawBytes,
			ReleaseDate:   time.Unix(int64(data.File.Date), 0),
			SubjectHeader: subjectHeader,
			OriginalIndex: data.OriginalIndex,
		})
	}

	// Get file infos with priority-based filename selection
	// GetFileInfos processes ALL files including PAR2 files; SeparateFiles handles the split
	fileInfos := fileinfo.GetFileInfos(filesWithFirstSegment, par2Descriptors, parsed.Filename)
	if len(fileInfos) == 0 {
		p.log.WarnContext(ctx, "Failed to get file infos from network, falling back to NZB XML data",
			"nzb_path", nzbPath)
		fileInfos = p.fallbackGetFileInfos(n.Files)
	}

	if len(fileInfos) == 0 {
		return nil, errors.NewNonRetryableError("NZB file contains no valid files. This can be caused because the file has missing segments in your providers.", nil)
	}

	maxParse := max(min(len(fileInfos), 20), 1)
	concPool := concpool.NewWithResults[fileResult]().WithMaxGoroutines(maxParse).WithContext(ctx)

	// Process files in parallel using conc pool
	for _, info := range fileInfos {
		concPool.Go(func(ctx context.Context) (fileResult, error) {
			parsedFile, err := p.parseFile(ctx, n.Meta, parsed.Filename, info, firstSegmentSizeCache, nzbStandardPartSize, notFoundIDs)

			return fileResult{
				parsedFile: parsedFile,
				err:        err,
			}, nil
		})
	}

	// Wait for all goroutines to complete and collect results
	results, err := concPool.Wait()
	if err != nil {
		if stderrors.Is(err, context.Canceled) {
			return nil, errors.NewNonRetryableError("parsing canceled", err)
		}

		return nil, errors.NewNonRetryableError("failed to get file infos", err)
	}

	// Check for errors and collect valid results
	var parsedFiles []*ParsedFile
	for _, result := range results {
		if result.err != nil {
			slog.InfoContext(ctx, "Failed to parse file", "error", result.err)
			continue
		}
		parsedFiles = append(parsedFiles, result.parsedFile)
	}

	// Check if all files are PAR2 files - indicates missing segments
	if len(parsedFiles) > 0 {
		allPar2 := true
		for _, pf := range parsedFiles {
			if !pf.IsPar2Archive {
				allPar2 = false
				break
			}
		}

		if allPar2 {
			return nil, errors.NewNonRetryableError("NZB file contains only PAR2 files. This indicates that there are missing segments in your providers.", nil)
		}
	}

	// Aggregate results in the original order
	// Note: OriginalIndex is already set from the original n.Files order during parsing
	for _, parsedFile := range parsedFiles {
		parsed.Files = append(parsed.Files, *parsedFile)
		parsed.TotalSize += parsedFile.Size
		parsed.SegmentsCount += len(parsedFile.Segments)
	}

	// Determine NZB type based on content analysis
	parsed.Type = p.determineNzbType(parsed.Files)

	// Propagate archive type to confirmed archive parts only.
	// For split archives only the first volume contains the magic-byte header, so
	// Is7zArchive / IsRarArchive may be false on subsequent parts even though they
	// are archive parts. Correct that now that we know the NZB type.
	// Propagation is gated on existing detection (magic bytes or extension) so that
	// non-archive sidecars (.txt, .nfo, etc.) are never wrongly classified.
	p.propagateArchiveType(parsed)

	return parsed, nil
}

// parseFile processes a single file entry from the NZB
// Uses fileInfo for filename, size, and type information
// firstSegmentSizeCache contains pre-fetched yEnc PartSize values for first segments to avoid redundant fetching.
// nzbStandardPartSize, when >0, is the yEnc PartSize of a representative middle segment in the NZB;
// it lets normalization skip the per-file second-segment fetch.
func (p *Parser) parseFile(ctx context.Context, meta map[string]string, nzbFilename string, info *fileinfo.FileInfo, firstSegmentSizeCache map[string]int64, nzbStandardPartSize int64, notFoundIDs map[string]struct{}) (*ParsedFile, error) {
	if len(info.NzbFile.Segments) == 0 {
		return nil, fmt.Errorf("file has no segments")
	}

	sort.Sort(info.NzbFile.Segments)

	// Normalize segment sizes using yEnc PartSize headers if needed
	// This handles cases where NZB segment sizes include yEnc encoding overhead
	if p.poolManager != nil && p.poolManager.HasPool() {
		// Look up cached first segment size to avoid redundant fetching
		// Safe to access Segments[0] since files without segments are filtered earlier
		cachedFirstSegmentSize := firstSegmentSizeCache[info.NzbFile.Segments[0].ID]

		err := p.normalizeSegmentSizesWithYenc(ctx, info.NzbFile.Segments, cachedFirstSegmentSize, nzbStandardPartSize, notFoundIDs)
		if err != nil {
			// Log the error but continue with original segment sizes
			// This ensures processing continues even if yEnc header fetching fails
			p.log.WarnContext(ctx, "Failed to normalize segment sizes with yEnc headers",
				"error", err,
				"segments", len(info.NzbFile.Segments))
		}
	}

	// Convert segments
	segments := make([]*metapb.SegmentData, len(info.NzbFile.Segments))

	for i, seg := range info.NzbFile.Segments {
		segments[i] = &metapb.SegmentData{
			Id:          seg.ID,
			StartOffset: int64(0),
			EndOffset:   int64(seg.Bytes - 1),
			SegmentSize: int64(seg.Bytes),
		}
	}

	// Get file size from fileInfo (priority-based: PAR2 > yEnc headers)
	var totalSize int64

	if info.FileSize != nil {
		totalSize = *info.FileSize
	}

	// Sanity check: Ensure totalSize is at least the sum of its segments.
	// This prevents "seek beyond file size" errors when yEnc headers report incorrect sizes.
	var segmentSum int64
	for _, seg := range info.NzbFile.Segments {
		segmentSum += int64(seg.Bytes)
	}

	if totalSize < segmentSum {
		totalSize = segmentSum
	}

	// Usenet Drive files parsing
	var (
		password string
		salt     string
	)
	if meta != nil {
		if pwd, ok := meta["password"]; ok && pwd != "" {
			password = pwd
		}
		if s, ok := meta["salt"]; ok && s != "" {
			salt = s
		}
	}

	// Use filename from fileInfo (priority-based: PAR2 > Subject > yEnc headers)
	filename := info.Filename
	enc := metapb.Encryption_NONE // Default to no encryption
	var nzbdavID string
	var aesKey []byte
	var aesIv []byte

	// Extract extra metadata from subject if present (nzbdav compatibility)
	if strings.HasPrefix(info.NzbFile.Subject, "NZBDAV_ID:") {
		parts := strings.SplitSeq(info.NzbFile.Subject, " ")
		for part := range parts {
			if after, ok := strings.CutPrefix(part, "NZBDAV_ID:"); ok {
				nzbdavID = after
			} else if after, ok := strings.CutPrefix(part, "AES_KEY:"); ok {
				keyStr := after
				if key, err := base64.StdEncoding.DecodeString(keyStr); err == nil {
					aesKey = key
					enc = metapb.Encryption_AES
				}
			} else if after, ok := strings.CutPrefix(part, "AES_IV:"); ok {
				ivStr := after
				if iv, err := base64.StdEncoding.DecodeString(ivStr); err == nil {
					aesIv = iv
				}
			} else if after, ok := strings.CutPrefix(part, "DECODED_SIZE:"); ok {
				if size, err := strconv.ParseInt(after, 10, 64); err == nil && size > 0 {
					totalSize = size
				}
			}
		}
	}

	// Check metadata for overrides
	if meta != nil {
		if metaFilename, ok := meta["file_name"]; ok && metaFilename != "" {
			if fSize, ok := meta["file_size"]; ok {
				// This is a usenet-drive nzb with one file
				metaFilename = nzbtrim.TrimNzbExtension(nzbFilename)

				if fe, ok := meta["file_extension"]; ok {
					metaFilename = metaFilename + fe
				} else {
					fileExt := filepath.Ext(metaFilename)
					if fileExt == "" {
						if fe, ok := meta["file_extension"]; ok {
							metaFilename = metaFilename + fe
						}
					}
				}

				fSizeInt, err := strconv.ParseInt(fSize, 10, 64)
				if err != nil {
					return nil, errors.NewNonRetryableError("failed to parse file size", err)
				}

				totalSize = fSizeInt
			}

			// This will add support for rclone encrypted files
			if strings.HasSuffix(strings.ToLower(metaFilename), rclone.EncFileExtension) {
				filename = metaFilename[:len(metaFilename)-4]
				enc = metapb.Encryption_RCLONE

				decSize, err := rclone.DecryptedSize(totalSize)
				if err != nil {
					return nil, errors.NewNonRetryableError("failed to get decrypted size", err)
				}

				totalSize = decSize
			} else {
				filename = metaFilename
			}
		}

		if metaCipher, ok := meta["cipher"]; ok && metaCipher != "" {
			if metaCipher == string(encryption.RCloneCipherType) {
				enc = metapb.Encryption_RCLONE
			}
		}
	}

	// Use RAR/7z detection from fileInfo (includes magic byte detection)
	parsedFile := &ParsedFile{
		Subject:       info.NzbFile.Subject,
		Filename:      filename,
		Size:          totalSize,
		Segments:      segments,
		Groups:        info.NzbFile.Groups,
		IsRarArchive:  info.IsRar,
		Is7zArchive:   info.Is7z,
		Encryption:    enc,
		Password:      password,
		Salt:          salt,
		AesKey:        aesKey,
		AesIv:         aesIv,
		ReleaseDate:   info.ReleaseDate,
		IsPar2Archive: info.IsPar2Archive,
		OriginalIndex: info.OriginalIndex,
		NzbdavID:      nzbdavID,
	}

	return parsedFile, nil
}

// fetchAllFirstSegments fetches the first segment data for all files in parallel.
// Returns a slice of FirstSegmentData, a set of segment IDs that returned 430 Not Found
// (permanent — safe to skip in subsequent fetches), and any fatal error.
func (p *Parser) fetchAllFirstSegments(ctx context.Context, files []nzbparser.NzbFile, progressTracker progress.ProgressTracker) ([]*FirstSegmentData, map[string]struct{}, error) {
	cache := make([]*FirstSegmentData, 0, len(files))
	notFoundIDs := make(map[string]struct{})

	// Return empty cache if no pool manager available
	if p.poolManager == nil || !p.poolManager.HasPool() {
		return cache, notFoundIDs, nil
	}

	cp, err := p.poolManager.GetPool()
	if err != nil {
		p.log.DebugContext(context.Background(), "Failed to get connection pool for first segment fetching", "error", err)
		return cache, notFoundIDs, nil
	}

	// Use conc pool for parallel fetching — I/O-bound, so use more than NumCPU
	type fetchResult struct {
		segmentID  string
		isNotFound bool // true when 430 Not Found (permanent)
		data       *FirstSegmentData
		err        error
	}

	maxFetch := max(min(len(files), 20), 1)
	concPool := concpool.NewWithResults[fetchResult]().WithMaxGoroutines(maxFetch).WithContext(ctx)

	// Atomic counter for progress tracking — incremented by each goroutine on completion
	var doneCount atomic.Int64
	totalFiles := len(files)

	// Fetch first segment of each file in parallel
	for idx, file := range files {
		// Capture the index and file for the goroutine
		// Use &file to heap-allocate the copy, preventing use-after-free
		// when the goroutine accesses it after the loop iteration ends
		originalIndex := idx
		fileToFetch := &file

		concPool.Go(func(ctx context.Context) (fetchResult, error) {
			defer func() {
				if progressTracker != nil {
					progressTracker.Update(int(doneCount.Add(1)), totalFiles)
				}
			}()
			ctx = slogutil.With(ctx, "file", fileToFetch.Filename)

			// Skip files without segments
			if len(fileToFetch.Segments) == 0 {
				return fetchResult{
					segmentID: fileToFetch.Subject,
					data: &FirstSegmentData{
						File:                fileToFetch,
						MissingFirstSegment: true,
						OriginalIndex:       originalIndex,
					},
					err: fmt.Errorf("file has no segments"),
				}, nil
			}

			firstSegment := fileToFetch.Segments[0]

			// Create context with timeout
			ctx, cancel := context.WithTimeout(ctx, time.Second*30)
			defer cancel()

			// Get body for the first segment (v4 returns decoded bytes + YEnc metadata)
			result, err := cp.BodyPriority(ctx, firstSegment.ID)
			if err != nil {
				notFound := stderrors.Is(err, nntppool.ErrArticleNotFound)
				return fetchResult{
					segmentID:  firstSegment.ID,
					isNotFound: notFound,
					data: &FirstSegmentData{
						File:                fileToFetch,
						MissingFirstSegment: true,
						IsArticleNotFound:   notFound,
						OriginalIndex:       originalIndex,
					},
					err: fmt.Errorf("failed to get body: %w", err),
				}, nil
			}

			if p.poolManager != nil {
				p.poolManager.IncArticlesDownloaded()
				p.poolManager.UpdateDownloadProgress("", int64(len(result.Bytes)))
			}

			headers := result.YEnc

			// Use decoded bytes from result (up to 16KB for PAR2 detection).
			// 16KB completion from subsequent segments is deferred — it's only
			// needed if the NZB actually contains PAR2 descriptors, and that
			// can only be decided after all first segments are back.
			const maxRead = 16 * 1024
			rawBytes := result.Bytes
			if len(rawBytes) > maxRead {
				rawBytes = rawBytes[:maxRead]
			}

			return fetchResult{
				segmentID: firstSegment.ID,
				data: &FirstSegmentData{
					File:          fileToFetch,
					Headers:       headers,
					RawBytes:      rawBytes,
					OriginalIndex: originalIndex,
				},
			}, nil
		})
	}

	// Wait for all fetches to complete
	results, err := concPool.Wait()
	if err != nil {
		if stderrors.Is(err, context.Canceled) {
			return nil, notFoundIDs, errors.NewNonRetryableError("fetching first segments canceled", err)
		}

		return nil, notFoundIDs, errors.NewNonRetryableError("failed to fetch first segments", err)
	}

	// Build cache from all fetches (successful and failed)
	// Also collect permanently-missing segment IDs to skip redundant calls later
	for _, result := range results {
		if result.err != nil {
			if result.isNotFound && result.segmentID != "" {
				notFoundIDs[result.segmentID] = struct{}{}
			}
			// Add the data with MissingFirstSegment=true to track the failure
			if result.data != nil {
				cache = append(cache, result.data)
			}
			continue
		}

		cache = append(cache, result.data)
	}

	for _, data := range cache {
		if data == nil || data.File == nil || data.MissingFirstSegment {
			continue
		}

		if len(data.RawBytes) == 0 {
			p.log.WarnContext(context.Background(), "First segment has no data",
				"file", data.File.Subject)
		}
	}

	return cache, notFoundIDs, nil
}

// pickRepresentativeMiddleSegment picks one "middle" segment (the second
// segment of a multi-segment, non-missing, non-404 file) whose yEnc header
// size can serve as the NZB-wide standard PartSize. Files produced by the
// same encoder share this value, so one fetch replaces one-per-file fetches.
func pickRepresentativeMiddleSegment(cache []*FirstSegmentData, notFoundIDs map[string]struct{}) (nzbparser.NzbSegment, []string, bool) {
	for _, d := range cache {
		if d == nil || d.File == nil || d.MissingFirstSegment {
			continue
		}
		if len(d.File.Segments) < 3 {
			continue
		}
		seg := d.File.Segments[1]
		if _, known404 := notFoundIDs[seg.ID]; known404 {
			continue
		}
		return seg, d.File.Groups, true
	}
	return nzbparser.NzbSegment{}, nil, false
}

// hasPar2IndexCandidate reports whether any cached first segment looks like a
// PAR2 index file (magic bytes + small segment count).
func (p *Parser) hasPar2IndexCandidate(cache []*FirstSegmentData) bool {
	const maxIndexSegments = 5
	for _, d := range cache {
		if d == nil || d.File == nil || d.MissingFirstSegment {
			continue
		}
		if len(d.File.Segments) == 0 || len(d.File.Segments) > maxIndexSegments {
			continue
		}
		if par2.HasMagicBytes(d.RawBytes) {
			return true
		}
	}
	return false
}

// needs16KBCompletion decides whether a file is worth completing up to 16KB
// from additional segments. We skip obvious non-archive sidecars (.nfo, .txt,
// .srt, …) and files already at or past 16KB — neither benefits from PAR2
// Hash16k matching.
func needs16KBCompletion(d *FirstSegmentData, maxRead int) bool {
	if d == nil || d.File == nil || d.MissingFirstSegment {
		return false
	}
	if len(d.RawBytes) >= maxRead {
		return false
	}
	if len(d.File.Segments) <= 1 {
		return false
	}
	if par2.HasMagicBytes(d.RawBytes) {
		return false // PAR2 files are themselves matched on their descriptor content, not Hash16k
	}
	name := strings.ToLower(d.File.Filename)
	switch filepath.Ext(name) {
	case ".nfo", ".txt", ".srt", ".sub", ".jpg", ".jpeg", ".png", ".nzb", ".sfv", ".md5":
		return false
	}
	return true
}

// complete16KBReads fetches additional segments for files whose first segment
// returned less than 16KB. Only called when the NZB actually contains PAR2
// descriptors that could match the resulting MD5(first16KB). Best-effort:
// missing or failed segments leave RawBytes as-is.
func (p *Parser) complete16KBReads(ctx context.Context, cache []*FirstSegmentData, notFoundIDs map[string]struct{}) {
	const maxRead = 16 * 1024
	if p.poolManager == nil || !p.poolManager.HasPool() {
		return
	}
	cp, err := p.poolManager.GetPool()
	if err != nil {
		return
	}

	var targets []*FirstSegmentData
	for _, d := range cache {
		if needs16KBCompletion(d, maxRead) {
			targets = append(targets, d)
		}
	}
	if len(targets) == 0 {
		return
	}

	maxFetch := max(min(len(targets), 20), 1)
	pool := concpool.New().WithMaxGoroutines(maxFetch).WithContext(ctx)
	for _, d := range targets {
		pool.Go(func(ctx context.Context) error {
			// Determine additional segments needed based on NZB-reported bytes
			bytesRead := len(d.RawBytes)
			estimatedTotal := bytesRead
			var segsNeeded []nzbparser.NzbSegment
			for i := 1; i < len(d.File.Segments) && estimatedTotal < maxRead; i++ {
				seg := d.File.Segments[i]
				if _, known404 := notFoundIDs[seg.ID]; known404 {
					continue
				}
				segsNeeded = append(segsNeeded, seg)
				estimatedTotal += seg.Bytes
			}
			if len(segsNeeded) == 0 {
				return nil
			}

			segResults := make([][]byte, len(segsNeeded))
			g, gctx := errgroup.WithContext(ctx)
			for i, seg := range segsNeeded {
				g.Go(func() error {
					segCtx, segCancel := context.WithTimeout(gctx, time.Second*30)
					defer segCancel()
					sr, err := cp.BodyPriority(segCtx, seg.ID)
					if err != nil {
						return nil // best-effort
					}
					if p.poolManager != nil {
						p.poolManager.IncArticlesDownloaded()
						p.poolManager.UpdateDownloadProgress("", int64(len(sr.Bytes)))
					}
					segResults[i] = sr.Bytes
					return nil
				})
			}
			_ = g.Wait()

			buffer := make([]byte, maxRead)
			copy(buffer, d.RawBytes)
			for _, segBytes := range segResults {
				if len(segBytes) == 0 || bytesRead >= maxRead {
					break
				}
				n := copy(buffer[bytesRead:], segBytes)
				bytesRead += n
			}
			d.RawBytes = buffer[:bytesRead]
			return nil
		})
	}
	_ = pool.Wait()
}

// fetchYencHeaders fetches the yenc header to get the actual part size for a specific segment.
// It uses BodyAsync with io.Discard + onMeta to return headers as soon as =ybegin/=ypart
// lines are parsed, without waiting for the full article body to transfer.
func (p *Parser) fetchYencHeaders(ctx context.Context, segment nzbparser.NzbSegment, groups []string) (nntppool.YEncMeta, error) {
	if p.poolManager == nil {
		return nntppool.YEncMeta{}, errors.NewNonRetryableError("no pool manager available", nil)
	}

	cp, err := p.poolManager.GetPool()
	if err != nil {
		return nntppool.YEncMeta{}, errors.NewNonRetryableError("no connection pool available", err)
	}

	// onMeta fires after =ybegin/=ypart parsing (~first 2 lines),
	// while the body continues draining to io.Discard in the background.
	metaCh := make(chan nntppool.YEncMeta, 1)
	resultCh := cp.BodyAsync(ctx, segment.ID, io.Discard, func(meta nntppool.YEncMeta) {
		metaCh <- meta
	})

	// Wait for either: headers via onMeta (fast), full result (error or no yEnc), or context cancel.
	select {
	case headers := <-metaCh:
		if headers.PartSize <= 0 {
			return nntppool.YEncMeta{}, errors.NewNonRetryableError("invalid part size from yenc header", nil)
		}

		if p.poolManager != nil {
			p.poolManager.IncArticlesDownloaded()
			p.poolManager.UpdateDownloadProgress("", int64(headers.PartSize))
		}

		return headers, nil
	case result := <-resultCh:
		// BodyAsync completed before onMeta fired — either error or non-yEnc article
		if result.Err != nil {
			return nntppool.YEncMeta{}, errors.NewNonRetryableError("failed to get body", result.Err)
		}

		if p.poolManager != nil {
			p.poolManager.IncArticlesDownloaded()
			p.poolManager.UpdateDownloadProgress("", int64(result.Body.YEnc.PartSize))
		}

		// onMeta didn't fire but body completed — use headers from result
		headers := result.Body.YEnc
		if headers.PartSize <= 0 {
			return nntppool.YEncMeta{}, errors.NewNonRetryableError("invalid part size from yenc header", nil)
		}
		return headers, nil
	case <-ctx.Done():
		return nntppool.YEncMeta{}, errors.NewNonRetryableError("context canceled", ctx.Err())
	}
}

// normalizeSegmentSizesWithYenc normalizes segment sizes using yEnc PartSize headers.
// This handles cases where NZB segment sizes include yEnc overhead.
// cachedFirstSegmentSize is the pre-fetched PartSize for the first segment (guaranteed to be > 0).
// nzbStandardPartSize, when >0, is a representative middle-segment PartSize shared across the NZB;
// passing it here skips the per-file second-segment network call for files with 3+ segments.
// notFoundIDs is the set of segment IDs known to return 430; those are skipped without a network call.
func (p *Parser) normalizeSegmentSizesWithYenc(ctx context.Context, segments []nzbparser.NzbSegment, cachedFirstSegmentSize int64, nzbStandardPartSize int64, notFoundIDs map[string]struct{}) error {
	firstPartSize := cachedFirstSegmentSize
	if firstPartSize <= 0 {
		if _, known404 := notFoundIDs[segments[0].ID]; known404 {
			return fmt.Errorf("first segment %s is known not found, skipping yEnc normalization", segments[0].ID)
		}
		// Fetch PartSize from first segment if not in cache
		firstPartHeaders, err := p.fetchYencHeaders(ctx, segments[0], nil)
		if err != nil {
			return fmt.Errorf("failed to fetch first segment yEnc part size: %w", err)
		}
		firstPartSize = int64(firstPartHeaders.PartSize)
	}

	if len(segments) == 1 {
		segments[0].Bytes = int(firstPartSize)
		return nil
	}

	// Handle files with exactly 2 segments (first and last only)
	if len(segments) == 2 {
		segments[0].Bytes = int(firstPartSize)

		if _, known404 := notFoundIDs[segments[1].ID]; known404 {
			return fmt.Errorf("second segment %s is known not found, skipping yEnc normalization", segments[1].ID)
		}
		// Fetch PartSize from last segment
		lastPartHeaders, err := p.fetchYencHeaders(ctx, segments[1], nil)
		if err != nil {
			return fmt.Errorf("failed to fetch last segment yEnc part size: %w", err)
		}
		segments[1].Bytes = int(lastPartHeaders.PartSize)

		return nil
	}

	// Determine the standard (middle-segment) part size and the actual last-segment size.
	// The standard size is either reused from the NZB-wide representative fetch,
	// or fetched once per file when the shared value is unavailable.
	lastSegmentIndex := len(segments) - 1

	if _, known404 := notFoundIDs[segments[lastSegmentIndex].ID]; known404 {
		return fmt.Errorf("last segment %s is known not found, skipping yEnc normalization", segments[lastSegmentIndex].ID)
	}

	standardPartSize := nzbStandardPartSize
	var lastPartHeaders nntppool.YEncMeta

	if standardPartSize > 0 {
		// Shared value available — only the last segment needs to be fetched.
		h, err := p.fetchYencHeaders(ctx, segments[lastSegmentIndex], nil)
		if err != nil {
			return fmt.Errorf("failed to fetch last segment yEnc part size: %w", err)
		}
		lastPartHeaders = h
	} else {
		if _, known404 := notFoundIDs[segments[1].ID]; known404 {
			return fmt.Errorf("second segment %s is known not found, skipping yEnc normalization", segments[1].ID)
		}
		var secondPartHeaders nntppool.YEncMeta
		g, gctx := errgroup.WithContext(ctx)
		g.Go(func() error {
			h, err := p.fetchYencHeaders(gctx, segments[1], nil)
			if err != nil {
				return fmt.Errorf("failed to fetch second segment yEnc part size: %w", err)
			}
			secondPartHeaders = h
			return nil
		})
		g.Go(func() error {
			h, err := p.fetchYencHeaders(gctx, segments[lastSegmentIndex], nil)
			if err != nil {
				return fmt.Errorf("failed to fetch last segment yEnc part size: %w", err)
			}
			lastPartHeaders = h
			return nil
		})
		if err := g.Wait(); err != nil {
			return err
		}
		standardPartSize = int64(secondPartHeaders.PartSize)
	}

	// Apply the sizes:
	// - First segment: use its actual size
	segments[0].Bytes = int(firstPartSize)

	// - Middle segments (indices 1 through n-2): use standard size from second segment
	for i := 1; i < len(segments)-1; i++ {
		segments[i].Bytes = int(standardPartSize)
	}

	// - Last segment: use its actual size
	segments[lastSegmentIndex].Bytes = int(lastPartHeaders.PartSize)

	return nil
}

// fallbackGetFileInfos is a "dumb" fallback that extracts file info directly from NZB XML
// without any network validation. This is used when the first segments are missing.
func (p *Parser) fallbackGetFileInfos(files []nzbparser.NzbFile) []*fileinfo.FileInfo {
	fileInfos := make([]*fileinfo.FileInfo, 0)

	for i, file := range files {
		// Basic PAR2 skip
		if fileinfo.IsPar2File(file.Filename) {
			continue
		}

		// Skip files without segments
		if len(file.Segments) == 0 {
			continue
		}

		// Calculate basic size from segments
		var size int64
		for _, seg := range file.Segments {
			size += int64(seg.Bytes)
		}

		// Create a basic FileInfo
		info := &fileinfo.FileInfo{
			NzbFile:       file,
			Filename:      file.Filename,
			ReleaseDate:   time.Unix(int64(file.Date), 0),
			IsPar2Archive: false,
			FileSize:      &size,
			IsRar:         fileinfo.HasRarMagic(nil) || fileinfo.IsRarFile(file.Filename),
			Is7z:          fileinfo.Is7zFile(file.Filename),
			OriginalIndex: i,
		}

		fileInfos = append(fileInfos, info)
	}

	return fileInfos
}

// determineNzbType analyzes the parsed files to determine the NZB type
func (p *Parser) determineNzbType(files []ParsedFile) NzbType {
	// Exclude PAR2 files — a single media file + N PAR2 files is still a single-file NZB
	var mediaFiles []ParsedFile
	for _, f := range files {
		if !f.IsPar2Archive && !fileinfo.IsPar2File(f.Filename) {
			mediaFiles = append(mediaFiles, f)
		}
	}
	if len(mediaFiles) == 0 {
		return NzbTypeMultiFile // all-PAR2 edge case; allPar2 check handles this earlier
	}
	files = mediaFiles

	if len(files) == 1 {
		// Single file NZB
		if files[0].IsRarArchive {
			return NzbTypeRarArchive
		}
		if files[0].Is7zArchive {
			return NzbType7zArchive
		}
		return NzbTypeSingleFile
	}

	// Multiple files - check if any are RAR or 7zip archives
	hasRarFiles := false
	has7zFiles := false
	for _, file := range files {
		if file.IsRarArchive {
			hasRarFiles = true
		}
		if file.Is7zArchive {
			has7zFiles = true
		}
	}

	// Prioritize RAR if both types exist (shouldn't normally happen)
	if hasRarFiles {
		return NzbTypeRarArchive
	}
	if has7zFiles {
		return NzbType7zArchive
	}

	return NzbTypeMultiFile
}

// propagateArchiveType sets the archive-type flag on non-PAR2 files that are
// confirmed archive parts. Propagation is gated on the file already being
// detected as an archive (via magic bytes or extension), preventing non-archive
// sidecars (.txt, .nfo, etc.) from being wrongly classified.
func (p *Parser) propagateArchiveType(parsed *ParsedNzb) {
	switch parsed.Type {
	case NzbType7zArchive:
		for i := range parsed.Files {
			f := &parsed.Files[i]
			if !f.IsPar2Archive && !fileinfo.IsPar2File(f.Filename) &&
				(f.Is7zArchive || fileinfo.Is7zFile(f.Filename)) {
				f.Is7zArchive = true
			}
		}
	case NzbTypeRarArchive:
		for i := range parsed.Files {
			f := &parsed.Files[i]
			if !f.IsPar2Archive && !fileinfo.IsPar2File(f.Filename) &&
				(f.IsRarArchive || fileinfo.IsRarFile(f.Filename)) {
				f.IsRarArchive = true
			}
		}
	}
}

// GetMetadata extracts metadata from the NZB head section
func (p *Parser) GetMetadata(nzbXML *nzbparser.Nzb) map[string]string {
	metadata := make(map[string]string)

	if nzbXML.Meta == nil {
		return metadata
	}

	return nzbXML.Meta
}

// ValidateNzb performs basic validation on the parsed NZB
func (p *Parser) ValidateNzb(parsed *ParsedNzb) error {
	if parsed.TotalSize <= 0 {
		return errors.NewNonRetryableError("invalid NZB: total size is zero", nil)
	}

	if parsed.SegmentsCount <= 0 {
		return errors.NewNonRetryableError("invalid NZB: no segments found", nil)
	}

	for i, file := range parsed.Files {
		if len(file.Segments) == 0 {
			return errors.NewNonRetryableError(fmt.Sprintf("invalid NZB: file %d has no segments", i), nil)
		}

		if file.Size <= 0 {
			return errors.NewNonRetryableError(fmt.Sprintf("invalid NZB: file %d has invalid size", i), nil)
		}

		if len(file.Groups) == 0 {
			return errors.NewNonRetryableError(fmt.Sprintf("invalid NZB: file %d has no groups", i), nil)
		}
	}

	return nil
}
