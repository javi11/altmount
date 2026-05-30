package iso

import (
	"context"
	"io"
	"log/slog"
	"sort"
	"strings"

	"github.com/javi11/altmount/internal/progress"
)

// MainFeaturePlaylist is the result of analysing a Blu-ray's BDMV.
// Streams is the ordered list of M2TS file entries that, concatenated,
// form the main feature; the slice is empty if no parseable playlist
// was found.
type MainFeaturePlaylist struct {
	PlaylistName    string         // e.g. "00800.MPLS" — for logging only
	DurationTicks   int64          // sum of (OUT-IN) at 45 kHz — informational, not used for selection
	Streams         []isoFileEntry // ordered M2TS entries (duplicates preserved if the playlist legitimately repeats a clip)
	UniqueClipBytes uint64         // sum of file sizes of UNIQUE clips referenced; the primary scoring metric
	UniqueClipCount int            // number of distinct clips referenced; scoring tiebreaker
	// ClipInTimes and ClipDurations are parallel to Streams: the MPLS
	// PlayItem IN_time and (OUT−IN) for each stream, in 45 kHz ticks. They
	// drive the continuous-timeline remux of the concatenated clips.
	ClipInTimes   []int64
	ClipDurations []int64
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
func ResolveMainFeature(ctx context.Context, rs io.ReadSeeker, files []isoFileEntry, progressTracker *progress.Tracker) *MainFeaturePlaylist {
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

	// Read every playlist up front in as few sequential passes as possible.
	// Reading one .mpls at a time costs a reader teardown + a fresh NNTP
	// fetch of the segment(s) covering it (the backing DecryptingFile
	// discards its reader on every Seek). Playlists cluster contiguously in
	// BDMV/PLAYLIST/, so coalescing their reads lets the reader prefetch
	// across the whole directory at once instead of re-fetching overlapping
	// segments once per file. See readPlaylistsCoalesced.
	playlistData := readPlaylistsCoalesced(rs, playlistEntries)

	var best *MainFeaturePlaylist
	for idx, pe := range playlistEntries {
		// Report progress per playlist examined — the granular signal that
		// keeps the queue item's bar moving during BD analysis. Network I/O
		// is now front-loaded into readPlaylistsCoalesced, so this loop only
		// parses already-buffered bytes. nil-safe.
		progressTracker.Update(idx+1, len(playlistEntries))
		data, ok := playlistData[pe.path]
		if !ok {
			continue
		}
		pl, err := ParseMPLS(data)
		if err != nil {
			continue
		}

		// Resolve clip names in playlist order, preferring M2TS over SSIF.
		// Build the ordered streams slice (duplicates preserved — a real BD
		// feature may legitimately repeat a clip, and the output virtual
		// file must follow the playlist order faithfully) AND a separate
		// dedupe-by-name byte sum that drives playlist selection. Without
		// the dedupe, a menu-navigation playlist that points 200+ times at
		// the same ~80s menu M2TS would score higher than a real 30-chapter
		// main feature, and we'd serve 30+ GB of looped menu.
		streams := make([]isoFileEntry, 0, len(pl.PlayItems))
		inTimes := make([]int64, 0, len(pl.PlayItems))
		durations := make([]int64, 0, len(pl.PlayItems))
		seenClips := make(map[string]struct{}, len(pl.PlayItems))
		var uniqueClipBytes uint64
		for _, it := range pl.PlayItems {
			name := strings.ToUpper(it.ClipName)
			entry, ok := m2tsByClip[name]
			if !ok {
				entry, ok = ssifByClip[name]
			}
			if !ok {
				continue
			}
			streams = append(streams, entry)
			// Per-clip timing, parallel to streams (45 kHz). OUT may be < IN
			// on malformed entries; clamp the span to 0 in that case.
			var dur int64
			if it.OutTime > it.InTime {
				dur = int64(it.OutTime - it.InTime)
			}
			inTimes = append(inTimes, int64(it.InTime))
			durations = append(durations, dur)
			if _, dup := seenClips[name]; !dup {
				seenClips[name] = struct{}{}
				uniqueClipBytes += entry.size
			}
		}
		if len(streams) == 0 {
			continue
		}

		cand := &MainFeaturePlaylist{
			PlaylistName:    pe.path,
			DurationTicks:   pl.DurationTicks(),
			Streams:         streams,
			UniqueClipBytes: uniqueClipBytes,
			UniqueClipCount: len(seenClips),
			ClipInTimes:     inTimes,
			ClipDurations:   durations,
		}
		slog.DebugContext(ctx, "Blu-ray playlist candidate",
			"playlist", pe.path,
			"play_items", len(pl.PlayItems),
			"resolved_streams", len(streams),
			"unique_clips", len(seenClips),
			"unique_clip_bytes", uniqueClipBytes,
			"duration_seconds", cand.DurationTicks/45000,
		)
		if best == nil || isBetterPlaylist(cand, best) {
			best = cand
		}
	}
	if best != nil {
		slog.InfoContext(ctx, "Blu-ray main feature playlist resolved",
			"playlist", best.PlaylistName,
			"clips", len(best.Streams),
			"unique_clips", best.UniqueClipCount,
			"unique_clip_bytes", best.UniqueClipBytes,
			"duration_seconds", best.DurationTicks/45000,
		)
	}
	return best
}

// isBetterPlaylist returns true when cand should replace best. Score by
// total bytes of unique clips referenced — a real main feature pulls in
// ~30 distinct chapter clips totalling tens of GB, while a Blu-ray menu
// navigation playlist references one small clip repeatedly and therefore
// always loses on this metric regardless of how many PlayItems it
// inflates the raw duration with. Final tie: earlier filename wins,
// relying on playlistEntries being lex-sorted before iteration so we
// only swap when strictly better.
func isBetterPlaylist(cand, best *MainFeaturePlaylist) bool {
	if cand.UniqueClipBytes != best.UniqueClipBytes {
		return cand.UniqueClipBytes > best.UniqueClipBytes
	}
	return cand.UniqueClipCount > best.UniqueClipCount
}

const (
	// playlistRunGapThreshold bounds how far apart two playlist extents may
	// sit on disc before readPlaylistsCoalesced splits them into separate
	// sequential reads. Bridging a small gap reads a few extra contiguous
	// bytes (prefetched in the background) but avoids the reader teardown +
	// fresh NNTP fetch a separate run costs (1-5s on Usenet). 4 MiB ≈ 6 NNTP
	// segments — comfortably larger than typical inter-file padding, so a
	// real PLAYLIST directory collapses to a single run.
	playlistRunGapThreshold = 4 << 20

	// playlistRunSizeCeiling caps a single coalesced read so a malformed
	// on-disc extent table cannot make us allocate gigabytes. A contiguous
	// run that would exceed this is split. No real PLAYLIST directory (a few
	// MB of tiny files) approaches it.
	playlistRunSizeCeiling = 64 << 20
)

// byteRun is one contiguous span of disc bytes read in a single pass. buf is
// the span's bytes once read, or nil if the read failed (callers skip any
// playlist whose extents fall in a failed run).
type byteRun struct {
	start int64 // inclusive byte offset
	end   int64 // exclusive byte offset
	buf   []byte
}

// findRun returns the run fully containing [off, end), or nil. Every extent
// passed to readPlaylistsCoalesced is contained in exactly one run by
// construction, so this only returns nil when the matching run's read failed
// (buf == nil) — which the caller treats the same as "not found".
func findRun(runs []byteRun, off, end int64) *byteRun {
	for i := range runs {
		if runs[i].buf != nil && runs[i].start <= off && end <= runs[i].end {
			return &runs[i]
		}
	}
	return nil
}

// readPlaylistsCoalesced reads every entry's bytes using as few sequential
// streaming reads as possible, returning a map keyed by entry.path. Entries
// that cannot be fully read (seek/read error in their run, or zero usable
// bytes) are simply absent — the caller skips them, mirroring the per-file
// error handling readISOFile drove previously.
//
// Reconstruction is byte-identical to readISOFile: each entry's extents are
// concatenated in disc order, taking each extent's bytes from the run buffer
// that covers it.
func readPlaylistsCoalesced(rs io.ReadSeeker, entries []isoFileEntry) map[string][]byte {
	// Flatten all non-empty extents and sort by disc offset so we can group
	// neighbours into runs.
	type flatExtent struct {
		offset int64
		length int64
	}
	var exts []flatExtent
	for _, e := range entries {
		for _, x := range e.extents {
			if x.length == 0 {
				continue
			}
			exts = append(exts, flatExtent{
				offset: int64(x.lba) * iso9660SectorSize,
				length: int64(x.length),
			})
		}
	}
	if len(exts) == 0 {
		return map[string][]byte{}
	}
	sort.Slice(exts, func(i, j int) bool { return exts[i].offset < exts[j].offset })

	// Group extents into runs, starting a new run when the gap to the next
	// extent exceeds the threshold or the run would grow past the ceiling.
	// runEnd tracks the max end so overlapping/duplicate extents merge.
	var runs []byteRun
	cur := byteRun{start: exts[0].offset, end: exts[0].offset + exts[0].length}
	for _, x := range exts[1:] {
		xEnd := x.offset + x.length
		if x.offset-cur.end > playlistRunGapThreshold || xEnd-cur.start > playlistRunSizeCeiling {
			runs = append(runs, cur)
			cur = byteRun{start: x.offset, end: xEnd}
			continue
		}
		if xEnd > cur.end {
			cur.end = xEnd
		}
	}
	runs = append(runs, cur)

	// One Seek + one ReadFull per run. A failed run keeps buf == nil.
	for i := range runs {
		r := &runs[i]
		if _, err := rs.Seek(r.start, io.SeekStart); err != nil {
			continue
		}
		buf := make([]byte, r.end-r.start)
		if _, err := io.ReadFull(rs, buf); err != nil {
			continue
		}
		r.buf = buf
	}

	// Reconstruct each entry from the run buffers, preserving extent order.
	out := make(map[string][]byte, len(entries))
	for _, e := range entries {
		data := make([]byte, 0, e.size)
		ok := true
		for _, x := range e.extents {
			if x.length == 0 {
				continue
			}
			off := int64(x.lba) * iso9660SectorSize
			length := int64(x.length)
			r := findRun(runs, off, off+length)
			if r == nil {
				ok = false
				break
			}
			lo := off - r.start
			hi := lo + length
			if lo < 0 || hi > int64(len(r.buf)) {
				ok = false
				break
			}
			data = append(data, r.buf[lo:hi]...)
		}
		if ok && len(data) > 0 {
			out[e.path] = data
		}
	}
	return out
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
