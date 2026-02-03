package filesystem

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/javi11/altmount/internal/importer/parser"
	"github.com/javi11/altmount/internal/metadata"
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
		// If the file is at the root, return root
		// The processor will handle creating a folder if needed (e.g. for archives or multi-file NZBs)
		return "/"
	}

	// Ignore .nzbs folder if present (persistent storage)
	if strings.Contains(relDir, ".nzbs") {
		parts := strings.Split(relDir, string(filepath.Separator))
		filtered := make([]string, 0, len(parts))
		for _, p := range parts {
			if p != ".nzbs" {
				filtered = append(filtered, p)
			}
		}
		relDir = filepath.Join(filtered...)
	}

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
	// Now, also strip the media file extension if it exists
	// Common media extensions: .mkv, .mp4, .avi, .flv, .wmv, .mov, .webm
	// This is not exhaustive, but covers common cases.
	mediaExtensions := []string{".mkv", ".mp4", ".avi", ".flv", ".wmv", ".mov", ".webm", ".ts", ".iso"}

	for _, ext := range mediaExtensions {
		if strings.HasSuffix(strings.ToLower(nzbBaseName), ext) {
			nzbBaseName = strings.TrimSuffix(nzbBaseName, ext)
			break
		}
	}

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
		normalizedFilename = filepath.Clean(normalizedFilename)
		normalizedFilename = strings.TrimPrefix(normalizedFilename, "/")

		dir := filepath.Dir(normalizedFilename)
		name := filepath.Base(normalizedFilename)

		// Check for redundant nesting (e.g. file.mkv/file.mkv)
		// If the last directory component matches the filename, flatten the structure
		// Also check without extension for cases like Movie/Movie.mkv
		nameWithoutExt := strings.TrimSuffix(name, filepath.Ext(name))
		if filepath.Base(dir) == name || filepath.Base(dir) == nameWithoutExt {
			dir = filepath.Dir(dir)
		}

		// Flatten redundant nesting against parent directory (same-level duplicate names)
		parentDirName := filepath.Base(virtualDir)
		if dir == parentDirName {
			dir = "."
		} else if strings.HasPrefix(dir, parentDirName+"/") {
			dir = strings.TrimPrefix(dir, parentDirName+"/")
		}

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
	normalizedFilename = filepath.Clean(normalizedFilename)
	normalizedFilename = strings.TrimPrefix(normalizedFilename, "/")

	dir := filepath.Dir(normalizedFilename)
	name := filepath.Base(normalizedFilename)

	// Check for redundant nesting (e.g. file.mkv/file.mkv)
	// If the last directory component matches the filename, flatten the structure
	// Also check without extension for cases like Movie/Movie.mkv
	nameWithoutExt := strings.TrimSuffix(name, filepath.Ext(name))
	if filepath.Base(dir) == name || filepath.Base(dir) == nameWithoutExt {
		dir = filepath.Dir(dir)
	}

	// Flatten redundant nesting against parent directory (same-level duplicate names)
	parentDirName := filepath.Base(baseDir)
	if dir == parentDirName {
		dir = "."
	} else if strings.HasPrefix(dir, parentDirName+"/") {
		dir = strings.TrimPrefix(dir, parentDirName+"/")
	}

	if dir == "." || dir == "/" {
		return baseDir, name
	}

	virtualPath := filepath.Join(baseDir, dir)
	virtualPath = strings.ReplaceAll(virtualPath, string(filepath.Separator), "/")
	return virtualPath, name
}
