package archive

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/javi11/altmount/internal/importer/archive/iso"
	"github.com/javi11/altmount/internal/pool"
	"github.com/javi11/altmount/internal/progress"
)

// analyzedISO bundles an ISO Content with its inspection result and its
// place in a multi-disc grouping. Used internally by ExpandISOContents.
type analyzedISO struct {
	src      Content          // original ISO Content (for fallback / metadata)
	analyzed *iso.AnalyzedISO // result of iso.AnalyzeISO
	discNum  int              // parsed disc number; 0 when label has no disc suffix
	groupKey string           // base name stripped of any DISC/CD/PART suffix
}

// ExpandISOContents replaces .iso entries in contents with the media they
// contain, applying two Blu-ray-aware optimisations on top of the legacy
// "pick the largest file" behaviour:
//
//  1. Within a disc, if BDMV/PLAYLIST/*.mpls identifies a main feature
//     spanning multiple M2TS clips, the clips are virtually concatenated
//     into one Content via NestedSources — the player sees a single file.
//  2. Across discs in the same archive group (e.g. DISC_1 and DISC_2 ISOs
//     in one NZB), discs sharing a stripped volume label are merged so
//     the cross-disc movie also plays as one file.
//
// Non-ISO entries pass through unchanged. Per-ISO errors are non-fatal:
// on failure the original .iso Content is kept so downstream still has
// something to work with.
func ExpandISOContents(
	ctx context.Context,
	expand bool,
	contents []Content,
	poolManager pool.Manager,
	maxPrefetch int,
	readTimeout time.Duration,
	analyzeTimeout time.Duration,
	allowedExtensions []string,
	progressTracker *progress.Tracker,
) ([]Content, error) {
	if !expand {
		return contents, nil
	}

	var (
		result    []Content
		groups    = make(map[string][]analyzedISO)
		groupKeys []string
	)

	// Count the ISO entries up front so each can be given an equal slice of
	// the progress tracker's range; isoIdx walks the ISOs as we process them.
	numISOs := 0
	for _, c := range contents {
		if !c.IsDirectory && strings.ToLower(filepath.Ext(c.Filename)) == ".iso" {
			numISOs++
		}
	}
	isoIdx := 0

	for _, c := range contents {
		if c.IsDirectory || strings.ToLower(filepath.Ext(c.Filename)) != ".iso" {
			result = append(result, c)
			continue
		}

		src := iso.ISOSource{
			Filename: c.Filename,
			Segments: c.Segments,
			AesKey:   c.AesKey,
			AesIV:    c.AesIV,
			Size:     c.Size,
		}
		// Give this ISO its slice of the overall range so per-playlist
		// updates inside AnalyzeISO stay within [isoIdx, isoIdx+1] of the
		// band; bump the parent to the slice boundary once it completes so
		// even non-BDMV ISOs (no playlist loop) advance the bar.
		a, err := iso.AnalyzeISO(ctx, src, poolManager, maxPrefetch, readTimeout, analyzeTimeout, allowedExtensions, progressTracker.Slice(isoIdx, numISOs))
		isoIdx++
		progressTracker.Update(isoIdx, numISOs)
		if err != nil {
			slog.WarnContext(ctx, "Failed to analyze ISO content, keeping ISO as-is",
				"file", c.Filename, "error", err)
			result = append(result, c)
			continue
		}
		if len(a.Files) == 0 && len(a.MainFeature) == 0 {
			result = append(result, c)
			continue
		}

		key, discNum := discGroupKey(a.VolumeLabel, c.Filename)
		entry := analyzedISO{src: c, analyzed: a, discNum: discNum, groupKey: key}
		if _, exists := groups[key]; !exists {
			groupKeys = append(groupKeys, key)
		}
		groups[key] = append(groups[key], entry)
	}

	sort.Strings(groupKeys) // deterministic output order
	for _, key := range groupKeys {
		g := groups[key]
		sort.SliceStable(g, func(i, j int) bool { return g[i].discNum < g[j].discNum })

		// Concatenate main features only when *every* member of the group
		// has one — mixing BDMV and non-BDMV in a single group is almost
		// always a false grouping, so fall back to per-disc handling.
		allHaveMainFeature := true
		for _, e := range g {
			if len(e.analyzed.MainFeature) == 0 {
				allHaveMainFeature = false
				break
			}
		}

		if allHaveMainFeature {
			merged, ok := buildMainFeatureContent(ctx, key, g)
			if ok {
				result = append(result, merged)
				continue
			}
		}

		// Fallback: legacy per-ISO largest-file selection.
		for _, e := range g {
			nc, ok := buildLargestFileContent(e.src, e.analyzed.Files)
			if !ok {
				result = append(result, e.src)
				continue
			}
			result = append(result, nc)
		}
	}

	return result, nil
}

