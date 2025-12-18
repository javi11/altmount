package parser

import (
	"context"
	"errors"
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
	"github.com/javi11/altmount/internal/importer/parser/fileinfo"
	"github.com/javi11/altmount/internal/importer/parser/par2"
	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/javi11/altmount/internal/pool"
	"github.com/javi11/altmount/internal/slogutil"
	"github.com/javi11/nntppool/v2"
	"github.com/javi11/nntppool/v2/pkg/nntpcli"
	"github.com/javi11/nzbparser"
	concpool "github.com/sourcegraph/conc/pool"
)

// NewNonRetryableError creates a non-retryable error (defined here to avoid import cycles)
func NewNonRetryableError(message string, cause error) error {
	if cause != nil {
		return fmt.Errorf("%s: %w", message, cause)
	}
	return fmt.Errorf("%s", message)
}

// FirstSegmentData holds cached data from the first segment of an NZB file
// This avoids redundant fetching when both PAR2 extraction and file parsing need the same data
type FirstSegmentData struct {
	File                *nzbparser.NzbFile  // Reference to the NZB file (for groups, subject, metadata)
	Headers             nntpcli.YencHeaders // yEnc headers (FileName, FileSize, PartSize)
	RawBytes            []byte              // Up to 16KB of raw data for PAR2 detection (may be less if segment is smaller)
	MissingFirstSegment bool                // True if first segment download failed (article not found, etc.)
	OriginalIndex       int                 // Original position in the parsed NZB file list
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

	n, err := nzbparser.Parse(r)
	if err != nil {
		return nil, NewNonRetryableError("failed to parse NZB XML", err)
	}

	if len(n.Files) == 0 {
		return nil, NewNonRetryableError("NZB file contains no files", nil)
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
		if errors.Is(err, context.Canceled) {
			return nil, NewNonRetryableError("extracting PAR2 file descriptors canceled", err)
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
		return nil, NewNonRetryableError("NZB file contains no valid files. This can be caused because the file has missing segments in your providers.", nil)
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
		if errors.Is(err, context.Canceled) {
			return nil, NewNonRetryableError("parsing canceled", err)
		}

		return nil, NewNonRetryableError("failed to get file infos", err)
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
			return nil, NewNonRetryableError("NZB file contains only PAR2 files. This indicates that there are missing segments in your providers.", nil)
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

			if errors.Is(err, nntppool.ErrArticleNotFoundInProviders) {
				return nil, NewNonRetryableError(fmt.Sprintf("failed to fetch yEnc headers: missing articles in all providers: %s", info.NzbFile.Subject), err)
			}
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

	// Extract nzbdavID from subject if present
	if strings.HasPrefix(info.NzbFile.Subject, "NZBDAV_ID:") {
		parts := strings.SplitN(info.NzbFile.Subject, " ", 2)
		if len(parts) > 0 {
			nzbdavID = strings.TrimPrefix(parts[0], "NZBDAV_ID:")
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
					return nil, NewNonRetryableError("failed to parse file size", err)
				}

				totalSize = fSizeInt
			}

			// This will add support for rclone encrypted files
			if strings.HasSuffix(strings.ToLower(metaFilename), rclone.EncFileExtension) {
				filename = metaFilename[:len(metaFilename)-4]
				enc = metapb.Encryption_RCLONE

				decSize, err := rclone.DecryptedSize(totalSize)
				if err != nil {
					return nil, NewNonRetryableError("failed to get decrypted size", err)
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
		originalIndex := idx
		fileToFetch := file

		concPool.Go(func(ctx context.Context) (fetchResult, error) {
			ctx = slogutil.With(ctx, "file", fileToFetch.Filename)

			// Skip files without segments
			if len(fileToFetch.Segments) == 0 {
				return fetchResult{
					segmentID: fileToFetch.Subject,
					data: &FirstSegmentData{
						File:                &fileToFetch,
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

			// Get body reader for the first segment
			r, err := cp.BodyReader(ctx, firstSegment.ID, nil)
			if err != nil {
				return fetchResult{
					segmentID: firstSegment.ID,
					data: &FirstSegmentData{
						File:                &fileToFetch,
						MissingFirstSegment: true,
						OriginalIndex:       originalIndex,
					},
					err: fmt.Errorf("failed to get body reader: %w", err),
				}, nil
			}
			defer r.Close()

			// Get yEnc headers
			headers, err := r.GetYencHeaders()
			if err != nil {
				return fetchResult{
					segmentID: firstSegment.ID,
					data: &FirstSegmentData{
						File:                &fileToFetch,
						MissingFirstSegment: true,
						OriginalIndex:       originalIndex,
					},
					err: fmt.Errorf("failed to get yenc headers: %w", err),
				}, nil
			}

			// Read up to 16KB for PAR2 detection and hash matching
			// PAR2 Hash16k requires exactly 16KB (or entire file if smaller)
			const maxRead = 16 * 1024
			buffer := make([]byte, maxRead)

			// Try to read exactly 16KB (or until EOF for smaller files)
			bytesRead, err := io.ReadFull(r, buffer)

			// io.ErrUnexpectedEOF is acceptable - file/segment is smaller than 16KB
			// io.EOF means the segment is empty (should not happen but handle gracefully)
			if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
				return fetchResult{
					segmentID: firstSegment.ID,
					data: &FirstSegmentData{
						File:                &fileToFetch,
						MissingFirstSegment: true,
						OriginalIndex:       originalIndex,
					},
					err: fmt.Errorf("failed to read segment data: %w", err),
				}, nil
			}

			// Check if we need to read from additional segments to reach 16KB
			// This is necessary for PAR2 Hash16k matching when segments are small
			if bytesRead < maxRead && len(fileToFetch.Segments) > 1 {
				p.log.DebugContext(ctx, "First segment provided less than 16KB, reading from additional segments",
					"file", fileToFetch.Subject,
					"first_segment_bytes", bytesRead,
					"total_segments", len(fileToFetch.Segments))

				// Read from subsequent segments until we have 16KB or run out of segments
				for segIdx := 1; segIdx < len(fileToFetch.Segments) && bytesRead < maxRead; segIdx++ {
					segment := fileToFetch.Segments[segIdx]

					// Create a new context for this segment
					segCtx, segCancel := context.WithTimeout(ctx, time.Second*30)

					segReader, err := cp.BodyReader(segCtx, segment.ID, nil)
					if err != nil {
						segCancel()
						p.log.DebugContext(ctx, "Failed to read additional segment for 16KB completion",
							"segment_index", segIdx,
							"error", err)
						break // Stop trying, use what we have
					}

					// Use closure to ensure cleanup via defer regardless of how we exit
					shouldBreak := func() bool {
						defer segReader.Close()
						defer segCancel()

						// Read remaining bytes needed
						remainingBytes := maxRead - bytesRead
						tempBuffer := make([]byte, remainingBytes)

						n, err := io.ReadFull(segReader, tempBuffer)
						if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
							p.log.DebugContext(ctx, "Error reading from additional segment",
								"segment_index", segIdx,
								"error", err)
							return true // break outer loop
						}

						// Append to our buffer
						copy(buffer[bytesRead:], tempBuffer[:n])
						bytesRead += n

						p.log.DebugContext(ctx, "Read additional bytes from segment",
							"segment_index", segIdx,
							"bytes_read", n,
							"total_bytes", bytesRead)

						return false
					}()

					if shouldBreak || bytesRead >= maxRead {
						break
					}
				}
			}

			// Trim buffer to actual bytes read
			rawBytes := buffer[:bytesRead]

			return fetchResult{
				segmentID: firstSegment.ID,
				data: &FirstSegmentData{
					File:          &fileToFetch,
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
		if errors.Is(err, context.Canceled) {
			return nil, NewNonRetryableError("fetching first segments canceled", err)
		}

		return nil, NewNonRetryableError("failed to fetch first segments", err)
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

// fetchYencPartSize fetches the yenc header to get the actual part size for a specific segment
func (p *Parser) fetchYencHeaders(ctx context.Context, segment nzbparser.NzbSegment, groups []string) (nntpcli.YencHeaders, error) {
	if p.poolManager == nil {
		return nntpcli.YencHeaders{}, NewNonRetryableError("no pool manager available", nil)
	}

	cp, err := p.poolManager.GetPool()
	if err != nil {
		return nntpcli.YencHeaders{}, NewNonRetryableError("no connection pool available", err)
	}

	r, err := cp.BodyReader(ctx, segment.ID, nil)
	if err != nil {
		return nntpcli.YencHeaders{}, NewNonRetryableError("failed to get body reader: %w", err)
	}
	defer r.Close()

	headers, err := r.GetYencHeaders()
	if err != nil {
		return nntpcli.YencHeaders{}, fmt.Errorf("failed to get yenc headers: %w", err)
	}

	if headers.PartSize <= 0 {
		return nntpcli.YencHeaders{}, NewNonRetryableError("invalid part size from yenc header", nil)
	}

	return headers, nil
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
	lastPartSize := int64(lastPartHeaders.PartSize)

	// Apply the sizes:
	// - First segment: use its actual size
	segments[0].Bytes = int(firstPartSize)

	// - Middle segments (indices 1 through n-2): use standard size from second segment
	for i := 1; i < len(segments)-1; i++ {
		segments[i].Bytes = int(standardPartSize)
	}

	// - Last segment: use its actual size
	segments[lastSegmentIndex].Bytes = int(lastPartSize)

	return nil
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
		return NewNonRetryableError("invalid NZB: total size is zero", nil)
	}

	if parsed.SegmentsCount <= 0 {
		return NewNonRetryableError("invalid NZB: no segments found", nil)
	}

	for i, file := range parsed.Files {
		if len(file.Segments) == 0 {
			return NewNonRetryableError(fmt.Sprintf("invalid NZB: file %d has no segments", i), nil)
		}

		if file.Size <= 0 {
			return NewNonRetryableError(fmt.Sprintf("invalid NZB: file %d has invalid size", i), nil)
		}

		if len(file.Groups) == 0 {
			return NewNonRetryableError(fmt.Sprintf("invalid NZB: file %d has no groups", i), nil)
		}
	}

	return nil
}
