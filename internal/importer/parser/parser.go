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

	"github.com/avast/retry-go/v4"
	"github.com/javi11/altmount/internal/encryption"
	"github.com/javi11/altmount/internal/encryption/rclone"
	"github.com/javi11/altmount/internal/importer/parser/fileinfo"
	"github.com/javi11/altmount/internal/importer/parser/par2"
	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/javi11/altmount/internal/pool"
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
	File     *nzbparser.NzbFile  // Reference to the NZB file (for groups, subject, metadata)
	Headers  nntpcli.YencHeaders // yEnc headers (FileName, FileSize, PartSize)
	RawBytes []byte              // Up to 16KB of raw data for PAR2 detection (may be less if segment is smaller)
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
func (p *Parser) ParseFile(r io.Reader, nzbPath string) (*ParsedNzb, error) {
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
	var segSize int64
	if n.Meta != nil {
		if pwd, ok := n.Meta["password"]; ok && pwd != "" {
			parsed.SetPassword(pwd)
		}

		if v, ok := n.Meta["chunk_size"]; ok {
			if iv, err := strconv.ParseInt(v, 10, 64); err == nil && iv > 0 {
				segSize = iv
			}
		}
	}

	// Fetch first segment data for all files in parallel
	// This cache is used by both PAR2 extraction and file parsing to avoid redundant fetches
	firstSegmentCache := p.fetchAllFirstSegments(n.Files)

	// Extract PAR2 file descriptors before processing files
	// This provides accurate filename and size information via MD5 hash matching
	// Convert firstSegmentCache to par2.FirstSegmentData format
	par2Cache := make(map[string]*par2.FirstSegmentData)
	for id, data := range firstSegmentCache {
		par2Cache[id] = &par2.FirstSegmentData{
			File:     data.File,
			RawBytes: data.RawBytes,
		}
	}

	par2Descriptors, err := par2.GetFileDescriptors(n.Files, par2Cache, p.poolManager, p.log)
	if err != nil {
		p.log.Warn("Failed to extract PAR2 file descriptors", "error", err)
	}

	// Extract file information using priority-based filename selection
	// Convert firstSegmentCache to fileinfo format
	filesWithFirstSegment := make([]*fileinfo.NzbFileWithFirstSegment, 0, len(firstSegmentCache))
	for _, data := range firstSegmentCache {
		if data.File == nil {
			continue
		}
		filesWithFirstSegment = append(filesWithFirstSegment, &fileinfo.NzbFileWithFirstSegment{
			NzbFile:     data.File,
			Headers:     &data.Headers,
			First16KB:   data.RawBytes,
			ReleaseDate: time.Now(), // TODO: Extract from NZB metadata if available
		})
	}

	// Get file infos with priority-based filename selection
	// This already filters out PAR2 files
	fileInfos := fileinfo.GetFileInfos(filesWithFirstSegment, par2Descriptors, p.log)
	if len(fileInfos) == 0 {
		return nil, NewNonRetryableError("NZB file contains no valid files (only PAR2)", nil)
	}

	concPool := concpool.NewWithResults[fileResult]().WithMaxGoroutines(runtime.NumCPU())

	// Process files in parallel using conc pool
	for _, info := range fileInfos {
		concPool.Go(func() fileResult {
			parsedFile, err := p.parseFile(n.Meta, parsed.Filename, info)

			return fileResult{
				parsedFile: parsedFile,
				err:        err,
			}
		})
	}

	// Wait for all goroutines to complete and collect results
	results := concPool.Wait()

	// Check for errors and collect valid results
	var parsedFiles []*ParsedFile
	for i, result := range results {
		if result.err != nil {
			return nil, fmt.Errorf("failed to parse file %s: %w", fileInfos[i].NzbFile.Subject, result.err)
		}
		parsedFiles = append(parsedFiles, result.parsedFile)
	}

	// Aggregate results in the original order
	for _, parsedFile := range parsedFiles {
		parsed.Files = append(parsed.Files, *parsedFile)
		parsed.TotalSize += parsedFile.Size
		parsed.SegmentsCount += len(parsedFile.Segments)

		if len(parsedFile.Segments) > 0 {
			// Find the corresponding file info to check segment bytes
			for _, info := range fileInfos {
				if info.NzbFile.Subject == parsedFile.Subject {
					if len(info.NzbFile.Segments) > 0 && info.NzbFile.Segments[0].Bytes > int(segSize) {
						// Fallback to the first segment size encountered
						segSize = int64(info.NzbFile.Segments[0].Bytes)
					}
					break
				}
			}
		}
	}

	parsed.SegmentSize = segSize

	// Determine NZB type based on content analysis
	parsed.Type = p.determineNzbType(parsed.Files)

	return parsed, nil
}

