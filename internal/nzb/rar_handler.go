package nzb

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/nntppool"
	"github.com/nwaples/rardecode/v2"
)

// RarHandler handles RAR archive analysis and content extraction
type RarHandler struct {
	log        *slog.Logger
	cp         nntppool.UsenetConnectionPool
	maxWorkers int
}

// NewRarHandler creates a new RAR handler
func NewRarHandler(cp nntppool.UsenetConnectionPool, maxWorkers int) *RarHandler {
	return &RarHandler{
		log:        slog.Default().With("component", "rar-handler"),
		cp:         cp,
		maxWorkers: maxWorkers,
	}
}

// AnalyzeRarContentFromNzb analyzes a RAR archive directly from NZB data without downloading
// This implementation uses rardecode.OpenReader with a virtual filesystem to stream RAR data from Usenet
// The virtualFile parameter is the directory that will contain the extracted files (not the RAR file itself)
func (rh *RarHandler) AnalyzeRarContentFromNzb(ctx context.Context, nzbFile *database.NzbFile, rarFiles []ParsedFile, virtualDir *database.VirtualFile) ([]database.RarContent, error) {
	// Create Usenet filesystem for RAR access - this is the key component that enables
	// rardecode.OpenReader to read RAR parts directly from Usenet without downloading
	ufs := NewUsenetFileSystem(ctx, rh.cp, nzbFile, rarFiles, rh.maxWorkers)

	// Get sorted RAR files for proper multi-part reading
	rarFileNames := ufs.GetRarFiles()
	if len(rarFileNames) == 0 {
		return nil, fmt.Errorf("no RAR files found in the archive")
	}

	// Start with the first RAR file (usually .rar or .part001.rar)
	mainRarFile := rarFileNames[0]

	rh.log.Info("Starting RAR analysis via Usenet streaming",
		"main_file", mainRarFile,
		"total_parts", len(rarFileNames),
		"nzb_segments", len(nzbFile.SegmentsData))

	// This is where the magic happens: rardecode.OpenReader will call ufs.Open() to access
	// RAR part files, which in turn creates UsenetFile instances that stream data directly
	// from Usenet using createUsenetReader() - no local file storage required!
	rarReader, err := rardecode.OpenReader(mainRarFile, rardecode.FileSystem(ufs))
	if err != nil {
		return nil, fmt.Errorf("failed to open RAR archive %s via Usenet filesystem: %w", mainRarFile, err)
	}
	defer rarReader.Close()

	rh.log.Debug("Successfully opened RAR archive via Usenet streaming", "main_file", mainRarFile)

	// Extract file entries from the RAR archive
	// Each call to rarReader.Next() may trigger reading from different RAR parts,
	// all streamed transparently from Usenet via our virtual filesystem
	var rarContents []database.RarContent
	fileCount := 0
	for {
		header, err := rarReader.Next()
		if err == io.EOF || (err != nil &&
			(strings.Contains(err.Error(), "rardecode: bad header crc") ||
				strings.Contains(err.Error(), "RAR signature not found"))) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read RAR header from Usenet stream: %w", err)
		}

		// Skip directories - focus on files
		if header.IsDir {
			rh.log.Debug("Skipping directory entry", "path", header.Name)
			continue
		}

		fileCount++
		rh.log.Debug("Streaming RAR file header from Usenet",
			"file", header.Name,
			"size", header.UnPackedSize,
			"compressed", header.PackedSize,
			"file_num", fileCount)

		// Extract CRC32 if available - note: rardecode v2 may not expose CRC32 directly
		var crc32Ptr *string
		// CRC32 field may not be available in this version of rardecode

		// Add extended metadata from the RAR header
		var modTimePtr *time.Time
		if !header.ModificationTime.IsZero() {
			modTimePtr = &header.ModificationTime
		}

		rarContent := database.RarContent{
			VirtualFileID:  virtualDir.ID,
			InternalPath:   filepath.Clean(header.Name),
			Filename:       filepath.Base(header.Name),
			Size:           header.UnPackedSize,
			CompressedSize: header.PackedSize,
			CRC32:          crc32Ptr,
			IsDirectory:    header.IsDir,
			ModTime:        modTimePtr,
			// Note: FileOffset and RarPartIndex could be populated if needed for advanced streaming
		}

		rarContents = append(rarContents, rarContent)

		rh.log.Debug("Successfully streamed RAR file metadata from Usenet",
			"path", rarContent.InternalPath,
			"size", rarContent.Size,
			"compressed", rarContent.CompressedSize,
		)
	}

	rh.log.Info("Successfully analyzed RAR content via Usenet streaming",
		"archive_dir", virtualDir.VirtualPath,
		"files_found", len(rarContents),
		"rar_parts", len(rarFileNames),
		"total_bytes_in_rar", func() int64 {
			var total int64
			for _, rc := range rarContents {
				total += rc.Size
			}
			return total
		}(),
	)

	return rarContents, nil
}

