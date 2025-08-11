package nzb

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/javi11/altmount/internal/database"
)

// Processor handles the processing and storage of parsed NZB files
type Processor struct {
	parser     *Parser
	repo       *database.Repository
	rarHandler *RarHandler
}

// NewProcessor creates a new NZB processor
func NewProcessor(repo *database.Repository) *Processor {
	return &Processor{
		parser:     NewParser(),
		repo:       repo,
		rarHandler: NewRarHandler(),
	}
}

// ProcessNzbFile processes an NZB file and stores it in the database
func (proc *Processor) ProcessNzbFile(nzbPath string) error {
	return proc.ProcessNzbFileWithRoot(nzbPath, "")
}

// ProcessNzbFileWithRoot processes an NZB file maintaining the folder structure relative to watchRoot
func (proc *Processor) ProcessNzbFileWithRoot(nzbPath, watchRoot string) error {
	// Check if NZB file already exists in database
	existing, err := proc.repo.GetNzbFileByPath(nzbPath)
	if err != nil {
		return fmt.Errorf("failed to check existing NZB: %w", err)
	}

	if existing != nil {
		return fmt.Errorf("NZB file already processed: %s", nzbPath)
	}

	// Open and parse the NZB file
	file, err := os.Open(nzbPath)
	if err != nil {
		return fmt.Errorf("failed to open NZB file: %w", err)
	}
	defer file.Close()

	parsed, err := proc.parser.ParseFile(file, nzbPath)
	if err != nil {
		return fmt.Errorf("failed to parse NZB file: %w", err)
	}

	// Validate the parsed NZB
	if err := proc.parser.ValidateNzb(parsed); err != nil {
		return fmt.Errorf("NZB validation failed: %w", err)
	}

	// Calculate the relative virtual directory path for this NZB
	virtualDir := proc.calculateVirtualDirectory(nzbPath, watchRoot)

	// Process within a transaction
	return proc.repo.WithTransaction(func(txRepo *database.Repository) error {
		// Ensure all parent directories exist
		if err := proc.ensureParentDirectories(txRepo, virtualDir); err != nil {
			return fmt.Errorf("failed to create parent directories: %w", err)
		}

		// Create the NZB file record
		nzbFile := &database.NzbFile{
			Path:          parsed.Path,
			Filename:      parsed.Filename,
			Size:          parsed.TotalSize,
			NzbType:       parsed.Type,
			SegmentsCount: parsed.SegmentsCount,
			SegmentsData:  proc.parser.ConvertToDbSegments(parsed.Files),
			SegmentSize:   parsed.SegmentSize,
		}

		if err := txRepo.CreateNzbFile(nzbFile); err != nil {
			return fmt.Errorf("failed to create NZB file record: %w", err)
		}

		// Process based on NZB type with virtual directory context
		switch parsed.Type {
		case database.NzbTypeSingleFile:
			return proc.processSingleFileWithDir(txRepo, nzbFile, parsed.Files[0], virtualDir)
		case database.NzbTypeMultiFile:
			return proc.processMultiFileWithDir(txRepo, nzbFile, parsed.Files, virtualDir)
		case database.NzbTypeRarArchive:
			return proc.processRarArchiveWithDir(txRepo, nzbFile, parsed.Files, virtualDir)
		default:
			return fmt.Errorf("unknown NZB type: %s", parsed.Type)
		}
	})
}

// processSingleFile handles NZBs with a single file (legacy method)
func (proc *Processor) processSingleFile(repo *database.Repository, nzbFile *database.NzbFile, file ParsedFile) error {
	return proc.processSingleFileWithDir(repo, nzbFile, file, "/")
}

