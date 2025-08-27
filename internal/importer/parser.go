package importer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/javi11/altmount/internal/encryption"
	"github.com/javi11/altmount/internal/encryption/rclone"
	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/javi11/altmount/internal/pool"
	"github.com/javi11/nntppool"
	"github.com/javi11/nzbparser"
)

// NzbType represents the type of NZB content
type NzbType string

const (
	NzbTypeSingleFile NzbType = "single_file"
	NzbTypeMultiFile  NzbType = "multi_file"
	NzbTypeRarArchive NzbType = "rar_archive"
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
}

// ParsedFile represents a file extracted from the NZB
type ParsedFile struct {
	Subject      string
	Filename     string
	Size         int64
	Segments     []*metapb.SegmentData
	Groups       []string
	IsRarArchive bool
	Encryption   metapb.Encryption // Encryption type (e.g., "rclone"), nil if not encrypted
	Password     string            // Password from NZB meta, nil if not encrypted
	Salt         string            // Salt from NZB meta, nil if not encrypted
}

var (
	// Pattern to detect RAR files
	rarPattern = regexp.MustCompile(`(?i)\.r(ar|\d+)$|\.part\d+\.rar$`)
	// Pattern to detect PAR2 files
	par2Pattern = regexp.MustCompile(`(?i)\.par2$|\.p\d+$|\.vol\d+\+\d+\.par2$`)
)

// Parser handles NZB file parsing
type Parser struct {
	poolManager pool.Manager // Pool manager for dynamic pool access
	log         *slog.Logger // Logger for debug/error messages
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
		if v, ok := n.Meta["chunk_size"]; ok {
			if iv, err := strconv.ParseInt(v, 10, 64); err == nil && iv > 0 {
				segSize = iv
			}
		}
	}

	// Process each file in the NZB
	for _, file := range n.Files {
		// Skip PAR2 files
		if par2Pattern.MatchString(file.Filename) {
			continue
		}

		parsedFile, err := p.parseFile(file, n.Meta)
		if err != nil {
			return nil, fmt.Errorf("failed to parse file %s: %w", file.Subject, err)
		}

		parsed.Files = append(parsed.Files, *parsedFile)
		parsed.TotalSize += parsedFile.Size
		parsed.SegmentsCount += len(parsedFile.Segments)

		if len(file.Segments) > 0 && file.Segments[0].Bytes > int(segSize) {
			// Fallback to the first segment size encountered
			segSize = int64(file.Segments[0].Bytes)
		}
	}

	parsed.SegmentSize = segSize

	// Determine NZB type based on content analysis
	parsed.Type = p.determineNzbType(parsed.Files)

	return parsed, nil
}

