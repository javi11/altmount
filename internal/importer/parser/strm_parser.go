package parser

import (
	"bufio"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/javi11/altmount/internal/encryption"
	"github.com/javi11/altmount/internal/encryption/rclone"
	"github.com/javi11/altmount/internal/errors"
	"github.com/javi11/altmount/internal/importer/parser/fileinfo"
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
		return nil, errors.NewNonRetryableError("failed to read STRM file", err)
	}

	if nxgLink == "" {
		return nil, errors.NewNonRetryableError("no valid NXG link found in STRM file", nil)
	}

	// Parse the NXG link
	parsedFile, err := p.parseNxgLink(nxgLink)
	if err != nil {
		return nil, errors.NewNonRetryableError("failed to parse NXG link", err)
	}

	// Create ParsedNzb structure
	parsed := &ParsedNzb{
		Path:          strmPath,
		Filename:      filepath.Base(strmPath),
		Type:          NzbTypeStrm,
		Files:         []ParsedFile{*parsedFile},
		TotalSize:     parsedFile.Size,
		SegmentsCount: len(parsedFile.Segments),
	}

	return parsed, nil
}

// parseNxgLink parses an NXG link and returns a ParsedFile
func (p *StrmParser) parseNxgLink(nxgLink string) (*ParsedFile, error) {
	// Parse the URL
	u, err := url.Parse(nxgLink)
	if err != nil {
		return nil, errors.NewNonRetryableError("invalid NXG URL", err)
	}

	if u.Scheme != "nxglnk" {
		return nil, errors.NewNonRetryableError(fmt.Sprintf("invalid URL scheme: %s, expected nxglnk", u.Scheme), nil)
	}

	// Extract query parameters
	params := u.Query()

	// Extract required parameters
	h := params.Get("h")
	if h == "" {
		return nil, errors.NewNonRetryableError("missing required parameter 'h'", nil)
	}

	chunkSizeStr := params.Get("chunk_size")
	if chunkSizeStr == "" {
		return nil, errors.NewNonRetryableError("missing required parameter 'chunk_size'", nil)
	}
	chunkSize, err := strconv.ParseInt(chunkSizeStr, 10, 64)
	if err != nil {
		return nil, errors.NewNonRetryableError("invalid chunk_size", err)
	}

	fileSizeStr := params.Get("file_size")
	if fileSizeStr == "" {
		return nil, errors.NewNonRetryableError("missing required parameter 'file_size'", nil)
	}
	fileSize, err := strconv.ParseInt(fileSizeStr, 10, 64)
	if err != nil {
		return nil, errors.NewNonRetryableError("invalid file_size", err)
	}

	filename := params.Get("name")
	if filename == "" {
		return nil, errors.NewNonRetryableError("missing required parameter 'name'", nil)
	}

	cipher := params.Get("cipher")
	password := params.Get("password")
	salt := params.Get("salt")

	// Decode NXG header using the nxg library to validate it
	ngxh, err := nxg.DecodeNXGHeader(h)
	if err != nil {
		return nil, errors.NewNonRetryableError("failed to decode NXG header", err)
	}

	// Create header from the h parameter (h is the base64 encoded header string)
	header := nxg.Header([]byte(h))

	// Calculate number of segments needed
	numSegments := ngxh.TotalDataParts

	// Generate segment IDs using the NXG library
	segmentIDs := make([]string, numSegments)
	for i := int64(0); i < numSegments; i++ {
		segmentID, err := header.GenerateSegmentID(nxg.PartTypeData, i+1)
		if err != nil {
			return nil, errors.NewNonRetryableError(fmt.Sprintf("failed to generate segment ID for part %d", i+1), err)
		}

		segmentIDs[i] = segmentID
	}

	// Convert segment IDs to metadata segments
	segments := make([]*metapb.SegmentData, len(segmentIDs))

	for i, segmentID := range segmentIDs {
		segmentSize := chunkSize

		segments[i] = &metapb.SegmentData{
			Id:          segmentID,
			StartOffset: 0,
			EndOffset:   segmentSize - 1,
			SegmentSize: segmentSize,
		}
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
				return nil, errors.NewNonRetryableError("failed to calculate decrypted size", err)
			}
			actualFileSize = decSize
		}
	default:
		enc = metapb.Encryption_NONE
	}

	// Check if this is a RAR file
	isRarArchive := fileinfo.IsRarFile(actualFilename)

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
		ReleaseDate:  time.Now(), // STRM files don't have release dates, use current time
	}

	return parsedFile, nil
}

// ValidateStrmFile performs basic validation on the parsed STRM file
func (p *StrmParser) ValidateStrmFile(parsed *ParsedNzb) error {
	if parsed.Type != NzbTypeStrm {
		return errors.NewNonRetryableError(fmt.Sprintf("invalid STRM: wrong type %s", parsed.Type), nil)
	}

	if len(parsed.Files) != 1 {
		return errors.NewNonRetryableError(fmt.Sprintf("invalid STRM: should contain exactly one file, got %d", len(parsed.Files)), nil)
	}

	if parsed.TotalSize <= 0 {
		return errors.NewNonRetryableError("invalid STRM: total size is zero", nil)
	}

	if parsed.SegmentsCount <= 0 {
		return errors.NewNonRetryableError("invalid STRM: no segments found", nil)
	}

	file := parsed.Files[0]
	if len(file.Segments) == 0 {
		return errors.NewNonRetryableError("invalid STRM: file has no segments", nil)
	}

	if file.Size <= 0 {
		return errors.NewNonRetryableError("invalid STRM: file has invalid size", nil)
	}

	return nil
}
