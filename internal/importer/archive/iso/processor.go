package iso

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/javi11/altmount/internal/pool"
)

// AnalyzeISOContent enumerates all allowed media files inside the given ISO source
// and returns ISOFileContent entries with Usenet segment mappings.
func AnalyzeISOContent(
	ctx context.Context,
	src ISOSource,
	poolManager pool.Manager,
	maxPrefetch int,
	readTimeout time.Duration,
	allowedExtensions []string,
) ([]ISOFileContent, error) {
	rs, closer, err := NewISOReadSeeker(ctx, src, poolManager, maxPrefetch, readTimeout)
	if err != nil {
		return nil, fmt.Errorf("iso: creating read seeker for %q: %w", src.Filename, err)
	}
	defer closer.Close()

	files, err := ListISOFiles(rs)
	if err != nil {
		return nil, fmt.Errorf("iso: listing files in %q: %w", src.Filename, err)
	}

	var result []ISOFileContent
	for _, entry := range files {
		if !isAllowedFile(entry.path, int64(entry.size), allowedExtensions) {
			continue
		}

		isoOffset := int64(entry.lba) * iso9660SectorSize

		fc := ISOFileContent{
			InternalPath: entry.path,
			Filename:     filepath.Base(entry.path),
			Size:         int64(entry.size),
		}

		if len(src.AesKey) == 0 {
			// Unencrypted: slice segments to cover exactly this file's bytes
			sliced, _ := sliceSegmentsForRange(src.Segments, isoOffset, int64(entry.size))
			fc.Segments = sliced
		} else {
			// Encrypted: create a NestedSource so the VFS can decrypt and seek
			fc.NestedSource = &ISONestedSource{
				Segments:        src.Segments,
				AesKey:          src.AesKey,
				AesIV:           src.AesIV,
				InnerOffset:     isoOffset,
				InnerLength:     int64(entry.size),
				InnerVolumeSize: src.Size,
			}
		}

		result = append(result, fc)
	}

	return result, nil
}

// isAllowedFile returns true if the file extension is in the allowed list.
// An empty allowedExtensions list allows all files.
func isAllowedFile(path string, size int64, allowedExtensions []string) bool {
	if size == 0 {
		return false
	}
	if len(allowedExtensions) == 0 {
		return true
	}
	ext := strings.ToLower(filepath.Ext(path))
	for _, allowed := range allowedExtensions {
		if strings.ToLower(allowed) == ext {
			return true
		}
	}
	return false
}

// sliceSegmentsForRange returns the subset of segments covering [offset, offset+size-1].
// Copied from the sevenzip package — kept local to avoid a cross-package dependency.
func sliceSegmentsForRange(segments []*metapb.SegmentData, offset int64, size int64) ([]*metapb.SegmentData, int64) {
	if size <= 0 || offset < 0 {
		return nil, 0
	}

	targetStart := offset
	targetEnd := offset + size - 1
	var covered int64
	var out []*metapb.SegmentData

	var absPos int64
	for _, seg := range segments {
		segSize := seg.EndOffset - seg.StartOffset + 1
		if segSize <= 0 {
			continue
		}
		segAbsStart := absPos
		segAbsEnd := absPos + segSize - 1

		if segAbsEnd < targetStart {
			absPos += segSize
			continue
		}
		if segAbsStart > targetEnd {
			break
		}

		overlapStart := max(segAbsStart, targetStart)
		overlapEnd := min(segAbsEnd, targetEnd)

		if overlapEnd >= overlapStart {
			relStart := seg.StartOffset + (overlapStart - segAbsStart)
			relEnd := seg.StartOffset + (overlapEnd - segAbsStart)
			if relStart < seg.StartOffset {
				relStart = seg.StartOffset
			}
			if relEnd > seg.EndOffset {
				relEnd = seg.EndOffset
			}
			out = append(out, &metapb.SegmentData{
				Id:          seg.Id,
				StartOffset: relStart,
				EndOffset:   relEnd,
				SegmentSize: seg.SegmentSize,
			})
			covered += relEnd - relStart + 1
			if overlapEnd == targetEnd {
				break
			}
		}
		absPos += segSize
	}

	return out, covered
}
