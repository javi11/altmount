package api

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	metapb "github.com/javi11/altmount/internal/metadata/proto"
)

// handleGetFileMetadata handles GET /files/info requests
//
//	@Summary		Get file metadata
//	@Description	Returns metadata for a mounted NZB file including segment info, encryption, and status.
//	@Tags			Files
//	@Produce		json
//	@Param			path	query		string	true	"Virtual path to the file"
//	@Success		200		{object}	APIResponse{data=FileMetadataResponse}
//	@Failure		400		{object}	APIResponse
//	@Failure		500		{object}	APIResponse
//	@Security		BearerAuth
//	@Router			/files/info [get]
func (s *Server) handleGetFileMetadata(c *fiber.Ctx) error {
	// Get path from query parameters
	path := c.Query("path")
	if path == "" {
		return RespondBadRequest(c, "Path parameter is required", "MISSING_PATH")
	}

	// Get metadata from the reader
	metadata, err := s.metadataReader.GetFileMetadata(path)
	if err != nil {
		return RespondInternalError(c, "Failed to read metadata", err.Error())
	}

	if metadata == nil {
		return RespondNotFound(c, "File metadata", "")
	}

	// Convert protobuf metadata to API response
	response := s.convertToFileMetadataResponse(metadata)
	return RespondSuccess(c, response)
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

	// Convert nested sources
	var nestedSources []NestedSourceResponse
	nestedSegmentCount := 0
	for i, ns := range metadata.NestedSources {
		segs := make([]NestedSegmentResponse, len(ns.Segments))
		for j, seg := range ns.Segments {
			segs[j] = NestedSegmentResponse{
				SegmentSize: seg.SegmentSize,
				StartOffset: seg.StartOffset,
				EndOffset:   seg.EndOffset,
				MessageID:   seg.Id,
			}
		}
		nestedSegmentCount += len(ns.Segments)
		nestedSources = append(nestedSources, NestedSourceResponse{
			VolumeIndex:     i,
			InnerLength:     ns.InnerLength,
			InnerVolumeSize: ns.InnerVolumeSize,
			Encrypted:       len(ns.AesKey) > 0,
			SegmentCount:    len(ns.Segments),
			Segments:        segs,
		})
	}

	// Total segment count includes nested source segments
	segmentCount := len(metadata.SegmentData) + nestedSegmentCount

	// Convert timestamps
	createdAt := time.Unix(metadata.CreatedAt, 0).Format(time.RFC3339)
	modifiedAt := time.Unix(metadata.ModifiedAt, 0).Format(time.RFC3339)

	return &FileMetadataResponse{
		FileSize:          metadata.FileSize,
		SourceNzbPath:     metadata.SourceNzbPath,
		Status:            statusStr,
		SegmentCount:      segmentCount,
		AvailableSegments: nil, // TODO: Implement actual available segment count
		Encryption:        encryptionStr,
		CreatedAt:         createdAt,
		ModifiedAt:        modifiedAt,
		PasswordProtected: metadata.Password != "",
		Segments:          segments,
		NestedSources:     nestedSources,
	}
}