// parseFile processes a single file entry from the NZB
// Uses fileInfo for filename, size, and type information
func (p *Parser) parseFile(meta map[string]string, nzbFilename string, info *fileinfo.FileInfo) (*ParsedFile, error) {
	sort.Sort(info.NzbFile.Segments)

	// Normalize segment sizes using yEnc PartSize headers if needed
	// This handles cases where NZB segment sizes include yEnc encoding overhead
	if p.poolManager != nil && p.poolManager.HasPool() {
		err := p.normalizeSegmentSizesWithYenc(info.NzbFile.Segments)
		if err != nil {
			// Log the error but continue with original segment sizes
			// This ensures processing continues even if yEnc header fetching fails
			p.log.Warn("Failed to normalize segment sizes with yEnc headers",
				"error", err,
				"segments", len(info.NzbFile.Segments))

			if errors.Is(err, nntppool.ErrArticleNotFoundInProviders) {
				return nil, NewNonRetryableError("failed to fetch yEnc headers: missing articles in all providers", err)
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
	totalSize := *info.FileSize

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
		Subject:      info.NzbFile.Subject,
		Filename:     filename,
		Size:         totalSize,
		Segments:     segments,
		Groups:       info.NzbFile.Groups,
		IsRarArchive: info.IsRar,
		Is7zArchive:  info.Is7z,
		Encryption:   enc,
		Password:     password,
		Salt:         salt,
	}

	return parsedFile, nil
}

// fetchAllFirstSegments fetches the first segment data for all files in parallel
// Returns a map of segmentID -> FirstSegmentData for efficient lookup
func (p *Parser) fetchAllFirstSegments(files []nzbparser.NzbFile) map[string]*FirstSegmentData {
	cache := make(map[string]*FirstSegmentData)

	// Return empty cache if no pool manager available
	if p.poolManager == nil || !p.poolManager.HasPool() {
		return cache
	}

	cp, err := p.poolManager.GetPool()
	if err != nil {
		p.log.Debug("Failed to get connection pool for first segment fetching", "error", err)
		return cache
	}

	// Use conc pool for parallel fetching
	type fetchResult struct {
		segmentID string
		data      *FirstSegmentData
		err       error
	}

	concPool := concpool.NewWithResults[fetchResult]().WithMaxGoroutines(runtime.NumCPU())

	// Fetch first segment of each file in parallel
	for i := range files {
		file := &files[i]

		// Skip files without segments
		if len(file.Segments) == 0 {
			continue
		}

		concPool.Go(func() fetchResult {
			firstSegment := file.Segments[0]

			// Create context with timeout
			ctx, cancel := context.WithTimeout(context.Background(), time.Second*30)
			defer cancel()

			// Get body reader for the first segment
			r, err := cp.BodyReader(ctx, firstSegment.ID, file.Groups)
			if err != nil {
				return fetchResult{
					segmentID: firstSegment.ID,
					err:       fmt.Errorf("failed to get body reader: %w", err),
				}
			}
			defer r.Close()

			// Get yEnc headers
			headers, err := r.GetYencHeaders()
			if err != nil {
				return fetchResult{
					segmentID: firstSegment.ID,
					err:       fmt.Errorf("failed to get yenc headers: %w", err),
				}
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
					err:       fmt.Errorf("failed to read segment data: %w", err),
				}
			}

			// Check if we need to read from additional segments to reach 16KB
			// This is necessary for PAR2 Hash16k matching when segments are small
			if bytesRead < maxRead && len(file.Segments) > 1 {
				p.log.Debug("First segment provided less than 16KB, reading from additional segments",
					"file", file.Subject,
					"first_segment_bytes", bytesRead,
					"total_segments", len(file.Segments))

				// Read from subsequent segments until we have 16KB or run out of segments
				for segIdx := 1; segIdx < len(file.Segments) && bytesRead < maxRead; segIdx++ {
					segment := file.Segments[segIdx]

					// Create a new context for this segment
					segCtx, segCancel := context.WithTimeout(context.Background(), time.Second*30)

					segReader, err := cp.BodyReader(segCtx, segment.ID, file.Groups)
					if err != nil {
						segCancel()
						p.log.Debug("Failed to read additional segment for 16KB completion",
							"segment_index", segIdx,
							"error", err)
						break // Stop trying, use what we have
					}

					// Read remaining bytes needed
					remainingBytes := maxRead - bytesRead
					tempBuffer := make([]byte, remainingBytes)

					n, err := io.ReadFull(segReader, tempBuffer)
					segReader.Close()
					segCancel()

					if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
						p.log.Debug("Error reading from additional segment",
							"segment_index", segIdx,
							"error", err)
						break // Stop trying, use what we have
					}

					// Append to our buffer
					copy(buffer[bytesRead:], tempBuffer[:n])
					bytesRead += n

					p.log.Debug("Read additional bytes from segment",
						"segment_index", segIdx,
						"bytes_read", n,
						"total_bytes", bytesRead)

					if bytesRead >= maxRead {
						break // We have enough data
					}
				}
			}

			// Trim buffer to actual bytes read
			rawBytes := buffer[:bytesRead]

			return fetchResult{
				segmentID: firstSegment.ID,
				data: &FirstSegmentData{
					File:     file,
					Headers:  headers,
					RawBytes: rawBytes,
				},
			}
		})
	}

	// Wait for all fetches to complete
	results := concPool.Wait()

	// Build cache from successful fetches
	for _, result := range results {
		if result.err != nil {
			p.log.Debug("Failed to fetch first segment",
				"segment_id", result.segmentID,
				"error", result.err)
			continue
		}

		if result.data != nil {
			cache[result.segmentID] = result.data
		}
	}

	p.log.Debug("Fetched first segments",
		"total_files", len(files),
		"successful", len(cache))

	// Validation: Check for files with insufficient data for PAR2 matching
	const expectedSize = 16 * 1024
	for segID, data := range cache {
		if len(data.RawBytes) < expectedSize {
			// This is expected for small files (< 16KB total)
			// But could indicate an issue if the file is actually larger
			p.log.Debug("First segment data is less than 16KB",
				"segment_id", segID,
				"data_size", len(data.RawBytes),
				"expected_size", expectedSize,
				"note", "This is expected for small files, but may affect PAR2 matching for larger files")
		}

		if len(data.RawBytes) == 0 {
			p.log.Warn("First segment has no data",
				"segment_id", segID,
				"file", data.File.Subject)
		}
	}

	return cache
}

