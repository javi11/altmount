package sevenzip

import (
	"context"

	"github.com/javi11/altmount/internal/importer/parser"
	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/javi11/altmount/internal/progress"
)

// Processor interface for analyzing 7zip content from NZB data
type Processor interface {
	// AnalyzeSevenZipContentFromNzb analyzes a 7zip archive directly from NZB data
	// without downloading. Returns an array of Content with file metadata and segments.
	// password parameter is used to unlock password-protected 7zip archives.
	// progressTracker is used to report progress during analysis.
	AnalyzeSevenZipContentFromNzb(ctx context.Context, sevenZipFiles []parser.ParsedFile, password string, progressTracker *progress.Tracker) ([]Content, error)
	// CreateFileMetadataFromSevenZipContent creates FileMetadata from Content for the metadata
	// system. This is used to convert Content into the protobuf format used by the metadata system.
	CreateFileMetadataFromSevenZipContent(content Content, sourceNzbPath string, releaseDate int64, nzbdavId string) *metapb.FileMetadata
}

// NestedSource represents one inner RAR volume's contribution to a nested file.
// Used when a file is inside an inner RAR that is itself inside an outer 7zip.
type NestedSource struct {
	Segments        []*metapb.SegmentData // Outer 7zip segments covering this inner volume
	AesKey          []byte                // Outer AES key (empty if unencrypted)
	AesIV           []byte                // Outer AES IV
	InnerOffset     int64                 // Offset within decrypted inner volume where file data starts
	InnerLength     int64                 // Bytes of target file from this source
	InnerVolumeSize int64                 // Total decrypted size of inner volume (for AES cipher)
}

// Content represents a file within a 7zip archive for processing
type Content struct {
	InternalPath  string                `json:"internal_path"`
	Filename      string                `json:"filename"`
	Size          int64                 `json:"size"`
	PackedSize    int64                 `json:"packed_size"`              // Packed/compressed size for nested content validation
	Segments      []*metapb.SegmentData `json:"segments"`                // Segment data for this file
	IsDirectory   bool                  `json:"is_directory,omitempty"`  // Indicates if this is a directory
	AesKey        []byte                `json:"aes_key,omitempty"`       // AES encryption key (if encrypted)
	AesIV         []byte                `json:"aes_iv,omitempty"`        // AES initialization vector (if encrypted)
	NzbdavID      string                `json:"nzbdav_id,omitempty"`     // Original ID from nzbdav
	NestedSources []NestedSource        `json:"nested_sources,omitempty"` // Nested RAR sources (encrypted outer 7z)
}