// convertFileStatusToString converts FileStatus enum to string
func (s *Server) convertFileStatusToString(status metapb.FileStatus) string {
	switch status {
	case metapb.FileStatus_FILE_STATUS_HEALTHY:
		return "healthy"
	case metapb.FileStatus_FILE_STATUS_CORRUPTED:
		return "corrupted"
	case metapb.FileStatus_FILE_STATUS_DEGRADED:
		return "degraded"
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
//
//	@Summary		Export file metadata as NZB
//	@Description	Exports the metadata of a mounted file back to a downloadable NZB file.
//	@Tags			Files
//	@Produce		application/x-nzb
//	@Param			path	query	string	true	"Virtual path to the file"
//	@Success		200
//	@Failure		400	{object}	APIResponse
//	@Failure		500	{object}	APIResponse
//	@Security		BearerAuth
//	@Router			/files/export-nzb [get]
func (s *Server) handleExportMetadataToNZB(c *fiber.Ctx) error {
	// Get path from query parameters
	path := c.Query("path")
	if path == "" {
		return RespondBadRequest(c, "Path parameter is required", "MISSING_PATH")
	}

	// Get metadata from the reader
	metadata, err := s.metadataReader.GetFileMetadata(path)
	if err != nil {
		return RespondInternalError(c, "Failed to read metadata", err.Error())
	}

	if metadata == nil {
		return RespondNotFound(c, "File metadata", "")
	}

	// Default download name derived from the virtual file.
	filename := filepath.Base(path)
	if idx := strings.LastIndex(filename, "."); idx != -1 {
		filename = filename[:idx]
	}
	nzbFilename := filename + ".nzb"

	// Prefer the faithful original NZB regenerated from the v3 store (all original
	// files, segments, posters/groups/dates). Fall back to the synthetic single-file
	// NZB for v1 metadata or when the store is missing.
	var nzbContent []byte
	if metadata.StoreRef != "" && s.metadataService != nil {
		regen, regenErr := s.metadataService.Store().RegenerateNZB(metadata.StoreRef)
		if regenErr != nil {
			return RespondInternalError(c, "Failed to regenerate NZB from store", regenErr.Error())
		}
		if regen != nil {
			// BuildNZB emits no <head>, so re-attach the encryption/password meta
			// from the metadata; otherwise the exported NZB would lose the password.
			nzbContent = s.injectEncryptionMeta(regen, metadata)
			// Name the download after the release (the store), not the single virtual file.
			nzbFilename = strings.TrimSuffix(filepath.Base(metadata.StoreRef), ".nzbz") + ".nzb"
		}
	}

	if nzbContent == nil {
		generated, genErr := s.generateNZBFromMetadata(metadata, path)
		if genErr != nil {
			return RespondInternalError(c, "Failed to generate NZB", genErr.Error())
		}
		nzbContent = generated
	}

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

		if metadata.Salt != "" {
			nzb.Head.Meta = append(nzb.Head.Meta, nzbMeta{Type: "salt", Value: metadata.Salt})
		}
	}

	// Preserve the password whenever present (e.g. RAR-password releases), even when
	// the content is not AES-encrypted, so the exported NZB stays re-importable.
	if metadata.Password != "" {
		nzb.Head.Meta = append(nzb.Head.Meta, nzbMeta{Type: "password", Value: metadata.Password})
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

// injectEncryptionMeta attaches cipher/password/salt <meta> tags to a
// store-regenerated NZB so exported releases stay re-importable. nzb.BuildNZB
// emits no <head>, so a new one is inserted after the opening <nzb> tag. The
// password is preserved whenever present (e.g. RAR-password releases), even when
// the content is not AES-encrypted; cipher/salt are added only for AES content.
func (s *Server) injectEncryptionMeta(nzbContent []byte, metadata *metapb.FileMetadata) []byte {
	if metadata.Encryption == metapb.Encryption_NONE && metadata.Password == "" {
		return nzbContent
	}

	var head bytes.Buffer
	head.WriteString("  <head>\n")
	if metadata.Encryption != metapb.Encryption_NONE {
		head.WriteString(`    <meta type="cipher">`)
		_ = xml.EscapeText(&head, []byte(s.convertEncryptionToString(metadata.Encryption)))
		head.WriteString("</meta>\n")
	}
	if metadata.Password != "" {
		head.WriteString(`    <meta type="password">`)
		_ = xml.EscapeText(&head, []byte(metadata.Password))
		head.WriteString("</meta>\n")
	}
	if metadata.Salt != "" {
		head.WriteString(`    <meta type="salt">`)
		_ = xml.EscapeText(&head, []byte(metadata.Salt))
		head.WriteString("</meta>\n")
	}
	head.WriteString("  </head>\n")

	// ReplaceAllStringFunc inserts the head literally — unlike ReplaceAllString, it
	// does not interpret "$" in the replacement, so passwords containing "$" survive.
	re := regexp.MustCompile(`<nzb[^>]*>\n?`)
	return []byte(re.ReplaceAllStringFunc(string(nzbContent), func(m string) string {
		return m + head.String()
	}))
}

// BatchExportRequest represents the batch export request body
type BatchExportRequest struct {
	RootPath string `json:"root_path"`
}

// Archive detection patterns
var (
	rarPattern      = regexp.MustCompile(`(?i)\.(?:rar|r\d+|[r-z]\d{2})$|\.part\d+\.rar$`)
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
	return slices.Contains(archiveExts, ext)
}

// handleBatchExportNZB handles POST /files/export-batch requests
//
//	@Summary		Batch export files as NZBs
//	@Description	Exports multiple mounted files as NZBs packed in a single ZIP archive.
//	@Tags			Files
//	@Accept			json
//	@Produce		application/zip
//	@Param			body	body	object{paths=[]string}	true	"Virtual paths to export"
//	@Success		200
//	@Failure		400	{object}	APIResponse
//	@Failure		500	{object}	APIResponse
//	@Security		BearerAuth
//	@Router			/files/export-batch [post]
func (s *Server) handleBatchExportNZB(c *fiber.Ctx) error {
	ctx := context.Background()

	// Parse request body
	var req BatchExportRequest
	if err := c.BodyParser(&req); err != nil {
		return RespondBadRequest(c, "Invalid request body", err.Error())
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
		return RespondInternalError(c, "Failed to collect metadata files", err.Error())
	}

	if len(metadataFiles) == 0 {
		return RespondError(c, fiber.StatusNotFound, ErrCodeNotFound, "No metadata files found", "")
	}

	slog.InfoContext(ctx, "Collected metadata files",
		"total_count", len(metadataFiles))

	// Set ZIP headers before streaming begins.
	timestamp := time.Now().Unix()
	zipFilename := fmt.Sprintf("nzb-export-%d.zip", timestamp)
	c.Set("Content-Type", "application/zip")
	c.Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, zipFilename))

	// Stream the ZIP instead of buffering the whole archive in memory.
	c.Context().SetBodyStreamWriter(func(w *bufio.Writer) {
		zipWriter := zip.NewWriter(w)
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

		// Headers are already sent, so finalize errors can only be logged.
		if err := zipWriter.Close(); err != nil {
			slog.ErrorContext(ctx, "Failed to close ZIP writer", "error", err)
			return
		}
		if err := w.Flush(); err != nil {
			slog.WarnContext(ctx, "Failed to flush export stream", "error", err)
			return
		}

		slog.InfoContext(ctx, "Batch export completed",
			"exported_count", exportedCount,
			"skipped_count", skippedCount)
	})

	return nil
}