// fetchYencPartSize fetches the yenc header to get the actual part size for a specific segment
func (p *Parser) fetchYencHeaders(segment nzbparser.NzbSegment, groups []string) (nntpcli.YencHeaders, error) {
	if p.poolManager == nil {
		return nntpcli.YencHeaders{}, NewNonRetryableError("no pool manager available", nil)
	}

	cp, err := p.poolManager.GetPool()
	if err != nil {
		return nntpcli.YencHeaders{}, NewNonRetryableError("no connection pool available", err)
	}

	var result nntpcli.YencHeaders
	err = retry.Do(
		func() error {
			// Create context with timeout for each retry attempt
			ctx, cancel := context.WithTimeout(context.Background(), time.Second*30)
			defer cancel()

			// Get a connection from the pool
			r, err := cp.BodyReader(ctx, segment.ID, groups)
			if err != nil {
				return fmt.Errorf("failed to get body reader: %w", err)
			}
			defer r.Close()

			if r == nil {
				return fmt.Errorf("no connection pool available")
			}

			// Get yenc headers
			h, err := r.GetYencHeaders()
			if err != nil {
				return fmt.Errorf("failed to get yenc headers: %w", err)
			}

			result = h
			return nil
		},
		retry.Attempts(3),
		retry.Delay(1*time.Second),
		retry.DelayType(retry.BackOffDelay),
		retry.MaxDelay(5*time.Second),
		retry.OnRetry(func(n uint, err error) {
			p.log.Debug("Retrying fetchYencHeaders",
				"attempt", n+1,
				"segment_id", segment.ID,
				"error", err)
		}),
	)
	if err != nil {
		return nntpcli.YencHeaders{}, err
	}

	if result.PartSize <= 0 {
		return nntpcli.YencHeaders{}, fmt.Errorf("invalid part size from yenc header: %d", result.PartSize)
	}

	return result, nil
}

