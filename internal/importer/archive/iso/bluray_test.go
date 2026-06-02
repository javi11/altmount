package iso

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"testing"

	"github.com/javi11/altmount/internal/progress"
)

// recordingBroadcaster captures progress updates for assertions in tests.
type recordingBroadcaster struct {
	percentages []int
	stages      []string
}

func (rb *recordingBroadcaster) UpdateProgress(_ int, percentage int) {
	rb.percentages = append(rb.percentages, percentage)
	rb.stages = append(rb.stages, "")
}

func (rb *recordingBroadcaster) UpdateProgressWithStage(_ int, percentage int, stage string) {
	rb.percentages = append(rb.percentages, percentage)
	rb.stages = append(rb.stages, stage)
}

// mkEntry builds a single-extent isoFileEntry — the common case for tests.
func mkEntry(path string, lba uint32, size uint64) isoFileEntry {
	return isoFileEntry{
		path:    path,
		size:    size,
		extents: []isoExtent{{lba: lba, length: size}},
	}
}

// makeImage assembles an in-memory disc image by placing each piece of
// data at the sector index given in its key. The returned reader can be
// used as if it were a real ISO read-seeker.
func makeImage(t *testing.T, pieces map[uint32][]byte) io.ReadSeeker {
	t.Helper()
	var maxSect uint32
	for s, b := range pieces {
		end := s + uint32((len(b)+iso9660SectorSize-1)/iso9660SectorSize)
		if end > maxSect {
			maxSect = end
		}
	}
	if maxSect == 0 {
		maxSect = 1
	}
	img := make([]byte, int(maxSect)*iso9660SectorSize)
	for s, b := range pieces {
		copy(img[int(s)*iso9660SectorSize:], b)
	}
	return bytes.NewReader(img)
}

