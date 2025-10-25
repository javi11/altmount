package importer

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/javi11/altmount/internal/encryption/rclone"
	"github.com/javi11/altmount/internal/metadata"
	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/javi11/altmount/internal/pool"
	concpool "github.com/sourcegraph/conc/pool"
)

// Processor handles the processing and storage of parsed NZB files using metadata storage
type Processor struct {
	parser                  *Parser
	strmParser              *StrmParser
	metadataService         *metadata.MetadataService
	rarProcessor            RarProcessor
	sevenZipProcessor       SevenZipProcessor
	poolManager             pool.Manager // Pool manager for dynamic pool access
	maxValidationGoroutines int          // Maximum concurrent goroutines for segment validation
	fullSegmentValidation   bool         // Whether to validate all segments or just a random sample
	log                     *slog.Logger

	// Pre-compiled regex patterns for RAR file sorting
	rarPartPattern    *regexp.Regexp // pattern.part###.rar
	rarRPattern       *regexp.Regexp // pattern.r### or pattern.r##
	rarNumericPattern *regexp.Regexp // pattern.### (numeric extensions)
}

// NewProcessor creates a new NZB processor using metadata storage
func NewProcessor(metadataService *metadata.MetadataService, poolManager pool.Manager, maxValidationGoroutines int, fullSegmentValidation bool) *Processor {
	return &Processor{
		parser:                  NewParser(poolManager),
		strmParser:              NewStrmParser(),
		metadataService:         metadataService,
		rarProcessor:            NewRarProcessor(poolManager, 10, 64),      // 10 max workers, 64MB cache for RAR analysis
		sevenZipProcessor:       NewSevenZipProcessor(poolManager, 10, 64), // 10 max workers, 64MB cache for 7zip analysis
		poolManager:             poolManager,
		maxValidationGoroutines: maxValidationGoroutines,
		fullSegmentValidation:   fullSegmentValidation,
		log:                     slog.Default().With("component", "nzb-processor"),

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

	// Initialize batch tracking map for this import
	// Tracks all files created in this import to handle collisions correctly
	currentBatchFiles := make(map[string]bool)

	proc.log.Info("Processing file",
		"file_path", filePath,
		"virtual_dir", virtualDir,
		"type", parsed.Type,
		"total_size", parsed.TotalSize,
		"files", len(parsed.Files))

	// Process based on file type
	switch parsed.Type {
	case NzbTypeSingleFile:
		return proc.processSingleFileWithDir(parsed, virtualDir, currentBatchFiles)
	case NzbTypeMultiFile:
		return proc.processMultiFileWithDir(parsed, virtualDir, currentBatchFiles)
	case NzbTypeRarArchive:
		return proc.processRarArchiveWithDir(parsed, virtualDir, currentBatchFiles)
	case NzbType7zArchive:
		return proc.process7zArchiveWithDir(parsed, virtualDir, currentBatchFiles)
	case NzbTypeStrm:
		return proc.processStrmFileWithDir(parsed, virtualDir, currentBatchFiles)
	default:
		return "", NewNonRetryableError(fmt.Sprintf("unknown file type: %s", parsed.Type), nil)
	}
}

// processSingleFileWithDir handles NZBs with a single file in a specific virtual directory
func (proc *Processor) processSingleFileWithDir(parsed *ParsedNzb, virtualDir string, currentBatchFiles map[string]bool) (string, error) {
	regularFiles, _ := proc.separatePar2Files(parsed.Files)

	file := regularFiles[0] // Single file NZB, take the first regular file

	// Create the directory structure if needed
	if err := proc.ensureDirectoryExists(virtualDir); err != nil {
		return "", fmt.Errorf("failed to create directory structure: %w", err)
	}

	// Handle potential filename collisions
	uniqueFilename := proc.getUniqueFilename(virtualDir, file.Filename, currentBatchFiles)

	// Create virtual file path with potentially adjusted filename
	virtualFilePath := filepath.Join(virtualDir, uniqueFilename)
	virtualFilePath = strings.ReplaceAll(virtualFilePath, string(filepath.Separator), "/")

	// Track this file in current batch
	currentBatchFiles[virtualFilePath] = true

	// Validate segments comprehensively (size, structure, and reachability)
	if err := proc.validateSegments(file.Filename, file.Size, file.Segments, file.Encryption); err != nil {
		return "", NewNonRetryableError(err.Error(), nil)
	}

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
func (proc *Processor) processMultiFileWithDir(parsed *ParsedNzb, virtualDir string, currentBatchFiles map[string]bool) (string, error) {
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

		// Handle potential filename collisions
		uniqueFilename := proc.getUniqueFilename(parentPath, filename, currentBatchFiles)

		// Create virtual file path with potentially adjusted filename
		virtualPath := filepath.Join(parentPath, uniqueFilename)
		virtualPath = strings.ReplaceAll(virtualPath, string(filepath.Separator), "/")

		// Track this file in current batch
		currentBatchFiles[virtualPath] = true

		// Validate segments comprehensively (size, structure, and reachability)
		if err := proc.validateSegments(filename, file.Size, file.Segments, file.Encryption); err != nil {
			return "", NewNonRetryableError(err.Error(), nil)
		}

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
func (proc *Processor) processRarArchiveWithDir(parsed *ParsedNzb, virtualDir string, currentBatchFiles map[string]bool) (string, error) {
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

			// Handle potential filename collisions
			uniqueFilename := proc.getUniqueFilename(parentPath, filename, currentBatchFiles)

			// Create virtual file path with potentially adjusted filename
			virtualPath := filepath.Join(parentPath, uniqueFilename)
			virtualPath = strings.ReplaceAll(virtualPath, string(filepath.Separator), "/")

			// Track this file in current batch
			currentBatchFiles[virtualPath] = true

			// Validate segments comprehensively (size, structure, and reachability)
			if err := proc.validateSegments(filename, file.Size, file.Segments, file.Encryption); err != nil {
				return "", NewNonRetryableError(err.Error(), nil)
			}

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
		// Use the nzbVirtualDir directly to avoid double nesting
		// All RAR contents will be flattened into this directory
		rarDirPath := nzbVirtualDir

		// Ensure RAR archive directory exists
		if err := proc.ensureDirectoryExists(rarDirPath); err != nil {
			return "", fmt.Errorf("failed to create RAR directory %s: %w", rarDirPath, err)
		}

		proc.log.Info("Processing RAR archive with content analysis",
			"archive", filepath.Base(rarDirPath),
			"parts", len(rarFiles),
			"rar_dir", rarDirPath)

		// Analyze RAR content using the new RAR handler with timeout
		// Use a generous timeout for large RAR archives
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		rarContents, err := proc.rarProcessor.AnalyzeRarContentFromNzb(ctx, rarFiles)
		if err != nil {
			proc.log.Error("Failed to analyze RAR archive content",
				"archive", nzbBaseName,
				"error", err)

			return "", err
		}

		proc.log.Info("Successfully analyzed RAR archive content",
			"archive", filepath.Base(rarDirPath),
			"files_in_archive", len(rarContents))

		// Process each file found in the RAR archive
		for _, rarContent := range rarContents {
			// Skip directories
			if rarContent.IsDirectory {
				proc.log.Debug("Skipping directory in RAR archive", "path", rarContent.InternalPath)
				continue
			}

			// Flatten the internal path by extracting only the base filename
			// Normalize backslashes first (Windows-style paths in RAR archives)
			normalizedInternalPath := strings.ReplaceAll(rarContent.InternalPath, "\\", "/")
			baseFilename := filepath.Base(normalizedInternalPath)

			// Generate a unique filename to handle duplicates
			uniqueFilename := proc.getUniqueFilename(rarDirPath, baseFilename, currentBatchFiles)

			// Create the virtual file path directly in the RAR directory (flattened)
			virtualFilePath := filepath.Join(rarDirPath, uniqueFilename)
			virtualFilePath = strings.ReplaceAll(virtualFilePath, string(filepath.Separator), "/")

			// Track this file in current batch
			currentBatchFiles[virtualFilePath] = true

			// Validate segments comprehensively (size, structure, and reachability)
			if err := proc.validateSegments(baseFilename, rarContent.Size, rarContent.Segments, metapb.Encryption_NONE); err != nil {
				return "", NewNonRetryableError(err.Error(), nil)
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
				"file", uniqueFilename,
				"original_internal_path", rarContent.InternalPath,
				"virtual_path", virtualFilePath,
				"size", rarContent.Size,
				"segments", len(rarContent.Segments))
		}

		proc.log.Info("Successfully processed RAR archive with content analysis",
			"archive", filepath.Base(rarDirPath),
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
	// Normalize backslashes to forward slashes (Windows-style paths in NZB/RAR files)
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

// analyzeDirectoryStructureWithBase analyzes files to determine directory structure within a base directory
func (proc *Processor) analyzeDirectoryStructureWithBase(files []ParsedFile, baseDir string) *DirectoryStructure {
	// Simple implementation: group files by common prefixes in their filenames within the base directory
	pathMap := make(map[string]bool)

	for _, file := range files {
		// Normalize backslashes to forward slashes (Windows-style paths in NZB/RAR files)
		normalizedFilename := strings.ReplaceAll(file.Filename, "\\", "/")
		
		dir := filepath.Dir(normalizedFilename)
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

// getUniqueFilename generates a unique filename handling two types of collisions:
// 1. Within-batch collision (file from current import): Add suffix (_1, _2, etc.)
// 2. Cross-batch collision (file from previous import): Override by deleting old metadata
func (proc *Processor) getUniqueFilename(basePath, filename string, currentBatchFiles map[string]bool) string {
	// Start with the original filename
	candidatePath := filepath.Join(basePath, filename)
	candidatePath = strings.ReplaceAll(candidatePath, string(filepath.Separator), "/")

	// Check if this path collides with a file from the current batch
	if currentBatchFiles[candidatePath] {
		// Within-batch collision: Add suffix to keep both files
		proc.log.Debug("Within-batch collision detected, adding suffix",
			"path", candidatePath,
			"original_filename", filename)

		counter := 1
		candidateFilename := filename
		ext := filepath.Ext(filename)
		nameWithoutExt := strings.TrimSuffix(filename, ext)

		// Find next available suffix
		for {
			candidateFilename = fmt.Sprintf("%s_%d%s", nameWithoutExt, counter, ext)
			candidatePath = filepath.Join(basePath, candidateFilename)
			candidatePath = strings.ReplaceAll(candidatePath, string(filepath.Separator), "/")

			// Check if this suffixed path is also in current batch or exists on disk
			if !currentBatchFiles[candidatePath] {
				metadataPath := proc.metadataService.GetMetadataFilePath(candidatePath)
				if _, err := os.Stat(metadataPath); os.IsNotExist(err) {
					// Path is available
					return candidateFilename
				}
			}
			counter++
		}
	}

	// Check if metadata file exists from a previous import
	metadataPath := proc.metadataService.GetMetadataFilePath(candidatePath)
	if _, err := os.Stat(metadataPath); err == nil {
		// Cross-batch collision: Override by deleting old metadata
		proc.log.Info("Cross-batch collision detected, overriding existing file",
			"path", candidatePath,
			"old_metadata_path", metadataPath)

		if err := proc.metadataService.DeleteFileMetadata(candidatePath); err != nil {
			proc.log.Warn("Failed to delete old metadata during override",
				"path", candidatePath,
				"error", err)
		}
	}

	// No collision or handled cross-batch collision, use original filename
	return filename
}

// Helper function to create string pointer
func stringPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// validateSegments performs comprehensive validation of file segments including size verification
// and reachability checks. It validates that segments are structurally sound, accessible via
// the Usenet connection pool, and that their total size matches the expected file size (accounting
// for encryption overhead). This replaces the previous separate validateSegmentSizes and
// validateSegmentsReachable functions for better performance and consistency.
func (proc *Processor) validateSegments(filename string, fileSize int64, segments []*metapb.SegmentData, encryption metapb.Encryption) error {
	if len(segments) == 0 {
		return fmt.Errorf("no segments provided for file %s", filename)
	}

	// First, verify that the connection pool is available
	// This ensures we can actually fetch segment data when needed
	usenetPool, err := proc.poolManager.GetPool()
	if err != nil {
		return fmt.Errorf("cannot write metadata for %s: usenet connection pool unavailable: %w", filename, err)
	}

	if usenetPool == nil {
		return fmt.Errorf("cannot write metadata for %s: usenet connection pool is nil", filename)
	}

	// First loop: Calculate total size from ALL segments
	// This validates file completeness regardless of sampling mode
	totalSegmentSize := int64(0)
	for i, segment := range segments {
		if segment == nil {
			return fmt.Errorf("segment %d is nil for file %s", i, filename)
		}

		// Validate segment has valid offsets
		if segment.StartOffset < 0 || segment.EndOffset < 0 {
			return fmt.Errorf("invalid offsets (start=%d, end=%d) in segment %d for file %s",
				segment.StartOffset, segment.EndOffset, i, filename)
		}

		if segment.StartOffset > segment.EndOffset {
			return fmt.Errorf("start offset greater than end offset (start=%d, end=%d) in segment %d for file %s",
				segment.StartOffset, segment.EndOffset, i, filename)
		}

		// Calculate segment size
		segSize := segment.EndOffset - segment.StartOffset + 1
		if segSize <= 0 {
			return fmt.Errorf("non-positive size %d in segment %d for file %s", segSize, i, filename)
		}

		// Validate segment has a valid Usenet message ID
		if segment.Id == "" {
			return fmt.Errorf("empty message ID in segment %d for file %s (cannot retrieve data)", i, filename)
		}

		// Accumulate total size from all segments
		totalSegmentSize += segSize
	}

	// Determine which segments to validate for reachability
	var segmentsToValidate []*metapb.SegmentData
	if proc.fullSegmentValidation {
		// Validate all segments for reachability
		segmentsToValidate = segments
	} else {
		// Validate a random sample of up to 10 segments for reachability
		sampleSize := 10
		if len(segments) < sampleSize {
			sampleSize = len(segments)
		}

		// Create a random sample
		segmentsToValidate = make([]*metapb.SegmentData, sampleSize)
		if sampleSize == len(segments) {
			// If we're validating all anyway, just use the original slice
			segmentsToValidate = segments
		} else {
			// Random sampling without replacement
			perm := rand.Perm(len(segments))
			for i := 0; i < sampleSize; i++ {
				segmentsToValidate[i] = segments[perm[i]]
			}
		}
	}

	// Second loop: Validate reachability of sampled segments only
	pl := concpool.New().WithErrors().WithFirstError().WithMaxGoroutines(proc.maxValidationGoroutines)
	for _, segment := range segmentsToValidate {
		pl.Go(func() error {
			// Create a context with timeout for the validation
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			// Check if the segment is reachable via pool.Stat()
			// This verifies that the message ID exists on the Usenet server
			_, err := usenetPool.Stat(ctx, segment.Id, []string{})
			if err != nil {
				return fmt.Errorf("segment with ID %s unreachable for file %s: %w", segment.Id, filename, err)
			}

			return nil
		})
	}

	if err := pl.Wait(); err != nil {
		return err
	}

	// For encrypted files, segments contain encrypted data, so we need to convert
	// the decrypted file size to encrypted size for comparison
	expectedSize := fileSize
	if encryption == metapb.Encryption_RCLONE {
		expectedSize = rclone.EncryptedSize(fileSize)
	}

	if totalSegmentSize != expectedSize {
		sizeType := "decrypted"
		if encryption == metapb.Encryption_RCLONE {
			sizeType = "encrypted"
		}

		return fmt.Errorf("file '%s' is incomplete: expected %d bytes (%s) but found %d bytes (missing %d bytes)",
			filename, expectedSize, sizeType, totalSegmentSize, expectedSize-totalSegmentSize)
	}

	if proc.fullSegmentValidation {
		proc.log.Debug("All segments validated successfully",
			"file", filename,
			"segment_count", len(segments),
			"total_size", totalSegmentSize)
	} else {
		proc.log.Debug("Random sample of segments validated successfully",
			"file", filename,
			"total_segments", len(segments),
			"validated_segments", len(segmentsToValidate),
			"total_size", totalSegmentSize)
	}

	return nil
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

// separate7zFiles separates 7zip files from regular files
func (proc *Processor) separate7zFiles(files []ParsedFile) ([]ParsedFile, []ParsedFile) {
	var regularFiles []ParsedFile
	var sevenZipFiles []ParsedFile

	for _, file := range files {
		if file.Is7zArchive {
			sevenZipFiles = append(sevenZipFiles, file)
		} else {
			regularFiles = append(regularFiles, file)
		}
	}

	return regularFiles, sevenZipFiles
}

// process7zArchiveWithDir handles NZBs containing 7zip archives and regular files in a specific virtual directory
func (proc *Processor) process7zArchiveWithDir(parsed *ParsedNzb, virtualDir string, currentBatchFiles map[string]bool) (string, error) {
	// Create a folder named after the NZB file for multi-file imports
	nzbBaseName := strings.TrimSuffix(parsed.Filename, filepath.Ext(parsed.Filename))
	nzbVirtualDir := filepath.Join(virtualDir, nzbBaseName)
	nzbVirtualDir = strings.ReplaceAll(nzbVirtualDir, string(filepath.Separator), "/")

	// Separate 7zip files from regular files
	regularFiles, sevenZipFiles := proc.separate7zFiles(parsed.Files)

	// Filter out PAR2 files from regular files
	regularFiles, _ = proc.separatePar2Files(regularFiles)

	// Process regular files first (non-7z files like MKV, MP4, etc.)
	if len(regularFiles) > 0 {
		proc.log.Info("Processing regular files in 7zip archive NZB",
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

			// Handle potential filename collisions
			uniqueFilename := proc.getUniqueFilename(parentPath, filename, currentBatchFiles)

			// Create virtual file path with potentially adjusted filename
			virtualPath := filepath.Join(parentPath, uniqueFilename)
			virtualPath = strings.ReplaceAll(virtualPath, string(filepath.Separator), "/")

			// Track this file in current batch
			currentBatchFiles[virtualPath] = true

			// Validate segments comprehensively (size, structure, and reachability)
			if err := proc.validateSegments(filename, file.Size, file.Segments, file.Encryption); err != nil {
				return "", NewNonRetryableError(err.Error(), nil)
			}

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

	// Process 7zip archives if any exist
	if len(sevenZipFiles) > 0 {
		// Use the nzbVirtualDir directly to avoid double nesting
		// All 7zip contents will be flattened into this directory
		sevenZipDirPath := nzbVirtualDir

		// Ensure 7zip archive directory exists
		if err := proc.ensureDirectoryExists(sevenZipDirPath); err != nil {
			return "", fmt.Errorf("failed to create 7zip directory %s: %w", sevenZipDirPath, err)
		}

		proc.log.Info("Processing 7zip archive with content analysis",
			"archive", filepath.Base(sevenZipDirPath),
			"parts", len(sevenZipFiles),
			"7z_dir", sevenZipDirPath)

		// Analyze 7zip content using the 7zip handler with timeout
		// Use a generous timeout for large 7zip archives
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		sevenZipContents, err := proc.sevenZipProcessor.AnalyzeSevenZipContentFromNzb(ctx, sevenZipFiles)
		if err != nil {
			proc.log.Error("Failed to analyze 7zip archive content",
				"archive", nzbBaseName,
				"error", err)

			return "", err
		}

		proc.log.Info("Successfully analyzed 7zip archive content",
			"archive", filepath.Base(sevenZipDirPath),
			"files_in_archive", len(sevenZipContents))

		// Process each file found in the 7zip archive
		for _, sevenZipContent := range sevenZipContents {
			// Skip directories
			if sevenZipContent.IsDirectory {
				proc.log.Debug("Skipping directory in 7zip archive", "path", sevenZipContent.InternalPath)
				continue
			}

			// Flatten the internal path by extracting only the base filename
			// Normalize backslashes first (Windows-style paths in 7zip archives)
			normalizedInternalPath := strings.ReplaceAll(sevenZipContent.InternalPath, "\\", "/")
			baseFilename := filepath.Base(normalizedInternalPath)

			// Generate a unique filename to handle duplicates
			uniqueFilename := proc.getUniqueFilename(sevenZipDirPath, baseFilename, currentBatchFiles)

			// Create the virtual file path directly in the 7zip directory (flattened)
			virtualFilePath := filepath.Join(sevenZipDirPath, uniqueFilename)
			virtualFilePath = strings.ReplaceAll(virtualFilePath, string(filepath.Separator), "/")

			// Track this file in current batch
			currentBatchFiles[virtualFilePath] = true

			// Validate segments comprehensively (size, structure, and reachability)
			if err := proc.validateSegments(baseFilename, sevenZipContent.Size, sevenZipContent.Segments, metapb.Encryption_NONE); err != nil {
				return "", NewNonRetryableError(err.Error(), nil)
			}

			// Create file metadata using the 7zip handler's helper function
			fileMeta := proc.sevenZipProcessor.CreateFileMetadataFromSevenZipContent(
				sevenZipContent,
				parsed.Path,
			)

			// Write file metadata to disk
			if err := proc.metadataService.WriteFileMetadata(virtualFilePath, fileMeta); err != nil {
				return "", fmt.Errorf("failed to write metadata for 7zip file %s: %w", sevenZipContent.Filename, err)
			}

			proc.log.Debug("Created metadata for 7zip extracted file",
				"file", uniqueFilename,
				"original_internal_path", sevenZipContent.InternalPath,
				"virtual_path", virtualFilePath,
				"size", sevenZipContent.Size,
				"segments", len(sevenZipContent.Segments))
		}

		proc.log.Info("Successfully processed 7zip archive with content analysis",
			"archive", filepath.Base(sevenZipDirPath),
			"files_processed", len(sevenZipContents))
	}

	return nzbVirtualDir, nil
}

// processStrmFileWithDir handles STRM files (single file from NXG link) in a specific virtual directory
func (proc *Processor) processStrmFileWithDir(parsed *ParsedNzb, virtualDir string, currentBatchFiles map[string]bool) (string, error) {
	if len(parsed.Files) != 1 {
		return "", NewNonRetryableError(fmt.Sprintf("STRM file should contain exactly one file, got %d", len(parsed.Files)), nil)
	}

	file := parsed.Files[0]

	// Create the directory structure if needed
	if err := proc.ensureDirectoryExists(virtualDir); err != nil {
		return "", fmt.Errorf("failed to create directory structure: %w", err)
	}

	// Handle potential filename collisions
	uniqueFilename := proc.getUniqueFilename(virtualDir, file.Filename, currentBatchFiles)

	// Create virtual file path with potentially adjusted filename
	virtualFilePath := filepath.Join(virtualDir, uniqueFilename)
	virtualFilePath = strings.ReplaceAll(virtualFilePath, string(filepath.Separator), "/")

	// Track this file in current batch
	currentBatchFiles[virtualFilePath] = true

	// Validate segments comprehensively (size, structure, and reachability)
	if err := proc.validateSegments(file.Filename, file.Size, file.Segments, file.Encryption); err != nil {
		return "", NewNonRetryableError(err.Error(), nil)
	}

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