// normalizeSegmentSizesWithYenc normalizes segment sizes using yEnc PartSize headers
// This handles cases where NZB segment sizes include yEnc overhead
func (p *Parser) normalizeSegmentSizesWithYenc(segments []nzbparser.NzbSegment) error {
	if len(segments) == 1 {
		firstPartHeaders, err := p.fetchYencHeaders(segments[0], nil)
		if err != nil {
			return fmt.Errorf("failed to fetch first segment yEnc part size: %w", err)
		}

		segments[0].Bytes = int(firstPartHeaders.PartSize)

		return nil
	}

	// Handle files with exactly 2 segments (first and last only)
	if len(segments) == 2 {
		p.log.Debug("Normalizing segment sizes for 2-segment file")

		// Fetch PartSize from first segment
		firstPartHeaders, err := p.fetchYencHeaders(segments[0], nil)
		if err != nil {
			return fmt.Errorf("failed to fetch first segment yEnc part size: %w", err)
		}

		// Fetch PartSize from last segment
		lastPartHeaders, err := p.fetchYencHeaders(segments[1], nil)
		if err != nil {
			return fmt.Errorf("failed to fetch last segment yEnc part size: %w", err)
		}

		segments[0].Bytes = int(firstPartHeaders.PartSize)
		segments[1].Bytes = int(lastPartHeaders.PartSize)

		p.log.Debug("Normalized 2 segments",
			"first_size", firstPartHeaders.PartSize,
			"last_size", lastPartHeaders.PartSize)

		return nil
	}

	// Fetch PartSize from first segment
	firstPartHeaders, err := p.fetchYencHeaders(segments[0], nil)
	if err != nil {
		return fmt.Errorf("failed to fetch first segment yEnc part size: %w", err)
	}
	firstPartSize := int64(firstPartHeaders.PartSize)

	// Fetch PartSize from second segment (this represents the "standard" segment size)
	secondPartHeaders, err := p.fetchYencHeaders(segments[1], nil)
	if err != nil {
		return fmt.Errorf("failed to fetch second segment yEnc part size: %w", err)
	}
	standardPartSize := int64(secondPartHeaders.PartSize)

	// Fetch PartSize from last segment
	lastSegmentIndex := len(segments) - 1
	lastPartHeaders, err := p.fetchYencHeaders(segments[lastSegmentIndex], nil)
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
