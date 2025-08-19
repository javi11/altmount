package importer

import (
	"bufio"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/javi11/altmount/internal/encryption"
	"github.com/javi11/altmount/internal/encryption/rclone"
	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/javi11/nxg"
)

// StrmParser handles STRM file parsing containing NXG links
type StrmParser struct {
	log *slog.Logger
}

// NewStrmParser creates a new STRM parser
func NewStrmParser() *StrmParser {
	return &StrmParser{
		log: slog.Default().With("component", "strm-parser"),
	}
}

// ParseStrmFile parses a STRM file containing an NXG link
func (p *StrmParser) ParseStrmFile(r io.Reader, strmPath string) (*ParsedNzb, error) {
	scanner := bufio.NewScanner(r)

	// Read the first non-empty line from the STRM file
	var nxgLink string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" && strings.HasPrefix(line, "nxglnk://") {
			nxgLink = line
			break
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to read STRM file: %w", err)
	}

	if nxgLink == "" {
		return nil, fmt.Errorf("no valid NXG link found in STRM file")
	}

	// Parse the NXG link
	parsedFile, err := p.parseNxgLink(nxgLink)
	if err != nil {
		return nil, fmt.Errorf("failed to parse NXG link: %w", err)
	}

	// Create ParsedNzb structure
	parsed := &ParsedNzb{
		Path:          strmPath,
		Filename:      filepath.Base(strmPath),
		Type:          NzbTypeStrm,
		Files:         []ParsedFile{*parsedFile},
		TotalSize:     parsedFile.Size,
		SegmentsCount: len(parsedFile.Segments),
		SegmentSize:   0, // Will be set from chunk_size
	}

	// Extract segment size from the first segment if available
	if len(parsedFile.Segments) > 0 {
		// For NXG links, all segments should be the same size except possibly the last one
		firstSegmentSize := parsedFile.Segments[0].EndOffset - parsedFile.Segments[0].StartOffset + 1
		parsed.SegmentSize = firstSegmentSize
	}

	return parsed, nil
}

// parseNxgLink parses an NXG link and returns a ParsedFile
func (p *StrmParser) parseNxgLink(nxgLink string) (*ParsedFile, error) {
	// Parse the URL
	u, err := url.Parse(nxgLink)
	if err != nil {
		return nil, fmt.Errorf("invalid NXG URL: %w", err)
	}

	if u.Scheme != "nxglnk" {
		return nil, fmt.Errorf("invalid URL scheme: %s, expected nxglnk", u.Scheme)
	}

	// Extract query parameters
	params := u.Query()

	// Extract required parameters
	h := params.Get("h")
	if h == "" {
		return nil, fmt.Errorf("missing required parameter 'h'")
	}

	chunkSizeStr := params.Get("chunk_size")
	if chunkSizeStr == "" {
		return nil, fmt.Errorf("missing required parameter 'chunk_size'")
	}
	chunkSize, err := strconv.ParseInt(chunkSizeStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid chunk_size: %w", err)
	}

	fileSizeStr := params.Get("file_size")
	if fileSizeStr == "" {
		return nil, fmt.Errorf("missing required parameter 'file_size'")
	}
	fileSize, err := strconv.ParseInt(fileSizeStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid file_size: %w", err)
	}

	filename := params.Get("name")
	if filename == "" {
		return nil, fmt.Errorf("missing required parameter 'name'")
	}

	cipher := params.Get("cipher")
	password := params.Get("password")
	salt := params.Get("salt")

	// Decode NXG header using the nxg library to validate it
	_, err = nxg.DecodeNXGHeader(h)
	if err != nil {
		return nil, fmt.Errorf("failed to decode NXG header: %w", err)
	}

	// Create header from the h parameter (h is the base64 encoded header string)
	var header nxg.Header
	if len(h) > len(header) {
		return nil, fmt.Errorf("NXG header too long: %d bytes, expected max %d", len(h), len(header))
	}
	copy(header[:], []byte(h))

	// Calculate number of segments needed
	numSegments := (fileSize + chunkSize - 1) / chunkSize // Ceiling division

	// Generate segment IDs using the NXG library
	segmentIDs := make([]string, numSegments)
	for i := int64(0); i < numSegments; i++ {
		segmentID, err := header.GenerateSegmentID(nxg.PartTypeData, i+1)
		if err != nil {
			return nil, fmt.Errorf("failed to generate segment ID for part %d: %w", i+1, err)
		}
		segmentIDs[i] = segmentID
	}

	// Convert segment IDs to metadata segments
	segments := make([]*metapb.SegmentData, len(segmentIDs))
	var currentOffset int64

	for i, segmentID := range segmentIDs {
		segmentSize := chunkSize
		// Last segment might be smaller
		if currentOffset+chunkSize > fileSize {
			segmentSize = fileSize - currentOffset
		}

		segments[i] = &metapb.SegmentData{
			Id:          segmentID,
			StartOffset: currentOffset,
			EndOffset:   currentOffset + segmentSize - 1,
			SegmentSize: segmentSize,
		}
		currentOffset += segmentSize
	}

	// Determine encryption type
	var enc metapb.Encryption
	actualFilename := filename
	actualFileSize := fileSize

	switch cipher {
	case string(encryption.RCloneCipherType):
		enc = metapb.Encryption_RCLONE
		// If rclone encrypted, adjust filename and file size
		if strings.HasSuffix(strings.ToLower(filename), rclone.EncFileExtension) {
			actualFilename = filename[:len(filename)-4]
			decSize, err := rclone.DecryptedSize(fileSize)
			if err != nil {
				return nil, fmt.Errorf("failed to calculate decrypted size: %w", err)
			}
			actualFileSize = decSize
		}
	case string(encryption.HeadersCipherType):
		enc = metapb.Encryption_HEADERS
	default:
		enc = metapb.Encryption_NONE
	}

	// Check if this is a RAR file
	isRarArchive := rarPattern.MatchString(actualFilename)

	parsedFile := &ParsedFile{
		Subject:      fmt.Sprintf("NXG: %s", actualFilename),
		Filename:     actualFilename,
		Size:         actualFileSize,
		Segments:     segments,
		Groups:       []string{}, // NXG links don't have groups
		IsRarArchive: isRarArchive,
		Encryption:   enc,
		Password:     password,
		Salt:         salt,
	}

	return parsedFile, nil
}

// ValidateStrmFile performs basic validation on the parsed STRM file
func (p *StrmParser) ValidateStrmFile(parsed *ParsedNzb) error {
	if parsed.Type != NzbTypeStrm {
		return fmt.Errorf("invalid STRM: wrong type %s", parsed.Type)
	}

	if len(parsed.Files) != 1 {
		return fmt.Errorf("invalid STRM: should contain exactly one file, got %d", len(parsed.Files))
	}

	if parsed.TotalSize <= 0 {
		return fmt.Errorf("invalid STRM: total size is zero")
	}

	if parsed.SegmentsCount <= 0 {
		return fmt.Errorf("invalid STRM: no segments found")
	}

	file := parsed.Files[0]
	if len(file.Segments) == 0 {
		return fmt.Errorf("invalid STRM: file has no segments")
	}

	if file.Size <= 0 {
		return fmt.Errorf("invalid STRM: file has invalid size")
	}

	return nil
}
