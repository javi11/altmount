package importer

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/javi11/altmount/internal/metadata"
	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/javi11/altmount/internal/pool"
)

// Processor handles the processing and storage of parsed NZB files using metadata storage
type Processor struct {
	parser          *Parser
	strmParser      *StrmParser
	metadataService *metadata.MetadataService
	rarProcessor    RarProcessor
	poolManager     pool.Manager // Pool manager for dynamic pool access
	log             *slog.Logger

	// Pre-compiled regex patterns for RAR file sorting
	rarPartPattern    *regexp.Regexp // pattern.part###.rar
	rarRPattern       *regexp.Regexp // pattern.r### or pattern.r##
	rarNumericPattern *regexp.Regexp // pattern.### (numeric extensions)
}

// NewProcessor creates a new NZB processor using metadata storage
func NewProcessor(metadataService *metadata.MetadataService, poolManager pool.Manager) *Processor {
	return &Processor{
		parser:          NewParser(poolManager),
		strmParser:      NewStrmParser(),
		metadataService: metadataService,
		rarProcessor:    NewRarProcessor(poolManager, 10), // 10 max workers for RAR analysis
		poolManager:     poolManager,
		log:             slog.Default().With("component", "nzb-processor"),

		// Initialize pre-compiled regex patterns for RAR file sorting
		rarPartPattern:    regexp.MustCompile(`^(.+)\.part(\d+)\.rar$`), // filename.part001.rar
		rarRPattern:       regexp.MustCompile(`^(.+)\.r(\d+)$`),         // filename.r00, filename.r01
		rarNumericPattern: regexp.MustCompile(`^(.+)\.(\d+)$`),          // filename.001, filename.002
	}
}

// ProcessNzbFileWithRelativePath processes an NZB or STRM file maintaining the folder structure relative to relative path
func (proc *Processor) ProcessNzbFile(filePath, relativePath string) (string, error) {
	// Open and parse the file
	file, err := os.Open(filePath)
	if err != nil {
		return "", NewNonRetryableError("failed to open file", err)
	}
	defer file.Close()

	var parsed *ParsedNzb

	// Determine file type and parse accordingly
	if strings.HasSuffix(strings.ToLower(filePath), ".strm") {
		parsed, err = proc.strmParser.ParseStrmFile(file, filePath)
		if err != nil {
			return "", NewNonRetryableError("failed to parse STRM file", err)
		}

		// Validate the parsed STRM
		if err := proc.strmParser.ValidateStrmFile(parsed); err != nil {
			return "", NewNonRetryableError("STRM validation failed", err)
		}
	} else {
		parsed, err = proc.parser.ParseFile(file, filePath)
		if err != nil {
			return "", NewNonRetryableError("failed to parse NZB file", err)
		}

		// Validate the parsed NZB
		if err := proc.parser.ValidateNzb(parsed); err != nil {
			return "", NewNonRetryableError("NZB validation failed", err)
		}
	}

	// Calculate the relative virtual directory path for this file
	virtualDir := proc.calculateVirtualDirectory(filePath, relativePath)

	proc.log.Info("Processing file",
		"file_path", filePath,
		"virtual_dir", virtualDir,
		"type", parsed.Type,
		"total_size", parsed.TotalSize,
		"files", len(parsed.Files))

	// Process based on file type
	switch parsed.Type {
	case NzbTypeSingleFile:
		return proc.processSingleFileWithDir(parsed, virtualDir)
	case NzbTypeMultiFile:
		return proc.processMultiFileWithDir(parsed, virtualDir)
	case NzbTypeRarArchive:
		return proc.processRarArchiveWithDir(parsed, virtualDir)
	case NzbTypeStrm:
		return proc.processStrmFileWithDir(parsed, virtualDir)
	default:
		return "", NewNonRetryableError(fmt.Sprintf("unknown file type: %s", parsed.Type), nil)
	}
}

