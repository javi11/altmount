package rar

import (
	"context"

	"github.com/javi11/altmount/internal/importer/archive"
	"github.com/javi11/altmount/internal/importer/parser"
	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/javi11/altmount/internal/progress"
)

// Content is an alias for archive.Content
type Content = archive.Content

// NestedSource is an alias for archive.NestedSource
type NestedSource = archive.NestedSource

// Processor interface for analyzing RAR content from NZB data
type Processor interface {
	// AnalyzeRarContentFromNzb analyzes a RAR archive directly from NZB data
	// without downloading. Returns an array of Content with file metadata and segments.
	// password parameter is used to unlock password-protected RAR archives.
	// progressTracker is used to report progress during analysis.
	AnalyzeRarContentFromNzb(ctx context.Context, rarFiles []parser.ParsedFile, password string, progressTracker *progress.Tracker) ([]Content, error)
	// CreateFileMetadataFromRarContent creates FileMetadata from Content for the metadata
	// system. This is used to convert Content into the protobuf format used by the metadata system.
	CreateFileMetadataFromRarContent(content Content, sourceNzbPath string, releaseDate int64, nzbdavId string) *metapb.FileMetadata
}
