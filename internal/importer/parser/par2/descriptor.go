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

// nzbSegmentLoader adapts []nzbparser.NzbSegment into usenet.SegmentLoader.
// Each raw NZB segment maps with Start=0, End=Bytes-1 (all data is usable).
type nzbSegmentLoader struct {
	segs   nzbparser.NzbSegments
	groups []string
}

func (l nzbSegmentLoader) GetSegment(index int) (usenet.Segment, []string, bool) {
	if index < 0 || index >= len(l.segs) {
		return usenet.Segment{}, nil, false
	}
	seg := l.segs[index]
	return usenet.Segment{
		Id:    seg.ID,
		Start: 0,
		End:   int64(seg.Bytes) - 1,
		Size:  int64(seg.Bytes),
	}, l.groups, true
}

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

	// Create context with timeout (30 seconds per segment should be enough)
	// For multi-segment files, this gives adequate time
	ctx, cancel := context.WithTimeout(ctx, time.Second*30*time.Duration(len(par2File.Segments)))
	defer cancel()

	// Build segment loader and compute total size
	loader := nzbSegmentLoader{segs: par2File.Segments, groups: par2File.Groups}
	var totalSize int64
	for _, seg := range par2File.Segments {
		totalSize += int64(seg.Bytes)
	}

	// Create UsenetReader (provides retry, prefetch, and metrics for free)
	rg := usenet.GetSegmentsInRange(ctx, 0, totalSize-1, loader)
	r, err := usenet.NewUsenetReader(ctx, poolManager.GetPool, rg, 5, poolManager, "", nil)
	if err != nil {
		return descriptors, fmt.Errorf("failed to create usenet reader: %w", err)
	}
	defer r.Close()

	// Create packet reader for streaming across all segments
	packetReader := NewPacketReader(r)

	// Read packets until we hit an error or reach the end
	// Since we're now reading all segments, we may have many more packets
	// Increase limit to accommodate larger PAR2 files with many FileDesc packets
	maxPackets := 1000 // Limit the number of packets to process
	packetCount := 0
	var lastError error

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
				break
			}

			if len(descriptors) == 0 {
				return descriptors, fmt.Errorf("corrupted PAR2 file: failed to read packet header: %w", err)
			}
			slog.DebugContext(ctx, "Corrupted packet header encountered, returning partial PAR2 descriptors", "error", err, "descriptors_found", len(descriptors))
			break
		}

		packetCount++

		// Check if this is a FileDesc packet
		if header.Type == PacketTypeFileDesc {
			// Read and parse the file descriptor
			desc, err := packetReader.ReadFileDescriptor(header)
			if err != nil {
				slog.DebugContext(ctx, "Failed to read file descriptor from corrupted packet", "error", err)
				lastError = err
				continue
			}

			descriptors = append(descriptors, *desc)
		} else {
			// Skip non-FileDesc packets
			if err := packetReader.SkipPacketBody(header); err != nil {
				if len(descriptors) == 0 {
					return descriptors, fmt.Errorf("corrupted PAR2 file: failed to skip packet body: %w", err)
				}
				slog.DebugContext(ctx, "Corrupted packet body encountered, returning partial PAR2 descriptors", "error", err, "descriptors_found", len(descriptors))
				break
			}
		}
	}

	if len(descriptors) == 0 && lastError != nil {
		return descriptors, fmt.Errorf("corrupted PAR2 file: failed to extract any file descriptors: %w", lastError)
	}

	return descriptors, nil
}
