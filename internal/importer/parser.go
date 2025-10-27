package importer

import (
	"context"
	"crypto/md5"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/avast/retry-go/v4"
	"github.com/javi11/altmount/internal/encryption"
	"github.com/javi11/altmount/internal/encryption/rclone"
	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/javi11/altmount/internal/pool"
	"github.com/javi11/nntppool/v2"
	"github.com/javi11/nntppool/v2/pkg/nntpcli"
	"github.com/javi11/nzbparser"
	concpool "github.com/sourcegraph/conc/pool"
)

// NzbType represents the type of NZB content
type NzbType string

const (
	NzbTypeSingleFile NzbType = "single_file"
	NzbTypeMultiFile  NzbType = "multi_file"
	NzbTypeRarArchive NzbType = "rar_archive"
	NzbType7zArchive  NzbType = "7z_archive"
	NzbTypeStrm       NzbType = "strm_file"
)

// ParsedNzb contains the parsed NZB data and extracted metadata
type ParsedNzb struct {
	Path          string
	Filename      string
	TotalSize     int64
	Type          NzbType
	Files         []ParsedFile
	SegmentsCount int
	SegmentSize   int64
	password      string
}

// ParsedFile represents a file extracted from the NZB
type ParsedFile struct {
	Subject      string
	Filename     string
	Size         int64
	Segments     []*metapb.SegmentData
	Groups       []string
	IsRarArchive bool
	Is7zArchive  bool
	Encryption   metapb.Encryption // Encryption type (e.g., "rclone"), nil if not encrypted
	Password     string            // Password from NZB meta, nil if not encrypted
	Salt         string            // Salt from NZB meta, nil if not encrypted
}

var (
	// Pattern to detect RAR files
	rarPattern = regexp.MustCompile(`(?i)\.r(ar|\d+)$|\.part\d+\.rar$`)
	// Pattern to detect 7zip files
	sevenZipPattern = regexp.MustCompile(`(?i)\.7z$|\.7z\.\d+$`)
	// Pattern to detect PAR2 files
	par2Pattern = regexp.MustCompile(`(?i)\.par2$|\.p\d+$|\.vol\d+\+\d+\.par2$`)
)

// PAR2PacketHeader represents the header of a PAR2 packet
type PAR2PacketHeader struct {
	Magic      [8]byte  // "PAR2\0PKT"
	Length     uint64   // Total packet length including header
	MD5Hash    [16]byte // MD5 hash of packet
	RecoveryID [16]byte // Recovery Set ID
	Type       [16]byte // Packet type
}

// PAR2FileDesc represents a file description packet content
type PAR2FileDesc struct {
	FileID     [16]byte // Unique file identifier
	FileMD5    [16]byte // MD5 hash of entire file
	File16kMD5 [16]byte // MD5 hash of first 16KB
	FileLength uint64   // File length in bytes
	Filename   string   // Original filename (variable length)
}

// Parser handles NZB file parsing
type Parser struct {
	poolManager  pool.Manager  // Pool manager for dynamic pool access
	log          *slog.Logger  // Logger for debug/error messages
	deobfuscator *Deobfuscator // Filename deobfuscator
}