func TestResolveMainFeature(t *testing.T) {
	t.Parallel()

	t.Run("picks longest playlist", func(t *testing.T) {
		t.Parallel()
		// Two playlists:
		//   00001.MPLS  → 1 clip, short (extras playlist)
		//   00800.MPLS  → 3 clips, long  (main feature)
		short := buildMPLS(t, "0200", []MPLSPlayItem{
			{ClipName: "00010", InTime: 0, OutTime: 45000},
		}, nil)
		long := buildMPLS(t, "0200", []MPLSPlayItem{
			{ClipName: "00001", InTime: 0, OutTime: 90 * 45000},
			{ClipName: "00002", InTime: 0, OutTime: 60 * 45000},
			{ClipName: "00003", InTime: 0, OutTime: 30 * 45000},
		}, nil)

		rs := makeImage(t, map[uint32][]byte{
			100: short,
			110: long,
		})

		// File listing: two playlists and four M2TS clips (one extra).
		files := []isoFileEntry{
			mkEntry("BDMV/PLAYLIST/00001.MPLS", 100, uint64(len(short))),
			mkEntry("BDMV/PLAYLIST/00800.MPLS", 110, uint64(len(long))),
			mkEntry("BDMV/STREAM/00001.M2TS", 200, 1_000_000),
			mkEntry("BDMV/STREAM/00002.M2TS", 300, 2_000_000),
			mkEntry("BDMV/STREAM/00003.M2TS", 400, 3_000_000),
			mkEntry("BDMV/STREAM/00010.M2TS", 500, 500_000),
		}

		got := ResolveMainFeature(context.Background(), rs, files, nil)
		if got == nil {
			t.Fatal("ResolveMainFeature returned nil")
		}
		if got.PlaylistName != "BDMV/PLAYLIST/00800.MPLS" {
			t.Errorf("PlaylistName = %q, want 00800.MPLS", got.PlaylistName)
		}
		if len(got.Streams) != 3 {
			t.Fatalf("Streams len = %d, want 3", len(got.Streams))
		}
		wantOrder := []string{"BDMV/STREAM/00001.M2TS", "BDMV/STREAM/00002.M2TS", "BDMV/STREAM/00003.M2TS"}
		for i, s := range got.Streams {
			if s.path != wantOrder[i] {
				t.Errorf("Streams[%d].path = %q, want %q", i, s.path, wantOrder[i])
			}
		}
	})

	t.Run("reports progress per playlist", func(t *testing.T) {
		t.Parallel()
		short := buildMPLS(t, "0200", []MPLSPlayItem{
			{ClipName: "00010", InTime: 0, OutTime: 45000},
		}, nil)
		long := buildMPLS(t, "0200", []MPLSPlayItem{
			{ClipName: "00001", InTime: 0, OutTime: 90 * 45000},
			{ClipName: "00002", InTime: 0, OutTime: 60 * 45000},
		}, nil)
		rs := makeImage(t, map[uint32][]byte{100: short, 110: long})
		files := []isoFileEntry{
			mkEntry("BDMV/PLAYLIST/00001.MPLS", 100, uint64(len(short))),
			mkEntry("BDMV/PLAYLIST/00800.MPLS", 110, uint64(len(long))),
			mkEntry("BDMV/STREAM/00001.M2TS", 200, 1_000_000),
			mkEntry("BDMV/STREAM/00002.M2TS", 300, 2_000_000),
			mkEntry("BDMV/STREAM/00010.M2TS", 500, 500_000),
		}

		rb := &recordingBroadcaster{}
		tracker := progress.NewTracker(rb, 7, 10, 30).WithStage("Analyzing ISO")

		if got := ResolveMainFeature(context.Background(), rs, files, tracker); got == nil {
			t.Fatal("ResolveMainFeature returned nil")
		}

		// Two playlists → at least one update; every update must carry the
		// stage, stay inside [10,30], and be non-decreasing.
		if len(rb.percentages) == 0 {
			t.Fatal("expected progress updates, got none")
		}
		prev := -1
		for i, p := range rb.percentages {
			if rb.stages[i] != "Analyzing ISO" {
				t.Errorf("update %d stage = %q, want %q", i, rb.stages[i], "Analyzing ISO")
			}
			if p < 10 || p > 30 {
				t.Errorf("update %d percentage = %d, want within [10,30]", i, p)
			}
			if p < prev {
				t.Errorf("update %d percentage = %d decreased from %d", i, p, prev)
			}
			prev = p
		}
	})

	t.Run("non-BDMV disc returns nil", func(t *testing.T) {
		t.Parallel()
		files := []isoFileEntry{
			mkEntry("movie.mkv", 100, 1_000_000),
		}
		if got := ResolveMainFeature(context.Background(), bytes.NewReader(make([]byte, 16*iso9660SectorSize)), files, nil); got != nil {
			t.Errorf("expected nil for non-BDMV disc, got %+v", got)
		}
	})

	t.Run("BDMV with no parseable MPLS returns nil", func(t *testing.T) {
		t.Parallel()
		rs := makeImage(t, map[uint32][]byte{
			100: []byte("not a real mpls"),
		})
		files := []isoFileEntry{
			mkEntry("BDMV/PLAYLIST/00001.MPLS", 100, 15),
			mkEntry("BDMV/STREAM/00001.M2TS", 200, 1_000_000),
		}
		if got := ResolveMainFeature(context.Background(), rs, files, nil); got != nil {
			t.Errorf("expected nil for unparseable MPLS, got %+v", got)
		}
	})

	t.Run("3D BD: playlist resolves against SSIF when M2TS missing", func(t *testing.T) {
		t.Parallel()
		// Avatar-2-style 3D-only release: BDMV/STREAM/*.M2TS holds only
		// extras (tiny). The real main feature lives in BDMV/STREAM/SSIF/
		// and is referenced by its own MPLS. The resolver must index SSIF
		// so the long playlist resolves and wins.
		extras := buildMPLS(t, "0200", []MPLSPlayItem{
			{ClipName: "00010", InTime: 0, OutTime: 90 * 45000}, // 90s extra
		}, nil)
		mainFeature3D := buildMPLS(t, "0200", []MPLSPlayItem{
			{ClipName: "00100", InTime: 0, OutTime: 60 * 60 * 45000},
			{ClipName: "00101", InTime: 0, OutTime: 60 * 60 * 45000},
			{ClipName: "00102", InTime: 0, OutTime: 12 * 60 * 45000}, // 132 min total
		}, nil)

		rs := makeImage(t, map[uint32][]byte{
			100: extras,
			110: mainFeature3D,
		})

		files := []isoFileEntry{
			mkEntry("BDMV/PLAYLIST/00001.MPLS", 100, uint64(len(extras))),
			mkEntry("BDMV/PLAYLIST/00800.MPLS", 110, uint64(len(mainFeature3D))),
			// Only the extras live as M2TS:
			mkEntry("BDMV/STREAM/00010.M2TS", 200, 50_000_000),
			// Main feature is SSIF only:
			mkEntry("BDMV/STREAM/SSIF/00100.SSIF", 300, 25_000_000_000),
			mkEntry("BDMV/STREAM/SSIF/00101.SSIF", 400, 25_000_000_000),
			mkEntry("BDMV/STREAM/SSIF/00102.SSIF", 500, 5_000_000_000),
		}

		got := ResolveMainFeature(context.Background(), rs, files, nil)
		if got == nil {
			t.Fatal("ResolveMainFeature returned nil — SSIF index missing?")
		}
		if got.PlaylistName != "BDMV/PLAYLIST/00800.MPLS" {
			t.Errorf("PlaylistName = %q, want 00800.MPLS (3D main feature)", got.PlaylistName)
		}
		if len(got.Streams) != 3 {
			t.Fatalf("Streams len = %d, want 3 SSIF clips", len(got.Streams))
		}
		wantOrder := []string{
			"BDMV/STREAM/SSIF/00100.SSIF",
			"BDMV/STREAM/SSIF/00101.SSIF",
			"BDMV/STREAM/SSIF/00102.SSIF",
		}
		for i, s := range got.Streams {
			if s.path != wantOrder[i] {
				t.Errorf("Streams[%d].path = %q, want %q", i, s.path, wantOrder[i])
			}
		}
	})

	t.Run("hybrid 3D BD: prefers M2TS over SSIF when both exist", func(t *testing.T) {
		t.Parallel()
		// Both 2D MPLS (refs M2TS) and 3D MPLS (refs SSIF) point at clips
		// of the same name. With both files present, the M2TS version is
		// the right pick: smaller bytes, universal playback. The resolver
		// should select it even if the 3D playlist is marginally longer.
		mainFeature := buildMPLS(t, "0200", []MPLSPlayItem{
			{ClipName: "00100", InTime: 0, OutTime: 60 * 60 * 45000},
		}, nil)
		rs := makeImage(t, map[uint32][]byte{100: mainFeature})

		files := []isoFileEntry{
			mkEntry("BDMV/PLAYLIST/00800.MPLS", 100, uint64(len(mainFeature))),
			mkEntry("BDMV/STREAM/00100.M2TS", 200, 20_000_000_000),
			mkEntry("BDMV/STREAM/SSIF/00100.SSIF", 300, 40_000_000_000),
		}

		got := ResolveMainFeature(context.Background(), rs, files, nil)
		if got == nil {
			t.Fatal("ResolveMainFeature returned nil")
		}
		if len(got.Streams) != 1 {
			t.Fatalf("Streams len = %d, want 1", len(got.Streams))
		}
		if got.Streams[0].path != "BDMV/STREAM/00100.M2TS" {
			t.Errorf("picked %q, want M2TS over SSIF", got.Streams[0].path)
		}
		if got.SourceKind != "m2ts" {
			t.Errorf("SourceKind = %q, want m2ts", got.SourceKind)
		}
	})

	t.Run("playlist falls back to SSIF only when M2TS set incomplete", func(t *testing.T) {
		t.Parallel()
		data := buildMPLS(t, "0200", []MPLSPlayItem{
			{ClipName: "00100", InTime: 0, OutTime: 60 * 45000},
			{ClipName: "00101", InTime: 0, OutTime: 60 * 45000},
		}, nil)
		rs := makeImage(t, map[uint32][]byte{100: data})

		files := []isoFileEntry{
			mkEntry("BDMV/PLAYLIST/00800.MPLS", 100, uint64(len(data))),
			mkEntry("BDMV/STREAM/00100.M2TS", 200, 10_000_000),
			mkEntry("BDMV/STREAM/SSIF/00100.SSIF", 300, 20_000_000),
			mkEntry("BDMV/STREAM/SSIF/00101.SSIF", 400, 20_000_000),
		}

		got := ResolveMainFeature(context.Background(), rs, files, nil)
		if got == nil {
			t.Fatal("ResolveMainFeature returned nil")
		}
		if got.SourceKind != "ssif" {
			t.Errorf("SourceKind = %q, want ssif", got.SourceKind)
		}
		wantOrder := []string{
			"BDMV/STREAM/SSIF/00100.SSIF",
			"BDMV/STREAM/SSIF/00101.SSIF",
		}
		for i, s := range got.Streams {
			if s.path != wantOrder[i] {
				t.Errorf("Streams[%d].path = %q, want %q", i, s.path, wantOrder[i])
			}
		}
	})

	t.Run("playlist with only mixed partial M2TS and SSIF is skipped", func(t *testing.T) {
		t.Parallel()
		data := buildMPLS(t, "0200", []MPLSPlayItem{
			{ClipName: "00100", InTime: 0, OutTime: 60 * 45000},
			{ClipName: "00101", InTime: 0, OutTime: 60 * 45000},
		}, nil)
		rs := makeImage(t, map[uint32][]byte{100: data})

		files := []isoFileEntry{
			mkEntry("BDMV/PLAYLIST/00800.MPLS", 100, uint64(len(data))),
			mkEntry("BDMV/STREAM/00100.M2TS", 200, 10_000_000),
			mkEntry("BDMV/STREAM/SSIF/00101.SSIF", 400, 20_000_000),
		}

		if got := ResolveMainFeature(context.Background(), rs, files, nil); got != nil {
			t.Fatalf("ResolveMainFeature returned mixed candidate %+v, want nil", got)
		}
	})

	t.Run("playlist referencing missing M2TS yields nil", func(t *testing.T) {
		t.Parallel()
		// Playlist references a clip that has no corresponding M2TS entry.
		data := buildMPLS(t, "0200", []MPLSPlayItem{
			{ClipName: "99999", InTime: 0, OutTime: 45000},
		}, nil)
		rs := makeImage(t, map[uint32][]byte{
			100: data,
		})
		files := []isoFileEntry{
			mkEntry("BDMV/PLAYLIST/00001.MPLS", 100, uint64(len(data))),
			mkEntry("BDMV/STREAM/00001.M2TS", 200, 1_000_000),
		}
		if got := ResolveMainFeature(context.Background(), rs, files, nil); got != nil {
			t.Errorf("expected nil when MPLS references unknown clip, got %+v", got)
		}
	})

	t.Run("prefers feature over menu when menu has more PlayItems", func(t *testing.T) {
		t.Parallel()
		// The Avatar 3D regression: a menu navigation playlist with 201
		// PlayItems all pointing at the same ~80s menu clip would beat the
		// real main feature under the old duration-sum scoring because
		// 201 × 80s > 30 × 6min. The fix scores by unique-clip bytes,
		// where the menu's single 100MB clip loses to the feature's
		// 30 × 600MB chapter clips totalling 18 GB.
		menuItems := make([]MPLSPlayItem, 201)
		for i := range menuItems {
			// All 201 PlayItems reference the SAME menu clip — exactly the
			// pattern observed in the user's failing case.
			menuItems[i] = MPLSPlayItem{
				ClipName: "00149",
				InTime:   0,
				OutTime:  80 * 45000, // 80s, so total raw duration is 201 × 80s = 16200s ≈ 4.5h
			}
		}
		menu := buildMPLS(t, "0200", menuItems, nil)

		featureItems := make([]MPLSPlayItem, 30)
		for i := range featureItems {
			featureItems[i] = MPLSPlayItem{
				ClipName: fmt.Sprintf("%05d", 1+i), // 30 distinct clips: 00001..00030
				InTime:   0,
				OutTime:  6 * 60 * 45000, // 6 min/chapter → 30 × 6 = 180 min total raw duration
			}
		}
		feature := buildMPLS(t, "0200", featureItems, nil)

		rs := makeImage(t, map[uint32][]byte{
			100: menu,
			110: feature,
		})

		files := []isoFileEntry{
			mkEntry("BDMV/PLAYLIST/00000.MPLS", 100, uint64(len(menu))),
			mkEntry("BDMV/PLAYLIST/00800.MPLS", 110, uint64(len(feature))),
			// Menu clip: ~100 MB, one entry.
			mkEntry("BDMV/STREAM/00149.M2TS", 1000, 100_000_000),
		}
		// 30 distinct feature clips, ~600 MB each → ~18 GB total unique bytes.
		for i := range featureItems {
			files = append(files, mkEntry(
				fmt.Sprintf("BDMV/STREAM/%05d.M2TS", 1+i),
				2000+uint32(i)*10,
				600_000_000,
			))
		}

		got := ResolveMainFeature(context.Background(), rs, files, nil)
		if got == nil {
			t.Fatal("ResolveMainFeature returned nil — feature playlist should have won")
		}
		if got.PlaylistName != "BDMV/PLAYLIST/00800.MPLS" {
			t.Fatalf("PlaylistName = %q, want 00800.MPLS (the real feature). The menu's 201 PlayItems must not be allowed to beat the feature's 30 distinct chapters.", got.PlaylistName)
		}
		if got.UniqueClipCount != 30 {
			t.Errorf("UniqueClipCount = %d, want 30 (one per feature chapter)", got.UniqueClipCount)
		}
		if got.UniqueClipBytes != 30*600_000_000 {
			t.Errorf("UniqueClipBytes = %d, want %d", got.UniqueClipBytes, uint64(30*600_000_000))
		}
		if len(got.Streams) != 30 {
			t.Errorf("Streams len = %d, want 30 (the playlist's actual playback order)", len(got.Streams))
		}
	})

	t.Run("preserves legitimate clip repetition in output streams", func(t *testing.T) {
		t.Parallel()
		// A real BD playlist may legitimately repeat a clip (e.g., a
		// "previously on..." recap at the start of each chapter). The fix
		// dedupes only for scoring; the output Streams slice must retain
		// the playlist's actual playback order, including duplicates.
		data := buildMPLS(t, "0200", []MPLSPlayItem{
			{ClipName: "00001", InTime: 0, OutTime: 30 * 45000},  // A
			{ClipName: "00002", InTime: 0, OutTime: 60 * 45000},  // B
			{ClipName: "00001", InTime: 0, OutTime: 30 * 45000},  // A again
			{ClipName: "00003", InTime: 0, OutTime: 90 * 45000},  // C
		}, nil)
		rs := makeImage(t, map[uint32][]byte{100: data})

		files := []isoFileEntry{
			mkEntry("BDMV/PLAYLIST/00800.MPLS", 100, uint64(len(data))),
			mkEntry("BDMV/STREAM/00001.M2TS", 200, 100),
			mkEntry("BDMV/STREAM/00002.M2TS", 300, 200),
			mkEntry("BDMV/STREAM/00003.M2TS", 400, 300),
		}

		got := ResolveMainFeature(context.Background(), rs, files, nil)
		if got == nil {
			t.Fatal("ResolveMainFeature returned nil")
		}

		// Output preserves [A, B, A, C] exactly.
		if len(got.Streams) != 4 {
			t.Fatalf("Streams len = %d, want 4 (dedupe must not collapse the output)", len(got.Streams))
		}
		wantPaths := []string{
			"BDMV/STREAM/00001.M2TS",
			"BDMV/STREAM/00002.M2TS",
			"BDMV/STREAM/00001.M2TS",
			"BDMV/STREAM/00003.M2TS",
		}
		for i, s := range got.Streams {
			if s.path != wantPaths[i] {
				t.Errorf("Streams[%d].path = %q, want %q", i, s.path, wantPaths[i])
			}
		}

		// Scoring metrics use dedupe: 3 unique clips totalling 100+200+300.
		if got.UniqueClipCount != 3 {
			t.Errorf("UniqueClipCount = %d, want 3", got.UniqueClipCount)
		}
		if got.UniqueClipBytes != 600 {
			t.Errorf("UniqueClipBytes = %d, want 600 (100+200+300, A counted once)", got.UniqueClipBytes)
		}
	})

	t.Run("when all playlists are menus, picks the largest deterministically", func(t *testing.T) {
		t.Parallel()
		// Degenerate disc: every MPLS is a menu-style single-clip
		// repetition. Algorithm must still return *something* without
		// crashing and must be deterministic across runs. Picks the one
		// with the largest unique-clip bytes (i.e., the largest target
		// clip, since each playlist has only one unique clip).
		menuA := buildMPLS(t, "0200", []MPLSPlayItem{
			{ClipName: "00100", InTime: 0, OutTime: 80 * 45000},
			{ClipName: "00100", InTime: 0, OutTime: 80 * 45000},
		}, nil)
		menuB := buildMPLS(t, "0200", []MPLSPlayItem{
			{ClipName: "00200", InTime: 0, OutTime: 80 * 45000},
			{ClipName: "00200", InTime: 0, OutTime: 80 * 45000},
			{ClipName: "00200", InTime: 0, OutTime: 80 * 45000},
		}, nil)

		rs := makeImage(t, map[uint32][]byte{
			100: menuA,
			110: menuB,
		})
		files := []isoFileEntry{
			mkEntry("BDMV/PLAYLIST/00001.MPLS", 100, uint64(len(menuA))),
			mkEntry("BDMV/PLAYLIST/00002.MPLS", 110, uint64(len(menuB))),
			mkEntry("BDMV/STREAM/00100.M2TS", 200, 50_000_000),  // 50 MB
			mkEntry("BDMV/STREAM/00200.M2TS", 300, 100_000_000), // 100 MB — larger
		}

		got := ResolveMainFeature(context.Background(), rs, files, nil)
		if got == nil {
			t.Fatal("ResolveMainFeature returned nil for a disc full of menus — should still pick one")
		}
		if got.PlaylistName != "BDMV/PLAYLIST/00002.MPLS" {
			t.Errorf("PlaylistName = %q, want 00002.MPLS (its unique clip is 100 MB vs 50 MB)", got.PlaylistName)
		}
	})
}