// processSingleFileWithDir handles NZBs with a single file in a specific virtual directory
func (proc *Processor) processSingleFileWithDir(repo *database.Repository, nzbFile *database.NzbFile, file ParsedFile, virtualDir string) error {
	// Get parent directory
	parentDir, err := proc.getOrCreateParentDirectory(repo, virtualDir)
	if err != nil {
		return fmt.Errorf("failed to get parent directory: %w", err)
	}

	// Create virtual file entry in the specified directory
	virtualPath := filepath.Join(virtualDir, file.Filename)
	virtualPath = strings.ReplaceAll(virtualPath, string(filepath.Separator), "/")

	vf := &database.VirtualFile{
		NzbFileID:   int64Ptr(nzbFile.ID),
		ParentID:    parentDir,
		VirtualPath: virtualPath,
		Filename:    file.Filename,
		Size:        file.Size,
		IsDirectory: false,
	}

	if err := repo.CreateVirtualFile(vf); err != nil {
		return fmt.Errorf("failed to create virtual file for single file: %w", err)
	}

	// Store additional metadata if needed
	if len(file.Groups) > 0 {
		groupsStr := strings.Join(file.Groups, ",")
		if err := repo.SetFileMetadata(vf.ID, "groups", groupsStr); err != nil {
			return fmt.Errorf("failed to set groups metadata: %w", err)
		}
	}

	return nil
}

// processMultiFile handles NZBs with multiple files (legacy method)
func (proc *Processor) processMultiFile(repo *database.Repository, nzbFile *database.NzbFile, files []ParsedFile) error {
	return proc.processMultiFileWithDir(repo, nzbFile, files, "/")
}

// processMultiFileWithDir handles NZBs with multiple files in a specific virtual directory
func (proc *Processor) processMultiFileWithDir(repo *database.Repository, nzbFile *database.NzbFile, files []ParsedFile, virtualDir string) error {
	// Create directory structure based on common path prefixes within the virtual directory
	dirStructure := proc.analyzeDirectoryStructureWithBase(files, virtualDir)

	// Create directories first, tracking parent relationships
	createdDirs := make(map[string]*database.VirtualFile)
	for _, dir := range dirStructure.directories {
		// Get parent directory ID
		var parentID *int64
		if dir.parent != nil {
			if parentVF, exists := createdDirs[*dir.parent]; exists {
				parentID = &parentVF.ID
			} else {
				// Get or create parent directory
				parentDir, err := proc.getOrCreateParentDirectory(repo, *dir.parent)
				if err != nil {
					return fmt.Errorf("failed to get parent directory %s: %w", *dir.parent, err)
				}
				parentID = parentDir
			}
		}

		vf := &database.VirtualFile{
			NzbFileID:   int64Ptr(nzbFile.ID),
			ParentID:    parentID,
			VirtualPath: dir.path,
			Filename:    dir.name,
			Size:        0,
			IsDirectory: true,
		}

		if err := repo.CreateVirtualFile(vf); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir.path, err)
		}

		createdDirs[dir.path] = vf
	}

	// Create file entries
	for _, file := range files {
		parentPath, filename := proc.determineFileLocationWithBase(file, dirStructure, virtualDir)

		// Get parent directory ID
		var parentID *int64
		if parentVF, exists := createdDirs[parentPath]; exists {
			parentID = &parentVF.ID
		} else {
			// Get or create parent directory
			parentDir, err := proc.getOrCreateParentDirectory(repo, parentPath)
			if err != nil {
				return fmt.Errorf("failed to get parent directory %s: %w", parentPath, err)
			}
			parentID = parentDir
		}

		virtualPath := filepath.Join(parentPath, filename)
		virtualPath = strings.ReplaceAll(virtualPath, string(filepath.Separator), "/")

		vf := &database.VirtualFile{
			NzbFileID:   int64Ptr(nzbFile.ID),
			ParentID:    parentID,
			VirtualPath: virtualPath,
			Filename:    filename,
			Size:        file.Size,
			IsDirectory: false,
		}

		if err := repo.CreateVirtualFile(vf); err != nil {
			return fmt.Errorf("failed to create virtual file %s: %w", filename, err)
		}

		// Store metadata
		if len(file.Groups) > 0 {
			groupsStr := strings.Join(file.Groups, ",")
			if err := repo.SetFileMetadata(vf.ID, "groups", groupsStr); err != nil {
				return fmt.Errorf("failed to set groups metadata: %w", err)
			}
		}
	}

	return nil
}

// processRarArchive handles NZBs containing RAR archives (legacy method)
func (proc *Processor) processRarArchive(repo *database.Repository, nzbFile *database.NzbFile, files []ParsedFile) error {
	return proc.processRarArchiveWithDir(repo, nzbFile, files, "/")
}

