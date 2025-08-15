package nzb

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/nntppool"
)

// Processor handles the processing and storage of parsed NZB files
type Processor struct {
	parser     *Parser
	repo       *database.Repository
	rarHandler *RarHandler
	cp         nntppool.UsenetConnectionPool // Connection pool for yenc header fetching
}

// NewProcessor creates a new NZB processor
func NewProcessor(repo *database.Repository, cp nntppool.UsenetConnectionPool) *Processor {
	return &Processor{
		parser:     NewParser(cp),
		repo:       repo,
		rarHandler: NewRarHandler(cp, 10), // 10 max workers for RAR analysis
		cp:         cp,
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
			Path:           parsed.Path,
			Filename:       parsed.Filename,
			Size:           parsed.TotalSize,
			NzbType:        parsed.Type,
			SegmentsCount:  parsed.SegmentsCount,
			SegmentsData:   proc.parser.ConvertToDbSegments(parsed.Files),
			SegmentSize:    parsed.SegmentSize,
			RclonePassword: parsed.Password,
			RcloneSalt:     parsed.Salt,
			ParentNzbID:    nil,                      // This is the main NZB file
			PartType:       database.NzbPartTypeMain, // Mark as main NZB file
			ArchiveName:    nil,                      // Not applicable for main NZB
		}

		if err := txRepo.CreateNzbFile(nzbFile); err != nil {
			return fmt.Errorf("failed to create NZB file record: %w", err)
		}

		// Separate PAR2 files from regular files
		regularFiles, par2Files := proc.separatePar2Files(parsed.Files)

		// Process PAR2 files separately
		if len(par2Files) > 0 {
			if err := proc.processPar2Files(txRepo, nzbFile, par2Files); err != nil {
				return fmt.Errorf("failed to process PAR2 files: %w", err)
			}
		}

		// Process regular files based on NZB type with virtual directory context
		// Skip processing if all files were PAR2 files
		if len(regularFiles) == 0 {
			return nil
		}

		switch parsed.Type {
		case database.NzbTypeSingleFile:
			if len(regularFiles) > 0 {
				return proc.processSingleFileWithDir(txRepo, nzbFile, regularFiles[0], virtualDir)
			}
		case database.NzbTypeMultiFile:
			return proc.processMultiFileWithDir(txRepo, nzbFile, regularFiles, virtualDir)
		case database.NzbTypeRarArchive:
			return proc.processRarArchiveWithDir(txRepo, nzbFile, regularFiles, virtualDir)
		default:
			return fmt.Errorf("unknown NZB type: %s", parsed.Type)
		}

		return nil
	})
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
		Encryption:  file.Encryption,
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
			Encryption:  nil, // Directories are not encrypted
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
			Encryption:  file.Encryption,
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