// countingReadSeeker wraps an io.ReadSeeker and counts Seek calls so tests can
// pin the "one Seek per run" performance property of readPlaylistsCoalesced.
type countingReadSeeker struct {
	rs    io.ReadSeeker
	seeks int
}

func (c *countingReadSeeker) Read(p []byte) (int, error) { return c.rs.Read(p) }

func (c *countingReadSeeker) Seek(offset int64, whence int) (int64, error) {
	c.seeks++
	return c.rs.Seek(offset, whence)
}

// secByte returns n bytes all set to v — a distinguishable per-sector pattern.
func secByte(v byte, n int) []byte { return bytes.Repeat([]byte{v}, n) }

func TestReadPlaylistsCoalesced(t *testing.T) {
	t.Parallel()

	t.Run("single run, multiple files reconstruct byte-exact", func(t *testing.T) {
		t.Parallel()
		a := secByte(0xA1, 100)
		b := secByte(0xB2, 200)
		rs := makeImage(t, map[uint32][]byte{10: a, 11: b})
		entries := []isoFileEntry{
			mkEntry("BDMV/PLAYLIST/00001.MPLS", 10, uint64(len(a))),
			mkEntry("BDMV/PLAYLIST/00002.MPLS", 11, uint64(len(b))),
		}
		got := readPlaylistsCoalesced(rs, entries)
		if !bytes.Equal(got["BDMV/PLAYLIST/00001.MPLS"], a) {
			t.Errorf("entry 00001 bytes mismatch")
		}
		if !bytes.Equal(got["BDMV/PLAYLIST/00002.MPLS"], b) {
			t.Errorf("entry 00002 bytes mismatch")
		}
	})

	t.Run("gap beyond threshold splits into two runs", func(t *testing.T) {
		t.Parallel()
		a := secByte(0xA1, 100)
		b := secByte(0xB2, 100)
		// sector 5000 is 5000*2048 ≈ 10 MiB in, well past the 4 MiB gap
		// threshold from sector 10, forcing a second run.
		far := uint32(5000)
		rs := &countingReadSeeker{rs: makeImage(t, map[uint32][]byte{10: a, far: b})}
		entries := []isoFileEntry{
			mkEntry("near.MPLS", 10, uint64(len(a))),
			mkEntry("far.MPLS", far, uint64(len(b))),
		}
		got := readPlaylistsCoalesced(rs, entries)
		if !bytes.Equal(got["near.MPLS"], a) {
			t.Errorf("near bytes mismatch")
		}
		if !bytes.Equal(got["far.MPLS"], b) {
			t.Errorf("far bytes mismatch")
		}
		if rs.seeks != 2 {
			t.Errorf("seeks = %d, want 2 (two runs)", rs.seeks)
		}
	})

	t.Run("multi-extent entry concatenates in disc order regardless of layout", func(t *testing.T) {
		t.Parallel()
		// Extent[0] lives AFTER extent[1] on disc; reconstruction must still
		// emit extent[0] bytes first.
		hi := secByte(0x50, 100) // at sector 50
		lo := secByte(0x10, 100) // at sector 10
		rs := makeImage(t, map[uint32][]byte{10: lo, 50: hi})
		entry := isoFileEntry{
			path: "multi.MPLS",
			size: 200,
			extents: []isoExtent{
				{lba: 50, length: 100}, // first in file order
				{lba: 10, length: 100}, // second in file order
			},
		}
		got := readPlaylistsCoalesced(rs, []isoFileEntry{entry})
		want := append(append([]byte{}, hi...), lo...)
		if !bytes.Equal(got["multi.MPLS"], want) {
			t.Errorf("multi-extent concat order wrong")
		}
	})

	t.Run("overlapping extents both reconstruct", func(t *testing.T) {
		t.Parallel()
		// Two single-byte-pattern sectors; entry A spans both sectors,
		// entry B only the first. They overlap in the same run buffer.
		s10 := secByte(0xAA, iso9660SectorSize)
		s11 := secByte(0xBB, iso9660SectorSize)
		rs := makeImage(t, map[uint32][]byte{10: s10, 11: s11})
		entryA := isoFileEntry{path: "A", size: 2 * iso9660SectorSize, extents: []isoExtent{{lba: 10, length: 2 * iso9660SectorSize}}}
		entryB := mkEntry("B", 10, iso9660SectorSize)
		got := readPlaylistsCoalesced(rs, []isoFileEntry{entryA, entryB})
		wantA := append(append([]byte{}, s10...), s11...)
		if !bytes.Equal(got["A"], wantA) {
			t.Errorf("entry A (overlapping span) mismatch")
		}
		if !bytes.Equal(got["B"], s10) {
			t.Errorf("entry B (overlapping span) mismatch")
		}
	})

	t.Run("zero-extent entry is absent", func(t *testing.T) {
		t.Parallel()
		a := secByte(0xA1, 100)
		rs := makeImage(t, map[uint32][]byte{10: a})
		entries := []isoFileEntry{
			mkEntry("real.MPLS", 10, uint64(len(a))),
			{path: "empty.MPLS", size: 0, extents: nil},
			{path: "zerolen.MPLS", size: 0, extents: []isoExtent{{lba: 20, length: 0}}},
		}
		got := readPlaylistsCoalesced(rs, entries)
		if _, ok := got["empty.MPLS"]; ok {
			t.Errorf("empty.MPLS should be absent")
		}
		if _, ok := got["zerolen.MPLS"]; ok {
			t.Errorf("zerolen.MPLS should be absent")
		}
		if !bytes.Equal(got["real.MPLS"], a) {
			t.Errorf("real.MPLS mismatch")
		}
	})

	t.Run("read error skips only the affected run", func(t *testing.T) {
		t.Parallel()
		good := secByte(0xA1, 100)
		// "bad" entry points far past the end of the image (no backing piece),
		// so its run's ReadFull fails. It lives in its own run (gap > threshold)
		// so the good entry is unaffected.
		rs := makeImage(t, map[uint32][]byte{10: good})
		entries := []isoFileEntry{
			mkEntry("good.MPLS", 10, uint64(len(good))),
			mkEntry("bad.MPLS", 5000, 4096),
		}
		got := readPlaylistsCoalesced(rs, entries)
		if !bytes.Equal(got["good.MPLS"], good) {
			t.Errorf("good.MPLS should still be present and correct")
		}
		if _, ok := got["bad.MPLS"]; ok {
			t.Errorf("bad.MPLS should be absent after read error")
		}
	})

	t.Run("differential: matches readISOFile byte-for-byte", func(t *testing.T) {
		t.Parallel()
		a := secByte(0xA1, 137)
		b := secByte(0xB2, 901)
		c := secByte(0xC3, 100)
		d := secByte(0xD4, 100)
		rs := makeImage(t, map[uint32][]byte{10: a, 11: b, 12: c, 13: d})
		entries := []isoFileEntry{
			mkEntry("one.MPLS", 10, uint64(len(a))),
			mkEntry("two.MPLS", 11, uint64(len(b))),
			// multi-extent file pulling from sectors 12 and 13
			{path: "three.MPLS", size: 200, extents: []isoExtent{{lba: 12, length: 100}, {lba: 13, length: 100}}},
		}
		got := readPlaylistsCoalesced(rs, entries)
		for _, e := range entries {
			want, err := readISOFile(rs, e)
			if err != nil {
				t.Fatalf("readISOFile(%s): %v", e.path, err)
			}
			if !bytes.Equal(got[e.path], want) {
				t.Errorf("%s: coalesced bytes differ from readISOFile", e.path)
			}
		}
	})

	t.Run("contiguous playlists trigger a single Seek", func(t *testing.T) {
		t.Parallel()
		pieces := map[uint32][]byte{}
		var entries []isoFileEntry
		// 50 adjacent small playlists in one contiguous region.
		for i := 0; i < 50; i++ {
			sect := uint32(100 + i)
			data := secByte(byte(i), 64)
			pieces[sect] = data
			entries = append(entries, mkEntry(fmt.Sprintf("BDMV/PLAYLIST/%05d.MPLS", i), sect, uint64(len(data))))
		}
		rs := &countingReadSeeker{rs: makeImage(t, pieces)}
		got := readPlaylistsCoalesced(rs, entries)
		if len(got) != 50 {
			t.Fatalf("got %d playlists, want 50", len(got))
		}
		if rs.seeks != 1 {
			t.Errorf("seeks = %d, want 1 (single coalesced run for contiguous playlists)", rs.seeks)
		}
	})
}
