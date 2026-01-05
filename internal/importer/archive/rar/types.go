package rar

import (
	"context"

	"github.com/javi11/altmount/internal/importer/parser"
	"github.com/javi11/altmount/internal/progress"
	metapb "github.com/javi11/altmount/internal/metadata/proto"
)

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

// Content represents a file within a RAR archive for processing
type Content struct {
	InternalPath string                `json:"internal_path"`
	Filename     string                `json:"filename"`
	Size         int64                 `json:"size"`                   // Uncompressed size (for file metadata)
	PackedSize   int64                 `json:"packed_size"`            // Compressed size in RAR (for segment validation)
	Segments     []*metapb.SegmentData `json:"segments"`               // Segment data for this file
	IsDirectory  bool                  `json:"is_directory,omitempty"` // Indicates if this is a directory
	AesKey       []byte                `json:"aes_key,omitempty"`      // AES encryption key (if encrypted)
	AesIV        []byte                `json:"aes_iv,omitempty"`       // AES initialization vector (if encrypted)
	NzbdavID     string                `json:"nzbdav_id,omitempty"`    // Original ID from nzbdav
}
