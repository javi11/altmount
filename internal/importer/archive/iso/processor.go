package iso

import (
	"context"
	"fmt"
	"log/slog"
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
	analyzeTimeout time.Duration,
	allowedExtensions []string,
) (*AnalyzedISO, error) {
	start := time.Now()
	// Hard cap the whole walk. A degraded NNTP provider can otherwise stall
	// AnalyzeISO for minutes per ISO. analyzeTimeout <= 0 disables the cap
	// (used by tests that exercise other paths).
	if analyzeTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, analyzeTimeout)
		defer cancel()
	}
	// Fail fast when the deadline is already exceeded (e.g. caller passed a
	// past deadline, or analyzeTimeout fired between WithTimeout and the
	// first NNTP read).
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("iso: analysing %q: %w", src.Filename, err)
	}

	rs, closer, err := NewISOReadSeeker(ctx, src, poolManager, maxPrefetch, readTimeout)
	if err != nil {
		return nil, fmt.Errorf("iso: creating read seeker for %q: %w", src.Filename, err)
	}
	defer closer.Close()

	entries, err := ListISOFiles(ctx, rs)
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

	// Single completion log: raw entry count, filtered file count, BD clip
	// count, and total time. Previously this function emitted two separate
	// INFO lines per successful analysis ("ISO analysed" + "ISO analyse
	// complete"); they're consolidated here.
	slog.InfoContext(ctx, "ISO analysed",
		"filename", src.Filename,
		"iso_size_bytes", src.Size,
		"entries", len(entries),
		"files", len(out.Files),
		"main_feature_clips", len(out.MainFeature),
		"duration_seconds", time.Since(start).Seconds(),
	)

	return out, nil
}

// buildFileContent turns one ISO directory entry into an ISOFileContent,
// emitting one ISONestedSource per on-disc extent. Concatenating the
// sources' byte ranges yields the complete file. This is the path that
// previously fed BAD bytes for multi-extent files like Avatar's 17 GiB
// 00022.m2ts (945 extents) — only the first extent's data was correct.
func buildFileContent(src ISOSource, e isoFileEntry) ISOFileContent {
	fc := ISOFileContent{
		InternalPath: e.path,
		Filename:     filepath.Base(e.path),
		Size:         int64(e.size),
		Sources:      make([]ISONestedSource, 0, len(e.extents)),
	}
	for _, ext := range e.extents {
		isoOffset := int64(ext.lba) * iso9660SectorSize
		extLen := int64(ext.length)
		if len(src.AesKey) == 0 {
			// Unencrypted: pre-slice outer segments to cover this extent
			// only. The downstream nested reader treats InnerOffset as
			// an offset within the (already-sliced) segment chain.
			sliced, _ := sliceSegmentsForRange(src.Segments, isoOffset, extLen)
			fc.Sources = append(fc.Sources, ISONestedSource{
				Segments:        sliced,
				InnerOffset:     0,
				InnerLength:     extLen,
				InnerVolumeSize: extLen,
			})
		} else {
			// Encrypted: AES-CBC needs the IV chain from byte 0 of the
			// outer ISO, so every source gets the full outer segments
			// and the cipher seeks via InnerOffset.
			fc.Sources = append(fc.Sources, ISONestedSource{
				Segments:        src.Segments,
				AesKey:          src.AesKey,
				AesIV:           src.AesIV,
				InnerOffset:     isoOffset,
				InnerLength:     extLen,
				InnerVolumeSize: src.Size,
			})
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