// processSingleFileWithDir handles NZBs with a single file in a specific virtual directory
func (proc *Processor) processSingleFileWithDir(parsed *ParsedNzb, virtualDir string) (string, error) {
	regularFiles, _ := proc.separatePar2Files(parsed.Files)

	file := regularFiles[0] // Single file NZB, take the first regular file

	// Create the directory structure if needed
	if err := proc.ensureDirectoryExists(virtualDir); err != nil {
		return "", fmt.Errorf("failed to create directory structure: %w", err)
	}

	// Create virtual file path
	virtualFilePath := filepath.Join(virtualDir, file.Filename)
	virtualFilePath = strings.ReplaceAll(virtualFilePath, string(filepath.Separator), "/")
	// Create file metadata using simplified schema
	fileMeta := proc.metadataService.CreateFileMetadata(
		file.Size,
		parsed.Path,
		metapb.FileStatus_FILE_STATUS_HEALTHY,
		file.Segments,
		file.Encryption,
		file.Password,
		file.Salt,
	)

	// Write file metadata to disk
	if err := proc.metadataService.WriteFileMetadata(virtualFilePath, fileMeta); err != nil {
		return "", fmt.Errorf("failed to write metadata for single file %s: %w", file.Filename, err)
	}

	// Store additional metadata if needed
	if len(file.Groups) > 0 {
		proc.log.Debug("Groups metadata", "file", file.Filename, "groups", strings.Join(file.Groups, ","))
	}

	proc.log.Info("Successfully processed single file NZB",
		"file", file.Filename,
		"virtual_path", virtualFilePath,
		"size", file.Size)

	return virtualFilePath, nil
}

// processMultiFileWithDir handles NZBs with multiple files in a specific virtual directory
func (proc *Processor) processMultiFileWithDir(parsed *ParsedNzb, virtualDir string) (string, error) {
	// Create a folder named after the NZB file for multi-file imports
	nzbBaseName := strings.TrimSuffix(parsed.Filename, filepath.Ext(parsed.Filename))
	nzbVirtualDir := filepath.Join(virtualDir, nzbBaseName)
	nzbVirtualDir = strings.ReplaceAll(nzbVirtualDir, string(filepath.Separator), "/")

	// Create directory structure based on common path prefixes within the NZB virtual directory
	dirStructure := proc.analyzeDirectoryStructureWithBase(parsed.Files, nzbVirtualDir)

	// Create directories first using real filesystem
	for _, dir := range dirStructure.directories {
		if err := proc.ensureDirectoryExists(dir.path); err != nil {
			return "", fmt.Errorf("failed to create directory %s: %w", dir.path, err)
		}
	}

	regularFiles, _ := proc.separatePar2Files(parsed.Files)

	// Create file entries
	for _, file := range regularFiles {
		parentPath, filename := proc.determineFileLocationWithBase(file, dirStructure, nzbVirtualDir)

		// Ensure parent directory exists
		if err := proc.ensureDirectoryExists(parentPath); err != nil {
			return "", fmt.Errorf("failed to create parent directory %s: %w", parentPath, err)
		}

		// Create virtual file path
		virtualPath := filepath.Join(parentPath, filename)
		virtualPath = strings.ReplaceAll(virtualPath, string(filepath.Separator), "/")

		// Create file metadata using simplified schema
		fileMeta := proc.metadataService.CreateFileMetadata(
			file.Size,
			parsed.Path,
			metapb.FileStatus_FILE_STATUS_HEALTHY,
			file.Segments,
			file.Encryption,
			file.Password,
			file.Salt,
		)

		// Write file metadata to disk
		if err := proc.metadataService.WriteFileMetadata(virtualPath, fileMeta); err != nil {
			return "", fmt.Errorf("failed to write metadata for file %s: %w", filename, err)
		}

		// Store additional metadata if needed
		if len(file.Groups) > 0 {
			proc.log.Debug("Groups metadata", "file", filename, "groups", strings.Join(file.Groups, ","))
		}

		proc.log.Debug("Created metadata file",
			"file", filename,
			"virtual_path", virtualPath,
			"size", file.Size)
	}

	proc.log.Info("Successfully processed multi-file NZB",
		"virtual_dir", nzbVirtualDir,
		"files", len(regularFiles),
		"directories", len(dirStructure.directories))

	return nzbVirtualDir, nil
}

