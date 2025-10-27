package steps

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/javi11/altmount/internal/importer/parser"
	"github.com/javi11/altmount/internal/metadata"
)

// CalculateVirtualDirStep determines the virtual directory path
type CalculateVirtualDirStep struct {
	nzbPath      string
	relativePath string
}

// NewCalculateVirtualDirStep creates a step to calculate virtual directory
func NewCalculateVirtualDirStep(nzbPath, relativePath string) *CalculateVirtualDirStep {
	return &CalculateVirtualDirStep{
		nzbPath:      nzbPath,
		relativePath: relativePath,
	}
}

// Execute calculates the virtual directory path
func (s *CalculateVirtualDirStep) Execute(ctx context.Context, pctx *ProcessingContext) error {
	pctx.VirtualDir = CalculateVirtualDirectory(s.nzbPath, s.relativePath)
	return nil
}

// Name returns the step name
func (s *CalculateVirtualDirStep) Name() string {
	return "CalculateVirtualDir"
}

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

// EnsureDirectoryStep ensures a directory exists in the metadata filesystem
type EnsureDirectoryStep struct {
	metadataService *metadata.MetadataService
}

// NewEnsureDirectoryStep creates a step to ensure directory exists
func NewEnsureDirectoryStep(metadataService *metadata.MetadataService) *EnsureDirectoryStep {
	return &EnsureDirectoryStep{metadataService: metadataService}
}

// Execute ensures the directory exists
func (s *EnsureDirectoryStep) Execute(ctx context.Context, pctx *ProcessingContext) error {
	return EnsureDirectoryExists(pctx.VirtualDir, s.metadataService)
}

// Name returns the step name
func (s *EnsureDirectoryStep) Name() string {
	return "EnsureDirectory"
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

// CreateNzbFolderStep creates a folder named after the NZB file
type CreateNzbFolderStep struct {
	metadataService *metadata.MetadataService
}

// NewCreateNzbFolderStep creates a step to create NZB folder
func NewCreateNzbFolderStep(metadataService *metadata.MetadataService) *CreateNzbFolderStep {
	return &CreateNzbFolderStep{metadataService: metadataService}
}

// Execute creates the NZB folder and updates context
func (s *CreateNzbFolderStep) Execute(ctx context.Context, pctx *ProcessingContext) error {
	nzbBaseName := strings.TrimSuffix(pctx.Parsed.Filename, filepath.Ext(pctx.Parsed.Filename))
	nzbVirtualDir := filepath.Join(pctx.VirtualDir, nzbBaseName)
	nzbVirtualDir = strings.ReplaceAll(nzbVirtualDir, string(filepath.Separator), "/")

	// Update context with new virtual directory
	pctx.VirtualDir = nzbVirtualDir

	// Ensure directory exists
	return EnsureDirectoryExists(nzbVirtualDir, s.metadataService)
}

// Name returns the step name
func (s *CreateNzbFolderStep) Name() string {
	return "CreateNzbFolder"
}

// AnalyzeDirectoryStructureStep analyzes files to determine directory structure
type AnalyzeDirectoryStructureStep struct {
	metadataService *metadata.MetadataService
}

// NewAnalyzeDirectoryStructureStep creates a step to analyze directory structure
func NewAnalyzeDirectoryStructureStep(metadataService *metadata.MetadataService) *AnalyzeDirectoryStructureStep {
	return &AnalyzeDirectoryStructureStep{metadataService: metadataService}
}

// Execute analyzes directory structure from files
func (s *AnalyzeDirectoryStructureStep) Execute(ctx context.Context, pctx *ProcessingContext) error {
	if len(pctx.RegularFiles) == 0 {
		return nil
	}

	pctx.DirectoryStructure = AnalyzeDirectoryStructureWithBase(pctx.RegularFiles, pctx.VirtualDir)
	return nil
}

// Name returns the step name
func (s *AnalyzeDirectoryStructureStep) Name() string {
	return "AnalyzeDirectoryStructure"
}

// AnalyzeDirectoryStructureWithBase analyzes files to determine directory structure within a base directory
func AnalyzeDirectoryStructureWithBase(files []parser.ParsedFile, baseDir string) *DirectoryStructure {
	pathMap := make(map[string]bool)

	for _, file := range files {
		normalizedFilename := strings.ReplaceAll(file.Filename, "\\", "/")
		dir := filepath.Dir(normalizedFilename)
		if dir != "." && dir != "/" {
			virtualPath := filepath.Join(baseDir, dir)
			virtualPath = strings.ReplaceAll(virtualPath, string(filepath.Separator), "/")
			pathMap[virtualPath] = true
		}
	}

	var dirs []DirectoryInfo
	for path := range pathMap {
		parent := filepath.Dir(path)
		if parent == "." || parent == "/" {
			parent = baseDir
		}

		dirs = append(dirs, DirectoryInfo{
			Path:   path,
			Name:   filepath.Base(path),
			Parent: stringPtr(parent),
		})
	}

	return &DirectoryStructure{
		Directories: dirs,
		CommonRoot:  baseDir,
	}
}

// CreateDirectoriesStep creates all directories in the analyzed structure
type CreateDirectoriesStep struct {
	metadataService *metadata.MetadataService
}

// NewCreateDirectoriesStep creates a step to create directories
func NewCreateDirectoriesStep(metadataService *metadata.MetadataService) *CreateDirectoriesStep {
	return &CreateDirectoriesStep{metadataService: metadataService}
}

// Execute creates all directories
func (s *CreateDirectoriesStep) Execute(ctx context.Context, pctx *ProcessingContext) error {
	if pctx.DirectoryStructure == nil {
		return nil
	}

	for _, dir := range pctx.DirectoryStructure.Directories {
		if err := EnsureDirectoryExists(dir.Path, s.metadataService); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir.Path, err)
		}
	}

	return nil
}

// Name returns the step name
func (s *CreateDirectoriesStep) Name() string {
	return "CreateDirectories"
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

// Helper function to create string pointer
func stringPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
