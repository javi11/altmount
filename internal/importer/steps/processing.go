package steps

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/javi11/altmount/internal/importer/parser"
	"github.com/javi11/altmount/internal/metadata"
	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/javi11/altmount/internal/pool"
)

// CalculateVirtualDirectory determines the virtual directory path based on NZB file location
func CalculateVirtualDirectory(nzbPath, relativePath string) string {
	if relativePath == "" {
		return "/"
	}

	nzbPath = filepath.Clean(nzbPath)
	relativePath = filepath.Clean(relativePath)

	relPath, err := filepath.Rel(relativePath, nzbPath)
	if err != nil || strings.HasPrefix(relPath, "..") {
		if strings.HasPrefix(relativePath, "/") {
			return filepath.Clean(relativePath)
		}
		return "/" + strings.ReplaceAll(relativePath, string(filepath.Separator), "/")
	}

	relDir := filepath.Dir(relPath)
	if relDir == "." || relDir == "" {
		return "/"
	}

	virtualPath := "/" + strings.ReplaceAll(relDir, string(filepath.Separator), "/")
	return filepath.Clean(virtualPath)
}

// SeparateFiles separates files by type (regular, archive, PAR2) based on NZB type
func SeparateFiles(files []parser.ParsedFile, nzbType parser.NzbType) (regular, archive, par2 []parser.ParsedFile) {
	switch nzbType {
	case parser.NzbTypeRarArchive:
		for _, file := range files {
			if file.IsRarArchive {
				archive = append(archive, file)
			} else if IsPar2File(file.Filename) {
				par2 = append(par2, file)
			} else {
				regular = append(regular, file)
			}
		}

	case parser.NzbType7zArchive:
		for _, file := range files {
			if file.Is7zArchive {
				archive = append(archive, file)
			} else if IsPar2File(file.Filename) {
				par2 = append(par2, file)
			} else {
				regular = append(regular, file)
			}
		}

	default:
		// For single file and multi-file types, just separate PAR2 files
		for _, file := range files {
			if IsPar2File(file.Filename) {
				par2 = append(par2, file)
			} else {
				regular = append(regular, file)
			}
		}
	}

	return regular, archive, par2
}

// IsPar2File checks if a filename is a PAR2 repair file
func IsPar2File(filename string) bool {
	lower := strings.ToLower(filename)
	return strings.HasSuffix(lower, ".par2")
}

// EnsureDirectoryExists creates directory structure in the metadata filesystem
func EnsureDirectoryExists(virtualDir string, metadataService *metadata.MetadataService) error {
	if virtualDir == "/" {
		return nil
	}

	metadataDir := metadataService.GetMetadataDirectoryPath(virtualDir)
	if err := os.MkdirAll(metadataDir, 0755); err != nil {
		return fmt.Errorf("failed to create metadata directory %s: %w", metadataDir, err)
	}

	return nil
}

// CreateNzbFolder creates a folder named after the NZB file
func CreateNzbFolder(virtualDir, nzbFilename string, metadataService *metadata.MetadataService) (string, error) {
	nzbBaseName := strings.TrimSuffix(nzbFilename, filepath.Ext(nzbFilename))
	nzbVirtualDir := filepath.Join(virtualDir, nzbBaseName)
	nzbVirtualDir = strings.ReplaceAll(nzbVirtualDir, string(filepath.Separator), "/")

	if err := EnsureDirectoryExists(nzbVirtualDir, metadataService); err != nil {
		return "", err
	}

	return nzbVirtualDir, nil
}

