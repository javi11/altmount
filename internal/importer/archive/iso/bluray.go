package iso

import (
	"context"
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
		slog.InfoContext(ctx, "Blu-ray main feature playlist resolved",
			"playlist", best.PlaylistName,
			"clips", len(best.Streams),
			"duration_seconds", best.DurationTicks/45000,
		)
	}
	return best
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

// readISOFile reads the full contents of one isoFileEntry from rs,
// concatenating bytes across every on-disc extent. MPLS files are tiny
// (~KBs) and almost always single-extent, but multi-extent MPLS is
// legal so we iterate.
func readISOFile(rs io.ReadSeeker, e isoFileEntry) ([]byte, error) {
	out := make([]byte, 0, e.size)
	for _, ext := range e.extents {
		if _, err := rs.Seek(int64(ext.lba)*iso9660SectorSize, io.SeekStart); err != nil {
			return nil, err
		}
		chunk := make([]byte, ext.length)
		if _, err := io.ReadFull(rs, chunk); err != nil {
			return nil, err
		}
		out = append(out, chunk...)
	}
	return out, nil
}
