package parser

import (
	"context"
	"encoding/base64"
	stderrors "errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/javi11/altmount/internal/encryption"
	"github.com/javi11/altmount/internal/encryption/rclone"
	"github.com/javi11/altmount/internal/errors"
	"github.com/javi11/altmount/internal/importer/parser/fileinfo"
	"github.com/javi11/altmount/internal/importer/parser/par2"
	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/javi11/altmount/internal/pool"
	"github.com/javi11/altmount/internal/slogutil"
	"github.com/javi11/nntppool/v4"
	"github.com/javi11/nzbparser"
	concpool "github.com/sourcegraph/conc/pool"
)

// FirstSegmentData holds cached data from the first segment of an NZB file
// This avoids redundant fetching when both PAR2 extraction and file parsing need the same data
type FirstSegmentData struct {
	File                *nzbparser.NzbFile   // Reference to the NZB file (for groups, subject, metadata)
	Headers             nntppool.YEncMeta    // yEnc headers (FileName, FileSize, PartSize)
	RawBytes            []byte               // Up to 16KB of raw data for PAR2 detection (may be less if segment is smaller)
	MissingFirstSegment bool                 // True if first segment download failed (article not found, etc.)
	OriginalIndex       int                  // Original position in the parsed NZB file list
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

// ParseFile parses an NZB file from a reader
func (p *Parser) ParseFile(ctx context.Context, r io.Reader, nzbPath string) (*ParsedNzb, error) {
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
	firstSegmentCache, err := p.fetchAllFirstSegments(ctx, n.Files)
	if err != nil {
		return nil, err
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

	par2Descriptors, err := par2.GetFileDescriptors(ctx, par2Cache, p.poolManager)
	if err != nil {
		if stderrors.Is(err, context.Canceled) {
			return nil, errors.NewNonRetryableError("extracting PAR2 file descriptors canceled", err)
		}

		p.log.WarnContext(ctx, "Failed to extract PAR2 file descriptors", "error", err)
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

		filesWithFirstSegment = append(filesWithFirstSegment, &fileinfo.NzbFileWithFirstSegment{
			NzbFile:       data.File,
			Headers:       &data.Headers,
			First16KB:     data.RawBytes,
			ReleaseDate:   time.Unix(int64(data.File.Date), 0),
			OriginalIndex: data.OriginalIndex,
		})
	}

	// Get file infos with priority-based filename selection
	// This already filters out PAR2 files
	fileInfos := fileinfo.GetFileInfos(filesWithFirstSegment, par2Descriptors)
	if len(fileInfos) == 0 {
		p.log.WarnContext(ctx, "Failed to get file infos from network, falling back to NZB XML data",
			"nzb_path", nzbPath)
		fileInfos = p.fallbackGetFileInfos(n.Files)
	}

	if len(fileInfos) == 0 {
		return nil, errors.NewNonRetryableError("NZB file contains no valid files. This can be caused because the file has missing segments in your providers.", nil)
	}

	concPool := concpool.NewWithResults[fileResult]().WithMaxGoroutines(runtime.NumCPU()).WithContext(ctx)

	// Process files in parallel using conc pool
	for _, info := range fileInfos {
		concPool.Go(func(ctx context.Context) (fileResult, error) {
			parsedFile, err := p.parseFile(ctx, n.Meta, parsed.Filename, info, firstSegmentSizeCache)

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

	return parsed, nil
}

// parseFile processes a single file entry from the NZB
// Uses fileInfo for filename, size, and type information
// firstSegmentSizeCache contains pre-fetched yEnc PartSize values for first segments to avoid redundant fetching
func (p *Parser) parseFile(ctx context.Context, meta map[string]string, nzbFilename string, info *fileinfo.FileInfo, firstSegmentSizeCache map[string]int64) (*ParsedFile, error) {
	sort.Sort(info.NzbFile.Segments)

	// Normalize segment sizes using yEnc PartSize headers if needed
	// This handles cases where NZB segment sizes include yEnc encoding overhead
	if p.poolManager != nil && p.poolManager.HasPool() {
		// Look up cached first segment size to avoid redundant fetching
		// Safe to access Segments[0] since files without segments are filtered earlier
		cachedFirstSegmentSize := firstSegmentSizeCache[info.NzbFile.Segments[0].ID]

		err := p.normalizeSegmentSizesWithYenc(ctx, info.NzbFile.Segments, cachedFirstSegmentSize)
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
		parts := strings.Split(info.NzbFile.Subject, " ")
		for _, part := range parts {
			if strings.HasPrefix(part, "NZBDAV_ID:") {
				nzbdavID = strings.TrimPrefix(part, "NZBDAV_ID:")
			} else if strings.HasPrefix(part, "AES_KEY:") {
				keyStr := strings.TrimPrefix(part, "AES_KEY:")
				if key, err := base64.StdEncoding.DecodeString(keyStr); err == nil {
					aesKey = key
					enc = metapb.Encryption_AES
				}
			} else if strings.HasPrefix(part, "AES_IV:") {
				ivStr := strings.TrimPrefix(part, "AES_IV:")
				if iv, err := base64.StdEncoding.DecodeString(ivStr); err == nil {
					aesIv = iv
				}
			} else if strings.HasPrefix(part, "DECODED_SIZE:") {
				if size, err := strconv.ParseInt(strings.TrimPrefix(part, "DECODED_SIZE:"), 10, 64); err == nil && size > 0 {
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
				metaFilename = strings.TrimSuffix(nzbFilename, filepath.Ext(nzbFilename))

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

// fetchAllFirstSegments fetches the first segment data for all files in parallel
// Returns a slice of FirstSegmentData preserving all fetched data
func (p *Parser) fetchAllFirstSegments(ctx context.Context, files []nzbparser.NzbFile) ([]*FirstSegmentData, error) {
	cache := make([]*FirstSegmentData, 0, len(files))

	// Return empty cache if no pool manager available
	if p.poolManager == nil || !p.poolManager.HasPool() {
		return cache, nil
	}

	cp, err := p.poolManager.GetPool()
	if err != nil {
		p.log.DebugContext(context.Background(), "Failed to get connection pool for first segment fetching", "error", err)
		return cache, nil
	}

	// Use conc pool for parallel fetching
	type fetchResult struct {
		segmentID string
		data      *FirstSegmentData
		err       error
	}

	concPool := concpool.NewWithResults[fetchResult]().WithMaxGoroutines(runtime.NumCPU()).WithContext(ctx)

	// Fetch first segment of each file in parallel
	for idx, file := range files {
		// Capture the index and file for the goroutine
		// Use &file to heap-allocate the copy, preventing use-after-free
		// when the goroutine accesses it after the loop iteration ends
		originalIndex := idx
		fileToFetch := &file

		concPool.Go(func(ctx context.Context) (fetchResult, error) {
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
				return fetchResult{
					segmentID: firstSegment.ID,
					data: &FirstSegmentData{
						File:                fileToFetch,
						MissingFirstSegment: true,
						OriginalIndex:       originalIndex,
					},
					err: fmt.Errorf("failed to get body: %w", err),
				}, nil
			}

			headers := result.YEnc

			// Use decoded bytes from result (up to 16KB for PAR2 detection)
			const maxRead = 16 * 1024
			rawBytes := result.Bytes
			bytesRead := len(rawBytes)
			if bytesRead > maxRead {
				rawBytes = rawBytes[:maxRead]
				bytesRead = maxRead
			}

			// Check if we need to read from additional segments to reach 16KB
			// This is necessary for PAR2 Hash16k matching when segments are small
			if bytesRead < maxRead && len(fileToFetch.Segments) > 1 {
				p.log.DebugContext(ctx, "First segment provided less than 16KB, reading from additional segments",
					"file", fileToFetch.Subject,
					"first_segment_bytes", bytesRead,
					"total_segments", len(fileToFetch.Segments))

				// Pre-allocate buffer if we need to combine multiple segments
				buffer := make([]byte, maxRead)
				copy(buffer, rawBytes)

				// Read from subsequent segments until we have 16KB or run out of segments
				for segIdx := 1; segIdx < len(fileToFetch.Segments) && bytesRead < maxRead; segIdx++ {
					segment := fileToFetch.Segments[segIdx]

					// Create a new context for this segment
					segCtx, segCancel := context.WithTimeout(ctx, time.Second*30)

					segResult, err := cp.BodyPriority(segCtx, segment.ID)
					segCancel()
					if err != nil {
						p.log.DebugContext(ctx, "Failed to read additional segment for 16KB completion",
							"segment_index", segIdx,
							"error", err)
						break // Stop trying, use what we have
					}

					// Copy remaining bytes needed from this segment
					remainingBytes := maxRead - bytesRead
					segBytes := segResult.Bytes
					if len(segBytes) > remainingBytes {
						segBytes = segBytes[:remainingBytes]
					}
					copy(buffer[bytesRead:], segBytes)
					bytesRead += len(segBytes)

					p.log.DebugContext(ctx, "Read additional bytes from segment",
						"segment_index", segIdx,
						"bytes_read", len(segBytes),
						"total_bytes", bytesRead)

					if bytesRead >= maxRead {
						break
					}
				}

				rawBytes = buffer[:bytesRead]
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
			return nil, errors.NewNonRetryableError("fetching first segments canceled", err)
		}

		return nil, errors.NewNonRetryableError("failed to fetch first segments", err)
	}

	// Build cache from all fetches (successful and failed)
	for _, result := range results {
		if result.err != nil {
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

	return cache, nil
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
		return headers, nil
	case result := <-resultCh:
		// BodyAsync completed before onMeta fired — either error or non-yEnc article
		if result.Err != nil {
			return nntppool.YEncMeta{}, errors.NewNonRetryableError("failed to get body", result.Err)
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

// normalizeSegmentSizesWithYenc normalizes segment sizes using yEnc PartSize headers
// This handles cases where NZB segment sizes include yEnc overhead
// cachedFirstSegmentSize is the pre-fetched PartSize for the first segment (guaranteed to be > 0)
func (p *Parser) normalizeSegmentSizesWithYenc(ctx context.Context, segments []nzbparser.NzbSegment, cachedFirstSegmentSize int64) error {
	if len(segments) == 1 {
		// Use cached first segment size (guaranteed to exist after filtering)
		segments[0].Bytes = int(cachedFirstSegmentSize)
		return nil
	}

	// Handle files with exactly 2 segments (first and last only)
	if len(segments) == 2 {
		// Use cached first segment size (guaranteed to exist after filtering)
		segments[0].Bytes = int(cachedFirstSegmentSize)

		// Fetch PartSize from last segment
		lastPartHeaders, err := p.fetchYencHeaders(ctx, segments[1], nil)
		if err != nil {
			return fmt.Errorf("failed to fetch last segment yEnc part size: %w", err)
		}
		segments[1].Bytes = int(lastPartHeaders.PartSize)

		return nil
	}

	// Use cached first segment size (guaranteed to exist after filtering)
	firstPartSize := cachedFirstSegmentSize

	// Fetch PartSize from second segment (this represents the "standard" segment size)
	secondPartHeaders, err := p.fetchYencHeaders(ctx, segments[1], nil)
	if err != nil {
		return fmt.Errorf("failed to fetch second segment yEnc part size: %w", err)
	}
	standardPartSize := int64(secondPartHeaders.PartSize)

	// Fetch PartSize from last segment
	lastSegmentIndex := len(segments) - 1
	lastPartHeaders, err := p.fetchYencHeaders(ctx, segments[lastSegmentIndex], nil)
	if err != nil {
		return fmt.Errorf("failed to fetch last segment yEnc part size: %w", err)
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
