package api

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
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

	// Get filename for file entry (not for metadata)
	filename := filepath.Base(filePath)

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

	// Add PAR2 files if present
	for _, par2File := range metadata.Par2Files {
		par2FileEntry := nzbFile{
			Poster:  "altmount@export",
			Date:    fmt.Sprintf("%d", time.Now().UnixMilli()),
			Subject: par2File.Filename,
			Groups: nzbGroups{
				Groups: []string{"alt.binaries.misc"},
			},
			Segments: []nzbSegment{},
		}

		// Add PAR2 file segments
		for i, segment := range par2File.SegmentData {
			par2FileEntry.Segments = append(par2FileEntry.Segments, nzbSegment{
				Bytes:  segment.SegmentSize,
				Number: i + 1,
				ID:     segment.Id,
			})
		}

		nzb.Files = append(nzb.Files, par2FileEntry)
	}

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

// BatchExportRequest represents the batch export request body
type BatchExportRequest struct {
	RootPath string `json:"root_path"`
}

// Archive detection patterns
var (
	rarPattern      = regexp.MustCompile(`(?i)\.r(ar|\d+)$|\.part\d+\.rar$`)
	sevenZipPattern = regexp.MustCompile(`(?i)\.7z$|\.7z\.\d+$`)
	archiveExts     = []string{".rar", ".7z", ".zip", ".tar", ".gz", ".bz2", ".arj", ".arc"}
)

// shouldExcludeFile determines if a file should be excluded based on archive patterns
func shouldExcludeFile(filename string, excludeArchives bool) bool {
	if !excludeArchives {
		return false
	}

	// Check for RAR pattern (includes multi-part like .r00, .r01, etc.)
	if rarPattern.MatchString(filename) {
		return true
	}

	// Check for 7zip pattern
	if sevenZipPattern.MatchString(filename) {
		return true
	}

	// Check for common archive extensions
	ext := strings.ToLower(filepath.Ext(filename))
	for _, archiveExt := range archiveExts {
		if ext == archiveExt {
			return true
		}
	}

	return false
}

// handleBatchExportNZB handles POST /files/export-batch requests
func (s *Server) handleBatchExportNZB(c *fiber.Ctx) error {
	ctx := context.Background()

	// Parse request body
	var req BatchExportRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{
			"success": false,
			"message": "Invalid request body",
			"details": err.Error(),
		})
	}

	slog.InfoContext(ctx, "Batch NZB export requested")

	// Always use "/" as the virtual path to export from the metadata root
	// The req.RootPath is not needed since we export all metadata files
	virtualRootPath := "/"

	// Get metadata root directory
	metadataRootPath := s.metadataReader.GetMetadataService().GetMetadataDirectoryPath(virtualRootPath)

	// Collect all metadata files
	var metadataFiles []string
	err := filepath.Walk(metadataRootPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories
		if info.IsDir() {
			return nil
		}

		// Only process .meta files
		if filepath.Ext(path) != ".meta" {
			return nil
		}

		metadataFiles = append(metadataFiles, path)
		return nil
	})

	if err != nil {
		slog.ErrorContext(ctx, "Failed to walk metadata directory",
			"error", err,
			"path", metadataRootPath)
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Failed to collect metadata files",
			"details": err.Error(),
		})
	}

	if len(metadataFiles) == 0 {
		return c.Status(404).JSON(fiber.Map{
			"success": false,
			"message": "No metadata files found",
		})
	}

	slog.InfoContext(ctx, "Collected metadata files",
		"total_count", len(metadataFiles))

	// Create ZIP archive in memory
	var zipBuffer bytes.Buffer
	zipWriter := zip.NewWriter(&zipBuffer)

	exportedCount := 0
	skippedCount := 0

	for _, metaPath := range metadataFiles {
		// Calculate virtual path
		relPath, err := filepath.Rel(metadataRootPath, metaPath)
		if err != nil {
			slog.WarnContext(ctx, "Failed to calculate relative path",
				"error", err,
				"meta_path", metaPath)
			continue
		}

		// Remove .meta extension to get virtual filename
		virtualFilename := strings.TrimSuffix(relPath, ".meta")

		// Check if should exclude based on archive pattern
		// Archives and AES-encrypted files are always excluded
		if shouldExcludeFile(virtualFilename, true) {
			skippedCount++
			slog.DebugContext(ctx, "Skipping archive file",
				"filename", virtualFilename)
			continue
		}

		// Calculate full virtual path
		virtualPath := filepath.Join(virtualRootPath, virtualFilename)
		virtualPath = strings.ReplaceAll(virtualPath, string(filepath.Separator), "/")

		// Read metadata
		metadata, err := s.metadataReader.GetFileMetadata(virtualPath)
		if err != nil {
			slog.WarnContext(ctx, "Failed to read metadata",
				"error", err,
				"virtual_path", virtualPath)
			continue
		}

		if metadata == nil {
			slog.WarnContext(ctx, "Metadata not found",
				"virtual_path", virtualPath)
			continue
		}

		// Skip AES-encrypted files (encrypted archives)
		if metadata.Encryption == metapb.Encryption_AES {
			skippedCount++
			continue
		}

		// Generate NZB
		nzbContent, err := s.generateNZBFromMetadata(metadata, virtualPath)
		if err != nil {
			slog.WarnContext(ctx, "Failed to generate NZB",
				"error", err,
				"virtual_path", virtualPath)
			continue
		}

		// Create NZB filename
		nzbFilename := strings.TrimSuffix(virtualFilename, filepath.Ext(virtualFilename)) + ".nzb"

		// Add to ZIP
		writer, err := zipWriter.Create(nzbFilename)
		if err != nil {
			slog.WarnContext(ctx, "Failed to create ZIP entry",
				"error", err,
				"filename", nzbFilename)
			continue
		}

		if _, err := io.Copy(writer, bytes.NewReader(nzbContent)); err != nil {
			slog.WarnContext(ctx, "Failed to write NZB to ZIP",
				"error", err,
				"filename", nzbFilename)
			continue
		}

		exportedCount++
	}

	// Close ZIP writer
	if err := zipWriter.Close(); err != nil {
		slog.ErrorContext(ctx, "Failed to close ZIP writer",
			"error", err)
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Failed to finalize ZIP archive",
			"details": err.Error(),
		})
	}

	if exportedCount == 0 {
		return c.Status(404).JSON(fiber.Map{
			"success": false,
			"message": "No files were exported",
			"details": fmt.Sprintf("Skipped %d files (archives or AES-encrypted)", skippedCount),
		})
	}

	slog.InfoContext(ctx, "Batch export completed",
		"exported_count", exportedCount,
		"skipped_count", skippedCount)

	// Set response headers for ZIP download
	timestamp := time.Now().Unix()
	zipFilename := fmt.Sprintf("nzb-export-%d.zip", timestamp)

	c.Set("Content-Type", "application/zip")
	c.Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, zipFilename))

	return c.Send(zipBuffer.Bytes())
}
