package nzb

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"

	"github.com/javi11/altmount/internal/database"
	"github.com/nwaples/rardecode/v2"
)

// RarHandler handles RAR archive analysis and content extraction
type RarHandler struct {
	log *slog.Logger
}

// NewRarHandler creates a new RAR handler
func NewRarHandler() *RarHandler {
	return &RarHandler{
		log: slog.Default().With("component", "rar-handler"),
	}
}

// AnalyzeRarContent analyzes a RAR archive to extract its internal file structure
// This implementation uses the rardecode library to parse RAR headers and extract file lists
func (rh *RarHandler) AnalyzeRarContent(rarData []byte, virtualFile *database.VirtualFile, nzbFile *database.NzbFile) ([]database.RarContent, error) {
	if !rh.IsValidRarArchive(rarData) {
		return nil, fmt.Errorf("invalid RAR archive data")
	}

	// Extract file list from RAR headers
	fileEntries, err := rh.ExtractRarFileList(rarData)
	if err != nil {
		return nil, fmt.Errorf("failed to extract RAR file list: %w", err)
	}

	// Convert to database format
	rarContents := make([]database.RarContent, 0, len(fileEntries))
	for _, entry := range fileEntries {
		// Skip directories for now - focus on files
		if entry.IsDirectory {
			continue
		}

		var crc32Ptr *string
		if entry.CRC32 != "" {
			crc32Ptr = &entry.CRC32
		}

		rarContent := database.RarContent{
			VirtualFileID:  virtualFile.ID,
			InternalPath:   entry.Path,
			Filename:       entry.Filename,
			Size:           entry.Size,
			CompressedSize: entry.CompressedSize,
			CRC32:          crc32Ptr,
		}
		rarContents = append(rarContents, rarContent)
	}

	rh.log.Info("Successfully analyzed RAR content",
		"archive", virtualFile.VirtualPath,
		"files_found", len(rarContents),
	)

	return rarContents, nil
}

// ExtractRarFileList extracts the file list from RAR headers without full decompression
func (rh *RarHandler) ExtractRarFileList(rarData []byte) ([]RarFileEntry, error) {
	reader := bytes.NewReader(rarData)
	rarReader, err := rardecode.NewReader(reader, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create RAR reader: %w", err)
	}

	var entries []RarFileEntry
	for {
		header, err := rarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read RAR header: %w", err)
		}

		// Extract file information from header
		entry := RarFileEntry{
			Path:           filepath.Clean(header.Name),
			Filename:       filepath.Base(header.Name),
			Size:           header.UnPackedSize,
			CompressedSize: header.PackedSize,
			CRC32:          "", // CRC not available in rardecode v2
			IsDirectory:    header.IsDir,
			ModTime:        header.ModificationTime,
			Attributes:     header.HostOS,
		}

		entries = append(entries, entry)
		rh.log.Debug("Found RAR entry",
			"path", entry.Path,
			"size", entry.Size,
			"compressed_size", entry.CompressedSize,
			"is_dir", entry.IsDirectory,
		)
	}

	return entries, nil
}

// IsValidRarArchive checks if the given data represents a valid RAR archive
func (rh *RarHandler) IsValidRarArchive(data []byte) bool {
	// Check for RAR signature
	if len(data) < 7 {
		return false
	}

	// RAR signature: "Rar!" followed by 0x1A 0x07 0x00 or 0x1A 0x07 0x01
	rarSignature := []byte{0x52, 0x61, 0x72, 0x21, 0x1A, 0x07}

	for i, b := range rarSignature {
		if data[i] != b {
			return false
		}
	}

	return true
}

// GetRarVersion determines the RAR archive version
func (rh *RarHandler) GetRarVersion(data []byte) (int, error) {
	if !rh.IsValidRarArchive(data) {
		return 0, fmt.Errorf("not a valid RAR archive")
	}

	if len(data) < 7 {
		return 0, fmt.Errorf("insufficient data to determine RAR version")
	}

	// RAR version is in the 7th byte
	version := int(data[6])
	return version, nil
}
