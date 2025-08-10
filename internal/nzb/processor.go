package nzb

import (
	"fmt"
	"os"
	"path/filepath"
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

	// Process within a transaction
	return proc.repo.WithTransaction(func(txRepo *database.Repository) error {
		// Create the NZB file record
		nzbFile := &database.NzbFile{
			Path:          parsed.Path,
			Filename:      parsed.Filename,
			Size:          parsed.TotalSize,
			NzbType:       parsed.Type,
			SegmentsCount: parsed.SegmentsCount,
			SegmentsData:  proc.parser.ConvertToDbSegments(parsed.Files),
		}

		if err := txRepo.CreateNzbFile(nzbFile); err != nil {
			return fmt.Errorf("failed to create NZB file record: %w", err)
		}

		// Process based on NZB type
		switch parsed.Type {
		case database.NzbTypeSingleFile:
			return proc.processSingleFile(txRepo, nzbFile, parsed.Files[0])
		case database.NzbTypeMultiFile:
			return proc.processMultiFile(txRepo, nzbFile, parsed.Files)
		case database.NzbTypeRarArchive:
			return proc.processRarArchive(txRepo, nzbFile, parsed.Files)
		default:
			return fmt.Errorf("unknown NZB type: %s", parsed.Type)
		}
	})
}

// processSingleFile handles NZBs with a single file
func (proc *Processor) processSingleFile(repo *database.Repository, nzbFile *database.NzbFile, file ParsedFile) error {
	// Create virtual file entry
	vf := &database.VirtualFile{
		NzbFileID:   nzbFile.ID,
		VirtualPath: "/" + file.Filename,
		Filename:    file.Filename,
		Size:        file.Size,
		IsDirectory: false,
		ParentPath:  stringPtr("/"),
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

// processMultiFile handles NZBs with multiple files
func (proc *Processor) processMultiFile(repo *database.Repository, nzbFile *database.NzbFile, files []ParsedFile) error {
	// Create directory structure based on common path prefixes
	dirStructure := proc.analyzeDirectoryStructure(files)

	// Create directories first
	createdDirs := make(map[string]*database.VirtualFile)
	for _, dir := range dirStructure.directories {
		vf := &database.VirtualFile{
			NzbFileID:   nzbFile.ID,
			VirtualPath: dir.path,
			Filename:    dir.name,
			Size:        0,
			IsDirectory: true,
			ParentPath:  dir.parent,
		}

		if err := repo.CreateVirtualFile(vf); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir.path, err)
		}

		createdDirs[dir.path] = vf
	}

	// Create file entries
	for _, file := range files {
		parentPath, filename := proc.determineFileLocation(file, dirStructure)

		vf := &database.VirtualFile{
			NzbFileID:   nzbFile.ID,
			VirtualPath: filepath.Join(parentPath, filename),
			Filename:    filename,
			Size:        file.Size,
			IsDirectory: false,
			ParentPath:  stringPtr(parentPath),
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

// processRarArchive handles NZBs containing RAR archives
func (proc *Processor) processRarArchive(repo *database.Repository, nzbFile *database.NzbFile, files []ParsedFile) error {
	// For RAR archives, we need to analyze the RAR content
	// For now, we'll create the RAR files and mark them for later processing

	for _, file := range files {
		if !file.IsRarArchive {
			// Non-RAR file in a RAR archive NZB - treat as regular file
			vf := &database.VirtualFile{
				NzbFileID:   nzbFile.ID,
				VirtualPath: "/" + file.Filename,
				Filename:    file.Filename,
				Size:        file.Size,
				IsDirectory: false,
				ParentPath:  stringPtr("/"),
			}

			if err := repo.CreateVirtualFile(vf); err != nil {
				return fmt.Errorf("failed to create non-RAR file: %w", err)
			}
			continue
		}

		// Create virtual file for the RAR archive
		vf := &database.VirtualFile{
			NzbFileID:   nzbFile.ID,
			VirtualPath: "/" + file.Filename,
			Filename:    file.Filename,
			Size:        file.Size,
			IsDirectory: false,
			ParentPath:  stringPtr("/"),
		}

		if err := repo.CreateVirtualFile(vf); err != nil {
			return fmt.Errorf("failed to create RAR file: %w", err)
		}

		// Attempt to analyze RAR contents
		// This would typically require downloading and analyzing RAR headers
		// For now, we'll mark it for later processing
		if err := repo.SetFileMetadata(vf.ID, "rar_analysis_needed", "true"); err != nil {
			return fmt.Errorf("failed to set RAR analysis flag: %w", err)
		}

		// If we have pre-analyzed RAR contents, store them
		if len(file.RarContents) > 0 {
			for _, rarEntry := range file.RarContents {
				rc := &database.RarContent{
					VirtualFileID:  vf.ID,
					InternalPath:   rarEntry.Path,
					Filename:       rarEntry.Filename,
					Size:           rarEntry.Size,
					CompressedSize: rarEntry.CompressedSize,
					CRC32:          stringPtr(rarEntry.CRC32),
				}

				if err := repo.CreateRarContent(rc); err != nil {
					return fmt.Errorf("failed to create RAR content entry: %w", err)
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

// determineFileLocation determines where a file should be placed in the virtual structure
func (proc *Processor) determineFileLocation(file ParsedFile, _ *DirectoryStructure) (parentPath, filename string) {
	dir := filepath.Dir(file.Filename)
	name := filepath.Base(file.Filename)

	if dir == "." || dir == "/" {
		return "/", name
	}

	return "/" + dir, name
}

// AnalyzeRarContentFromData analyzes RAR content when actual RAR data is available
// This method can be called after downloading RAR segments to extract the internal file structure
func (proc *Processor) AnalyzeRarContentFromData(rarData []byte, virtualFileID int64) error {
	// Get the virtual file from database
	virtualFile, err := proc.repo.GetVirtualFile(virtualFileID)
	if err != nil {
		return fmt.Errorf("failed to get virtual file: %w", err)
	}

	// Use RAR handler to analyze content
	rarContents, err := proc.rarHandler.AnalyzeRarContent(rarData, virtualFile, nil)
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

// Helper function to create string pointer
func stringPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