// buildMainFeatureContent concatenates every member's MainFeature into a
// single Content whose NestedSources chain spans every M2TS in disc and
// playlist order. Returns (zero, false) when, after conversion, the chain
// is empty.
func buildMainFeatureContent(ctx context.Context, groupKey string, g []analyzedISO) (Content, bool) {
	var (
		sources      []NestedSource
		totalSize    int64
		firstISOName string
		nzbdavID     string
	)
	// Per-clip timeline table for the continuous-timeline remux. We walk
	// clips in output order across every disc, building a running 90 kHz
	// timeline: clip 0 keeps its native base (delta 0); each later clip is
	// lifted to start where the cumulative authored duration places it.
	//   timeline_start_90k[k] = base0_90k + 2 * Σ_{j<k} durationTicks[j]
	//   delta_90k[k]          = timeline_start_90k[k] − inTime[k]*2
	// All from MPLS data already in hand — no extra import-time reads.
	var (
		clipBoundaries []ClipBoundary
		base0_90k      int64 = -1
		cum90k         int64
		anyTiming      bool
		byteMismatch   bool
	)
	for _, e := range g {
		if firstISOName == "" {
			firstISOName = e.src.Filename
			nzbdavID = e.src.NzbdavID
		}
		for _, fc := range e.analyzed.MainFeature {
			var clipByteLen int64
			for _, ns := range isoFileContentToNestedSources(fc) {
				if ns.InnerLength <= 0 {
					continue
				}
				sources = append(sources, ns)
				totalSize += ns.InnerLength
				clipByteLen += ns.InnerLength
			}
			if clipByteLen == 0 {
				continue
			}
			if fc.Size != 0 && fc.Size != clipByteLen {
				byteMismatch = true
			}
			inBase90k := fc.InTimeTicks * 2
			if base0_90k < 0 {
				base0_90k = inBase90k
			}
			timelineStart90k := base0_90k + cum90k
			clipBoundaries = append(clipBoundaries, ClipBoundary{
				ByteLen:  clipByteLen,
				Delta90k: timelineStart90k - inBase90k,
			})
			if fc.InTimeTicks != 0 || fc.DurationTicks != 0 {
				anyTiming = true
			}
			cum90k += fc.DurationTicks * 2
		}
	}
	if len(sources) == 0 {
		return Content{}, false
	}
	// Only attach the timeline table when we actually have MPLS timing;
	// without it the remux filter must stay disabled (empty → bypassed).
	if !anyTiming {
		clipBoundaries = nil
	} else {
		var boundaryBytes int64
		for _, cb := range clipBoundaries {
			boundaryBytes += cb.ByteLen
		}
		if byteMismatch || boundaryBytes != totalSize {
			slog.WarnContext(ctx, "Disabling Blu-ray timeline remux due to clip boundary byte mismatch",
				"group", groupKey,
				"boundary_bytes", boundaryBytes,
				"size_bytes", totalSize,
			)
			clipBoundaries = nil
		}
	}

	filename := mainFeatureFilename(groupKey, firstISOName)
	slog.InfoContext(ctx, "Built Blu-ray main-feature virtual file",
		"group", groupKey,
		"discs", len(g),
		"clips", len(clipBoundaries),
		"extents", len(sources),
		"size_bytes", totalSize,
		"timeline_seconds", cum90k/90000,
		"filename", filename,
	)

	return Content{
		InternalPath:      filename,
		Filename:          filename,
		Size:              totalSize,
		PackedSize:        totalSize,
		NzbdavID:          nzbdavID,
		NestedSources:     sources,
		ISOExpansionIndex: 1,
		ClipBoundaries:    clipBoundaries,
	}, true
}