// processRarArchiveWithDir handles NZBs containing RAR archives in a specific virtual directory
func (proc *Processor) processRarArchiveWithDir(repo *database.Repository, nzbFile *database.NzbFile, files []ParsedFile, virtualDir string) error {
	// For RAR archives, we create directory structure based on the archive contents
	// Each RAR archive becomes a directory, and we create virtual files for its contents

	for _, file := range files {
		if !file.IsRarArchive {
			// Non-RAR file in a RAR archive NZB - treat as regular file in virtual directory
			parentDir, err := proc.getOrCreateParentDirectory(repo, virtualDir)
			if err != nil {
				return fmt.Errorf("failed to get parent directory: %w", err)
			}

			virtualPath := filepath.Join(virtualDir, file.Filename)
			virtualPath = strings.ReplaceAll(virtualPath, string(filepath.Separator), "/")

			vf := &database.VirtualFile{
				NzbFileID:   int64Ptr(nzbFile.ID),
				ParentID:    parentDir,
				VirtualPath: virtualPath,
				Filename:    file.Filename,
				Size:        file.Size,
				IsDirectory: false,
			}

			if err := repo.CreateVirtualFile(vf); err != nil {
				return fmt.Errorf("failed to create non-RAR file: %w", err)
			}
			continue
		}

		// For RAR files, create a directory structure
		baseName := strings.TrimSuffix(file.Filename, filepath.Ext(file.Filename))
		// Remove common RAR suffixes like .part01, .part001, etc.
		rarDirPattern := regexp.MustCompile(`\.(part\d+|r\d+)$`)
		baseName = rarDirPattern.ReplaceAllString(baseName, "")

		rarDirPath := filepath.Join(virtualDir, baseName)
		rarDirPath = strings.ReplaceAll(rarDirPath, string(filepath.Separator), "/")
		
		// Get parent directory ID
		parentDir, err := proc.getOrCreateParentDirectory(repo, virtualDir)
		if err != nil {
			return fmt.Errorf("failed to get parent directory: %w", err)
		}

		// Create directory for the RAR archive content in virtual directory
		rarDir := &database.VirtualFile{
			NzbFileID:   int64Ptr(nzbFile.ID),
			ParentID:    parentDir,
			VirtualPath: rarDirPath,
			Filename:    baseName,
			Size:        0, // Directory size
			IsDirectory: true,
		}

		if err := repo.CreateVirtualFile(rarDir); err != nil {
			return fmt.Errorf("failed to create RAR directory %s: %w", rarDirPath, err)
		}

		// Create a virtual file representing the RAR archive itself within the directory
		rarArchiveFile := &database.VirtualFile{
			NzbFileID:   int64Ptr(nzbFile.ID),
			ParentID:    &rarDir.ID,
			VirtualPath: rarDirPath + "/" + file.Filename,
			Filename:    file.Filename,
			Size:        file.Size,
			IsDirectory: false,
		}

		if err := repo.CreateVirtualFile(rarArchiveFile); err != nil {
			return fmt.Errorf("failed to create RAR file entry: %w", err)
		}

		// Mark for RAR content analysis
		if err := repo.SetFileMetadata(rarArchiveFile.ID, "rar_analysis_needed", "true"); err != nil {
			return fmt.Errorf("failed to set RAR analysis flag: %w", err)
		}

		// If we have pre-analyzed RAR contents, create virtual files for them
		if len(file.RarContents) > 0 {
			for _, rarEntry := range file.RarContents {
				// Store RAR content metadata
				rc := &database.RarContent{
					VirtualFileID:  rarArchiveFile.ID,
					InternalPath:   rarEntry.Path,
					Filename:       rarEntry.Filename,
					Size:           rarEntry.Size,
					CompressedSize: rarEntry.CompressedSize,
					CRC32:          stringPtr(rarEntry.CRC32),
				}

				if err := repo.CreateRarContent(rc); err != nil {
					return fmt.Errorf("failed to create RAR content entry: %w", err)
				}

				// Create virtual files for extracted content
				contentPath := rarDirPath + "/" + rarEntry.Filename
				contentFile := &database.VirtualFile{
					NzbFileID:   int64Ptr(nzbFile.ID),
					ParentID:    &rarDir.ID,
					VirtualPath: contentPath,
					Filename:    rarEntry.Filename,
					Size:        rarEntry.Size,
					IsDirectory: rarEntry.IsDirectory,
				}

				if err := repo.CreateVirtualFile(contentFile); err != nil {
					return fmt.Errorf("failed to create RAR content file %s: %w", contentPath, err)
				}

				// Mark this as extracted from RAR
				if err := repo.SetFileMetadata(contentFile.ID, "extracted_from_rar", rarArchiveFile.VirtualPath); err != nil {
					return fmt.Errorf("failed to set RAR extraction metadata: %w", err)
				}
			}
		}
	}

	return nil
}

