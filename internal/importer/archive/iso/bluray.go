package iso

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"strings"
)

// MainFeaturePlaylist is the result of analysing a Blu-ray's BDMV.
// Streams is the ordered list of M2TS file entries that, concatenated,
// form the main feature; the slice is empty if no parseable playlist
// was found.
type MainFeaturePlaylist struct {
	PlaylistName  string         // e.g. "00800.MPLS" — for logging only
	DurationTicks int64          // sum of (OUT-IN) at 45 kHz
	Streams       []isoFileEntry // ordered M2TS entries
}

// ResolveMainFeature inspects the entries returned by ListISOFiles for a
// Blu-ray (BDMV) structure and returns the playlist that represents the
// main movie. Returns nil if the disc is not BDMV, has no .mpls, or no
// playlist resolves to a non-empty M2TS sequence.
//
// Selection heuristic: pick the playlist with the longest total
// presentation duration. Ties break on PlayItem count (more clips wins),
// then lexicographically smallest filename for determinism.
//
// Failures parsing individual playlists are non-fatal — we skip them and
// keep evaluating the rest, mirroring how every Blu-ray player tolerates
// malformed entries in BDMV/PLAYLIST/.
func ResolveMainFeature(ctx context.Context, rs io.ReadSeeker, files []isoFileEntry) *MainFeaturePlaylist {
	// Build per-clip indexes. M2TS streams live at BDMV/STREAM/<NNNNN>.M2TS
	// and carry the 2D version (or the only version on a 2D disc). SSIF
	// streams live at BDMV/STREAM/SSIF/<NNNNN>.SSIF and carry the
	// stereoscopic interleaved 3D version — on 3D-only Blu-ray releases
	// the main feature playlist references SSIF clips, while the M2TS
	// directory holds only extras. We prefer M2TS when both exist (smaller
	// bytes, universal playback) and fall back to SSIF when only it
	// resolves the playlist's clip names.
	m2tsByClip := make(map[string]isoFileEntry)
	ssifByClip := make(map[string]isoFileEntry)
	var playlistEntries []isoFileEntry
	for _, f := range files {
		up := strings.ToUpper(f.path)
		switch {
		case strings.HasPrefix(up, "BDMV/PLAYLIST/") && strings.HasSuffix(up, ".MPLS"):
			playlistEntries = append(playlistEntries, f)
		case strings.HasPrefix(up, "BDMV/STREAM/SSIF/") && strings.HasSuffix(up, ".SSIF"):
			base := up[len("BDMV/STREAM/SSIF/") : len(up)-len(".SSIF")]
			ssifByClip[base] = f
		case strings.HasPrefix(up, "BDMV/STREAM/") && strings.HasSuffix(up, ".M2TS"):
			base := up[len("BDMV/STREAM/") : len(up)-len(".M2TS")]
			m2tsByClip[base] = f
		}
	}
	// [DEBUG-isobd] One-shot summary of what the resolver actually sees in
	// this ISO. Distinct prefix lets us confirm the live binary includes
	// this instrumentation and lets users grep their logs cleanly.
	var (
		allSum, m2tsSum, ssifSum int64
		biggest                  = topNBySize(files, 6)
	)
	for _, f := range files {
		allSum += int64(f.size)
	}
	for _, f := range m2tsByClip {
		m2tsSum += int64(f.size)
	}
	for _, f := range ssifByClip {
		ssifSum += int64(f.size)
	}
	slog.InfoContext(ctx, "[DEBUG-isobd] bdmv scan",
		"total_files", len(files),
		"playlists", len(playlistEntries),
		"m2ts_clips", len(m2tsByClip),
		"ssif_clips", len(ssifByClip),
		"all_files_sum_bytes", allSum,
		"m2ts_sum_bytes", m2tsSum,
		"ssif_sum_bytes", ssifSum,
		"top6_largest", biggest,
		"sample_paths", samplePaths(files, 12),
	)

	if len(playlistEntries) == 0 || (len(m2tsByClip) == 0 && len(ssifByClip) == 0) {
		return nil
	}

	// Deterministic order: shorter filenames (and lexicographic ties) win
	// the tie-break later.
	sort.Slice(playlistEntries, func(i, j int) bool {
		return playlistEntries[i].path < playlistEntries[j].path
	})

	var best *MainFeaturePlaylist
	for _, pe := range playlistEntries {
		data, err := readISOFile(rs, pe)
		if err != nil {
			continue
		}
		pl, err := ParseMPLS(data)
		if err != nil {
			continue
		}

		// Resolve clip names in playlist order, preferring M2TS over SSIF.
		streams := make([]isoFileEntry, 0, len(pl.PlayItems))
		for _, it := range pl.PlayItems {
			name := strings.ToUpper(it.ClipName)
			if entry, ok := m2tsByClip[name]; ok {
				streams = append(streams, entry)
				continue
			}
			if entry, ok := ssifByClip[name]; ok {
				streams = append(streams, entry)
			}
		}
		// [DEBUG-isobd] Per-playlist evaluation so we can see which mpls
		// resolved how many clips and why a given candidate won or lost.
		var totalSize int64
		for _, s := range streams {
			totalSize += int64(s.size)
		}
		slog.InfoContext(ctx, "[DEBUG-isobd] mpls evaluated",
			"name", pe.path,
			"items", len(pl.PlayItems),
			"resolved_clips", len(streams),
			"unresolved", len(pl.PlayItems)-len(streams),
			"duration_ticks", pl.DurationTicks(),
			"streams_total_bytes", totalSize,
		)

		if len(streams) == 0 {
			continue
		}

		cand := &MainFeaturePlaylist{
			PlaylistName:  pe.path,
			DurationTicks: pl.DurationTicks(),
			Streams:       streams,
		}
		if best == nil || isBetterPlaylist(cand, best, len(pl.PlayItems), len(best.Streams)) {
			best = cand
		}
	}
	if best != nil {
		slog.InfoContext(ctx, "[DEBUG-isobd] main feature picked",
			"playlist", best.PlaylistName,
			"clips", len(best.Streams),
			"duration_ticks", best.DurationTicks,
		)
	}
	return best
}