// NewParser creates a new NZB parser
func NewParser(poolManager pool.Manager) *Parser {
	return &Parser{
		poolManager:  poolManager,
		log:          slog.Default().With("component", "nzb-parser"),
		deobfuscator: NewDeobfuscator(poolManager),
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
			parsed.password = pwd
		}

		if v, ok := n.Meta["chunk_size"]; ok {
			if iv, err := strconv.ParseInt(v, 10, 64); err == nil && iv > 0 {
				segSize = iv
			}
		}
	}

	// Extract PAR2 file descriptors before processing files
	// This provides accurate filename and size information via MD5 hash matching
	par2Descriptors := p.extractPAR2Descriptors(n.Files)

	// Process each file in the NZB in parallel
	// Filter out PAR2 files first
	var validFiles []nzbparser.NzbFile
	for _, file := range n.Files {
		if !par2Pattern.MatchString(file.Filename) {
			validFiles = append(validFiles, file)
		}
	}

	if len(validFiles) == 0 {
		return nil, NewNonRetryableError("NZB file contains no valid files (only PAR2)", nil)
	}

	// Use conc pool for parallel processing with proper error handling
	type fileResult struct {
		parsedFile *ParsedFile
		err        error
	}

	concPool := concpool.NewWithResults[fileResult]().WithMaxGoroutines(runtime.NumCPU())

	// Process files in parallel using conc pool
	for _, file := range validFiles {
		concPool.Go(func() fileResult {
			parsedFile, err := p.parseFile(file, n.Meta, n.Files, parsed.Filename, par2Descriptors)

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
			return nil, fmt.Errorf("failed to parse file %s: %w", validFiles[i].Subject, result.err)
		}
		parsedFiles = append(parsedFiles, result.parsedFile)
	}

	// Aggregate results in the original order
	for _, parsedFile := range parsedFiles {
		parsed.Files = append(parsed.Files, *parsedFile)
		parsed.TotalSize += parsedFile.Size
		parsed.SegmentsCount += len(parsedFile.Segments)

		if len(parsedFile.Segments) > 0 {
			// Find the corresponding original file to check segment bytes
			for _, file := range validFiles {
				if file.Subject == parsedFile.Subject {
					if len(file.Segments) > 0 && file.Segments[0].Bytes > int(segSize) {
						// Fallback to the first segment size encountered
						segSize = int64(file.Segments[0].Bytes)
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
func (p *Parser) parseFile(file nzbparser.NzbFile, meta map[string]string, allFiles []nzbparser.NzbFile, nzbFilename string, par2Descriptors map[[16]byte]*PAR2FileDesc) (*ParsedFile, error) {
	sort.Sort(file.Segments)

	// Fetch yEnc headers from the first segment to get correct filename and file size, some nzbs have wrong filename in the segments
	var yencFilename string
	var yencFileSize int64
	if p.poolManager != nil && p.poolManager.HasPool() && len(file.Segments) > 0 {
		firstPartHeaders, err := p.fetchYencHeaders(file.Segments[0], nil)
		if err != nil {
			// If we can't fetch yEnc headers, log and continue with original sizes
			return nil, fmt.Errorf("failed to fetch first segment yEnc part size: %w", err)
		}

		yencFilename = firstPartHeaders.FileName
		yencFileSize = int64(firstPartHeaders.FileSize)
	}

	// Normalize segment sizes using yEnc PartSize headers if needed
	// This handles cases where NZB segment sizes include yEnc encoding overhead
	if p.poolManager != nil && p.poolManager.HasPool() {
		err := p.normalizeSegmentSizesWithYenc(file.Segments)
		if err != nil {
			// Log the error but continue with original segment sizes
			// This ensures processing continues even if yEnc header fetching fails
			p.log.Warn("Failed to normalize segment sizes with yEnc headers",
				"error", err,
				"segments", len(file.Segments))

			if errors.Is(err, nntppool.ErrArticleNotFoundInProviders) {
				return nil, NewNonRetryableError("failed to fetch yEnc headers: missing articles in all providers", err)
			}
		}
	}

	// Convert segments
	segments := make([]*metapb.SegmentData, len(file.Segments))

	for i, seg := range file.Segments {
		segments[i] = &metapb.SegmentData{
			Id:          seg.ID,
			StartOffset: int64(0),
			EndOffset:   int64(seg.Bytes - 1),
			SegmentSize: int64(seg.Bytes),
		}
	}

	// Use yEnc file size if available, otherwise calculate using the sophisticated logic
	var totalSize int64
	if yencFileSize > 0 {
		totalSize = yencFileSize
	} else {
		var err error
		totalSize, err = p.calculateFileSize(file)
		if err != nil {
			// If we can't get the actual size, fallback to segment sum
			totalSize = p.calculateSegmentSum(file)
		}
	}

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

	// Extract filename - priority: PAR2 match > yEnc headers > meta file_name > file.Filename
	enc := metapb.Encryption_NONE // Default to no encryption

	// Try PAR2 matching first if descriptors are available
	var par2Matched *PAR2FileDesc
	if len(par2Descriptors) > 0 {
		par2Matched = p.matchFileToPAR2Descriptor(file, par2Descriptors)
	}

	// Start with PAR2 matched filename if available, then yEnc, otherwise use NZB filename
	var filename string
	if par2Matched != nil {
		// PAR2 match is highest priority - use PAR2 filename and size
		filename = par2Matched.Filename
		totalSize = int64(par2Matched.FileLength)
	} else {
		// Fallback to existing logic: yEnc > NZB filename
		filename = yencFilename
		if filename == "" || IsProbablyObfuscated(filename) {
			filename = file.Filename
		}
	}

	// Check metadata for overrides
	if meta != nil {
		if metaFilename, ok := meta["file_name"]; ok && metaFilename != "" {
			if fSize, ok := meta["file_size"]; ok {
				// This is a usenet-drive nzb with one file
				metaFilename = strings.TrimSuffix(nzbFilename, filepath.Ext(nzbFilename))
				fileExt := filepath.Ext(metaFilename)
				if fileExt == "" {
					if fe, ok := meta["file_extension"]; ok {
						metaFilename = metaFilename + fe
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

	// Attempt deobfuscation if filename appears obfuscated
	if IsProbablyObfuscated(filename) {
		p.log.Debug("Attempting deobfuscation", "filename", filename, "subject", file.Subject)

		// Attempt deobfuscation using all available files in the NZB
		if result := p.deobfuscator.DeobfuscateFilename(filename, allFiles, file); result.Success {
			filename = result.DeobfuscatedFilename
		} else {
			p.log.Warn("Unable to deobfuscate filename",
				"filename", filename,
				"subject", file.Subject)
		}
	}

	// Check if this is a RAR file or 7zip file
	isRarArchive := rarPattern.MatchString(filename)
	is7zArchive := sevenZipPattern.MatchString(filename)

	parsedFile := &ParsedFile{
		Subject:      file.Subject,
		Filename:     filename,
		Size:         totalSize,
		Segments:     segments,
		Groups:       file.Groups,
		IsRarArchive: isRarArchive,
		Is7zArchive:  is7zArchive,
		Encryption:   enc,
		Password:     password,
		Salt:         salt,
	}

	return parsedFile, nil
}

// calculateFileSize implements the sophisticated size calculation logic
func (p *Parser) calculateFileSize(file nzbparser.NzbFile) (int64, error) {
	// Priority 3: Different segment sizes - fetch yenc header to get actual file size
	if p.poolManager != nil && p.poolManager.HasPool() {
		if actualSize, err := p.fetchActualFileSizeFromYencHeader(file); err == nil {
			return actualSize, nil
		}
	}

	// Fallback: use segment sum if yenc header fetch failed
	return p.calculateSegmentSum(file), nil
}

// calculateSegmentSum calculates the total size by summing all segment sizes
func (p *Parser) calculateSegmentSum(file nzbparser.NzbFile) int64 {
	var segmentSum int64
	for _, seg := range file.Segments {
		segmentSum += int64(seg.Bytes)
	}
	return segmentSum
}

// fetchActualFileSizeFromYencHeader fetches the yenc header to get the actual file size
func (p *Parser) fetchActualFileSizeFromYencHeader(file nzbparser.NzbFile) (int64, error) {
	if p.poolManager == nil {
		return 0, NewNonRetryableError("no pool manager available", nil)
	}

	cp, err := p.poolManager.GetPool()
	if err != nil {
		return 0, NewNonRetryableError("no connection pool available", err)
	}

	if len(file.Segments) == 0 {
		return 0, fmt.Errorf("no segments available")
	}

	// Use first segment to get yenc headers
	firstSegment := file.Segments[0]

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*30)
	defer cancel()

	// Get a connection from the pool
	r, err := cp.BodyReader(ctx, firstSegment.ID, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to get body reader: %w", err)
	}
	defer r.Close()

	// Get yenc headers
	h, err := r.GetYencHeaders()
	if err != nil {
		return 0, fmt.Errorf("failed to get yenc headers: %w", err)
	}

	if h.FileSize <= 0 {
		return 0, fmt.Errorf("invalid file size from yenc header: %d", h.FileSize)
	}

	return int64(h.FileSize), nil
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

// extractPAR2Descriptors extracts file descriptors from PAR2 files in the NZB
// Returns a map of File16kMD5 hash to PAR2FileDesc for fast lookup
func (p *Parser) extractPAR2Descriptors(allFiles []nzbparser.NzbFile) map[[16]byte]*PAR2FileDesc {
	descriptors := make(map[[16]byte]*PAR2FileDesc)

	if p.poolManager == nil || !p.poolManager.HasPool() {
		p.log.Debug("No pool manager available for PAR2 extraction")
		return descriptors
	}

	// Find the smallest PAR2 file (likely the index file, not a .vol file)
	var smallestPAR2 *nzbparser.NzbFile
	var smallestSize int64 = -1

	for i := range allFiles {
		file := &allFiles[i]
		// Look for .par2 files but exclude .vol files (recovery volumes)
		if strings.HasSuffix(strings.ToLower(file.Filename), ".par2") &&
			!strings.Contains(strings.ToLower(file.Filename), ".vol") {

			// Calculate total file size from segments
			var totalSize int64
			for _, seg := range file.Segments {
				totalSize += int64(seg.Bytes)
			}

			if smallestSize == -1 || totalSize < smallestSize {
				smallestSize = totalSize
				smallestPAR2 = file
			}
		}
	}

	if smallestPAR2 == nil {
		p.log.Debug("No PAR2 index file found in NZB")
		return descriptors
	}

	// Download and parse the PAR2 file
	parsed := p.parsePAR2File(*smallestPAR2)
	if len(parsed) == 0 {
		p.log.Warn("Failed to extract any file descriptors from PAR2",
			"filename", smallestPAR2.Filename)
		return descriptors
	}

	// Build the lookup map
	for i := range parsed {
		desc := &parsed[i]
		descriptors[desc.File16kMD5] = desc
	}

	return descriptors
}

// parsePAR2File downloads and parses a PAR2 file to extract file descriptors
func (p *Parser) parsePAR2File(par2File nzbparser.NzbFile) []PAR2FileDesc {
	var descriptors []PAR2FileDesc

	if len(par2File.Segments) == 0 {
		return descriptors
	}

	cp, err := p.poolManager.GetPool()
	if err != nil {
		p.log.Debug("Failed to get connection pool for PAR2 parsing", "error", err)
		return descriptors
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*30)
	defer cancel()

	// Get the first segment to start parsing
	firstSegment := par2File.Segments[0]
	r, err := cp.BodyReader(ctx, firstSegment.ID, par2File.Groups)
	if err != nil {
		p.log.Debug("Failed to get body reader for PAR2 file", "error", err)
		return descriptors
	}
	defer r.Close()

	// Stream parse PAR2 packets
	maxPackets := 100 // Limit the number of packets to process
	packetCount := 0

	for packetCount < maxPackets {
		// Parse packet header
		header, err := p.parsePAR2Header(r)
		if err != nil {
			p.log.Debug("Failed to parse PAR2 header or reached end", "error", err)
			break
		}

		packetCount++

		// Check if this is a file description packet
		expectedFileDescType := [16]byte{'P', 'A', 'R', ' ', '2', '.', '0', 0, 'F', 'i', 'l', 'e', 'D', 'e', 's', 'c'}
		if header.Type == expectedFileDescType {

			// Parse file description packet
			fileDesc, err := p.parseFileDescPacket(r, header.Length)
			if err != nil {
				p.log.Debug("Failed to parse file description packet", "error", err)
				// Skip to next packet
				continue
			}

			descriptors = append(descriptors, *fileDesc)
		} else {
			// Skip non-file-description packets
			remainingBytes := header.Length - 64 // Header is 64 bytes
			if remainingBytes > 0 {
				if err := p.skipBytes(r, remainingBytes); err != nil {
					p.log.Debug("Failed to skip packet content", "error", err)
					break
				}
			}
		}
	}

	p.log.Debug("Completed PAR2 parsing",
		"packets_processed", packetCount,
		"descriptors_found", len(descriptors))

	return descriptors
}

// parsePAR2Header reads and validates a PAR2 packet header from a reader
func (p *Parser) parsePAR2Header(r io.Reader) (*PAR2PacketHeader, error) {
	header := &PAR2PacketHeader{}

	// Read the header (8 + 8 + 16 + 16 + 16 = 64 bytes)
	if err := binary.Read(r, binary.LittleEndian, header); err != nil {
		return nil, fmt.Errorf("failed to read PAR2 header: %w", err)
	}

	// Validate magic signature
	expectedMagic := [8]byte{'P', 'A', 'R', '2', 0, 'P', 'K', 'T'}
	if header.Magic != expectedMagic {
		return nil, fmt.Errorf("invalid PAR2 magic signature")
	}

	return header, nil
}

// parseFileDescPacket reads and parses a file description packet
func (p *Parser) parseFileDescPacket(r io.Reader, packetLength uint64) (*PAR2FileDesc, error) {
	// Calculate remaining bytes after header (64 bytes)
	contentLength := packetLength - 64
	if contentLength < 56 { // Minimum: 16 + 16 + 16 + 8 = 56 bytes for fixed fields
		return nil, fmt.Errorf("file description packet too small: %d bytes", contentLength)
	}

	desc := &PAR2FileDesc{}

	// Read fixed fields (56 bytes total)
	if err := binary.Read(r, binary.LittleEndian, &desc.FileID); err != nil {
		return nil, fmt.Errorf("failed to read FileID: %w", err)
	}
	if err := binary.Read(r, binary.LittleEndian, &desc.FileMD5); err != nil {
		return nil, fmt.Errorf("failed to read FileMD5: %w", err)
	}
	if err := binary.Read(r, binary.LittleEndian, &desc.File16kMD5); err != nil {
		return nil, fmt.Errorf("failed to read File16kMD5: %w", err)
	}
	if err := binary.Read(r, binary.LittleEndian, &desc.FileLength); err != nil {
		return nil, fmt.Errorf("failed to read FileLength: %w", err)
	}

	// Read filename (remaining bytes)
	filenameLength := contentLength - 56
	if filenameLength > 0 {
		filenameBytes := make([]byte, filenameLength)
		if _, err := io.ReadFull(r, filenameBytes); err != nil {
			return nil, fmt.Errorf("failed to read filename: %w", err)
		}

		// Remove padding bytes (PAR2 uses 4-byte alignment)
		// Find the actual end of the filename by looking for null bytes
		actualLength := filenameLength
		for i := len(filenameBytes) - 1; i >= 0; i-- {
			if filenameBytes[i] == 0 || filenameBytes[i] < 32 {
				actualLength = uint64(i)
			} else {
				break
			}
		}

		desc.Filename = string(filenameBytes[:actualLength])
	}

	return desc, nil
}

// skipBytes skips the specified number of bytes in the reader
func (p *Parser) skipBytes(r io.Reader, n uint64) error {
	if n == 0 {
		return nil
	}

	// Use io.CopyN with a discard writer to skip bytes efficiently
	_, err := io.CopyN(io.Discard, r, int64(n))
	return err
}

// matchFileToPAR2Descriptor matches a file to its PAR2 descriptor using MD5 hash of first 16KB
// Returns the matched descriptor if found, nil otherwise
func (p *Parser) matchFileToPAR2Descriptor(
	file nzbparser.NzbFile,
	par2Descriptors map[[16]byte]*PAR2FileDesc,
) *PAR2FileDesc {
	// Can't match if no pool available or no segments
	if p.poolManager == nil || !p.poolManager.HasPool() || len(file.Segments) == 0 {
		return nil
	}

	cp, err := p.poolManager.GetPool()
	if err != nil {
		p.log.Debug("Failed to get connection pool for PAR2 matching", "error", err)
		return nil
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*30)
	defer cancel()

	// Download first 16KB of the first segment
	firstSegment := file.Segments[0]
	r, err := cp.BodyReader(ctx, firstSegment.ID, file.Groups)
	if err != nil {
		p.log.Debug("Failed to get body reader for PAR2 matching",
			"subject", file.Subject,
			"error", err)
		return nil
	}
	defer r.Close()

	// Read up to 16KB of data
	const maxRead = 16 * 1024 // 16KB
	buffer := make([]byte, maxRead)

	bytesRead, err := io.ReadFull(r, buffer)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		p.log.Debug("Failed to read first 16KB for PAR2 matching",
			"subject", file.Subject,
			"error", err)
		return nil
	}

	// If we read less than 16KB, use only what we read
	if bytesRead < maxRead {
		buffer = buffer[:bytesRead]
	}

	// Compute MD5 hash of the data
	hash := md5.Sum(buffer)

	// Look up in PAR2 descriptors map
	if desc, found := par2Descriptors[hash]; found {
		return desc
	}

	return nil
}