// DirectoryStructure represents the analyzed directory structure
type DirectoryStructure struct {
	directories []DirectoryInfo
	commonRoot  string
}

// DirectoryInfo represents information about a directory
type DirectoryInfo struct {
	path   string
	name   string
	parent *string
}

// analyzeDirectoryStructure analyzes files to determine directory structure
func (proc *Processor) analyzeDirectoryStructure(files []ParsedFile) *DirectoryStructure {
	// Simple implementation: group files by common prefixes in their filenames
	pathMap := make(map[string]bool)

	for _, file := range files {
		dir := filepath.Dir(file.Filename)
		if dir != "." && dir != "/" {
			pathMap[dir] = true
		}
	}

	var dirs []DirectoryInfo
	for path := range pathMap {
		parent := filepath.Dir(path)
		if parent == "." || parent == "/" {
			parent = "/"
		}

		dirs = append(dirs, DirectoryInfo{
			path:   "/" + path,
			name:   filepath.Base(path),
			parent: stringPtr(parent),
		})
	}

	return &DirectoryStructure{
		directories: dirs,
		commonRoot:  "/",
	}
}

// determineFileLocation determines where a file should be placed in the virtual structure (legacy method)
func (proc *Processor) determineFileLocation(file ParsedFile, dirStructure *DirectoryStructure) (parentPath, filename string) {
	return proc.determineFileLocationWithBase(file, dirStructure, "/")
}

// determineFileLocationWithBase determines where a file should be placed in the virtual structure within a base directory
func (proc *Processor) determineFileLocationWithBase(file ParsedFile, _ *DirectoryStructure, baseDir string) (parentPath, filename string) {
	dir := filepath.Dir(file.Filename)
	name := filepath.Base(file.Filename)

	if dir == "." || dir == "/" {
		return baseDir, name
	}

	virtualPath := filepath.Join(baseDir, dir)
	virtualPath = strings.ReplaceAll(virtualPath, string(filepath.Separator), "/")
	return virtualPath, name
}

// analyzeDirectoryStructureWithBase analyzes files to determine directory structure within a base directory
func (proc *Processor) analyzeDirectoryStructureWithBase(files []ParsedFile, baseDir string) *DirectoryStructure {
	// Simple implementation: group files by common prefixes in their filenames within the base directory
	pathMap := make(map[string]bool)

	for _, file := range files {
		dir := filepath.Dir(file.Filename)
		if dir != "." && dir != "/" {
			// Add the directory path within the base directory
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
			path:   path,
			name:   filepath.Base(path),
			parent: stringPtr(parent),
		})
	}

	return &DirectoryStructure{
		directories: dirs,
		commonRoot:  baseDir,
	}
}

// AnalyzeRarContentFromData analyzes RAR content when actual RAR data is available
// This method can be called after downloading RAR segments to extract the internal file structure
func (proc *Processor) AnalyzeRarContentFromData(r io.Reader, virtualFileID int64) error {
	// Get the virtual file from database
	virtualFile, err := proc.repo.GetVirtualFile(virtualFileID)
	if err != nil {
		return fmt.Errorf("failed to get virtual file: %w", err)
	}

	// Use RAR handler to analyze content
	rarContents, err := proc.rarHandler.AnalyzeRarContent(r, virtualFile, nil)
	if err != nil {
		return fmt.Errorf("failed to analyze RAR content: %w", err)
	}

	// Store RAR contents in database
	return proc.repo.WithTransaction(func(txRepo *database.Repository) error {
		for _, content := range rarContents {
			if err := txRepo.CreateRarContent(&content); err != nil {
				return fmt.Errorf("failed to store RAR content entry %s: %w", content.Filename, err)
			}
		}

		// Remove the analysis needed flag
		return txRepo.DeleteFileMetadata(virtualFileID, "rar_analysis_needed")
	})
}

