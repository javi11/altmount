package nzb

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/altmount/internal/encryption/rclone"
	"github.com/javi11/nntppool"
	"github.com/javi11/nzbparser"
)

// ParsedNzb contains the parsed NZB data and extracted metadata
type ParsedNzb struct {
	Path          string
	Filename      string
	TotalSize     int64
	Type          database.NzbType
	Files         []ParsedFile
	SegmentsCount int
	SegmentSize   int64
	Password      *string // Password from NZB meta, nil if not encrypted
	Salt          *string // Salt from NZB meta, nil if not encrypted
}

// ParsedFile represents a file extracted from the NZB
type ParsedFile struct {
	Subject      string
	Filename     string
	Size         int64
	Segments     []database.NzbSegment
	Groups       []string
	IsRarArchive bool
	RarContents  []RarFileEntry // Only populated if IsRarArchive is true
	Encryption   *string        // Encryption type (e.g., "rclone"), nil if not encrypted
}

// RarFileEntry represents a file within a RAR archive
type RarFileEntry struct {
	Path           string
	Filename       string
	Size           int64
	CompressedSize int64
	CRC32          string
	IsDirectory    bool
	ModTime        time.Time
	Attributes     uint8
}

var (
	// Pattern to detect RAR files
	rarPattern = regexp.MustCompile(`(?i)\.r(ar|\d+)$|\.part\d+\.rar$`)
)

// Parser handles NZB file parsing
type Parser struct {
	cp nntppool.UsenetConnectionPool // Connection pool for yenc header fetching
}

// NewParser creates a new NZB parser
func NewParser(cp nntppool.UsenetConnectionPool) *Parser {
	return &Parser{
		cp: cp,
	}
}

// ParseFile parses an NZB file from a reader
func (p *Parser) ParseFile(r io.Reader, nzbPath string) (*ParsedNzb, error) {
	n, err := nzbparser.Parse(r)
	if err != nil {
		return nil, fmt.Errorf("failed to parse NZB XML: %w", err)
	}

	if len(n.Files) == 0 {
		return nil, fmt.Errorf("NZB file contains no files")
	}

	parsed := &ParsedNzb{
		Path:     nzbPath,
		Filename: filepath.Base(nzbPath),
		Files:    make([]ParsedFile, 0, len(n.Files)),
	}

	// Extract credentials from metadata if present
	if n.Meta != nil {
		if password, ok := n.Meta["password"]; ok && password != "" {
			parsed.Password = &password
		}
		if salt, ok := n.Meta["salt"]; ok && salt != "" {
			parsed.Salt = &salt
		}
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
		parsedFile, err := p.parseFile(file, n.Meta)
		if err != nil {
			return nil, fmt.Errorf("failed to parse file %s: %w", file.Subject, err)
		}

		parsed.Files = append(parsed.Files, *parsedFile)
		parsed.TotalSize += parsedFile.Size
		parsed.SegmentsCount += len(parsedFile.Segments)

		if segSize == 0 && len(file.Segments) > 0 {
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
	// Convert segments
	segments := make([]database.NzbSegment, len(file.Segments))

	for i, seg := range file.Segments {
		segments[i] = database.NzbSegment{
			Number:    seg.Number,
			Bytes:     int64(seg.Bytes),
			MessageID: seg.ID,
			Groups:    file.Groups,
		}
	}

	// Calculate total size using the sophisticated logic
	totalSize, err := p.calculateFileSize(file)
	if err != nil {
		// If we can't get the actual size, fallback to segment sum
		totalSize = p.calculateSegmentSum(file)
	}

	// Extract filename - priority: meta file_name > file.Filename
	var encryption *string

	filename := file.Filename
	if meta != nil {
		if metaFilename, ok := meta["file_name"]; ok && metaFilename != "" {
			// This will add support for rclone encrypted files
			if strings.HasSuffix(strings.ToLower(metaFilename), rclone.EncFileExtension) {
				filename = metaFilename[:len(metaFilename)-4]
				encType := "rclone"
				encryption = &encType

				decSize, err := rclone.DecryptedSize(totalSize)
				if err != nil {
					return nil, fmt.Errorf("failed to get decrypted size: %w", err)
				}

				totalSize = decSize
			} else {
				filename = metaFilename
			}
		}
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
		Encryption:   encryption,
	}

	return parsedFile, nil
}

// calculateFileSize implements the sophisticated size calculation logic
func (p *Parser) calculateFileSize(file nzbparser.NzbFile) (int64, error) {
	// Priority 1: If file.Bytes is present, use that as totalSize
	if file.Bytes > 0 {
		return int64(file.Bytes), nil
	}

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
	if p.cp != nil {
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
	if p.cp == nil {
		return 0, fmt.Errorf("no connection pool available")
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
	r, err := p.cp.BodyReader(ctx, firstSegment.ID, file.Groups)
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

// determineNzbType analyzes the parsed files to determine the NZB type
func (p *Parser) determineNzbType(files []ParsedFile) database.NzbType {
	if len(files) == 1 {
		// Single file NZB
		if files[0].IsRarArchive {
			return database.NzbTypeRarArchive
		}
		return database.NzbTypeSingleFile
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
		return database.NzbTypeRarArchive
	}

	return database.NzbTypeMultiFile
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
		return fmt.Errorf("invalid NZB: total size is zero")
	}

	if parsed.SegmentsCount <= 0 {
		return fmt.Errorf("invalid NZB: no segments found")
	}

	for i, file := range parsed.Files {
		if len(file.Segments) == 0 {
			return fmt.Errorf("invalid NZB: file %d has no segments", i)
		}

		if file.Size <= 0 {
			return fmt.Errorf("invalid NZB: file %d has invalid size", i)
		}

		if len(file.Groups) == 0 {
			return fmt.Errorf("invalid NZB: file %d has no groups", i)
		}
	}

	return nil
}

// ConvertToDbSegments converts ParsedFile segments to database format
func (p *Parser) ConvertToDbSegments(files []ParsedFile) database.NzbSegments {
	var allSegments database.NzbSegments

	for _, file := range files {
		allSegments = append(allSegments, file.Segments...)
	}

	return allSegments
}

// ConvertToDbSegmentsForFile converts segments from a single ParsedFile to database format
func (p *Parser) ConvertToDbSegmentsForFile(file ParsedFile) database.NzbSegments {
	return file.Segments
}