// CreateDirectoriesForFiles analyzes files and creates their parent directories
func CreateDirectoriesForFiles(virtualDir string, files []parser.ParsedFile, metadataService *metadata.MetadataService) error {
	// Collect unique directory paths
	dirs := make(map[string]bool)

	for _, file := range files {
		normalizedFilename := strings.ReplaceAll(file.Filename, "\\", "/")
		dir := filepath.Dir(normalizedFilename)
		if dir != "." && dir != "/" {
			virtualPath := filepath.Join(virtualDir, dir)
			virtualPath = strings.ReplaceAll(virtualPath, string(filepath.Separator), "/")
			dirs[virtualPath] = true
		}
	}

	// Create all directories
	for dir := range dirs {
		if err := EnsureDirectoryExists(dir, metadataService); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	return nil
}

// DetermineFileLocation determines where a file should be placed in the virtual structure
func DetermineFileLocation(file parser.ParsedFile, baseDir string) (parentPath, filename string) {
	normalizedFilename := strings.ReplaceAll(file.Filename, "\\", "/")
	dir := filepath.Dir(normalizedFilename)
	name := filepath.Base(normalizedFilename)

	if dir == "." || dir == "/" {
		return baseDir, name
	}

	virtualPath := filepath.Join(baseDir, dir)
	virtualPath = strings.ReplaceAll(virtualPath, string(filepath.Separator), "/")
	return virtualPath, name
}

// ProcessSingleFile processes a single file (creates and writes metadata)
func ProcessSingleFile(
	ctx context.Context,
	virtualDir string,
	file parser.ParsedFile,
	nzbPath string,
	metadataService *metadata.MetadataService,
	poolManager pool.Manager,
	maxValidationGoroutines int,
	fullSegmentValidation bool,
	log *slog.Logger,
) (string, error) {
	// Create virtual file path
	virtualFilePath := filepath.Join(virtualDir, file.Filename)
	virtualFilePath = strings.ReplaceAll(virtualFilePath, string(filepath.Separator), "/")

	// Validate segments
	if err := ValidateSegmentsForFile(
		ctx,
		file.Filename,
		file.Size,
		file.Segments,
		file.Encryption,
		poolManager,
		maxValidationGoroutines,
		fullSegmentValidation,
	); err != nil {
		return "", err
	}

	// Create file metadata
	fileMeta := metadataService.CreateFileMetadata(
		file.Size,
		nzbPath,
		metapb.FileStatus_FILE_STATUS_HEALTHY,
		file.Segments,
		file.Encryption,
		file.Password,
		file.Salt,
		file.ReleaseDate.Unix(),
	)

	// Delete old metadata if exists (simple collision handling)
	metadataPath := metadataService.GetMetadataFilePath(virtualFilePath)
	if _, err := os.Stat(metadataPath); err == nil {
		_ = metadataService.DeleteFileMetadata(virtualFilePath)
	}

	// Write file metadata to disk
	if err := metadataService.WriteFileMetadata(virtualFilePath, fileMeta); err != nil {
		return "", fmt.Errorf("failed to write metadata for single file %s: %w", file.Filename, err)
	}

	log.Info("Successfully processed single file",
		"file", file.Filename,
		"virtual_path", virtualFilePath,
		"size", file.Size)

	return virtualFilePath, nil
}

// ProcessRegularFiles processes multiple regular files
func ProcessRegularFiles(
	ctx context.Context,
	virtualDir string,
	files []parser.ParsedFile,
	nzbPath string,
	metadataService *metadata.MetadataService,
	poolManager pool.Manager,
	maxValidationGoroutines int,
	fullSegmentValidation bool,
	log *slog.Logger,
) error {
	if len(files) == 0 {
		return nil
	}

	for _, file := range files {
		parentPath, filename := DetermineFileLocation(file, virtualDir)

		// Ensure parent directory exists
		if err := EnsureDirectoryExists(parentPath, metadataService); err != nil {
			return fmt.Errorf("failed to create parent directory %s: %w", parentPath, err)
		}

		// Create virtual file path
		virtualPath := filepath.Join(parentPath, filename)
		virtualPath = strings.ReplaceAll(virtualPath, string(filepath.Separator), "/")

		// Validate segments
		if err := ValidateSegmentsForFile(
			ctx,
			filename,
			file.Size,
			file.Segments,
			file.Encryption,
			poolManager,
			maxValidationGoroutines,
			fullSegmentValidation,
		); err != nil {
			return err
		}

		// Create file metadata
		fileMeta := metadataService.CreateFileMetadata(
			file.Size,
			nzbPath,
			metapb.FileStatus_FILE_STATUS_HEALTHY,
			file.Segments,
			file.Encryption,
			file.Password,
			file.Salt,
			file.ReleaseDate.Unix(),
		)

		// Delete old metadata if exists (simple collision handling)
		metadataPath := metadataService.GetMetadataFilePath(virtualPath)
		if _, err := os.Stat(metadataPath); err == nil {
			_ = metadataService.DeleteFileMetadata(virtualPath)
		}

		// Write file metadata to disk
		if err := metadataService.WriteFileMetadata(virtualPath, fileMeta); err != nil {
			return fmt.Errorf("failed to write metadata for file %s: %w", filename, err)
		}

		log.Debug("Created metadata file",
			"file", filename,
			"virtual_path", virtualPath,
			"size", file.Size)
	}

	log.Info("Successfully processed regular files",
		"virtual_dir", virtualDir,
		"files", len(files))

	return nil
}
