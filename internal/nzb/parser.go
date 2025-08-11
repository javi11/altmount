package nzb

import (
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strconv"
	"time"

	"github.com/javi11/altmount/internal/database"
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
type Parser struct{}

// NewParser creates a new NZB parser
func NewParser() *Parser {
	return &Parser{}
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
		parsedFile, err := p.parseFile(file)
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
func (p *Parser) parseFile(file nzbparser.NzbFile) (*ParsedFile, error) {
	// Convert segments
	segments := make([]database.NzbSegment, len(file.Segments))
	var totalSize int64

	for i, seg := range file.Segments {
		segments[i] = database.NzbSegment{
			Number:    seg.Number,
			Bytes:     int64(seg.Bytes),
			MessageID: seg.ID,
			Groups:    file.Groups,
		}
		totalSize += int64(seg.Bytes)
	}

	// Extract filename from subject
	filename := file.Filename

	// Check if this is a RAR file
	isRarArchive := rarPattern.MatchString(filename)

	parsedFile := &ParsedFile{
		Subject:      file.Subject,
		Filename:     filename,
		Size:         totalSize,
		Segments:     segments,
		Groups:       file.Groups,
		IsRarArchive: isRarArchive,
	}

	return parsedFile, nil
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