// processRarArchiveWithDir handles NZBs containing RAR archives in a specific virtual directory
func (proc *Processor) processRarArchiveWithDir(repo *database.Repository, nzbFile *database.NzbFile, files []ParsedFile, virtualDir string) error {
	// For RAR archives, we group files by their base archive name and process only the first part
	// of each archive to get all the files inside, since RAR parts contain the same file listing

	// First, handle non-RAR files
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
				Encryption:  file.Encryption,
			}

			if err := repo.CreateVirtualFile(vf); err != nil {
				return fmt.Errorf("failed to create non-RAR file: %w", err)
			}
		}
	}

	// Group RAR files by their archive base name
	rarArchives := proc.groupRarFilesByArchive(files)

	// Process each RAR archive
	for archiveName, archiveFiles := range rarArchives {
		// Create individual NZB records for each RAR part file
		err := proc.createRarPartNzbFiles(repo, nzbFile, archiveName, archiveFiles)
		if err != nil {
			return fmt.Errorf("failed to create RAR part NZB files for archive %s: %w", archiveName, err)
		}

		// Create directory for the RAR archive content
		rarDirPath := filepath.Join(virtualDir, archiveName)
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
			Filename:    archiveName,
			Size:        0, // Directory size
			IsDirectory: true,
			Encryption:  nil, // Directories are not encrypted
		}

		if err := repo.CreateVirtualFile(rarDir); err != nil {
			return fmt.Errorf("failed to create RAR directory %s: %w", rarDirPath, err)
		}

		// Perform real-time RAR analysis using the newly created RAR part NZB files
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute) // 5 minute timeout for analysis
		defer cancel()

		// Analyze RAR content using streaming from Usenet - now use the individual RAR part files
		rarContents, _, err := proc.rarHandler.AnalyzeRarContentFromNzb(ctx, nzbFile, archiveFiles, rarDir)
		if err != nil {
			// If real-time analysis fails, mark for later analysis
			if err := repo.SetFileMetadata(rarDir.ID, "rar_analysis_needed", "true"); err != nil {
				return fmt.Errorf("failed to set RAR analysis flag: %w", err)
			}
			if err := repo.SetFileMetadata(rarDir.ID, "rar_analysis_error", err.Error()); err != nil {
				return fmt.Errorf("failed to set RAR analysis error: %w", err)
			}
		} else {
			// Successfully analyzed - store the content
			for _, rarContent := range rarContents {
				if err := repo.CreateRarContent(&rarContent); err != nil {
					return fmt.Errorf("failed to create RAR content entry: %w", err)
				}

				// Create virtual files for extracted content
				contentPath := rarDirPath + "/" + rarContent.Filename
				contentFile := &database.VirtualFile{
					NzbFileID:   int64Ptr(nzbFile.ID),
					ParentID:    &rarDir.ID,
					VirtualPath: contentPath,
					Filename:    rarContent.Filename,
					Size:        rarContent.Size,
					IsDirectory: rarContent.IsDirectory,
					Encryption:  nil, // RAR content files are not encrypted with rclone
				}

				if err := repo.CreateVirtualFile(contentFile); err != nil {
					return fmt.Errorf("failed to create RAR content file %s: %w", contentPath, err)
				}

				// Mark this as extracted from RAR
				if err := repo.SetFileMetadata(contentFile.ID, "extracted_from_rar", rarDirPath); err != nil {
					return fmt.Errorf("failed to set RAR extraction metadata: %w", err)
				}
			}

			// Mark analysis as completed
			if err := repo.SetFileMetadata(rarDir.ID, "rar_analysis_completed", time.Now().UTC().Format(time.RFC3339)); err != nil {
				return fmt.Errorf("failed to set RAR analysis completed flag: %w", err)
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
				Encryption:  nil, // System directories are not encrypted
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

// processPar2Files processes PAR2 files and stores them separately
func (proc *Processor) processPar2Files(repo *database.Repository, nzbFile *database.NzbFile, par2Files []ParsedFile) error {
	for _, file := range par2Files {
		// Create segments data for this specific file
		segments := proc.parser.ConvertToDbSegmentsForFile(file)

		par2File := &database.Par2File{
			NzbFileID:    nzbFile.ID,
			Filename:     file.Filename,
			Size:         file.Size,
			SegmentsData: segments,
		}

		if err := repo.CreatePar2File(par2File); err != nil {
			return fmt.Errorf("failed to create PAR2 file %s: %w", file.Filename, err)
		}
	}

	return nil
}

// isPartOfSameRarSet determines if two RAR files are part of the same multi-part archive
func isPartOfSameRarSet(filename1, filename2 string) bool {
	base1, _ := splitRarFilenameForComparison(filename1)
	base2, _ := splitRarFilenameForComparison(filename2)
	return base1 == base2
}

// splitRarFilenameForComparison splits a RAR filename into base and extension parts for comparison
func splitRarFilenameForComparison(filename string) (base, ext string) {
	// Handle patterns like .part001.rar, .part01.rar
	partPattern := regexp.MustCompile(`^(.+)\.part\d+\.rar$`)
	if matches := partPattern.FindStringSubmatch(filename); len(matches) > 1 {
		return matches[1], strings.TrimPrefix(filename, matches[1]+".")
	}

	// Handle patterns like .rar, .r00, .r01
	if strings.HasSuffix(strings.ToLower(filename), ".rar") {
		return strings.TrimSuffix(filename, filepath.Ext(filename)), "rar"
	}

	rPattern := regexp.MustCompile(`^(.+)\.r(\d+)$`)
	if matches := rPattern.FindStringSubmatch(filename); len(matches) > 2 {
		return matches[1], "r" + matches[2]
	}

	return filename, ""
}

// groupRarFilesByArchive groups RAR files by their archive base name
func (proc *Processor) groupRarFilesByArchive(files []ParsedFile) map[string][]ParsedFile {
	rarArchives := make(map[string][]ParsedFile)

	for _, file := range files {
		if !file.IsRarArchive {
			continue
		}

		// Get the base archive name (remove part numbers and extensions)
		baseName := proc.getRarArchiveBaseName(file.Filename)
		if baseName == "" {
			baseName = file.Filename // Fallback to original filename
		}

		rarArchives[baseName] = append(rarArchives[baseName], file)
	}

	return rarArchives
}

// getRarArchiveBaseName extracts the base name of a RAR archive (without part numbers)
func (proc *Processor) getRarArchiveBaseName(filename string) string {
	// Handle patterns like movie.part001.rar, movie.part01.rar
	partPattern := regexp.MustCompile(`^(.+)\.part\d+\.rar$`)
	if matches := partPattern.FindStringSubmatch(filename); len(matches) > 1 {
		return matches[1]
	}

	// Handle patterns like movie.rar, movie.r00, movie.r01
	if strings.HasSuffix(strings.ToLower(filename), ".rar") {
		return strings.TrimSuffix(filename, filepath.Ext(filename))
	}

	rPattern := regexp.MustCompile(`^(.+)\.r\d+$`)
	if matches := rPattern.FindStringSubmatch(filename); len(matches) > 1 {
		return matches[1]
	}

	// Fallback: remove common RAR suffixes
	rarDirPattern := regexp.MustCompile(`\.(part\d+|r\d+)$`)
	baseName := strings.TrimSuffix(filename, filepath.Ext(filename))
	return rarDirPattern.ReplaceAllString(baseName, "")
}

// sortRarFiles sorts RAR files in the correct order (first part first)
func (proc *Processor) sortRarFiles(files []ParsedFile) []ParsedFile {
	sorted := make([]ParsedFile, len(files))
	copy(sorted, files)

	sort.Slice(sorted, func(i, j int) bool {
		return proc.compareRarFilenames(sorted[i].Filename, sorted[j].Filename)
	})

	return sorted
}

// compareRarFilenames compares RAR filenames for proper sorting
func (proc *Processor) compareRarFilenames(a, b string) bool {
	// Extract base names and extensions
	aBase, aExt := proc.splitRarFilename(a)
	bBase, bExt := proc.splitRarFilename(b)

	// If different base names, use lexical order
	if aBase != bBase {
		return aBase < bBase
	}

	// Same base name, sort by extension/part number
	aNum := proc.extractRarPartNumber(aExt)
	bNum := proc.extractRarPartNumber(bExt)

	return aNum < bNum
}

// splitRarFilename splits a RAR filename into base and extension parts
func (proc *Processor) splitRarFilename(filename string) (base, ext string) {
	// Handle patterns like .part001.rar, .part01.rar
	partPattern := regexp.MustCompile(`^(.+)\.part\d+\.rar$`)
	if matches := partPattern.FindStringSubmatch(filename); len(matches) > 1 {
		return matches[1], strings.TrimPrefix(filename, matches[1]+".")
	}

	// Handle patterns like .rar, .r00, .r01
	if strings.HasSuffix(strings.ToLower(filename), ".rar") {
		return strings.TrimSuffix(filename, filepath.Ext(filename)), "rar"
	}

	rPattern := regexp.MustCompile(`^(.+)\.r(\d+)$`)
	if matches := rPattern.FindStringSubmatch(filename); len(matches) > 2 {
		return matches[1], "r" + matches[2]
	}

	return filename, ""
}

// extractRarPartNumber extracts numeric part from RAR extension for sorting
func (proc *Processor) extractRarPartNumber(ext string) int {
	// .rar is always first (part 0)
	if ext == "rar" {
		return 0
	}

	// Extract number from .r00, .r01, etc.
	rPattern := regexp.MustCompile(`^r(\d+)$`)
	if matches := rPattern.FindStringSubmatch(ext); len(matches) > 1 {
		if num := proc.parseInt(matches[1]); num >= 0 {
			return num + 1 // .r00 becomes 1, .r01 becomes 2, etc.
		}
	}

	// Extract number from .part001.rar, .part01.rar, etc.
	partPattern := regexp.MustCompile(`^part(\d+)\.rar$`)
	if matches := partPattern.FindStringSubmatch(ext); len(matches) > 1 {
		if num := proc.parseInt(matches[1]); num >= 0 {
			return num
		}
	}

	return 999999 // Unknown format goes last
}

// parseInt safely converts string to int
func (proc *Processor) parseInt(s string) int {
	num := 0
	for _, r := range s {
		if r >= '0' && r <= '9' {
			num = num*10 + int(r-'0')
		} else {
			return -1
		}
	}
	return num
}

// createRarPartNzbFiles creates individual NZB file records for each RAR part
func (proc *Processor) createRarPartNzbFiles(repo *database.Repository, parentNzbFile *database.NzbFile, archiveName string, archiveFiles []ParsedFile) error {
	// First, mark the parent NZB file as main type
	parentNzbFile.PartType = database.NzbPartTypeMain

	// Sort archive files to ensure proper order
	sortedFiles := proc.sortRarFiles(archiveFiles)

	for _, rarFile := range sortedFiles {
		// Create unique path for each RAR part by appending the part filename
		// Format: "/path/to/parent.nzb#movie.part01.rar"
		// This ensures UNIQUE constraint compliance while maintaining parent relationship
		rarPartPath := fmt.Sprintf("%s#%s", parentNzbFile.Path, rarFile.Filename)

		// Create an NZB file record for this RAR part
		rarPartNzbFile := &database.NzbFile{
			Path:           rarPartPath,                  // Unique path for this RAR part
			Filename:       rarFile.Filename,             // Actual RAR part filename (movie.rar, movie.r00, etc.)
			Size:           rarFile.Size,                 // Size of this specific part
			NzbType:        parentNzbFile.NzbType,        // Same type as parent
			SegmentsCount:  len(rarFile.Segments),        // Number of segments for this part
			SegmentsData:   rarFile.Segments,             // Only segments for this specific part
			SegmentSize:    parentNzbFile.SegmentSize,    // Use parent's segment size
			RclonePassword: parentNzbFile.RclonePassword, // Inherit encryption settings
			RcloneSalt:     parentNzbFile.RcloneSalt,     // Inherit encryption settings
			ParentNzbID:    &parentNzbFile.ID,            // Link to parent NZB
			PartType:       database.NzbPartTypeRarPart,  // Mark as RAR part
			ArchiveName:    &archiveName,                 // Archive group name
		}

		// Create the RAR part NZB file record
		err := repo.CreateRarPartNzbFile(rarPartNzbFile)
		if err != nil {
			return fmt.Errorf("failed to create RAR part NZB file for %s: %w", rarFile.Filename, err)
		}
	}

	return nil
}