// samplePaths returns up to max paths from files, intended for diagnostic
// logging. The list is taken in iteration order — not sorted — so the user
// sees what ListISOFiles actually emitted.
func samplePaths(files []isoFileEntry, max int) []string {
	n := min(len(files), max)
	out := make([]string, 0, n)
	for i := range n {
		out = append(out, files[i].path)
	}
	return out
}

// topNBySize returns "path=size" entries for the n largest files. Used by
// diagnostic logging to reveal whether the ISO actually contains the
// multi-GB clips a real Blu-ray main feature would have.
func topNBySize(files []isoFileEntry, n int) []string {
	if len(files) == 0 || n <= 0 {
		return nil
	}
	cp := make([]isoFileEntry, len(files))
	copy(cp, files)
	sort.Slice(cp, func(i, j int) bool { return cp[i].size > cp[j].size })
	k := min(len(cp), n)
	out := make([]string, 0, k)
	for i := range k {
		out = append(out, cp[i].path+"="+formatBytes(int64(cp[i].size)))
	}
	return out
}

// formatBytes renders a byte count compactly for log readability.
// Uses base-2 units (KiB, MiB, GiB) for clarity.
func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%dB", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%ciB", float64(b)/float64(div), "KMGTPE"[exp])
}

// isBetterPlaylist returns true when cand should replace best.
// Comparison: longer duration > more PlayItems > earlier filename.
// The filename tie-break relies on playlistEntries being sorted before
// iteration so the smaller path is seen first; we therefore only swap
// when strictly better.
func isBetterPlaylist(cand, best *MainFeaturePlaylist, candItems, bestItems int) bool {
	if cand.DurationTicks != best.DurationTicks {
		return cand.DurationTicks > best.DurationTicks
	}
	return candItems > bestItems
}

// readISOFile reads the full contents of one isoFileEntry from rs.
// MPLS files are tiny (~KBs), so a one-shot read is fine.
func readISOFile(rs io.ReadSeeker, e isoFileEntry) ([]byte, error) {
	if _, err := rs.Seek(int64(e.lba)*iso9660SectorSize, io.SeekStart); err != nil {
		return nil, err
	}
	buf := make([]byte, e.size)
	if _, err := io.ReadFull(rs, buf); err != nil {
		return nil, err
	}
	return buf, nil
}
