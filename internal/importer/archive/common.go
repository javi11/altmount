package archive

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	metapb "github.com/javi11/altmount/internal/metadata/proto"
)

// ParseInt safely converts string to int
func ParseInt(s string) int {
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

// HasExtension checks if a filename has an extension
func HasExtension(filename string) bool {
	return filepath.Ext(filename) != ""
}

var (
	// ErrNoAllowedFiles indicates that the archive contains no files matching allowed extensions
	ErrNoAllowedFiles = errors.New("archive contains no files with allowed extensions")
	// ErrNoFilesProcessed indicates that no files were successfully processed (all files failed validation)
	ErrNoFilesProcessed = errors.New("no files were successfully processed (all files failed validation)")
)

// NestedSource represents one inner archive volume's contribution to a nested file.
// Used when a file is inside an inner archive that is itself inside an outer archive.
type NestedSource struct {
	Segments        []*metapb.SegmentData // Outer archive segments covering this inner volume
	AesKey          []byte                // Outer AES key (empty if unencrypted)
	AesIV           []byte                // Outer AES IV
	InnerOffset     int64                 // Offset within decrypted inner volume where file data starts
	InnerLength     int64                 // Bytes of target file from this source
	InnerVolumeSize int64                 // Total decrypted size of inner volume (for AES cipher)
}

// Content represents a file within an archive for processing
type Content struct {
	InternalPath  string                `json:"internal_path"`
	Filename      string                `json:"filename"`
	Size          int64                 `json:"size"`                     // Uncompressed size (for file metadata)
	PackedSize    int64                 `json:"packed_size"`              // Compressed size in archive (for segment validation)
	Segments      []*metapb.SegmentData `json:"segments"`                 // Segment data for this file
	IsDirectory   bool                  `json:"is_directory,omitempty"`   // Indicates if this is a directory
	AesKey        []byte                `json:"aes_key,omitempty"`        // AES encryption key (if encrypted)
	AesIV         []byte                `json:"aes_iv,omitempty"`         // AES initialization vector (if encrypted)
	NzbdavID      string                `json:"nzbdav_id,omitempty"`      // Original ID from nzbdav
	NestedSources []NestedSource        `json:"nested_sources,omitempty"` // Nested archive sources (encrypted outer)
	// ISOExpansionIndex is non-zero for files expanded from an ISO archive.
	// It is the 1-based position of this file when all ISO files in the archive
	// are sorted by size descending (1 = largest / main feature).
	// Zero means this Content did not come from an ISO.
	ISOExpansionIndex int `json:"iso_expansion_index,omitempty"`
}

// GetContentSegmentCount returns the total number of segments for a Content,
// counting NestedSources segments for encrypted nested archive content.
func GetContentSegmentCount(content Content) int {
	if len(content.NestedSources) > 0 {
		total := 0
		for _, ns := range content.NestedSources {
			total += len(ns.Segments)
		}
		return total
	}
	return len(content.Segments)
}

// GetContentSegments returns all segments for a Content,
// collecting from NestedSources for encrypted nested archive content.
func GetContentSegments(content Content) []*metapb.SegmentData {
	if len(content.NestedSources) > 0 {
		var all []*metapb.SegmentData
		for _, ns := range content.NestedSources {
			all = append(all, ns.Segments...)
		}
		return all
	}
	return content.Segments
}

// ValidateSegmentIntegrity checks if the segments provided for a file actually cover the expected size.
// Returns an error if segment coverage is significantly lower than expected (1% shortfall threshold).
func ValidateSegmentIntegrity(ctx context.Context, content Content) error {
	const shortfallThresholdPercent = 1 // Fail if more than 1% of the file is missing

	if len(content.NestedSources) > 0 {
		// For nested sources, validate each source independently
		for _, ns := range content.NestedSources {
			var covered int64
			for _, seg := range ns.Segments {
				covered += (seg.EndOffset - seg.StartOffset + 1)
			}

			shortfall := ns.InnerLength - covered
			if shortfall > 0 {
				shortfallPercent := (shortfall * 100) / ns.InnerLength
				if shortfallPercent >= shortfallThresholdPercent {
					return fmt.Errorf("corrupted nested source: missing %d bytes (%d%% of part)", shortfall, shortfallPercent)
				}
			}
		}
	} else {
		// For standard files, validate total segment coverage against PackedSize (if available)
		var totalCovered int64
		for _, seg := range content.Segments {
			totalCovered += (seg.EndOffset - seg.StartOffset + 1)
		}

		expectedSize := content.PackedSize
		if expectedSize <= 0 {
			expectedSize = content.Size
		}

		if expectedSize > 0 {
			shortfall := expectedSize - totalCovered
			if shortfall > 0 {
				shortfallPercent := (shortfall * 100) / expectedSize
				if shortfallPercent >= shortfallThresholdPercent {
					return fmt.Errorf("corrupted file: missing %d bytes (%d%% of total size)", shortfall, shortfallPercent)
				}
			}
		}
	}

	return nil
}
