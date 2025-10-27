package steps

import (
	"context"
	"strings"

	"github.com/javi11/altmount/internal/importer/parser"
)

// SeparateFilesStep separates files by type (regular, archive, PAR2)
type SeparateFilesStep struct {
	fileType parser.NzbType
}

// NewSeparateFilesStep creates a step to separate files by type
func NewSeparateFilesStep(fileType parser.NzbType) *SeparateFilesStep {
	return &SeparateFilesStep{fileType: fileType}
}

// Execute separates files based on the NZB type
func (s *SeparateFilesStep) Execute(ctx context.Context, pctx *ProcessingContext) error {
	switch s.fileType {
	case parser.NzbTypeRarArchive:
		pctx.RegularFiles, pctx.ArchiveFiles = SeparateRarFiles(pctx.Parsed.Files)
		// Also filter out PAR2 files from regular files
		pctx.RegularFiles, pctx.Par2Files = SeparatePar2Files(pctx.RegularFiles)

	case parser.NzbType7zArchive:
		pctx.RegularFiles, pctx.ArchiveFiles = Separate7zFiles(pctx.Parsed.Files)
		// Also filter out PAR2 files from regular files
		pctx.RegularFiles, pctx.Par2Files = SeparatePar2Files(pctx.RegularFiles)

	default:
		// For single file and multi-file types, just separate PAR2 files
		pctx.RegularFiles, pctx.Par2Files = SeparatePar2Files(pctx.Parsed.Files)
	}

	return nil
}

// Name returns the step name
func (s *SeparateFilesStep) Name() string {
	return "SeparateFiles"
}

// IsPar2File checks if a filename is a PAR2 repair file
func IsPar2File(filename string) bool {
	lower := strings.ToLower(filename)
	return strings.HasSuffix(lower, ".par2")
}

// SeparatePar2Files separates PAR2 files from regular files
func SeparatePar2Files(files []parser.ParsedFile) ([]parser.ParsedFile, []parser.ParsedFile) {
	var regularFiles []parser.ParsedFile
	var par2Files []parser.ParsedFile

	for _, file := range files {
		if IsPar2File(file.Filename) {
			par2Files = append(par2Files, file)
		} else {
			regularFiles = append(regularFiles, file)
		}
	}

	return regularFiles, par2Files
}

// SeparateRarFiles separates RAR files from regular files
func SeparateRarFiles(files []parser.ParsedFile) ([]parser.ParsedFile, []parser.ParsedFile) {
	var regularFiles []parser.ParsedFile
	var rarFiles []parser.ParsedFile

	for _, file := range files {
		if file.IsRarArchive {
			rarFiles = append(rarFiles, file)
		} else {
			regularFiles = append(regularFiles, file)
		}
	}

	return regularFiles, rarFiles
}

// Separate7zFiles separates 7zip files from regular files
func Separate7zFiles(files []parser.ParsedFile) ([]parser.ParsedFile, []parser.ParsedFile) {
	var regularFiles []parser.ParsedFile
	var sevenZipFiles []parser.ParsedFile

	for _, file := range files {
		if file.Is7zArchive {
			sevenZipFiles = append(sevenZipFiles, file)
		} else {
			regularFiles = append(regularFiles, file)
		}
	}

	return regularFiles, sevenZipFiles
}