// GetPendingRarAnalysis returns virtual files that need RAR content analysis
func (proc *Processor) GetPendingRarAnalysis() ([]*database.VirtualFile, error) {
	// This would query the database for files with "rar_analysis_needed" metadata
	// Implementation depends on having a method to query by metadata in the repository
	return nil, fmt.Errorf("pending RAR analysis query not implemented")
}

// calculateVirtualDirectory determines the virtual directory path based on NZB file location relative to watch root
func (proc *Processor) calculateVirtualDirectory(nzbPath, watchRoot string) string {
	if watchRoot == "" {
		// No watch root specified, place in root directory
		return "/"
	}

	// Clean paths for consistent comparison
	nzbPath = filepath.Clean(nzbPath)
	watchRoot = filepath.Clean(watchRoot)

	// Get relative path from watch root to NZB file
	relPath, err := filepath.Rel(watchRoot, nzbPath)
	if err != nil {
		// If we can't get relative path, default to root
		return "/"
	}

	// Get directory of NZB file (without filename)
	relDir := filepath.Dir(relPath)

	// Convert to virtual path
	if relDir == "." || relDir == "" {
		// NZB is directly in watch root
		return "/"
	}

	// Ensure virtual path starts with / and uses forward slashes
	virtualPath := "/" + strings.ReplaceAll(relDir, string(filepath.Separator), "/")
	return filepath.Clean(virtualPath)
}

// ensureParentDirectories creates all necessary parent directories in the virtual filesystem
func (proc *Processor) ensureParentDirectories(repo *database.Repository, virtualDir string) error {
	if virtualDir == "/" {
		// Root directory already exists, nothing to do
		return nil
	}

	// Split path into components and create each level
	parts := strings.Split(strings.Trim(virtualDir, "/"), "/")
	currentPath := ""
	var currentParentID *int64 // Root level has nil parent

	for _, part := range parts {
		currentPath = filepath.Join(currentPath, part)
		virtualPath := "/" + strings.ReplaceAll(currentPath, string(filepath.Separator), "/")

		// Check if directory already exists
		existing, err := repo.GetVirtualFileByPath(virtualPath)
		if err != nil {
			return fmt.Errorf("failed to check directory %s: %w", virtualPath, err)
		}

		if existing == nil {
			// Create directory
			dir := &database.VirtualFile{
				NzbFileID:   nil, // System directory, no associated NZB
				ParentID:    currentParentID,
				VirtualPath: virtualPath,
				Filename:    part,
				Size:        0,
				IsDirectory: true,
			}

			if err := repo.CreateVirtualFile(dir); err != nil {
				return fmt.Errorf("failed to create directory %s: %w", virtualPath, err)
			}

			// Update parent ID for next iteration
			currentParentID = &dir.ID
		} else {
			// Directory exists, use its ID as parent for next iteration
			currentParentID = &existing.ID
		}
	}

	return nil
}

// Helper function to create string pointer
func stringPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// Helper function to create int64 pointer
func int64Ptr(i int64) *int64 {
	return &i
}

// getOrCreateParentDirectory gets or creates a parent directory and returns its ID
func (proc *Processor) getOrCreateParentDirectory(repo *database.Repository, virtualDir string) (*int64, error) {
	if virtualDir == "/" {
		// Root directory - parent_id is NULL
		return nil, nil
	}

	// Check if directory exists
	existing, err := repo.GetVirtualFileByPath(virtualDir)
	if err != nil {
		return nil, fmt.Errorf("failed to check directory %s: %w", virtualDir, err)
	}

	if existing != nil {
		if !existing.IsDirectory {
			return nil, fmt.Errorf("path exists but is not a directory: %s", virtualDir)
		}
		return &existing.ID, nil
	}

	// Directory doesn't exist, ensure parent directories first
	if err := proc.ensureParentDirectories(repo, virtualDir); err != nil {
		return nil, fmt.Errorf("failed to create parent directories: %w", err)
	}

	// Now get the directory that was just created
	created, err := repo.GetVirtualFileByPath(virtualDir)
	if err != nil {
		return nil, fmt.Errorf("failed to get created directory: %w", err)
	}
	if created == nil {
		return nil, fmt.Errorf("directory was not created: %s", virtualDir)
	}

	return &created.ID, nil
}
