package bluray

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"path/filepath"
	"sort"
)

// StreamInfo represents information about a single video stream in the Blu-ray
type StreamInfo struct {
	StreamPath    string // Full path to .m2ts file (e.g., "BDMV/STREAM/00000.m2ts")
	StreamName    string // Base name (e.g., "00000")
	ClipInfoPath  string // Path to corresponding .clpi file
	Size          int64  // File size in bytes
	DisplayName   string // Human-readable name for the stream
}

// StreamsBySize is a sortable slice of StreamInfo by size (descending)
type StreamsBySize []StreamInfo

func (s StreamsBySize) Len() int           { return len(s) }
func (s StreamsBySize) Less(i, j int) bool { return s[i].Size > s[j].Size } // Descending order
func (s StreamsBySize) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }

// ParseBlurayStreams analyzes the BDMV structure and returns a list of significant video streams
// It prioritizes larger files as they typically contain the main feature
func ParseBlurayStreams(ctx context.Context, structure *BDMVStructure, fileSystem fs.FS) ([]StreamInfo, error) {
	if structure == nil {
		return nil, fmt.Errorf("BDMV structure is nil")
	}

	if len(structure.StreamFiles) == 0 {
		return nil, fmt.Errorf("no stream files found in BDMV structure")
	}

	streams := make([]StreamInfo, 0, len(structure.StreamFiles))

	// Collect stream information
	for _, streamPath := range structure.StreamFiles {
		streamInfo := StreamInfo{
			StreamPath:   streamPath,
			StreamName:   GetStreamBaseName(streamPath),
			ClipInfoPath: GetCorrespondingClipInfo(streamPath, structure.ClipFiles),
		}

		// Get file size
		fileInfo, err := fs.Stat(fileSystem, streamPath)
		if err != nil {
			slog.WarnContext(ctx, "Failed to stat stream file, skipping",
				"stream_path", streamPath,
				"error", err)
			continue
		}

		streamInfo.Size = fileInfo.Size()

		// Generate display name
		streamInfo.DisplayName = generateDisplayName(streamInfo, len(streams))

		streams = append(streams, streamInfo)
	}

	if len(streams) == 0 {
		return nil, fmt.Errorf("no valid streams found after analysis")
	}

	// Sort streams by size (largest first)
	sort.Sort(StreamsBySize(streams))

	// Filter to only include significant streams (> 100MB)
	// This helps exclude small clips like trailers, menus, etc.
	const minStreamSize = 100 * 1024 * 1024 // 100 MB

	significantStreams := make([]StreamInfo, 0)
	for _, stream := range streams {
		if stream.Size >= minStreamSize {
			significantStreams = append(significantStreams, stream)
		}
	}

	// If no significant streams found, return the largest one anyway
	if len(significantStreams) == 0 {
		slog.InfoContext(ctx, "No streams over minimum size, including largest stream",
			"min_size_mb", minStreamSize/(1024*1024),
			"largest_stream", streams[0].StreamName,
			"size_mb", streams[0].Size/(1024*1024))
		significantStreams = []StreamInfo{streams[0]}
	}

	// Re-label display names based on filtered list
	for i := range significantStreams {
		significantStreams[i].DisplayName = generateDisplayNameFromRank(i, len(significantStreams))
	}

	slog.InfoContext(ctx, "Parsed Blu-ray streams",
		"total_streams", len(streams),
		"significant_streams", len(significantStreams))

	return significantStreams, nil
}

// generateDisplayName creates a human-readable name for a stream
func generateDisplayName(stream StreamInfo, index int) string {
	sizeMB := stream.Size / (1024 * 1024)
	return fmt.Sprintf("Stream %s (%d MB)", stream.StreamName, sizeMB)
}

// generateDisplayNameFromRank creates a display name based on the stream's rank
func generateDisplayNameFromRank(rank, total int) string {
	if total == 1 {
		return "Main Feature"
	}

	if rank == 0 {
		return "Main Feature"
	} else if rank == 1 {
		return "Feature 2"
	} else if rank == 2 {
		return "Feature 3"
	}

	return fmt.Sprintf("Feature %d", rank+1)
}

// FilterMainFeature returns only the largest stream (typically the main feature film)
func FilterMainFeature(streams []StreamInfo) []StreamInfo {
	if len(streams) == 0 {
		return streams
	}

	// Streams should already be sorted by size, so return the first one
	return []StreamInfo{streams[0]}
}

// GetStreamsByMinSize returns streams that are at least minSizeBytes in size
func GetStreamsByMinSize(streams []StreamInfo, minSizeBytes int64) []StreamInfo {
	filtered := make([]StreamInfo, 0)
	for _, stream := range streams {
		if stream.Size >= minSizeBytes {
			filtered = append(filtered, stream)
		}
	}
	return filtered
}

// GetRelativePath returns the path of the stream relative to the BDMV root
func (si *StreamInfo) GetRelativePath() string {
	return filepath.ToSlash(si.StreamPath)
}

// GetSafeFilename returns a filesystem-safe filename for the stream
func (si *StreamInfo) GetSafeFilename() string {
	// Replace spaces and special characters in display name
	safeName := si.DisplayName
	safeName = filepath.Clean(safeName)
	return fmt.Sprintf("%s.m2ts", safeName)
}
