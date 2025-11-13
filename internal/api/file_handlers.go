package api

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	metapb "github.com/javi11/altmount/internal/metadata/proto"
)

// handleGetFileMetadata handles GET /files/info requests
func (s *Server) handleGetFileMetadata(c *fiber.Ctx) error {
	// Get path from query parameters
	path := c.Query("path")
	if path == "" {
		return c.Status(400).JSON(fiber.Map{
			"success": false,
			"message": "Path parameter is required",
			"details": "MISSING_PATH",
		})
	}

	// Get metadata from the reader
	metadata, err := s.metadataReader.GetFileMetadata(path)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Failed to read metadata",
			"details": err.Error(),
		})
	}

	if metadata == nil {
		return c.Status(404).JSON(fiber.Map{
			"success": false,
			"message": "File metadata not found",
		})
	}

	// Convert protobuf metadata to API response
	response := s.convertToFileMetadataResponse(metadata)
	return c.Status(200).JSON(fiber.Map{
		"success": true,
		"data":    response,
	})
}

// convertToFileMetadataResponse converts protobuf FileMetadata to API response
func (s *Server) convertToFileMetadataResponse(metadata *metapb.FileMetadata) *FileMetadataResponse {
	// Convert status enum to string
	statusStr := s.convertFileStatusToString(metadata.Status)

	// Convert encryption enum to string
	encryptionStr := s.convertEncryptionToString(metadata.Encryption)

	// Convert segments
	segments := make([]SegmentInfoResponse, len(metadata.SegmentData))
	for i, segment := range metadata.SegmentData {
		segments[i] = SegmentInfoResponse{
			SegmentSize: segment.SegmentSize,
			StartOffset: segment.StartOffset,
			EndOffset:   segment.EndOffset,
			MessageID:   segment.Id,
			Available:   true, // TODO: Implement actual availability check
		}
	}

	// Convert timestamps
	createdAt := time.Unix(metadata.CreatedAt, 0).Format(time.RFC3339)
	modifiedAt := time.Unix(metadata.ModifiedAt, 0).Format(time.RFC3339)

	return &FileMetadataResponse{
		FileSize:          metadata.FileSize,
		SourceNzbPath:     metadata.SourceNzbPath,
		Status:            statusStr,
		SegmentCount:      len(metadata.SegmentData),
		AvailableSegments: nil, // TODO: Implement actual available segment count
		Encryption:        encryptionStr,
		CreatedAt:         createdAt,
		ModifiedAt:        modifiedAt,
		PasswordProtected: metadata.Password != "",
		Segments:          segments,
	}
}

// convertFileStatusToString converts FileStatus enum to string
func (s *Server) convertFileStatusToString(status metapb.FileStatus) string {
	switch status {
	case metapb.FileStatus_FILE_STATUS_HEALTHY:
		return "healthy"
	case metapb.FileStatus_FILE_STATUS_CORRUPTED:
		return "corrupted"
	default:
		return "unspecified"
	}
}

// convertEncryptionToString converts Encryption enum to string
func (s *Server) convertEncryptionToString(encryption metapb.Encryption) string {
	switch encryption {
	case metapb.Encryption_RCLONE:
		return "rclone"
	case metapb.Encryption_HEADERS:
		return "headers"
	default:
		return "none"
	}
}

// NZB XML structures for export
type nzbFile struct {
	XMLName  xml.Name     `xml:"file"`
	Poster   string       `xml:"poster,attr"`
	Date     string       `xml:"date,attr"`
	Subject  string       `xml:"subject,attr"`
	Groups   nzbGroups    `xml:"groups"`
	Segments []nzbSegment `xml:"segments>segment"`
}

type nzbGroups struct {
	Groups []string `xml:"group"`
}

type nzbSegment struct {
	Bytes  int64  `xml:"bytes,attr"`
	Number int    `xml:"number,attr"`
	ID     string `xml:",chardata"`
}

type nzbMeta struct {
	Type  string `xml:"type,attr"`
	Value string `xml:",chardata"`
}

type nzbRoot struct {
	XMLName xml.Name  `xml:"nzb"`
	Xmlns   string    `xml:"xmlns,attr"`
	Head    nzbHead   `xml:"head"`
	Files   []nzbFile `xml:"file"`
}