// parseFile processes a single file entry from the NZB
func (p *Parser) parseFile(file nzbparser.NzbFile, meta map[string]string) (*ParsedFile, error) {

	// Normalize segment sizes using yEnc PartSize headers if needed
	// This handles cases where NZB segment sizes include yEnc encoding overhead
	if p.poolManager != nil && p.poolManager.HasPool() && len(file.Segments) >= 2 {
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
	// Calculate total size using the sophisticated logic
	totalSize, err := p.calculateFileSize(file)
	if err != nil {
		// If we can't get the actual size, fallback to segment sum
		totalSize = p.calculateSegmentSum(file)
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

	// Extract filename - priority: meta file_name > file.Filename
	enc := metapb.Encryption_NONE // Default to no encryption

	filename := file.Filename
	if meta != nil {
		if metaFilename, ok := meta["file_name"]; ok && metaFilename != "" {
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

	if IsProbablyObfuscated(filename) {
		p.log.Warn("File appears obfuscated", "filename", filename, "subject", file.Subject)

		return nil, NewNonRetryableError(fmt.Sprintf("file appears obfuscated: %s", filename), nil)
	}

	// Check if this is a RAR file
	isRarArchive := rarPattern.MatchString(filename)

	parsedFile := &ParsedFile{
		Subject:      file.Subject,
		Filename:     filename,
		Size:         totalSize,
		Segments:     segments,
		Groups:       file.Groups,
		IsRarArchive: isRarArchive,
		Encryption:   enc,
		Password:     password,
		Salt:         salt,
	}

	return parsedFile, nil
}

// calculateFileSize implements the sophisticated size calculation logic
func (p *Parser) calculateFileSize(file nzbparser.NzbFile) (int64, error) {
	// No file.Bytes available, need to analyze segments
	if len(file.Segments) < 2 {
		// Not enough segments to compare, use segment sum
		return p.calculateSegmentSum(file), nil
	}

	firstSegSize := int64(file.Segments[0].Bytes)
	secondSegSize := int64(file.Segments[1].Bytes)

	// Priority 2: If first and second segments have the same size, use segment sum
	if firstSegSize == secondSegSize {
		return p.calculateSegmentSum(file), nil
	}

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
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()

	// Get a connection from the pool
	r, err := cp.BodyReader(ctx, firstSegment.ID, file.Groups)
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
func (p *Parser) fetchYencPartSize(segment nzbparser.NzbSegment, groups []string) (int64, error) {
	if p.poolManager == nil {
		return 0, NewNonRetryableError("no pool manager available", nil)
	}

	cp, err := p.poolManager.GetPool()
	if err != nil {
		return 0, NewNonRetryableError("no connection pool available", err)
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()

	// Get a connection from the pool
	r, err := cp.BodyReader(ctx, segment.ID, groups)
	if err != nil {
		return 0, fmt.Errorf("failed to get body reader: %w", err)
	}
	defer r.Close()

	// Get yenc headers
	h, err := r.GetYencHeaders()
	if err != nil {
		return 0, fmt.Errorf("failed to get yenc headers: %w", err)
	}

	if h.PartSize <= 0 {
		return 0, fmt.Errorf("invalid part size from yenc header: %d", h.PartSize)
	}

	return int64(h.PartSize), nil
}

// normalizeSegmentSizesWithYenc normalizes segment sizes using yEnc PartSize headers
// This handles cases where NZB segment sizes include yEnc overhead
func (p *Parser) normalizeSegmentSizesWithYenc(segments []nzbparser.NzbSegment) error {
	if len(segments) < 2 {
		// Not enough segments to determine if normalization is needed
		return nil
	}

	firstSegSize := segments[0].Bytes
	secondSegSize := segments[1].Bytes

	// If first and second segments have the same size, assume no yEnc overhead
	if firstSegSize == secondSegSize {
		p.log.Debug("Segments have consistent sizes, skipping yEnc normalization")
		return nil
	}

	// Different segment sizes detected - fetch yEnc headers to get actual part sizes
	// Fetch PartSize from first segment
	firstPartSize, err := p.fetchYencPartSize(segments[0], nil)
	if err != nil {
		// If we can't fetch yEnc headers, log and continue with original sizes
		return fmt.Errorf("failed to fetch first segment yEnc part size: %w", err)
	}

	// Fetch PartSize from last segment
	lastSegmentIndex := len(segments) - 1
	lastPartSize, err := p.fetchYencPartSize(segments[lastSegmentIndex], nil)
	if err != nil {
		// If we can't fetch yEnc headers, log and continue with original sizes
		return fmt.Errorf("failed to fetch last segment yEnc part size: %w", err)
	}

	// Override all segments except the last one with the first segment's PartSize
	for i := 0; i < len(segments)-1; i++ {
		segments[i].Bytes = int(firstPartSize)
	}

	// Override the last segment with its specific PartSize
	lastSegmentIdx := len(segments) - 1
	segments[lastSegmentIdx].Bytes = int(lastPartSize)

	return nil
}

// determineNzbType analyzes the parsed files to determine the NZB type
func (p *Parser) determineNzbType(files []ParsedFile) NzbType {
	if len(files) == 1 {
		// Single file NZB
		if files[0].IsRarArchive {
			return NzbTypeRarArchive
		}
		return NzbTypeSingleFile
	}

	// Multiple files - check if any are RAR archives
	hasRarFiles := false
	for _, file := range files {
		if file.IsRarArchive {
			hasRarFiles = true
			break
		}
	}

	if hasRarFiles {
		return NzbTypeRarArchive
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