// processRarArchiveWithDir handles NZBs containing RAR archives and regular files in a specific virtual directory
func (proc *Processor) processRarArchiveWithDir(parsed *ParsedNzb, virtualDir string) (string, error) {
	// Create a folder named after the NZB file for multi-file imports
	nzbBaseName := strings.TrimSuffix(parsed.Filename, filepath.Ext(parsed.Filename))
	nzbVirtualDir := filepath.Join(virtualDir, nzbBaseName)
	nzbVirtualDir = strings.ReplaceAll(nzbVirtualDir, string(filepath.Separator), "/")

	// Separate RAR files from regular files
	regularFiles, rarFiles := proc.separateRarFiles(parsed.Files)

	// Filter out PAR2 files from regular files
	regularFiles, _ = proc.separatePar2Files(regularFiles)

	// Process regular files first (non-RAR files like MKV, MP4, etc.)
	if len(regularFiles) > 0 {
		proc.log.Info("Processing regular files in RAR archive NZB",
			"virtual_dir", nzbVirtualDir,
			"regular_files", len(regularFiles))

		// Create directory structure for regular files
		dirStructure := proc.analyzeDirectoryStructureWithBase(regularFiles, nzbVirtualDir)

		// Create directories first
		for _, dir := range dirStructure.directories {
			if err := proc.ensureDirectoryExists(dir.path); err != nil {
				return "", fmt.Errorf("failed to create directory %s: %w", dir.path, err)
			}
		}

		// Process each regular file
		for _, file := range regularFiles {
			parentPath, filename := proc.determineFileLocationWithBase(file, dirStructure, nzbVirtualDir)

			// Ensure parent directory exists
			if err := proc.ensureDirectoryExists(parentPath); err != nil {
				return "", fmt.Errorf("failed to create parent directory %s: %w", parentPath, err)
			}

			// Create virtual file path
			virtualPath := filepath.Join(parentPath, filename)
			virtualPath = strings.ReplaceAll(virtualPath, string(filepath.Separator), "/")

			// Create file metadata
			fileMeta := proc.metadataService.CreateFileMetadata(
				file.Size,
				parsed.Path,
				metapb.FileStatus_FILE_STATUS_HEALTHY,
				file.Segments,
				file.Encryption,
				file.Password,
				file.Salt,
			)

			// Write file metadata to disk
			if err := proc.metadataService.WriteFileMetadata(virtualPath, fileMeta); err != nil {
				return "", fmt.Errorf("failed to write metadata for regular file %s: %w", filename, err)
			}

			proc.log.Debug("Created metadata for regular file",
				"file", filename,
				"virtual_path", virtualPath,
				"size", file.Size)
		}

		proc.log.Info("Successfully processed regular files",
			"virtual_dir", nzbVirtualDir,
			"files_processed", len(regularFiles))
	}

	// Process RAR archives if any exist
	if len(rarFiles) > 0 {
		// Rename RAR files to match the first file's base name that will allow parse rar that have different files name
		rarFiles = renameRarFilesToMatchFirstFile(rarFiles)

		// Create directory for the single RAR archive content
		nzbBaseName := strings.TrimSuffix(parsed.Filename, filepath.Ext(parsed.Filename))
		rarDirPath := filepath.Join(nzbVirtualDir, nzbBaseName)
		rarDirPath = strings.ReplaceAll(rarDirPath, string(filepath.Separator), "/")

		// Ensure RAR archive directory exists
		if err := proc.ensureDirectoryExists(rarDirPath); err != nil {
			return "", fmt.Errorf("failed to create RAR directory %s: %w", rarDirPath, err)
		}

		proc.log.Info("Processing RAR archive with content analysis",
			"archive", nzbBaseName,
			"parts", len(rarFiles),
			"rar_dir", rarDirPath)

		// Analyze RAR content using the new RAR handler
		ctx := context.Background()
		rarContents, err := proc.rarProcessor.AnalyzeRarContentFromNzb(ctx, rarFiles)
		if err != nil {
			proc.log.Error("Failed to analyze RAR archive content",
				"archive", nzbBaseName,
				"error", err)
			// Fallback to simplified mode if RAR analysis fails
			return "", err
		}

		proc.log.Info("Successfully analyzed RAR archive content",
			"archive", nzbBaseName,
			"files_in_archive", len(rarContents))

		// Process each file found in the RAR archive
		for _, rarContent := range rarContents {
			// Skip directories
			if rarContent.IsDirectory {
				proc.log.Debug("Skipping directory in RAR archive", "path", rarContent.InternalPath)
				continue
			}

			// Determine the virtual file path for this extracted file
			// The file path should be relative to the RAR directory
			virtualFilePath := filepath.Join(rarDirPath, rarContent.InternalPath)
			virtualFilePath = strings.ReplaceAll(virtualFilePath, string(filepath.Separator), "/")

			// Ensure parent directory exists for nested files
			parentDir := filepath.Dir(virtualFilePath)
			if err := proc.ensureDirectoryExists(parentDir); err != nil {
				return "", fmt.Errorf("failed to create parent directory %s for RAR file: %w", parentDir, err)
			}

			// Create file metadata using the RAR handler's helper function
			fileMeta := proc.rarProcessor.CreateFileMetadataFromRarContent(
				rarContent,
				parsed.Path,
			)

			// Write file metadata to disk
			if err := proc.metadataService.WriteFileMetadata(virtualFilePath, fileMeta); err != nil {
				return "", fmt.Errorf("failed to write metadata for RAR file %s: %w", rarContent.Filename, err)
			}

			proc.log.Debug("Created metadata for RAR extracted file",
				"file", rarContent.Filename,
				"internal_path", rarContent.InternalPath,
				"virtual_path", virtualFilePath,
				"size", rarContent.Size,
				"segments", len(rarContent.Segments))
		}

		proc.log.Info("Successfully processed RAR archive with content analysis",
			"archive", nzbBaseName,
			"files_processed", len(rarContents))
	}

	return nzbVirtualDir, nil
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

// calculateVirtualDirectory determines the virtual directory path based on NZB file location relative to watch root
func (proc *Processor) calculateVirtualDirectory(nzbPath, relativePath string) string {
	if relativePath == "" {
		// No watch root specified, place in root directory
		return "/"
	}

	// Clean paths for consistent comparison
	nzbPath = filepath.Clean(nzbPath)
	relativePath = filepath.Clean(relativePath)

	// Get relative path from watch root to NZB file
	relPath, err := filepath.Rel(relativePath, nzbPath)
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

// ensureDirectoryExists creates directory structure in the metadata filesystem
func (proc *Processor) ensureDirectoryExists(virtualDir string) error {
	if virtualDir == "/" {
		// Root directory always exists
		return nil
	}

	// Get the actual filesystem path for this virtual directory
	metadataDir := proc.metadataService.GetMetadataDirectoryPath(virtualDir)

	// Create the directory structure using os.MkdirAll
	if err := os.MkdirAll(metadataDir, 0755); err != nil {
		return fmt.Errorf("failed to create metadata directory %s: %w", metadataDir, err)
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

// isPar2File checks if a filename is a PAR2 repair file
func (proc *Processor) isPar2File(filename string) bool {
	lower := strings.ToLower(filename)
	return strings.HasSuffix(lower, ".par2")
}

// separatePar2Files separates PAR2 files from regular files
func (proc *Processor) separatePar2Files(files []ParsedFile) ([]ParsedFile, []ParsedFile) {
	var regularFiles []ParsedFile
	var par2Files []ParsedFile

	for _, file := range files {
		if proc.isPar2File(file.Filename) {
			par2Files = append(par2Files, file)
		} else {
			regularFiles = append(regularFiles, file)
		}
	}

	return regularFiles, par2Files
}

// separateRarFiles separates RAR files from regular files
func (proc *Processor) separateRarFiles(files []ParsedFile) ([]ParsedFile, []ParsedFile) {
	var regularFiles []ParsedFile
	var rarFiles []ParsedFile

	for _, file := range files {
		if file.IsRarArchive {
			rarFiles = append(rarFiles, file)
		} else {
			regularFiles = append(regularFiles, file)
		}
	}

	return regularFiles, rarFiles
}

// processStrmFileWithDir handles STRM files (single file from NXG link) in a specific virtual directory
func (proc *Processor) processStrmFileWithDir(parsed *ParsedNzb, virtualDir string) (string, error) {
	if len(parsed.Files) != 1 {
		return "", NewNonRetryableError(fmt.Sprintf("STRM file should contain exactly one file, got %d", len(parsed.Files)), nil)
	}

	file := parsed.Files[0]

	// Create the directory structure if needed
	if err := proc.ensureDirectoryExists(virtualDir); err != nil {
		return "", fmt.Errorf("failed to create directory structure: %w", err)
	}

	// Create virtual file path
	virtualFilePath := filepath.Join(virtualDir, file.Filename)
	virtualFilePath = strings.ReplaceAll(virtualFilePath, string(filepath.Separator), "/")

	// Create file metadata using simplified schema
	fileMeta := proc.metadataService.CreateFileMetadata(
		file.Size,
		parsed.Path,
		metapb.FileStatus_FILE_STATUS_HEALTHY,
		file.Segments,
		file.Encryption,
		file.Password,
		file.Salt,
	)

	// Write file metadata to disk
	if err := proc.metadataService.WriteFileMetadata(virtualFilePath, fileMeta); err != nil {
		return "", fmt.Errorf("failed to write metadata for STRM file %s: %w", file.Filename, err)
	}

	proc.log.Info("Successfully processed STRM file",
		"file", file.Filename,
		"virtual_path", virtualFilePath,
		"size", file.Size,
		"segments", len(file.Segments))

	return virtualDir, nil
}