type nzbHead struct {
	Meta []nzbMeta `xml:"meta"`
}

// handleExportMetadataToNZB handles GET /files/export-nzb requests
func (s *Server) handleExportMetadataToNZB(c *fiber.Ctx) error {
	// Get path from query parameters
	path := c.Query("path")
	if path == "" {
		return c.Status(400).JSON(fiber.Map{
			"success": false,
			"message": "Path parameter is required",
			"details": "MISSING_PATH",
		})
	}

	// Get metadata from the reader
	metadata, err := s.metadataReader.GetFileMetadata(path)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Failed to read metadata",
			"details": err.Error(),
		})
	}

	if metadata == nil {
		return c.Status(404).JSON(fiber.Map{
			"success": false,
			"message": "File metadata not found",
		})
	}

	// Generate NZB from metadata
	nzbContent, err := s.generateNZBFromMetadata(metadata, path)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Failed to generate NZB",
			"details": err.Error(),
		})
	}

	// Extract filename from path
	filename := filepath.Base(path)
	// Remove any existing extension and add .nzb
	if idx := strings.LastIndex(filename, "."); idx != -1 {
		filename = filename[:idx]
	}
	nzbFilename := filename + ".nzb"

	// Set response headers for file download
	c.Set("Content-Type", "application/x-nzb")
	c.Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, nzbFilename))

	return c.Send(nzbContent)
}

// generateNZBFromMetadata creates an NZB file from metadata
func (s *Server) generateNZBFromMetadata(metadata *metapb.FileMetadata, filePath string) ([]byte, error) {
	// Create NZB structure
	nzb := nzbRoot{
		Xmlns: "http://www.newzbin.com/DTD/2003/nzb",
		Head: nzbHead{
			Meta: []nzbMeta{},
		},
		Files: []nzbFile{},
	}

	// Add metadata to head
	filename := filepath.Base(filePath)
	nzb.Head.Meta = append(nzb.Head.Meta, nzbMeta{Type: "file_name", Value: filename})
	nzb.Head.Meta = append(nzb.Head.Meta, nzbMeta{Type: "file_size", Value: fmt.Sprintf("%d", metadata.FileSize)})

	// Add file extension
	if ext := filepath.Ext(filename); ext != "" {
		nzb.Head.Meta = append(nzb.Head.Meta, nzbMeta{Type: "file_extension", Value: ext})
	}

	// Add encryption info if present
	if metadata.Encryption != metapb.Encryption_NONE {
		encType := s.convertEncryptionToString(metadata.Encryption)
		nzb.Head.Meta = append(nzb.Head.Meta, nzbMeta{Type: "cipher", Value: encType})

		if metadata.Password != "" {
			nzb.Head.Meta = append(nzb.Head.Meta, nzbMeta{Type: "password", Value: metadata.Password})
		}

		if metadata.Salt != "" {
			nzb.Head.Meta = append(nzb.Head.Meta, nzbMeta{Type: "salt", Value: metadata.Salt})
		}
	}

	// Create a single file entry with all segments
	file := nzbFile{
		Poster:  "altmount@export",
		Date:    fmt.Sprintf("%d", time.Now().UnixMilli()),
		Subject: filename,
		Groups: nzbGroups{
			Groups: []string{"alt.binaries.misc"},
		},
		Segments: []nzbSegment{},
	}

	// Add segments
	for i, segment := range metadata.SegmentData {
		file.Segments = append(file.Segments, nzbSegment{
			Bytes:  segment.SegmentSize,
			Number: i + 1,
			ID:     segment.Id,
		})
	}

	nzb.Files = append(nzb.Files, file)

	// Marshal to XML with proper header
	var buf bytes.Buffer
	buf.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	buf.WriteString("\n")
	buf.WriteString(`<!DOCTYPE nzb PUBLIC "-//newzBin//DTD NZB 1.1//EN" "http://www.newzbin.com/DTD/nzb/nzb-1.1.dtd">`)
	buf.WriteString("\n")

	encoder := xml.NewEncoder(&buf)
	encoder.Indent("", "  ")
	if err := encoder.Encode(nzb); err != nil {
		return nil, fmt.Errorf("failed to encode NZB: %w", err)
	}

	return buf.Bytes(), nil
}
