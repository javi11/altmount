package par2

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/javi11/altmount/internal/pool"
	"github.com/javi11/altmount/internal/usenet"
	"github.com/javi11/nzbparser"
)

// FirstSegmentData holds cached data from the first segment of an NZB file
// This is passed from the parser to avoid redundant fetches
type FirstSegmentData struct {
	File     *nzbparser.NzbFile
	RawBytes []byte // Up to 16KB for PAR2 detection
}

// GetFileDescriptors extracts file descriptors from PAR2 files in the NZB
// Similar to C# GetPar2FileDescriptorsStep.GetPar2FileDescriptors
// Uses cached first segment data and streams through the PAR2 file
func GetFileDescriptors(
	ctx context.Context,
	firstSegmentCache []*FirstSegmentData,
	poolManager pool.Manager,
) (map[[16]byte]*FileDescriptor, error) {
	descriptors := make(map[[16]byte]*FileDescriptor)

	if poolManager == nil || !poolManager.HasPool() {
		slog.DebugContext(ctx, "No pool manager available for PAR2 extraction")
		return descriptors, nil
	}

	// Find the PAR2 index file (smallest file with PAR2 magic bytes)
	// Similar to C# code: files.Where(x => HasPar2MagicBytes).MinBy(x => segments.Count)
	var par2IndexFile *nzbparser.NzbFile
	smallestSegmentCount := -1

	for _, cachedData := range firstSegmentCache {
		// Skip invalid entries
		if cachedData == nil || cachedData.File == nil || len(cachedData.File.Segments) == 0 {
			continue
		}

		// Check for PAR2 magic bytes using cached data
		if HasMagicBytes(cachedData.RawBytes) {
			segmentCount := len(cachedData.File.Segments)

			// Select the PAR2 file with the smallest segment count (likely the index file)
			if smallestSegmentCount == -1 || segmentCount < smallestSegmentCount {
				smallestSegmentCount = segmentCount
				par2IndexFile = cachedData.File
			}
		}
	}

	if par2IndexFile == nil {
		return descriptors, nil
	}

	// Parse the PAR2 file and extract file descriptors
	fileDescriptors, err := readFileDescriptors(ctx, par2IndexFile, poolManager)
	if err != nil {
		return descriptors, fmt.Errorf("failed to read PAR2 file descriptors: %w", err)
	}

	// Build lookup map by Hash16k for fast matching
	for i := range fileDescriptors {
		desc := &fileDescriptors[i]
		descriptors[desc.Hash16k] = desc
	}

	return descriptors, nil
}

// readFileDescriptors streams through a PAR2 file and extracts all file descriptors
// Similar to C# Par2.ReadFileDescriptions
// This function reads ALL segments of the PAR2 file sequentially to find all FileDesc packets
func readFileDescriptors(
	ctx context.Context,
	par2File *nzbparser.NzbFile,
	poolManager pool.Manager,
) ([]FileDescriptor, error) {
	var descriptors []FileDescriptor

	if len(par2File.Segments) == 0 {
		return descriptors, fmt.Errorf("PAR2 file has no segments")
	}

	cp, err := poolManager.GetPool()
	if err != nil {
		return descriptors, fmt.Errorf("failed to get connection pool: %w", err)
	}

	// Create context with timeout (30 seconds per segment should be enough)
	// For multi-segment files, this gives adequate time
	ctx, cancel := context.WithTimeout(ctx, time.Second*30*time.Duration(len(par2File.Segments)))
	defer cancel()

	// Create sequential reader that will read ALL segments of the PAR2 file
	// This is critical because FileDesc packets can be in any segment, not just the first
	r, err := usenet.NewSequentialReader(ctx, par2File.Segments, nil, cp)
	if err != nil {
		return descriptors, fmt.Errorf("failed to create sequential reader: %w", err)
	}
	defer r.Close()

	// Create packet reader for streaming across all segments
	packetReader := NewPacketReader(r)

	// Read packets until we hit an error or reach the end
	// Since we're now reading all segments, we may have many more packets
	// Increase limit to accommodate larger PAR2 files with many FileDesc packets
	maxPackets := 1000 // Limit the number of packets to process
	packetCount := 0

	for packetCount < maxPackets {
		select {
		case <-ctx.Done():
			return descriptors, ctx.Err()
		default:
		}

		// Read packet header
		header, err := packetReader.ReadHeader()
		if err != nil {
			if err == io.EOF {
				// Reached end of file
				break
			}

			break
		}

		packetCount++

		// Check if this is a FileDesc packet
		if header.Type == PacketTypeFileDesc {
			// Read and parse the file descriptor
			desc, err := packetReader.ReadFileDescriptor(header)
			if err != nil {
				slog.DebugContext(ctx, "Failed to read file descriptor", "error", err)
				// Skip to next packet
				continue
			}

			descriptors = append(descriptors, *desc)
		} else {
			// Skip non-FileDesc packets
			if err := packetReader.SkipPacketBody(header); err != nil {
				slog.DebugContext(ctx, "Failed to skip packet body", "error", err)
				break
			}
		}
	}

	return descriptors, nil
}
