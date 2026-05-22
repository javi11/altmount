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

// AnalyzeISO inspects the given ISO source and returns:
//   - the volume label (for multi-disc grouping),
//   - the filtered list of inner files (Files),
//   - the ordered MainFeature M2TS list when the ISO is a Blu-ray with a
//     resolvable playlist (nil otherwise).
//
// allowedExtensions only filters Files. MainFeature is always returned for
// BDMV discs regardless of the extension list — its existence is the
// signal callers use to opt into virtual concatenation.
func AnalyzeISO(
	ctx context.Context,
	src ISOSource,
	poolManager pool.Manager,
	maxPrefetch int,
	readTimeout time.Duration,
	allowedExtensions []string,
) (*AnalyzedISO, error) {
	rs, closer, err := NewISOReadSeeker(ctx, src, poolManager, maxPrefetch, readTimeout)
	if err != nil {
		return nil, fmt.Errorf("iso: creating read seeker for %q: %w", src.Filename, err)
	}
	defer closer.Close()

	entries, err := ListISOFiles(rs)
	if err != nil {
		return nil, fmt.Errorf("iso: listing files in %q: %w", src.Filename, err)
	}

	out := &AnalyzedISO{VolumeLabel: ReadVolumeLabel(rs)}

	for _, e := range entries {
		if !isAllowedFile(e.path, int64(e.size), allowedExtensions) {
			continue
		}
		out.Files = append(out.Files, buildFileContent(src, e))
	}

	if mf := ResolveMainFeature(ctx, rs, entries); mf != nil {
		out.DurationTicks = mf.DurationTicks
		for _, e := range mf.Streams {
			out.MainFeature = append(out.MainFeature, buildFileContent(src, e))
		}
	}

	return out, nil
}

// buildFileContent turns one ISO directory entry into an ISOFileContent,
// slicing or referencing the source's Usenet segments according to whether
// the ISO is encrypted.
func buildFileContent(src ISOSource, e isoFileEntry) ISOFileContent {
	isoOffset := int64(e.lba) * iso9660SectorSize
	fc := ISOFileContent{
		InternalPath: e.path,
		Filename:     filepath.Base(e.path),
		Size:         int64(e.size),
	}
	if len(src.AesKey) == 0 {
		// Unencrypted: pre-slice segments so this content stands alone.
		sliced, _ := sliceSegmentsForRange(src.Segments, isoOffset, int64(e.size))
		fc.Segments = sliced
	} else {
		// Encrypted: AES-CBC requires the full inner volume + offset so
		// the cipher can chain IVs from the start of the ISO.
		fc.NestedSource = &ISONestedSource{
			Segments:        src.Segments,
			AesKey:          src.AesKey,
			AesIV:           src.AesIV,
			InnerOffset:     isoOffset,
			InnerLength:     int64(e.size),
			InnerVolumeSize: src.Size,
		}
	}
	return fc
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