// buildLargestFileContent reproduces the pre-existing "pick the single
// biggest file inside the ISO" behaviour. Kept as a fallback for ISOs
// that have no BDMV main feature.
func buildLargestFileContent(src Content, files []iso.ISOFileContent) (Content, bool) {
	if len(files) == 0 {
		return Content{}, false
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Size > files[j].Size })
	f := files[0]
	nc := Content{
		InternalPath:      f.InternalPath,
		Filename:          f.Filename,
		Size:              f.Size,
		PackedSize:        f.Size,
		NzbdavID:          src.NzbdavID,
		ISOExpansionIndex: 1,
	}
	nc.NestedSources = isoFileContentToNestedSources(f)
	if len(nc.NestedSources) == 0 {
		return Content{}, false
	}
	return nc, true
}

// isoFileContentToNestedSources fans an ISOFileContent's on-disc extents
// out into one NestedSource per extent, preserving disc order. Concating
// the resulting sources yields the file's bytes — the multi-extent fix
// for Blu-ray main-feature M2TS files lives here.
func isoFileContentToNestedSources(fc iso.ISOFileContent) []NestedSource {
	out := make([]NestedSource, 0, len(fc.Sources))
	for _, s := range fc.Sources {
		out = append(out, NestedSource{
			Segments:        s.Segments,
			AesKey:          s.AesKey,
			AesIV:           s.AesIV,
			InnerOffset:     s.InnerOffset,
			InnerLength:     s.InnerLength,
			InnerVolumeSize: s.InnerVolumeSize,
		})
	}
	return out
}

// discSuffixPattern matches volume labels like "AVATAR_FIRE_AND_ASH_DISC_1",
// "MOVIE-CD2", "TITLE PART 3", etc. Capture 1 is the stripped base name,
// capture 2 is the disc identifier (numeric or single letter).
var discSuffixPattern = regexp.MustCompile(`(?i)^(.+?)[ _\-]*(?:disc|cd|part|d|side)[ _\-]*([0-9]+|[a-z])$`)

// discGroupKey computes the disc-grouping key and parsed disc number for
// an ISO. It prefers the volume label and falls back to the ISO filename
// (without extension) when the label is empty or doesn't match a disc
// pattern. Single-disc ISOs return key=<full label or filename>, discNum=0.
func discGroupKey(label, isoFilename string) (string, int) {
	candidates := []string{label}
	if isoFilename != "" {
		candidates = append(candidates, strings.TrimSuffix(isoFilename, filepath.Ext(isoFilename)))
	}
	for _, c := range candidates {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		if m := discSuffixPattern.FindStringSubmatch(c); m != nil {
			base := normaliseGroupKey(m[1])
			return base, parseDiscNumber(m[2])
		}
	}
	for _, c := range candidates {
		c = strings.TrimSpace(c)
		if c != "" {
			return normaliseGroupKey(c), 0
		}
	}
	return "", 0
}

func normaliseGroupKey(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, "_- ")
	return strings.ToUpper(s)
}

// parseDiscNumber turns "1" → 1, "2" → 2, "A" → 1, "B" → 2, etc.
func parseDiscNumber(s string) int {
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	if len(s) == 1 {
		c := strings.ToUpper(s)[0]
		if c >= 'A' && c <= 'Z' {
			return int(c-'A') + 1
		}
	}
	return 0
}

// mainFeatureFilename derives a sensible filename for the virtual concat.
// Downstream renaming (see rar/sevenzip aggregator post-processing) will
// usually replace the base name with the NZB release name; we only need a
// valid .m2ts extension here.
func mainFeatureFilename(groupKey, isoFilename string) string {
	const ext = ".m2ts"
	if groupKey != "" {
		return fmt.Sprintf("%s%s", groupKey, ext)
	}
	if isoFilename != "" {
		stem := strings.TrimSuffix(isoFilename, filepath.Ext(isoFilename))
		return stem + ext
	}
	return "main_feature" + ext
}