// AnalyzeRarContent analyzes a RAR archive to extract its internal file structure (legacy method)
// This implementation uses the rardecode library to parse RAR headers and extract file lists
func (rh *RarHandler) AnalyzeRarContent(r io.Reader, virtualFile *database.VirtualFile, nzbFile *database.NzbFile) ([]database.RarContent, error) {
	// Extract file list from RAR headers
	fileEntries, err := rh.ExtractRarFileList(r)
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
func (rh *RarHandler) ExtractRarFileList(reader io.Reader) ([]RarFileEntry, error) {
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

// RarContentReadSeeker combines io.ReadCloser and io.Seeker for RAR content access
type RarContentReadSeeker interface {
	io.ReadCloser
	io.Seeker
}

// Compile-time interface check
var _ RarContentReadSeeker = (*rarContentReader)(nil)

// CreateRarContentReader creates a streaming reader with seek support for a specific file within a RAR archive
// This method demonstrates the full streaming capability: it opens the RAR archive via
// the Usenet filesystem and provides a reader/seeker for a specific file within the archive,
// all without downloading the complete RAR files to disk.
func (rh *RarHandler) CreateRarContentReader(ctx context.Context, nzbFile *database.NzbFile, rarFiles []ParsedFile, targetPath string) (RarContentReadSeeker, error) {
	// Create Usenet filesystem for RAR access - this enables rardecode.OpenReader
	// to read RAR parts directly from Usenet via createUsenetReader()
	ufs := NewUsenetFileSystem(ctx, rh.cp, nzbFile, rarFiles, rh.maxWorkers)

	// Get sorted RAR files for proper multi-part reading
	rarFileNames := ufs.GetRarFiles()
	if len(rarFileNames) == 0 {
		return nil, fmt.Errorf("no RAR files found in the archive")
	}

	// Start with the first RAR file (usually .rar or .part001.rar)
	mainRarFile := rarFileNames[0]

	rh.log.Info("Creating streaming reader for RAR content",
		"target_file", targetPath,
		"main_rar", mainRarFile,
		"total_parts", len(rarFileNames))

	// Open the RAR archive using the virtual filesystem - this is where rardecode.OpenReader
	// will call ufs.Open() to access RAR part files, which stream data from Usenet
	rarReader, err := rardecode.OpenFS(mainRarFile,
		rardecode.FileSystem(ufs),
		rardecode.SkipCheck,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to open RAR archive %s via Usenet filesystem: %w", mainRarFile, err)
	}

	f, err := rarReader.Open(targetPath)
	if err == io.EOF {
		return nil, fmt.Errorf("file not found in RAR archive: %s", targetPath)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read RAR header from Usenet stream: %w", err)
	}

	rh.log.Info("Found target file in RAR archive",
		"file", targetPath)

	return &rarContentReader{
		rarReader: f,
	}, nil
}

// rarContentReader wraps a rardecode reader for a specific file with seek support
// Implements RarContentReadSeeker interface for efficient RAR content access
type rarContentReader struct {
	rarReader fs.File
}

func (rcr *rarContentReader) Read(p []byte) (n int, err error) {
	return rcr.rarReader.Read(p)
}

func (rcr *rarContentReader) Close() error {
	if rcr.rarReader != nil {
		return rcr.rarReader.Close()
	}
	return nil
}

// Seek implements io.Seeker for efficient access within RAR content
func (rcr *rarContentReader) Seek(offset int64, whence int) (int64, error) {
	if rcr.rarReader == nil {
		return 0, fmt.Errorf("RAR reader is closed")
	}

	// rardecode.ReadCloser has a Seek method, use it directly
	if r, ok := rcr.rarReader.(io.Seeker); ok {
		n, err := r.Seek(offset, whence)
		return n, err
	}

	return 0, fmt.Errorf("RAR reader does not support seeking")
}
